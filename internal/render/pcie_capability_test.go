package render_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

const pcieViewDirectory = "sys/bus/pci/devices/fixture-pci"
const pcieViewCanary = "DISCARDED-PCIE-VIEW-CANARY"

type pcieViewOpaqueError struct{ calls *int }

func (e pcieViewOpaqueError) Error() string { (*e.calls)++; return pcieViewCanary }
func (e pcieViewOpaqueError) Is(error) bool { (*e.calls)++; return false }
func (e pcieViewOpaqueError) Unwrap() error { (*e.calls)++; return nil }

type pcieViewSource struct {
	inventory.PathSource
	failSpeed bool
	callbacks int
}

func (s *pcieViewSource) ReadFile(name string) ([]byte, error) {
	if s.failSpeed && name == pcieViewDirectory+"/max_link_speed" {
		return []byte(pcieViewCanary), pcieViewOpaqueError{&s.callbacks}
	}
	return s.PathSource.ReadFile(name)
}

func pcieViewPayload(t *testing.T, speed, width *string, fail bool) app.InventoryPayload {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, filepath.FromSlash(pcieViewDirectory))
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct {
		name  string
		value *string
	}{{"max_link_speed", speed}, {"max_link_width", width}} {
		if field.value != nil {
			if err := os.WriteFile(filepath.Join(directory, field.name), []byte(*field.value), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, name := range []string{"config", "reset"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(pcieViewCanary), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := &pcieViewSource{PathSource: inventory.NewRootPathSource(root), failSpeed: fail}
	snapshot, diagnostics, err := inventory.NewLinuxCollectorWithSource(source).CollectWithDiagnostics(t.Context())
	if err != nil || source.callbacks != 0 {
		t.Fatal("collector prerequisite or opaque-error safety failed")
	}
	wantFailures := 0
	if fail {
		wantFailures = 1
	}
	if int(diagnostics.ScalarFailures) != wantFailures {
		t.Fatal("read-failure prerequisite changed")
	}
	payload, err := app.BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Components) != 1 || payload.Components[0].Type != inventory.ComponentPCI || payload.Components[0].ID != "pci:fixture-pci" || payload.Components[0].Identity != (app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) || len(payload.TestResults) != 0 {
		t.Fatal("real collector/public payload prerequisite failed")
	}
	return payload
}

type pcieViewRenderer struct {
	name   string
	html   bool
	render func(app.InventoryPayload) ([]byte, error)
}

func pcieViewRenderers() []pcieViewRenderer {
	previous := report.PreviousReport{
		ManifestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		BundleSHA256:   "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	return []pcieViewRenderer{
		{"legacy_html", true, func(p app.InventoryPayload) ([]byte, error) {
			return render.InventoryHTMLForVersion(p, "0.0.1-fixture")
		}},
		{"legacy_pdf", false, func(p app.InventoryPayload) ([]byte, error) { return render.InventoryPDFForVersion(p, "0.0.1-fixture") }},
		{"legacy_linked_html", true, func(p app.InventoryPayload) ([]byte, error) {
			return render.InventoryRetestHTMLForVersion(p, "0.0.1-fixture", previous)
		}},
		{"legacy_linked_pdf", false, func(p app.InventoryPayload) ([]byte, error) {
			return render.InventoryRetestPDFForVersion(p, "0.0.1-fixture", previous)
		}},
		{"current_html", true, func(p app.InventoryPayload) ([]byte, error) {
			return render.InventoryMemoryCapacityHTMLForVersion(p, "0.0.1-fixture")
		}},
		{"current_pdf", false, func(p app.InventoryPayload) ([]byte, error) {
			return render.InventoryMemoryCapacityPDFForVersion(p, "0.0.1-fixture")
		}},
		{"current_linked_html", true, func(p app.InventoryPayload) ([]byte, error) {
			return render.InventoryMemoryCapacityRetestHTMLForVersion(p, "0.0.1-fixture", previous)
		}},
		{"current_linked_pdf", false, func(p app.InventoryPayload) ([]byte, error) {
			return render.InventoryMemoryCapacityRetestPDFForVersion(p, "0.0.1-fixture", previous)
		}},
	}
}

func TestPCIeCapabilityAllViewProfilesRetainLiteralStatesAndNoHealthVerdict(t *testing.T) {
	for _, tc := range []struct {
		name, speed, width, speedState, speedValue, widthState, widthValue string
		absentSpeed, failSpeed                                             bool
	}{
		{name: "reported", speed: "64.0 GT/s PCIe", width: "32", speedState: "reported", speedValue: "64000", widthState: "reported", widthValue: "32"},
		{name: "unknown", speed: "Unknown", width: "255", speedState: "unknown", widthState: "unknown"},
		{name: "unsupported", speed: "128.0 GT/s PCIe", width: "3", speedState: "unsupported", widthState: "unsupported"},
		{name: "malformed", speed: pcieViewCanary, width: "08", speedState: "malformed", widthState: "malformed"},
		{name: "unavailable", absentSpeed: true, width: "4", speedState: "unavailable", widthState: "reported", widthValue: "4"},
		{name: "read_error", failSpeed: true, width: "16", speedState: "read_error", widthState: "reported", widthValue: "16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			speed := &tc.speed
			if tc.absentSpeed {
				speed = nil
			}
			payload := pcieViewPayload(t, speed, &tc.width, tc.failSpeed)
			want := []inventory.Attribute{{Name: "address", Value: "fixture-pci"}}
			if tc.speedValue != "" {
				want = append(want, inventory.Attribute{Name: "max_link_speed_mts", Value: tc.speedValue})
			}
			want = append(want, inventory.Attribute{Name: "max_link_speed_state", Value: tc.speedState})
			if tc.widthValue != "" {
				want = append(want, inventory.Attribute{Name: "max_link_width_lanes", Value: tc.widthValue})
			}
			want = append(want, inventory.Attribute{Name: "max_link_width_state", Value: tc.widthState})
			if !reflect.DeepEqual(payload.Components[0].Attributes, want) {
				t.Fatal("literal state/value prerequisite failed")
			}
			before, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			for _, view := range pcieViewRenderers() {
				t.Run(view.name, func(t *testing.T) {
					raw, err := view.render(payload)
					if err != nil {
						t.Fatal(err)
					}
					for _, attribute := range want {
						literal := "    " + attribute.Name + ": " + attribute.Value
						if view.html {
							literal = "<th scope=\"row\">" + attribute.Name + "</th><td>" + attribute.Value + "</td>"
						}
						if !bytes.Contains(raw, []byte(literal)) {
							t.Fatal("generic view lost exact PCI field/state", attribute.Name)
						}
					}
					for _, forbidden := range []string{pcieViewCanary, "PCIe healthy", "PCIe bandwidth passed"} {
						if bytes.Contains(raw, []byte(forbidden)) {
							t.Fatal("view exposed discarded data or invented a PCIe verdict")
						}
					}
					if tc.speedValue == "" && bytes.Contains(raw, []byte("max_link_speed_mts")) {
						t.Fatal("nonreported speed acquired a numeric value")
					}
					if tc.widthValue == "" && bytes.Contains(raw, []byte("max_link_width_lanes")) {
						t.Fatal("nonreported width acquired a numeric value")
					}
					if view.html {
						if !bytes.Contains(raw, []byte("Confidence</th><td>unavailable</td>")) || !bytes.Contains(raw, []byte("Fingerprint</th><td class=\"fingerprint\">Unavailable</td>")) {
							t.Fatal("HTML invented PCI identity")
						}
					} else if !bytes.Contains(raw, []byte("Confidence: unavailable")) || !bytes.Contains(raw, []byte("Fingerprint: Unavailable")) {
						t.Fatal("PDF invented PCI identity")
					}
				})
			}
			after, err := json.Marshal(payload)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("view changed original signed inventory")
			}
		})
	}
}

func TestPCIeCleanAbsencePreservesAllExistingViewProfileBytes(t *testing.T) {
	// Fixed pre-capability public PCI shape; both current-link files are absent.
	const legacy = "{\"schema_version\":1,\"components\":[{\"type\":\"pci\",\"id\":\"pci:fixture-pci\",\"attributes\":[{\"name\":\"address\",\"value\":\"fixture-pci\"}],\"identity\":{\"algorithm\":\"hmac-sha256\",\"key_epoch\":\"power-fixture\",\"confidence\":\"unavailable\"}}],\"system_fingerprint\":{\"algorithm\":\"hmac-sha256\",\"key_epoch\":\"power-fixture\",\"key_id\":\"hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44\",\"identified_parts\":0,\"unidentified_parts\":1}}"
	var old app.InventoryPayload
	if err := json.Unmarshal([]byte(legacy), &old); err != nil {
		t.Fatal(err)
	}
	payload := pcieViewPayload(t, nil, nil, false)
	raw, err := json.Marshal(payload)
	if err != nil || string(raw) != legacy {
		t.Fatal("clean absence changed exact historical public PCI payload")
	}
	for _, view := range pcieViewRenderers() {
		t.Run(view.name, func(t *testing.T) {
			want, err := view.render(old)
			if err != nil {
				t.Fatal(err)
			}
			got, err := view.render(payload)
			if err != nil || !bytes.Equal(got, want) || bytes.Contains(got, []byte("max_link_")) || bytes.Contains(got, []byte(pcieViewCanary)) {
				t.Fatal("clean absence changed existing renderer/profile output")
			}
		})
	}
}
