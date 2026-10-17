package main

import (
	"bufio"
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

func runDisplay(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	return runDisplayWith(ctx, arguments, input, stdout, stderr, graphics.ProbePreferredDisplays)
}

func runDisplayWith(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer, probe visualCollector) int {
	flags := flag.NewFlagSet("display", flag.ContinueOnError)
	flags.SetOutput(stderr)
	displayTime := flags.Duration("pattern-duration", 5*time.Second, "preferred-mode pattern display time")
	findingTimeout := flags.Duration("finding-timeout", 15*time.Second, "single operator-finding response deadline")
	outputPath := flags.String("output", "-", "new JSON output file, or - for stdout")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *displayTime < time.Second || *displayTime > 30*time.Second || *findingTimeout < time.Second || *findingTimeout > 5*time.Minute {
		fmt.Fprintln(stderr, "display requires 1s-30s patterns, a 1s-5m finding timeout, and no positional arguments")
		return 2
	}
	fmt.Fprintf(stdout, "Connected displays will show native/preferred patterns for %s. Inspect pixels, geometry, color, flicker, scaling, overscan, and artifacts.\n", displayTime.Round(time.Second))
	result := probe(ctx, "/", *displayTime, graphics.NewExecRunner("/run/hhpt", *displayTime+15*time.Second, 64<<10))
	connectors := patternedConnectors(result)
	if len(connectors) > 0 {
		fmt.Fprintln(stdout, "Finding order:", strings.Join(connectors, ", "))
		fmt.Fprintln(stdout, "Enter P/F/I or a seven-letter P/F/I code per display, comma-separated; Enter means inconclusive.")
		fmt.Fprint(stdout, "Pixels Geometry Color Flicker Scaling Overscan Artifacts: ")
		line, timedOut, readErr := readFindingLine(ctx, input, *findingTimeout)
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

func readFindingLine(ctx context.Context, input io.Reader, timeout time.Duration) (string, bool, error) {
	type response struct {
		line string
		err  error
	}
	result := make(chan response, 1)
	go func() {
		line, err := bufio.NewReader(input).ReadString('\n')
		result <- response{strings.TrimSpace(line), err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case value := <-result:
		return value.line, false, value.err
	case <-ctx.Done():
		return "", false, ctx.Err()
	case <-timer.C:
		return "", true, nil
	}
}
