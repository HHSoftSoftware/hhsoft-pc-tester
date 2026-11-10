package app_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

type pcieCurrentAppSource struct {
	inventory.PathSource
	fail  bool
	calls int
}

func (s *pcieCurrentAppSource) ReadFile(name string) ([]byte, error) {
	if s.fail && name == pcieAppDirectory+"/current_link_speed" {
		return []byte("8.0 GT/s PCIe\n" + pcieAppCanary), pcieAppOpaqueError{&s.calls}
	}
	return s.PathSource.ReadFile(name)
}

func TestPCIeNegotiatedPayloadLiteralStatesAndValues(t *testing.T) {
	for _, tc := range []struct {
		name, speed, width, speedState, speedValue, widthState, widthValue string
		absent, fail                                                       bool
	}{
		{name: "reported", speed: "8.0 GT/s PCIe\n", width: "4\n", speedState: "reported", speedValue: "8000", widthState: "reported", widthValue: "4"},
		{name: "unknown_and_reserved", speed: "Unknown", width: "0", speedState: "unknown", widthState: "unsupported"},
		{name: "current_255_not_unknown", speed: "5.0 GT/s PCIe", width: "255", speedState: "reported", speedValue: "5000", widthState: "unsupported"},
		{name: "unsupported", speed: "128.0 GT/s PCIe", width: "3", speedState: "unsupported", widthState: "unsupported"},
		{name: "malformed", speed: pcieAppCanary, width: "08", speedState: "malformed", widthState: "malformed"},
		{name: "empty", speed: "", width: "", speedState: "malformed", widthState: "malformed"},
		{name: "absent", absent: true, width: "8", speedState: "unavailable", widthState: "reported", widthValue: "8"},
		{name: "read_error", fail: true, width: "16", speedState: "read_error", widthState: "reported", widthValue: "16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := pcieAppRoot(t)
			if !tc.absent {
				pcieAppPut(t, root, "current_link_speed", tc.speed)
			}
			pcieAppPut(t, root, "current_link_width", tc.width)
			source := &pcieCurrentAppSource{PathSource: inventory.NewRootPathSource(root), fail: tc.fail}
			snapshot, diagnostics, err := inventory.NewLinuxCollectorWithSource(source).CollectWithDiagnostics(t.Context())
			if err != nil || source.calls != 0 {
				t.Fatal("collection or opaque errors changed")
			}
			wantFailures := uint32(0)
			if tc.fail {
				wantFailures = 1
			}
			if diagnostics.ScalarFailures != wantFailures {
				t.Fatal("observation failure count changed")
			}
			payload := pcieAppPayload(t, snapshot)
			want := []inventory.Attribute{{Name: "address", Value: "fixture-pci"}}
			if tc.speedValue != "" {
				want = append(want, inventory.Attribute{Name: "current_link_speed_mts", Value: tc.speedValue})
			}
			want = append(want, inventory.Attribute{Name: "current_link_speed_state", Value: tc.speedState})
			if tc.widthValue != "" {
				want = append(want, inventory.Attribute{Name: "current_link_width_lanes", Value: tc.widthValue})
			}
			want = append(want, inventory.Attribute{Name: "current_link_width_state", Value: tc.widthState})
			if len(payload.Components) != 1 || !reflect.DeepEqual(payload.Components[0].Attributes, want) {
				t.Fatal("current literals did not reach public payload")
			}
			if payload.Components[0].Identity != (app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) || payload.SystemFingerprint.IdentifiedParts != 0 || payload.SystemFingerprint.UnidentifiedParts != 1 || len(payload.TestResults) != 0 {
				t.Fatal("current values acquired identity or test credit")
			}
			if bytes.Contains(pcieAppJSON(t, payload), []byte(pcieAppCanary)) {
				t.Fatal("malformed private bytes escaped")
			}
		})
	}
}
