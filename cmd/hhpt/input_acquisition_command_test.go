package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func inputAcquisitionDiagnostic(status inputtest.Status) inputtest.Result {
	result := completeInputEvidence()
	result.Status = status
	if status == inputtest.StatusInconclusive {
		result.Devices = nil
		result.ObservedCategories = nil
		result.Detail = "no requested input event devices were discovered"
	}
	if status == inputtest.StatusError || status == inputtest.StatusAborted {
		result.Devices = append(result.Devices, inputtest.DeviceResult{Interface: "event1", Category: "keyboard"})
		result.Detail = "input descriptor closure could not be confirmed"
	}
	return result
}

func TestInputAcquisitionCommandAndSessionRetainDiagnostics(t *testing.T) {
	// Private ownership injection is exercised in internal/inputtest. These
	// public values test the real command, session decoder and evidence gate.
	for _, status := range []inputtest.Status{inputtest.StatusPass, inputtest.StatusInconclusive, inputtest.StatusError, inputtest.StatusAborted} {
		t.Run(string(status), func(t *testing.T) {
			evidence := inputAcquisitionDiagnostic(status)
			var stdout, stderr bytes.Buffer
			calls := 0
			code := runInputWith(t.Context(), nil, &stdout, &stderr, func(ctx context.Context, options inputtest.Options) inputtest.Result {
				calls++
				if ctx.Err() != nil || options.Root != "/" || options.Duration != 0 || len(options.Categories) != 0 {
					t.Fatal("production readiness options changed")
				}
				return evidence
			})
			var got inputtest.Result
			if code != 0 || calls != 1 || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &got) != nil || !reflect.DeepEqual(got, evidence) {
				t.Fatal("command altered readiness diagnostic")
			}
			for _, interrupted := range []bool{false, true} {
				outcome, state, err := sessionWorkerOutcomeEvidence(stdout.Bytes(), nil, inputtest.Schema, code, interrupted)
				wantState := "completed"
				if interrupted && (status == inputtest.StatusError || status == inputtest.StatusAborted) {
					wantState = "interrupted"
				}
				if err != nil || outcome != string(status) || state != wantState {
					t.Fatal("session promoted or discarded diagnostic")
				}
			}
			if _, err := newValidatedQuickTestResult("input.readiness.quick", 1, app.TestStatus(status), got); err != nil {
				t.Fatal(err)
			}
			if status != inputtest.StatusPass {
				if _, err := newValidatedQuickTestResult("input.readiness.quick", 1, app.TestPass, got); err == nil {
					t.Fatal("caller pass overrode non-pass evidence")
				}
				got.Status = inputtest.StatusPass
				if _, err := newValidatedQuickTestResult("input.readiness.quick", 1, app.TestPass, got); err == nil {
					t.Fatal("unreadable or empty result was promoted to pass")
				}
			}
		})
	}
	for _, arguments := range [][]string{{"extra"}, {"-unknown"}, {"-duration", "-1s"}, {"-duration", "301s"}} {
		calls := 0
		if code := runInputWith(t.Context(), arguments, &bytes.Buffer{}, &bytes.Buffer{}, func(context.Context, inputtest.Options) inputtest.Result { calls++; return completeInputEvidence() }); code != 2 || calls != 0 {
			t.Fatal("invalid invocation reached input acquisition")
		}
	}
	calls := 0
	if code := runInputWith(t.Context(), []string{"-duration", "25ms", "-categories", "keyboard,touchpad"}, &bytes.Buffer{}, &bytes.Buffer{}, func(_ context.Context, options inputtest.Options) inputtest.Result {
		calls++
		if options.Duration != 25*time.Millisecond || !reflect.DeepEqual(options.Categories, []string{"keyboard", "touchpad"}) {
			t.Fatal("activity options changed")
		}
		return inputAcquisitionDiagnostic(inputtest.StatusInconclusive)
	}); code != 0 || calls != 1 {
		t.Fatal("bounded activity invocation not retained")
	}
}

func TestInputAcquisitionKeepsExistingSessionStopPolicy(t *testing.T) {
	for _, status := range []inputtest.Status{inputtest.StatusError, inputtest.StatusAborted} {
		t.Run(string(status), func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			original := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, stdout, stderr io.Writer) int {
				if worker.ID != "input.readiness.quick" {
					return original(ctx, worker, input, stdout, stderr)
				}
				h.calls = append(h.calls, worker.ID)
				return runInputWith(ctx, []string{"-output", worker.OutputPath}, stdout, stderr, func(context.Context, inputtest.Options) inputtest.Result { return inputAcquisitionDiagnostic(status) })
			}
			if code := h.run(t.Context()); code != 0 || !h.finalized {
				t.Fatalf("reportable input status changed session policy: %d %s", code, h.stderr.String())
			}
			seen, later := false, false
			for _, id := range h.calls {
				if id == "input.readiness.quick" {
					seen = true
				} else if seen {
					later = true
				}
			}
			if !seen || later != (status == inputtest.StatusError) {
				t.Fatal("input error/aborted changed later-worker policy")
			}
			found := false
			for _, worker := range h.journal.Workers {
				if worker.ID == "input.readiness.quick" {
					found = true
					if worker.Outcome != string(status) || worker.State != "completed" {
						t.Fatal("input diagnostic lost")
					}
				}
			}
			wantStop := "completed"
			if status == inputtest.StatusAborted {
				wantStop = "worker_stop"
			}
			if !found || h.journal.StopReason != wantStop || sessionSafetyWorker("input.readiness.quick") {
				t.Fatal("new global cleanup/safety stop was invented")
			}
		})
	}
}

func prepareInputAcquisitionFinalization(t *testing.T, evidence inputtest.Result, outcome string) ([]string, inventory.Snapshot, string) {
	t.Helper()
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x46}, 32)
	keyPath := writeTestKey(t, directory, "fingerprint.key", key)
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentInput, "input:keyboard", map[string]string{"category": "keyboard", "handlers": "kbd event0"})})
	// Guided keyboard is explicitly excluded in the real frozen selection.
	frozen, err := plan.BuildWithSelection(snapshot, plan.ProfileQuick, "x86_64", key, "ephemeral-session", []string{"input.keyboard.quick", "inspection.exterior.quick"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "plan.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := plan.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	planned := map[string]plan.TestStatus{}
	count := 0
	for _, row := range decoded.Tests {
		planned[row.ID] = row.Status
		if row.ID == "input.readiness.quick" {
			count++
			if row.Status != plan.TestScheduled || row.Version != 1 {
				t.Fatal("input readiness fixture is not scheduled")
			}
		}
	}
	if !decoded.Frozen || count != 1 || planned["input.keyboard.quick"] != plan.TestExcluded {
		t.Fatal("fixture selection did not preserve its intended boundary")
	}
	output := filepath.Join(directory, "input.hhpcr")
	arguments := []string{"-fingerprint-key", keyPath, "-plan", path, "-evidence-dir", directory, "-session-id", "input-fixture", "-output", output}
	for _, definition := range currentResultDefinitionsForTest(t, decoded) {
		status := string(planned[definition.id])
		if definition.id == "input.readiness.quick" {
			status = outcome
			writeSessionFixture(t, filepath.Join(directory, definition.filename), evidence)
		} else if planned[definition.id] == plan.TestScheduled {
			t.Fatal("unrelated worker accidentally scheduled")
		}
		arguments = append(arguments, "-outcome", definition.id+"="+status)
	}
	return arguments, snapshot, output
}

func assertInputSignedDiagnostic(t *testing.T, evidence inputtest.Result) {
	t.Helper()
	arguments, snapshot, output := prepareInputAcquisitionFinalization(t, evidence, string(evidence.Status))
	collections := 0
	var stdout, stderr bytes.Buffer
	if code := runFinalizeTestWith(t, t.Context(), arguments, &stdout, &stderr, func(context.Context) (inventory.Snapshot, error) { collections++; return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x57}, 64)), time.Now); code != 0 || collections != 1 || stderr.Len() != 0 {
		t.Fatalf("diagnostic finalization: %d %d %s", code, collections, stderr.String())
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var payload app.InventoryPayload
	if json.Unmarshal(verified.InventoryJSON, &payload) != nil {
		t.Fatal("invalid signed payload")
	}
	found := false
	for _, row := range payload.TestResults {
		if row.ID == "input.readiness.quick" {
			found = true
			if row.Status != app.TestStatus(evidence.Status) || !bytes.Equal(row.Evidence, encodeEvidenceFixture(t, evidence)) {
				t.Fatal("signed diagnostic changed")
			}
		}
	}
	if !found {
		t.Fatal("signed readiness diagnostic absent")
	}
	if evidence.Status != inputtest.StatusPass {
		arguments, snapshot, output = prepareInputAcquisitionFinalization(t, evidence, "pass")
		collected := false
		var rejectedOut, rejectedErr bytes.Buffer
		code := runFinalizeTestWith(t, t.Context(), arguments, &rejectedOut, &rejectedErr, func(context.Context) (inventory.Snapshot, error) { collected = true; return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x57}, 64)), time.Now)
		if code != 1 || collected || rejectedOut.Len() != 0 || rejectedErr.String() != "validate evidence input.readiness.quick: schema or status does not match supervisor outcome\n" {
			t.Fatalf("wrong false-pass rejection: %d %v %q", code, collected, rejectedErr.String())
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("false pass created a report")
		}
	}
}

func TestInputAcquisitionManualFinalizerRetainsExactDiagnosticPrefix(t *testing.T) {
	for _, status := range []inputtest.Status{inputtest.StatusPass, inputtest.StatusInconclusive, inputtest.StatusError, inputtest.StatusAborted} {
		t.Run(string(status), func(t *testing.T) { assertInputSignedDiagnostic(t, inputAcquisitionDiagnostic(status)) })
	}
}
