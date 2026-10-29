package sessionevidence

import (
	"encoding/hex"
	"errors"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
)

var ErrInvalid = errors.New("session evidence is incomplete or contradictory")

func Decode(raw []byte) (Journal, error) {
	value, err := decodeJournalWire(raw)
	if err != nil {
		return Journal{}, err
	}
	if err := ValidateStructure(value); err != nil {
		return Journal{}, err
	}
	return value, nil
}
func validDigest(value string) bool { return validPrefixedDigest(value, "sha256:") }
func validPrefixedDigest(value, prefix string) bool {
	if len(value) != len(prefix)+64 || !strings.HasPrefix(value, prefix) || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value[len(prefix):])
	return err == nil
}
func safe(value safemode.Result) bool {
	return value.Safe() && value.StorageDetail == "" && value.NetworkDetail == ""
}
func HasHeat(value Journal) bool {
	return value.Thermal.FirstFinding != nil || len(value.LocalThermalSources) > 0
}

func ValidateStructure(j Journal) error {
	if !IsStrictSchema(j.Schema) || !validDigest(j.PlanID) || j.BudgetMilliseconds < 10 || j.BudgetMilliseconds > 900000 || j.ElapsedMilliseconds < 0 || j.ElapsedMilliseconds > 3600000 {
		return ErrInvalid
	}
	if j.PreparationStartedMilliseconds < 0 || j.InventoryPreparedMilliseconds < j.PreparationStartedMilliseconds || j.StopRequestedMilliseconds < j.InventoryPreparedMilliseconds || j.ElapsedMilliseconds < j.StopRequestedMilliseconds || j.ElapsedMilliseconds-j.StopRequestedMilliseconds > 2000 {
		return ErrInvalid
	}
	if ValidateMonitor(j.Monitor, j.Monitor.ElapsedMilliseconds) != nil || j.SafetyJoinedMilliseconds < j.StopRequestedMilliseconds || j.SafetyJoinedMilliseconds > j.Monitor.ElapsedMilliseconds || j.Monitor.ElapsedMilliseconds > j.ElapsedMilliseconds {
		return ErrInvalid
	}
	if thermal.ValidateResult(j.Thermal, true) != nil || !j.Thermal.Joined || j.Thermal.Initial.CompletedMilliseconds > j.PreparationStartedMilliseconds || j.Thermal.JoinedMilliseconds < j.StopRequestedMilliseconds || j.Thermal.JoinedMilliseconds > j.Thermal.ElapsedMilliseconds || j.Thermal.ElapsedMilliseconds > j.ElapsedMilliseconds || j.Thermal.Final.RequestedMilliseconds < j.InventoryPreparedMilliseconds || j.Thermal.Final.CompletedMilliseconds > j.StopRequestedMilliseconds {
		return ErrInvalid
	}
	switch j.StopReason {
	case "completed", "operator_cancelled", "run_deadline", "test_deadline", "worker_stop", "thermal_limit":
	default:
		return ErrInvalid
	}
	if (j.StopReason == "thermal_limit") != HasHeat(j) {
		return ErrInvalid
	}
	ids := workerIDs(j.Schema)
	maximumSources := 2
	if j.Schema == SchemaV8 {
		maximumSources = 3
	}
	if len(j.Workers) != len(ids) || j.Safety == nil || j.LocalThermalSources == nil || len(j.LocalThermalSources) > maximumSources {
		return ErrInvalid
	}
	phases := []string{"preflight"}
	stopped := false
	previousEnd := int64(0)
	for index, worker := range j.Workers {
		if worker.ID != ids[index] {
			return ErrInvalid
		}
		switch worker.State {
		case "not_scheduled":
			if worker.Timing != nil {
				return ErrInvalid
			}
			switch worker.Outcome {
			case "excluded", "not_present", "unsupported", "missing_driver":
			default:
				return ErrInvalid
			}
		case "not_started":
			if worker.Timing != nil || worker.Outcome != "aborted" || j.StopReason == "completed" {
				return ErrInvalid
			}
			stopped = true
		case "completed", "interrupted":
			if stopped || worker.Timing == nil {
				return ErrInvalid
			}
			if finding := j.Thermal.FirstFinding; finding != nil && (finding.Phase == "initial" || finding.Phase == "periodic" && worker.Timing.StartedMilliseconds > finding.ElapsedMilliseconds) {
				return ErrInvalid
			}
			timing := worker.Timing
			if timing.StartedMilliseconds < previousEnd || timing.StartedMilliseconds < j.Thermal.Initial.CompletedMilliseconds || timing.FinishedMilliseconds < timing.StartedMilliseconds || timing.FinishedMilliseconds > j.PreparationStartedMilliseconds {
				return ErrInvalid
			}
			previousEnd = timing.FinishedMilliseconds
			if worker.State == "interrupted" {
				if j.StopReason == "completed" || worker.Outcome != "aborted" && worker.Outcome != "error" {
					return ErrInvalid
				}
				stopped = true
			} else {
				switch worker.Outcome {
				case "pass", "fail", "skip", "inconclusive", "error", "aborted":
				default:
					return ErrInvalid
				}
				if worker.Outcome == "fail" || worker.Outcome == "aborted" || (worker.ID == "cpu.quick" || worker.ID == "memory.quick" || j.Schema == SchemaV8 && worker.ID == "cpu.stress") && worker.Outcome != "pass" {
					if j.StopReason == "completed" {
						return ErrInvalid
					}
					stopped = true
				}
			}
			if (worker.ID == "network.readiness.quick" || worker.ID == "storage.readiness.quick") && (worker.State != "completed" || worker.Outcome != "pass" && worker.Outcome != "inconclusive") {
				return ErrInvalid
			}
			phases = append(phases, "before-"+worker.ID)
		default:
			return ErrInvalid
		}
	}
	phases = append(phases, "final", "pre-sign")
	if len(j.Safety) != len(phases) {
		return ErrInvalid
	}
	for index, observation := range j.Safety {
		if observation.Phase != phases[index] || !safe(observation.Result) {
			return ErrInvalid
		}
	}
	if !validStorageSourceStructure(j) || !validStressSourceStructure(j) {
		return ErrInvalid
	}
	previousSource := -1
	for _, source := range j.LocalThermalSources {
		index := 0
		schema := quick.CPUSchemaV2
		switch source.WorkerID {
		case "cpu.quick":
			if supportsCurrentWorkerEvidence(j.Schema) && source.EvidenceSchema == quick.CPUSchema {
				schema = quick.CPUSchema
			}
		case "memory.quick":
			index = 1
			if j.Schema == SchemaV8 {
				index = 2
			}
			schema = quick.MemorySchemaV2
			if supportsCurrentWorkerEvidence(j.Schema) && source.EvidenceSchema == quick.MemorySchema {
				schema = quick.MemorySchema
			}
		case "cpu.stress":
			if j.Schema != SchemaV8 {
				return ErrInvalid
			}
			index, schema = 1, quick.CPUStressSchema
		default:
			return ErrInvalid
		}
		worker := j.Workers[index]
		if index <= previousSource || worker.Timing == nil || source.EvidenceSchema != schema || !validDigest(source.EvidenceFileSHA256) || !validDigest(source.EvidenceTypedSHA256) || source.LimitMilliCelsius != thermal.LimitMilliCelsius || source.PeakMilliCelsius < source.LimitMilliCelsius || source.PeakMilliCelsius > 250000 || source.StartedMilliseconds != worker.Timing.StartedMilliseconds || source.FinishedMilliseconds != worker.Timing.FinishedMilliseconds {
			return ErrInvalid
		}
		previousSource = index
	}
	return nil
}

func CloneJournal(j Journal) Journal {
	if j.CPUStressSource != nil {
		value := *j.CPUStressSource
		j.CPUStressSource = &value
	}
	if j.StorageSource != nil {
		value := *j.StorageSource
		j.StorageSource = &value
	}
	j.Workers = append([]WorkerRecord(nil), j.Workers...)
	for index := range j.Workers {
		if j.Workers[index].Timing != nil {
			value := *j.Workers[index].Timing
			j.Workers[index].Timing = &value
		}
	}
	j.Safety = append([]SafetyRecord(nil), j.Safety...)
	if j.LocalThermalSources != nil {
		j.LocalThermalSources = append([]LocalThermalSource{}, j.LocalThermalSources...)
	}
	if j.Monitor.FirstFailure != nil {
		failure := *j.Monitor.FirstFailure
		if failure.Observation != nil {
			value := *failure.Observation
			failure.Observation = &value
		}
		j.Monitor.FirstFailure = &failure
	}
	if j.Monitor.LastObservation != nil {
		value := *j.Monitor.LastObservation
		j.Monitor.LastObservation = &value
	}
	j.Thermal = thermal.CloneResult(j.Thermal)
	return j
}

// ThermalViewOf consumes a validated journal. It returns independent storage.
func ThermalViewOf(j Journal) ThermalView {
	scope := Scope
	if j.Schema == SchemaV8 {
		scope = StandardScope
	}
	finding := "no_limit_observed"
	if HasHeat(j) {
		finding = "limit_reached"
	}
	return ThermalView{Scope: scope, Finding: finding, Coverage: j.Thermal.Aggregate.Coverage, StopReason: j.StopReason,
		PeakObserved: j.Thermal.Aggregate.ValidReadings > 0, PeakMilliCelsius: j.Thermal.Aggregate.PeakMilliCelsius,
		LimitMilliCelsius: j.Thermal.LimitMilliCelsius, AcceptedObservations: j.Thermal.Aggregate.AcceptedObservations,
		MaximumGapMilliseconds: j.Thermal.MaximumGapMilliseconds, CleanupConfirmed: j.Thermal.CleanupConfirmed,
		LocalSources: append([]LocalThermalSource{}, j.LocalThermalSources...)}
}
