package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/comparison"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

type inspectionCoordinatorInput struct {
	t                         *testing.T
	h                         *sessionHarness
	line                      string
	readErr, discardErr       error
	cancel                    context.CancelFunc
	discards, reads, finishes int
	events                    []string
}

func (f *inspectionCoordinatorInput) Read([]byte) (int, error) {
	f.t.Fatal("coordinator used an unbounded input read")
	return 0, io.ErrNoProgress
}
func (f *inspectionCoordinatorInput) DiscardUnpromptedInput(ctx context.Context) error {
	f.discards++
	f.events = append(f.events, "discard")
	if ctx.Err() != nil || f.reads != 0 || strings.Contains(f.h.stderr.String(), "Visible exterior condition") {
		f.t.Fatal("actual coordinator handoff did not precede prompt/read")
	}
	return f.discardErr
}
func (f *inspectionCoordinatorInput) ReadInspectionConditions(ctx context.Context, prompt func() error) ([]exteriorinspection.ConditionResponseV2, error) {
	f.reads++
	f.events = append(f.events, "read")
	_, bounded := ctx.Deadline()
	if f.discards != 1 || f.finishes != 0 || !bounded || prompt == nil {
		f.t.Fatal("actual command did not consume the bounded borrowed owner")
	}
	if err := prompt(); err != nil {
		return nil, err
	}
	if !strings.Contains(f.h.stderr.String(), "Visible exterior condition") {
		f.t.Fatal("single four-criterion prompt missing")
	}
	if f.cancel != nil {
		f.cancel()
	}
	responses := []exteriorinspection.ConditionResponseV2{{Text: f.line, Complete: true}}
	if f.readErr == nil {
		responses = append(responses, exteriorinspection.ConditionResponseV2{Text: "A", Complete: true},
			exteriorinspection.ConditionResponseV2{Text: "N", Complete: true}, exteriorinspection.ConditionResponseV2{Text: "N", Complete: true})
	}
	return responses, f.readErr
}

func newInspectionCoordinator(t *testing.T) (*sessionHarness, *inspectionCoordinatorInput) {
	t.Helper()
	ids, err := plan.SelectableQuickTests("x86_64")
	if err != nil {
		t.Fatal(err)
	}
	var excluded []string
	for _, id := range ids {
		if id != "inspection.exterior.quick" {
			excluded = append(excluded, id)
		}
	}
	h := newSessionHarness(t, "x86_64", excluded)
	f := &inspectionCoordinatorInput{t: t, h: h, line: "N"}
	h.dependencies.Now = func() time.Time { h.clock = time.Now(); return h.clock }
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
		if worker.ID != "inspection.exterior.quick" || worker.Version != 2 || filepath.Base(worker.OutputPath) != "exterior-condition.json" || worker.Timeout != 45*time.Second || input != f || len(h.calls) != 0 {
			t.Fatal("unrelated or duplicate worker entered")
		}
		h.calls = append(h.calls, worker.ID)
		return runSessionWorker(ctx, worker, input, out, diagnostic)
	}
	h.dependencies.FinishInput = func() error {
		f.finishes++
		f.events = append(f.events, "finish")
		h.inputFinished = true
		if f.finishes != 1 {
			t.Fatal("input owner was finished twice")
		}
		return nil
	}
	return h, f
}

func inspectSignedCoordinator(t *testing.T, h *sessionHarness, wantStatus, wantFinding, wantStop string, wantCount int) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.directory, "session.hhpcr"))
	if err != nil {
		t.Fatal("actual signed report missing")
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal("actual signed report failed verification", err)
	}
	var payload app.InventoryPayload
	if json.Unmarshal(artifact.InventoryJSON, &payload) != nil || len(payload.TestResults) != 23 {
		t.Fatal("full current catalog not signed")
	}
	got, err := app.ValidatedExteriorCondition(payload)
	if err != nil || got == nil || got.Status != wantStatus || got.Findings.EnclosureIntegrity != wantFinding || got.StopReason != wantStop || got.ResponsesCompleted != wantCount {
		t.Fatal("signed finite inspection changed", err)
	}
	if wantCount == 4 {
		if got.Findings.HingeMountCondition != "not_applicable" || got.Findings.PortExteriorCondition != "no_issue_observed" || got.Findings.SurfaceFinishCondition != "no_issue_observed" {
			t.Fatal("completed criteria changed")
		}
	} else if got.Findings.HingeMountCondition != "unobserved" || got.Findings.PortExteriorCondition != "unobserved" || got.Findings.SurfaceFinishCondition != "unobserved" {
		t.Fatal("partial response invented later observations")
	}
	evidence, err := os.ReadFile(filepath.Join(h.directory, "exterior-condition.json"))
	if err != nil {
		t.Fatal(err)
	}
	found, integrity := false, false
	for _, row := range payload.TestResults {
		if row.ID == "inspection.exterior.quick" {
			found = true
			var signed, worker exteriorinspection.ResultV2
			if json.Unmarshal(row.Evidence, &signed) != nil || json.Unmarshal(evidence, &worker) != nil || signed != worker ||
				row.Version != 2 || row.PlannedStatus != app.PlannedScheduled || string(row.Status) != wantStatus {
				t.Fatal("worker evidence lost at finalizer")
			}
		}
		if row.ID == "run.integrity.quick" {
			integrity = true
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if err != nil || value.Session == nil || value.Session.Schema != sessionevidence.SchemaV14 || len(value.Session.Workers) != 14 {
				t.Fatal("actual v14 session absent", err)
			}
			last := value.Session.Workers[13]
			if last.ID != "inspection.exterior.quick" || last.Outcome != wantStatus || last.Timing == nil ||
				got.ObservationMilliseconds > last.Timing.FinishedMilliseconds-last.Timing.StartedMilliseconds {
				t.Fatal("observation detached from actual worker interval")
			}
		}
	}
	if !found || !integrity {
		t.Fatal("signed rows absent")
	}
	for _, view := range [][]byte{artifact.RenderedReport, artifact.RenderedPDF} {
		if !bytes.Contains(view, []byte(wantFinding)) || !bytes.Contains(view, []byte(wantStop)) {
			t.Fatal("signed views lost finding or stop reason")
		}
	}
	ready, err := os.ReadFile(filepath.Join(h.directory, "session-ready.txt"))
	if err != nil || !bytes.Contains(ready, []byte("exterior_inspection="+wantStatus)) ||
		!strings.Contains(h.stdout.String(), "HHPT_EXTERIOR_INSPECTION status="+wantStatus) {
		t.Fatal("readiness/progress lost inspection outcome")
	}
	for _, private := range [][]byte{bytes.Repeat([]byte{0x31}, 32), []byte(h.directory), []byte("hard fixture input")} {
		if bytes.Contains(artifact.InventoryJSON, private) {
			t.Fatal("input key/path/fault leaked into signed evidence")
		}
	}
}

func TestInspectionRealCoordinatorSignsFiniteWorkerEvidence(t *testing.T) {
	for _, test := range []struct {
		name, line, status, finding, stop string
		readErr                           error
		cancel                            bool
	}{
		{"pass", "N", "pass", "no_issue_observed", "completed", nil, false},
		{"defect", "D", "fail", "issue_present", "completed", nil, false},
		{"uncertain", "I", "inconclusive", "unobserved", "completed", nil, false},
		{"cancel_favorable", "N", "aborted", "unobserved", "operator_cancelled", context.Canceled, true},
		{"cancel_defect", "D", "fail", "issue_present", "operator_cancelled", context.Canceled, true},
		{"hard_defect", "D", "error", "issue_present", "input_error", errors.New("hard fixture input"), false},
		{"hard_cancel_defect", "D", "error", "issue_present", "input_error", errors.Join(errors.New("hard fixture input"), context.Canceled), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, f := newInspectionCoordinator(t)
			f.line, f.readErr = test.line, test.readErr
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				f.cancel = cancel
			}
			collects, gates := 0, 0
			useRealSessionFinalizer(t, h, func(ctx context.Context) (inventory.Snapshot, error) {
				if ctx.Err() != nil || f.finishes != 1 {
					t.Fatal("collection inherited stopped worker or overlapped owner")
				}
				collects++
				f.events = append(f.events, "collect")
				return h.snapshot, nil
			}, nil)
			finalize := h.dependencies.Finalize
			h.dependencies.Finalize = func(ctx context.Context, request sessionFinalization, out, diagnostic io.Writer) sessionFinalizationResult {
				deadline, bounded := ctx.Deadline()
				remaining := time.Until(deadline)
				if ctx.Err() != nil || f.finishes != 1 || !bounded || remaining <= 0 || remaining > 30*time.Second || request.Gate == nil {
					t.Fatal("real finalizer did not receive fresh bounded integrity context")
				}
				gate := request.Gate
				request.Gate = func(gateContext context.Context) (sessionJournal, error) {
					gates++
					if gates != 1 || gateContext != ctx || gateContext.Err() != nil || f.finishes != 1 {
						t.Fatal("actual live gate escaped its single fresh-context boundary")
					}
					return gate(gateContext)
				}
				f.events = append(f.events, "finalize")
				return finalize(ctx, request, out, diagnostic)
			}
			code := runSessionWith(ctx, h.arguments, f, &h.stdout, &h.stderr, h.dependencies)
			if code != 0 || !h.finalized || f.discards != 1 || f.reads != 1 || f.finishes != 1 || collects != 1 || gates != 1 ||
				!reflect.DeepEqual(f.events, []string{"discard", "read", "finish", "finalize", "collect"}) {
				t.Fatalf("complete coordinator chain failed code=%d: %s", code, &h.stderr)
			}
			wantCount := 4
			if test.readErr != nil {
				wantCount = 1
			}
			inspectSignedCoordinator(t, h, test.status, test.finding, test.stop, wantCount)
		})
	}
}

func TestInspectionCoordinatorRejectsOwnerFailureBeforeFinalization(t *testing.T) {
	for _, test := range []struct {
		name                  string
		discardErr, finishErr error
		code, reads           int
	}{
		{"handoff_cleanup", errSessionInputCleanup, nil, workerCleanupUncertainExitCode, 0},
		{"finish_failure", nil, errSessionInput, 1, 1},
		{"finish_cleanup", nil, errSessionInputCleanup, workerCleanupUncertainExitCode, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, f := newInspectionCoordinator(t)
			f.discardErr = test.discardErr
			h.dependencies.FinishInput = func() error { f.finishes++; h.inputFinished = true; return test.finishErr }
			h.dependencies.Finalize = func(context.Context, sessionFinalization, io.Writer, io.Writer) sessionFinalizationResult {
				t.Fatal("unclosed or failed owner reached finalization")
				return sessionFinalizationResult{Code: 1}
			}
			code := runSessionWith(t.Context(), h.arguments, f, &h.stdout, &h.stderr, h.dependencies)
			if code != test.code || f.discards != 1 || f.reads != test.reads || f.finishes != 1 {
				t.Fatal("owner failure boundary changed", code)
			}
			for _, name := range []string{"session.hhpcr", "session-ready.txt"} {
				if _, err := os.Lstat(filepath.Join(h.directory, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("owner failure acquired signed/readiness credit")
				}
			}
		})
	}
}

func TestInspectionFinalizerRejectsExistingForgeryAfterValidPrerequisite(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  func([]byte) []byte
		outcome string
	}{
		{"malformed", func([]byte) []byte { return []byte("{") }, "error"},
		{"oversized", func([]byte) []byte { return bytes.Repeat([]byte(" "), 4097) }, "error"},
		{"directory", func(b []byte) []byte { return bytes.Clone(b) }, "error"},
		{"unknown_schema", func(b []byte) []byte { return bytes.Replace(b, []byte("inspection/v2"), []byte("inspection/v9"), 1) }, "error"},
		{"duplicate_finding", func(b []byte) []byte {
			return bytes.Replace(b, []byte("\"enclosure_integrity\":"), []byte("\"enclosure_integrity\":\"unobserved\",\"enclosure_integrity\":"), 1)
		}, "error"},
		{"defect_promoted", func(b []byte) []byte {
			return bytes.Replace(b, []byte("no_issue_observed"), []byte("issue_present"), 1)
		}, "pass"},
		{"wrong_supervisor_status", func(b []byte) []byte { return bytes.Clone(b) }, "error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newJournalFixture(t, "inspection.exterior.quick")
			value := inspectionCurrentConditionValue(t, "no_issue_observed", "pass")
			f.worker(t, "inspection.exterior.quick", "completed", "pass", value)
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			// The same full producer path must accept the positive before it is mutated.
			positive := filepath.Join(f.directory, "positive.hhpcr")
			args := f.args(t)
			for i := range args {
				if args[i] == "-output" {
					args[i+1] = positive
				}
			}
			collect := func(context.Context) (inventory.Snapshot, error) { return f.snapshot, nil }
			var out, diagnostic bytes.Buffer
			gateCalls := 0
			gate := func(context.Context) (sessionJournal, error) {
				gateCalls++
				return sessionevidence.CloneJournal(f.journal), nil
			}
			if code := runFinalizeTestOwned(t, t.Context(), args, &out, &diagnostic, collect, bytes.NewReader(bytes.Repeat([]byte{0x55}, 128)), time.Now, gate).Code; code != 0 || gateCalls != 1 {
				t.Fatalf("positive full finalizer prerequisite failed: %d %s", code, &diagnostic)
			}
			valid, err := os.ReadFile(positive)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := bundle.VerifySelfIssued(bytes.NewReader(valid), int64(len(valid))); err != nil {
				t.Fatal("positive signature prerequisite", err)
			}
			changed := test.change(encoded)
			if test.name != "wrong_supervisor_status" && test.name != "directory" && bytes.Equal(changed, encoded) {
				t.Fatal("fixture mutation missed target")
			}
			path := filepath.Join(f.directory, "exterior-condition.json")
			if test.name == "directory" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, changed, 0600); err != nil {
				t.Fatal(err)
			}
			for i := range f.journal.Workers {
				if f.journal.Workers[i].ID == "inspection.exterior.quick" {
					f.journal.Workers[i].Outcome = test.outcome
				}
			}
			args = f.args(t)
			out.Reset()
			diagnostic.Reset()
			gateCalls = 0
			entropy := bytes.NewReader(bytes.Repeat([]byte{0x56}, 128))
			if code := runFinalizeTestOwned(t, t.Context(), args, &out, &diagnostic, collect, entropy, time.Now, gate).Code; code != 1 ||
				!strings.Contains(diagnostic.String(), "validate inspection evidence:") || entropy.Len() != 128 || gateCalls != 0 {
				t.Fatal("present forgery became a generic supervisor fallback", code)
			}
			if _, err := os.Lstat(f.reportPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("forgery acquired signed output")
			}
		})
	}
}

func TestInspectionCompareCommandPreservesIndependentComponentClassification(t *testing.T) {
	const partID = "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af"
	const systemID = "hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449"
	const keyID = "hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44"
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"wwid": "fixture-exterior-disk", "model": "Fixture disk"})})
	key := bytes.Repeat([]byte{0x39}, 32)
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x53}, 32))
	public := private.Public().(ed25519.PublicKey)
	for _, state := range []struct{ name, finding, status, stop string }{
		{"pass", "no_defect_observed", "pass", "completed"},
		{"defect", "defect_observed", "fail", "completed"},
		{"hard_defect", "defect_observed", "error", "input_error"},
	} {
		t.Run(state.name, func(t *testing.T) {
			var archives [][]byte
			var refs []comparison.ReportReference
			for index := 0; index < 2; index++ {
				finding, status, stop := "no_defect_observed", "pass", "completed"
				if index == 1 {
					finding, status, stop = state.finding, state.status, state.stop
				}
				value := exteriorinspection.Result{Schema: exteriorinspection.Schema, Source: "operator_reported", Scope: "whole_device", Criterion: "visible_exterior_enclosure_damage", Finding: finding, ResponseComplete: true, StopReason: stop, Status: status, ObservationMilliseconds: 1}
				raw, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				rows := []app.TestResult{
					{ID: exteriorinspection.TestID, Version: 1, Status: app.TestStatus(status), PlannedStatus: app.PlannedScheduled, PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: raw},
					{ID: "run.integrity.quick", Version: 1, Status: app.TestPass, PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: json.RawMessage("{\"status\":\"pass\"}")},
				}
				session := "before-inspection"
				if index == 1 {
					session = "after-inspection"
				}
				encoded, err := captureBundle(snapshot, rows, key, "inspection-fixture", private, "fixture-signer", session, time.Unix(int64(index+1), 0), app.CoverageFrozenPlan)
				if err != nil {
					t.Fatal(err)
				}
				artifact, err := bundle.Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{"fixture-signer": public})
				if err != nil {
					t.Fatal(err)
				}
				var payload app.InventoryPayload
				if json.Unmarshal(artifact.InventoryJSON, &payload) != nil || len(payload.Components) != 1 ||
					payload.Components[0].Identity.Value != partID || payload.SystemFingerprint.Value != systemID || payload.SystemFingerprint.KeyID != keyID {
					t.Fatal("independent literal identity prerequisite")
				}
				archives = append(archives, encoded)
				refs = append(refs, comparison.ReportReference{SessionID: session, GeneratedTime: artifact.Envelope.GeneratedTime, SoftwareVersion: artifact.Envelope.Version, SignerKeyID: "fixture-signer", ManifestDigest: artifact.Envelope.ManifestDigest, SystemFingerprint: payload.SystemFingerprint, ComponentCount: 1})
			}
			reads := []string{}
			reader := func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
				if ctx != t.Context() {
					t.Fatal("compare context changed")
				}
				reads = append(reads, path)
				switch path {
				case "before":
					if kind != retestArtifactInput {
						t.Fatal("before role")
					}
					return bytes.Clone(archives[0]), nil
				case "after":
					if kind != retestArtifactInput {
						t.Fatal("after role")
					}
					return bytes.Clone(archives[1]), nil
				case "key":
					if kind != retestPublicKeyInput {
						t.Fatal("key role")
					}
					return bytes.Clone(public), nil
				default:
					t.Fatal("unexpected input")
					return nil, errRetestInput
				}
			}
			var out, diagnostic bytes.Buffer
			if code := runCompareDestination(t.Context(), []string{"-public-key", "key", "-key-id", "fixture-signer", "before", "after"}, &out, &diagnostic, writeDestination, reader); code != 0 {
				t.Fatal("actual compare command failed", code)
			}
			component := comparison.Component{Type: inventory.ComponentBlock, ID: "block:sda", IdentitySource: "wwid", Identity: partID, IdentityConfidence: "durable", Attributes: []inventory.Attribute{{Name: "model", Value: "Fixture disk"}}}
			want := comparison.Result{Schema: "hhsoft.pc-tester.hardware-comparison/v1", Policy: "hhsoft.parts-swap.v1", Status: comparison.StatusUnchanged, Comparable: true, Complete: true, Before: refs[0], After: refs[1], Summary: comparison.Summary{Matched: 1}, Matched: []comparison.Match{{Before: component, After: component, LocationChange: false}}}
			var got comparison.Result
			if json.Unmarshal(out.Bytes(), &got) != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(reads, []string{"before", "key", "after", "key"}) {
				t.Fatal("command changed identity policy/references or input roles")
			}
		})
	}
}

func TestInspectionLinkedFinalizerAndPairVerifyUseExactIndependentParent(t *testing.T) {
	for _, test := range []struct {
		name, finding, status string
		selfParent            bool
	}{
		{"trusted_pass", "no_issue_observed", "pass", false},
		{"trusted_defect", "issue_present", "fail", false},
		{"self_issued_pass", "no_issue_observed", "pass", true},
		{"self_issued_defect", "issue_present", "fail", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newJournalFixture(t, "inspection.exterior.quick")
			value := inspectionCurrentConditionValue(t, test.finding, test.status)
			f.worker(t, "inspection.exterior.quick", "completed", test.status, value)
			private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x73}, 32))
			public := private.Public().(ed25519.PublicKey)
			// Independent historical v1 parent remains unchanged by current child authority.
			parentValue := exteriorinspection.Result{Schema: exteriorinspection.Schema, Source: "operator_reported", Scope: "whole_device", Criterion: "visible_exterior_enclosure_damage", Finding: "no_defect_observed", ResponseComplete: true, StopReason: "completed", Status: "pass", ObservationMilliseconds: 1}
			parentEvidence, err := json.Marshal(parentValue)
			if err != nil {
				t.Fatal(err)
			}
			rows := []app.TestResult{
				{ID: "inspection.exterior.quick", Version: 1, Status: app.TestPass, PlannedStatus: app.PlannedScheduled, PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: parentEvidence},
				{ID: "run.integrity.quick", Version: 1, Status: app.TestPass, PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: json.RawMessage("{\"status\":\"pass\"}")},
			}
			createParent := func(id string) []byte {
				var raw []byte
				var err error
				if test.selfParent {
					raw, err = captureSelfIssuedBundle(f.snapshot, rows, bytes.Repeat([]byte{0x57}, 32), "ephemeral-session", private, public, id, time.Unix(1, 0), app.CoverageFrozenPlan)
				} else {
					raw, err = captureBundle(f.snapshot, rows, bytes.Repeat([]byte{0x57}, 32), "ephemeral-session", private, "inspection-parent-key", id, time.Unix(1, 0), app.CoverageFrozenPlan)
				}
				if err != nil {
					t.Fatal("valid parent producer prerequisite", err)
				}
				return raw
			}
			verifyParent := func(raw []byte) bundle.Artifact {
				var artifact bundle.Artifact
				var err error
				if test.selfParent {
					artifact, err = bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
				} else {
					artifact, err = bundle.Verify(bytes.NewReader(raw), int64(len(raw)), report.Keyring{"inspection-parent-key": public})
				}
				if err != nil {
					t.Fatal("independent parent trust prerequisite", err)
				}
				return artifact
			}
			parentRaw := createParent("prior-inspection-fixture")
			parent := verifyParent(parentRaw)
			parentPath := filepath.Join(f.directory, "parent.hhpcr")
			keyPath := writeTestKey(t, f.directory, "parent-public.key", public)
			if err := os.WriteFile(parentPath, parentRaw, 0600); err != nil {
				t.Fatal(err)
			}
			parentArgs := []string{"-previous-report", parentPath}
			if test.selfParent {
				parentArgs = append(parentArgs, "-previous-self-issued")
			} else {
				parentArgs = append(parentArgs, "-previous-public-key", keyPath, "-previous-key-id", "inspection-parent-key")
			}
			args := f.args(t)
			const childID = "12345678-1234-4234-8234-123456789abc"
			for i := range args {
				if args[i] == "-session-id" {
					args[i+1] = childID
				}
			}
			args = append(args, parentArgs...)
			var out, diagnostic bytes.Buffer
			collects, gates := 0, 0
			gate := func(context.Context) (sessionJournal, error) {
				gates++
				return sessionevidence.CloneJournal(f.journal), nil
			}
			collect := func(context.Context) (inventory.Snapshot, error) { collects++; return f.snapshot, nil }
			if code := runFinalizeTestOwned(t, t.Context(), args, &out, &diagnostic, collect, bytes.NewReader(bytes.Repeat([]byte{0x74}, 128)), time.Now, gate).Code; code != 0 || collects != 1 || gates != 1 {
				t.Fatalf("linked full finalizer failed: code=%d %s", code, &diagnostic)
			}
			childRaw, err := os.ReadFile(f.reportPath)
			if err != nil {
				t.Fatal(err)
			}
			child, err := bundle.VerifySelfIssued(bytes.NewReader(childRaw), int64(len(childRaw)))
			if err != nil {
				t.Fatal("linked child verification", err)
			}
			want := report.PreviousReport{ManifestDigest: parent.Envelope.ManifestDigest, BundleSHA256: fmt.Sprintf("sha256:%x", parent.BundleSHA256)}
			if child.Manifest.PreviousReport == nil || *child.Manifest.PreviousReport != want || child.Envelope.SessionID != childID || child.Envelope.SessionID == parent.Envelope.SessionID || child.Manifest.RevisionKind != "retest" {
				t.Fatal("exact independently verified parent or child identity changed")
			}
			var payload app.InventoryPayload
			if json.Unmarshal(child.InventoryJSON, &payload) != nil {
				t.Fatal("child payload")
			}
			got, err := app.ValidatedExteriorCondition(payload)
			if err != nil || got == nil || *got != value {
				t.Fatal("child inherited prior finding instead of its own evidence", err)
			}
			if !app.HasSessionThermalClaim(child.InventoryJSON) {
				t.Fatal("current observed child lost its matching session")
			}
			for _, view := range [][]byte{child.RenderedReport, child.RenderedPDF} {
				if !bytes.Contains(view, []byte(test.finding)) || !bytes.Contains(view, []byte(want.ManifestDigest)) || !bytes.Contains(view, []byte(want.BundleSHA256)) {
					t.Fatal("linked views lost current evidence or exact parent")
				}
			}
			verifyArgs := append([]string{"-self-issued"}, parentArgs...)
			verifyArgs = append(verifyArgs, f.reportPath)
			out.Reset()
			diagnostic.Reset()
			if code := runVerifyWithInput(t.Context(), verifyArgs, &out, &diagnostic, captureFixtureInput(t)); code != 0 || !strings.Contains(out.String(), "exact supplied parent matched") {
				t.Fatal("actual pair command rejected exact independent parent", code)
			}
			// The alternate parent has the same content manifest but a different
			// signed envelope and canonical archive. Content-only matching must fail.
			alternateRaw := createParent("other-inspection-fixture")
			alternate := verifyParent(alternateRaw)
			if alternate.Envelope.ManifestDigest != parent.Envelope.ManifestDigest || alternate.BundleSHA256 == parent.BundleSHA256 {
				t.Fatal("dual-digest counterexample did not reach exact-container boundary")
			}
			alternatePath := filepath.Join(f.directory, "alternate-parent.hhpcr")
			if err := os.WriteFile(alternatePath, alternateRaw, 0600); err != nil {
				t.Fatal(err)
			}
			wrong := append([]string(nil), verifyArgs...)
			for i := range wrong {
				if wrong[i] == "-previous-report" {
					wrong[i+1] = alternatePath
				}
			}
			out.Reset()
			diagnostic.Reset()
			if code := runVerifyWithInput(t.Context(), wrong, &out, &diagnostic, captureFixtureInput(t)); code != 1 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "exact predecessor relation mismatch") {
				t.Fatal("inspection weakened exact-parent verification", code)
			}
			unchanged, err := os.ReadFile(parentPath)
			if err != nil || !bytes.Equal(unchanged, parentRaw) {
				t.Fatal("child operation changed predecessor")
			}
		})
	}
}

func inspectionCurrentConditionValue(t *testing.T, finding, status string) exteriorinspection.ResultV2 {
	t.Helper()
	value := exteriorinspection.ResultV2{
		Schema: "hhsoft.pc-tester.exterior-inspection/v2",
		Source: "operator_reported", Scope: "visible_exterior_condition",
		Findings: exteriorinspection.ConditionFindingsV2{
			EnclosureIntegrity: finding, HingeMountCondition: "not_applicable",
			PortExteriorCondition: "no_issue_observed", SurfaceFinishCondition: "no_issue_observed",
		},
		ResponsesCompleted: 4, StopReason: "completed", Status: status, ObservationMilliseconds: 1,
	}
	if exteriorinspection.ValidateV2(value) != nil {
		t.Fatal("independent current inspection prerequisite")
	}
	return value
}
