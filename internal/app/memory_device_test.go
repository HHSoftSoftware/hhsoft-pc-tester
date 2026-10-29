package app

import (
	"bytes"
	"encoding/json"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func memoryAppFixture(t *testing.T, state string) InventoryPayload {
	t.Helper()
	root := t.TempDir()
	files := map[string][]byte{"sys/class/block/sda/wwid": []byte("fixture-exterior-disk\n"), "sys/class/block/sda/device/model": []byte("Fixture disk\n")}
	if state == "present" {
		raw := []byte{17, 21, 0x71, 0x29, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x40, 9, 0, 0, 0, 34, 0, 0}
		files["sys/firmware/dmi/entries/17-42/raw"] = append(raw, []byte("PRIVATE-SERIAL\x00PRIVATE-LOCATOR\x00\x00")...)
		files["sys/firmware/dmi/entries/17-42/handle"] = []byte("FORBIDDEN-HANDLE")
		files["sys/firmware/dmi/tables/DMI"] = []byte("FORBIDDEN-TABLE")
	} else if state == "empty" {
		if err := os.MkdirAll(filepath.Join(root, "sys/firmware/dmi/entries"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, raw := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, raw, 0600); err != nil {
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
	wantCount := 1
	if state == "present" {
		wantCount = 2
	}
	if len(payload.Components) != wantCount || payload.SystemFingerprint.IdentifiedParts != 1 || payload.SystemFingerprint.UnidentifiedParts != wantCount-1 || payload.SystemFingerprint.KeyID != "hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44" || payload.SystemFingerprint.Value != "hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449" || payload.Components[0].Identity.Value != "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af" {
		t.Fatal("literal identity/count prerequisite")
	}
	if state == "present" && payload.Components[1].Identity != (ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) {
		t.Fatal("firmware row invented physical identity")
	}
	return payload
}
func memoryAppAttributes(capacity string) []inventory.Attribute {
	return []inventory.Attribute{{Name: "capacity_bytes", Value: capacity}, {Name: "capacity_bytes_state", Value: "reported"}, {Name: "form_factor", Value: "dimm"}, {Name: "form_factor_state", Value: "reported"}, {Name: "memory_type", Value: "ddr5"}, {Name: "memory_type_state", Value: "reported"}, {Name: "observation_source", Value: "smbios_type17"}}
}

func TestMemoryDeviceCollectedPayloadRetainsFiniteFieldsAndUnavailableIdentity(t *testing.T) {
	p := memoryAppFixture(t, "present")
	want := []ComponentRecord{
		{Type: inventory.ComponentBlock, ID: "block:sda", Attributes: []inventory.Attribute{{Name: "kind", Value: "device"}, {Name: "model", Value: "Fixture disk"}, {Name: "name", Value: "sda"}}, Identity: ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Source: "wwid", Value: "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af", Confidence: "durable"}},
		{Type: inventory.ComponentType("memory_device"), ID: "memory_device:1", Attributes: memoryAppAttributes("17179869184"), Identity: ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}},
	}
	if p.SchemaVersion != 1 || !reflect.DeepEqual(p.Components, want) {
		t.Fatal("exact finite public row", p.Components)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"PRIVATE", "FORBIDDEN", "fixture-exterior-disk", "17-42", "serial", "locator", "physical presence", "healthy"} {
		if bytes.Contains(raw, []byte(marker)) {
			t.Fatal("private data or unsupported verdict escaped")
		}
	}
}
func TestMemoryDeviceMissingAndEmptyKeepLiteralPayloadBytes(t *testing.T) {
	const legacy = `{"schema_version":1,"components":[{"type":"block","id":"block:sda","attributes":[{"name":"kind","value":"device"},{"name":"model","value":"Fixture disk"},{"name":"name","value":"sda"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"power-fixture","source":"wwid","value":"hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af","confidence":"durable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"power-fixture","key_id":"hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44","value":"hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449","identified_parts":1,"unidentified_parts":0}}`
	for _, state := range []string{"missing", "empty"} {
		t.Run(state, func(t *testing.T) {
			raw, err := json.Marshal(memoryAppFixture(t, state))
			if err != nil || string(raw) != legacy {
				t.Fatal("old payload changed", err, string(raw))
			}
		})
	}
}
