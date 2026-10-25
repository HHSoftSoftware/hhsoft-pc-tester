package graphics

import (
	"context"
	"testing"
	"time"
)

type deadlineRunner struct {
	calls int
	run   func(context.Context, int) (string, error)
}

func (runner *deadlineRunner) Run(ctx context.Context, _ string, _ ...string) (string, error) {
	runner.calls++
	return runner.run(ctx, runner.calls)
}
func (runner *deadlineRunner) RunInteractive(ctx context.Context, _ time.Duration, name string, args ...string) (string, error) {
	return runner.Run(ctx, name, args...)
}

func TestGraphicsStagesShareDeadlineAndNeverPassInterruptedPattern(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel()
	deadline, _ := ctx.Deadline()
	runner := &deadlineRunner{run: func(stage context.Context, call int) (string, error) {
		actual, ok := stage.Deadline()
		if !ok || !actual.Equal(deadline) {
			t.Fatal("graphics stage received a new command budget")
		}
		if call == 1 {
			time.Sleep(15 * time.Millisecond)
			return "OpenGL core profile renderer: Fixture GPU\n", nil
		}
		<-stage.Done()
		return "", stage.Err()
	}}
	result := Probe(ctx, cleanupDisplayFixture(t, 1), 120, runner)
	if runner.calls != 2 || result.Status != StatusError || result.Renderer.Status != StatusPass || result.Pattern.Status != StatusError {
		t.Fatalf("interrupted graphics pattern became success or lost earlier evidence: %+v calls=%d", result, runner.calls)
	}
}

func TestCanceledRendererDoesNotStartPatternAndDisplayStopsLaterCards(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	runner := &deadlineRunner{run: func(context.Context, int) (string, error) {
		cancel()
		return "OpenGL core profile renderer: Fixture GPU\n", nil
	}}
	result := Probe(ctx, cleanupDisplayFixture(t, 1), 120, runner)
	if runner.calls != 1 || result.Status != StatusAborted || result.Renderer.Status != StatusPass || result.Pattern.Status != StatusAborted {
		t.Fatal("canceled renderer launched a pattern or discarded completed observations")
	}
	ctx, cancel = context.WithCancel(t.Context())
	runner = &deadlineRunner{run: func(_ context.Context, call int) (string, error) {
		if call == 2 {
			cancel()
			return "", context.Canceled
		}
		return "", nil
	}}
	visual := ProbePreferredDisplays(ctx, cleanupDisplayFixture(t, 3), time.Second, runner)
	if runner.calls != 2 || visual.Status != StatusAborted || len(visual.Patterns) != 2 || visual.Patterns[0].Status != StatusPass || visual.Patterns[1].Status != StatusAborted {
		t.Fatalf("display cancellation launched another card or erased evidence: %+v calls=%d", visual, runner.calls)
	}
}

func TestPreCanceledGraphicsAndDisplayStartNoWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runner := &deadlineRunner{run: func(context.Context, int) (string, error) {
		t.Fatal("pre-canceled probe started a worker")
		return "", nil
	}}
	root := cleanupDisplayFixture(t, 2)
	if result := Probe(ctx, root, 120, runner); result.Status != StatusAborted || len(result.Cards) != 0 {
		t.Fatal("pre-canceled graphics performed discovery")
	}
	if result := ProbePreferredDisplays(ctx, root, time.Second, runner); result.Status != StatusAborted || len(result.Patterns) != 0 {
		t.Fatal("pre-canceled display performed work")
	}
}

func TestInterruptionPreservesFailureObservationsAndCleanupFlag(t *testing.T) {
	result := Result{Status: StatusPass, CleanupUncertain: true, Renderer: RendererResult{Status: StatusFail}, Pattern: PatternResult{Status: StatusPass}}
	result.MarkInterrupted(context.DeadlineExceeded)
	result.MarkInterrupted(context.DeadlineExceeded)
	if result.Status != StatusFail || !result.CleanupUncertain || result.Pattern.Status != StatusPass || len(result.Warnings) != 1 {
		t.Fatal("interruption changed completed graphics evidence")
	}
	visual := VisualResult{Status: StatusPass, CleanupUncertain: true, Patterns: []DisplayPattern{{Status: StatusPass}}, Findings: []VisualFinding{{Criteria: map[string]Status{"pixels": StatusFail}}}}
	visual.MarkInterrupted(context.Canceled)
	if visual.Status != StatusFail || !visual.CleanupUncertain || visual.Findings[0].Criteria["pixels"] != StatusFail {
		t.Fatal("interruption hid an operator failure")
	}
}
