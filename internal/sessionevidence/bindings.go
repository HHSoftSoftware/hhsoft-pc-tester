package sessionevidence

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/keyboardevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
)

func reportStatus(outcome string) string {
	switch outcome {
	case "excluded", "not_present", "unsupported":
		return "skip"
	case "missing_driver":
		return "inconclusive"
	default:
		return outcome
	}
}

type executionNote struct {
	Schema       string `json:"schema"`
	Status       string `json:"status"`
	SourceStatus string `json:"source_status"`
	Detail       string `json:"detail"`
}

func hasKeyboardSchema(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return false
		}
		if err != nil {
			return true
		}
		if value, ok := token.(string); ok && strings.EqualFold(value, inputtest.KeyboardSchema) {
			return true
		}
	}
}

func ValidateBindings(j Journal, bindings []ResultBinding) error {
	if ValidateStructure(j) != nil {
		return ErrInvalid
	}
	byID := make(map[string]ResultBinding, len(bindings))
	for _, binding := range bindings {
		if binding.ID == "" || len(binding.Evidence) == 0 || len(binding.Evidence) > 1<<20 || !json.Valid(binding.Evidence) {
			return ErrInvalid
		}
		if _, exists := byID[binding.ID]; exists {
			return ErrInvalid
		}
		if hasKeyboardSchema(binding.Evidence) && (j.Schema != Schema || binding.ID != "input.keyboard.quick") ||
			j.Schema == SchemaV4 && binding.ID == "input.keyboard.quick" && binding.Version >= 2 {
			return ErrInvalid
		}
		byID[binding.ID] = binding
	}
	localSources := make(map[string]LocalThermalSource, len(j.LocalThermalSources))
	for _, source := range j.LocalThermalSources {
		localSources[source.WorkerID] = source
	}
	wanted := 0
	for _, worker := range j.Workers {
		binding, present := byID[worker.ID]
		if !present || binding.ReportStatus != reportStatus(worker.Outcome) || (worker.State == "not_scheduled") != (binding.PlannedStatus != "scheduled") || worker.State == "not_scheduled" && worker.Outcome != binding.PlannedStatus {
			return ErrInvalid
		}
		if len(binding.Evidence) == 0 || len(binding.Evidence) > 1<<20 || !json.Valid(binding.Evidence) {
			return ErrInvalid
		}
		if worker.ID == "input.keyboard.quick" {
			if binding.Version != 2 || validateKeyboardBinding(worker, binding) != nil {
				return ErrInvalid
			}
			continue
		}
		if worker.ID != "cpu.quick" && worker.ID != "memory.quick" {
			continue
		}
		var header struct {
			Schema string `json:"schema"`
		}
		if json.Unmarshal(binding.Evidence, &header) != nil {
			return ErrInvalid
		}
		if header.Schema == "hhsoft.pc-tester.execution-note/v1" {
			var note executionNote
			if evidencejson.ValidateShape(binding.Evidence, reflect.TypeOf(note)) != nil || json.Unmarshal(binding.Evidence, &note) != nil || note.Status != binding.ReportStatus || note.SourceStatus != worker.Outcome {
				return ErrInvalid
			}
			if worker.State != "not_started" && worker.State != "not_scheduled" && worker.Outcome != "error" && worker.Outcome != "aborted" {
				return ErrInvalid
			}
			if _, exists := localSources[worker.ID]; exists {
				return ErrInvalid
			}
			continue
		}
		if worker.Timing == nil {
			return ErrInvalid
		}
		local, err := quickevidence.DecodeLocal(worker.ID, binding.Evidence)
		if err != nil || string(local.Status) != worker.Outcome || local.DurationMilliseconds > worker.Timing.FinishedMilliseconds-worker.Timing.StartedMilliseconds || local.Safety.ThermalObservations > 0 && local.Safety.ThermalLimitMilliCelsius != thermal.LimitMilliCelsius {
			return ErrInvalid
		}
		hot := local.Safety.ThermalSamples > 0 && local.Safety.PeakMilliCelsius >= local.Safety.ThermalLimitMilliCelsius
		source, exists := localSources[worker.ID]
		if hot != exists {
			return ErrInvalid
		}
		if hot {
			wanted++
			if source.EvidenceSchema != local.Schema || source.EvidenceTypedSHA256 != local.TypedSHA256 || source.PeakMilliCelsius != local.Safety.PeakMilliCelsius || source.LimitMilliCelsius != local.Safety.ThermalLimitMilliCelsius {
				return ErrInvalid
			}
		}
	}
	if wanted != len(localSources) {
		return ErrInvalid
	}
	return nil
}

func validateKeyboardBinding(worker WorkerRecord, binding ResultBinding) error {
	var header struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(binding.Evidence, &header) != nil {
		return ErrInvalid
	}
	if header.Schema == "hhsoft.pc-tester.execution-note/v1" {
		var note executionNote
		if evidencejson.ValidateShape(binding.Evidence, reflect.TypeOf(note)) != nil || json.Unmarshal(binding.Evidence, &note) != nil || note.Status != binding.ReportStatus || note.SourceStatus != worker.Outcome || hasKeyboardSchema(binding.Evidence) {
			return ErrInvalid
		}
		if worker.State != "not_started" && worker.State != "not_scheduled" && worker.Outcome != "error" && worker.Outcome != "aborted" {
			return ErrInvalid
		}
		return nil
	}
	value, err := keyboardevidence.Decode(binding.Evidence)
	if err != nil || worker.Timing == nil || string(value.Status) != worker.Outcome || value.DurationMilliseconds > worker.Timing.FinishedMilliseconds-worker.Timing.StartedMilliseconds {
		return ErrInvalid
	}
	return nil
}

// ValidateUnexecutedKeyboard permits a standalone frozen-plan report to retain
// a non-scheduled keyboard disposition. Functional or attempted observations
// require the current session journal and its worker interval.
func ValidateUnexecutedKeyboard(binding ResultBinding) error {
	if binding.ID != "input.keyboard.quick" || binding.Version != 2 || binding.ReportStatus != reportStatus(binding.PlannedStatus) {
		return ErrInvalid
	}
	switch binding.PlannedStatus {
	case "excluded", "not_present", "unsupported", "missing_driver":
	default:
		return ErrInvalid
	}
	var note executionNote
	if evidencejson.ValidateShape(binding.Evidence, reflect.TypeOf(note)) != nil || json.Unmarshal(binding.Evidence, &note) != nil ||
		note.Schema != "hhsoft.pc-tester.execution-note/v1" || note.Status != binding.ReportStatus || note.SourceStatus != binding.PlannedStatus || hasKeyboardSchema(binding.Evidence) {
		return ErrInvalid
	}
	return nil
}

func DecodeIntegrity(raw []byte) (IntegrityEvidence, error) {
	var value IntegrityEvidence
	if evidencejson.ValidateShape(raw, reflect.TypeOf(value)) != nil || json.Unmarshal(raw, &value) != nil || value.Schema != IntegritySchema || value.Session == nil || ValidateStructure(*value.Session) != nil || value.PlanID != value.Session.PlanID || !validDigest(value.PlanID) || !validPrefixedDigest(value.PlannedHardwareGraph, "hmac-sha256:") || !validPrefixedDigest(value.FinalHardwareGraph, "hmac-sha256:") || value.PlannedComponentCount < 0 || value.FinalComponentCount < 0 {
		return IntegrityEvidence{}, ErrInvalid
	}
	// Apply the journal's bound to its original nested JSON bytes, including
	// whitespace, rather than to a smaller re-encoding of the typed value.
	var nested struct {
		Session json.RawMessage `json:"session"`
	}
	if json.Unmarshal(raw, &nested) != nil {
		return IntegrityEvidence{}, ErrInvalid
	}
	journal, err := Decode(nested.Session)
	if err != nil {
		return IntegrityEvidence{}, ErrInvalid
	}
	value.Session = &journal
	if len(value.RemovedComponents) > value.PlannedComponentCount || len(value.AddedComponents) > value.FinalComponentCount || value.PlannedComponentCount-len(value.RemovedComponents)+len(value.AddedComponents) != value.FinalComponentCount || len(value.ChangedComponents) > min(value.PlannedComponentCount-len(value.RemovedComponents), value.FinalComponentCount-len(value.AddedComponents)) {
		return IntegrityEvidence{}, ErrInvalid
	}
	changed := len(value.AddedComponents)+len(value.RemovedComponents)+len(value.ChangedComponents) > 0
	if changed == (value.PlannedHardwareGraph == value.FinalHardwareGraph) || value.Status != "pass" && value.Status != "fail" || (value.Status == "fail") != changed {
		return IntegrityEvidence{}, ErrInvalid
	}
	return value, nil
}
