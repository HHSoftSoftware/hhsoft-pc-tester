//go:build linux || windows

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func TestCaptureInputRealFilesAndPreservation(t *testing.T) {
	for _, test := range []struct {
		name       string
		slot, size int
	}{
		{"valid", -1, 0}, {"short-fingerprint", 0, 31}, {"long-fingerprint", 0, 33},
		{"short-signing", 1, 63}, {"long-signing", 1, 65},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newCaptureInputHarness(t)
			if test.slot >= 0 {
				h.keys[test.slot] = bytes.Repeat([]byte{0x19}, test.size)
			}
			directory := t.TempDir()
			paths := []string{filepath.Join(directory, "fingerprint"), filepath.Join(directory, "signing")}
			for i, path := range paths {
				if err := os.WriteFile(path, h.keys[i], 0600); err != nil {
					t.Fatal(err)
				}
			}
			h.args[1], h.args[3] = paths[0], paths[1]
			var kinds []retestInputKind
			read := func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
				index := len(kinds)
				if index >= len(paths) || path != paths[index] {
					t.Fatal("wrong key acquisition order")
				}
				kinds = append(kinds, kind)
				return readRetestInput(ctx, path, kind)
			}
			code := h.run(t.Context(), read, h.write)
			wantKinds := []retestInputKind{retestFingerprintKeyInput, retestSigningKeyInput}
			if test.slot == 0 {
				wantKinds = wantKinds[:1]
			}
			if !reflect.DeepEqual(kinds, wantKinds) {
				t.Fatal("real adapter kind changed")
			}
			if test.slot >= 0 {
				h.stopped(code, 0, captureInputDiagnostic(test.slot))
			} else {
				if code != 0 || h.stderr.Len() != 0 || h.stdout.Len() != 0 {
					t.Fatal("real key capture failed", code, h.stderr.String())
				}
				public := ed25519.PrivateKey(h.keys[1]).Public().(ed25519.PublicKey)
				if _, err := bundle.Verify(bytes.NewReader(h.output.Bytes()), int64(h.output.Len()), report.Keyring{"capture-fixture-key": public}); err != nil {
					t.Fatal(err)
				}
			}
			for i, path := range paths {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, h.keys[i]) {
					t.Fatal("capture modified input bytes", err)
				}
			}
		})
	}
}
