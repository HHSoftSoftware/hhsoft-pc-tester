package quick

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const CPUScalarSuite = "binary64-normal-exact-v1"
const CPUScalarVectorCount = 12

type CPUScalarEvidence struct {
	Suite            string            `json:"suite"`
	VectorCount      int               `json:"vector_count"`
	ChecksPassed     uint64            `json:"checks_passed"`
	WorkersCompleted int               `json:"workers_completed"`
	Workers          []CPUScalarWorker `json:"workers"`
}

type CPUScalarWorker struct {
	Index        int                `json:"index"`
	Iterations   uint64             `json:"iterations"`
	ChecksPassed uint64             `json:"checks_passed"`
	Mismatch     *CPUScalarMismatch `json:"mismatch,omitempty"`
}

type CPUScalarMismatch struct {
	Vector       string `json:"vector"`
	ExpectedBits string `json:"expected_bits"`
	ActualBits   string `json:"actual_bits"`
}

type CPUCalculationFailures struct {
	Integer bool `json:"integer"`
	SHA256  bool `json:"sha256"`
	Scalar  bool `json:"scalar"`
}

type scalarVector struct {
	id                    string
	operation             byte
	left, right, expected uint64
}

// A fresh value keeps the production operands private and immutable to callers.
func scalarVectors() [CPUScalarVectorCount]scalarVector {
	return [CPUScalarVectorCount]scalarVector{
		{"add1", '+', 0x3ff8000000000000, 0x4002000000000000, 0x400e000000000000},
		{"add2", '+', 0xc020000000000000, 0x3fe0000000000000, 0xc01e000000000000},
		{"add3", '+', 0x3eb0000000000000, 0x3ea0000000000000, 0x3eb8000000000000},
		{"sub1", '-', 0x401f000000000000, 0x400c000000000000, 0x4011000000000000},
		{"sub2", '-', 0xbff4000000000000, 0x4004000000000000, 0xc00e000000000000},
		{"sub3", '-', 0x4270000000000000, 0x4260000000000000, 0x4260000000000000},
		{"mul1", '*', 0x3ff8000000000000, 0x4004000000000000, 0x400e000000000000},
		{"mul2", '*', 0xc008000000000000, 0x3fd0000000000000, 0xbfe8000000000000},
		{"mul3", '*', 0x3eb0000000000000, 0x41d0000000000000, 0x4090000000000000},
		{"div1", '/', 0x401e000000000000, 0x4004000000000000, 0x4008000000000000},
		{"div2", '/', 0xc022000000000000, 0x4010000000000000, 0xc002000000000000},
		{"div3", '/', 0x3e10000000000000, 0x4090000000000000, 0x3d70000000000000},
	}
}

//go:noinline
func scalarRound(operation byte, left, right float64) float64 {
	switch operation {
	case '+':
		return left + right
	case '-':
		return left - right
	case '*':
		return left * right
	default:
		return left / right
	}
}

type cpuOperations struct {
	now     func() time.Time
	integer func(uint64) uint64
	sha     func([]byte) [32]byte
	scalar  func(int, int, byte, float64, float64) float64
}

func productionCPUOperations() cpuOperations {
	return cpuOperations{time.Now, integerRound, sha256.Sum256,
		func(_, _ int, operation byte, left, right float64) float64 {
			return scalarRound(operation, left, right)
		}}
}

type cpuWorkerResult struct {
	row      CPUScalarWorker
	state    uint64
	failures CPUCalculationFailures
	overflow bool
}

func cpuStopped(ctx context.Context, deadline time.Time, failed *atomic.Bool, now func() time.Time) bool {
	return ctx.Err() != nil || failed.Load() || !now().Before(deadline)
}

func hexBits(value uint64) string {
	text := strconv.FormatUint(value, 16)
	return strings.Repeat("0", 16-len(text)) + text
}

func runCPUWorker(ctx context.Context, deadline time.Time, index int, failed *atomic.Bool, operations cpuOperations) (result cpuWorkerResult) {
	result.row.Index = index
	result.state = uint64(index) + 0x0123456789abcdef
	vectors := scalarVectors()
	for !cpuStopped(ctx, deadline, failed, operations.now) {
		if result.row.Iterations&0xff == 0 {
			result.failures.Integer = operations.integer(0x0123456789abcdef) != expectedIntegerRound
			if result.failures.Integer {
				failed.Store(true)
				return result
			}
			if cpuStopped(ctx, deadline, failed, operations.now) {
				return result
			}
			result.failures.SHA256 = operations.sha(knownAnswerInput) != expectedKnownAnswer
			if result.failures.SHA256 {
				failed.Store(true)
				return result
			}
			for slot, vector := range vectors {
				if cpuStopped(ctx, deadline, failed, operations.now) {
					return result
				}
				actual := math.Float64bits(operations.scalar(index, slot, vector.operation, math.Float64frombits(vector.left), math.Float64frombits(vector.right)))
				if actual != vector.expected {
					result.row.Mismatch = &CPUScalarMismatch{vector.id, hexBits(vector.expected), hexBits(actual)}
					result.failures.Scalar = true
					failed.Store(true)
					return result
				}
				if cpuStopped(ctx, deadline, failed, operations.now) {
					return result
				}
				if result.row.ChecksPassed == math.MaxUint64 {
					result.overflow = true
					failed.Store(true)
					return result
				}
				result.row.ChecksPassed++
			}
		}
		if cpuStopped(ctx, deadline, failed, operations.now) {
			return result
		}
		state := operations.integer(result.state)
		if cpuStopped(ctx, deadline, failed, operations.now) {
			return result
		}
		if result.row.Iterations == math.MaxUint64 {
			result.overflow = true
			failed.Store(true)
			return result
		}
		result.state = state
		result.row.Iterations++
	}
	return result
}

func RunCPU(ctx context.Context, options CPUOptions) CPUResult {
	return runCPUWith(ctx, options, newSafetyMonitor)
}

func runCPUWith(ctx context.Context, options CPUOptions, startMonitor safetyMonitorFactory) CPUResult {
	return runCPUWithOperations(ctx, options, startMonitor, productionCPUOperations())
}

func runCPUWithOperations(ctx context.Context, options CPUOptions, startMonitor safetyMonitorFactory, operations cpuOperations) (result CPUResult) {
	result = CPUResult{Schema: CPUSchema, Status: StatusError, KnownAnswerSHA256: hex.EncodeToString(expectedKnownAnswer[:]),
		Scalar: CPUScalarEvidence{Suite: CPUScalarSuite, VectorCount: CPUScalarVectorCount, Workers: []CPUScalarWorker{}}, Safety: unstartedSafety()}
	if options.Duration <= 0 || options.Duration > 5*time.Minute || options.GlobalTimeout < 10*time.Millisecond || options.GlobalTimeout > 10*time.Minute || options.ThermalLimit < 50000 || options.ThermalLimit > 125000 {
		result.Detail = "invalid CPU quick-test limits"
		return result
	}
	if options.Workers <= 0 {
		options.Workers = runtime.NumCPU()
	}
	if options.Workers > 64 {
		options.Workers = 64
	}
	result.Workers = options.Workers
	result.Safety.GlobalTimeoutMilliseconds = options.GlobalTimeout.Milliseconds()
	result.Safety.ThermalLimitMilliCelsius = options.ThermalLimit
	if options.Root == "" {
		options.Root = "/"
	}
	monitor := startMonitor(ctx, options.Root, options.GlobalTimeout, options.ThermalLimit)
	defer finishSafety(monitor, &result.Status, &result.Detail, &result.Safety)
	if err := monitor.failure(); err != nil {
		result.Status, result.Detail = safetyStatus(err), safetyDetail(err)
		return result
	}
	started := time.Now()
	workContext, stopWork := context.WithTimeout(monitor.context, options.Duration)
	defer stopWork()
	deadline, _ := workContext.Deadline()
	var failed atomic.Bool
	var workers sync.WaitGroup
	joined := make([]cpuWorkerResult, options.Workers)
	launched := 0
	for index := range joined {
		if cpuStopped(workContext, deadline, &failed, operations.now) {
			break
		}
		launched++
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			joined[index] = runCPUWorker(workContext, deadline, index, &failed, operations)
		}(index)
	}
	workers.Wait()
	result.DurationMilliseconds = time.Since(started).Milliseconds()
	overflow := collectCPUWorkers(&result, joined[:launched])
	if result.CalculationFailures.Integer || result.CalculationFailures.SHA256 || result.CalculationFailures.Scalar {
		result.Status, result.Detail = StatusFail, "known-answer calculation mismatch"
		return result
	}
	if overflow {
		result.Detail = "CPU evidence counter overflow"
		return result
	}
	if err := monitor.failure(); err != nil {
		result.Status, result.Detail = safetyStatus(err), safetyDetail(err)
		return result
	}
	// A timer notification may arrive after the actual global deadline. The
	// work loop's independent deadline check must not turn that stop into pass.
	if globalDeadline, ok := monitor.context.Deadline(); ok && !operations.now().Before(globalDeadline) {
		result.Status, result.Detail = StatusAborted, "global deadline exceeded"
		return result
	}
	if result.Iterations == 0 || launched != options.Workers || result.Scalar.WorkersCompleted != options.Workers {
		result.Status, result.Detail = StatusInconclusive, "incomplete calculation coverage"
		return result
	}
	result.Status = StatusPass
	return result
}

// An internal counter overflow cannot be serialized as accepted evidence, even
// if a later monitor failure changes its diagnostic text or status.
func collectCPUWorkers(result *CPUResult, joined []cpuWorkerResult) bool {
	overflow := false
	for _, worker := range joined {
		result.Scalar.Workers = append(result.Scalar.Workers, worker.row)
		if worker.row.ChecksPassed >= CPUScalarVectorCount {
			result.Scalar.WorkersCompleted++
		}
		result.CalculationFailures.Integer = result.CalculationFailures.Integer || worker.failures.Integer
		result.CalculationFailures.SHA256 = result.CalculationFailures.SHA256 || worker.failures.SHA256
		result.CalculationFailures.Scalar = result.CalculationFailures.Scalar || worker.failures.Scalar
		if worker.overflow || math.MaxUint64-result.Iterations < worker.row.Iterations || math.MaxUint64-result.Scalar.ChecksPassed < worker.row.ChecksPassed {
			overflow = true
		} else {
			result.Iterations += worker.row.Iterations
			result.Scalar.ChecksPassed += worker.row.ChecksPassed
		}
		// Intentional modulo state aggregation is independent of evidence counts.
		result.StateAggregate += worker.state
	}
	if overflow {
		result.Schema = ""
	}
	return overflow
}
