package sessionevidence

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/pointerevidence"
)

// Inspect every token before decoding can discard duplicate or aliased claims.
// Unknown pointer schema versions also cannot hide in execution-note details.
func hasPointerSchema(raw []byte) bool {
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
			name, _, versioned := strings.Cut(value, "/")
			if versioned && strings.EqualFold(name, "hhsoft.pc-tester.pointer-functional") {
				return true
			}
		}
	}
}

// ValidatePointerNote preserves nonfunctional standalone outcomes. Scheduled
// error/aborted notes claim no observations or session monitoring. A journal,
// when present, must separately agree with the worker state and outcome.
func ValidatePointerNote(binding ResultBinding) error {
	if binding.ID != "input.pointer.quick" || binding.Version != 1 && binding.Version != 2 || hasPointerSchema(binding.Evidence) {
		return ErrInvalid
	}
	var note executionNote
	if evidencejson.ValidateShape(binding.Evidence, reflect.TypeOf(note)) != nil || json.Unmarshal(binding.Evidence, &note) != nil ||
		note.Schema != "hhsoft.pc-tester.execution-note/v1" || note.Status != binding.ReportStatus {
		return ErrInvalid
	}
	switch binding.PlannedStatus {
	case "excluded", "not_present", "unsupported", "missing_driver":
		if note.SourceStatus != binding.PlannedStatus || note.Status != reportStatus(binding.PlannedStatus) {
			return ErrInvalid
		}
	case "scheduled":
		if note.Status != "error" && note.Status != "aborted" || note.SourceStatus != note.Status {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validatePointerBinding(worker WorkerRecord, binding ResultBinding) error {
	if binding.Version != 1 && binding.Version != 2 {
		return ErrInvalid
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(binding.Evidence, &header) != nil {
		return ErrInvalid
	}
	if header.Schema == "hhsoft.pc-tester.execution-note/v1" {
		if ValidatePointerNote(binding) != nil || worker.State != "not_started" && worker.State != "not_scheduled" && worker.Outcome != "error" && worker.Outcome != "aborted" {
			return ErrInvalid
		}
		return nil
	}
	value, err := pointerevidence.Decode(binding.Evidence)
	if err != nil || worker.Timing == nil || string(value.Status) != worker.Outcome || binding.PlannedStatus != "scheduled" {
		return ErrInvalid
	}
	if binding.Version == 1 {
		if value.Schema != inputtest.PointerSchemaV1 {
			return ErrInvalid
		}
		return nil
	}
	if value.Schema != inputtest.PointerSchema || value.DurationMilliseconds > worker.Timing.FinishedMilliseconds-worker.Timing.StartedMilliseconds {
		return ErrInvalid
	}
	return nil
}
