//go:build linux || windows

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistKeysRealRegularFilesRequireEachExactLength(t *testing.T) {
	for index, role := range []string{"fingerprint", "public"} {
		for _, size := range []int{31, 32, 33} {
			t.Run(fmt.Sprintf("%s/%d", role, size), func(t *testing.T) {
				h := newPersistKeyFixture(t, false, true)
				keys := [][]byte{append([]byte(nil), h.key...), append([]byte(nil), h.public...)}
				if size == 31 {
					keys[index] = keys[index][:31]
				}
				if size == 33 {
					keys[index] = append(keys[index], 0)
				}
				for i, path := range []string{h.keyPath, h.publicPath} {
					if err := os.WriteFile(path, keys[i], 0600); err != nil {
						t.Fatal(err)
					}
				}
				h.acquire = func(_ int, ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
					return readRetestInput(ctx, path, kind)
				}
				code := h.run()
				if size == 32 {
					h.success(code, h.stdout.Bytes())
				} else {
					h.rejected(code, persistKeyDiagnostic(index), index+1)
				}
			})
		}
	}
}

func TestPersistKeysRealDispatchRejectsNonRegularFingerprint(t *testing.T) {
	h := newPersistKeyFixture(t, true, false)
	h.argument("-fingerprint-key", t.TempDir())
	h.args[len(h.args)-1] = filepath.Join(t.TempDir(), "missing-report.hhpcr")
	// The missing report is a second barrier: this real dispatch cannot reach media.
	code := runPersist(h.ctx, h.args, &h.stdout, &h.stderr)
	h.rejected(code, "read fingerprint key: input acquisition failed", 0)
}
