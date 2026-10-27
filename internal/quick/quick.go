package quick

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	CPUSchemaV2          = "hhsoft.pc-tester.cpu/v2"
	CPUSchema            = "hhsoft.pc-tester.cpu/v3"
	MemorySchema         = "hhsoft.pc-tester.memory/v2"
	defaultMemoryBytes   = 64 << 20
	minimumMemoryBytes   = 4 << 20
	maximumMemoryBytes   = 512 << 20
	expectedIntegerRound = uint64(0xeec487b7604b67d5)
)

type Status string

const (
	StatusPass         Status = "pass"
	StatusFail         Status = "fail"
	StatusInconclusive Status = "inconclusive"
	StatusError        Status = "error"
	StatusAborted      Status = "aborted"
)

type CPUOptions struct {
	Duration      time.Duration
	GlobalTimeout time.Duration
	Workers       int
	Root          string
	ThermalLimit  int
}

type CPUResult struct {
	Schema               string                 `json:"schema"`
	Status               Status                 `json:"status"`
	Workers              int                    `json:"workers"`
	Iterations           uint64                 `json:"iterations"`
	StateAggregate       uint64                 `json:"state_aggregate"`
	KnownAnswerSHA256    string                 `json:"known_answer_sha256"`
	DurationMilliseconds int64                  `json:"duration_ms"`
	Scalar               CPUScalarEvidence      `json:"scalar"`
	CalculationFailures  CPUCalculationFailures `json:"calculation_failures"`
	Safety               SafetyEvidence         `json:"safety"`
	Detail               string                 `json:"detail,omitempty"`
}

type MemoryOptions struct {
	Bytes         int64
	Passes        int
	GlobalTimeout time.Duration
	Root          string
	ThermalLimit  int
}

type MemoryResult struct {
	Schema               string         `json:"schema"`
	Status               Status         `json:"status"`
	AllocatedBytes       int64          `json:"allocated_bytes"`
	ExaminedBytes        int64          `json:"examined_bytes"`
	Passes               int            `json:"passes"`
	PatternsPerPass      int            `json:"patterns_per_pass"`
	DurationMilliseconds int64          `json:"duration_ms"`
	Safety               SafetyEvidence `json:"safety"`
	FailureOffset        int64          `json:"failure_offset,omitempty"`
	ExpectedByte         int            `json:"expected_byte,omitempty"`
	ActualByte           int            `json:"actual_byte,omitempty"`
	Detail               string         `json:"detail,omitempty"`
}

type safetyMonitorFactory func(context.Context, string, time.Duration, int) *safetyMonitor

func RunMemory(ctx context.Context, options MemoryOptions) MemoryResult {
	return runMemoryWith(ctx, options, newSafetyMonitor)
}

func runMemoryWith(ctx context.Context, options MemoryOptions, startMonitor safetyMonitorFactory) (result MemoryResult) {
	result = MemoryResult{Schema: MemorySchema, Status: StatusError, PatternsPerPass: 3, Safety: unstartedSafety()}
	if options.Passes < 1 || options.Passes > 8 || options.GlobalTimeout < time.Second || options.GlobalTimeout > 10*time.Minute || options.ThermalLimit < 50000 || options.ThermalLimit > 125000 {
		result.Detail = "invalid memory quick-test limits"
		return result
	}
	result.Safety.GlobalTimeoutMilliseconds = options.GlobalTimeout.Milliseconds()
	result.Safety.ThermalLimitMilliCelsius = options.ThermalLimit
	if options.Root == "" {
		options.Root = "/"
	}
	if options.Bytes == 0 {
		options.Bytes = RecommendedMemoryBytes(options.Root)
	}
	if options.Bytes < minimumMemoryBytes {
		result.Status = StatusInconclusive
		result.Detail = "insufficient safely available memory"
		return result
	}
	if options.Bytes > maximumMemoryBytes {
		result.Detail = "requested memory exceeds the bounded quick-test limit"
		return result
	}
	result.AllocatedBytes = options.Bytes
	result.Passes = options.Passes
	monitor := startMonitor(ctx, options.Root, options.GlobalTimeout, options.ThermalLimit)
	defer finishSafety(monitor, &result.Status, &result.Detail, &result.Safety)
	if err := monitor.failure(); err != nil {
		result.Status = safetyStatus(err)
		result.Detail = safetyDetail(err)
		return result
	}
	started := time.Now()
	buffer := make([]byte, int(options.Bytes))
	for pass := 0; pass < options.Passes; pass++ {
		for pattern := 0; pattern < result.PatternsPerPass; pattern++ {
			for offset := range buffer {
				buffer[offset] = memoryPattern(pass, pattern, offset)
				if offset%(256<<10) == 0 {
					if err := monitor.failure(); err != nil {
						result.Status = safetyStatus(err)
						result.Detail = safetyDetail(err)
						result.DurationMilliseconds = time.Since(started).Milliseconds()
						return result
					}
				}
			}
			for offset, actual := range buffer {
				expected := memoryPattern(pass, pattern, offset)
				if actual != expected {
					result.Status = StatusFail
					result.FailureOffset = int64(offset)
					result.ExpectedByte = int(expected)
					result.ActualByte = int(actual)
					result.Detail = "memory pattern mismatch"
					result.DurationMilliseconds = time.Since(started).Milliseconds()
					return result
				}
				result.ExaminedBytes++
				if offset%(256<<10) == 0 {
					if err := monitor.failure(); err != nil {
						result.Status = safetyStatus(err)
						result.Detail = safetyDetail(err)
						result.DurationMilliseconds = time.Since(started).Milliseconds()
						return result
					}
				}
			}
		}
	}
	runtime.KeepAlive(buffer)
	result.DurationMilliseconds = time.Since(started).Milliseconds()
	if err := monitor.failure(); err != nil {
		result.Status = safetyStatus(err)
		result.Detail = safetyDetail(err)
		return result
	}
	result.Status = StatusPass
	return result
}

func RecommendedMemoryBytes(root string) int64 {
	data, err := os.ReadFile(filepath.Join(filepath.Clean(root), "proc/meminfo"))
	if err != nil {
		return minimumMemoryBytes
	}
	var availableKiB int64
	for _, line := range strings.Split(string(data), "\n") {
		name, value, found := strings.Cut(line, ":")
		if !found || !strings.EqualFold(strings.TrimSpace(name), "MemAvailable") {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) > 0 {
			availableKiB, _ = strconv.ParseInt(fields[0], 10, 64)
		}
		break
	}
	result := availableKiB * 1024 / 4
	if result > defaultMemoryBytes {
		result = defaultMemoryBytes
	}
	if result < minimumMemoryBytes {
		return minimumMemoryBytes - 1
	}
	return result / (1 << 20) * (1 << 20)
}

var (
	knownAnswerInput    = []byte("abc")
	expectedKnownAnswer = [32]byte{0xba, 0x78, 0x16, 0xbf, 0x8f, 0x01, 0xcf, 0xea, 0x41, 0x41, 0x40, 0xde, 0x5d, 0xae, 0x22, 0x23, 0xb0, 0x03, 0x61, 0xa3, 0x96, 0x17, 0x7a, 0x9c, 0xb4, 0x10, 0xff, 0x61, 0xf2, 0x00, 0x15, 0xad}
)

func integerRound(value uint64) uint64 {
	for round := 0; round < 4096; round++ {
		value ^= value << 13
		value ^= value >> 7
		value ^= value << 17
		value += 0x9e3779b97f4a7c15
	}
	return value
}
func memoryPattern(pass, pattern, offset int) byte {
	switch pattern {
	case 0:
		return byte(pass)
	case 1:
		return ^byte(pass)
	default:
		return byte(uint64(offset)*0x9e3779b97f4a7c15>>56) ^ byte(pass*67)
	}
}

func safetyStatus(err error) Status {
	if errors.Is(err, ErrThermalLimit) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return StatusAborted
	}
	return StatusError
}

func safetyDetail(err error) string {
	switch {
	case errors.Is(err, ErrThermalLimit):
		return "thermal safety limit reached"
	case errors.Is(err, context.DeadlineExceeded):
		return "global deadline exceeded"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "safety monitor error"
	}
}
