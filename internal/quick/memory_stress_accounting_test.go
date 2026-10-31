package quick

import (
	"math"
	"reflect"
	"testing"
)

func TestMemoryStressLiteralCycleSeedsAndKernelPhases(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cycle   uint64
		seed    int
		pattern []byte
	}{
		{"zero", 0, 0, []byte{0, 158, 60, 218, 120, 23, 181, 83}},
		{"one", 1, 1, []byte{67, 221, 127, 153, 59, 84, 246, 16}},
		{"seven", 7, 7, []byte{213, 75, 233, 15, 173, 194, 96, 134}},
		{"last_seed", 255, 255, []byte{189, 35, 129, 103, 197, 170, 8, 238}},
		{"wrap", 256, 0, []byte{0, 158, 60, 218, 120, 23, 181, 83}},
		{"after_wrap", 257, 1, []byte{67, 221, 127, 153, 59, 84, 246, 16}},
		{"signed386_edge", 2147483647, 255, []byte{189, 35, 129, 103, 197, 170, 8, 238}},
		{"beyond_signed386", 2147483648, 0, []byte{0, 158, 60, 218, 120, 23, 181, 83}},
		{"uint64_edge", 18446744073709551615, 255, []byte{189, 35, 129, 103, 197, 170, 8, 238}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seed := int(tc.cycle % 256)
			if seed != tc.seed {
				t.Fatal("unsafe or incorrect bounded seed")
			}
			buffer := make([]byte, 8)
			for phase := 0; phase < 10; phase++ {
				n, m := memoryChunk(buffer, seed, phase, 0, 8)
				if n != 8 || m != nil {
					t.Fatalf("phase %d did not complete", phase)
				}
				if phase == 4 && !reflect.DeepEqual(buffer, tc.pattern) {
					t.Fatalf("literal address pattern differs: %v", buffer)
				}
				if phase == 9 && !reflect.DeepEqual(buffer, []byte{170, 170, 170, 170, 170, 170, 170, 170}) {
					t.Fatal("terminal state differs")
				}
			}
		})
	}
}

func TestMemoryStressLiteralReadWriteAndPartialAccounting(t *testing.T) {
	const size = uint64(4194305)
	reads := [...]uint64{0, 0, 1, 1, 2, 2, 3, 3, 4, 5, 6}
	writes := [...]uint64{0, 1, 1, 2, 2, 3, 3, 4, 5, 6, 6}
	for phase := uint64(0); phase <= 10; phase++ {
		r, w, ok := memoryStressTotals(size, phase, 0)
		if !ok || r != reads[phase]*size || w != writes[phase]*size {
			t.Fatalf("literal phase %d totals %d/%d", phase, r, w)
		}
	}
	partial := MemoryStressResult{AllocatedBytes: size, Coverage: MemoryStressCoverage{
		CompletedPhases: 27, CompletedCycles: 2, LoadCycles: 2, PhaseBytes: 3,
		ReadBytes: 62914578, WrittenBytes: 67108883,
	}}
	if !ValidMemoryStressCoverage(partial) {
		t.Fatal("independent two-cycle/phase7 prefix rejected")
	}
	before := partial
	partial.Coverage.UncreditedBytes = 4
	if !ValidMemoryStressCoverage(partial) || partial.Coverage.ReadBytes != before.Coverage.ReadBytes || partial.Coverage.WrittenBytes != before.Coverage.WrittenBytes {
		t.Fatal("uncredited work became accepted work")
	}
	terminal := MemoryStressResult{AllocatedBytes: size, Coverage: MemoryStressCoverage{
		CompletedPhases: 20, CompletedCycles: 2, LoadCycles: 1, ReadBytes: 50331660, WrittenBytes: 50331660, TerminalReadBytes: size,
	}}
	if !ValidMemoryStressCoverage(terminal) || terminal.Coverage.CompletedCycles != 2 {
		t.Fatal("terminal read changed cycle accounting")
	}
	terminal.Coverage.CompletedPhases = 21
	if ValidMemoryStressCoverage(terminal) {
		t.Fatal("terminal read promoted to another phase")
	}
}

func TestMemoryStressCountersRejectOverflowWithoutMutation(t *testing.T) {
	if n, ok := memoryStressAdd(math.MaxUint64-1, 1); !ok || n != math.MaxUint64 {
		t.Fatal("exact add boundary rejected")
	}
	if _, ok := memoryStressAdd(math.MaxUint64, 1); ok {
		t.Fatal("addition wrapped")
	}
	if n, ok := memoryStressMultiply(3074457345618258602, 6); !ok || n != 18446744073709551612 {
		t.Fatal("exact multiply boundary rejected")
	}
	if _, ok := memoryStressMultiply(3074457345618258603, 6); ok {
		t.Fatal("multiplication wrapped")
	}
	if _, _, ok := memoryStressTotals(4194304, math.MaxUint64, 0); ok {
		t.Fatal("phase/cycle totals wrapped")
	}
	v := MemoryStressResult{AllocatedBytes: 4194304, Coverage: MemoryStressCoverage{CompletedPhases: math.MaxUint64, CompletedCycles: math.MaxUint64 / 10}}
	before := v
	if creditMemoryStress(&v, 1, false) || !reflect.DeepEqual(v, before) {
		t.Fatal("invalid/overflow credit partially committed")
	}
	v = MemoryStressResult{AllocatedBytes: 4194304}
	before = v
	if creditMemoryStress(&v, 262145, false) || !reflect.DeepEqual(v, before) {
		t.Fatal("over-chunk request committed")
	}
}

func TestMemoryStressKernelMismatchPrefixIsIndependentOfFaultOffset(t *testing.T) {
	for _, tc := range []struct {
		name          string
		phase, offset int
		expected      byte
		prefix        int64
	}{
		{"first_verify", 1, 0, 0, 0},
		{"inverse", 3, 7, 255, 7},
		{"address_pattern", 5, 0, 0, 0},
		{"forward_inversion", 7, 0, 170, 0},
		{"reverse_offset_zero", 8, 0, 85, 7},
		{"reverse_last_offset", 8, 7, 85, 0},
		{"final_verify", 9, 7, 170, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buffer := make([]byte, 8)
			for phase := 0; phase < tc.phase; phase++ {
				if n, m := memoryChunk(buffer, 0, phase, 0, 8); n != 8 || m != nil {
					t.Fatal("valid prefix prerequisite")
				}
			}
			if buffer[tc.offset] != tc.expected {
				t.Fatal("literal pre-fault byte prerequisite")
			}
			buffer[tc.offset] ^= 1
			actual := buffer[tc.offset]
			n, m := memoryChunk(buffer, 0, tc.phase, 0, 8)
			if n != tc.prefix || m == nil || m.Offset != int64(tc.offset) || m.ExpectedByte != int(tc.expected) || m.ActualByte != int(actual) || buffer[tc.offset] != actual {
				t.Fatalf("fault overwritten or wrong prefix: %d %+v", n, m)
			}
		})
	}
}
