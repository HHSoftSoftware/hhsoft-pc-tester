package storagetest

import (
	"context"
	"fmt"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func physicalNode(id, number string) Node {
	return Node{ID: id, Number: number, Kind: "physical", Transport: "ata", Geometry: &Geometry{CapacityBytes: 1048576, LogicalBlockBytes: 512}, USBAncestors: []string{}, PhysicalChildren: []string{}, Holders: []string{}, Slaves: []string{}}
}

func completeDiscovery(nodes ...Node) Discovery {
	if nodes == nil {
		nodes = []Node{}
	}
	for i := range nodes {
		if nodes[i].Geometry != nil {
			copied := *nodes[i].Geometry
			nodes[i].Geometry = &copied
		}
		if nodes[i].LoopBacking != nil {
			copied := *nodes[i].LoopBacking
			nodes[i].LoopBacking = &copied
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return Discovery{Complete: true, BackingComplete: true, SwapState: "none", Nodes: nodes, MountedNumbers: []string{}, MemoryMounts: []MemoryMount{}, BootBackingNumbers: []string{}}
}

func boundLoop(id, number, kind, backing string) Node {
	node := physicalNode(id, number)
	node.Kind = "loop"
	node.Transport = "virtual"
	node.LoopState = "configured"
	node.LoopBacking = &LoopBacking{Kind: kind, Device: backing, BindingSHA256: strings.Repeat("a", 64)}
	return node
}

// Existing topology cases use a current typed fixture source derived from their
// controlled nodes. Dedicated fixture-source tests supply independent snapshots.
func admitFromNodes(discovery Discovery, requested, fixtures []string) (Admission, error) {
	components := []inventory.Component{}
	usb := map[string]bool{}
	for _, node := range discovery.Nodes {
		components = append(components, inventory.Component{Type: inventory.ComponentBlock, ID: node.ID, Attributes: []inventory.Attribute{{Name: "device_number", Value: node.Number}}})
		for _, id := range node.USBAncestors {
			usb[id] = true
		}
	}
	for id := range usb {
		components = append(components, inventory.Component{Type: inventory.ComponentUSB, ID: id})
	}
	return Admit(context.Background(), discovery, requested, fixtures, inventory.Snapshot{SchemaVersion: 1, Components: components})
}

func requireAdmissionError(t *testing.T, discovery Discovery, requested, fixtures []string) {
	t.Helper()
	got, err := admitFromNodes(discovery, requested, fixtures)
	if err != ErrAdmission || !reflect.DeepEqual(got, Admission{}) {
		t.Fatalf("unexpected admission: %+v, %v", got, err)
	}
}

func TestAdmissionMountedPartitionExcludesWholeFamily(t *testing.T) {
	a := physicalNode("block:a", "8:0")
	a.PhysicalChildren = []string{"block:a1", "block:a2"}
	a1 := physicalNode("block:a1", "8:1")
	a1.Kind = "partition"
	a1.Parent = a.ID
	a2 := physicalNode("block:a2", "8:2")
	a2.Kind = "partition"
	a2.Parent = a.ID
	b := physicalNode("block:b", "8:16")
	d := completeDiscovery(a, a1, a2, b)
	d.MountedNumbers = []string{"8:2"}
	got, err := admitFromNodes(d, nil, []string{})
	if err != nil || !reflect.DeepEqual(got.RequestedIDs, []string{"block:a", "block:b"}) || len(got.Targets) != 2 {
		t.Fatalf("admission: %+v, %v", got, err)
	}
	if !reflect.DeepEqual(got.Targets[0].Exclusions, []string{"mounted"}) || len(got.Targets[0].Ranges) != 0 {
		t.Fatalf("mounted sibling retained work: %+v", got.Targets[0])
	}
	if len(got.Targets[1].Exclusions) != 0 || !reflect.DeepEqual(got.Targets[1].Ranges, []Range{{0, 65536}, {491520, 65536}, {983040, 65536}}) {
		t.Fatalf("unrelated media lost or schedule wrong: %+v", got.Targets[1])
	}
}

func TestAdmissionFixturesAndUSBRemainExcludedWithoutRemovableFlag(t *testing.T) {
	a := physicalNode("block:a", "8:0")
	a.USBAncestors = []string{"usb:1-2"}
	b := physicalNode("block:b", "8:16")
	d := completeDiscovery(a, b)
	got, err := admitFromNodes(d, []string{"block:b", "block:a"}, []string{"usb:1-2", "block:b"})
	if err != nil || !reflect.DeepEqual(got.FixtureIDs, []string{"block:b", "usb:1-2"}) {
		t.Fatalf("fixture admission: %+v %v", got, err)
	}
	if !reflect.DeepEqual(got.Targets[0].Exclusions, []string{"fixture", "usb"}) || !reflect.DeepEqual(got.Targets[1].Exclusions, []string{"fixture"}) {
		t.Fatalf("fixture exclusion lost: %+v", got.Targets)
	}
	for _, target := range got.Targets {
		if len(target.Ranges) != 0 {
			t.Fatal("excluded target has reads")
		}
	}
	requireAdmissionError(t, d, nil, []string{"usb:missing"})
	requireAdmissionError(t, d, nil, []string{"block:b", "block:b"})
}

func TestAdmissionStackedAndLoopFamiliesNeverReceiveSamples(t *testing.T) {
	a := physicalNode("block:a", "8:0")
	a.Holders = []string{"block:dm-0"}
	dm := physicalNode("block:dm-0", "253:0")
	dm.Kind = "mapper"
	dm.Transport = "virtual"
	dm.Slaves = []string{"block:a"}
	b := physicalNode("block:b", "8:16")
	loop := boundLoop("block:loop0", "7:0", "block", "8:16")
	d := completeDiscovery(a, dm, b, loop)
	got, err := admitFromNodes(d, nil, []string{})
	if err != nil || len(got.Targets) != 4 {
		t.Fatalf("stacked admission: %+v %v", got, err)
	}
	want := map[string][]string{"block:a": {"stacked"}, "block:b": {"loop"}, "block:dm-0": {"stacked"}, "block:loop0": {"loop"}}
	for _, target := range got.Targets {
		if !reflect.DeepEqual(target.Exclusions, want[target.ID]) || len(target.Ranges) != 0 {
			t.Fatalf("unsafe family: %+v", target)
		}
	}
	loop = boundLoop("block:loop0", "7:0", "tmpfs", "0:31")
	d = completeDiscovery(b, loop)
	d.MemoryMounts = []MemoryMount{{"0:31", "tmpfs"}}
	d.BootBackingNumbers = []string{"0:31"}
	got, err = admitFromNodes(d, nil, []string{})
	if err != nil || len(got.Targets[0].Exclusions) != 0 || len(got.Targets[0].Ranges) != 3 || !reflect.DeepEqual(got.Targets[1].Exclusions, []string{"loop"}) {
		t.Fatalf("proven memory backing contaminated unrelated disk: %+v %v", got, err)
	}
	d.MemoryMounts = []MemoryMount{}
	requireAdmissionError(t, d, nil, []string{})
}

func TestAdmissionRejectsContradictoryOrIncompleteTopology(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Discovery)
	}{
		{"incomplete", func(d *Discovery) { d.Complete = false }},
		{"backing incomplete", func(d *Discovery) { d.BackingComplete = false }},
		{"active swap", func(d *Discovery) { d.SwapState = "active" }},
		{"unknown swap", func(d *Discovery) { d.SwapState = "unknown" }},
		{"unknown mounted family", func(d *Discovery) { d.MountedNumbers = []string{"8:99"} }},
		{"unknown boot backing", func(d *Discovery) { d.BootBackingNumbers = []string{"0:99"} }},
		{"missing child", func(d *Discovery) { d.Nodes[0].PhysicalChildren = []string{"block:a1"} }},
		{"missing holder", func(d *Discovery) { d.Nodes[0].Holders = []string{"block:dm-0"} }},
		{"noncanonical number", func(d *Discovery) { d.Nodes[0].Number = "08:0" }},
		{"duplicate number", func(d *Discovery) { d.Nodes[1].Number = "8:0" }},
		{"duplicate id", func(d *Discovery) { d.Nodes[1].ID = "block:a" }},
		{"reverse ids", func(d *Discovery) { d.Nodes[0], d.Nodes[1] = d.Nodes[1], d.Nodes[0] }},
		{"unselected missing capacity", func(d *Discovery) { d.Nodes[1].Geometry = nil }},
		{"unselected unknown kind", func(d *Discovery) { d.Nodes[1].Kind = "raid" }},
		{"null relationship", func(d *Discovery) { d.Nodes[0].Slaves = nil }},
		{"null mounted set", func(d *Discovery) { d.MountedNumbers = nil }},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			d := completeDiscovery(physicalNode("block:a", "8:0"), physicalNode("block:b", "8:16"))
			item.mutate(&d)
			requireAdmissionError(t, d, []string{"block:a"}, []string{})
		})
	}
	a := physicalNode("block:a", "8:0")
	a.Holders = []string{"block:b"}
	a.Slaves = []string{"block:b"}
	b := physicalNode("block:b", "8:16")
	b.Holders = []string{"block:a"}
	b.Slaves = []string{"block:a"}
	requireAdmissionError(t, completeDiscovery(a, b), nil, []string{})
	a = boundLoop("block:a", "7:0", "block", "7:1")
	b = boundLoop("block:b", "7:1", "block", "7:0")
	requireAdmissionError(t, completeDiscovery(a, b), nil, []string{})
}

func TestAdmissionDisklessExplicitEmptyAndRequestedIdentity(t *testing.T) {
	got, err := admitFromNodes(completeDiscovery(), nil, []string{})
	if err != nil || got.Targets == nil || len(got.Targets) != 0 || got.RequestedIDs == nil {
		t.Fatalf("diskless: %+v %v", got, err)
	}
	d := completeDiscovery(physicalNode("block:a", "8:0"))
	got, err = admitFromNodes(d, []string{}, []string{})
	if err != nil || len(got.Targets) != 0 {
		t.Fatalf("explicit empty selected work: %+v %v", got, err)
	}
	requireAdmissionError(t, d, []string{"block:missing"}, []string{})
	requireAdmissionError(t, d, []string{"block:a", "block:a"}, []string{})
	requireAdmissionError(t, d, nil, nil)
	child := physicalNode("block:a1", "8:1")
	child.Kind = "partition"
	child.Parent = "block:a"
	d.Nodes[0].PhysicalChildren = []string{child.ID}
	d.Nodes = append(d.Nodes, child)
	requireAdmissionError(t, d, []string{child.ID}, []string{})
}

func TestAdmissionLiteralDeviceAndEdgeBounds(t *testing.T) {
	nodes := make([]Node, 256)
	for i := range nodes {
		nodes[i] = physicalNode(fmt.Sprintf("block:d%03d", i), fmt.Sprintf("8:%d", i))
	}
	d := completeDiscovery(nodes...)
	got, err := admitFromNodes(d, nil, []string{})
	if err != nil || len(got.Targets) != 256 {
		t.Fatalf("exact registry bound: %d %v", len(got.Targets), err)
	}
	d.Nodes = append(d.Nodes, physicalNode("block:d256", "8:256"))
	requireAdmissionError(t, d, nil, []string{})
	d = completeDiscovery(physicalNode("block:a", "8:0"))
	for i := 0; i < 1024; i++ {
		d.Nodes[0].USBAncestors = append(d.Nodes[0].USBAncestors, fmt.Sprintf("usb:u%04d", i))
	}
	got, err = admitFromNodes(d, nil, []string{})
	if err != nil || !reflect.DeepEqual(got.Targets[0].Exclusions, []string{"usb"}) {
		t.Fatalf("exact edge bound: %+v %v", got, err)
	}
	d.Nodes[0].USBAncestors = append(d.Nodes[0].USBAncestors, "usb:u1024")
	requireAdmissionError(t, d, nil, []string{})
	requireAdmissionError(t, completeDiscovery(), make([]string, 257), []string{})
	requireAdmissionError(t, completeDiscovery(), nil, make([]string, 9))
}

func TestAdmissionCopiesCallerSelectionsAndHasNoCrossCallState(t *testing.T) {
	d := completeDiscovery(physicalNode("block:a", "8:0"), physicalNode("block:b", "8:16"))
	requested := []string{"block:b", "block:a"}
	fixtures := []string{"block:b"}
	first, err := admitFromNodes(d, requested, fixtures)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(requested, []string{"block:b", "block:a"}) || !reflect.DeepEqual(fixtures, []string{"block:b"}) {
		t.Fatal("caller selections mutated")
	}
	requested[0] = "block:changed"
	fixtures[0] = "block:changed"
	d.Nodes[0].ID = "block:changed"
	if !reflect.DeepEqual(first.RequestedIDs, []string{"block:a", "block:b"}) || !reflect.DeepEqual(first.FixtureIDs, []string{"block:b"}) || first.Targets[0].ID != "block:a" {
		t.Fatal("result aliases caller input")
	}
	second, err := admitFromNodes(completeDiscovery(physicalNode("block:b", "8:16")), nil, []string{})
	if err != nil || len(second.Targets[0].Exclusions) != 0 || len(second.Targets[0].Ranges) != 3 {
		t.Fatalf("prior fixture leaked into next call: %+v %v", second, err)
	}
}

func TestAdmissionSCSIDirectMediaPreservesLiteralSchedule(t *testing.T) {
	node := physicalNode("block:sda", "8:0")
	node.Transport = "scsi"
	node.Geometry.CapacityBytes = 98304
	node.Geometry.LogicalBlockBytes = 4096
	got, err := admitFromNodes(completeDiscovery(node), []string{"block:sda"}, []string{})
	want := []Target{{ID: "block:sda", Number: "8:0", CapacityBytes: 98304, LogicalBlockBytes: 4096, Exclusions: []string{}, Ranges: []Range{{0, 65536}, {16384, 65536}, {32768, 65536}}}}
	if err != nil || !reflect.DeepEqual(got.Targets, want) || !reflect.DeepEqual(got.RequestedIDs, []string{"block:sda"}) {
		t.Fatalf("direct SCSI admission: %+v %v", got, err)
	}
	coverage, err := RangeCoverage(got.Targets[0].Ranges)
	if err != nil || coverage != 98304 {
		t.Fatalf("overlapping SCSI coverage: %d %v", coverage, err)
	}
}
