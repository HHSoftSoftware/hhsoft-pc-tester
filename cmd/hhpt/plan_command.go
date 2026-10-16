package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func runPlan(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	profile := flags.String("profile", string(plan.ProfileQuick), "quick, standard, extended, or destructive")
	architecture := flags.String("architecture", nativeArchitecture(), "architecture label")
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	keyPath := flags.String("fingerprint-key", "", "file containing exactly 32 random bytes")
	keyEpoch := flags.String("key-epoch", "local-1", "non-secret deployment key epoch label")
	fixtureComponents := flags.String("fixture-components", "", "internal file binding selected test-fixture component IDs")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *keyPath == "" {
		fmt.Fprintln(stderr, "plan requires -fingerprint-key; the hardware graph is never exported with an unkeyed digest")
		return 2
	}
	fingerprintKey, err := os.ReadFile(*keyPath)
	if err != nil {
		fmt.Fprintf(stderr, "read fingerprint key: %v\n", err)
		return 1
	}
	snapshot, err := inventory.NewLinuxCollector("/").Collect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "collect inventory: %v\n", err)
		return 1
	}
	if *fixtureComponents != "" {
		snapshot, err = excludeFixtureComponentsFromFile(snapshot, *fixtureComponents)
		if err != nil {
			fmt.Fprintf(stderr, "exclude deployment-key fixture: %v\n", err)
			return 1
		}
	}
	result, err := plan.Build(snapshot, plan.Profile(strings.ToLower(strings.TrimSpace(*profile))), *architecture, fingerprintKey, *keyEpoch)
	if err != nil {
		fmt.Fprintf(stderr, "build test plan: %v\n", err)
		return 1
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode test plan: %v\n", err)
		return 1
	}
	destination, closeDestination, err := openDestination(*outputPath, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "open output: %v\n", err)
		return 1
	}
	defer closeDestination()
	if _, err := destination.Write(append(encoded, '\n')); err != nil {
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
