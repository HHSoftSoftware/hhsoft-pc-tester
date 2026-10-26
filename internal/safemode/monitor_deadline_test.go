package safemode

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

func monitorBlockedObserver(t *testing.T) (func(context.Context) Result, <-chan context.Context, func()) {
	t.Helper()
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	return func(ctx context.Context) Result { started <- ctx; <-release; return monitorSafeResult() }, started, unblock
}

func awaitObserver(t *testing.T, started <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-started:
		return ctx
	case <-time.After(5 * time.Second):
		t.Fatal("observer did not start")
		return nil
	}
}

func awaitMonitorDone(t *testing.T, monitor *Monitor) {
	t.Helper()
	select {
	case <-monitor.done:
	case <-time.After(5 * time.Second):
		t.Fatal("owned observer did not join")
	}
}

func TestMonitorLateCleanResultCannotClearDeadline(t *testing.T) {
	for _, deliverTimer := range []bool{true, false} {
		name := "result selected before timer"
		if deliverTimer {
			name = "deadline signals while observer blocked"
		}
		t.Run(name, func(t *testing.T) {
			clock := newMonitorClock()
			observe, started, release := monitorBlockedObserver(t)
			monitor := startTestMonitor(t, clock, observe, nil)
			clock.advance(time.Second, true)
			ctx := awaitObserver(t, started)
			clock.advance(time.Second, deliverTimer)
			if deliverTimer {
				awaitMonitor(t, "observation timeout", monitor.Failed)
				if ctx.Err() != context.DeadlineExceeded {
					t.Fatalf("deadline context: %v", ctx.Err())
				}
			}
			release()
			awaitMonitorDone(t, monitor)
			result := monitor.Stop(context.Background())
			if result.Status != "error" || !result.CleanupConfirmed || result.FirstFailure == nil || result.FirstFailure.Reason != "observation_timeout" ||
				result.AttemptedObservations != 1 || result.CompletedObservations != 1 || result.LastCompletedMilliseconds != 2000 ||
				result.LastObservation == nil || !strictMonitorSafety(*result.LastObservation) {
				t.Fatalf("late clean result: %+v", result)
			}
		})
	}
}

func TestMonitorStopLetsValidObservationFinish(t *testing.T) {
	clock := newMonitorClock()
	observe, started, release := monitorBlockedObserver(t)
	monitor := startTestMonitor(t, clock, observe, nil)
	clock.advance(time.Second, true)
	ctx := awaitObserver(t, started)
	stopped := make(chan MonitorResult, 1)
	go func() { stopped <- monitor.Stop(context.Background()) }()
	awaitMonitor(t, "stop request", func() bool { monitor.mutex.Lock(); defer monitor.mutex.Unlock(); return monitor.stopRequested })
	if ctx.Err() != nil {
		t.Fatal("stop cancelled a still-valid observation")
	}
	clock.advance(250*time.Millisecond, true)
	release()
	select {
	case result := <-stopped:
		if result.Status != "pass" || !result.CleanupConfirmed || result.FirstFailure != nil || result.CompletedObservations != 1 ||
			result.ElapsedMilliseconds != 1250 || result.FirstCompletedMilliseconds != 1250 || result.MaximumGapMilliseconds != 1250 {
			t.Fatalf("valid stop result: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return after observer joined")
	}
}

func TestMonitorIgnoringContextMakesCleanupUncertainAndImmutable(t *testing.T) {
	clock := newMonitorClock()
	observe, started, release := monitorBlockedObserver(t)
	monitor := startTestMonitor(t, clock, observe, nil)
	clock.advance(time.Second, true)
	ctx := awaitObserver(t, started)
	clock.advance(time.Second, true)
	awaitMonitor(t, "observation timeout", monitor.Failed)
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatal("observer deadline did not cancel context")
	}
	stopContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := monitor.Stop(stopContext)
	if result.Status != "error" || result.CleanupConfirmed || result.FirstFailure == nil || result.FirstFailure.Reason != "observation_timeout" ||
		result.AttemptedObservations != 1 || result.CompletedObservations != 0 {
		t.Fatalf("unconfirmed cleanup: %+v", result)
	}
	baseline := cloneMonitorResult(result)
	result.FirstFailure.Reason = "caller mutation"
	release()
	awaitMonitorDone(t, monitor)
	clock.advance(10*time.Second, true)
	if again := monitor.Stop(context.Background()); !reflect.DeepEqual(again, baseline) {
		t.Fatalf("uncertainty changed after late join: %+v", again)
	}
}

func TestMonitorStopDeadlineCannotBeWonByLateJoin(t *testing.T) {
	clock := newMonitorClock()
	observe, started, release := monitorBlockedObserver(t)
	monitor := startTestMonitor(t, clock, observe, nil)
	clock.advance(time.Second, true)
	awaitObserver(t, started)
	stopped := make(chan MonitorResult, 1)
	go func() { stopped <- monitor.Stop(context.Background()) }()
	awaitMonitor(t, "stop timer", func() bool {
		clock.mutex.Lock()
		defer clock.mutex.Unlock()
		for timer := range clock.timers {
			if timer.due.Equal(clock.current.Add(MonitorStopTimeout)) {
				return true
			}
		}
		return false
	})
	// Do not fire either timer: only the late observer completion wakes the
	// supervisor. Stop must compare the actual join timestamp against its bound.
	clock.advance(MonitorStopTimeout, false)
	release()
	select {
	case result := <-stopped:
		if result.CleanupConfirmed || result.Status == "pass" || result.FirstFailure == nil {
			t.Fatalf("late join claimed cleanup: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("late join did not release stop")
	}
}
