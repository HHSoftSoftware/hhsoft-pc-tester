//go:build !linux && !windows

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveFingerprintOtherPlatformRejectsInput(t *testing.T) {
	h := newSaveFingerprintFixture(t, false)
	h.input = nil
	h.acquire = readRetestInput
	if err := os.WriteFile(h.keyPath, h.key, 0600); err != nil {
		t.Fatal(err)
	}
	h.rejected(h.run("1\nRPT-15B6C5\n"), "read fingerprint key: input acquisition failed", 1, 1, 1)

	// The real wrapper must retain the explicit unsupported-platform failure.
	// Its missing source cannot permit target discovery if input admission regresses.
	var stdout, stderr bytes.Buffer
	input := &saveFingerprintConsole{reader: strings.NewReader("1\n")}
	arguments := []string{"save", "-fingerprint-key", h.keyPath, filepath.Join(t.TempDir(), "absent.hhpcr")}
	if code := run(context.Background(), arguments, input, &stdout, &stderr); code != 1 || input.reads != 0 || stdout.Len() != 0 || stderr.String() != "read fingerprint key: input acquisition failed\n" {
		t.Fatal("unsupported production input boundary changed")
	}
}
