package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

var errInventoryDiagnostics = errors.New("inventory: invalid observation diagnostics")
var errInventoryWarning = errors.New("inventory: observation warning could not be written")

const inventoryWarningLimit = 128

type inventoryDiagnosticCollector func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error)

func runInventoryWithDiagnostics(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect inventoryDiagnosticCollector) int {
	return runInventoryWithDiagnosticInput(ctx, arguments, stdout, stderr, collect, writeDestination, readRetestInput)
}

// The existing command still owns key admission, fixture exclusions, payload
// construction and destination closure. Inventory, plan and direct capture
// consume the companion; finalization/session callers retain Collect.
func runInventoryWithDiagnosticInput(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect inventoryDiagnosticCollector, writeOutput destinationWriter, readInput retestInputReader) int {
	if stderr == nil {
		return 1
	}
	var diagnostics inventory.CollectionDiagnostics
	var snapshotOnly snapshotCollector
	if collect != nil {
		snapshotOnly = func(ctx context.Context) (inventory.Snapshot, error) {
			snapshot, observed, err := collect(ctx)
			if err != nil {
				return inventory.Snapshot{}, err
			}
			if !observed.Valid() {
				return inventory.Snapshot{}, errInventoryDiagnostics
			}
			diagnostics = observed
			return snapshot, nil
		}
	}
	var destination destinationWriter
	if writeOutput != nil {
		destination = func(path string, stdout io.Writer, write func(io.Writer) error) error {
			if err := writeInventoryObservationWarning(stderr, diagnostics); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return writeOutput(path, stdout, write)
		}
	}
	return runInventoryWithInput(ctx, arguments, stdout, stderr, snapshotOnly, destination, readInput)
}

func runPlanWithDiagnostics(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect inventoryDiagnosticCollector) int {
	return runPlanWithDiagnosticInput(ctx, arguments, stdout, stderr, collect, writeDestination, readRetestInput)
}

// Keep plan admission, graph construction and canonical output in the existing
// command. The warning is delivered before the destination can be acquired.
func runPlanWithDiagnosticInput(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect inventoryDiagnosticCollector, writeOutput destinationWriter, readInput retestInputReader) int {
	if stderr == nil {
		return 1
	}
	var diagnostics inventory.CollectionDiagnostics
	var snapshotOnly snapshotCollector
	if collect != nil {
		snapshotOnly = func(ctx context.Context) (inventory.Snapshot, error) {
			snapshot, observed, err := collect(ctx)
			if err != nil {
				return inventory.Snapshot{}, err
			}
			if !observed.Valid() {
				return inventory.Snapshot{}, errInventoryDiagnostics
			}
			diagnostics = observed
			return snapshot, nil
		}
	}
	var destination destinationWriter
	if writeOutput != nil {
		destination = func(path string, stdout io.Writer, write func(io.Writer) error) error {
			if err := writeInventoryObservationWarning(stderr, diagnostics); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return writeOutput(path, stdout, write)
		}
	}
	return runPlanWithInput(ctx, arguments, stdout, stderr, snapshotOnly, destination, readInput)
}

// Capture keeps key admission and optional session-ID entropy in the existing
// command. A successful warning must precede every worker and report effect.
func runCaptureWithDiagnosticDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect inventoryDiagnosticCollector, probeGraphics graphicsCollector, probeCPU cpuCollector, probeMemory memoryCollector, probeInput inputCollector, probeNetwork networkCollector, probeStorage storageCollector, probeUSB usbCollector, random io.Reader, now func() time.Time, writeOutput destinationWriter, readInput retestInputReader) int {
	if stderr == nil {
		return 1
	}
	snapshotOnly := func(ctx context.Context) (inventory.Snapshot, error) {
		if collect == nil {
			return inventory.Snapshot{}, errInventoryDiagnostics
		}
		snapshot, observed, err := collect(ctx)
		if err != nil {
			return inventory.Snapshot{}, err
		}
		if !observed.Valid() {
			return inventory.Snapshot{}, errInventoryDiagnostics
		}
		if err := ctx.Err(); err != nil {
			return inventory.Snapshot{}, err
		}
		if err := writeInventoryObservationWarning(stderr, observed); err != nil {
			return inventory.Snapshot{}, err
		}
		if err := ctx.Err(); err != nil {
			return inventory.Snapshot{}, err
		}
		return snapshot, nil
	}
	return runCaptureWithDestination(ctx, arguments, stdout, stderr, snapshotOnly, probeGraphics, probeCPU, probeMemory, probeInput, probeNetwork, probeStorage, probeUSB, random, now, writeOutput, readInput)
}

func writeInventoryObservationWarning(destination io.Writer, diagnostics inventory.CollectionDiagnostics) error {
	if !diagnostics.Valid() {
		return errInventoryDiagnostics
	}
	if !diagnostics.HasFailures() {
		return nil
	}
	if destination == nil {
		return errInventoryWarning
	}
	message := fmt.Sprintf("inventory: metadata reads failed (scalar=%d cpu=%d memory=%d input=%d)\n",
		diagnostics.ScalarFailures, diagnostics.CPUFailures, diagnostics.MemoryFailures, diagnostics.InputFailures)
	if len(message) > inventoryWarningLimit {
		return errInventoryWarning
	}
	n, err := io.WriteString(destination, message)
	if err != nil || n != len(message) {
		return errInventoryWarning
	}
	return nil
}
