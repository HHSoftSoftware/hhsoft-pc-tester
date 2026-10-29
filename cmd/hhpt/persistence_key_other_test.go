//go:build !linux && !windows

package main

import (
	"context"
	"os"
	"testing"
)

func TestPersistKeysOtherPlatformProductionAcquisitionIsRejected(t *testing.T) {
	for index, role := range []string{"fingerprint", "public"} {
		t.Run(role, func(t *testing.T) {
			h := newPersistKeyFixture(t, false, false)
			if err := os.WriteFile(h.keyPath, h.key, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(h.publicPath, h.public, 0600); err != nil {
				t.Fatal(err)
			}
			forwardFixture := saveFixtureInput(t)
			h.acquire = func(i int, ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
				if i < index {
					// A test-only ordinary-file adapter establishes the first-key prerequisite.
					return forwardFixture(ctx, path, kind)
				}
				return readRetestInput(ctx, path, kind)
			}
			h.rejected(h.run(), persistKeyDiagnostic(index), index+1)
		})
	}
}
