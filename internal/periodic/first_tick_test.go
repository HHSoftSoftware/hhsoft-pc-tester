package periodic

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestMonitorStopAtFirstIntervalWithoutDeliveryLatchesCoverageFailure(t *testing.T) {
	for _, elapsed := range []time.Duration{time.Second, time.Second + time.Millisecond} {
		t.Run(elapsed.String(), func(t *testing.T) {
			clock := newMonitorClock()
			var observations, cancellations atomic.Int32
			monitor := startTestMonitor(t, clock, func(context.Context) testObservation {
				observations.Add(1)
				return monitorSafeResult()
			}, func() { cancellations.Add(1) })
			// Time advances monotonically, but the scheduler has not delivered
			// the first periodic timer when Stop takes ownership of cleanup.
			clock.advance(elapsed, false)
			result := monitor.Stop(context.Background())
			if !result.Joined || !result.CleanupConfirmed || !monitor.Failed() ||
				result.FirstFailure == nil || result.FirstFailure.Reason != "sampling_gap" ||
				result.AttemptedObservations != 0 || result.CompletedObservations != 0 ||
				result.LastObservation != nil || observations.Load() != 0 || cancellations.Load() != 1 ||
				result.ElapsedMilliseconds != elapsed.Milliseconds() || result.MaximumGapMilliseconds != elapsed.Milliseconds() {
				t.Fatal("confirmed cleanup must retain the missing first observation as a coverage failure")
			}
		})
	}
}
