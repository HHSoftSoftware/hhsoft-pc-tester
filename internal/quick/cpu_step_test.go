package quick

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type cpuStepOracle struct {
	id          string
	operation   byte
	left, right float64
	bits        uint64
}

func cpuStepOracles() [12]cpuStepOracle {
	return [12]cpuStepOracle{
		{"add1", '+', 1.5, 2.25, 0x400e000000000000},
		{"add2", '+', -8, .5, 0xc01e000000000000},
		{"add3", '+', 0x1p-20, 0x1p-21, 0x3eb8000000000000},
		{"sub1", '-', 7.75, 3.5, 0x4011000000000000},
		{"sub2", '-', -1.25, 2.5, 0xc00e000000000000},
		{"sub3", '-', 0x1p40, 0x1p39, 0x4260000000000000},
		{"mul1", '*', 1.5, 2.5, 0x400e000000000000},
		{"mul2", '*', -3, .25, 0xbfe8000000000000},
		{"mul3", '*', 0x1p-20, 0x1p30, 0x4090000000000000},
		{"div1", '/', 7.5, 2.5, 0x4008000000000000},
		{"div2", '/', -9, 4, 0xc002000000000000},
		{"div3", '/', 0x1p-30, 0x1p10, 0x3d70000000000000},
	}
}

func TestCPUStepRealInitialWorkAndPeriodicContinuation(t *testing.T) {
	result := cpuWorkerResult{row: CPUScalarWorker{Index: 5}, state: 0x0123456789abcdf4}
	vectors := scalarVectors()
	oracles := cpuStepOracles()
	operations := productionCPUOperations()
	realInteger, realSHA, realScalar := operations.integer, operations.sha, operations.scalar
	now := time.Unix(100, 0)
	operations.now = func() time.Time { return now }
	integerCalls, scalarCalls := 0, 0
	var checkedAt []uint64
	operations.integer = func(value uint64) uint64 {
		integerCalls++
		return realInteger(value)
	}
	operations.sha = func(input []byte) [32]byte {
		if string(input) != "abc" {
			t.Fatal("known-answer input changed")
		}
		checkedAt = append(checkedAt, result.row.Iterations)
		value := realSHA(input)
		if fmt.Sprintf("%x", value) != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
			t.Fatal("real SHA answer differs from literal oracle")
		}
		return value
	}
	operations.scalar = func(worker, slot int, operation byte, left, right float64) float64 {
		oracle := oracles[scalarCalls%12]
		if worker != 5 || slot != scalarCalls%12 || operation != oracle.operation || left != oracle.left || right != oracle.right {
			t.Fatal("worker identity or arithmetic operands changed")
		}
		scalarCalls++
		value := realScalar(worker, slot, operation, left, right)
		if math.Float64bits(value) != oracle.bits {
			t.Fatal("real scalar answer differs from literal oracle")
		}
		return value
	}
	var failed atomic.Bool
	for iteration := 1; iteration <= 257; iteration++ {
		if !stepCPUWorker(context.Background(), now.Add(time.Second), &failed, operations, &vectors, &result) {
			t.Fatal("valid checked step stopped")
		}
		switch iteration {
		case 1:
			if result.row.Iterations != 1 || result.row.ChecksPassed != 12 || result.state != 0x28b2b4e18ccf4839 {
				t.Fatal("initial checked iteration differs")
			}
		case 256:
			if result.row.Iterations != 256 || result.row.ChecksPassed != 12 || result.state != 0x2dded990310996cf {
				t.Fatal("periodic boundary changed")
			}
		}
	}
	if result.row.Iterations != 257 || result.row.ChecksPassed != 24 || result.state != 0x8729a82649a4e433 ||
		integerCalls != 259 || scalarCalls != 24 || !reflect.DeepEqual(checkedAt, []uint64{0, 256}) ||
		result.row.Mismatch != nil || result.failures != (CPUCalculationFailures{}) || result.overflow || failed.Load() {
		t.Fatal("continuation, actual work, cadence or accounting differs")
	}
}

func TestCPUStepQuickWorkerUsesCheckedIteration(t *testing.T) {
	now := time.Unix(100, 0)
	deadline := now.Add(time.Second)
	operations := productionCPUOperations()
	realInteger, realScalar := operations.integer, operations.scalar
	operations.now = func() time.Time { return now }
	integerCalls, scalarCalls := 0, 0
	operations.integer = func(input uint64) uint64 {
		integerCalls++
		if integerCalls == 3 {
			now = deadline
		}
		return realInteger(input)
	}
	operations.scalar = func(worker, slot int, operation byte, left, right float64) float64 {
		scalarCalls++
		return realScalar(worker, slot, operation, left, right)
	}
	var failed atomic.Bool
	result := runCPUWorker(context.Background(), deadline, 5, &failed, operations)
	if integerCalls != 3 || scalarCalls != 12 || result.row.Index != 5 || result.row.Iterations != 1 ||
		result.row.ChecksPassed != 12 || result.state != 0x28b2b4e18ccf4839 || result.row.Mismatch != nil ||
		result.failures != (CPUCalculationFailures{}) || result.overflow || failed.Load() {
		t.Fatal("quick worker lost its checked iteration or credited a late recurrence")
	}
}

func TestCPUStepOperationStopAndMismatchBoundaries(t *testing.T) {
	stages := []string{"integer_check", "sha", "add1", "add2", "add3", "sub1", "sub2", "sub3", "mul1", "mul2", "mul3", "div1", "div2", "div3", "recurrence"}
	for position, stage := range stages {
		for _, boundary := range []string{"before", "at", "after", "cancel", "peer_failure"} {
			for _, outcome := range []string{"success", "mismatch"} {
				if stage == "recurrence" && outcome == "mismatch" {
					continue // Recurrence state has no separate known-answer oracle.
				}
				t.Run(stage+"/"+boundary+"/"+outcome, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					now := time.Unix(100, 0)
					deadline := now.Add(time.Second)
					result := cpuWorkerResult{row: CPUScalarWorker{Index: 5}, state: 0x0123456789abcdf4}
					vectors := scalarVectors()
					oracles := cpuStepOracles()
					var failed atomic.Bool
					var trace []string
					visit := func(label string) bool {
						trace = append(trace, label)
						if label != stage {
							return false
						}
						switch boundary {
						case "before":
							now = deadline.Add(-time.Nanosecond)
						case "at":
							now = deadline
						case "after":
							now = deadline.Add(time.Nanosecond)
						case "cancel":
							cancel()
						case "peer_failure":
							failed.Store(true)
						}
						return true
					}
					operations := productionCPUOperations()
					realInteger, realSHA, realScalar := operations.integer, operations.sha, operations.scalar
					operations.now = func() time.Time { return now }
					integerCalls := 0
					operations.integer = func(input uint64) uint64 {
						integerCalls++
						label := "integer_check"
						if integerCalls == 2 {
							label = "recurrence"
						}
						hit := visit(label)
						value := realInteger(input)
						if hit && outcome == "mismatch" {
							value ^= 1
						}
						return value
					}
					operations.sha = func(input []byte) [32]byte {
						hit := visit("sha")
						value := realSHA(input)
						if hit && outcome == "mismatch" {
							value[0] ^= 1
						}
						return value
					}
					operations.scalar = func(worker, slot int, operation byte, left, right float64) float64 {
						oracle := oracles[slot]
						if worker != 5 || operation != oracle.operation || left != oracle.left || right != oracle.right {
							t.Fatal("operation prerequisite differs")
						}
						hit := visit(oracle.id)
						value := realScalar(worker, slot, operation, left, right)
						if hit && outcome == "mismatch" {
							value = math.Float64frombits(math.Float64bits(value) ^ 1)
						}
						return value
					}
					committed := stepCPUWorker(ctx, deadline, &failed, operations, &vectors, &result)
					wantCommit := boundary == "before" && outcome == "success"
					wantTrace := stages[:position+1]
					wantChecks := uint64(0)
					if position >= 2 {
						wantChecks = uint64(position - 2)
					}
					wantState, wantIterations := uint64(0x0123456789abcdf4), uint64(0)
					if wantCommit {
						wantTrace, wantChecks = stages, 12
						wantState, wantIterations = 0x28b2b4e18ccf4839, 1
					}
					wantFailures := CPUCalculationFailures{}
					if outcome == "mismatch" {
						switch stage {
						case "integer_check":
							wantFailures.Integer = true
						case "sha":
							wantFailures.SHA256 = true
						default:
							wantFailures.Scalar = true
						}
					}
					if committed != wantCommit || !reflect.DeepEqual(trace, wantTrace) ||
						result.row.Index != 5 || result.row.Iterations != wantIterations || result.row.ChecksPassed != wantChecks ||
						result.state != wantState || result.failures != wantFailures || result.overflow ||
						failed.Load() != (outcome == "mismatch" || boundary == "peer_failure") {
						t.Fatalf("operation reachability, stop order or credit differs: trace=%v iterations=%d checks=%d", trace, result.row.Iterations, result.row.ChecksPassed)
					}
					if wantFailures.Scalar {
						oracle := oracles[position-2]
						if result.row.Mismatch == nil || result.row.Mismatch.Vector != oracle.id ||
							result.row.Mismatch.ExpectedBits != fmt.Sprintf("%016x", oracle.bits) ||
							result.row.Mismatch.ActualBits != fmt.Sprintf("%016x", oracle.bits^1) {
							t.Fatal("late scalar mismatch or exact bits were lost")
						}
					} else if result.row.Mismatch != nil {
						t.Fatal("scalar mismatch was invented")
					}
				})
			}
		}
	}
}

func TestCPUStepEntryRejectsStoppedWork(t *testing.T) {
	for _, reason := range []string{"at_deadline", "after_deadline", "cancelled", "peer_failed"} {
		t.Run(reason, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Unix(100, 0)
			deadline := now.Add(time.Second)
			var failed atomic.Bool
			switch reason {
			case "at_deadline":
				now = deadline
			case "after_deadline":
				now = deadline.Add(time.Nanosecond)
			case "cancelled":
				cancel()
			case "peer_failed":
				failed.Store(true)
			}
			result := cpuWorkerResult{row: CPUScalarWorker{Index: 5}, state: 0x0123456789abcdf4}
			before := result
			vectors := scalarVectors()
			operations := productionCPUOperations()
			operations.now = func() time.Time { return now }
			calls := 0
			operations.integer = func(uint64) uint64 { calls++; return 0 }
			operations.sha = func([]byte) [32]byte { calls++; return [32]byte{} }
			operations.scalar = func(int, int, byte, float64, float64) float64 { calls++; return 0 }
			if stepCPUWorker(ctx, deadline, &failed, operations, &vectors, &result) || calls != 0 || !reflect.DeepEqual(result, before) {
				t.Fatal("stopped entry executed or mutated work")
			}
		})
	}
}

func TestCPUStepCounterOverflowStopsBeforeCommit(t *testing.T) {
	for _, field := range []string{"iterations", "checks", "second_check"} {
		t.Run(field, func(t *testing.T) {
			result := cpuWorkerResult{row: CPUScalarWorker{Index: 5}, state: 0x0123456789abcdf4}
			switch field {
			case "iterations":
				result.row.Iterations = math.MaxUint64
			case "checks":
				result.row.ChecksPassed = math.MaxUint64
			case "second_check":
				result.row.ChecksPassed = math.MaxUint64 - 1
			}
			vectors := scalarVectors()
			operations := productionCPUOperations()
			realInteger, realSHA, realScalar := operations.integer, operations.sha, operations.scalar
			now := time.Unix(100, 0)
			operations.now = func() time.Time { return now }
			integerCalls, shaCalls, scalarCalls := 0, 0, 0
			operations.integer = func(v uint64) uint64 { integerCalls++; return realInteger(v) }
			operations.sha = func(v []byte) [32]byte { shaCalls++; return realSHA(v) }
			operations.scalar = func(worker, slot int, operation byte, left, right float64) float64 {
				scalarCalls++
				return realScalar(worker, slot, operation, left, right)
			}
			var failed atomic.Bool
			if stepCPUWorker(context.Background(), now.Add(time.Second), &failed, operations, &vectors, &result) ||
				!result.overflow || !failed.Load() || result.state != 0x0123456789abcdf4 ||
				result.row.Index != 5 || result.failures != (CPUCalculationFailures{}) || result.row.Mismatch != nil || integerCalls != 1 {
				t.Fatal("overflow wrapped, changed work state or became a mismatch")
			}
			if field == "iterations" {
				if result.row.Iterations != math.MaxUint64 || result.row.ChecksPassed != 0 || shaCalls != 0 || scalarCalls != 0 {
					t.Fatal("iteration overflow did not reach exactly one recurrence")
				}
			} else {
				wantScalar := 1
				if field == "second_check" {
					wantScalar = 2
				}
				if result.row.Iterations != 0 || result.row.ChecksPassed != math.MaxUint64 || shaCalls != 1 || scalarCalls != wantScalar {
					t.Fatal("check overflow did not reach its exact operation boundary")
				}
			}
		})
	}
}

func TestCPUStepRecurrenceModuloIsNotCounterOverflow(t *testing.T) {
	result := cpuWorkerResult{row: CPUScalarWorker{Index: 5, Iterations: 1, ChecksPassed: 12}, state: math.MaxUint64}
	vectors := scalarVectors()
	operations := productionCPUOperations()
	realInteger, realSHA, realScalar := operations.integer, operations.sha, operations.scalar
	now := time.Unix(100, 0)
	operations.now = func() time.Time { return now }
	integerCalls, shaCalls, scalarCalls := 0, 0, 0
	operations.integer = func(v uint64) uint64 { integerCalls++; return realInteger(v) }
	operations.sha = func(v []byte) [32]byte { shaCalls++; return realSHA(v) }
	operations.scalar = func(worker, slot int, operation byte, left, right float64) float64 {
		scalarCalls++
		return realScalar(worker, slot, operation, left, right)
	}
	var failed atomic.Bool
	if !stepCPUWorker(context.Background(), now.Add(time.Second), &failed, operations, &vectors, &result) ||
		result.state != 0xe78c07b1134a6b30 || result.row.Iterations != 2 || result.row.ChecksPassed != 12 ||
		integerCalls != 1 || shaCalls != 0 || scalarCalls != 0 || result.overflow || failed.Load() ||
		result.failures != (CPUCalculationFailures{}) || result.row.Mismatch != nil {
		t.Fatal("uint64 recurrence wrapping changed evidence accounting")
	}
}

func TestCPUStepPeriodicBoundaryKeepsCompletedScalarCoverage(t *testing.T) {
	for _, mode := range []string{"late_success", "late_mismatch", "late_recurrence"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Unix(100, 0)
			deadline := now.Add(time.Second)
			result := cpuWorkerResult{row: CPUScalarWorker{Index: 5, Iterations: 256, ChecksPassed: 12}, state: 0x2dded990310996cf}
			vectors := scalarVectors()
			operations := productionCPUOperations()
			realInteger, realScalar := operations.integer, operations.scalar
			operations.now = func() time.Time { return now }
			integerCalls, scalarCalls := 0, 0
			operations.integer = func(v uint64) uint64 {
				integerCalls++
				if mode == "late_recurrence" && integerCalls == 2 {
					now = deadline
				}
				return realInteger(v)
			}
			operations.scalar = func(worker, slot int, operation byte, left, right float64) float64 {
				scalarCalls++
				value := realScalar(worker, slot, operation, left, right)
				if slot == 11 && mode != "late_recurrence" {
					now = deadline
					if mode == "late_mismatch" {
						value = math.Float64frombits(math.Float64bits(value) ^ 1)
					}
				}
				return value
			}
			var failed atomic.Bool
			if stepCPUWorker(context.Background(), deadline, &failed, operations, &vectors, &result) ||
				result.row.Iterations != 256 || result.state != 0x2dded990310996cf || scalarCalls != 12 || result.overflow ||
				result.failures.Integer || result.failures.SHA256 {
				t.Fatal("periodic step changed state or failed to reach the last vector")
			}
			wantChecks, wantInteger := uint64(23), 1
			if mode == "late_recurrence" {
				wantChecks, wantInteger = 24, 2
			}
			if result.row.ChecksPassed != wantChecks || integerCalls != wantInteger ||
				result.failures.Scalar != (mode == "late_mismatch") || failed.Load() != (mode == "late_mismatch") {
				t.Fatal("completed checks or stop accounting changed")
			}
			if mode == "late_mismatch" {
				if result.row.Mismatch == nil || result.row.Mismatch.Vector != "div3" ||
					result.row.Mismatch.ExpectedBits != "3d70000000000000" || result.row.Mismatch.ActualBits != "3d70000000000001" {
					t.Fatal("periodic late mismatch lost")
				}
			} else if result.row.Mismatch != nil {
				t.Fatal("periodic scalar mismatch invented")
			}
		})
	}
}
