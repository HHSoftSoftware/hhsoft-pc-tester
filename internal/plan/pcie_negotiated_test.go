package plan

import (
	"bytes"
	"encoding/json"
	"maps"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func pciePlanSnapshot(kind inventory.ComponentType, id string, values map[string]string) inventory.Snapshot {
	return inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(kind, id, values)})
}

func TestPCIeNegotiatedProjectionPreservesHistoricalGraphAndPlan(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	base := map[string]string{"address": "0000:00:01.0"}
	before := pciePlanSnapshot(inventory.ComponentPCI, "pci:0000:00:01.0", base)
	// Computed from literal pre-observation JSON and the unchanged HMAC domains,
	// independently of the collector, projection and production field list.
	want := HardwareGraph{Algorithm: "hmac-sha256", KeyEpoch: "pcie-fixture", Value: "hmac-sha256:abd48e71d2026f16bd5891ec97c93395caf4a0a0d41d663bca6e2393cb828b58", Components: []HardwareComponent{{Type: inventory.ComponentPCI, ID: "pci:0000:00:01.0", Value: "hmac-sha256:aa7ee0e4de9d9e7ee50af99a2ec6ffd4dd403edae607e139406a56ee37ec8b24"}}}
	graph, err := ObserveHardwareGraph(before, key, "pcie-fixture")
	if err != nil || !reflect.DeepEqual(graph, want) {
		t.Fatal("historical PCI graph vector changed", err)
	}
	frozen, err := Build(before, ProfileQuick, "x86_64", key, "pcie-fixture")
	if err != nil {
		t.Fatal(err)
	}
	frozenBytes, _ := json.Marshal(frozen)
	for _, name := range []string{"current_link_speed_state", "current_link_speed_mts", "current_link_width_state", "current_link_width_lanes"} {
		for _, value := range []string{"reported", "unavailable", "malformed", "2500", "64000", "1", "32"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				attrs := maps.Clone(base)
				attrs[name] = value
				after := pciePlanSnapshot(inventory.ComponentPCI, "pci:0000:00:01.0", attrs)
				got, err := ObserveHardwareGraph(after, key, "pcie-fixture")
				if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(CompareHardwareGraphs(graph, got), HardwareDelta{}) {
					t.Fatal("current observation changed configuration binding")
				}
				next, err := Build(after, ProfileQuick, "x86_64", key, "pcie-fixture")
				if err != nil {
					t.Fatal(err)
				}
				nextBytes, _ := json.Marshal(next)
				if !bytes.Equal(nextBytes, frozenBytes) {
					t.Fatal("current observation changed exact plan bytes")
				}
				left, _ := json.Marshal(before)
				right, _ := json.Marshal(after)
				if bytes.Equal(left, right) {
					t.Fatal("projection erased the inventory observation itself")
				}
			})
		}
	}
}

func TestPCIeNegotiatedProjectionIsFiniteAndTypeScoped(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	for _, kind := range []inventory.ComponentType{inventory.ComponentPCI, inventory.ComponentCPU} {
		for _, name := range []string{"current_link_speed_state", "current_link_speed_mts", "current_link_width_state", "current_link_width_lanes", "current_link_speed_mts_extra", "current_link_width", "current_other", "max_link_speed_state", "max_link_speed_mts", "max_link_width_state", "max_link_width_lanes", "address", "class", "vendor_id", "device_id"} {
			t.Run(string(kind)+"/"+name, func(t *testing.T) {
				left := pciePlanSnapshot(kind, "fixture", map[string]string{name: "first"})
				right := pciePlanSnapshot(kind, "fixture", map[string]string{name: "second"})
				before, err := ObserveHardwareGraph(left, key, "fixture")
				if err != nil {
					t.Fatal(err)
				}
				after, err := ObserveHardwareGraph(right, key, "fixture")
				if err != nil {
					t.Fatal(err)
				}
				// Literal requirement set, independent of the implementation helper.
				excluded := kind == inventory.ComponentPCI && (name == "current_link_speed_state" || name == "current_link_speed_mts" || name == "current_link_width_state" || name == "current_link_width_lanes")
				if reflect.DeepEqual(before, after) != excluded {
					t.Fatal("projection widened or failed to exclude a declared observation")
				}
				if !excluded && (before.Value == after.Value || before.Components[0].Value == after.Components[0].Value) {
					t.Fatal("configuration change lost per-component or whole-graph binding")
				}
			})
		}
	}
	base := pciePlanSnapshot(inventory.ComponentPCI, "pci:a", map[string]string{"address": "a"})
	graph, _ := ObserveHardwareGraph(base, key, "fixture")
	for _, after := range []inventory.Snapshot{
		pciePlanSnapshot(inventory.ComponentPCI, "pci:b", map[string]string{"address": "a"}),
		inventory.NewSnapshot(nil),
		inventory.NewSnapshot(append(base.Components, inventory.NewComponent(inventory.ComponentPCI, "pci:b", map[string]string{"address": "b"}))),
	} {
		got, err := ObserveHardwareGraph(after, key, "fixture")
		if err != nil || got.Value == graph.Value || reflect.DeepEqual(CompareHardwareGraphs(graph, got), HardwareDelta{}) {
			t.Fatal("component identity/count change was hidden")
		}
	}
}
