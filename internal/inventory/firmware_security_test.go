package inventory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const firmwareTestSecure = "sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"
const firmwareTestSetup = "sys/firmware/efi/efivars/SetupMode-8be4df61-93ca-11d2-aa0d-00e098032b8c"

func firmwareTestWrite(t *testing.T, root, name string, raw []byte) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestFirmwareSecurityLiteralFieldParsers(t *testing.T) {
	for _, tc := range []struct {
		name, field, raw, value, state string
		variable                       bool
	}{
		{"width32", "uefi_platform_bits", "32\n", "32", "reported", false},
		{"width64", "uefi_platform_bits", "64", "64", "reported", false},
		{"width_future", "uefi_platform_bits", "128\n", "", "unsupported", false},
		{"width_zero", "uefi_platform_bits", "0", "", "unsupported", false},
		{"tpm1", "version_major", "1\n", "1", "reported", false},
		{"tpm2", "version_major", "2", "2", "reported", false},
		{"tpm_future", "version_major", "3", "", "unsupported", false},
		{"tpm_huge", "version_major", "999999999999999999999999999999999999999999", "", "unsupported", false},
		{"secure0", "secure_boot", "\x06\x00\x00\x00\x00", "0", "reported", true},
		{"secure1", "secure_boot", "\x06\x00\x00\x00\x01", "1", "reported", true},
		{"setup0", "setup_mode_flag", "\x06\x00\x00\x00\x00", "0", "reported", true},
		{"setup1", "setup_mode_flag", "\x06\x00\x00\x00\x01", "1", "reported", true},
		{"attribute_extra", "secure_boot", "\x07\x00\x00\x00\x01", "", "unsupported", true},
		{"attribute_big_endian", "secure_boot", "\x00\x00\x00\x06\x01", "", "unsupported", true},
		{"attribute_high_bit", "setup_mode_flag", "\x06\x00\x00\x80\x00", "", "unsupported", true},
		{"unknown_flag", "setup_mode_flag", "\x06\x00\x00\x00\x02", "", "unsupported", true},
		{"missing_data", "secure_boot", "\x06\x00\x00\x00", "", "malformed", true},
		{"extra_data", "secure_boot", "\x06\x00\x00\x00\x01\x00", "", "malformed", true},
		{"binary_newline", "secure_boot", "\x06\x00\x00\x00\x01\n", "", "malformed", true},
		{"empty_variable", "setup_mode_flag", "", "", "malformed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(tc.raw)
			original := bytes.Clone(raw)
			value, state := parseFirmwareSecurityScalar(firmwareSecurityField{name: tc.field, variable: tc.variable}, raw)
			if value != tc.value || state != tc.state || !bytes.Equal(raw, original) {
				t.Fatal("literal field oracle", value, state)
			}
		})
	}
}

func TestFirmwareSecurityRawASCIIBeforeNormalization(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"empty", ""}, {"newline", "\n"}, {"leading_space", " 32"}, {"trailing_space", "32 "},
		{"leading_zero", "032"}, {"plus", "+32"}, {"minus", "-1"}, {"fraction", "32.0"}, {"exponent", "3e1"},
		{"tab", "32\t"}, {"crlf", "32\r\n"}, {"two_lf", "32\n\n"}, {"embedded_lf", "3\n2"},
		{"nul", "32\x00"}, {"unicode", "\uFF13\uFF12"}, {"non_ascii", "\xff"}, {"junk", "32x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, field := range []string{"uefi_platform_bits", "version_major"} {
				value, state := parseFirmwareSecurityScalar(firmwareSecurityField{name: field}, []byte(tc.raw))
				if value != "" || state != "malformed" {
					t.Fatal("raw syntax normalized into observation")
				}
			}
		})
	}
}

func TestFirmwareSecurityRealCollectorExactConflictingFlagsAndOrdinals(t *testing.T) {
	root := t.TempDir()
	firmwareTestWrite(t, root, "sys/firmware/efi/fw_platform_size", []byte("64\n"))
	firmwareTestWrite(t, root, firmwareTestSecure, []byte{6, 0, 0, 0, 1})
	firmwareTestWrite(t, root, firmwareTestSetup, []byte{6, 0, 0, 0, 1})
	firmwareTestWrite(t, root, "sys/class/tpm/z-PRIVATE-EK-SERIAL/tpm_version_major", []byte("1\n"))
	firmwareTestWrite(t, root, "sys/class/tpm/a-PRIVATE-EK-SERIAL/tpm_version_major", []byte("2\n"))
	// These unapproved inputs must never become evidence.
	firmwareTestWrite(t, root, "sys/firmware/efi/efivars/PK-PRIVATE", []byte("PRIVATE-CERTIFICATE"))
	firmwareTestWrite(t, root, "sys/class/tpm/a-PRIVATE-EK-SERIAL/pcrs", []byte("PRIVATE-PCR-EVENTS"))
	snapshot, err := NewLinuxCollector(root).Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	const want = `{"schema_version":1,"components":[{"type":"firmware_security","id":"firmware_security:uefi","attributes":[{"name":"secure_boot","value":"1"},{"name":"secure_boot_state","value":"reported"},{"name":"setup_mode_flag","value":"1"},{"name":"setup_mode_flag_state","value":"reported"},{"name":"uefi_platform_bits","value":"64"},{"name":"uefi_platform_bits_state","value":"reported"}]},{"type":"tpm","id":"tpm:1","attributes":[{"name":"version_major","value":"2"},{"name":"version_major_state","value":"reported"}]},{"type":"tpm","id":"tpm:2","attributes":[{"name":"version_major","value":"1"},{"name":"version_major_state","value":"reported"}]}]}`
	if err != nil || string(raw) != want {
		t.Fatal("literal collector bytes", err, string(raw))
	}
	for _, secret := range []string{"PRIVATE", "efivars", "8be4df61", "verified", "healthy"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("private data or verdict escaped")
		}
	}
}

func TestFirmwareSecurityCompletedMalformedFieldRetainsOnlyStates(t *testing.T) {
	root := t.TempDir()
	firmwareTestWrite(t, root, firmwareTestSecure, nil)
	firmwareTestWrite(t, root, firmwareTestSetup, []byte{7, 0, 0, 0, 1})
	snapshot, err := NewLinuxCollector(root).Collect(t.Context())
	want := Snapshot{SchemaVersion: 1, Components: []Component{{Type: ComponentType("firmware_security"), ID: "firmware_security:uefi", Attributes: []Attribute{{"secure_boot_state", "malformed"}, {"setup_mode_flag_state", "unsupported"}, {"uefi_platform_bits_state", "unavailable"}}}}}
	if err != nil || !reflect.DeepEqual(snapshot, want) {
		t.Fatal("completed malformed observation lost", err, snapshot)
	}
}

func TestFirmwareSecurityMissingAndEmptyPreserveNoDataBytes(t *testing.T) {
	for _, label := range []string{"missing", "empty"} {
		t.Run(label, func(t *testing.T) {
			root := t.TempDir()
			if label == "empty" {
				for _, path := range []string{"sys/class/tpm", "sys/firmware/efi/efivars"} {
					if err := os.MkdirAll(filepath.Join(root, path), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			snapshot, err := NewLinuxCollector(root).Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(snapshot)
			if err != nil || string(raw) != `{"schema_version":1,"components":[]}` {
				t.Fatal("unobserved firmware invented a row", err, string(raw))
			}
		})
	}
}

func TestFirmwareSecurityObservedTPMWithUnavailableVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sys/class/tpm/PRIVATE-EK"), 0700); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewLinuxCollector(root).Collect(t.Context())
	want := Snapshot{SchemaVersion: 1, Components: []Component{{Type: ComponentType("tpm"), ID: "tpm:1", Attributes: []Attribute{{"version_major_state", "unavailable"}}}}}
	if err != nil || !reflect.DeepEqual(snapshot, want) {
		t.Fatal("observed interface row lost", err, snapshot)
	}
}
