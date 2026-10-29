package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
)

// The coordinator's deterministic seam keeps phase-snapshot assertions separate
// from the concurrent monitor engine, which has its own controlled-clock tests.
type sessionMonitorFixture struct {
	failed bool
	stops  int
	result func() safemode.MonitorResult
}

func (monitor *sessionMonitorFixture) Failed() bool { return monitor.failed }
func (monitor *sessionMonitorFixture) StopWithJoin(ctx context.Context) safemode.StopReceipt {
	monitor.stops++
	r := monitor.result()
	return safemode.StopReceipt{Result: r, Joined: r.CleanupConfirmed, JoinedMilliseconds: r.ElapsedMilliseconds}
}

func sessionMonitorSummary(elapsed int64) safemode.MonitorResult {
	result := safemode.MonitorResult{Schema: safemode.MonitorSchema, Status: "pass",
		IntervalMilliseconds: 1000, ObservationTimeoutMilliseconds: 1000, MaximumAllowedGapMilliseconds: 2500,
		CleanupConfirmed: true, ElapsedMilliseconds: elapsed, MaximumGapMilliseconds: elapsed}
	if count := elapsed / 1000; count > 0 {
		result.AttemptedObservations, result.CompletedObservations = uint64(count), uint64(count)
		result.FirstCompletedMilliseconds, result.LastCompletedMilliseconds = 1000, count*1000
		result.MaximumGapMilliseconds = 1000
		result.LastObservation = &safemode.Result{Schema: safemode.Schema, Status: "pass", StorageObserved: true, NetworkObserved: true}
	}
	return result
}

func TestSessionMonitorFailureOverridesCancellationAndLaterSafeSnapshots(t *testing.T) {
	for _, target := range []string{"cpu.quick", "input.pointer.quick", "display.operator.quick"} {
		t.Run(target, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			ctx, operatorCancel := context.WithCancel(t.Context())
			defer operatorCancel()
			monitor := &sessionMonitorFixture{}
			var cancelWork context.CancelFunc
			h.dependencies.StartMonitor = func(started time.Time, cancel context.CancelFunc) (sessionSafetyMonitor, error) {
				cancelWork = cancel
				return monitor, nil
			}
			monitor.result = func() safemode.MonitorResult {
				result := sessionMonitorSummary(1500)
				result.Status = "fail"
				result.FirstFailure = &safemode.MonitorFailure{Reason: "unsafe_observation", ElapsedMilliseconds: 1200,
					Observation: &safemode.Result{Schema: safemode.Schema, Status: "fail", StorageObserved: true, NetworkObserved: true, ActiveNonLoopInterfaces: 1}}
				return result
			}
			worker := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, errOut io.Writer) int {
				code := worker(ctx, w, in, out, errOut)
				if w.ID == target {
					monitor.failed = true
					cancelWork()
					operatorCancel()
					if ctx.Err() == nil {
						t.Fatal("monitor did not cancel active worker")
					}
				}
				return code
			}
			if code := h.run(ctx); code != 1 || h.finalized || monitor.stops != 1 || h.calls[len(h.calls)-1] != target {
				t.Fatalf("monitor failure allowed work or signing: code=%d stops=%d", code, monitor.stops)
			}
			var retained safemode.MonitorResult
			raw, err := os.ReadFile(filepath.Join(h.directory, "session-safety-monitor.json"))
			if err != nil || json.Unmarshal(raw, &retained) != nil || retained.FirstFailure == nil || retained.FirstFailure.Observation.ActiveNonLoopInterfaces != 1 {
				t.Fatal("first unsafe observation was not retained")
			}
			for _, name := range []string{"session-journal.json", "session-ready.txt", "session.hhpcr"} {
				if _, err := os.Stat(filepath.Join(h.directory, name)); !os.IsNotExist(err) {
					t.Fatal("unsafe monitor produced signable output")
				}
			}
		})
	}
}

func TestSessionMonitorJoinsOnEveryExitAndBlocksUncertainOrMalformedSummary(t *testing.T) {
	for _, failure := range []string{"preflight", "worker-cleanup", "input-cleanup", "final-safety", "monitor-cleanup", "monitor-timeout", "monitor-gap", "monitor-late-failure", "monitor-save", "monitor-invalid"} {
		t.Run(failure, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			monitor := &sessionMonitorFixture{result: func() safemode.MonitorResult {
				result := sessionMonitorSummary(0)
				switch failure {
				case "monitor-cleanup":
					result.CleanupConfirmed = false
				case "monitor-invalid":
					result.IntervalMilliseconds = 2
				case "monitor-gap":
					result = sessionMonitorSummary(3000)
					result.MaximumGapMilliseconds = 3000
				case "monitor-timeout", "monitor-late-failure":
					result.Status = "error"
					result.FirstFailure = &safemode.MonitorFailure{Reason: "observation_timeout"}
				}
				return result
			}}
			h.dependencies.StartMonitor = func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error) {
				if failure == "monitor-save" {
					writeSessionFixture(t, filepath.Join(h.directory, "session-safety-monitor.json"), "preserved")
				}
				return monitor, nil
			}
			if failure == "worker-cleanup" {
				h.dependencies.RunWorker = func(context.Context, sessionWorker, io.Reader, io.Writer, io.Writer) int { return 12 }
			}
			if failure == "input-cleanup" {
				h.dependencies.FinishInput = func() error { return errors.New("synthetic join failure") }
			}
			observe := h.dependencies.ObserveSafety
			h.dependencies.ObserveSafety = func(ctx context.Context) safemode.Result {
				result := observe(ctx)
				if failure == "preflight" || failure == "final-safety" && h.inputFinished {
					result.Status = "error"
				}
				if failure == "monitor-late-failure" && h.inputFinished {
					monitor.failed = true
				}
				return result
			}
			code := h.run(t.Context())
			want := 1
			if failure == "worker-cleanup" || failure == "monitor-cleanup" {
				want = 12
			}
			if code != want || monitor.stops != 1 || h.finalized {
				t.Fatalf("code=%d stops=%d finalized=%t", code, monitor.stops, h.finalized)
			}
		})
	}
}

func TestSessionMonitorWindowIncludesInputCleanupAndFinalSafety(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	h.dependencies.FinishInput = func() error { h.inputFinished = true; h.clock = h.clock.Add(2500 * time.Millisecond); return nil }
	var earlyStop atomic.Bool
	monitor := &sessionMonitorFixture{result: func() safemode.MonitorResult {
		earlyStop.Store(!h.inputFinished || h.safetyCalls != 15)
		return sessionMonitorSummary(2500)
	}}
	h.dependencies.StartMonitor = func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error) { return monitor, nil }
	if code := h.run(t.Context()); code != 0 || earlyStop.Load() || monitor.stops != 1 || h.journal.ElapsedMilliseconds != 2500 || h.journal.Monitor.CompletedObservations != 2 {
		t.Fatalf("coverage window not preserved: code=%d stops=%d", code, monitor.stops)
	}
}

func TestSessionMonitorStartFailureCannotRunWorkers(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	h.dependencies.StartMonitor = func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error) {
		return nil, errors.New("synthetic start failure")
	}
	if code := h.run(t.Context()); code != 1 || len(h.calls) != 0 || h.safetyCalls != 0 || h.finalized {
		t.Fatal("monitor start failure allowed work")
	}
}

func TestSessionRealMonitorCancelsBlockedAdapter(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	// Synthetic observer data tests the real concurrent cancellation path without
	// changing the machine's mounts, interfaces, or device state.
	h.dependencies.StartMonitor = func(started time.Time, cancel context.CancelFunc) (sessionSafetyMonitor, error) {
		return safemode.StartMonitor(func(context.Context) safemode.Result {
			return safemode.Result{Schema: safemode.Schema, Status: "fail", StorageObserved: true,
				NetworkObserved: true, WritableBlockMounts: 1}
		}, safemode.MonitorOptions{Started: started, Cancel: cancel})
	}
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, _ io.Reader, _, _ io.Writer) int {
		h.calls = append(h.calls, worker.ID)
		select {
		case <-ctx.Done():
			return 1
		case <-time.After(5 * time.Second):
			t.Fatal("periodic safety failure did not stop active adapter")
			return 1
		}
	}
	if code := h.run(t.Context()); code != 1 || h.finalized || len(h.calls) != 1 || h.calls[0] != "cpu.quick" {
		t.Fatalf("real monitor allowed later work or signing: code=%d", code)
	}
	var result safemode.MonitorResult
	raw, err := os.ReadFile(filepath.Join(h.directory, "session-safety-monitor.json"))
	if err != nil || json.Unmarshal(raw, &result) != nil || !result.CleanupConfirmed ||
		result.FirstFailure == nil || result.FirstFailure.Reason != "unsafe_observation" ||
		result.FirstFailure.Observation == nil || result.FirstFailure.Observation.WritableBlockMounts != 1 {
		t.Fatal("real monitor failure or clean join was not retained")
	}
}
