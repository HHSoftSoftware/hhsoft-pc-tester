package workerexec

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestReaperRetainsExitAndCleanupFailure(t *testing.T) {
	cleanupFailure := errors.New("synthetic cleanup failure")
	exitFailure := errors.New("synthetic exit failure")
	done := make(chan processExit, 1)
	done <- processExit{exitCode: 17, err: exitFailure}
	result := awaitReaper(done, time.Now().Add(-time.Second), cleanupFailure)
	if result.exitCode != 17 || !errors.Is(result.err, cleanupFailure) || !errors.Is(result.err, exitFailure) {
		t.Fatalf("completion = %#v", result)
	}
}

func TestUnfinishedReaperReturnsBoundedUncertainty(t *testing.T) {
	done := make(chan processExit, 1)
	started := time.Now()
	result := awaitReaper(done, started.Add(20*time.Millisecond), nil)
	if !errors.Is(result.err, ErrCleanupUncertain) || result.exitCode != -1 || time.Since(started) > time.Second {
		t.Fatalf("completion = %#v, elapsed = %v", result, time.Since(started))
	}
	// The eventual owned reaper can finish after the caller has returned.
	done <- processExit{exitCode: 0}
}

func TestOutputDrainDeadlineRetainsBoundedEvidence(t *testing.T) {
	capture, err := newOutputCapture(4, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.close()
	_, _ = capture.stdout.Write([]byte("ABCDEFG"))
	_, _ = capture.stderr.Write([]byte("012345"))
	started := time.Now()
	if err := capture.finish(started.Add(20 * time.Millisecond)); !errors.Is(err, ErrCleanupUncertain) {
		t.Fatalf("drain error = %v", err)
	}
	result := capture.result(-1)
	if time.Since(started) > time.Second || string(result.Stdout) != "ABCD" || string(result.Stderr) != "012" ||
		!result.StdoutTruncated || !result.StderrTruncated {
		t.Fatalf("drain result = %#v, elapsed = %v", result, time.Since(started))
	}
	if _, err := capture.stdoutWrite.Write([]byte("x")); err == nil {
		t.Fatal("drain deadline did not close the owned pipe")
	}
}

func TestOutputDrainFailureCannotReturnSuccess(t *testing.T) {
	capture, err := newOutputCapture(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.close()
	failure := errors.New("synthetic pipe failure")
	capture.done <- failure
	capture.done <- nil
	if err := capture.finish(time.Now().Add(-time.Second)); !errors.Is(err, ErrCleanupUncertain) || !errors.Is(err, failure) {
		t.Fatalf("drain error = %v", err)
	}
}

func TestBoundedSnapshotsAreIndependentDuringWrites(t *testing.T) {
	buffer := newBoundedBuffer(16)
	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		for index := 0; index < 500; index++ {
			_, _ = buffer.Write([]byte("ABCD"))
		}
	}()
	for index := 0; index < 500; index++ {
		copy, _ := buffer.snapshot()
		if len(copy) > 16 {
			t.Fatal("snapshot exceeded its limit")
		}
		for index := range copy {
			copy[index] = 'x'
		}
	}
	writers.Wait()
	copy, truncated := buffer.snapshot()
	if string(copy) != "ABCDABCDABCDABCD" || !truncated {
		t.Fatalf("snapshot = %q, truncated = %v", copy, truncated)
	}
}
