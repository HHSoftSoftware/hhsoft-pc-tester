package render

import (
	"bytes"
	"encoding/json"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func firmwareViewFixture(t *testing.T, present bool) app.InventoryPayload {
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
	payload, err := app.BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
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
			if payload.Components[index].Identity != (app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) {
				t.Fatal("new identity invented")
			}
		}
	}
	return payload
}
func firmwareViewAttributes() []inventory.Attribute {
	return []inventory.Attribute{{Name: "secure_boot", Value: "1"}, {Name: "secure_boot_state", Value: "reported"}, {Name: "setup_mode_flag", Value: "1"}, {Name: "setup_mode_flag_state", Value: "reported"}, {Name: "uefi_platform_bits", Value: "64"}, {Name: "uefi_platform_bits_state", Value: "reported"}}
}

func TestFirmwareSecurityViewsPreserveFlagsAndUnavailableIdentity(t *testing.T) {
	p := firmwareViewFixture(t, true)
	if !reflect.DeepEqual(p.Components[1].Attributes, firmwareViewAttributes()) {
		t.Fatal("literal field prerequisite")
	}
	original, _ := json.Marshal(p)
	for _, format := range []string{"html", "pdf"} {
		t.Run(format, func(t *testing.T) {
			var raw []byte
			var err error
			if format == "html" {
				raw, err = InventoryHTMLForVersion(p, "0.0.1-fixture")
			} else {
				raw, err = InventoryPDFForVersion(p, "0.0.1-fixture")
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range append(firmwareViewAttributes(), inventory.Attribute{Name: "version_major", Value: "2"}, inventory.Attribute{Name: "version_major_state", Value: "reported"}) {
				literal := "    " + a.Name + ": " + a.Value
				if format == "html" {
					literal = `<th scope="row">` + a.Name + `</th><td>` + a.Value + `</td>`
				}
				if !bytes.Contains(raw, []byte(literal)) {
					t.Fatal("finite field lost", a.Name)
				}
			}
			for _, literal := range []string{"firmware_security:uefi", "tpm:1", "unavailable"} {
				if !bytes.Contains(raw, []byte(literal)) {
					t.Fatal("row/identity lost")
				}
			}
			for _, secret := range []string{"PRIVATE", "FORBIDDEN", "fixture-exterior-disk", "verified health", "TPM trusted", "attestation passed"} {
				if bytes.Contains(raw, []byte(secret)) {
					t.Fatal("privacy or verdict")
				}
			}
		})
	}
	after, _ := json.Marshal(p)
	if !bytes.Equal(original, after) {
		t.Fatal("renderer mutated payload")
	}
}
func TestFirmwareSecurityNoObservationKeepsHistoricalViews(t *testing.T) {
	const legacy = `{"schema_version":1,"components":[{"type":"block","id":"block:sda","attributes":[{"name":"kind","value":"device"},{"name":"model","value":"Fixture disk"},{"name":"name","value":"sda"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"power-fixture","source":"wwid","value":"hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af","confidence":"durable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"power-fixture","key_id":"hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44","value":"hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449","identified_parts":1,"unidentified_parts":0}}`
	var old app.InventoryPayload
	if err := json.Unmarshal([]byte(legacy), &old); err != nil {
		t.Fatal(err)
	}
	current := firmwareViewFixture(t, false)
	raw, _ := json.Marshal(current)
	if string(raw) != legacy {
		t.Fatal("old canonical bytes")
	}
	for _, render := range []func(app.InventoryPayload, string) ([]byte, error){InventoryHTMLForVersion, InventoryPDFForVersion} {
		want, err := render(old, "0.0.1-fixture")
		if err != nil {
			t.Fatal(err)
		}
		got, err := render(current, "0.0.1-fixture")
		if err != nil || !bytes.Equal(got, want) || bytes.Contains(got, []byte("firmware_security")) {
			t.Fatal("old view changed", err)
		}
	}
}
