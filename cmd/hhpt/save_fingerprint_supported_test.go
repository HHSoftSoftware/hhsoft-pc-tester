//go:build linux || windows

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveFingerprintRealRegularFileSizes(t *testing.T) {
	for _, test := range []struct {
		name string
		size int
	}{{"short", 31}, {"exact", 32}, {"long", 33}} {
		t.Run(test.name, func(t *testing.T) {
			h := newSaveFingerprintFixture(t, true)
			h.input = nil
			h.acquire = readRetestInput
			key := append(append([]byte(nil), h.key...), 1)[:test.size]
			if err := os.WriteFile(h.keyPath, key, 0600); err != nil {
				t.Fatal(err)
			}
			code := h.run("1\nRPT-15B6C5\n")
			if test.size == 32 {
				h.savedSuccessfully(code, 1)
			} else {
				h.rejected(code, "read fingerprint key: input acquisition failed", 1, 1, 1)
			}
		})
	}
}

func TestSaveFingerprintRealDispatchRejectsNonRegularInput(t *testing.T) {
	directory := t.TempDir()
	keyPath := filepath.Join(directory, "directory-key")
	if err := os.Mkdir(keyPath, 0700); err != nil {
		t.Fatal(err)
	}
	// A missing source is a second barrier against any real target discovery.
	// The distinct key diagnostic still proves which boundary rejected first.
	arguments := []string{"save", "-fingerprint-key", keyPath, filepath.Join(directory, "absent.hhpcr")}
	input := &saveFingerprintConsole{reader: strings.NewReader("1\nRPT-15B6C5\n")}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), arguments, input, &stdout, &stderr)
	if code != 1 || input.reads != 0 || stdout.Len() != 0 || stderr.String() != "read fingerprint key: input acquisition failed\n" {
		t.Fatalf("production dispatch input rejection: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), keyPath) {
		t.Fatal("production input diagnostic exposed the pathname")
	}
}
