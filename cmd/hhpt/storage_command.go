package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
)

func runStorage(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("storage", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "storage accepts no positional arguments")
		return 2
	}
	return writeQuickResult(storage.Run(ctx, storage.Options{Root: "/"}), *outputPath, stdout, stderr, "storage")
}
