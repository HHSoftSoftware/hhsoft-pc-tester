package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

const pointerCommandTimeout = 90 * time.Second

type pointerCollector func(context.Context, inputtest.PointerOptions) inputtest.PointerResult

func runPointer(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runPointerWith(ctx, arguments, stdout, stderr, inputtest.RunPointer)
}

func runPointerWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect pointerCollector) int {
	flags := flag.NewFlagSet("pointer", flag.ContinueOnError)
	flags.SetOutput(stderr)
	duration := flags.Duration("duration", 30*time.Second, "time to observe movement, button and advertised wheel actions on every relative pointer")
	timeout := flags.Duration("timeout", pointerCommandTimeout, "whole-command deadline, from 10ms to 90s")
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *duration < 10*time.Millisecond || *duration > pointerCommandTimeout ||
		*timeout < 10*time.Millisecond || *timeout > pointerCommandTimeout || *duration > *timeout {
		fmt.Fprintln(stderr, "pointer requires a 10ms-90s observation duration no longer than its 10ms-90s command timeout, and no positional arguments")
		return 2
	}
	runContext, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	result := inputtest.PointerResult{Schema: inputtest.PointerSchema, LimitMilliseconds: duration.Milliseconds()}
	if commandStopReason(runContext) == nil {
		fmt.Fprintf(stderr, "For up to %s, move each relative pointer left, right, up, and down. Press and release its primary button and each advertised right or middle button. Scroll each advertised wheel axis in both directions. Every supported device must complete these actions.\n", *duration)
		result = collect(runContext, inputtest.PointerOptions{Root: "/", Duration: *duration})
	}
	result.MarkInterrupted(commandStopReason(runContext))
	return writeQuickResult(result, *outputPath, stdout, stderr, "pointer")
}
