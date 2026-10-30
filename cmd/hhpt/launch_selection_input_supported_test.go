//go:build linux || windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLaunchSelectionInputRealFileBounds(t *testing.T) {
	for _, test := range []struct {
		name string
		size int
		pass bool
	}{
		{"minimum", 1, true},
		{"maximum", 16384, true},
		{"empty", 0, false},
		{"oversize", 16385, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "launch.json")
			want := bytes.Repeat([]byte{'x'}, test.size)
			if err := os.WriteFile(path, want, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readRetestInput(t.Context(), path, retestLaunchSelectionInput)
			if test.pass {
				if err != nil || !bytes.Equal(got, want) {
					t.Fatal("real launch acquisition failed")
				}
			} else {
				assertRetestInputFailure(t, got, err, false)
			}
			// A retained Windows owner also prevents this removal.
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLaunchSelectionInputRealMissingAndDirectory(t *testing.T) {
	directory := t.TempDir()
	for _, test := range []struct{ name, path string }{
		{"missing", filepath.Join(directory, "absent")},
		{"directory", directory},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := readRetestInput(t.Context(), test.path, retestLaunchSelectionInput)
			assertRetestInputFailure(t, got, err, false)
		})
	}
}
