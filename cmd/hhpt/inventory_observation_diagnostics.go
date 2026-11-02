package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

var errInventoryDiagnostics = errors.New("inventory: invalid observation diagnostics")
var errInventoryWarning = errors.New("inventory: observation warning could not be written")

const inventoryWarningLimit = 128

type inventoryDiagnosticCollector func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error)

func runInventoryWithDiagnostics(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect inventoryDiagnosticCollector) int {
	return runInventoryWithDiagnosticInput(ctx, arguments, stdout, stderr, collect, writeDestination, readRetestInput)
}

// The existing command still owns key admission, fixture exclusions, payload
// construction and destination closure. Inventory, plan, capture and both
// finalization routes consume the same-pass diagnostics companion.
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

func runFinalizeWithDiagnostics(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect inventoryDiagnosticCollector, random io.Reader, now func() time.Time) int {
	return runFinalizeWithDiagnosticInput(ctx, arguments, stdout, stderr, collect, random, now, nil, writeDestination, nil, readRetestInput, "").Code
}

func finalizeSessionWithDiagnostics(ctx context.Context, request sessionFinalization, stdout, stderr io.Writer, collect inventoryDiagnosticCollector, random io.Reader, now func() time.Time) sessionFinalizationResult {
	arguments := []string{"-plan", request.PlanPath, "-fingerprint-key", request.FingerprintKeyPath,
		"-key-epoch", request.KeyEpoch, "-evidence-dir", request.EvidenceDirectory, "-output", request.OutputPath}
	if request.FixtureComponents != "" {
		arguments = append(arguments, "-fixture-components", request.FixtureComponents)
	}
	for _, outcome := range request.Outcomes {
		arguments = append(arguments, "-outcome", outcome)
	}
	return runFinalizeWithDiagnosticInput(ctx, arguments, stdout, stderr, collect, random, now, request.Gate, writeDestination, request.Previous, readRetestInput, request.Profile)
}

// Final collection follows existing admission, evidence and worker cleanup.
// Copy without callbacks, then let the finalizer freeze its touchpad inputs
// before the warning can call a writer or consult the context again.
func runFinalizeWithDiagnosticInput(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect inventoryDiagnosticCollector, random io.Reader, now func() time.Time, gate sessionFinalGate, writeOutput destinationWriter, previous *verifiedPreviousReport, readInput retestInputReader, executionProfile plan.Profile) sessionFinalizationResult {
	if stderr == nil {
		return sessionFinalizationResult{Code: 1}
	}
	var diagnostics inventory.CollectionDiagnostics
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
		owned := copyFinalInventoryObservation(snapshot)
		diagnostics = observed
		return owned, nil
	}
	observe := func() error {
		return writeInventoryObservationWarning(stderr, diagnostics)
	}
	return runFinalizeOwnedDestinationWithPreviousForProfileAndObservation(ctx, arguments, stdout, stderr, snapshotOnly, random, now, gate, writeOutput, previous, readInput, executionProfile, observe)
}

// Preserve the supplied representation, including nil versus empty slices,
// component/attribute order, duplicate attributes and unnormalized values.
func copyFinalInventoryObservation(snapshot inventory.Snapshot) inventory.Snapshot {
	owned := snapshot
	if snapshot.Components != nil {
		owned.Components = make([]inventory.Component, len(snapshot.Components))
		copy(owned.Components, snapshot.Components)
		for index := range owned.Components {
			if snapshot.Components[index].Attributes != nil {
				owned.Components[index].Attributes = make([]inventory.Attribute, len(snapshot.Components[index].Attributes))
				copy(owned.Components[index].Attributes, snapshot.Components[index].Attributes)
			}
		}
	}
	return owned
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
