package storagesample

import (
	"context"
	"errors"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/workerexec"
)

type sampleBudget struct {
	start, last, deadline time.Time
	limit                 time.Duration
	now                   func() time.Time
}

func newSampleBudget(limit time.Duration, now func() time.Time) (*sampleBudget, error) {
	if now == nil || limit < time.Millisecond || limit > 180*time.Second || limit%time.Millisecond != 0 {
		return nil, errSampleProtocol
	}
	start := now()
	if start.IsZero() || !start.Add(limit).After(start) {
		return nil, errSampleProtocol
	}
	return &sampleBudget{start: start, last: start, deadline: start.Add(limit), limit: limit, now: now}, nil
}

// The same budget survives discovery, all targets and final closure. A callback
// can block past it; observing the clock afterwards cannot qualify late work.
func (budget *sampleBudget) observe(ctx context.Context) (time.Time, string) {
	current := budget.now()
	if current.IsZero() || current.Before(budget.last) || current.Sub(budget.start) < 0 {
		return budget.last, "clock_invalid"
	}
	budget.last = current
	if current.Sub(budget.start) > time.Hour {
		return current, "clock_invalid"
	}
	if !current.Before(budget.deadline) {
		return current, "deadline"
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return current, "deadline"
		}
		return current, "cancelled"
	}
	return current, ""
}

func sampleMilliseconds(start, end time.Time, roundUp bool) int64 {
	delta := end.Sub(start)
	value := int64(delta / time.Millisecond)
	if roundUp && delta%time.Millisecond != 0 {
		value++
	}
	return value
}

type sampleTargetOwner interface {
	Check(context.Context) error
	Close() error
}

type sampleParentOperations struct {
	open func(context.Context, storagetest.Target) (sampleTargetOwner, error)
	run  func(context.Context, storagetest.Target, string, time.Duration) (workerexec.Result, error)
}

// Summary is raw diagnostic accounting; only Device.Accepted permits its
// contribution to the producer's accepted totals.
type sampleParentOutcome struct {
	Device           storagetest.DeviceResult
	Summary          storagetest.ObservationSummary
	Error            string
	CleanupConfirmed bool
}

// parentSampleTarget owns acquisition, process completion, final binding and
// exactly one parent-owner close. The adapters must establish the live target
// and discovery/lifetime contract; these interfaces do not manufacture one.
// No accepted flag is set until successful final binding and closure, a closed
// child protocol, owned-process cleanup and the real run/target deadlines agree.
func parentSampleTarget(ctx context.Context, budget *sampleBudget, target storagetest.Target, snapshot string, operations sampleParentOperations) (out sampleParentOutcome, err error) {
	if nilSampleValue(ctx) || budget == nil || budget.now == nil || operations.open == nil || operations.run == nil || !sampleSnapshotDigest(snapshot) {
		return out, errSampleProtocol
	}
	initial, inputErr := prepareSampleObservation(target)
	if inputErr != nil {
		return out, inputErr
	}
	target = copyAdmittedSampleTarget(target)
	out = sampleParentOutcome{
		Device: storagetest.DeviceResult{Interface: initial.Interface, Requested: true, Eligibility: "eligible", Exclusion: "none",
			Status: "not_started", Error: "none", CloseConfirmed: true},
		Error: "none", CleanupConfirmed: true,
	}
	started, reason := budget.observe(ctx)
	if reason != "" {
		out.Error = reason
		return out, nil
	}
	out.Device.StartedMilliseconds = sampleMilliseconds(budget.start, started, false)
	out.Device.Status, out.Device.Error = "error", "open_error"
	fail := func(code string) {
		if out.Error == "none" {
			out.Error = code
		}
		if out.Device.Observation != nil && out.Device.Observation.Status != "pass" {
			out.Device.Status, out.Device.Error = out.Device.Observation.Status, out.Device.Observation.Error
		} else {
			out.Device.Status, out.Device.Error = "error", out.Error
			if out.Error == "cancelled" {
				out.Device.Status = "aborted"
			}
		}
	}
	check := func(requireRemaining bool) (time.Time, bool) {
		current, code := budget.observe(ctx)
		targetEnd := started.Add(10 * time.Second)
		if code == "" && (current.After(targetEnd) || requireRemaining && !current.Before(targetEnd)) {
			code = "deadline"
		}
		if code != "" {
			fail(code)
			return current, false
		}
		return current, true
	}
	owner, openErr := operations.open(ctx, copyAdmittedSampleTarget(target))
	owned := !nilSampleValue(owner)
	childClosed := true
	if owned {
		out.Device.CloseConfirmed = false
	}
	// Install closure immediately, including owner-plus-error returns.
	defer func() {
		if owned {
			if owner.Close() != nil {
				out.CleanupConfirmed = false
				out.Error = "cleanup_unconfirmed"
			} else {
				out.Device.CloseConfirmed = childClosed
			}
		}
		current, within := check(false)
		out.Device.FinishedMilliseconds = sampleMilliseconds(budget.start, current, true)
		if !out.CleanupConfirmed {
			fail("cleanup_unconfirmed")
		}
		interval := out.Device.FinishedMilliseconds - out.Device.StartedMilliseconds
		if interval > 10000 && out.Error == "none" {
			fail("deadline")
		}
		if out.Error == "none" && within && out.Device.CloseConfirmed && out.Device.FinalBindingConfirmed && out.Device.Observation != nil {
			out.Device.Accepted = true
			out.Device.Status, out.Device.Error = out.Device.Observation.Status, out.Device.Observation.Error
		} else {
			if out.Error == "none" {
				fail("protocol_error")
			} else {
				fail(out.Error)
			}
		}
	}()
	if openErr != nil || !owned {
		fail("open_error")
		return out, nil
	}
	if _, ok := check(true); !ok {
		return out, nil
	}
	if owner.Check(ctx) != nil {
		fail("identity_changed")
		return out, nil
	}
	current, ok := check(true)
	if !ok {
		return out, nil
	}
	end := budget.deadline
	if targetEnd := started.Add(10 * time.Second); targetEnd.Before(end) {
		end = targetEnd
	}
	remaining := end.Sub(current)
	if remaining <= 0 {
		fail("deadline")
		return out, nil
	}
	completed, runErr := operations.run(ctx, copyAdmittedSampleTarget(target), snapshot, remaining)
	reply, summary, decodeErr := decodeSampleWorkerReply(target, snapshot, completed.Stdout)
	if decodeErr == nil {
		out.Device.Observation = reply.Observation
		out.Summary = summary
		// An explicit child close failure remains uncertainty even if another
		// runner or output failure is observed at the same boundary.
		if !reply.CloseConfirmed {
			childClosed = false
			out.CleanupConfirmed = false
			out.Error = "cleanup_unconfirmed"
		}
	}
	switch {
	case errors.Is(runErr, workerexec.ErrCleanupUncertain) || !out.CleanupConfirmed:
		childClosed = false
		out.CleanupConfirmed = false
		out.Error = "cleanup_unconfirmed"
		fail(out.Error)
	case completed.StdoutTruncated || completed.StderrTruncated || len(completed.Stderr) != 0:
		fail("output_error")
	case runErr != nil:
		if errors.Is(runErr, context.DeadlineExceeded) {
			fail("deadline")
		} else if errors.Is(runErr, context.Canceled) {
			fail("cancelled")
		} else if completed.ExitCode < 0 {
			fail("spawn_error")
		} else {
			fail("protocol_error")
		}
	case completed.ExitCode != 0 || decodeErr != nil:
		fail("protocol_error")
	case reply.Error != "none":
		fail(reply.Error)
	case !reply.FinalBindingConfirmed || reply.Observation == nil:
		fail("protocol_error")
	}
	if _, ok := check(true); !ok || out.Error != "none" {
		return out, nil
	}
	if owner.Check(ctx) != nil {
		fail("identity_changed")
		return out, nil
	}
	if _, ok := check(true); !ok {
		return out, nil
	}
	out.Device.FinalBindingConfirmed = true
	return out, nil
}
