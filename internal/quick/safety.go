package quick

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrThermalLimit       = errors.New("thermal limit reached")
	errThermalObservation = errors.New("thermal observation timed out")
	errThermalPanic       = errors.New("thermal observer panicked")
	errThermalInvalid     = errors.New("invalid thermal observation")
	errThermalCleanup     = errors.New("thermal observer cleanup unconfirmed")
	errStressCoverage     = errors.New("thermal coverage unavailable for CPU stress")
	errStressCounter      = errors.New("CPU stress thermal counter overflow")
)

const (
	thermalInterval           = 250 * time.Millisecond
	thermalObservationTimeout = time.Second
	thermalStopTimeout        = 2 * time.Second
)

type SafetyEvidence struct {
	GlobalTimeoutMilliseconds  int64  `json:"global_timeout_ms"`
	ThermalLimitMilliCelsius   int    `json:"thermal_limit_millicelsius"`
	ThermalSensors             int    `json:"thermal_sensors"`
	ThermalSamples             int    `json:"thermal_samples"`
	PeakMilliCelsius           int    `json:"peak_millicelsius,omitempty"`
	MonitorStatus              string `json:"monitor_status"`
	MonitorFailure             string `json:"monitor_failure,omitempty"`
	CleanupConfirmed           bool   `json:"cleanup_confirmed"`
	ThermalObservations        int    `json:"thermal_observations"`
	ThermalCoverage            string `json:"thermal_coverage"`
	ThermalRejectedReadings    int    `json:"thermal_rejected_readings"`
	ThermalUnavailableReadings int    `json:"thermal_unavailable_readings"`
	ThermalEnumerationErrors   int    `json:"thermal_enumeration_errors"`
}

func unstartedSafety() SafetyEvidence {
	return SafetyEvidence{MonitorStatus: "not_started", CleanupConfirmed: true, ThermalCoverage: ThermalUnavailable}
}

type thermalObserver func(context.Context, string) ThermalSnapshot
type thermalCompletion struct {
	snapshot ThermalSnapshot
	finished time.Time
	panicked bool
	invoked  bool
}
type safetyMonitor struct {
	context                    context.Context
	cancel                     context.CancelCauseFunc
	timerCancel                context.CancelFunc
	done, ready, stopRequested chan struct{}
	stopOnce, readyOnce        sync.Once
	stopTimeout                time.Duration
	mutex                      sync.Mutex
	result                     SafetyEvidence
	failureErr, finalCause     error
	frozen                     bool
	joinedAt                   time.Time
	stress                     *CPUStressStopFacts
}

func newSafetyMonitor(parent context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
	return startSafetyMonitor(parent, root, timeout, limit, ObserveThermals, thermalInterval, thermalObservationTimeout, thermalStopTimeout)
}

// The initial observation is supervised too. A blocked kernel read can outlive
// its deadline, but cannot start load or make the caller wait indefinitely.
func startSafetyMonitor(parent context.Context, root string, timeout time.Duration, limit int, observe thermalObserver, interval, observationTimeout, stopTimeout time.Duration) *safetyMonitor {
	return startSafetyMonitorWithPolicy(parent, root, timeout, limit, observe, interval, observationTimeout, stopTimeout, false)
}

func startSafetyMonitorWithPolicy(parent context.Context, root string, timeout time.Duration, limit int, observe thermalObserver, interval, observationTimeout, stopTimeout time.Duration, strictStress bool) *safetyMonitor {
	timed, timerCancel := context.WithTimeout(parent, timeout)
	guarded, cancel := context.WithCancelCause(timed)
	monitor := &safetyMonitor{context: guarded, cancel: cancel, timerCancel: timerCancel,
		done: make(chan struct{}), ready: make(chan struct{}), stopRequested: make(chan struct{}), stopTimeout: stopTimeout,
		result: SafetyEvidence{GlobalTimeoutMilliseconds: timeout.Milliseconds(), ThermalLimitMilliCelsius: limit,
			MonitorStatus: "running", ThermalCoverage: ThermalUnavailable}}
	if strictStress {
		monitor.stress = &CPUStressStopFacts{}
	}
	go monitor.run(root, observe, interval, observationTimeout)
	select {
	case <-monitor.ready:
	case <-guarded.Done():
	}
	return monitor
}
func (monitor *safetyMonitor) signalReady() { monitor.readyOnce.Do(func() { close(monitor.ready) }) }
func callThermalObserver(observe thermalObserver, ctx context.Context, root string) (completion thermalCompletion) {
	if ctx.Err() != nil {
		return thermalCompletion{finished: time.Now()}
	}
	completion.invoked = true
	defer func() {
		if recover() != nil {
			completion.panicked = true
		}
		completion.finished = time.Now()
	}()
	completion.snapshot = observe(ctx, root)
	return completion
}
func (monitor *safetyMonitor) run(root string, observe thermalObserver, interval, timeout time.Duration) {
	jobs := make(chan context.Context, 1)
	completed := make(chan thermalCompletion, 1)
	observerDone := make(chan struct{})
	go func() {
		defer close(observerDone)
		for ctx := range jobs {
			completed <- callThermalObserver(observe, ctx, root)
		}
	}()
	defer func() {
		close(jobs)
		<-observerDone
		monitor.mutex.Lock()
		monitor.joinedAt = time.Now()
		monitor.mutex.Unlock()
		close(monitor.done)
	}()
	defer monitor.signalReady()
	if monitor.context.Err() != nil {
		return
	}
	for {
		select {
		case <-monitor.stopRequested:
			return
		case <-monitor.context.Done():
			return
		default:
		}
		deadline := time.Now().Add(timeout)
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		jobs <- ctx
		timer := time.NewTimer(time.Until(deadline))
		ending := false
		stopRequested, contextDone := monitor.stopRequested, monitor.context.Done()
		var completion thermalCompletion
	observation:
		for {
			select {
			case completion = <-completed:
				break observation
			case <-timer.C:
				// A queued timely result wins over timer-select ordering.
				select {
				case completion = <-completed:
				default:
					monitor.fail(errThermalObservation)
					cancel()
					monitor.signalReady()
					// Retain ownership even if the kernel read outlives its
					// deadline. stop separately bounds the caller's join.
					completion = <-completed
				}
				break observation
			case <-stopRequested:
				// Normal cleanup lets the current bounded observation finish.
				ending, stopRequested = true, nil
			case <-contextDone:
				ending, contextDone = true, nil
				cancel()
				monitor.signalReady()
			}
		}
		timer.Stop()
		monitor.complete(completion, deadline)
		cancel()
		monitor.signalReady()
		if ending || monitor.failure() != nil {
			return
		}
		pause := time.NewTimer(interval)
		select {
		case <-pause.C:
		case <-monitor.stopRequested:
			pause.Stop()
			return
		case <-monitor.context.Done():
			pause.Stop()
			return
		}
	}
}
func (monitor *safetyMonitor) complete(completion thermalCompletion, deadline time.Time) {
	if !completion.finished.Before(deadline) {
		monitor.fail(errThermalObservation)
	}
	if !completion.invoked {
		return
	}
	if completion.panicked {
		monitor.fail(errThermalPanic)
		return
	}
	sample := completion.snapshot
	if !ValidThermalSnapshot(sample) {
		monitor.fail(errThermalInvalid)
		return
	}
	monitor.mutex.Lock()
	if monitor.frozen {
		monitor.mutex.Unlock()
		return
	}
	evidence := &monitor.result
	if monitor.stress != nil && !monitor.acceptStressSample(sample) {
		monitor.mutex.Unlock()
		monitor.fail(errStressCounter)
		return
	}
	if evidence.ThermalObservations == 0 {
		evidence.ThermalCoverage = sample.Coverage
	} else if evidence.ThermalCoverage != sample.Coverage {
		evidence.ThermalCoverage = ThermalPartial
	}
	evidence.ThermalObservations++
	evidence.ThermalSensors = max(evidence.ThermalSensors, sample.Sensors)
	evidence.ThermalSamples += sample.Sensors
	evidence.PeakMilliCelsius = max(evidence.PeakMilliCelsius, sample.PeakMilliCelsius)
	evidence.ThermalRejectedReadings += sample.RejectedReadings
	evidence.ThermalUnavailableReadings += sample.UnavailableReadings
	evidence.ThermalEnumerationErrors += sample.EnumerationErrors
	limitReached := sample.Sensors > 0 && sample.PeakMilliCelsius >= evidence.ThermalLimitMilliCelsius
	monitor.mutex.Unlock()
	if limitReached {
		monitor.fail(ErrThermalLimit)
	}
	if monitor.stress != nil && sample.Coverage != ThermalAvailable {
		monitor.fail(errStressCoverage)
	}
}

// ValidThermalSnapshot checks the sampler's count and coverage invariants. It
// does not establish safe temperature or physical sensor coverage.
func ValidThermalSnapshot(sample ThermalSnapshot) bool {
	if sample.Sensors < 0 || sample.PeakMilliCelsius < 0 || sample.PeakMilliCelsius > 250000 ||
		sample.RejectedReadings < 0 || sample.UnavailableReadings < 0 || sample.EnumerationErrors < 0 ||
		sample.Sensors == 0 && sample.PeakMilliCelsius != 0 {
		return false
	}
	switch sample.Coverage {
	case ThermalAvailable:
		return sample.Sensors > 0 && sample.RejectedReadings == 0 && sample.UnavailableReadings == 0 && sample.EnumerationErrors == 0
	case ThermalUnavailable:
		return sample.Sensors == 0 && sample.RejectedReadings == 0 && sample.EnumerationErrors == 0
	case ThermalPartial:
		return sample.RejectedReadings > 0 || sample.EnumerationErrors > 0 || sample.Sensors > 0 && sample.UnavailableReadings > 0
	default:
		return false
	}
}
func (monitor *safetyMonitor) fail(err error) {
	monitor.mutex.Lock()
	if !monitor.frozen && monitor.stress != nil {
		switch err {
		case ErrThermalLimit:
			monitor.stress.ThermalLimit = true
		case errThermalObservation:
			monitor.stress.ObservationTimeout = true
		case errThermalPanic:
			monitor.stress.ObserverPanic = true
		case errThermalInvalid:
			monitor.stress.InvalidObservation = true
		case errStressCounter:
			monitor.stress.CounterOverflow = true
		}
	}
	if !monitor.frozen && monitor.failureErr == nil {
		monitor.failureErr = err
	}
	monitor.mutex.Unlock()
	monitor.cancel(err)
}

// A normal stop requests no new readings without pretending the operator
// cancelled. All callers receive the same frozen result; late readings cannot
// rewrite a timed-out cleanup or previously returned evidence.
func (monitor *safetyMonitor) stop() {
	monitor.stopBefore(time.Time{})
}

// stopBefore gives the memory-stress owner an absolute join boundary. A zero
// boundary preserves the existing quick/CPU stop window and ownership rules.
func (monitor *safetyMonitor) stopBefore(absoluteDeadline time.Time) {
	monitor.stopOnce.Do(func() {
		deadline := time.Now().Add(monitor.stopTimeout)
		if !absoluteDeadline.IsZero() && absoluteDeadline.Before(deadline) {
			deadline = absoluteDeadline
		}
		close(monitor.stopRequested)
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		select {
		case <-monitor.done:
		case <-timer.C:
		}
		monitor.mutex.Lock()
		joined := !monitor.joinedAt.IsZero() && !monitor.joinedAt.After(deadline)
		monitor.result.CleanupConfirmed = joined
		if !joined {
			monitor.failureErr = errThermalCleanup
			if monitor.stress != nil {
				monitor.stress.CleanupUnconfirmed = true
			}
		}
		monitor.finalCause = monitor.failureErr
		if monitor.finalCause == nil {
			monitor.finalCause = context.Cause(monitor.context)
		}
		monitor.result.MonitorStatus = "completed"
		if monitor.result.ThermalObservations == 0 {
			monitor.result.MonitorStatus = "not_started"
		}
		if monitor.failureErr != nil {
			monitor.result.MonitorStatus = "failed"
			monitor.result.MonitorFailure = thermalFailureReason(monitor.failureErr)
		}
		if !joined {
			monitor.result.MonitorStatus = "cleanup_unconfirmed"
		}
		monitor.frozen = true
		monitor.mutex.Unlock()
		monitor.cancel(context.Canceled)
		monitor.timerCancel()
	})
}
func thermalFailureReason(err error) string {
	switch err {
	case ErrThermalLimit:
		return "thermal_limit"
	case errThermalObservation:
		return "observation_timeout"
	case errThermalPanic:
		return "observer_panic"
	case errThermalInvalid:
		return "invalid_observation"
	case errThermalCleanup:
		return "cleanup_unconfirmed"
	case errStressCoverage:
		return "coverage_loss"
	case errStressCounter:
		return "counter_overflow"
	default:
		return ""
	}
}
func (monitor *safetyMonitor) failure() error {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	if monitor.frozen {
		return monitor.finalCause
	}
	if monitor.failureErr != nil {
		return monitor.failureErr
	}
	return context.Cause(monitor.context)
}
func (monitor *safetyMonitor) evidence() SafetyEvidence {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	return monitor.result
}
func finishSafety(monitor *safetyMonitor, status *Status, detail *string, evidence *SafetyEvidence) {
	monitor.stop()
	*evidence = monitor.evidence()
	if err := monitor.failure(); err != nil && *status != StatusFail {
		*status, *detail = safetyStatus(err), safetyDetail(err)
	}
}

// acceptStressSample runs under the monitor lock before any aggregate write.
// All source counts and sums fit the same bound on both supported word sizes.
func (monitor *safetyMonitor) acceptStressSample(sample ThermalSnapshot) bool {
	const maximum = 2147483647
	facts, evidence := monitor.stress, &monitor.result
	if sample.Sensors > 0 && sample.PeakMilliCelsius >= evidence.ThermalLimitMilliCelsius {
		facts.ThermalLimit = true
	}
	if evidence.ThermalObservations == 0 {
		facts.InitialCoveragePartial = sample.Coverage == ThermalPartial
		facts.InitialCoverageUnavailable = sample.Coverage == ThermalUnavailable
	} else if sample.Coverage != ThermalAvailable {
		facts.CoverageLost = true
	}
	pairs := [][2]int{
		{evidence.ThermalObservations, 1},
		{evidence.ThermalSamples, sample.Sensors},
		{evidence.ThermalRejectedReadings, sample.RejectedReadings},
		{evidence.ThermalUnavailableReadings, sample.UnavailableReadings},
		{evidence.ThermalEnumerationErrors, sample.EnumerationErrors},
	}
	if evidence.ThermalSensors < 0 || evidence.ThermalSensors > maximum || sample.Sensors > maximum {
		return false
	}
	for _, pair := range pairs {
		if pair[0] < 0 || pair[1] < 0 || pair[0] > maximum || pair[1] > maximum-pair[0] {
			return false
		}
	}
	return true
}

func (monitor *safetyMonitor) stressFacts() CPUStressStopFacts {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	if monitor.stress == nil {
		return CPUStressStopFacts{}
	}
	return *monitor.stress
}

// admitCPUStress observes evidence, independent facts and first failure together.
// A valid adverse sample is a reportable stop, never an accounting defect.
func (monitor *safetyMonitor) admitCPUStress() bool {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	if monitor.stress == nil || monitor.frozen || monitor.failureErr != nil || monitor.context.Err() != nil {
		return false
	}
	facts, evidence := monitor.stress, monitor.result
	return !facts.ThermalLimit && !facts.InitialCoveragePartial && !facts.InitialCoverageUnavailable &&
		!facts.CoverageLost && !facts.ObservationTimeout && !facts.ObserverPanic &&
		!facts.InvalidObservation && !facts.CounterOverflow &&
		evidence.ThermalObservations > 0 && evidence.ThermalCoverage == ThermalAvailable &&
		evidence.PeakMilliCelsius < evidence.ThermalLimitMilliCelsius
}
