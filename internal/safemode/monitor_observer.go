package safemode

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

type monitorCompletion struct {
	job               monitorJob
	observation       Result
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

func callMonitorObserver(observe func(context.Context) Result, ctx context.Context) (result Result, panicked bool) {
	panicked = true
	defer func() { _ = recover() }()
	result = observe(ctx)
	panicked = false
	return
}

func (monitor *Monitor) observe(jobs <-chan monitorJob, completions chan<- monitorCompletion, done chan<- struct{}, observe func(context.Context) Result) {
	defer close(done)
	for job := range jobs {
		completion := monitorCompletion{job: job}
		if monitor.clock.now().Before(job.deadline) && job.ctx.Err() == nil {
			completion.invoked = true
			completion.observation, completion.panicked = callMonitorObserver(observe, job.ctx)
		}
		completion.finished = monitor.clock.now()
		completions <- completion
	}
}

func (monitor *Monitor) complete(completion monitorCompletion) {
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
	observation := monitorObservation(completion.observation)
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
	if gap > MonitorMaximumGap {
		monitor.fail("sampling_gap", nil, finished)
	}
	if !strictMonitorSafety(completion.observation) {
		reason := "invalid_observation"
		if completion.observation.Schema == Schema && completion.observation.Status == "fail" &&
			(completion.observation.WritableBlockMounts > 0 || completion.observation.ActiveNonLoopInterfaces > 0) {
			reason = "unsafe_observation"
		}
		monitor.fail(reason, &completion.observation, finished)
	}
}
