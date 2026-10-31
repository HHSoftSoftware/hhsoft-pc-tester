//go:build !linux

package inputtest

import (
	"context"
	"testing"
	"time"
)

func TestTouchscreenOtherPlatformIsExplicitAndClean(t *testing.T) {
	r := RunTouchscreen(t.Context(), TouchscreenOptions{Duration: 45 * time.Second})
	if r.Schema != TouchscreenSchema || r.Status != StatusInconclusive || r.StopReason != "unsupported_platform" ||
		!r.CleanupConfirmed || r.Devices == nil || len(r.Devices) != 0 || r.LimitMilliseconds != 45000 {
		t.Fatal("unsupported platform was hidden or reported as functional success")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r = RunTouchscreen(ctx, TouchscreenOptions{Duration: 45 * time.Second})
	if r.Status != StatusAborted || r.StopReason != "operator_cancelled" || !r.CleanupConfirmed {
		t.Fatal("unsupported platform lost clean cancellation")
	}
	r = RunTouchscreen(t.Context(), TouchscreenOptions{Duration: 120*time.Second + time.Millisecond})
	if r.Status != StatusError || r.Detail != "invalid_options" || r.LimitMilliseconds != 0 {
		t.Fatal("worker maximum duration was not bounded")
	}
}
