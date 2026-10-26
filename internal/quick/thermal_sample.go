package quick

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	ThermalAvailable   = "available"
	ThermalPartial     = "partial"
	ThermalUnavailable = "unavailable"
)

// ThermalSnapshot describes supported sysfs readings, not physical thermal
// coverage. An unavailable reading must never be interpreted as a safe one.
type ThermalSnapshot struct {
	Coverage            string `json:"coverage"`
	Sensors             int    `json:"sensors"`
	PeakMilliCelsius    int    `json:"peak_millicelsius"`
	RejectedReadings    int    `json:"rejected_readings"`
	UnavailableReadings int    `json:"unavailable_readings"`
	EnumerationErrors   int    `json:"enumeration_errors"`
}

// ObserveThermals retains counts only; sensor paths and labels are not evidence.
// Context checkpoints bound cooperative traversal. The owning monitor must
// still supervise a kernel metadata operation which does not return promptly.
func ObserveThermals(ctx context.Context, root string) ThermalSnapshot {
	if ctx == nil || ctx.Err() != nil {
		return ThermalSnapshot{Coverage: ThermalPartial, EnumerationErrors: 1}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return ThermalSnapshot{Coverage: ThermalPartial, EnumerationErrors: 1}
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return ThermalSnapshot{Coverage: ThermalPartial, EnumerationErrors: 1}
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return ThermalSnapshot{Coverage: ThermalPartial, EnumerationErrors: 1}
	}
	sample := thermalSampler{ctx: ctx, root: root, seen: make(map[string]bool)}
	thermalDirectory := filepath.Join(root, "sys/class/thermal")
	entries := sample.directory(thermalDirectory, true)
	for _, entry := range entries {
		if !sample.contextOK() {
			break
		}
		if !numberedThermalName(entry.Name(), "thermal_zone", "", true) {
			continue
		}
		sample.add(filepath.Join(thermalDirectory, entry.Name(), "temp"))
	}
	hwmonDirectory := filepath.Join(root, "sys/class/hwmon")
	for _, entry := range sample.directory(hwmonDirectory, true) {
		if !sample.contextOK() {
			break
		}
		if !numberedThermalName(entry.Name(), "hwmon", "", true) {
			continue
		}
		directory := filepath.Join(hwmonDirectory, entry.Name())
		chip, valid := sample.hwmonName(directory)
		if !valid {
			if !sample.stopped {
				sample.enumerationErrors++
			}
			continue
		}
		// The hwmon ABI permits attributes in the class directory or the
		// older physical-device directory. Resolve exact aliases once; equal
		// temperatures or labels do not establish physical sensor identity.
		sample.hwmon(directory, chip, false)
		sample.hwmon(filepath.Join(directory, "device"), chip, true)
	}
	sample.contextOK()
	result := ThermalSnapshot{Coverage: ThermalUnavailable, Sensors: sample.sensors, PeakMilliCelsius: sample.peak,
		RejectedReadings: sample.rejected, UnavailableReadings: sample.unavailable, EnumerationErrors: sample.enumerationErrors}
	if sample.rejected > 0 || sample.enumerationErrors > 0 || sample.sensors > 0 && sample.unavailable > 0 {
		result.Coverage = ThermalPartial
	} else if sample.sensors > 0 {
		result.Coverage = ThermalAvailable
	}
	return result
}

type thermalSampler struct {
	ctx                                      context.Context
	root                                     string
	seen                                     map[string]bool
	sensors                                  int
	peak                                     int
	rejected, unavailable, enumerationErrors int
	stopped                                  bool
}

func (sample *thermalSampler) contextOK() bool {
	if sample.stopped {
		return false
	}
	if sample.ctx.Err() != nil {
		sample.stopped = true
		sample.enumerationErrors++
		return false
	}
	return true
}

func (sample *thermalSampler) hwmon(directory, chip string, optional bool) {
	for _, entry := range sample.directory(directory, optional) {
		if !sample.contextOK() {
			break
		}
		// Use the standard millidegree input ABI except for known channels
		// requiring board-specific conversion. Never infer temperatures from
		// voltage channels, limits, labels, or historical readings.
		if !numberedThermalName(entry.Name(), "temp", "_input", false) {
			continue
		}
		channel := strings.TrimSuffix(entry.Name(), "_input")
		if !hwmonTemperatureChannel(chip, channel) {
			continue
		}
		canonical, valid := sample.inputPath(filepath.Join(directory, entry.Name()))
		if !valid {
			continue
		}
		state := max(sample.optionalState(filepath.Join(directory, channel+"_enable"), "0", "1"),
			sample.optionalState(filepath.Join(directory, channel+"_fault"), "1", "0"),
			sample.optionalState(filepath.Join(directory, channel+"_type"), "0", "1", "2", "3", "4", "5", "6"))
		if !sample.contextOK() {
			break
		}
		switch state {
		case thermalReadingUnavailable:
			sample.unavailable++
		case thermalReadingRejected:
			sample.rejected++
		default:
			sample.read(canonical)
		}
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
	if !sample.contextOK() {
		return "", false
	}
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

type thermalReadingState uint8

const (
	thermalReadingReady thermalReadingState = iota
	thermalReadingUnavailable
	thermalReadingRejected
)

func (sample *thermalSampler) optionalState(path, unavailable string, allowed ...string) thermalReadingState {
	if !sample.contextOK() {
		return thermalReadingRejected
	}
	// These ABI attributes are optional. A present but unreadable or malformed
	// value cannot establish that its associated reading is trustworthy.
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return thermalReadingReady
	} else if err != nil {
		return thermalReadingRejected
	}
	canonical, err := sample.resolve(path)
	if err != nil {
		return thermalReadingRejected
	}
	value, err := readThermalAttribute(canonical)
	if err != nil {
		return thermalReadingRejected
	}
	if value == unavailable {
		return thermalReadingUnavailable
	}
	for _, candidate := range allowed {
		if value == candidate {
			return thermalReadingReady
		}
	}
	return thermalReadingRejected
}

func (sample *thermalSampler) add(path string) {
	if canonical, valid := sample.inputPath(path); valid {
		sample.read(canonical)
	}
}

func (sample *thermalSampler) inputPath(path string) (string, bool) {
	if !sample.contextOK() {
		return "", false
	}
	canonical, err := sample.resolve(path)
	if err != nil {
		if !sample.stopped {
			sample.rejected++
		}
		return "", false
	}
	if sample.seen[canonical] {
		return "", false
	}
	sample.seen[canonical] = true
	return canonical, true
}

func (sample *thermalSampler) read(canonical string) {
	if !sample.contextOK() {
		return
	}
	text, err := readThermalAttribute(canonical)
	// Cancellation can race a completed read. Retain its valid measurement,
	// including heat discovered during cleanup, and disclose incomplete traversal.
	defer sample.contextOK()
	if err != nil {
		sample.rejected++
		return
	}
	value, err := strconv.Atoi(text)
	// Retain the existing quick-worker plausibility range and evidence
	// semantics. Missing or invalid telemetry is never counted as a sensor.
	if err != nil || value < 0 || value > 250000 {
		sample.rejected++
		return
	}
	sample.sensors++
	if value > sample.peak {
		sample.peak = value
	}
}

func (sample *thermalSampler) directory(path string, optional bool) []os.DirEntry {
	if !sample.contextOK() {
		return nil
	}
	canonical, err := sample.resolve(path)
	if err != nil {
		if optional && errors.Is(err, os.ErrNotExist) && sample.absentDirectory(path) {
			return nil
		}
		if !sample.stopped {
			sample.enumerationErrors++
		}
		return nil
	}
	before, err := os.Stat(canonical)
	if err != nil || !before.IsDir() {
		sample.enumerationErrors++
		return nil
	}
	file, err := openThermalAttribute(canonical)
	if err != nil {
		sample.enumerationErrors++
		return nil
	}
	defer func() {
		if file.Close() != nil {
			sample.enumerationErrors++
		}
	}()
	opened, err := file.Stat()
	if err != nil || !opened.IsDir() || !os.SameFile(before, opened) {
		sample.enumerationErrors++
		return nil
	}
	var entries []os.DirEntry
	for sample.contextOK() {
		batch, readErr := file.ReadDir(128)
		entries = append(entries, batch...)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			sample.enumerationErrors++
			break
		}
	}
	if !sample.contextOK() {
		return nil
	}
	// Preserve os.ReadDir's stable alias order while checking cancellation
	// between batches rather than silently capping the device enumeration.
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	return entries
}

func (sample *thermalSampler) absentDirectory(path string) bool {
	// A missing optional directory is different from a dangling or escaping
	// ancestor. Verify the nearest existing ancestor before claiming absence.
	for sample.contextOK() {
		_, err := os.Lstat(path)
		if err == nil {
			canonical, resolveErr := sample.resolve(path)
			if resolveErr != nil {
				return false
			}
			info, statErr := os.Stat(canonical)
			return statErr == nil && info.IsDir()
		}
		if !errors.Is(err, os.ErrNotExist) || path == sample.root {
			return false
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
	return false
}

func (sample *thermalSampler) resolve(path string) (string, error) {
	if !sample.contextOK() {
		return "", errors.New("thermal observation interrupted")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !sample.contextOK() {
		return "", errors.New("thermal observation interrupted")
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
