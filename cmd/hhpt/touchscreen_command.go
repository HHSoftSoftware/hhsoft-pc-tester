package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchscreenevidence"
)

const touchscreenCommandTimeout = 120 * time.Second
const touchscreenCommandDeviceLimit = 128

const touchscreenCommandGuide = "Touchscreen check: lift all contacts, then touch with one finger and move left/right and up/down repeatedly. Lift to finish the contact. Repeat the motions if needed: unchanged axes may delay a fresh baseline. Only a single DIRECT contact is supported; multitouch, stylus and pressure extensions are unsupported.\n"

type touchscreenCollector func(context.Context, inputtest.TouchscreenOptions) inputtest.TouchscreenResult

func runTouchscreen(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runTouchscreenWith(ctx, arguments, stdout, stderr, inputtest.RunTouchscreen)
}

func runTouchscreenWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect touchscreenCollector) int {
	return runTouchscreenWithDestination(ctx, arguments, stdout, stderr, collect, writeDestination)
}

// This command owns no console reader and creates no process. The collector
// owns device cleanup; the existing destination helper owns any output file.
func runTouchscreenWithDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect touchscreenCollector, writeOutput destinationWriter) int {
	if nilRetestInputValue(ctx) || nilRetestInputValue(stdout) || nilRetestInputValue(stderr) ||
		collect == nil || writeOutput == nil {
		return 2
	}
	flags := flag.NewFlagSet("touchscreen", flag.ContinueOnError)
	// Flag errors can echo arbitrary caller strings. Only finite diagnostics
	// leave this command; paths, device names and underlying errors do not.
	flags.SetOutput(io.Discard)
	duration := flags.Duration("duration", 45*time.Second, "bounded observation duration")
	timeout := flags.Duration("timeout", touchscreenCommandTimeout, "global test deadline")
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	if flags.Parse(arguments) != nil || flags.NArg() != 0 ||
		*duration < 10*time.Millisecond || *duration > touchscreenCommandTimeout ||
		*timeout < 10*time.Millisecond || *timeout > touchscreenCommandTimeout || *duration > *timeout ||
		*outputPath == "" {
		fmt.Fprintln(stderr, "touchscreen accepts only duration, timeout and output; duration and timeout must be 10ms-120s with duration <= timeout")
		return 2
	}

	runContext, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	result := inputtest.TouchscreenResult{
		Schema: inputtest.TouchscreenSchema, Status: inputtest.StatusInconclusive,
		StopReason: "error", Detail: "incomplete_actions",
		LimitMilliseconds: duration.Milliseconds(), CleanupConfirmed: true,
		Devices: []inputtest.TouchscreenDeviceResult{},
	}
	stopped := commandStopReason(runContext)
	if stopped == nil {
		if writeAll(stderr, []byte(touchscreenCommandGuide)) != nil {
			return 1 // No device acquisition after a failed instruction write.
		}
		stopped = commandStopReason(runContext)
	}
	if stopped == nil {
		result = collect(runContext, inputtest.TouchscreenOptions{Root: "/", Duration: *duration})
	} else {
		result.MarkInterrupted(stopped)
	}

	// Cleanup uncertainty survives malformed claims. An oversized row set
	// cannot establish cleanup with a bounded inspection.
	cleanupUnconfirmed := touchscreenCommandCleanupUnconfirmed(result)
	finishCode := func(code int) int {
		if cleanupUnconfirmed {
			fmt.Fprintln(stderr, "touchscreen cleanup could not be confirmed; stop the session")
			return workerCleanupUncertainExitCode
		}
		return code
	}
	validationErr := touchscreenevidence.ValidateResult(result)
	if result.LimitMilliseconds != duration.Milliseconds() ||
		validationErr != nil && !errors.Is(validationErr, touchscreenevidence.ErrCleanupUnconfirmed) {
		fmt.Fprintln(stderr, "touchscreen collector returned invalid evidence")
		return finishCode(1)
	}

	// Freeze callback-owned nested storage before another context or output
	// callback. This is a bounded structural copy, not proof of kernel history.
	raw, err := json.Marshal(result)
	if err != nil {
		fmt.Fprintln(stderr, "touchscreen result could not be encoded")
		return finishCode(1)
	}
	frozen, err := touchscreenevidence.Decode(raw)
	if err != nil && !errors.Is(err, touchscreenevidence.ErrCleanupUnconfirmed) {
		fmt.Fprintln(stderr, "touchscreen collector returned invalid evidence")
		return finishCode(1)
	}
	publicationStartStop := commandStopReason(runContext)
	frozen.MarkInterrupted(publicationStartStop)
	err = touchscreenevidence.ValidateResult(frozen)
	if err != nil && !errors.Is(err, touchscreenevidence.ErrCleanupUnconfirmed) {
		fmt.Fprintln(stderr, "touchscreen interrupted evidence is invalid")
		return finishCode(1)
	}
	encoded, err := json.MarshalIndent(frozen, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "touchscreen result could not be encoded")
		return finishCode(1)
	}
	encoded = append(encoded, '\n')
	calls := 0
	var callbackErr error
	outputErr := writeOutput(*outputPath, stdout, func(destination io.Writer) error {
		calls++
		if calls != 1 || nilRetestInputValue(destination) {
			callbackErr = io.ErrClosedPipe
			return callbackErr
		}
		callbackErr = writeAll(destination, encoded)
		return callbackErr
	})
	if outputErr != nil || callbackErr != nil || calls != 1 {
		fmt.Fprintln(stderr, "touchscreen result output failed")
		return finishCode(1)
	}
	// Publication cannot extend the observation deadline. Bytes already written
	// are preserved, but a late cancellation cannot return command success.
	if publicationStartStop == nil && commandStopReason(runContext) != nil {
		fmt.Fprintln(stderr, "touchscreen result publication was interrupted")
		return finishCode(1)
	}
	return finishCode(0)
}

func touchscreenCommandCleanupUnconfirmed(result inputtest.TouchscreenResult) bool {
	if !result.CleanupConfirmed || len(result.Devices) > touchscreenCommandDeviceLimit {
		return true
	}
	for _, device := range result.Devices {
		if !device.CleanupConfirmed {
			return true
		}
	}
	return false
}
