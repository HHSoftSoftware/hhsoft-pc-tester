package bundle

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func powerBundlePayload(t *testing.T, withSupply bool) app.InventoryPayload {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{"sys/class/block/sda/wwid": "fixture-exterior-disk\n", "sys/class/block/sda/device/model": "Fixture disk\n"}
	if withSupply {
		for name, value := range map[string]string{"type": "Battery\n", "scope": "Device\n", "online": "2\n", "status": "Full\n", "capacity": "100\n", "cycle_count": "0\n", "energy_full": "11\n", "energy_full_design": "10\n", "charge_full": "-1\n", "charge_full_design": "0\n", "energy_now": "17\n", "charge_now": "-19\n", "serial_number": "BUNDLE-PRIVATE-SERIAL", "uevent": "BUNDLE-PRIVATE-UEVENT"} {
			files["sys/class/power_supply/hid-PRIVATE-AA-BB-CC-DD-EE-FF/"+name] = value
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
	snapshot, err := inventory.NewLinuxCollector(root).Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := app.BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	count := 1
	if withSupply {
		count = 2
	}
	if len(payload.Components) != count || payload.SystemFingerprint.KeyID != "hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44" || payload.SystemFingerprint.Value != "hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449" || payload.Components[0].Identity.Value != "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af" || payload.SystemFingerprint.IdentifiedParts != 1 || payload.SystemFingerprint.UnidentifiedParts != count-1 {
		t.Fatal("literal identified control prerequisite")
	}
	if withSupply && (payload.Components[1].ID != "power_supply:1" || payload.Components[1].Identity != (app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"})) {
		t.Fatal("power identity prerequisite")
	}
	return payload
}

func TestPowerSupplyIndependentSignedProfilesRetainExactProducerBytes(t *testing.T) {
	for _, supply := range []bool{false, true} {
		for _, linked := range []bool{false, true} {
			for _, self := range []bool{false, true} {
				for _, views := range []string{"none", "both"} {
					t.Run(fmt.Sprintf("supply=%t/linked=%t/self=%t/%s", supply, linked, self, views), func(t *testing.T) {
						payload := powerBundlePayload(t, supply)
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
						for _, secret := range []string{"hid-PRIVATE-AA-BB-CC-DD-EE-FF", "BUNDLE-PRIVATE-SERIAL", "BUNDLE-PRIVATE-UEVENT", "fixture-exterior-disk"} {
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

func TestPowerSupplyResignedViewsCannotReplaceReportedTelemetry(t *testing.T) {
	payload := powerBundlePayload(t, true)
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
					h = bytes.Replace(h, []byte(`<th scope="row">online</th><td>2</td>`), []byte(`<th scope="row">online</th><td>1</td>`), 1)
				} else {
					p = bytes.Replace(p, []byte("online: 2"), []byte("online: 1"), 1)
				}
				if bytes.Equal(h, html) && bytes.Equal(p, pdf) {
					t.Fatal("view fault was not applied")
				}
				// Re-sign the changed views: rejection must be semantic, not a broken hash/signature.
				bad, _, badKeys := independentlySignedRetest(t, raw, h, p, nil, self, nil, nil)
				if _, err := verifyViewFixture(bad, badKeys, self); err == nil {
					t.Fatal("correctly signed substituted telemetry view accepted")
				}
			})
		}
	}
}

func TestPowerSupplyNowCurrentSignedProducersAndConsumers(t *testing.T) {
	for _, mode := range capacityProfileModes() {
		for _, views := range []string{"both", "html", "pdf"} {
			t.Run(mode.name+"/"+views, func(t *testing.T) {
				payload := powerBundlePayload(t, true)
				attributes := make(map[string]string)
				for _, a := range payload.Components[1].Attributes {
					attributes[a.Name] = a.Value
				}
				for name, value := range map[string]string{"energy_now_uwh": "17", "energy_now_uwh_state": "reported", "charge_now_uah": "-19", "charge_now_uah_state": "reported"} {
					if attributes[name] != value {
						t.Fatal("literal collected unit prerequisite")
					}
				}
				raw, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				previous := capacityProfilePrevious(mode.linked)
				html, pdf := capacityProfileViews(t, payload, previous, true)
				if views == "html" {
					pdf = nil
				}
				if views == "pdf" {
					html = nil
				}
				spec := capacityProfileSpec{current: true, self: mode.self, previous: previous}
				encoded, envelope, keys := capacityProfileSigned(t, raw, html, pdf, spec)
				artifact := capacityProfileAccepted(t, encoded, keys, mode.self)
				if artifact.Manifest.Schema != capacityProfileSchema(true, mode.linked) || artifact.Manifest.ViewProfile != "hhsoft.pc-tester.memory-capacity-view/v1" || !bytes.Equal(artifact.InventoryJSON, raw) || !bytes.Equal(artifact.RenderedReport, html) || !bytes.Equal(artifact.RenderedPDF, pdf) {
					t.Fatal("current authenticated members or fixed profile changed")
				}
				if mode.linked && (artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != *previous) || !mode.linked && artifact.Manifest.PreviousReport != nil {
					t.Fatal("exact signed relation changed")
				}
				var embedded []byte
				if mode.self {
					embedded = keys[envelope.KeyID]
				}
				digest, err := MemoryCapacityManifestDigestWithPDF(raw, html, pdf, embedded, previous)
				if err != nil || digest != envelope.ManifestDigest {
					t.Fatal("current manifest disagrees with independent encoder")
				}
				var actual bytes.Buffer
				if err := WriteMemoryCapacityWithPDF(&actual, raw, envelope, html, pdf, embedded, previous); err != nil || !bytes.Equal(actual.Bytes(), encoded) {
					t.Fatal("current producer differs from independent signed archive", err)
				}
				if mode.self {
					if _, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), nil); err == nil {
						t.Fatal("self-issued bytes acquired independent trust")
					}
				} else {
					wrong := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x17}, 32)).Public().(ed25519.PublicKey)
					if bytes.Equal(wrong, keys[envelope.KeyID]) {
						t.Fatal("wrong signer prerequisite")
					}
					if _, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{envelope.KeyID: wrong}); err == nil {
						t.Fatal("wrong trusted signer accepted current bytes")
					}
					if _, err := VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded))); err == nil {
						t.Fatal("external bytes acquired embedded trust")
					}
				}
				for _, forbidden := range []string{"hid-PRIVATE-AA-BB-CC-DD-EE-FF", "BUNDLE-PRIVATE-SERIAL", "BUNDLE-PRIVATE-UEVENT", "fixture-exterior-disk"} {
					if bytes.Contains(encoded, []byte(forbidden)) {
						t.Fatal("private fixture input reached current archive")
					}
				}
			})
		}
	}
}

func TestPowerSupplyNowCurrentSignedViewSubstitutionRejected(t *testing.T) {
	for _, mode := range capacityProfileModes() {
		for _, field := range []struct{ name, value, changed string }{
			{"energy_now_uwh", "17", "18"}, {"energy_now_uwh_state", "reported", "unavailable"},
			{"charge_now_uah", "-19", "-18"}, {"charge_now_uah_state", "reported", "unavailable"},
		} {
			for _, format := range []string{"html", "pdf"} {
				t.Run(mode.name+"/"+field.name+"/"+format, func(t *testing.T) {
					payload := powerBundlePayload(t, true)
					raw, err := json.Marshal(payload)
					if err != nil {
						t.Fatal(err)
					}
					previous := capacityProfilePrevious(mode.linked)
					html, pdf := capacityProfileViews(t, payload, previous, true)
					spec := capacityProfileSpec{current: true, self: mode.self, previous: previous}
					good, _, keys := capacityProfileSigned(t, raw, html, pdf, spec)
					capacityProfileAccepted(t, good, keys, mode.self)
					h, p := bytes.Clone(html), bytes.Clone(pdf)
					old := `<th scope="row">` + field.name + `</th><td>` + field.value + `</td>`
					next := `<th scope="row">` + field.name + `</th><td>` + field.changed + `</td>`
					if format == "html" {
						if bytes.Count(h, []byte(old)) != 1 {
							t.Fatal("unique HTML mutation prerequisite")
						}
						h = bytes.Replace(h, []byte(old), []byte(next), 1)
					} else {
						old, next = "    "+field.name+": "+field.value, "    "+field.name+": "+field.changed
						if bytes.Count(p, []byte(old)) != 1 {
							t.Fatal("unique PDF mutation prerequisite")
						}
						p = bytes.Replace(p, []byte(old), []byte(next), 1)
					}
					if bytes.Equal(h, html) && bytes.Equal(p, pdf) {
						t.Fatal("ineffective view mutation")
					}
					bad, envelope, badKeys := capacityProfileSigned(t, raw, h, p, spec)
					capacityProfileRejected(t, bad, badKeys, mode.self)
					var embedded []byte
					if mode.self {
						embedded = badKeys[envelope.KeyID]
					}
					if _, err := MemoryCapacityManifestDigestWithPDF(raw, h, p, embedded, previous); err == nil {
						t.Fatal("current digest admitted substituted observations")
					}
					var output bytes.Buffer
					if err := WriteMemoryCapacityWithPDF(&output, raw, envelope, h, p, embedded, previous); err == nil || output.Len() != 0 {
						t.Fatal("current writer published substituted observations")
					}
				})
			}
		}
	}
}
