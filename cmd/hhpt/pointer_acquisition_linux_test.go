//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/pointerevidence"
)

func TestPointerActualFixtureProducerErrorSurvivesCLIAndFinalizer(t *testing.T) {
	root := t.TempDir()
	registry := filepath.Join(root, "proc/bus/input/devices")
	if err := os.MkdirAll(filepath.Dir(registry), 0700); err != nil {
		t.Fatal(err)
	}
	// Duplicate registry keys are an acquisition/parser hard failure before any
	// event open; this fixture cannot touch a physical or guest event device.
	if err := os.WriteFile(registry, []byte("N: Name=\"Fixture Mouse\"\nH: Handlers=mouse0 event0\nH: Handlers=mouse0 event1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	called := 0
	collect := func(ctx context.Context, options inputtest.PointerOptions) inputtest.PointerResult {
		called++
		options.Root = root
		result := inputtest.RunPointer(ctx, options)
		cancel()
		return result
	}
	var out, stderr bytes.Buffer
	if code := runPointerWith(ctx, nil, &out, &stderr, collect); code != 0 || called != 1 {
		t.Fatal("actual producer evidence failed to write")
	}
	result, err := pointerevidence.Decode(out.Bytes())
	if err != nil || result.Status != inputtest.StatusError || result.StopReason != "error" || result.Detail != "pointer registry unavailable or invalid" || len(result.Devices) != 0 {
		t.Fatalf("actual producer hard error was lost: %#v %v", result, err)
	}
	if outcome, state, err := sessionWorkerOutcomeEvidence(out.Bytes(), nil, inputtest.PointerSchema, 0, true); err != nil || outcome != "error" || state != "interrupted" {
		t.Fatal("actual session evidence changed")
	}
	assertPointerAcquisitionFinalizer(t, out.Bytes(), result.Status)
}
