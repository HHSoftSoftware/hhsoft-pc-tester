package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
)

const defaultAutomaticStart = 15 * time.Second

const deploymentKeySelectionExitCode = 10

type interactionWaiter func(context.Context, time.Duration, io.Reader) (bool, error)

func runUI(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	return runUIWith(ctx, arguments, input, stdout, stderr, waitForOperatorInteraction)
}

func runUIWith(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer, wait interactionWaiter) int {
	flags := flag.NewFlagSet("ui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	automaticStart := flags.Duration("auto-start", defaultAutomaticStart, "begin the safe read-only profile after this inactivity period")
	legacyMode := flags.Bool("legacy", false, "show the reduced-coverage BIOS/CSM fallback label")
	deploymentKeyMedia := flags.Bool("deployment-key-media", false, "offer an explicitly selected read-only deployment-key token")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *automaticStart < time.Second || *automaticStart > 5*time.Minute {
		fmt.Fprintln(stderr, "ui requires -auto-start between 1s and 5m and accepts no positional arguments")
		return 2
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
	if !interacted {
		fmt.Fprintln(stdout, "\nStarting automatic safe read-only profile.")
		return 0
	}

	fmt.Fprintln(stdout, "\nOperator activity detected. Automatic start cancelled.")
	return readMenu(ctx, input, stdout, stderr, *deploymentKeyMedia && !*legacyMode)
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

func readMenu(ctx context.Context, input io.Reader, stdout, stderr io.Writer, deploymentKeyMedia bool) int {
	reader := bufio.NewReader(input)
	for {
		fmt.Fprintln(stdout, "\n[1 or Enter] Start safe read-only quick profile")
		if deploymentKeyMedia {
			fmt.Fprintln(stdout, "[K]          Start with a stable deployment-key USB token")
		}
		fmt.Fprintln(stdout, "[W]          Wait at the operator menu")
		fmt.Fprint(stdout, "Selection: ")
		selection, err := reader.ReadString('\n')
		if err != nil {
			if ctx.Err() != nil {
				return 1
			}
			fmt.Fprintf(stderr, "read menu selection: %v\n", err)
			return 1
		}
		switch strings.ToLower(strings.TrimSpace(selection)) {
		case "", "1":
			fmt.Fprintln(stdout, "Starting operator-selected safe read-only profile.")
			return 0
		case "w":
			fmt.Fprintln(stdout, "Automatic start remains disabled.")
		case "k":
			if deploymentKeyMedia {
				fmt.Fprintln(stdout, "Stable deployment-key media selected.")
				return deploymentKeySelectionExitCode
			}
			fmt.Fprintln(stderr, "Unknown selection; no test started.")
		default:
			fmt.Fprintln(stderr, "Unknown selection; no test started.")
		}
	}
}
