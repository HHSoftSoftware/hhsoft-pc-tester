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

func runGraphics(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
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

	result := graphics.Probe(ctx, "/", *frames, graphics.NewExecRunner("/run/hhpt", 20*time.Second, 64<<10))
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
