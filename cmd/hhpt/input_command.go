package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

func runInput(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("input", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	duration := flags.Duration("duration", 0, "bounded activity observation; zero checks read-only event readiness")
	categories := flags.String("categories", "", "comma-separated requested categories; blank observes every present category")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *duration < 0 || *duration > 5*time.Minute {
		fmt.Fprintln(stderr, "input accepts -duration between 0 and 5m and no positional arguments")
		return 2
	}
	var requested []string
	if strings.TrimSpace(*categories) != "" {
		requested = strings.Split(*categories, ",")
	}
	result := inputtest.Run(ctx, inputtest.Options{Root: "/", Duration: *duration, Categories: requested})
	return writeQuickResult(result, *outputPath, stdout, stderr, "input")
}
