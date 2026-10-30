//go:build !linux && !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLaunchSelectionInputUnsupportedPlatform(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launch.json")
	wire := []byte(`{"schema_version":1,"profile":"quick","architecture":"x86_64","excluded_tests":[]}`)
	if err := os.WriteFile(path, wire, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readRetestInput(t.Context(), path, retestLaunchSelectionInput)
	assertRetestInputFailure(t, got, err, false)
	h := newLaunchPlanHarness(t)
	h.arguments[len(h.arguments)-1] = path
	h.read = readRetestInput
	if code := h.run(t.Context()); code != 1 || h.stderr.String() != "read launch selection: input acquisition failed\n" || h.stdout.Len() != 0 || len(h.events) != 0 {
		t.Fatal("unsupported launch input reached planner effects")
	}
}
