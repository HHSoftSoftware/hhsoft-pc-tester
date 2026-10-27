package sessionevidence

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
)

func scalarCPUHotBinding(t *testing.T) (Journal, []ResultBinding) {
	t.Helper()
	j, rows := hotJournal(t)
	var cpu quick.CPUResult
	if json.Unmarshal(rows[0].Evidence, &cpu) != nil {
		t.Fatal("invalid historical test data")
	}
	cpu.Schema = quick.CPUSchema
	cpu.KnownAnswerSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	cpu.Scalar = quick.CPUScalarEvidence{Suite: quick.CPUScalarSuite, VectorCount: 12, Workers: []quick.CPUScalarWorker{}}
	rows[0].Version = 2
	rows[0].Evidence, _ = json.Marshal(cpu)
	local, err := quickevidence.DecodeLocal("cpu.quick", rows[0].Evidence)
	if err != nil {
		t.Fatal(err)
	}
	j.LocalThermalSources[0].EvidenceSchema = local.Schema
	j.LocalThermalSources[0].EvidenceTypedSHA256 = local.TypedSHA256
	return j, rows
}

func TestCPUSessionKeepsHistoricalHashesAndVersionPairs(t *testing.T) {
	j, rows := hotJournal(t)
	rows[0].Version = 1
	typed := j.LocalThermalSources[0].EvidenceTypedSHA256
	if ValidateBindings(j, rows) != nil {
		t.Fatal("v5 lost CPU-v2 definition-1 evidence")
	}
	j, rows = historicalPointerSession(j, rows)
	if ValidateBindings(j, rows) != nil || j.LocalThermalSources[0].EvidenceTypedSHA256 != typed {
		t.Fatal("v4 lost historical CPU hash or binding")
	}
	j, rows = scalarCPUHotBinding(t)
	if ValidateBindings(j, rows) != nil {
		t.Fatal("v5 rejected current scalar CPU evidence")
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, rows[0].Evidence, "", "  ") != nil {
		t.Fatal("cannot indent fixture")
	}
	rows[0].Evidence = pretty.Bytes()
	if ValidateBindings(j, rows) != nil {
		t.Fatal("typed scalar hash depends on JSON whitespace")
	}
}

func TestCPUSessionRejectsDowngradeAndMisboundScalarEvidence(t *testing.T) {
	for _, defect := range []string{"definition1", "definition0", "future_definition", "historical_schema", "v4", "duration", "duplicate_schema", "lost_hash", "misplaced"} {
		t.Run(defect, func(t *testing.T) {
			j, rows := scalarCPUHotBinding(t)
			switch defect {
			case "definition1":
				rows[0].Version = 1
			case "definition0":
				rows[0].Version = 0
			case "future_definition":
				rows[0].Version = 3
			case "historical_schema":
				rows[0].Evidence = []byte(hotCPU)
			case "v4":
				j, rows = historicalPointerSession(j, rows)
			case "duration":
				rows[0].Evidence = bytes.Replace(rows[0].Evidence, []byte(`"duration_ms":10`), []byte(`"duration_ms":51`), 1)
				local, err := quickevidence.DecodeLocal("cpu.quick", rows[0].Evidence)
				if err != nil {
					t.Fatal(err)
				}
				j.LocalThermalSources[0].EvidenceTypedSHA256 = local.TypedSHA256
			case "duplicate_schema":
				rows[0].Evidence = append([]byte(`{"schema":"hhsoft.pc-tester.cpu/v2",`), rows[0].Evidence[1:]...)
			case "lost_hash":
				old, err := quickevidence.DecodeLocal("cpu.quick", []byte(hotCPU))
				if err != nil {
					t.Fatal(err)
				}
				j.LocalThermalSources[0].EvidenceTypedSHA256 = old.TypedSHA256
			case "misplaced":
				rows[1].Evidence = rows[0].Evidence
			}
			if ValidateBindings(j, rows) == nil {
				t.Fatal("invalid scalar CPU binding accepted")
			}
		})
	}
}

func TestCPUExecutionNotesHaveClosedCurrentVersions(t *testing.T) {
	j := shortJournal()
	rows := shortBindings(j)
	rows[0].Version = 2
	if ValidateBindings(j, rows) != nil {
		t.Fatal("current excluded CPU note rejected")
	}
	for _, version := range []int{-1, 3} {
		copyRows := append([]ResultBinding(nil), rows...)
		copyRows[0].Version = version
		if ValidateBindings(j, copyRows) == nil {
			t.Fatal("unknown CPU note definition accepted")
		}
	}
	for _, detail := range []string{quick.CPUSchema, "HHSOFT.PC-TESTER.CPU/V3", quick.CPUSchemaV2} {
		copyRows := append([]ResultBinding(nil), rows...)
		copyRows[0].Evidence, _ = json.Marshal(executionNote{Schema: "hhsoft.pc-tester.execution-note/v1", Status: "skip", SourceStatus: "excluded", Detail: detail})
		if ValidateBindings(j, copyRows) == nil {
			t.Fatal("functional CPU evidence hidden in note")
		}
	}
	j, rows = historicalPointerSession(j, rows)
	if ValidateBindings(j, rows) == nil {
		t.Fatal("v4 accepted current CPU definition note")
	}
}
