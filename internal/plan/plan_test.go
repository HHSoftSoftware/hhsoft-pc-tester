package plan

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestBuildFreezesDeterministicApplicablePlan(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentMemory, "memory:system", map[string]string{"total_kib": "8000000"}),
		inventory.NewComponent(inventory.ComponentCPU, "cpu:0", map[string]string{"model": "Fixture CPU"}),
		inventory.NewComponent(inventory.ComponentVideo, "video:drm:card0", map[string]string{"category": "display_controller"}),
		inventory.NewComponent(inventory.ComponentSound, "sound:card0", map[string]string{"interface": "card0"}),
		inventory.NewComponent(inventory.ComponentPCI, "pci:0000:00:02.0", map[string]string{"class": "0x030000"}),
	})
	first, err := Build(snapshot, ProfileQuick, "x86_64", key, "lab-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(snapshot, ProfileQuick, "x86_64", key, "lab-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || !first.Frozen || !strings.HasPrefix(first.ID, "sha256:") || !strings.HasPrefix(first.HardwareGraph.Value, "hmac-sha256:") {
		t.Fatalf("plan was not frozen deterministically: %#v %#v", first, second)
	}
	assertTestStatus(t, first, "graphics.quick", TestScheduled)
	assertTestStatus(t, first, "display.operator.quick", TestMissingDriver)
	assertTestStatus(t, first, "cpu.quick", TestScheduled)
	assertTestStatus(t, first, "memory.quick", TestScheduled)
	assertTestStatus(t, first, "audio.readiness.quick", TestScheduled)
	assertTestStatus(t, first, "audio.quick", TestUnsupported)
	assertTestStatus(t, first, "cpu.stress", TestExcluded)
	assertTestStatus(t, first, "camera.quick", TestNotPresent)

	changed := inventory.NewSnapshot(append(snapshot.Components,
		inventory.NewComponent(inventory.ComponentUSB, "usb:1-2", map[string]string{"vendor_id": "cafe"}),
	))
	third, err := Build(changed, ProfileQuick, "x86_64", key, "lab-1")
	if err != nil {
		t.Fatal(err)
	}
	if third.ID == first.ID || third.HardwareGraph.Value == first.HardwareGraph.Value {
		t.Fatal("hardware change did not change frozen plan binding")
	}
	down := inventory.NewSnapshot(append(snapshot.Components,
		inventory.NewComponent(inventory.ComponentNetwork, "network:eth0", map[string]string{"operational_state": "down"}),
	))
	fourth, err := Build(down, ProfileQuick, "x86_64", key, "lab-1")
	if err != nil {
		t.Fatal(err)
	}
	up := inventory.NewSnapshot(append(snapshot.Components,
		inventory.NewComponent(inventory.ComponentNetwork, "network:eth0", map[string]string{"operational_state": "up"}),
	))
	fifth, err := Build(up, ProfileQuick, "x86_64", key, "lab-1")
	if err != nil {
		t.Fatal(err)
	}
	if fourth.ID != fifth.ID || fourth.HardwareGraph.Value != fifth.HardwareGraph.Value {
		t.Fatal("volatile interface state changed the plan ID")
	}
}

func TestBuildReportsMissingDriverAndRejectsUnsafeInput(t *testing.T) {
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentPCI, "pci:0000:00:02.0", map[string]string{"class": "0x030000"}),
	})
	result, err := Build(snapshot, ProfileQuick, "x86", bytes.Repeat([]byte{1}, 32), "")
	if err != nil {
		t.Fatal(err)
	}
	assertCapabilityStatus(t, result, "graphics", CapabilityMissingDriver)
	assertTestStatus(t, result, "graphics.quick", TestMissingDriver)
	if _, err := Build(snapshot, "unsafe", "x86", bytes.Repeat([]byte{1}, 32), "lab"); err == nil {
		t.Fatal("accepted an unknown profile")
	}
	if _, err := Build(snapshot, ProfileQuick, "x86", []byte("short"), "lab"); err == nil {
		t.Fatal("accepted a short fingerprint key")
	}
}

func TestBuildReportsMissingDriverForCompositeUSBAudio(t *testing.T) {
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentUSB, "usb:1-2", map[string]string{"class": "00", "audio_interface": "true"}),
	})
	result, err := Build(snapshot, ProfileQuick, "x86_64", bytes.Repeat([]byte{1}, 32), "lab")
	if err != nil {
		t.Fatal(err)
	}
	assertCapabilityStatus(t, result, "audio", CapabilityMissingDriver)
	assertTestStatus(t, result, "audio.readiness.quick", TestMissingDriver)
}

func TestBuildExcludesModernGraphicsFromLegacyImage(t *testing.T) {
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentPCI, "pci:0000:00:02.0", map[string]string{"class": "0x030000"}),
		inventory.NewComponent(inventory.ComponentVideo, "video:drm:card0", map[string]string{"category": "display_controller"}),
		inventory.NewComponent(inventory.ComponentVideo, "video:connector:card0-vga-1", map[string]string{"category": "display_connector"}),
	})
	result, err := Build(snapshot, ProfileQuick, "legacy-x86", bytes.Repeat([]byte{1}, 32), "lab")
	if err != nil {
		t.Fatal(err)
	}
	assertTestStatus(t, result, "graphics.quick", TestUnsupported)
	assertTestStatus(t, result, "display.operator.quick", TestUnsupported)
}

func TestDefinitionValidationIsFailClosed(t *testing.T) {
	invalid := []byte(`{"capabilities":[{"id":"storage","providers":[{"type":"block"}]}],"tests":[{"id":"storage.erase","version":1,"capability":"storage","profiles":["quick"],"destructive":true,"estimated_seconds":1,"timeout_seconds":2}]}`)
	if _, _, err := loadDefinitions(invalid); err == nil {
		t.Fatal("accepted a destructive test in the quick profile")
	}
}

func TestHardwareGraphDeltaRecordsAddedRemovedAndChangedComponents(t *testing.T) {
	key := bytes.Repeat([]byte{8}, 32)
	initial, err := ObserveHardwareGraph(inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"model": "Disk A"}),
		inventory.NewComponent(inventory.ComponentUSB, "usb:1-1", map[string]string{"vendor_id": "0001"}),
	}), key, "lab-1")
	if err != nil {
		t.Fatal(err)
	}
	final, err := ObserveHardwareGraph(inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"model": "Disk B"}),
		inventory.NewComponent(inventory.ComponentPCI, "pci:0000:00:01.0", map[string]string{"class": "0x010100"}),
	}), key, "lab-1")
	if err != nil {
		t.Fatal(err)
	}
	delta := CompareHardwareGraphs(initial, final)
	if len(delta.Added) != 1 || delta.Added[0].ID != "pci:0000:00:01.0" || len(delta.Removed) != 1 || delta.Removed[0].ID != "usb:1-1" || len(delta.Changed) != 1 || delta.Changed[0].ID != "block:sda" {
		t.Fatalf("unexpected hardware delta: %+v", delta)
	}
}

func TestDecodeValidatesFrozenPlanBytes(t *testing.T) {
	result, err := Build(inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentCPU, "cpu:0", map[string]string{"model": "Fixture CPU"}),
	}), ProfileQuick, "x86_64", bytes.Repeat([]byte{9}, 32), "lab-1")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(encoded); err != nil {
		t.Fatal(err)
	}
	result.HardwareGraph.Components[0].Value = "hmac-sha256:" + strings.Repeat("0", 64)
	encoded, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(encoded); err == nil {
		t.Fatal("accepted a plan whose component binding no longer matches its ID")
	}
}

func FuzzDecodeHostilePlan(f *testing.F) {
	result, err := Build(inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentCPU, "cpu:0", map[string]string{"model": "Fixture CPU"}),
	}), ProfileQuick, "x86_64", bytes.Repeat([]byte{9}, 32), "lab-1")
	if err != nil {
		f.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(encoded)
	f.Add([]byte{})
	f.Add(append(append([]byte(nil), encoded...), []byte(`{"trailing":true}`)...))
	f.Fuzz(func(t *testing.T, value []byte) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("Decode panicked for %d hostile bytes: %v", len(value), recovered)
			}
		}()
		_, _ = Decode(value)
	})
}

func assertCapabilityStatus(t *testing.T, result Plan, id string, want CapabilityStatus) {
	t.Helper()
	for _, capability := range result.Capabilities {
		if capability.ID == id {
			if capability.Status != want {
				t.Fatalf("capability %s = %s, want %s", id, capability.Status, want)
			}
			return
		}
	}
	t.Fatalf("capability %s is absent from plan", id)
}

func assertTestStatus(t *testing.T, result Plan, id string, want TestStatus) {
	t.Helper()
	for _, test := range result.Tests {
		if test.ID == id {
			if test.Status != want {
				t.Fatalf("test %s = %s, want %s", id, test.Status, want)
			}
			return
		}
	}
	t.Fatalf("test %s is absent from plan", id)
}
