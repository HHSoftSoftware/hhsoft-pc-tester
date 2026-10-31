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

func runMemoryStress(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runMemoryStressWithDestination(ctx, arguments, stdout, stderr, quick.RunMemoryStress, writeDestination)
}

// Zero means a valid diagnostic was emitted, not a passing memory observation.
func runMemoryStressWithDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer,
	probe func(context.Context, quick.MemoryStressOptions) (quick.MemoryStressResult, error), writeOutput destinationWriter) int {
	flags := flag.NewFlagSet("memory-stress", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	size := flags.Int64("bytes", 0, "working-set bytes; zero selects at most 64MiB and one quarter of freshly available memory")
	output := flags.String("output", "-", "new JSON output file, or - for stdout")
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *size < 0 ||
		*size != 0 && (*size < 4<<20 || *size > 64<<20) || *output == "" {
		fmt.Fprintln(stderr, "memory-stress: invalid arguments")
		return 2
	}
	if nilRetestInputValue(ctx) || probe == nil || writeOutput == nil {
		fmt.Fprintln(stderr, "memory-stress: invalid context or dependencies")
		return 2
	}
	value, err := probe(ctx, quick.MemoryStressOptions{Bytes: *size, Root: "/"})
	if errors.Is(err, quick.ErrMemoryStressCleanupUnconfirmed) {
		fmt.Fprintln(stderr, "memory-stress: cleanup was not confirmed")
		return workerCleanupUncertainExitCode
	}
	if errors.Is(err, quick.ErrMemoryStressOptions) {
		fmt.Fprintln(stderr, "memory-stress: invalid options")
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "memory-stress: diagnostic accounting failed")
		return 1
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(encoded)+1 > 1<<20 {
		fmt.Fprintln(stderr, "memory-stress: result encoding failed")
		return 1
	}
	if _, _, err = quickevidence.DecodeMemoryStress(encoded); err != nil {
		if errors.Is(err, quick.ErrMemoryStressCleanupUnconfirmed) {
			fmt.Fprintln(stderr, "memory-stress: cleanup was not confirmed")
			return workerCleanupUncertainExitCode
		}
		fmt.Fprintln(stderr, "memory-stress: invalid diagnostic evidence")
		return 1
	}
	if err := writeOutput(*output, stdout, func(destination io.Writer) error { return writeAll(destination, append(encoded, '\n')) }); err != nil {
		fmt.Fprintln(stderr, "memory-stress: result output failed")
		return 1
	}
	return 0
}
