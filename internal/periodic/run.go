package periodic

import (
	"context"
	"time"
)

func (monitor *Monitor[T, A]) run(observe func(context.Context) T) {
	jobs := make(chan monitorJob, 1)
	completions := make(chan monitorCompletion[T], 1)
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
	nextStart := monitor.options.Started.Add(monitor.options.Interval)
	initialPending := monitor.options.Mode == InitialPeriodicFinal
	stopSignal, failureSignal := monitor.stop, monitor.failure
	stopping := false
	var active *monitorJob
	var pending *monitorCompletion[T]
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
		monitor.checkFinalRequest(now, false)
		if active != nil && !now.Before(active.deadline) {
			active.cancel(context.DeadlineExceeded)
			monitor.failPhase("observation_timeout", active.phase, nil, now)
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
		monitor.mutex.Lock()
		finalPending := monitor.finalRequested && !monitor.result.Final.Attempted
		monitor.mutex.Unlock()
		if !stopping && active == nil && (initialPending || finalPending || !now.Before(nextStart)) {
			base, cancel := context.WithCancelCause(context.Background())
			job := monitorJob{cancel: cancel, started: now, deadline: now.Add(monitor.options.ObservationTimeout), phase: PeriodicPhase}
			monitor.mutex.Lock()
			if monitor.stopRequested || monitor.result.FirstFailure != nil {
				monitor.mutex.Unlock()
				cancel(context.Canceled)
				stopping = true
				continue
			}
			if monitor.options.Mode == InitialPeriodicFinal {
				// Admission may have happened since the loop's earlier clock read.
				// Capture actual dispatch under the admission lock; never backdate a
				// newly admitted final read to a pending periodic tick.
				now = monitor.clock.now()
				job.started, job.deadline = now, now.Add(monitor.options.ObservationTimeout)
				requested := monitor.initialRequestedAt
				if !initialPending && monitor.finalRequested && !monitor.result.Final.Attempted {
					requested = monitor.finalRequestedAt
				}
				if now.Before(requested) {
					monitor.mutex.Unlock()
					cancel(context.Canceled)
					monitor.fail("clock_error", nil, now)
					continue
				}
			}
			if initialPending {
				job.phase = InitialPhase
				initialPending = false
				monitor.result.Initial.Attempted = true
				monitor.result.Initial.StartedMilliseconds = max(0, now.Sub(monitor.options.Started).Milliseconds())
			} else if monitor.finalRequested && !monitor.result.Final.Attempted {
				if monitor.finalContext.Err() != nil || !now.Before(monitor.finalDeadline) {
					monitor.mutex.Unlock()
					cancel(context.Canceled)
					monitor.checkFinalRequest(now, false)
					continue
				}
				job.phase = FinalPhase
				job.deadline = minTime(job.deadline, monitor.finalDeadline)
				monitor.result.Final.Attempted = true
				monitor.result.Final.StartedMilliseconds = max(0, now.Sub(monitor.options.Started).Milliseconds())
			}
			monitor.result.AttemptedObservations++
			monitor.mutex.Unlock()
			job.ctx = observationContext{Context: base, deadline: job.deadline}
			active = &job
			if job.phase != FinalPhase {
				nextStart = now.Add(monitor.options.Interval)
			}
			jobs <- job
		}
		var timer monitorTimer
		var timerSignal <-chan time.Time
		if !failed {
			monitor.mutex.Lock()
			wake := monitor.lastCompleted.Add(monitor.options.MaximumGap).Add(time.Nanosecond)
			if monitor.finalRequested && !monitor.result.Final.Completed && monitor.finalDeadline.Before(wake) {
				wake = monitor.finalDeadline
			}
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
		case <-monitor.finalRequest:
		}
		if timer != nil {
			timer.stop()
		}
	}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
