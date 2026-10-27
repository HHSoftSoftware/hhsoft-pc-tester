package main

import (
	"context"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
)

type sessionThermalFixture struct {
	clock         func() int64
	finding       chan struct{}
	ready         func(context.Context) error
	final         func(context.Context) error
	stop          func(context.Context) thermal.Result
	failed        bool
	stops, finals int
}

func (m *sessionThermalFixture) Ready(ctx context.Context) error {
	if m.ready != nil {
		return m.ready(ctx)
	}
	return ctx.Err()
}
func (m *sessionThermalFixture) ObserveFinal(ctx context.Context) error {
	m.finals++
	if m.final != nil {
		return m.final(ctx)
	}
	return ctx.Err()
}
func (m *sessionThermalFixture) Finding() <-chan struct{} { return m.finding }
func (m *sessionThermalFixture) Failed() bool             { return m.failed }
func (m *sessionThermalFixture) Stop(ctx context.Context) thermal.Result {
	m.stops++
	if m.stop != nil {
		return m.stop(ctx)
	}
	return sessionThermalSummary(m.clock())
}

func sessionThermalSummary(elapsed int64) thermal.Result {
	unavailable := quick.ThermalSnapshot{Coverage: quick.ThermalUnavailable}
	periodic := uint64(elapsed / 1000)
	final := thermal.PhaseRecord{RequestedMilliseconds: elapsed, Attempted: true, Completed: true, StartedMilliseconds: elapsed, CompletedMilliseconds: elapsed, Observation: &unavailable}
	return thermal.Result{Schema: thermal.Schema, Status: "completed", LimitMilliCelsius: 90000, IntervalMilliseconds: 1000, ObservationTimeoutMilliseconds: 1000, MaximumAllowedGapMilliseconds: 2500, StopTimeoutMilliseconds: 2000,
		AttemptedObservations: periodic + 2, CompletedObservations: periodic + 2, PeriodicAttempts: periodic, PeriodicCompletions: periodic, ElapsedMilliseconds: elapsed, LastCompletedMilliseconds: elapsed, MaximumGapMilliseconds: min(elapsed, 1000),
		Initial: thermal.PhaseRecord{Attempted: true, Completed: true, Observation: &unavailable}, Final: final, Aggregate: thermal.Aggregate{Coverage: quick.ThermalUnavailable, AcceptedObservations: periodic + 2, UnavailableObservations: periodic + 2}, CleanupConfirmed: true, Joined: true, JoinedMilliseconds: elapsed, LastObservation: &unavailable}
}
