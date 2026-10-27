package periodic

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAssessmentPanicJoinsOwnerAndPreservesDeadlinePrecedence(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "on time", true: "late"}[late], func(t *testing.T) {
			clock := newMonitorClock()
			var cancellations atomic.Int32
			monitor, err := startEmptyMonitor(func(context.Context) testObservation {
				if late {
					clock.advance(time.Second, false)
				}
				return monitorSafeResult()
			}, func(testObservation) Assessment[testObservation] {
				panic("untrusted assessment location")
			}, testOptions(clock.now(), func() { cancellations.Add(1) }), clock)
			if err != nil {
				t.Fatal(err)
			}
			awaitMonitor(t, "initial timer", clock.waiting)
			clock.advance(time.Second, true)
			awaitMonitorDone(t, monitor)
			result := monitor.Stop(context.Background())
			want := "observer_panic"
			if late {
				want = "observation_timeout"
			}
			raw, _ := json.Marshal(result)
			if result.FirstFailure == nil || result.FirstFailure.Reason != want || !result.CleanupConfirmed ||
				result.AttemptedObservations != 1 || result.CompletedObservations != 0 || result.LastObservation != nil ||
				cancellations.Load() != 1 || strings.Contains(string(raw), "untrusted") {
				t.Fatalf("assessment panic escaped owned cleanup: %+v", result)
			}
		})
	}
}

func TestAssessmentCopiesNormalizedValuesIntoIndependentSnapshots(t *testing.T) {
	type observation struct{ Note string }
	clock := newMonitorClock()
	monitor, err := startEmptyMonitor(func(context.Context) observation {
		return observation{Note: "untrusted location"}
	}, func(observation) Assessment[observation] {
		return Assessment[observation]{Value: observation{Note: "fixed diagnostic"}, FailureReason: "policy_failure"}
	}, testOptions(clock.now(), nil), clock)
	if err != nil {
		t.Fatal(err)
	}
	awaitMonitor(t, "initial timer", clock.waiting)
	clock.advance(time.Second, true)
	select {
	case <-monitor.done:
	case <-time.After(5 * time.Second):
		t.Fatal("failed assessment did not join observer")
	}
	result := monitor.Stop(context.Background())
	if result.FirstFailure == nil || result.FirstFailure.Observation == nil || result.LastObservation == nil ||
		result.FirstFailure.Observation.Note != "fixed diagnostic" || result.LastObservation.Note != "fixed diagnostic" {
		t.Fatalf("normalized evidence not retained: %+v", result)
	}
	result.FirstFailure.Observation.Note = "caller mutation"
	if result.LastObservation.Note != "fixed diagnostic" {
		t.Fatal("failure observation aliases last observation")
	}
	result.LastObservation.Note = "another mutation"
	again := monitor.Stop(nil)
	if again.LastObservation.Note != "fixed diagnostic" || again.FirstFailure.Observation.Note != "fixed diagnostic" {
		t.Fatal("caller changed the immutable result")
	}
}

func TestInvalidAssessmentAndTimingNeverStartObserver(t *testing.T) {
	clock := newMonitorClock()
	var calls atomic.Int32
	observe := func(context.Context) testObservation { calls.Add(1); return monitorSafeResult() }
	valid := testOptions(clock.now(), nil)
	if _, err := startEmptyMonitor(observe, nil, valid, clock); err == nil {
		t.Fatal("nil assessment accepted")
	}
	for _, change := range []func(*Options){
		func(value *Options) { value.Interval = 0 },
		func(value *Options) { value.ObservationTimeout = -1 },
		func(value *Options) { value.MaximumGap = 0 },
		func(value *Options) { value.StopTimeout = -1 },
	} {
		options := valid
		change(&options)
		if _, err := startEmptyMonitor(observe, testAssessment, options, clock); err == nil {
			t.Fatal("invalid timing accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid start invoked observer")
	}
}

func TestAssessmentTimeDoesNotMoveObserverCompletion(t *testing.T) {
	clock := newMonitorClock()
	monitor, err := startEmptyMonitor(func(context.Context) testObservation {
		return monitorSafeResult()
	}, func(value testObservation) Assessment[testObservation] {
		// The observer already completed at 1000 ms. Policy evaluation reaches
		// its deadline, but must not retroactively turn that read into timeout.
		clock.advance(time.Second, false)
		return Assessment[testObservation]{Value: value, FailureReason: "policy_failure"}
	}, testOptions(clock.now(), nil), clock)
	if err != nil {
		t.Fatal(err)
	}
	awaitMonitor(t, "initial timer", clock.waiting)
	clock.advance(time.Second, true)
	awaitMonitorDone(t, monitor)
	result := monitor.Stop(context.Background())
	if result.FirstFailure == nil || result.FirstFailure.Reason != "policy_failure" ||
		result.FirstFailure.ElapsedMilliseconds != 1000 || result.CompletedObservations != 1 ||
		result.FirstCompletedMilliseconds != 1000 || result.LastCompletedMilliseconds != 1000 ||
		result.ElapsedMilliseconds != 2000 || !result.CleanupConfirmed {
		t.Fatalf("assessment moved observation completion: %+v", result)
	}
}
