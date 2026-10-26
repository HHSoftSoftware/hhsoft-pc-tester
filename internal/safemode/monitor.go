package safemode

import (
	"context"
	"errors"
	"sync"
	"time"
)

const MonitorSchema = "hhsoft.pc-tester.periodic-safety/v1"

const (
	MonitorInterval           = time.Second
	MonitorObservationTimeout = time.Second
	MonitorMaximumGap         = 2500 * time.Millisecond
	MonitorStopTimeout        = 2 * time.Second
)

type MonitorOptions struct {
	Started time.Time
	// Cancel is the session's nonblocking context cancellation function.
	Cancel context.CancelFunc
}

type MonitorFailure struct {
	Reason              string  `json:"reason"`
	ElapsedMilliseconds int64   `json:"elapsed_ms"`
	Observation         *Result `json:"observation,omitempty"`
}

// MonitorResult discloses periodic snapshot coverage, not continuous detection.
// Completion offsets and gaps include the uncovered beginning and ending edges.
type MonitorResult struct {
	Schema                         string          `json:"schema"`
	Status                         string          `json:"status"`
	IntervalMilliseconds           int64           `json:"interval_ms"`
	ObservationTimeoutMilliseconds int64           `json:"observation_timeout_ms"`
	MaximumAllowedGapMilliseconds  int64           `json:"maximum_allowed_gap_ms"`
	AttemptedObservations          uint64          `json:"attempted_observations"`
	CompletedObservations          uint64          `json:"completed_observations"`
	ElapsedMilliseconds            int64           `json:"elapsed_ms"`
	FirstCompletedMilliseconds     int64           `json:"first_completed_ms"`
	LastCompletedMilliseconds      int64           `json:"last_completed_ms"`
	MaximumGapMilliseconds         int64           `json:"maximum_gap_ms"`
	CleanupConfirmed               bool            `json:"cleanup_confirmed"`
	FirstFailure                   *MonitorFailure `json:"first_failure,omitempty"`
	LastObservation                *Result         `json:"last_observation,omitempty"`
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

type Monitor struct {
	mutex               sync.Mutex
	freezeMutex         sync.Mutex
	clock               monitorClock
	options             MonitorOptions
	result              MonitorResult
	final               *MonitorResult
	lastCompleted       time.Time
	lastClock           time.Time
	joinedAt            time.Time
	failure, stop, done chan struct{}
	stopOnce            sync.Once
	stopRequested       bool
	stopDeadline        time.Time
	stopContext         context.Context
}

func StartMonitor(observe func(context.Context) Result, options MonitorOptions) (*Monitor, error) {
	return startMonitor(observe, options, realMonitorClock{})
}

func startMonitor(observe func(context.Context) Result, options MonitorOptions, clock monitorClock) (*Monitor, error) {
	if observe == nil || clock == nil || options.Started.IsZero() {
		return nil, errors.New("periodic safety monitor requires an observer and start time")
	}
	now := clock.now()
	if now.Before(options.Started) {
		return nil, errors.New("periodic safety monitor start is in the future")
	}
	monitor := &Monitor{clock: clock, options: options, lastCompleted: options.Started, lastClock: options.Started,
		failure: make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{}),
		result: MonitorResult{Schema: MonitorSchema, Status: "pass", IntervalMilliseconds: MonitorInterval.Milliseconds(),
			ObservationTimeoutMilliseconds: MonitorObservationTimeout.Milliseconds(), MaximumAllowedGapMilliseconds: MonitorMaximumGap.Milliseconds()}}
	go monitor.run(observe)
	return monitor, nil
}

func (monitor *Monitor) Failure() <-chan struct{} { return monitor.failure }

func (monitor *Monitor) Failed() bool {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	return monitor.result.FirstFailure != nil || monitor.final != nil && monitor.final.FirstFailure != nil
}
