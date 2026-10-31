package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func stressSignedPayload(t *testing.T, artifact bundle.Artifact) app.InventoryPayload {
	t.Helper()
	var payload app.InventoryPayload
	if json.Unmarshal(artifact.InventoryJSON, &payload) != nil || app.ValidateCoverage(payload) != nil {
		t.Fatal("signed standard payload rejected")
	}
	return payload
}

func TestCPUStressSignedStandardViewsAndCopiedAccessor(t *testing.T) {
	h := newStandardHarness(t, commandStressPass())
	artifact := verifyStandardHarness(t, h)
	if artifact.Envelope.Schema != report.EnvelopeSchemaV1 || artifact.Manifest.Schema != bundle.ManifestSchema {
		t.Fatal("ordinary envelope or manifest changed")
	}
	payload := stressSignedPayload(t, artifact)
	if len(payload.TestResults) != 23 || payload.CoverageScope != app.CoverageFrozenPlan {
		t.Fatal("current catalog coverage missing")
	}
	value, err := app.ValidatedCPUStress(payload)
	if err != nil || value == nil || value.ObservedLoadMilliseconds != 600000 || value.Workers[0].LoadChecksPassed != 12 {
		t.Fatal("signed stress participation changed", err)
	}
	original := *value.Workers[0].ReadyMilliseconds
	*value.Workers[0].ReadyMilliseconds = 999
	value.Workers[0].LoadChecksPassed = 0
	again, err := app.ValidatedCPUStress(payload)
	if err != nil || again == nil || *again.Workers[0].ReadyMilliseconds != original || again.Workers[0].LoadChecksPassed != 12 {
		t.Fatal("accessor returned aliased evidence")
	}
	thermal, err := app.ValidatedSessionThermal(payload)
	if err != nil || thermal == nil || thermal.Scope != sessionevidence.StandardScope {
		t.Fatal("standard report retained quick scope")
	}
	for _, view := range [][]byte{artifact.RenderedReport, artifact.RenderedPDF} {
		for _, phrase := range []string{"CPU STRESS", "Standard-session execution", "600000", "Checked arithmetic during a shared elapsed load window."} {
			if !bytes.Contains(view, []byte(phrase)) {
				t.Fatal("canonical view lost standard stress evidence", phrase)
			}
		}
	}
	for _, row := range payload.TestResults {
		if row.ID == "run.integrity.quick" {
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if err != nil || value.Session == nil || value.Session.Schema != sessionevidence.SchemaV12 ||
				value.Session.Profile != "standard" || value.Session.CPUStressSource == nil || len(value.Session.Workers) != 15 {
				t.Fatal("full strict standard journal missing")
			}
		}
	}
}

func TestCPUStressSignedNoViewUsesSameStrictCoverage(t *testing.T) {
	h := newStandardHarness(t, commandStressNoLoad())
	artifact := verifyStandardHarness(t, h)
	payload := stressSignedPayload(t, artifact)
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x67}, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	id, err := bundle.SelfIssuedKeyID(public)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := bundle.SelfIssuedManifestDigestWithPDF(artifact.InventoryJSON, nil, nil, public)
	if err != nil {
		t.Fatal(err)
	}
	envelope := artifact.Envelope
	envelope.ManifestDigest, envelope.KeyID = digest, id
	if report.Sign(&envelope, private) != nil || report.Verify(envelope, report.Keyring{id: public}) != nil {
		t.Fatal("independent no-view signature prerequisite")
	}
	var output bytes.Buffer
	if err := bundle.WriteSelfIssuedWithPDF(&output, artifact.InventoryJSON, envelope, nil, nil, public); err != nil {
		t.Fatal(err)
	}
	got, err := bundle.VerifySelfIssued(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil || len(got.RenderedReport) != 0 || len(got.RenderedPDF) != 0 || !bytes.Equal(got.InventoryJSON, artifact.InventoryJSON) {
		t.Fatal("no-view verification changed coverage", err)
	}
	view, err := app.ValidatedCPUStress(payload)
	if err != nil || view == nil || view.Status != "inconclusive" || view.LoadStarted {
		t.Fatal("no-load signed evidence lost")
	}
}

func TestCPUStressLinkedStandardKeepsExactParentAndOwnOutcome(t *testing.T) {
	parentHarness := newStandardHarness(t, commandStressPass())
	parent := verifyStandardHarness(t, parentHarness)
	parentRaw, err := os.ReadFile(filepath.Join(parentHarness.directory, "session.hhpcr"))
	if err != nil {
		t.Fatal("parent fixture")
	}
	childHarness := newStandardHarness(t, commandStressNoLoad())
	parentPath := filepath.Join(childHarness.directory, "parent.hhpcr")
	if os.WriteFile(parentPath, parentRaw, 0600) != nil {
		t.Fatal("parent fixture")
	}
	childHarness.arguments = append(childHarness.arguments, "-previous-report", parentPath, "-previous-self-issued")
	child := verifyStandardHarness(t, childHarness)
	want := report.PreviousReport{ManifestDigest: parent.Envelope.ManifestDigest, BundleSHA256: fmt.Sprintf("sha256:%x", parent.BundleSHA256)}
	if child.Envelope.Schema != report.EnvelopeSchemaV1 || child.Manifest.Schema != bundle.RetestManifestSchema ||
		child.Manifest.PreviousReport == nil || *child.Manifest.PreviousReport != want ||
		child.Envelope.SessionID == parent.Envelope.SessionID || child.Manifest.RevisionKind != "retest" {
		t.Fatal("linked standard manifest or exact parent changed")
	}
	childPayload := stressSignedPayload(t, child)
	value, err := app.ValidatedCPUStress(childPayload)
	if err != nil || value == nil || value.Status != "inconclusive" || value.LoadStarted {
		t.Fatal("child inherited parent stress pass")
	}
	for _, view := range [][]byte{child.RenderedReport, child.RenderedPDF} {
		if !bytes.Contains(view, []byte(want.ManifestDigest)) || !bytes.Contains(view, []byte(want.BundleSHA256)) {
			t.Fatal("linked view lost complete parent digests")
		}
	}
	var out, diagnostic bytes.Buffer
	args := []string{"-self-issued", "-previous-report", parentPath, "-previous-self-issued", filepath.Join(childHarness.directory, "session.hhpcr")}
	if code := runVerifyWithInput(t.Context(), args, &out, &diagnostic, captureFixtureInput(t)); code != 0 ||
		!strings.Contains(out.String(), "exact supplied parent matched") {
		t.Fatal("pair verifier rejected exact parent", code)
	}
}

func TestCPUStressUnstartedSignedSourceCannotBeInventedByView(t *testing.T) {
	h := newStandardHarness(t, commandStressPass())
	run := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
		if worker.ID != "cpu.quick" {
			return run(ctx, worker, input, out, diagnostic)
		}
		h.calls = append(h.calls, worker.ID)
		writeSessionFixture(t, worker.OutputPath, cpuScalarFailureFixture(0, false))
		return 0
	}
	artifact := verifyStandardHarness(t, h)
	payload := stressSignedPayload(t, artifact)
	value, err := app.ValidatedCPUStress(payload)
	if err != nil || value != nil || !reflect.DeepEqual(h.calls, []string{"cpu.quick"}) {
		t.Fatal("unstarted stress became observed evidence")
	}
	for _, view := range [][]byte{artifact.RenderedReport, artifact.RenderedPDF} {
		if bytes.Contains(view, []byte("CPU STRESS (STANDARD PROFILE)")) {
			t.Fatal("unstarted note invented stress section")
		}
	}
}
