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

func commandMemoryTime(n int64) *int64 { return &n }

func commandMemoryPass() quick.MemoryStressResult {
	return quick.MemoryStressResult{
		Schema: "hhsoft.pc-tester.memory-stress/v1", Status: "pass", StopReason: "completed", Suite: "byte-patterns-aa55-inversions-v1",
		RequestedLoadMilliseconds: 1800000, LocalWatchdogMilliseconds: 1860000, ThermalPolicy: "available-observations/v1",
		RequestedBytes: 4194304, AvailableBytes: 16777216, AllocatedBytes: 4194304, EffectiveDeadlineMilliseconds: 1860000, EffectiveDeadlineSource: "watchdog",
		AvailabilityFinishedMilliseconds: commandMemoryTime(1), AllocationFinishedMilliseconds: commandMemoryTime(2),
		LoadStartedMilliseconds: commandMemoryTime(3), LoadStoppedMilliseconds: commandMemoryTime(1800003), ObservedLoadMilliseconds: 1800000,
		TerminalStartedMilliseconds: commandMemoryTime(1800004), TerminalStoppedMilliseconds: commandMemoryTime(1800005), FinishedMilliseconds: 1800006, AccountingValid: true,
		Availability: quick.MemoryStressAvailability{Started: true, Opened: true, RawBytes: 23, ReadCalls: 2, EOF: true, Valid: true, CloseConfirmed: true},
		Coverage:     quick.MemoryStressCoverage{CompletedPhases: 20, CompletedCycles: 2, LoadCycles: 1, ReadBytes: 50331648, WrittenBytes: 50331648, TerminalReadBytes: 4194304},
		StopFacts:    quick.MemoryStressStopFacts{NormalLoadComplete: true},
		Safety:       quick.SafetyEvidence{GlobalTimeoutMilliseconds: 1860000, ThermalLimitMilliCelsius: 90000, ThermalSensors: 1, ThermalSamples: 2, PeakMilliCelsius: 42000, MonitorStatus: "completed", CleanupConfirmed: true, ThermalObservations: 2, ThermalCoverage: "available"},
	}
}

func requireCommandMemoryFixture(t *testing.T, v quick.MemoryStressResult) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = quickevidence.DecodeMemoryStress(raw); err != nil {
		t.Fatalf("invalid independent command prerequisite: %v", err)
	}
}

func TestMemoryStressCommandDispatchesRealStoppedProducer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, diagnostic bytes.Buffer
	code := run(ctx, []string{"memory-stress"}, strings.NewReader(""), &out, &diagnostic)
	if code != 0 || diagnostic.Len() != 0 || !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
		t.Fatalf("real dispatch failed: %d %s", code, diagnostic.String())
	}
	v, _, err := quickevidence.DecodeMemoryStress(out.Bytes())
	if err != nil || v.Status != "aborted" || v.StopReason != "parent_cancelled" || v.AllocatedBytes != 0 || v.Availability.Started || v.Safety.MonitorStatus != "not_started" {
		t.Fatal("real stopped dispatch acquired resources or lost valid diagnostic")
	}
	var help bytes.Buffer
	usage(&help)
	if !strings.Contains(help.String(), "hhpt memory-stress [-bytes N] [-output FILE]") || !strings.Contains(help.String(), "hhpt memory [-bytes N] [-passes 2] [-timeout 30s] [-thermal-limit-c 90] [-output FILE]") {
		t.Fatal("new help missing or quick-memory help changed")
	}
}

func TestMemoryStressCommandRejectsControlOverridesBeforeProbe(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"duration", []string{"-duration", "1s"}},
		{"timeout", []string{"-timeout", "1s"}},
		{"passes", []string{"-passes", "1"}},
		{"thermal", []string{"-thermal-limit-c", "100"}},
		{"root", []string{"-root", "/fixture"}},
		{"negative", []string{"-bytes", "-1"}},
		{"small", []string{"-bytes", "4194303"}},
		{"large", []string{"-bytes", "67108865"}},
		{"overflow", []string{"-bytes", "9223372036854775808"}},
		{"empty_output", []string{"-output", ""}},
		{"extra", []string{"extra"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, writes := 0, 0
			var out, diagnostic bytes.Buffer
			code := runMemoryStressWithDestination(context.Background(), tc.args, &out, &diagnostic,
				func(context.Context, quick.MemoryStressOptions) (quick.MemoryStressResult, error) {
					calls++
					return commandMemoryPass(), nil
				},
				func(string, io.Writer, func(io.Writer) error) error { writes++; return nil })
			if code != 2 || calls != 0 || writes != 0 || out.Len() != 0 || diagnostic.String() != "memory-stress: invalid arguments\n" {
				t.Fatal("fixed controls reached a producer or writer")
			}
		})
	}
}

func TestMemoryStressCommandBindsOptionsAndOwnedDestination(t *testing.T) {
	value := commandMemoryPass()
	requireCommandMemoryFixture(t, value)
	ctx := context.WithValue(context.Background(), struct{}{}, "fixture")
	calls, writes := 0, 0
	var out, diagnostic bytes.Buffer
	code := runMemoryStressWithDestination(ctx, []string{"-bytes", "4194304", "-output", "-"}, &out, &diagnostic,
		func(got context.Context, opts quick.MemoryStressOptions) (quick.MemoryStressResult, error) {
			calls++
			if got != ctx || opts.Bytes != 4194304 || opts.Root != "/" {
				t.Fatal("command controls not bound")
			}
			return value, nil
		},
		func(path string, destination io.Writer, render func(io.Writer) error) error {
			writes++
			if calls != 1 || path != "-" || destination != &out {
				t.Fatal("writer order/ownership changed")
			}
			return render(destination)
		})
	if code != 0 || calls != 1 || writes != 1 || diagnostic.Len() != 0 {
		t.Fatal("valid command failed")
	}
	decoded, _, err := quickevidence.DecodeMemoryStress(out.Bytes())
	if err != nil || decoded.Coverage != value.Coverage {
		t.Fatal("output evidence changed")
	}
	path := filepath.Join(t.TempDir(), "memory.json")
	out.Reset()
	diagnostic.Reset()
	probe := func(context.Context, quick.MemoryStressOptions) (quick.MemoryStressResult, error) { return value, nil }
	if code := runMemoryStressWithDestination(ctx, []string{"-bytes", "4194304", "-output", path}, &out, &diagnostic, probe, writeDestination); code != 0 || out.Len() != 0 {
		t.Fatal("owned new file not written")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := quickevidence.DecodeMemoryStress(before); err != nil {
		t.Fatal("file evidence invalid")
	}
	diagnostic.Reset()
	if code := runMemoryStressWithDestination(ctx, []string{"-bytes", "4194304", "-output", path}, &out, &diagnostic, probe, writeDestination); code != 1 || diagnostic.String() != "memory-stress: result output failed\n" {
		t.Fatal("existing output was not rejected")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("existing output replaced")
	}
}

func TestMemoryStressCommandPreservesCleanupAndPrivateErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*quick.MemoryStressResult)
		err     error
		code    int
		message string
	}{
		{"options", nil, quick.ErrMemoryStressOptions, 2, "invalid options"},
		{"accounting", nil, errors.Join(quick.ErrMemoryStressAccounting, errors.New("private-path")), 1, "diagnostic accounting failed"},
		{"cleanup_first", nil, errors.Join(quick.ErrMemoryStressCleanupUnconfirmed, quick.ErrMemoryStressAccounting, errors.New("private-path")), 12, "cleanup was not confirmed"},
		{"cleanup_in_value", func(v *quick.MemoryStressResult) { v.Safety.CleanupConfirmed = false }, nil, 12, "cleanup was not confirmed"},
		{"bad_counter", func(v *quick.MemoryStressResult) { v.Coverage.ReadBytes++ }, nil, 1, "invalid diagnostic evidence"},
		{"wrong_schema", func(v *quick.MemoryStressResult) { v.Schema = "future" }, nil, 1, "invalid diagnostic evidence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := commandMemoryPass()
			requireCommandMemoryFixture(t, v)
			if tc.change != nil {
				tc.change(&v)
			}
			var out, diagnostic bytes.Buffer
			writes := 0
			code := runMemoryStressWithDestination(context.Background(), []string{"-bytes", "4194304"}, &out, &diagnostic,
				func(context.Context, quick.MemoryStressOptions) (quick.MemoryStressResult, error) { return v, tc.err },
				func(string, io.Writer, func(io.Writer) error) error { writes++; return nil })
			if code != tc.code || writes != 0 || out.Len() != 0 || diagnostic.String() != "memory-stress: "+tc.message+"\n" || strings.Contains(diagnostic.String(), "private") {
				t.Fatalf("error boundary changed: %d %s", code, diagnostic.String())
			}
		})
	}
}

func TestMemoryStressCommandOutputFailureAndValidNonPass(t *testing.T) {
	v := commandMemoryPass()
	requireCommandMemoryFixture(t, v)
	for _, mode := range []string{"short_write", "owned_close_failure"} {
		t.Run(mode, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			calls := 0
			code := runMemoryStressWithDestination(context.Background(), []string{"-bytes", "4194304"}, &out, &diagnostic,
				func(context.Context, quick.MemoryStressOptions) (quick.MemoryStressResult, error) { return v, nil },
				func(_ string, _ io.Writer, render func(io.Writer) error) error {
					calls++
					if mode == "short_write" {
						return render(stressShortWriter{})
					}
					var owned bytes.Buffer
					if err := render(&owned); err != nil {
						return err
					}
					if owned.Len() == 0 {
						t.Fatal("close failure occurred before write prerequisite")
					}
					return errors.New("private-owned-close")
				})
			if code != 1 || calls != 1 || out.Len() != 0 || diagnostic.String() != "memory-stress: result output failed\n" {
				t.Fatal("owned output failure became success or leaked detail")
			}
		})
	}
	v.Status = "inconclusive"
	v.StopReason = "thermal_coverage"
	v.StopFacts.CoverageLost = true
	v.Safety.ThermalObservations = 3
	v.Safety.ThermalCoverage = "partial"
	v.Safety.MonitorStatus = "failed"
	v.Safety.MonitorFailure = "coverage_loss"
	requireCommandMemoryFixture(t, v)
	var out, diagnostic bytes.Buffer
	code := runMemoryStressWithDestination(context.Background(), []string{"-bytes", "4194304"}, &out, &diagnostic,
		func(context.Context, quick.MemoryStressOptions) (quick.MemoryStressResult, error) { return v, nil }, writeDestination)
	decoded, _, err := quickevidence.DecodeMemoryStress(out.Bytes())
	if code != 0 || diagnostic.Len() != 0 || err != nil || decoded.Status != "inconclusive" {
		t.Fatal("valid non-pass diagnostic suppressed or promoted")
	}
}

func TestMemoryStressCommandRejectsMissingDependencies(t *testing.T) {
	probe := func(context.Context, quick.MemoryStressOptions) (quick.MemoryStressResult, error) {
		t.Fatal("invalid dependency reached probe")
		return quick.MemoryStressResult{}, nil
	}
	for _, mode := range []string{"context", "probe", "writer"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			p := probe
			w := destinationWriter(writeDestination)
			if mode == "context" {
				ctx = nil
			}
			if mode == "probe" {
				p = nil
			}
			if mode == "writer" {
				w = nil
			}
			var out, diagnostic bytes.Buffer
			if code := runMemoryStressWithDestination(ctx, nil, &out, &diagnostic, p, w); code != 2 || out.Len() != 0 || diagnostic.String() != "memory-stress: invalid context or dependencies\n" {
				t.Fatal("missing dependency admitted")
			}
		})
	}
}
