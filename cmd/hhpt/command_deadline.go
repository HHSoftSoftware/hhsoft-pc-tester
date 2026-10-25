package main

import (
	"context"
	"time"
)

// Check the monotonic deadline as well as the cancellation signal: a completed
// response must not become a pass merely because the timer callback ran late.
func commandStopReason(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}
