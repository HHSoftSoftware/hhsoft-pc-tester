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
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchpadevidence"
)

const touchpadCommandTimeout = 90 * time.Second
const touchpadCommandDeviceLimit = 128

const touchpadCommandGuide = "Touchpad check: lift all fingers, then use one finger to move left/right and up/down repeatedly. Lift to finish the contact, and press and release the primary button. Repeat the motions if needed: unchanged axes may delay a fresh baseline. Keep to one contact; multitouch and gestures are unsupported.\n"

type touchpadCollector func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult

func runTouchpad(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runTouchpadWith(ctx, arguments, stdout, stderr, inputtest.RunTouchpad)
}

func runTouchpadWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect touchpadCollector) int {
	return runTouchpadWithDestination(ctx, arguments, stdout, stderr, collect, writeDestination)
}

// This command owns no console reader and creates no process. The collector
// owns device cleanup; the existing destination helper owns any output file.
func runTouchpadWithDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect touchpadCollector, writeOutput destinationWriter) int {
	if nilRetestInputValue(ctx) || nilRetestInputValue(stdout) || nilRetestInputValue(stderr) ||
		collect == nil || writeOutput == nil {
		return 2
	}
	flags := flag.NewFlagSet("touchpad", flag.ContinueOnError)
	// Flag errors can echo arbitrary caller strings. Only finite diagnostics
	// leave this command; paths, device names and underlying errors do not.
	flags.SetOutput(io.Discard)
	duration := flags.Duration("duration", 30*time.Second, "bounded observation duration")
	timeout := flags.Duration("timeout", touchpadCommandTimeout, "global test deadline")
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	if flags.Parse(arguments) != nil || flags.NArg() != 0 ||
		*duration < 10*time.Millisecond || *duration > touchpadCommandTimeout ||
		*timeout < 10*time.Millisecond || *timeout > touchpadCommandTimeout || *duration > *timeout ||
		*outputPath == "" {
		fmt.Fprintln(stderr, "touchpad accepts only duration, timeout and output; duration and timeout must be 10ms-90s with duration <= timeout")
		return 2
	}

	runContext, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	result := inputtest.TouchpadResult{
		Schema: inputtest.TouchpadSchema, Status: inputtest.StatusInconclusive,
		StopReason: "error", Detail: "incomplete_actions",
		LimitMilliseconds: duration.Milliseconds(), CleanupConfirmed: true,
		Devices: []inputtest.TouchpadDeviceResult{},
	}
	stopped := commandStopReason(runContext)
	if stopped == nil {
		if writeAll(stderr, []byte(touchpadCommandGuide)) != nil {
			return 1 // No device acquisition after a failed instruction write.
		}
		stopped = commandStopReason(runContext)
	}
	if stopped == nil {
		result = collect(runContext, inputtest.TouchpadOptions{Root: "/", Duration: *duration})
	} else {
		result.MarkInterrupted(stopped)
	}

	// Cleanup uncertainty survives malformed claims. An oversized row set
	// cannot establish cleanup with a bounded inspection.
	cleanupUnconfirmed := touchpadCommandCleanupUnconfirmed(result)
	finishCode := func(code int) int {
		if cleanupUnconfirmed {
			fmt.Fprintln(stderr, "touchpad cleanup could not be confirmed; stop the session")
			return workerCleanupUncertainExitCode
		}
		return code
	}
	validationErr := touchpadevidence.ValidateResult(result)
	if result.LimitMilliseconds != duration.Milliseconds() ||
		validationErr != nil && !errors.Is(validationErr, touchpadevidence.ErrCleanupUnconfirmed) {
		fmt.Fprintln(stderr, "touchpad collector returned invalid evidence")
		return finishCode(1)
	}

	// Freeze callback-owned nested storage before another context or output
	// callback. This is a bounded structural copy, not proof of kernel history.
	raw, err := json.Marshal(result)
	if err != nil {
		fmt.Fprintln(stderr, "touchpad result could not be encoded")
		return finishCode(1)
	}
	frozen, err := touchpadevidence.Decode(raw)
	if err != nil && !errors.Is(err, touchpadevidence.ErrCleanupUnconfirmed) {
		fmt.Fprintln(stderr, "touchpad collector returned invalid evidence")
		return finishCode(1)
	}
	publicationStartStop := commandStopReason(runContext)
	frozen.MarkInterrupted(publicationStartStop)
	err = touchpadevidence.ValidateResult(frozen)
	if err != nil && !errors.Is(err, touchpadevidence.ErrCleanupUnconfirmed) {
		fmt.Fprintln(stderr, "touchpad interrupted evidence is invalid")
		return finishCode(1)
	}
	encoded, err := json.MarshalIndent(frozen, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "touchpad result could not be encoded")
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
		fmt.Fprintln(stderr, "touchpad result output failed")
		return finishCode(1)
	}
	// Publication cannot extend the observation deadline. Bytes already written
	// are preserved, but a late cancellation cannot return command success.
	if publicationStartStop == nil && commandStopReason(runContext) != nil {
		fmt.Fprintln(stderr, "touchpad result publication was interrupted")
		return finishCode(1)
	}
	return finishCode(0)
}

func touchpadCommandCleanupUnconfirmed(result inputtest.TouchpadResult) bool {
	if !result.CleanupConfirmed || len(result.Devices) > touchpadCommandDeviceLimit {
		return true
	}
	for _, device := range result.Devices {
		if !device.CleanupConfirmed {
			return true
		}
	}
	return false
}
