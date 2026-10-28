//go:build !linux && !windows

package main

import "testing"

func TestFinalizerFingerprintUnsupportedAdapterStopsBeforePlan(t *testing.T) {
	h := newFinalizerFingerprintHarness(t)
	result := sessionFinalizationResult{Code: runFinalizeWith(t.Context(), h.args, &h.stdout, &h.stderr, h.collect, h, h.now)}
	h.stopped(result, 0, "read fingerprint key: input acquisition failed\n")
}
