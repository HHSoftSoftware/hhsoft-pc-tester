package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, arguments []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		usage(stderr)
		return 2
	}
	switch arguments[0] {
	case "_mount-report":
		return runInternalReportMount(ctx, arguments[1:], stdin, stderr)
	case "audio":
		return runAudio(ctx, arguments[1:], stdout, stderr)
	case "capture":
		return runCapture(ctx, arguments[1:], stdout, stderr)
	case "compare":
		return runCompare(arguments[1:], stdout, stderr)
	case "cpu":
		return runCPU(ctx, arguments[1:], stdout, stderr)
	case "deployment-key":
		return runDeploymentKey(ctx, arguments[1:], stdin, stdout, stderr)
	case "display":
		return runDisplay(ctx, arguments[1:], stdin, stdout, stderr)
	case "graphics":
		return runGraphics(ctx, arguments[1:], stdout, stderr)
	case "finalize":
		return runFinalize(ctx, arguments[1:], stdout, stderr)
	case "input":
		return runInput(ctx, arguments[1:], stdout, stderr)
	case "inventory":
		return runInventory(ctx, arguments[1:], stdout, stderr)
	case "keyboard":
		return runKeyboard(ctx, arguments[1:], stdin, stdout, stderr)
	case "memory":
		return runMemory(ctx, arguments[1:], stdout, stderr)
	case "network":
		return runNetwork(ctx, arguments[1:], stdout, stderr)
	case "persist":
		return runPersist(ctx, arguments[1:], stdout, stderr)
	case "plan":
		return runPlan(ctx, arguments[1:], stdout, stderr)
	case "pointer":
		return runPointer(ctx, arguments[1:], stdout, stderr)
	case "report-target":
		return runReportTarget(ctx, arguments[1:], stdout, stderr)
	case "save":
		return runSave(ctx, arguments[1:], stdin, stdout, stderr)
	case "safety":
		return runSafety(ctx, arguments[1:], stdout, stderr)
	case "session":
		return runSession(ctx, arguments[1:], stdin, stdout, stderr)
	case "storage":
		return runStorage(ctx, arguments[1:], stdout, stderr)
	case "usb":
		return runUSB(ctx, arguments[1:], stdout, stderr)
	case "ui":
		return runUI(ctx, arguments[1:], stdin, stdout, stderr)
	case "verify":
		return runVerify(ctx, arguments[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, product.Name, product.Version)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", arguments[0])
		usage(stderr)
		return 2
	}
}

func runInventory(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runInventoryWith(ctx, arguments, stdout, stderr, inventory.NewLinuxCollector("/").Collect)
}

func runInventoryWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector) int {
	return runInventoryWithDestination(ctx, arguments, stdout, stderr, collect, writeDestination)
}

func runInventoryWithDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, writeOutput destinationWriter) int {
	flags := flag.NewFlagSet("inventory", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	keyPath := flags.String("fingerprint-key", "", "file containing exactly 32 random bytes")
	keyEpoch := flags.String("key-epoch", "local-1", "non-secret deployment key epoch label")
	fixtureComponents := flags.String("fixture-components", "", "internal file binding selected test-fixture component IDs")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *keyPath == "" {
		fmt.Fprintln(stderr, "inventory requires -fingerprint-key; portable unkeyed component IDs are intentionally not exported")
		return 2
	}
	fingerprintKey, err := os.ReadFile(*keyPath)
	if err != nil {
		fmt.Fprintf(stderr, "read fingerprint key: %v\n", err)
		return 1
	}
	snapshot, err := collect(ctx)
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
	payload, err := app.BuildInventoryPayload(snapshot, fingerprintKey, *keyEpoch)
	if err != nil {
		fmt.Fprintf(stderr, "build inventory payload: %v\n", err)
		return 1
	}
	if err := writeOutput(*outputPath, stdout, func(destination io.Writer) error {
		encoder := json.NewEncoder(destination)
		encoder.SetIndent("", "  ")
		return encoder.Encode(payload)
	}); err != nil {
		fmt.Fprintf(stderr, "write inventory: %v\n", err)
		return 1
	}
	return 0
}

func usage(destination io.Writer) {
	fmt.Fprintln(destination, "usage: hhpt audio [-output FILE]")
	fmt.Fprintln(destination, "       hhpt capture -fingerprint-key FILE -signing-key FILE -key-id ID -output REPORT.hhpcr [-key-epoch LABEL] [-session-id ID] [-cpu-duration 3s] [-memory-bytes N] [-graphics-pattern-frames N]")
	fmt.Fprintln(destination, "       hhpt compare (-self-issued | -public-key FILE -key-id ID [-after-public-key FILE -after-key-id ID]) [-output FILE] BEFORE.hhpcr AFTER.hhpcr")
	fmt.Fprintln(destination, "       hhpt finalize -fingerprint-key FILE -plan PLAN.json -outcome TEST=STATUS... [-evidence-dir DIR] -output REPORT.hhpcr")
	fmt.Fprintln(destination, "usage: hhpt inventory -fingerprint-key FILE [-key-epoch LABEL] [-output FILE]")
	fmt.Fprintln(destination, "       hhpt plan -fingerprint-key FILE [-key-epoch LABEL] [-profile quick|standard|extended|destructive] [-selection-file FILE] [-output FILE]")
	fmt.Fprintln(destination, "       hhpt cpu [-duration 3s] [-timeout 20s] [-workers N] [-thermal-limit-c 90] [-output FILE]")
	fmt.Fprintln(destination, "       hhpt deployment-key init -epoch LABEL -output PATH/hhpt-deployment-key.v1")
	fmt.Fprintln(destination, "       hhpt deployment-key inspect PATH/hhpt-deployment-key.v1")
	fmt.Fprintln(destination, "       hhpt deployment-key load-media -discovery-key FILE")
	fmt.Fprintln(destination, "       hhpt display [-pattern-duration 5s] [-finding-timeout 15s] [-output FILE]")
	fmt.Fprintln(destination, "       hhpt memory [-bytes N] [-passes 2] [-timeout 30s] [-thermal-limit-c 90] [-output FILE]")
	fmt.Fprintln(destination, "       hhpt input [-duration 0] [-categories LIST] [-output FILE]")
	fmt.Fprintln(destination, "       hhpt keyboard [-duration 30s] [-timeout 90s] [-output FILE]")
	fmt.Fprintln(destination, "       hhpt pointer [-duration 30s] [-timeout 90s] [-output FILE]")
	fmt.Fprintln(destination, "       hhpt network [-allow-active] [-output FILE]")
	fmt.Fprintln(destination, "       hhpt report-target -fingerprint-key FILE [-device /dev/DEVICE -confirm CODE] [-output FILE]")
	fmt.Fprintln(destination, "       hhpt persist -authorization FILE -fingerprint-key FILE (-self-issued | -public-key FILE -key-id ID) [-name NAME.hhpcr] REPORT.hhpcr")
	fmt.Fprintln(destination, "       hhpt graphics [-pattern-frames N] [-output FILE]")
	fmt.Fprintln(destination, "       hhpt storage [-output FILE]")
	fmt.Fprintln(destination, "       hhpt safety [-output FILE] [-summary]")
	fmt.Fprintln(destination, "       hhpt session -fingerprint-key FILE -plan PLAN.json [-evidence-dir DIR] [-timeout 5m] -output REPORT.hhpcr")
	fmt.Fprintln(destination, "       hhpt usb [-output FILE]")
	fmt.Fprintln(destination, "       hhpt save -fingerprint-key FILE REPORT.hhpcr")
	fmt.Fprintln(destination, "       hhpt verify (-self-issued | -public-key FILE -key-id ID) REPORT.hhpcr")
	fmt.Fprintln(destination, "       hhpt ui [-auto-start 15s] [-legacy] [-deployment-key-media] [-selection-output FILE]")
	fmt.Fprintln(destination, "       session/finalize/verify: optional -previous-report FILE with either -previous-self-issued or both -previous-public-key FILE -previous-key-id ID")
	fmt.Fprintln(destination, "       hhpt version")
}
