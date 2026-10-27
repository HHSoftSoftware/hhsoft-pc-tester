package quick

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestMemoryOldPatternTwoMatchesIndependentBitOracles(t *testing.T) {
	offsets := []int{0, 1, 2, 255, 256, 262143, 262144, 4194303}
	for _, row := range []struct {
		pass     int
		expected []byte
	}{
		{0, []byte{0, 158, 60, 153, 55, 72, 230, 208}},
		{1, []byte{67, 221, 127, 218, 116, 11, 165, 147}},
		{7, []byte{213, 75, 233, 76, 226, 157, 51, 5}},
	} {
		buffer := make([]byte, minimumMemoryBytes)
		for first := 0; first < len(buffer); first += memoryChunkBytes {
			memoryChunk(buffer, row.pass, 4, first, min(memoryChunkBytes, len(buffer)-first))
		}
		for i, offset := range offsets {
			if buffer[offset] != row.expected[i] {
				t.Fatal("old pattern bit oracle changed")
			}
			if n, m := memoryChunk(buffer, row.pass, 5, offset, 1); n != 1 || m != nil {
				t.Fatal("actual byte read disagrees with oracle")
			}
		}
	}
}

func TestMemoryKernelFirstFailureAcrossChunkAndPassBoundaries(t *testing.T) {
	const n = memoryChunkBytes + 2
	for _, pass := range []int{0, 1, 7} {
		for _, phase := range []int{1, 3, 5, 7, 8, 9} {
			for _, fault := range []int{0, memoryChunkBytes - 1, memoryChunkBytes, memoryChunkBytes + 1} {
				t.Run(fmt.Sprintf("pass%d/phase%d/offset%d", pass, phase, fault), func(t *testing.T) {
					buffer := make([]byte, n)
					for prior := 0; prior < phase; prior++ {
						for first := 0; first < n; first += memoryChunkBytes {
							if _, m := memoryChunk(buffer, pass, prior, first, min(memoryChunkBytes, n-first)); m != nil {
								t.Fatal("unexpected setup mismatch")
							}
						}
					}
					expected := buffer[fault]
					buffer[fault] ^= 1
					matched := int64(0)
					var mismatch *MemoryMismatch
					for first := 0; first < n; first += memoryChunkBytes {
						count, m := memoryChunk(buffer, pass, phase, first, min(memoryChunkBytes, n-first))
						matched += count
						if m != nil {
							mismatch = m
							break
						}
					}
					want := fault
					if phase == 8 {
						want = n - 1 - fault
					}
					if matched != int64(want) || mismatch == nil || mismatch.Offset != int64(fault) || mismatch.ExpectedByte != int(expected) || mismatch.ActualByte != int(expected^1) || buffer[fault] != expected^1 {
						t.Fatal("first fault, traversal or no-overwrite rule changed")
					}
				})
			}
		}
	}
}

func TestMemoryStopsBeforeEachPhaseWithoutAdmittingItsChunk(t *testing.T) {
	for phase := 0; phase < 10; phase++ {
		t.Run(fmt.Sprint(phase), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clock := time.Now()
			var monitor *safetyMonitor
			chunks := 0
			ops := memoryOperations{now: func() time.Time { return clock }, beforeChunk: func(_ []byte, _, current, first, _ int) {
				if current == phase && first == 0 {
					cancel()
				}
			}, afterChunk: func([]byte, int, int, int, int) { chunks++ }}
			got := runMemoryWithOperations(ctx, MemoryOptions{Bytes: minimumMemoryBytes, Passes: 1, GlobalTimeout: 10 * time.Second, ThermalLimit: 90000}, memoryTestFactory(&monitor), ops)
			reads, writes := int64(0), int64(0)
			for previous := 0; previous < phase; previous++ {
				for _, event := range []int{1, 3, 5, 7, 8, 9} {
					if previous == event {
						reads += minimumMemoryBytes
					}
				}
				for _, event := range []int{0, 2, 4, 6, 7, 8} {
					if previous == event {
						writes += minimumMemoryBytes
					}
				}
			}
			if got.Status != StatusAborted || got.Coverage.CompletedPhases != phase || got.Coverage.PhaseBytes != 0 || got.Coverage.UncreditedBytes != 0 || got.ExaminedBytes != reads || got.Coverage.WrittenBytes != writes || chunks != phase*int(minimumMemoryBytes)/memoryChunkBytes || !got.Safety.CleanupConfirmed {
				t.Fatalf("phase admission changed: %+v", got)
			}
		})
	}
}

func TestMemoryShortFinalChunkAndTwoCompletePasses(t *testing.T) {
	for _, late := range []bool{false, true} {
		clock := time.Now()
		var monitor *safetyMonitor
		ops := memoryOperations{now: func() time.Time { return clock }, afterChunk: func(buffer []byte, pass, phase, first, count int) {
			if late && pass == 1 && phase == 9 && first+count == len(buffer) {
				clock, _ = monitor.context.Deadline()
			}
		}}
		n := int64(minimumMemoryBytes + 1)
		got := runMemoryWithOperations(t.Context(), MemoryOptions{Bytes: n, Passes: 2, GlobalTimeout: 10 * time.Second, ThermalLimit: 90000}, memoryTestFactory(&monitor), ops)
		if !got.Safety.CleanupConfirmed || got.Mismatch != nil || got.Coverage.WrittenBytes != 12*n {
			t.Fatalf("two-pass writes changed: %+v", got)
		}
		if late {
			if got.Status != StatusAborted || got.Coverage.CompletedPhases != 19 || got.Coverage.CompletedPasses != 1 || got.Coverage.PhaseBytes != n-1 || got.Coverage.UncreditedBytes != 1 || got.ExaminedBytes != 12*n-1 {
				t.Fatalf("short late chunk credited: %+v", got)
			}
		} else if got.Status != StatusPass || got.Coverage.CompletedPhases != 20 || got.Coverage.CompletedPasses != 2 || got.ExaminedBytes != 12*n || got.Coverage.UncreditedBytes != 0 {
			t.Fatalf("two passes incomplete: %+v", got)
		}
	}
}
