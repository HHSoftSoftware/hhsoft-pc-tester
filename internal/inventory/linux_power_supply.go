package inventory

import (
	"path"
	"strconv"
)

type powerSupplyField struct {
	input, name      string
	enum             bool
	minimum, maximum int64
}

var powerSupplyFields = [...]powerSupplyField{
	{"type", "supply_type", true, 0, 0},
	{"scope", "scope", true, 0, 0},
	{"present", "present", false, 0, 1},
	{"online", "online", false, 0, 2},
	{"status", "status", true, 0, 0},
	{"capacity", "capacity_percent", false, 0, 100},
	{"cycle_count", "cycle_count", false, 1, 2147483647},
	{"energy_full", "energy_full_uwh", false, -2147483648, 2147483647},
	{"energy_full_design", "energy_full_design_uwh", false, -2147483648, 2147483647},
	{"charge_full", "charge_full_uah", false, -2147483648, 2147483647},
	{"charge_full_design", "charge_full_design_uah", false, -2147483648, 2147483647},
}

// IsPowerSupplyObservationAttribute identifies the closed telemetry projection.
// Callers must also check the component type; other types retain their policy.
func IsPowerSupplyObservationAttribute(name string) bool {
	for _, field := range powerSupplyFields {
		if name == field.name || name == field.name+"_state" {
			return true
		}
	}
	return false
}

func (collector *linuxCollection) collectPowerSupplies() []Component {
	root := path.Join(collector.Paths.Sys, "class/power_supply")
	var components []Component
	for index, entry := range collectionItems(collector, collector.readDir(root)) {
		if !collector.check() {
			return nil
		}
		// Kernel names may contain identifiers. Only a nonpersistent ordinal
		// reaches the snapshot; a same-count replacement can be invisible.
		id := "power_supply:" + strconv.Itoa(index+1)
		base := path.Join(root, entry.Name())
		attributes := make(map[string]string, 2*len(powerSupplyFields))
		for _, field := range collectionItems(collector, powerSupplyFields[:]) {
			data, observed := collector.readScalar(path.Join(base, field.input))
			if !collector.check() {
				return nil
			}
			value, state := "", "unavailable"
			if observed {
				value, state = parsePowerSupplyScalar(field, data)
			}
			if !collector.check() {
				return nil
			}
			attributes[field.name+"_state"] = state
			if state == "reported" {
				attributes[field.name] = value
			}
		}
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentPowerSupply, id, attributes))
	}
	return components
}

// Parse completed raw bytes before generic component normalization. These
// finite observations are driver reports, not health or physical truth.
func parsePowerSupplyScalar(field powerSupplyField, data []byte) (string, string) {
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	if len(data) == 0 || data[0] == ' ' || data[len(data)-1] == ' ' {
		return "", "malformed"
	}
	for _, value := range data {
		if value < 0x20 || value > 0x7e {
			return "", "malformed"
		}
	}
	if field.enum {
		value := string(data)
		if knownPowerSupplyEnum(field.input, value) {
			return value, "reported"
		}
		return "", "unsupported"
	}
	start := 0
	if data[0] == '-' {
		start = 1
		if len(data) == 1 || data[1] == '0' {
			return "", "malformed"
		}
	}
	if data[start] == '0' {
		if len(data) != start+1 {
			return "", "malformed"
		}
	} else if data[start] < '1' || data[start] > '9' {
		return "", "malformed"
	}
	for _, value := range data[start:] {
		if value < '0' || value > '9' {
			return "", "malformed"
		}
	}
	value, err := strconv.ParseInt(string(data), 10, 32)
	if err != nil {
		return "", "out_of_range"
	}
	if field.input == "cycle_count" && value == 0 {
		return "", "unavailable"
	}
	if value < field.minimum || value > field.maximum {
		return "", "out_of_range"
	}
	return strconv.FormatInt(value, 10), "reported"
}

func knownPowerSupplyEnum(field, value string) bool {
	switch field {
	case "type":
		switch value {
		case "Unknown", "Battery", "UPS", "Mains", "USB", "USB_DCP", "USB_CDP", "USB_ACA", "USB_C", "USB_PD", "USB_PD_DRP", "BrickID", "Wireless":
			return true
		}
	case "scope":
		switch value {
		case "Unknown", "System", "Device":
			return true
		}
	case "status":
		switch value {
		case "Unknown", "Charging", "Discharging", "Not charging", "Full":
			return true
		}
	}
	return false
}
