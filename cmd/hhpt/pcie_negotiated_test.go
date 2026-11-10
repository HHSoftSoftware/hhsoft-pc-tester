//go:build linux || windows

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/comparison"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

// The command tests specify the public contract independently of the collector's
// attribute classifier. Missing values must stay missing in signed payloads.
func pcieNegotiatedCommandFields(row app.ComponentRecord) map[string]string {
	got := make(map[string]string)
	for _, a := range row.Attributes {
		switch a.Name {
		case "current_link_speed_state", "current_link_speed_mts", "current_link_width_state", "current_link_width_lanes":
			got[a.Name] = a.Value
		}
	}
	return got
}

func TestPCIeNegotiatedSignedCaptureRetainsObservationsAndStablePlan(t *testing.T) {
	for _, tc := range []struct {
		name, speed, width string
		want               map[string]string
	}{
		{"reported", "16.0 GT/s PCIe", "8", map[string]string{"current_link_speed_state": "reported", "current_link_speed_mts": "16000", "current_link_width_state": "reported", "current_link_width_lanes": "8"}},
		{"unknown_and_reserved", "Unknown", "255", map[string]string{"current_link_speed_state": "unknown", "current_link_width_state": "unsupported"}},
		{"unsupported", "128.0 GT/s PCIe", "3", map[string]string{"current_link_speed_state": "unsupported", "current_link_width_state": "unsupported"}},
		{"malformed", pcieCommandCanary, "08", map[string]string{"current_link_speed_state": "malformed", "current_link_width_state": "malformed"}},
		{"speed_only", "8.0 GT/s PCIe", "", map[string]string{"current_link_speed_state": "reported", "current_link_speed_mts": "8000", "current_link_width_state": "unavailable"}},
		{"width_only", "", "32", map[string]string{"current_link_speed_state": "unavailable", "current_link_width_state": "reported", "current_link_width_lanes": "32"}},
		{"both_absent", "", "", map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := pcieCommandFixture(t)
			for _, field := range []struct{ name, value string }{{"current_link_speed", tc.speed}, {"current_link_width", tc.width}} {
				if field.value == "" {
					if err := os.Remove(filepath.Join(f.root, filepath.FromSlash(pcieCommandDirectory+"/"+field.name))); err != nil {
						t.Fatal(err)
					}
				} else {
					pcieCommandPut(t, f.root, field.name, field.value)
				}
			}
			snapshot := pcieCommandCollect(t, f.root, false)
			excluded, err := plan.SelectableQuickTests("x86_64")
			if err != nil {
				t.Fatal(err)
			}
			current, err := plan.BuildWithSelection(snapshot, plan.ProfileQuick, "x86_64", bytes.Repeat([]byte{0x39}, 32), "power-fixture", excluded)
			if err != nil {
				t.Fatal(err)
			}
			beforePlan, _ := json.Marshal(f.frozen)
			afterPlan, _ := json.Marshal(current)
			if !bytes.Equal(beforePlan, afterPlan) {
				t.Fatal("current observations changed frozen plan bytes")
			}
			for _, trust := range []string{"external", "self_issued"} {
				t.Run(trust, func(t *testing.T) {
					_, old, _ := pcieCommandCapture(t, f.snapshot, trust == "self_issued", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", 1)
					_, artifact, _ := pcieCommandCapture(t, snapshot, trust == "self_issued", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", 1)
					row := pcieCommandRow(t, pcieCommandPayload(t, artifact))
					if !reflect.DeepEqual(pcieNegotiatedCommandFields(row), tc.want) {
						t.Fatal("signed negotiated fields differ from the finite contract", row.Attributes)
					}
					if bytes.Equal(old.InventoryJSON, artifact.InventoryJSON) || bytes.Equal(old.RenderedReport, artifact.RenderedReport) || bytes.Equal(old.RenderedPDF, artifact.RenderedPDF) || old.Envelope.ManifestDigest == artifact.Envelope.ManifestDigest {
						t.Fatal("changed telemetry disappeared from authenticated report bytes")
					}
					for name, value := range tc.want {
						if !bytes.Contains(artifact.RenderedReport, []byte("<th scope=\"row\">"+name+"</th><td>"+value+"</td>")) || !bytes.Contains(artifact.RenderedPDF, []byte("    "+name+": "+value)) {
							t.Fatal("verified view lost a negotiated field")
						}
					}
					pcieCommandPrivacy(t, artifact.InventoryJSON, artifact.RenderedReport, artifact.RenderedPDF)
				})
			}
		})
	}
}

// Rebuild only ZIP framing and its CRCs. The old manifest and signature are
// retained so rejection cannot be explained solely by a broken ZIP checksum.
func pcieNegotiatedChangedMember(t *testing.T, original []byte, name string, replacement []byte) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(original), int64(len(original)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	found := name == ""
	for _, file := range reader.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		closeErr := r.Close()
		if err != nil || closeErr != nil {
			t.Fatal("fixture member read/close failed")
		}
		if file.Name == name {
			body = replacement
			found = true
		}
		header := &zip.FileHeader{Name: file.Name, Method: zip.Store}
		header.SetMode(0600)
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		w, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := w.Write(body); err != nil || n != len(body) {
			t.Fatal("fixture member write failed")
		}
	}
	if !found || writer.Close() != nil {
		t.Fatal("fixture archive incomplete")
	}
	return out.Bytes()
}

func TestPCIeNegotiatedObservationsRemainAuthenticated(t *testing.T) {
	for _, trust := range []string{"external", "self_issued"} {
		t.Run(trust, func(t *testing.T) {
			f := pcieCommandFixture(t)
			raw, artifact, public := pcieCommandCapture(t, f.snapshot, trust == "self_issued", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", 1)
			verify := func(body []byte) error {
				var err error
				if trust == "self_issued" {
					_, err = bundle.VerifySelfIssued(bytes.NewReader(body), int64(len(body)))
				} else {
					_, err = bundle.Verify(bytes.NewReader(body), int64(len(body)), report.Keyring{"pcie-consumer-key": public})
				}
				return err
			}
			if verify(pcieNegotiatedChangedMember(t, raw, "", nil)) != nil {
				t.Fatal("unchanged independent archive framing failed")
			}
			payload := pcieCommandPayload(t, artifact)
			changed := false
			for i := range payload.Components {
				for j := range payload.Components[i].Attributes {
					a := &payload.Components[i].Attributes[j]
					if payload.Components[i].Type == inventory.ComponentPCI && a.Name == "current_link_speed_mts" {
						if a.Value != "2500" {
							t.Fatal("mutation prerequisite")
						}
						a.Value = "5000"
						changed = true
					}
				}
			}
			if !changed {
				t.Fatal("no signed telemetry to mutate")
			}
			inventoryJSON, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			html := bytes.Replace(artifact.RenderedReport, []byte("<th scope=\"row\">current_link_speed_mts</th><td>2500</td>"), []byte("<th scope=\"row\">current_link_speed_mts</th><td>5000</td>"), 1)
			pdf := bytes.Replace(artifact.RenderedPDF, []byte("    current_link_speed_mts: 2500"), []byte("    current_link_speed_mts: 5000"), 1)
			if bytes.Equal(html, artifact.RenderedReport) || bytes.Equal(pdf, artifact.RenderedPDF) {
				t.Fatal("view mutation prerequisite")
			}
			for _, item := range []struct {
				name string
				body []byte
			}{{"inventory.json", inventoryJSON}, {"views/report.html", html}, {"views/report.pdf", pdf}} {
				t.Run(item.name, func(t *testing.T) {
					if verify(pcieNegotiatedChangedMember(t, raw, item.name, item.body)) == nil {
						t.Fatal("old signature authenticated changed telemetry")
					}
				})
			}
			// The independent signer produces valid member digests/signatures. A
			// mismatched canonical view must still fail after re-signing.
			positive := extendedMemorySignedArchive(t, artifact, artifact.InventoryJSON, artifact.RenderedReport, artifact.RenderedPDF)
			if _, err := bundle.VerifySelfIssued(bytes.NewReader(positive), int64(len(positive))); err != nil {
				t.Fatal("independently signed positive prerequisite", err)
			}
			for _, item := range []struct {
				name                 string
				inventory, html, pdf []byte
			}{
				{"inventory", inventoryJSON, artifact.RenderedReport, artifact.RenderedPDF},
				{"html", artifact.InventoryJSON, html, artifact.RenderedPDF},
				{"pdf", artifact.InventoryJSON, artifact.RenderedReport, pdf},
			} {
				t.Run("resigned_"+item.name, func(t *testing.T) {
					bad := extendedMemorySignedArchive(t, artifact, item.inventory, item.html, item.pdf)
					if _, err := bundle.VerifySelfIssued(bytes.NewReader(bad), int64(len(bad))); err == nil {
						t.Fatal("signature bypassed canonical telemetry/view agreement")
					}
				})
			}
		})
	}
}

func TestPCIeNegotiatedRealComparePreservesLocationsCountsAndRejectsIdentityForgery(t *testing.T) {
	for _, mode := range []string{"changed_location", "synthetic_repeated_attributes"} {
		t.Run(mode, func(t *testing.T) {
			f := pcieCommandFixture(t)
			leftSnapshot := inventory.NewSnapshot(f.snapshot.Components)
			pcieCommandPut(t, f.root, "current_link_speed", "8.0 GT/s PCIe")
			rightSnapshot := pcieCommandCollect(t, f.root, false)
			for i := range rightSnapshot.Components {
				if rightSnapshot.Components[i].Type == inventory.ComponentPCI {
					rightSnapshot.Components[i].ID = "pci:changed-location"
					if mode == "changed_location" {
						for j := range rightSnapshot.Components[i].Attributes {
							if rightSnapshot.Components[i].Attributes[j].Name == "address" {
								rightSnapshot.Components[i].Attributes[j].Value = "changed-location"
							}
						}
					}
				}
			}
			if mode == "synthetic_repeated_attributes" {
				// Different observed IDs with identical attributes is a synthetic
				// ambiguity fixture, not a claim about physical PCI locations.
				for _, row := range f.snapshot.Components {
					if row.Type == inventory.ComponentPCI {
						row.ID = "pci:second-observation"
						leftSnapshot.Components = append(leftSnapshot.Components, row)
					}
				}
			}
			build := func(snapshot inventory.Snapshot) app.InventoryPayload {
				p, err := app.BuildInventoryPayload(inventory.NewSnapshot(snapshot.Components), bytes.Repeat([]byte{0x39}, 32), "power-fixture")
				if err != nil {
					t.Fatal(err)
				}
				return p
			}
			before, after := build(leftSnapshot), build(rightSnapshot)
			left, l, public := powerCommandSignedPayload(t, before, "pcie-before", 1)
			right, r, _ := powerCommandSignedPayload(t, after, "pcie-after", 2)
			leftPath, rightPath := filepath.Join(f.directory, "before.hhpcr"), filepath.Join(f.directory, "after.hhpcr")
			for _, item := range []struct {
				path string
				body []byte
			}{{leftPath, left}, {rightPath, right}} {
				if err := os.WriteFile(item.path, item.body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			keyPath := writeTestKey(t, f.directory, "compare.pub", public)
			args := []string{"-public-key", keyPath, "-key-id", "power-compare-key", leftPath, rightPath}
			var out, diagnostic bytes.Buffer
			if code := runCompare(t.Context(), args, &out, &diagnostic); code != 0 {
				t.Fatal("signed comparison failed", code, diagnostic.String())
			}
			var got comparison.Result
			if json.Unmarshal(out.Bytes(), &got) != nil {
				t.Fatal("invalid comparison output")
			}
			if !got.Comparable || got.Complete || got.Status != comparison.StatusInconclusive || got.Summary.Matched != 1 || got.Summary.Moved != 0 || got.Summary.Replaced != 0 || got.Summary.Added != 0 || got.Summary.Removed != 0 || got.Before.ManifestDigest != l.Envelope.ManifestDigest || got.After.ManifestDigest != r.Envelope.ManifestDigest || got.Before.ComponentCount != len(before.Components) || got.After.ComponentCount != len(after.Components) {
				t.Fatal("telemetry promoted to physical continuity or lost authenticated counts", got.Summary)
			}
			checkRows := func(want []app.ComponentRecord, old bool) {
				expected, actual := map[string][]inventory.Attribute{}, map[string][]inventory.Attribute{}
				for _, row := range want {
					if row.Type == inventory.ComponentPCI {
						expected[row.ID] = row.Attributes
					}
				}
				for _, change := range got.Unresolved {
					rows := change.After
					if old {
						rows = change.Before
					}
					for _, row := range rows {
						if row.Type == inventory.ComponentPCI {
							if _, exists := actual[row.ID]; exists {
								t.Fatal("duplicated comparison observation")
							}
							actual[row.ID] = row.Attributes
							if row.Identity != "" || row.IdentityConfidence != "unavailable" {
								t.Fatal("invented PCI identity")
							}
						}
					}
				}
				if !reflect.DeepEqual(actual, expected) {
					t.Fatal("comparison lost original PCI observations")
				}
			}
			checkRows(before.Components, true)
			checkRows(after.Components, false)
			pcieCommandPrivacy(t, out.Bytes(), diagnostic.Bytes())
			for _, side := range []string{"before", "after"} {
				original, path, session, second := after, rightPath, "pcie-after", int64(2)
				if side == "before" {
					original, path, session, second = before, leftPath, "pcie-before", 1
				}
				encoded, _ := json.Marshal(original)
				var forged app.InventoryPayload
				if json.Unmarshal(encoded, &forged) != nil {
					t.Fatal("forgery fixture clone")
				}
				for i := range forged.Components {
					if forged.Components[i].Type == inventory.ComponentPCI {
						forged.Components[i].Identity.Source = "serial"
						forged.Components[i].Identity.Value = "hmac-sha256:" + strings.Repeat("f", 64)
						forged.Components[i].Identity.Confidence = "durable"
						forged.SystemFingerprint.IdentifiedParts++
						forged.SystemFingerprint.UnidentifiedParts--
						break
					}
				}
				bad, _, _ := powerCommandSignedPayload(t, forged, session, second)
				if err := os.WriteFile(path, bad, 0600); err != nil {
					t.Fatal(err)
				}
				out.Reset()
				diagnostic.Reset()
				if code := runCompare(t.Context(), args, &out, &diagnostic); code != 1 || out.Len() != 0 || diagnostic.String() != "compare reports failed\n" {
					t.Fatal("correctly signed PCI identity forgery reached comparison", code)
				}
				restore := right
				if side == "before" {
					restore = left
				}
				if err := os.WriteFile(path, restore, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
