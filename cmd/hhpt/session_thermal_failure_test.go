package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
)

func TestSessionThermalFailuresJoinBothOwnersAndBlockKeyGeneration(t *testing.T) {
	for _, fault := range []string{"start", "initial", "during-worker", "during-inventory", "final", "final-expired", "cleanup", "invalid-summary", "diagnostic-save", "journal-save"} {
		t.Run(fault, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			random := &journalForbiddenRandom{}
			safety := &sessionMonitorFixture{result: func() safemode.MonitorResult {
				if !h.inputFinished {
					t.Error("safety owner stopped before input cleanup")
				}
				return sessionMonitorSummary(0)
			}}
			m := &sessionThermalFixture{clock: func() int64 { return 0 }, finding: make(chan struct{})}
			var fail context.CancelFunc
			h.dependencies.StartMonitor = func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error) { return safety, nil }
			h.dependencies.StartThermal = func(_ time.Time, _ context.CancelFunc, terminal context.CancelFunc) (sessionThermalMonitor, error) {
				fail = terminal
				if fault == "start" {
					return nil, errors.New("synthetic start failure")
				}
				if fault == "diagnostic-save" {
					writeSessionFixture(t, filepath.Join(h.directory, "session-thermal-monitor.json"), "retained")
				}
				return m, nil
			}
			if fault == "initial" {
				m.ready = func(context.Context) error { return errors.New("synthetic initial failure") }
			}
			if fault == "final" {
				m.final = func(context.Context) error { return errors.New("synthetic final failure") }
			}
			if fault == "final-expired" {
				m.final = func(ctx context.Context) error { <-ctx.Done(); return nil }
			}
			m.stop = func(context.Context) thermal.Result {
				if !h.inputFinished {
					t.Error("thermal owner stopped before input cleanup")
				}
				r := sessionThermalSummary(0)
				if fault == "cleanup" {
					r.Status = "cleanup_unconfirmed"
					r.Joined = false
					r.CleanupConfirmed = false
					r.FirstFailure = &thermal.FailureRecord{Reason: "cleanup_unconfirmed", Phase: "none"}
				}
				if fault == "invalid-summary" {
					r.Aggregate.AcceptedObservations++
				}
				return r
			}
			worker := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, errOut io.Writer) int {
				code := worker(ctx, w, in, out, errOut)
				if fault == "during-worker" {
					m.failed = true
					fail()
					if ctx.Err() == nil {
						t.Error("thermal failure did not cancel work")
					}
				}
				return code
			}
			useRealSessionFinalizer(t, h, func(ctx context.Context) (inventory.Snapshot, error) {
				if fault == "during-inventory" {
					m.failed = true
					fail()
					if ctx.Err() == nil {
						t.Error("thermal failure did not cancel preparation")
					}
				}
				if fault == "journal-save" {
					writeSessionFixture(t, filepath.Join(h.directory, "session-journal.json"), "retained")
				}
				return h.snapshot, nil
			}, random)
			code := h.run(t.Context())
			want := 1
			if fault == "cleanup" {
				want = workerCleanupUncertainExitCode
			}
			wantStops := 1
			if fault == "start" {
				wantStops = 0
			}
			if code != want || safety.stops != 1 || m.stops != wantStops || random.calls != 0 || !h.inputFinished {
				t.Fatalf("failure escaped ownership: code=%d safety=%d thermal=%d entropy=%d", code, safety.stops, m.stops, random.calls)
			}
			for _, name := range []string{"session.hhpcr", "session-ready.txt"} {
				if _, err := os.Stat(filepath.Join(h.directory, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("uncertain thermal session produced output")
				}
			}
		})
	}
}

func TestSessionCannotAcceptMissingRepeatedOrChangedFinalGate(t *testing.T) {
	for _, fault := range []string{"missing", "repeated", "changed-journal"} {
		t.Run(fault, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			finalize := h.dependencies.Finalize
			h.dependencies.Finalize = func(ctx context.Context, request sessionFinalization, out, errOut io.Writer) sessionFinalizationResult {
				if fault == "missing" {
					return sessionFinalizationResult{}
				}
				result := finalize(ctx, request, out, errOut)
				if fault == "repeated" {
					if _, err := request.Gate(ctx); err == nil {
						t.Fatal("gate accepted repeated use")
					}
				}
				if fault == "changed-journal" {
					result.Journal.BudgetMilliseconds++
				}
				return result
			}
			if code := h.run(t.Context()); code == 0 {
				t.Fatal("invalid live finalization produced readiness")
			}
			if _, err := os.Stat(filepath.Join(h.directory, "session-ready.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid live finalization created ready marker")
			}
		})
	}
}

func TestSessionFinalizerRetainsHeatDiscoveredAtStop(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	useRealSessionFinalizer(t, h, nil, nil)
	h.dependencies.StartThermal = func(time.Time, context.CancelFunc, context.CancelFunc) (sessionThermalMonitor, error) {
		return &sessionThermalFixture{finding: make(chan struct{}), stop: func(context.Context) thermal.Result {
			r := sessionThermalSummary(0)
			hot := *r.Final.Observation
			hot.Coverage = "available"
			hot.Sensors = 1
			hot.PeakMilliCelsius = 97000
			r.Final.Observation = &hot
			r.LastObservation = &hot
			r.Aggregate.Coverage = "partial"
			r.Aggregate.AvailableObservations = 1
			r.Aggregate.UnavailableObservations = 1
			r.Aggregate.MaximumSensors = 1
			r.Aggregate.ValidReadings = 1
			r.Aggregate.PeakMilliCelsius = 97000
			r.FirstFinding = &thermal.FindingRecord{Reason: "thermal_limit", Phase: "final", Observation: hot}
			return r
		}}, nil
	}
	if code := h.run(t.Context()); code != 0 {
		t.Fatalf("late stop heat rejected: %d %s", code, &h.stderr)
	}
	view := verifiedThermalView(t, h)
	if view.Finding != "limit_reached" || view.StopReason != "thermal_limit" {
		t.Fatal("pre-finalization stop reason hid late heat")
	}
}

func TestSessionTerminalFailureCannotCancelItsOwnCleanupReserve(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	random := &journalForbiddenRandom{}
	useRealSessionFinalizer(t, h, nil, random)
	failed := make(chan struct{})
	var terminal context.CancelFunc
	h.dependencies.StartMonitor = func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error) {
		return &joiningSafetyFixture{stop: func(ctx context.Context) safemode.StopReceipt {
			<-failed
			r := sessionMonitorSummary(0)
			r.CleanupConfirmed = ctx.Err() == nil
			return safemode.StopReceipt{Result: r, Joined: r.CleanupConfirmed}
		}}, nil
	}
	h.dependencies.StartThermal = func(_ time.Time, _, fail context.CancelFunc) (sessionThermalMonitor, error) {
		terminal = fail
		return &sessionThermalFixture{finding: make(chan struct{}), stop: func(ctx context.Context) thermal.Result {
			terminal()
			close(failed)
			r := sessionThermalSummary(0)
			r.Status = "failed"
			r.FirstFailure = &thermal.FailureRecord{Reason: "observation_timeout", Phase: "periodic"}
			if ctx.Err() != nil {
				r.Status = "cleanup_unconfirmed"
				r.CleanupConfirmed = false
				r.Joined = false
			}
			return r
		}}, nil
	}
	if code := h.run(t.Context()); code != 1 || random.calls != 0 {
		t.Fatalf("terminal failure manufactured uncertain cleanup: code=%d reads=%d", code, random.calls)
	}
}
