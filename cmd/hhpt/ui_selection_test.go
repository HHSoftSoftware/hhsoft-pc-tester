package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUISelectionDefaultStartPaths(t *testing.T) {
	for _, test := range []struct {
		name       string
		input      string
		arguments  []string
		interacted bool
		wantCode   int
	}{
		{name: "automatic"},
		{name: "enter", input: "\n", interacted: true},
		{name: "one", input: "1\n", interacted: true},
		{name: "deployment key", input: "k\n", arguments: []string{"-deployment-key-media"}, interacted: true, wantCode: deploymentKeySelectionExitCode},
		{name: "legacy automatic", arguments: []string{"-legacy"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "selection.json")
			arguments := append([]string{"-selection-output", path}, test.arguments...)
			wait := func(context.Context, time.Duration, io.Reader) (bool, error) { return test.interacted, nil }
			var output bytes.Buffer
			if got := runUIWith(context.Background(), arguments, strings.NewReader(test.input), &output, &output, wait); got != test.wantCode {
				t.Fatalf("ui returned %d, want %d: %s", got, test.wantCode, &output)
			}
			assertUISelectionFile(t, path, `{"excluded_tests":[]}`+"\n")
		})
	}
}

func TestUISelectionChoicesSurviveWaitAndDeploymentKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	var output bytes.Buffer
	got := runUIWith(context.Background(), []string{"-selection-output", path, "-deployment-key-media"},
		strings.NewReader("c\n8\n2\n\nw\nk\n"), &output, &output, uiOperatorWait)
	if got != deploymentKeySelectionExitCode {
		t.Fatalf("ui returned %d: %s", got, &output)
	}
	assertUISelectionFile(t, path, `{"excluded_tests":["cpu.quick","memory.quick"]}`+"\n")
	for _, text := range []string{"[C]", "CPU computation", "Memory patterns", "Pointer movement, buttons and wheels", "Quick tests selected: 9 of 11.", "Automatic start remains disabled."} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("output is missing %q: %s", text, &output)
		}
	}
}

func TestUISelectionToggleAndRestoreDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	var output bytes.Buffer
	got := runUIWith(context.Background(), []string{"-selection-output", path},
		strings.NewReader("c\n2\n2\n6\na\n\n1\n"), &output, &output, uiOperatorWait)
	if got != 0 {
		t.Fatalf("ui returned %d: %s", got, &output)
	}
	assertUISelectionFile(t, path, `{"excluded_tests":[]}`+"\n")
}

func TestUISelectionInvalidChoicesDoNotChangeTests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	var output bytes.Buffer
	got := runUIWith(context.Background(), []string{"-selection-output", path},
		strings.NewReader("unknown\nc\n0\n12\n-1\nx\n1 2\n2\n\n1\n"), &output, &output, uiOperatorWait)
	if got != 0 {
		t.Fatalf("ui returned %d: %s", got, &output)
	}
	assertUISelectionFile(t, path, `{"excluded_tests":["cpu.quick"]}`+"\n")
	if strings.Count(output.String(), "Unknown selection") != 6 {
		t.Fatalf("invalid choices were not rejected: %s", &output)
	}
}

func TestUISelectionNeverStartsOnIncompleteInput(t *testing.T) {
	for name, input := range map[string]string{
		"empty": "", "customized": "c\n2\n", "back": "c\n2\n\n", "partial start": "1",
		"partial key": "k", "partial test": "c\n2", "invalid": "unknown\n",
		"oversized start": strings.Repeat(" ", maximumMenuLineBytes+1) + "1\n",
		"oversized test":  "c\n" + strings.Repeat(" ", maximumMenuLineBytes+1) + "2\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "selection.json")
			var output bytes.Buffer
			got := runUIWith(context.Background(), []string{"-selection-output", path, "-deployment-key-media"},
				strings.NewReader(input), &output, &output, uiOperatorWait)
			if got != 1 {
				t.Fatalf("incomplete input returned %d: %s", got, &output)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("incomplete input created selection: %v", err)
			}
			if strings.Contains(output.String(), "Starting operator-selected") || strings.Contains(output.String(), "Stable deployment-key media selected.") {
				t.Fatalf("incomplete input started a run: %s", &output)
			}
		})
	}
}

func TestUISelectionOversizedLinesAreFullyDiscarded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	longLine := strings.Repeat(" ", maximumMenuLineBytes*3) + "1\n"
	var output bytes.Buffer
	got := runUIWith(context.Background(), []string{"-selection-output", path},
		strings.NewReader(longLine+"c\n"+longLine+"2\n\n1\n"), &output, &output, uiOperatorWait)
	if got != 0 {
		t.Fatalf("ui returned %d: %s", got, &output)
	}
	assertUISelectionFile(t, path, `{"excluded_tests":["cpu.quick"]}`+"\n")
	if strings.Count(output.String(), "Selection too long") != 2 {
		t.Fatalf("oversized lines were not rejected: %s", &output)
	}
}

func TestUISelectionExistingOutputNeverReplaced(t *testing.T) {
	for _, test := range []struct {
		name, input string
		interacted  bool
	}{
		{name: "automatic"}, {name: "manual", input: "1\n", interacted: true},
		{name: "deployment key", input: "k\n", interacted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "selection.json")
			if err := os.WriteFile(path, []byte("existing selection\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			wait := func(context.Context, time.Duration, io.Reader) (bool, error) { return test.interacted, nil }
			var output bytes.Buffer
			got := runUIWith(context.Background(), []string{"-selection-output", path, "-deployment-key-media"},
				strings.NewReader(test.input), &output, &output, wait)
			if got != 1 || !strings.Contains(output.String(), "save test selection:") {
				t.Fatalf("existing output did not stop start: %d %s", got, &output)
			}
			assertUISelectionFile(t, path, "existing selection\n")
			if strings.Contains(output.String(), "Starting automatic") || strings.Contains(output.String(), "Starting operator-selected") || strings.Contains(output.String(), "Stable deployment-key media selected.") {
				t.Fatalf("output failure reported a started run: %s", &output)
			}
		})
	}
}

func TestUISelectionReadErrorDuringOversizedLineStopsBeforeLaterInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	input := io.MultiReader(strings.NewReader(strings.Repeat(" ", maximumMenuLineBytes*3)), &uiRecoveringReader{})
	var output bytes.Buffer
	got := runUIWith(context.Background(), []string{"-selection-output", path}, input, &output, &output, uiOperatorWait)
	if got != 1 || !strings.Contains(output.String(), "input interrupted") {
		t.Fatalf("read error did not stop the UI: %d %s", got, &output)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("input after a read error started a run: %v", err)
	}
}

func TestUISelectionMissingParentStopsStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "selection.json")
	var output bytes.Buffer
	if got := runUIWith(context.Background(), []string{"-selection-output", path}, strings.NewReader("1\n"), &output, &output, uiOperatorWait); got != 1 {
		t.Fatalf("output error returned %d: %s", got, &output)
	}
}

func TestUISelectionNeedsOutputBeforeOfferingChoices(t *testing.T) {
	var output bytes.Buffer
	if got := runUIWith(context.Background(), nil, strings.NewReader("c\n"), &output, &output, uiOperatorWait); got != 1 {
		t.Fatalf("unrecorded choices returned %d: %s", got, &output)
	}
	if strings.Contains(output.String(), "[C]") || !strings.Contains(output.String(), "Test selection needs -selection-output") {
		t.Fatalf("unrecorded choices were offered or silently discarded: %s", &output)
	}
}

func TestUISelectionRejectsNonfileOutputBeforeWaiting(t *testing.T) {
	for _, path := range []string{"-", "   "} {
		var output bytes.Buffer
		wait := func(context.Context, time.Duration, io.Reader) (bool, error) {
			t.Fatal("invalid output reached the countdown")
			return false, nil
		}
		if got := runUIWith(context.Background(), []string{"-selection-output", path}, strings.NewReader(""), &output, &output, wait); got != 2 {
			t.Fatalf("invalid output returned %d: %s", got, &output)
		}
	}
}

func TestUISelectionMonitoringFailureStillRecordsOperatorChoices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	wait := func(context.Context, time.Duration, io.Reader) (bool, error) {
		return false, errors.New("not a console")
	}
	var output bytes.Buffer
	got := runUIWith(context.Background(), []string{"-selection-output", path}, strings.NewReader("c\n2\n\n1\n"), &output, &output, wait)
	if got != 0 || !strings.Contains(output.String(), "fail-safe operator menu enabled") {
		t.Fatalf("fallback returned %d: %s", got, &output)
	}
	assertUISelectionFile(t, path, `{"excluded_tests":["cpu.quick"]}`+"\n")
}

func TestUISelectionReadErrorAndCancellationDoNotStart(t *testing.T) {
	for _, interacted := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "selection.json")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		wait := func(context.Context, time.Duration, io.Reader) (bool, error) { return interacted, nil }
		var output bytes.Buffer
		if got := runUIWith(ctx, []string{"-selection-output", path}, strings.NewReader("1\n"), &output, &output, wait); got != 1 {
			t.Fatalf("cancelled input returned %d: %s", got, &output)
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cancelled input created output: %v", err)
		}
	}
	path := filepath.Join(t.TempDir(), "selection.json")
	var output bytes.Buffer
	input := io.MultiReader(strings.NewReader("c\n2\n"), uiFailedReader{})
	if got := runUIWith(context.Background(), []string{"-selection-output", path}, input, &output, &output, uiOperatorWait); got != 1 {
		t.Fatalf("failed input returned %d: %s", got, &output)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed input created output: %v", err)
	}
}

func TestUISelectionLegacyExposesOnlySupportedQuickChoices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	var output bytes.Buffer
	got := runUIWith(context.Background(), []string{"-selection-output", path, "-legacy", "-deployment-key-media"},
		strings.NewReader("c\n10\n2\n6\n\nk\n1\n"), &output, &output, uiOperatorWait)
	if got != 0 {
		t.Fatalf("legacy menu returned %d: %s", got, &output)
	}
	assertUISelectionFile(t, path, `{"excluded_tests":["cpu.quick","memory.quick"]}`+"\n")
	if !strings.Contains(output.String(), "Graphics and display tests are unsupported in legacy mode.") ||
		strings.Contains(output.String(), "Graphics pattern") || strings.Contains(output.String(), "Display inspection") ||
		strings.Contains(output.String(), "Stable deployment-key media selected.") {
		t.Fatalf("legacy unsupported choices were not explicit: %s", &output)
	}
}

func TestUISelectionAllowsInventoryAndSafetyWithoutQuickTests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	var output bytes.Buffer
	got := runUIWith(context.Background(), []string{"-selection-output", path},
		strings.NewReader("c\n1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n\n1\n"), &output, &output, uiOperatorWait)
	if got != 0 || !strings.Contains(output.String(), "No quick tests selected; inventory and safety checks still run.") {
		t.Fatalf("all exclusions returned %d: %s", got, &output)
	}
	assertUISelectionFile(t, path, `{"excluded_tests":["audio.readiness.quick","cpu.quick","display.operator.quick","graphics.quick","input.keyboard.quick","input.pointer.quick","input.readiness.quick","memory.quick","network.readiness.quick","storage.readiness.quick","usb.readiness.quick"]}`+"\n")
}

func uiOperatorWait(context.Context, time.Duration, io.Reader) (bool, error) { return true, nil }

type uiFailedReader struct{}

func (uiFailedReader) Read([]byte) (int, error) { return 0, errors.New("input failed") }

type uiRecoveringReader struct{ interrupted bool }

func (reader *uiRecoveringReader) Read(destination []byte) (int, error) {
	if !reader.interrupted {
		reader.interrupted = true
		return 0, errors.New("input interrupted")
	}
	return copy(destination, "1\n"), nil
}

func assertUISelectionFile(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != want {
		t.Fatalf("selection bytes = %q, want %q", raw, want)
	}
}
