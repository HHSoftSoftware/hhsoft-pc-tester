package sessionevidence

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
)

// HasInspectionClaim selects strict validation; it never accepts evidence.
// Inspect all tokens, including escaped strings and earlier duplicate members.
func HasInspectionClaim(raw []byte) bool { return hasInspectionClaim(raw, true) }

// HasInspectionSchemaClaim distinguishes observed evidence from an integrity
// record's test-ID references. It still inspects every decoded string token.
func HasInspectionSchemaClaim(raw []byte) bool { return hasInspectionClaim(raw, false) }

func hasInspectionClaim(raw []byte, includeID bool) bool {
	if len(raw) == 0 {
		return false
	}
	if !json.Valid(raw) {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return false
		}
		if err != nil {
			return true
		}
		if value, ok := token.(string); ok {
			family, _, _ := strings.Cut(value, "/")
			if includeID && strings.EqualFold(value, exteriorinspection.TestID) ||
				strings.EqualFold(family, "hhsoft.pc-tester.exterior-inspection") {
				return true
			}
		}
	}
}

// ValidateInspectionNote admits only the fixed supervisor dispositions.
// Hardware absence cannot stand in for an explicitly excluded operator task.
func ValidateInspectionNote(binding ResultBinding) error {
	if binding.ID != exteriorinspection.TestID || binding.Version != 1 || HasInspectionClaim(binding.Evidence) {
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

func validateInspectionBinding(worker WorkerRecord, binding ResultBinding) error {
	if binding.ID != exteriorinspection.TestID || binding.Version != 1 {
		return ErrInvalid
	}
	if !HasInspectionClaim(binding.Evidence) {
		if ValidateInspectionNote(binding) != nil {
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
	value, err := exteriorinspection.Decode(binding.Evidence)
	if err != nil || binding.PlannedStatus != "scheduled" || value.Status != binding.ReportStatus ||
		value.Status != worker.Outcome || worker.Timing == nil ||
		(worker.State != "completed" && worker.State != "interrupted") ||
		value.ObservationMilliseconds > worker.Timing.FinishedMilliseconds-worker.Timing.StartedMilliseconds {
		return ErrInvalid
	}
	return nil
}
