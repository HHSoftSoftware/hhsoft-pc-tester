package quick

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func coldThermalSnapshot() ThermalSnapshot {
	return ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 42000}
}

func awaitThermalDone(t *testing.T, monitor *safetyMonitor) {
	t.Helper()
	select {
	case <-monitor.done:
	case <-time.After(time.Second):
		t.Fatal("released observer did not join")
	}
}

func TestBlockedInitialThermalObservationCannotStartLoad(t *testing.T) {
	for _, worker := range []string{"cpu", "memory"} {
		t.Run(worker, func(t *testing.T) {
			release := make(chan struct{})
			var monitor *safetyMonitor
			var calls atomic.Int32
			factory := func(ctx context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
				monitor = startSafetyMonitor(ctx, root, timeout, limit, func(context.Context, string) ThermalSnapshot {
					calls.Add(1)
					<-release
					return coldThermalSnapshot()
				}, time.Millisecond, 20*time.Millisecond, 20*time.Millisecond)
				return monitor
			}
			started := time.Now()
			var status Status
			var safety SafetyEvidence
			var work uint64
			if worker == "cpu" {
				result := runCPUWith(context.Background(), CPUOptions{Duration: time.Second, GlobalTimeout: 2 * time.Second, Workers: 1, ThermalLimit: 90000}, factory)
				status, safety, work = result.Status, result.Safety, result.Iterations
			} else {
				result := runMemoryWith(context.Background(), MemoryOptions{Bytes: minimumMemoryBytes, Passes: 1, GlobalTimeout: 2 * time.Second, ThermalLimit: 90000}, factory)
				status, safety, work = result.Status, result.Safety, uint64(result.ExaminedBytes)
			}
			elapsed := time.Since(started)
			frozen := monitor.evidence()
			close(release)
			awaitThermalDone(t, monitor)
			monitor.stop()
			if elapsed > time.Second || work != 0 || status != StatusError || safety.CleanupConfirmed ||
				safety.MonitorStatus != "cleanup_unconfirmed" || safety.MonitorFailure != "cleanup_unconfirmed" || calls.Load() != 1 {
				t.Fatalf("blocked initial observation escaped its bounds: elapsed=%v work=%d status=%s safety=%+v calls=%d", elapsed, work, status, safety, calls.Load())
			}
			if !reflect.DeepEqual(frozen, monitor.evidence()) || !reflect.DeepEqual(frozen, safety) {
				t.Fatal("late completion rewrote already returned evidence")
			}
		})
	}
}

func TestThermalTimeoutSurvivesLateColdResult(t *testing.T) {
	release := make(chan struct{})
	monitor := startSafetyMonitor(context.Background(), "", time.Second, 90000, func(context.Context, string) ThermalSnapshot {
		<-release
		return coldThermalSnapshot()
	}, time.Millisecond, 20*time.Millisecond, time.Second)
	if !errors.Is(monitor.failure(), errThermalObservation) {
		t.Fatal("initial observation deadline was not latched")
	}
	close(release)
	monitor.stop()
	result := monitor.evidence()
	if !result.CleanupConfirmed || result.MonitorStatus != "failed" || result.MonitorFailure != "observation_timeout" || result.ThermalObservations != 1 {
		t.Fatalf("late cold result erased the timeout or evidence: %+v", result)
	}
}

func TestWorkerFinalizationRetainsInFlightOverheat(t *testing.T) {
	for _, worker := range []string{"cpu", "memory"} {
		t.Run(worker, func(t *testing.T) {
			second := make(chan struct{})
			release := make(chan struct{})
			calls := 0
			factory := func(ctx context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
				monitor := startSafetyMonitor(ctx, root, timeout, limit, func(ctx context.Context, _ string) ThermalSnapshot {
					calls++
					if calls == 1 {
						return coldThermalSnapshot()
					}
					close(second)
					<-release
					if ctx.Err() != nil {
						return ThermalSnapshot{}
					}
					return ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 97000}
				}, time.Millisecond, time.Second, time.Second)
				go func() { <-monitor.stopRequested; close(release) }()
				<-second
				return monitor
			}
			var status Status
			var safety SafetyEvidence
			var detail string
			if worker == "cpu" {
				result := runCPUWith(context.Background(), CPUOptions{Duration: 10 * time.Millisecond, GlobalTimeout: 2 * time.Second, Workers: 1, ThermalLimit: 90000}, factory)
				status, safety, detail = result.Status, result.Safety, result.Detail
			} else {
				result := runMemoryWith(context.Background(), MemoryOptions{Bytes: minimumMemoryBytes, Passes: 1, GlobalTimeout: 2 * time.Second, ThermalLimit: 90000}, factory)
				status, safety, detail = result.Status, result.Safety, result.Detail
			}
			if status != StatusAborted || detail != "thermal safety limit reached" || !safety.CleanupConfirmed ||
				safety.ThermalObservations != 2 || safety.PeakMilliCelsius != 97000 || safety.MonitorFailure != "thermal_limit" {
				t.Fatalf("worker omitted heat discovered during cleanup: %s %q %+v", status, detail, safety)
			}
		})
	}
}

func TestThermalStopPreservesKnownHardwareFailure(t *testing.T) {
	release := make(chan struct{})
	monitor := startSafetyMonitor(context.Background(), "", time.Second, 90000, func(context.Context, string) ThermalSnapshot {
		<-release
		return coldThermalSnapshot()
	}, time.Millisecond, 20*time.Millisecond, 20*time.Millisecond)
	status, detail := StatusFail, "memory pattern mismatch"
	var evidence SafetyEvidence
	finishSafety(monitor, &status, &detail, &evidence)
	close(release)
	awaitThermalDone(t, monitor)
	if status != StatusFail || detail != "memory pattern mismatch" || evidence.CleanupConfirmed || evidence.MonitorStatus != "cleanup_unconfirmed" {
		t.Fatalf("cleanup overwrote independent hardware failure: %s %q %+v", status, detail, evidence)
	}
}

func TestThermalNormalStopAndConcurrentStopAreStable(t *testing.T) {
	monitor := startSafetyMonitor(context.Background(), "", time.Second, 90000, func(context.Context, string) ThermalSnapshot {
		return coldThermalSnapshot()
	}, time.Hour, time.Second, time.Second)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() { defer group.Done(); monitor.stop() }()
	}
	group.Wait()
	result := monitor.evidence()
	if monitor.failure() != nil || !result.CleanupConfirmed || result.MonitorStatus != "completed" || result.ThermalObservations != 1 {
		t.Fatalf("normal stop fabricated cancellation or lost ownership: %+v %v", result, monitor.failure())
	}
}

func TestThermalObserverFailuresAreFixedAndBounded(t *testing.T) {
	for _, test := range []struct {
		name     string
		observer thermalObserver
		reason   string
	}{
		{"panic", func(context.Context, string) ThermalSnapshot { panic("untrusted sensor detail") }, "observer_panic"},
		{"invalid", func(context.Context, string) ThermalSnapshot { return ThermalSnapshot{Coverage: ThermalAvailable} }, "invalid_observation"},
	} {
		t.Run(test.name, func(t *testing.T) {
			monitor := startSafetyMonitor(context.Background(), "", time.Second, 90000, test.observer, time.Millisecond, time.Second, time.Second)
			monitor.stop()
			result := monitor.evidence()
			if !result.CleanupConfirmed || result.MonitorStatus != "failed" || result.MonitorFailure != test.reason || result.ThermalObservations != 0 {
				t.Fatalf("observer failure was misreported: %+v", result)
			}
		})
	}
}
