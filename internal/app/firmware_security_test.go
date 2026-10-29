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

func firmwareAppFixture(t *testing.T, present bool) InventoryPayload {
	t.Helper()
	root := t.TempDir()
	files := map[string][]byte{"sys/class/block/sda/wwid": []byte("fixture-exterior-disk\n"), "sys/class/block/sda/device/model": []byte("Fixture disk\n")}
	if present {
		files["sys/firmware/efi/fw_platform_size"] = []byte("64\n")
		files["sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"] = []byte{6, 0, 0, 0, 1}
		files["sys/firmware/efi/efivars/SetupMode-8be4df61-93ca-11d2-aa0d-00e098032b8c"] = []byte{6, 0, 0, 0, 1}
		files["sys/class/tpm/PRIVATE-EK-SERIAL/tpm_version_major"] = []byte("2\n")
		files["sys/class/tpm/PRIVATE-EK-SERIAL/pcrs"] = []byte("FORBIDDEN-PCR-CANARY")
		files["sys/firmware/efi/efivars/PK-PRIVATE-CERTIFICATE"] = []byte("FORBIDDEN-CERTIFICATE-CANARY")
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
	if present {
		wantCount = 3
	}
	if len(payload.Components) != wantCount || payload.SystemFingerprint.IdentifiedParts != 1 || payload.SystemFingerprint.UnidentifiedParts != wantCount-1 || payload.SystemFingerprint.KeyID != "hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44" || payload.SystemFingerprint.Value != "hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449" || payload.Components[0].Identity.Value != "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af" {
		t.Fatal("literal identity/count prerequisite")
	}
	if present {
		for _, index := range []int{1, 2} {
			if payload.Components[index].Identity != (ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) {
				t.Fatal("new identity invented")
			}
		}
	}
	return payload
}
func firmwareAppAttributes() []inventory.Attribute {
	return []inventory.Attribute{{Name: "secure_boot", Value: "1"}, {Name: "secure_boot_state", Value: "reported"}, {Name: "setup_mode_flag", Value: "1"}, {Name: "setup_mode_flag_state", Value: "reported"}, {Name: "uefi_platform_bits", Value: "64"}, {Name: "uefi_platform_bits_state", Value: "reported"}}
}

func TestFirmwareSecurityCollectedPayloadMatchesLiteralFieldsAndIdentity(t *testing.T) {
	p := firmwareAppFixture(t, true)
	want := []ComponentRecord{
		{Type: inventory.ComponentBlock, ID: "block:sda", Attributes: []inventory.Attribute{{Name: "kind", Value: "device"}, {Name: "model", Value: "Fixture disk"}, {Name: "name", Value: "sda"}}, Identity: ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Source: "wwid", Value: "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af", Confidence: "durable"}},
		{Type: inventory.ComponentType("firmware_security"), ID: "firmware_security:uefi", Attributes: firmwareAppAttributes(), Identity: ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}},
		{Type: inventory.ComponentType("tpm"), ID: "tpm:1", Attributes: []inventory.Attribute{{Name: "version_major", Value: "2"}, {Name: "version_major_state", Value: "reported"}}, Identity: ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}},
	}
	if p.SchemaVersion != 1 || !reflect.DeepEqual(p.Components, want) {
		t.Fatal("exact finite public record changed", p.Components)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE", "FORBIDDEN", "fixture-exterior-disk", "8be4df61", "efivars", "verified health"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("private data or health assertion escaped")
		}
	}
}
func TestFirmwareSecurityNoObservationPreservesLiteralPayload(t *testing.T) {
	const legacy = `{"schema_version":1,"components":[{"type":"block","id":"block:sda","attributes":[{"name":"kind","value":"device"},{"name":"model","value":"Fixture disk"},{"name":"name","value":"sda"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"power-fixture","source":"wwid","value":"hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af","confidence":"durable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"power-fixture","key_id":"hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44","value":"hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449","identified_parts":1,"unidentified_parts":0}}`
	p := firmwareAppFixture(t, false)
	raw, err := json.Marshal(p)
	if err != nil || string(raw) != legacy {
		t.Fatal("old payload bytes changed", err, string(raw))
	}
}
