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
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

func memoryOnlyHarness(t *testing.T) *sessionHarness {
	t.Helper()
	ids, err := plan.SelectableQuickTests("x86_64")
	if err != nil {
		t.Fatal(err)
	}
	excluded := []string{}
	for _, id := range ids {
		if id != "memory.quick" {
			excluded = append(excluded, id)
		}
	}
	h := newSessionHarness(t, "x86_64", excluded)
	useRealSessionFinalizer(t, h, nil, nil)
	return h
}

func signedMemoryPayload(t *testing.T, h *sessionHarness) app.InventoryPayload {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.directory, "session.hhpcr"))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var payload app.InventoryPayload
	if json.Unmarshal(artifact.InventoryJSON, &payload) != nil {
		t.Fatal("invalid signed payload")
	}
	if len(payload.TestResults) != 23 || len(h.journal.Workers) != 12 {
		t.Fatal("RAM increment changed row/worker counts")
	}
	return payload
}

func TestRealMemoryCommandCrossesSessionFinalizerAndSignedConsumers(t *testing.T) {
	h := memoryOnlyHarness(t)
	root := t.TempDir()
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, _ io.Reader, out, errOut io.Writer) int {
		if worker.ID != "memory.quick" || worker.Timeout != 30*time.Second {
			t.Fatal("wrong worker or expanded budget")
		}
		h.calls = append(h.calls, worker.ID)
		return runMemoryWith(ctx, []string{"-bytes", "4194304", "-passes", "1", "-timeout", "30s", "-output", worker.OutputPath}, out, errOut,
			func(ctx context.Context, options quick.MemoryOptions) quick.MemoryResult {
				options.Root = root
				value := quick.RunMemory(ctx, options)
				if value.Status != quick.StatusPass {
					t.Fatalf("real bounded RAM run failed: %+v", value)
				}
				h.clock = h.clock.Add(time.Duration(value.DurationMilliseconds+1) * time.Millisecond)
				return value
			})
	}
	if code := h.run(t.Context()); code != 0 || !h.finalized || len(h.calls) != 1 {
		t.Fatalf("real RAM not signed: %d %s", code, &h.stderr)
	}
	payload := signedMemoryPayload(t, h)
	view, err := app.ValidatedMemory(payload)
	if err != nil || view == nil || view.CompletedPhases != 10 || view.ExaminedBytes != 25165824 || view.WrittenBytes != 25165824 || view.AttemptedReadBytes != 25165824 || view.Mismatch != nil {
		t.Fatal("real RAM summary changed", err)
	}
}

// These are completed-result fixtures for the command/session boundary.
// Actual corruption and traversal are tested separately at the RAM kernel.
func memoryInversionFailureFixture(phase, pass int, reverse bool) quick.MemoryResult {
	v := completeMemoryEvidence()
	v.Status, v.Passes, v.DurationMilliseconds = quick.StatusFail, pass+1, 0
	v.Coverage.CompletedPhases, v.Coverage.CompletedPasses = 10*pass+phase, pass
	reads := [...]int64{0, 0, 1, 1, 2, 2, 3, 3, 4, 5}
	writes := [...]int64{0, 1, 1, 2, 2, 3, 3, 4, 5, 6}
	v.ExaminedBytes, v.Coverage.WrittenBytes = v.AllocatedBytes*(6*int64(pass)+reads[phase]), v.AllocatedBytes*(6*int64(pass)+writes[phase])
	expected, offset := 0xaa, int64(0)
	if reverse {
		expected, offset = 0x55, v.AllocatedBytes-1
	}
	v.Mismatch = &quick.MemoryMismatch{Offset: offset, ExpectedByte: expected, ActualByte: expected ^ 1}
	return v
}

func TestMemoryFaultFixturesRetainOffsetZeroAndStopPriorityInSignedReports(t *testing.T) {
	for _, fault := range []struct {
		name        string
		phase, pass int
		reverse     bool
	}{{"forward_zero", 7, 0, false}, {"reverse", 8, 0, true}, {"final_zero", 9, 0, false}, {"later_pass", 9, 1, false}} {
		for _, stop := range []string{"fault", "operator", "thermal"} {
			t.Run(fault.name+"/"+stop, func(t *testing.T) {
				h := memoryOnlyHarness(t)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				h.dependencies.RunWorker = func(_ context.Context, worker sessionWorker, _ io.Reader, _, _ io.Writer) int {
					if worker.ID != "memory.quick" {
						t.Fatal("unselected work ran")
					}
					h.calls = append(h.calls, worker.ID)
					v := memoryInversionFailureFixture(fault.phase, fault.pass, fault.reverse)
					if stop == "thermal" {
						v.Safety.MonitorStatus, v.Safety.MonitorFailure, v.Safety.ThermalCoverage = "failed", "thermal_limit", "available"
						v.Safety.ThermalSensors, v.Safety.ThermalSamples, v.Safety.PeakMilliCelsius = 1, 1, 95000
					}
					writeSessionFixture(t, worker.OutputPath, v)
					h.clock = h.clock.Add(5 * time.Millisecond)
					if stop == "operator" {
						cancel()
					}
					return 1
				}
				if code := h.run(ctx); code != 0 || !h.finalized || len(h.calls) != 1 {
					t.Fatalf("joined RAM fault not reportable: %d %s", code, &h.stderr)
				}
				view, err := app.ValidatedMemory(signedMemoryPayload(t, h))
				if err != nil || view == nil || view.Mismatch == nil || view.CurrentPass != fault.pass || view.CompletedPhases != 10*fault.pass+fault.phase || view.AttemptedReadBytes != view.ExaminedBytes+1 {
					t.Fatal("signed RAM fault lost", err)
				}
				wantOffset := int64(0)
				if fault.reverse {
					wantOffset = view.AllocatedBytes - 1
				}
				if view.Mismatch.Offset != wantOffset || h.journal.Workers[1].Outcome != "fail" || !h.inputFinished || !h.journal.Monitor.CleanupConfirmed || !h.journal.Thermal.Joined {
					t.Fatal("fault offset, priority or cleanup changed")
				}
			})
		}
	}
}

func TestMemoryOverflowCannotBeRestoredByFinalization(t *testing.T) {
	for _, status := range []quick.Status{quick.StatusError, quick.StatusAborted, quick.StatusFail} {
		h := memoryOnlyHarness(t)
		h.dependencies.RunWorker = func(_ context.Context, worker sessionWorker, _ io.Reader, _, _ io.Writer) int {
			v := memoryInversionFailureFixture(7, 0, false)
			v.Schema, v.Status, v.Detail = "", status, "later safety diagnostic"
			writeSessionFixture(t, worker.OutputPath, v)
			return 1
		}
		if code := h.run(t.Context()); code != 1 || h.finalized {
			t.Fatal("invalid overflow schema signed")
		}
		if _, err := os.Stat(filepath.Join(h.directory, "session.hhpcr")); !os.IsNotExist(err) {
			t.Fatal("overflow report exists")
		}
	}
	for _, version := range []int{0, 1, 3} {
		if _, err := newValidatedQuickTestResult("memory.quick", version, app.TestPass, completeMemoryEvidence()); err == nil {
			t.Fatal("wrong current definition accepted")
		}
	}
	if _, err := newValidatedQuickTestResult("memory.quick", 2, app.TestPass, completeMemoryEvidence()); err != nil {
		t.Fatal(err)
	}
}

func TestMemorySessionUsesRemainingRunDeadlineWithoutFreshWorkerWindow(t *testing.T) {
	for _, budget := range []time.Duration{5 * time.Second, 60 * time.Second} {
		h := memoryOnlyHarness(t)
		h.arguments = append(h.arguments, "-timeout", budget.String())
		started := h.clock
		h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, _ io.Reader, _, _ io.Writer) int {
			deadline, ok := ctx.Deadline()
			if !ok || !deadline.Equal(started.Add(min(budget, 30*time.Second))) || worker.Timeout != 30*time.Second {
				t.Fatal("memory obtained a fresh or expanded deadline")
			}
			writeSessionFixture(t, worker.OutputPath, sessionCompleteEvidence(worker.ID))
			return 0
		}
		if code := h.run(t.Context()); code != 0 || !h.finalized {
			t.Fatalf("bounded memory session failed: %d %s", code, &h.stderr)
		}
	}
}
