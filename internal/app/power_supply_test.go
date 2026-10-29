package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

const powerAppPartHMAC = "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af"
const powerAppSystemHMAC = "hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449"
const powerAppKeyHMAC = "hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44"

func powerAppFixture(t *testing.T, supplies map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"sys/class/block/sda/wwid":         "fixture-exterior-disk\n",
		"sys/class/block/sda/device/model": "Fixture disk\n",
	}
	for name, fields := range supplies {
		for field, value := range fields {
			files["sys/class/power_supply/"+name+"/"+field] = value
		}
	}
	for name, value := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func powerAppControl() ComponentRecord {
	return ComponentRecord{Type: inventory.ComponentBlock, ID: "block:sda", Attributes: []inventory.Attribute{
		{Name: "kind", Value: "device"}, {Name: "model", Value: "Fixture disk"}, {Name: "name", Value: "sda"},
	}, Identity: ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Source: "wwid", Value: powerAppPartHMAC, Confidence: "durable"}}
}

func TestPowerSupplyCollectorPayloadUsesOnlyFinitePublicOrdinals(t *testing.T) {
	const first = "a-private-SERIAL-LOCATION-CANARY"
	const second = "z-hid-AA-BB-CC-DD-EE-FF"
	root := powerAppFixture(t, map[string]map[string]string{
		first:  {"type": "Battery\n", "scope": "System\n", "present": "1\n", "online": "2\n", "status": "Not charging\n", "capacity": "0\n", "cycle_count": "0\n", "energy_full": "-1\n", "energy_full_design": "0\n", "charge_full": "2147483647\n", "charge_full_design": "-2147483648\n", "serial_number": "FORBIDDEN-SERIAL-CANARY", "model_name": "FORBIDDEN-MODEL-CANARY", "uevent": "FORBIDDEN-UEVENT-CANARY"},
		second: {"type": "FUTURE-ENUM-CANARY\n", "scope": "Device\n", "status": "Not  charging\n", "capacity": "101\n", "online": "01\n", "manufacturer": "FORBIDDEN-MANUFACTURER-CANARY"},
	})
	snapshot, err := inventory.NewLinuxCollector(root).Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	unavailable := ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}
	want := InventoryPayload{SchemaVersion: 1, SystemFingerprint: SystemFingerprint{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", KeyID: powerAppKeyHMAC, Value: powerAppSystemHMAC, IdentifiedParts: 1, UnidentifiedParts: 2}, Components: []ComponentRecord{
		powerAppControl(),
		{Type: inventory.ComponentType("power_supply"), ID: "power_supply:1", Identity: unavailable, Attributes: []inventory.Attribute{
			{Name: "capacity_percent", Value: "0"}, {Name: "capacity_percent_state", Value: "reported"},
			{Name: "charge_full_design_uah", Value: "-2147483648"}, {Name: "charge_full_design_uah_state", Value: "reported"},
			{Name: "charge_full_uah", Value: "2147483647"}, {Name: "charge_full_uah_state", Value: "reported"},
			{Name: "cycle_count_state", Value: "unavailable"},
			{Name: "energy_full_design_uwh", Value: "0"}, {Name: "energy_full_design_uwh_state", Value: "reported"},
			{Name: "energy_full_uwh", Value: "-1"}, {Name: "energy_full_uwh_state", Value: "reported"},
			{Name: "online", Value: "2"}, {Name: "online_state", Value: "reported"},
			{Name: "present", Value: "1"}, {Name: "present_state", Value: "reported"},
			{Name: "scope", Value: "System"}, {Name: "scope_state", Value: "reported"},
			{Name: "status", Value: "Not charging"}, {Name: "status_state", Value: "reported"},
			{Name: "supply_type", Value: "Battery"}, {Name: "supply_type_state", Value: "reported"},
		}},
		{Type: inventory.ComponentType("power_supply"), ID: "power_supply:2", Identity: unavailable, Attributes: []inventory.Attribute{
			{Name: "capacity_percent_state", Value: "out_of_range"}, {Name: "charge_full_design_uah_state", Value: "unavailable"}, {Name: "charge_full_uah_state", Value: "unavailable"}, {Name: "cycle_count_state", Value: "unavailable"}, {Name: "energy_full_design_uwh_state", Value: "unavailable"}, {Name: "energy_full_uwh_state", Value: "unavailable"}, {Name: "online_state", Value: "malformed"}, {Name: "present_state", Value: "unavailable"}, {Name: "scope", Value: "Device"}, {Name: "scope_state", Value: "reported"}, {Name: "status_state", Value: "unsupported"}, {Name: "supply_type_state", Value: "unsupported"},
		}},
	}}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("finite payload or independent HMAC mismatch: %#v", payload)
	}
	for _, value := range []any{snapshot, payload} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{first, second, "FORBIDDEN-SERIAL-CANARY", "FORBIDDEN-MODEL-CANARY", "FORBIDDEN-UEVENT-CANARY", "FORBIDDEN-MANUFACTURER-CANARY", "FUTURE-ENUM-CANARY", "Not  charging"} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Fatal("private or rejected supply bytes escaped")
			}
		}
	}
}

func TestPowerSupplyMissingAndEmptyClassPreserveLiteralPayload(t *testing.T) {
	// The literal predates power rows and includes an independently fixed identity.
	const legacy = `{"schema_version":1,"components":[{"type":"block","id":"block:sda","attributes":[{"name":"kind","value":"device"},{"name":"model","value":"Fixture disk"},{"name":"name","value":"sda"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"power-fixture","source":"wwid","value":"hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af","confidence":"durable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"power-fixture","key_id":"hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44","value":"hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449","identified_parts":1,"unidentified_parts":0}}`
	for _, state := range []string{"missing", "empty"} {
		t.Run(state, func(t *testing.T) {
			root := powerAppFixture(t, nil)
			if state == "empty" {
				if err := os.MkdirAll(filepath.Join(root, "sys", "class", "power_supply"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := inventory.NewLinuxCollector(root).Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			payload, err := BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(payload)
			if err != nil || string(raw) != legacy {
				t.Fatal("no-supply historical payload changed", err, string(raw))
			}
		})
	}
}
