//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

type inspectionBrokerForwarder struct {
	console  *sessionConsole
	requests chan inspectionControlledRequest
}
type inspectionControlledRequest struct {
	barrier *inspectionSelectBarrier
	cancel  context.CancelFunc
}

func (f *inspectionBrokerForwarder) Read(p []byte) (int, error) { return f.console.Read(p) }
func (f *inspectionBrokerForwarder) DiscardUnpromptedInput(ctx context.Context) error {
	return f.console.DiscardUnpromptedInput(ctx)
}
func (f *inspectionBrokerForwarder) ReadFindingLine(ctx context.Context, limit int) (string, error) {
	request, cancel := context.WithCancel(ctx)
	defer cancel()
	barrier := &inspectionSelectBarrier{Context: request, entered: make(chan struct{}), release: make(chan struct{})}
	f.requests <- inspectionControlledRequest{barrier, cancel}
	// Only the real broker receives the wrapper. Its first Done call is the
	// outer select operand, after that select captured the original channel.
	return f.console.ReadFindingLine(barrier, limit)
}

func TestInspectionRealBrokerFaultReachesCoordinatorAndSignedReport(t *testing.T) {
	for _, test := range []struct{ name, input, status, finding, stop string }{
		{"queued_overflow", strings.Repeat("x", 4097), "error", "not_inspected", "input_error"},
		{"queued_defect", "D\n", "fail", "defect_observed", "operator_cancelled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, _ := newInspectionCoordinator(t)
			reader, writer := sessionPipe(t)
			flags := inputFlags(t, reader)
			owned, input, finish, err := prepareSessionInput(t.Context(), reader)
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			console := input.(*sessionConsole)
			forward := &inspectionBrokerForwarder{console: console, requests: make(chan inspectionControlledRequest, 1)}
			var actualFinish error
			finishes := 0
			h.dependencies.FinishInput = func() error {
				finishes++
				actualFinish = finish()
				h.inputFinished = actualFinish == nil
				return actualFinish
			}
			h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, diagnostic io.Writer) int {
				if w.ID != "inspection.exterior.quick" || in != forward {
					t.Fatal("unexpected coordinator dispatch")
				}
				h.calls = append(h.calls, w.ID)
				return runSessionWorker(ctx, w, in, out, diagnostic)
			}
			useRealSessionFinalizer(t, h, func(ctx context.Context) (inventory.Snapshot, error) {
				if ctx.Err() != nil || finishes != 1 || actualFinish != nil {
					t.Fatal("actual owner was not clean before collection")
				}
				return h.snapshot, nil
			}, nil)
			done := make(chan int, 1)
			go func() { done <- runSessionWith(owned, h.arguments, forward, &h.stdout, &h.stderr, h.dependencies) }()
			var control inspectionControlledRequest
			select {
			case control = <-forward.requests:
			case <-time.After(3 * time.Second):
				t.Fatal("actual adapter did not request a line")
			}
			select {
			case <-control.barrier.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("real select operand barrier missing")
			}
			released := false
			defer func() {
				if !released {
					close(control.barrier.release)
				}
			}()
			console.mutex.Lock()
			request := console.request
			valid := request != nil && request.ctx == control.barrier && console.failure == nil && !console.stopped && owned.Err() == nil && request.ctx.Err() == nil
			var original chan sessionLineResult
			replacement := make(chan sessionLineResult, 1)
			if valid {
				original = request.result
				valid = len(original) == 0 && cap(original) == 1
				if valid {
					request.result = replacement
				}
			}
			console.mutex.Unlock()
			if !valid || control.barrier.doneCalls.Load() != 1 {
				t.Fatal("captured old-channel prerequisite failed")
			}
			if n, err := writer.WriteString(test.input); err != nil || n != len(test.input) {
				t.Fatal("synthetic input failed")
			}
			inspectionAwait(t, func() bool {
				console.mutex.Lock()
				defer console.mutex.Unlock()
				return console.request == nil && len(replacement) == 1 && len(original) == 0 && console.failure == nil && owned.Err() == nil && request.ctx.Err() == nil
			})
			console.mutex.Lock()
			if test.name == "queued_overflow" && len(request.text) != 4096 {
				t.Error("real bounded overflow was not reached")
			}
			console.mutex.Unlock()
			control.cancel()
			if control.barrier.Err() != context.Canceled || len(original) != 0 || len(replacement) != 1 {
				t.Fatal("cancellation was not the sole ready outer branch")
			}
			close(control.barrier.release)
			released = true
			select {
			case code := <-done:
				if code != 0 || !h.finalized || finishes != 1 || actualFinish != nil || control.barrier.doneCalls.Load() != 1 || len(original) != 0 || len(replacement) != 0 {
					t.Fatal("actual cancellation branch or clean owner chain failed", code)
				}
			case <-time.After(6 * time.Second):
				t.Fatal("coordinator did not finish")
			}
			inspectSignedCoordinator(t, h, test.status, test.finding, test.stop, test.name == "queued_defect")
			if inputFlags(t, reader) != flags {
				t.Fatal("real owner did not restore descriptor flags")
			}
			if _, err := writer.WriteString("later\n"); err != nil {
				t.Fatal(err)
			}
			if err := reader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			later := make([]byte, 6)
			if _, err := io.ReadFull(reader, later); err != nil || string(later) != "later\n" {
				t.Fatal("listener survived owner finish")
			}
		})
	}
}

func TestInspectionCoordinatorConsumesActualPersistentOwnerFailure(t *testing.T) {
	for _, name := range []string{"eof", "restore"} {
		t.Run(name, func(t *testing.T) {
			h, _ := newInspectionCoordinator(t)
			reader, writer := sessionPipe(t)
			owned, input, finish, err := prepareSessionInput(t.Context(), reader)
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			console := input.(*sessionConsole)
			finishes := 0
			var finishErr error
			h.dependencies.FinishInput = func() error { finishes++; finishErr = finish(); return finishErr }
			h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, diagnostic io.Writer) int {
				if w.ID != "inspection.exterior.quick" || in != console {
					t.Fatal("unexpected coordinator dispatch")
				}
				h.calls = append(h.calls, w.ID)
				return runSessionWorker(ctx, w, in, out, diagnostic)
			}
			h.dependencies.Finalize = func(context.Context, sessionFinalization, io.Writer, io.Writer) sessionFinalizationResult {
				t.Fatal("persistent owner fault reached signer")
				return sessionFinalizationResult{Code: 1}
			}
			done := make(chan int, 1)
			go func() { done <- runSessionWith(owned, h.arguments, console, &h.stdout, &h.stderr, h.dependencies) }()
			pendingSessionLine(t, console)
			if name == "eof" {
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := reader.Close(); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case code := <-done:
				if finishes != 1 || finishErr == nil {
					t.Fatal("actual failed finish receipt was not consumed")
				}
				if name == "eof" {
					if code != 1 || !errors.Is(finishErr, errSessionInput) {
						t.Fatal("EOF owner fault lost", code)
					}
				} else if code != workerCleanupUncertainExitCode || !errors.Is(finishErr, errSessionInputCleanup) {
					t.Fatal("restore uncertainty lost", code)
				}
			case <-time.After(6 * time.Second):
				t.Fatal("failed owner did not join")
			}
			for _, file := range []string{"session.hhpcr", "session-ready.txt"} {
				if _, err := os.Lstat(filepath.Join(h.directory, file)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("persistent owner fault acquired report credit")
				}
			}
		})
	}
}
