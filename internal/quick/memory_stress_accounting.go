package quick

import "math"

func memoryStressAdd(a, b uint64) (uint64, bool) {
	if b > math.MaxUint64-a {
		return 0, false
	}
	return a + b, true
}

func memoryStressMultiply(a, b uint64) (uint64, bool) {
	if a != 0 && b > math.MaxUint64/a {
		return 0, false
	}
	return a * b, true
}

func memoryStressTotals(size, phases, position uint64) (uint64, uint64, bool) {
	readsBefore := [...]uint64{0, 0, 1, 1, 2, 2, 3, 3, 4, 5}
	writesBefore := [...]uint64{0, 1, 1, 2, 2, 3, 3, 4, 5, 6}
	cycles, phase := phases/10, phases%10
	full, ok := memoryStressMultiply(cycles, 6)
	if !ok {
		return 0, 0, false
	}
	reads, rok := memoryStressAdd(full, readsBefore[phase])
	writes, wok := memoryStressAdd(full, writesBefore[phase])
	if !rok || !wok {
		return 0, 0, false
	}
	reads, rok = memoryStressMultiply(reads, size)
	writes, wok = memoryStressMultiply(writes, size)
	if !rok || !wok {
		return 0, 0, false
	}
	if phase%2 == 1 || phase == 8 {
		reads, rok = memoryStressAdd(reads, position)
	}
	if phase%2 == 0 || phase == 7 {
		writes, wok = memoryStressAdd(writes, position)
	}
	return reads, writes, rok && wok
}

// ValidMemoryStressCoverage is shared by the owner and strict decoder. It
// validates logical successful accesses, not physical memory traffic.
func ValidMemoryStressCoverage(v MemoryStressResult) bool {
	c := v.Coverage
	if v.AllocatedBytes == 0 {
		return c == (MemoryStressCoverage{})
	}
	if v.AllocatedBytes < MemoryStressMinimumBytes || v.AllocatedBytes > MemoryStressMaximumBytes ||
		c.CompletedCycles != c.CompletedPhases/10 || c.LoadCycles > c.CompletedCycles ||
		c.CompletedCycles-c.LoadCycles > 1 ||
		c.PhaseBytes >= v.AllocatedBytes || c.TerminalReadBytes > v.AllocatedBytes ||
		c.UncreditedBytes > min(uint64(memoryChunkBytes), v.AllocatedBytes-c.PhaseBytes) ||
		c.TerminalUncreditedBytes > min(uint64(memoryChunkBytes), v.AllocatedBytes-c.TerminalReadBytes) {
		return false
	}
	reads, writes, ok := memoryStressTotals(v.AllocatedBytes, c.CompletedPhases, c.PhaseBytes)
	if !ok || reads != c.ReadBytes || writes != c.WrittenBytes {
		return false
	}
	if c.TerminalReadBytes != 0 || c.TerminalUncreditedBytes != 0 {
		return c.CompletedPhases%10 == 0 && c.PhaseBytes == 0 && c.UncreditedBytes == 0
	}
	return true
}

func creditMemoryStress(v *MemoryStressResult, count uint64, terminal bool) bool {
	if !ValidMemoryStressCoverage(*v) || v.AllocatedBytes == 0 {
		return false
	}
	c := &v.Coverage
	if terminal {
		if c.CompletedPhases%10 != 0 || c.PhaseBytes != 0 || c.UncreditedBytes != 0 ||
			count > min(uint64(memoryChunkBytes), v.AllocatedBytes-c.TerminalReadBytes) {
			return false
		}
		c.TerminalReadBytes += count
		c.TerminalUncreditedBytes = 0
		return true
	}
	if c.TerminalReadBytes != 0 || c.TerminalUncreditedBytes != 0 ||
		count > min(uint64(memoryChunkBytes), v.AllocatedBytes-c.PhaseBytes) {
		return false
	}
	phase := c.CompletedPhases % 10
	next := *c
	var ok bool
	if phase%2 == 1 || phase == 8 {
		next.ReadBytes, ok = memoryStressAdd(next.ReadBytes, count)
		if !ok {
			return false
		}
	}
	if phase%2 == 0 || phase == 7 {
		next.WrittenBytes, ok = memoryStressAdd(next.WrittenBytes, count)
		if !ok {
			return false
		}
	}
	next.PhaseBytes += count
	next.UncreditedBytes = 0
	if next.PhaseBytes == v.AllocatedBytes {
		next.CompletedPhases, ok = memoryStressAdd(next.CompletedPhases, 1)
		if !ok {
			return false
		}
		next.CompletedCycles = next.CompletedPhases / 10
		next.PhaseBytes = 0
	}
	*c = next
	return true
}
