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

// HasMemoryStressClaim selects the strict family, including escaped and earlier
// duplicate claims. An inactive historical test ID alone is not an observation.
func HasMemoryStressClaim(raw []byte) bool {
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
			if strings.EqualFold(family, "hhsoft.pc-tester.memory-stress") {
				return true
			}
			switch strings.ToLower(value) {
			case "memory_stress_source", "load_cycles", "terminal_uncredited_bytes":
				return true
			}
		}
	}
}

func validMemoryStressSourceStructure(j Journal) bool {
	if !IsExtendedSchema(j.Schema) {
		return j.MemoryStressSource == nil
	}
	if j.Profile != "extended" || j.StorageSource != nil || j.CPUStressSource != nil {
		return false
	}
	// ValidateStructure has already checked the literal fifteen-worker order.
	worker := j.Workers[2]
	if worker.State == "not_scheduled" || worker.State == "not_started" {
		return j.MemoryStressSource == nil
	}
	if j.Workers[1].State != "completed" || j.Workers[1].Outcome != "pass" {
		return false
	}
	source := j.MemoryStressSource
	return source != nil && source.WorkerID == "memory.stress" && source.EvidenceSchema == quick.MemoryStressSchema &&
		validDigest(source.EvidenceFileSHA256) && validDigest(source.EvidenceTypedSHA256)
}

func validateMemoryStressBinding(j Journal, worker WorkerRecord, binding ResultBinding, sources map[string]LocalThermalSource) (bool, error) {
	if !IsExtendedSchema(j.Schema) || binding.ID != "memory.stress" || binding.Version != 2 {
		return false, ErrInvalid
	}
	if worker.State == "not_scheduled" || worker.State == "not_started" {
		var note executionNote
		if j.MemoryStressSource != nil || HasMemoryStressClaim(binding.Evidence) || HasCPUStressClaim(binding.Evidence) || hasNewMemoryClaim(binding.Evidence) ||
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
	value, digest, err := quickevidence.DecodeMemoryStress(binding.Evidence)
	if err != nil {
		return false, err
	}
	if j.MemoryStressSource == nil || worker.Timing == nil || binding.PlannedStatus != "scheduled" ||
		string(value.Status) != worker.Outcome || value.FinishedMilliseconds > worker.Timing.FinishedMilliseconds-worker.Timing.StartedMilliseconds ||
		j.MemoryStressSource.EvidenceSchema != value.Schema || j.MemoryStressSource.EvidenceTypedSHA256 != digest {
		return false, ErrInvalid
	}
	// The finalizer owns original-file hashing. A signed JSON report may compact
	// equivalent evidence; its consumer verifies the independent typed digest.
	hot := value.Safety.ThermalSamples > 0 && value.Safety.PeakMilliCelsius >= value.Safety.ThermalLimitMilliCelsius
	source, present := sources[worker.ID]
	if hot != present {
		return false, ErrInvalid
	}
	if hot && (source.EvidenceSchema != value.Schema || source.EvidenceFileSHA256 != j.MemoryStressSource.EvidenceFileSHA256 ||
		source.EvidenceTypedSHA256 != digest || source.PeakMilliCelsius != value.Safety.PeakMilliCelsius || source.LimitMilliCelsius != value.Safety.ThermalLimitMilliCelsius) {
		return false, ErrInvalid
	}
	return hot, nil
}
