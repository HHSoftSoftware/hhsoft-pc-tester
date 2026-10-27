package safemode

import (
	"context"
	"errors"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/periodic"
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

// Monitor retains the public safety contract over one owned periodic observer.
type Monitor struct{ owner *periodic.Monitor[Result] }

func StartMonitor(observe func(context.Context) Result, options MonitorOptions) (*Monitor, error) {
	if observe == nil || options.Started.IsZero() {
		return nil, errors.New("periodic safety monitor requires an observer and start time")
	}
	owner, err := periodic.Start(observe, assessMonitorObservation, periodic.Options{
		Started: options.Started, Cancel: options.Cancel,
		Interval: MonitorInterval, ObservationTimeout: MonitorObservationTimeout,
		MaximumGap: MonitorMaximumGap, StopTimeout: MonitorStopTimeout,
	})
	if errors.Is(err, periodic.ErrStartInFuture) {
		return nil, errors.New("periodic safety monitor start is in the future")
	}
	if err != nil {
		return nil, err
	}
	return &Monitor{owner: owner}, nil
}

func (monitor *Monitor) Failure() <-chan struct{} {
	if monitor.owner == nil {
		return nil
	}
	return monitor.owner.Failure()
}
func (monitor *Monitor) Failed() bool {
	if monitor.owner == nil {
		return false
	}
	return monitor.owner.Failed()
}
func (monitor *Monitor) Stop(ctx context.Context) MonitorResult {
	return monitorResult(monitor.owner.Stop(ctx))
}
