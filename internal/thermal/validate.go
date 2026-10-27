package thermal

import (
	"errors"
	"math"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

// ValidateResult checks typed evidence independently of its runtime producer.
// requireSignable permits telemetry limitations and heat, never a failed owner.
func ValidateResult(r Result, requireSignable bool) error {
	invalid := func() error { return errors.New("invalid run thermal evidence") }
	if r.Schema != Schema || r.LimitMilliCelsius != LimitMilliCelsius ||
		r.IntervalMilliseconds != Interval.Milliseconds() || r.ObservationTimeoutMilliseconds != ObservationTimeout.Milliseconds() ||
		r.MaximumAllowedGapMilliseconds != MaximumGap.Milliseconds() || r.StopTimeoutMilliseconds != StopTimeout.Milliseconds() ||
		r.ElapsedMilliseconds < 0 || r.ElapsedMilliseconds > time.Hour.Milliseconds() ||
		r.CompletedObservations > r.AttemptedObservations || r.PeriodicCompletions > r.PeriodicAttempts ||
		r.AttemptedObservations > uint64(time.Hour/Interval)+2 ||
		r.FirstCompletedMilliseconds < 0 || r.LastCompletedMilliseconds < r.FirstCompletedMilliseconds || r.MaximumGapMilliseconds < 0 ||
		r.JoinedMilliseconds < 0 || r.JoinedMilliseconds > time.Hour.Milliseconds() || !r.Joined && r.JoinedMilliseconds != 0 {
		return invalid()
	}
	switch r.Status {
	case "completed":
		if r.FirstFailure != nil || !r.CleanupConfirmed || !r.Joined {
			return invalid()
		}
	case "failed":
		if r.FirstFailure == nil || !r.CleanupConfirmed || !r.Joined {
			return invalid()
		}
	case "cleanup_unconfirmed":
		if r.FirstFailure == nil || r.CleanupConfirmed {
			return invalid()
		}
	default:
		return invalid()
	}
	if requireSignable && r.Status != "completed" {
		return invalid()
	}
	upper := r.ElapsedMilliseconds
	if r.FirstFailure != nil && r.FirstFailure.Reason == "clock_error" {
		upper = time.Hour.Milliseconds()
	}
	if r.LastCompletedMilliseconds > upper || r.MaximumGapMilliseconds > upper {
		return invalid()
	}
	attempts, completions := r.PeriodicAttempts, r.PeriodicCompletions
	for _, phase := range []PhaseRecord{r.Initial, r.Final} {
		if phase.RequestedMilliseconds < 0 || phase.RequestedMilliseconds > upper ||
			phase.StartedMilliseconds < 0 || phase.StartedMilliseconds > upper ||
			phase.CompletedMilliseconds < 0 || phase.CompletedMilliseconds > upper {
			return invalid()
		}
		if !phase.Attempted {
			if phase.Completed || phase.StartedMilliseconds != 0 || phase.CompletedMilliseconds != 0 || phase.Observation != nil {
				return invalid()
			}
			continue
		}
		if attempts == math.MaxUint64 || phase.StartedMilliseconds < phase.RequestedMilliseconds {
			return invalid()
		}
		attempts++
		if !phase.Completed {
			if phase.CompletedMilliseconds != 0 || phase.Observation != nil {
				return invalid()
			}
			continue
		}
		if completions == math.MaxUint64 || phase.CompletedMilliseconds < phase.StartedMilliseconds ||
			phase.Observation == nil || !quick.ValidThermalSnapshot(*phase.Observation) {
			return invalid()
		}
		completions++
	}
	if attempts != r.AttemptedObservations || completions != r.CompletedObservations {
		return invalid()
	}
	if r.PeriodicAttempts > 0 && (!r.Initial.Attempted ||
		upper < r.Initial.StartedMilliseconds || r.PeriodicAttempts > uint64((upper-r.Initial.StartedMilliseconds)/Interval.Milliseconds())) {
		return invalid()
	}
	if r.CompletedObservations == 0 {
		if r.FirstCompletedMilliseconds != 0 || r.LastCompletedMilliseconds != 0 || r.LastObservation != nil {
			return invalid()
		}
	} else if !r.Initial.Completed || r.FirstCompletedMilliseconds != r.Initial.CompletedMilliseconds ||
		r.LastObservation == nil || !quick.ValidThermalSnapshot(*r.LastObservation) {
		return invalid()
	}
	if r.Final.Completed && (!r.Initial.Completed || r.Final.StartedMilliseconds < r.Initial.CompletedMilliseconds ||
		r.Final.CompletedMilliseconds > r.LastCompletedMilliseconds) {
		return invalid()
	}
	if err := validateAggregate(r.Aggregate); err != nil || r.Aggregate.AcceptedObservations > r.CompletedObservations {
		return invalid()
	}
	if failure := r.FirstFailure; failure != nil {
		if failure.ElapsedMilliseconds < 0 || failure.ElapsedMilliseconds > upper || !validPhase(failure.Phase, true) {
			return invalid()
		}
		switch failure.Reason {
		case "observation_timeout", "observer_panic", "clock_error", "sampling_gap", "cleanup_unconfirmed", "invalid_observation",
			"aggregate_overflow", "required_phase_missing", "final_request_cancelled", "final_request_timeout", "final_request_duplicate", "final_request_invalid":
		default:
			return invalid()
		}
		if failure.Observation != nil && !quick.ValidThermalSnapshot(*failure.Observation) {
			return invalid()
		}
		if failure.Reason == "invalid_observation" && (failure.Observation == nil ||
			*failure.Observation != (quick.ThermalSnapshot{Coverage: quick.ThermalUnavailable})) {
			return invalid()
		}
	}
	if finding := r.FirstFinding; finding != nil {
		if finding.Reason != "thermal_limit" || !validPhase(finding.Phase, false) ||
			finding.ElapsedMilliseconds < 0 || finding.ElapsedMilliseconds > upper ||
			!quick.ValidThermalSnapshot(finding.Observation) || finding.Observation.Sensors == 0 || finding.Observation.PeakMilliCelsius < LimitMilliCelsius {
			return invalid()
		}
		if finding.Phase == "periodic" && r.PeriodicAttempts == 0 {
			return invalid()
		}
		for name, phase := range map[string]PhaseRecord{"initial": r.Initial, "final": r.Final} {
			if finding.Phase == name && (!phase.Attempted || finding.ElapsedMilliseconds < phase.StartedMilliseconds ||
				phase.Completed && (finding.ElapsedMilliseconds != phase.CompletedMilliseconds || finding.Observation != *phase.Observation)) {
				return invalid()
			}
		}
	}
	if r.Status != "completed" {
		return nil
	}
	if !r.Initial.Completed || !r.Final.Completed || r.AttemptedObservations != r.CompletedObservations ||
		r.Aggregate.AcceptedObservations != r.CompletedObservations || r.JoinedMilliseconds > r.ElapsedMilliseconds ||
		r.JoinedMilliseconds < r.LastCompletedMilliseconds || r.MaximumGapMilliseconds > MaximumGap.Milliseconds() ||
		r.MaximumGapMilliseconds < r.FirstCompletedMilliseconds || r.MaximumGapMilliseconds < r.ElapsedMilliseconds-r.LastCompletedMilliseconds ||
		r.Initial.CompletedMilliseconds-r.Initial.StartedMilliseconds > ObservationTimeout.Milliseconds() ||
		r.Final.CompletedMilliseconds-r.Final.StartedMilliseconds > ObservationTimeout.Milliseconds() ||
		r.Final.CompletedMilliseconds-r.Final.RequestedMilliseconds > FinalTimeout.Milliseconds() {
		return invalid()
	}
	// Cleanup elapsed cannot manufacture time in which a periodic read completed.
	if r.PeriodicCompletions > uint64((r.LastCompletedMilliseconds-r.Initial.StartedMilliseconds)/Interval.Milliseconds()) {
		return invalid()
	}
	if r.CompletedObservations > 1 {
		span := uint64(r.LastCompletedMilliseconds - r.FirstCompletedMilliseconds)
		gaps := r.CompletedObservations - 1
		if span/gaps > uint64(r.MaximumGapMilliseconds) || span/gaps == uint64(r.MaximumGapMilliseconds) && span%gaps != 0 {
			return invalid()
		}
		if r.MaximumGapMilliseconds > max(r.FirstCompletedMilliseconds,
			r.LastCompletedMilliseconds-r.FirstCompletedMilliseconds, r.ElapsedMilliseconds-r.LastCompletedMilliseconds) {
			return invalid()
		}
	}
	hot := r.Aggregate.ValidReadings > 0 && r.Aggregate.PeakMilliCelsius >= LimitMilliCelsius
	if hot != (r.FirstFinding != nil) {
		return invalid()
	}
	if r.FirstFinding != nil {
		finding := r.FirstFinding
		if finding.ElapsedMilliseconds < r.FirstCompletedMilliseconds || finding.ElapsedMilliseconds > r.LastCompletedMilliseconds ||
			finding.Phase == "periodic" && finding.ElapsedMilliseconds < r.Initial.StartedMilliseconds+Interval.Milliseconds() ||
			r.Initial.Observation.Sensors > 0 && r.Initial.Observation.PeakMilliCelsius >= LimitMilliCelsius && finding.Phase != "initial" {
			return invalid()
		}
	}
	// Initial and final are distinct accepted reads even at the same rounded
	// timestamp. Their explicit observations cannot exceed the aggregate totals.
	known := Aggregate{Coverage: quick.ThermalUnavailable}
	for _, observation := range []*quick.ThermalSnapshot{r.Initial.Observation, r.Final.Observation} {
		known.AcceptedObservations++
		switch observation.Coverage {
		case quick.ThermalAvailable:
			known.AvailableObservations++
		case quick.ThermalPartial:
			known.PartialObservations++
		case quick.ThermalUnavailable:
			known.UnavailableObservations++
		}
		known.MaximumSensors = max(known.MaximumSensors, uint64(observation.Sensors))
		known.ValidReadings += uint64(observation.Sensors)
		known.PeakMilliCelsius = max(known.PeakMilliCelsius, observation.PeakMilliCelsius)
		known.RejectedReadings += uint64(observation.RejectedReadings)
		known.UnavailableReadings += uint64(observation.UnavailableReadings)
		known.EnumerationErrors += uint64(observation.EnumerationErrors)
	}
	if known.AvailableObservations > r.Aggregate.AvailableObservations || known.PartialObservations > r.Aggregate.PartialObservations ||
		known.UnavailableObservations > r.Aggregate.UnavailableObservations || known.ValidReadings > r.Aggregate.ValidReadings ||
		known.RejectedReadings > r.Aggregate.RejectedReadings || known.UnavailableReadings > r.Aggregate.UnavailableReadings ||
		known.EnumerationErrors > r.Aggregate.EnumerationErrors {
		return invalid()
	}
	if r.CompletedObservations == 2 {
		known.Coverage = r.Aggregate.Coverage // Class-count validation above fixes this value.
		if known != r.Aggregate || *r.LastObservation != *r.Final.Observation {
			return invalid()
		}
	}
	for _, observation := range []*quick.ThermalSnapshot{r.Initial.Observation, r.Final.Observation, r.LastObservation} {
		if uint64(observation.Sensors) > r.Aggregate.MaximumSensors || observation.PeakMilliCelsius > r.Aggregate.PeakMilliCelsius ||
			uint64(observation.RejectedReadings) > r.Aggregate.RejectedReadings || uint64(observation.UnavailableReadings) > r.Aggregate.UnavailableReadings ||
			uint64(observation.EnumerationErrors) > r.Aggregate.EnumerationErrors {
			return invalid()
		}
	}
	if r.FirstFinding != nil && (r.FirstFinding.Observation.PeakMilliCelsius > r.Aggregate.PeakMilliCelsius ||
		uint64(r.FirstFinding.Observation.Sensors) > r.Aggregate.MaximumSensors) {
		return invalid()
	}
	return nil
}

func validPhase(value string, none bool) bool {
	return value == "initial" || value == "periodic" || value == "final" || none && value == "none"
}

func validateAggregate(a Aggregate) error {
	invalid := errors.New("invalid thermal aggregate")
	if a.AvailableObservations > a.AcceptedObservations || a.PartialObservations > a.AcceptedObservations-a.AvailableObservations ||
		a.UnavailableObservations != a.AcceptedObservations-a.AvailableObservations-a.PartialObservations ||
		a.PeakMilliCelsius < 0 || a.PeakMilliCelsius > 250000 || a.MaximumSensors > math.MaxInt64 ||
		a.MaximumSensors > a.ValidReadings || (a.MaximumSensors == 0) != (a.ValidReadings == 0) ||
		a.ValidReadings == 0 && a.PeakMilliCelsius != 0 {
		return invalid
	}
	if a.AcceptedObservations == 0 {
		if a != (Aggregate{Coverage: quick.ThermalUnavailable}) {
			return invalid
		}
		return nil
	}
	withSensors := a.AcceptedObservations - a.UnavailableObservations
	if a.ValidReadings < a.AvailableObservations || a.ValidReadings > 0 &&
		(withSensors == 0 || a.ValidReadings/withSensors > a.MaximumSensors ||
			a.ValidReadings/withSensors == a.MaximumSensors && a.ValidReadings%withSensors != 0) {
		return invalid
	}
	want := quick.ThermalPartial
	if a.AvailableObservations == a.AcceptedObservations {
		want = quick.ThermalAvailable
	}
	if a.UnavailableObservations == a.AcceptedObservations {
		want = quick.ThermalUnavailable
	}
	if a.Coverage != want || a.PartialObservations == 0 && (a.RejectedReadings != 0 || a.EnumerationErrors != 0) {
		return invalid
	}
	if want == quick.ThermalAvailable && (a.MaximumSensors == 0 || a.UnavailableReadings != 0) {
		return invalid
	}
	if want == quick.ThermalUnavailable && (a.ValidReadings != 0 || a.RejectedReadings != 0 || a.EnumerationErrors != 0) {
		return invalid
	}
	degraded := a.RejectedReadings
	for _, count := range []uint64{a.UnavailableReadings, a.EnumerationErrors} {
		if count > math.MaxUint64-degraded {
			degraded = math.MaxUint64
		} else {
			degraded += count
		}
	}
	if a.PartialObservations > degraded {
		return invalid
	}
	// Unavailable readings alone make a scan partial only when that same scan
	// also has a valid sensor. Error-free partial scans each need such a reading.
	errors := a.RejectedReadings
	if a.EnumerationErrors > math.MaxUint64-errors {
		errors = math.MaxUint64
	} else {
		errors += a.EnumerationErrors
	}
	needsSensors := a.PartialObservations - min(a.PartialObservations, errors)
	if a.UnavailableReadings < needsSensors || a.ValidReadings < a.AvailableObservations ||
		a.ValidReadings-a.AvailableObservations < needsSensors {
		return invalid
	}
	return nil
}
