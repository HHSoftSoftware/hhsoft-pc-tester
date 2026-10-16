package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestUIAutomaticStartAfterInactivity(t *testing.T) {
	wait := func(context.Context, time.Duration, io.Reader) (bool, error) {
		return false, nil
	}
	var output bytes.Buffer
	if result := runUIWith(context.Background(), nil, strings.NewReader(""), &output, &output, wait); result != 0 {
		t.Fatalf("ui returned %d", result)
	}
	if !strings.Contains(output.String(), "15 seconds") || !strings.Contains(output.String(), "automatic safe read-only profile") {
		t.Fatalf("unexpected output %q", output.String())
	}
}

func TestUIRequiresSelectionAfterActivity(t *testing.T) {
	wait := func(context.Context, time.Duration, io.Reader) (bool, error) {
		return true, nil
	}
	var output bytes.Buffer
	if result := runUIWith(context.Background(), nil, strings.NewReader("w\n1\n"), &output, &output, wait); result != 0 {
		t.Fatalf("ui returned %d", result)
	}
	if !strings.Contains(output.String(), "Automatic start remains disabled.") || !strings.Contains(output.String(), "operator-selected safe read-only profile") {
		t.Fatalf("unexpected output %q", output.String())
	}
}

func TestUILegacyModeIsVisible(t *testing.T) {
	wait := func(context.Context, time.Duration, io.Reader) (bool, error) {
		return false, nil
	}
	var output bytes.Buffer
	if result := runUIWith(context.Background(), []string{"-legacy"}, strings.NewReader(""), &output, &output, wait); result != 0 {
		t.Fatalf("ui returned %d", result)
	}
	if !strings.Contains(output.String(), "LEGACY BIOS/CSM") || !strings.Contains(output.String(), "lower assurance") {
		t.Fatalf("legacy mode not visible: %q", output.String())
	}
}

func TestUIFailsSafeWhenInputMonitoringFails(t *testing.T) {
	wait := func(context.Context, time.Duration, io.Reader) (bool, error) {
		return false, errors.New("denied")
	}
	var output bytes.Buffer
	if result := runUIWith(context.Background(), nil, strings.NewReader("1\n"), &output, &output, wait); result != 0 {
		t.Fatalf("ui returned %d", result)
	}
	if !strings.Contains(output.String(), "Automatic start cancelled; fail-safe operator menu enabled.") {
		t.Fatalf("unexpected output %q", output.String())
	}
}
