package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

func runCPU(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runCPUWith(ctx, arguments, stdout, stderr, quick.RunCPU)
}

func runCPUWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, probe func(context.Context, quick.CPUOptions) quick.CPUResult) int {
	return runCPUWithDestination(ctx, arguments, stdout, stderr, probe, writeDestination)
}

func runCPUWithDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer, probe func(context.Context, quick.CPUOptions) quick.CPUResult, writeOutput destinationWriter) int {
	flags := flag.NewFlagSet("cpu", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	duration := flags.Duration("duration", 3*time.Second, "bounded computation duration")
	globalTimeout := flags.Duration("timeout", 20*time.Second, "global test deadline")
	workers := flags.Int("workers", 0, "parallel workers; zero uses detected CPUs up to 64")
	thermalLimit := flags.Int("thermal-limit-c", 90, "abort threshold in degrees Celsius")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *workers < 0 || *workers > 64 || *thermalLimit < 50 || *thermalLimit > 125 {
		fmt.Fprintln(stderr, "cpu accepts bounded duration/timeout, up to 64 workers, and a 50-125C limit")
		return 2
	}
	result := probe(ctx, quick.CPUOptions{
		Duration: *duration, GlobalTimeout: *globalTimeout, Workers: *workers,
		Root: "/", ThermalLimit: *thermalLimit * 1000,
	})
	code := writeQuickResultDestination(result, *outputPath, stdout, stderr, "CPU", writeOutput)
	if quickCleanupUnconfirmed(result.Safety) {
		fmt.Fprintln(stderr, "CPU thermal observer cleanup could not be confirmed; stop the session. Diagnostic evidence remains in RAM when available.")
		return workerCleanupUncertainExitCode
	}
	return code
}

func runMemory(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runMemoryWith(ctx, arguments, stdout, stderr, quick.RunMemory)
}

func runMemoryWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, probe func(context.Context, quick.MemoryOptions) quick.MemoryResult) int {
	return runMemoryWithDestination(ctx, arguments, stdout, stderr, probe, writeDestination)
}

func runMemoryWithDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer, probe func(context.Context, quick.MemoryOptions) quick.MemoryResult, writeOutput destinationWriter) int {
	flags := flag.NewFlagSet("memory", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	bytes := flags.Int64("bytes", 0, "working-set bytes; zero safely chooses up to 64 MiB")
	passes := flags.Int("passes", 2, "one to eight bounded pattern passes")
	globalTimeout := flags.Duration("timeout", 30*time.Second, "global test deadline")
	thermalLimit := flags.Int("thermal-limit-c", 90, "abort threshold in degrees Celsius")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *bytes < 0 || *passes < 1 || *passes > 8 || *thermalLimit < 50 || *thermalLimit > 125 {
		fmt.Fprintln(stderr, "memory accepts a bounded byte count, one to eight passes, and a 50-125C limit")
		return 2
	}
	result := probe(ctx, quick.MemoryOptions{
		Bytes: *bytes, Passes: *passes, GlobalTimeout: *globalTimeout,
		Root: "/", ThermalLimit: *thermalLimit * 1000,
	})
	code := writeQuickResultDestination(result, *outputPath, stdout, stderr, "memory", writeOutput)
	if quickCleanupUnconfirmed(result.Safety) {
		fmt.Fprintln(stderr, "Memory thermal observer cleanup could not be confirmed; stop the session. Diagnostic evidence remains in RAM when available.")
		return workerCleanupUncertainExitCode
	}
	return code
}

func quickCleanupUnconfirmed(safety quick.SafetyEvidence) bool {
	return !safety.CleanupConfirmed || safety.MonitorStatus == "cleanup_unconfirmed"
}

func writeQuickResult(result any, outputPath string, stdout, stderr io.Writer, name string) int {
	return writeQuickResultDestination(result, outputPath, stdout, stderr, name, writeDestination)
}

func writeQuickResultDestination(result any, outputPath string, stdout, stderr io.Writer, name string, writeOutput destinationWriter) int {
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode %s result: %v\n", name, err)
		return 1
	}
	if err := writeOutput(outputPath, stdout, func(destination io.Writer) error {
		_, err := destination.Write(append(encoded, '\n'))
		return err
	}); err != nil {
		fmt.Fprintf(stderr, "write %s result: %v\n", name, err)
		return 1
	}
	return 0
}
