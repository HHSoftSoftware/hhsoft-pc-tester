package graphics

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTruncatedToolOutputCannotEstablishDisplayAcceptance(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []string{"complete", "stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "sys/class/drm/card0/device/uevent", "DRIVER=fixture\n")
			writeFile(t, root, "sys/class/drm/card0-DP-1/status", "connected\n")
			writeFile(t, root, "sys/class/drm/card0-DP-1/modes", "1920x1080\n")
			runner := &outputFixtureRunner{runner: NewExecRunner(t.TempDir(), 10*time.Second, 128), executable: executable, stream: stream}
			result := ProbePreferredDisplays(t.Context(), root, time.Millisecond, runner)
			if len(result.Patterns) != 1 {
				t.Fatalf("expected actual pattern attempt: %#v", result)
			}
			if stream == "complete" {
				if runner.err != nil || result.Patterns[0].Status != StatusPass {
					t.Fatalf("complete successful worker rejected: %#v, %v", result, runner.err)
				}
				if err := ApplyVisualFindingCodes(&result, []string{"P"}); err != nil || result.Status != StatusPass {
					t.Fatalf("complete operator acceptance rejected: %#v, %v", result, err)
				}
				return
			}
			// The real child exits successfully, but its failure message is beyond
			// the capture bound. A missing diagnostic must not become proof of success.
			if strings.Contains(runner.output, "failed to set mode") || runner.err == nil || !strings.Contains(runner.err.Error(), "truncated") {
				t.Fatalf("fixture did not exercise truncated diagnostics: %q, %v", runner.output, runner.err)
			}
			if err := ApplyVisualFindingCodes(&result, []string{"P"}); err != nil {
				t.Fatal(err)
			}
			if result.Patterns[0].Status != StatusInconclusive || result.Status != StatusInconclusive || len(result.Findings) != 0 {
				t.Fatalf("incomplete tool evidence was accepted: %#v", result)
			}
		})
	}
}

type outputFixtureRunner struct {
	runner     *ExecRunner
	executable string
	stream     string
	output     string
	err        error
}

func (runner *outputFixtureRunner) Run(ctx context.Context, _ string, _ ...string) (string, error) {
	runner.output, runner.err = runner.runner.Run(ctx, runner.executable, "-test.run=^TestGraphicsOutputWorker$", "graphics-output-fixture-"+runner.stream)
	return runner.output, runner.err
}

func (runner *outputFixtureRunner) RunInteractive(ctx context.Context, duration time.Duration, _ string, _ ...string) (string, error) {
	runner.output, runner.err = runner.runner.RunInteractive(ctx, duration, runner.executable, "-test.run=^TestGraphicsOutputWorker$", "graphics-output-fixture-"+runner.stream)
	return runner.output, runner.err
}

func TestGraphicsOutputWorker(t *testing.T) {
	argument := os.Args[len(os.Args)-1]
	const prefix = "graphics-output-fixture-"
	if !strings.HasPrefix(argument, prefix) {
		return
	}
	stream := strings.TrimPrefix(argument, prefix)
	if stream == "complete" {
		fmt.Fprintln(os.Stdout, "pattern completed")
		os.Exit(0)
	}
	output := os.Stdout
	if stream == "stderr" {
		output = os.Stderr
	} else if stream != "stdout" {
		os.Exit(2)
	}
	fmt.Fprintln(output, strings.Repeat("ordinary diagnostic ", 32))
	fmt.Fprintln(output, "failed to set mode")
	os.Exit(0)
}
