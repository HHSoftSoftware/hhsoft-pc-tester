//go:build !linux && !windows

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestInventoryPlanFingerprintUnsupportedDefaultAcquisition(t *testing.T) {
	commands := []struct {
		name string
		run  func(context.Context, []string, io.Writer, io.Writer, snapshotCollector, destinationWriter) int
	}{
		{"inventory", runInventoryWithDestination},
		{"plan", runPlanWithDestination},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fingerprint.fixture")
			// Public synthetic input, valid for the supported adapters.
			key := make([]byte, 32)
			for i := range key {
				key[i] = byte(i)
			}
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal("create owned fingerprint fixture failed")
			}
			n, writeErr := file.Write(key)
			closeErr := file.Close()
			if n != len(key) || writeErr != nil || closeErr != nil {
				t.Fatal("complete owned fingerprint fixture failed")
			}
			collections, destinations := 0, 0
			collect := func(context.Context) (inventory.Snapshot, error) {
				collections++
				return inventory.NewSnapshot(nil), nil
			}
			write := func(string, io.Writer, func(io.Writer) error) error {
				destinations++
				return nil
			}
			var stdout, stderr bytes.Buffer
			code := command.run(t.Context(), []string{"-fingerprint-key", path}, &stdout, &stderr, collect, write)
			if code != 1 || stdout.Len() != 0 || stderr.String() != "read fingerprint key: input acquisition failed\n" {
				t.Fatalf("unsupported input boundary: code=%d stdout_bytes=%d stderr_matches=%t",
					code, stdout.Len(), stderr.String() == "read fingerprint key: input acquisition failed\n")
			}
			if collections != 0 || destinations != 0 {
				t.Fatalf("unsupported input reached consumers: collections=%d destinations=%d", collections, destinations)
			}
		})
	}
}
