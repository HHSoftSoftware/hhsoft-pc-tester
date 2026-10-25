package graphics

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/workerexec"
)

type cleanupFailureRunner struct {
	calls   int
	failAt  int
	failure error
}

func (runner *cleanupFailureRunner) Run(context.Context, string, ...string) (string, error) {
	runner.calls++
	if runner.calls == runner.failAt {
		return "", runner.failure
	}
	return "OpenGL core profile renderer: Fixture GPU\n", nil
}

func (runner *cleanupFailureRunner) RunInteractive(ctx context.Context, _ time.Duration, name string, arguments ...string) (string, error) {
	return runner.Run(ctx, name, arguments...)
}

func cleanupDisplayFixture(t *testing.T, cards int) string {
	t.Helper()
	root := t.TempDir()
	for index := 0; index < cards; index++ {
		card := fmt.Sprintf("card%d", index)
		writeFile(t, root, "sys/class/drm/"+card+"/device/uevent", "DRIVER=fixture\n")
		writeFile(t, root, "sys/class/drm/"+card+"-HDMI-A-1/status", "connected\n")
		writeFile(t, root, "sys/class/drm/"+card+"-HDMI-A-1/modes", "1920x1080\n")
	}
	return root
}

func TestUnconfirmedRendererCleanupNeverStartsPattern(t *testing.T) {
	runner := &cleanupFailureRunner{failAt: 1, failure: fmt.Errorf("injected cleanup: %w", workerexec.ErrCleanupUncertain)}
	result := Probe(t.Context(), cleanupDisplayFixture(t, 1), 12, runner)
	if !result.CleanupUncertain || runner.calls != 1 || result.Pattern.Status != StatusAborted || result.Status == StatusPass {
		t.Fatalf("cleanup uncertainty allowed further work or success: calls=%d result=%+v", runner.calls, result)
	}
}

func TestUnconfirmedPatternCleanupReachesCaller(t *testing.T) {
	runner := &cleanupFailureRunner{failAt: 2, failure: workerexec.ErrCleanupUncertain}
	result := Probe(t.Context(), cleanupDisplayFixture(t, 1), 12, runner)
	if !result.CleanupUncertain || runner.calls != 2 || result.Renderer.Status != StatusPass || result.Pattern.Status != StatusInconclusive || result.Status == StatusPass {
		t.Fatalf("pattern cleanup failure was lost: calls=%d result=%+v", runner.calls, result)
	}
}

func TestUnconfirmedDisplayCleanupStopsLaterCardsAndOperatorAcceptance(t *testing.T) {
	runner := &cleanupFailureRunner{failAt: 2, failure: fmt.Errorf("injected cleanup: %w", workerexec.ErrCleanupUncertain)}
	result := ProbePreferredDisplays(t.Context(), cleanupDisplayFixture(t, 3), time.Second, runner)
	if !result.CleanupUncertain || runner.calls != 2 || len(result.Patterns) != 2 || result.Patterns[0].Status != StatusPass || result.Patterns[1].Status != StatusInconclusive {
		t.Fatalf("display cleanup failure lost earlier evidence or started another card: calls=%d result=%+v", runner.calls, result)
	}
	if err := ApplyVisualFindingCodes(&result, []string{"p"}); err == nil || len(result.Findings) != 0 || result.Status == StatusPass {
		t.Fatal("operator findings accepted a run whose worker cleanup was unconfirmed")
	}
}

func TestOrdinaryGraphicsErrorRetainsExistingCoverageBehavior(t *testing.T) {
	runner := &cleanupFailureRunner{failAt: 1, failure: errors.New("ordinary renderer failure")}
	result := Probe(t.Context(), cleanupDisplayFixture(t, 1), 12, runner)
	if result.CleanupUncertain || runner.calls != 2 || result.Pattern.Status != StatusPass || result.Status != StatusInconclusive {
		t.Fatalf("ordinary failure acquired terminal cleanup semantics: calls=%d result=%+v", runner.calls, result)
	}
}
