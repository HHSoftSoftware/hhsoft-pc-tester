package inventory

import "path"

// Current speed and width are independent kernel observations, not an atomic
// measurement, bandwidth estimate or health verdict. Two clean absences keep
// historical PCI component bytes unchanged.
func (collector *linuxCollection) addPCIeNegotiatedObservations(attributes map[string]string, base string) {
	// The kernel uses the same speed strings for maximum and current speed.
	speedState, speed := collector.readPCIeCapability(path.Join(base, "current_link_speed"), parsePCIeMaximumSpeed)
	if !collector.check() {
		return
	}
	widthState, width := collector.readPCIeCapability(path.Join(base, "current_link_width"), parsePCIeNegotiatedWidth)
	if !collector.check() || speedState == pcieUnavailable && widthState == pcieUnavailable {
		return
	}
	attributes["current_link_speed_state"] = speedState
	attributes["current_link_width_state"] = widthState
	if speedState == pcieReported {
		attributes["current_link_speed_mts"] = speed
	}
	if widthState == pcieReported {
		attributes["current_link_width_lanes"] = width
	}
}

func parsePCIeNegotiatedWidth(data []byte) (string, string) {
	value := pcieScalarToken(data)
	switch value {
	case "1", "2", "4", "8", "12", "16", "32":
		return pcieReported, value
	}
	if _, ok := pcieUnsigned(value); !ok {
		return pcieMalformed, ""
	}
	// Unlike maximum width, current width has no 255 unknown sentinel. Neither
	// zero nor an unlisted width establishes absence or a broken link.
	return pcieUnsupported, ""
}

// IsPCIeNegotiatedObservationAttribute identifies current observations that
// remain in authenticated reports but do not identify hardware configuration.
// Callers must apply this projection only to PCI components.
func IsPCIeNegotiatedObservationAttribute(name string) bool {
	switch name {
	case "current_link_speed_state", "current_link_speed_mts", "current_link_width_state", "current_link_width_lanes":
		return true
	default:
		return false
	}
}
