package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
)

const defaultAutomaticStart = 15 * time.Second

const deploymentKeySelectionExitCode = 10
const standardSelectionExitCode = 11

type interactionWaiter func(context.Context, time.Duration, io.Reader) (bool, error)

func runUI(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	return runUIWith(ctx, arguments, input, stdout, stderr, waitForOperatorInteraction)
}

func runUIWith(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer, wait interactionWaiter) int {
	return runUIWithSelectionOutput(ctx, arguments, input, stdout, stderr, wait, createUISelectionFile)
}

func runUIWithSelectionOutput(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer, wait interactionWaiter, create uiSelectionCreator) int {
	if nilRetestInputValue(ctx) || wait == nil {
		fmt.Fprintln(stderr, "ui input dependencies unavailable")
		return 1
	}
	flags := flag.NewFlagSet("ui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	automaticStart := flags.Duration("auto-start", defaultAutomaticStart, "begin the safe read-only profile after this inactivity period")
	legacyMode := flags.Bool("legacy", false, "show the reduced-coverage BIOS/CSM fallback label")
	deploymentKeyMedia := flags.Bool("deployment-key-media", false, "offer an explicitly selected read-only deployment-key token")
	selectionOutput := flags.String("selection-output", "", "new file recording the chosen safe quick tests")
	launchOutput := flags.String("launch-selection-output", "", "new file binding quick or standard profile and selected tests")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *automaticStart < time.Second || *automaticStart > 5*time.Minute {
		fmt.Fprintln(stderr, "ui requires -auto-start between 1s and 5m and accepts no positional arguments")
		return 2
	}

	selectionSet, launchSet := false, false
	flags.Visit(func(value *flag.Flag) {
		selectionSet = selectionSet || value.Name == "selection-output"
		launchSet = launchSet || value.Name == "launch-selection-output"
	})
	if selectionSet && launchSet {
		fmt.Fprintln(stderr, "ui selection output flags are mutually exclusive")
		return 2
	}
	path := *selectionOutput
	if launchSet {
		path = *launchOutput
	}
	var chosen *quickMenuSelection
	if path != "" || launchSet {
		architecture := nativeArchitecture()
		if *legacyMode {
			architecture = "legacy-x86"
		}
		var err error
		chosen, err = newQuickMenuSelection(path, architecture)
		if err != nil {
			fmt.Fprintf(stderr, "prepare test selection: %v\n", err)
			return 2
		}
		if create == nil {
			fmt.Fprintln(stderr, "ui output dependencies unavailable")
			return 1
		}
		chosen.launchSelection, chosen.create = launchSet, create
	}
	printSplash(stdout, *automaticStart, *legacyMode)
	interacted, err := wait(ctx, *automaticStart, input)
	if err != nil {
		if ctx.Err() != nil {
			return 1
		}
		fmt.Fprintf(stderr, "input monitoring unavailable: %v\n", err)
		fmt.Fprintln(stderr, "Automatic start cancelled; fail-safe operator menu enabled.")
		interacted = true
	}
	if ctx.Err() != nil {
		return 1
	}
	if !interacted {
		if err := chosen.write(ctx, plan.ProfileQuick); err != nil {
			fmt.Fprintf(stderr, "save test selection: %v; no test started\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "\nStarting automatic safe read-only profile.")
		return 0
	}

	fmt.Fprintln(stdout, "\nOperator activity detected. Automatic start cancelled.")
	return readMenu(ctx, input, stdout, stderr, *deploymentKeyMedia && !*legacyMode, chosen)
}

func printSplash(destination io.Writer, automaticStart time.Duration, legacyMode bool) {
	seconds := int(automaticStart.Round(time.Second) / time.Second)
	fmt.Fprintln(destination, "+----------------------------------------------------------+")
	fmt.Fprintf(destination, "| %-56s |\n", product.Name+" "+product.Version)
	if legacyMode {
		fmt.Fprintln(destination, "| LEGACY BIOS/CSM: reduced coverage / lower assurance     |")
	} else {
		fmt.Fprintln(destination, "| Safe boot: offline / read-only / no host-disk mounts      |")
	}
	fmt.Fprintln(destination, "+----------------------------------------------------------+")
	fmt.Fprintf(destination, "Automatic safe read-only quick profile starts after %d seconds of inactivity.\n", seconds)
	fmt.Fprintln(destination, "Press a key or move a mouse/touch pointer for the menu.")
}

func readMenu(ctx context.Context, input io.Reader, stdout, stderr io.Writer, deploymentKeyMedia bool, chosen *quickMenuSelection) int {
	reader := bufio.NewReaderSize(input, maximumMenuLineBytes+1)
	for {
		fmt.Fprintln(stdout, "\n[1 or Enter] Start safe read-only quick profile")
		if chosen != nil {
			chosen.summary(stdout)
			fmt.Fprintln(stdout, "[C]          Choose safe quick tests")
			if chosen.launchSelection {
				fmt.Fprintln(stdout, "[S]          Start standard profile with ephemeral identity")
				fmt.Fprintln(stdout, "             Includes a 600-second sustained CPU load; 15-minute cooperative session budget.")
			}
		}
		if deploymentKeyMedia {
			fmt.Fprintln(stdout, "[K]          Start with a stable deployment-key USB token")
		}
		fmt.Fprintln(stdout, "[W]          Wait at the operator menu")
		fmt.Fprint(stdout, "Selection: ")
		selection, err := readMenuLine(ctx, reader)
		if err != nil {
			if errors.Is(err, errMenuLineTooLong) {
				fmt.Fprintln(stderr, "Selection too long; no test started.")
				continue
			}
			if ctx.Err() != nil {
				return 1
			}
			fmt.Fprintf(stderr, "read menu selection: %v\n", err)
			return 1
		}
		switch strings.ToLower(strings.TrimSpace(selection)) {
		case "", "1":
			if err := chosen.write(ctx, plan.ProfileQuick); err != nil {
				fmt.Fprintf(stderr, "save test selection: %v; no test started\n", err)
				return 1
			}
			fmt.Fprintln(stdout, "Starting operator-selected safe read-only profile.")
			return 0
		case "s":
			if chosen == nil || !chosen.launchSelection {
				fmt.Fprintln(stderr, "Standard profile needs -launch-selection-output; no test started.")
				continue
			}
			if chosen.hasExclusions() {
				fmt.Fprintln(stderr, "Standard profile requires all defaults; use C then A to restore them. Choices unchanged; no test started.")
				continue
			}
			if err := chosen.write(ctx, plan.ProfileStandard); err != nil {
				fmt.Fprintf(stderr, "save test selection: %v; no test started\n", err)
				return 1
			}
			fmt.Fprintln(stdout, "Starting operator-selected standard profile with ephemeral identity.")
			return standardSelectionExitCode
		case "w":
			fmt.Fprintln(stdout, "Automatic start remains disabled.")
		case "k":
			if deploymentKeyMedia {
				if err := chosen.write(ctx, plan.ProfileQuick); err != nil {
					fmt.Fprintf(stderr, "save test selection: %v; no test started\n", err)
					return 1
				}
				fmt.Fprintln(stdout, "Stable deployment-key media selected.")
				return deploymentKeySelectionExitCode
			}
			fmt.Fprintln(stderr, "Unknown selection; no test started.")
		case "c":
			if chosen == nil {
				fmt.Fprintln(stderr, "Test selection needs -selection-output; no test started.")
				continue
			}
			if err := chosen.configure(ctx, reader, stdout, stderr); err != nil {
				fmt.Fprintf(stderr, "read test selection: %v; no test started\n", err)
				return 1
			}
		default:
			fmt.Fprintln(stderr, "Unknown selection; no test started.")
		}
	}
}
