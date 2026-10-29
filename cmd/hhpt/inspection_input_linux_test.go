//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
)

type inspectionSelectBarrier struct {
	context.Context
	entered, release chan struct{}
	once             sync.Once
	doneCalls        atomic.Int32
}

func (ctx *inspectionSelectBarrier) Done() <-chan struct{} {
	ctx.doneCalls.Add(1)
	ctx.once.Do(func() {
		close(ctx.entered)
		<-ctx.release
	})
	return ctx.Context.Done()
}

func inspectionAwait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("required broker state was not reached")
}

func TestInspectionRealBrokerCancellationPreservesQueuedFault(t *testing.T) {
	for _, test := range []struct{ name, input, finding, reason, status string }{
		{"overflow", strings.Repeat("x", 4097), "not_inspected", "input_error", "error"},
		{"adverse_cr", "D\r", "defect_observed", "operator_cancelled", "fail"},
		{"adverse_lf", "D\n", "defect_observed", "operator_cancelled", "fail"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader, writer := sessionPipe(t)
			flags := inputFlags(t, reader)
			ownerContext, cancelOwner := context.WithCancel(t.Context())
			defer cancelOwner()
			owned, input, finish, err := prepareSessionInput(ownerContext, reader)
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			console := input.(*sessionConsole)
			requestContext, cancelRequest := context.WithCancel(t.Context())
			defer cancelRequest()
			barrier := &inspectionSelectBarrier{Context: requestContext, entered: make(chan struct{}), release: make(chan struct{})}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(barrier.release) }) }
			defer release()
			type answer struct {
				line     string
				complete bool
				err      error
			}
			done := make(chan answer, 1)
			// Do not put the barrier into a new derived context: its first Done
			// call must occur at the broker's real outer select.
			go func() {
				line, complete, err := readInspectionLine(barrier, console)
				done <- answer{line, complete, err}
			}()
			select {
			case <-barrier.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("outer select operand barrier not reached")
			}
			console.mutex.Lock()
			request := console.request
			valid := request != nil && request.ctx == barrier && console.failure == nil &&
				console.discard == nil && !console.stopped && owned.Err() == nil && requestContext.Err() == nil
			var original chan sessionLineResult
			replacement := make(chan sessionLineResult, 1)
			if valid {
				original = request.result
				valid = cap(original) == 1 && len(original) == 0
				if valid {
					request.result = replacement
				}
			}
			console.mutex.Unlock()
			if !valid || barrier.doneCalls.Load() != 1 {
				t.Fatal("live request/captured operand prerequisite missing")
			}

			// The select already captured original before Done blocked. The
			// listener sees replacement under the same mutex. The test neither
			// sends to nor receives from either result channel.
			if n, err := io.WriteString(writer, test.input); err != nil || n != len(test.input) {
				t.Fatal("fixture write failed")
			}
			inspectionAwait(t, func() bool {
				console.mutex.Lock()
				defer console.mutex.Unlock()
				return console.request == nil && len(replacement) == 1 && len(original) == 0 &&
					console.failure == nil && !console.stopped && requestContext.Err() == nil && owned.Err() == nil
			})
			console.mutex.Lock()
			if test.name == "overflow" && len(request.text) != 4096 {
				t.Error("real overflow limit not reached")
			}
			console.mutex.Unlock()
			cancelRequest()
			if requestContext.Err() != context.Canceled || len(original) != 0 || len(replacement) != 1 {
				t.Fatal("cancellation readiness prerequisite missing")
			}
			release()
			var result answer
			select {
			case result = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("forced cancellation branch did not return")
			}
			if barrier.doneCalls.Load() != 1 || len(original) != 0 || len(replacement) != 0 {
				t.Fatal("actual inner cancellation drain did not consume the replacement result")
			}
			if !errors.Is(result.err, context.Canceled) {
				t.Fatal("request cancellation lost")
			}
			if test.name == "overflow" {
				if !errors.Is(result.err, errSessionInput) || result.complete || result.line != "" {
					t.Fatal("actual overflow was erased or credited")
				}
			} else if result.err != context.Canceled || !result.complete || result.line != "D" {
				t.Fatal("clean stop identity or completed adverse line changed")
			}
			evidence, err := exteriorinspection.Evaluate(result.line, result.complete, inspectionStop(result.err, requestContext.Err(), false), 17)
			if err != nil || evidence.Finding != test.finding || evidence.StopReason != test.reason || evidence.Status != test.status {
				t.Fatalf("actual input adapter lost fault/adverse evidence: %+v %v", evidence, err)
			}
			if owned.Err() != nil {
				t.Fatal("request cancellation stopped the separate console owner")
			}
			if err := finish(); err != nil {
				t.Fatalf("request-local result acquired a persistent owner fault: %v", err)
			}
			if inputFlags(t, reader) != flags {
				t.Fatal("owner flags were not restored")
			}
			if _, err := writer.WriteString("future\n"); err != nil {
				t.Fatal(err)
			}
			if err := reader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			future := make([]byte, 7)
			if _, err := io.ReadFull(reader, future); err != nil || string(future) != "future\n" {
				t.Fatal("listener survived actual owner completion")
			}
		})
	}
}

func TestInspectionRealBrokerDirectResultCounterparts(t *testing.T) {
	for _, test := range []struct {
		name, input, line string
		fault             bool
	}{
		{"negative", "N\n", "N", false},
		{"adverse", "D\r", "D", false},
		{"uncertain", "I\n", "I", false},
		{"overflow", strings.Repeat("x", 4097), "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader, writer := sessionPipe(t)
			owned, input, finish, err := prepareSessionInput(t.Context(), reader)
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			console := input.(*sessionConsole)
			done := make(chan sessionLineResult, 1)
			go func() { line, err := console.ReadFindingLine(owned, 4096); done <- sessionLineResult{line, err} }()
			pendingSessionLine(t, console)
			if n, err := io.WriteString(writer, test.input); err != nil || n != len(test.input) {
				t.Fatal("fixture write failed")
			}
			select {
			case result := <-done:
				if result.line != test.line || test.fault && !errors.Is(result.err, errSessionInput) || !test.fault && result.err != nil {
					t.Fatal("direct result counterpart changed")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("direct result did not complete")
			}
			if err := finish(); err != nil {
				t.Fatal("request-local result dirtied owner cleanup")
			}
		})
	}
}

func TestInspectionActualOwnerFaultPreventsStandaloneOutput(t *testing.T) {
	for _, name := range []string{"eof", "restore", "join_timeout"} {
		t.Run(name, func(t *testing.T) {
			reader, writer := sessionPipe(t)
			prompted := make(chan struct{})
			var promptOnce sync.Once
			prompt := inspectionPromptWriter{write: func(data []byte) (int, error) {
				if strings.Contains(string(data), "Exterior enclosure observation") {
					promptOnce.Do(func() { close(prompted) })
				}
				return len(data), nil
			}}
			var finishErr error
			finishes := 0
			var console *sessionConsole
			prepare := func(ctx context.Context, input io.Reader) (context.Context, io.Reader, func() error, error) {
				owned, source, finish, err := prepareSessionInput(ctx, input)
				if err != nil {
					return owned, source, finish, err
				}
				console = source.(*sessionConsole)
				return owned, source, func() error {
					finishes++
					if name == "join_timeout" {
						console.mutex.Lock()
					}
					finishErr = finish()
					if name == "join_timeout" {
						console.mutex.Unlock()
						select {
						case <-console.done:
						case <-time.After(3 * time.Second):
							t.Error("delayed owner did not subsequently join")
						}
					}
					return finishErr
				}, nil
			}
			var stdout bytes.Buffer
			done := make(chan int, 1)
			go func() {
				done <- runInspectionCommand(t.Context(), nil, reader, &stdout, prompt, false, prepare, writeDestination, time.Now)
			}()
			select {
			case <-prompted:
			case <-time.After(3 * time.Second):
				t.Fatal("real owned prompt not reached")
			}
			switch name {
			case "eof":
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
			case "restore":
				if err := reader.Close(); err != nil {
					t.Fatal(err)
				}
			case "join_timeout":
				pendingSessionLine(t, console)
				if _, err := writer.WriteString("N\n"); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case code := <-done:
				if finishes != 1 || stdout.Len() != 0 || finishErr == nil {
					t.Fatal("failed actual owner receipt permitted output")
				}
				if name == "eof" {
					if code != 1 || !errors.Is(finishErr, errSessionInput) {
						t.Fatal("persistent EOF failure lost")
					}
				} else if code != 12 || !errors.Is(finishErr, errSessionInputCleanup) {
					t.Fatal("actual restore/join uncertainty lost")
				}
			case <-time.After(6 * time.Second):
				t.Fatal("owned command failed to return")
			}
		})
	}
}
