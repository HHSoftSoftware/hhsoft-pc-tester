//go:build linux

package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionDiscardSeparatesQueuedInputFromNextFinding(t *testing.T) {
	reader, writer := sessionPipe(t)
	_, input, finish, err := prepareSessionInput(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	console := input.(*sessionConsole)
	if _, err := writer.Write([]byte("\x1b[A\x1b[B\x1b[C\x1b[Dstale\n")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := discardSessionInput(ctx, input); err != nil {
		t.Fatal(err)
	}
	done := make(chan sessionLineResult, 1)
	go func() { line, err := console.ReadFindingLine(ctx, 32); done <- sessionLineResult{line, err} }()
	pendingSessionLine(t, console)
	if _, err := writer.Write([]byte("p\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil || result.line != "p" {
			t.Fatal("queued keyboard input crossed into a later finding")
		}
	case <-ctx.Done():
		t.Fatal("later finding did not complete")
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	if err := discardSessionInput(ctx, input); err == nil {
		t.Fatal("stopped owner acknowledged a queue boundary")
	}
}

func TestSessionDiscardCannotConsumeActiveFinding(t *testing.T) {
	reader, writer := sessionPipe(t)
	_, input, finish, err := prepareSessionInput(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	console := input.(*sessionConsole)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan sessionLineResult, 1)
	go func() { line, err := console.ReadFindingLine(ctx, 32); done <- sessionLineResult{line, err} }()
	pendingSessionLine(t, console)
	if err := discardSessionInput(ctx, input); err == nil {
		t.Fatal("discard was accepted during a finding request")
	}
	if _, err := writer.Write([]byte("f\n")); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil || result.line != "f" {
		t.Fatal("rejected discard changed the pending finding")
	}
}

func TestSessionDiscardHonorsCancellationAndInputLoss(t *testing.T) {
	for _, stop := range []string{"request", "ctrl-c", "closed-input"} {
		t.Run(stop, func(t *testing.T) {
			reader, writer := sessionPipe(t)
			owned, input, finish, err := prepareSessionInput(t.Context(), reader)
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			switch stop {
			case "request":
				cancel()
			case "ctrl-c":
				if _, err := writer.Write([]byte{3}); err != nil {
					t.Fatal(err)
				}
			case "closed-input":
				_ = writer.Close()
			}
			if err := discardSessionInput(ctx, input); err == nil {
				t.Fatal("canceled or failed input produced a successful handoff")
			} else if stop == "request" && !errors.Is(err, context.Canceled) {
				t.Fatal("request cancellation reason was lost")
			}
			if stop != "request" {
				select {
				case <-owned.Done():
				case <-ctx.Done():
					t.Fatal("input loss did not stop the console owner")
				}
			}
		})
	}
}

func TestSessionConsoleRetainsTypedRestorationFailure(t *testing.T) {
	reader, _ := sessionPipe(t)
	owned, _, finish, err := prepareSessionInput(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	select {
	case <-owned.Done():
	case <-time.After(time.Second):
		t.Fatal("closed owner was not detected")
	}
	first := finish()
	if !errors.Is(first, errSessionInputCleanup) {
		t.Fatal("restoration uncertainty lacked its typed receipt")
	}
	if again := finish(); again != first {
		t.Fatal("repeated cleanup replaced the retained receipt")
	}
}
