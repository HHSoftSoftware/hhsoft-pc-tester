package bundle

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func memoryBundleFixture(t *testing.T, state string) app.InventoryPayload {
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
	payload, err := app.BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
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
	if state == "present" && payload.Components[1].Identity != (app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) {
		t.Fatal("firmware row invented physical identity")
	}
	return payload
}
func memoryBundleAttributes(capacity string) []inventory.Attribute {
	return []inventory.Attribute{{Name: "capacity_bytes", Value: capacity}, {Name: "capacity_bytes_state", Value: "reported"}, {Name: "form_factor", Value: "dimm"}, {Name: "form_factor_state", Value: "reported"}, {Name: "memory_type", Value: "ddr5"}, {Name: "memory_type_state", Value: "reported"}, {Name: "observation_source", Value: "smbios_type17"}}
}
func TestMemoryDeviceIndependentSignedProfilesRetainExactProducerBytes(t *testing.T) {
	for _, present := range []bool{false, true} {
		for _, linked := range []bool{false, true} {
			for _, self := range []bool{false, true} {
				for _, views := range []string{"none", "both"} {
					t.Run(fmt.Sprintf("present=%t/linked=%t/self=%t/%s", present, linked, self, views), func(t *testing.T) {
						state := "missing"
						if present {
							state = "present"
						}
						payload := memoryBundleFixture(t, state)
						if present && !reflect.DeepEqual(payload.Components[1].Attributes, memoryBundleAttributes("17179869184")) {
							t.Fatal("literal finite producer prerequisite")
						}
						raw, err := json.Marshal(payload)
						if err != nil {
							t.Fatal(err)
						}
						var previous *report.PreviousReport
						if linked {
							p := retestReference()
							previous = &p
						}
						var html, pdf []byte
						if views == "both" {
							if linked {
								html, err = render.InventoryRetestHTMLForVersion(payload, "0.0.1-fixture", *previous)
							} else {
								html, err = render.InventoryHTMLForVersion(payload, "0.0.1-fixture")
							}
							if err != nil {
								t.Fatal(err)
							}
							if linked {
								pdf, err = render.InventoryRetestPDFForVersion(payload, "0.0.1-fixture", *previous)
							} else {
								pdf, err = render.InventoryPDFForVersion(payload, "0.0.1-fixture")
							}
							if err != nil {
								t.Fatal(err)
							}
						}
						// This retained encoder independently writes manifest, signature framing and ZIP bytes.
						archive, envelope, keys := independentlySignedRetest(t, raw, html, pdf, previous, self, nil, nil)
						artifact, err := verifyViewFixture(archive, keys, self)
						if err != nil || !bytes.Equal(artifact.InventoryJSON, raw) || !bytes.Equal(artifact.RenderedReport, html) || !bytes.Equal(artifact.RenderedPDF, pdf) {
							t.Fatal("verified producer payload/view prerequisite", err)
						}
						if linked && (artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != *previous) || !linked && artifact.Manifest.PreviousReport != nil {
							t.Fatal("signed relation changed")
						}
						var actual bytes.Buffer
						var public []byte
						if self {
							public = keys[envelope.KeyID]
						}
						if linked {
							err = WriteRetestWithPDF(&actual, raw, envelope, html, pdf, *previous, public)
						} else if self {
							err = WriteSelfIssuedWithPDF(&actual, raw, envelope, html, pdf, public)
						} else {
							err = WriteWithPDF(&actual, raw, envelope, html, pdf)
						}
						if err != nil || !bytes.Equal(actual.Bytes(), archive) {
							t.Fatal("real producer differs from independent archive", err)
						}
						for _, secret := range []string{"PRIVATE-SERIAL", "FORBIDDEN-HANDLE", "FORBIDDEN-TABLE", "fixture-exterior-disk"} {
							if bytes.Contains(archive, []byte(secret)) {
								t.Fatal("private source bytes escaped canonical archive")
							}
						}
						if self {
							if _, err := Verify(bytes.NewReader(archive), int64(len(archive)), report.Keyring{}); err == nil {
								t.Fatal("self-issued archive silently became independently trusted")
							}
						} else {
							wrong := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x17}, 32)).Public().(ed25519.PublicKey)
							if _, err := Verify(bytes.NewReader(archive), int64(len(archive)), report.Keyring{envelope.KeyID: wrong}); err == nil {
								t.Fatal("wrong trusted signer accepted")
							}
						}
					})
				}
			}
		}
	}
}

func TestMemoryDeviceResignedViewsCannotReplaceReportedTelemetry(t *testing.T) {
	payload := memoryBundleFixture(t, "present")
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	html, err := render.InventoryHTMLForVersion(payload, "0.0.1-fixture")
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := render.InventoryPDFForVersion(payload, "0.0.1-fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, self := range []bool{false, true} {
		good, _, keys := independentlySignedRetest(t, raw, html, pdf, nil, self, nil, nil)
		if _, err := verifyViewFixture(good, keys, self); err != nil {
			t.Fatal("valid signed view prerequisite", err)
		}
		for _, format := range []string{"html", "pdf"} {
			t.Run(fmt.Sprintf("self=%t/%s", self, format), func(t *testing.T) {
				h, p := bytes.Clone(html), bytes.Clone(pdf)
				if format == "html" {
					h = bytes.Replace(h, []byte(`<th scope="row">capacity_bytes</th><td>17179869184</td>`), []byte(`<th scope="row">capacity_bytes</th><td>17179869185</td>`), 1)
				} else {
					p = bytes.Replace(p, []byte("capacity_bytes: 17179869184"), []byte("capacity_bytes: 17179869185"), 1)
				}
				if bytes.Equal(h, html) && bytes.Equal(p, pdf) {
					t.Fatal("view fault was not applied")
				}
				// Re-sign the changed views: rejection must be semantic, not a broken hash/signature.
				bad, _, badKeys := independentlySignedRetest(t, raw, h, p, nil, self, nil, nil)
				if _, err := verifyViewFixture(bad, badKeys, self); err == nil {
					t.Fatal("correctly signed substituted memory-device view accepted")
				}
			})
		}
	}
}
