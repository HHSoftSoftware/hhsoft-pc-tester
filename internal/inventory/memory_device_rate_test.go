package inventory

import (
	"bytes"
	"reflect"
	"testing"
)

// Synthetic Type 17 bytes; independent literal expectations live in the tests.
func memoryRateTestRecord(length byte, maximum, configured uint16, maxExtended, configuredExtended uint32) []byte {
	raw := memoryTestRecord(length, 16384, 9, 34, 0, []byte("RATE-PRIVATE-CANARY\x00\x00"))
	for _, field := range []struct {
		offset int
		value  uint32
		width  int
	}{{21, uint32(maximum), 2}, {32, uint32(configured), 2}, {84, maxExtended, 4}, {88, configuredExtended, 4}} {
		for i := 0; i < field.width && field.offset+i < int(length); i++ {
			raw[field.offset+i] = byte(field.value >> (8 * i))
		}
	}
	return raw
}

func memoryRateExpected(maximum, maxState, configured, configuredState string) map[string]string {
	attrs := memoryTestAttributes("17179869184", "reported", "dimm", "reported", "ddr5", "reported")
	attrs["maximum_transfer_rate_mts_state"], attrs["configured_transfer_rate_mts_state"] = maxState, configuredState
	if maxState == "reported" {
		attrs["maximum_transfer_rate_mts"] = maximum
	}
	if configuredState == "reported" {
		attrs["configured_transfer_rate_mts"] = configured
	}
	return attrs
}

func TestMemoryDeviceMaximumTransferRateLiteralBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name         string
		length       byte
		base         uint16
		extended     uint32
		value, state string
	}{
		{"base_absent", 21, 0, 0, "", "unavailable"},
		{"base_partial", 22, 65535, 0, "", "malformed"},
		{"zero", 23, 0, 0, "", "unknown"},
		{"one", 23, 1, 0, "1", "reported"},
		{"little_endian", 23, 256, 0, "256", "reported"},
		{"direct_3200", 23, 3200, 0, "3200", "reported"},
		{"direct_maximum", 23, 65534, 0, "65534", "reported"},
		{"sentinel_legacy", 23, 65535, 0, "", "unsupported"},
		{"extension_absent", 84, 65535, 0, "", "unsupported"},
		{"extension_partial1", 85, 65535, 4294967295, "", "malformed"},
		{"extension_partial2", 86, 65535, 4294967295, "", "malformed"},
		{"extension_partial3", 87, 65535, 4294967295, "", "malformed"},
		{"extension_zero", 88, 65535, 0, "", "unsupported"},
		{"extension_one", 88, 65535, 1, "", "unsupported"},
		{"extension_small", 88, 65535, 65534, "", "unsupported"},
		{"extension_threshold", 88, 65535, 65535, "65535", "reported"},
		{"extension_little_endian", 88, 65535, 65536, "65536", "reported"},
		{"extension_maximum_386", 88, 65535, 2147483647, "2147483647", "reported"},
		{"extension_reserved_bit", 88, 65535, 2147483648, "", "unsupported"},
		{"extension_all_bits", 88, 65535, 4294967295, "", "unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := memoryRateTestRecord(tc.length, tc.base, 0, tc.extended, 0)
			before := bytes.Clone(raw)
			got := parseMemoryDeviceRecord(raw)
			value, exists := got["maximum_transfer_rate_mts"]
			if got["maximum_transfer_rate_mts_state"] != tc.state || value != tc.value || exists != (tc.state == "reported") || !bytes.Equal(raw, before) {
				t.Fatal("literal transfer-rate boundary or input ownership", got)
			}
			if got["capacity_bytes"] != "17179869184" || got["capacity_bytes_state"] != "reported" || got["form_factor"] != "dimm" || got["memory_type"] != "ddr5" {
				t.Fatal("rate damaged independent selected fields")
			}
		})
	}
}

func TestMemoryDeviceConfiguredTransferRateLiteralBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name         string
		length       byte
		base         uint16
		extended     uint32
		value, state string
	}{
		{"base_absent", 32, 0, 0, "", "unavailable"},
		{"base_partial", 33, 65535, 0, "", "malformed"},
		{"zero", 34, 0, 0, "", "unknown"},
		{"one", 34, 1, 0, "1", "reported"},
		{"little_endian", 34, 256, 0, "256", "reported"},
		{"direct_3200", 34, 3200, 0, "3200", "reported"},
		{"direct_maximum", 34, 65534, 0, "65534", "reported"},
		{"sentinel_legacy", 34, 65535, 0, "", "unsupported"},
		{"extension_absent", 88, 65535, 0, "", "unsupported"},
		{"extension_partial1", 89, 65535, 4294967295, "", "malformed"},
		{"extension_partial2", 90, 65535, 4294967295, "", "malformed"},
		{"extension_partial3", 91, 65535, 4294967295, "", "malformed"},
		{"extension_zero", 92, 65535, 0, "", "unsupported"},
		{"extension_one", 92, 65535, 1, "", "unsupported"},
		{"extension_small", 92, 65535, 65534, "", "unsupported"},
		{"extension_threshold", 92, 65535, 65535, "65535", "reported"},
		{"extension_little_endian", 92, 65535, 65536, "65536", "reported"},
		{"extension_maximum_386", 92, 65535, 2147483647, "2147483647", "reported"},
		{"extension_reserved_bit", 92, 65535, 2147483648, "", "unsupported"},
		{"extension_all_bits", 92, 65535, 4294967295, "", "unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := memoryRateTestRecord(tc.length, 0, tc.base, 0, tc.extended)
			before := bytes.Clone(raw)
			got := parseMemoryDeviceRecord(raw)
			value, exists := got["configured_transfer_rate_mts"]
			if got["configured_transfer_rate_mts_state"] != tc.state || value != tc.value || exists != (tc.state == "reported") || !bytes.Equal(raw, before) {
				t.Fatal("literal transfer-rate boundary or input ownership", got)
			}
			if got["capacity_bytes"] != "17179869184" || got["capacity_bytes_state"] != "reported" || got["form_factor"] != "dimm" || got["memory_type"] != "ddr5" {
				t.Fatal("rate damaged independent selected fields")
			}
		})
	}
}

func TestMemoryDeviceTransferRatesRemainIndependent(t *testing.T) {
	for _, tc := range []struct {
		name                                                 string
		length                                               byte
		maximum, configured                                  uint16
		maxExtended, configuredExtended                      uint32
		maxValue, maxState, configuredValue, configuredState string
	}{
		{"configured_above_maximum", 92, 4800, 6000, 0, 0, "4800", "reported", "6000", "reported"},
		{"distinct_extensions", 92, 65535, 65535, 65536, 131072, "65536", "reported", "131072", "reported"},
		{"maximum_bad_extension", 92, 65535, 3200, 2147483648, 0, "", "unsupported", "3200", "reported"},
		{"configured_bad_extension", 92, 4800, 65535, 0, 65534, "4800", "reported", "", "unsupported"},
		{"configured_partial_extension", 90, 65535, 65535, 65536, 131072, "65536", "reported", "", "malformed"},
		{"maximum_absent_extension", 34, 65535, 3200, 0, 0, "", "unsupported", "3200", "reported"},
		{"configured_absent_extension", 88, 4800, 65535, 65536, 0, "4800", "reported", "", "unsupported"},
		{"direct_ignores_reserved_extensions", 92, 4800, 6000, 4294967295, 4294967295, "4800", "reported", "6000", "reported"},
		{"zero_ignores_reserved_extensions", 92, 0, 0, 4294967295, 4294967295, "", "unknown", "", "unknown"},
		{"direct_ignores_partial_extension", 89, 4800, 6000, 4294967295, 4294967295, "4800", "reported", "6000", "reported"},
		{"zero_ignores_partial_extension", 85, 0, 0, 4294967295, 4294967295, "", "unknown", "", "unknown"},
		{"direct_ignores_partial_maximum_extension", 85, 4800, 6000, 4294967295, 4294967295, "4800", "reported", "6000", "reported"},
		{"zero_ignores_partial_configured_extension", 89, 0, 0, 4294967295, 4294967295, "", "unknown", "", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := memoryRateTestRecord(tc.length, tc.maximum, tc.configured, tc.maxExtended, tc.configuredExtended)
			if got := parseMemoryDeviceRecord(raw); !reflect.DeepEqual(got, memoryRateExpected(tc.maxValue, tc.maxState, tc.configuredValue, tc.configuredState)) {
				t.Fatal("independent firmware claims changed", got)
			}
		})
	}
}

func TestMemoryDeviceTransferRateStringsAndFramingCannotSupplyFields(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		length                    byte
		maxState, configuredState string
	}{
		{"absent_base", 21, "unavailable", "unavailable"},
		{"partial_base", 22, "malformed", "unavailable"},
		{"absent_maximum_extension", 84, "unsupported", "unsupported"},
		{"partial_maximum_extension", 85, "malformed", "unsupported"},
		{"absent_configured_extension", 88, "reported", "unsupported"},
		{"partial_configured_extension", 89, "reported", "malformed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := memoryRateTestRecord(tc.length, 65535, 65535, 65536, 131072)
			raw = append(raw[:tc.length], []byte{0xff, 0xff, 0xff, 0x7f, 'P', 'R', 'I', 'V', 'A', 'T', 'E', 0, 0}...)
			maximum := ""
			if tc.maxState == "reported" {
				maximum = "65536"
			}
			if got := parseMemoryDeviceRecord(raw); !reflect.DeepEqual(got, memoryRateExpected(maximum, tc.maxState, "", tc.configuredState)) {
				t.Fatal("string bytes completed formatted field", got)
			}
		})
	}
	for _, fault := range []string{"wrong_type", "missing_terminator", "trailing_byte", "concatenated"} {
		t.Run(fault, func(t *testing.T) {
			raw := memoryRateTestRecord(92, 4800, 6000, 0, 0)
			switch fault {
			case "wrong_type":
				raw[0] = 16
			case "missing_terminator":
				raw = raw[:len(raw)-2]
			case "trailing_byte":
				raw = append(raw, 1)
			case "concatenated":
				raw = append(raw, raw...)
			}
			want := memoryTestAttributes("", "malformed", "", "malformed", "", "malformed")
			if got := parseMemoryDeviceRecord(raw); !reflect.DeepEqual(got, want) {
				t.Fatal("bad framing retained selected fields", got)
			}
		})
	}
}

func TestMemoryDeviceTransferRatesDoNotInferCapacityOrPresence(t *testing.T) {
	for _, variant := range []string{"empty_capacity", "unsupported_capacity", "unknown_type"} {
		t.Run(variant, func(t *testing.T) {
			raw := memoryRateTestRecord(92, 4800, 6000, 0, 0)
			want := memoryRateExpected("4800", "reported", "6000", "reported")
			switch variant {
			case "empty_capacity":
				raw[12], raw[13] = 0, 0
				want["capacity_bytes"] = "0"
			case "unsupported_capacity":
				raw[12], raw[13] = 0, 0x80
				delete(want, "capacity_bytes")
				want["capacity_bytes_state"] = "unsupported"
			case "unknown_type":
				raw[18] = 2
				delete(want, "memory_type")
				want["memory_type_state"] = "unknown"
			}
			if got := parseMemoryDeviceRecord(raw); !reflect.DeepEqual(got, want) {
				t.Fatal("rate changed an independent claim", got)
			}
		})
	}
}

func TestMemoryDeviceTransferRatesAreNumericAcrossMemoryTypes(t *testing.T) {
	for _, tc := range []struct {
		name string
		code byte
		kind string
	}{{"older_ddr", 18, "ddr"}, {"ddr5", 34, "ddr5"}} {
		t.Run(tc.name, func(t *testing.T) {
			raw := memoryRateTestRecord(34, 4800, 6000, 0, 0)
			raw[18] = tc.code
			want := memoryRateExpected("4800", "reported", "6000", "reported")
			want["memory_type"] = tc.kind
			if got := parseMemoryDeviceRecord(raw); !reflect.DeepEqual(got, want) {
				t.Fatal("memory type scaled a reported numeric transfer rate", got)
			}
		})
	}
}
