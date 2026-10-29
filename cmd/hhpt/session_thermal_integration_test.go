package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
)

func useRealSessionFinalizer(t *testing.T, h *sessionHarness, collect snapshotCollector, random io.Reader) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.directory, "synthetic.key"), bytes.Repeat([]byte{0x31}, 32), 0600); err != nil {
		t.Fatal("write synthetic key")
	}
	if collect == nil {
		collect = func(context.Context) (inventory.Snapshot, error) { return h.snapshot, nil }
	}
	if random == nil {
		random = bytes.NewReader(bytes.Repeat([]byte{0x52}, 128))
	}
	h.dependencies.Finalize = func(ctx context.Context, request sessionFinalization, out, errOut io.Writer) sessionFinalizationResult {
		gate := request.Gate
		request.Gate = func(ctx context.Context) (sessionJournal, error) {
			journal, err := gate(ctx)
			if err != nil {
				cause := "journal_io_or_other"
				switch {
				case errors.Is(err, sessionevidence.ErrInvalid):
					cause = "journal_structure"
				case err.Error() == "pre-sign safety failed":
					cause = "pre_sign_safety"
				case err.Error() == "final thermal observation failed":
					cause = "final_thermal_observation"
				case err.Error() == "session monitors did not complete safely":
					cause = "monitor_completion"
				}
				t.Logf("live thermal gate diagnostic: cause=%s context_cancelled=%t", cause, ctx.Err() != nil)
			}
			return journal, err
		}
		args := []string{"-plan", request.PlanPath, "-fingerprint-key", request.FingerprintKeyPath, "-key-epoch", request.KeyEpoch, "-evidence-dir", request.EvidenceDirectory, "-output", request.OutputPath, "-session-id", "live-thermal-fixture"}
		for _, outcome := range request.Outcomes {
			args = append(args, "-outcome", outcome)
		}
		result := runFinalizeTestOwned(t, ctx, args, out, errOut, collect, random, time.Now, request.Gate)
		h.request = request
		h.finalized = result.Code == 0
		if result.Journal != nil {
			h.journal = sessionevidence.CloneJournal(*result.Journal)
		}
		return result
	}
}

func realThermalHarness(t *testing.T, excluded []string, temperature string) (*sessionHarness, func(string)) {
	t.Helper()
	h := newSessionHarness(t, "x86_64", excluded)
	t.Cleanup(func() {
		if t.Failed() {
			logRealThermalHarnessDiagnostics(t, h)
		}
	})
	root := t.TempDir()
	path := filepath.Join(root, "sys/class/thermal/thermal_zone0/temp")
	write := func(value string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal("create synthetic sensor")
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal("write synthetic sensor")
		}
	}
	if temperature != "" {
		write(temperature)
	}
	h.dependencies.Now = time.Now
	h.dependencies.StartMonitor = func(started time.Time, fail context.CancelFunc) (sessionSafetyMonitor, error) {
		return safemode.StartMonitor(func(context.Context) safemode.Result { return journalSafeObservation() }, safemode.MonitorOptions{Started: started, Cancel: fail})
	}
	h.dependencies.StartThermal = func(started time.Time, cancel, fail context.CancelFunc) (sessionThermalMonitor, error) {
		return thermal.Start(thermal.Options{Started: started, Root: root, CancelWork: cancel, Fail: fail})
	}
	h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, _ io.Reader, _, _ io.Writer) int {
		if ctx.Err() != nil {
			t.Fatal("worker started after cancellation")
		}
		h.calls = append(h.calls, w.ID)
		writeSessionFixture(t, w.OutputPath, sessionCompleteEvidence(w.ID))
		return 0
	}
	useRealSessionFinalizer(t, h, nil, nil)
	return h, write
}

// Failure output is deliberately limited to fixed labels, booleans and timing
// counters from this test's own retained files. Never print paths or raw data.
func logRealThermalHarnessDiagnostics(t *testing.T, h *sessionHarness) {
	t.Helper()
	var safety safemode.MonitorResult
	raw, err := readSmallRegularFile(filepath.Join(h.directory, "session-safety-monitor.json"), 65536)
	safetyRead := err == nil && json.Unmarshal(raw, &safety) == nil
	var temperature thermal.Result
	raw, err = readSmallRegularFile(filepath.Join(h.directory, "session-thermal-monitor.json"), 65536)
	thermalRead := err == nil && json.Unmarshal(raw, &temperature) == nil
	t.Logf("live thermal monitor diagnostic: safety_read=%t safety_valid=%t safety_cleanup=%t safety_failure=%t safety_completed=%d safety_elapsed_ms=%d safety_gap_ms=%d thermal_read=%t thermal_valid=%t thermal_cleanup=%t thermal_joined=%t thermal_failure=%t thermal_attempted=%d thermal_completed=%d thermal_elapsed_ms=%d thermal_gap_ms=%d initial_completed_ms=%d final_requested_ms=%d final_completed_ms=%d",
		safetyRead, safetyRead && validateSessionMonitor(safety, safety.ElapsedMilliseconds) == nil,
		safety.CleanupConfirmed, safety.FirstFailure != nil, safety.CompletedObservations,
		safety.ElapsedMilliseconds, safety.MaximumGapMilliseconds, thermalRead,
		thermalRead && thermal.ValidateResult(temperature, true) == nil, temperature.CleanupConfirmed,
		temperature.Joined, temperature.FirstFailure != nil, temperature.AttemptedObservations,
		temperature.CompletedObservations, temperature.ElapsedMilliseconds, temperature.MaximumGapMilliseconds,
		temperature.Initial.CompletedMilliseconds, temperature.Final.RequestedMilliseconds,
		temperature.Final.CompletedMilliseconds)
}

func verifiedThermalView(t *testing.T, h *sessionHarness) *sessionevidence.ThermalView {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.directory, "session.hhpcr"))
	if err != nil {
		t.Fatal("signed report missing")
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var payload app.InventoryPayload
	if json.Unmarshal(artifact.InventoryJSON, &payload) != nil {
		t.Fatal("invalid signed payload")
	}
	view, err := app.ValidatedSessionThermal(payload)
	if err != nil || view == nil {
		t.Fatalf("shared consumer rejected signed thermal evidence: %v", err)
	}
	if h.journal.StopReason != view.StopReason {
		t.Fatal("returned journal differs from signed report")
	}
	ready, err := os.ReadFile(filepath.Join(h.directory, "session-ready.txt"))
	if err != nil || !bytes.Contains(ready, []byte("thermal_finding="+view.Finding)) || !bytes.Contains(ready, []byte("thermal_coverage="+view.Coverage)) || !bytes.Contains(ready, []byte("session_stop="+view.StopReason)) {
		t.Fatal("readiness differs from signed thermal evidence")
	}
	return view
}

func TestSessionRealThermalImmediateAllOffAndInitialHeat(t *testing.T) {
	for _, mode := range []string{"unavailable", "available", "initial-hot"} {
		t.Run(mode, func(t *testing.T) {
			excluded, err := plan.SelectableQuickTests("x86_64")
			if err != nil {
				t.Fatal(err)
			}
			temperature := ""
			if mode == "available" {
				temperature = "41000\n"
			}
			if mode == "initial-hot" {
				temperature = "98000\n"
				excluded = nil
			}
			h, _ := realThermalHarness(t, excluded, temperature)
			if code := h.run(t.Context()); code != 0 {
				t.Fatalf("live immediate session failed: %d %s", code, &h.stderr)
			}
			view := verifiedThermalView(t, h)
			if len(h.calls) != 0 || !h.journal.Thermal.Initial.Completed || !h.journal.Thermal.Final.Completed || view.AcceptedObservations < 2 {
				t.Fatal("immediate run omitted coverage or started hot work")
			}
			if (mode == "initial-hot") != (view.Finding == "limit_reached") {
				t.Fatal("initial heat classification changed")
			}
			if mode == "unavailable" && (view.Coverage != "unavailable" || view.PeakObserved) {
				t.Fatal("unavailable sensor promoted to pass")
			}
		})
	}
}

func TestSessionRealThermalCancelsEveryWorkerAndLatchesAfterCooling(t *testing.T) {
	for _, target := range sessionWorkerIDs {
		t.Run(target, func(t *testing.T) {
			h, write := realThermalHarness(t, nil, "41000\n")
			acceptedSafetySample := make(chan struct{})
			h.dependencies.StartMonitor = func(started time.Time, fail context.CancelFunc) (sessionSafetyMonitor, error) {
				observations := 0
				return safemode.StartMonitor(func(context.Context) safemode.Result {
					observations++
					// The owner admits another serialized observation only after
					// consuming the previous safe completion. Do not race heat
					// cancellation against the safety monitor's first due tick.
					if observations == 2 {
						close(acceptedSafetySample)
					}
					return journalSafeObservation()
				}, safemode.MonitorOptions{Started: started, Cancel: fail})
			}
			worker := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, errOut io.Writer) int {
				code := worker(ctx, w, in, out, errOut)
				if w.ID != target {
					return code
				}
				coverageDeadline := time.NewTimer(4 * time.Second)
				defer coverageDeadline.Stop()
				select {
				case <-acceptedSafetySample:
				case <-ctx.Done():
					t.Fatal("target worker cancelled before accepted safety coverage")
				case <-coverageDeadline.C:
					t.Fatal("safety monitor did not establish accepted coverage")
				}
				write("98000\n")
				select {
				case <-ctx.Done():
				case <-time.After(4 * time.Second):
					t.Fatal("real sampled heat did not cancel active worker")
				}
				write("41000\n")
				return code
			}
			if code := h.run(t.Context()); code != 0 {
				t.Fatalf("joined hot worker was not reportable: %d %s", code, &h.stderr)
			}
			view := verifiedThermalView(t, h)
			if h.calls[len(h.calls)-1] != target || h.journal.Monitor.CompletedObservations == 0 || view.Finding != "limit_reached" || h.journal.Thermal.FirstFinding == nil || h.journal.Thermal.Final.Observation.PeakMilliCelsius != 41000 || view.PeakMilliCelsius != 98000 {
				t.Fatal("heat latch, stop ordering, or cooling evidence lost")
			}
		})
	}
}

func TestSessionRealThermalCoversCleanupAndFinalInventory(t *testing.T) {
	for _, phase := range []string{"input-cleanup", "inventory", "pre-sign"} {
		t.Run(phase, func(t *testing.T) {
			h, write := realThermalHarness(t, nil, "41000\n")
			switch phase {
			case "input-cleanup":
				h.dependencies.FinishInput = func() error { h.inputFinished = true; write("98000\n"); return nil }
			case "inventory":
				useRealSessionFinalizer(t, h, func(context.Context) (inventory.Snapshot, error) { write("98000\n"); return h.snapshot, nil }, nil)
			case "pre-sign":
				observe := h.dependencies.ObserveSafety
				h.dependencies.ObserveSafety = func(ctx context.Context) safemode.Result {
					r := observe(ctx)
					if h.safetyCalls == 14 {
						write("98000\n")
					}
					return r
				}
			}
			if code := h.run(t.Context()); code != 0 {
				t.Fatalf("late heat failed finalization: %d %s", code, &h.stderr)
			}
			view := verifiedThermalView(t, h)
			if len(h.calls) != 12 || view.Finding != "limit_reached" || view.StopReason != "thermal_limit" || h.journal.Thermal.Final.Observation.PeakMilliCelsius != 98000 {
				t.Fatal("final inventory window did not retain late heat")
			}
		})
	}
}

func TestSessionLocalHeatSourceSurvivesUnavailableGlobalSensors(t *testing.T) {
	for _, id := range []string{"cpu.quick", "memory.quick"} {
		for _, status := range []quick.Status{quick.StatusAborted, quick.StatusFail} {
			t.Run(id+"/"+string(status), func(t *testing.T) {
				h := newSessionHarness(t, "x86_64", nil)
				useRealSessionFinalizer(t, h, nil, nil)
				worker := h.dependencies.RunWorker
				h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, errOut io.Writer) int {
					if w.ID != id {
						return worker(ctx, w, in, out, errOut)
					}
					h.calls = append(h.calls, w.ID)
					safety := completeCPUEvidence().Safety
					safety.ThermalSensors = 1
					safety.ThermalSamples = 1
					safety.ThermalCoverage = "available"
					safety.PeakMilliCelsius = 97000
					safety.MonitorStatus = "failed"
					safety.MonitorFailure = "thermal_limit"
					writeSessionFixture(t, w.OutputPath, quickSafetyFixture(id, status, safety))
					h.clock = h.clock.Add(20 * time.Millisecond)
					return 1
				}
				if code := h.run(t.Context()); code != 0 {
					t.Fatalf("local hot evidence rejected: %d %s", code, &h.stderr)
				}
				view := verifiedThermalView(t, h)
				if view.Finding != "limit_reached" || view.Coverage != "unavailable" || view.PeakObserved || len(view.LocalSources) != 1 || view.LocalSources[0].WorkerID != id || h.journal.Workers[len(h.calls)-1].Outcome != string(status) {
					t.Fatal("local heat or explicit worker failure downgraded")
				}
			})
		}
	}
}

func TestSessionRejectsChangedOrDisappearedLocalHeatBeforeSigning(t *testing.T) {
	for _, mutation := range []string{"cooler", "disappeared", "reformatted", "changed-heat"} {
		t.Run(mutation, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			random := &journalForbiddenRandom{}
			useRealSessionFinalizer(t, h, nil, random)
			var path string
			var captured []byte
			h.dependencies.RunWorker = func(_ context.Context, w sessionWorker, _ io.Reader, _, _ io.Writer) int {
				path = w.OutputPath
				h.calls = append(h.calls, w.ID)
				safety := completeCPUEvidence().Safety
				safety.ThermalSensors = 1
				safety.ThermalSamples = 1
				safety.ThermalCoverage = "available"
				safety.PeakMilliCelsius = 97000
				safety.MonitorStatus = "failed"
				safety.MonitorFailure = "thermal_limit"
				writeSessionFixture(t, path, quickSafetyFixture(w.ID, quick.StatusAborted, safety))
				captured, _ = os.ReadFile(path)
				h.clock = h.clock.Add(20 * time.Millisecond)
				return 0
			}
			h.dependencies.FinishInput = func() error {
				h.inputFinished = true
				switch mutation {
				case "cooler":
					writeSessionFixture(t, path, completeCPUEvidence())
				case "disappeared":
					return os.Remove(path)
				case "reformatted":
					return os.WriteFile(path, append(append([]byte(nil), captured...), '\n'), 0600)
				case "changed-heat":
					return os.WriteFile(path, bytes.Replace(captured, []byte("97000"), []byte("96000"), 1), 0600)
				}
				return nil
			}
			if code := h.run(t.Context()); code == 0 || random.calls != 0 {
				t.Fatalf("changed source reached signing: %d reads=%d", code, random.calls)
			}
			if _, err := os.Stat(filepath.Join(h.directory, "session.hhpcr")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("changed source produced report")
			}
			// The already-read source remains sufficient to reconstruct its original heat,
			// independently of the changed path. No second file read is part of this API.
			outcome, state, err := sessionWorkerOutcomeEvidence(captured, nil, quick.CPUSchema, 0, false)
			if err != nil {
				t.Fatal(err)
			}
			j := sessionJournal{}
			w := sessionWorkerRecord{ID: "cpu.quick", State: state, Outcome: outcome, Timing: &sessionevidence.WorkerTiming{}}
			if retainLocalThermalSource(&j, w, captured, nil) != nil || len(j.LocalThermalSources) != 1 || j.LocalThermalSources[0].PeakMilliCelsius != 97000 {
				t.Fatal("retained immutable source lost heat")
			}
		})
	}
}

func TestFinalizeRejectsSerializedSessionGate(t *testing.T) {
	fixture := newJournalFixture(t)
	arguments := fixture.args(t)
	arguments = append(arguments, "-session-journal", filepath.Join(fixture.directory, "session-journal.json"), "-run-stop", fixture.journal.StopReason)
	random := &journalForbiddenRandom{}
	var errOut bytes.Buffer
	if code := runFinalizeTestWith(t, t.Context(), arguments, io.Discard, &errOut, func(context.Context) (inventory.Snapshot, error) {
		t.Fatal("file replay reached inventory")
		return inventory.Snapshot{}, nil
	}, random, time.Now); code != 2 || random.calls != 0 || !strings.Contains(errOut.String(), "live coordinator gate") {
		t.Fatal("serialized journal became authority")
	}
}

func TestSessionFinalGateRequiresBothStopCallsUnderOneDeadline(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	entered := make(chan time.Time, 2)
	release := make(chan struct{})
	done := make(chan int, 1)
	h.dependencies.StartMonitor = func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error) {
		return &joiningSafetyFixture{stop: func(ctx context.Context) safemode.StopReceipt {
			d, _ := ctx.Deadline()
			entered <- d
			<-release
			return safemode.StopReceipt{Result: sessionMonitorSummary(0), Joined: true}
		}}, nil
	}
	h.dependencies.StartThermal = func(time.Time, context.CancelFunc, context.CancelFunc) (sessionThermalMonitor, error) {
		return &sessionThermalFixture{finding: make(chan struct{}), stop: func(ctx context.Context) thermal.Result {
			d, _ := ctx.Deadline()
			entered <- d
			<-release
			return sessionThermalSummary(0)
		}}, nil
	}
	go func() { done <- h.run(t.Context()) }()
	first, second := <-entered, <-entered
	if !first.Equal(second) || time.Until(first) > 2*time.Second || time.Until(first) <= 0 {
		t.Fatal("monitor joins did not share one bounded deadline")
	}
	select {
	case <-done:
		t.Fatal("finalization escaped active owners")
	default:
	}
	close(release)
	if code := <-done; code != 0 {
		t.Fatalf("joined owners failed: %d %s", code, &h.stderr)
	}
}

type joiningSafetyFixture struct {
	stop func(context.Context) safemode.StopReceipt
}

func (m *joiningSafetyFixture) Failed() bool { return false }
func (m *joiningSafetyFixture) StopWithJoin(ctx context.Context) safemode.StopReceipt {
	return m.stop(ctx)
}
