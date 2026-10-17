package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
)

func runSafety(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runSafetyWith(ctx, arguments, stdout, stderr, safemode.Observe)
}

func runSafetyWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, observe func(context.Context) safemode.Result) int {
	flags := flag.NewFlagSet("safety", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "-", "new safety evidence file, or - for stdout")
	summary := flags.Bool("summary", false, "print measured safe counts after writing a separate evidence file")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || (*summary && *output == "-") {
		fmt.Fprintln(stderr, "safety accepts no targets; -summary requires a separate evidence output")
		return 2
	}
	result := observe(ctx)
	if code := writeQuickResult(result, *output, stdout, stderr, "runtime safety"); code != 0 {
		return code
	}
	if !result.Safe() {
		fmt.Fprintln(stderr, "runtime safety: unsafe or incomplete kernel observations; active tests blocked")
		return 1
	}
	if *summary {
		_, err := fmt.Fprintf(stdout, "writable_block_mounts=%d active_non_loop_interfaces=%d\n", result.WritableBlockMounts, result.ActiveNonLoopInterfaces)
		if err != nil {
			return 1
		}
	}
	return 0
}
