package thermal

import (
	"math"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/periodic"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

func assess(snapshot quick.ThermalSnapshot) periodic.Assessment[quick.ThermalSnapshot] {
	if !quick.ValidThermalSnapshot(snapshot) {
		return periodic.Assessment[quick.ThermalSnapshot]{Value: quick.ThermalSnapshot{Coverage: quick.ThermalUnavailable}, FailureReason: "invalid_observation"}
	}
	result := periodic.Assessment[quick.ThermalSnapshot]{Value: snapshot}
	if snapshot.Sensors > 0 && snapshot.PeakMilliCelsius >= LimitMilliCelsius {
		result.FindingReason = "thermal_limit"
	}
	return result
}

func reduce(prior Aggregate, observation periodic.Observation[quick.ThermalSnapshot]) periodic.Reduction[Aggregate] {
	result := periodic.Reduction[Aggregate]{Value: prior}
	if !observation.EligibleForAggregation {
		return result
	}
	sample := observation.Assessment.Value
	if !quick.ValidThermalSnapshot(sample) || observation.Assessment.FailureReason != "" {
		result.FailureReason = "invalid_observation"
		return result
	}
	next := prior
	add := func(target *uint64, count uint64) bool {
		if count > math.MaxUint64-*target {
			return false
		}
		*target += count
		return true
	}
	class := &next.UnavailableObservations
	if sample.Coverage == quick.ThermalAvailable {
		class = &next.AvailableObservations
	}
	if sample.Coverage == quick.ThermalPartial {
		class = &next.PartialObservations
	}
	if !add(&next.AcceptedObservations, 1) || !add(class, 1) ||
		!add(&next.ValidReadings, uint64(sample.Sensors)) || !add(&next.RejectedReadings, uint64(sample.RejectedReadings)) ||
		!add(&next.UnavailableReadings, uint64(sample.UnavailableReadings)) || !add(&next.EnumerationErrors, uint64(sample.EnumerationErrors)) {
		result.FailureReason = "aggregate_overflow"
		return result
	}
	next.MaximumSensors = max(next.MaximumSensors, uint64(sample.Sensors))
	next.PeakMilliCelsius = max(next.PeakMilliCelsius, sample.PeakMilliCelsius)
	next.Coverage = quick.ThermalPartial
	if next.AvailableObservations == next.AcceptedObservations {
		next.Coverage = quick.ThermalAvailable
	}
	if next.UnavailableObservations == next.AcceptedObservations {
		next.Coverage = quick.ThermalUnavailable
	}
	result.Value = next
	return result
}
