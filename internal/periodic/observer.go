package periodic

import (
	"context"
	"time"
)

// A single worker owns every synchronous observation. The supervisor can
// report a deadline without pretending that a blocked kernel call has joined.
type monitorJob struct {
	ctx               context.Context
	cancel            context.CancelCauseFunc
	started, deadline time.Time
	phase             Phase
}

type monitorCompletion[T any] struct {
	job               monitorJob
	observation       T
	finished          time.Time
	invoked, panicked bool
}

// The supervisor's clock enforces this deadline, including in deterministic
// tests. The wrapped context exposes the same deadline and cancellation cause.
type observationContext struct {
	context.Context
	deadline time.Time
}

func (ctx observationContext) Deadline() (time.Time, bool) { return ctx.deadline, true }
func (ctx observationContext) Err() error {
	if context.Cause(ctx.Context) == context.DeadlineExceeded {
		return context.DeadlineExceeded
	}
	return ctx.Context.Err()
}

func callMonitorObserver[T any](observe func(context.Context) T, ctx context.Context) (result T, panicked bool) {
	panicked = true
	defer func() { _ = recover() }()
	result = observe(ctx)
	panicked = false
	return
}

func (monitor *Monitor[T, A]) observe(jobs <-chan monitorJob, completions chan<- monitorCompletion[T], done chan<- struct{}, observe func(context.Context) T) {
	defer close(done)
	for job := range jobs {
		completion := monitorCompletion[T]{job: job}
		if monitor.clock.now().Before(job.deadline) && job.ctx.Err() == nil {
			completion.invoked = true
			completion.observation, completion.panicked = callMonitorObserver(observe, job.ctx)
		}
		completion.finished = monitor.clock.now()
		completions <- completion
	}
}

func (monitor *Monitor[T, A]) complete(completion monitorCompletion[T]) {
	defer completion.job.cancel(context.Canceled)
	monitor.freezeMutex.Lock()
	defer monitor.freezeMutex.Unlock()
	monitor.mutex.Lock()
	frozen := monitor.final != nil
	monitor.mutex.Unlock()
	if frozen {
		return
	}
	finished := completion.finished
	phase := completion.job.phase
	if finished.Before(completion.job.started) {
		monitor.failPhase("clock_error", phase, nil, finished)
		return
	}
	if phase == FinalPhase {
		monitor.checkFinalRequest(finished, false)
	}
	// A ready result cannot win a select race against its expired deadline.
	if !finished.Before(completion.job.deadline) {
		completion.job.cancel(context.DeadlineExceeded)
		monitor.failPhase("observation_timeout", phase, nil, finished)
	}
	if !completion.invoked {
		monitor.failPhase("observation_timeout", phase, nil, finished)
		return
	}
	if completion.panicked {
		monitor.failPhase("observer_panic", phase, nil, finished)
		return
	}
	assessment, panicked := callAssessment(monitor.assess, completion.observation)
	if panicked {
		monitor.failPhase("observer_panic", phase, nil, finished)
		return
	}
	observation := assessment.Value
	monitor.mutex.Lock()
	elapsed := max(0, finished.Sub(monitor.options.Started).Milliseconds())
	gap := finished.Sub(monitor.lastCompleted)
	prior := monitor.result.Aggregate
	monitor.mutex.Unlock()
	if gap > monitor.options.MaximumGap {
		monitor.failPhase("sampling_gap", phase, nil, finished)
	}
	if assessment.FailureReason != "" {
		monitor.failPhase(assessment.FailureReason, phase, &observation, finished)
	}
	if assessment.FailureReason == "" && assessment.FindingReason != "" {
		monitor.find(assessment.FindingReason, phase, observation, finished)
	}
	monitor.mutex.Lock()
	eligible := monitor.result.FirstFailure == nil && monitor.final == nil
	monitor.mutex.Unlock()
	reduction := Reduction[A]{Value: prior}
	if monitor.reduce != nil {
		var panicked bool
		reduction, panicked = callReduction(monitor.reduce, prior, Observation[T]{Phase: phase,
			StartedMilliseconds:   max(0, completion.job.started.Sub(monitor.options.Started).Milliseconds()),
			CompletedMilliseconds: elapsed, EligibleForAggregation: eligible, Assessment: assessment})
		if panicked {
			monitor.failPhase("observer_panic", phase, nil, finished)
			return
		}
		if reduction.FailureReason != "" {
			monitor.failPhase(reduction.FailureReason, phase, &observation, finished)
		}
	}
	monitor.mutex.Lock()
	// The owner enforces eligibility itself, even if a faulty reducer changes
	// its returned aggregate after an ineligible observation.
	if eligible && reduction.FailureReason == "" && monitor.result.FirstFailure == nil && monitor.final == nil {
		monitor.result.Aggregate = reduction.Value
	}
	monitor.result.MaximumGapMilliseconds = max(monitor.result.MaximumGapMilliseconds,
		elapsed-monitor.result.LastCompletedMilliseconds)
	monitor.result.CompletedObservations++
	if monitor.result.CompletedObservations == 1 {
		monitor.result.FirstCompletedMilliseconds = elapsed
	}
	monitor.result.LastCompletedMilliseconds = elapsed
	monitor.result.LastObservation = &observation
	monitor.lastCompleted = finished
	var record *PhaseSummary[T]
	if phase == InitialPhase {
		record = &monitor.result.Initial
	}
	if phase == FinalPhase {
		record = &monitor.result.Final
	}
	if record != nil {
		value := observation
		record.Completed, record.CompletedMilliseconds, record.Observation = true, elapsed, &value
	}
	monitor.mutex.Unlock()
	if phase == InitialPhase {
		monitor.initialOnce.Do(func() { close(monitor.initialDone) })
	}
	if phase == FinalPhase {
		monitor.finalOnce.Do(func() { close(monitor.finalDone) })
	}
}

func callReduction[T, A any](reduce Reducer[T, A], prior A, observation Observation[T]) (result Reduction[A], panicked bool) {
	panicked = true
	defer func() { _ = recover() }()
	result = reduce(prior, observation)
	panicked = false
	return
}

// Policy is trusted pure code, but a programming panic must still leave the
// owned observer joined and the monitor failed. Never retain the panic value.
func callAssessment[T any](assess func(T) Assessment[T], value T) (result Assessment[T], panicked bool) {
	panicked = true
	defer func() { _ = recover() }()
	result = assess(value)
	panicked = false
	return
}
