package safemode

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMonitorUnsafeSnapshotLatchesBeforeCancellation(t *testing.T) {
	clock := newMonitorClock()
	var calls, cancellations atomic.Int32
	var recovered atomic.Bool
	var monitor *Monitor
	monitor = startTestMonitor(t, clock, func(context.Context) Result {
		result := monitorSafeResult()
		if calls.Add(1) == 2 && !recovered.Load() {
			result.Status, result.ActiveNonLoopInterfaces = "fail", 1
		}
		return result
	}, func() {
		if !monitor.Failed() {
			panic("failure was not latched before cancellation")
		}
		cancellations.Add(1)
	})
	clock.advance(time.Second, true)
	awaitMonitorCount(t, monitor, 1)
	clock.advance(time.Second, true)
	awaitMonitor(t, "unsafe failure and cancellation", func() bool { return monitor.Failed() && cancellations.Load() == 1 })
	recovered.Store(true)
	clock.advance(time.Second, true)
	result := monitor.Stop(context.Background())
	if result.Status != "fail" || !result.CleanupConfirmed || result.FirstFailure == nil || result.FirstFailure.Reason != "unsafe_observation" ||
		result.FirstFailure.Observation == nil || result.FirstFailure.Observation.ActiveNonLoopInterfaces != 1 ||
		result.CompletedObservations != 2 || calls.Load() != 2 || cancellations.Load() != 1 {
		t.Fatalf("unsafe latch: %+v, calls=%d", result, calls.Load())
	}
}

func TestMonitorUnknownAndContradictorySnapshotsFailClosed(t *testing.T) {
	cases := map[string]func(*Result){
		"schema":                 func(value *Result) { value.Schema = "untrusted schema" },
		"status":                 func(value *Result) { value.Status = "untrusted status" },
		"missing storage":        func(value *Result) { value.StorageObserved = false },
		"missing network":        func(value *Result) { value.NetworkObserved = false },
		"negative mount count":   func(value *Result) { value.WritableBlockMounts = -1 },
		"negative network count": func(value *Result) { value.ActiveNonLoopInterfaces = -1 },
		"unsafe pass":            func(value *Result) { value.ActiveNonLoopInterfaces = 1 },
		"storage detail":         func(value *Result) { value.StorageDetail = "untrusted location" },
		"network detail":         func(value *Result) { value.NetworkDetail = "untrusted location" },
		"error":                  func(value *Result) { value.Status = "error" },
		"aborted":                func(value *Result) { value.Status = "aborted" },
		"unexplained fail":       func(value *Result) { value.Status = "fail" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			clock := newMonitorClock()
			value := monitorSafeResult()
			mutate(&value)
			monitor := startTestMonitor(t, clock, func(context.Context) Result { return value }, nil)
			clock.advance(time.Second, true)
			awaitMonitor(t, "invalid observation failure", monitor.Failed)
			result := monitor.Stop(context.Background())
			if result.Status != "error" || !result.CleanupConfirmed || result.FirstFailure == nil || result.FirstFailure.Reason != "invalid_observation" ||
				result.CompletedObservations != 1 || result.FirstFailure.Observation == nil {
				t.Fatalf("invalid result accepted: %+v", result)
			}
			encoded, err := json.Marshal(result)
			if err != nil || strings.Contains(string(encoded), "untrusted") {
				t.Fatal("arbitrary observer text entered evidence")
			}
		})
	}
}

func TestMonitorMissingAndExcessiveCoverageFails(t *testing.T) {
	for _, test := range []struct {
		name          string
		before, after time.Duration
		samples       uint64
		expectedGap   int64
	}{
		{"no samples at one second", 0, time.Second, 0, 1000},
		{"missed initial sampling gap", 0, 3 * time.Second, 0, 3000},
		{"raw gap cannot round into success", 0, MonitorMaximumGap + time.Nanosecond, 0, 2500},
		{"missed trailing gap", time.Second, 3 * time.Second, 1, 3000},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock := newMonitorClock()
			monitor := startTestMonitor(t, clock, func(context.Context) Result { return monitorSafeResult() }, nil)
			if test.before > 0 {
				clock.advance(test.before, true)
				awaitMonitorCount(t, monitor, test.samples)
			}
			// Suppress timer delivery to model a scheduler which misses a wakeup.
			// Stop must validate elapsed coverage independently of timer selection.
			clock.advance(test.after, false)
			result := monitor.Stop(context.Background())
			if result.Status != "error" || result.FirstFailure == nil || result.FirstFailure.Reason != "sampling_gap" ||
				!result.CleanupConfirmed || result.CompletedObservations != test.samples || result.MaximumGapMilliseconds != test.expectedGap {
				t.Fatalf("missed coverage accepted: %+v", result)
			}
		})
	}
}

func TestMonitorObserverPanicIsBoundedAndSanitized(t *testing.T) {
	clock := newMonitorClock()
	monitor := startTestMonitor(t, clock, func(context.Context) Result { panic("untrusted panic location") }, nil)
	clock.advance(time.Second, true)
	awaitMonitor(t, "observer panic failure", monitor.Failed)
	result := monitor.Stop(context.Background())
	encoded, _ := json.Marshal(result)
	if result.Status != "error" || !result.CleanupConfirmed || result.FirstFailure == nil || result.FirstFailure.Reason != "observer_panic" ||
		result.AttemptedObservations != 1 || result.CompletedObservations != 0 || result.LastObservation != nil || strings.Contains(string(encoded), "untrusted") {
		t.Fatalf("panic result: %+v", result)
	}
}
