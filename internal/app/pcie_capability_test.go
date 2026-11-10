package app_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

const pcieAppDirectory = "sys/bus/pci/devices/fixture-pci"
const pcieAppCanary = "DISCARDED-PCIE-CONSUMER-CANARY"

func pcieAppPut(t *testing.T, root, name, value string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(pcieAppDirectory+"/"+name))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func pcieAppRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(pcieAppDirectory)), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

type pcieAppOpaqueError struct{ calls *int }

func (e pcieAppOpaqueError) Error() string { (*e.calls)++; return pcieAppCanary }
func (e pcieAppOpaqueError) Is(error) bool { (*e.calls)++; return false }
func (e pcieAppOpaqueError) Unwrap() error { (*e.calls)++; return nil }

type pcieAppSource struct {
	inventory.PathSource
	reads     []string
	failSpeed bool
	callbacks int
}

func (s *pcieAppSource) ReadFile(name string) ([]byte, error) {
	s.reads = append(s.reads, name)
	if s.failSpeed && name == pcieAppDirectory+"/max_link_speed" {
		return []byte(pcieAppCanary), pcieAppOpaqueError{&s.callbacks}
	}
	return s.PathSource.ReadFile(name)
}

func pcieAppJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func pcieAppNoForbiddenReads(t *testing.T, reads []string) {
	t.Helper()
	for _, name := range reads {
		for _, forbidden := range []string{"/config", "/reset", "/retrain"} {
			if strings.Contains(name, forbidden) {
				t.Fatal("consumer fixture caused forbidden acquisition", name)
			}
		}
	}
}

func pcieAppPayload(t *testing.T, snapshot inventory.Snapshot) app.InventoryPayload {
	t.Helper()
	payload, err := app.BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestPCIeCapabilityCollectorPayloadPreservesLiteralStatesAndNoIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, speed, width, speedState, speedValue, widthState, widthValue string
		speedAbsent, widthAbsent, failSpeed                                bool
	}{
		{name: "reported", speed: "16.0 GT/s PCIe\n", width: "8\n", speedState: "reported", speedValue: "16000", widthState: "reported", widthValue: "8"},
		{name: "unknown", speed: "Unknown\n", width: "255\n", speedState: "unknown", widthState: "unknown"},
		{name: "unsupported", speed: "128.0 GT/s PCIe", width: "3", speedState: "unsupported", widthState: "unsupported"},
		{name: "malformed", speed: pcieAppCanary, width: "08", speedState: "malformed", widthState: "malformed"},
		{name: "empty", speed: "", width: "", speedState: "malformed", widthState: "malformed"},
		{name: "speed_absent", speedAbsent: true, width: "4", speedState: "unavailable", widthState: "reported", widthValue: "4"},
		{name: "width_absent", speed: "2.5 GT/s PCIe", widthAbsent: true, speedState: "reported", speedValue: "2500", widthState: "unavailable"},
		{name: "read_error", failSpeed: true, width: "16", speedState: "read_error", widthState: "reported", widthValue: "16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := pcieAppRoot(t)
			if !tc.speedAbsent {
				pcieAppPut(t, root, "max_link_speed", tc.speed)
			}
			if !tc.widthAbsent {
				pcieAppPut(t, root, "max_link_width", tc.width)
			}
			for _, name := range []string{"config", "reset"} {
				pcieAppPut(t, root, name, pcieAppCanary)
			}
			source := &pcieAppSource{PathSource: inventory.NewRootPathSource(root), failSpeed: tc.failSpeed}
			snapshot, diagnostics, err := inventory.NewLinuxCollectorWithSource(source).CollectWithDiagnostics(t.Context())
			if err != nil {
				t.Fatal("actual collector failed")
			}
			wantFailures := 0
			if tc.failSpeed {
				wantFailures = 1
			}
			if int(diagnostics.ScalarFailures) != wantFailures || source.callbacks != 0 {
				t.Fatal("diagnostics or opaque-error callback changed")
			}
			pcieAppNoForbiddenReads(t, source.reads)
			payload := pcieAppPayload(t, snapshot)
			want := []inventory.Attribute{{Name: "address", Value: "fixture-pci"}}
			if tc.speedValue != "" {
				want = append(want, inventory.Attribute{Name: "max_link_speed_mts", Value: tc.speedValue})
			}
			want = append(want, inventory.Attribute{Name: "max_link_speed_state", Value: tc.speedState})
			if tc.widthValue != "" {
				want = append(want, inventory.Attribute{Name: "max_link_width_lanes", Value: tc.widthValue})
			}
			want = append(want, inventory.Attribute{Name: "max_link_width_state", Value: tc.widthState})
			if len(payload.Components) != 1 || payload.Components[0].Type != inventory.ComponentPCI || payload.Components[0].ID != "pci:fixture-pci" || !reflect.DeepEqual(payload.Components[0].Attributes, want) {
				t.Fatal("literal PCI maximum field/state oracle failed")
			}
			if payload.Components[0].Identity != (app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) || payload.SystemFingerprint.IdentifiedParts != 0 || payload.SystemFingerprint.UnidentifiedParts != 1 || len(payload.TestResults) != 0 {
				t.Fatal("capability observation acquired identity or functional authority")
			}
			for _, value := range []any{snapshot, payload, diagnostics} {
				if bytes.Contains(pcieAppJSON(t, value), []byte(pcieAppCanary)) {
					t.Fatal("discarded producer bytes escaped")
				}
			}
		})
	}
}

func TestPCIeCurrentLinkFilesChangePayloadButLeaveGraphAndPlanExact(t *testing.T) {
	for _, state := range []string{"maxima_absent", "maxima_present"} {
		t.Run(state, func(t *testing.T) {
			root := pcieAppRoot(t)
			if state == "maxima_present" {
				pcieAppPut(t, root, "max_link_speed", "32.0 GT/s PCIe")
				pcieAppPut(t, root, "max_link_width", "16")
			}
			pcieAppPut(t, root, "current_link_speed", "2.5 GT/s PCIe")
			pcieAppPut(t, root, "current_link_width", "1")
			source := &pcieAppSource{PathSource: inventory.NewRootPathSource(root)}
			collector := inventory.NewLinuxCollectorWithSource(source)
			before, err := collector.Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			beforeReads := append([]string(nil), source.reads...)
			key := bytes.Repeat([]byte{0x39}, 32)
			left, err := plan.ObserveHardwareGraph(before, key, "power-fixture")
			if err != nil {
				t.Fatal(err)
			}
			oldPlan, err := plan.Build(before, plan.ProfileQuick, "x86_64", key, "power-fixture")
			if err != nil {
				t.Fatal(err)
			}
			pcieAppPut(t, root, "current_link_speed", pcieAppCanary)
			pcieAppPut(t, root, "current_link_width", "32")
			source.reads = nil
			after, err := collector.Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			right, err := plan.ObserveHardwareGraph(after, key, "power-fixture")
			if err != nil {
				t.Fatal(err)
			}
			newPlan, err := plan.Build(after, plan.ProfileQuick, "x86_64", key, "power-fixture")
			if err != nil {
				t.Fatal(err)
			}
			pcieAppNoForbiddenReads(t, source.reads)
			if !reflect.DeepEqual(beforeReads, source.reads) || bytes.Equal(pcieAppJSON(t, before), pcieAppJSON(t, after)) || bytes.Equal(pcieAppJSON(t, pcieAppPayload(t, before)), pcieAppJSON(t, pcieAppPayload(t, after))) || !reflect.DeepEqual(left, right) || !bytes.Equal(pcieAppJSON(t, oldPlan), pcieAppJSON(t, newPlan)) || !reflect.DeepEqual(plan.CompareHardwareGraphs(left, right), plan.HardwareDelta{}) {
				t.Fatal("current telemetry lost its observation or changed a configuration binding")
			}
			oldPayload, newPayload := pcieAppPayload(t, before), pcieAppPayload(t, after)
			if !reflect.DeepEqual(oldPayload.SystemFingerprint, newPayload.SystemFingerprint) || oldPayload.Components[0].Identity != newPayload.Components[0].Identity || len(newPayload.TestResults) != 0 || bytes.Contains(pcieAppJSON(t, newPayload), []byte(pcieAppCanary)) {
				t.Fatal("current observation acquired identity/test authority or exported malformed bytes")
			}
		})
	}
}

func TestPCIeMaximumObservationChangesGraphBindingWithoutStableIdentity(t *testing.T) {
	for _, tc := range []struct{ name, field, replacement string }{
		{"speed", "max_link_speed", "32.0 GT/s PCIe"},
		{"width", "max_link_width", "16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := pcieAppRoot(t)
			pcieAppPut(t, root, "max_link_speed", "16.0 GT/s PCIe")
			pcieAppPut(t, root, "max_link_width", "8")
			before, err := inventory.NewLinuxCollector(root).Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			key := bytes.Repeat([]byte{0x39}, 32)
			left, err := plan.ObserveHardwareGraph(before, key, "power-fixture")
			if err != nil {
				t.Fatal(err)
			}
			oldPlan, err := plan.Build(before, plan.ProfileQuick, "x86_64", key, "power-fixture")
			if err != nil {
				t.Fatal(err)
			}
			pcieAppPut(t, root, tc.field, tc.replacement)
			after, err := inventory.NewLinuxCollector(root).Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			right, err := plan.ObserveHardwareGraph(after, key, "power-fixture")
			if err != nil {
				t.Fatal(err)
			}
			newPlan, err := plan.Build(after, plan.ProfileQuick, "x86_64", key, "power-fixture")
			if err != nil {
				t.Fatal(err)
			}
			wantDelta := plan.HardwareDelta{Changed: []plan.HardwareComponentRef{{Type: inventory.ComponentPCI, ID: "pci:fixture-pci"}}}
			if left.Value == right.Value || oldPlan.ID == newPlan.ID || !reflect.DeepEqual(plan.CompareHardwareGraphs(left, right), wantDelta) || oldPlan.HardwareGraph.Value != left.Value || newPlan.HardwareGraph.Value != right.Value {
				t.Fatal("maximum capability was not bound as an observed configuration change")
			}
			if !reflect.DeepEqual(oldPlan.Tests, newPlan.Tests) || !reflect.DeepEqual(oldPlan.Capabilities, newPlan.Capabilities) || oldPlan.DefinitionsDigest != newPlan.DefinitionsDigest {
				t.Fatal("passive PCI maximum observation activated a functional test")
			}
			oldPayload, newPayload := pcieAppPayload(t, before), pcieAppPayload(t, after)
			if !reflect.DeepEqual(oldPayload.SystemFingerprint, newPayload.SystemFingerprint) || oldPayload.Components[0].Identity != newPayload.Components[0].Identity || newPayload.Components[0].Identity.Confidence != "unavailable" || len(newPayload.TestResults) != 0 {
				t.Fatal("configuration binding was promoted to stable identity or test evidence")
			}
		})
	}
}
