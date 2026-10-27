package sessionevidence

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
)

func memoryHotBinding(t *testing.T, current bool) (Journal, []ResultBinding) {
	t.Helper()
	raw, err := os.ReadFile("../quickevidence/testdata/memory-v2-baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source string
		TypedSHA256  string `json:"typed_sha256"`
		SourceSHA256 string `json:"source_sha256"`
	}
	if json.Unmarshal(raw, &cases) != nil {
		t.Fatal("invalid baseline")
	}
	old := cases[2]
	if old.Name != "aborted" {
		t.Fatal("wrong baseline case")
	}
	evidence := []byte(old.Source)
	version := 1
	if current {
		var value quick.MemoryResult
		if json.Unmarshal(evidence, &value) != nil {
			t.Fatal("invalid fixture")
		}
		value.Schema, value.Coverage = quick.MemorySchema, quick.MemoryCoverage{Suite: quick.MemorySuite}
		evidence, _ = json.Marshal(value)
		version = 2
	}
	local, err := quickevidence.DecodeLocal("memory.quick", evidence)
	if err != nil {
		t.Fatal(err)
	}
	if !current && local.TypedSHA256 != old.TypedSHA256 {
		t.Fatal("historical canonical hash changed")
	}
	j := shortJournal()
	j.StopReason = "thermal_limit"
	j.Workers[1] = WorkerRecord{ID: "memory.quick", State: "completed", Outcome: "aborted", Timing: &WorkerTiming{10, 60}}
	j.Safety = append(j.Safety[:1:1], append([]SafetyRecord{{Phase: "before-memory.quick", Result: j.Safety[0].Result}}, j.Safety[1:]...)...)
	j.LocalThermalSources = []LocalThermalSource{{WorkerID: "memory.quick", EvidenceSchema: local.Schema,
		EvidenceFileSHA256: "sha256:" + old.SourceSHA256, EvidenceTypedSHA256: local.TypedSHA256,
		PeakMilliCelsius: 95000, LimitMilliCelsius: 90000, StartedMilliseconds: 10, FinishedMilliseconds: 60}}
	rows := shortBindings(j)
	rows[1] = ResultBinding{ID: "memory.quick", Version: version, PlannedStatus: "scheduled", ReportStatus: "aborted", Evidence: evidence}
	return j, rows
}

func TestMemorySessionPreservesHistoricalHashesAndExactVersions(t *testing.T) {
	j, rows := memoryHotBinding(t, false)
	before, _ := json.Marshal(j.LocalThermalSources)
	if err := ValidateBindings(j, rows); err != nil {
		t.Fatal("v5 lost v2", err)
	}
	j, rows = historicalPointerSession(j, rows)
	if err := ValidateBindings(j, rows); err != nil {
		t.Fatal("v4 lost v2", err)
	}
	after, _ := json.Marshal(j.LocalThermalSources)
	if !bytes.Equal(before, after) {
		t.Fatal("historical source or typed hash changed")
	}
	j, rows = memoryHotBinding(t, true)
	if err := ValidateBindings(j, rows); err != nil {
		t.Fatal("v5 rejected v3", err)
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, rows[1].Evidence, "", "  ") != nil {
		t.Fatal("invalid fixture")
	}
	rows[1].Evidence = pretty.Bytes()
	if ValidateBindings(j, rows) != nil {
		t.Fatal("typed hash depends on whitespace")
	}
}

func TestMemorySessionRejectsImpossibleVersionTimeAndSourceBindings(t *testing.T) {
	for _, defect := range []string{"definition1", "definition0", "future", "historical_schema", "v4", "duration", "duplicate", "lost_hash", "misplaced"} {
		t.Run(defect, func(t *testing.T) {
			j, rows := memoryHotBinding(t, true)
			switch defect {
			case "definition1":
				rows[1].Version = 1
			case "definition0":
				rows[1].Version = 0
			case "future":
				rows[1].Version = 3
			case "historical_schema":
				_, old := memoryHotBinding(t, false)
				rows[1].Evidence = old[1].Evidence
			case "v4":
				j, rows = historicalPointerSession(j, rows)
			case "duration":
				rows[1].Evidence = bytes.Replace(rows[1].Evidence, []byte(`"duration_ms":20`), []byte(`"duration_ms":51`), 1)
				local, err := quickevidence.DecodeLocal("memory.quick", rows[1].Evidence)
				if err != nil {
					t.Fatal(err)
				}
				j.LocalThermalSources[0].EvidenceTypedSHA256 = local.TypedSHA256
			case "duplicate":
				rows[1].Evidence = append([]byte(`{"schema":"hhsoft.pc-tester.memory/v2",`), rows[1].Evidence[1:]...)
			case "lost_hash":
				old, _ := memoryHotBinding(t, false)
				j.LocalThermalSources[0].EvidenceTypedSHA256 = old.LocalThermalSources[0].EvidenceTypedSHA256
			case "misplaced":
				rows[0].Evidence = rows[1].Evidence
			}
			if ValidateBindings(j, rows) == nil {
				t.Fatal("invalid memory binding accepted")
			}
		})
	}
}

func TestMemoryExecutionNotesStayClosedAndSchemaSpecific(t *testing.T) {
	j := shortJournal()
	rows := shortBindings(j)
	rows[1].Version = 2
	if ValidateBindings(j, rows) != nil {
		t.Fatal("current excluded memory note rejected")
	}
	for _, version := range []int{-1, 3} {
		copyRows := append([]ResultBinding(nil), rows...)
		copyRows[1].Version = version
		if ValidateBindings(j, copyRows) == nil {
			t.Fatal("unknown version accepted")
		}
	}
	for _, detail := range []string{quick.MemorySchema, "HHSOFT.PC-TESTER.MEMORY/V3", quick.MemorySchemaV2} {
		copyRows := append([]ResultBinding(nil), rows...)
		copyRows[1].Evidence, _ = json.Marshal(executionNote{Schema: "hhsoft.pc-tester.execution-note/v1", Status: "skip", SourceStatus: "excluded", Detail: detail})
		if ValidateBindings(j, copyRows) == nil {
			t.Fatal("hidden memory claim accepted")
		}
	}
	j, rows = historicalPointerSession(j, rows)
	if ValidateBindings(j, rows) == nil {
		t.Fatal("v4 accepted current memory note")
	}
}
