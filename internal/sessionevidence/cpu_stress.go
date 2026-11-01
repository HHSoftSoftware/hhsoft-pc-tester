package sessionevidence

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
)

// HasCPUStressClaim selects strict admission, including escaped or earlier
// duplicate claims. A historical cpu.stress test ID alone is not an observation.
func HasCPUStressClaim(raw []byte) bool {
	if len(raw) == 0 {
		return false
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
			if strings.EqualFold(family, "hhsoft.pc-tester.cpu-stress") {
				return true
			}
			switch strings.ToLower(value) {
			case "cpu_stress_source", "requested_load_ms", "local_watchdog_ms", "load_started_ms", "load_entered_ms", "load_checks_passed", "preparation_checks_passed":
				return true
			}
		}
	}
}

func validStressSourceStructure(j Journal) bool {
	if !IsStandardSchema(j.Schema) {
		if IsExtendedSchema(j.Schema) {
			return j.Profile == "extended" && j.CPUStressSource == nil
		}
		return j.Profile == "" && j.CPUStressSource == nil
	}
	if j.Profile != "standard" || j.StorageSource != nil {
		return false
	}
	worker := j.Workers[1]
	if worker.State == "not_scheduled" || worker.State == "not_started" {
		return j.CPUStressSource == nil
	}
	if j.Workers[0].State != "completed" || j.Workers[0].Outcome != "pass" {
		return false
	}
	source := j.CPUStressSource
	return source != nil && source.WorkerID == "cpu.stress" && source.EvidenceSchema == quick.CPUStressSchema &&
		validDigest(source.EvidenceFileSHA256) && validDigest(source.EvidenceTypedSHA256)
}

func validateStressBinding(j Journal, worker WorkerRecord, binding ResultBinding, sources map[string]LocalThermalSource) (bool, error) {
	if !IsStandardSchema(j.Schema) || binding.ID != "cpu.stress" || binding.Version != 1 {
		return false, ErrInvalid
	}
	if worker.State == "not_scheduled" || worker.State == "not_started" {
		var note executionNote
		if j.CPUStressSource != nil || HasCPUStressClaim(binding.Evidence) || hasNewCPUClaim(binding.Evidence) ||
			evidencejson.ValidateShape(binding.Evidence, reflect.TypeOf(note)) != nil || json.Unmarshal(binding.Evidence, &note) != nil ||
			note.Schema != "hhsoft.pc-tester.execution-note/v1" || note.Status != binding.ReportStatus || note.SourceStatus != worker.Outcome {
			return false, ErrInvalid
		}
		detail := "not run; disposition recorded in the frozen plan"
		if worker.State == "not_started" {
			detail = "bounded evidence was not produced; outcome recorded by the run supervisor"
		}
		if note.Detail != detail {
			return false, ErrInvalid
		}
		if _, present := sources[worker.ID]; present {
			return false, ErrInvalid
		}
		return false, nil
	}
	value, digest, err := quickevidence.DecodeCPUStress(binding.Evidence)
	if err != nil {
		return false, err
	}
	if j.CPUStressSource == nil || worker.Timing == nil || binding.PlannedStatus != "scheduled" ||
		string(value.Status) != worker.Outcome || value.FinishedMilliseconds > worker.Timing.FinishedMilliseconds-worker.Timing.StartedMilliseconds ||
		j.CPUStressSource.EvidenceSchema != value.Schema || j.CPUStressSource.EvidenceTypedSHA256 != digest {
		return false, ErrInvalid
	}
	// Original-file hashing belongs to the file-owning finalizer. JSON report
	// compaction does not preserve those original file bytes.
	hot := value.Safety.ThermalSamples > 0 && value.Safety.PeakMilliCelsius >= value.Safety.ThermalLimitMilliCelsius
	source, present := sources[worker.ID]
	if hot != present {
		return false, ErrInvalid
	}
	if hot && (source.EvidenceSchema != value.Schema || source.EvidenceFileSHA256 != j.CPUStressSource.EvidenceFileSHA256 ||
		source.EvidenceTypedSHA256 != digest || source.PeakMilliCelsius != value.Safety.PeakMilliCelsius || source.LimitMilliCelsius != value.Safety.ThermalLimitMilliCelsius) {
		return false, ErrInvalid
	}
	return hot, nil
}
