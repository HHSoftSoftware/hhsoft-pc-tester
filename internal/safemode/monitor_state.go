package safemode

import (
	"context"
	"time"
)

func strictMonitorSafety(result Result) bool {
	return result.Safe() && result.StorageDetail == "" && result.NetworkDetail == ""
}

// Never retain arbitrary observer strings, paths, or panic values in evidence.
func monitorObservation(result Result) Result {
	if result.Schema != Schema {
		result.Status = "error"
	}
	result.Schema = Schema
	switch result.Status {
	case "pass", "fail", "error", "aborted":
	default:
		result.Status = "error"
	}
	if result.StorageDetail != "" {
		result.StorageDetail = "block mount safety could not be established"
	}
	if result.NetworkDetail != "" {
		result.NetworkDetail = "offline network state could not be established"
	}
	return result
}

func cloneMonitorResult(result MonitorResult) MonitorResult {
	if result.FirstFailure != nil {
		failure := *result.FirstFailure
		if failure.Observation != nil {
			value := *failure.Observation
			failure.Observation = &value
		}
		result.FirstFailure = &failure
	}
	if result.LastObservation != nil {
		value := *result.LastObservation
		result.LastObservation = &value
	}
	return result
}

func (monitor *Monitor) fail(reason string, observation *Result, now time.Time) {
	monitor.mutex.Lock()
	if monitor.result.FirstFailure != nil {
		monitor.mutex.Unlock()
		return
	}
	failure := &MonitorFailure{Reason: reason, ElapsedMilliseconds: max(0, now.Sub(monitor.options.Started).Milliseconds())}
	if observation != nil {
		value := monitorObservation(*observation)
		failure.Observation = &value
	}
	monitor.result.FirstFailure = failure
	monitor.result.Status = "error"
	if reason == "unsafe_observation" {
		monitor.result.Status = "fail"
	}
	monitor.mutex.Unlock()
	close(monitor.failure)
	if monitor.options.Cancel != nil {
		monitor.options.Cancel()
	}
}

func (monitor *Monitor) recordGap(now time.Time) bool {
	monitor.mutex.Lock()
	backwards := now.Before(monitor.lastClock)
	if !backwards {
		monitor.lastClock = now
	}
	gap := now.Sub(monitor.lastCompleted)
	monitor.mutex.Unlock()
	if backwards {
		monitor.fail("clock_error", nil, now)
		return false
	}
	if gap > MonitorMaximumGap {
		monitor.fail("sampling_gap", nil, now)
		return false
	}
	return true
}

func (monitor *Monitor) freeze(clean bool, now time.Time) MonitorResult {
	monitor.freezeMutex.Lock()
	defer monitor.freezeMutex.Unlock()
	monitor.mutex.Lock()
	if monitor.final != nil {
		result := cloneMonitorResult(*monitor.final)
		monitor.mutex.Unlock()
		return result
	}
	clean = clean && !monitor.joinedAt.IsZero() && monitor.joinedAt.Before(monitor.stopDeadline) && monitor.stopContext != nil && monitor.stopContext.Err() == nil
	monitor.mutex.Unlock()
	if !clean {
		monitor.fail("cleanup_unconfirmed", nil, now)
	}
	monitor.recordGap(now)
	monitor.mutex.Lock()
	zeroLong := monitor.result.CompletedObservations == 0 && now.Sub(monitor.options.Started) >= MonitorInterval
	monitor.mutex.Unlock()
	if zeroLong {
		monitor.fail("sampling_gap", nil, now)
	}
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	if monitor.final == nil {
		monitor.result.ElapsedMilliseconds = max(0, now.Sub(monitor.options.Started).Milliseconds())
		// Report gaps in the same integer coordinate system as the offsets.
		// The raw monotonic duration was checked above before rounding.
		monitor.result.MaximumGapMilliseconds = max(monitor.result.MaximumGapMilliseconds,
			monitor.result.ElapsedMilliseconds-monitor.result.LastCompletedMilliseconds)
		monitor.result.CleanupConfirmed = clean
		value := cloneMonitorResult(monitor.result)
		monitor.final = &value
	}
	return cloneMonitorResult(*monitor.final)
}

func (monitor *Monitor) requestStop(ctx context.Context) time.Time {
	monitor.freezeMutex.Lock()
	defer monitor.freezeMutex.Unlock()
	monitor.stopOnce.Do(func() {
		monitor.mutex.Lock()
		monitor.stopRequested = true
		monitor.stopDeadline = monitor.clock.now().Add(MonitorStopTimeout)
		monitor.stopContext = ctx
		close(monitor.stop)
		monitor.mutex.Unlock()
	})
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	if monitor.final != nil {
		return monitor.stopDeadline
	}
	if deadline, ok := ctx.Deadline(); ok {
		// Map a caller's remaining duration into the monitor's clock domain.
		// A later caller can shorten the original cleanup window, never renew it.
		candidate := monitor.clock.now().Add(max(0, time.Until(deadline)))
		if candidate.Before(monitor.stopDeadline) {
			monitor.stopDeadline = candidate
		}
	}
	return monitor.stopDeadline
}

// Stop prevents new observations and waits for the owned observer to return.
// It does not cancel a valid in-flight observation. An uncertain first result
// remains uncertain on every later call, even if the observer eventually exits.
func (monitor *Monitor) Stop(ctx context.Context) MonitorResult {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := monitor.requestStop(ctx)
	monitor.mutex.Lock()
	if monitor.final != nil {
		result := cloneMonitorResult(*monitor.final)
		monitor.mutex.Unlock()
		return result
	}
	monitor.mutex.Unlock()
	timer := monitor.clock.timer(max(0, deadline.Sub(monitor.clock.now())))
	defer timer.stop()
	joined := func() bool {
		monitor.mutex.Lock()
		defer monitor.mutex.Unlock()
		return ctx.Err() == nil && monitor.stopContext.Err() == nil && !monitor.joinedAt.IsZero() && monitor.joinedAt.Before(monitor.stopDeadline)
	}
	select {
	case <-monitor.done:
		return monitor.freeze(joined(), monitor.clock.now())
	default:
	}
	select {
	case <-monitor.done:
		return monitor.freeze(joined(), monitor.clock.now())
	case <-ctx.Done():
		return monitor.freeze(false, monitor.clock.now())
	case <-timer.channel():
		return monitor.freeze(joined(), monitor.clock.now())
	}
}
