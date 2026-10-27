package thermal

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/periodic"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

func available(peak int) quick.ThermalSnapshot {
	return quick.ThermalSnapshot{Coverage: quick.ThermalAvailable, Sensors: 1, PeakMilliCelsius: peak}
}

func TestAggregateCoverageAndHeatRemainIndependent(t *testing.T) {
	samples := []quick.ThermalSnapshot{available(100000), {Coverage: quick.ThermalUnavailable, UnavailableReadings: 2},
		{Coverage: quick.ThermalPartial, RejectedReadings: 3, EnumerationErrors: 1}}
	var aggregate Aggregate
	for _, sample := range samples {
		result := reduce(aggregate, periodic.Observation[quick.ThermalSnapshot]{EligibleForAggregation: true, Assessment: assess(sample)})
		if result.FailureReason != "" {
			t.Fatal(result.FailureReason)
		}
		aggregate = result.Value
	}
	want := Aggregate{Coverage: quick.ThermalPartial, AcceptedObservations: 3, AvailableObservations: 1,
		PartialObservations: 1, UnavailableObservations: 1, MaximumSensors: 1, ValidReadings: 1,
		PeakMilliCelsius: 100000, RejectedReadings: 3, UnavailableReadings: 2, EnumerationErrors: 1}
	if aggregate != want {
		t.Fatalf("aggregate: %+v", aggregate)
	}
	if err := validateAggregate(aggregate); err != nil {
		t.Fatal(err)
	}
	if assess(samples[0]).FindingReason != "thermal_limit" || assess(samples[1]).FindingReason != "" {
		t.Fatal("coverage mislabeled as heat")
	}
	result := reduce(aggregate, periodic.Observation[quick.ThermalSnapshot]{Assessment: assess(available(250000))})
	if result.Value != aggregate || result.FailureReason != "" {
		t.Fatal("ineligible sample changed aggregate")
	}
}

func TestAssessmentRejectsInvalidCountsWithoutRetainingTextOrHeat(t *testing.T) {
	for _, sample := range []quick.ThermalSnapshot{
		{Coverage: "untrusted location", Sensors: 1, PeakMilliCelsius: 100000},
		{Coverage: quick.ThermalAvailable, Sensors: -1}, {Coverage: quick.ThermalAvailable, Sensors: 1, PeakMilliCelsius: 250001},
		{Coverage: quick.ThermalUnavailable, PeakMilliCelsius: 100000},
		{Coverage: quick.ThermalPartial}, {Coverage: quick.ThermalAvailable, Sensors: 1, EnumerationErrors: 1},
	} {
		result := assess(sample)
		if result.Value != (quick.ThermalSnapshot{Coverage: quick.ThermalUnavailable}) || result.FailureReason != "invalid_observation" || result.FindingReason != "" {
			t.Fatalf("invalid assessment: %+v", result)
		}
	}
	for _, peak := range []int{0, 89999, 90000, 185000, 250000} {
		result := assess(available(peak))
		if result.FailureReason != "" || (result.FindingReason == "thermal_limit") != (peak >= LimitMilliCelsius) {
			t.Fatalf("boundary %d: %+v", peak, result)
		}
	}
}

func TestAggregateOverflowIsAtomic(t *testing.T) {
	for _, set := range []func(*Aggregate){
		func(a *Aggregate) { a.AcceptedObservations = math.MaxUint64 },
		func(a *Aggregate) { a.PartialObservations = math.MaxUint64 },
		func(a *Aggregate) { a.ValidReadings = math.MaxUint64 },
		func(a *Aggregate) { a.RejectedReadings = math.MaxUint64 },
		func(a *Aggregate) { a.UnavailableReadings = math.MaxUint64 },
		func(a *Aggregate) { a.EnumerationErrors = math.MaxUint64 },
	} {
		prior := Aggregate{Coverage: quick.ThermalPartial}
		set(&prior)
		sample := quick.ThermalSnapshot{Coverage: quick.ThermalPartial, Sensors: 1, RejectedReadings: 1, UnavailableReadings: 1, EnumerationErrors: 1}
		result := reduce(prior, periodic.Observation[quick.ThermalSnapshot]{EligibleForAggregation: true, Assessment: assess(sample)})
		if result.FailureReason != "aggregate_overflow" || result.Value != prior {
			t.Fatal("overflow published partial arithmetic")
		}
	}
}

func TestPartialAggregateRequiresSensorsWhenUnavailableIsOnlyDegradation(t *testing.T) {
	for _, value := range []Aggregate{
		{Coverage: quick.ThermalPartial, AcceptedObservations: 1, PartialObservations: 1, UnavailableReadings: 1},
		{Coverage: quick.ThermalPartial, AcceptedObservations: 2, AvailableObservations: 1, PartialObservations: 1,
			MaximumSensors: 1, ValidReadings: 1, UnavailableReadings: 1},
	} {
		if validateAggregate(value) == nil {
			t.Fatal("sensorless scan mislabeled partial by disabled readings alone")
		}
	}
	valid := Aggregate{Coverage: quick.ThermalPartial, AcceptedObservations: 1, PartialObservations: 1,
		MaximumSensors: 1, ValidReadings: 1, UnavailableReadings: 1}
	if err := validateAggregate(valid); err != nil {
		t.Fatal(err)
	}
}

func completedFixture() Result {
	initial, final := available(85000), available(40000)
	return Result{Schema: Schema, Status: "completed", LimitMilliCelsius: 90000,
		IntervalMilliseconds: 1000, ObservationTimeoutMilliseconds: 1000, MaximumAllowedGapMilliseconds: 2500, StopTimeoutMilliseconds: 2000,
		AttemptedObservations: 2, CompletedObservations: 2, ElapsedMilliseconds: 60, FirstCompletedMilliseconds: 0, LastCompletedMilliseconds: 50, MaximumGapMilliseconds: 50,
		Initial:          PhaseRecord{Attempted: true, Completed: true, Observation: &initial},
		Final:            PhaseRecord{RequestedMilliseconds: 50, Attempted: true, Completed: true, StartedMilliseconds: 50, CompletedMilliseconds: 50, Observation: &final},
		Aggregate:        Aggregate{Coverage: quick.ThermalAvailable, AcceptedObservations: 2, AvailableObservations: 2, MaximumSensors: 1, ValidReadings: 2, PeakMilliCelsius: 85000},
		CleanupConfirmed: true, Joined: true, JoinedMilliseconds: 55, LastObservation: &final}
}

func TestTypedValidationRejectsIndependentContradictions(t *testing.T) {
	if err := ValidateResult(completedFixture(), true); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*Result)
	}{
		{"schema", func(r *Result) { r.Schema = "old" }},
		{"limit", func(r *Result) { r.LimitMilliCelsius++ }},
		{"missing initial", func(r *Result) { r.Initial = PhaseRecord{} }},
		{"missing final", func(r *Result) { r.Final = PhaseRecord{} }},
		{"reused pre-request read", func(r *Result) { r.Final.RequestedMilliseconds = 51 }},
		{"late phase", func(r *Result) {
			r.Final.CompletedMilliseconds = 1051
			r.LastCompletedMilliseconds = 1051
			r.ElapsedMilliseconds = 1051
			r.JoinedMilliseconds = 1051
			r.MaximumGapMilliseconds = 1051
		}},
		{"invented periodic tick", func(r *Result) { r.PeriodicAttempts = 1; r.AttemptedObservations++ }},
		{"cleanup tail cannot backdate periodic completion", func(r *Result) {
			r.PeriodicAttempts, r.PeriodicCompletions = 1, 1
			r.AttemptedObservations, r.CompletedObservations = 3, 3
			r.Aggregate.AcceptedObservations, r.Aggregate.AvailableObservations, r.Aggregate.ValidReadings = 3, 3, 3
			r.Final.RequestedMilliseconds, r.Final.StartedMilliseconds, r.Final.CompletedMilliseconds = 0, 0, 0
			r.LastCompletedMilliseconds, r.ElapsedMilliseconds, r.JoinedMilliseconds, r.MaximumGapMilliseconds = 0, 1000, 1000, 1000
		}},
		{"missing join", func(r *Result) { r.Joined = false; r.JoinedMilliseconds = 0 }},
		{"join after boundary", func(r *Result) { r.JoinedMilliseconds = 61 }},
		{"join before return", func(r *Result) { r.JoinedMilliseconds = 49 }},
		{"gap too small", func(r *Result) { r.MaximumGapMilliseconds = 49 }},
		{"impossible counts", func(r *Result) { r.CompletedObservations = math.MaxUint64 }},
		{"missing acceptance", func(r *Result) { r.Aggregate.AcceptedObservations-- }},
		{"misclassified coverage", func(r *Result) { r.Aggregate.Coverage = quick.ThermalUnavailable }},
		{"underreported readings", func(r *Result) { r.Aggregate.ValidReadings-- }},
		{"invented readings", func(r *Result) { r.Aggregate.ValidReadings++ }},
		{"underreported peak", func(r *Result) { r.Aggregate.PeakMilliCelsius-- }},
		{"invented peak", func(r *Result) { r.Aggregate.PeakMilliCelsius++ }},
		{"missing heat latch", func(r *Result) { r.Aggregate.PeakMilliCelsius = 90000; r.Initial.Observation.PeakMilliCelsius = 90000 }},
		{"fake cold heat latch", func(r *Result) {
			r.FirstFinding = &FindingRecord{Reason: "thermal_limit", Phase: "initial", Observation: *r.Initial.Observation}
		}},
		{"terminal failure hidden by completed status", func(r *Result) { r.FirstFailure = &FailureRecord{Reason: "observation_timeout", Phase: "initial"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := CloneResult(completedFixture())
			test.change(&r)
			if ValidateResult(r, true) == nil {
				t.Fatal("contradictory summary accepted")
			}
		})
	}
}

func TestUnavailableAndHotEvidenceAreSignableCoverageNotPass(t *testing.T) {
	for _, hot := range []bool{false, true} {
		r := completedFixture()
		if hot {
			r.Initial.Observation.PeakMilliCelsius = 250000
			r.Aggregate.PeakMilliCelsius = 250000
			r.FirstFinding = &FindingRecord{Reason: "thermal_limit", Phase: "initial", Observation: *r.Initial.Observation}
		} else {
			empty := quick.ThermalSnapshot{Coverage: quick.ThermalUnavailable}
			r.Initial.Observation, r.Final.Observation, r.LastObservation = &empty, &empty, &empty
			r.Aggregate = Aggregate{Coverage: quick.ThermalUnavailable, AcceptedObservations: 2, UnavailableObservations: 2}
		}
		if err := ValidateResult(r, true); err != nil {
			t.Fatal(err)
		}
		copy := CloneResult(r)
		copy.Initial.Observation.Coverage = "caller mutation"
		if err := ValidateResult(r, true); err != nil {
			t.Fatal("clone changed original")
		}
		data, _ := json.Marshal(r)
		if strings.Contains(string(data), `"status":"pass"`) {
			t.Fatal("telemetry reported as a health pass")
		}
	}
}

func TestLateHeatDiagnosticDoesNotBecomeAcceptedCoverage(t *testing.T) {
	sample := available(250000)
	r := Result{Schema: Schema, Status: "failed", LimitMilliCelsius: 90000, IntervalMilliseconds: 1000,
		ObservationTimeoutMilliseconds: 1000, MaximumAllowedGapMilliseconds: 2500, StopTimeoutMilliseconds: 2000,
		AttemptedObservations: 1, CompletedObservations: 1, ElapsedMilliseconds: 1000, FirstCompletedMilliseconds: 1000,
		LastCompletedMilliseconds: 1000, MaximumGapMilliseconds: 1000,
		Initial:   PhaseRecord{Attempted: true, Completed: true, CompletedMilliseconds: 1000, Observation: &sample},
		Aggregate: Aggregate{Coverage: quick.ThermalUnavailable}, CleanupConfirmed: true, Joined: true, JoinedMilliseconds: 1000,
		FirstFailure: &FailureRecord{Reason: "observation_timeout", Phase: "initial", ElapsedMilliseconds: 1000},
		FirstFinding: &FindingRecord{Reason: "thermal_limit", Phase: "initial", ElapsedMilliseconds: 1000, Observation: sample}, LastObservation: &sample}
	if err := ValidateResult(r, false); err != nil {
		t.Fatal(err)
	}
	if ValidateResult(r, true) == nil {
		t.Fatal("late hot diagnostic authorized signing")
	}
}

func TestActualOwnerAdapterInitialAndFinalHeatWithUnavailableTelemetry(t *testing.T) {
	for _, heatAt := range []int32{0, 1, 2} {
		var calls, findings atomic.Int32
		monitor, err := start(Options{Started: time.Now(), CancelWork: func() { findings.Add(1) }}, func(context.Context) quick.ThermalSnapshot {
			if calls.Add(1) == heatAt {
				return available(185000)
			}
			return quick.ThermalSnapshot{Coverage: quick.ThermalUnavailable}
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := monitor.Ready(ctx); err != nil {
			t.Fatal(err)
		}
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), time.Second)
		if err := monitor.ObserveFinal(ctx); err != nil {
			t.Fatal(err)
		}
		cancel()
		result := monitor.Stop(context.Background())
		if err := ValidateResult(result, true); err != nil {
			t.Fatalf("real adapter result: %+v: %v", result, err)
		}
		if calls.Load() != 2 || result.PeriodicAttempts != 0 || monitor.Failed() || (findings.Load() == 1) != (heatAt != 0) {
			t.Fatalf("adapter counts: %+v", result)
		}
		if again := monitor.Stop(nil); !reflect.DeepEqual(result, again) {
			t.Fatal("stop changed wire result")
		}
	}
}
