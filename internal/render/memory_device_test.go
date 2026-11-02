package render

import (
	"bytes"
	"encoding/json"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func memoryViewFixture(t *testing.T, state string) app.InventoryPayload {
	t.Helper()
	root := t.TempDir()
	present := state != "missing" && state != "empty"
	files := map[string][]byte{"sys/class/block/sda/wwid": []byte("fixture-exterior-disk\n"), "sys/class/block/sda/device/model": []byte("Fixture disk\n")}
	if present {
		raw := []byte{17, 21, 0x71, 0x29, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x40, 9, 0, 0, 0, 34, 0, 0}
		if state != "present" {
			raw = append(raw, make([]byte, 13)...)
			raw[1] = 34
			raw[21], raw[22], raw[32], raw[33] = 0xc0, 0x12, 0x70, 0x17
			if state == "empty_rates" {
				raw[12], raw[13] = 0, 0
			}
			if state == "unsupported_rates" {
				raw[21], raw[22] = 0xff, 0xff
			}
			if state == "partial_rates" {
				raw = raw[:33]
				raw[1] = 33
			}
		}
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
	payload, err := app.BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	wantCount := 1
	if present {
		wantCount = 2
	}
	if len(payload.Components) != wantCount || payload.SystemFingerprint.IdentifiedParts != 1 || payload.SystemFingerprint.UnidentifiedParts != wantCount-1 || payload.SystemFingerprint.KeyID != "hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44" || payload.SystemFingerprint.Value != "hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449" || payload.Components[0].Identity.Value != "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af" {
		t.Fatal("literal identity/count prerequisite")
	}
	if present && payload.Components[1].Identity != (app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) {
		t.Fatal("firmware row invented physical identity")
	}
	return payload
}
func memoryViewAttributes(capacity string) []inventory.Attribute {
	return []inventory.Attribute{{Name: "capacity_bytes", Value: capacity}, {Name: "capacity_bytes_state", Value: "reported"}, {Name: "configured_transfer_rate_mts_state", Value: "unavailable"}, {Name: "form_factor", Value: "dimm"}, {Name: "form_factor_state", Value: "reported"}, {Name: "maximum_transfer_rate_mts_state", Value: "unavailable"}, {Name: "memory_type", Value: "ddr5"}, {Name: "memory_type_state", Value: "reported"}, {Name: "observation_source", Value: "smbios_type17"}}
}

func TestMemoryDeviceViewsPreserveFiniteFieldsWithoutPrivateIdentity(t *testing.T) {
	p := memoryViewFixture(t, "present")
	if !reflect.DeepEqual(p.Components[1].Attributes, memoryViewAttributes("17179869184")) {
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
			for _, a := range memoryViewAttributes("17179869184") {
				literal := "    " + a.Name + ": " + a.Value
				if format == "html" {
					literal = `<th scope="row">` + a.Name + `</th><td>` + a.Value + `</td>`
				}
				if !bytes.Contains(raw, []byte(literal)) {
					t.Fatal("finite field missing", a.Name)
				}
			}
			for _, literal := range []string{"memory_device:1", "unavailable"} {
				if !bytes.Contains(raw, []byte(literal)) {
					t.Fatal("row or identity lost")
				}
			}
			for _, marker := range []string{"PRIVATE", "FORBIDDEN", "17-42", "fixture-exterior-disk", "healthy", "ECC passed", "physical presence"} {
				if bytes.Contains(raw, []byte(marker)) {
					t.Fatal("private source or verdict escaped")
				}
			}
		})
	}
	after, _ := json.Marshal(p)
	if !bytes.Equal(after, original) {
		t.Fatal("renderer mutated payload")
	}
}
func TestMemoryDeviceMissingAndEmptyKeepHistoricalViews(t *testing.T) {
	const legacy = `{"schema_version":1,"components":[{"type":"block","id":"block:sda","attributes":[{"name":"kind","value":"device"},{"name":"model","value":"Fixture disk"},{"name":"name","value":"sda"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"power-fixture","source":"wwid","value":"hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af","confidence":"durable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"power-fixture","key_id":"hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44","value":"hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449","identified_parts":1,"unidentified_parts":0}}`
	var old app.InventoryPayload
	if err := json.Unmarshal([]byte(legacy), &old); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"missing", "empty"} {
		t.Run(state, func(t *testing.T) {
			current := memoryViewFixture(t, state)
			raw, _ := json.Marshal(current)
			if string(raw) != legacy {
				t.Fatal("old payload bytes")
			}
			for _, render := range []func(app.InventoryPayload, string) ([]byte, error){InventoryHTMLForVersion, InventoryPDFForVersion} {
				want, err := render(old, "0.0.1-fixture")
				if err != nil {
					t.Fatal(err)
				}
				got, err := render(current, "0.0.1-fixture")
				if err != nil || !bytes.Equal(got, want) || bytes.Contains(got, []byte("memory_device")) {
					t.Fatal("old view changed", err)
				}
			}
		})
	}
}

func TestMemoryDeviceRateViewsKeepCapacitySemantics(t *testing.T) {
	previous := report.PreviousReport{ManifestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BundleSHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	routes := []struct {
		name   string
		render func(app.InventoryPayload, string) ([]byte, error)
	}{
		{"legacy_html", InventoryHTMLForVersion}, {"legacy_pdf", InventoryPDFForVersion},
		{"legacy_linked_html", func(p app.InventoryPayload, v string) ([]byte, error) {
			return InventoryRetestHTMLForVersion(p, v, previous)
		}},
		{"legacy_linked_pdf", func(p app.InventoryPayload, v string) ([]byte, error) {
			return InventoryRetestPDFForVersion(p, v, previous)
		}},
		{"current_html", InventoryMemoryCapacityHTMLForVersion}, {"current_pdf", InventoryMemoryCapacityPDFForVersion},
		{"current_linked_html", func(p app.InventoryPayload, v string) ([]byte, error) {
			return InventoryMemoryCapacityRetestHTMLForVersion(p, v, previous)
		}},
		{"current_linked_pdf", func(p app.InventoryPayload, v string) ([]byte, error) {
			return InventoryMemoryCapacityRetestPDFForVersion(p, v, previous)
		}},
	}
	for _, state := range []string{"rates", "empty_rates", "unsupported_rates", "partial_rates"} {
		t.Run(state, func(t *testing.T) {
			payload := memoryViewFixture(t, state)
			original, _ := json.Marshal(payload)
			var historical app.InventoryPayload
			if err := json.Unmarshal(original, &historical); err != nil {
				t.Fatal(err)
			}
			var retained []inventory.Attribute
			for _, a := range historical.Components[1].Attributes {
				if a.Name != "maximum_transfer_rate_mts" && a.Name != "maximum_transfer_rate_mts_state" && a.Name != "configured_transfer_rate_mts" && a.Name != "configured_transfer_rate_mts_state" {
					retained = append(retained, a)
				}
			}
			historical.Components[1].Attributes = retained
			summary := memoryCapacityReportDocument(payload.Components)
			if !reflect.DeepEqual(summary, memoryCapacityReportDocument(historical.Components)) {
				t.Fatal("rates changed capacity projection")
			}
			if state == "empty_rates" && (summary.Unpopulated != 1 || summary.Populated != 0 || summary.FirmwareCapacity != "0 bytes" || summary.Relationship != "Not comparable from these observations.") {
				t.Fatal("rate claims changed empty-entry meaning", summary)
			}
			for _, route := range routes {
				t.Run(route.name, func(t *testing.T) {
					raw, err := route.render(payload, "0.0.1-fixture")
					if err != nil {
						t.Fatal(err)
					}
					for _, a := range payload.Components[1].Attributes {
						literal := a.Name + ": " + a.Value
						if bytes.Contains(raw, []byte("<html")) {
							literal = `<th scope="row">` + a.Name + `</th><td>` + a.Value + `</td>`
						}
						if !bytes.Contains(raw, []byte(literal)) {
							t.Fatal("canonical view lost selected field", a.Name)
						}
					}
					for _, forbidden := range []string{"PRIVATE", "FORBIDDEN", "clock_mhz", "ECC passed"} {
						if bytes.Contains(raw, []byte(forbidden)) {
							t.Fatal("rate view inferred meaning")
						}
					}
				})
			}
			after, _ := json.Marshal(payload)
			if !bytes.Equal(after, original) {
				t.Fatal("rate renderer mutated inventory")
			}
		})
	}
}
