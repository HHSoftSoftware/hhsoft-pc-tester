package inventory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const memoryTestRegistry = "sys/firmware/dmi/entries"
const memoryTestRaw = "sys/firmware/dmi/entries/17-0/raw"

func memoryTestRecord(length byte, size uint16, form, kind byte, extended uint32, tail []byte) []byte {
	raw := make([]byte, int(length))
	raw[0], raw[1], raw[2], raw[3] = 17, length, 0x71, 0x29
	raw[12], raw[13], raw[14], raw[18] = byte(size), byte(size>>8), form, kind
	if length >= 32 {
		raw[28], raw[29], raw[30], raw[31] = byte(extended), byte(extended>>8), byte(extended>>16), byte(extended>>24)
	}
	return append(raw, tail...)
}
func memoryTestWrite(t *testing.T, root, name string, raw []byte) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func memoryTestAttributes(capacity, capacityState, form, formState, kind, kindState string) map[string]string {
	out := map[string]string{"observation_source": "smbios_type17", "capacity_bytes_state": capacityState, "form_factor_state": formState, "memory_type_state": kindState}
	out["maximum_transfer_rate_mts_state"] = "unavailable"
	out["configured_transfer_rate_mts_state"] = "unavailable"
	if capacityState == "malformed" && formState == "malformed" && kindState == "malformed" {
		out["maximum_transfer_rate_mts_state"] = "malformed"
		out["configured_transfer_rate_mts_state"] = "malformed"
	}
	if capacityState == "reported" {
		out["capacity_bytes"] = capacity
	}
	if formState == "reported" {
		out["form_factor"] = form
	}
	if kindState == "reported" {
		out["memory_type"] = kind
	}
	return out
}
func TestMemoryDeviceCapacityLiteralBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name         string
		length       byte
		size         uint16
		extended     uint32
		value, state string
	}{
		{"empty", 21, 0, 0, "0", "reported"}, {"one_mib", 21, 1, 0, "1048576", "reported"},
		{"little_endian", 21, 0x0100, 0, "268435456", "reported"}, {"largest_direct_mib", 21, 0x7ffe, 0, "34357641216", "reported"},
		{"one_kib", 21, 0x8001, 0, "1024", "reported"}, {"largest_kib", 21, 0xfffe, 0, "33552384", "reported"},
		{"unknown", 21, 0xffff, 0, "", "unknown"}, {"zero_kib_policy", 21, 0x8000, 0, "", "unsupported"},
		{"extension_absent21", 21, 0x7fff, 0, "", "unknown"}, {"extension_absent28", 28, 0x7fff, 0, "", "unknown"},
		{"extension_partial29", 29, 0x7fff, 0, "", "malformed"}, {"extension_partial30", 30, 0x7fff, 0, "", "malformed"}, {"extension_partial31", 31, 0x7fff, 0, "", "malformed"},
		{"extension_zero_policy", 32, 0x7fff, 0, "", "unsupported"}, {"extension_below_threshold", 32, 0x7fff, 32766, "", "unsupported"},
		{"extension_threshold", 32, 0x7fff, 32767, "34358689792", "reported"}, {"extension_32gib", 32, 0x7fff, 32768, "34359738368", "reported"},
		{"extension_64gib_less_mib", 32, 0x7fff, 65535, "68718428160", "reported"},
		{"extension_maximum_386", 32, 0x7fff, 0x7fffffff, "2251799812636672", "reported"},
		{"extension_reserved_bit", 32, 0x7fff, 0x80008000, "", "unsupported"},
		{"unused_extension", 32, 16, 0xffffffff, "16777216", "reported"}, {"future_formatted_length", 255, 0x7fff, 32768, "34359738368", "reported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := memoryTestRecord(tc.length, tc.size, 9, 34, tc.extended, []byte{'x', 0, 0})
			before := bytes.Clone(raw)
			want := memoryTestAttributes(tc.value, tc.state, "dimm", "reported", "ddr5", "reported")
			switch tc.length {
			case 28, 29, 30, 31, 32:
				want["maximum_transfer_rate_mts_state"] = "unknown"
			case 255:
				want["maximum_transfer_rate_mts_state"] = "unknown"
				want["configured_transfer_rate_mts_state"] = "unknown"
			}
			got := parseMemoryDeviceRecord(raw)
			if !reflect.DeepEqual(got, want) || !bytes.Equal(raw, before) {
				t.Fatal("literal capacity/state boundary", got, want)
			}
		})
	}
}
func TestMemoryDeviceEveryEnumCodeHasFiniteDisposition(t *testing.T) {
	form := [256]string{1: "other", 3: "simm", 4: "sip", 5: "chip", 6: "dip", 7: "zip", 8: "proprietary_card", 9: "dimm", 10: "tsop", 11: "row_of_chips", 12: "rimm", 13: "sodimm", 14: "srimm", 15: "fb_dimm", 16: "die", 17: "camm", 18: "cudimm", 19: "csodimm"}
	kind := [256]string{1: "other", 3: "dram", 4: "edram", 5: "vram", 6: "sram", 7: "ram", 8: "rom", 9: "flash", 10: "eeprom", 11: "feprom", 12: "eprom", 13: "cdram", 14: "3dram", 15: "sdram", 16: "sgram", 17: "rdram", 18: "ddr", 19: "ddr2", 20: "ddr2_fb_dimm", 24: "ddr3", 25: "fbd2", 26: "ddr4", 27: "lpddr", 28: "lpddr2", 29: "lpddr3", 30: "lpddr4", 31: "logical_nonvolatile_device", 32: "hbm", 33: "hbm2", 34: "ddr5", 35: "lpddr5", 36: "hbm3", 37: "mrdimm"}
	for code := 0; code < 256; code++ {
		t.Run(fmt.Sprintf("form_%03d", code), func(t *testing.T) {
			state := "unsupported"
			if code == 2 {
				state = "unknown"
			} else if form[code] != "" {
				state = "reported"
			}
			got := parseMemoryDeviceRecord(memoryTestRecord(21, 16, byte(code), 34, 0, []byte{0, 0}))
			want := memoryTestAttributes("16777216", "reported", form[code], state, "ddr5", "reported")
			if !reflect.DeepEqual(got, want) {
				t.Fatal("form-factor code escaped finite mapping", code, got)
			}
		})
		t.Run(fmt.Sprintf("type_%03d", code), func(t *testing.T) {
			state := "unsupported"
			if code == 2 {
				state = "unknown"
			} else if kind[code] != "" {
				state = "reported"
			}
			got := parseMemoryDeviceRecord(memoryTestRecord(21, 16, 9, byte(code), 0, []byte{0, 0}))
			want := memoryTestAttributes("16777216", "reported", "dimm", "reported", kind[code], state)
			if !reflect.DeepEqual(got, want) {
				t.Fatal("memory-type code escaped finite mapping", code, got)
			}
		})
	}
}
func TestMemoryDeviceWholeRecordFramingRejectsAllFields(t *testing.T) {
	good := memoryTestRecord(21, 16, 9, 34, 0, []byte{0, 0})
	for _, variant := range []string{"nil", "short_header", "wrong_type", "short_formatted", "length_past_record", "missing_terminator", "single_nul", "trailing_byte", "concatenated", "early_terminator"} {
		t.Run(variant, func(t *testing.T) {
			raw := bytes.Clone(good)
			switch variant {
			case "nil":
				raw = nil
			case "short_header":
				raw = raw[:3]
			case "wrong_type":
				raw[0] = 16
			case "short_formatted":
				raw[1] = 20
			case "length_past_record":
				raw[1] = 32
			case "missing_terminator":
				raw = append(raw[:21], []byte("PRIVATE-STRING")...)
			case "single_nul":
				raw = raw[:22]
			case "trailing_byte":
				raw = append(raw, 1)
			case "concatenated":
				raw = append(raw, good...)
			case "early_terminator":
				raw = append(raw, []byte("PRIVATE-TAIL\x00\x00")...)
			}
			want := memoryTestAttributes("", "malformed", "", "malformed", "", "malformed")
			if got := parseMemoryDeviceRecord(raw); !reflect.DeepEqual(got, want) {
				t.Fatal("framing retained a favorable field", got)
			}
		})
	}
}
func TestMemoryDeviceOpaqueFieldsAndStringAreaStayPrivate(t *testing.T) {
	base := memoryTestRecord(32, 0x7fff, 13, 34, 32768, []byte("PRIVATE-SERIAL\x00PRIVATE-LOCATOR\x00\x00"))
	want := memoryTestAttributes("34359738368", "reported", "sodimm", "reported", "ddr5", "reported")
	want["maximum_transfer_rate_mts_state"] = "unknown"
	for _, variant := range []string{"private_strings", "non_utf8_strings", "unselected_formatted", "selected_maximum_rate", "string_bytes_are_not_extension", "unknown_with_valid_enums"} {
		t.Run(variant, func(t *testing.T) {
			raw := bytes.Clone(base)
			expected := want
			switch variant {
			case "non_utf8_strings":
				raw = append(raw[:32], []byte{0xff, 0xfe, 0, 0}...)
			case "unselected_formatted":
				for _, i := range []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 15, 16, 17, 19, 20, 23, 24, 25, 26, 27} {
					raw[i] = 0xff
				}
			case "selected_maximum_rate":
				raw[21], raw[22] = 0x80, 0x0c
				expected = memoryTestAttributes("34359738368", "reported", "sodimm", "reported", "ddr5", "reported")
				expected["maximum_transfer_rate_mts_state"] = "reported"
				expected["maximum_transfer_rate_mts"] = "3200"
			case "string_bytes_are_not_extension":
				raw = memoryTestRecord(28, 0x7fff, 13, 34, 0, []byte{0xff, 0x7f, 1, 1, 0, 0})
				expected = memoryTestAttributes("", "unknown", "sodimm", "reported", "ddr5", "reported")
				expected["maximum_transfer_rate_mts_state"] = "unknown"
			case "unknown_with_valid_enums":
				raw = memoryTestRecord(21, 0xffff, 13, 34, 0, []byte{0, 0})
				expected = memoryTestAttributes("", "unknown", "sodimm", "reported", "ddr5", "reported")
			}
			got := parseMemoryDeviceRecord(raw)
			if !reflect.DeepEqual(got, expected) {
				t.Fatal("opaque/unselected bytes changed selected observations", got)
			}
			encoded, err := json.Marshal(got)
			if err != nil || bytes.Contains(encoded, []byte("PRIVATE")) || bytes.Contains(encoded, []byte("serial")) || bytes.Contains(encoded, []byte("locator")) {
				t.Fatal("private record data escaped")
			}
		})
	}
}
func TestMemoryDeviceRegistryNameUsesCanonicalUnboundedSuffix(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      bool
	}{
		{"zero", "17-0", true}, {"one", "17-1", true}, {"ten", "17-10", true}, {"large_suffix", "17-" + strings.Repeat("9", 80), true},
		{"empty", "", false}, {"prefix_only", "17-", false}, {"leading_zero", "17-01", false}, {"double_zero", "17-00", false},
		{"other_type", "16-1", false}, {"future_type", "117-1", false}, {"plus", "17-+1", false}, {"minus", "17--1", false},
		{"space", "17-1 ", false}, {"newline", "17-1\n", false}, {"non_ascii", "17-\uFF11", false}, {"nul", "17-1\x00", false}, {"path", "17-1/raw", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := memoryDeviceEntryName(tc.raw); got != tc.want {
				t.Fatal("registry spelling", got)
			}
		})
	}
}
func TestMemoryDeviceRealCollectorHasExactPrivateOrdinals(t *testing.T) {
	root := t.TempDir()
	memoryTestWrite(t, root, "sys/firmware/dmi/entries/17-2/raw", memoryTestRecord(21, 0xffff, 2, 0, 0, []byte("PRIVATE-SERIAL\x00\x00")))
	memoryTestWrite(t, root, "sys/firmware/dmi/entries/17-10/raw", memoryTestRecord(21, 0, 9, 34, 0, []byte{0, 0}))
	memoryTestWrite(t, root, "sys/firmware/dmi/entries/16-1/raw", []byte("FORBIDDEN-OTHER-TYPE"))
	memoryTestWrite(t, root, "sys/firmware/dmi/tables/DMI", []byte("FORBIDDEN-RAW-TABLE"))
	memoryTestWrite(t, root, "sys/firmware/dmi/entries/17-10/handle", []byte("PRIVATE-HANDLE"))
	snapshot, err := NewLinuxCollector(root).Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	const want = `{"schema_version":1,"components":[{"type":"memory_device","id":"memory_device:1","attributes":[{"name":"capacity_bytes","value":"0"},{"name":"capacity_bytes_state","value":"reported"},{"name":"configured_transfer_rate_mts_state","value":"unavailable"},{"name":"form_factor","value":"dimm"},{"name":"form_factor_state","value":"reported"},{"name":"maximum_transfer_rate_mts_state","value":"unavailable"},{"name":"memory_type","value":"ddr5"},{"name":"memory_type_state","value":"reported"},{"name":"observation_source","value":"smbios_type17"}]},{"type":"memory_device","id":"memory_device:2","attributes":[{"name":"capacity_bytes_state","value":"unknown"},{"name":"configured_transfer_rate_mts_state","value":"unavailable"},{"name":"form_factor_state","value":"unknown"},{"name":"maximum_transfer_rate_mts_state","value":"unavailable"},{"name":"memory_type_state","value":"unsupported"},{"name":"observation_source","value":"smbios_type17"}]}]}`
	if err != nil || string(raw) != want {
		t.Fatal("exact collector bytes", err, string(raw))
	}
	for _, marker := range []string{"PRIVATE", "FORBIDDEN", "17-10", "17-2", "serial", "handle", "healthy"} {
		if bytes.Contains(raw, []byte(marker)) {
			t.Fatal("private routing or verdict escaped")
		}
	}
}
func TestMemoryDeviceNoEligibleRegistryPreservesNoDataBytes(t *testing.T) {
	for _, variant := range []string{"missing", "empty", "ineligible"} {
		t.Run(variant, func(t *testing.T) {
			root := t.TempDir()
			if variant != "missing" {
				if err := os.MkdirAll(filepath.Join(root, memoryTestRegistry), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if variant == "ineligible" {
				memoryTestWrite(t, root, memoryTestRegistry+"/17-01/raw", []byte("FORBIDDEN"))
				memoryTestWrite(t, root, memoryTestRegistry+"/18-0/raw", []byte("FORBIDDEN"))
			}
			snapshot, err := NewLinuxCollector(root).Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(snapshot)
			if err != nil || string(raw) != `{"schema_version":1,"components":[]}` {
				t.Fatal("absence invented memory state", err, string(raw))
			}
		})
	}
}
func TestMemoryDeviceUnavailableDoesNotReplaceOSMemory(t *testing.T) {
	root := t.TempDir()
	memoryTestWrite(t, root, "proc/meminfo", []byte("MemTotal: 8192 kB\nMemAvailable: 4096 kB\n"))
	if err := os.MkdirAll(filepath.Join(root, memoryTestRegistry, "17-0"), 0700); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewLinuxCollector(root).Collect(t.Context())
	want := Snapshot{SchemaVersion: 1, Components: []Component{
		{Type: ComponentMemory, ID: "memory:system", Attributes: []Attribute{{"available_kib", "4096"}, {"total_kib", "8192"}}},
		{Type: ComponentType("memory_device"), ID: "memory_device:1", Attributes: []Attribute{{"capacity_bytes_state", "unavailable"}, {"configured_transfer_rate_mts_state", "unavailable"}, {"form_factor_state", "unavailable"}, {"maximum_transfer_rate_mts_state", "unavailable"}, {"memory_type_state", "unavailable"}, {"observation_source", "smbios_type17"}}},
	}}
	if err != nil || !reflect.DeepEqual(snapshot, want) {
		t.Fatal("OS memory or unavailable row changed", err, snapshot)
	}
}
