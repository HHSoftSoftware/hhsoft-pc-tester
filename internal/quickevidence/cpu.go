package quickevidence

import (
	"encoding/json"
	"math"
	"reflect"
	"regexp"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

// These historical wire types are immutable: their field order and omitempty
// tags are part of CPU-v2's canonical typed hash, including its safety object.
type cpuV2 struct {
	Schema               string       `json:"schema"`
	Status               quick.Status `json:"status"`
	Workers              int          `json:"workers"`
	Iterations           uint64       `json:"iterations"`
	StateAggregate       uint64       `json:"state_aggregate"`
	KnownAnswerSHA256    string       `json:"known_answer_sha256"`
	DurationMilliseconds int64        `json:"duration_ms"`
	Safety               cpuSafetyV2  `json:"safety"`
	Detail               string       `json:"detail,omitempty"`
}

type cpuSafetyV2 struct {
	GlobalTimeoutMilliseconds  int64  `json:"global_timeout_ms"`
	ThermalLimitMilliCelsius   int    `json:"thermal_limit_millicelsius"`
	ThermalSensors             int    `json:"thermal_sensors"`
	ThermalSamples             int    `json:"thermal_samples"`
	PeakMilliCelsius           int    `json:"peak_millicelsius,omitempty"`
	MonitorStatus              string `json:"monitor_status"`
	MonitorFailure             string `json:"monitor_failure,omitempty"`
	CleanupConfirmed           bool   `json:"cleanup_confirmed"`
	ThermalObservations        int    `json:"thermal_observations"`
	ThermalCoverage            string `json:"thermal_coverage"`
	ThermalRejectedReadings    int    `json:"thermal_rejected_readings"`
	ThermalUnavailableReadings int    `json:"thermal_unavailable_readings"`
	ThermalEnumerationErrors   int    `json:"thermal_enumeration_errors"`
}

func (v cpuSafetyV2) current() quick.SafetyEvidence {
	return quick.SafetyEvidence{GlobalTimeoutMilliseconds: v.GlobalTimeoutMilliseconds, ThermalLimitMilliCelsius: v.ThermalLimitMilliCelsius,
		ThermalSensors: v.ThermalSensors, ThermalSamples: v.ThermalSamples, PeakMilliCelsius: v.PeakMilliCelsius,
		MonitorStatus: v.MonitorStatus, MonitorFailure: v.MonitorFailure, CleanupConfirmed: v.CleanupConfirmed,
		ThermalObservations: v.ThermalObservations, ThermalCoverage: v.ThermalCoverage, ThermalRejectedReadings: v.ThermalRejectedReadings,
		ThermalUnavailableReadings: v.ThermalUnavailableReadings, ThermalEnumerationErrors: v.ThermalEnumerationErrors}
}

// CPUScalarSummary is copied only from fully validated v3 evidence. The zero
// value denotes historical CPU or memory evidence without scalar observations.
type CPUScalarSummary struct {
	Present             bool
	Suite               string
	VectorCount         int
	ChecksPassed        uint64
	WorkersStarted      int
	WorkersCompleted    int
	MismatchedWorkers   int
	CalculationFailures quick.CPUCalculationFailures
}

func decodeCPU(encoded []byte) (LocalEvidence, []byte, error) {
	var header struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(encoded, &header) != nil {
		return LocalEvidence{}, nil, ErrInvalid
	}
	var value quick.CPUResult
	var canonical []byte
	switch header.Schema {
	case quick.CPUSchemaV2:
		var old cpuV2
		if evidencejson.ValidateShape(encoded, reflect.TypeOf(old)) != nil || json.Unmarshal(encoded, &old) != nil {
			return LocalEvidence{}, nil, ErrInvalid
		}
		value = quick.CPUResult{Schema: old.Schema, Status: old.Status, Workers: old.Workers, Iterations: old.Iterations,
			StateAggregate: old.StateAggregate, KnownAnswerSHA256: old.KnownAnswerSHA256, DurationMilliseconds: old.DurationMilliseconds,
			Safety: old.Safety.current(), Detail: old.Detail}
		canonical, _ = json.Marshal(old)
	case quick.CPUSchema:
		if evidencejson.ValidateShape(encoded, reflect.TypeOf(value)) != nil || json.Unmarshal(encoded, &value) != nil {
			return LocalEvidence{}, nil, ErrInvalid
		}
		canonical, _ = json.Marshal(value)
	default:
		return LocalEvidence{}, nil, ErrInvalid
	}
	if !validStatus(value.Status) {
		return LocalEvidence{}, nil, ErrInvalid
	}
	if quickCleanupUnconfirmed(value.Safety) {
		return LocalEvidence{}, nil, ErrCleanupUnconfirmed
	}
	if !validateCPUEvidence(value) {
		return LocalEvidence{}, nil, ErrInvalid
	}
	result := LocalEvidence{Schema: value.Schema, Status: value.Status, Safety: value.Safety, DurationMilliseconds: value.DurationMilliseconds}
	if value.Schema == quick.CPUSchema {
		summary, valid := validateCPUScalar(value)
		if !valid {
			return LocalEvidence{}, nil, ErrInvalid
		}
		result.Scalar = summary
	}
	return result, canonical, nil
}

var scalarBits = regexp.MustCompile(`^[0-9a-f]{16}$`)

func validateCPUScalar(value quick.CPUResult) (CPUScalarSummary, bool) {
	scalar := value.Scalar
	invalid := CPUScalarSummary{}
	if scalar.Suite != "binary64-normal-exact-v1" || scalar.VectorCount != 12 || scalar.Workers == nil ||
		value.Workers < 0 || value.Workers > 64 || len(scalar.Workers) > value.Workers || scalar.WorkersCompleted < 0 {
		return invalid, false
	}
	if value.Workers == 0 && (value.Status != quick.StatusError || value.Safety.MonitorStatus != "not_started" ||
		value.Safety.GlobalTimeoutMilliseconds != 0 || value.Safety.ThermalLimitMilliCelsius != 0) {
		return invalid, false
	}
	if value.KnownAnswerSHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		return invalid, false
	}
	// Fixed bit oracles are independent of the producer's arithmetic operations.
	ids := [...]string{"add1", "add2", "add3", "sub1", "sub2", "sub3", "mul1", "mul2", "mul3", "div1", "div2", "div3"}
	oracles := [...]string{"400e000000000000", "c01e000000000000", "3eb8000000000000", "4011000000000000", "c00e000000000000", "4260000000000000",
		"400e000000000000", "bfe8000000000000", "4090000000000000", "4008000000000000", "c002000000000000", "3d70000000000000"}
	var iterations, checks uint64
	completed, mismatches := 0, 0
	knownAnswerFailureRows := 0
	for index, worker := range scalar.Workers {
		if worker.Index != index || math.MaxUint64-iterations < worker.Iterations || math.MaxUint64-checks < worker.ChecksPassed {
			return invalid, false
		}
		iterations += worker.Iterations
		checks += worker.ChecksPassed
		q, r := worker.Iterations/256, worker.Iterations%256
		if q > math.MaxUint64/12 {
			return invalid, false
		}
		base := q * 12
		if r == 0 && worker.ChecksPassed == base && worker.Mismatch == nil {
			knownAnswerFailureRows++
		}
		if worker.ChecksPassed < base || worker.ChecksPassed-base > 12 || r != 0 && worker.ChecksPassed-base != 12 {
			return invalid, false
		}
		if worker.ChecksPassed >= 12 {
			completed++
		}
		if mismatch := worker.Mismatch; mismatch != nil {
			slot := worker.ChecksPassed - base
			if r != 0 || slot >= 12 || mismatch.Vector != ids[slot] || mismatch.ExpectedBits != oracles[slot] ||
				!scalarBits.MatchString(mismatch.ActualBits) || mismatch.ActualBits == mismatch.ExpectedBits {
				return invalid, false
			}
			mismatches++
		}
	}
	if iterations != value.Iterations || checks != scalar.ChecksPassed || completed != scalar.WorkersCompleted ||
		value.CalculationFailures.Scalar != (mismatches != 0) {
		return invalid, false
	}
	failures := value.CalculationFailures
	// A worker returns immediately on its first mismatch. Distinct integer and
	// SHA failures therefore need distinct rows, neither with a scalar mismatch.
	knownAnswerFailureKinds := 0
	if failures.Integer {
		knownAnswerFailureKinds++
	}
	if failures.SHA256 {
		knownAnswerFailureKinds++
	}
	if knownAnswerFailureKinds > knownAnswerFailureRows {
		return invalid, false
	}
	if (failures.Integer || failures.SHA256 || failures.Scalar) != (value.Status == quick.StatusFail) {
		return invalid, false
	}
	if value.Status == quick.StatusPass && (len(scalar.Workers) != value.Workers || completed != value.Workers) {
		return invalid, false
	}
	if value.Safety.ThermalObservations == 0 && (len(scalar.Workers) != 0 || checks != 0) {
		return invalid, false
	}
	return CPUScalarSummary{Present: true, Suite: scalar.Suite, VectorCount: scalar.VectorCount, ChecksPassed: checks,
		WorkersStarted: len(scalar.Workers), WorkersCompleted: completed, MismatchedWorkers: mismatches, CalculationFailures: failures}, true
}
