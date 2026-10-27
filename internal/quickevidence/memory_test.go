package quickevidence

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

func memoryFixture() quick.MemoryResult {
	return quick.MemoryResult{Schema: quick.MemorySchema, Status: quick.StatusPass, AllocatedBytes: 4 << 20, ExaminedBytes: 24 << 20,
		Passes: 1, PatternsPerPass: 3, DurationMilliseconds: 20,
		Coverage: quick.MemoryCoverage{Suite: quick.MemorySuite, CompletedPhases: 10, CompletedPasses: 1, WrittenBytes: 24 << 20},
		Safety: quick.SafetyEvidence{GlobalTimeoutMilliseconds: 30000, ThermalLimitMilliCelsius: 90000,
			MonitorStatus: "completed", CleanupConfirmed: true, ThermalObservations: 1, ThermalCoverage: "unavailable"}}
}

func decodeMemoryFixture(t *testing.T, value quick.MemoryResult) (LocalEvidence, error) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return DecodeLocal("memory.quick", raw)
}

func TestMemoryHistoricalCanonicalDigestsRemainExact(t *testing.T) {
	var rows []struct {
		Name, Canonical, Source string
		Typed                   string `json:"typed_sha256"`
	}
	raw, err := os.ReadFile("testdata/memory-v2-baseline.json")
	if err != nil || json.Unmarshal(raw, &rows) != nil || len(rows) != 3 {
		t.Fatal("historical fixture unavailable")
	}
	for _, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			for _, data := range []string{row.Source, row.Canonical} {
				v, err := DecodeLocal("memory.quick", []byte(data))
				if err != nil || v.TypedSHA256 != row.Typed || v.Memory.Present || v.Schema != quick.MemorySchemaV2 {
					t.Fatal("historical typed bytes or interpretation changed")
				}
			}
		})
	}
}

func TestMemoryCoveragePrefixAndObservedTotals(t *testing.T) {
	for completed := 0; completed <= 20; completed++ {
		v := memoryFixture()
		v.Status, v.Passes = quick.StatusAborted, 2
		v.Coverage.CompletedPhases, v.Coverage.CompletedPasses = completed, completed/10
		v.ExaminedBytes, v.Coverage.WrittenBytes = 0, 0
		// Event lists count the oracle independently of decoder prefix tables.
		for event := 0; event < completed; event++ {
			for _, read := range []int{1, 3, 5, 7, 8, 9} {
				if event%10 == read {
					v.ExaminedBytes += v.AllocatedBytes
				}
			}
			for _, write := range []int{0, 2, 4, 6, 7, 8} {
				if event%10 == write {
					v.Coverage.WrittenBytes += v.AllocatedBytes
				}
			}
		}
		value, err := decodeMemoryFixture(t, v)
		if err != nil || !value.Memory.Present || value.Memory.ObservedMatchedBytes != v.ExaminedBytes || value.Memory.ObservedWrittenBytes != v.Coverage.WrittenBytes || value.Memory.CompletedPhases != completed {
			t.Fatalf("prefix %d rejected", completed)
		}
	}
	v := memoryFixture()
	v.Status = quick.StatusFail
	v.Coverage.CompletedPhases, v.Coverage.CompletedPasses = 8, 0
	v.Coverage.PhaseBytes, v.Coverage.UncreditedBytes = 256<<10, 7
	v.ExaminedBytes, v.Coverage.WrittenBytes = 4*v.AllocatedBytes+(256<<10), 5*v.AllocatedBytes+(256<<10)
	v.Mismatch = &quick.MemoryMismatch{Offset: v.AllocatedBytes - 1 - (256 << 10) - 7, ExpectedByte: 0x55, ActualByte: 0}
	value, err := decodeMemoryFixture(t, v)
	if err != nil || value.Memory.AttemptedReadBytes != v.ExaminedBytes+8 || value.Memory.ObservedWrittenBytes != v.Coverage.WrittenBytes+7 || value.Memory.Direction != "descending" {
		t.Fatal("uncredited reverse mismatch differs")
	}
	value.Memory.Mismatch.ActualByte = 123
	again, err := decodeMemoryFixture(t, v)
	if err != nil || again.Memory.Mismatch.ActualByte != 0 {
		t.Fatal("summary aliases evidence")
	}
}

func TestMemoryCoverageRejectsIndependentContradictions(t *testing.T) {
	for name, mutate := range map[string]func(*quick.MemoryResult){
		"old work only": func(v *quick.MemoryResult) { v.ExaminedBytes = 12 << 20 },
		"phase count":   func(v *quick.MemoryResult) { v.Coverage.CompletedPhases = 11 },
		"pass count":    func(v *quick.MemoryResult) { v.Coverage.CompletedPasses = 0 },
		"patterns":      func(v *quick.MemoryResult) { v.PatternsPerPass = 6 },
		"suite":         func(v *quick.MemoryResult) { v.Coverage.Suite = "other" },
		"overflow":      func(v *quick.MemoryResult) { v.Coverage.WrittenBytes = math.MaxInt64 },
		"negative":      func(v *quick.MemoryResult) { v.Coverage.UncreditedBytes = -1 },
		"late pass":     func(v *quick.MemoryResult) { v.Coverage.UncreditedBytes = 1 },
		"unallocated":   func(v *quick.MemoryResult) { v.AllocatedBytes = 0 },
		"empty fail":    func(v *quick.MemoryResult) { v.Status = quick.StatusFail },
		"fault after full pass": func(v *quick.MemoryResult) {
			v.Status = quick.StatusFail
			v.Mismatch = &quick.MemoryMismatch{ActualByte: 1}
		},
		"no initial sample": func(v *quick.MemoryResult) { v.Safety.ThermalObservations = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			v := memoryFixture()
			mutate(&v)
			if _, err := decodeMemoryFixture(t, v); err == nil {
				t.Fatal("impossible evidence accepted")
			}
		})
	}
	v := memoryFixture()
	v.Status, v.Coverage.CompletedPhases, v.Coverage.CompletedPasses = quick.StatusAborted, 7, 0
	v.ExaminedBytes, v.Coverage.WrittenBytes = 3*v.AllocatedBytes, 4*v.AllocatedBytes
	for _, units := range []int64{1, (256 << 10) - 1, (256 << 10) + 1} {
		v.Coverage.UncreditedBytes = units
		if _, err := decodeMemoryFixture(t, v); err == nil {
			t.Fatal("partial correct terminal chunk accepted")
		}
	}
	v.Coverage.UncreditedBytes = 256 << 10
	if _, err := decodeMemoryFixture(t, v); err != nil {
		t.Fatal("full uncredited chunk rejected")
	}
	v.Status, v.Coverage.UncreditedBytes = quick.StatusFail, 0
	v.Mismatch = &quick.MemoryMismatch{ExpectedByte: 0xaa, ActualByte: 0}
	if _, err := decodeMemoryFixture(t, v); err != nil {
		t.Fatal("offset-zero mismatch rejected")
	}
	for _, offset := range []int64{-1, 1, v.AllocatedBytes} {
		v.Mismatch.Offset = offset
		if _, err := decodeMemoryFixture(t, v); err == nil {
			t.Fatal("wrong mismatch cursor accepted")
		}
	}
}

func TestMemoryV3ClosedShapeAndNoDowngrade(t *testing.T) {
	raw, _ := json.Marshal(memoryFixture())
	for _, altered := range []string{
		strings.Replace(string(raw), `"memory_coverage":`, `"Memory_Coverage":`, 1),
		strings.Replace(string(raw), `"completed_phases":10`, `"completed_phases":10,"completed_phases":10`, 1),
		strings.Replace(string(raw), `"completed_phases":10`, `"completed_phases":10.0`, 1),
		strings.Replace(string(raw), `"written_bytes":25165824`, `"written_bytes":true`, 1),
		strings.Replace(string(raw), `"schema":`, `"mismatch":null,"schema":`, 1),
		strings.Replace(string(raw), "memory/v3", "memory/v2", 1),
	} {
		if _, err := DecodeLocal("memory.quick", []byte(altered)); err == nil {
			t.Fatal("noncanonical shape accepted")
		}
	}
}
