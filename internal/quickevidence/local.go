// Package quickevidence independently validates retained CPU/RAM evidence.
package quickevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"reflect"
)

var ErrInvalid = errors.New("CPU or memory execution evidence does not establish valid safety and cleanup")
var ErrCleanupUnconfirmed = errors.New("CPU or memory thermal observer cleanup was not confirmed")
var ErrFailureDowngrade = errors.New("memory evidence would downgrade an explicit failure")

type LocalEvidence struct {
	Schema               string
	Status               quick.Status
	Safety               quick.SafetyEvidence
	DurationMilliseconds int64
	TypedSHA256          string
	Scalar               CPUScalarSummary
}

func DecodeLocal(workerID string, encoded []byte) (LocalEvidence, error) {
	var result LocalEvidence
	var canonical []byte
	switch workerID {
	case "cpu.quick":
		var err error
		result, canonical, err = decodeCPU(encoded)
		if err != nil {
			return LocalEvidence{}, err
		}
	case "memory.quick":
		var value quick.MemoryResult
		if evidencejson.ValidateShape(encoded, reflect.TypeOf(value)) != nil || json.Unmarshal(encoded, &value) != nil || value.Schema != quick.MemorySchema || !validStatus(value.Status) {
			return result, ErrInvalid
		}
		if quickCleanupUnconfirmed(value.Safety) {
			return result, ErrCleanupUnconfirmed
		}
		if value.ExpectedByte != value.ActualByte && value.Status != quick.StatusFail {
			return result, ErrFailureDowngrade
		}
		if !validateMemoryEvidence(value) {
			return result, ErrInvalid
		}
		result = LocalEvidence{Schema: value.Schema, Status: value.Status, Safety: value.Safety, DurationMilliseconds: value.DurationMilliseconds}
		canonical, _ = json.Marshal(value)
	default:
		return result, ErrInvalid
	}
	material := []byte("hhsoft.pc-tester.local-thermal-evidence/v1\x00" + result.Schema + "\x00")
	material = append(material, canonical...)
	digest := sha256.Sum256(material)
	result.TypedSHA256 = "sha256:" + hex.EncodeToString(digest[:])
	return result, nil
}
func validStatus(status quick.Status) bool {
	return status == quick.StatusPass || status == quick.StatusFail || status == quick.StatusInconclusive || status == quick.StatusError || status == quick.StatusAborted
}
func quickCleanupUnconfirmed(safety quick.SafetyEvidence) bool {
	return !safety.CleanupConfirmed || safety.MonitorStatus == "cleanup_unconfirmed" || safety.MonitorFailure == "cleanup_unconfirmed"
}
func validQuickSafety(safety quick.SafetyEvidence, status quick.Status, duration, minimumTimeout int64) bool {
	if duration < 0 || quickCleanupUnconfirmed(safety) || safety.ThermalObservations < 0 || safety.ThermalSensors < 0 ||
		safety.ThermalSamples < safety.ThermalSensors || safety.PeakMilliCelsius < 0 || safety.PeakMilliCelsius > 250000 ||
		safety.ThermalRejectedReadings < 0 || safety.ThermalUnavailableReadings < 0 || safety.ThermalEnumerationErrors < 0 {
		return false
	}
	observations, sensors, samples := int64(safety.ThermalObservations), int64(safety.ThermalSensors), int64(safety.ThermalSamples)
	if sensors == 0 && (samples != 0 || safety.PeakMilliCelsius != 0) || sensors > 0 && observations == 0 {
		return false
	}
	if sensors > 0 {
		// ceil(samples/sensors) avoids overflowing sensors*observations on
		// either supported word size while checking the same necessary bound.
		minimumObservations := samples / sensors
		if samples%sensors != 0 {
			minimumObservations++
		}
		if minimumObservations > observations {
			return false
		}
	}
	if observations == 0 && (sensors != 0 || samples != 0 || safety.PeakMilliCelsius != 0 ||
		safety.ThermalRejectedReadings != 0 || safety.ThermalUnavailableReadings != 0 || safety.ThermalEnumerationErrors != 0 ||
		safety.ThermalCoverage != "unavailable") {
		return false
	}
	switch safety.ThermalCoverage {
	case "available":
		if sensors == 0 || safety.ThermalRejectedReadings != 0 || safety.ThermalUnavailableReadings != 0 || safety.ThermalEnumerationErrors != 0 || samples-sensors < observations-1 {
			return false
		}
	case "unavailable":
		if sensors != 0 || safety.ThermalRejectedReadings != 0 || safety.ThermalEnumerationErrors != 0 {
			return false
		}
	case "partial":
		if safety.ThermalRejectedReadings == 0 && safety.ThermalUnavailableReadings == 0 && safety.ThermalEnumerationErrors == 0 {
			// Mixed available/empty observations can be partial without a read
			// error, but require a valid reading and at least one empty scan.
			if sensors == 0 || observations < 2 || samples/sensors > observations-1 || samples/sensors == observations-1 && samples%sensors != 0 {
				return false
			}
		} else if sensors == 0 && safety.ThermalRejectedReadings == 0 && safety.ThermalEnumerationErrors == 0 {
			return false
		}
	default:
		return false
	}
	limitsValid := safety.GlobalTimeoutMilliseconds >= minimumTimeout && safety.GlobalTimeoutMilliseconds <= 600000 &&
		safety.ThermalLimitMilliCelsius >= 50000 && safety.ThermalLimitMilliCelsius <= 125000
	if !limitsValid && !(safety.MonitorStatus == "not_started" && status == quick.StatusError &&
		safety.GlobalTimeoutMilliseconds == 0 && safety.ThermalLimitMilliCelsius == 0) {
		return false
	}
	switch safety.MonitorStatus {
	case "not_started":
		return observations == 0 && safety.MonitorFailure == "" && duration == 0 && status != quick.StatusPass && status != quick.StatusFail
	case "completed":
		if observations == 0 || safety.MonitorFailure != "" || safety.PeakMilliCelsius >= safety.ThermalLimitMilliCelsius {
			return false
		}
	case "failed":
		switch safety.MonitorFailure {
		case "thermal_limit":
			return observations > 0 && sensors > 0 && safety.PeakMilliCelsius >= safety.ThermalLimitMilliCelsius && (status == quick.StatusAborted || status == quick.StatusFail)
		case "observation_timeout", "observer_panic", "invalid_observation":
			return status == quick.StatusError || status == quick.StatusFail
		default:
			return false
		}
	default:
		return false
	}
	return status != quick.StatusPass || duration <= safety.GlobalTimeoutMilliseconds
}

func validateCPUEvidence(result quick.CPUResult) bool {
	if !validQuickSafety(result.Safety, result.Status, result.DurationMilliseconds, 10) ||
		result.Safety.ThermalObservations == 0 && (result.Iterations != 0 || result.StateAggregate != 0 || result.DurationMilliseconds != 0) {
		return false
	}
	if result.Status != quick.StatusPass {
		return true
	}
	known := sha256.Sum256([]byte("abc"))
	return result.Workers > 0 && result.Workers <= 64 && result.Iterations > 0 && result.KnownAnswerSHA256 == hex.EncodeToString(known[:])
}

func validateMemoryEvidence(result quick.MemoryResult) bool {
	if result.ExpectedByte != result.ActualByte && result.Status != quick.StatusFail {
		return false
	}
	if !validQuickSafety(result.Safety, result.Status, result.DurationMilliseconds, 1000) ||
		result.Safety.ThermalObservations == 0 && (result.ExaminedBytes != 0 || result.FailureOffset != 0 || result.ExpectedByte != 0 || result.ActualByte != 0 || result.DurationMilliseconds != 0) {
		return false
	}
	if result.Status != quick.StatusPass {
		return true
	}
	return result.AllocatedBytes >= 4<<20 && result.AllocatedBytes <= 512<<20 && result.Passes > 0 && result.Passes <= 8 && result.PatternsPerPass == 3 &&
		result.ExaminedBytes == result.AllocatedBytes*int64(result.Passes)*int64(result.PatternsPerPass) &&
		result.FailureOffset == 0 && result.ExpectedByte == 0 && result.ActualByte == 0
}
