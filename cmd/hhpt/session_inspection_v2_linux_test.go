//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestExteriorConditionConsoleOwnsOneBufferedBatch(t *testing.T) {
	for _, test := range []struct {
		name, input string
		wantCount   int
		wantErr     bool
	}{
		{"lf", "N\nA\nN\nN\n", 4, false},
		{"crlf", "N\r\nA\r\nN\r\nN\r\n", 4, false},
		{"overlong", "D\n" + strings.Repeat("x", 4097), 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader, writer := sessionPipe(t)
			flags := inputFlags(t, reader)
			ctx, input, finish, err := prepareSessionInput(t.Context(), reader)
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			console := input.(*sessionConsole)
			bounded, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			prompts := 0
			responses, err := console.ReadInspectionConditions(bounded, func() error {
				prompts++
				console.mutex.Lock()
				armed := console.conditionRequest != nil && console.request == nil
				console.mutex.Unlock()
				if !armed {
					t.Fatal("batch not armed before prompt")
				}
				_, err := writer.WriteString(test.input + "future\n")
				return err
			})
			complete := 0
			for _, response := range responses {
				if response.Complete {
					complete++
				}
			}
			if prompts != 1 || complete != test.wantCount || (err != nil) != test.wantErr || len(responses) > 4 {
				t.Fatal("buffered batch lost or expanded")
			}
			if test.wantErr {
				if len(responses) != 2 || responses[0].Text != "D" || responses[1].Complete || responses[1].Text != "" {
					t.Fatal("fault lost completed prefix or exposed partial input")
				}
			} else if len(responses) != 4 || responses[0].Text != "N" || responses[1].Text != "A" || responses[2].Text != "N" || responses[3].Text != "N" {
				t.Fatal("CRLF shifted criteria")
			}
			if err := finish(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-console.done:
			default:
				t.Fatal("console reader survived finish")
			}
			if inputFlags(t, reader) != flags {
				t.Fatal("owner flags not restored")
			}
			_ = reader.SetReadDeadline(time.Now().Add(time.Second))
			future := make([]byte, 7)
			if _, err := io.ReadFull(reader, future); err != nil || string(future) != "future\n" {
				t.Fatal("finished owner consumed future input")
			}
		})
	}
}

func TestExteriorConditionConsoleCancellationRetainsOnlyCompletePrefix(t *testing.T) {
	reader, writer := sessionPipe(t)
	ctx, input, finish, err := prepareSessionInput(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	console := input.(*sessionConsole)
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan sessionConditionResult, 1)
	go func() {
		responses, err := console.ReadInspectionConditions(requestCtx, func() error { _, err := writer.WriteString("D\npartial"); return err })
		done <- sessionConditionResult{responses: responses, err: err}
	}()
	deadline := time.Now().Add(time.Second)
	reached := false
	for time.Now().Before(deadline) {
		console.mutex.Lock()
		reached = console.conditionRequest != nil && console.conditionRequest.batch.completed == 1 && len(console.conditionRequest.batch.text) == 7
		console.mutex.Unlock()
		if reached {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !reached {
		t.Fatal("complete-prefix/partial-line prerequisite not reached")
	}
	if _, err := console.ReadFindingLine(ctx, 32); err != errSessionInput {
		t.Fatal("second line owner admitted")
	}
	if err := console.DiscardUnpromptedInput(ctx); !errors.Is(err, errSessionInput) {
		t.Fatal("concurrent discard owner admitted")
	}
	if _, err := console.ReadInspectionConditions(ctx, func() error { t.Fatal("second batch prompted"); return nil }); err != errSessionInput {
		t.Fatal("second batch owner admitted")
	}
	cancel()
	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) || len(result.responses) != 2 ||
			result.responses[0].Text != "D" || !result.responses[0].Complete ||
			result.responses[1].Text != "" || result.responses[1].Complete {
			t.Fatal("cancelled batch lost prefix or credited partial input")
		}
	case <-time.After(time.Second):
		t.Fatal("batch did not stop")
	}
	console.mutex.Lock()
	pending := console.conditionRequest != nil
	console.mutex.Unlock()
	if pending {
		t.Fatal("cancelled request retained")
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-console.done:
	default:
		t.Fatal("batch owner not joined")
	}
}

func TestExteriorConditionConsolePromptFailureDisarmsBatch(t *testing.T) {
	reader, _ := sessionPipe(t)
	ctx, input, finish, err := prepareSessionInput(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	console := input.(*sessionConsole)
	responses, err := console.ReadInspectionConditions(ctx, func() error { return errors.New("synthetic prompt failure") })
	if !errors.Is(err, errInspectionConditionPrompt) || len(responses) != 0 {
		t.Fatal("failed prompt retained responses")
	}
	console.mutex.Lock()
	pending := console.conditionRequest != nil
	console.mutex.Unlock()
	if pending {
		t.Fatal("failed prompt leaked batch request")
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
}

func TestExteriorConditionConsoleResumesOnlyForExplicitNextRequest(t *testing.T) {
	reader, writer := sessionPipe(t)
	ctx, input, finish, err := prepareSessionInput(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	console := input.(*sessionConsole)
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	responses, err := console.ReadInspectionConditions(bounded, func() error { _, err := writer.WriteString("N\r\nA\r\nN\r\nN\r\nD\n"); return err })
	if err != nil || len(responses) != 4 {
		t.Fatal("batch prerequisite failed")
	}
	console.mutex.Lock()
	paused := console.conditionPaused
	console.mutex.Unlock()
	if !paused {
		t.Fatal("owner continued draining future bytes")
	}
	line, err := console.ReadFindingLine(bounded, 32)
	if err != nil || line != "D" {
		t.Fatal("explicit next request lost or shifted future input")
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
}

func TestExteriorConditionConsoleCancellationBeforeLFPreservesPartialBoundary(t *testing.T) {
	reader, writer := sessionPipe(t)
	ctx, input, finish, err := prepareSessionInput(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	console := input.(*sessionConsole)
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan sessionConditionResult, 1)
	go func() {
		responses, err := console.ReadInspectionConditions(requestCtx, func() error { _, err := writer.WriteString("D\nN\r"); return err })
		done <- sessionConditionResult{responses: responses, err: err}
	}()
	deadline := time.Now().Add(time.Second)
	reached := false
	for time.Now().Before(deadline) {
		console.mutex.Lock()
		reached = console.conditionRequest != nil && console.conditionRequest.batch.completed == 1 && console.conditionRequest.batch.pendingCR
		console.mutex.Unlock()
		if reached {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !reached {
		t.Fatal("CR-before-LF prerequisite not reached")
	}
	cancel()
	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) || len(result.responses) != 2 || result.responses[0].Text != "D" ||
			!result.responses[0].Complete || result.responses[1].Complete || result.responses[1].Text != "" {
			t.Fatal("partial CRLF acquired authority")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled batch did not stop")
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString("\nfuture\n"); err != nil {
		t.Fatal(err)
	}
	_ = reader.SetReadDeadline(time.Now().Add(time.Second))
	future := make([]byte, 8)
	if _, err := io.ReadFull(reader, future); err != nil || string(future) != "\nfuture\n" {
		t.Fatal("joined owner consumed later delimiter or future input")
	}
}
