package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
)

// These are independently specified evidence fixtures, not producer output.
// I256/C24 includes the second scalar sweep before an uncredited recurrence.
func commandStressPass() quick.CPUStressResult {
	start, ready, stop := int64(1), int64(1), int64(600001)
	return quick.CPUStressResult{
		Schema: quick.CPUStressSchema, Status: quick.StatusPass, StopReason: "completed",
		RequestedLoadMilliseconds: 600000, LocalWatchdogMilliseconds: 660000,
		ThermalPolicy: "available-observations/v1", WorkerSelection: "explicit",
		WorkersRequested: 1, WorkersLaunched: 1, WorkersReady: 1,
		EffectiveDeadlineMilliseconds: 660000, EffectiveDeadlineSource: "watchdog",
		LoadStarted: true, LoadStartedMilliseconds: &start, LoadStoppedMilliseconds: &stop,
		ObservedLoadMilliseconds: 600000, WorkersJoinedMilliseconds: 600002, FinishedMilliseconds: 600003,
		Iterations: 256, ChecksPassed: 24,
		KnownAnswerSHA256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		ScalarSuite:       "binary64-normal-exact-v1", ScalarVectorCount: 12, AccountingValid: true,
		StopFacts: quick.CPUStressStopFacts{NormalLoadComplete: true},
		Safety: quick.SafetyEvidence{GlobalTimeoutMilliseconds: 660000, ThermalLimitMilliCelsius: 90000,
			ThermalSensors: 1, ThermalSamples: 2, PeakMilliCelsius: 41000, MonitorStatus: "completed",
			CleanupConfirmed: true, ThermalObservations: 2, ThermalCoverage: "available"},
		Workers: []quick.CPUStressWorker{{Index: 0, StartedMilliseconds: 0, ReadyMilliseconds: &ready,
			LoadEnteredMilliseconds: &start, StoppedMilliseconds: stop, ExitReason: "completed",
			PreparationIterations: 1, PreparationChecksPassed: 12, Iterations: 256, ChecksPassed: 24,
			LoadIterations: 255, LoadChecksPassed: 12, FailurePhase: "none"}},
	}
}

func commandStressNoLoad() quick.CPUStressResult {
	value := commandStressPass()
	value.Status, value.StopReason = quick.StatusInconclusive, "insufficient_budget"
	value.EffectiveDeadlineMilliseconds, value.EffectiveDeadlineSource = 600000, "parent"
	value.WorkersLaunched, value.WorkersReady = 0, 0
	value.LoadStarted, value.LoadStartedMilliseconds, value.LoadStoppedMilliseconds = false, nil, nil
	value.ObservedLoadMilliseconds, value.WorkersJoinedMilliseconds, value.FinishedMilliseconds = 0, 0, 0
	value.Iterations, value.ChecksPassed = 0, 0
	value.Workers = []quick.CPUStressWorker{}
	value.StopFacts = quick.CPUStressStopFacts{InsufficientBudget: true, LaunchIncomplete: true, ReadinessIncomplete: true}
	value.Safety = quick.SafetyEvidence{GlobalTimeoutMilliseconds: 660000, ThermalLimitMilliCelsius: 90000,
		MonitorStatus: "not_started", CleanupConfirmed: true, ThermalCoverage: "unavailable"}
	return value
}

func stressFixtureBytes(t *testing.T, value quick.CPUStressResult) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal("fixture encoding failed")
	}
	if _, _, err := quickevidence.DecodeCPUStress(raw); err != nil {
		t.Fatal("independent stress prerequisite rejected", err)
	}
	return raw
}

func TestCPUStressCommandExplicitOptionsAndStrictEmission(t *testing.T) {
	for _, test := range []struct {
		name    string
		workers int
		value   quick.CPUStressResult
	}{
		{"pass", 1, commandStressPass()}, {"no_load_inconclusive", 0, commandStressNoLoad()},
	} {
		t.Run(test.name, func(t *testing.T) {
			stressFixtureBytes(t, test.value)
			var out, diagnostic bytes.Buffer
			calls := 0
			args := []string{}
			if test.workers != 0 {
				args = []string{"-workers", "1"}
			}
			probe := func(ctx context.Context, options quick.CPUStressOptions) (quick.CPUStressResult, error) {
				calls++
				if ctx != t.Context() || options.Workers != test.workers || options.Root != "/" {
					t.Fatal("command changed options or context")
				}
				return test.value, nil
			}
			if code := runCPUStressWithDestination(t.Context(), args, &out, &diagnostic, probe, writeDestination); code != 0 || calls != 1 || diagnostic.Len() != 0 {
				t.Fatalf("valid diagnostic failed: code=%d calls=%d", code, calls)
			}
			parsed, _, err := quickevidence.DecodeCPUStress(out.Bytes())
			if err != nil || parsed.Status != test.value.Status {
				t.Fatal("diagnostic status changed")
			}
		})
	}
}

func TestCPUStressCommandRejectsOverridesBeforeProbe(t *testing.T) {
	for _, args := range [][]string{{"-workers", "-1"}, {"-workers", "65"}, {"-duration", "1s"}, {"-timeout", "1s"}, {"-thermal-limit-c", "100"}, {"-root", "/fixture"}, {"extra"}, {"-output", ""}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			calls := 0
			probe := func(context.Context, quick.CPUStressOptions) (quick.CPUStressResult, error) {
				calls++
				return commandStressPass(), nil
			}
			var out, diagnostic bytes.Buffer
			if code := runCPUStressWithDestination(t.Context(), args, &out, &diagnostic, probe, writeDestination); code != 2 || calls != 0 || out.Len() != 0 {
				t.Fatal("invalid command started workload or emitted evidence")
			}
		})
	}
}

func TestCPUStressCommandErrorsCannotEmitOrHideCleanup(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code int
	}{
		{"options", quick.ErrCPUStressOptions, 2},
		{"accounting", quick.ErrCPUStressAccounting, 1},
		{"unknown", errors.New("private diagnostic marker"), 1},
		{"cleanup", quick.ErrCPUStressCleanupUnconfirmed, 12},
		{"cleanup_and_options", errors.Join(quick.ErrCPUStressOptions, quick.ErrCPUStressCleanupUnconfirmed), 12},
		{"cleanup_and_accounting", errors.Join(quick.ErrCPUStressAccounting, quick.ErrCPUStressCleanupUnconfirmed), 12},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			writes := 0
			writer := func(string, io.Writer, func(io.Writer) error) error {
				writes++
				return errors.New("private output marker")
			}
			code := runCPUStressWithDestination(t.Context(), nil, &out, &diagnostic,
				func(context.Context, quick.CPUStressOptions) (quick.CPUStressResult, error) {
					return quick.CPUStressResult{}, test.err
				}, writer)
			if code != test.code || writes != 0 || out.Len() != 0 || strings.Contains(diagnostic.String(), "private") {
				t.Fatal("fatal diagnostic escaped or output was attempted")
			}
		})
	}
}

func TestCPUStressCommandValidatesBeforeExclusiveOutput(t *testing.T) {
	valid := commandStressPass()
	stressFixtureBytes(t, valid)
	for _, mode := range []string{"bad_duration", "missing_rows", "counter", "unknown_reason", "existing_file", "short_write", "output_error"} {
		t.Run(mode, func(t *testing.T) {
			value := commandStressPass()
			switch mode {
			case "bad_duration":
				value.ObservedLoadMilliseconds--
			case "missing_rows":
				value.Workers = nil
			case "counter":
				value.ChecksPassed++
			case "unknown_reason":
				value.StopReason = "private reason"
			}
			var out, diagnostic bytes.Buffer
			writes := 0
			writer := destinationWriter(func(_ string, out io.Writer, emit func(io.Writer) error) error {
				writes++
				if mode == "output_error" {
					return errors.New("private output marker")
				}
				if mode == "short_write" {
					return emit(stressShortWriter{})
				}
				return emit(out)
			})
			args := []string{}
			if mode == "existing_file" {
				path := filepath.Join(t.TempDir(), "stress.json")
				if os.WriteFile(path, []byte("retained"), 0600) != nil {
					t.Fatal("fixture")
				}
				writer, args = writeDestination, []string{"-output", path}
			}
			code := runCPUStressWithDestination(t.Context(), args, &out, &diagnostic,
				func(context.Context, quick.CPUStressOptions) (quick.CPUStressResult, error) { return value, nil }, writer)
			if code != 1 || strings.Contains(diagnostic.String(), "private") {
				t.Fatal("invalid evidence or failed output accepted")
			}
			if mode == "bad_duration" || mode == "missing_rows" || mode == "counter" || mode == "unknown_reason" {
				if writes != 0 {
					t.Fatal("validation followed output")
				}
			}
			if (mode == "short_write" || mode == "output_error") && writes != 1 {
				t.Fatal("output failure fixture did not reach the writer")
			}
			if mode == "existing_file" {
				raw, err := os.ReadFile(args[1])
				if err != nil || string(raw) != "retained" {
					t.Fatal("existing file changed")
				}
			}
		})
	}
}

type stressShortWriter struct{}

func (stressShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestCPUStressDispatchRejectsMissingAndInvalidEvidence(t *testing.T) {
	raw := stressFixtureBytes(t, commandStressNoLoad())
	for _, interrupted := range []bool{false, true} {
		outcome, state, err := sessionWorkerOutcomeEvidence(raw, nil, quick.CPUStressSchema, 0, interrupted)
		if err != nil || outcome != "inconclusive" || state != "completed" {
			t.Fatal("valid non-pass discarded")
		}
		for _, bad := range []struct {
			raw  []byte
			read error
			code int
		}{
			{nil, os.ErrNotExist, 0}, {nil, errors.Join(os.ErrNotExist, errors.New("owned fault")), 1},
			{[]byte("{"), nil, 0}, {raw, nil, 1}, {raw, nil, 2},
		} {
			if _, _, err := sessionWorkerOutcomeEvidence(bad.raw, bad.read, quick.CPUStressSchema, bad.code, interrupted); err == nil {
				t.Fatal("dispatched stress became a fallback note")
			}
		}
		if _, _, err := sessionWorkerOutcomeEvidence(nil, os.ErrNotExist, quick.CPUStressSchema, 12, interrupted); !errors.Is(err, quick.ErrCPUStressCleanupUnconfirmed) {
			t.Fatal("cleanup exit lost priority")
		}
	}
}
