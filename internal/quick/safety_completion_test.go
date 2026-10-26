package quick

import (
	"context"
	"errors"
	"testing"
	"time"
)

func completedTestMonitor() *safetyMonitor {
	ctx, cancel := context.WithCancelCause(context.Background())
	return &safetyMonitor{context: ctx, cancel: cancel, timerCancel: func() {},
		done: make(chan struct{}), stopRequested: make(chan struct{}),
		result: SafetyEvidence{GlobalTimeoutMilliseconds: 1000, ThermalLimitMilliCelsius: 90000,
			MonitorStatus: "running", ThermalCoverage: ThermalUnavailable}}
}

func TestThermalCompletionUsesMeasurementTime(t *testing.T) {
	monitor := completedTestMonitor()
	deadline := time.Now().Add(-time.Second)
	monitor.complete(thermalCompletion{snapshot: coldThermalSnapshot(), invoked: true,
		finished: deadline.Add(-time.Millisecond)}, deadline)
	if monitor.failure() != nil || monitor.evidence().ThermalObservations != 1 {
		t.Fatal("processing delay changed a timely observation into a timeout")
	}
	monitor.complete(thermalCompletion{snapshot: coldThermalSnapshot(), invoked: true, finished: deadline}, deadline)
	if !errors.Is(monitor.failure(), errThermalObservation) {
		t.Fatal("an observation completed at its deadline was accepted")
	}
}

func TestThermalTimelyJoinSurvivesReadyTimeout(t *testing.T) {
	for i := 0; i < 64; i++ {
		monitor := completedTestMonitor()
		monitor.complete(thermalCompletion{snapshot: coldThermalSnapshot(), invoked: true,
			finished: time.Now()}, time.Now().Add(time.Second))
		monitor.joinedAt = time.Now().Add(-time.Second)
		close(monitor.done)
		// Both the completed join and the zero-duration timer can be ready.
		monitor.stop()
		if result := monitor.evidence(); !result.CleanupConfirmed || result.MonitorStatus != "completed" || monitor.failure() != nil {
			t.Fatalf("select ordering erased a timely join: %+v", result)
		}
	}
}

func TestThermalCancelledInvocationDoesNotReadSensors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	completion := callThermalObserver(func(context.Context, string) ThermalSnapshot {
		t.Fatal("cancelled queued observation invoked the sensor reader")
		return ThermalSnapshot{}
	}, ctx, "")
	if completion.invoked || completion.panicked || completion.finished.IsZero() {
		t.Fatalf("cancelled invocation was misreported: %+v", completion)
	}
}

func TestThermalCoverageRetainsAnUnavailableInterval(t *testing.T) {
	monitor := completedTestMonitor()
	for _, sample := range []ThermalSnapshot{{Coverage: ThermalUnavailable}, coldThermalSnapshot(), coldThermalSnapshot()} {
		monitor.complete(thermalCompletion{snapshot: sample, invoked: true, finished: time.Now()}, time.Now().Add(time.Second))
	}
	result := monitor.evidence()
	if result.ThermalCoverage != ThermalPartial || result.ThermalObservations != 3 || result.ThermalSamples != 2 || result.ThermalSensors != 1 {
		t.Fatalf("later readings erased missing coverage: %+v", result)
	}
}

func TestUpperRangeThermalReadingsStillStopLoad(t *testing.T) {
	for _, value := range []int{185000, 250000} {
		monitor := startSafetyMonitor(context.Background(), "", time.Second, 90000,
			func(context.Context, string) ThermalSnapshot {
				return ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: value}
			}, time.Second, time.Second, time.Second)
		monitor.stop()
		result := monitor.evidence()
		if !errors.Is(monitor.failure(), ErrThermalLimit) || result.PeakMilliCelsius != value || result.MonitorFailure != "thermal_limit" || !result.CleanupConfirmed {
			t.Fatalf("valid high temperature was discarded: %+v", result)
		}
	}
}
