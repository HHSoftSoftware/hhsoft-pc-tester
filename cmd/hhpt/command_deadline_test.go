package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func decodeCommandEvidence(t *testing.T, output *bytes.Buffer, destination any) {
	t.Helper()
	start := bytes.Index(output.Bytes(), []byte("{\n"))
	if start < 0 || json.Unmarshal(output.Bytes()[start:], destination) != nil {
		t.Fatal("command omitted usable diagnostic evidence")
	}
}

func TestGraphicsAndDisplayWholeCommandDefaultsMatchCatalogLimits(t *testing.T) {
	frozen, err := plan.Build(inventory.NewSnapshot(nil), plan.ProfileQuick, "x86_64", make([]byte, 32), "deadline-fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range frozen.Tests {
		if row.ID == "graphics.quick" && time.Duration(row.TimeoutSeconds)*time.Second != graphicsCommandTimeout ||
			row.ID == "display.operator.quick" && time.Duration(row.TimeoutSeconds)*time.Second != displayCommandTimeout {
			t.Fatal("command default no longer matches its frozen catalog deadline")
		}
	}
	for _, command := range []string{"graphics", "display"} {
		var output, stderr bytes.Buffer
		var remaining time.Duration
		var bounded bool
		check := func(ctx context.Context) {
			deadline, ok := ctx.Deadline()
			bounded, remaining = ok, time.Until(deadline)
		}
		var code int
		if command == "graphics" {
			code = runGraphicsWith(t.Context(), nil, &output, &stderr, func(ctx context.Context, _ string, _ int, _ graphics.Runner) graphics.Result {
				check(ctx)
				return completeGraphicsEvidence()
			})
		} else {
			code = runDisplayWith(t.Context(), nil, strings.NewReader(""), &output, &stderr, func(ctx context.Context, _ string, _ time.Duration, _ graphics.InteractiveRunner) graphics.VisualResult {
				check(ctx)
				return graphics.VisualResult{Schema: graphics.VisualSchema, Status: graphics.StatusSkip}
			})
		}
		want := 30 * time.Second
		if command == "display" {
			want = 45 * time.Second
		}
		if code != 0 || !bounded || remaining > want || remaining < want-time.Second {
			t.Fatalf("%s whole-command budget: code=%d remaining=%s bounded=%t", command, code, remaining, bounded)
		}
	}
}

func TestGraphicsAndDisplayRejectUnboundedTimeoutBeforeWork(t *testing.T) {
	for _, timeout := range []string{"0", "-1s", "1ms", "46s", "1h"} {
		called := false
		var output, stderr bytes.Buffer
		code := runGraphicsWith(t.Context(), []string{"-timeout", timeout}, &output, &stderr, func(context.Context, string, int, graphics.Runner) graphics.Result {
			called = true
			return graphics.Result{}
		})
		if code != 2 || called {
			t.Fatalf("graphics accepted timeout %s", timeout)
		}
		code = runDisplayWith(t.Context(), []string{"-timeout", timeout}, strings.NewReader("P\n"), &output, &stderr, func(context.Context, string, time.Duration, graphics.InteractiveRunner) graphics.VisualResult {
			called = true
			return graphics.VisualResult{}
		})
		if code != 2 || called {
			t.Fatalf("display accepted timeout %s", timeout)
		}
	}
}

func TestCanceledCommandsStartNoProbeOrInput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	var output, stderr bytes.Buffer
	code := runGraphicsWith(ctx, nil, &output, &stderr, func(context.Context, string, int, graphics.Runner) graphics.Result {
		called = true
		return graphics.Result{}
	})
	var graphicsResult graphics.Result
	decodeCommandEvidence(t, &output, &graphicsResult)
	if code != 0 || called || graphicsResult.Status != graphics.StatusAborted {
		t.Fatal("pre-canceled graphics started or lost abort evidence")
	}
	output.Reset()
	input := strings.NewReader("P\n")
	code = runDisplayWith(ctx, nil, input, &output, &stderr, func(context.Context, string, time.Duration, graphics.InteractiveRunner) graphics.VisualResult {
		called = true
		return graphics.VisualResult{}
	})
	var displayResult graphics.VisualResult
	decodeCommandEvidence(t, &output, &displayResult)
	if code != 0 || called || input.Len() != 2 || displayResult.Status != graphics.StatusAborted {
		t.Fatal("pre-canceled display started or consumed input")
	}
}

func TestCommandDeadlineAndCancellationPreserveCompletedFailures(t *testing.T) {
	for _, command := range []string{"graphics", "display"} {
		for _, failure := range []bool{false, true} {
			for _, deadline := range []bool{false, true} {
				ctx, cancel := context.WithCancel(t.Context())
				var output, stderr bytes.Buffer
				stop := func(ctx context.Context) {
					if deadline {
						<-ctx.Done()
					} else {
						cancel()
					}
				}
				var code int
				var status graphics.Status
				if command == "graphics" {
					code = runGraphicsWith(ctx, []string{"-timeout", "20ms"}, &output, &stderr, func(ctx context.Context, _ string, _ int, _ graphics.Runner) graphics.Result {
						result := completeGraphicsEvidence()
						if failure {
							result.Pattern.Status = graphics.StatusFail
						}
						stop(ctx)
						return result
					})
					var result graphics.Result
					decodeCommandEvidence(t, &output, &result)
					status = result.Status
					if failure && result.Pattern.Status != graphics.StatusFail {
						t.Fatal("completed graphics failure was erased")
					}
				} else {
					input := strings.NewReader("P\n")
					code = runDisplayWith(ctx, []string{"-timeout", "20ms"}, input, &output, &stderr, func(ctx context.Context, _ string, _ time.Duration, _ graphics.InteractiveRunner) graphics.VisualResult {
						result := completeDisplayEvidence()
						if failure {
							result.Findings[0].Criteria["pixels"] = graphics.StatusFail
						}
						stop(ctx)
						return result
					})
					var result graphics.VisualResult
					decodeCommandEvidence(t, &output, &result)
					status = result.Status
					if input.Len() != 2 || failure && result.Findings[0].Criteria["pixels"] != graphics.StatusFail {
						t.Fatal("stopped display consumed input or erased completed finding")
					}
				}
				cancel()
				schema := graphics.Schema
				if command == "display" {
					schema = graphics.VisualSchema
				}
				start := bytes.Index(output.Bytes(), []byte("{\n"))
				if actual, err := testEvidenceStatus(output.Bytes()[start:], schema); err != nil || actual != string(status) {
					t.Fatalf("stopped evidence cannot survive finalization: %s %v", actual, err)
				}
				want := graphics.StatusAborted
				if deadline {
					want = graphics.StatusError
				}
				if failure {
					want = graphics.StatusFail
				}
				if code != 0 || status != want {
					t.Fatalf("%s stopped result=%s code=%d, want %s", command, status, code, want)
				}
			}
		}
	}
}

type findingBrokerFixture struct {
	read       func(context.Context, int) (string, error)
	directRead bool
}

func (broker *findingBrokerFixture) Read([]byte) (int, error) {
	broker.directRead = true
	return 0, errors.New("direct broker reads forbidden")
}
func (broker *findingBrokerFixture) ReadFindingLine(ctx context.Context, limit int) (string, error) {
	return broker.read(ctx, limit)
}

func TestDisplayToolsAndPromptShareOneDeadline(t *testing.T) {
	var toolDeadline time.Time
	broker := &findingBrokerFixture{read: func(ctx context.Context, limit int) (string, error) {
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.Equal(toolDeadline) || limit != 4096 {
			t.Fatal("finding prompt received a fresh budget or wrong line bound")
		}
		<-ctx.Done()
		return "", ctx.Err()
	}}
	probe := func(ctx context.Context, _ string, _ time.Duration, _ graphics.InteractiveRunner) graphics.VisualResult {
		toolDeadline, _ = ctx.Deadline()
		time.Sleep(20 * time.Millisecond)
		result := completeDisplayEvidence()
		result.Findings = nil
		result.Status = graphics.StatusInconclusive
		return result
	}
	var output, stderr bytes.Buffer
	code := runDisplayWith(t.Context(), []string{"-timeout", "60ms"}, broker, &output, &stderr, probe)
	var result graphics.VisualResult
	decodeCommandEvidence(t, &output, &result)
	if code != 0 || result.Status != graphics.StatusError || broker.directRead {
		t.Fatal("whole display deadline did not bound the operator prompt")
	}
}

func TestFindingStopRaceKeepsDefectButNeverAcceptsLatePass(t *testing.T) {
	for _, response := range []string{"P", "F", "ppfpppp"} {
		ctx, cancel := context.WithCancel(t.Context())
		broker := &findingBrokerFixture{read: func(context.Context, int) (string, error) { cancel(); return response, context.Canceled }}
		probe := func(context.Context, string, time.Duration, graphics.InteractiveRunner) graphics.VisualResult {
			result := completeDisplayEvidence()
			result.Findings = nil
			result.Status = graphics.StatusInconclusive
			return result
		}
		var output, stderr bytes.Buffer
		code := runDisplayWith(ctx, nil, broker, &output, &stderr, probe)
		var result graphics.VisualResult
		decodeCommandEvidence(t, &output, &result)
		want := graphics.StatusFail
		if response == "P" {
			want = graphics.StatusAborted
		}
		if code != 0 || result.Status != want || broker.directRead {
			t.Fatalf("stop race converted %s to %s", response, result.Status)
		}
		for _, finding := range result.Findings {
			for _, status := range finding.Criteria {
				if status == graphics.StatusPass {
					t.Fatal("late operator pass was accepted")
				}
			}
		}
	}
}

func TestFindingReadersAreBoundedAndDoNotReadAhead(t *testing.T) {
	for _, input := range []io.Reader{strings.NewReader("P\nnext\n"), bytes.NewReader([]byte("P\nnext\n")), bytes.NewBufferString("P\nnext\n")} {
		line, timedOut, err := readFindingLine(t.Context(), input, time.Second)
		if line != "P" || timedOut || err != nil {
			t.Fatalf("complete finding rejected: %q %v", line, err)
		}
		remaining, err := io.ReadAll(input)
		if err != nil || string(remaining) != "next\n" {
			t.Fatal("finding consumed a later prompt's input")
		}
	}
	input := strings.NewReader(strings.Repeat("P", maximumFindingLineBytes+1) + "\n")
	if line, _, err := readFindingLine(t.Context(), input, time.Second); err == nil || line != "" {
		t.Fatal("oversized finding was accepted")
	}
	unknown := &findingReadError{data: "P\n", err: nil}
	if _, _, err := readFindingLine(t.Context(), unknown, time.Second); !errors.Is(err, errFindingInputUnsupported) || unknown.data != "P\n" {
		t.Fatal("unsupported uninterruptible reader was called")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader := strings.NewReader("P\n")
	if _, _, err := readFindingLine(ctx, reader, time.Second); !errors.Is(err, context.Canceled) || reader.Len() != 2 {
		t.Fatal("pre-canceled finding consumed input")
	}
}
