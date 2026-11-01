package sessionevidence

import (
	"encoding/json"
	"reflect"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
)

// ValidateInspectionConditionNote admits only the fixed supervisor dispositions.
// Hardware absence cannot stand in for an explicitly excluded operator task.
func ValidateInspectionConditionNote(binding ResultBinding) error {
	if binding.ID != exteriorinspection.TestID || binding.Version != 2 || HasInspectionClaim(binding.Evidence) {
		return ErrInvalid
	}
	var note executionNote
	if evidencejson.ValidateShape(binding.Evidence, reflect.TypeOf(note)) != nil ||
		json.Unmarshal(binding.Evidence, &note) != nil ||
		note.Schema != "hhsoft.pc-tester.execution-note/v1" || note.Status != binding.ReportStatus {
		return ErrInvalid
	}
	switch binding.PlannedStatus {
	case "excluded":
		if note.Status != "skip" || note.SourceStatus != "excluded" ||
			note.Detail != "not run; disposition recorded in the frozen plan" {
			return ErrInvalid
		}
	case "scheduled":
		if note.Status != "error" && note.Status != "aborted" || note.SourceStatus != note.Status ||
			note.Detail != "bounded evidence was not produced; outcome recorded by the run supervisor" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validateInspectionConditionBinding(worker WorkerRecord, binding ResultBinding) error {
	if binding.ID != exteriorinspection.TestID || binding.Version != 2 {
		return ErrInvalid
	}
	if !HasInspectionClaim(binding.Evidence) {
		if ValidateInspectionConditionNote(binding) != nil {
			return ErrInvalid
		}
		if worker.State == "not_scheduled" && worker.Outcome == "excluded" ||
			worker.State == "not_started" && worker.Outcome == "aborted" ||
			(worker.State == "completed" || worker.State == "interrupted") &&
				(worker.Outcome == "error" || worker.Outcome == "aborted") {
			return nil
		}
		return ErrInvalid
	}
	value, err := exteriorinspection.DecodeV2(binding.Evidence)
	if err != nil || binding.PlannedStatus != "scheduled" || value.Status != binding.ReportStatus ||
		value.Status != worker.Outcome || worker.Timing == nil ||
		(worker.State != "completed" && worker.State != "interrupted") ||
		value.ObservationMilliseconds > worker.Timing.FinishedMilliseconds-worker.Timing.StartedMilliseconds {
		return ErrInvalid
	}
	return nil
}

// SupportsInspectionV2 grants four-criterion authority only to these journals.
func SupportsInspectionV2(schema string) bool {
	return schema == SchemaV14 || schema == SchemaV15 || schema == SchemaV16
}

func WorkerIDsV14() [14]string { return WorkerIDsV11() }
func WorkerIDsV15() [15]string { return WorkerIDsV12() }
func WorkerIDsV16() [15]string { return WorkerIDsV13() }
