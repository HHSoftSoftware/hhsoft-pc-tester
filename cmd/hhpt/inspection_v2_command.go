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

func runInspectionCondition(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	return runInspectionConditionCommand(ctx, arguments, input, stdout, stderr, false, prepareSessionInput, writeDestination, time.Now)
}

func runInspectionConditionOwned(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	return runInspectionConditionCommand(ctx, arguments, input, stdout, stderr, true, nil, writeDestination, time.Now)
}

func runInspectionConditionCommand(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer,
	borrowed bool, prepare inspectionInputPreparer, writeOutput destinationWriter, now func() time.Time) int {
	if nilRetestInputValue(stdout) || nilRetestInputValue(stderr) {
		return 1
	}
	flags := flag.NewFlagSet("inspect-exterior-condition", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	timeout := flags.Duration("timeout", inspectionCommandTimeout, "shared observation timeout, from 10ms to 45s")
	output := flags.String("output", "-", "new JSON output file, or - for stdout")
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *timeout < 10*time.Millisecond || *timeout > inspectionCommandTimeout {
		fmt.Fprintln(stderr, "inspect-exterior-condition requires a 10ms-45s shared timeout and no positional arguments")
		return 2
	}
	if nilRetestInputValue(ctx) || nilRetestInputValue(input) || writeOutput == nil || now == nil ||
		!borrowed && prepare == nil || borrowed && prepare != nil {
		fmt.Fprintln(stderr, "exterior condition input is unavailable")
		return 1
	}
	var result exteriorinspection.ResultV2
	var observationErr error
	if borrowed {
		if discardErr := discardSessionInput(ctx, input); discardErr != nil {
			if errors.Is(discardErr, errSessionInputCleanup) {
				fmt.Fprintln(stderr, "exterior condition input cleanup could not be confirmed")
				return workerCleanupUncertainExitCode
			}
			result, observationErr = exteriorinspection.EvaluateV2(nil, inspectionStop(discardErr, ctx.Err(), false), 0)
		} else {
			result, observationErr = observeInspectionCondition(ctx, input, stderr, *timeout, now)
		}
	} else if ctx.Err() != nil {
		result, observationErr = observeInspectionCondition(ctx, input, stderr, *timeout, now)
	} else {
		owned, source, finish, prepareErr := prepare(ctx, input)
		if finish == nil {
			fmt.Fprintln(stderr, "exterior condition input cleanup could not be confirmed")
			return workerCleanupUncertainExitCode
		}
		if nilRetestInputValue(owned) || nilRetestInputValue(source) {
			prepareErr = errors.Join(prepareErr, errSessionInput)
		}
		if prepareErr == nil {
			result, observationErr = observeInspectionCondition(owned, source, stderr, *timeout, now)
		}
		finishErr := finish()
		if errors.Is(prepareErr, errSessionInputCleanup) || errors.Is(finishErr, errSessionInputCleanup) {
			fmt.Fprintln(stderr, "exterior condition input cleanup could not be confirmed")
			return workerCleanupUncertainExitCode
		}
		if prepareErr != nil || finishErr != nil {
			fmt.Fprintln(stderr, "exterior condition input failed")
			return 1
		}
	}
	if observationErr != nil {
		fmt.Fprintln(stderr, "exterior condition returned invalid evidence")
		return 1
	}
	if err := writeOutput(*output, stdout, func(destination io.Writer) error {
		return json.NewEncoder(completeOutputWriter{destination}).Encode(result)
	}); err != nil {
		fmt.Fprintln(stderr, "write exterior condition result failed")
		return 1
	}
	if result.Status == "error" {
		return 1
	}
	return 0
}

func observeInspectionCondition(ctx context.Context, input io.Reader, promptWriter io.Writer, timeout time.Duration, now func() time.Time) (exteriorinspection.ResultV2, error) {
	if nilRetestInputValue(ctx) || nilRetestInputValue(input) || nilRetestInputValue(promptWriter) ||
		now == nil || timeout < 10*time.Millisecond || timeout > inspectionCommandTimeout {
		return exteriorinspection.ResultV2{}, exteriorinspection.ErrEvidence
	}
	if stopped := ctx.Err(); stopped != nil {
		return exteriorinspection.EvaluateV2(nil, inspectionStop(stopped, stopped, false), 0)
	}
	started := now()
	deadline := started.Add(timeout)
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline = parent
	}
	if !started.Before(deadline) {
		return exteriorinspection.EvaluateV2(nil, "deadline", 0)
	}
	readContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	prompt := "Visible exterior condition (operator reported, without moving or opening the device).\n" +
		"Enter one answer per line in this order. D: issue observed; N: no issue observed; I: unobserved.\n" +
		"1. Enclosure integrity (cracks, missing/broken pieces, deformation): D/N/I\n" +
		"2. Visible hinge and mount condition: D/N/I, or A only when no hinge exists. Hidden or unsafe-to-see hinges: I\n" +
		"3. Visible port exterior condition (openings and exposed parts, not function): D/N/I\n" +
		"4. Visible surface finish condition (scratches, scuffs, finish loss): D/N/I\n"
	responses, readErr := readInspectionConditionLines(readContext, input, func() error {
		_, err := io.WriteString(completeOutputWriter{promptWriter}, prompt)
		return err
	})
	decided := now()
	if decided.Before(started) {
		return exteriorinspection.ResultV2{}, exteriorinspection.ErrEvidence
	}
	if errors.Is(readErr, errInspectionConditionPrompt) {
		return exteriorinspection.EvaluateV2([]exteriorinspection.ConditionResponseV2{{}}, "prompt_error", inspectionObservation(started, decided, deadline))
	}
	if len(responses) > 4 {
		return exteriorinspection.ResultV2{}, exteriorinspection.ErrEvidence
	}
	for _, response := range responses {
		if len(response.Text) > maximumFindingLineBytes {
			return exteriorinspection.ResultV2{}, exteriorinspection.ErrEvidence
		}
	}
	reason := inspectionStop(readErr, readContext.Err(), !decided.Before(deadline))
	return exteriorinspection.EvaluateV2(responses, reason, inspectionObservation(started, decided, deadline))
}
