package quick

import (
	"context"
	"math"
	"runtime"
	"time"
)

const MemorySuite = "byte-patterns-aa55-inversions-v1"
const memoryChunkBytes = 256 << 10

type MemoryResult struct {
	Schema               string          `json:"schema"`
	Status               Status          `json:"status"`
	AllocatedBytes       int64           `json:"allocated_bytes"`
	ExaminedBytes        int64           `json:"examined_bytes"`
	Passes               int             `json:"passes"`
	PatternsPerPass      int             `json:"patterns_per_pass"`
	DurationMilliseconds int64           `json:"duration_ms"`
	Coverage             MemoryCoverage  `json:"memory_coverage"`
	Safety               SafetyEvidence  `json:"safety"`
	Mismatch             *MemoryMismatch `json:"mismatch,omitempty"`
	Detail               string          `json:"detail,omitempty"`
}

type MemoryCoverage struct {
	Suite           string `json:"suite"`
	CompletedPhases int    `json:"completed_phases"`
	CompletedPasses int    `json:"completed_passes"`
	PhaseBytes      int64  `json:"phase_bytes"`
	UncreditedBytes int64  `json:"uncredited_bytes"`
	WrittenBytes    int64  `json:"written_bytes"`
}

type MemoryMismatch struct {
	Offset       int64 `json:"offset"`
	ExpectedByte int   `json:"expected_byte"`
	ActualByte   int   `json:"actual_byte"`
}

// Hooks are private and run only at chunk boundaries. The actual byte kernel
// has no callbacks, and RunMemory never accepts an alternate suite or kernel.
type memoryOperations struct {
	now                     func() time.Time
	beforeChunk, afterChunk func([]byte, int, int, int, int)
}

func RunMemory(ctx context.Context, options MemoryOptions) MemoryResult {
	return runMemoryWith(ctx, options, newSafetyMonitor)
}

func runMemoryWith(ctx context.Context, options MemoryOptions, startMonitor safetyMonitorFactory) MemoryResult {
	return runMemoryWithOperations(ctx, options, startMonitor, memoryOperations{now: time.Now})
}

func runMemoryWithOperations(ctx context.Context, options MemoryOptions, startMonitor safetyMonitorFactory, operations memoryOperations) (result MemoryResult) {
	result = MemoryResult{Schema: MemorySchema, Status: StatusError, PatternsPerPass: 3,
		Coverage: MemoryCoverage{Suite: MemorySuite}, Safety: unstartedSafety()}
	if options.Passes < 1 || options.Passes > 8 || options.GlobalTimeout < time.Second || options.GlobalTimeout > 10*time.Minute || options.ThermalLimit < 50000 || options.ThermalLimit > 125000 {
		result.Detail = "invalid memory quick-test limits"
		return result
	}
	result.Passes = options.Passes
	result.Safety.GlobalTimeoutMilliseconds = options.GlobalTimeout.Milliseconds()
	result.Safety.ThermalLimitMilliCelsius = options.ThermalLimit
	if options.Root == "" {
		options.Root = "/"
	}
	if options.Bytes == 0 {
		options.Bytes = RecommendedMemoryBytes(options.Root)
	}
	if options.Bytes < minimumMemoryBytes {
		result.Status, result.Detail = StatusInconclusive, "insufficient safely available memory"
		return result
	}
	if options.Bytes > maximumMemoryBytes {
		result.Detail = "requested memory exceeds the bounded quick-test limit"
		return result
	}
	monitor := startMonitor(ctx, options.Root, options.GlobalTimeout, options.ThermalLimit)
	defer finishSafety(monitor, &result.Status, &result.Detail, &result.Safety)
	deadline, bounded := monitor.context.Deadline()
	if !bounded {
		result.Schema, result.Detail = "", "memory monitor omitted its deadline"
		return result
	}
	stopped := func() error {
		if err := monitor.failure(); err != nil {
			return err
		}
		if !operations.now().Before(deadline) {
			return context.DeadlineExceeded
		}
		return nil
	}
	stop := func(err error) { result.Status, result.Detail = safetyStatus(err), safetyDetail(err) }
	started := operations.now()
	if err := stopped(); err != nil {
		stop(err)
		return result
	}
	defer func() { result.DurationMilliseconds = operations.now().Sub(started).Milliseconds() }()
	buffer := make([]byte, int(options.Bytes))
	defer func() { runtime.KeepAlive(buffer) }()
	result.AllocatedBytes = int64(len(buffer))
	for result.Coverage.CompletedPhases < 10*options.Passes {
		phase, pass := result.Coverage.CompletedPhases%10, result.Coverage.CompletedPhases/10
		first := int(result.Coverage.PhaseBytes)
		count := min(memoryChunkBytes, len(buffer)-first)
		if operations.beforeChunk != nil {
			operations.beforeChunk(buffer, pass, phase, first, count)
		}
		if err := stopped(); err != nil {
			stop(err)
			return result
		}
		completed, mismatch := memoryChunk(buffer, pass, phase, first, count)
		if operations.afterChunk != nil {
			operations.afterChunk(buffer, pass, phase, first, count)
		}
		stopErr := stopped()
		if stopErr == nil {
			if !commitMemoryChunk(&result, completed) {
				result.Detail = "memory evidence counter overflow"
			}
		} else {
			result.Coverage.UncreditedBytes = completed
		}
		// An in-flight fault remains a fault even when its preceding correct
		// units are too late to count as accepted coverage.
		if mismatch != nil {
			result.Mismatch = mismatch
			result.Status, result.Detail = StatusFail, "memory pattern mismatch"
			return result
		}
		if result.Schema == "" {
			return result
		}
		if stopErr != nil {
			stop(stopErr)
			return result
		}
	}
	if err := stopped(); err != nil {
		stop(err)
		return result
	}
	result.Status = StatusPass
	return result
}

// The noinline boundary keeps fills and subsequent comparisons as separate
// calls over actual buffer memory. Counts describe logical operations only.
//
//go:noinline
func memoryChunk(buffer []byte, pass, phase, first, count int) (int64, *MemoryMismatch) {
	for unit := 0; unit < count; unit++ {
		offset := first + unit
		if phase == 8 {
			offset = len(buffer) - 1 - offset
		}
		expected := byte(0xaa)
		if phase < 6 {
			expected = memoryPattern(pass, phase/2, offset)
		} else if phase == 8 {
			expected = 0x55
		}
		if phase < 7 && phase%2 == 0 {
			buffer[offset] = expected
			continue
		}
		actual := buffer[offset]
		if actual != expected {
			return int64(unit), &MemoryMismatch{Offset: int64(offset), ExpectedByte: int(expected), ActualByte: int(actual)}
		}
		if phase == 7 {
			buffer[offset] = 0x55
		} else if phase == 8 {
			buffer[offset] = 0xaa
		}
	}
	return int64(count), nil
}

func commitMemoryChunk(result *MemoryResult, count int64) bool {
	c := &result.Coverage
	invalid := func() bool { result.Schema = ""; return false }
	if result.AllocatedBytes < minimumMemoryBytes || result.AllocatedBytes > maximumMemoryBytes || result.Passes < 1 || result.Passes > 8 ||
		c.CompletedPhases < 0 || c.CompletedPhases >= 10*result.Passes || c.CompletedPasses != c.CompletedPhases/10 ||
		c.PhaseBytes < 0 || c.PhaseBytes >= result.AllocatedBytes || count < 0 || count > min(int64(memoryChunkBytes), result.AllocatedBytes-c.PhaseBytes) ||
		result.ExaminedBytes < 0 || c.WrittenBytes < 0 {
		return invalid()
	}
	phase := c.CompletedPhases % 10
	read, write := phase%2 == 1 || phase == 8, phase%2 == 0 || phase == 7
	if read && result.ExaminedBytes > math.MaxInt64-count || write && c.WrittenBytes > math.MaxInt64-count {
		return invalid()
	}
	if read {
		result.ExaminedBytes += count
	}
	if write {
		c.WrittenBytes += count
	}
	c.PhaseBytes += count
	if c.PhaseBytes == result.AllocatedBytes {
		c.CompletedPhases++
		c.CompletedPasses = c.CompletedPhases / 10
		c.PhaseBytes = 0
	}
	return true
}
