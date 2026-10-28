package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
)

// Appliance supervisors must stop the session on this code, even if saving
// the diagnostic result also fails. Cleanup uncertainty is not an ordinary
// inconclusive hardware observation.
const workerCleanupUncertainExitCode = 12

const graphicsCommandTimeout = 30 * time.Second

func runGraphics(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runGraphicsWith(ctx, arguments, stdout, stderr, graphics.Probe)
}

func runGraphicsWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, probe func(context.Context, string, int, graphics.Runner) graphics.Result) (code int) {
	return runGraphicsWithDestination(ctx, arguments, stdout, stderr, probe, writeDestination)
}

func runGraphicsWithDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer, probe func(context.Context, string, int, graphics.Runner) graphics.Result, writeOutput destinationWriter) (code int) {
	flags := flag.NewFlagSet("graphics", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	frames := flags.Int("pattern-frames", 0, "show a KMS display pattern for up to 600 frames")
	timeout := flags.Duration("timeout", graphicsCommandTimeout, "whole-command deadline, from 10ms to 30s")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *frames < 0 || *frames > 600 || *timeout < 10*time.Millisecond || *timeout > graphicsCommandTimeout {
		fmt.Fprintln(stderr, "graphics accepts 0-600 pattern frames, a 10ms-30s command timeout, and no positional arguments")
		return 2
	}
	runContext, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	result := graphics.Result{Schema: graphics.Schema}
	if commandStopReason(runContext) == nil {
		result = probe(runContext, "/", *frames, graphics.NewExecRunner("/run/hhpt", 20*time.Second, 64<<10))
	}
	result.MarkInterrupted(commandStopReason(runContext))
	if result.CleanupUncertain {
		defer func() {
			fmt.Fprintln(stderr, "Worker cleanup could not be confirmed; stop the session. Diagnostic evidence remains in RAM when available.")
			code = workerCleanupUncertainExitCode
		}()
	}
	if err := writeOutput(*outputPath, stdout, func(destination io.Writer) error {
		encoder := json.NewEncoder(destination)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}); err != nil {
		fmt.Fprintf(stderr, "write graphics result: %v\n", err)
		return 1
	}
	return 0
}
