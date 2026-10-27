package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

func TestPointerCommandGuidancePreservesMachineReadableEvidence(t *testing.T) {
	var stdout, stderr bytes.Buffer
	collect := func(ctx context.Context, options inputtest.PointerOptions) inputtest.PointerResult {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 90*time.Second || options.Root != "/" || options.Duration != 30*time.Second {
			t.Fatal("pointer command lost its fixed root or bounds")
		}
		return completePointerEvidence()
	}
	if code := runPointerWith(t.Context(), nil, &stdout, &stderr, collect); code != 0 {
		t.Fatalf("pointer returned %d", code)
	}
	var result inputtest.PointerResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Status != inputtest.StatusPass {
		t.Fatal("guidance corrupted JSON or the completed result")
	}
	for _, text := range []string{"each relative pointer", "left, right, up, and down", "Press and release", "right or middle", "each advertised wheel axis in both directions", "Every supported device"} {
		if !strings.Contains(stderr.String(), text) {
			t.Errorf("missing guidance: %s", text)
		}
	}
}

func TestPointerCommandRejectsUnboundedAndMalformedRequests(t *testing.T) {
	for _, arguments := range [][]string{
		{"-duration", "0"}, {"-duration", "9ms"}, {"-duration", "91s"},
		{"-timeout", "9ms"}, {"-timeout", "91s"}, {"-duration", "2s", "-timeout", "1s"},
		{"-unknown"}, {"positional"},
	} {
		collect := func(context.Context, inputtest.PointerOptions) inputtest.PointerResult {
			t.Fatal("invalid request reached device observation")
			return inputtest.PointerResult{}
		}
		if code := runPointerWith(t.Context(), arguments, io.Discard, io.Discard, collect); code != 2 {
			t.Fatalf("invalid arguments %v returned %d", arguments, code)
		}
	}
	// Check command dispatch without opening any device on the test host.
	var stderr bytes.Buffer
	if code := run(t.Context(), []string{"pointer", "-duration", "0"}, strings.NewReader(""), io.Discard, &stderr); code != 2 || strings.Contains(stderr.String(), "unknown command") {
		t.Fatal("pointer command is not wired into dispatch")
	}
}

func TestPointerCommandCancellationCannotRetainPass(t *testing.T) {
	for _, before := range []bool{true, false} {
		ctx, cancel := context.WithCancel(t.Context())
		if before {
			cancel()
		}
		called := false
		collect := func(context.Context, inputtest.PointerOptions) inputtest.PointerResult {
			called = true
			cancel()
			return completePointerEvidence()
		}
		var output bytes.Buffer
		code := runPointerWith(ctx, nil, &output, io.Discard, collect)
		cancel()
		var result inputtest.PointerResult
		if code != 0 || json.Unmarshal(output.Bytes(), &result) != nil || result.Status != inputtest.StatusAborted || result.StopReason != "operator_cancelled" || called == before {
			t.Fatal("cancelled pointer observation retained a pass or discarded its evidence")
		}
	}
}

func TestPointerCommandRejectsLatePassAndKeepsObservations(t *testing.T) {
	collect := func(ctx context.Context, _ inputtest.PointerOptions) inputtest.PointerResult {
		<-ctx.Done()
		return completePointerEvidence()
	}
	var output bytes.Buffer
	if code := runPointerWith(t.Context(), []string{"-duration", "10ms", "-timeout", "10ms"}, &output, io.Discard, collect); code != 0 {
		t.Fatalf("deadline evidence returned %d", code)
	}
	var result inputtest.PointerResult
	if json.Unmarshal(output.Bytes(), &result) != nil || result.Status != inputtest.StatusError || result.StopReason != "deadline" || len(result.Devices) == 0 {
		t.Fatal("late completion claimed a pass or lost prior observations")
	}
}

func TestPointerCommandDoesNotReplaceEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pointer.json")
	prior := []byte("prior evidence\n")
	if err := os.WriteFile(path, prior, 0600); err != nil {
		t.Fatal("could not create synthetic prior evidence")
	}
	collect := func(context.Context, inputtest.PointerOptions) inputtest.PointerResult {
		return completePointerEvidence()
	}
	if code := runPointerWith(t.Context(), []string{"-output", path}, io.Discard, io.Discard, collect); code != 1 {
		t.Fatalf("existing evidence returned %d", code)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, prior) {
		t.Fatal("existing evidence was changed")
	}
}
