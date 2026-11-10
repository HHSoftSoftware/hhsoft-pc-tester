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
)

type pcieCurrentViewSource struct {
	inventory.PathSource
	fail  bool
	calls int
}

func (s *pcieCurrentViewSource) ReadFile(name string) ([]byte, error) {
	if s.fail && name == pcieViewDirectory+"/current_link_speed" {
		return []byte(pcieViewCanary), pcieViewOpaqueError{&s.calls}
	}
	return s.PathSource.ReadFile(name)
}

func TestPCIeNegotiatedAllEightViewsRetainAuthenticatedObservations(t *testing.T) {
	for _, tc := range []struct {
		name, speed, width, speedState, speedValue, widthState, widthValue string
		absent, fail                                                       bool
	}{
		{name: "reported", speed: "2.5 GT/s PCIe", width: "1", speedState: "reported", speedValue: "2500", widthState: "reported", widthValue: "1"},
		{name: "unknown_unsupported", speed: "Unknown", width: "255", speedState: "unknown", widthState: "unsupported"},
		{name: "malformed", speed: pcieViewCanary, width: "08", speedState: "malformed", widthState: "malformed"},
		{name: "unavailable", absent: true, width: "4", speedState: "unavailable", widthState: "reported", widthValue: "4"},
		{name: "read_error", fail: true, width: "16", speedState: "read_error", widthState: "reported", widthValue: "16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			base := filepath.Join(root, filepath.FromSlash(pcieViewDirectory))
			if err := os.MkdirAll(base, 0700); err != nil {
				t.Fatal(err)
			}
			fields := map[string]string{"current_link_width": tc.width}
			if !tc.absent {
				fields["current_link_speed"] = tc.speed
			}
			for name, value := range fields {
				if err := os.WriteFile(filepath.Join(base, name), []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			source := &pcieCurrentViewSource{PathSource: inventory.NewRootPathSource(root), fail: tc.fail}
			snapshot, err := inventory.NewLinuxCollectorWithSource(source).Collect(t.Context())
			if err != nil || source.calls != 0 {
				t.Fatal("actual collector prerequisite")
			}
			payload, err := app.BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
			if err != nil {
				t.Fatal(err)
			}
			want := []inventory.Attribute{{Name: "address", Value: "fixture-pci"}}
			if tc.speedValue != "" {
				want = append(want, inventory.Attribute{Name: "current_link_speed_mts", Value: tc.speedValue})
			}
			want = append(want, inventory.Attribute{Name: "current_link_speed_state", Value: tc.speedState})
			if tc.widthValue != "" {
				want = append(want, inventory.Attribute{Name: "current_link_width_lanes", Value: tc.widthValue})
			}
			want = append(want, inventory.Attribute{Name: "current_link_width_state", Value: tc.widthState})
			if !reflect.DeepEqual(payload.Components[0].Attributes, want) {
				t.Fatal("literal current-field oracle failed")
			}
			original, _ := json.Marshal(payload)
			for _, view := range pcieViewRenderers() {
				t.Run(view.name, func(t *testing.T) {
					raw, err := view.render(payload)
					if err != nil {
						t.Fatal(err)
					}
					for _, a := range want {
						literal := "    " + a.Name + ": " + a.Value
						if view.html {
							literal = "<th scope=\"row\">" + a.Name + "</th><td>" + a.Value + "</td>"
						}
						if !bytes.Contains(raw, []byte(literal)) {
							t.Fatal("view omitted exact current observation", a.Name)
						}
					}
					for _, forbidden := range []string{pcieViewCanary, "PCIe healthy", "PCIe bandwidth passed", "link degraded"} {
						if bytes.Contains(raw, []byte(forbidden)) {
							t.Fatal("raw bytes or health inference escaped")
						}
					}
					if tc.speedValue == "" && bytes.Contains(raw, []byte("current_link_speed_mts")) {
						t.Fatal("nonreported speed became numeric")
					}
					if tc.widthValue == "" && bytes.Contains(raw, []byte("current_link_width_lanes")) {
						t.Fatal("nonreported width became numeric")
					}
					var changed app.InventoryPayload
					if err := json.Unmarshal(original, &changed); err != nil {
						t.Fatal(err)
					}
					for i := range changed.Components[0].Attributes {
						if changed.Components[0].Attributes[i].Name == "current_link_width_state" {
							changed.Components[0].Attributes[i].Value = "unavailable"
						}
					}
					attributes := changed.Components[0].Attributes[:0]
					for _, attribute := range changed.Components[0].Attributes {
						if attribute.Name != "current_link_width_lanes" {
							attributes = append(attributes, attribute)
						}
					}
					changed.Components[0].Attributes = attributes
					changedView, err := view.render(changed)
					if err != nil || bytes.Equal(raw, changedView) {
						t.Fatal("view erased a changed current observation")
					}
				})
			}
			final, _ := json.Marshal(payload)
			if !bytes.Equal(original, final) {
				t.Fatal("renderer modified authenticated observation")
			}
		})
	}
}
