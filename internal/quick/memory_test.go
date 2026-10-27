package quick

import (
	"context"
	"math"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestMemoryInversionKernelTraversal(t *testing.T) {
	buffer := []byte{0, 0, 0}
	if n, m := memoryChunk(buffer, 0, 6, 0, 3); n != 3 || m != nil {
		t.Fatal("initialization failed")
	}
	want := [][]byte{{0x55, 0xaa, 0xaa}, {0x55, 0x55, 0xaa}, {0x55, 0x55, 0x55}, {0x55, 0x55, 0xaa}, {0x55, 0xaa, 0xaa}, {0xaa, 0xaa, 0xaa}}
	for i := range want {
		phase := 7
		if i >= 3 {
			phase = 8
		}
		if n, m := memoryChunk(buffer, 0, phase, i%3, 1); n != 1 || m != nil || !reflect.DeepEqual(buffer, want[i]) {
			t.Fatalf("transition %d: bytes=%x completed=%d mismatch=%v", i, buffer, n, m)
		}
	}
	if n, m := memoryChunk(buffer, 0, 9, 0, 3); n != 3 || m != nil {
		t.Fatal("final verification failed")
	}
}

func TestMemoryKernelCorruptionsAndOverwriteControl(t *testing.T) {
	for _, phase := range []int{1, 3, 5, 7, 8, 9} {
		for fault := 0; fault < 3; fault++ {
			t.Run(strconv.Itoa(phase)+"/"+strconv.Itoa(fault), func(t *testing.T) {
				buffer := []byte{0xaa, 0xaa, 0xaa}
				switch phase {
				case 1:
					buffer = []byte{0, 0, 0}
				case 3:
					buffer = []byte{255, 255, 255}
				case 5:
					buffer = []byte{0, 158, 60} // independent high-byte oracle
				case 8:
					buffer = []byte{0x55, 0x55, 0x55}
				}
				expected := buffer[fault]
				buffer[fault] ^= 1
				n, m := memoryChunk(buffer, 0, phase, 0, 3)
				units := fault
				if phase == 8 {
					units = 2 - fault
				}
				if n != int64(units) || m == nil || m.Offset != int64(fault) || m.ExpectedByte != int(expected) || m.ActualByte != int(expected^1) || buffer[fault] != expected^1 {
					t.Fatal("wrong first mismatch, direction or overwrite")
				}
			})
		}
	}
	// A logical alias model is represented by equivalent corruption between
	// real kernel calls; distinct Go slice indices are not physical aliases.
	for _, phase := range []int{7, 8} {
		buffer := []byte{0xaa, 0xaa, 0xaa}
		if phase == 8 {
			buffer = []byte{0x55, 0x55, 0x55}
		}
		memoryChunk(buffer, 0, phase, 0, 1)
		written := 0
		if phase == 8 {
			written = 2
		}
		buffer[1] = buffer[written]
		if n, m := memoryChunk(buffer, 0, phase, 1, 1); n != 0 || m == nil || m.Offset != 1 {
			t.Fatal("neighbor corruption disappeared")
		}
	}
	buffer := []byte{0, 0xaa, 0xaa}
	memoryChunk(buffer, 0, 6, 0, 3)
	if _, m := memoryChunk(buffer, 0, 7, 0, 3); m != nil {
		t.Fatal("pre-overwrite corruption should not claim detection")
	}
}

func memoryTestFactory(captured **safetyMonitor) safetyMonitorFactory {
	return func(ctx context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
		m := startSafetyMonitor(ctx, root, timeout, limit, func(context.Context, string) ThermalSnapshot {
			return ThermalSnapshot{Coverage: ThermalUnavailable}
		}, time.Hour, time.Second, 2*time.Second)
		*captured = m
		return m
	}
}

func TestMemoryCoordinatorLateChunksAndFaultPriority(t *testing.T) {
	for _, mode := range []string{"deadline", "cancel", "late_fault", "timely_fault", "heat_fault"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clock := time.Now()
			var monitor *safetyMonitor
			calls := 0
			fault := mode == "late_fault" || mode == "timely_fault" || mode == "heat_fault"
			phaseWanted := 0
			if fault {
				phaseWanted = 8
			}
			operations := memoryOperations{now: func() time.Time { return clock }}
			operations.beforeChunk = func(buffer []byte, _, phase, first, _ int) {
				calls++
				if fault && phase == phaseWanted && first == 0 {
					buffer[len(buffer)-2] = 0
				}
			}
			operations.afterChunk = func(_ []byte, _, phase, first, _ int) {
				if phase != phaseWanted || first != 0 {
					return
				}
				switch mode {
				case "deadline", "late_fault":
					clock, _ = monitor.context.Deadline()
				case "cancel":
					cancel()
				case "heat_fault":
					monitor.complete(thermalCompletion{snapshot: ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 100000}, finished: time.Now(), invoked: true}, time.Now().Add(time.Second))
				}
			}
			got := runMemoryWithOperations(ctx, MemoryOptions{Bytes: minimumMemoryBytes, Passes: 1, GlobalTimeout: 10 * time.Second, ThermalLimit: 90000}, memoryTestFactory(&monitor), operations)
			if !got.Safety.CleanupConfirmed {
				t.Fatal("monitor did not join")
			}
			if !fault {
				if got.Status != StatusAborted || calls != 1 || got.Coverage.CompletedPhases != 0 || got.Coverage.PhaseBytes != 0 || got.Coverage.UncreditedBytes != memoryChunkBytes || got.Coverage.WrittenBytes != 0 || got.ExaminedBytes != 0 {
					t.Fatalf("late chunk credited: %+v", got)
				}
				return
			}
			credited, pending := int64(0), int64(1)
			if mode == "timely_fault" {
				credited, pending = 1, 0
			}
			if got.Status != StatusFail || got.Mismatch == nil || got.Mismatch.Offset != minimumMemoryBytes-2 || got.Mismatch.ExpectedByte != 0x55 || got.Coverage.CompletedPhases != 8 || got.Coverage.PhaseBytes != credited || got.Coverage.UncreditedBytes != pending || got.ExaminedBytes != 4*minimumMemoryBytes+credited || got.Coverage.WrittenBytes != 5*minimumMemoryBytes+credited {
				t.Fatalf("fault/credit differs: %+v", got)
			}
		})
	}
}

func TestMemoryCursorOverflowIsStructurallyUnsignable(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		v := MemoryResult{Schema: MemorySchema, Status: StatusError, AllocatedBytes: minimumMemoryBytes, Passes: 1, ExaminedBytes: math.MaxInt64,
			Coverage: MemoryCoverage{Suite: MemorySuite, CompletedPhases: 1, WrittenBytes: minimumMemoryBytes}}
		if mismatch {
			v.Status = StatusFail
			v.Mismatch = &MemoryMismatch{ExpectedByte: 0, ActualByte: 1}
		}
		if commitMemoryChunk(&v, 1) || v.Schema != "" || mismatch && v.Status != StatusFail {
			t.Fatal("overflow regained signable schema or downgraded fault")
		}
	}
}

func TestMemoryFinalChunkAndPassAdmissionRetainExactCoverage(t *testing.T) {
	for _, mode := range []string{"late_chunk", "final_deadline", "final_cancel", "complete"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clock := time.Now()
			var monitor *safetyMonitor
			lastChunk, afterLastReads, chunks := false, 0, 0
			operations := memoryOperations{now: func() time.Time {
				if lastChunk {
					afterLastReads++
					if mode == "late_chunk" || afterLastReads == 2 && mode == "final_deadline" {
						clock, _ = monitor.context.Deadline()
					}
				}
				return clock
			}}
			operations.beforeChunk = func([]byte, int, int, int, int) { chunks++ }
			operations.afterChunk = func(buffer []byte, _, phase, first, count int) {
				lastChunk = phase == 9 && first+count == len(buffer)
			}
			// The clock callback follows monitor.failure. Cancel from the first
			// final-chunk clock read: that chunk is accepted, but the distinct
			// provisional-pass check must observe cancellation before success.
			readClock := operations.now
			operations.now = func() time.Time {
				now := readClock()
				if lastChunk && afterLastReads == 1 && mode == "final_cancel" {
					cancel()
				}
				return now
			}
			got := runMemoryWithOperations(ctx, MemoryOptions{Bytes: minimumMemoryBytes, Passes: 1, GlobalTimeout: 10 * time.Second, ThermalLimit: 90000}, memoryTestFactory(&monitor), operations)
			if !got.Safety.CleanupConfirmed || chunks != 10*int(minimumMemoryBytes)/memoryChunkBytes || got.Mismatch != nil {
				t.Fatalf("final boundary lost work or cleanup: %+v", got)
			}
			if mode == "late_chunk" {
				if got.Status != StatusAborted || got.Coverage.CompletedPhases != 9 || got.Coverage.PhaseBytes != minimumMemoryBytes-memoryChunkBytes || got.Coverage.UncreditedBytes != memoryChunkBytes || got.ExaminedBytes != 6*minimumMemoryBytes-memoryChunkBytes || got.Coverage.WrittenBytes != 6*minimumMemoryBytes {
					t.Fatalf("late final chunk credited: %+v", got)
				}
				return
			}
			want := StatusAborted
			if mode == "complete" {
				want = StatusPass
			}
			if got.Status != want || got.Coverage.CompletedPhases != 10 || got.Coverage.CompletedPasses != 1 || got.Coverage.PhaseBytes != 0 || got.Coverage.UncreditedBytes != 0 || got.ExaminedBytes != 6*minimumMemoryBytes || got.Coverage.WrittenBytes != 6*minimumMemoryBytes {
				t.Fatalf("final admission changed completed work: %+v", got)
			}
		})
	}
}

func TestMemoryAllocationAdmissionRechecksTheLastObservedTime(t *testing.T) {
	var monitor *safetyMonitor
	reads, chunks := 0, 0
	operations := memoryOperations{now: func() time.Time {
		reads++
		deadline, _ := monitor.context.Deadline()
		if reads == 1 {
			return deadline.Add(-time.Nanosecond)
		}
		return deadline
	}, beforeChunk: func([]byte, int, int, int, int) { chunks++ }}
	got := runMemoryWithOperations(t.Context(), MemoryOptions{Bytes: minimumMemoryBytes, Passes: 1, GlobalTimeout: time.Second, ThermalLimit: 90000}, memoryTestFactory(&monitor), operations)
	if got.Status != StatusAborted || got.AllocatedBytes != 0 || got.DurationMilliseconds != 0 || chunks != 0 || !got.Safety.CleanupConfirmed {
		t.Fatalf("allocation admitted after observed expiry: %+v", got)
	}
}

func TestMemorySelectorRejectsSignedWrapAndPreservesBounds(t *testing.T) {
	for input, expected := range map[string]int64{
		"-9007199254740993": minimumMemoryBytes - 1, "9223372036854775808": minimumMemoryBytes - 1,
		"-1": minimumMemoryBytes - 1, "bad": minimumMemoryBytes - 1, "16383": minimumMemoryBytes - 1,
		"16384": 4 << 20, "262143": 63 << 20, "262144": 64 << 20, "9223372036854775807": 64 << 20,
	} {
		t.Run(input, func(t *testing.T) {
			root := t.TempDir()
			writeQuickFixture(t, root, "proc/meminfo", "MemAvailable: "+input+" kB\n")
			if got := RecommendedMemoryBytes(root); got != expected {
				t.Fatalf("selected %d, want %d", got, expected)
			}
		})
	}
	if RecommendedMemoryBytes(t.TempDir()) != minimumMemoryBytes {
		t.Fatal("unreadable-file fallback changed")
	}
}
