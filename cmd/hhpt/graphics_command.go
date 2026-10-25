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

func runGraphics(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runGraphicsWith(ctx, arguments, stdout, stderr, graphics.Probe)
}

func runGraphicsWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, probe func(context.Context, string, int, graphics.Runner) graphics.Result) (code int) {
	flags := flag.NewFlagSet("graphics", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	frames := flags.Int("pattern-frames", 0, "show a KMS display pattern for up to 600 frames")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *frames < 0 || *frames > 600 {
		fmt.Fprintln(stderr, "graphics accepts -pattern-frames between 0 and 600 and no positional arguments")
		return 2
	}

	result := probe(ctx, "/", *frames, graphics.NewExecRunner("/run/hhpt", 20*time.Second, 64<<10))
	if result.CleanupUncertain {
		defer func() {
			fmt.Fprintln(stderr, "Worker cleanup could not be confirmed; stop the session. Diagnostic evidence remains in RAM when available.")
			code = workerCleanupUncertainExitCode
		}()
	}
	destination, closeDestination, err := openDestination(*outputPath, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "open output: %v\n", err)
		return 1
	}
	defer closeDestination()
	encoder := json.NewEncoder(destination)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(stderr, "write graphics result: %v\n", err)
		return 1
	}
	return 0
}
