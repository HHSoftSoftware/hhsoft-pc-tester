package quick

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type stressFixtureClock struct {
	base     time.Time
	nanos    atomic.Int64
	readHook func()
}

func (clock *stressFixtureClock) now() time.Time {
	if clock.readHook != nil {
		clock.readHook()
	}
	return clock.base.Add(time.Duration(clock.nanos.Load()))
}

type stressFixture struct {
	clock        *stressFixtureClock
	operations   cpuStressOperations
	monitor      *safetyMonitor
	monitorCalls int
	observations atomic.Int32
}

func newStressFixture(t *testing.T) *stressFixture {
	t.Helper()
	h := &stressFixture{clock: &stressFixtureClock{base: time.Now()}}
	h.operations = cpuStressOperations{
		now:    h.clock.now,
		numCPU: func() int { return 3 },
		monitor: func(ctx context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
			h.monitorCalls++
			if root != "/fixture" || timeout != 660*time.Second || limit != 90000 {
				t.Fatal("changed observation arguments")
			}
			h.monitor = startSafetyMonitorWithPolicy(ctx, root, timeout, limit,
				func(context.Context, string) ThermalSnapshot {
					h.observations.Add(1)
					return ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 42000}
				}, time.Hour, time.Second, time.Second, true)
			return h.monitor
		},
		worker: func(int) cpuOperations { return productionCPUOperations() },
	}
	return h
}

func requireStressOwnersClosed(t *testing.T, h *stressFixture, result CPUStressResult) {
	t.Helper()
	if !result.Safety.CleanupConfirmed {
		t.Fatal("cleanup not confirmed")
	}
	if h.monitor != nil {
		select {
		case <-h.monitor.done:
		default:
			t.Fatal("monitor owner not joined")
		}
		if !h.monitor.frozen {
			t.Fatal("monitor receipt not frozen")
		}
	}
	if result.WorkersJoinedMilliseconds > result.FinishedMilliseconds {
		t.Fatal("join after finish")
	}
	for _, row := range result.Workers {
		if row.StoppedMilliseconds > result.WorkersJoinedMilliseconds {
			t.Fatal("worker still owned")
		}
	}
}

type nilStressContext struct{ context.Context }

func TestCPUStressAdmissionPreservesNoOwnerBoundaries(t *testing.T) {
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	actual, actualErr := RunCPUStress(stopped, CPUStressOptions{Workers: 1})
	if actualErr != nil || actual.StopReason != "parent_cancelled" || actual.WorkersLaunched != 0 {
		t.Fatal("real wrapper bypassed stopped-context admission")
	}

	var typedNil *nilStressContext
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		workers int
	}{
		{"nil_context", nil, 1}, {"typed_nil_context", typedNil, 1},
		{"negative_workers", context.Background(), -1}, {"over_cap", context.Background(), 65},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newStressFixture(t)
			result, err := runCPUStressWith(tc.ctx, CPUStressOptions{Workers: tc.workers, Root: "/fixture"}, h.operations)
			if !errors.Is(err, ErrCPUStressOptions) || h.monitorCalls != 0 || !reflect.DeepEqual(result, CPUStressResult{}) {
				t.Fatal("invalid admission reached an owner")
			}
		})
	}
	for _, tc := range []struct {
		name                      string
		supplied, available, want int
		selection                 string
	}{
		{"automatic", 0, 3, 3, "automatic"}, {"automatic_cap", 0, 128, 64, "automatic"}, {"explicit", 2, 3, 2, "explicit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newStressFixture(t)
			h.operations.numCPU = func() int { return tc.available }
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			result, err := runCPUStressWith(ctx, CPUStressOptions{Workers: tc.supplied, Root: "/fixture"}, h.operations)
			if err != nil || result.Status != StatusAborted || result.StopReason != "parent_cancelled" ||
				result.WorkersRequested != tc.want || result.WorkerSelection != tc.selection || h.monitorCalls != 0 ||
				result.LoadStarted || len(result.Workers) != 0 || !result.StopFacts.ParentCancelled || result.StopFacts.InsufficientBudget {
				t.Fatal("clean stop or worker resolution changed")
			}
			requireStressOwnersClosed(t, h, result)
		})
	}
}

func TestCPUStressBudgetUsesOriginAndConservativeMilliseconds(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		parent, afterMonitor time.Duration
		want                 string
		monitorCalls         int
	}{
		{"exact_target", 600 * time.Second, 0, "insufficient_budget", 0},
		{"short_target", 600*time.Second - time.Nanosecond, 0, "insufficient_budget", 0},
		{"expired_parent", -time.Second, 0, "parent_deadline", 0},
		{"fractional_headroom", 659999900 * time.Microsecond, 59999500 * time.Microsecond, "insufficient_budget", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newStressFixture(t)
			ctx, cancel := context.WithDeadline(context.Background(), h.clock.base.Add(tc.parent))
			defer cancel()
			start := h.operations.monitor
			h.operations.monitor = func(ctx context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
				got, ok := ctx.Deadline()
				want := h.clock.base.Add(min(tc.parent, 660*time.Second))
				if !ok || !got.Equal(want) {
					t.Fatal("watchdog restarted after preparation")
				}
				m := start(ctx, root, timeout, limit)
				h.clock.nanos.Store(int64(tc.afterMonitor))
				return m
			}
			h.operations.worker = func(int) cpuOperations { panic("unexpected arithmetic admission") }
			result, err := runCPUStressWith(ctx, CPUStressOptions{Workers: 1, Root: "/fixture"}, h.operations)
			if err != nil || result.StopReason != tc.want || h.monitorCalls != tc.monitorCalls || result.LoadStarted ||
				result.LocalWatchdogMilliseconds != 660000 || result.Safety.GlobalTimeoutMilliseconds != 660000 {
				t.Fatal("budget/absolute deadline evidence changed")
			}
			if tc.want == "parent_deadline" && result.StopFacts.InsufficientBudget {
				t.Fatal("expiry fabricated budget failure")
			}
			requireStressOwnersClosed(t, h, result)
		})
	}
	t.Run("ready_budget_exhausted", func(t *testing.T) {
		h := newStressFixture(t)
		h.operations.worker = func(int) cpuOperations {
			op := productionCPUOperations()
			actual := op.integer
			calls := 0
			op.integer = func(value uint64) uint64 {
				calls++
				got := actual(value)
				if calls == 2 {
					h.clock.nanos.Store(int64(60 * time.Second))
				}
				return got
			}
			return op
		}
		result, err := runCPUStressWith(context.Background(), CPUStressOptions{Workers: 1, Root: "/fixture"}, h.operations)
		if err != nil || result.StopReason != "insufficient_budget" || result.LoadStarted || result.WorkersReady != 1 ||
			len(result.Workers) != 1 || result.Workers[0].PreparationIterations != 1 || result.Workers[0].PreparationChecksPassed != 12 ||
			result.Workers[0].LoadIterations != 0 || result.Workers[0].LoadChecksPassed != 0 {
			t.Fatal("readiness counts became load or restarted watchdog")
		}
		requireStressOwnersClosed(t, h, result)
	})
}

func TestCPUStressRealWorkHasSeparateReadyAndLoadSweeps(t *testing.T) {
	for _, workers := range []int{1, 2} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			h := newStressFixture(t)
			var reached atomic.Int32
			release := make(chan struct{})
			calls := make([][3]int, workers)
			h.operations.worker = func(index int) cpuOperations {
				op := productionCPUOperations()
				integer, sha, scalar := op.integer, op.sha, op.scalar
				op.integer = func(value uint64) uint64 {
					calls[index][0]++
					got := integer(value)
					if calls[index][0] == 259 {
						if reached.Add(1) == int32(workers) {
							h.clock.nanos.Store(int64(600 * time.Second))
							close(release)
						}
						<-release
					}
					return got
				}
				op.sha = func(raw []byte) [32]byte { calls[index][1]++; return sha(raw) }
				op.scalar = func(worker, slot int, kind byte, left, right float64) float64 {
					calls[index][2]++
					return scalar(worker, slot, kind, left, right)
				}
				return op
			}
			result, err := runCPUStressWith(context.Background(), CPUStressOptions{Workers: workers, Root: "/fixture"}, h.operations)
			if err != nil || result.Status != StatusPass || result.StopReason != "completed" || !result.LoadStarted ||
				result.ObservedLoadMilliseconds != 600000 || result.WorkersReady != workers ||
				result.Iterations != uint64(workers)*256 || result.ChecksPassed != uint64(workers)*24 ||
				!result.StopFacts.NormalLoadComplete || result.StopFacts.ParentDeadline || result.StopFacts.WatchdogDeadline ||
				result.StopFacts.ParentCancelled || result.Safety.MonitorStatus != "completed" {
				t.Fatal("real periodic work did not establish the separate elapsed load")
			}
			wantState := uint64(0x1d8a34cd5bd78cea)
			if workers == 2 {
				wantState = 0x8491abfdd31672ef
			}
			if result.StateAggregate != wantState {
				t.Fatal("continuing lifetime state differs from fixed vector")
			}
			for index, row := range result.Workers {
				if calls[index] != [3]int{259, 2, 24} || row.Index != index || row.PreparationIterations != 1 ||
					row.PreparationChecksPassed != 12 || row.Iterations != 256 || row.ChecksPassed != 24 ||
					row.LoadIterations != 255 || row.LoadChecksPassed != 12 || row.ReadyMilliseconds == nil ||
					row.LoadEnteredMilliseconds == nil || row.StoppedMilliseconds != 600000 || row.ExitReason != "completed" {
					t.Fatal("late recurrence gained credit or readiness was reset")
				}
			}
			requireStressOwnersClosed(t, h, result)
		})
	}
}

func TestCPUStressProducerKeepsLateFaultsAndPartialPreparation(t *testing.T) {
	for _, phase := range []string{"readiness", "load"} {
		for _, stage := range []string{"integer", "sha", "scalar"} {
			for _, stop := range []string{"cancel", "deadline"} {
				for _, mismatch := range []bool{false, true} {
					t.Run(phase+"/"+stage+"/"+stop+"/"+fmt.Sprint(mismatch), func(t *testing.T) {
						h := newStressFixture(t)
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()
						reached := false
						h.operations.worker = func(int) cpuOperations {
							op := productionCPUOperations()
							integer, sha, scalar := op.integer, op.sha, op.scalar
							integerCalls, shaCalls, scalarCalls := 0, 0, 0
							fault := func() {
								reached = true
								if stop == "cancel" {
									cancel()
								} else {
									h.clock.nanos.Store(int64(660 * time.Second))
								}
							}
							integerTarget, suiteTarget := 1, 1
							if phase == "load" {
								integerTarget, suiteTarget = 258, 2
							}
							op.integer = func(value uint64) uint64 {
								integerCalls++
								got := integer(value)
								if stage == "integer" && integerCalls == integerTarget {
									fault()
									if mismatch {
										got ^= 1
									}
								}
								return got
							}
							op.sha = func(raw []byte) [32]byte {
								shaCalls++
								got := sha(raw)
								if stage == "sha" && shaCalls == suiteTarget {
									fault()
									if mismatch {
										got[0] ^= 1
									}
								}
								return got
							}
							op.scalar = func(worker, slot int, kind byte, left, right float64) float64 {
								scalarCalls++
								got := scalar(worker, slot, kind, left, right)
								if stage == "scalar" && scalarCalls == (suiteTarget-1)*12+6 {
									fault()
									if mismatch {
										got = math.Float64frombits(math.Float64bits(got) ^ 1)
									}
								}
								return got
							}
							return op
						}
						result, err := runCPUStressWith(ctx, CPUStressOptions{Workers: 1, Root: "/fixture"}, h.operations)
						if err != nil || !reached || len(result.Workers) != 1 || result.StopFacts.InsufficientBudget {
							t.Fatal("fault prerequisite or clean stop changed")
						}
						want := StatusAborted
						if mismatch {
							want = StatusFail
						}
						if result.Status != want {
							t.Fatal("late mismatch was lost or late success promoted")
						}
						row := result.Workers[0]
						if phase == "readiness" {
							if result.LoadStarted || row.ReadyMilliseconds != nil || row.LoadIterations != 0 || row.LoadChecksPassed != 0 ||
								row.PreparationIterations != row.Iterations || row.PreparationChecksPassed != row.ChecksPassed {
								t.Fatal("partial readiness mislabeled load")
							}
						} else if !result.LoadStarted || row.PreparationIterations != 1 || row.PreparationChecksPassed != 12 || row.Iterations != 256 {
							t.Fatal("load fault did not reach the continuing lifetime state")
						}
						if mismatch && (row.FailurePhase != phase || row.ExitReason != "calculation_mismatch") {
							t.Fatal("wrong failure phase")
						}
						if stage == "scalar" && row.ChecksPassed != map[string]uint64{"readiness": 5, "load": 17}[phase] {
							t.Fatal("late scalar gained a check")
						}
						requireStressOwnersClosed(t, h, result)
					})
				}
			}
		}
	}
}

func TestCPUStressReadyOwnerWakesOnStopWithoutRelease(t *testing.T) {
	h := newStressFixture(t)
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	monitor := h.operations.monitor(parent, "/fixture", 660*time.Second, 90000)
	defer monitor.stop()
	clock := &cpuStressClock{read: h.clock.now}
	clock.origin = clock.now()
	ctx, cancel := context.WithCancel(monitor.context)
	notices := make(chan cpuStressNotice, 2)
	release := make(chan struct{})
	done := make(chan cpuStressJoined, 1)
	var failed atomic.Bool
	go func() {
		done <- runCPUStressWorker(ctx, h.clock.base.Add(660*time.Second), parent, time.Time{}, false,
			h.clock.base.Add(660*time.Second), 5, &failed, monitor, clock,
			func(int) cpuOperations { return productionCPUOperations() }, release, &cpuStressRelease{}, notices, cancel)
	}()
	notice := <-notices
	if !notice.ready || notice.index != 5 {
		t.Fatal("nonzero owner failed real readiness")
	}
	cancel()
	select {
	case row := <-done:
		if row.row.Iterations != 1 || row.row.ChecksPassed != 12 || row.row.LoadIterations != 0 ||
			row.row.LoadChecksPassed != 0 || row.entered.IsZero() == false || row.ready.IsZero() ||
			row.row.ExitReason != "peer_stop" {
			t.Fatal("parked owner gained work or lost readiness")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parked owner did not join on real stop")
	}
}

func TestCPUStressNonzeroWorkerFailureStopsCoordinator(t *testing.T) {
	for _, mode := range []string{"mismatch", "panic"} {
		t.Run(mode, func(t *testing.T) {
			h := newStressFixture(t)
			var started atomic.Int32
			both := make(chan struct{})
			h.operations.worker = func(index int) cpuOperations {
				if started.Add(1) == 2 {
					close(both)
				}
				<-both
				op := productionCPUOperations()
				if index == 1 {
					op.scalar = func(int, int, byte, float64, float64) float64 {
						if mode == "panic" {
							panic("fixture worker fault")
						}
						return 0
					}
				}
				return op
			}
			result, err := runCPUStressWith(context.Background(), CPUStressOptions{Workers: 2, Root: "/fixture"}, h.operations)
			if len(result.Workers) != 2 || result.LoadStarted || !result.StopFacts.ReadinessIncomplete {
				t.Fatal("terminal nonzero worker allowed release")
			}
			if mode == "panic" {
				if !errors.Is(err, ErrCPUStressAccounting) || result.Status != StatusError || !result.StopFacts.WorkerPanic {
					t.Fatal("worker panic accepted")
				}
			} else if err != nil || result.Status != StatusFail || !result.Workers[1].CalculationFailures.Scalar {
				t.Fatal("nonzero mismatch lost")
			}
			requireStressOwnersClosed(t, h, result)
		})
	}
}

func TestCPUStressReleaseStopDoesNotInventBudgetFailure(t *testing.T) {
	for _, mode := range []string{"cancel", "hot", "coverage"} {
		t.Run(mode, func(t *testing.T) {
			h := newStressFixture(t)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			var initialDone atomic.Bool
			var reads atomic.Int32
			reached := false
			h.operations.worker = func(int) cpuOperations {
				op := productionCPUOperations()
				actual := op.integer
				calls := 0
				op.integer = func(value uint64) uint64 {
					calls++
					got := actual(value)
					if calls == 2 {
						initialDone.Store(true)
					}
					return got
				}
				return op
			}
			// The first three reads finish the initial step and readiness decision;
			// the next three reach the coordinator's single release decision.
			h.clock.readHook = func() {
				if initialDone.Load() && reads.Add(1) == 6 {
					reached = true
					switch mode {
					case "cancel":
						cancel()
					case "hot":
						h.monitor.complete(thermalCompletion{invoked: true, finished: time.Now(),
							snapshot: ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 95000}}, time.Now().Add(time.Second))
					case "coverage":
						h.monitor.complete(thermalCompletion{invoked: true, finished: time.Now(),
							snapshot: ThermalSnapshot{Coverage: ThermalUnavailable}}, time.Now().Add(time.Second))
					}
				}
			}
			result, err := runCPUStressWith(parent, CPUStressOptions{Workers: 1, Root: "/fixture"}, h.operations)
			if !reached || err != nil || result.LoadStarted || result.StopFacts.InsufficientBudget ||
				result.WorkersReady != 1 || result.Workers[0].Iterations != 1 || result.Workers[0].LoadIterations != 0 {
				t.Fatal("final live stop was mislabeled budget or failed to reach ready release boundary")
			}
			want := map[string]string{"cancel": "parent_cancelled", "hot": "thermal_limit", "coverage": "thermal_coverage"}[mode]
			if result.StopReason != want {
				t.Fatal("actual release stop lost")
			}
			requireStressOwnersClosed(t, h, result)
		})
	}
}

func TestCPUStressAccountingUsesCheckedSumsAndModuloState(t *testing.T) {
	h := newStressFixture(t)
	clock := &cpuStressClock{read: h.clock.now}
	clock.origin = clock.now()
	for _, field := range []string{"iterations", "checks"} {
		t.Run(field, func(t *testing.T) {
			result := CPUStressResult{AccountingValid: true, Workers: []CPUStressWorker{}}
			one := cpuStressJoined{started: clock.origin, stopped: clock.origin, state: math.MaxUint64}
			two := cpuStressJoined{started: clock.origin, stopped: clock.origin, state: 2}
			if field == "iterations" {
				one.row.Iterations, two.row.Iterations = math.MaxUint64, 1
			} else {
				one.row.ChecksPassed, two.row.ChecksPassed = math.MaxUint64, 1
			}
			collectCPUStressWorker(&result, one, clock)
			collectCPUStressWorker(&result, two, clock)
			if !result.StopFacts.CounterOverflow || result.StateAggregate != 1 {
				t.Fatal("checked count confused with modulo state")
			}
		})
	}
}

func TestCPUStressPrimaryFactsAndConservativeTimeAreIndependent(t *testing.T) {
	passed := CPUStressResult{AccountingValid: true, LoadStarted: true, EffectiveDeadlineMilliseconds: 660000,
		FinishedMilliseconds: 600001, ObservedLoadMilliseconds: 600000,
		StopFacts: CPUStressStopFacts{NormalLoadComplete: true}}
	start, entry := int64(0), int64(0)
	passed.LoadStartedMilliseconds = &start
	passed.Workers = []CPUStressWorker{{LoadEnteredMilliseconds: &entry, StoppedMilliseconds: 600000}}
	for _, tc := range []struct {
		name   string
		mutate func(*CPUStressResult)
		status Status
		reason string
	}{
		{"complete", func(*CPUStressResult) {}, StatusPass, "completed"},
		{"wire_finish_tie", func(r *CPUStressResult) { r.FinishedMilliseconds = 660000 }, StatusInconclusive, "incomplete_work"},
		{"later_heat", func(r *CPUStressResult) { r.StopFacts.ThermalLimit = true }, StatusAborted, "thermal_limit"},
		{"timeout_plus_heat", func(r *CPUStressResult) { r.StopFacts.ObservationTimeout = true; r.StopFacts.ThermalLimit = true }, StatusError, "observer_failure"},
		{"coverage_plus_heat", func(r *CPUStressResult) { r.StopFacts.CoverageLost = true; r.StopFacts.ThermalLimit = true }, StatusAborted, "thermal_limit"},
		{"mismatch_plus_heat", func(r *CPUStressResult) { r.CalculationFailures.Scalar = true; r.StopFacts.ThermalLimit = true }, StatusFail, "calculation_mismatch"},
		{"cleanup_plus_mismatch", func(r *CPUStressResult) { r.CalculationFailures.Integer = true; r.StopFacts.CleanupUnconfirmed = true }, StatusFail, "calculation_mismatch"},
		{"parent_local_tie", func(r *CPUStressResult) {
			r.StopFacts.ParentDeadline = true
			r.StopFacts.WatchdogDeadline = true
			r.EffectiveDeadlineSource = "both"
		}, StatusAborted, "parent_deadline"},
		{"local_first", func(r *CPUStressResult) {
			r.StopFacts.ParentDeadline = true
			r.StopFacts.WatchdogDeadline = true
			r.EffectiveDeadlineSource = "watchdog"
		}, StatusAborted, "watchdog"},
		{"normal_without_entry", func(r *CPUStressResult) {
			r.Workers = []CPUStressWorker{{StoppedMilliseconds: 600000}}
			r.StopFacts.EarlyWorkerExit = true
		}, StatusInconclusive, "incomplete_work"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := passed
			tc.mutate(&result)
			status, reason := classifyCPUStress(result)
			if status != tc.status || reason != tc.reason || !result.StopFacts.NormalLoadComplete {
				t.Fatal("primary reason erased independent facts")
			}
		})
	}
}

func TestCPUStressClockFaultsCannotBecomeSignedEvidence(t *testing.T) {
	for _, mode := range []string{"regression", "out_of_range"} {
		t.Run(mode, func(t *testing.T) {
			h := newStressFixture(t)
			h.operations.worker = func(int) cpuOperations {
				op := productionCPUOperations()
				actual := op.integer
				op.integer = func(value uint64) uint64 {
					got := actual(value)
					if mode == "regression" {
						h.clock.nanos.Store(-int64(time.Millisecond))
					} else {
						h.clock.nanos.Store(int64(time.Hour + time.Millisecond))
					}
					return got
				}
				return op
			}
			result, err := runCPUStressWith(context.Background(), CPUStressOptions{Workers: 1, Root: "/fixture"}, h.operations)
			if !errors.Is(err, ErrCPUStressAccounting) || result.AccountingValid || !result.StopFacts.ClockInvalid || result.LoadStarted {
				t.Fatal("bad clock gained accepted evidence")
			}
			requireStressOwnersClosed(t, h, result)
		})
	}
}

func TestCPUStressReleasedOwnerCanStopBeforeEntry(t *testing.T) {
	h := newStressFixture(t)
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	monitor := h.operations.monitor(parent, "/fixture", 660*time.Second, 90000)
	defer monitor.stop()
	clock := &cpuStressClock{read: h.clock.now}
	clock.origin = clock.now()
	ctx, cancel := context.WithCancel(monitor.context)
	defer cancel()
	notices := make(chan cpuStressNotice, 2)
	release := make(chan struct{})
	done := make(chan cpuStressJoined, 1)
	shared := &cpuStressRelease{}
	var failed atomic.Bool
	go func() {
		done <- runCPUStressWorker(ctx, h.clock.base.Add(660*time.Second), parent, time.Time{}, false,
			h.clock.base.Add(660*time.Second), 3, &failed, monitor, clock,
			func(int) cpuOperations { return productionCPUOperations() }, release, shared, notices, cancel)
	}()
	ready := <-notices
	if !ready.ready || ready.index != 3 {
		t.Fatal("nonzero readiness prerequisite missing")
	}
	reached := false
	h.clock.readHook = func() {
		if !reached {
			reached = true
			cancel()
		}
	}
	shared.started, shared.deadline = h.clock.base, h.clock.base.Add(600*time.Second)
	close(release)
	select {
	case worker := <-done:
		if !reached || worker.ready.IsZero() || !worker.entered.IsZero() || worker.row.LoadIterations != 0 ||
			worker.row.LoadChecksPassed != 0 || worker.row.PreparationIterations != 1 || worker.row.PreparationChecksPassed != 12 ||
			worker.row.ExitReason != "peer_stop" {
			t.Fatal("release/cancel fabricated load participation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("released cancelled owner did not join")
	}
}

func TestCPUStressFinalDecisionDoesNotBackdateTheLoadWindow(t *testing.T) {
	h := newStressFixture(t)
	var initialDone atomic.Bool
	var reads atomic.Int32
	reached := false
	h.operations.worker = func(int) cpuOperations {
		op := productionCPUOperations()
		actual := op.integer
		calls := 0
		op.integer = func(value uint64) uint64 {
			calls++
			got := actual(value)
			if calls == 2 {
				initialDone.Store(true)
			}
			return got
		}
		return op
	}
	h.clock.readHook = func() {
		if initialDone.Load() && reads.Add(1) == 6 {
			reached = true
			h.clock.nanos.Store(int64(60 * time.Second))
		}
	}
	result, err := runCPUStressWith(context.Background(), CPUStressOptions{Workers: 1, Root: "/fixture"}, h.operations)
	if !reached || err != nil || result.LoadStarted || result.StopReason != "insufficient_budget" ||
		!result.StopFacts.InsufficientBudget || result.WorkersReady != 1 || result.Iterations != 1 ||
		result.ChecksPassed != 12 || result.Workers[0].LoadIterations != 0 {
		t.Fatal("final admission used an earlier timestamp for the shared window")
	}
	requireStressOwnersClosed(t, h, result)
}
