package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
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
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
)

const extendedMemoryWorkerOrder = "cpu.quick memory.quick memory.stress audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"

func commandMemoryNoLoad() quick.MemoryStressResult {
	return quick.MemoryStressResult{
		Schema: "hhsoft.pc-tester.memory-stress/v1", Status: "inconclusive", StopReason: "insufficient_budget", Suite: "byte-patterns-aa55-inversions-v1",
		RequestedLoadMilliseconds: 1800000, LocalWatchdogMilliseconds: 1860000, ThermalPolicy: "available-observations/v1",
		RequestedBytes: 4194304, EffectiveDeadlineMilliseconds: 1800000, EffectiveDeadlineSource: "parent", AccountingValid: true,
		StopFacts:    quick.MemoryStressStopFacts{InsufficientBudget: true},
		Availability: quick.MemoryStressAvailability{CloseConfirmed: true},
		Safety: quick.SafetyEvidence{GlobalTimeoutMilliseconds: 1860000, ThermalLimitMilliCelsius: 90000,
			MonitorStatus: "not_started", CleanupConfirmed: true, ThermalCoverage: "unavailable"},
	}
}

func setExtendedMemoryPlan(t *testing.T, h *sessionHarness) {
	t.Helper()
	var err error
	h.frozen, err = plan.Build(h.snapshot, plan.ProfileExtended, "x86_64", bytes.Repeat([]byte{0x31}, 32), "ephemeral-session")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(h.frozen)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Decode(raw); err != nil {
		t.Fatal("real current Extended plan prerequisite", err)
	}
	writeSessionFixture(t, filepath.Join(h.directory, "plan.json"), h.frozen)
	h.arguments = append(h.arguments, "-profile", "extended")
}

func newExtendedMemoryHarness(t *testing.T, value quick.MemoryStressResult) *sessionHarness {
	t.Helper()
	requireCommandMemoryFixture(t, value)
	h := newSessionHarness(t, "x86_64", nil)
	setExtendedMemoryPlan(t, h)
	run := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
		if worker.ID != "memory.stress" {
			return run(ctx, worker, input, out, diagnostic)
		}
		if ctx.Err() != nil || worker.Timeout != 1860*time.Second {
			t.Fatal("memory stress dispatched outside its fixed row")
		}
		deadline, ok := ctx.Deadline()
		if !ok || deadline.After(h.clock.Add(1860*time.Second)) {
			t.Fatal("memory stress received an enlarged deadline")
		}
		h.calls = append(h.calls, worker.ID)
		writeSessionFixture(t, worker.OutputPath, value)
		h.clock = h.clock.Add(time.Duration(value.FinishedMilliseconds) * time.Millisecond)
		return 0
	}
	// This existing fixture calls the real profile-aware owner using
	// request.Profile. It retains real keys, live gate, signing and views.
	useStandardFinalizer(t, h, nil)
	return h
}

func extendedMemoryArtifact(t *testing.T, h *sessionHarness) bundle.Artifact {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.directory, "session.hhpcr"))
	if err != nil {
		t.Fatal("signed Extended report absent")
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal("Extended signature and canonical views", err)
	}
	return artifact
}

func verifyExtendedMemoryHarness(t *testing.T, h *sessionHarness) bundle.Artifact {
	t.Helper()
	if code := h.run(t.Context()); code != 0 || !h.finalized {
		t.Fatalf("real Extended finalizer prerequisite: %d %s", code, &h.stderr)
	}
	return extendedMemoryArtifact(t, h)
}

func TestExtendedMemorySessionLiteralOrderBudgetAndOwnedSource(t *testing.T) {
	h := newExtendedMemoryHarness(t, commandMemoryPass())
	artifact := verifyExtendedMemoryHarness(t, h)
	wantCalls := []string{"cpu.quick", "memory.quick", "memory.stress", "audio.readiness.quick", "input.readiness.quick",
		"input.keyboard.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick",
		"usb.readiness.quick", "graphics.quick", "display.operator.quick", "inspection.exterior.quick"}
	var ids []string
	for _, row := range h.journal.Workers {
		ids = append(ids, row.ID)
	}
	if !reflect.DeepEqual(ids, strings.Fields(extendedMemoryWorkerOrder)) || !reflect.DeepEqual(h.calls, wantCalls) ||
		h.journal.Schema != "hhsoft.pc-tester.quick-session/v13" || h.journal.Profile != "extended" ||
		h.journal.BudgetMilliseconds != 2700000 || h.request.Profile != plan.ProfileExtended || h.journal.CPUStressSource != nil {
		t.Fatal("literal Extended order, budget or profile changed")
	}
	raw, err := os.ReadFile(filepath.Join(h.directory, "memory-stress.json"))
	if err != nil || h.journal.MemoryStressSource == nil || h.journal.MemoryStressSource.EvidenceFileSHA256 != sourceFileDigest(raw) {
		t.Fatal("exact owned memory source missing")
	}
	_, digest, err := quickevidence.DecodeMemoryStress(raw)
	if err != nil || h.journal.MemoryStressSource.EvidenceTypedSHA256 != digest {
		t.Fatal("typed memory source missing")
	}
	value, err := app.ValidatedMemoryStress(stressSignedPayload(t, artifact))
	if err != nil || value == nil || value.Coverage.CompletedCycles != 2 || value.Coverage.LoadCycles != 1 ||
		value.Coverage.ReadBytes != 50331648 || value.Coverage.TerminalReadBytes != 4194304 {
		t.Fatal("independent cycle/terminal facts changed", err)
	}
	ready, err := os.ReadFile(filepath.Join(h.directory, "session-ready.txt"))
	if err != nil || !bytes.Contains(ready, []byte("profile=extended memory_stress=pass")) ||
		bytes.Contains(ready, []byte("cpu_stress=pass")) {
		t.Fatal("ready marker changed profile or invented CPU stress")
	}
}

func TestExtendedMemorySessionRequiresExplicitProfileAndBoundedTimeout(t *testing.T) {
	for _, mode := range []string{"omitted", "quick", "standard", "destructive", "EXTENDED", "too_long", "zero"} {
		t.Run(mode, func(t *testing.T) {
			h := newExtendedMemoryHarness(t, commandMemoryPass())
			if mode == "too_long" {
				h.arguments = append(h.arguments, "-timeout", "45m1s")
			} else if mode == "zero" {
				h.arguments = append(h.arguments, "-timeout", "0s")
			} else {
				h.arguments = h.arguments[:len(h.arguments)-2]
				if mode != "omitted" {
					h.arguments = append(h.arguments, "-profile", mode)
				}
			}
			starts := 0
			h.dependencies.StartMonitor = func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error) { starts++; return nil, nil }
			if code := h.run(t.Context()); code == 0 || starts != 0 || h.safetyCalls != 0 || len(h.calls) != 0 || h.finalized {
				t.Fatal("invalid profile or timeout reached acquisition")
			}
			if _, err := os.Lstat(filepath.Join(h.directory, "session.hhpcr")); !os.IsNotExist(err) {
				t.Fatal("invalid invocation created output")
			}
		})
	}
}

func TestExtendedMemorySessionNoLoadIsObservedAndStopsLaterWork(t *testing.T) {
	value := commandMemoryNoLoad()
	value.EffectiveDeadlineMilliseconds = 19000
	h := newExtendedMemoryHarness(t, value)
	h.arguments = append(h.arguments, "-timeout", "20s")
	artifact := verifyExtendedMemoryHarness(t, h)
	if !reflect.DeepEqual(h.calls, []string{"cpu.quick", "memory.quick", "memory.stress"}) ||
		h.journal.StopReason != "worker_stop" || h.journal.MemoryStressSource == nil ||
		h.journal.BudgetMilliseconds != 20000 || h.journal.Workers[2].State != "completed" ||
		h.journal.Workers[2].Outcome != "inconclusive" {
		t.Fatal("bounded no-load diagnostic became an unstarted note or a pass")
	}
	for _, worker := range h.journal.Workers[3:] {
		if worker.ID == "input.touchpad.quick" || worker.ID == "input.touchscreen.quick" {
			if worker.State != "not_scheduled" || worker.Outcome != "not_present" || worker.Timing != nil {
				t.Fatal("absent device lost frozen disposition")
			}
		} else if worker.State != "not_started" || worker.Outcome != "aborted" || worker.Timing != nil {
			t.Fatal("later workload started after a non-pass")
		}
	}
	view, err := app.ValidatedMemoryStress(stressSignedPayload(t, artifact))
	if err != nil || view == nil || view.Availability.Started || view.AllocatedBytes != 0 || view.LoadStartedMilliseconds != nil {
		t.Fatal("no-load evidence was hidden or inflated", err)
	}
	if !bytes.Contains(artifact.RenderedReport, []byte("Cleanup confirmed</th><td>true</td>")) {
		t.Fatal("unacquired availability owner was reported as an unclosed resource")
	}
}

func TestExtendedMemoryQuickFailureCannotStartStress(t *testing.T) {
	h := newExtendedMemoryHarness(t, commandMemoryPass())
	failure := memoryInversionFailureFixture(7, 0, false)
	raw, err := json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := quickevidence.DecodeLocal("memory.quick", raw); err != nil {
		t.Fatal("independent quick mismatch prerequisite", err)
	}
	run := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
		if worker.ID != "memory.quick" {
			return run(ctx, worker, input, out, diagnostic)
		}
		h.calls = append(h.calls, worker.ID)
		writeSessionFixture(t, worker.OutputPath, failure)
		return 1
	}
	artifact := verifyExtendedMemoryHarness(t, h)
	if !reflect.DeepEqual(h.calls, []string{"cpu.quick", "memory.quick"}) || h.journal.MemoryStressSource != nil ||
		h.journal.Workers[1].Outcome != "fail" || h.journal.Workers[2].State != "not_started" {
		t.Fatal("failed quick prerequisite was bypassed")
	}
	value, err := app.ValidatedMemoryStress(stressSignedPayload(t, artifact))
	if err != nil || value != nil || bytes.Contains(artifact.RenderedReport, []byte("MEMORY STRESS (EXTENDED PROFILE)")) {
		t.Fatal("honest unstarted note became a stress observation", err)
	}
}

func TestExtendedMemoryDispatchedOutputFailuresCannotSignOrPublishReady(t *testing.T) {
	for _, mode := range []string{"missing", "malformed", "nonzero", "cleanup", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			h := newExtendedMemoryHarness(t, commandMemoryNoLoad())
			run := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
				if worker.ID != "memory.stress" {
					return run(ctx, worker, input, out, diagnostic)
				}
				h.calls = append(h.calls, worker.ID)
				switch mode {
				case "missing":
					return 0
				case "cleanup":
					return 12
				case "nonzero":
					writeSessionFixture(t, worker.OutputPath, commandMemoryNoLoad())
					return 1
				default:
					raw := []byte("{")
					if mode == "oversized" {
						raw = bytes.Repeat([]byte(" "), (1<<20)+1)
					}
					if os.WriteFile(worker.OutputPath, raw, 0600) != nil {
						t.Fatal("owned failure fixture")
					}
					return 0
				}
			}
			want := 1
			if mode == "cleanup" {
				want = 12
			}
			if code := h.run(t.Context()); code != want || h.finalized || len(h.calls) != 3 || !h.inputFinished {
				t.Fatalf("dispatch failure escaped its boundary: %d %s", code, &h.stderr)
			}
			for _, name := range []string{"session.hhpcr", "session-ready.txt"} {
				if _, err := os.Lstat(filepath.Join(h.directory, name)); !os.IsNotExist(err) {
					t.Fatal("uncertain cleanup or evidence published output")
				}
			}
		})
	}
}

func TestExtendedMemoryMismatchSurvivesCancellationAndLocalHeat(t *testing.T) {
	for _, mode := range []string{"mismatch", "cancelled", "thermal"} {
		t.Run(mode, func(t *testing.T) {
			value := commandMemoryPass()
			value.Status, value.StopReason = "fail", "memory_mismatch"
			value.Coverage.TerminalReadBytes = 0
			value.Mismatch = &quick.MemoryStressMismatch{Stage: "terminal", Cycle: 2, Phase: 9, Offset: 0, ExpectedByte: 170, ActualByte: 171}
			wantStop := "worker_stop"
			if mode == "cancelled" {
				value.StopFacts.ParentCancelled = true
				wantStop = "operator_cancelled"
			}
			if mode == "thermal" {
				value.StopFacts.ThermalLimit = true
				value.Safety.PeakMilliCelsius = 95000
				value.Safety.MonitorStatus, value.Safety.MonitorFailure = "failed", "thermal_limit"
				wantStop = "thermal_limit"
			}
			requireCommandMemoryFixture(t, value)
			h := newExtendedMemoryHarness(t, value)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			run := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
				code := run(ctx, worker, input, out, diagnostic)
				if worker.ID == "memory.stress" && mode == "cancelled" {
					cancel()
				}
				return code
			}
			if code := h.run(ctx); code != 0 || !h.finalized || len(h.calls) != 3 || h.journal.StopReason != wantStop {
				t.Fatalf("joined mismatch was not reportable: %d %s", code, &h.stderr)
			}
			got, err := app.ValidatedMemoryStress(stressSignedPayload(t, extendedMemoryArtifact(t, h)))
			if err != nil || got == nil || got.Status != "fail" || got.Mismatch == nil || got.Mismatch.Offset != 0 ||
				got.Coverage.ReadBytes != 50331648 || got.Coverage.TerminalReadBytes != 0 ||
				h.journal.Workers[2].State != "completed" || h.journal.Workers[2].Outcome != "fail" ||
				!h.inputFinished || !h.journal.Monitor.CleanupConfirmed || !h.journal.Thermal.Joined {
				t.Fatal("mismatch, exact counters or cleanup priority changed", err)
			}
		})
	}
}

func TestExtendedMemoryRehashedQuickPrerequisiteCannotBeBypassed(t *testing.T) {
	for _, state := range []plan.TestStatus{plan.TestExcluded, plan.TestUnsupported, plan.TestNotPresent, plan.TestMissingDriver} {
		t.Run(string(state), func(t *testing.T) {
			h := newExtendedMemoryHarness(t, commandMemoryPass())
			changed, stressScheduled := false, false
			for index := range h.frozen.Tests {
				row := &h.frozen.Tests[index]
				if row.ID == "memory.quick" {
					row.Status, changed = state, true
				}
				if row.ID == "memory.stress" {
					stressScheduled = row.Status == plan.TestScheduled
				}
			}
			if !changed || !stressScheduled {
				t.Fatal("prerequisite mutation was ineffective")
			}
			h.frozen.ID = ""
			raw, err := json.Marshal(h.frozen)
			if err != nil {
				t.Fatal(err)
			}
			h.frozen.ID = fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
			writeSessionFixture(t, filepath.Join(h.directory, "plan.json"), h.frozen)
			starts := 0
			h.dependencies.StartMonitor = func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error) { starts++; return nil, nil }
			if code := h.run(t.Context()); code == 0 || starts != 0 || h.safetyCalls != 0 || len(h.calls) != 0 || h.finalized {
				t.Fatal("rehashed invalid prerequisite reached work")
			}
		})
	}
}

func TestExtendedMemoryRealDispatchRetainsFixedLoadWhenBudgetIsShort(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "memory-stress.json")
	var out, diagnostic bytes.Buffer
	code := runSessionWorker(ctx, sessionWorker{ID: "memory.stress", Architecture: "x86_64", OutputPath: path, Timeout: 1860 * time.Second},
		strings.NewReader(""), &out, &diagnostic)
	if code != 0 {
		t.Fatalf("real short-budget diagnostic failed: %d %s", code, &diagnostic)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("owned diagnostic missing")
	}
	value, _, err := quickevidence.DecodeMemoryStress(raw)
	if err != nil || value.Status != "inconclusive" || value.StopReason != "insufficient_budget" ||
		value.RequestedLoadMilliseconds != 1800000 || value.LocalWatchdogMilliseconds != 1860000 ||
		value.RequestedBytes != 0 || value.AllocatedBytes != 0 || value.Availability.Started || value.LoadStartedMilliseconds != nil {
		t.Fatal("session dispatch shortened the load or acquired memory", err)
	}
}

func TestExtendedMemoryAbsentHardwareRetainsUnobservedDisposition(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	components := make([]inventory.Component, 0, len(h.snapshot.Components))
	for _, component := range h.snapshot.Components {
		if component.Type != inventory.ComponentMemory {
			components = append(components, component)
		}
	}
	h.snapshot = inventory.NewSnapshot(components)
	setExtendedMemoryPlan(t, h)
	useStandardFinalizer(t, h, nil)
	artifact := verifyExtendedMemoryHarness(t, h)
	if h.journal.MemoryStressSource != nil || h.journal.Workers[1].Outcome != "not_present" ||
		h.journal.Workers[2].Outcome != "not_present" || h.journal.Workers[2].State != "not_scheduled" {
		t.Fatal("absence became observed stress")
	}
	value, err := app.ValidatedMemoryStress(stressSignedPayload(t, artifact))
	if err != nil || value != nil || bytes.Contains(artifact.RenderedReport, []byte("MEMORY STRESS (EXTENDED PROFILE)")) {
		t.Fatal("absent memory was promoted in a signed view")
	}
}

func TestExtendedMemoryRealThermalMonitorCancelsTargetAndRetainsCooling(t *testing.T) {
	h, write := realThermalHarness(t, nil, "41000\n")
	setExtendedMemoryPlan(t, h)
	useStandardFinalizer(t, h, nil)
	accepted := make(chan struct{})
	h.dependencies.StartMonitor = func(started time.Time, fail context.CancelFunc) (sessionSafetyMonitor, error) {
		observations := 0
		return safemode.StartMonitor(func(context.Context) safemode.Result {
			observations++
			if observations == 2 {
				close(accepted)
			}
			return journalSafeObservation()
		}, safemode.MonitorOptions{Started: started, Cancel: fail})
	}
	run := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
		if worker.ID != "memory.stress" {
			return run(ctx, worker, input, out, diagnostic)
		}
		h.calls = append(h.calls, worker.ID)
		timer := time.NewTimer(4 * time.Second)
		defer timer.Stop()
		select {
		case <-accepted:
		case <-ctx.Done():
			t.Fatal("memory target cancelled before accepted safety observation")
		case <-timer.C:
			t.Fatal("safety observation prerequisite was not reached")
		}
		write("98000\n")
		select {
		case <-ctx.Done():
		case <-time.After(4 * time.Second):
			t.Fatal("real thermal observation did not cancel memory target")
		}
		write("41000\n")
		value := commandMemoryNoLoad()
		value.Status, value.StopReason = "aborted", "parent_cancelled"
		value.EffectiveDeadlineMilliseconds, value.EffectiveDeadlineSource = 1860000, "watchdog"
		value.StopFacts = quick.MemoryStressStopFacts{ParentCancelled: true}
		requireCommandMemoryFixture(t, value)
		writeSessionFixture(t, worker.OutputPath, value)
		return 0
	}
	if code := h.run(t.Context()); code != 0 || !h.finalized {
		t.Fatalf("joined real thermal stop was not reportable: %d %s", code, &h.stderr)
	}
	view := verifiedThermalView(t, h)
	if !reflect.DeepEqual(h.calls, []string{"cpu.quick", "memory.quick", "memory.stress"}) ||
		h.journal.Workers[2].State != "interrupted" || h.journal.Workers[2].Outcome != "aborted" ||
		h.journal.MemoryStressSource == nil || h.journal.Monitor.CompletedObservations == 0 ||
		view.Finding != "limit_reached" || h.journal.Thermal.FirstFinding == nil ||
		h.journal.Thermal.Final.Observation.PeakMilliCelsius != 41000 || view.PeakMilliCelsius != 98000 {
		t.Fatal("thermal cancellation, evidence ownership or cooling latch was lost")
	}
}
