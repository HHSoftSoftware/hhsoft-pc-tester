package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
)

type visualCollector func(context.Context, string, time.Duration, graphics.InteractiveRunner) graphics.VisualResult

const displayCommandTimeout = 45 * time.Second

func runDisplay(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	return runDisplayWith(ctx, arguments, input, stdout, stderr, graphics.ProbePreferredDisplays)
}

func runDisplayWith(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer, probe visualCollector) (code int) {
	flags := flag.NewFlagSet("display", flag.ContinueOnError)
	flags.SetOutput(stderr)
	displayTime := flags.Duration("pattern-duration", 5*time.Second, "preferred-mode pattern display time")
	findingTimeout := flags.Duration("finding-timeout", 15*time.Second, "single operator-finding response deadline")
	timeout := flags.Duration("timeout", displayCommandTimeout, "whole-command deadline, from 10ms to 45s")
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *displayTime < time.Second || *displayTime > 30*time.Second || *findingTimeout < time.Second || *findingTimeout > 5*time.Minute || *timeout < 10*time.Millisecond || *timeout > displayCommandTimeout {
		fmt.Fprintln(stderr, "display requires 1s-30s patterns, a 1s-5m finding timeout, a 10ms-45s command timeout, and no positional arguments")
		return 2
	}
	runContext, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	fmt.Fprintf(stdout, "Connected displays will show native/preferred patterns for %s. Inspect pixels, geometry, color, flicker, scaling, overscan, and artifacts.\n", displayTime.Round(time.Second))
	result := graphics.VisualResult{Schema: graphics.VisualSchema}
	if commandStopReason(runContext) == nil {
		result = probe(runContext, "/", *displayTime, graphics.NewExecRunner("/run/hhpt", *displayTime+15*time.Second, 64<<10))
	}
	if result.CleanupUncertain {
		defer func() {
			fmt.Fprintln(stderr, "Worker cleanup could not be confirmed; stop the session. Diagnostic evidence remains in RAM when available.")
			code = workerCleanupUncertainExitCode
		}()
	}
	connectors := patternedConnectors(result)
	if len(connectors) > 0 && len(result.Findings) == 0 && !result.CleanupUncertain && commandStopReason(runContext) == nil {
		fmt.Fprintln(stdout, "Finding order:", strings.Join(connectors, ", "))
		fmt.Fprintln(stdout, "Enter P/F/I or a seven-letter P/F/I code per display, comma-separated; Enter means inconclusive.")
		fmt.Fprint(stdout, "Pixels Geometry Color Flicker Scaling Overscan Artifacts: ")
		line, timedOut, readErr := readFindingLine(runContext, input, *findingTimeout)
		var codes []string
		if timedOut {
			fmt.Fprintln(stdout, "Operator finding timed out; visual quality is inconclusive.")
		} else if readErr != nil {
			fmt.Fprintf(stderr, "read operator finding: %v; visual quality is inconclusive\n", readErr)
		} else {
			codes = parseFindingCodes(line, len(connectors))
			if line != "" && codes == nil {
				fmt.Fprintln(stderr, "Invalid finding code; visual quality is inconclusive.")
			}
		}
		if (timedOut || readErr != nil) && line != "" {
			// A fully received defect remains evidence. A response racing a
			// stop cannot supply new passing operator observations.
			codes = parseFindingCodes(strings.Map(func(character rune) rune {
				if character == 'p' || character == 'P' {
					return 'i'
				}
				return character
			}, line), len(connectors))
		}
		if codes == nil {
			codes = make([]string, len(connectors))
			for index := range codes {
				codes[index] = "i"
			}
		}
		if err := graphics.ApplyVisualFindingCodes(&result, codes); err != nil {
			fmt.Fprintf(stderr, "apply operator findings: %v\n", err)
			return 1
		}
	}
	result.MarkInterrupted(commandStopReason(runContext))
	destination, closeDestination, err := openDestination(*outputPath, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "open output: %v\n", err)
		return 1
	}
	defer closeDestination()
	encoder := json.NewEncoder(destination)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(stderr, "write display result: %v\n", err)
		return 1
	}
	return 0
}

func patternedConnectors(result graphics.VisualResult) []string {
	var connectors []string
	for _, pattern := range result.Patterns {
		if pattern.Status != graphics.StatusPass {
			continue
		}
		for _, connector := range pattern.Connectors {
			connectors = append(connectors, connector.Connector)
		}
	}
	return connectors
}

func parseFindingCodes(line string, count int) []string {
	line = strings.TrimSpace(line)
	if count == 0 {
		return []string{}
	}
	if line == "" {
		line = "i"
	}
	if len(line) == 1 && strings.ContainsAny(strings.ToLower(line), "pfi") {
		result := make([]string, count)
		for index := range result {
			result[index] = line
		}
		return result
	}
	values := strings.Split(line, ",")
	if len(values) != count {
		return nil
	}
	for index := range values {
		values[index] = strings.TrimSpace(values[index])
		if len(values[index]) != len(graphics.VisualCriteria) {
			return nil
		}
		for _, character := range strings.ToLower(values[index]) {
			if !strings.ContainsRune("pfi", character) {
				return nil
			}
		}
	}
	return values
}
