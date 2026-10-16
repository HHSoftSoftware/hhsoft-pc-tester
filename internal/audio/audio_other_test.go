//go:build !linux

package audio

import (
	"context"
	"testing"
)

func TestRunUnsupportedPlatform(t *testing.T) {
	result := Run(t.Context(), Options{Root: t.TempDir()})
	if result.Schema != Schema || result.Status != StatusError || result.Readiness != ReadinessUnknown || result.MediaStreamsOpened {
		t.Fatalf("unexpected platform result: %#v", result)
	}
}

func TestRunCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := Run(ctx, Options{})
	if result.Status != StatusAborted || result.Readiness != ReadinessUnknown || result.MediaStreamsOpened {
		t.Fatalf("unexpected cancellation result: %#v", result)
	}
}
