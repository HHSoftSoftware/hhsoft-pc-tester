package storagetest

import (
	"context"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func typedSnapshot(rows ...inventory.Component) inventory.Snapshot {
	if rows == nil {
		rows = []inventory.Component{}
	}
	return inventory.Snapshot{SchemaVersion: 1, Components: rows}
}

func TestFixtureSourceUSBWithoutBlockDescendants(t *testing.T) {
	current := typedSnapshot(inventory.Component{Type: inventory.ComponentUSB, ID: "usb:1-2"})
	for _, discovery := range []Discovery{completeDiscovery(), completeDiscovery(physicalNode("block:a", "8:0"))} {
		got, err := Admit(context.Background(), discovery, nil, []string{"usb:1-2"}, current)
		if err != nil || got.ResolvedFixtureMask != 1 || len(got.FixtureObservations) != 1 || got.FixtureObservations[0] != (FixtureObservation{ID: "usb:1-2", Type: "usb"}) || len(got.Targets) != len(discovery.Nodes) || len(got.NodeFixtureMasks) != len(discovery.Nodes) {
			t.Fatalf("USB fixture source: %+v %v", got, err)
		}
		if len(discovery.Nodes) != 0 && (got.NodeFixtureMasks[0] != 0 || got.Targets[0].FixtureMask != 0 || !reflect.DeepEqual(got.Targets[0].Ranges, []Range{{0, 65536}, {491520, 65536}, {983040, 65536}})) {
			t.Fatalf("invented descendant or lost healthy target: %+v", got)
		}
	}
}

func TestFixtureSourceRejectsMissingAmbiguousWrongKindAndStaleRows(t *testing.T) {
	d := completeDiscovery(physicalNode("block:a", "8:0"))
	d.Nodes[0].USBAncestors = []string{"usb:1-2"}
	for _, current := range []inventory.Snapshot{
		typedSnapshot(),
		typedSnapshot(inventory.Component{Type: inventory.ComponentBlock, ID: "usb:1-2"}),
		typedSnapshot(inventory.Component{Type: inventory.ComponentUSB, ID: "usb:1-2"}, inventory.Component{Type: inventory.ComponentUSB, ID: "usb:1-2"}),
		typedSnapshot(inventory.Component{Type: inventory.ComponentUSB, ID: "usb:1-2"}, inventory.Component{Type: inventory.ComponentPCI, ID: "usb:1-2"}),
		{SchemaVersion: 2, Components: []inventory.Component{{Type: inventory.ComponentUSB, ID: "usb:1-2"}}},
		{SchemaVersion: 1, Components: nil},
	} {
		if got, err := Admit(context.Background(), d, nil, []string{"usb:1-2"}, current); err != ErrAdmission || !reflect.DeepEqual(got, Admission{}) {
			t.Fatalf("unresolved source accepted: %+v %v", got, err)
		}
	}
	valid := typedSnapshot(inventory.Component{Type: inventory.ComponentUSB, ID: "usb:1-2"})
	if _, err := Admit(context.Background(), d, nil, []string{"usb:1-2"}, valid); err != nil {
		t.Fatal(err)
	}
	if _, err := Admit(context.Background(), d, nil, []string{"usb:1-2"}, typedSnapshot()); err != ErrAdmission {
		t.Fatal("earlier source resolution reused after disappearance")
	}
}

func TestFixtureMasksPropagateOnlyActualFamiliesAndPreservePlannedScope(t *testing.T) {
	a := physicalNode("block:a", "8:0")
	a.PhysicalChildren = []string{"block:a1"}
	a.USBAncestors = []string{"usb:1-2"}
	a1 := physicalNode("block:a1", "8:1")
	a1.Kind = "partition"
	a1.Parent = "block:a"
	d := completeDiscovery(a, a1, physicalNode("block:b", "8:16"))
	current := typedSnapshot(inventory.Component{Type: inventory.ComponentUSB, ID: "usb:1-2"}, inventory.Component{Type: inventory.ComponentBlock, ID: "block:b", Attributes: []inventory.Attribute{{Name: "device_number", Value: "8:16"}}})
	fixtures := []string{"usb:1-2", "block:b"}
	got, err := Admit(context.Background(), d, []string{"block:b", "block:a"}, fixtures, current)
	if err != nil || got.ResolvedFixtureMask != 3 || !reflect.DeepEqual(got.NodeFixtureMasks, []uint8{2, 2, 1}) || !reflect.DeepEqual(got.FixtureIDs, []string{"block:b", "usb:1-2"}) {
		t.Fatalf("mask mapping: %+v %v", got, err)
	}
	if got.Targets[0].FixtureMask != 2 || got.Targets[1].FixtureMask != 1 || len(got.Targets[0].Ranges) != 0 || len(got.Targets[1].Ranges) != 0 || !reflect.DeepEqual(got.Targets[0].Exclusions, []string{"fixture", "usb"}) {
		t.Fatalf("planned exclusion lost: %+v", got.Targets)
	}
	if !reflect.DeepEqual(fixtures, []string{"usb:1-2", "block:b"}) {
		t.Fatal("caller fixture order mutated")
	}
	permuted, err := Admit(context.Background(), d, []string{"block:a", "block:b"}, []string{"block:b", "usb:1-2"}, typedSnapshot(current.Components[1], current.Components[0]))
	if err != nil || !reflect.DeepEqual(got, permuted) {
		t.Fatalf("canonical order changed: %+v %v", permuted, err)
	}
	standalone, err := Admit(context.Background(), d, nil, fixtures, current)
	if err != nil || len(standalone.Targets) != 0 || len(d.Nodes) != 3 || len(standalone.NodeFixtureMasks) != 3 {
		t.Fatalf("reserved family retained as standalone work: %+v %v", standalone, err)
	}
}
