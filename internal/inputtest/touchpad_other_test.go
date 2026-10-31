//go:build !linux

package inputtest

import (
	"context"
	"testing"
	"time"
)

func TestTouchpadOtherPlatformIsExplicitAndClosed(t *testing.T) {
	for _, mode := range []string{"unsupported", "cancelled", "deadline", "nil_context", "short", "long"} {
		t.Run(mode, func(t *testing.T) {
			var ctx context.Context = t.Context()
			duration := time.Second
			if mode == "cancelled" {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if mode == "deadline" {
				c, cancel := context.WithDeadline(ctx, time.Time{})
				defer cancel()
				ctx = c
			}
			if mode == "nil_context" {
				ctx = nil
			}
			if mode == "short" {
				duration = time.Millisecond
			}
			if mode == "long" {
				duration = 91 * time.Second
			}
			r := RunTouchpad(ctx, TouchpadOptions{Duration: duration})
			if r.Schema != TouchpadSchema || !r.CleanupConfirmed || r.Status == StatusPass || len(r.Devices) != 0 {
				t.Fatal("unsupported platform fabricated observation")
			}
			switch mode {
			case "unsupported":
				if r.Status != StatusInconclusive || r.Detail != "unsupported_platform" {
					t.Fatal("unsupported status mismatch")
				}
			case "cancelled":
				if r.Status != StatusAborted || r.StopReason != "operator_cancelled" {
					t.Fatal("cancellation lost")
				}
			case "deadline":
				if r.Status != StatusError || r.StopReason != "deadline" {
					t.Fatal("deadline lost")
				}
			default:
				if r.Status != StatusError || r.Detail != "invalid_options" || r.LimitMilliseconds != 0 {
					t.Fatal("invalid options accepted")
				}
			}
		})
	}
}
