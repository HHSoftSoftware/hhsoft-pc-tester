package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func runPlan(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	collector := inventory.NewLinuxCollector("/")
	return runPlanWith(ctx, arguments, stdout, stderr, collector.Collect)
}

func runPlanWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector) int {
	return runPlanWithDestination(ctx, arguments, stdout, stderr, collect, writeDestination)
}

func runPlanWithDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, writeOutput destinationWriter) int {
	return runPlanWithInput(ctx, arguments, stdout, stderr, collect, writeOutput, readRetestInput)
}

func runPlanWithInput(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, writeOutput destinationWriter, readInput retestInputReader) int {
	return runPlanWithDocumentInput(ctx, arguments, stdout, stderr, collect, writeOutput, readInput, readSmallRegularFile)
}

func runPlanWithDocumentInput(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, writeOutput destinationWriter, readInput retestInputReader, readDocument smallRegularReader) int {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	profile := flags.String("profile", string(plan.ProfileQuick), "quick, standard, extended, or destructive")
	architecture := flags.String("architecture", nativeArchitecture(), "architecture label")
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	keyPath := flags.String("fingerprint-key", "", "file containing exactly 32 random bytes")
	keyEpoch := flags.String("key-epoch", "local-1", "non-secret deployment key epoch label")
	fixtureComponents := flags.String("fixture-components", "", "internal file binding selected test-fixture component IDs")
	selectionPath := flags.String("selection-file", "", "bounded JSON file listing safe quick-test exclusions")
	launchSelectionPath := flags.String("launch-selection-file", "", "checked JSON file binding the launch profile and architecture")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "plan accepts no positional arguments")
		return 2
	}
	if *keyPath == "" {
		fmt.Fprintln(stderr, "plan requires -fingerprint-key; the hardware graph is never exported with an unkeyed digest")
		return 2
	}
	selectionSet, launchSelectionSet := false, false
	flags.Visit(func(value *flag.Flag) {
		if value.Name == "selection-file" {
			selectionSet = true
		}
		if value.Name == "launch-selection-file" {
			launchSelectionSet = true
		}
	})
	if selectionSet && launchSelectionSet {
		fmt.Fprintln(stderr, "plan selection modes are mutually exclusive")
		return 2
	}
	if launchSelectionSet && *launchSelectionPath == "" {
		fmt.Fprintln(stderr, "plan requires a nonempty -launch-selection-file")
		return 2
	}
	var excludedTests []string
	if *selectionPath != "" {
		if readDocument == nil {
			fmt.Fprintln(stderr, "read quick-test selection: input acquisition failed")
			return 1
		}
		encoded, err := readDocument(*selectionPath, plan.MaximumSelectionBytes)
		if err != nil {
			fmt.Fprintf(stderr, "read quick-test selection: %v\n", err)
			return 1
		}
		excludedTests, err = plan.DecodeSelection(encoded, strings.TrimSpace(*architecture))
		if err != nil {
			fmt.Fprintf(stderr, "validate quick-test selection: %v\n", err)
			return 1
		}
	}
	if nilRetestInputValue(ctx) || readInput == nil || collect == nil || writeOutput == nil || ctx.Err() != nil {
		fmt.Fprintln(stderr, "plan input acquisition failed")
		return 1
	}
	if *launchSelectionPath != "" {
		encoded, err := readInput(ctx, *launchSelectionPath, retestLaunchSelectionInput)
		if err != nil {
			fmt.Fprintln(stderr, "read launch selection: input acquisition failed")
			return 1
		}
		excludedTests, err = plan.DecodeLaunchSelection(encoded, plan.Profile(strings.ToLower(strings.TrimSpace(*profile))), strings.TrimSpace(*architecture))
		if err != nil {
			fmt.Fprintln(stderr, "read launch selection: input acquisition failed")
			return 1
		}
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "plan input acquisition failed")
			return 1
		}
	}
	fingerprintKey, err := readInput(ctx, *keyPath, retestFingerprintKeyInput)
	if err != nil || len(fingerprintKey) != 32 {
		fmt.Fprintln(stderr, "read fingerprint key: input acquisition failed")
		return 1
	}
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "plan input acquisition failed")
		return 1
	}
	snapshot, err := collect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "collect inventory: %v\n", err)
		return 1
	}
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "plan input acquisition failed")
		return 1
	}
	if *fixtureComponents != "" {
		snapshot, err = excludeFixtureComponentsFromFile(snapshot, *fixtureComponents)
		if err != nil {
			fmt.Fprintf(stderr, "exclude deployment-key fixture: %v\n", err)
			return 1
		}
	}
	result, err := plan.BuildWithSelection(snapshot, plan.Profile(strings.ToLower(strings.TrimSpace(*profile))), *architecture, fingerprintKey, *keyEpoch, excludedTests)
	if err != nil {
		fmt.Fprintf(stderr, "build test plan: %v\n", err)
		return 1
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode test plan: %v\n", err)
		return 1
	}
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "plan input acquisition failed")
		return 1
	}
	if err := writeOutput(*outputPath, stdout, func(destination io.Writer) error {
		_, err := destination.Write(append(encoded, '\n'))
		return err
	}); err != nil {
		fmt.Fprintf(stderr, "write test plan: %v\n", err)
		return 1
	}
	return 0
}

func nativeArchitecture() string {
	switch runtime.GOARCH {
	case "386":
		return "x86"
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return runtime.GOARCH
	}
}
