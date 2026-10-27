package periodic

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type phaseAggregate struct{ Count, Peak int }

type admissionClock struct {
	*testMonitorClock
	armed             atomic.Bool
	captured, release chan struct{}
}

func (clock *admissionClock) now() time.Time {
	value := clock.testMonitorClock.now()
	if clock.armed.CompareAndSwap(true, false) {
		close(clock.captured)
		<-clock.release
	}
	return value
}

func TestFinalDispatchCannotReuseClockReadBeforeAdmission(t *testing.T) {
	base := newMonitorClock()
	clock := &admissionClock{testMonitorClock: base, captured: make(chan struct{}), release: make(chan struct{})}
	options := testOptions(base.now(), nil)
	options.Mode = InitialPeriodicFinal
	monitor, err := startMonitor(func(context.Context) int { return 10 }, phaseAssess, phaseReduce, options, clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { monitor.Stop(context.Background()) })
	if err := monitor.WaitInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitMonitor(t, "periodic wait", base.waiting)
	clock.armed.Store(true)
	base.advance(time.Second, true)
	select {
	case <-clock.captured:
	case <-time.After(time.Second):
		t.Fatal("loop clock was not captured")
	}
	base.advance(5*time.Millisecond, false)
	finished := make(chan error, 1)
	go func() { finished <- finalRead(monitor) }()
	awaitMonitor(t, "admission after stale clock capture", func() bool { monitor.mutex.Lock(); defer monitor.mutex.Unlock(); return monitor.finalRequested })
	close(clock.release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	result := monitor.Stop(nil)
	if result.Final.RequestedMilliseconds != 1005 || result.Final.StartedMilliseconds < 1005 || result.FirstFailure != nil {
		t.Fatalf("backdated final dispatch: %+v", result)
	}
}

func phaseAssess(value int) Assessment[int] {
	if value < 0 {
		return Assessment[int]{FailureReason: "invalid_observation"}
	}
	result := Assessment[int]{Value: value}
	if value >= 90 {
		result.FindingReason = "thermal_limit"
	}
	return result
}

func phaseReduce(prior phaseAggregate, observation Observation[int]) Reduction[phaseAggregate] {
	if observation.EligibleForAggregation {
		prior.Count++
		prior.Peak = max(prior.Peak, observation.Assessment.Value)
	}
	return Reduction[phaseAggregate]{Value: prior}
}

func phaseSnapshot(monitor *Monitor[int, phaseAggregate]) Summary[int, phaseAggregate] {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	return cloneSummary(monitor.result)
}

func phaseMonitor(t *testing.T, clock *testMonitorClock, observe func(context.Context) int, reduce Reducer[int, phaseAggregate], options Options) *Monitor[int, phaseAggregate] {
	t.Helper()
	options.Mode = InitialPeriodicFinal
	monitor, err := startMonitor(observe, phaseAssess, reduce, options, clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		monitor.Stop(ctx)
	})
	return monitor
}

func finalRead(monitor *Monitor[int, phaseAggregate]) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return monitor.ObserveFinal(ctx)
}

func TestPhasesAllOffStillOwnsTwoFreshReadsAndIndependentCopies(t *testing.T) {
	clock := newMonitorClock()
	var calls atomic.Int32
	monitor := phaseMonitor(t, clock, func(context.Context) int { return int(calls.Add(1)) }, phaseReduce, testOptions(clock.now(), nil))
	if err := monitor.WaitInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := finalRead(monitor); err != nil {
		t.Fatal(err)
	}
	result := monitor.Stop(context.Background())
	if !result.CleanupConfirmed || !result.Joined || result.FirstFailure != nil ||
		result.AttemptedObservations != 2 || result.CompletedObservations != 2 ||
		!result.Initial.Completed || !result.Final.Completed || *result.Initial.Observation != 1 || *result.Final.Observation != 2 ||
		result.Aggregate != (phaseAggregate{Count: 2, Peak: 2}) || calls.Load() != 2 {
		t.Fatalf("phase accounting: %+v", result)
	}
	before := cloneSummary(result)
	*result.Initial.Observation, *result.Final.Observation, *result.LastObservation = 99, 98, 97
	if again := monitor.Stop(nil); !reflect.DeepEqual(before, again) {
		t.Fatal("phase observations alias caller copies")
	}
	if err := finalRead(monitor); err == nil || !reflect.DeepEqual(before, monitor.Stop(nil)) {
		t.Fatal("post-stop request changed frozen evidence")
	}
}

func TestInitialHeatDoesNotStopSamplingAndFailureCannotEraseIt(t *testing.T) {
	clock := newMonitorClock()
	var calls, heat, failures atomic.Int32
	options := testOptions(clock.now(), func() { failures.Add(1) })
	options.OnFinding = func() { heat.Add(1) }
	monitor := phaseMonitor(t, clock, func(context.Context) int {
		switch calls.Add(1) {
		case 1:
			return 100
		case 2:
			return 20
		default:
			return -1
		}
	}, phaseReduce, options)
	if err := monitor.WaitInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-monitor.Finding():
	default:
		t.Fatal("initial heat not latched")
	}
	if monitor.Failed() || heat.Load() != 1 {
		t.Fatal("heat poisoned monitor integrity")
	}
	clock.advance(time.Second, true)
	awaitMonitor(t, "cool periodic read", func() bool { return phaseSnapshot(monitor).CompletedObservations == 2 })
	clock.advance(time.Second, true)
	awaitMonitor(t, "later invalid observation", monitor.Failed)
	result := monitor.Stop(nil)
	if result.FirstFailure.Reason != "invalid_observation" || result.FirstFinding == nil || result.FirstFinding.Phase != InitialPhase ||
		result.FirstFinding.Observation != 100 || result.Aggregate != (phaseAggregate{Count: 2, Peak: 100}) ||
		result.CompletedObservations != 3 || heat.Load() != 1 || failures.Load() != 1 {
		t.Fatalf("lost independent heat/failure: %+v", result)
	}
}

func TestFinalQueuesBehindOwnedReadWithoutInventingPeriodicTick(t *testing.T) {
	clock := newMonitorClock()
	var calls atomic.Int32
	release := make(chan struct{})
	monitor := phaseMonitor(t, clock, func(context.Context) int {
		if calls.Add(1) == 2 {
			<-release
		}
		return 20
	}, phaseReduce, testOptions(clock.now(), nil))
	if err := monitor.WaitInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Second, true)
	awaitMonitor(t, "owned periodic read", func() bool { return calls.Load() == 2 })
	final := make(chan error, 1)
	go func() { final <- finalRead(monitor) }()
	awaitMonitor(t, "admitted final request", func() bool { monitor.mutex.Lock(); defer monitor.mutex.Unlock(); return monitor.finalRequested })
	if phaseSnapshot(monitor).Final.Attempted || calls.Load() != 2 {
		t.Fatal("final reused or overlapped the active read")
	}
	close(release)
	if err := <-final; err != nil {
		t.Fatal(err)
	}
	result := monitor.Stop(nil)
	if result.AttemptedObservations != 3 || result.CompletedObservations != 3 || calls.Load() != 3 ||
		result.Final.StartedMilliseconds != 1000 || !result.CleanupConfirmed || result.FirstFailure != nil {
		t.Fatalf("final queue: %+v", result)
	}
}

func TestFinalRevocationAndDuplicateNeverAuthorizeQueuedRead(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancelled", true: "duplicate"}[duplicate], func(t *testing.T) {
			clock := newMonitorClock()
			var calls atomic.Int32
			release := make(chan struct{})
			monitor := phaseMonitor(t, clock, func(context.Context) int {
				if calls.Add(1) == 2 {
					<-release
					return 110
				}
				return 10
			}, phaseReduce, testOptions(clock.now(), nil))
			if err := monitor.WaitInitial(context.Background()); err != nil {
				t.Fatal(err)
			}
			clock.advance(time.Second, true)
			awaitMonitor(t, "blocked periodic read", func() bool { return calls.Load() == 2 })
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			final := make(chan error, 1)
			go func() { final <- monitor.ObserveFinal(ctx) }()
			awaitMonitor(t, "final admission", func() bool { monitor.mutex.Lock(); defer monitor.mutex.Unlock(); return monitor.finalRequested })
			want := "final_request_cancelled"
			if duplicate {
				want = "final_request_duplicate"
				if err := finalRead(monitor); err == nil {
					t.Fatal("duplicate accepted")
				}
			} else {
				cancel()
			}
			if err := <-final; err == nil {
				t.Fatal("revoked final returned success")
			}
			close(release)
			result := monitor.Stop(nil)
			if result.FirstFailure.Reason != want || result.Final.Attempted || result.Aggregate.Count != 1 ||
				result.FirstFinding == nil || result.FirstFinding.Observation != 110 || calls.Load() != 2 {
				t.Fatalf("revoked request: %+v", result)
			}
		})
	}
}

func TestInitialWaitCancellationDoesNotCancelOwner(t *testing.T) {
	clock := newMonitorClock()
	release := make(chan struct{})
	var calls atomic.Int32
	monitor := phaseMonitor(t, clock, func(context.Context) int {
		if calls.Add(1) == 1 {
			<-release
		}
		return 10
	}, phaseReduce, testOptions(clock.now(), nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(monitor.WaitInitial(ctx), context.Canceled) || monitor.Failed() {
		t.Fatal("caller cancellation changed owner")
	}
	close(release)
	if err := monitor.WaitInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := finalRead(monitor); err != nil {
		t.Fatal(err)
	}
	if !monitor.Stop(nil).CleanupConfirmed {
		t.Fatal("owner failed to join")
	}
}

func TestOwnerDiscardsIneligibleReducerValuesAndRetainsLateHeat(t *testing.T) {
	clock := newMonitorClock()
	var eligible atomic.Bool
	monitor := phaseMonitor(t, clock, func(context.Context) int { clock.advance(time.Second, false); return 120 },
		func(_ phaseAggregate, observation Observation[int]) Reduction[phaseAggregate] {
			eligible.Store(observation.EligibleForAggregation)
			return Reduction[phaseAggregate]{Value: phaseAggregate{Count: 999, Peak: 999}}
		}, testOptions(clock.now(), nil))
	if err := monitor.WaitInitial(context.Background()); err == nil {
		t.Fatal("late initial accepted")
	}
	result := monitor.Stop(nil)
	if eligible.Load() || result.Aggregate != (phaseAggregate{}) || result.FirstFailure.Reason != "observation_timeout" ||
		result.FirstFinding == nil || result.FirstFinding.Observation != 120 || result.CompletedObservations != 1 {
		t.Fatalf("late value accepted: %+v", result)
	}
}

func TestFrozenOwnerDiscardsLateFindingReductionAndCallbacks(t *testing.T) {
	clock := newMonitorClock()
	entered, release := make(chan struct{}), make(chan struct{})
	var heat, reductions atomic.Int32
	options := testOptions(clock.now(), nil)
	options.OnFinding = func() { heat.Add(1) }
	monitor := phaseMonitor(t, clock, func(context.Context) int { close(entered); <-release; return 120 },
		func(value phaseAggregate, observation Observation[int]) Reduction[phaseAggregate] {
			reductions.Add(1)
			return phaseReduce(value, observation)
		}, options)
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := monitor.Stop(ctx)
	close(release)
	select {
	case <-monitor.done:
	case <-time.After(time.Second):
		t.Fatal("released owner did not exit")
	}
	if heat.Load() != 0 || reductions.Load() != 0 || before.FirstFinding != nil || before.Aggregate.Count != 0 ||
		!reflect.DeepEqual(before, monitor.Stop(nil)) {
		t.Fatal("late callback or aggregate escaped frozen result")
	}
}

func TestReducerFailuresPreserveHeatAndRequiredPhaseFailure(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "returned failure", true: "panic"}[panics], func(t *testing.T) {
			clock := newMonitorClock()
			monitor := phaseMonitor(t, clock, func(context.Context) int { return 120 },
				func(phaseAggregate, Observation[int]) Reduction[phaseAggregate] {
					if panics {
						panic("untrusted reducer payload")
					}
					return Reduction[phaseAggregate]{Value: phaseAggregate{Count: 999}, FailureReason: "aggregate_overflow"}
				}, testOptions(clock.now(), nil))
			if err := monitor.WaitInitial(context.Background()); err == nil {
				t.Fatal("failed reduction accepted")
			}
			result := monitor.Stop(nil)
			want, count := "aggregate_overflow", uint64(1)
			if panics {
				want, count = "observer_panic", 0
			}
			if result.FirstFailure.Reason != want || result.FirstFinding == nil || result.Aggregate.Count != 0 ||
				result.CompletedObservations != count || !result.CleanupConfirmed {
				t.Fatalf("reducer outcome: %+v", result)
			}
		})
	}
	clock := newMonitorClock()
	monitor := phaseMonitor(t, clock, func(context.Context) int { return 0 }, phaseReduce, testOptions(clock.now(), nil))
	if err := monitor.WaitInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result := monitor.Stop(nil); result.FirstFailure == nil || result.FirstFailure.Reason != "required_phase_missing" {
		t.Fatalf("missing final: %+v", result)
	}
}

func TestFinalRequestRequiresLiveDeadlineAndRejectsDelayedProfile(t *testing.T) {
	for _, ctx := range []context.Context{nil, context.Background()} {
		clock := newMonitorClock()
		monitor := phaseMonitor(t, clock, func(context.Context) int { return 0 }, phaseReduce, testOptions(clock.now(), nil))
		if err := monitor.WaitInitial(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := monitor.ObserveFinal(ctx); err == nil {
			t.Fatal("invalid final context accepted")
		}
		if result := monitor.Stop(nil); result.FirstFailure.Reason != "final_request_invalid" {
			t.Fatalf("invalid final context: %+v", result)
		}
	}
	clock := newMonitorClock()
	monitor := startTestMonitor(t, clock, func(context.Context) testObservation { return monitorSafeResult() }, nil)
	if monitor.WaitInitial(context.Background()) == nil || monitor.ObserveFinal(context.Background()) == nil || monitor.Failed() {
		t.Fatal("phase request changed delayed profile")
	}
}

func TestFinalRequestDeadlineBoundsReadAndCannotBeRenewed(t *testing.T) {
	clock := newMonitorClock()
	release := make(chan struct{})
	entered := make(chan struct{})
	var calls atomic.Int32
	monitor := phaseMonitor(t, clock, func(context.Context) int {
		if calls.Add(1) == 2 {
			close(entered)
			<-release
		}
		return 10
	}, phaseReduce, testOptions(clock.now(), nil))
	if err := monitor.WaitInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	final := make(chan error, 1)
	go func() { final <- monitor.ObserveFinal(ctx) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("final was not dispatched")
	}
	select {
	case err := <-final:
		if err == nil {
			t.Fatal("expired final succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("final request ignored its deadline")
	}
	close(release)
	result := monitor.Stop(nil)
	if result.FirstFailure.Reason != "final_request_timeout" || result.Aggregate.Count != 1 || !result.CleanupConfirmed {
		t.Fatalf("final deadline: %+v", result)
	}
}

func TestPhasedFifteenMinuteRunRetainsOnlyBoundedAggregate(t *testing.T) {
	clock := newMonitorClock()
	monitor := phaseMonitor(t, clock, func(context.Context) int { return 1 }, phaseReduce, testOptions(clock.now(), nil))
	if err := monitor.WaitInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	for count := uint64(2); count <= 901; count++ {
		clock.advance(time.Second, true)
		awaitMonitor(t, "periodic aggregate", func() bool { return phaseSnapshot(monitor).CompletedObservations == count })
	}
	if err := finalRead(monitor); err != nil {
		t.Fatal(err)
	}
	result := monitor.Stop(nil)
	if result.Aggregate != (phaseAggregate{Count: 902, Peak: 1}) || result.CompletedObservations != 902 ||
		result.ElapsedMilliseconds != 900000 || result.MaximumGapMilliseconds != 1000 || result.FirstFailure != nil {
		t.Fatalf("bounded long run: %+v", result)
	}
}
