package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func HasCPUStressClaim(raw []byte) bool { return sessionevidence.HasCPUStressClaim(raw) }

// ValidatedCPUStress returns independently decoded evidence only after the full
// report and the standard session have established the observation's bindings.
func ValidatedCPUStress(payload InventoryPayload) (*quick.CPUStressResult, error) {
	if err := ValidateCoverage(payload); err != nil {
		return nil, err
	}
	for _, row := range payload.TestResults {
		if row.ID == "cpu.stress" && HasCPUStressClaim(row.Evidence) {
			value, _, err := quickevidence.DecodeCPUStress(row.Evidence)
			if err != nil {
				return nil, err
			}
			return &value, nil
		}
	}
	return nil, nil
}

func validateCPUStressReport(payload InventoryPayload) error {
	claimed := false
	for _, row := range payload.TestResults {
		observedStatus := strings.EqualFold(row.ID, "cpu.stress") && (row.Status == TestPass || row.Status == TestFail || row.Status == TestInconclusive && row.PlannedStatus == PlannedScheduled ||
			(row.Status == TestError || row.Status == TestAborted) && !legacyCPUStressFailureNote(row))
		if !HasCPUStressClaim(row.Evidence) && !observedStatus {
			continue
		}
		claimed = true
		if row.ID == "run.integrity.quick" {
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if err != nil || row.Version != 1 || value.Session == nil || value.Session.Schema != sessionevidence.SchemaV8 {
				return errors.New("invalid standard-session CPU stress reference")
			}
			continue
		}
		if row.ID != "cpu.stress" || row.Version != 1 || row.PlannedStatus != PlannedScheduled {
			return errors.New("CPU stress observations require scheduled stress definition 1")
		}
		value, _, err := quickevidence.DecodeCPUStress(row.Evidence)
		if err != nil || string(value.Status) != string(row.Status) {
			return errors.New("invalid CPU stress observation")
		}
	}
	if !claimed {
		return nil
	}
	if payload.CoverageScope != CoverageFrozenPlan {
		return errors.New("CPU stress observations require frozen-plan coverage")
	}
	var journal *sessionevidence.Journal
	bindings := make([]sessionevidence.ResultBinding, 0, len(payload.TestResults))
	for _, row := range payload.TestResults {
		if row.ID == "run.integrity.quick" {
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if journal != nil || err != nil || row.Version != 1 || value.Session == nil || value.Session.Schema != sessionevidence.SchemaV8 ||
				value.PlanID != row.PlanID || value.PlannedHardwareGraph != row.HardwareGraph || value.Status != string(row.Status) {
				return errors.New("CPU stress observation has no valid standard-session binding")
			}
			journal = value.Session
			continue
		}
		bindings = append(bindings, sessionevidence.ResultBinding{ID: row.ID, Version: row.Version, PlannedStatus: string(row.PlannedStatus), ReportStatus: string(row.Status), Evidence: row.Evidence})
	}
	if journal == nil {
		return errors.New("CPU stress observation has no standard session")
	}
	return sessionevidence.ValidateBindings(*journal, bindings)
}

// Historical supervisor notes describe absent bounded evidence, not an observed
// stress run. A new standard-session journal still requires every dispatched
// stress row to bind strict evidence independently of this legacy exemption.
func legacyCPUStressFailureNote(row TestResult) bool {
	if row.ID != "cpu.stress" || row.Version != 1 || row.Status != TestError && row.Status != TestAborted {
		return false
	}
	var note struct {
		Schema       string `json:"schema"`
		Status       string `json:"status"`
		SourceStatus string `json:"source_status"`
		Detail       string `json:"detail"`
	}
	return evidencejson.ValidateShape(row.Evidence, reflect.TypeOf(note)) == nil && json.Unmarshal(row.Evidence, &note) == nil &&
		note.Schema == "hhsoft.pc-tester.execution-note/v1" && note.Status == string(row.Status) && note.SourceStatus == note.Status &&
		note.Detail == "bounded evidence was not produced; outcome recorded by the run supervisor"
}

// HasCPUStressReference routes an inventory through full admission even when an
// observed row has lost its schema and coverage fields. It does not turn an
// historical unexecuted note into an observation.
func HasCPUStressReference(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return false
		}
		if err != nil {
			return true
		}
		if value, ok := token.(string); ok && strings.EqualFold(value, "cpu.stress") {
			return true
		}
	}
}
