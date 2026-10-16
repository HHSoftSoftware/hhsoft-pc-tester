package quick

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrThermalLimit = errors.New("thermal limit reached")

type SafetyEvidence struct {
	GlobalTimeoutMilliseconds int64 `json:"global_timeout_ms"`
	ThermalLimitMilliCelsius  int   `json:"thermal_limit_millicelsius"`
	ThermalSensors            int   `json:"thermal_sensors"`
	ThermalSamples            int   `json:"thermal_samples"`
	PeakMilliCelsius          int   `json:"peak_millicelsius,omitempty"`
}

type safetyMonitor struct {
	context       context.Context
	cancel        context.CancelCauseFunc
	timerCancel   context.CancelFunc
	done          chan struct{}
	root          string
	thermalLimit  int
	globalTimeout time.Duration

	mutex   sync.Mutex
	sensors int
	samples int
	peak    int
}

func newSafetyMonitor(parent context.Context, root string, globalTimeout time.Duration, thermalLimit int) *safetyMonitor {
	timedContext, timerCancel := context.WithTimeout(parent, globalTimeout)
	guardedContext, cancel := context.WithCancelCause(timedContext)
	monitor := &safetyMonitor{
		context: guardedContext, cancel: cancel, timerCancel: timerCancel, done: make(chan struct{}),
		root: root, thermalLimit: thermalLimit, globalTimeout: globalTimeout,
	}
	monitor.sampleThermals()
	go monitor.run()
	return monitor
}

func (monitor *safetyMonitor) run() {
	defer close(monitor.done)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-monitor.context.Done():
			return
		case <-ticker.C:
			monitor.sampleThermals()
		}
	}
}

func (monitor *safetyMonitor) sampleThermals() {
	sensors, peak := thermalSample(monitor.root)
	monitor.mutex.Lock()
	if sensors > monitor.sensors {
		monitor.sensors = sensors
	}
	monitor.samples += sensors
	if peak > monitor.peak {
		monitor.peak = peak
	}
	limitReached := peak >= monitor.thermalLimit && monitor.thermalLimit > 0
	monitor.mutex.Unlock()
	if limitReached {
		monitor.cancel(ErrThermalLimit)
	}
}

func (monitor *safetyMonitor) stop() {
	monitor.cancel(context.Canceled)
	monitor.timerCancel()
	<-monitor.done
}

func (monitor *safetyMonitor) failure() error {
	select {
	case <-monitor.context.Done():
		return context.Cause(monitor.context)
	default:
		return nil
	}
}

func (monitor *safetyMonitor) evidence() SafetyEvidence {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()
	return SafetyEvidence{
		GlobalTimeoutMilliseconds: monitor.globalTimeout.Milliseconds(),
		ThermalLimitMilliCelsius:  monitor.thermalLimit,
		ThermalSensors:            monitor.sensors,
		ThermalSamples:            monitor.samples,
		PeakMilliCelsius:          monitor.peak,
	}
}

func thermalSample(root string) (int, int) {
	entries, err := os.ReadDir(filepath.Join(filepath.Clean(root), "sys/class/thermal"))
	if err != nil {
		return 0, 0
	}
	sensors, peak := 0, 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "thermal_zone") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(filepath.Clean(root), "sys/class/thermal", entry.Name(), "temp"))
		if err != nil || len(data) > 64 {
			continue
		}
		value, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || value < 0 || value > 250000 {
			continue
		}
		sensors++
		if value > peak {
			peak = value
		}
	}
	return sensors, peak
}
