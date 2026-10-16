package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
)

func runNetwork(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("network", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	allowActive := flags.Bool("allow-active", false, "accept administratively active interfaces without generating traffic")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "network accepts no positional arguments")
		return 2
	}
	result := networktest.Run(ctx, networktest.Options{Root: "/", AllowActive: *allowActive})
	return writeQuickResult(result, *outputPath, stdout, stderr, "network")
}
