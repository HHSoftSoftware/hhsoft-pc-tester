package inventory

import (
	"path"
	"strconv"
	"strings"
)

const (
	pcieReported    = "reported"
	pcieUnknown     = "unknown"
	pcieUnsupported = "unsupported"
	pcieMalformed   = "malformed"
	pcieUnavailable = "unavailable"
	pcieReadError   = "read_error"
)

// These are device maximum capabilities, not negotiated or path bandwidth.
// Two clean absences leave historical PCI observations unchanged.
func (collector *linuxCollection) addPCIeCapabilities(attributes map[string]string, base string) {
	speedState, speed := collector.readPCIeCapability(path.Join(base, "max_link_speed"), parsePCIeMaximumSpeed)
	if !collector.check() {
		return
	}
	widthState, width := collector.readPCIeCapability(path.Join(base, "max_link_width"), parsePCIeMaximumWidth)
	if !collector.check() || speedState == pcieUnavailable && widthState == pcieUnavailable {
		return
	}
	attributes["max_link_speed_state"] = speedState
	attributes["max_link_width_state"] = widthState
	if speedState == pcieReported {
		attributes["max_link_speed_mts"] = speed
	}
	if widthState == pcieReported {
		attributes["max_link_width_lanes"] = width
	}
}

func (collector *linuxCollection) readPCIeCapability(name string, parse func([]byte) (string, string)) (string, string) {
	failures := collector.acquisitionBudget.diagnostics.ScalarFailures
	data, observed := collector.readScalar(name)
	if !collector.check() {
		return "", ""
	}
	if !observed {
		if collector.acquisitionBudget.diagnostics.ScalarFailures > failures {
			return pcieReadError, ""
		}
		return pcieUnavailable, ""
	}
	state, value := parse(data)
	if !collector.check() {
		return "", ""
	}
	return state, value
}

// Only one optional LF is framing. General inventory whitespace normalization
// must not turn malformed sysfs bytes into an admitted capability.
func pcieScalarToken(data []byte) string {
	if len(data) != 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	return string(data)
}

func parsePCIeMaximumSpeed(data []byte) (string, string) {
	value := pcieScalarToken(data)
	switch value {
	case "2.5 GT/s PCIe":
		return pcieReported, "2500"
	case "5.0 GT/s PCIe":
		return pcieReported, "5000"
	case "8.0 GT/s PCIe":
		return pcieReported, "8000"
	case "16.0 GT/s PCIe":
		return pcieReported, "16000"
	case "32.0 GT/s PCIe":
		return pcieReported, "32000"
	case "64.0 GT/s PCIe":
		return pcieReported, "64000"
	case "Unknown":
		return pcieUnknown, ""
	}
	number, ok := strings.CutSuffix(value, " GT/s PCIe")
	if !ok || len(number) < 3 || number[len(number)-2] != '.' {
		return pcieMalformed, ""
	}
	fraction := number[len(number)-1]
	if fraction < '0' || fraction > '9' {
		return pcieMalformed, ""
	}
	whole, ok := pcieUnsigned(number[:len(number)-2])
	maximum := ^uint64(0)
	if !ok || whole > maximum/1000 {
		return pcieMalformed, ""
	}
	fractionMTs := uint64(fraction-'0') * 100
	if whole*1000 > maximum-fractionMTs {
		return pcieMalformed, ""
	}
	return pcieUnsupported, ""
}

func parsePCIeMaximumWidth(data []byte) (string, string) {
	value := pcieScalarToken(data)
	switch value {
	case "1", "2", "4", "8", "12", "16", "32":
		return pcieReported, value
	case "255":
		return pcieUnknown, ""
	}
	if _, ok := pcieUnsigned(value); !ok {
		return pcieMalformed, ""
	}
	// Zero is reserved, not an observed zero-lane link or absent device.
	return pcieUnsupported, ""
}

func pcieUnsigned(value string) (uint64, bool) {
	if value == "" || len(value) > 1 && value[0] == '0' {
		return 0, false
	}
	for index := range len(value) {
		if value[index] < '0' || value[index] > '9' {
			return 0, false
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	return parsed, err == nil
}
