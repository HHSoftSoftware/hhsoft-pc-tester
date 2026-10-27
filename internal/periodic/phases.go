package periodic

import (
	"context"
	"errors"
	"time"
)

func (monitor *Monitor[T, A]) WaitInitial(ctx context.Context) error {
	if monitor.options.Mode != InitialPeriodicFinal || ctx == nil {
		return errors.New("initial observation is unavailable for this request")
	}
	select {
	case <-monitor.initialDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	if monitor.result.FirstFailure != nil || !monitor.result.Initial.Completed {
		return errors.New("initial observation did not complete successfully")
	}
	return nil
}

// A final request is one bounded admission, serialized with Stop and freeze.
// The context bounds queue time as well as the fresh read; it is never renewed.
func (monitor *Monitor[T, A]) ObserveFinal(ctx context.Context) error {
	if monitor.options.Mode != InitialPeriodicFinal {
		return errors.New("final observation is unavailable for this profile")
	}
	monitor.freezeMutex.Lock()
	monitor.mutex.Lock()
	if monitor.stopRequested || monitor.final != nil {
		monitor.mutex.Unlock()
		monitor.freezeMutex.Unlock()
		return errors.New("final observation requested after stop")
	}
	now := monitor.clock.now()
	reason := ""
	var deadline time.Time
	if monitor.finalRequested {
		reason = "final_request_duplicate"
	} else if ctx == nil {
		reason = "final_request_invalid"
	} else if bound, ok := ctx.Deadline(); !ok {
		reason = "final_request_invalid"
	} else if ctx.Err() != nil || !time.Now().Before(bound) {
		reason = "final_request_cancelled"
		if ctx.Err() == context.DeadlineExceeded || !time.Now().Before(bound) {
			reason = "final_request_timeout"
		}
	} else {
		deadline = now.Add(time.Until(bound))
	}
	if reason != "" {
		monitor.mutex.Unlock()
		monitor.failPhase(reason, FinalPhase, nil, now)
		monitor.freezeMutex.Unlock()
		return errors.New(reason)
	}
	if monitor.result.FirstFailure != nil {
		monitor.mutex.Unlock()
		monitor.freezeMutex.Unlock()
		return errors.New("final observation requested after monitor failure")
	}
	monitor.finalRequested, monitor.finalContext, monitor.finalDeadline = true, ctx, deadline
	monitor.finalRequestedAt = now
	monitor.result.Final.RequestedMilliseconds = max(0, now.Sub(monitor.options.Started).Milliseconds())
	monitor.mutex.Unlock()
	monitor.finalRequest <- struct{}{}
	monitor.freezeMutex.Unlock()
	select {
	case <-monitor.finalDone:
	case <-monitor.failure:
	case <-ctx.Done():
	}
	monitor.checkFinalRequest(monitor.clock.now(), true)
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	if monitor.result.FirstFailure != nil || !monitor.result.Final.Completed {
		return errors.New("final observation did not complete successfully")
	}
	return nil
}

func (monitor *Monitor[T, A]) checkFinalRequest(now time.Time, afterWait bool) {
	monitor.mutex.Lock()
	requested, ctx, deadline := monitor.finalRequested, monitor.finalContext, monitor.finalDeadline
	completed := monitor.result.Final.Completed
	monitor.mutex.Unlock()
	if !requested || completed && !afterWait {
		return
	}
	reason := ""
	if ctx.Err() != nil {
		reason = "final_request_cancelled"
		if ctx.Err() == context.DeadlineExceeded {
			reason = "final_request_timeout"
		}
	} else if (!completed || afterWait) && !now.Before(deadline) {
		reason = "final_request_timeout"
	}
	if reason != "" {
		monitor.failPhase(reason, FinalPhase, nil, now)
	}
}
