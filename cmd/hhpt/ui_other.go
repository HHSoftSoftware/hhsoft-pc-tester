//go:build !linux

package main

import (
	"context"
	"io"
	"time"
)

func waitForOperatorInteraction(ctx context.Context, timeout time.Duration, input io.Reader) (bool, error) {
	result := make(chan error, 1)
	go func() {
		var singleByte [1]byte
		_, err := input.Read(singleByte[:])
		result <- err
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err == nil, err
	case <-ctx.Done():
		return false, ctx.Err()
	case <-timer.C:
		return false, nil
	}
}
