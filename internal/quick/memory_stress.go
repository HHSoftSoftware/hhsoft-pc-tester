package quick

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type memoryStressOperations struct {
	now                     func() time.Time
	open                    func(string) (io.ReadCloser, error)
	allocate                func(int) []byte
	monitor                 safetyMonitorFactory
	beforeChunk, afterChunk func([]byte, uint64, int, int, int, bool)
}

// RunMemoryStress owns one bounded working set for a fixed elapsed load.
// It is a standalone diagnostic, not memory.quick or session authority.
func RunMemoryStress(ctx context.Context, options MemoryStressOptions) (MemoryStressResult, error) {
	return runMemoryStressWith(ctx, options, memoryStressOperations{
		now: time.Now,
		open: func(root string) (io.ReadCloser, error) {
			return os.Open(filepath.Join(filepath.Clean(root), "proc/meminfo"))
		},
		allocate: func(n int) []byte { return make([]byte, n) },
		monitor:  newCPUStressSafetyMonitor,
	})
}

func runMemoryStressWith(parent context.Context, options MemoryStressOptions, operations memoryStressOperations) (result MemoryStressResult, returned error) {
	if nilCPUStressContext(parent) || options.Bytes < 0 ||
		options.Bytes != 0 && (uint64(options.Bytes) < MemoryStressMinimumBytes || uint64(options.Bytes) > MemoryStressMaximumBytes) ||
		operations.now == nil || operations.open == nil || operations.allocate == nil || operations.monitor == nil {
		return MemoryStressResult{}, ErrMemoryStressOptions
	}
	if options.Root == "" {
		options.Root = "/"
	}
	clock := &cpuStressClock{read: operations.now}
	origin := clock.now()
	clock.origin = origin
	local := origin.Add(time.Duration(MemoryStressWatchdogMilliseconds) * time.Millisecond)
	effective, source := local, "watchdog"
	parentDeadline, hasParent := parent.Deadline()
	if hasParent {
		if parentDeadline.Before(local) {
			effective, source = parentDeadline, "parent"
		} else if parentDeadline.Equal(local) {
			source = "both"
		}
	}
	result = MemoryStressResult{
		Schema: MemoryStressSchema, Status: StatusInconclusive, StopReason: "incomplete_work", Suite: MemorySuite,
		RequestedLoadMilliseconds: MemoryStressLoadMilliseconds, LocalWatchdogMilliseconds: MemoryStressWatchdogMilliseconds,
		ThermalPolicy: MemoryStressThermalPolicy, RequestedBytes: uint64(options.Bytes),
		EffectiveDeadlineSource: source, AccountingValid: true,
		Availability: MemoryStressAvailability{CloseConfirmed: true}, Safety: unstartedSafety(),
	}
	if effective.After(origin) {
		result.EffectiveDeadlineMilliseconds = clock.offset(effective)
	}
	result.Safety.GlobalTimeoutMilliseconds = MemoryStressWatchdogMilliseconds
	result.Safety.ThermalLimitMilliCelsius = MemoryStressThermalLimitMilliCelsius
	var monitor *safetyMonitor
	var cancel context.CancelFunc
	var buffer []byte
	stamp := func(at time.Time) *int64 { value := clock.offset(at); return &value }
	defer func() {
		if recover() != nil {
			result.StopFacts.WorkerPanic = true
			result.AccountingValid = false
		}
		stopped := clock.now()
		if result.LoadStartedMilliseconds != nil && result.LoadStoppedMilliseconds == nil {
			result.LoadStoppedMilliseconds = stamp(stopped)
		}
		if result.LoadStartedMilliseconds != nil && result.LoadStoppedMilliseconds != nil {
			result.ObservedLoadMilliseconds = min(MemoryStressLoadMilliseconds, max(int64(0), *result.LoadStoppedMilliseconds-*result.LoadStartedMilliseconds))
		}
		if result.TerminalStartedMilliseconds != nil && result.TerminalStoppedMilliseconds == nil {
			result.TerminalStoppedMilliseconds = stamp(stopped)
		}
		if monitor != nil {
			monitor.stopBefore(effective)
			result.Safety = monitor.evidence()
			mergeMemoryStressThermal(&result.StopFacts, monitor.stressFacts())
		}
		if cancel != nil {
			cancel()
		}
		finished := clock.now()
		result.FinishedMilliseconds = clock.offset(finished)
		recordMemoryStressExternal(&result.StopFacts, parent, parentDeadline, hasParent, local, finished)
		result.StopFacts.ClockInvalid = result.StopFacts.ClockInvalid || clock.invalid.Load()
		result.StopFacts.CleanupUnconfirmed = result.StopFacts.CleanupUnconfirmed ||
			!result.Safety.CleanupConfirmed || !result.Availability.CloseConfirmed
		if result.StopFacts.ClockInvalid || result.StopFacts.CounterOverflow || result.StopFacts.WorkerPanic ||
			!ValidMemoryStressCoverage(result) {
			result.AccountingValid = false
		}
		result.Status, result.StopReason = MemoryStressOutcome(result)
		if !result.AccountingValid {
			returned = errors.Join(returned, ErrMemoryStressAccounting)
		}
		if result.StopFacts.CleanupUnconfirmed {
			returned = errors.Join(returned, ErrMemoryStressCleanupUnconfirmed)
		}
		runtime.KeepAlive(buffer)
	}()
	budget := func(at time.Time) bool {
		recordMemoryStressExternal(&result.StopFacts, parent, parentDeadline, hasParent, local, at)
		if result.StopFacts.ParentCancelled || result.StopFacts.ParentDeadline || result.StopFacts.WatchdogDeadline || clock.invalid.Load() {
			return false
		}
		if !at.Add(time.Duration(MemoryStressLoadMilliseconds)*time.Millisecond).Before(effective) ||
			clock.offset(at)+MemoryStressLoadMilliseconds >= result.EffectiveDeadlineMilliseconds {
			result.StopFacts.InsufficientBudget = true
			return false
		}
		return true
	}
	if !budget(clock.now()) {
		return result, nil
	}
	timed, timerCancel := context.WithDeadline(parent, local)
	cancel = timerCancel
	monitor = operations.monitor(timed, options.Root, time.Duration(MemoryStressWatchdogMilliseconds)*time.Millisecond, MemoryStressThermalLimitMilliCelsius)
	if monitor == nil || monitor.stress == nil {
		result.AccountingValid = false
		return result, nil
	}
	if !monitor.admitCPUStress() || !budget(clock.now()) {
		return result, nil
	}
	completion, joined := acquireMemoryStressAvailability(monitor.context, func() (io.ReadCloser, error) { return operations.open(options.Root) }, clock)
	result.Availability = completion.evidence
	if !joined {
		result.StopFacts.CleanupUnconfirmed = true
		return result, nil
	}
	result.AvailabilityFinishedMilliseconds = stamp(completion.finished)
	result.AvailableBytes = completion.available
	if !completion.evidence.Valid || !completion.evidence.CloseConfirmed || !completion.finished.Before(effective) ||
		!monitor.admitCPUStress() || !budget(clock.now()) {
		return result, nil
	}
	size := uint64(options.Bytes)
	limit := result.AvailableBytes / 4
	if size == 0 {
		size = min(MemoryStressMaximumBytes, limit) / (1 << 20) * (1 << 20)
	}
	if size < MemoryStressMinimumBytes || size > limit {
		result.Availability.Failure = "insufficient"
		return result, nil
	}
	// This conversion is bounded by64MiB on both supported word sizes.
	buffer = operations.allocate(int(size))
	if uint64(len(buffer)) != size || uint64(cap(buffer)) != size {
		result.AccountingValid = false
		return result, nil
	}
	result.AllocatedBytes = size
	allocated := clock.now()
	result.AllocationFinishedMilliseconds = stamp(allocated)
	if !monitor.admitCPUStress() || !budget(allocated) {
		return result, nil
	}
	started := clock.now()
	if !budget(started) || !monitor.admitCPUStress() {
		return result, nil
	}
	result.LoadStartedMilliseconds = stamp(started)
	loadEnd := started.Add(time.Duration(MemoryStressLoadMilliseconds) * time.Millisecond)
	stopped := func(at time.Time) bool {
		recordMemoryStressExternal(&result.StopFacts, parent, parentDeadline, hasParent, local, at)
		return result.StopFacts.ParentCancelled || result.StopFacts.ParentDeadline || result.StopFacts.WatchdogDeadline ||
			clock.invalid.Load() || monitor.failure() != nil
	}
	step := func(terminal bool) bool {
		c := &result.Coverage
		cycle, phase, first := c.CompletedCycles, int(c.CompletedPhases%10), int(c.PhaseBytes)
		if terminal {
			phase, first = 9, int(c.TerminalReadBytes)
		}
		count := min(memoryChunkBytes, len(buffer)-first)
		trial := result
		if !creditMemoryStress(&trial, uint64(count), terminal) {
			result.StopFacts.CounterOverflow = true
			return false
		}
		if operations.beforeChunk != nil {
			operations.beforeChunk(buffer, cycle, phase, first, count, terminal)
		}
		before := clock.now()
		if stopped(before) {
			return false
		}
		// Recheck a fresh cycle at the final pre-kernel observation. An
		// already-active cycle may finish; elapsed load cannot start another.
		if !terminal && phase == 0 && first == 0 && !before.Before(loadEnd) {
			result.StopFacts.NormalLoadComplete = true
			result.LoadStoppedMilliseconds = stamp(before)
			return true
		}
		// Only a byte-sized seed reaches memoryPattern(int). The cycle itself
		// remains uint64; neither an unbounded int cast nor pass*67 can wrap386.
		completed, mismatch := memoryChunk(buffer, int(cycle%256), phase, first, count)
		if completed < 0 || completed > int64(count) {
			result.AccountingValid = false
			return false
		}
		if terminal {
			c.TerminalUncreditedBytes = uint64(completed)
		} else {
			c.UncreditedBytes = uint64(completed)
		}
		if mismatch != nil && result.Mismatch == nil {
			stage := "cycle"
			if terminal {
				stage = "terminal"
			}
			result.Mismatch = &MemoryStressMismatch{Stage: stage, Cycle: cycle, Phase: phase, Offset: uint64(mismatch.Offset), ExpectedByte: mismatch.ExpectedByte, ActualByte: mismatch.ActualByte}
		}
		if operations.afterChunk != nil {
			operations.afterChunk(buffer, cycle, phase, first, count, terminal)
		}
		after := clock.now()
		late := stopped(after)
		if !late {
			if !creditMemoryStress(&result, uint64(completed), terminal) {
				result.StopFacts.CounterOverflow = true
				return false
			}
			if !terminal && result.Coverage.CompletedCycles > cycle && !after.After(loadEnd) {
				next, ok := memoryStressAdd(result.Coverage.LoadCycles, 1)
				if !ok {
					result.StopFacts.CounterOverflow = true
					return false
				}
				result.Coverage.LoadCycles = next
			}
		}
		return !late && mismatch == nil
	}
	for {
		now := clock.now()
		if stopped(now) {
			return result, nil
		}
		if !now.Before(loadEnd) && !result.StopFacts.NormalLoadComplete {
			result.StopFacts.NormalLoadComplete = true
			result.LoadStoppedMilliseconds = stamp(now)
		}
		if result.StopFacts.NormalLoadComplete && result.Coverage.CompletedPhases%10 == 0 && result.Coverage.PhaseBytes == 0 {
			break
		}
		if !step(false) {
			return result, nil
		}
	}
	if stopped(clock.now()) || result.Coverage.CompletedCycles == 0 {
		return result, nil
	}
	result.TerminalStartedMilliseconds = stamp(clock.now())
	for result.Coverage.TerminalReadBytes < result.AllocatedBytes {
		if !step(true) {
			return result, nil
		}
	}
	result.TerminalStoppedMilliseconds = stamp(clock.now())
	return result, nil
}

func recordMemoryStressExternal(f *MemoryStressStopFacts, parent context.Context, parentDeadline time.Time, hasParent bool, local, now time.Time) {
	f.ParentDeadline = f.ParentDeadline || hasParent && !now.Before(parentDeadline) || parent.Err() == context.DeadlineExceeded
	f.WatchdogDeadline = f.WatchdogDeadline || !now.Before(local)
	f.ParentCancelled = f.ParentCancelled || parent.Err() == context.Canceled
}

func mergeMemoryStressThermal(f *MemoryStressStopFacts, thermal CPUStressStopFacts) {
	f.CleanupUnconfirmed = f.CleanupUnconfirmed || thermal.CleanupUnconfirmed
	f.CounterOverflow = f.CounterOverflow || thermal.CounterOverflow
	f.InitialCoveragePartial = f.InitialCoveragePartial || thermal.InitialCoveragePartial
	f.InitialCoverageUnavailable = f.InitialCoverageUnavailable || thermal.InitialCoverageUnavailable
	f.CoverageLost = f.CoverageLost || thermal.CoverageLost
	f.ObservationTimeout = f.ObservationTimeout || thermal.ObservationTimeout
	f.ObserverPanic = f.ObserverPanic || thermal.ObserverPanic
	f.InvalidObservation = f.InvalidObservation || thermal.InvalidObservation
	f.ThermalLimit = f.ThermalLimit || thermal.ThermalLimit
}

// MemoryStressOutcome preserves independent facts and is also checked by the
// strict decoder. A valid diagnostic can describe a non-passing observation.
func MemoryStressOutcome(v MemoryStressResult) (Status, string) {
	f := v.StopFacts
	switch {
	case v.Mismatch != nil:
		return StatusFail, "memory_mismatch"
	case f.CleanupUnconfirmed:
		return StatusError, "cleanup_unconfirmed"
	case !v.AccountingValid || f.CounterOverflow || f.ClockInvalid || f.WorkerPanic:
		return StatusError, "invalid_accounting"
	case f.ObservationTimeout || f.ObserverPanic || f.InvalidObservation:
		return StatusError, "observer_failure"
	case f.ThermalLimit:
		return StatusAborted, "thermal_limit"
	case f.InitialCoveragePartial || f.InitialCoverageUnavailable || f.CoverageLost:
		return StatusInconclusive, "thermal_coverage"
	case f.ParentDeadline && v.EffectiveDeadlineSource != "watchdog":
		return StatusAborted, "parent_deadline"
	case f.WatchdogDeadline:
		return StatusAborted, "watchdog"
	case f.ParentDeadline:
		return StatusAborted, "parent_deadline"
	case f.ParentCancelled:
		return StatusAborted, "parent_cancelled"
	case f.InsufficientBudget:
		return StatusInconclusive, "insufficient_budget"
	case v.Availability.Failure == "open_failed":
		return StatusInconclusive, "availability_unavailable"
	case v.Availability.Failure == "insufficient":
		return StatusInconclusive, "insufficient_availability"
	case v.Availability.Failure != "":
		return StatusError, "availability_failed"
	case !memoryStressPassTimes(v):
		return StatusInconclusive, "incomplete_work"
	default:
		return StatusPass, "completed"
	}
}

func memoryStressPassTimes(v MemoryStressResult) bool {
	if v.LoadStartedMilliseconds == nil || v.LoadStoppedMilliseconds == nil ||
		v.TerminalStartedMilliseconds == nil || v.TerminalStoppedMilliseconds == nil ||
		v.ObservedLoadMilliseconds != MemoryStressLoadMilliseconds || !v.StopFacts.NormalLoadComplete ||
		v.Coverage.LoadCycles == 0 || v.Coverage.CompletedPhases%10 != 0 || v.Coverage.PhaseBytes != 0 ||
		v.Coverage.TerminalReadBytes != v.AllocatedBytes || v.AllocatedBytes < MemoryStressMinimumBytes ||
		v.Coverage.UncreditedBytes != 0 || v.Coverage.TerminalUncreditedBytes != 0 ||
		!v.Availability.Valid || !v.Availability.CloseConfirmed || !v.Safety.CleanupConfirmed ||
		v.Safety.ThermalCoverage != ThermalAvailable || v.FinishedMilliseconds >= v.EffectiveDeadlineMilliseconds {
		return false
	}
	return *v.LoadStartedMilliseconds+MemoryStressLoadMilliseconds <= *v.LoadStoppedMilliseconds &&
		*v.LoadStoppedMilliseconds <= *v.TerminalStartedMilliseconds &&
		*v.TerminalStartedMilliseconds <= *v.TerminalStoppedMilliseconds &&
		*v.TerminalStoppedMilliseconds <= v.FinishedMilliseconds
}
