package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/keyboardevidence"
)

const keyboardCommandTimeout = 90 * time.Second

type keyboardCollector func(context.Context, inputtest.KeyboardOptions) inputtest.KeyboardResult
type keyboardInputPreparer func(context.Context, io.Reader) (context.Context, io.Reader, func() error, error)

func runKeyboard(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	return runKeyboardWith(ctx, arguments, input, stdout, stderr, inputtest.RunKeyboard, prepareSessionInput)
}

// The session owns input for all workers; acquiring it again would introduce a
// second reader and a second terminal restoration owner.
func runKeyboardOwned(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	return runKeyboardWith(ctx, arguments, input, stdout, stderr, inputtest.RunKeyboard,
		func(ctx context.Context, input io.Reader) (context.Context, io.Reader, func() error, error) {
			return ctx, input, func() error { return nil }, nil
		})
}

func runKeyboardWith(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer,
	collect keyboardCollector, prepare keyboardInputPreparer) int {
	flags := flag.NewFlagSet("keyboard", flag.ContinueOnError)
	flags.SetOutput(stderr)
	duration := flags.Duration("duration", 30*time.Second, "time to observe six fixed press-and-release actions on every keyboard")
	timeout := flags.Duration("timeout", keyboardCommandTimeout, "whole-command deadline, from 10ms to 90s")
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *duration < 10*time.Millisecond || *duration > keyboardCommandTimeout ||
		*timeout < 10*time.Millisecond || *timeout > keyboardCommandTimeout || *duration > *timeout {
		fmt.Fprintln(stderr, "keyboard requires a 10ms-90s observation duration no longer than its 10ms-90s command timeout, and no positional arguments")
		return 2
	}
	runContext, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	result := inputtest.KeyboardResult{Schema: inputtest.KeyboardSchema, Status: inputtest.StatusError,
		StopReason: "error", LimitMilliseconds: duration.Milliseconds(), CleanupConfirmed: true,
		Devices: []inputtest.KeyboardDeviceResult{}}
	inputFailed := false
	if commandStopReason(runContext) == nil {
		owned, source, finish, err := prepare(runContext, input)
		if err != nil {
			if !errors.Is(err, errSessionInputCleanup) {
				fmt.Fprintln(stderr, "Keyboard console input could not be controlled.")
				return 1
			}
			result.CleanupConfirmed, result.StopReason = false, "cleanup_unconfirmed"
			result.Detail = "cleanup_unconfirmed"
			fmt.Fprintln(stderr, "Keyboard console ownership could not be confirmed.")
		} else {
			finished := false
			finishOnce := func() error {
				if finished {
					return nil
				}
				finished = true
				return finish()
			}
			defer finishOnce()
			fmt.Fprintf(stderr, "For up to %s, press and release left Shift, right Shift, Up, Down, Left, and Right on each keyboard. Do not type text. Ctrl-C cancels. Only these six controls are checked.\n", *duration)
			if commandStopReason(owned) == nil {
				result = collect(owned, inputtest.KeyboardOptions{Root: "/", Duration: *duration})
			}
			result.MarkInterrupted(commandStopReason(owned))
			if commandStopReason(owned) == nil {
				if err := discardSessionInput(owned, source); err != nil {
					if commandStopReason(owned) != nil {
						result.MarkInterrupted(commandStopReason(owned))
					} else {
						result.CleanupConfirmed, result.Status, result.StopReason = false, inputtest.StatusError, "cleanup_unconfirmed"
						result.Detail = "cleanup_unconfirmed"
						fmt.Fprintln(stderr, "Keyboard console handoff could not be confirmed.")
					}
				}
			}
			if err := finishOnce(); err != nil {
				inputFailed = true
				if !result.CleanupConfirmed || errors.Is(err, errSessionInputCleanup) {
					result.CleanupConfirmed, result.Status, result.StopReason = false, inputtest.StatusError, "cleanup_unconfirmed"
					result.Detail = "cleanup_unconfirmed"
					fmt.Fprintln(stderr, "Keyboard console cleanup could not be confirmed.")
				} else {
					result.Status, result.StopReason, result.Detail = inputtest.StatusError, "error", "console_unavailable"
				}
			}
			if !inputFailed {
				result.MarkInterrupted(commandStopReason(owned))
			}
		}
	}
	if !inputFailed {
		result.MarkInterrupted(commandStopReason(runContext))
	}
	validationErr := keyboardevidence.ValidateResult(result)
	if result.LimitMilliseconds != duration.Milliseconds() || validationErr != nil && !errors.Is(validationErr, keyboardevidence.ErrCleanupUnconfirmed) {
		fmt.Fprintln(stderr, "keyboard worker returned invalid evidence")
		if !result.CleanupConfirmed {
			return workerCleanupUncertainExitCode
		}
		return 1
	}
	code := writeQuickResult(result, *outputPath, stdout, stderr, "keyboard")
	if !result.CleanupConfirmed {
		fmt.Fprintln(stderr, "Keyboard cleanup could not be confirmed; stop the session. Diagnostic evidence remains in RAM when available.")
		return workerCleanupUncertainExitCode
	}
	if inputFailed {
		return 1
	}
	return code
}
