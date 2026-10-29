package quickevidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

var ErrInvalidCPUStress = errors.New("CPU stress evidence is incomplete or contradictory")

// DecodeCPUStress admits only the complete, owned stress-v1 observation. The
// typed digest commits to its canonical encoding, not to original file bytes.
func DecodeCPUStress(raw []byte) (quick.CPUStressResult, string, error) {
	var value quick.CPUStressResult
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) ||
		!cpuStressShape(raw, reflect.TypeOf(value), 0) || json.Unmarshal(raw, &value) != nil {
		return quick.CPUStressResult{}, "", ErrInvalidCPUStress
	}
	if quickCleanupUnconfirmed(value.Safety) || value.StopFacts.CleanupUnconfirmed {
		return quick.CPUStressResult{}, "", quick.ErrCPUStressCleanupUnconfirmed
	}
	if !validCPUStress(value) {
		return quick.CPUStressResult{}, "", ErrInvalidCPUStress
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return quick.CPUStressResult{}, "", ErrInvalidCPUStress
	}
	material := append([]byte("hhsoft.pc-tester.cpu-stress-evidence/v1\x00"), canonical...)
	digest := sha256.Sum256(material)
	return value, "sha256:" + hex.EncodeToString(digest[:]), nil
}

// This schema requires nullable pointer members. The historical shape checker
// deliberately rejects pointer nulls, so its behavior is not changed here.
func cpuStressShape(raw []byte, kind reflect.Type, depth int) bool {
	if depth > 32 {
		return false
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return kind.Kind() == reflect.Pointer
	}
	if kind.Kind() == reflect.Pointer {
		return cpuStressShape(raw, kind.Elem(), depth+1)
	}
	switch kind.Kind() {
	case reflect.Struct:
		decoder := json.NewDecoder(bytes.NewReader(raw))
		first, err := decoder.Token()
		if err != nil || first != json.Delim('{') {
			return false
		}
		fields, required := map[string]reflect.Type{}, map[string]bool{}
		for i := 0; i < kind.NumField(); i++ {
			field := kind.Field(i)
			parts := strings.Split(field.Tag.Get("json"), ",")
			fields[parts[0]], required[parts[0]] = field.Type, len(parts) == 1
		}
		seen := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			name, ok := token.(string)
			if err != nil || !ok || seen[name] || fields[name] == nil {
				return false
			}
			seen[name] = true
			var encoded json.RawMessage
			if decoder.Decode(&encoded) != nil || !cpuStressShape(encoded, fields[name], depth+1) {
				return false
			}
		}
		last, err := decoder.Token()
		if err != nil || last != json.Delim('}') {
			return false
		}
		if _, err := decoder.Token(); err != io.EOF {
			return false
		}
		for name, mandatory := range required {
			if mandatory && !seen[name] {
				return false
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil || values == nil || len(values) > 64 {
			return false
		}
		for _, encoded := range values {
			if !cpuStressShape(encoded, kind.Elem(), depth+1) {
				return false
			}
		}
	default:
		// Typed unmarshal also rejects booleans in integer fields, fractions,
		// overflow and strings that would otherwise be accepted as zero values.
		if json.Unmarshal(raw, reflect.New(kind).Interface()) != nil {
			return false
		}
	}
	return true
}

func stressTime(value int64) bool { return value >= 0 && value <= 3600000 }

func stressFailures(value quick.CPUCalculationFailures) int {
	count := 0
	if value.Integer {
		count++
	}
	if value.SHA256 {
		count++
	}
	if value.Scalar {
		count++
	}
	return count
}

func validCPUStress(v quick.CPUStressResult) bool {
	f := v.StopFacts
	if v.Schema != quick.CPUStressSchema || !validStatus(v.Status) || !v.AccountingValid ||
		f.ClockInvalid || f.CounterOverflow || f.WorkerPanic ||
		v.RequestedLoadMilliseconds != 600000 || v.LocalWatchdogMilliseconds != 660000 ||
		v.ThermalPolicy != "available-observations/v1" ||
		(v.WorkerSelection != "automatic" && v.WorkerSelection != "explicit") ||
		v.WorkersRequested < 1 || v.WorkersRequested > 64 || v.WorkersLaunched < 0 ||
		v.WorkersLaunched > v.WorkersRequested || v.Workers == nil || len(v.Workers) != v.WorkersLaunched ||
		v.WorkersReady < 0 || v.WorkersReady > v.WorkersLaunched ||
		v.EffectiveDeadlineMilliseconds < 0 || v.EffectiveDeadlineMilliseconds > 660000 ||
		!stressTime(v.WorkersJoinedMilliseconds) || !stressTime(v.FinishedMilliseconds) ||
		v.FinishedMilliseconds < v.WorkersJoinedMilliseconds ||
		v.KnownAnswerSHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" ||
		v.ScalarSuite != "binary64-normal-exact-v1" || v.ScalarVectorCount != 12 {
		return false
	}
	switch v.EffectiveDeadlineSource {
	case "parent":
	case "watchdog", "both":
		if v.EffectiveDeadlineMilliseconds != 660000 {
			return false
		}
	default:
		return false
	}
	if v.WorkersLaunched == 0 && v.StateAggregate != 0 {
		return false
	}
	if v.FinishedMilliseconds >= 660000 && !f.WatchdogDeadline ||
		v.EffectiveDeadlineSource == "parent" && v.FinishedMilliseconds > v.EffectiveDeadlineMilliseconds && !f.ParentDeadline {
		return false
	}
	if f.WatchdogDeadline && v.FinishedMilliseconds < 660000 ||
		(f.ParentDeadline || f.WatchdogDeadline) && v.FinishedMilliseconds < v.EffectiveDeadlineMilliseconds ||
		f.WatchdogDeadline && v.EffectiveDeadlineSource == "parent" && !f.ParentDeadline ||
		f.ParentDeadline && v.EffectiveDeadlineSource == "watchdog" && !f.WatchdogDeadline ||
		v.EffectiveDeadlineSource == "both" && f.ParentDeadline != f.WatchdogDeadline {
		return false
	}
	if f.LaunchIncomplete != (v.WorkersLaunched != v.WorkersRequested) ||
		f.ReadinessIncomplete != (v.WorkersReady != v.WorkersRequested) ||
		f.InitialCoveragePartial && f.InitialCoverageUnavailable ||
		(f.InitialCoveragePartial || f.InitialCoverageUnavailable) && (v.WorkersLaunched != 0 || f.CoverageLost) ||
		f.InsufficientBudget && (v.LoadStarted || v.FinishedMilliseconds+600000 < v.EffectiveDeadlineMilliseconds) {
		return false
	}
	if !validCPUStressSafety(v) {
		return false
	}
	var iterations, checks uint64
	var failures quick.CPUCalculationFailures
	ready := 0
	incompleteChecks, earlyProven, missingEntry := false, false, false
	allPassRows := true
	minStop := int64(3600000)
	for index, w := range v.Workers {
		if w.Index != index || !stressTime(w.StartedMilliseconds) || !stressTime(w.StoppedMilliseconds) ||
			w.StoppedMilliseconds < w.StartedMilliseconds || w.StoppedMilliseconds > v.WorkersJoinedMilliseconds ||
			w.PreparationIterations > 1 || w.PreparationChecksPassed > 12 ||
			w.Iterations < w.PreparationIterations || w.ChecksPassed < w.PreparationChecksPassed ||
			w.LoadIterations != w.Iterations-w.PreparationIterations || w.LoadChecksPassed != w.ChecksPassed-w.PreparationChecksPassed ||
			math.MaxUint64-iterations < w.Iterations || math.MaxUint64-checks < w.ChecksPassed ||
			!validStressCalculation(w) {
			return false
		}
		iterations += w.Iterations
		checks += w.ChecksPassed
		failures.Integer = failures.Integer || w.CalculationFailures.Integer
		failures.SHA256 = failures.SHA256 || w.CalculationFailures.SHA256
		failures.Scalar = failures.Scalar || w.CalculationFailures.Scalar
		if w.StoppedMilliseconds < minStop {
			minStop = w.StoppedMilliseconds
		}
		if w.ReadyMilliseconds != nil {
			ready++
			if !stressTime(*w.ReadyMilliseconds) || *w.ReadyMilliseconds < w.StartedMilliseconds ||
				*w.ReadyMilliseconds > w.StoppedMilliseconds || w.PreparationIterations != 1 || w.PreparationChecksPassed != 12 {
				return false
			}
		} else if w.LoadEnteredMilliseconds != nil || w.LoadIterations != 0 || w.LoadChecksPassed != 0 {
			return false
		}
		if !v.LoadStarted {
			if w.LoadEnteredMilliseconds != nil || w.LoadIterations != 0 || w.LoadChecksPassed != 0 || w.FailurePhase == "load" || w.ExitReason == "completed" {
				return false
			}
		} else {
			if v.LoadStartedMilliseconds == nil || w.ReadyMilliseconds == nil || *w.ReadyMilliseconds > *v.LoadStartedMilliseconds || w.FailurePhase == "readiness" {
				return false
			}
			if w.LoadEnteredMilliseconds == nil {
				if w.LoadIterations != 0 || w.LoadChecksPassed != 0 || w.FailurePhase != "none" {
					return false
				}
				earlyProven = true
				missingEntry = true
				allPassRows = false
			} else if !stressTime(*w.LoadEnteredMilliseconds) || *w.LoadEnteredMilliseconds < *v.LoadStartedMilliseconds || *w.LoadEnteredMilliseconds > w.StoppedMilliseconds {
				return false
			}
			target := *v.LoadStartedMilliseconds + 600000
			if w.StoppedMilliseconds < target {
				earlyProven = true
			}
			if w.LoadEnteredMilliseconds != nil && *w.LoadEnteredMilliseconds >= target {
				allPassRows = false
			}
			if w.LoadIterations == 0 || w.LoadChecksPassed < 12 {
				incompleteChecks = true
			}
			if w.ExitReason != "completed" || w.StoppedMilliseconds < target {
				allPassRows = false
			}
		}
		if !validStressWorkerReason(w, v) {
			return false
		}
	}
	if ready != v.WorkersReady || iterations != v.Iterations || checks != v.ChecksPassed || failures != v.CalculationFailures ||
		f.IncompleteLoadChecks != incompleteChecks || earlyProven && !f.EarlyWorkerExit {
		return false
	}
	if !v.LoadStarted {
		if v.LoadStartedMilliseconds != nil || v.LoadStoppedMilliseconds != nil || v.ObservedLoadMilliseconds != 0 ||
			f.NormalLoadComplete || f.EarlyWorkerExit || f.IncompleteLoadChecks {
			return false
		}
	} else {
		if v.WorkersReady != v.WorkersRequested || v.LoadStartedMilliseconds == nil || v.LoadStoppedMilliseconds == nil ||
			!stressTime(*v.LoadStartedMilliseconds) || !stressTime(*v.LoadStoppedMilliseconds) ||
			*v.LoadStoppedMilliseconds < *v.LoadStartedMilliseconds || *v.LoadStoppedMilliseconds != minStop ||
			*v.LoadStartedMilliseconds+600000 >= v.EffectiveDeadlineMilliseconds ||
			v.ObservedLoadMilliseconds < 0 || v.ObservedLoadMilliseconds > 600000 {
			return false
		}
		difference := *v.LoadStoppedMilliseconds - *v.LoadStartedMilliseconds
		upper := min(difference, int64(600000))
		lower := min(max(difference-1, int64(0)), int64(600000))
		if v.ObservedLoadMilliseconds < lower || v.ObservedLoadMilliseconds > upper ||
			f.NormalLoadComplete != (v.ObservedLoadMilliseconds == 600000) ||
			f.EarlyWorkerExit != (missingEntry || v.ObservedLoadMilliseconds < 600000) {
			return false
		}
	}
	pass := v.LoadStarted && f.NormalLoadComplete && allPassRows && !incompleteChecks &&
		v.FinishedMilliseconds < v.EffectiveDeadlineMilliseconds
	status, reason := stressPrimary(v, pass)
	return v.Status == status && v.StopReason == reason
}

func validStressCalculation(w quick.CPUStressWorker) bool {
	q, r := w.Iterations/256, w.Iterations%256
	if q > math.MaxUint64/12 {
		return false
	}
	base := q * 12
	if w.ChecksPassed < base || w.ChecksPassed-base > 12 || r != 0 && w.ChecksPassed-base != 12 || stressFailures(w.CalculationFailures) > 1 {
		return false
	}
	if w.PreparationIterations == 1 && w.PreparationChecksPassed != 12 {
		return false
	}
	failure := stressFailures(w.CalculationFailures) != 0
	if !failure {
		return w.FailurePhase == "none" && w.Mismatch == nil
	}
	if w.FailurePhase != "readiness" && w.FailurePhase != "load" || w.ExitReason != "calculation_mismatch" {
		return false
	}
	if w.FailurePhase == "readiness" && w.ReadyMilliseconds != nil || w.FailurePhase == "load" && w.LoadEnteredMilliseconds == nil {
		return false
	}
	if w.CalculationFailures.Scalar {
		ids := [...]string{"add1", "add2", "add3", "sub1", "sub2", "sub3", "mul1", "mul2", "mul3", "div1", "div2", "div3"}
		bits := [...]string{"400e000000000000", "c01e000000000000", "3eb8000000000000", "4011000000000000", "c00e000000000000", "4260000000000000", "400e000000000000", "bfe8000000000000", "4090000000000000", "4008000000000000", "c002000000000000", "3d70000000000000"}
		slot := w.ChecksPassed - base
		return r == 0 && slot < 12 && w.Mismatch != nil && w.Mismatch.Vector == ids[slot] && w.Mismatch.ExpectedBits == bits[slot] && scalarBits.MatchString(w.Mismatch.ActualBits) && w.Mismatch.ActualBits != w.Mismatch.ExpectedBits
	}
	return r == 0 && w.ChecksPassed == base && w.Mismatch == nil
}

func validStressWorkerReason(w quick.CPUStressWorker, v quick.CPUStressResult) bool {
	f := v.StopFacts
	if (w.ExitReason == "calculation_mismatch") != (stressFailures(w.CalculationFailures) != 0) {
		return false
	}
	switch w.ExitReason {
	case "completed":
		return v.LoadStarted && v.LoadStartedMilliseconds != nil && w.StoppedMilliseconds >= *v.LoadStartedMilliseconds+600000
	case "calculation_mismatch":
		return true
	case "observer_failure":
		return f.ObservationTimeout || f.ObserverPanic || f.InvalidObservation
	case "thermal_limit":
		return f.ThermalLimit
	case "coverage_loss":
		return f.CoverageLost
	case "parent_deadline":
		return f.ParentDeadline
	case "watchdog":
		return f.WatchdogDeadline
	case "cancelled":
		return f.ParentCancelled
	case "peer_stop":
		return v.Status != quick.StatusPass
	case "incomplete_work":
		return v.Status != quick.StatusPass
	default:
		return false
	}
}

func stressPrimary(v quick.CPUStressResult, full bool) (quick.Status, string) {
	f := v.StopFacts
	switch {
	case stressFailures(v.CalculationFailures) != 0:
		return quick.StatusFail, "calculation_mismatch"
	case f.ObservationTimeout || f.ObserverPanic || f.InvalidObservation:
		return quick.StatusError, "observer_failure"
	case f.ThermalLimit:
		return quick.StatusAborted, "thermal_limit"
	case f.InitialCoveragePartial || f.InitialCoverageUnavailable || f.CoverageLost:
		return quick.StatusInconclusive, "thermal_coverage"
	case f.ParentDeadline && (v.EffectiveDeadlineSource == "parent" || v.EffectiveDeadlineSource == "both"):
		return quick.StatusAborted, "parent_deadline"
	case f.WatchdogDeadline:
		return quick.StatusAborted, "watchdog"
	case f.ParentDeadline:
		return quick.StatusAborted, "parent_deadline"
	case f.ParentCancelled:
		return quick.StatusAborted, "parent_cancelled"
	case f.InsufficientBudget:
		return quick.StatusInconclusive, "insufficient_budget"
	case f.LaunchIncomplete || f.ReadinessIncomplete || f.EarlyWorkerExit || f.IncompleteLoadChecks || !full:
		return quick.StatusInconclusive, "incomplete_work"
	default:
		return quick.StatusPass, "completed"
	}
}

func validCPUStressSafety(v quick.CPUStressResult) bool {
	s, f := v.Safety, v.StopFacts
	if s.GlobalTimeoutMilliseconds != 660000 || s.ThermalLimitMilliCelsius != 90000 ||
		s.PeakMilliCelsius < 0 || s.PeakMilliCelsius > 250000 {
		return false
	}
	for _, count := range []int{s.ThermalObservations, s.ThermalSensors, s.ThermalSamples, s.ThermalRejectedReadings, s.ThermalUnavailableReadings, s.ThermalEnumerationErrors} {
		if count < 0 || int64(count) > 2147483647 {
			return false
		}
	}
	o, sensors, samples := int64(s.ThermalObservations), int64(s.ThermalSensors), int64(s.ThermalSamples)
	if samples < sensors || sensors == 0 && (samples != 0 || s.PeakMilliCelsius != 0) || sensors > 0 && o == 0 {
		return false
	}
	if sensors > 0 {
		minimum := samples / sensors
		if samples%sensors != 0 {
			minimum++
		}
		if minimum > o {
			return false
		}
	}
	if o == 0 && (sensors != 0 || samples != 0 || s.PeakMilliCelsius != 0 || s.ThermalRejectedReadings != 0 || s.ThermalUnavailableReadings != 0 || s.ThermalEnumerationErrors != 0 || s.ThermalCoverage != "unavailable") {
		return false
	}
	switch s.ThermalCoverage {
	case "available":
		if sensors == 0 || s.ThermalRejectedReadings != 0 || s.ThermalUnavailableReadings != 0 || s.ThermalEnumerationErrors != 0 || samples-sensors < o-1 {
			return false
		}
	case "unavailable":
		if sensors != 0 || s.ThermalRejectedReadings != 0 || s.ThermalEnumerationErrors != 0 {
			return false
		}
	case "partial":
		if s.ThermalRejectedReadings == 0 && s.ThermalUnavailableReadings == 0 && s.ThermalEnumerationErrors == 0 {
			if sensors == 0 || o < 2 || samples/sensors > o-1 || samples/sensors == o-1 && samples%sensors != 0 {
				return false
			}
		} else if sensors == 0 && s.ThermalRejectedReadings == 0 && s.ThermalEnumerationErrors == 0 {
			return false
		}
	default:
		return false
	}
	if f.ThermalLimit != (samples > 0 && s.PeakMilliCelsius >= 90000) ||
		f.InitialCoveragePartial && (o != 1 || s.ThermalCoverage != "partial") ||
		f.InitialCoverageUnavailable && (o != 1 || s.ThermalCoverage != "unavailable") ||
		f.CoverageLost && (o < 2 || s.ThermalCoverage != "partial") ||
		v.WorkersLaunched > 0 && (o == 0 || f.InitialCoveragePartial || f.InitialCoverageUnavailable) {
		return false
	}
	if o > 0 && s.ThermalCoverage == "unavailable" && !f.InitialCoverageUnavailable ||
		s.ThermalCoverage == "partial" && !f.InitialCoveragePartial && !f.CoverageLost {
		return false
	}
	hard := f.ObservationTimeout || f.ObserverPanic || f.InvalidObservation
	coverage := f.CoverageLost || f.InitialCoveragePartial || f.InitialCoverageUnavailable
	switch s.MonitorStatus {
	case "not_started":
		return o == 0 && s.MonitorFailure == "" && v.WorkersLaunched == 0 && v.Iterations == 0 && v.ChecksPassed == 0 && v.StateAggregate == 0 && !v.LoadStarted && !hard && !coverage && !f.ThermalLimit &&
			(f.InsufficientBudget || f.ParentCancelled || f.ParentDeadline || f.WatchdogDeadline)
	case "completed":
		return o > 0 && s.MonitorFailure == "" && s.ThermalCoverage == "available" && !hard && !coverage && !f.ThermalLimit
	case "failed":
		switch s.MonitorFailure {
		case "thermal_limit":
			return f.ThermalLimit
		case "coverage_loss":
			return coverage
		case "observation_timeout":
			return f.ObservationTimeout
		case "observer_panic":
			return f.ObserverPanic
		case "invalid_observation":
			return f.InvalidObservation
		default:
			return false
		}
	default:
		return false
	}
}
