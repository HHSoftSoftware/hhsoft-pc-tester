package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
)

func runCPUStress(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runCPUStressWithDestination(ctx, arguments, stdout, stderr, quick.RunCPUStress, writeDestination)
}

// The production path and tests share admission, strict evidence validation,
// and exclusive output. A zero exit code means a valid diagnostic was emitted.
func runCPUStressWithDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer,
	probe func(context.Context, quick.CPUStressOptions) (quick.CPUStressResult, error), writeOutput destinationWriter) int {
	flags := flag.NewFlagSet("cpu-stress", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workers := flags.Int("workers", 0, "workers; zero selects detected CPUs up to 64")
	output := flags.String("output", "-", "new JSON output file, or - for stdout")
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *workers < 0 || *workers > 64 || *output == "" {
		fmt.Fprintln(stderr, "cpu-stress: invalid arguments")
		return 2
	}
	if nilRetestInputValue(ctx) || probe == nil || writeOutput == nil {
		fmt.Fprintln(stderr, "cpu-stress: invalid context or dependencies")
		return 2
	}
	value, err := probe(ctx, quick.CPUStressOptions{Workers: *workers, Root: "/"})
	// Cleanup uncertainty takes precedence even when another error coexists.
	// An invalid-options result is not evidence and need not have safety fields.
	if errors.Is(err, quick.ErrCPUStressCleanupUnconfirmed) {
		fmt.Fprintln(stderr, "cpu-stress: cleanup was not confirmed")
		return workerCleanupUncertainExitCode
	}
	if errors.Is(err, quick.ErrCPUStressOptions) {
		fmt.Fprintln(stderr, "cpu-stress: invalid options")
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "cpu-stress: diagnostic accounting failed")
		return 1
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(encoded)+1 > 1<<20 {
		fmt.Fprintln(stderr, "cpu-stress: result encoding failed")
		return 1
	}
	if _, _, err = quickevidence.DecodeCPUStress(encoded); err != nil {
		if errors.Is(err, quick.ErrCPUStressCleanupUnconfirmed) {
			fmt.Fprintln(stderr, "cpu-stress: cleanup was not confirmed")
			return workerCleanupUncertainExitCode
		}
		fmt.Fprintln(stderr, "cpu-stress: invalid diagnostic evidence")
		return 1
	}
	if err := writeOutput(*output, stdout, func(destination io.Writer) error {
		return writeAll(destination, append(encoded, '\n'))
	}); err != nil {
		fmt.Fprintln(stderr, "cpu-stress: result output failed")
		return 1
	}
	return 0
}
