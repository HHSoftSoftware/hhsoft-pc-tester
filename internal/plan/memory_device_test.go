package plan

import (
	"bytes"
	"encoding/json"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func memoryPlanSnapshot() inventory.Snapshot {
	return inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentSystem, "system", map[string]string{"product_uuid": "11111111-2222-3333-4444-555555555555"}),
		inventory.NewComponent(inventory.ComponentType("memory_device"), "memory_device:1", map[string]string{"capacity_bytes": "17179869184", "capacity_bytes_state": "reported", "form_factor": "dimm", "form_factor_state": "reported", "memory_type": "ddr5", "memory_type_state": "reported", "observation_source": "smbios_type17"}),
	})
}
func TestMemoryDeviceIndependentGraphVector(t *testing.T) {
	got, err := ObserveHardwareGraph(memoryPlanSnapshot(), bytes.Repeat([]byte{'p'}, 32), "memory-test")
	want := HardwareGraph{Algorithm: "hmac-sha256", KeyEpoch: "memory-test", Value: "hmac-sha256:4f0fab6dc4ffe1f917862e4ad32784b718d132d5d259ae027148dcc4739e1365", Components: []HardwareComponent{
		{Type: inventory.ComponentType("memory_device"), ID: "memory_device:1", Value: "hmac-sha256:fbb61141a8608706eb7ce7ab847e216126c8a71383985700b964f9603612f740"},
		{Type: inventory.ComponentSystem, ID: "system", Value: "hmac-sha256:3ef7953c83044f1e0bb7c9f762fd1b2f6691ea6009e31e1a5804cb11692a0173"}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("independent literal graph", err, got)
	}
}
func TestMemoryDeviceEveryFiniteFieldChangesGraphAndFrozenPlan(t *testing.T) {
	key := bytes.Repeat([]byte{'p'}, 32)
	base := memoryPlanSnapshot()
	left, err := ObserveHardwareGraph(base, key, "memory-test")
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := Build(base, ProfileQuick, "x86_64", key, "memory-test")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ field, value string }{
		{"capacity_bytes", "8589934592"}, {"capacity_bytes_state", "unknown"}, {"form_factor", "sodimm"}, {"form_factor_state", "unsupported"},
		{"memory_type", "ddr4"}, {"memory_type_state", "unavailable"}, {"observation_source", "other-finite-source"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			changed := memoryPlanSnapshot()
			attrs := map[string]string{}
			for _, a := range changed.Components[0].Attributes {
				attrs[a.Name] = a.Value
			}
			attrs[tc.field] = tc.value
			switch tc.field {
			case "capacity_bytes_state":
				delete(attrs, "capacity_bytes")
			case "form_factor_state":
				delete(attrs, "form_factor")
			case "memory_type_state":
				delete(attrs, "memory_type")
			}
			changed.Components[0] = inventory.NewComponent(inventory.ComponentType("memory_device"), "memory_device:1", attrs)
			right, err := ObserveHardwareGraph(changed, key, "memory-test")
			if err != nil || right.Value == left.Value {
				t.Fatal("finite configuration silently volatile", err)
			}
			want := HardwareDelta{Changed: []HardwareComponentRef{{Type: inventory.ComponentType("memory_device"), ID: "memory_device:1"}}}
			if got := CompareHardwareGraphs(left, right); !reflect.DeepEqual(got, want) {
				t.Fatal("exact component delta", got)
			}
			next, err := Build(changed, ProfileQuick, "x86_64", key, "memory-test")
			if err != nil || next.ID == frozen.ID || next.DefinitionsDigest != frozen.DefinitionsDigest || !reflect.DeepEqual(next.Tests, frozen.Tests) || !reflect.DeepEqual(next.Capabilities, frozen.Capabilities) {
				t.Fatal("graph binding or existing worker selection changed", err)
			}
		})
	}
}
func TestMemoryDeviceMembershipAndUnknownMetadataRemainBound(t *testing.T) {
	key := bytes.Repeat([]byte{'p'}, 32)
	left, err := ObserveHardwareGraph(memoryPlanSnapshot(), key, "memory-test")
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"remove", "add", "unknown_attribute", "other_type_attribute"} {
		t.Run(variant, func(t *testing.T) {
			next := memoryPlanSnapshot()
			switch variant {
			case "remove":
				next.Components = next.Components[1:]
			case "add":
				next.Components = append(next.Components, inventory.NewComponent(inventory.ComponentType("memory_device"), "memory_device:2", map[string]string{"capacity_bytes_state": "unavailable", "form_factor_state": "unavailable", "memory_type_state": "unavailable", "observation_source": "smbios_type17"}))
			case "unknown_attribute":
				next.Components[0].Attributes = append(next.Components[0].Attributes, inventory.Attribute{Name: "future_field", Value: "kept"})
			default:
				next.Components[1].Attributes = append(next.Components[1].Attributes, inventory.Attribute{Name: "capacity_bytes_state", Value: "kept"})
			}
			right, err := ObserveHardwareGraph(next, key, "memory-test")
			if err != nil || right.Value == left.Value {
				t.Fatal("metadata or membership dropped", err)
			}
		})
	}
}
func TestMemoryDeviceNoObservationPreservesHistoricalGraph(t *testing.T) {
	for _, state := range []string{"missing", "empty"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			dmi := filepath.Join(root, "sys/class/dmi/id")
			if err := os.MkdirAll(dmi, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dmi, "product_uuid"), []byte("11111111-2222-3333-4444-555555555555"), 0600); err != nil {
				t.Fatal(err)
			}
			if state == "empty" {
				if err := os.MkdirAll(filepath.Join(root, "sys/firmware/dmi/entries"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := inventory.NewLinuxCollector(root).Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(snapshot)
			const want = `{"schema_version":1,"components":[{"type":"system","id":"system","attributes":[{"name":"product_uuid","value":"11111111-2222-3333-4444-555555555555"}]}]}`
			if err != nil || string(raw) != want {
				t.Fatal("old snapshot bytes changed")
			}
			graph, err := ObserveHardwareGraph(snapshot, bytes.Repeat([]byte{'p'}, 32), "memory-test")
			if err != nil || graph.Value != "hmac-sha256:9c181c7743d2bd663de28772ea8df728e71990a3918085e136cf2b9338eb12c3" {
				t.Fatal("old graph changed", err)
			}
		})
	}
}

func TestMemoryDeviceTransferRatesBindActualGraphAndPlan(t *testing.T) {
	collect := func(maximum, configured uint16) inventory.Snapshot {
		t.Helper()
		root := t.TempDir()
		path := filepath.Join(root, "sys/firmware/dmi/entries/17-0/raw")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		raw := make([]byte, 36)
		raw[0], raw[1], raw[12], raw[13], raw[14], raw[18] = 17, 34, 0, 0x40, 9, 34
		raw[21], raw[22], raw[32], raw[33] = byte(maximum), byte(maximum>>8), byte(configured), byte(configured>>8)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		snapshot, err := inventory.NewLinuxCollector(root).Collect(t.Context())
		if err != nil || len(snapshot.Components) != 1 {
			t.Fatal("actual raw prerequisite", err)
		}
		return snapshot
	}
	base := collect(4800, 6000)
	key := bytes.Repeat([]byte{'p'}, 32)
	maximum, _ := base.Components[0].Attribute("maximum_transfer_rate_mts")
	configured, _ := base.Components[0].Attribute("configured_transfer_rate_mts")
	if maximum != "4800" || configured != "6000" {
		t.Fatal("independent literal rates")
	}
	left, err := ObserveHardwareGraph(base, key, "memory-test")
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := Build(base, ProfileQuick, "x86_64", key, "memory-test")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                string
		maximum, configured uint16
	}{{"maximum_value", 6400, 6000}, {"configured_value", 4800, 6400}, {"maximum_state", 0, 6000}, {"configured_state", 4800, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			next := collect(tc.maximum, tc.configured)
			right, err := ObserveHardwareGraph(next, key, "memory-test")
			if err != nil || left.Value == right.Value {
				t.Fatal("rate observation became volatile", err)
			}
			want := HardwareDelta{Changed: []HardwareComponentRef{{Type: inventory.ComponentMemoryDevice, ID: "memory_device:1"}}}
			if got := CompareHardwareGraphs(left, right); !reflect.DeepEqual(got, want) {
				t.Fatal("rate delta changed unrelated component", got)
			}
			nextPlan, err := Build(next, ProfileQuick, "x86_64", key, "memory-test")
			if err != nil || nextPlan.ID == frozen.ID || nextPlan.DefinitionsDigest != frozen.DefinitionsDigest || !reflect.DeepEqual(nextPlan.Tests, frozen.Tests) || !reflect.DeepEqual(nextPlan.Capabilities, frozen.Capabilities) {
				t.Fatal("rate binding or worker selection changed", err)
			}
		})
	}
}
