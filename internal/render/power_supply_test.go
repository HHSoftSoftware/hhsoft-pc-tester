package render

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func powerViewPayload(t *testing.T, state string) app.InventoryPayload {
	t.Helper()
	root := t.TempDir()
	class := filepath.Join(root, "sys", "class", "power_supply")
	if state != "missing" {
		if err := os.MkdirAll(class, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if state == "present" {
		directory := filepath.Join(class, "hid-PRIVATE-AA-BB-CC-DD-EE-FF")
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{"type": "Battery\n", "scope": "Device\n", "online": "2\n", "status": "Not charging\n", "capacity": "101\n", "cycle_count": "0\n", "energy_full": "-1\n", "energy_full_design": "0\n", "charge_full": "11\n", "charge_full_design": "10\n", "energy_now": "-17\n", "charge_now": "0\n", "serial_number": "FORBIDDEN-VIEW-SERIAL", "model_name": "FORBIDDEN-VIEW-MODEL"} {
			if err := os.WriteFile(filepath.Join(directory, name), []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	snapshot, err := inventory.NewLinuxCollector(root).Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := app.BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestPowerSupplyViewsRetainFiniteUnitsStatesAndUnavailableIdentity(t *testing.T) {
	payload := powerViewPayload(t, "present")
	if len(payload.Components) != 1 || payload.Components[0].Type != inventory.ComponentType("power_supply") || payload.Components[0].ID != "power_supply:1" || payload.Components[0].Identity != (app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) {
		t.Fatal("real collector row prerequisite")
	}
	wantAttributes := []inventory.Attribute{
		{Name: "capacity_percent_state", Value: "out_of_range"},
		{Name: "charge_full_design_uah", Value: "10"}, {Name: "charge_full_design_uah_state", Value: "reported"},
		{Name: "charge_full_uah", Value: "11"}, {Name: "charge_full_uah_state", Value: "reported"},
		{Name: "charge_now_uah", Value: "0"}, {Name: "charge_now_uah_state", Value: "reported"},
		{Name: "cycle_count_state", Value: "unavailable"},
		{Name: "energy_full_design_uwh", Value: "0"}, {Name: "energy_full_design_uwh_state", Value: "reported"},
		{Name: "energy_full_uwh", Value: "-1"}, {Name: "energy_full_uwh_state", Value: "reported"},
		{Name: "energy_now_uwh", Value: "-17"}, {Name: "energy_now_uwh_state", Value: "reported"},
		{Name: "online", Value: "2"}, {Name: "online_state", Value: "reported"},
		{Name: "present_state", Value: "unavailable"},
		{Name: "scope", Value: "Device"}, {Name: "scope_state", Value: "reported"},
		{Name: "status", Value: "Not charging"}, {Name: "status_state", Value: "reported"},
		{Name: "supply_type", Value: "Battery"}, {Name: "supply_type_state", Value: "reported"},
	}
	if !reflect.DeepEqual(payload.Components[0].Attributes, wantAttributes) {
		t.Fatal("literal finite field prerequisite")
	}
	prior, _ := json.Marshal(payload)
	for _, format := range []string{"html", "pdf"} {
		t.Run(format, func(t *testing.T) {
			var raw []byte
			var err error
			if format == "html" {
				raw, err = InventoryHTMLForVersion(payload, "0.0.1-fixture")
			} else {
				raw, err = InventoryPDFForVersion(payload, "0.0.1-fixture")
			}
			if err != nil || !bytes.Contains(raw, []byte("power_supply:1")) {
				t.Fatal("actual power view failed", err)
			}
			for _, attribute := range wantAttributes {
				literal := "    " + attribute.Name + ": " + attribute.Value
				if format == "html" {
					literal = `<th scope="row">` + attribute.Name + `</th><td>` + attribute.Value + `</td>`
				}
				if !bytes.Contains(raw, []byte(literal)) {
					t.Fatal("view lost exact field/state/unit", attribute.Name)
				}
			}
			for _, forbidden := range []string{"hid-PRIVATE-AA-BB-CC-DD-EE-FF", "FORBIDDEN-VIEW-SERIAL", "FORBIDDEN-VIEW-MODEL", "battery health", "zero wear", "system battery", "mWh", "mAh"} {
				if bytes.Contains(raw, []byte(forbidden)) {
					t.Fatal("view exported private input or invented a physical/unit claim")
				}
			}
			if format == "html" {
				if !bytes.Contains(raw, []byte(`Confidence</th><td>unavailable</td>`)) || !bytes.Contains(raw, []byte(`Fingerprint</th><td class="fingerprint">Unavailable</td>`)) {
					t.Fatal("HTML invented identifying evidence")
				}
			} else if !bytes.Contains(raw, []byte("Confidence: unavailable")) || !bytes.Contains(raw, []byte("Fingerprint: Unavailable")) {
				t.Fatal("PDF invented identifying evidence")
			}
		})
	}
	after, _ := json.Marshal(payload)
	if !bytes.Equal(prior, after) {
		t.Fatal("views mutated signed payload input")
	}
}

func TestPowerSupplyMissingAndEmptyViewsUseExactHistoricalPayload(t *testing.T) {
	const legacy = `{"schema_version":1,"components":[],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"power-fixture","key_id":"hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44","identified_parts":0,"unidentified_parts":0}}`
	var old app.InventoryPayload
	if err := json.Unmarshal([]byte(legacy), &old); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"missing", "empty"} {
		t.Run(state, func(t *testing.T) {
			payload := powerViewPayload(t, state)
			raw, err := json.Marshal(payload)
			if err != nil || string(raw) != legacy {
				t.Fatal("absence created new payload bytes", err)
			}
			for _, render := range []func(app.InventoryPayload, string) ([]byte, error){InventoryHTMLForVersion, InventoryPDFForVersion} {
				want, err := render(old, "0.0.1-fixture")
				if err != nil {
					t.Fatal(err)
				}
				got, err := render(payload, "0.0.1-fixture")
				if err != nil || !bytes.Equal(got, want) || bytes.Contains(got, []byte("power_supply")) || !bytes.Contains(got, []byte("No components reported.")) {
					t.Fatal("no-supply view changed historical payload rendering", err)
				}
			}
		})
	}
}

func TestPowerSupplyNowAllViewRoutesKeepUnitsAndUnavailableIdentity(t *testing.T) {
	payload := powerViewPayload(t, "present")
	prior, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	previous := report.PreviousReport{ManifestDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111", BundleSHA256: "sha256:2222222222222222222222222222222222222222222222222222222222222222"}
	for _, tc := range []struct {
		name                 string
		current, linked, pdf bool
	}{
		{"legacy_html", false, false, false}, {"legacy_pdf", false, false, true},
		{"legacy_linked_html", false, true, false}, {"legacy_linked_pdf", false, true, true},
		{"current_html", true, false, false}, {"current_pdf", true, false, true},
		{"current_linked_html", true, true, false}, {"current_linked_pdf", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var raw []byte
			var err error
			switch {
			case tc.current && tc.linked && tc.pdf:
				raw, err = InventoryMemoryCapacityRetestPDFForVersion(payload, "0.0.1-fixture", previous)
			case tc.current && tc.linked:
				raw, err = InventoryMemoryCapacityRetestHTMLForVersion(payload, "0.0.1-fixture", previous)
			case tc.current && tc.pdf:
				raw, err = InventoryMemoryCapacityPDFForVersion(payload, "0.0.1-fixture")
			case tc.current:
				raw, err = InventoryMemoryCapacityHTMLForVersion(payload, "0.0.1-fixture")
			case tc.linked && tc.pdf:
				raw, err = InventoryRetestPDFForVersion(payload, "0.0.1-fixture", previous)
			case tc.linked:
				raw, err = InventoryRetestHTMLForVersion(payload, "0.0.1-fixture", previous)
			case tc.pdf:
				raw, err = InventoryPDFForVersion(payload, "0.0.1-fixture")
			default:
				raw, err = InventoryHTMLForVersion(payload, "0.0.1-fixture")
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range []struct{ name, value string }{
				{"energy_now_uwh", "-17"}, {"energy_now_uwh_state", "reported"},
				{"charge_now_uah", "0"}, {"charge_now_uah_state", "reported"},
			} {
				literal := `<th scope="row">` + field.name + `</th><td>` + field.value + `</td>`
				if tc.pdf {
					literal = "    " + field.name + ": " + field.value
				}
				if bytes.Count(raw, []byte(literal)) != 1 {
					t.Fatal("view lost the literal present observation or units")
				}
			}
			confidence, identity := "Confidence</th><td>unavailable</td>", "Fingerprint</th><td class=\"fingerprint\">Unavailable</td>"
			if tc.pdf {
				confidence, identity = "Confidence: unavailable", "Fingerprint: Unavailable"
			}
			if !bytes.Contains(raw, []byte(confidence)) || !bytes.Contains(raw, []byte(identity)) {
				t.Fatal("observation invented identifying evidence")
			}
			for _, forbidden := range []string{"hid-PRIVATE-AA-BB-CC-DD-EE-FF", "FORBIDDEN-VIEW-SERIAL", "FORBIDDEN-VIEW-MODEL", "battery health", "zero wear", "estimated runtime", "mWh", "mAh", "current_now"} {
				if bytes.Contains(raw, []byte(forbidden)) {
					t.Fatal("view inferred physical truth or changed units")
				}
			}
		})
	}
	after, err := json.Marshal(payload)
	if err != nil || !bytes.Equal(prior, after) {
		t.Fatal("a view route mutated authenticated payload input")
	}
}
