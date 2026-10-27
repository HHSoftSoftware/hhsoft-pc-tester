package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
)

func TestSessionConsoleOwnerChecksCleanupOnEarlyReturns(t *testing.T) {
	for _, phase := range []string{"arguments", "missing-plan", "stale-evidence", "cancelled"} {
		for _, receipt := range []string{"clean", "observation-error", "cleanup-uncertain"} {
			t.Run(phase+"/"+receipt, func(t *testing.T) {
				h := newSessionHarness(t, "x86_64", nil)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				arguments, want := h.arguments, 1
				switch phase {
				case "arguments":
					arguments, want = nil, 2
				case "missing-plan":
					arguments = append([]string(nil), h.arguments...)
					arguments[1] = filepath.Join(h.directory, "missing-plan.json")
				case "stale-evidence":
					writeSessionFixture(t, filepath.Join(h.directory, "keyboard.json"), "preserved")
				case "cancelled":
					cancel()
				}
				var finishErr error
				switch receipt {
				case "observation-error":
					finishErr = errSessionInput
				case "cleanup-uncertain":
					finishErr = errors.Join(errSessionInput, errSessionInputCleanup)
					want = workerCleanupUncertainExitCode
				}
				prepared, finished := 0, 0
				prepare := func(ctx context.Context, input io.Reader) (context.Context, io.Reader, func() error, error) {
					prepared++
					return ctx, input, sync.OnceValue(func() error { finished++; return finishErr }), nil
				}
				code := runSessionWithInput(ctx, arguments, strings.NewReader(""), &h.stdout, &h.stderr, prepare)
				if code != want || prepared != 1 || finished != 1 {
					t.Fatalf("early return lost its console receipt: code=%d prepare=%d finish=%d", code, prepared, finished)
				}
				for _, name := range []string{"session-safety-monitor.json", "session-thermal-monitor.json", "session-ready.txt", "session.hhpcr"} {
					if _, err := os.Stat(filepath.Join(h.directory, name)); !os.IsNotExist(err) {
						t.Fatal("early return started monitoring or finalization")
					}
				}
			})
		}
	}
}

func TestSessionConsolePreparationDistinguishesRejectedInputFromFailedRollback(t *testing.T) {
	for _, cleanup := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected-input", true: "failed-rollback"}[cleanup], func(t *testing.T) {
			prepareErr, want := errSessionInput, 1
			if cleanup {
				prepareErr = errors.Join(prepareErr, errSessionInputCleanup)
				want = workerCleanupUncertainExitCode
			}
			prepared, finished := 0, 0
			prepare := func(ctx context.Context, input io.Reader) (context.Context, io.Reader, func() error, error) {
				prepared++
				return ctx, input, func() error { finished++; return nil }, prepareErr
			}
			var out, diagnostic bytes.Buffer
			if code := runSessionWithInput(t.Context(), nil, strings.NewReader(""), &out, &diagnostic, prepare); code != want || prepared != 1 || finished != 0 {
				t.Fatalf("setup rejection lost its cleanup classification: code=%d prepare=%d finish=%d", code, prepared, finished)
			}
		})
	}
}

func TestSessionConsoleCleanupOverridesEarlierFailureBeforeMonitorJoin(t *testing.T) {
	for _, phase := range []string{"complete", "preflight", "thermal-start", "keyboard-evidence", "worker-cleanup"} {
		for _, receipt := range []string{"clean", "observation-error", "cleanup-uncertain"} {
			t.Run(phase+"/"+receipt, func(t *testing.T) {
				h := newSessionHarness(t, "x86_64", nil)
				var finishErr error
				want := 1
				switch receipt {
				case "clean":
					if phase == "complete" {
						want = 0
					}
				case "observation-error":
					finishErr = errSessionInput
				case "cleanup-uncertain":
					finishErr = errors.Join(errSessionInput, errSessionInputCleanup)
					want = workerCleanupUncertainExitCode
				}
				if phase == "worker-cleanup" {
					want = workerCleanupUncertainExitCode
				}
				finished := 0
				var inputReleased, joinedTooSoon atomic.Bool
				h.dependencies.FinishInput = func() error {
					finished++
					h.inputFinished = true
					inputReleased.Store(true)
					return finishErr
				}
				monitor := &sessionMonitorFixture{result: func() safemode.MonitorResult {
					if !inputReleased.Load() {
						joinedTooSoon.Store(true)
					}
					return sessionMonitorSummary(0)
				}}
				h.dependencies.StartMonitor = func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error) { return monitor, nil }
				thermalMonitor := &sessionThermalFixture{clock: func() int64 { return 0 }, finding: make(chan struct{})}
				h.dependencies.StartThermal = func(time.Time, context.CancelFunc, context.CancelFunc) (sessionThermalMonitor, error) {
					if phase == "thermal-start" {
						return nil, errors.New("synthetic thermal startup failure")
					}
					return thermalMonitor, nil
				}
				observe := h.dependencies.ObserveSafety
				h.dependencies.ObserveSafety = func(ctx context.Context) safemode.Result {
					value := observe(ctx)
					if phase == "preflight" {
						value.Status = "error"
					}
					return value
				}
				run := h.dependencies.RunWorker
				h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
					if phase == "worker-cleanup" {
						return workerCleanupUncertainExitCode
					}
					if phase == "keyboard-evidence" && worker.ID == "input.keyboard.quick" {
						writeSessionFixture(t, worker.OutputPath, map[string]string{"schema": "invalid"})
						return 1
					}
					return run(ctx, worker, input, out, diagnostic)
				}
				code := h.run(t.Context())
				wantThermalStops := 1
				if phase == "thermal-start" {
					wantThermalStops = 0
				}
				if code != want || finished != 1 || joinedTooSoon.Load() || monitor.stops != 1 || thermalMonitor.stops != wantThermalStops || h.finalized != (want == 0) {
					t.Fatalf("console receipt or monitor ownership lost: code=%d finish=%d safety=%d thermal=%d finalized=%t", code, finished, monitor.stops, thermalMonitor.stops, h.finalized)
				}
				if want != 0 {
					if _, err := os.Stat(filepath.Join(h.directory, "session-ready.txt")); !os.IsNotExist(err) {
						t.Fatal("uncertain or failed input reached signed-report readiness")
					}
				}
			})
		}
	}
}
