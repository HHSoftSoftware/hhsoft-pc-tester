package quickevidence

import (
	"fmt"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

func TestMemoryAllWorkloadSizesAndPartialPhaseCounters(t *testing.T) {
	for _, n := range []int64{4 << 20, (4 << 20) + 1, 16 << 20, 64 << 20, 512 << 20} {
		for _, passes := range []int{1, 2, 8} {
			t.Run(fmt.Sprintf("bytes%d/passes%d", n, passes), func(t *testing.T) {
				for phase := 0; phase < 10; phase++ {
					v := memoryFixture()
					v.AllocatedBytes, v.Passes, v.Status = n, passes, quick.StatusAborted
					v.Coverage.CompletedPhases, v.Coverage.CompletedPasses = 10*(passes-1)+phase, passes-1
					v.ExaminedBytes, v.Coverage.WrittenBytes = 0, 0
					for i := 0; i < v.Coverage.CompletedPhases; i++ {
						for _, event := range []int{1, 3, 5, 7, 8, 9} {
							if i%10 == event {
								v.ExaminedBytes += n
							}
						}
						for _, event := range []int{0, 2, 4, 6, 7, 8} {
							if i%10 == event {
								v.Coverage.WrittenBytes += n
							}
						}
					}
					v.Coverage.PhaseBytes = 256 << 10
					for _, event := range []int{1, 3, 5, 7, 8, 9} {
						if phase == event {
							v.ExaminedBytes += 256 << 10
						}
					}
					for _, event := range []int{0, 2, 4, 6, 7, 8} {
						if phase == event {
							v.Coverage.WrittenBytes += 256 << 10
						}
					}
					if _, err := decodeMemoryFixture(t, v); err != nil {
						t.Fatalf("valid partial cursor rejected: phase%d", phase)
					}
					v.Coverage.PhaseBytes++
					if _, err := decodeMemoryFixture(t, v); err == nil {
						t.Fatal("unbound partial unit accepted")
					}
				}
				v := memoryFixture()
				v.AllocatedBytes, v.Passes = n, passes
				v.Coverage.CompletedPhases, v.Coverage.CompletedPasses = 10*passes, passes
				// Literal six sweeps widened before multiplication; maximum is
				// 25,769,803,776, exceeding both 32-bit signed and unsigned ranges.
				v.ExaminedBytes = 6 * n * int64(passes)
				v.Coverage.WrittenBytes = v.ExaminedBytes
				if n == 512<<20 && passes == 8 && v.ExaminedBytes != 25769803776 {
					t.Fatal("independent maximum changed")
				}
				if _, err := decodeMemoryFixture(t, v); err != nil {
					t.Fatal("complete bounded workload rejected", err)
				}
			})
		}
	}
}
