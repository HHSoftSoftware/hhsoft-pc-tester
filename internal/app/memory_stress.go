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

func HasMemoryStressClaim(raw []byte) bool { return sessionevidence.HasMemoryStressClaim(raw) }

// ValidatedMemoryStress returns independently decoded evidence only after the full
// report and the Extended session have established the observation's bindings.
func ValidatedMemoryStress(payload InventoryPayload) (*quick.MemoryStressResult, error) {
	if err := ValidateCoverage(payload); err != nil {
		return nil, err
	}
	for _, row := range payload.TestResults {
		if row.ID == "memory.stress" && HasMemoryStressClaim(row.Evidence) {
			value, _, err := quickevidence.DecodeMemoryStress(row.Evidence)
			if err != nil {
				return nil, err
			}
			return &value, nil
		}
	}
	return nil, nil
}

func validateMemoryStressReport(payload InventoryPayload) error {
	claimed := false
	for _, row := range payload.TestResults {
		observedStatus := strings.EqualFold(row.ID, "memory.stress") && (row.Status == TestPass || row.Status == TestFail || row.Status == TestInconclusive && row.PlannedStatus == PlannedScheduled ||
			(row.Status == TestError || row.Status == TestAborted) && !memoryStressFailureNote(row))
		if !HasMemoryStressClaim(row.Evidence) && !observedStatus {
			continue
		}
		claimed = true
		if row.ID == "run.integrity.quick" {
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if err != nil || row.Version != 1 || value.Session == nil || !sessionevidence.IsExtendedSchema(value.Session.Schema) {
				return errors.New("invalid Extended-session memory stress reference")
			}
			continue
		}
		if row.ID != "memory.stress" || row.Version != 2 || row.PlannedStatus != PlannedScheduled {
			return errors.New("memory stress observations require scheduled memory-stress definition 2")
		}
		value, _, err := quickevidence.DecodeMemoryStress(row.Evidence)
		if err != nil || string(value.Status) != string(row.Status) {
			return errors.New("invalid memory stress observation")
		}
	}
	if !claimed {
		return nil
	}
	if payload.CoverageScope != CoverageFrozenPlan {
		return errors.New("memory stress observations require frozen-plan coverage")
	}
	var journal *sessionevidence.Journal
	bindings := make([]sessionevidence.ResultBinding, 0, len(payload.TestResults))
	for _, row := range payload.TestResults {
		if row.ID == "run.integrity.quick" {
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if journal != nil || err != nil || row.Version != 1 || value.Session == nil || !sessionevidence.IsExtendedSchema(value.Session.Schema) ||
				value.PlanID != row.PlanID || value.PlannedHardwareGraph != row.HardwareGraph || value.Status != string(row.Status) {
				return errors.New("memory stress observation has no valid Extended-session binding")
			}
			journal = value.Session
			continue
		}
		bindings = append(bindings, sessionevidence.ResultBinding{ID: row.ID, Version: row.Version, PlannedStatus: string(row.PlannedStatus), ReportStatus: string(row.Status), Evidence: row.Evidence})
	}
	if journal == nil {
		return errors.New("memory stress observation has no Extended session")
	}
	return sessionevidence.ValidateBindings(*journal, bindings)
}

// Historical supervisor notes describe absent bounded evidence, not an observed
// stress run. A new Extended-session journal still requires every dispatched
// stress row to bind strict evidence independently of this legacy exemption.
func memoryStressFailureNote(row TestResult) bool {
	if row.ID != "memory.stress" || row.Version != 1 && row.Version != 2 || row.Status != TestError && row.Status != TestAborted {
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

// HasMemoryStressReference routes an inventory through full admission even when an
// observed row has lost its schema and coverage fields. It does not turn an
// historical unexecuted note into an observation.
func HasMemoryStressReference(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return false
		}
		if err != nil {
			return true
		}
		if value, ok := token.(string); ok && strings.EqualFold(value, "memory.stress") {
			return true
		}
	}
}

// admittedMemoryStressObservation is only a family disambiguator. Full report
// admission still requires the Extended journal, frozen-plan bindings and
// original source authority through validateMemoryStressReport.
func admittedMemoryStressObservation(row TestResult) bool {
	if row.ID != "memory.stress" || row.Version != 2 || row.PlannedStatus != PlannedScheduled || !HasMemoryStressClaim(row.Evidence) {
		return false
	}
	value, _, err := quickevidence.DecodeMemoryStress(row.Evidence)
	return err == nil && string(value.Status) == string(row.Status)
}
