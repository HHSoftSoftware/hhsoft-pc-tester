package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func networkAcquisitionDiagnostic(status networktest.Status) networktest.Result {
	if status == networktest.StatusPass {
		return completeNetworkEvidence()
	}
	return networktest.Result{Schema: networktest.Schema, Status: status, RequireOffline: true, Detail: "network acquisition did not complete"}
}

func TestNetworkAcquisitionCommandRetainsDiagnosticStatuses(t *testing.T) {
	for _, status := range []networktest.Status{networktest.StatusPass, networktest.StatusInconclusive, networktest.StatusError, networktest.StatusAborted} {
		t.Run(string(status), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			calls := 0
			code := runNetworkWith(t.Context(), nil, &stdout, &stderr, func(ctx context.Context, options networktest.Options) networktest.Result {
				calls++
				if ctx.Err() != nil || options.Root != "/" || options.AllowActive {
					t.Fatal("safe production options changed")
				}
				return networkAcquisitionDiagnostic(status)
			})
			var evidence networktest.Result
			if code != 0 || calls != 1 || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &evidence) != nil || evidence.Status != status || evidence.NetworkTrafficGenerated {
				t.Fatal("diagnostic serialization failed or promoted evidence")
			}
			for _, interrupted := range []bool{false, true} {
				outcome, state, err := sessionWorkerOutcomeEvidence(stdout.Bytes(), nil, networktest.Schema, code, interrupted)
				wantState := "completed"
				if interrupted && (status == networktest.StatusError || status == networktest.StatusAborted) {
					wantState = "interrupted"
				}
				if err != nil || outcome != string(status) || state != wantState {
					t.Fatal("session changed acquisition status")
				}
			}
			if status != networktest.StatusPass {
				evidence.Status = networktest.StatusPass
				if _, err := newValidatedQuickTestResult("network.readiness.quick", 1, app.TestPass, evidence); err == nil {
					t.Fatal("diagnostic was promoted to pass")
				}
			}
		})
	}
	for _, args := range [][]string{{"extra"}, {"-invalid"}} {
		calls := 0
		if code := runNetworkWith(t.Context(), args, &bytes.Buffer{}, &bytes.Buffer{}, func(context.Context, networktest.Options) networktest.Result {
			calls++
			return completeNetworkEvidence()
		}); code != 2 || calls != 0 {
			t.Fatal("usage error reached acquisition")
		}
	}
	t.Run("completed prefix remains an error", func(t *testing.T) {
		partial := completeNetworkEvidence()
		partial.Status = networktest.StatusError
		partial.Detail = "network metadata closure could not be confirmed"
		var stdout, stderr bytes.Buffer
		if code := runNetworkWith(t.Context(), nil, &stdout, &stderr, func(context.Context, networktest.Options) networktest.Result { return partial }); code != 0 || stderr.Len() != 0 {
			t.Fatal("partial diagnostic output failed")
		}
		var got networktest.Result
		if json.Unmarshal(stdout.Bytes(), &got) != nil || got.Status != networktest.StatusError || got.HardwareInterfaces != 1 || got.DriverBoundInterfaces != 1 || len(got.Interfaces) != 1 || got.Interfaces[0] != partial.Interfaces[0] {
			t.Fatal("completed prefix was discarded or promoted")
		}
		if outcome, state, err := sessionWorkerOutcomeEvidence(stdout.Bytes(), nil, networktest.Schema, 0, false); err != nil || outcome != "error" || state != "completed" {
			t.Fatal("partial diagnostic lost its failed outcome")
		}
		if _, err := newValidatedQuickTestResult("network.readiness.quick", 1, app.TestPass, got); err == nil {
			t.Fatal("complete earlier row authorized caller pass")
		}
		arguments, snapshot, output := prepareNetworkAcquisitionFinalization(t, got, "pass")
		collected := false
		var finalizeStdout, finalizeStderr bytes.Buffer
		if code := runFinalizeWith(t.Context(), arguments, &finalizeStdout, &finalizeStderr, func(context.Context) (inventory.Snapshot, error) { collected = true; return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x52}, 64)), time.Now); code != 1 || collected || finalizeStdout.Len() != 0 || finalizeStderr.String() != "validate evidence network.readiness.quick: schema or status does not match supervisor outcome\n" {
			t.Fatalf("partial diagnostic was not rejected by network evidence validation: code=%d collected=%v stderr=%q", code, collected, finalizeStderr.String())
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("partial diagnostic produced a claimed-pass report")
		}
	})
}

func TestNetworkAcquisitionSessionStopsUnsafeDiagnostics(t *testing.T) {
	for _, status := range []networktest.Status{networktest.StatusError, networktest.StatusAborted} {
		t.Run(string(status), func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			original := h.dependencies.RunWorker
			seen, later := false, false
			h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, stdout, stderr io.Writer) int {
				if seen {
					later = true
				}
				if worker.ID != "network.readiness.quick" {
					return original(ctx, worker, input, stdout, stderr)
				}
				seen = true
				h.calls = append(h.calls, worker.ID)
				return runNetworkWith(ctx, []string{"-output", worker.OutputPath}, stdout, stderr, func(context.Context, networktest.Options) networktest.Result {
					return networkAcquisitionDiagnostic(status)
				})
			}
			code := h.run(t.Context())
			if code != 1 || !seen || later || h.finalized || !bytes.Contains(h.stderr.Bytes(), []byte("HHPT_SESSION_UNSAFE phase=readiness")) {
				t.Fatal("unsafe acquisition continued to later workers or signing")
			}
			if _, err := os.Stat(filepath.Join(h.directory, "session-journal.json")); !os.IsNotExist(err) {
				t.Fatal("unsuccessful safety readiness created signable journal")
			}
		})
	}
}

func prepareNetworkAcquisitionFinalization(t *testing.T, evidence networktest.Result, outcome string) ([]string, inventory.Snapshot, string) {
	t.Helper()
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x49}, 32)
	keyPath := writeTestKey(t, directory, "fingerprint.key", key)
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentNetwork, "network:eth0", map[string]string{"name": "eth0"})})
	planPath := writeFinalizeTestPlan(t, directory, snapshot, key)
	planRaw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := plan.Decode(planRaw)
	if err != nil {
		t.Fatal(err)
	}
	networkRows := 0
	for _, row := range frozen.Tests {
		if row.ID == "network.readiness.quick" {
			networkRows++
			if row.Status != plan.TestScheduled || row.Version != 1 {
				t.Fatal("network evidence fixture is not scheduled in the decoded frozen plan")
			}
		}
	}
	if !frozen.Frozen || networkRows != 1 {
		t.Fatal("network evidence fixture lacks exactly one frozen scheduled network row")
	}
	output := filepath.Join(directory, "network.hhpcr")
	arguments := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "network-fixture", "-output", output}
	for _, definition := range quickResultDefinitions {
		status := "not_present"
		if definition.id == "network.readiness.quick" {
			status = outcome
			writeSessionFixture(t, filepath.Join(directory, definition.filename), evidence)
		}
		arguments = append(arguments, "-outcome", definition.id+"="+status)
	}
	return arguments, snapshot, output
}

func TestNetworkAcquisitionManualFinalizerPreservesEvidenceWithoutPromotion(t *testing.T) {
	for _, status := range []networktest.Status{networktest.StatusPass, networktest.StatusInconclusive, networktest.StatusError, networktest.StatusAborted} {
		t.Run(string(status), func(t *testing.T) {
			evidence := networkAcquisitionDiagnostic(status)
			args, snapshot, output := prepareNetworkAcquisitionFinalization(t, evidence, string(status))
			var finalizeStdout, finalizeStderr bytes.Buffer
			collections := 0
			if code := runFinalizeWith(t.Context(), args, &finalizeStdout, &finalizeStderr, func(context.Context) (inventory.Snapshot, error) { collections++; return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x52}, 64)), time.Now); code != 0 || collections != 1 || finalizeStderr.Len() != 0 {
				t.Fatalf("standalone diagnostic finalization failed: code=%d collections=%d stderr=%q", code, collections, finalizeStderr.String())
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
				t.Fatal("bad completed payload")
			}
			found := false
			for _, row := range payload.TestResults {
				if row.ID == "network.readiness.quick" {
					found = true
					if row.Status != app.TestStatus(status) || !bytes.Equal(row.Evidence, encodeEvidenceFixture(t, evidence)) {
						t.Fatal("signed evidence changed")
					}
				}
			}
			if !found {
				t.Fatal("network evidence omitted")
			}
			if status != networktest.StatusPass {
				args, snapshot, output = prepareNetworkAcquisitionFinalization(t, evidence, "pass")
				collected := false
				var rejectedStdout, rejectedStderr bytes.Buffer
				if code := runFinalizeWith(t.Context(), args, &rejectedStdout, &rejectedStderr, func(context.Context) (inventory.Snapshot, error) { collected = true; return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x52}, 64)), time.Now); code != 1 || collected || rejectedStdout.Len() != 0 || rejectedStderr.String() != "validate evidence network.readiness.quick: schema or status does not match supervisor outcome\n" {
					t.Fatalf("false pass was not rejected by network evidence validation: code=%d collected=%v stderr=%q", code, collected, rejectedStderr.String())
				}
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("false pass produced report")
				}
			}
		})
	}
}
