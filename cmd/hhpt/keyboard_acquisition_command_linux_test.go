//go:build linux

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/keyboardevidence"
)

func TestKeyboardActualRootRejectionSurvivesCLIInterruption(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{"proc/bus/input/devices": "N: Name=\"Fixture Keyboard\"\nH: Handlers=kbd event2\n", "sys/class/input/event2/dev": "13:66\n", "dev/input/event2": "regular inert fixture"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var produced inputtest.KeyboardResult
	calls, finishes := 0, 0
	collect := func(ctx context.Context, options inputtest.KeyboardOptions) inputtest.KeyboardResult {
		calls++
		options.Root = root
		produced = inputtest.RunKeyboard(ctx, options)
		cancel()
		return produced
	}
	prepare := func(ctx context.Context, _ io.Reader) (context.Context, io.Reader, func() error, error) {
		return ctx, &keyboardHandoffFixture{}, func() error { finishes++; return nil }, nil
	}
	var out, stderr bytes.Buffer
	if runKeyboardWith(ctx, nil, strings.NewReader(""), &out, &stderr, collect, prepare) != 0 || calls != 1 || finishes != 1 {
		t.Fatal("real root producer failed command composition")
	}
	actual, err := keyboardevidence.Decode(out.Bytes())
	if err != nil || !reflect.DeepEqual(actual, produced) || actual.Status != inputtest.StatusError || actual.Detail != "identity_unavailable" || !actual.CleanupConfirmed || len(actual.Devices) != 1 || actual.Devices[0].Readable {
		t.Fatalf("real rejected node was promoted or cancelled: %#v %v", actual, err)
	}
	if outcome, state, err := sessionWorkerOutcomeEvidence(out.Bytes(), nil, inputtest.KeyboardSchema, 0, true); err != nil || outcome != "error" || state != "interrupted" {
		t.Fatal("session masked real producer failure")
	}
	assertKeyboardAcquisitionFinalizerBoundary(t, out.Bytes(), false)
	data, err := os.ReadFile(filepath.Join(root, "dev/input/event2"))
	if err != nil || string(data) != "regular inert fixture" {
		t.Fatal("regular fixture changed")
	}
}
