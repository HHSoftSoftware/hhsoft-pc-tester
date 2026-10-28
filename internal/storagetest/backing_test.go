package storagetest

import (
	"reflect"
	"strings"
	"testing"
)

func TestAdmissionMemoryBackingRequiresExactFilesystemFact(t *testing.T) {
	for _, filesystem := range []string{"tmpfs", "ramfs"} {
		d := completeDiscovery(boundLoop("block:loop0", "7:0", filesystem, "0:31"))
		d.MemoryMounts = []MemoryMount{{"0:31", filesystem}}
		got, err := admitFromNodes(d, nil, []string{})
		if err != nil || !reflect.DeepEqual(got.Targets[0].Exclusions, []string{"loop"}) {
			t.Fatalf("exact %s fact rejected: %+v %v", filesystem, got, err)
		}
	}
	for _, item := range []struct {
		name   string
		mounts []MemoryMount
		kind   string
	}{
		{"missing fact", []MemoryMount{}, "tmpfs"},
		{"null facts", nil, "tmpfs"},
		{"wrong number", []MemoryMount{{"0:32", "tmpfs"}}, "tmpfs"},
		{"wrong allowed filesystem", []MemoryMount{{"0:31", "ramfs"}}, "tmpfs"},
		{"rootfs is not RAM proof", []MemoryMount{{"0:31", "rootfs"}}, "tmpfs"},
		{"overlay is not RAM proof", []MemoryMount{{"0:31", "overlay"}}, "tmpfs"},
		{"mixed case", []MemoryMount{{"0:31", "Tmpfs"}}, "tmpfs"},
		{"conflicting aliases", []MemoryMount{{"0:31", "ramfs"}, {"0:31", "tmpfs"}}, "tmpfs"},
		{"duplicate canonical fact", []MemoryMount{{"0:31", "tmpfs"}, {"0:31", "tmpfs"}}, "tmpfs"},
		{"nonzero anonymous claim", []MemoryMount{{"8:0", "tmpfs"}}, "tmpfs"},
		{"unknown backing kind", []MemoryMount{{"0:31", "tmpfs"}}, "memory"},
	} {
		t.Run(item.name, func(t *testing.T) {
			d := completeDiscovery(boundLoop("block:loop0", "7:0", item.kind, "0:31"))
			d.MemoryMounts = item.mounts
			requireAdmissionError(t, d, nil, []string{})
		})
	}
}

func TestAdmissionLoopBindingRequiresCanonicalTupleDigest(t *testing.T) {
	d := completeDiscovery(boundLoop("block:loop0", "7:0", "tmpfs", "0:31"))
	d.MemoryMounts = []MemoryMount{{"0:31", "tmpfs"}}
	_, err := admitFromNodes(d, nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	// These are distinct already-derived numeric-tuple digests. No raw inode,
	// filename, key bytes or media bytes are inputs to this projection model.
	d.Nodes[0].LoopBacking.BindingSHA256 = strings.Repeat("b", 64)
	_, err = admitFromNodes(d, nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{"", strings.Repeat("A", 64), strings.Repeat("a", 63), strings.Repeat("g", 64)} {
		d.Nodes[0].LoopBacking.BindingSHA256 = digest
		requireAdmissionError(t, d, nil, []string{})
	}
	d.Nodes[0] = physicalNode("block:a", "8:0")
	d.Nodes[0].LoopBacking = &LoopBacking{Kind: "tmpfs", Device: "0:31", BindingSHA256: strings.Repeat("a", 64)}
	requireAdmissionError(t, d, nil, []string{})
}

func TestAdmissionUnboundLoopIsExplicitZeroWork(t *testing.T) {
	loop := physicalNode("block:loop0", "7:0")
	loop.Kind = "loop"
	loop.Transport = "virtual"
	loop.LoopState = "unbound"
	loop.Geometry.CapacityBytes = 0
	d := completeDiscovery(physicalNode("block:a", "8:0"), loop)
	got, err := admitFromNodes(d, nil, []string{})
	if err != nil || len(got.Targets) != 2 || len(got.Targets[0].Ranges) != 3 || !reflect.DeepEqual(got.Targets[1].Exclusions, []string{"loop", "unbound"}) || len(got.Targets[1].Ranges) != 0 || got.Targets[1].CapacityBytes != 0 {
		t.Fatalf("unbound disposition: %+v %v", got, err)
	}
	for _, item := range []struct {
		name   string
		mutate func(*Discovery)
	}{
		{"mounted", func(d *Discovery) { d.MountedNumbers = []string{"7:0"} }},
		{"boot backing", func(d *Discovery) { d.BootBackingNumbers = []string{"7:0"} }},
		{"nonzero capacity", func(d *Discovery) { d.Nodes[1].Geometry.CapacityBytes = 512 }},
		{"configured attributes", func(d *Discovery) {
			d.Nodes[1].LoopBacking = &LoopBacking{Kind: "block", Device: "8:0", BindingSHA256: strings.Repeat("a", 64)}
		}},
		{"holder", func(d *Discovery) {
			d.Nodes[1].Holders = []string{"block:a"}
			d.Nodes[0].Slaves = []string{"block:loop0"}
		}},
		{"slave", func(d *Discovery) {
			d.Nodes[1].Slaves = []string{"block:a"}
			d.Nodes[0].Holders = []string{"block:loop0"}
		}},
		{"child", func(d *Discovery) { d.Nodes[1].PhysicalChildren = []string{"block:loop0p1"} }},
		{"uncertain state", func(d *Discovery) { d.Nodes[1].LoopState = "unknown" }},
		{"configured zero capacity", func(d *Discovery) {
			d.Nodes[1] = boundLoop("block:loop0", "7:0", "block", "8:0")
			d.Nodes[1].Geometry.CapacityBytes = 0
		}},
		{"another loop depends on unbound", func(d *Discovery) { d.Nodes[0] = boundLoop("block:a", "7:1", "block", "7:0") }},
	} {
		t.Run(item.name, func(t *testing.T) {
			current := completeDiscovery(physicalNode("block:a", "8:0"), loop)
			item.mutate(&current)
			requireAdmissionError(t, current, nil, []string{})
		})
	}
	bound := completeDiscovery(physicalNode("block:a", "8:0"), boundLoop("block:loop0", "7:0", "block", "8:0"))
	changed, err := admitFromNodes(bound, nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changed.Targets[0].Exclusions, []string{"loop"}) || len(changed.Targets[0].Ranges) != 0 {
		t.Fatal("configured backing failed to exclude its family")
	}
}
