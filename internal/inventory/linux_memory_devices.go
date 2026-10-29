package inventory

import (
	"bytes"
	"encoding/binary"
	"path"
	"strconv"
)

func (collector *linuxCollection) collectMemoryDevices() []Component {
	root := path.Join(collector.Paths.Sys, "firmware/dmi/entries")
	var components []Component
	for _, entry := range collectionItems(collector, collector.readDir(root)) {
		name := entry.Name()
		if !memoryDeviceEntryName(name) {
			continue
		}
		raw, observed := collector.readScalar(path.Join(root, name, "raw"))
		if !collector.check() {
			return nil
		}
		attributes := memoryDeviceStateAttributes("unavailable")
		if observed {
			attributes = parseMemoryDeviceRecord(raw)
		}
		// The registry name routes the read; the ordinal is not physical identity.
		components = append(components, NewComponent(ComponentMemoryDevice, "memory_device:"+strconv.Itoa(len(components)+1), attributes))
	}
	if !collector.check() {
		return nil
	}
	return components
}

// Accept the Type 17 registry spelling without converting the private suffix.
func memoryDeviceEntryName(name string) bool {
	if len(name) < 4 || name[:3] != "17-" {
		return false
	}
	number := name[3:]
	if number == "0" {
		return true
	}
	if number[0] < '1' || number[0] > '9' {
		return false
	}
	for i := 1; i < len(number); i++ {
		if number[i] < '0' || number[i] > '9' {
			return false
		}
	}
	return true
}

func memoryDeviceStateAttributes(state string) map[string]string {
	return map[string]string{
		"observation_source":   "smbios_type17",
		"capacity_bytes_state": state,
		"memory_type_state":    state,
		"form_factor_state":    state,
	}
}

// Project only the selected numeric fields from one complete bounded record.
// Handles, string indexes, opaque strings and other fields never become values.
func parseMemoryDeviceRecord(raw []byte) map[string]string {
	attributes := memoryDeviceStateAttributes("malformed")
	if len(raw) < 4 || raw[0] != 17 {
		return attributes
	}
	length := int(raw[1])
	if length < 21 || len(raw) < length+2 {
		return attributes
	}
	if terminator := bytes.Index(raw[length:], []byte{0, 0}); terminator < 0 || length+terminator+2 != len(raw) {
		return attributes
	}
	capacity, state := memoryDeviceCapacity(raw[:length])
	attributes["capacity_bytes_state"] = state
	if state == "reported" {
		attributes["capacity_bytes"] = capacity
	}
	for _, field := range []struct {
		name   string
		code   byte
		values map[byte]string
	}{
		{"memory_type", raw[18], memoryDeviceTypes},
		{"form_factor", raw[14], memoryDeviceFormFactors},
	} {
		state := "unsupported"
		if value, exists := field.values[field.code]; exists {
			attributes[field.name] = value
			state = "reported"
		} else if field.code == 2 {
			state = "unknown"
		}
		attributes[field.name+"_state"] = state
	}
	return attributes
}

func memoryDeviceCapacity(formatted []byte) (string, string) {
	size := binary.LittleEndian.Uint16(formatted[12:14])
	switch size {
	case 0:
		return "0", "reported"
	case 0xffff:
		return "", "unknown"
	case 0x8000:
		return "", "unsupported"
	case 0x7fff:
		if len(formatted) <= 28 {
			return "", "unknown"
		}
		if len(formatted) < 32 {
			return "", "malformed"
		}
		extended := binary.LittleEndian.Uint32(formatted[28:32])
		// Reserved bits and undersized extensions are unsupported product input.
		if extended&0x80000000 != 0 || extended < 32767 {
			return "", "unsupported"
		}
		return strconv.FormatUint(uint64(extended)*1048576, 10), "reported"
	}
	if size&0x8000 != 0 {
		return strconv.FormatUint(uint64(size&0x7fff)*1024, 10), "reported"
	}
	return strconv.FormatUint(uint64(size)*1048576, 10), "reported"
}

var memoryDeviceFormFactors = map[byte]string{
	1: "other", 3: "simm", 4: "sip", 5: "chip", 6: "dip", 7: "zip",
	8: "proprietary_card", 9: "dimm", 10: "tsop", 11: "row_of_chips",
	12: "rimm", 13: "sodimm", 14: "srimm", 15: "fb_dimm", 16: "die",
	17: "camm", 18: "cudimm", 19: "csodimm",
}

var memoryDeviceTypes = map[byte]string{
	1: "other", 3: "dram", 4: "edram", 5: "vram", 6: "sram", 7: "ram",
	8: "rom", 9: "flash", 10: "eeprom", 11: "feprom", 12: "eprom",
	13: "cdram", 14: "3dram", 15: "sdram", 16: "sgram", 17: "rdram",
	18: "ddr", 19: "ddr2", 20: "ddr2_fb_dimm", 24: "ddr3",
	25: "fbd2", 26: "ddr4", 27: "lpddr", 28: "lpddr2", 29: "lpddr3",
	30: "lpddr4", 31: "logical_nonvolatile_device", 32: "hbm",
	33: "hbm2", 34: "ddr5", 35: "lpddr5", 36: "hbm3", 37: "mrdimm",
}
