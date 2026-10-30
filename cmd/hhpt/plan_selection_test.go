package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func TestPlanCommandFreezesValidatedSelection(t *testing.T) {
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x25}, 32)
	keyPath := writeTestKey(t, directory, "fingerprint.key", key)
	selectionPath := filepath.Join(directory, "selection.json")
	if err := os.WriteFile(selectionPath, []byte(`{"excluded_tests":["usb.readiness.quick","cpu.quick"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil)})
	collect := func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }
	arguments := []string{"-fingerprint-key", keyPath, "-architecture", "x86_64", "-selection-file", selectionPath}
	var stdout, stderr bytes.Buffer
	if code := runPlanWithInput(t.Context(), arguments, &stdout, &stderr, collect, writeDestination, captureFixtureInput(t)); code != 0 {
		t.Fatalf("planner rejected selection: %d %s", code, stderr.String())
	}
	actual, err := plan.Decode(stdout.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	wanted, err := plan.BuildWithSelection(snapshot, plan.ProfileQuick, "x86_64", key, "local-1", []string{"cpu.quick", "usb.readiness.quick"})
	if err != nil || actual.ID != wanted.ID || len(actual.ExcludedTests) != 2 {
		t.Fatal("CLI selection did not bind the expected plan")
	}
	for _, row := range actual.Tests {
		if row.ID == "cpu.quick" && row.Status != plan.TestExcluded {
			t.Fatal("present selected test was not excluded")
		}
		if row.ID == "usb.readiness.quick" && row.Status != plan.TestNotPresent {
			t.Fatal("absent selected test lost its applicability reason")
		}
	}
}

func TestPlanCommandEmptySelectionPreservesDefaultBytes(t *testing.T) {
	directory := t.TempDir()
	keyPath := writeTestKey(t, directory, "fingerprint.key", bytes.Repeat([]byte{0x25}, 32))
	selectionPath := filepath.Join(directory, "selection.json")
	if err := os.WriteFile(selectionPath, []byte(`{"excluded_tests":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	collect := func(context.Context) (inventory.Snapshot, error) { return inventory.NewSnapshot(nil), nil }
	arguments := []string{"-fingerprint-key", keyPath, "-architecture", "x86_64"}
	var defaults, selected, stderr bytes.Buffer
	if code := runPlanWithInput(t.Context(), arguments, &defaults, &stderr, collect, writeDestination, captureFixtureInput(t)); code != 0 {
		t.Fatal(stderr.String())
	}
	arguments = append(arguments, "-selection-file", selectionPath)
	if code := runPlanWithInput(t.Context(), arguments, &selected, &stderr, collect, writeDestination, captureFixtureInput(t)); code != 0 {
		t.Fatal(stderr.String())
	}
	if !bytes.Equal(defaults.Bytes(), selected.Bytes()) || bytes.Contains(selected.Bytes(), []byte("excluded_tests")) {
		t.Fatal("explicit default changed planner wire bytes")
	}
}

func TestPlanCommandRejectsInvalidSelectionBeforeInventory(t *testing.T) {
	for _, contents := range []string{"missing-file", "directory", `{"excluded_tests":null}`, `{"excluded_tests":["unknown"]}`, `{"excluded_tests":["cpu.quick","cpu.quick"]}`, strings.Repeat(" ", plan.MaximumSelectionBytes+1)} {
		t.Run(contents[:min(len(contents), 50)], func(t *testing.T) {
			directory := t.TempDir()
			selectionPath := filepath.Join(directory, "selection.json")
			if contents == "directory" {
				if err := os.Mkdir(selectionPath, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if contents != "missing-file" {
				if err := os.WriteFile(selectionPath, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			outputPath := filepath.Join(directory, "plan.json")
			collected := false
			collect := func(context.Context) (inventory.Snapshot, error) {
				collected = true
				return inventory.NewSnapshot(nil), nil
			}
			var stdout, stderr bytes.Buffer
			arguments := []string{"-fingerprint-key", filepath.Join(directory, "unread.key"), "-selection-file", selectionPath, "-output", outputPath}
			if code := runPlanWithInput(t.Context(), arguments, &stdout, &stderr, collect, writeDestination, captureFixtureInput(t)); code != 1 || collected || !strings.Contains(stderr.String(), "quick-test selection") {
				t.Fatalf("invalid selection progressed: code=%d collected=%t stderr=%s", code, collected, stderr.String())
			}
			if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
				t.Fatal("invalid selection created a plan artifact")
			}
		})
	}
}

func TestPlanCommandRejectsPositionalArgumentsBeforeReadingInputs(t *testing.T) {
	directory := t.TempDir()
	keyPath := writeTestKey(t, directory, "fingerprint.key", bytes.Repeat([]byte{0x25}, 32))
	outputPath := filepath.Join(directory, "plan.json")
	selectionPath := filepath.Join(directory, "must-not-be-ignored.json")
	for _, arguments := range [][]string{
		{"-fingerprint-key", keyPath, "-output", outputPath, "stray", "-selection-file", selectionPath},
		{"-fingerprint-key", filepath.Join(directory, "unread.key"), "--", "-selection-file", selectionPath},
		{"stray", "-fingerprint-key", keyPath, "-selection-file", selectionPath},
	} {
		collected := false
		collect := func(context.Context) (inventory.Snapshot, error) {
			collected = true
			return inventory.NewSnapshot(nil), nil
		}
		var stdout, stderr bytes.Buffer
		if code := runPlanWithInput(t.Context(), arguments, &stdout, &stderr, collect, writeDestination, captureFixtureInput(t)); code != 2 || collected || stdout.Len() != 0 || !strings.Contains(stderr.String(), "no positional arguments") {
			t.Fatalf("positional tail bypassed selection: code=%d collected=%t stderr=%s", code, collected, stderr.String())
		}
		if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
			t.Fatal("misordered flags emitted an unintended default plan")
		}
	}
}

func TestPlanCommandRejectsSelectionSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "selection-target.json")
	if err := os.WriteFile(target, []byte(`{"excluded_tests":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	selectionPath := filepath.Join(directory, "selection-link.json")
	if err := os.Symlink(target, selectionPath); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		t.Fatal(err)
	}
	collected := false
	collect := func(context.Context) (inventory.Snapshot, error) {
		collected = true
		return inventory.NewSnapshot(nil), nil
	}
	var stdout, stderr bytes.Buffer
	arguments := []string{"-fingerprint-key", filepath.Join(directory, "unread.key"), "-selection-file", selectionPath}
	if code := runPlanWithInput(t.Context(), arguments, &stdout, &stderr, collect, writeDestination, captureFixtureInput(t)); code != 1 || collected || !strings.Contains(stderr.String(), "read quick-test selection") {
		t.Fatalf("selection symlink was followed: code=%d collected=%t stderr=%s", code, collected, stderr.String())
	}
}

func TestPlanCommandRejectsNonQuickExclusions(t *testing.T) {
	directory := t.TempDir()
	keyPath := writeTestKey(t, directory, "fingerprint.key", bytes.Repeat([]byte{0x25}, 32))
	selectionPath := filepath.Join(directory, "selection.json")
	encoded, err := json.Marshal(map[string][]string{"excluded_tests": {"cpu.quick"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selectionPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	arguments := []string{"-fingerprint-key", keyPath, "-profile", "standard", "-selection-file", selectionPath}
	collect := func(context.Context) (inventory.Snapshot, error) { return inventory.NewSnapshot(nil), nil }
	var stdout, stderr bytes.Buffer
	if code := runPlanWithInput(t.Context(), arguments, &stdout, &stderr, collect, writeDestination, captureFixtureInput(t)); code != 1 || stdout.Len() != 0 {
		t.Fatalf("non-quick exclusions accepted: %d %s", code, stderr.String())
	}
}
