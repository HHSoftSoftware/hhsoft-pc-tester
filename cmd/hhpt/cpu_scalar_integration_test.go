package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

func cpuScalarFailureFixture(slot int, later bool) quick.CPUResult {
	value := completeCPUEvidence()
	value.Status, value.Workers, value.DurationMilliseconds = quick.StatusFail, 2, 0
	value.CalculationFailures.Scalar = true
	iteration, checks := uint64(0), uint64(slot)
	if later {
		iteration, checks = 256, uint64(12+slot)
	}
	ids := [...]string{"add1", "add2", "add3", "sub1", "sub2", "sub3", "mul1", "mul2", "mul3", "div1", "div2", "div3"}
	bits := [...]string{"400e000000000000", "c01e000000000000", "3eb8000000000000", "4011000000000000", "c00e000000000000", "4260000000000000", "400e000000000000", "bfe8000000000000", "4090000000000000", "4008000000000000", "c002000000000000", "3d70000000000000"}
	value.Iterations = 1 + iteration
	value.Scalar.ChecksPassed = 12 + checks
	value.Scalar.WorkersCompleted = 1
	if later {
		value.Scalar.WorkersCompleted++
	}
	value.Scalar.Workers = []quick.CPUScalarWorker{{Index: 0, Iterations: 1, ChecksPassed: 12},
		{Index: 1, Iterations: iteration, ChecksPassed: checks, Mismatch: &quick.CPUScalarMismatch{Vector: ids[slot], ExpectedBits: bits[slot], ActualBits: "7ff8000000000001"}}}
	return value
}

func TestCPUScalarFaultsCrossCoordinatorFinalizerAndSignedConsumers(t *testing.T) {
	for _, test := range []struct {
		name  string
		slot  int
		later bool
	}{{"first", 0, false}, {"last", 11, false}, {"later", 5, true}} {
		for _, stop := range []string{"failure", "operator", "thermal"} {
			t.Run(test.name+"/"+stop, func(t *testing.T) {
				h := newSessionHarness(t, "x86_64", nil)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				useRealSessionFinalizer(t, h, nil, nil)
				h.dependencies.RunWorker = func(_ context.Context, worker sessionWorker, _ io.Reader, _, _ io.Writer) int {
					if worker.ID != "cpu.quick" {
						t.Fatal("later work after calculation failure")
					}
					h.calls = append(h.calls, worker.ID)
					value := cpuScalarFailureFixture(test.slot, test.later)
					if stop == "thermal" {
						value.Safety.MonitorStatus, value.Safety.MonitorFailure, value.Safety.ThermalCoverage = "failed", "thermal_limit", "available"
						value.Safety.ThermalSensors, value.Safety.ThermalSamples, value.Safety.PeakMilliCelsius = 1, 1, 95000
					}
					writeSessionFixture(t, worker.OutputPath, value)
					h.clock = h.clock.Add(5 * time.Millisecond)
					if stop == "operator" {
						cancel()
					}
					return 1
				}
				if code := h.run(ctx); code != 0 || !h.finalized || len(h.calls) != 1 {
					t.Fatalf("joined failure not reported: %d %s", code, &h.stderr)
				}
				if h.journal.Workers[0].Outcome != "fail" || h.journal.Workers[0].State != "completed" || !h.journal.Monitor.CleanupConfirmed || !h.journal.Thermal.Joined || !h.inputFinished {
					t.Fatal("failure or owned joins lost")
				}
				raw, err := os.ReadFile(filepath.Join(h.directory, "session.hhpcr"))
				if err != nil {
					t.Fatal(err)
				}
				artifact, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
				if err != nil {
					t.Fatal(err)
				}
				var payload app.InventoryPayload
				if json.Unmarshal(artifact.InventoryJSON, &payload) != nil {
					t.Fatal("invalid signed payload")
				}
				view, err := app.ValidatedCPUScalar(payload)
				if err != nil || view == nil || view.MismatchedWorkers != 1 || !view.CalculationFailures.Scalar || view.WorkersStarted != 2 || view.WorkersCompleted != 1+btoi(test.later) {
					t.Fatal("signed scalar finding changed", err)
				}
				if len(payload.TestResults) != 22 || len(h.journal.Workers) != 11 || !strings.Contains(string(artifact.InventoryJSON), "7ff8000000000001") {
					t.Fatal("coverage or exact mismatch lost")
				}
			})
		}
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestCPUScalarInternalOverflowNeverReachesSignature(t *testing.T) {
	for _, status := range []quick.Status{quick.StatusError, quick.StatusAborted, quick.StatusFail} {
		h := newSessionHarness(t, "x86_64", nil)
		useRealSessionFinalizer(t, h, nil, nil)
		h.dependencies.RunWorker = func(_ context.Context, worker sessionWorker, _ io.Reader, _, _ io.Writer) int {
			value := cpuScalarFailureFixture(0, false)
			value.Schema, value.Status, value.Detail = "", status, "later safety diagnostic"
			writeSessionFixture(t, worker.OutputPath, value)
			return 1
		}
		if code := h.run(t.Context()); code != 1 || h.finalized {
			t.Fatal("invalid overflow schema signed")
		}
		if _, err := os.Stat(filepath.Join(h.directory, "session.hhpcr")); !os.IsNotExist(err) {
			t.Fatal("overflow report exists")
		}
	}
}

func TestCPUScalarCurrentAllOffAndDirectDefinition(t *testing.T) {
	excluded, err := plan.SelectableQuickTests("x86_64")
	if err != nil {
		t.Fatal(err)
	}
	h := newSessionHarness(t, "x86_64", excluded)
	useRealSessionFinalizer(t, h, nil, nil)
	if code := h.run(t.Context()); code != 0 || len(h.calls) != 0 || !h.finalized {
		t.Fatalf("all-off failed: %d %s", code, &h.stderr)
	}
	for _, version := range []int{0, 1, 3} {
		if _, err := newValidatedQuickTestResult("cpu.quick", version, app.TestPass, completeCPUEvidence()); err == nil {
			t.Fatal("wrong direct CPU definition accepted")
		}
	}
	if _, err := newValidatedQuickTestResult("cpu.quick", 2, app.TestPass, completeCPUEvidence()); err != nil {
		t.Fatal(err)
	}
}
