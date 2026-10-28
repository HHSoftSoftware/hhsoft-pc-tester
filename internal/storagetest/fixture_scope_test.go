package storagetest

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestStandaloneFixtureScopeKeepsNodesAndOrdinaryUSBExclusions(t *testing.T) {
	d := completeDiscovery(physicalNode("block:a", "8:0"), physicalNode("block:b", "8:16"))
	current := typedSnapshot(inventory.Component{Type: inventory.ComponentBlock, ID: "block:a", Attributes: []inventory.Attribute{{Name: "device_number", Value: "8:0"}}})
	got, err := Admit(context.Background(), d, nil, []string{"block:a"}, current)
	if err != nil || !reflect.DeepEqual(got.RequestedIDs, []string{"block:b"}) || !reflect.DeepEqual(got.NodeFixtureMasks, []uint8{1, 0}) || len(got.Targets) != 1 || len(got.Targets[0].Ranges) != 3 || len(d.Nodes) != 2 {
		t.Fatalf("standalone fixture scope: %+v %v", got, err)
	}
	d.Nodes[0].USBAncestors = []string{"usb:1-2"}
	got, err = Admit(context.Background(), d, nil, []string{}, typedSnapshot())
	if err != nil || !reflect.DeepEqual(got.RequestedIDs, []string{"block:a", "block:b"}) || !reflect.DeepEqual(got.Targets[0].Exclusions, []string{"usb"}) || len(got.Targets[1].Ranges) != 3 {
		t.Fatalf("ordinary USB exclusion changed standalone scope: %+v %v", got, err)
	}
}

func TestFixtureResolutionBoundsOnlySelectedFixturesAndPreservesCancellation(t *testing.T) {
	rows := make([]inventory.Component, 0, 521)
	for i := 0; i < 513; i++ {
		rows = append(rows, inventory.Component{Type: inventory.ComponentCPU, ID: fmt.Sprintf("cpu:%d", i)})
	}
	fixtures := make([]string, 8)
	for i := range fixtures {
		fixtures[i] = fmt.Sprintf("usb:1-%d", i)
		rows = append(rows, inventory.Component{Type: inventory.ComponentUSB, ID: fixtures[i]})
	}
	d := completeDiscovery(physicalNode("block:a", "8:0"))
	current := typedSnapshot(rows...)
	got, err := Admit(context.Background(), d, nil, fixtures, current)
	if err != nil || got.ResolvedFixtureMask != 255 || len(got.FixtureObservations) != 8 || !reflect.DeepEqual(got.NodeFixtureMasks, []uint8{0}) || len(got.Targets) != 1 || len(got.Targets[0].Ranges) != 3 {
		t.Fatalf("selected fixture bounds lost unrelated instances: %+v %v", got, err)
	}
	for _, invalid := range [][]string{append(append([]string{}, fixtures...), "usb:1-8"), {"usb:1-0", "usb:1-0"}} {
		if got, err := Admit(context.Background(), d, nil, invalid, current); err != ErrAdmission || !reflect.DeepEqual(got, Admission{}) {
			t.Fatalf("invalid selected fixture list: %+v %v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := Admit(ctx, d, nil, fixtures, current); err != context.Canceled || !reflect.DeepEqual(got, Admission{}) {
		t.Fatalf("cancelled fixture resolution: %+v %v", got, err)
	}
}

func TestBlockFixtureSourceRequiresOneExactCurrentDeviceNumber(t *testing.T) {
	d := completeDiscovery(physicalNode("block:a", "8:0"))
	for _, attributes := range [][]inventory.Attribute{
		{}, {{Name: "device_number", Value: "8:16"}}, {{Name: "device_number", Value: "08:0"}},
		{{Name: "device_number", Value: "8:0"}, {Name: "device_number", Value: "8:0"}},
	} {
		current := typedSnapshot(inventory.Component{Type: inventory.ComponentBlock, ID: "block:a", Attributes: attributes})
		if got, err := Admit(context.Background(), d, nil, []string{"block:a"}, current); err != ErrAdmission || !reflect.DeepEqual(got, Admission{}) {
			t.Fatalf("ambiguous/stale block fixture: %+v %v", got, err)
		}
	}
	current := typedSnapshot(inventory.Component{Type: inventory.ComponentBlock, ID: "block:a", Attributes: []inventory.Attribute{{Name: "device_number", Value: "8:0"}}})
	d.Nodes[0].Number = "8:16"
	if got, err := Admit(context.Background(), d, nil, []string{"block:a"}, current); err != ErrAdmission || !reflect.DeepEqual(got, Admission{}) {
		t.Fatalf("changed discovery identity accepted old fixture: %+v %v", got, err)
	}
}

func TestFixtureSourceRejectsReservedNamesWithoutNarrowingOrdinaryNames(t *testing.T) {
	for _, id := range []string{"usb:.", "usb:..", "block:.", "block:.."} {
		current := typedSnapshot(inventory.Component{Type: inventory.ComponentUSB, ID: id})
		if got, err := Admit(context.Background(), completeDiscovery(), nil, []string{id}, current); err != ErrAdmission || !reflect.DeepEqual(got, Admission{}) {
			t.Fatalf("reserved fixture name accepted: %+v %v", got, err)
		}
	}
	for _, id := range []string{"usb:1-2.3", "usb:1-2:1.0", "usb:.ordinary", "usb:ordinary.."} {
		current := typedSnapshot(inventory.Component{Type: inventory.ComponentUSB, ID: id})
		if got, err := Admit(context.Background(), completeDiscovery(), nil, []string{id}, current); err != nil || got.ResolvedFixtureMask != 1 || len(got.Targets) != 0 {
			t.Fatalf("ordinary fixture name lost: %+v %v", got, err)
		}
	}
}
