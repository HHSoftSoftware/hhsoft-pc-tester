package quick

import (
	"context"
	"errors"
	"io"
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
	root, err := filepath.Abs(root)
	if err != nil {
		return 0, 0
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return 0, 0
	}
	sample := thermalSampler{root: root, seen: make(map[string]bool)}
	thermalDirectory := filepath.Join(root, "sys/class/thermal")
	entries := sample.directory(thermalDirectory)
	for _, entry := range entries {
		if !numberedThermalName(entry.Name(), "thermal_zone", "", true) {
			continue
		}
		sample.add(filepath.Join(thermalDirectory, entry.Name(), "temp"))
	}
	hwmonDirectory := filepath.Join(root, "sys/class/hwmon")
	for _, entry := range sample.directory(hwmonDirectory) {
		if !numberedThermalName(entry.Name(), "hwmon", "", true) {
			continue
		}
		directory := filepath.Join(hwmonDirectory, entry.Name())
		chip, valid := sample.hwmonName(directory)
		if !valid {
			continue
		}
		// The hwmon ABI permits attributes in the class directory or the
		// older physical-device directory. Resolve exact aliases once; equal
		// temperatures or labels do not establish physical sensor identity.
		sample.hwmon(directory, chip)
		sample.hwmon(filepath.Join(directory, "device"), chip)
	}
	return sample.sensors, sample.peak
}

type thermalSampler struct {
	root    string
	seen    map[string]bool
	sensors int
	peak    int
}

func (sample *thermalSampler) hwmon(directory, chip string) {
	for _, entry := range sample.directory(directory) {
		// Use the standard millidegree input ABI except for known channels
		// requiring board-specific conversion. Never infer temperatures from
		// voltage channels, limits, labels, or historical readings.
		if !numberedThermalName(entry.Name(), "temp", "_input", false) {
			continue
		}
		channel := strings.TrimSuffix(entry.Name(), "_input")
		if !hwmonTemperatureChannel(chip, channel) ||
			!sample.optionalValue(filepath.Join(directory, channel+"_enable"), "1") ||
			!sample.optionalValue(filepath.Join(directory, channel+"_fault"), "0") ||
			!sample.optionalValue(filepath.Join(directory, channel+"_type"), "1", "2", "3", "4", "5", "6") {
			continue
		}
		sample.add(filepath.Join(directory, entry.Name()))
	}
}

func hwmonTemperatureChannel(chip, channel string) bool {
	if numberedThermalName(chip, "peci_cputemp.cpu", "", true) {
		// These three input attributes are control/shutdown targets, not
		// observed temperatures. Die, DTS and per-core inputs remain usable.
		// https://docs.kernel.org/hwmon/peci-cputemp.html
		return channel != "temp3" && channel != "temp4" && channel != "temp5"
	}
	switch chip {
	case "vt1211":
		// Only the internal diode is converted by the driver. External
		// channels need board-specific scaling or thermistor conversion.
		// https://docs.kernel.org/hwmon/vt1211.html
		return channel == "temp2"
	case "wm831x":
		// The battery's temp2_input is a voltage; only temp1 is converted.
		// https://docs.kernel.org/hwmon/wm831x.html
		return channel == "temp1"
	default:
		return true
	}
}

func (sample *thermalSampler) hwmonName(directory string) (string, bool) {
	// Name is mandatory in the ABI. Without it, known voltage-producing
	// drivers cannot be distinguished from standard temperature drivers.
	path := filepath.Join(directory, "name")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		path = filepath.Join(directory, "device", "name")
	} else if err != nil {
		return "", false
	}
	canonical, err := sample.resolve(path)
	if err != nil {
		return "", false
	}
	name, err := readThermalAttribute(canonical)
	if err != nil || name == "" {
		return "", false
	}
	for _, character := range name {
		if character < '!' || character > '~' || character == '*' || character == '-' {
			return "", false
		}
	}
	return name, true
}

func (sample *thermalSampler) optionalValue(path string, allowed ...string) bool {
	// These ABI attributes are optional. A present but unreadable or malformed
	// value cannot establish that its associated reading is trustworthy.
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return true
	} else if err != nil {
		return false
	}
	canonical, err := sample.resolve(path)
	if err != nil {
		return false
	}
	value, err := readThermalAttribute(canonical)
	if err != nil {
		return false
	}
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func (sample *thermalSampler) add(path string) {
	canonical, err := sample.resolve(path)
	if err != nil || sample.seen[canonical] {
		return
	}
	text, err := readThermalAttribute(canonical)
	if err != nil {
		return
	}
	value, err := strconv.Atoi(text)
	// Retain the existing quick-worker plausibility range and evidence
	// semantics. Missing or invalid telemetry is never counted as a sensor.
	if err != nil || value < 0 || value > 250000 {
		return
	}
	sample.seen[canonical] = true
	sample.sensors++
	if value > sample.peak {
		sample.peak = value
	}
}

func (sample *thermalSampler) directory(path string) []os.DirEntry {
	canonical, err := sample.resolve(path)
	if err != nil {
		return nil
	}
	entries, _ := os.ReadDir(canonical)
	return entries
}

func (sample *thermalSampler) resolve(path string) (string, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(sample.root, canonical)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("thermal attribute outside root")
	}
	return canonical, nil
}

func numberedThermalName(name, prefix, suffix string, allowZero bool) bool {
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}
	number := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if number == "0" {
		return allowZero
	}
	if len(number) == 0 || number[0] < '1' || number[0] > '9' {
		return false
	}
	for _, digit := range number[1:] {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func readThermalAttribute(path string) (string, error) {
	before, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !before.Mode().IsRegular() {
		return "", errors.New("invalid thermal attribute type")
	}
	file, err := openThermalAttribute(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return "", errors.New("thermal attribute changed during open")
	}
	// sysfs files may report a size of zero or a page regardless of their
	// content, so bound the read itself rather than trusting file size.
	data, err := io.ReadAll(io.LimitReader(file, 65))
	if err != nil || len(data) > 64 {
		return "", errors.New("invalid thermal attribute length")
	}
	return strings.TrimSpace(string(data)), nil
}
