package quick

import (
	"fmt"
	"testing"
	"time"
)

func TestMemoryLateMismatchPrefixesIncludeZeroChunkEdgeAndShortTail(t *testing.T) {
	const n = int64(minimumMemoryBytes + 2)
	for _, offset := range []int64{0, memoryChunkBytes - 1, n - 1} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			clock := time.Now()
			var monitor *safetyMonitor
			first := offset / int64(memoryChunkBytes) * int64(memoryChunkBytes)
			ops := memoryOperations{now: func() time.Time { return clock }}
			ops.beforeChunk = func(buffer []byte, _, phase, start, _ int) {
				if phase == 7 && int64(start) == first {
					buffer[offset] = 0
				}
			}
			ops.afterChunk = func(_ []byte, _, phase, start, _ int) {
				if phase == 7 && int64(start) == first {
					clock, _ = monitor.context.Deadline()
				}
			}
			got := runMemoryWithOperations(t.Context(), MemoryOptions{Bytes: n, Passes: 1, GlobalTimeout: 10 * time.Second, ThermalLimit: 90000}, memoryTestFactory(&monitor), ops)
			if got.Status != StatusFail || got.Mismatch == nil || got.Mismatch.Offset != offset || got.Mismatch.ExpectedByte != 0xaa || got.Mismatch.ActualByte != 0 || got.Coverage.CompletedPhases != 7 || got.Coverage.PhaseBytes != first || got.Coverage.UncreditedBytes != offset-first || got.ExaminedBytes != 3*n+first || got.Coverage.WrittenBytes != 4*n+first || !got.Safety.CleanupConfirmed {
				t.Fatalf("late fault prefix changed: %+v", got)
			}
		})
	}
}
