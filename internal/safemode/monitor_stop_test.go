package safemode

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestMonitorActualTimerWithOwnedMetadataFixture(t *testing.T) {
	root := fixture(t)
	returned := make(chan struct{}, 1)
	monitor, err := StartMonitor(func(ctx context.Context) Result {
		result := check(ctx, root)
		select {
		case returned <- struct{}{}:
		default:
		}
		return result
	}, MonitorOptions{Started: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopTestMonitor(t, monitor) })
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("metadata observation did not complete")
	}
	result := stopTestMonitor(t, monitor)
	if result.Schema != MonitorSchema || result.Status != "pass" || !result.CleanupConfirmed || result.FirstFailure != nil ||
		result.IntervalMilliseconds != 1000 || result.ObservationTimeoutMilliseconds != 1000 || result.MaximumAllowedGapMilliseconds != 2500 ||
		result.CompletedObservations == 0 || result.AttemptedObservations != result.CompletedObservations || result.FirstCompletedMilliseconds < 1000 ||
		result.ElapsedMilliseconds < result.LastCompletedMilliseconds || result.MaximumGapMilliseconds > 2500 ||
		result.LastObservation == nil || !strictMonitorSafety(*result.LastObservation) {
		t.Fatalf("real timer/metadata coverage: %+v", result)
	}
}

func TestMonitorPublicArgumentErrorsUnchanged(t *testing.T) {
	var calls atomic.Int32
	observe := func(context.Context) Result { calls.Add(1); return monitorSafeResult() }
	for _, test := range []struct {
		observe func(context.Context) Result
		start   time.Time
		want    string
	}{
		{nil, time.Now(), "periodic safety monitor requires an observer and start time"},
		{observe, time.Time{}, "periodic safety monitor requires an observer and start time"},
		{observe, time.Now().Add(time.Hour), "periodic safety monitor start is in the future"},
	} {
		monitor, err := StartMonitor(test.observe, MonitorOptions{Started: test.start})
		if monitor != nil || err == nil || err.Error() != test.want {
			t.Fatalf("argument contract changed: %v", err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid start invoked observer")
	}
}

func TestMonitorUnstartedQueriesRemainEmpty(t *testing.T) {
	var monitor Monitor
	if monitor.Failed() || monitor.Failure() != nil {
		t.Fatal("unstarted monitor queries changed")
	}
}
