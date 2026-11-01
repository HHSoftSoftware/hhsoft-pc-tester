package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

// This finite archive fixture follows the established independent bundle tests.
// It deliberately bypasses the production writer's semantic admission, while
// requiring a valid signature and exact member digests before testing readers.
func extendedMemorySignedArchive(t *testing.T, original bundle.Artifact, payload, html, pdf []byte) []byte {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x79}, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	keyID, err := bundle.SelfIssuedKeyID(public)
	if err != nil {
		t.Fatal(err)
	}
	type file struct {
		Name      string `json:"name"`
		MediaType string `json:"media_type"`
		Size      int64  `json:"size"`
		SHA256    string `json:"sha256"`
	}
	type member struct {
		name, media string
		data        []byte
	}
	members := []member{{"inventory.json", "application/json", payload}}
	if html != nil {
		members = append(members, member{"views/report.html", "text/html", html})
	}
	if pdf != nil {
		members = append(members, member{"views/report.pdf", "application/pdf", pdf})
	}
	members = append(members, member{bundle.SelfIssuedKeyName, bundle.SelfIssuedMediaType, public})
	manifest := struct {
		Schema string `json:"schema"`
		Files  []file `json:"files"`
	}{Schema: "hhsoft.pc-tester.bundle-manifest/v1"}
	for _, entry := range members {
		manifest.Files = append(manifest.Files, file{entry.name, entry.media, int64(len(entry.data)),
			fmt.Sprintf("sha256:%x", sha256.Sum256(entry.data))})
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	envelope := original.Envelope
	envelope.Payload, envelope.KeyID = payload, keyID
	envelope.ManifestDigest = fmt.Sprintf("sha256:%x", sha256.Sum256(manifestBytes))
	if report.Sign(&envelope, private) != nil || report.Verify(envelope, report.Keyring{keyID: public}) != nil {
		t.Fatal("independent signature prerequisite")
	}
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	write := func(name string, value []byte) {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0600)
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := writer.Write(value); err != nil || n != len(value) {
			t.Fatal("owned archive fixture write")
		}
	}
	write("report-manifest.json", manifestBytes)
	for _, entry := range members {
		write(entry.name, entry.data)
	}
	write("signatures/report-envelope.json", envelopeBytes)
	if archive.Close() != nil {
		t.Fatal("owned archive fixture close")
	}
	return output.Bytes()
}

func extendedMemoryRow(t *testing.T, payload *app.InventoryPayload) *app.TestResult {
	t.Helper()
	for i := range payload.TestResults {
		if payload.TestResults[i].ID == "memory.stress" {
			return &payload.TestResults[i]
		}
	}
	t.Fatal("memory row prerequisite absent")
	return nil
}

func extendedMemoryIntegrityChange(t *testing.T, payload *app.InventoryPayload, change func(*sessionevidence.IntegrityEvidence)) {
	t.Helper()
	for i := range payload.TestResults {
		row := &payload.TestResults[i]
		if row.ID != "run.integrity.quick" {
			continue
		}
		value, err := sessionevidence.DecodeIntegrity(row.Evidence)
		if err != nil || value.Session == nil {
			t.Fatal("strict integrity prerequisite")
		}
		change(&value)
		row.Evidence, err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("integrity row prerequisite absent")
}

func TestExtendedMemorySignedViewsAndCopiedAccessor(t *testing.T) {
	h := newExtendedMemoryHarness(t, commandMemoryPass())
	artifact := verifyExtendedMemoryHarness(t, h)
	payload := stressSignedPayload(t, artifact)
	if artifact.Envelope.Schema != report.EnvelopeSchemaV1 || artifact.Manifest.Schema != bundle.ManifestSchema ||
		payload.CoverageScope != app.CoverageFrozenPlan || len(payload.TestResults) != 23 {
		t.Fatal("ordinary envelope, catalog coverage or manifest changed")
	}
	value, err := app.ValidatedMemoryStress(payload)
	if err != nil || value == nil || value.LoadStartedMilliseconds == nil || *value.LoadStartedMilliseconds != 3 ||
		value.Coverage.LoadCycles != 1 || value.Coverage.CompletedCycles != 2 || value.Coverage.CompletedPhases != 20 ||
		value.Coverage.ReadBytes != 50331648 || value.Coverage.WrittenBytes != 50331648 || value.Coverage.TerminalReadBytes != 4194304 {
		t.Fatal("literal signed memory facts changed", err)
	}
	*value.LoadStartedMilliseconds = 999
	value.Coverage.CompletedCycles = 99
	again, err := app.ValidatedMemoryStress(payload)
	if err != nil || again == nil || *again.LoadStartedMilliseconds != 3 || again.Coverage.CompletedCycles != 2 {
		t.Fatal("accessor exposed mutable source aliases")
	}
	thermal, err := app.ValidatedSessionThermal(payload)
	if err != nil || thermal == nil || thermal.Scope != sessionevidence.ExtendedScope {
		t.Fatal("Extended report retained another profile's scope", err)
	}
	for _, view := range [][]byte{artifact.RenderedReport, artifact.RenderedPDF} {
		for _, phrase := range []string{"MEMORY STRESS", "1800000", "50331648", "4194304"} {
			if !bytes.Contains(view, []byte(phrase)) {
				t.Fatal("canonical view lost a bounded memory fact", phrase)
			}
		}
		if bytes.Contains(view, []byte("CPU STRESS (STANDARD PROFILE)")) {
			t.Fatal("Extended view invented Standard-only CPU stress")
		}
	}
	// The retained PDF writer wraps fields across literal text-showing lines.
	// Read those lines in order; this ASCII scope has no PDF-escaped characters.
	var pdfLines []string
	for _, line := range strings.Split(string(artifact.RenderedPDF), "\n") {
		starts, finishes := strings.HasPrefix(line, "("), strings.HasSuffix(line, ") Tj")
		if starts != finishes {
			t.Fatal("canonical PDF has malformed text-line framing")
		}
		if starts {
			pdfLines = append(pdfLines, line[1:len(line)-4])
		}
	}
	pdfText := strings.Join(strings.Fields(strings.Join(pdfLines, " ")), " ")
	const scope = "Repeated byte-pattern verification in one bounded working set. This is not full-RAM coverage, a memory-bandwidth measurement or proof of hardware stability beyond this run."
	if !bytes.Contains(artifact.RenderedReport, []byte(scope)) || !strings.Contains(pdfText, scope) {
		t.Fatal("canonical view lost the complete bounded memory scope")
	}
	for _, views := range []bool{false, true} {
		var html, pdf []byte
		if views {
			html, pdf = artifact.RenderedReport, artifact.RenderedPDF
		}
		raw := extendedMemorySignedArchive(t, artifact, artifact.InventoryJSON, html, pdf)
		if _, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw))); err != nil {
			t.Fatal("independent archive positive prerequisite", err)
		}
	}
}

func TestExtendedMemorySignedConsumersRejectIndependentForgeries(t *testing.T) {
	h := newExtendedMemoryHarness(t, commandMemoryPass())
	artifact := verifyExtendedMemoryHarness(t, h)
	positive := extendedMemorySignedArchive(t, artifact, artifact.InventoryJSON, nil, nil)
	if _, err := bundle.VerifySelfIssued(bytes.NewReader(positive), int64(len(positive))); err != nil {
		t.Fatal("no-view archive positive prerequisite", err)
	}
	for _, mode := range []string{"wrong_id", "case_id", "version", "planned_status", "outcome", "coverage",
		"schema_case", "future_schema", "duplicate_schema", "unknown_member", "mixed_cpu", "mixed_quick",
		"journal_schema", "journal_profile", "null_source", "typed_digest", "plan_binding", "graph_binding", "worker_binding", "counters"} {
		t.Run(mode, func(t *testing.T) {
			payload := stressSignedPayload(t, artifact)
			row := extendedMemoryRow(t, &payload)
			switch mode {
			case "wrong_id":
				row.ID = "memory.stress.extra"
			case "case_id":
				row.ID = "MEMORY.STRESS"
			case "version":
				row.Version = 1
			case "planned_status":
				row.PlannedStatus = app.PlannedExcluded
			case "outcome":
				row.Status = app.TestFail
			case "coverage":
				payload.CoverageScope = ""
			case "schema_case":
				row.Evidence = bytes.Replace(row.Evidence, []byte("hhsoft.pc-tester.memory-stress/v1"), []byte("HHSOFT.pc-tester.memory-stress/v1"), 1)
			case "future_schema":
				row.Evidence = bytes.Replace(row.Evidence, []byte("hhsoft.pc-tester.memory-stress/v1"), []byte("hhsoft.pc-tester.memory-stress/v99"), 1)
			case "duplicate_schema":
				row.Evidence = append([]byte(`{"schema":"hhsoft.pc-tester.memory-stress/v1",`), row.Evidence[1:]...)
			case "unknown_member", "mixed_cpu", "mixed_quick":
				field := `"unexpected_memory_field":true,`
				if mode == "mixed_cpu" {
					field = `"workers":[{"load_checks_passed":12}],`
				} else if mode == "mixed_quick" {
					field = `"passes":2,`
				}
				row.Evidence = append([]byte("{"+field), row.Evidence[1:]...)
			case "journal_schema", "journal_profile":
				found := false
				for i := range payload.TestResults {
					integrityRow := &payload.TestResults[i]
					if integrityRow.ID != "run.integrity.quick" {
						continue
					}
					value, err := sessionevidence.DecodeIntegrity(integrityRow.Evidence)
					if err != nil || value.Session == nil {
						t.Fatal("valid header prerequisite")
					}
					canonical, err := json.Marshal(value)
					if err != nil {
						t.Fatal("canonical positive serialization")
					}
					integrityRow.Evidence = canonical
					if app.ValidateCoverage(payload) != nil {
						t.Fatal("canonical positive lost admission")
					}
					oldHeader := []byte(`"schema":"hhsoft.pc-tester.quick-session/v16"`)
					newHeader := []byte(`"schema":"hhsoft.pc-tester.quick-session/v12"`)
					if mode == "journal_schema" {
						value.Session.Schema = "hhsoft.pc-tester.quick-session/v12"
					} else {
						oldHeader, newHeader = []byte(`"profile":"extended"`), []byte(`"profile":"standard"`)
						value.Session.Profile = "standard"
					}
					if _, err := json.Marshal(value); !errors.Is(err, sessionevidence.ErrInvalid) {
						t.Fatal("strict serializer admitted an invalid header")
					}
					if bytes.Count(canonical, oldHeader) != 1 {
						t.Fatal("wire header mutation prerequisite")
					}
					changed := bytes.Replace(canonical, oldHeader, newHeader, 1)
					if !json.Valid(changed) || bytes.Equal(changed, canonical) ||
						!bytes.Equal(bytes.Replace(changed, newHeader, oldHeader, 1), canonical) {
						t.Fatal("wire header mutation changed unrelated evidence")
					}
					integrityRow.Evidence = changed
					found = true
				}
				if !found {
					t.Fatal("integrity header mutation absent")
				}
			case "null_source":
				extendedMemoryIntegrityChange(t, &payload, func(v *sessionevidence.IntegrityEvidence) { v.Session.MemoryStressSource = nil })
			case "typed_digest":
				extendedMemoryIntegrityChange(t, &payload, func(v *sessionevidence.IntegrityEvidence) {
					v.Session.MemoryStressSource.EvidenceTypedSHA256 = "sha256:" + strings.Repeat("1", 64)
				})
			case "plan_binding":
				row.PlanID = "sha256:" + strings.Repeat("2", 64)
			case "graph_binding":
				row.HardwareGraph = "sha256:" + strings.Repeat("3", 64)
			case "worker_binding":
				extendedMemoryIntegrityChange(t, &payload, func(v *sessionevidence.IntegrityEvidence) { v.Session.Workers[2].Outcome = "inconclusive" })
			case "counters":
				var value map[string]json.RawMessage
				if json.Unmarshal(row.Evidence, &value) != nil {
					t.Fatal("coverage mutation prerequisite")
				}
				var coverage map[string]json.RawMessage
				if json.Unmarshal(value["coverage"], &coverage) != nil {
					t.Fatal("coverage member missing")
				}
				coverage["completed_cycles"] = json.RawMessage("3")
				coverage["completed_phases"] = json.RawMessage("30")
				coverage["read_bytes"], coverage["written_bytes"] = json.RawMessage("75497472"), json.RawMessage("75497472")
				value["coverage"], _ = json.Marshal(coverage)
				row.Evidence, _ = json.Marshal(value)
			}
			raw, err := json.Marshal(payload)
			if err != nil || bytes.Equal(raw, artifact.InventoryJSON) {
				t.Fatal("forgery mutation ineffective")
			}
			if app.ValidateCoverage(payload) == nil {
				t.Fatal("app accepted forged memory claim")
			}
			if _, err := render.InventoryHTML(payload); err == nil {
				t.Fatal("HTML rendered an unbound memory claim")
			}
			if _, err := render.InventoryPDF(payload); err == nil {
				t.Fatal("PDF rendered an unbound memory claim")
			}
			if value, err := app.ValidatedMemoryStress(payload); err == nil || value != nil {
				t.Fatal("accessor released an unbound memory claim")
			}
			forged := extendedMemorySignedArchive(t, artifact, raw, nil, nil)
			if _, err := bundle.VerifySelfIssued(bytes.NewReader(forged), int64(len(forged))); err == nil {
				t.Fatal("valid signature bypassed no-view semantic admission")
			}
		})
	}
}

func TestExtendedMemoryEscapedSchemaRetainsStrictTypedBinding(t *testing.T) {
	h := newExtendedMemoryHarness(t, commandMemoryPass())
	artifact := verifyExtendedMemoryHarness(t, h)
	payload := stressSignedPayload(t, artifact)
	row := extendedMemoryRow(t, &payload)
	original := append([]byte(nil), row.Evidence...)
	row.Evidence = bytes.Replace(original, []byte("hhsoft.pc-tester.memory-stress/v1"), []byte(`\u0068hsoft.pc-tester.memory-stress/v1`), 1)
	_, first, firstErr := quickevidence.DecodeMemoryStress(original)
	_, second, secondErr := quickevidence.DecodeMemoryStress(row.Evidence)
	if bytes.Equal(original, row.Evidence) || firstErr != nil || secondErr != nil || first != second || app.ValidateCoverage(payload) != nil {
		t.Fatal("effective escaped semantic-equality prerequisite")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	encoded := extendedMemorySignedArchive(t, artifact, raw, nil, nil)
	if _, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded))); err != nil {
		t.Fatal("escaped valid memory claim rejected", err)
	}
	row.Evidence = bytes.Replace(row.Evidence, []byte("/v1"), []byte("/v99"), 1)
	if app.ValidateCoverage(payload) == nil {
		t.Fatal("escaped future family escaped strict decoding")
	}
}

func TestExtendedMemoryNoViewCannotHideStrippedClaimFields(t *testing.T) {
	h := newExtendedMemoryHarness(t, commandMemoryPass())
	artifact := verifyExtendedMemoryHarness(t, h)
	for _, mode := range []string{"stripped", "escaped_id", "case_id"} {
		t.Run(mode, func(t *testing.T) {
			payload := stressSignedPayload(t, artifact)
			row := *extendedMemoryRow(t, &payload)
			row.Evidence = json.RawMessage(`{}`)
			row.PlanID, row.HardwareGraph, row.PlannedStatus = "", "", ""
			if mode == "case_id" {
				row.ID = "MEMORY.STRESS"
			}
			payload.TestResults, payload.CoverageScope = []app.TestResult{row}, ""
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]json.RawMessage
			if json.Unmarshal(raw, &object) != nil {
				t.Fatal("inventory-only prerequisite")
			}
			delete(object, "schema_version")
			delete(object, "coverage_scope")
			raw, err = json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "escaped_id" {
				before := append([]byte(nil), raw...)
				raw = bytes.Replace(raw, []byte(`"memory.stress"`), []byte(`"\u006demory.stress"`), 1)
				if bytes.Equal(before, raw) {
					t.Fatal("escaped ID mutation ineffective")
				}
			}
			if app.HasMemoryStressClaim(raw) || !app.HasMemoryStressReference(raw) || bytes.Contains(raw, []byte("memory_stress_source")) {
				t.Fatal("reference-only fast-path prerequisite not established")
			}
			forged := extendedMemorySignedArchive(t, artifact, raw, nil, nil)
			if _, err := bundle.VerifySelfIssued(bytes.NewReader(forged), int64(len(forged))); err == nil {
				t.Fatal("stripped inventory-only bundle hid observed memory row")
			}
		})
	}
}

func TestExtendedMemoryCanonicalViewsRejectResignedTampering(t *testing.T) {
	h := newExtendedMemoryHarness(t, commandMemoryPass())
	artifact := verifyExtendedMemoryHarness(t, h)
	for _, mode := range []string{"html", "pdf"} {
		t.Run(mode, func(t *testing.T) {
			html, pdf := append([]byte(nil), artifact.RenderedReport...), append([]byte(nil), artifact.RenderedPDF...)
			if mode == "html" {
				html = append(html, []byte("\n<!-- altered memory statement -->")...)
			} else {
				pdf = append(pdf, []byte("\n% altered memory statement")...)
			}
			raw := extendedMemorySignedArchive(t, artifact, artifact.InventoryJSON, html, pdf)
			if _, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw))); err == nil {
				t.Fatal("resigned noncanonical memory view accepted")
			}
		})
	}
}

func TestExtendedMemoryLinkedRetestOwnsItsOutcomeAndParent(t *testing.T) {
	parentHarness := newExtendedMemoryHarness(t, commandMemoryPass())
	parent := verifyExtendedMemoryHarness(t, parentHarness)
	parentRaw, err := os.ReadFile(filepath.Join(parentHarness.directory, "session.hhpcr"))
	if err != nil {
		t.Fatal("parent source prerequisite")
	}
	childHarness := newExtendedMemoryHarness(t, commandMemoryNoLoad())
	parentPath := filepath.Join(childHarness.directory, "parent.hhpcr")
	if os.WriteFile(parentPath, parentRaw, 0600) != nil {
		t.Fatal("owned parent fixture")
	}
	childHarness.arguments = append(childHarness.arguments, "-previous-report", parentPath, "-previous-self-issued")
	child := verifyExtendedMemoryHarness(t, childHarness)
	want := report.PreviousReport{ManifestDigest: parent.Envelope.ManifestDigest, BundleSHA256: fmt.Sprintf("sha256:%x", parent.BundleSHA256)}
	if child.Manifest.Schema != bundle.RetestManifestSchema || child.Manifest.PreviousReport == nil ||
		*child.Manifest.PreviousReport != want || child.Manifest.RevisionKind != "retest" ||
		child.Envelope.SessionID == parent.Envelope.SessionID {
		t.Fatal("Extended revision lost its exact supplied parent")
	}
	value, err := app.ValidatedMemoryStress(stressSignedPayload(t, child))
	if err != nil || value == nil || value.Status != "inconclusive" || value.LoadStartedMilliseconds != nil {
		t.Fatal("child inherited the parent's completed load")
	}
	var out, diagnostic bytes.Buffer
	args := []string{"-self-issued", "-previous-report", parentPath, "-previous-self-issued", filepath.Join(childHarness.directory, "session.hhpcr")}
	if code := runVerifyWithInput(t.Context(), args, &out, &diagnostic, captureFixtureInput(t)); code != 0 ||
		!strings.Contains(out.String(), "exact supplied parent matched") {
		t.Fatal("real pair verifier rejected Extended revision", code)
	}
}

func TestExtendedMemoryQuickAndStandardKeepLiteralSessionAuthority(t *testing.T) {
	for _, profile := range []string{"quick", "standard"} {
		t.Run(profile, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			useRealSessionFinalizer(t, h, nil, nil)
			wantSchema, wantCount, wantBudget := "hhsoft.pc-tester.quick-session/v14", 14, int64(300000)
			if profile == "standard" {
				h = newStandardHarness(t, commandStressNoLoad())
				wantSchema, wantCount, wantBudget = "hhsoft.pc-tester.quick-session/v15", 15, 900000
			}
			artifact := verifyExtendedMemoryHarness(t, h)
			payload := stressSignedPayload(t, artifact)
			value, err := app.ValidatedMemoryStress(payload)
			journal, marshalErr := json.Marshal(h.journal)
			if err != nil || value != nil || marshalErr != nil || h.journal.Schema != wantSchema ||
				len(h.journal.Workers) != wantCount || h.journal.BudgetMilliseconds != wantBudget ||
				bytes.Contains(journal, []byte("memory_stress_source")) {
				t.Fatal("existing profile gained Extended authority")
			}
			for _, view := range [][]byte{artifact.RenderedReport, artifact.RenderedPDF} {
				if bytes.Contains(view, []byte("MEMORY STRESS (EXTENDED PROFILE)")) {
					t.Fatal("historical profile gained an observed memory section")
				}
			}
		})
	}
}
