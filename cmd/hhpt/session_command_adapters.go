package main

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"io"
	"time"
)

// No executable name or argument list is taken from plan JSON or console input.
func runSessionWorker(ctx context.Context, worker sessionWorker, input io.Reader, stdout, stderr io.Writer) int {
	arguments := []string{"-output", worker.OutputPath}
	switch worker.ID {
	case "cpu.stress":
		return runCPUStress(ctx, arguments, stdout, stderr)
	case "cpu.quick":
		return runCPU(ctx, append(arguments, "-duration", "3s", "-timeout", worker.Timeout.String(), "-thermal-limit-c", "90"), stdout, stderr)
	case "memory.quick":
		if worker.Architecture == "legacy-x86" {
			arguments = append(arguments, "-bytes", "16777216", "-passes", "1")
		} else {
			arguments = append(arguments, "-passes", "2")
		}
		return runMemory(ctx, append(arguments, "-timeout", worker.Timeout.String(), "-thermal-limit-c", "90"), stdout, stderr)
	case "audio.readiness.quick":
		return runAudio(ctx, arguments, stdout, stderr)
	case "input.readiness.quick":
		return runInput(ctx, append(arguments, "-duration", "0"), stdout, stderr)
	case "input.keyboard.quick":
		return runKeyboardOwned(ctx, append(arguments, "-duration", "30s", "-timeout", worker.Timeout.String()), input, stdout, stderr)
	case "input.pointer.quick":
		return runPointer(ctx, append(arguments, "-duration", "30s", "-timeout", worker.Timeout.String()), stdout, stderr)
	case touchpadExecutionID:
		return runTouchpad(ctx, append(arguments, "-duration", "30s", "-timeout", worker.Timeout.String()), stdout, stderr)
	case touchscreenExecutionID:
		return runTouchscreen(ctx, append(arguments, "-duration", "45s", "-timeout", worker.Timeout.String()), stdout, stderr)
	case "network.readiness.quick":
		return runNetwork(ctx, arguments, stdout, stderr)
	case "storage.readiness.quick":
		return runStorage(ctx, arguments, stdout, stderr)
	case "usb.readiness.quick":
		return runUSB(ctx, arguments, stdout, stderr)
	case "graphics.quick":
		return runGraphics(ctx, append(arguments, "-pattern-frames", "120", "-timeout", worker.Timeout.String()), stdout, stderr)
	case "inspection.exterior.quick":
		return runInspectionOwned(ctx, append(arguments, "-timeout", worker.Timeout.String()), input, stdout, stderr)
	case "display.operator.quick":
		return runDisplay(ctx, append(arguments, "-pattern-duration", "5s", "-finding-timeout", "15s", "-timeout", worker.Timeout.String()), input, stdout, stderr)
	default:
		fmt.Fprintln(stderr, "session refuses an unknown worker")
		return 2
	}
}

func finalizeSession(ctx context.Context, request sessionFinalization, stdout, stderr io.Writer) sessionFinalizationResult {
	collector := inventory.NewLinuxCollector("/")
	return finalizeSessionWith(ctx, request, stdout, stderr, collector.Collect, cryptorand.Reader, time.Now)
}

func finalizeSessionWith(ctx context.Context, request sessionFinalization, stdout, stderr io.Writer, collect snapshotCollector, random io.Reader, now func() time.Time) sessionFinalizationResult {
	arguments := []string{"-plan", request.PlanPath, "-fingerprint-key", request.FingerprintKeyPath,
		"-key-epoch", request.KeyEpoch, "-evidence-dir", request.EvidenceDirectory, "-output", request.OutputPath}
	if request.FixtureComponents != "" {
		arguments = append(arguments, "-fixture-components", request.FixtureComponents)
	}
	for _, outcome := range request.Outcomes {
		arguments = append(arguments, "-outcome", outcome)
	}
	return runFinalizeOwnedDestinationWithPreviousForProfile(ctx, arguments, stdout, stderr, collect, random, now, request.Gate, writeDestination, request.Previous, readRetestInput, request.Profile)
}
