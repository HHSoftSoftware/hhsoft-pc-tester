// Package periodic supervises one bounded periodic observer without owning
// domain schemas or serialization. Observation payloads are immutable values.
package periodic

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrStartInFuture = errors.New("periodic monitor start is in the future")

// Assessment is produced by trusted, bounded, pure policy code after the
// observer completion timestamp is captured. FailureReason is a fixed policy
// identifier, never untrusted observer text.
type Assessment[T any] struct {
	Value         T
	FailureReason string
	FindingReason string
}

type Mode uint8

const (
	DelayedPeriodic Mode = iota
	InitialPeriodicFinal
)

type Phase uint8

const (
	NoPhase Phase = iota
	PeriodicPhase
	InitialPhase
	FinalPhase
)

type Observation[T any] struct {
	Phase                                      Phase
	StartedMilliseconds, CompletedMilliseconds int64
	EligibleForAggregation                     bool
	Assessment                                 Assessment[T]
}

type Reduction[A any] struct {
	Value         A
	FailureReason string
}

// Reducers are bounded pure code. T and A contain only immutable scalar/string
// or fixed-array values, never pointers, slices, maps or retained contexts.
type Reducer[T, A any] func(A, Observation[T]) Reduction[A]

type Options struct {
	Started                                               time.Time
	Interval, ObservationTimeout, MaximumGap, StopTimeout time.Duration
	// Cancel is the session's nonblocking context cancellation function.
	Cancel    context.CancelFunc
	Mode      Mode
	OnFinding context.CancelFunc
}

type Failure[T any] struct {
	Reason              string
	ElapsedMilliseconds int64
	Observation         *T
	Phase               Phase
}

type PhaseSummary[T any] struct {
	RequestedMilliseconds                      int64
	Attempted, Completed                       bool
	StartedMilliseconds, CompletedMilliseconds int64
	Observation                                *T
}

type Finding[T any] struct {
	Reason              string
	Phase               Phase
	ElapsedMilliseconds int64
	Observation         T
}

// Summary discloses periodic snapshot coverage, not continuous detection.
// Completion offsets and gaps include the uncovered beginning and ending edges.
type Summary[T, A any] struct {
	IntervalMilliseconds           int64
	ObservationTimeoutMilliseconds int64
	MaximumAllowedGapMilliseconds  int64
	AttemptedObservations          uint64
	CompletedObservations          uint64
	ElapsedMilliseconds            int64
	FirstCompletedMilliseconds     int64
	LastCompletedMilliseconds      int64
	MaximumGapMilliseconds         int64
	CleanupConfirmed               bool
	FirstFailure                   *Failure[T]
	LastObservation                *T
	Initial, Final                 PhaseSummary[T]
	FirstFinding                   *Finding[T]
	Aggregate                      A
	Joined                         bool
	JoinedMilliseconds             int64
}

type monitorTimer interface {
	channel() <-chan time.Time
	stop() bool
}

type monitorClock interface {
	now() time.Time
	timer(time.Duration) monitorTimer
}

type realMonitorClock struct{}
type realMonitorTimer struct{ value *time.Timer }

func (realMonitorClock) now() time.Time { return time.Now() }
func (realMonitorClock) timer(duration time.Duration) monitorTimer {
	return realMonitorTimer{time.NewTimer(duration)}
}
func (timer realMonitorTimer) channel() <-chan time.Time { return timer.value.C }
func (timer realMonitorTimer) stop() bool                { return timer.value.Stop() }

// Monitor owns one observer. T and assessment values must be immutable plain
// values: no retained caller-owned mutable slices, maps or pointers.
type Monitor[T, A any] struct {
	assess                                        func(T) Assessment[T]
	reduce                                        Reducer[T, A]
	mutex                                         sync.Mutex
	freezeMutex                                   sync.Mutex
	clock                                         monitorClock
	options                                       Options
	result                                        Summary[T, A]
	final                                         *Summary[T, A]
	lastCompleted                                 time.Time
	lastClock                                     time.Time
	joinedAt                                      time.Time
	failure, stop, done                           chan struct{}
	stopOnce                                      sync.Once
	stopRequested                                 bool
	stopDeadline                                  time.Time
	stopContext                                   context.Context
	finding, initialDone, finalDone, finalRequest chan struct{}
	initialOnce, finalOnce                        sync.Once
	finalRequested                                bool
	finalContext                                  context.Context
	finalDeadline                                 time.Time
	initialRequestedAt, finalRequestedAt          time.Time
}

func Start[T, A any](observe func(context.Context) T, assess func(T) Assessment[T], reduce Reducer[T, A], options Options) (*Monitor[T, A], error) {
	return startMonitor(observe, assess, reduce, options, realMonitorClock{})
}

func startMonitor[T, A any](observe func(context.Context) T, assess func(T) Assessment[T], reduce Reducer[T, A], options Options, clock monitorClock) (*Monitor[T, A], error) {
	if observe == nil || assess == nil || clock == nil || options.Started.IsZero() ||
		options.Interval <= 0 || options.ObservationTimeout <= 0 || options.MaximumGap <= 0 || options.StopTimeout <= 0 || options.Mode > InitialPeriodicFinal {
		return nil, errors.New("periodic monitor requires observer, assessment, start time and positive limits")
	}
	now := clock.now()
	if now.Before(options.Started) {
		return nil, ErrStartInFuture
	}
	monitor := &Monitor[T, A]{assess: assess, reduce: reduce, clock: clock, options: options, lastCompleted: options.Started, lastClock: options.Started,
		failure: make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{}),
		finding: make(chan struct{}), initialDone: make(chan struct{}), finalDone: make(chan struct{}), finalRequest: make(chan struct{}, 1),
		result: Summary[T, A]{IntervalMilliseconds: options.Interval.Milliseconds(),
			ObservationTimeoutMilliseconds: options.ObservationTimeout.Milliseconds(), MaximumAllowedGapMilliseconds: options.MaximumGap.Milliseconds()}}
	if options.Mode == InitialPeriodicFinal {
		monitor.initialRequestedAt = now
		monitor.result.Initial.RequestedMilliseconds = now.Sub(options.Started).Milliseconds()
	}
	go monitor.run(observe)
	return monitor, nil
}

func (monitor *Monitor[T, A]) Failure() <-chan struct{} { return monitor.failure }
func (monitor *Monitor[T, A]) Finding() <-chan struct{} { return monitor.finding }

func (monitor *Monitor[T, A]) Failed() bool {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	return monitor.result.FirstFailure != nil || monitor.final != nil && monitor.final.FirstFailure != nil
}
