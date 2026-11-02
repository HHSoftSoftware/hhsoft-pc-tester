package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func powerSupplyTestField(t *testing.T, input string) powerSupplyField {
	t.Helper()
	for _, field := range powerSupplyFields {
		if field.input == input {
			return field
		}
	}
	t.Fatal("missing power scalar role")
	return powerSupplyField{}
}

func TestPowerSupplyFiniteEnumsRetainExactLabels(t *testing.T) {
	for _, item := range []struct {
		input  string
		labels []string
	}{
		{"type", []string{"Unknown", "Battery", "UPS", "Mains", "USB", "USB_DCP", "USB_CDP", "USB_ACA", "USB_C", "USB_PD", "USB_PD_DRP", "BrickID", "Wireless"}},
		{"scope", []string{"Unknown", "System", "Device"}},
		{"status", []string{"Unknown", "Charging", "Discharging", "Not charging", "Full"}},
	} {
		for _, label := range item.labels {
			t.Run(item.input+"/"+label, func(t *testing.T) {
				for _, suffix := range []string{"", "\n"} {
					value, state := parsePowerSupplyScalar(powerSupplyTestField(t, item.input), []byte(label+suffix))
					if value != label || state != "reported" {
						t.Fatalf("finite enum was not retained: %q %q", value, state)
					}
				}
			})
		}
	}
}

func TestPowerSupplyNumericBoundsAndUnavailableCycle(t *testing.T) {
	for _, item := range []struct{ input, raw, value, state string }{
		{"present", "0", "0", "reported"},
		{"present", "1", "1", "reported"},
		{"present", "-1", "", "out_of_range"},
		{"present", "2", "", "out_of_range"},
		{"online", "0", "0", "reported"},
		{"online", "1", "1", "reported"},
		{"online", "2", "2", "reported"},
		{"online", "-1", "", "out_of_range"},
		{"online", "3", "", "out_of_range"},
		{"capacity", "0", "0", "reported"},
		{"capacity", "100", "100", "reported"},
		{"capacity", "-1", "", "out_of_range"},
		{"capacity", "101", "", "out_of_range"},
		{"cycle_count", "0", "", "unavailable"},
		{"cycle_count", "1", "1", "reported"},
		{"cycle_count", "2147483647", "2147483647", "reported"},
		{"cycle_count", "-1", "", "out_of_range"},
		{"cycle_count", "2147483648", "", "out_of_range"},
	} {
		t.Run(item.input+"/"+item.raw, func(t *testing.T) {
			value, state := parsePowerSupplyScalar(powerSupplyTestField(t, item.input), []byte(item.raw+"\n"))
			if value != item.value || state != item.state {
				t.Fatalf("numeric meaning changed: %q %q", value, state)
			}
		})
	}
	for _, input := range []string{"energy_full", "energy_full_design", "charge_full", "charge_full_design", "energy_now", "charge_now"} {
		for _, item := range []struct{ raw, value, state string }{
			{"-2147483648", "-2147483648", "reported"},
			{"-1", "-1", "reported"}, {"0", "0", "reported"},
			{"2147483647", "2147483647", "reported"},
			{"-2147483649", "", "out_of_range"}, {"2147483648", "", "out_of_range"},
		} {
			t.Run(input+"/"+item.raw, func(t *testing.T) {
				value, state := parsePowerSupplyScalar(powerSupplyTestField(t, input), []byte(item.raw))
				if value != item.value || state != item.state {
					t.Fatalf("signed unit value changed: %q %q", value, state)
				}
			})
		}
	}
}

func TestPowerSupplyRawGrammarPrecedesNormalization(t *testing.T) {
	for _, item := range []struct{ label, raw string }{
		{"empty", ""}, {"newline", "\n"}, {"leading_space", " 1"},
		{"trailing_space", "1 "}, {"tab", "1\t"}, {"crlf", "1\r\n"},
		{"two_newlines", "1\n\n"}, {"embedded_newline", "1\n2"},
		{"nul", "1\x00"}, {"unicode_digit", "１"}, {"non_ascii", "\xff"},
		{"plus", "+1"}, {"leading_zero", "01"}, {"negative_zero", "-0"},
		{"negative_leading_zero", "-01"}, {"fraction", "1.0"}, {"exponent", "1e1"},
		{"junk", "1x"}, {"sign_only", "-"}, {"internal_space", "1 2"},
	} {
		t.Run("integer/"+item.label, func(t *testing.T) {
			value, state := parsePowerSupplyScalar(powerSupplyTestField(t, "energy_full"), []byte(item.raw))
			if value != "" || state != "malformed" {
				t.Fatal("noncanonical integer accepted")
			}
		})
	}
	for _, item := range []struct{ label, raw, value, state string }{
		{"exact", "Not charging\n", "Not charging", "reported"},
		{"literal_unknown", "Unknown", "Unknown", "reported"},
		{"double_space", "Not  charging", "", "unsupported"},
		{"case_alias", "not charging", "", "unsupported"},
		{"numeric", "2", "", "unsupported"},
		{"negative_numeric", "-1", "", "unsupported"},
		{"future", "Future-state", "", "unsupported"},
		{"empty", "", "", "malformed"},
		{"leading", " Full", "", "malformed"},
		{"trailing", "Full ", "", "malformed"},
		{"tab", "Not\tcharging", "", "malformed"},
		{"crlf", "Full\r\n", "", "malformed"},
		{"newline", "Full\n\n", "", "malformed"},
		{"nul", "Full\x00", "", "malformed"},
		{"non_ascii", "Füll", "", "malformed"},
	} {
		t.Run("enum/"+item.label, func(t *testing.T) {
			value, state := parsePowerSupplyScalar(powerSupplyTestField(t, "status"), []byte(item.raw))
			if value != item.value || state != item.state {
				t.Fatalf("raw enum state changed: %q %q", value, state)
			}
		})
	}
}

func TestPowerSupplyObservationAttributeSetIsClosed(t *testing.T) {
	// This literal list is independent of the production field descriptors.
	for _, name := range []string{"supply_type", "scope", "present", "online", "status", "capacity_percent", "cycle_count", "energy_full_uwh", "energy_full_design_uwh", "charge_full_uah", "charge_full_design_uah", "energy_now_uwh", "charge_now_uah"} {
		if !IsPowerSupplyObservationAttribute(name) || !IsPowerSupplyObservationAttribute(name+"_state") {
			t.Fatalf("telemetry projection omitted %q", name)
		}
	}
	for _, name := range []string{"", "type", "capacity", "serial_number", "name", "Supply_type", "energy_full", "online_state_state", "future_attribute", "energy_now", "charge_now", "energy_now_uwh_state_state", "charge_now_uwh", "Energy_now_uwh", "energy_now_uwh ", "energy_now_uwh.extra"} {
		if IsPowerSupplyObservationAttribute(name) {
			t.Fatalf("telemetry projection widened to %q", name)
		}
	}
}

func TestPowerSupplyRootCollectorKeepsFiniteRowsAndPrivateOrdinals(t *testing.T) {
	root := t.TempDir()
	values := map[string]string{
		"type": "Battery\n", "scope": "Device\n", "present": "0\n",
		"online": "2\n", "status": "Not charging\n", "capacity": "100\n",
		"cycle_count": "0\n", "energy_full": "-1\n", "energy_full_design": "0\n",
		"charge_full": "2147483647\n", "charge_full_design": "1\n",
		"energy_now": "17\n", "charge_now": "-19\n",
	}
	// Neither class entry is an approved public identifier.
	names := []string{"z-hid-PRIVATE-ADDR-22-battery", "a-hid-PRIVATE-ADDR-11-battery"}
	for _, name := range names {
		directory := filepath.Join(root, "sys", "class", "power_supply", name)
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		for field, value := range values {
			if err := os.WriteFile(filepath.Join(directory, field), []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Distinguish sorted enumeration from input construction order.
	if err := os.WriteFile(filepath.Join(root, "sys/class/power_supply", names[0], "capacity"), []byte("1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewLinuxCollector(root).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != 1 || len(snapshot.Components) != 2 {
		t.Fatalf("wrong supply closure: %#v", snapshot)
	}
	want := map[string]string{
		"supply_type": "Battery", "supply_type_state": "reported",
		"scope": "Device", "scope_state": "reported",
		"present": "0", "present_state": "reported",
		"online": "2", "online_state": "reported",
		"status": "Not charging", "status_state": "reported",
		"capacity_percent": "100", "capacity_percent_state": "reported",
		"cycle_count_state": "unavailable",
		"energy_full_uwh":   "-1", "energy_full_uwh_state": "reported",
		"energy_full_design_uwh": "0", "energy_full_design_uwh_state": "reported",
		"charge_full_uah": "2147483647", "charge_full_uah_state": "reported",
		"charge_full_design_uah": "1", "charge_full_design_uah_state": "reported",
		"energy_now_uwh": "17", "energy_now_uwh_state": "reported",
		"charge_now_uah": "-19", "charge_now_uah_state": "reported",
	}
	for index, component := range snapshot.Components {
		if component.Type != ComponentType("power_supply") || component.ID != []string{"power_supply:1", "power_supply:2"}[index] {
			t.Fatal("private names or unstable positions escaped enumeration")
		}
		if index == 1 {
			want["capacity_percent"] = "1"
		}
		actual := make(map[string]string)
		for _, attribute := range component.Attributes {
			if _, exists := actual[attribute.Name]; exists {
				t.Fatal("duplicate attribute")
			}
			actual[attribute.Name] = attribute.Value
		}
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("finite row mismatch: %#v", actual)
		}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range names {
		if bytes.Contains(raw, []byte(private)) {
			t.Fatal("raw supply name escaped")
		}
	}
}

func TestPowerSupplyUnavailableFieldsKeepAnUnidentifiedRow(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sys/class/power_supply/hid-PRIVATE-NAME"), 0700); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewLinuxCollector(root).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Components) != 1 || snapshot.Components[0].ID != "power_supply:1" {
		t.Fatal("unavailable supply disappeared")
	}
	want := []Attribute{
		{"capacity_percent_state", "unavailable"}, {"charge_full_design_uah_state", "unavailable"},
		{"charge_full_uah_state", "unavailable"}, {"charge_now_uah_state", "unavailable"}, {"cycle_count_state", "unavailable"},
		{"energy_full_design_uwh_state", "unavailable"}, {"energy_full_uwh_state", "unavailable"},
		{"energy_now_uwh_state", "unavailable"},
		{"online_state", "unavailable"}, {"present_state", "unavailable"}, {"scope_state", "unavailable"},
		{"status_state", "unavailable"}, {"supply_type_state", "unavailable"},
	}
	if !reflect.DeepEqual(snapshot.Components[0].Attributes, want) {
		t.Fatal("missing values invented observations")
	}
}

func TestPowerSupplyMissingAndEmptyClassPreserveHistoricalNoData(t *testing.T) {
	for _, label := range []string{"missing", "empty"} {
		t.Run(label, func(t *testing.T) {
			root := t.TempDir()
			if label == "empty" {
				if err := os.MkdirAll(filepath.Join(root, "sys/class/power_supply"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := NewLinuxCollector(root).Collect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(snapshot)
			if err != nil || string(raw) != "{\"schema_version\":1,\"components\":[]}" {
				t.Fatal("no-data bytes changed")
			}
		})
	}
}

func TestPowerSupplyNowRawGrammarAndUnits(t *testing.T) {
	// Literal raw grammar and units; no expectations are copied from descriptors.
	for _, input := range []string{"energy_now", "charge_now"} {
		for _, tc := range []struct{ name, raw, value, state string }{
			{"empty", "", "", "malformed"},
			{"zero", "0", "0", "reported"},
			{"positive", "17", "17", "reported"},
			{"negative", "-17", "-17", "reported"},
			{"terminal_lf", "17\n", "17", "reported"},
			{"zero_lf", "0\n", "0", "reported"},
			{"crlf", "17\r\n", "", "malformed"},
			{"bare_cr", "17\r", "", "malformed"},
			{"lf_only", "\n", "", "malformed"},
			{"repeated_lf", "17\n\n", "", "malformed"},
			{"leading_space", " 17", "", "malformed"},
			{"trailing_space", "17 ", "", "malformed"},
			{"nonbreaking_space", "17\u00a0", "", "malformed"},
			{"unicode_digits", "\uff11\uff17", "", "malformed"},
			{"plus", "+17", "", "malformed"},
			{"negative_zero", "-0", "", "malformed"},
			{"leading_zero", "017", "", "malformed"},
			{"negative_leading_zero", "-017", "", "malformed"},
			{"minus_only", "-", "", "malformed"},
			{"fraction", "1.7", "", "malformed"},
			{"exponent", "17e0", "", "malformed"},
			{"hex", "0x11", "", "malformed"},
			{"nul", "17\x00", "", "malformed"},
			{"tab", "17\t", "", "malformed"},
			{"delete", "17\x7f", "", "malformed"},
			{"invalid_utf8", "17\xff", "", "malformed"},
			{"internal_lf", "1\n7", "", "malformed"},
			{"long_overflow", "184467440737095516160", "", "out_of_range"},
		} {
			t.Run(input+"/"+tc.name, func(t *testing.T) {
				raw := []byte(tc.raw)
				original := append([]byte(nil), raw...)
				value, state := parsePowerSupplyScalar(powerSupplyTestField(t, input), raw)
				if value != tc.value || state != tc.state || !bytes.Equal(raw, original) {
					t.Fatal("present energy/charge raw grammar, units, or byte ownership changed")
				}
			})
		}
	}
}
