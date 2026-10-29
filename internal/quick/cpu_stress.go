package quick

import (
	"context"
	"encoding/hex"
	"errors"
	"math"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const (
	cpuStressLoad          = 600 * time.Second
	cpuStressWatchdog      = 660 * time.Second
	cpuStressMaximumOffset = time.Hour
)

type cpuStressOperations struct {
	now     func() time.Time
	numCPU  func() int
	monitor safetyMonitorFactory
	worker  func(int) cpuOperations
}

func RunCPUStress(ctx context.Context, options CPUStressOptions) (CPUStressResult, error) {
	return runCPUStressWith(ctx, options, cpuStressOperations{
		time.Now, runtime.NumCPU, newCPUStressSafetyMonitor,
		func(int) cpuOperations { return productionCPUOperations() },
	})
}

func newCPUStressSafetyMonitor(parent context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
	return startSafetyMonitorWithPolicy(parent, root, timeout, limit, ObserveThermals,
		thermalInterval, thermalObservationTimeout, thermalStopTimeout, true)
}

func nilCPUStressContext(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	value := reflect.ValueOf(ctx)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	}
	return false
}

// The shared clock records regressions before any time is credited. Its lock
// also gives injected clocks the same ordered observation contract as time.Now.
type cpuStressClock struct {
	read         func() time.Time
	mutex        sync.Mutex
	origin, last time.Time
	invalid      atomic.Bool
}

func (clock *cpuStressClock) now() (value time.Time) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	defer func() {
		if recover() != nil {
			clock.invalid.Store(true)
			value = clock.last
		}
	}()
	value = clock.read()
	if value.IsZero() || !clock.last.IsZero() && value.Before(clock.last) {
		clock.invalid.Store(true)
	}
	clock.last = value
	if !clock.origin.IsZero() && value.Sub(clock.origin) > cpuStressMaximumOffset {
		clock.invalid.Store(true)
	}
	return value
}

func (clock *cpuStressClock) offset(value time.Time) int64 {
	delta := value.Sub(clock.origin)
	if delta < 0 || delta > cpuStressMaximumOffset {
		clock.invalid.Store(true)
	}
	// Invalid diagnostics retain the measured value; they cannot be emitted or signed.
	return delta.Milliseconds()
}

type cpuStressRelease struct{ started, deadline time.Time }
type cpuStressNotice struct {
	ready              bool
	index              int
	at                 time.Time
	iterations, checks uint64
}
type cpuStressJoined struct {
	row                              CPUStressWorker
	state                            uint64
	started, ready, entered, stopped time.Time
	overflow, panicked               bool
}

func runCPUStressWith(parent context.Context, options CPUStressOptions, operations cpuStressOperations) (result CPUStressResult, returned error) {
	if nilCPUStressContext(parent) || options.Workers < 0 || options.Workers > 64 ||
		operations.now == nil || operations.numCPU == nil || operations.monitor == nil || operations.worker == nil {
		return CPUStressResult{}, ErrCPUStressOptions
	}
	selection := "explicit"
	if options.Workers == 0 {
		selection = "automatic"
		options.Workers = min(operations.numCPU(), 64)
		if options.Workers < 1 {
			return CPUStressResult{}, ErrCPUStressOptions
		}
	}
	if options.Root == "" {
		options.Root = "/"
	}
	clock := &cpuStressClock{read: operations.now}
	origin := clock.now()
	clock.origin = origin
	localDeadline := origin.Add(cpuStressWatchdog)
	effective := localDeadline
	deadlineSource := "watchdog"
	parentDeadline, hasParentDeadline := parent.Deadline()
	if hasParentDeadline {
		if parentDeadline.Before(localDeadline) {
			effective, deadlineSource = parentDeadline, "parent"
		} else if parentDeadline.Equal(localDeadline) {
			deadlineSource = "both"
		}
	}
	result = CPUStressResult{
		Schema: CPUStressSchema, Status: StatusInconclusive, StopReason: "incomplete_work",
		RequestedLoadMilliseconds: CPUStressLoadMilliseconds,
		LocalWatchdogMilliseconds: CPUStressWatchdogMilliseconds,
		ThermalPolicy:             CPUStressThermalPolicy, WorkerSelection: selection,
		WorkersRequested: options.Workers, EffectiveDeadlineSource: deadlineSource,
		KnownAnswerSHA256: hex.EncodeToString(expectedKnownAnswer[:]),
		ScalarSuite:       CPUScalarSuite, ScalarVectorCount: CPUScalarVectorCount,
		AccountingValid: true, Safety: unstartedSafety(), Workers: []CPUStressWorker{},
	}
	if effective.After(origin) {
		result.EffectiveDeadlineMilliseconds = clock.offset(effective)
	}
	result.Safety.GlobalTimeoutMilliseconds = CPUStressWatchdogMilliseconds
	result.Safety.ThermalLimitMilliCelsius = CPUStressThermalLimitMilliCelsius
	var monitor *safetyMonitor
	var cancelTimed context.CancelFunc
	var joinedAt time.Time
	defer func() {
		if joinedAt.IsZero() {
			joinedAt = clock.now()
		}
		result.WorkersJoinedMilliseconds = clock.offset(joinedAt)
		if monitor != nil {
			monitor.stop()
			result.Safety = monitor.evidence()
			mergeCPUStressThermalFacts(&result.StopFacts, monitor.stressFacts())
		}
		if cancelTimed != nil {
			cancelTimed()
		}
		finished := clock.now()
		result.FinishedMilliseconds = clock.offset(finished)
		recordCPUStressExternal(&result.StopFacts, parent, parentDeadline, hasParentDeadline, localDeadline, finished)
		result.StopFacts.LaunchIncomplete = result.WorkersLaunched != result.WorkersRequested
		result.StopFacts.ReadinessIncomplete = result.WorkersReady != result.WorkersRequested
		result.StopFacts.ClockInvalid = result.StopFacts.ClockInvalid || clock.invalid.Load()
		result.StopFacts.CleanupUnconfirmed = result.StopFacts.CleanupUnconfirmed || !result.Safety.CleanupConfirmed
		if result.StopFacts.CounterOverflow || result.StopFacts.ClockInvalid || result.StopFacts.WorkerPanic {
			result.AccountingValid = false
		}
		result.Status, result.StopReason = classifyCPUStress(result)
		if !result.AccountingValid {
			returned = errors.Join(returned, ErrCPUStressAccounting)
		}
		if result.StopFacts.CleanupUnconfirmed {
			returned = errors.Join(returned, ErrCPUStressCleanupUnconfirmed)
		}
	}()
	if !cpuStressBudget(&result, parent, parentDeadline, hasParentDeadline, localDeadline, effective, clock) {
		return result, nil
	}
	timed, cancel := context.WithDeadline(parent, localDeadline)
	cancelTimed = cancel
	monitor = operations.monitor(timed, options.Root, cpuStressWatchdog, CPUStressThermalLimitMilliCelsius)
	if monitor == nil {
		result.AccountingValid = false
		return result, nil
	}
	if monitor.stress == nil {
		result.AccountingValid = false
		return result, nil
	}
	if !monitor.admitCPUStress() {
		return result, nil
	}
	if !cpuStressBudget(&result, parent, parentDeadline, hasParentDeadline, localDeadline, effective, clock) {
		return result, nil
	}
	work, stopWork := context.WithCancel(monitor.context)
	defer stopWork()
	var failed atomic.Bool
	var owners sync.WaitGroup
	notices := make(chan cpuStressNotice, options.Workers*2)
	completed := make(chan cpuStressJoined, options.Workers)
	release := make(chan struct{})
	shared := &cpuStressRelease{}
	for index := 0; index < options.Workers; index++ {
		if cpuStopped(work, effective, &failed, clock.now) || clock.invalid.Load() {
			break
		}
		result.WorkersLaunched++
		owners.Add(1)
		go func(index int) {
			defer owners.Done()
			worker := runCPUStressWorker(work, effective, parent, parentDeadline, hasParentDeadline,
				localDeadline, index, &failed, monitor, clock, operations.worker, release, shared, notices, stopWork)
			completed <- worker
		}(index)
	}
	allReady := result.WorkersLaunched == options.Workers
	readyCount := 0
	acknowledged := make([]bool, options.Workers)
	for allReady && readyCount < options.Workers {
		select {
		case notice := <-notices:
			if !notice.ready {
				allReady = false
			} else if notice.index < 0 || notice.index >= len(acknowledged) || acknowledged[notice.index] ||
				notice.at.Before(origin) || !notice.at.Before(effective) || notice.iterations != 1 || notice.checks != CPUScalarVectorCount {
				result.AccountingValid = false
				allReady = false
			} else {
				acknowledged[notice.index] = true
				readyCount++
			}
		case <-work.Done():
			allReady = false
		}
	}
	if allReady && !cpuStopped(work, effective, &failed, clock.now) &&
		cpuStressBudget(&result, parent, parentDeadline, hasParentDeadline, localDeadline, effective, clock) {
		started := clock.now()
		// Recheck the exact timestamp used for release, not an earlier budget sample.
		startMS := clock.offset(started)
		if cpuStressBudgetAt(&result, parent, parentDeadline, hasParentDeadline, localDeadline, effective, started, clock) &&
			work.Err() == nil && !failed.Load() && !clock.invalid.Load() {
			shared.started, shared.deadline = started, started.Add(cpuStressLoad)
			result.LoadStarted = true
			result.LoadStartedMilliseconds = &startMS
			close(release)
		}
	}
	if !result.LoadStarted {
		stopWork()
	}
	owners.Wait()
	joinedAt = clock.now()
	joined := make([]cpuStressJoined, result.WorkersLaunched)
	for range joined {
		worker := <-completed
		joined[worker.row.Index] = worker
	}
	var firstStop time.Time
	for _, worker := range joined {
		collectCPUStressWorker(&result, worker, clock)
		if result.LoadStarted {
			if firstStop.IsZero() || worker.stopped.Before(firstStop) {
				firstStop = worker.stopped
			}
			if worker.entered.IsZero() || worker.stopped.Before(shared.deadline) {
				result.StopFacts.EarlyWorkerExit = true
			}
			if worker.row.LoadIterations == 0 || worker.row.LoadChecksPassed < CPUScalarVectorCount {
				result.StopFacts.IncompleteLoadChecks = true
			}
		}
	}
	if result.LoadStarted {
		stoppedMS := clock.offset(firstStop)
		result.LoadStoppedMilliseconds = &stoppedMS
		observed := firstStop.Sub(shared.started)
		if observed < 0 {
			clock.invalid.Store(true)
		} else {
			result.ObservedLoadMilliseconds = min(observed.Milliseconds(), CPUStressLoadMilliseconds)
		}
		result.StopFacts.NormalLoadComplete = !firstStop.Before(shared.deadline)
	}
	return result, nil
}

func cpuStressBudget(result *CPUStressResult, parent context.Context, parentDeadline time.Time, hasParent bool, local, effective time.Time, clock *cpuStressClock) bool {
	return cpuStressBudgetAt(result, parent, parentDeadline, hasParent, local, effective, clock.now(), clock)
}

func cpuStressBudgetAt(result *CPUStressResult, parent context.Context, parentDeadline time.Time, hasParent bool, local, effective, now time.Time, clock *cpuStressClock) bool {
	recordCPUStressExternal(&result.StopFacts, parent, parentDeadline, hasParent, local, now)
	if result.StopFacts.ParentCancelled || result.StopFacts.ParentDeadline || result.StopFacts.WatchdogDeadline || clock.invalid.Load() {
		return false
	}
	if !now.Add(cpuStressLoad).Before(effective) ||
		clock.offset(now)+CPUStressLoadMilliseconds >= result.EffectiveDeadlineMilliseconds {
		result.StopFacts.InsufficientBudget = true
		return false
	}
	return true
}

func recordCPUStressExternal(facts *CPUStressStopFacts, parent context.Context, parentDeadline time.Time, hasParent bool, local time.Time, now time.Time) {
	facts.ParentDeadline = facts.ParentDeadline || hasParent && !now.Before(parentDeadline) || parent.Err() == context.DeadlineExceeded
	facts.WatchdogDeadline = facts.WatchdogDeadline || !now.Before(local)
	facts.ParentCancelled = facts.ParentCancelled || parent.Err() == context.Canceled
}

func runCPUStressWorker(ctx context.Context, effective time.Time, parent context.Context, parentDeadline time.Time, hasParent bool,
	local time.Time, index int, failed *atomic.Bool, monitor *safetyMonitor, clock *cpuStressClock, factory func(int) cpuOperations,
	release <-chan struct{}, shared *cpuStressRelease, notices chan<- cpuStressNotice, stopWork context.CancelFunc) (out cpuStressJoined) {
	out.row.Index = index
	out.row.FailurePhase = "none"
	work := cpuWorkerResult{row: CPUScalarWorker{Index: index}, state: uint64(index) + 0x0123456789abcdef}
	var loadDeadline time.Time
	defer func() {
		if recover() != nil {
			out.panicked = true
			failed.Store(true)
		}
		out.stopped = clock.now()
		out.state, out.overflow = work.state, work.overflow
		out.row.Iterations, out.row.ChecksPassed = work.row.Iterations, work.row.ChecksPassed
		out.row.CalculationFailures, out.row.Mismatch = work.failures, work.row.Mismatch
		if out.entered.IsZero() {
			out.row.PreparationIterations, out.row.PreparationChecksPassed = work.row.Iterations, work.row.ChecksPassed
		}
		out.row.LoadIterations = work.row.Iterations - out.row.PreparationIterations
		out.row.LoadChecksPassed = work.row.ChecksPassed - out.row.PreparationChecksPassed
		if work.failures.Integer || work.failures.SHA256 || work.failures.Scalar {
			out.row.FailurePhase = "readiness"
			if !out.entered.IsZero() {
				out.row.FailurePhase = "load"
			}
		}
		out.row.ExitReason = cpuStressWorkerReason(out, ctx, parent, parentDeadline, hasParent, local, loadDeadline, monitor)
		if loadDeadline.IsZero() || out.stopped.Before(loadDeadline) || out.panicked || out.overflow || clock.invalid.Load() {
			stopWork()
		}
		notices <- cpuStressNotice{index: index, at: out.stopped}
	}()
	out.started = clock.now()
	operations := factory(index)
	if operations.integer == nil || operations.sha == nil || operations.scalar == nil {
		out.panicked = true
		return out
	}
	operations.now = clock.now
	vectors := scalarVectors()
	if !stepCPUWorker(ctx, effective, failed, operations, &vectors, &work) || clock.invalid.Load() {
		return out
	}
	readyAt := clock.now()
	if cpuStopped(ctx, effective, failed, clock.now) || clock.invalid.Load() {
		return out
	}
	out.ready = readyAt
	out.row.PreparationIterations, out.row.PreparationChecksPassed = work.row.Iterations, work.row.ChecksPassed
	notices <- cpuStressNotice{ready: true, index: index, at: readyAt, iterations: work.row.Iterations, checks: work.row.ChecksPassed}
	select {
	case <-release:
		loadDeadline = shared.deadline
	case <-ctx.Done():
		return out
	}
	if cpuStopped(ctx, loadDeadline, failed, clock.now) || clock.invalid.Load() {
		return out
	}
	out.entered = clock.now()
	if cpuStopped(ctx, loadDeadline, failed, clock.now) || clock.invalid.Load() {
		out.entered = time.Time{}
		return out
	}
	for !clock.invalid.Load() && stepCPUWorker(ctx, loadDeadline, failed, operations, &vectors, &work) {
	}
	return out
}

func cpuStressWorkerReason(worker cpuStressJoined, ctx, parent context.Context, parentDeadline time.Time, hasParent bool,
	local, loadDeadline time.Time, monitor *safetyMonitor) string {
	if worker.row.CalculationFailures.Integer || worker.row.CalculationFailures.SHA256 || worker.row.CalculationFailures.Scalar {
		return "calculation_mismatch"
	}
	if worker.panicked || worker.overflow {
		return "incomplete_work"
	}
	facts := monitor.stressFacts()
	recordCPUStressExternal(&facts, parent, parentDeadline, hasParent, local, worker.stopped)
	if facts.ObservationTimeout || facts.ObserverPanic || facts.InvalidObservation {
		return "observer_failure"
	}
	if facts.ThermalLimit {
		return "thermal_limit"
	}
	if facts.InitialCoveragePartial || facts.InitialCoverageUnavailable || facts.CoverageLost {
		return "coverage_loss"
	}
	if facts.ParentDeadline && (!facts.WatchdogDeadline || !hasParent || !local.Before(parentDeadline)) {
		return "parent_deadline"
	}
	if facts.WatchdogDeadline {
		return "watchdog"
	}
	if facts.ParentCancelled {
		return "cancelled"
	}
	if !loadDeadline.IsZero() && !worker.stopped.Before(loadDeadline) {
		return "completed"
	}
	if ctx.Err() != nil {
		return "peer_stop"
	}
	return "incomplete_work"
}

func collectCPUStressWorker(result *CPUStressResult, worker cpuStressJoined, clock *cpuStressClock) {
	row := worker.row
	row.StartedMilliseconds, row.StoppedMilliseconds = clock.offset(worker.started), clock.offset(worker.stopped)
	if !worker.ready.IsZero() {
		at := clock.offset(worker.ready)
		row.ReadyMilliseconds = &at
		result.WorkersReady++
	}
	if !worker.entered.IsZero() {
		at := clock.offset(worker.entered)
		row.LoadEnteredMilliseconds = &at
	}
	if worker.stopped.Before(worker.started) || !worker.ready.IsZero() && worker.ready.Before(worker.started) ||
		!worker.entered.IsZero() && (worker.ready.IsZero() || worker.entered.Before(worker.ready) || worker.stopped.Before(worker.entered)) {
		clock.invalid.Store(true)
	}
	result.Workers = append(result.Workers, row)
	result.CalculationFailures.Integer = result.CalculationFailures.Integer || row.CalculationFailures.Integer
	result.CalculationFailures.SHA256 = result.CalculationFailures.SHA256 || row.CalculationFailures.SHA256
	result.CalculationFailures.Scalar = result.CalculationFailures.Scalar || row.CalculationFailures.Scalar
	result.StopFacts.WorkerPanic = result.StopFacts.WorkerPanic || worker.panicked
	if worker.overflow || math.MaxUint64-result.Iterations < row.Iterations || math.MaxUint64-result.ChecksPassed < row.ChecksPassed {
		result.StopFacts.CounterOverflow = true
	} else {
		result.Iterations += row.Iterations
		result.ChecksPassed += row.ChecksPassed
	}
	result.StateAggregate += worker.state
}

func mergeCPUStressThermalFacts(target *CPUStressStopFacts, source CPUStressStopFacts) {
	target.InitialCoveragePartial = source.InitialCoveragePartial
	target.InitialCoverageUnavailable = source.InitialCoverageUnavailable
	target.CoverageLost = source.CoverageLost
	target.ObservationTimeout = source.ObservationTimeout
	target.ObserverPanic = source.ObserverPanic
	target.InvalidObservation = source.InvalidObservation
	target.ThermalLimit = source.ThermalLimit
	target.CounterOverflow = target.CounterOverflow || source.CounterOverflow
	target.CleanupUnconfirmed = source.CleanupUnconfirmed
}

func classifyCPUStress(result CPUStressResult) (Status, string) {
	f := result.StopFacts
	switch {
	case result.CalculationFailures.Integer || result.CalculationFailures.SHA256 || result.CalculationFailures.Scalar:
		return StatusFail, "calculation_mismatch"
	case f.CleanupUnconfirmed:
		return StatusError, "cleanup_unconfirmed"
	case !result.AccountingValid || f.CounterOverflow || f.ClockInvalid || f.WorkerPanic:
		return StatusError, "invalid_accounting"
	case f.ObservationTimeout || f.ObserverPanic || f.InvalidObservation:
		return StatusError, "observer_failure"
	case f.ThermalLimit:
		return StatusAborted, "thermal_limit"
	case f.InitialCoveragePartial || f.InitialCoverageUnavailable || f.CoverageLost:
		return StatusInconclusive, "thermal_coverage"
	case f.ParentDeadline && (!f.WatchdogDeadline || result.EffectiveDeadlineSource != "watchdog"):
		return StatusAborted, "parent_deadline"
	case f.WatchdogDeadline:
		return StatusAborted, "watchdog"
	case f.ParentCancelled:
		return StatusAborted, "parent_cancelled"
	case f.InsufficientBudget:
		return StatusInconclusive, "insufficient_budget"
	case !result.LoadStarted || !f.NormalLoadComplete || f.LaunchIncomplete || f.ReadinessIncomplete || f.EarlyWorkerExit || f.IncompleteLoadChecks || !cpuStressPassTimes(result):
		return StatusInconclusive, "incomplete_work"
	default:
		return StatusPass, "completed"
	}
}

func cpuStressPassTimes(result CPUStressResult) bool {
	if result.LoadStartedMilliseconds == nil || result.ObservedLoadMilliseconds != CPUStressLoadMilliseconds ||
		result.FinishedMilliseconds >= result.EffectiveDeadlineMilliseconds {
		return false
	}
	target := *result.LoadStartedMilliseconds + CPUStressLoadMilliseconds
	if target >= result.EffectiveDeadlineMilliseconds {
		return false
	}
	for _, row := range result.Workers {
		if row.LoadEnteredMilliseconds == nil || *row.LoadEnteredMilliseconds >= target || row.StoppedMilliseconds < target {
			return false
		}
	}
	return true
}
