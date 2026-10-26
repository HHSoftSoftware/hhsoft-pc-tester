package safemode

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testMonitorClock struct {
	mutex   sync.Mutex
	current time.Time
	timers  map[*testMonitorTimer]bool
}

type testMonitorTimer struct {
	clock *testMonitorClock
	due   time.Time
	value chan time.Time
}

func newMonitorClock() *testMonitorClock {
	return &testMonitorClock{current: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), timers: map[*testMonitorTimer]bool{}}
}
func (clock *testMonitorClock) now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.current
}
func (clock *testMonitorClock) timer(duration time.Duration) monitorTimer {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	timer := &testMonitorTimer{clock: clock, due: clock.current.Add(duration), value: make(chan time.Time, 1)}
	if duration <= 0 {
		timer.value <- clock.current
	} else {
		clock.timers[timer] = true
	}
	return timer
}
func (timer *testMonitorTimer) channel() <-chan time.Time { return timer.value }
func (timer *testMonitorTimer) stop() bool {
	timer.clock.mutex.Lock()
	defer timer.clock.mutex.Unlock()
	active := timer.clock.timers[timer]
	delete(timer.clock.timers, timer)
	return active
}
func (clock *testMonitorClock) advance(duration time.Duration, deliver bool) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.current = clock.current.Add(duration)
	if deliver {
		for timer := range clock.timers {
			if !timer.due.After(clock.current) {
				timer.value <- clock.current
				delete(clock.timers, timer)
			}
		}
	}
}
func (clock *testMonitorClock) waiting() bool {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return len(clock.timers) > 0
}

func monitorSafeResult() Result {
	return Result{Schema: Schema, Status: "pass", StorageObserved: true, NetworkObserved: true}
}

func awaitMonitor(t *testing.T, description string, ready func() bool) {
	t.Helper()
	deadline, tick := time.NewTimer(5*time.Second), time.NewTicker(time.Millisecond)
	defer deadline.Stop()
	defer tick.Stop()
	for !ready() {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("monitor did not reach %s", description)
		}
	}
}

func monitorSnapshot(monitor *Monitor) MonitorResult {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	return cloneMonitorResult(monitor.result)
}

func awaitMonitorCount(t *testing.T, monitor *Monitor, count uint64) {
	t.Helper()
	awaitMonitor(t, "completed observation", func() bool { return monitorSnapshot(monitor).CompletedObservations >= count })
}

func startTestMonitor(t *testing.T, clock *testMonitorClock, observe func(context.Context) Result, cancel context.CancelFunc) *Monitor {
	t.Helper()
	monitor, err := startMonitor(observe, MonitorOptions{Started: clock.now(), Cancel: cancel}, clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		monitor.Stop(ctx)
	})
	awaitMonitor(t, "initial timer", clock.waiting)
	return monitor
}

func TestMonitorShortRunAndPeriodicCoverage(t *testing.T) {
	t.Run("short run discloses no samples", func(t *testing.T) {
		clock := newMonitorClock()
		var calls atomic.Int32
		monitor := startTestMonitor(t, clock, func(context.Context) Result { calls.Add(1); return monitorSafeResult() }, nil)
		clock.advance(999*time.Millisecond, true)
		result := monitor.Stop(context.Background())
		if result.Status != "pass" || !result.CleanupConfirmed || result.FirstFailure != nil || result.LastObservation != nil ||
			result.AttemptedObservations != 0 || result.CompletedObservations != 0 || result.ElapsedMilliseconds != 999 ||
			result.MaximumGapMilliseconds != 999 || calls.Load() != 0 {
			t.Fatalf("short coverage: %+v", result)
		}
		clock.advance(10*time.Second, true)
		if again := monitor.Stop(context.Background()); !reflect.DeepEqual(again, result) {
			t.Fatal("stopped summary changed")
		}
	})
	t.Run("two samples and both uncovered edges", func(t *testing.T) {
		clock := newMonitorClock()
		monitor := startTestMonitor(t, clock, func(ctx context.Context) Result {
			deadline, ok := ctx.Deadline()
			if !ok || deadline.Sub(clock.now()) != MonitorObservationTimeout || ctx.Err() != nil {
				return Result{}
			}
			return monitorSafeResult()
		}, nil)
		clock.advance(time.Second, true)
		awaitMonitorCount(t, monitor, 1)
		clock.advance(time.Second, true)
		awaitMonitorCount(t, monitor, 2)
		clock.advance(400*time.Millisecond, true)
		result := monitor.Stop(context.Background())
		if result.Status != "pass" || !result.CleanupConfirmed || result.FirstFailure != nil || result.AttemptedObservations != 2 ||
			result.CompletedObservations != 2 || result.ElapsedMilliseconds != 2400 || result.FirstCompletedMilliseconds != 1000 ||
			result.LastCompletedMilliseconds != 2000 || result.MaximumGapMilliseconds != 1000 || result.LastObservation == nil || !strictMonitorSafety(*result.LastObservation) {
			t.Fatalf("periodic coverage: %+v", result)
		}
	})
	t.Run("reported gaps share offset rounding", func(t *testing.T) {
		clock := newMonitorClock()
		monitor := startTestMonitor(t, clock, func(context.Context) Result { clock.advance(900*time.Microsecond, false); return monitorSafeResult() }, nil)
		clock.advance(time.Second, true)
		awaitMonitorCount(t, monitor, 1)
		clock.advance(200*time.Microsecond, true)
		result := monitor.Stop(context.Background())
		if result.Status != "pass" || result.FirstCompletedMilliseconds != 1000 || result.ElapsedMilliseconds != 1001 || result.MaximumGapMilliseconds != 1000 {
			t.Fatalf("rounded coverage: %+v", result)
		}
	})
}

func TestMonitorFifteenMinuteCoverageRetainsOnlyBoundedAggregates(t *testing.T) {
	clock := newMonitorClock()
	monitor := startTestMonitor(t, clock, func(context.Context) Result { return monitorSafeResult() }, nil)
	const samples = uint64(900)
	for count := uint64(1); count <= samples; count++ {
		clock.advance(time.Second, true)
		// Acknowledge each completion before the next synthetic tick, without
		// waiting for the simulated fifteen-minute observation window.
		awaitMonitorCount(t, monitor, count)
	}
	// Include the final join window after the maximum session work duration.
	clock.advance(250*time.Millisecond, true)
	result := monitor.Stop(context.Background())
	if result.Status != "pass" || !result.CleanupConfirmed || result.FirstFailure != nil ||
		result.AttemptedObservations != samples || result.CompletedObservations != samples ||
		result.FirstCompletedMilliseconds != 1000 || result.LastCompletedMilliseconds != 900000 ||
		result.ElapsedMilliseconds != 900250 || result.MaximumGapMilliseconds != 1000 {
		t.Fatalf("long coverage aggregate: %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 1024 {
		t.Fatalf("monitor evidence grew with sample count: bytes=%d, err=%v", len(encoded), err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	want := []string{"schema", "status", "interval_ms", "observation_timeout_ms", "maximum_allowed_gap_ms", "attempted_observations",
		"completed_observations", "elapsed_ms", "first_completed_ms", "last_completed_ms", "maximum_gap_ms", "cleanup_confirmed", "last_observation"}
	if len(fields) != len(want) {
		t.Fatal("monitor evidence contains unexpected per-sample state")
	}
	for _, key := range want {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing bounded aggregate %s", key)
		}
	}
}
