//go:build !linux && !windows

package main

import (
	"context"
	"testing"
)

func TestRetestInputUnsupportedPlatform(t *testing.T) {
	for _, kind := range []retestInputKind{retestArtifactInput, retestPublicKeyInput, retestFingerprintKeyInput, retestSigningKeyInput} {
		data, err := readRetestInput(context.Background(), "must-not-be-opened", kind)
		assertRetestInputFailure(t, data, err, false)
	}
}

func TestCaptureInputUnsupportedPlatformStopsBeforeCollection(t *testing.T) {
	h := newCaptureInputHarness(t)
	h.stopped(h.run(t.Context(), readRetestInput, h.write), 0, "read fingerprint key: input acquisition failed\n")
}
