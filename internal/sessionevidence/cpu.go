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

func hasNewCPUClaim(raw []byte) bool {
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
				if err != nil {
					return true, err
				}
				name, ok := key.(string)
				if !ok {
					return true, ErrInvalid
				}
				if strings.EqualFold(name, "scalar") || strings.EqualFold(name, "calculation_failures") {
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
				return versioned && strings.EqualFold(prefix, "hhsoft.pc-tester.cpu") && value != quick.CPUSchemaV2, nil
			}
			return false, nil
		}
	}
	found, err := visit(0)
	if found || err != nil {
		return true
	}
	_, err = decoder.Token()
	return err != io.EOF
}

// ValidateCPUExecutionNote checks current nonfunctional CPU outcomes without
// permitting a scalar or thermal observation hidden inside an execution note.
func ValidateCPUExecutionNote(binding ResultBinding) error {
	if binding.ID != "cpu.quick" || binding.Version != 2 || hasNewCPUClaim(binding.Evidence) {
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
	// Old CPU schemas and thermal schemas cannot be smuggled through detail.
	decoder := json.NewDecoder(bytes.NewReader(binding.Evidence))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ErrInvalid
		}
		if value, ok := token.(string); ok && (strings.EqualFold(value, quick.CPUSchemaV2) || strings.EqualFold(value, "hhsoft.pc-tester.run-thermal/v1")) {
			return ErrInvalid
		}
	}
	return nil
}
