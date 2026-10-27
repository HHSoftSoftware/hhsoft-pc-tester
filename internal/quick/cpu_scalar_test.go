package quick

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestScalarFixedRuntimeOracles(t *testing.T) {
	// These decimal values and exact answer bits independently exercise the
	// production operation boundary, without using its vector array as oracle.
	cases := []struct {
		op          byte
		left, right float64
		bits        uint64
	}{
		{'+', 1.5, 2.25, 0x400e000000000000}, {'+', -8, .5, 0xc01e000000000000},
		{'+', 0x1p-20, 0x1p-21, 0x3eb8000000000000}, {'-', 7.75, 3.5, 0x4011000000000000},
		{'-', -1.25, 2.5, 0xc00e000000000000}, {'-', 0x1p40, 0x1p39, 0x4260000000000000},
		{'*', 1.5, 2.5, 0x400e000000000000}, {'*', -3, .25, 0xbfe8000000000000},
		{'*', 0x1p-20, 0x1p30, 0x4090000000000000}, {'/', 7.5, 2.5, 0x4008000000000000},
		{'/', -9, 4, 0xc002000000000000}, {'/', 0x1p-30, 0x1p10, 0x3d70000000000000},
	}
	vectors := scalarVectors()
	for index, test := range cases {
		if bits := math.Float64bits(scalarRound(test.op, test.left, test.right)); bits != test.bits {
			t.Fatalf("operation %d: %016x", index, bits)
		}
		v := vectors[index]
		if v.operation != test.op || v.left != math.Float64bits(test.left) || v.right != math.Float64bits(test.right) || v.expected != test.bits {
			t.Fatalf("vector %d differs from independent oracle", index)
		}
	}
}

func TestScalarWorkerStopAndLateMismatch(t *testing.T) {
	for _, mode := range []string{"before", "late_success", "late_mismatch", "cancel_success", "last_mismatch", "later_mismatch", "completed_suite"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Unix(100, 0)
			deadline := now.Add(time.Second)
			ops := productionCPUOperations()
			ops.now = func() time.Time { return now }
			ops.integer = func(uint64) uint64 { return expectedIntegerRound }
			calls := 0
			ops.scalar = func(worker, slot int, operation byte, a, b float64) float64 {
				if worker != 3 {
					t.Fatal("wrong worker identity")
				}
				calls++
				value := scalarRound(operation, a, b)
				if mode == "late_success" || mode == "late_mismatch" {
					now = deadline
				}
				if mode == "cancel_success" {
					cancel()
				}
				if mode == "late_mismatch" || mode == "last_mismatch" && calls == 12 || mode == "later_mismatch" && calls == 18 {
					return value + 1
				}
				return value
			}
			if mode == "before" {
				now = deadline
			}
			if mode == "completed_suite" {
				ops.integer = func(uint64) uint64 {
					if calls == 12 {
						now = deadline
					}
					return expectedIntegerRound
				}
			}
			var failed atomic.Bool
			got := runCPUWorker(ctx, deadline, 3, &failed, ops)
			switch mode {
			case "before":
				if calls != 0 || got.row.ChecksPassed != 0 {
					t.Fatal("work after stop")
				}
			case "late_success", "cancel_success":
				if calls != 1 || got.row.ChecksPassed != 0 || got.row.Mismatch != nil {
					t.Fatal("late credit")
				}
			case "late_mismatch":
				if got.row.ChecksPassed != 0 || got.row.Mismatch == nil || !got.failures.Scalar || !failed.Load() {
					t.Fatal("lost late failure")
				}
			case "last_mismatch":
				if got.row.ChecksPassed != 11 || got.row.Mismatch.Vector != "div3" || got.row.Iterations != 0 {
					t.Fatal("lost final-vector failure")
				}
			case "later_mismatch":
				if got.row.Iterations != 256 || got.row.ChecksPassed != 17 || got.row.Mismatch.Vector != "sub3" {
					t.Fatalf("later failure: %+v", got)
				}
			case "completed_suite":
				if got.row.Iterations != 0 || got.row.ChecksPassed != 12 || got.row.Mismatch != nil {
					t.Fatal("completed boundary lost")
				}
			}
		})
	}
}

func TestCPUScalarEarlyResultHasClosedEmptyEvidence(t *testing.T) {
	got := RunCPU(context.Background(), CPUOptions{})
	if got.Scalar.Suite != CPUScalarSuite || got.Scalar.VectorCount != 12 || got.Scalar.Workers == nil || len(got.Scalar.Workers) != 0 {
		t.Fatal("missing early scalar evidence")
	}
	raw, err := json.Marshal(got)
	if err != nil || !json.Valid(raw) {
		t.Fatal("invalid early JSON")
	}
}

func TestCPUCounterOverflowCannotRegainSignableSchema(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		monitor := newSafetyMonitor(ctx, t.TempDir(), time.Second, 90000)
		result := CPUResult{Schema: CPUSchema, Status: StatusError, Scalar: CPUScalarEvidence{Suite: CPUScalarSuite, VectorCount: 12, Workers: []CPUScalarWorker{}}}
		worker := cpuWorkerResult{row: CPUScalarWorker{Index: 0, Iterations: 256, ChecksPassed: 17}, overflow: true}
		if mismatch {
			worker.failures.Scalar = true
			worker.row.Mismatch = &CPUScalarMismatch{Vector: "sub3", ExpectedBits: "4260000000000000", ActualBits: "4260000000000001"}
		}
		if !collectCPUWorkers(&result, []cpuWorkerResult{worker}) {
			t.Fatal("overflow not retained")
		}
		if result.CalculationFailures.Scalar {
			result.Status = StatusFail
		}
		result.Detail = "CPU evidence counter overflow"
		cancel()
		finishSafety(monitor, &result.Status, &result.Detail, &result.Safety)
		if result.Schema != "" || result.Scalar.Workers == nil || mismatch && result.Status != StatusFail || !result.Safety.CleanupConfirmed {
			t.Fatal("overflow or failure priority lost")
		}
	}
}

func TestCPUKnownAnswerStopBoundaries(t *testing.T) {
	for _, mode := range []string{"cancel_integer", "deadline_integer", "global_integer", "integer_mismatch", "late_sha_mismatch"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Unix(100, 0)
			deadline := now.Add(time.Second)
			var failed atomic.Bool
			ops := productionCPUOperations()
			ops.now = func() time.Time { return now }
			shaCalls := 0
			ops.integer = func(uint64) uint64 {
				switch mode {
				case "cancel_integer":
					cancel()
				case "deadline_integer":
					now = deadline
				case "global_integer":
					failed.Store(true)
				case "integer_mismatch":
					cancel()
					return 0
				}
				return expectedIntegerRound
			}
			ops.sha = func([]byte) [32]byte { shaCalls++; cancel(); return [32]byte{} }
			got := runCPUWorker(ctx, deadline, 0, &failed, ops)
			if mode == "late_sha_mismatch" {
				if shaCalls != 1 || !got.failures.SHA256 || !failed.Load() {
					t.Fatal("late SHA mismatch lost")
				}
			} else if shaCalls != 0 {
				t.Fatal("SHA started after stop or integer failure")
			}
			if mode == "integer_mismatch" && (!got.failures.Integer || !failed.Load()) {
				t.Fatal("integer mismatch not latched")
			}
			if got.row.ChecksPassed != 0 || got.row.Iterations != 0 {
				t.Fatal("post-stop credit")
			}
		})
	}
}

func TestCPUActualGlobalDeadlinePrecedesTimerNotification(t *testing.T) {
	ops := productionCPUOperations()
	ops.now = func() time.Time { return time.Now().Add(time.Hour) }
	got := runCPUWithOperations(context.Background(), CPUOptions{Duration: time.Second, GlobalTimeout: time.Second, Workers: 1, Root: t.TempDir(), ThermalLimit: 90000}, newSafetyMonitor, ops)
	if got.Status != StatusAborted || got.Detail != "global deadline exceeded" || len(got.Scalar.Workers) != 0 || !got.Safety.CleanupConfirmed {
		t.Fatalf("actual deadline became ordinary completion: %+v", got)
	}
}

func TestCPUScalarFailureSurvivesMonitorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ops := productionCPUOperations()
	ops.scalar = func(_, _ int, operation byte, a, b float64) float64 {
		cancel()
		return scalarRound(operation, a, b) + 1
	}
	got := runCPUWithOperations(ctx, CPUOptions{Duration: time.Second, GlobalTimeout: time.Second, Workers: 1, Root: t.TempDir(), ThermalLimit: 90000}, newSafetyMonitor, ops)
	if got.Status != StatusFail || !got.CalculationFailures.Scalar || len(got.Scalar.Workers) != 1 || got.Scalar.Workers[0].Mismatch == nil || !got.Safety.CleanupConfirmed {
		t.Fatalf("failure downgrade or cleanup loss: %+v", got)
	}
}

func TestScalarEveryVectorStopBoundary(t *testing.T) {
	for slot := 0; slot < 12; slot++ {
		for _, mode := range []string{"cancel", "deadline", "other_worker_failure", "late_mismatch"} {
			t.Run(strconv.Itoa(slot)+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				now := time.Unix(100, 0)
				deadline := now.Add(time.Second)
				var failed atomic.Bool
				ops := productionCPUOperations()
				ops.now = func() time.Time { return now }
				calls := 0
				ops.scalar = func(worker, current int, operation byte, a, b float64) float64 {
					if worker != 2 {
						t.Fatal("wrong worker")
					}
					calls++
					answer := scalarRound(operation, a, b)
					if current == slot {
						switch mode {
						case "cancel":
							cancel()
						case "deadline":
							now = deadline
						case "other_worker_failure":
							failed.Store(true)
						case "late_mismatch":
							cancel()
							return math.Float64frombits(math.Float64bits(answer) ^ 1)
						}
					}
					return answer
				}
				value := runCPUWorker(ctx, deadline, 2, &failed, ops)
				if calls != slot+1 || value.row.ChecksPassed != uint64(slot) || value.row.Iterations != 0 {
					t.Fatal("new operation or successful credit after stop")
				}
				if (value.row.Mismatch != nil) != (mode == "late_mismatch") || value.failures.Scalar != (mode == "late_mismatch") {
					t.Fatal("in-flight mismatch discarded or invented")
				}
			})
		}
	}
}

func TestCPUStateAggregateKeepsIntentionalModuloArithmetic(t *testing.T) {
	value := CPUResult{Schema: CPUSchema, Scalar: CPUScalarEvidence{Workers: []CPUScalarWorker{}}}
	if collectCPUWorkers(&value, []cpuWorkerResult{{row: CPUScalarWorker{Index: 0}, state: math.MaxUint64},
		{row: CPUScalarWorker{Index: 1}, state: 2}}) || value.StateAggregate != 1 || value.Schema != CPUSchema {
		t.Fatal("workload state wrapping became evidence counter overflow")
	}
}

func TestCPUCoordinatorJoinsNonzeroWorkerFaultsAfterPriorCoverage(t *testing.T) {
	for _, test := range []struct {
		name               string
		faultCall          int
		iterations, checks uint64
		completed          int
	}{
		{"first", 1, 0, 0, 1}, {"last", 12, 0, 11, 1}, {"later", 18, 256, 17, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness, finishHarness := context.WithTimeout(t.Context(), 5*time.Second)
			defer finishHarness()
			ctx, cancel := context.WithCancel(harness)
			defer cancel()
			firstCompleted, faultObserved := make(chan struct{}), make(chan struct{})
			var calls [2]int
			var returned atomic.Int32
			ops := productionCPUOperations()
			ops.scalar = func(worker, _ int, operation byte, a, b float64) float64 {
				calls[worker]++ // Only this worker writes its own slot; read after join.
				answer := scalarRound(operation, a, b)
				if worker == 0 && calls[worker] == 13 {
					close(firstCompleted)
					// Deliberate run cancellation must finish propagating before
					// this operation returns; only the harness may end the wait early.
					select {
					case <-faultObserved:
					case <-harness.Done():
					}
					returned.Add(1)
					return answer
				}
				if worker == 1 && calls[worker] == 1 {
					select {
					case <-firstCompleted:
					case <-harness.Done():
						return answer
					}
				}
				if worker == 1 && calls[worker] == test.faultCall {
					cancel()
					close(faultObserved)
					returned.Add(1)
					return math.Float64frombits(math.Float64bits(answer) ^ 1)
				}
				return answer
			}
			value := runCPUWithOperations(ctx, CPUOptions{Duration: 3 * time.Second, GlobalTimeout: 4 * time.Second,
				Workers: 2, Root: t.TempDir(), ThermalLimit: 90000}, newSafetyMonitor, ops)
			if value.Status != StatusFail || !value.CalculationFailures.Scalar || value.CalculationFailures.Integer || value.CalculationFailures.SHA256 ||
				!value.Safety.CleanupConfirmed || returned.Load() != 2 || len(value.Scalar.Workers) != 2 || value.Scalar.WorkersCompleted != test.completed {
				t.Fatalf("failure, aggregation or join lost: %+v", value)
			}
			first, second := value.Scalar.Workers[0], value.Scalar.Workers[1]
			if first.Index != 0 || first.Iterations != 256 || first.ChecksPassed != 12 || first.Mismatch != nil || calls[0] != 13 ||
				second.Index != 1 || second.Iterations != test.iterations || second.ChecksPassed != test.checks || second.Mismatch == nil || calls[1] != test.faultCall ||
				value.Iterations != 256+test.iterations || value.Scalar.ChecksPassed != 12+test.checks {
				rows, err := json.Marshal(value.Scalar.Workers)
				if err != nil {
					t.Fatal("cannot encode worker diagnostic")
				}
				t.Fatalf("worker prefix, retained coverage or late credit differs: rows=%s calls=%v iterations=%d checks=%d completed=%d expected_second_iterations=%d expected_second_checks=%d expected_fault_calls=%d",
					rows, calls, value.Iterations, value.Scalar.ChecksPassed, value.Scalar.WorkersCompleted, test.iterations, test.checks, test.faultCall)
			}
		})
	}
}
