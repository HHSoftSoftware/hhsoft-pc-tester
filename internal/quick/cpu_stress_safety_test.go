package quick

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestCPUStressMonitorAdmissionUsesCoherentAdverseSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sample ThermalSnapshot
		status Status
		reason string
	}{
		{"hot", ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 95000}, StatusAborted, "thermal_limit"},
		{"coverage", ThermalSnapshot{Coverage: ThermalUnavailable}, StatusInconclusive, "thermal_coverage"},
		{"hot_partial", ThermalSnapshot{Coverage: ThermalPartial, Sensors: 1, PeakMilliCelsius: 95000, RejectedReadings: 1}, StatusAborted, "thermal_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newStressFixture(t)
			second, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			h.operations.monitor = func(ctx context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
				h.monitorCalls++
				h.monitor = startSafetyMonitorWithPolicy(ctx, root, timeout, limit, func(context.Context, string) ThermalSnapshot {
					if calls.Add(1) == 1 {
						return ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 42000}
					}
					close(second)
					<-release
					return tc.sample
				}, 0, time.Second, time.Second, true)
				<-second
				if h.monitor.failure() != nil || !h.monitor.admitCPUStress() {
					t.Fatal("first accepted cool observation missing")
				}
				close(release)
				<-h.monitor.context.Done()
				if h.monitor.admitCPUStress() {
					t.Fatal("admission ignored a newly completed adverse sample")
				}
				return h.monitor
			}
			h.operations.worker = func(int) cpuOperations { panic("unexpected adverse arithmetic admission") }
			result, err := runCPUStressWith(context.Background(), CPUStressOptions{Workers: 1, Root: "/fixture"}, h.operations)
			if err != nil || !result.AccountingValid || result.Status != tc.status || result.StopReason != tc.reason ||
				result.LoadStarted || result.WorkersLaunched != 0 || result.Safety.ThermalObservations != 2 || calls.Load() != 2 {
				t.Fatal("valid racing sample became accounting error or lost its finite outcome")
			}
			if tc.name != "coverage" && (!result.StopFacts.ThermalLimit || result.Safety.PeakMilliCelsius != 95000) {
				t.Fatal("accepted heat lost")
			}
			if tc.name != "hot" && !result.StopFacts.CoverageLost {
				t.Fatal("later coverage loss lost")
			}
			requireStressOwnersClosed(t, h, result)
		})
	}
}

func TestCPUStressInitialCoverageStopsOnlyOptInArithmetic(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sample ThermalSnapshot
		fact   string
	}{
		{"unavailable", ThermalSnapshot{Coverage: ThermalUnavailable}, "unavailable"},
		{"partial", ThermalSnapshot{Coverage: ThermalPartial, RejectedReadings: 1}, "partial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newStressFixture(t)
			h.operations.monitor = func(ctx context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
				h.monitorCalls++
				h.monitor = startSafetyMonitorWithPolicy(ctx, root, timeout, limit,
					func(context.Context, string) ThermalSnapshot { return tc.sample }, time.Hour, time.Second, time.Second, true)
				return h.monitor
			}
			h.operations.worker = func(int) cpuOperations { panic("unexpected incomplete-coverage arithmetic admission") }
			result, err := runCPUStressWith(context.Background(), CPUStressOptions{Workers: 1, Root: "/fixture"}, h.operations)
			if err != nil || result.Status != StatusInconclusive || result.StopReason != "thermal_coverage" ||
				result.Safety.MonitorFailure != "coverage_loss" || result.WorkersLaunched != 0 ||
				result.StopFacts.CoverageLost || result.StopFacts.InsufficientBudget ||
				result.StopFacts.InitialCoveragePartial != (tc.fact == "partial") || result.StopFacts.InitialCoverageUnavailable != (tc.fact == "unavailable") {
				t.Fatal("initial coverage classification changed")
			}
			requireStressOwnersClosed(t, h, result)
			// The original constructor's coverage behavior is retained for quick/MEM.
			legacy := startSafetyMonitor(context.Background(), "/fixture", time.Minute, 90000,
				func(context.Context, string) ThermalSnapshot { return tc.sample }, time.Hour, time.Second, time.Second)
			legacy.stop()
			if legacy.failure() != nil || legacy.stress != nil || legacy.evidence().MonitorStatus != "completed" ||
				legacy.evidence().ThermalCoverage != tc.sample.Coverage {
				t.Fatal("strict policy leaked into the retained monitor")
			}
		})
	}
}

func TestCPUStressMonitorRetainsLateHeatAndHardFaults(t *testing.T) {
	for _, mode := range []string{"hot", "hot_partial", "panic", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			h := newStressFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			h.operations.monitor = func(ctx context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
				h.monitorCalls++
				h.monitor = startSafetyMonitorWithPolicy(ctx, root, timeout, limit, func(context.Context, string) ThermalSnapshot {
					close(entered)
					<-release
					switch mode {
					case "panic":
						panic("late fixture observer failure")
					case "invalid":
						return ThermalSnapshot{Coverage: ThermalAvailable}
					case "hot_partial":
						return ThermalSnapshot{Coverage: ThermalPartial, Sensors: 1, PeakMilliCelsius: 95000, UnavailableReadings: 1}
					default:
						return ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 95000}
					}
				}, time.Hour, 2*time.Millisecond, time.Second, true)
				<-entered
				if h.monitor.failure() != errThermalObservation {
					t.Fatal("timeout was not reached while the real observer was owned")
				}
				close(release)
				return h.monitor
			}
			h.operations.worker = func(int) cpuOperations { panic("unexpected timed-out arithmetic admission") }
			result, err := runCPUStressWith(context.Background(), CPUStressOptions{Workers: 1, Root: "/fixture"}, h.operations)
			if err != nil || result.Status != StatusError || result.StopReason != "observer_failure" ||
				result.Safety.MonitorFailure != "observation_timeout" || !result.StopFacts.ObservationTimeout {
				t.Fatal("first timeout or joined observer classification lost")
			}
			if mode == "hot" || mode == "hot_partial" {
				if !result.StopFacts.ThermalLimit || result.Safety.PeakMilliCelsius != 95000 || result.Safety.ThermalSamples != 1 {
					t.Fatal("late accepted heat lost")
				}
			}
			if mode == "panic" && !result.StopFacts.ObserverPanic {
				t.Fatal("late panic lost behind first error")
			}
			if mode == "invalid" && !result.StopFacts.InvalidObservation {
				t.Fatal("late invalid sample lost behind first error")
			}
			requireStressOwnersClosed(t, h, result)
		})
	}
}

func TestCPUStressCleanupFailureFreezesOwnedReceipt(t *testing.T) {
	h := newStressFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	h.operations.monitor = func(ctx context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
		h.monitorCalls++
		h.monitor = startSafetyMonitorWithPolicy(ctx, root, timeout, limit, func(context.Context, string) ThermalSnapshot {
			close(entered)
			<-release
			return ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 95000}
		}, time.Hour, 2*time.Millisecond, 2*time.Millisecond, true)
		<-entered
		return h.monitor
	}
	result, err := runCPUStressWith(context.Background(), CPUStressOptions{Workers: 1, Root: "/fixture"}, h.operations)
	if !errors.Is(err, ErrCPUStressCleanupUnconfirmed) || result.Safety.CleanupConfirmed ||
		!result.StopFacts.CleanupUnconfirmed || result.Safety.MonitorStatus != "cleanup_unconfirmed" {
		close(release)
		t.Fatal("unjoined observer became accepted evidence")
	}
	before, facts := h.monitor.evidence(), h.monitor.stressFacts()
	close(release)
	select {
	case <-h.monitor.done:
	case <-time.After(2 * time.Second):
		t.Fatal("released observer failed to close")
	}
	h.monitor.stop()
	if !reflect.DeepEqual(h.monitor.evidence(), before) || h.monitor.stressFacts() != facts {
		t.Fatal("late hot completion rewrote frozen cleanup receipt")
	}
}

func TestCPUStressThermalSumsRejectOverflowBeforeAggregateCommit(t *testing.T) {
	for _, field := range []string{"observations", "samples", "rejected", "unavailable", "enumeration"} {
		t.Run(field, func(t *testing.T) {
			// Seed the named aggregate boundary, then reach the actual complete()
			// checked-commit path. This is a counter unit witness, not a sampled
			// history or a physical sensor-count claim.
			m := &safetyMonitor{context: context.Background(), cancel: func(error) {}, stress: &CPUStressStopFacts{},
				result: SafetyEvidence{GlobalTimeoutMilliseconds: 660000, ThermalLimitMilliCelsius: 90000,
					MonitorStatus: "running", ThermalCoverage: ThermalAvailable, ThermalSensors: 1,
					ThermalSamples: 1, ThermalObservations: 1, PeakMilliCelsius: 42000}}
			sample := ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 95000}
			switch field {
			case "observations":
				m.result.ThermalObservations = math.MaxInt32
				sample = ThermalSnapshot{Coverage: ThermalUnavailable}
			case "samples":
				m.result.ThermalSamples = math.MaxInt32
			case "rejected":
				m.result.ThermalRejectedReadings = math.MaxInt32
				sample.Coverage = ThermalPartial
				sample.RejectedReadings = 1
			case "unavailable":
				m.result.ThermalUnavailableReadings = math.MaxInt32
				sample.Coverage = ThermalPartial
				sample.UnavailableReadings = 1
			case "enumeration":
				m.result.ThermalEnumerationErrors = math.MaxInt32
				sample.Coverage = ThermalPartial
				sample.EnumerationErrors = 1
			}
			before := m.result
			m.complete(thermalCompletion{invoked: true, finished: time.Now(), snapshot: sample}, time.Now().Add(time.Second))
			if m.result != before || m.failure() != errStressCounter || !m.stressFacts().CounterOverflow {
				t.Fatal("overflow partially committed or wrapped")
			}
			if field != "observations" && !m.stressFacts().ThermalLimit {
				t.Fatal("valid overflow heat diagnostic lost")
			}
		})
	}
	t.Run("maximum_exact_and_source_bound", func(t *testing.T) {
		m := &safetyMonitor{stress: &CPUStressStopFacts{}, result: SafetyEvidence{ThermalLimitMilliCelsius: 90000}}
		sample := ThermalSnapshot{Coverage: ThermalAvailable, Sensors: math.MaxInt32, PeakMilliCelsius: 42000}
		m.mutex.Lock()
		ok := m.acceptStressSample(sample)
		m.mutex.Unlock()
		if !ok {
			t.Fatal("exact maximum rejected")
		}
		wide := int64(math.MaxInt32) + 1
		sample.Sensors = int(wide)
		if strconv.IntSize == 32 {
			if ValidThermalSnapshot(sample) {
				t.Fatal("wrapped source count accepted")
			}
		} else {
			m.mutex.Lock()
			ok = m.acceptStressSample(sample)
			m.mutex.Unlock()
			if ok {
				t.Fatal("out-of-bound source count accepted")
			}
		}
	})
}

func TestCPUStressRealMonitorOverflowIsFatalAndKeepsHeat(t *testing.T) {
	h := newStressFixture(t)
	second, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	h.operations.monitor = func(ctx context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
		h.monitorCalls++
		h.monitor = startSafetyMonitorWithPolicy(ctx, root, timeout, limit, func(context.Context, string) ThermalSnapshot {
			if calls.Add(1) == 1 {
				return ThermalSnapshot{Coverage: ThermalAvailable, Sensors: math.MaxInt32, PeakMilliCelsius: 42000}
			}
			close(second)
			<-release
			return ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 95000}
		}, 0, time.Second, time.Second, true)
		<-second
		close(release)
		<-h.monitor.context.Done()
		return h.monitor
	}
	result, err := runCPUStressWith(context.Background(), CPUStressOptions{Workers: 1, Root: "/fixture"}, h.operations)
	if !errors.Is(err, ErrCPUStressAccounting) || result.AccountingValid || !result.StopFacts.CounterOverflow ||
		!result.StopFacts.ThermalLimit || result.Safety.ThermalSamples != math.MaxInt32 ||
		result.Safety.ThermalObservations != 1 || result.Safety.PeakMilliCelsius != 42000 ||
		result.Safety.MonitorFailure != "counter_overflow" || result.WorkersLaunched != 0 {
		t.Fatal("real observer overflow became favorable or lost diagnostic heat")
	}
	requireStressOwnersClosed(t, h, result)
}
