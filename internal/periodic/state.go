package periodic

import (
	"context"
	"time"
)

func cloneSummary[T, A any](result Summary[T, A]) Summary[T, A] {
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
	for _, phase := range []*PhaseSummary[T]{&result.Initial, &result.Final} {
		if phase.Observation != nil {
			value := *phase.Observation
			phase.Observation = &value
		}
	}
	if result.FirstFinding != nil {
		value := *result.FirstFinding
		result.FirstFinding = &value
	}
	return result
}

func (monitor *Monitor[T, A]) fail(reason string, observation *T, now time.Time) {
	monitor.failPhase(reason, NoPhase, observation, now)
}

func (monitor *Monitor[T, A]) failPhase(reason string, phase Phase, observation *T, now time.Time) {
	monitor.mutex.Lock()
	if monitor.result.FirstFailure != nil || monitor.final != nil {
		monitor.mutex.Unlock()
		return
	}
	failure := &Failure[T]{Reason: reason, Phase: phase, ElapsedMilliseconds: max(0, now.Sub(monitor.options.Started).Milliseconds())}
	if observation != nil {
		value := *observation
		failure.Observation = &value
	}
	monitor.result.FirstFailure = failure
	close(monitor.failure)
	monitor.initialOnce.Do(func() { close(monitor.initialDone) })
	monitor.mutex.Unlock()
	if monitor.options.Cancel != nil {
		monitor.options.Cancel()
	}
}

func (monitor *Monitor[T, A]) find(reason string, phase Phase, observation T, now time.Time) {
	monitor.mutex.Lock()
	if monitor.final != nil || monitor.result.FirstFinding != nil {
		monitor.mutex.Unlock()
		return
	}
	monitor.result.FirstFinding = &Finding[T]{Reason: reason, Phase: phase,
		ElapsedMilliseconds: max(0, now.Sub(monitor.options.Started).Milliseconds()), Observation: observation}
	close(monitor.finding)
	monitor.mutex.Unlock()
	if monitor.options.OnFinding != nil {
		monitor.options.OnFinding()
	}
}

func (monitor *Monitor[T, A]) recordGap(now time.Time) bool {
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
	if gap > monitor.options.MaximumGap {
		monitor.fail("sampling_gap", nil, now)
		return false
	}
	return true
}

func (monitor *Monitor[T, A]) freeze(clean bool, now time.Time) Summary[T, A] {
	monitor.freezeMutex.Lock()
	defer monitor.freezeMutex.Unlock()
	monitor.mutex.Lock()
	if monitor.final != nil {
		result := cloneSummary(*monitor.final)
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
	zeroLong := monitor.options.Mode == DelayedPeriodic && monitor.result.CompletedObservations == 0 && now.Sub(monitor.options.Started) >= monitor.options.Interval
	missingPhase := monitor.options.Mode == InitialPeriodicFinal && (!monitor.result.Initial.Completed || !monitor.result.Final.Completed)
	monitor.mutex.Unlock()
	if zeroLong {
		monitor.fail("sampling_gap", nil, now)
	}
	if missingPhase {
		monitor.fail("required_phase_missing", nil, now)
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
		monitor.result.Joined = !monitor.joinedAt.IsZero()
		if monitor.result.Joined {
			monitor.result.JoinedMilliseconds = max(0, monitor.joinedAt.Sub(monitor.options.Started).Milliseconds())
		}
		value := cloneSummary(monitor.result)
		monitor.final = &value
	}
	return cloneSummary(*monitor.final)
}

func (monitor *Monitor[T, A]) requestStop(ctx context.Context) time.Time {
	monitor.freezeMutex.Lock()
	defer monitor.freezeMutex.Unlock()
	monitor.stopOnce.Do(func() {
		monitor.mutex.Lock()
		monitor.stopRequested = true
		monitor.stopDeadline = monitor.clock.now().Add(monitor.options.StopTimeout)
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
func (monitor *Monitor[T, A]) Stop(ctx context.Context) Summary[T, A] {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := monitor.requestStop(ctx)
	monitor.mutex.Lock()
	if monitor.final != nil {
		result := cloneSummary(*monitor.final)
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
