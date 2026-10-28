//go:build !linux && !windows

package main

import (
	"context"
	"testing"
)

func TestRetestInputUnsupportedPlatform(t *testing.T) {
	data, err := readRetestInput(context.Background(), "must-not-be-opened", retestArtifactInput)
	assertRetestInputFailure(t, data, err, false)
}
