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

func (monitor *Monitor[T]) observe(jobs <-chan monitorJob, completions chan<- monitorCompletion[T], done chan<- struct{}, observe func(context.Context) T) {
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

func (monitor *Monitor[T]) complete(completion monitorCompletion[T]) {
	defer completion.job.cancel(context.Canceled)
	finished := completion.finished
	if finished.Before(completion.job.started) {
		monitor.fail("clock_error", nil, finished)
		return
	}
	// A ready result cannot win a select race against its expired deadline.
	if !finished.Before(completion.job.deadline) {
		completion.job.cancel(context.DeadlineExceeded)
		monitor.fail("observation_timeout", nil, finished)
	}
	if !completion.invoked {
		monitor.fail("observation_timeout", nil, finished)
		return
	}
	if completion.panicked {
		monitor.fail("observer_panic", nil, finished)
		return
	}
	assessment, panicked := callAssessment(monitor.assess, completion.observation)
	if panicked {
		monitor.fail("observer_panic", nil, finished)
		return
	}
	observation := assessment.Value
	monitor.mutex.Lock()
	elapsed := max(0, finished.Sub(monitor.options.Started).Milliseconds())
	gap := finished.Sub(monitor.lastCompleted)
	monitor.result.MaximumGapMilliseconds = max(monitor.result.MaximumGapMilliseconds,
		elapsed-monitor.result.LastCompletedMilliseconds)
	monitor.result.CompletedObservations++
	if monitor.result.CompletedObservations == 1 {
		monitor.result.FirstCompletedMilliseconds = elapsed
	}
	monitor.result.LastCompletedMilliseconds = elapsed
	monitor.result.LastObservation = &observation
	monitor.lastCompleted = finished
	monitor.mutex.Unlock()
	if gap > monitor.options.MaximumGap {
		monitor.fail("sampling_gap", nil, finished)
	}
	if assessment.FailureReason != "" {
		monitor.fail(assessment.FailureReason, &observation, finished)
	}
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
