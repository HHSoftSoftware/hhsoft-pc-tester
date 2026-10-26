package safemode

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMonitorStopBoundReturnsWithoutAbandoningOwnership(t *testing.T) {
	clock := newMonitorClock()
	observe, started, release := monitorBlockedObserver(t)
	monitor := startTestMonitor(t, clock, observe, nil)
	clock.advance(time.Second, true)
	awaitObserver(t, started)
	stopped := make(chan MonitorResult, 1)
	go func() { stopped <- monitor.Stop(context.Background()) }()
	awaitMonitor(t, "fixed stop bound", func() bool {
		clock.mutex.Lock()
		defer clock.mutex.Unlock()
		for timer := range clock.timers {
			if timer.due.Equal(clock.current.Add(MonitorStopTimeout)) {
				return true
			}
		}
		return false
	})
	clock.advance(MonitorStopTimeout, true)
	var result MonitorResult
	select {
	case result = <-stopped:
		if result.Status == "pass" || result.CleanupConfirmed || result.FirstFailure == nil {
			t.Fatalf("blocked observer claimed cleanup: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop ignored its fixed upper bound")
	}
	select {
	case <-monitor.done:
		t.Fatal("observer reported joined while still blocked")
	default:
	}
	release()
	awaitMonitorDone(t, monitor)
	if again := monitor.Stop(context.Background()); !reflect.DeepEqual(again, result) {
		t.Fatal("late return cleared frozen uncertainty")
	}
}

func TestMonitorStaggeredStopsCannotRenewFirstCleanupDeadline(t *testing.T) {
	clock := newMonitorClock()
	observe, started, release := monitorBlockedObserver(t)
	monitor := startTestMonitor(t, clock, observe, nil)
	clock.advance(time.Second, true)
	awaitObserver(t, started)
	// Pause the first Stop caller after the actual shared request transition,
	// before it selects a timer or freezes a result. The second caller runs the
	// complete public Stop path and can therefore reach freeze first.
	firstDeadline := monitor.requestStop(context.Background())
	clock.advance(500*time.Millisecond, false)
	stopped := make(chan MonitorResult, 1)
	go func() { stopped <- monitor.Stop(context.Background()) }()
	awaitMonitor(t, "second stop wait", func() bool {
		clock.mutex.Lock()
		defer clock.mutex.Unlock()
		for timer := range clock.timers {
			if !timer.due.Before(firstDeadline) {
				return true
			}
		}
		return false
	})
	// Join after the original two seconds but before a renewed second-caller
	// bound. Suppress timer delivery so late completion wins both select races.
	clock.advance(1600*time.Millisecond, false)
	release()
	select {
	case result := <-stopped:
		if result.CleanupConfirmed || result.Status == "pass" {
			t.Fatalf("later caller renewed cleanup window: %+v", result)
		}
		if again := monitor.Stop(context.Background()); !reflect.DeepEqual(again, result) {
			t.Fatal("first caller received a different frozen result")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("staggered stop did not return")
	}
}

func TestMonitorConcurrentStopsReturnIndependentImmutableSnapshots(t *testing.T) {
	clock := newMonitorClock()
	var cancellations atomic.Int32
	monitor := startTestMonitor(t, clock, func(context.Context) Result { return monitorSafeResult() }, func() { cancellations.Add(1) })
	clock.advance(time.Second, true)
	awaitMonitorCount(t, monitor, 1)
	clock.advance(250*time.Millisecond, true)
	const callers = 32
	results := make(chan MonitorResult, callers)
	var wait sync.WaitGroup
	for index := 0; index < callers; index++ {
		wait.Add(1)
		go func() { defer wait.Done(); results <- monitor.Stop(context.Background()) }()
	}
	wait.Wait()
	baseline := <-results
	if baseline.Status != "pass" || !baseline.CleanupConfirmed || baseline.LastObservation == nil {
		t.Fatalf("initial stop: %+v", baseline)
	}
	for index := 1; index < callers; index++ {
		result := <-results
		if !reflect.DeepEqual(result, baseline) {
			t.Fatal("concurrent callers received inconsistent summaries")
		}
		result.LastObservation.Status = "caller mutation"
	}
	clock.advance(10*time.Second, true)
	// Model a second Stop which passed its early cached-result check before the
	// first caller froze the result, then reached freeze much later.
	if again := monitor.freeze(true, clock.now()); !reflect.DeepEqual(again, baseline) || monitor.Failed() || cancellations.Load() != 0 {
		t.Fatal("a delayed second freeze changed a clean stopped monitor")
	}
	if again := monitor.freeze(false, clock.now()); !reflect.DeepEqual(again, baseline) || monitor.Failed() {
		t.Fatal("later uncertainty mutated an immutable result")
	}
}

func TestMonitorActualTimerWithOwnedMetadataFixture(t *testing.T) {
	root := fixture(t)
	monitor, err := StartMonitor(func(ctx context.Context) Result { return check(ctx, root) }, MonitorOptions{Started: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { monitor.Stop(context.Background()) })
	awaitMonitorCount(t, monitor, 1)
	result := monitor.Stop(context.Background())
	if result.Schema != MonitorSchema || result.Status != "pass" || !result.CleanupConfirmed || result.FirstFailure != nil ||
		result.CompletedObservations == 0 || result.AttemptedObservations != result.CompletedObservations || result.FirstCompletedMilliseconds < 1000 ||
		result.ElapsedMilliseconds < result.LastCompletedMilliseconds || result.MaximumGapMilliseconds > 2500 ||
		result.LastObservation == nil || !strictMonitorSafety(*result.LastObservation) {
		t.Fatalf("real timer/metadata coverage: %+v", result)
	}
}

func TestMonitorInvalidArgumentsAndClockRollback(t *testing.T) {
	clock := newMonitorClock()
	observe := func(context.Context) Result { return monitorSafeResult() }
	for _, test := range []struct {
		observe func(context.Context) Result
		started time.Time
		clock   monitorClock
	}{
		{nil, clock.now(), clock}, {observe, time.Time{}, clock}, {observe, clock.now(), nil}, {observe, clock.now().Add(time.Second), clock},
	} {
		if _, err := startMonitor(test.observe, MonitorOptions{Started: test.started}, test.clock); err == nil {
			t.Fatal("invalid monitor arguments accepted")
		}
	}
	monitor := startTestMonitor(t, clock, observe, nil)
	clock.advance(-time.Nanosecond, false)
	result := monitor.Stop(context.Background())
	if result.Status != "error" || !result.CleanupConfirmed || result.FirstFailure == nil || result.FirstFailure.Reason != "clock_error" {
		t.Fatalf("clock rollback accepted: %+v", result)
	}
}
