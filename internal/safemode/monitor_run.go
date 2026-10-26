package safemode

import (
	"context"
	"time"
)

func (monitor *Monitor) run(observe func(context.Context) Result) {
	jobs := make(chan monitorJob, 1)
	completions := make(chan monitorCompletion, 1)
	observerDone := make(chan struct{})
	go monitor.observe(jobs, completions, observerDone, observe)
	defer func() {
		close(jobs)
		<-observerDone
		monitor.mutex.Lock()
		monitor.joinedAt = monitor.clock.now()
		close(monitor.done)
		monitor.mutex.Unlock()
	}()
	nextStart := monitor.options.Started.Add(MonitorInterval)
	stopSignal, failureSignal := monitor.stop, monitor.failure
	stopping := false
	var active *monitorJob
	var pending *monitorCompletion
	for {
		// Account for completed coverage before checking current sample age.
		if pending == nil && active != nil {
			select {
			case value := <-completions:
				pending = &value
			default:
			}
		}
		if pending != nil {
			monitor.complete(*pending)
			pending, active = nil, nil
		}
		select {
		case <-stopSignal:
			stopping, stopSignal = true, nil
		default:
		}
		now := monitor.clock.now()
		if active != nil && !now.Before(active.deadline) {
			active.cancel(context.DeadlineExceeded)
			monitor.fail("observation_timeout", nil, now)
		}
		monitor.recordGap(now)
		failed := monitor.Failed()
		if failed {
			stopping, failureSignal = true, nil
			if active != nil {
				active.cancel(context.Canceled)
			}
		}
		if stopping && active == nil {
			return
		}
		if !stopping && active == nil && !now.Before(nextStart) {
			base, cancel := context.WithCancelCause(context.Background())
			job := monitorJob{cancel: cancel, started: now, deadline: now.Add(MonitorObservationTimeout)}
			job.ctx = observationContext{Context: base, deadline: job.deadline}
			monitor.mutex.Lock()
			if monitor.stopRequested || monitor.result.FirstFailure != nil {
				monitor.mutex.Unlock()
				cancel(context.Canceled)
				stopping = true
				continue
			}
			monitor.result.AttemptedObservations++
			monitor.mutex.Unlock()
			active = &job
			nextStart = now.Add(MonitorInterval)
			jobs <- job
		}
		var timer monitorTimer
		var timerSignal <-chan time.Time
		if !failed {
			monitor.mutex.Lock()
			wake := monitor.lastCompleted.Add(MonitorMaximumGap).Add(time.Nanosecond)
			monitor.mutex.Unlock()
			if active != nil && active.deadline.Before(wake) {
				wake = active.deadline
			}
			if !stopping && active == nil && nextStart.Before(wake) {
				wake = nextStart
			}
			timer = monitor.clock.timer(max(0, wake.Sub(monitor.clock.now())))
			timerSignal = timer.channel()
		}
		select {
		case value := <-completions:
			pending = &value
		case <-stopSignal:
			stopping, stopSignal = true, nil
		case <-failureSignal:
			stopping, failureSignal = true, nil
		case <-timerSignal:
		}
		if timer != nil {
			timer.stop()
		}
	}
}
