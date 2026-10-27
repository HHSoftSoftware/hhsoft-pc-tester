// Package thermal discloses run-wide sampled telemetry and a latched heat
// finding independently of monitoring integrity and physical sensor coverage.
package thermal

import (
	"context"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/periodic"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

const Schema = "hhsoft.pc-tester.run-thermal/v1"

const (
	LimitMilliCelsius  = 90000
	Interval           = time.Second
	ObservationTimeout = time.Second
	MaximumGap         = 2500 * time.Millisecond
	StopTimeout        = 2 * time.Second
	FinalTimeout       = 2 * time.Second
)

type Options struct {
	Started          time.Time
	Root             string
	CancelWork, Fail context.CancelFunc
}

type Aggregate struct {
	Coverage                string `json:"coverage"`
	AcceptedObservations    uint64 `json:"accepted_observations"`
	AvailableObservations   uint64 `json:"available_observations"`
	PartialObservations     uint64 `json:"partial_observations"`
	UnavailableObservations uint64 `json:"unavailable_observations"`
	MaximumSensors          uint64 `json:"maximum_sensors"`
	ValidReadings           uint64 `json:"valid_readings"`
	PeakMilliCelsius        int    `json:"peak_millicelsius"`
	RejectedReadings        uint64 `json:"rejected_readings"`
	UnavailableReadings     uint64 `json:"unavailable_readings"`
	EnumerationErrors       uint64 `json:"enumeration_errors"`
}

type PhaseRecord struct {
	RequestedMilliseconds int64                  `json:"requested_ms"`
	Attempted             bool                   `json:"attempted"`
	Completed             bool                   `json:"completed"`
	StartedMilliseconds   int64                  `json:"started_ms"`
	CompletedMilliseconds int64                  `json:"completed_ms"`
	Observation           *quick.ThermalSnapshot `json:"observation,omitempty"`
}

type FindingRecord struct {
	Reason              string                `json:"reason"`
	Phase               string                `json:"phase"`
	ElapsedMilliseconds int64                 `json:"elapsed_ms"`
	Observation         quick.ThermalSnapshot `json:"observation"`
}

type FailureRecord struct {
	Reason              string                 `json:"reason"`
	Phase               string                 `json:"phase"`
	ElapsedMilliseconds int64                  `json:"elapsed_ms"`
	Observation         *quick.ThermalSnapshot `json:"observation,omitempty"`
}

type Result struct {
	Schema                         string                 `json:"schema"`
	Status                         string                 `json:"status"`
	LimitMilliCelsius              int                    `json:"limit_millicelsius"`
	IntervalMilliseconds           int64                  `json:"interval_ms"`
	ObservationTimeoutMilliseconds int64                  `json:"observation_timeout_ms"`
	MaximumAllowedGapMilliseconds  int64                  `json:"maximum_allowed_gap_ms"`
	StopTimeoutMilliseconds        int64                  `json:"stop_timeout_ms"`
	AttemptedObservations          uint64                 `json:"attempted_observations"`
	CompletedObservations          uint64                 `json:"completed_observations"`
	PeriodicAttempts               uint64                 `json:"periodic_attempts"`
	PeriodicCompletions            uint64                 `json:"periodic_completions"`
	ElapsedMilliseconds            int64                  `json:"elapsed_ms"`
	FirstCompletedMilliseconds     int64                  `json:"first_completed_ms"`
	LastCompletedMilliseconds      int64                  `json:"last_completed_ms"`
	MaximumGapMilliseconds         int64                  `json:"maximum_gap_ms"`
	Initial                        PhaseRecord            `json:"initial"`
	Final                          PhaseRecord            `json:"final"`
	Aggregate                      Aggregate              `json:"aggregate"`
	CleanupConfirmed               bool                   `json:"cleanup_confirmed"`
	Joined                         bool                   `json:"joined"`
	JoinedMilliseconds             int64                  `json:"joined_ms"`
	FirstFailure                   *FailureRecord         `json:"first_failure,omitempty"`
	FirstFinding                   *FindingRecord         `json:"first_finding,omitempty"`
	LastObservation                *quick.ThermalSnapshot `json:"last_observation,omitempty"`
}

type Monitor struct {
	owner *periodic.Monitor[quick.ThermalSnapshot, Aggregate]
}

func Start(options Options) (*Monitor, error) {
	return start(options, func(ctx context.Context) quick.ThermalSnapshot { return quick.ObserveThermals(ctx, options.Root) })
}

func start(options Options, observe func(context.Context) quick.ThermalSnapshot) (*Monitor, error) {
	owner, err := periodic.Start(observe, assess, reduce, periodic.Options{
		Started: options.Started, Mode: periodic.InitialPeriodicFinal,
		Interval: Interval, ObservationTimeout: ObservationTimeout, MaximumGap: MaximumGap, StopTimeout: StopTimeout,
		Cancel: options.Fail, OnFinding: options.CancelWork,
	})
	if err != nil {
		return nil, err
	}
	return &Monitor{owner: owner}, nil
}

func (monitor *Monitor) Ready(ctx context.Context) error { return monitor.owner.WaitInitial(ctx) }
func (monitor *Monitor) ObserveFinal(ctx context.Context) error {
	if ctx == nil {
		return monitor.owner.ObserveFinal(nil)
	}
	if _, ok := ctx.Deadline(); !ok {
		return monitor.owner.ObserveFinal(ctx)
	}
	bounded, cancel := context.WithTimeout(ctx, FinalTimeout)
	defer cancel()
	return monitor.owner.ObserveFinal(bounded)
}
func (monitor *Monitor) Finding() <-chan struct{}        { return monitor.owner.Finding() }
func (monitor *Monitor) Failed() bool                    { return monitor.owner.Failed() }
func (monitor *Monitor) Stop(ctx context.Context) Result { return resultOf(monitor.owner.Stop(ctx)) }

func phaseName(phase periodic.Phase) string {
	switch phase {
	case periodic.InitialPhase:
		return "initial"
	case periodic.PeriodicPhase:
		return "periodic"
	case periodic.FinalPhase:
		return "final"
	default:
		return "none"
	}
}

func resultOf(summary periodic.Summary[quick.ThermalSnapshot, Aggregate]) Result {
	phase := func(value periodic.PhaseSummary[quick.ThermalSnapshot]) PhaseRecord {
		return PhaseRecord{RequestedMilliseconds: value.RequestedMilliseconds, Attempted: value.Attempted,
			Completed: value.Completed, StartedMilliseconds: value.StartedMilliseconds,
			CompletedMilliseconds: value.CompletedMilliseconds, Observation: value.Observation}
	}
	result := Result{Schema: Schema, Status: "completed", LimitMilliCelsius: LimitMilliCelsius,
		IntervalMilliseconds: summary.IntervalMilliseconds, ObservationTimeoutMilliseconds: summary.ObservationTimeoutMilliseconds,
		MaximumAllowedGapMilliseconds: summary.MaximumAllowedGapMilliseconds, StopTimeoutMilliseconds: StopTimeout.Milliseconds(),
		AttemptedObservations: summary.AttemptedObservations, CompletedObservations: summary.CompletedObservations,
		PeriodicAttempts: summary.AttemptedObservations, PeriodicCompletions: summary.CompletedObservations,
		ElapsedMilliseconds: summary.ElapsedMilliseconds, FirstCompletedMilliseconds: summary.FirstCompletedMilliseconds,
		LastCompletedMilliseconds: summary.LastCompletedMilliseconds, MaximumGapMilliseconds: summary.MaximumGapMilliseconds,
		Initial: phase(summary.Initial), Final: phase(summary.Final), Aggregate: summary.Aggregate,
		CleanupConfirmed: summary.CleanupConfirmed, Joined: summary.Joined, JoinedMilliseconds: summary.JoinedMilliseconds,
		LastObservation: summary.LastObservation}
	if result.Aggregate.Coverage == "" {
		result.Aggregate.Coverage = quick.ThermalUnavailable
	}
	for _, phase := range []PhaseRecord{result.Initial, result.Final} {
		if phase.Attempted {
			result.PeriodicAttempts--
		}
		if phase.Completed {
			result.PeriodicCompletions--
		}
	}
	if summary.FirstFailure != nil {
		failure := summary.FirstFailure
		result.FirstFailure = &FailureRecord{Reason: failure.Reason, Phase: phaseName(failure.Phase),
			ElapsedMilliseconds: failure.ElapsedMilliseconds, Observation: failure.Observation}
		result.Status = "failed"
	}
	if !result.CleanupConfirmed {
		result.Status = "cleanup_unconfirmed"
	}
	if summary.FirstFinding != nil {
		finding := summary.FirstFinding
		result.FirstFinding = &FindingRecord{Reason: finding.Reason, Phase: phaseName(finding.Phase),
			ElapsedMilliseconds: finding.ElapsedMilliseconds, Observation: finding.Observation}
	}
	return CloneResult(result)
}

func CloneResult(value Result) Result {
	copyObservation := func(pointer *quick.ThermalSnapshot) *quick.ThermalSnapshot {
		if pointer == nil {
			return nil
		}
		result := *pointer
		return &result
	}
	value.Initial.Observation = copyObservation(value.Initial.Observation)
	value.Final.Observation = copyObservation(value.Final.Observation)
	value.LastObservation = copyObservation(value.LastObservation)
	if value.FirstFailure != nil {
		failure := *value.FirstFailure
		failure.Observation = copyObservation(failure.Observation)
		value.FirstFailure = &failure
	}
	if value.FirstFinding != nil {
		finding := *value.FirstFinding
		value.FirstFinding = &finding
	}
	return value
}
