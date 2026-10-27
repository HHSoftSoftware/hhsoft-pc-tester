package sessionevidence

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

func hasNewMemoryClaim(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var visit func(int) (bool, error)
	visit = func(depth int) (bool, error) {
		if depth > 64 {
			return true, ErrInvalid
		}
		token, err := decoder.Token()
		if err != nil {
			return true, err
		}
		switch token {
		case json.Delim('{'):
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok {
					return true, ErrInvalid
				}
				if strings.EqualFold(name, "memory_coverage") {
					return true, nil
				}
				if found, err := visit(depth + 1); found || err != nil {
					return found, err
				}
			}
			_, err = decoder.Token()
			return false, err
		case json.Delim('['):
			for decoder.More() {
				if found, err := visit(depth + 1); found || err != nil {
					return found, err
				}
			}
			_, err = decoder.Token()
			return false, err
		default:
			if value, ok := token.(string); ok {
				prefix, _, versioned := strings.Cut(value, "/")
				return strings.EqualFold(value, quick.MemorySuite) || versioned && strings.EqualFold(prefix, "hhsoft.pc-tester.memory") && value != quick.MemorySchemaV2, nil
			}
			return false, nil
		}
	}
	if found, err := visit(0); found || err != nil {
		return true
	}
	_, err := decoder.Token()
	return err != io.EOF
}

// ValidateMemoryExecutionNote keeps no-work notes separate from observations.
func ValidateMemoryExecutionNote(binding ResultBinding) error {
	if binding.ID != "memory.quick" || binding.Version != 2 || hasNewMemoryClaim(binding.Evidence) {
		return ErrInvalid
	}
	var note executionNote
	if evidencejson.ValidateShape(binding.Evidence, reflect.TypeOf(note)) != nil || json.Unmarshal(binding.Evidence, &note) != nil || note.Schema != "hhsoft.pc-tester.execution-note/v1" || note.Status != binding.ReportStatus {
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
	decoder := json.NewDecoder(bytes.NewReader(binding.Evidence))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ErrInvalid
		}
		if value, ok := token.(string); ok && (strings.EqualFold(value, quick.MemorySchemaV2) || strings.EqualFold(value, "hhsoft.pc-tester.run-thermal/v1")) {
			return ErrInvalid
		}
	}
	return nil
}
