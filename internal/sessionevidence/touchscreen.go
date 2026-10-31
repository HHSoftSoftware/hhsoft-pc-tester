package sessionevidence

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchscreenevidence"
)

const touchscreenTestID = "input.touchscreen.quick"
const touchscreenNoteUnscheduled = "not run; disposition recorded in the frozen plan"
const touchscreenNoteUnobserved = "bounded evidence was not produced; outcome recorded by the run supervisor"

// HasTouchscreenClaim selects strict validation; it never accepts an observation.
// Inspect every decoded token, including earlier duplicate and escaped values.
// Definition ID/version discrimination belongs to the enclosing ResultBinding.
func HasTouchscreenClaim(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	if len(raw) > 1<<20 || !utf8.Valid(raw) || !json.Valid(raw) {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return false
		}
		if err != nil {
			return true
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				depth++
				if depth > 64 {
					return true
				}
			case '}', ']':
				depth--
			}
		}
		if value, ok := token.(string); ok {
			family, _, _ := strings.Cut(value, "/")
			if strings.EqualFold(family, "hhsoft.pc-tester.touchscreen-functional") {
				return true
			}
		}
	}
}

// ValidateTouchscreenNote admits finite unobserved standalone dispositions.
// This establishes no device observation, original file absence or monitoring.
// The file-owning finalizer must separately prove direct absence when required.
func ValidateTouchscreenNote(binding ResultBinding) error {
	if binding.ID != touchscreenTestID || binding.Version != 2 ||
		len(binding.Evidence) == 0 || len(binding.Evidence) > 1<<20 ||
		HasTouchscreenClaim(binding.Evidence) {
		return ErrInvalid
	}
	var note executionNote
	if evidencejson.ValidateShape(binding.Evidence, reflect.TypeOf(note)) != nil ||
		json.Unmarshal(binding.Evidence, &note) != nil ||
		note.Schema != "hhsoft.pc-tester.execution-note/v1" || note.Status != binding.ReportStatus {
		return ErrInvalid
	}
	switch binding.PlannedStatus {
	case "excluded", "not_present", "unsupported", "missing_driver":
		if note.SourceStatus != binding.PlannedStatus ||
			note.Status != reportStatus(binding.PlannedStatus) || note.Detail != touchscreenNoteUnscheduled {
			return ErrInvalid
		}
	case "scheduled":
		if note.Status != "error" && note.Status != "aborted" ||
			note.SourceStatus != note.Status || note.Detail != touchscreenNoteUnobserved {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// The shared binder checks declared result/worker consistency only. Frozen
// plan/inventory ownership and actual command exit belong to their owner layer.
func validateTouchscreenBinding(worker WorkerRecord, binding ResultBinding) error {
	if worker.ID != touchscreenTestID || binding.ID != touchscreenTestID || binding.Version != 2 ||
		binding.ReportStatus != reportStatus(worker.Outcome) {
		return ErrInvalid
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(binding.Evidence, &header) != nil {
		return ErrInvalid
	}
	if header.Schema == "hhsoft.pc-tester.execution-note/v1" {
		if ValidateTouchscreenNote(binding) != nil {
			return ErrInvalid
		}
		var note executionNote
		if json.Unmarshal(binding.Evidence, &note) != nil || note.SourceStatus != worker.Outcome {
			return ErrInvalid
		}
		if worker.State == "not_scheduled" && binding.PlannedStatus != "scheduled" ||
			worker.State == "not_started" && worker.Outcome == "aborted" ||
			(worker.State == "completed" || worker.State == "interrupted") &&
				(worker.Outcome == "error" || worker.Outcome == "aborted") {
			return nil
		}
		return ErrInvalid
	}
	value, err := touchscreenevidence.Decode(binding.Evidence)
	if err != nil {
		return err
	}
	if binding.PlannedStatus != "scheduled" || string(value.Status) != binding.ReportStatus ||
		string(value.Status) != worker.Outcome ||
		(worker.State != "completed" && worker.State != "interrupted") || worker.Timing == nil ||
		worker.Timing.StartedMilliseconds < 0 ||
		worker.Timing.FinishedMilliseconds < worker.Timing.StartedMilliseconds ||
		value.DurationMilliseconds > worker.Timing.FinishedMilliseconds-worker.Timing.StartedMilliseconds {
		return ErrInvalid
	}
	return nil
}
