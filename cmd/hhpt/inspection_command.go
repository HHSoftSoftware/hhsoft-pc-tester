package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
)

const inspectionCommandTimeout = 45 * time.Second
const inspectionPrompt = "Exterior enclosure observation (operator reported). Look only at safely visible surfaces, without opening or moving the device. D: cracks, broken/missing enclosure pieces or deformation observed. N: none of those defects observed on inspected surfaces. I: not inspected or uncertain. Enter D/N/I; Enter records no observation.\n"

type inspectionInputPreparer func(context.Context, io.Reader) (context.Context, io.Reader, func() error, error)

func runInspection(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	return runInspectionCommand(ctx, arguments, input, stdout, stderr, false, prepareSessionInput, writeDestination, time.Now)
}

// A session lends its existing owner to this worker and checks its final join
// before finalization. This entry neither acquires nor finishes that owner.
func runInspectionOwned(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	return runInspectionCommand(ctx, arguments, input, stdout, stderr, true, nil, writeDestination, time.Now)
}

func runInspectionCommand(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer,
	borrowed bool, prepare inspectionInputPreparer, writeOutput destinationWriter, now func() time.Time) int {
	if nilRetestInputValue(stdout) || nilRetestInputValue(stderr) {
		return 1
	}
	flags := flag.NewFlagSet("inspect-exterior", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	timeout := flags.Duration("timeout", inspectionCommandTimeout, "observation timeout, from 10ms to 45s")
	output := flags.String("output", "-", "new JSON output file, or - for stdout")
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *timeout < 10*time.Millisecond || *timeout > inspectionCommandTimeout {
		fmt.Fprintln(stderr, "inspect-exterior requires a 10ms-45s timeout and no positional arguments")
		return 2
	}
	if nilRetestInputValue(ctx) || nilRetestInputValue(input) || writeOutput == nil || now == nil ||
		!borrowed && prepare == nil || borrowed && prepare != nil {
		fmt.Fprintln(stderr, "exterior inspection input is unavailable")
		return 1
	}

	var result exteriorinspection.Result
	var observationErr error
	if borrowed {
		// The same session owner discards queued input before this new prompt.
		// A hard handoff fault must not become a clean cancellation note.
		if discardErr := discardSessionInput(ctx, input); discardErr != nil {
			if errors.Is(discardErr, errSessionInputCleanup) {
				fmt.Fprintln(stderr, "exterior inspection input cleanup could not be confirmed")
				return workerCleanupUncertainExitCode
			}
			result, observationErr = exteriorinspection.Evaluate("", false, inspectionStop(discardErr, ctx.Err(), false), 0)
		} else {
			result, observationErr = observeInspection(ctx, input, stderr, *timeout, now)
		}
	} else if ctx.Err() != nil {
		result, observationErr = observeInspection(ctx, input, stderr, *timeout, now)
	} else {
		owned, source, finish, prepareErr := prepare(ctx, input)
		if finish == nil {
			fmt.Fprintln(stderr, "exterior inspection input cleanup could not be confirmed")
			return workerCleanupUncertainExitCode
		}
		if nilRetestInputValue(owned) || nilRetestInputValue(source) {
			prepareErr = errors.Join(prepareErr, errSessionInput)
		}
		if prepareErr == nil {
			result, observationErr = observeInspection(owned, source, stderr, *timeout, now)
		}
		// This receipt is checked before any output, even after failed acquisition
		// or a prompt/read failure. Observation time excludes this join.
		finishErr := finish()
		if errors.Is(prepareErr, errSessionInputCleanup) || errors.Is(finishErr, errSessionInputCleanup) {
			fmt.Fprintln(stderr, "exterior inspection input cleanup could not be confirmed")
			return workerCleanupUncertainExitCode
		}
		if prepareErr != nil || finishErr != nil {
			fmt.Fprintln(stderr, "exterior inspection input failed")
			return 1
		}
	}
	if observationErr != nil {
		fmt.Fprintln(stderr, "exterior inspection returned invalid evidence")
		return 1
	}
	if err := writeOutput(*output, stdout, func(destination io.Writer) error {
		return json.NewEncoder(destination).Encode(result)
	}); err != nil {
		fmt.Fprintln(stderr, "write exterior inspection result failed")
		return 1
	}
	if result.Status == "error" {
		return 1
	}
	return 0
}

func observeInspection(ctx context.Context, input io.Reader, prompt io.Writer, timeout time.Duration, now func() time.Time) (exteriorinspection.Result, error) {
	if nilRetestInputValue(ctx) || nilRetestInputValue(input) || nilRetestInputValue(prompt) ||
		now == nil || timeout < 10*time.Millisecond || timeout > inspectionCommandTimeout {
		return exteriorinspection.Result{}, exteriorinspection.ErrEvidence
	}
	if stopped := ctx.Err(); stopped != nil {
		return exteriorinspection.Evaluate("", false, inspectionStop(stopped, stopped, false), 0)
	}
	started := now()
	deadline := started.Add(timeout)
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline = parent
	}
	if !started.Before(deadline) {
		return exteriorinspection.Evaluate("", false, "deadline", 0)
	}
	readContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if _, err := io.WriteString(completeOutputWriter{prompt}, inspectionPrompt); err != nil {
		return exteriorinspection.Evaluate("", false, "prompt_error", inspectionObservation(started, now(), deadline))
	}
	line, complete, readErr := readInspectionLine(readContext, input)
	decided := now()
	reason := inspectionStop(readErr, readContext.Err(), !decided.Before(deadline))
	return exteriorinspection.Evaluate(line, complete, reason, inspectionObservation(started, decided, deadline))
}

func inspectionObservation(started, decided, deadline time.Time) int64 {
	if decided.After(deadline) {
		decided = deadline
	}
	if !decided.After(started) {
		return 0
	}
	return min(decided.Sub(started).Milliseconds(), exteriorinspection.MaxObservationMilliseconds)
}

func inspectionStop(readErr, stopped error, late bool) string {
	canceled, deadline, hard := false, false, false
	nodes := 0
	var visit func(error, int)
	visit = func(err error, depth int) {
		if err == nil {
			return
		}
		nodes++
		if nodes > 64 || depth > 32 {
			hard = true
			return
		}
		switch err {
		case io.EOF:
			return
		case context.Canceled:
			canceled = true
			return
		case context.DeadlineExceeded:
			deadline = true
			return
		}
		switch wrapped := err.(type) {
		case interface{ Unwrap() []error }:
			found := false
			for _, child := range wrapped.Unwrap() {
				if child != nil {
					found = true
					visit(child, depth+1)
				}
			}
			if !found {
				hard = true
			}
		case interface{ Unwrap() error }:
			child := wrapped.Unwrap()
			if child == nil {
				hard = true
			} else {
				visit(child, depth+1)
			}
		default:
			hard = true
		}
	}
	// EOF and context leaves are clean, including wrapped/joined trees. A hard
	// sibling must not be hidden by errors.Is matching a cancellation leaf.
	visit(readErr, 0)
	visit(stopped, 0)
	if hard {
		return "input_error"
	}
	if late || deadline {
		return "deadline"
	}
	if canceled {
		return "operator_cancelled"
	}
	return ""
}
