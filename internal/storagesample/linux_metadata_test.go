package storagesample

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

const linuxStorageTestMount = "1 0 0:1 / / rw - tmpfs tmpfs rw\n"
const linuxStorageTestSwap = "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n"

func linuxStorageTestNode(name, number string) linuxStorageNodeInput {
	return linuxStorageNodeInput{Name: name, Kind: "physical", Transport: "ata",
		Number: []byte(number + "\n"), Sectors: []byte("2048\n"), LogicalBlock: []byte("512\n"),
		ReadOnly: []byte("0\n"), Removable: []byte("0\n"), USBAncestors: []string{},
		PhysicalChildren: []string{}, Holders: []string{}, Slaves: []string{}}
}

func linuxStorageTestInput() linuxStorageMetadataInput {
	a := linuxStorageTestNode("a", "8:0")
	a.PhysicalChildren = []string{"a1"}
	a1 := linuxStorageTestNode("a1", "8:1")
	a1.Kind, a1.Parent, a1.Partition = "partition", "a", []byte("1\n")
	b := linuxStorageTestNode("b", "8:16")
	return linuxStorageMetadataInput{Registry: []string{"a", "a1", "b"}, Nodes: []linuxStorageNodeInput{a, a1, b},
		MountInfo: []byte(linuxStorageTestMount), Swaps: []byte(linuxStorageTestSwap), BootBackingNumbers: []string{}}
}

func requireLinuxStorageMetadataError(t *testing.T, input linuxStorageMetadataInput, want error) {
	t.Helper()
	got, err := parseLinuxStorageMetadata(input)
	if err != want || !reflect.DeepEqual(got, linuxStorageMetadataSnapshot{}) {
		t.Fatal("metadata failure retained facts or lost its closed error")
	}
}

func TestLinuxStorageMetadataOwnsCanonicalFacts(t *testing.T) {
	input := linuxStorageTestInput()
	input.Nodes[2].Transport, input.Nodes[2].Sectors = "virtio", []byte("16777216\n")
	input.Nodes[1].ReadOnly = []byte("1\n")
	input.Nodes = []linuxStorageNodeInput{input.Nodes[2], input.Nodes[1], input.Nodes[0]}
	input.Registry = []string{"b", "a1", "a"}
	input.MountInfo = []byte(linuxStorageTestMount + "2 1 8:1 / /owned ro - ext4 /dev/a1 ro\n")
	input.BootBackingNumbers = []string{"8:1"}
	got, err := parseLinuxStorageMetadata(input)
	if err != nil {
		t.Fatal("valid captured metadata rejected")
	}
	want := storagetest.Discovery{SwapState: "none",
		Nodes: []storagetest.Node{
			{ID: "block:a", Number: "8:0", Kind: "physical", Transport: "ata", Geometry: &storagetest.Geometry{CapacityBytes: 1048576, LogicalBlockBytes: 512},
				USBAncestors: []string{}, PhysicalChildren: []string{"block:a1"}, Holders: []string{}, Slaves: []string{}},
			{ID: "block:a1", Number: "8:1", Kind: "partition", Parent: "block:a", Transport: "ata", ReadOnly: true,
				Geometry:     &storagetest.Geometry{CapacityBytes: 1048576, LogicalBlockBytes: 512},
				USBAncestors: []string{}, PhysicalChildren: []string{}, Holders: []string{}, Slaves: []string{}},
			{ID: "block:b", Number: "8:16", Kind: "physical", Transport: "virtio", Geometry: &storagetest.Geometry{CapacityBytes: 8589934592, LogicalBlockBytes: 512},
				USBAncestors: []string{}, PhysicalChildren: []string{}, Holders: []string{}, Slaves: []string{}},
		},
		MountedNumbers: []string{"8:1"}, MemoryMounts: []storagetest.MemoryMount{{Number: "0:1", Filesystem: "tmpfs"}}, BootBackingNumbers: []string{"8:1"}}
	if got.rootDevice != "0:1" || !reflect.DeepEqual(got.discovery(), want) {
		t.Fatal("canonical facts differ from literal oracle")
	}
	input.Registry[0] = "changed"
	input.Nodes[0].Number[0], input.Nodes[0].Sectors[0] = '9', '0'
	input.Nodes[2].PhysicalChildren[0] = "changed"
	input.MountInfo[0], input.Swaps[0] = '9', 'X'
	input.BootBackingNumbers[0] = "8:16"
	if !reflect.DeepEqual(got.discovery(), want) {
		t.Fatal("retained input alias changed snapshot")
	}
	returned := got.discovery()
	returned.Nodes[0].Geometry.CapacityBytes = 0
	returned.Nodes[0].PhysicalChildren[0] = "block:b"
	returned.Nodes[2].Transport = "ata"
	returned.MountedNumbers[0], returned.BootBackingNumbers[0] = "8:16", "8:16"
	returned.MemoryMounts[0].Filesystem = "ramfs"
	returned.Complete, returned.BackingComplete = true, true
	if !reflect.DeepEqual(got.discovery(), want) {
		t.Fatal("returned view changed retained snapshot")
	}
	_, err = storagetest.Admit(context.Background(), got.discovery(), nil, []string{}, inventory.Snapshot{SchemaVersion: 1, Components: []inventory.Component{}})
	if err != storagetest.ErrAdmission {
		t.Fatal("pure parsing granted live admission")
	}
}

func TestLinuxStorageMetadataDisklessAndKnownGeometry(t *testing.T) {
	t.Run("diskless", func(t *testing.T) {
		input := linuxStorageTestInput()
		input.Registry, input.Nodes = []string{}, []linuxStorageNodeInput{}
		got, err := parseLinuxStorageMetadata(input)
		if err != nil || got.facts.Nodes == nil || len(got.facts.Nodes) != 0 || got.facts.Complete || got.facts.BackingComplete {
			t.Fatal("diskless view lost or promoted")
		}
	})
	for _, tc := range []struct {
		name, sectors, block string
		capacity             uint64
		logical              uint32
	}{
		{"empty", "0\n", "512\n", 0, 512},
		{"known_unsupported", "2048\n", "3\n", 1048576, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := linuxStorageTestInput()
			input.Nodes[2].Sectors = []byte(tc.sectors)
			input.Nodes[2].LogicalBlock = []byte(tc.block)
			got, err := parseLinuxStorageMetadata(input)
			if err != nil || got.facts.Nodes[2].Geometry.CapacityBytes != tc.capacity || got.facts.Nodes[2].Geometry.LogicalBlockBytes != tc.logical {
				t.Fatal("known geometry was confused with unavailable metadata")
			}
		})
	}
}

func TestLinuxStorageMetadataRejectsRegistryAndScalars(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*linuxStorageMetadataInput)
	}{
		{"nil_registry", func(v *linuxStorageMetadataInput) { v.Registry = nil }},
		{"nil_nodes", func(v *linuxStorageMetadataInput) { v.Nodes = nil }},
		{"missing_node", func(v *linuxStorageMetadataInput) { v.Nodes = v.Nodes[:2] }},
		{"duplicate_registry", func(v *linuxStorageMetadataInput) { v.Registry[2] = "a" }},
		{"duplicate_node", func(v *linuxStorageMetadataInput) { v.Nodes[2] = v.Nodes[0] }},
		{"unsafe_name", func(v *linuxStorageMetadataInput) { v.Registry[2] = "../b"; v.Nodes[2].Name = "../b" }},
		{"ambiguous_number", func(v *linuxStorageMetadataInput) { v.Nodes[2].Number = []byte("8:0\n") }},
		{"zero_major", func(v *linuxStorageMetadataInput) { v.Nodes[2].Number = []byte("0:16\n") }},
		{"leading_zero", func(v *linuxStorageMetadataInput) { v.Nodes[2].Number = []byte("08:16\n") }},
		{"major_overflow", func(v *linuxStorageMetadataInput) { v.Nodes[2].Number = []byte("4294967296:16\n") }},
		{"field_newline", func(v *linuxStorageMetadataInput) { v.Nodes[2].Number = []byte("8\n:16\n") }},
		{"sector_overflow", func(v *linuxStorageMetadataInput) { v.Nodes[2].Sectors = []byte("36028797018963968\n") }},
		{"missing_geometry", func(v *linuxStorageMetadataInput) { v.Nodes[2].LogicalBlock = nil }},
		{"scalar_trailing_data", func(v *linuxStorageMetadataInput) { v.Nodes[2].Sectors = []byte("2048\n0") }},
		{"invalid_boolean", func(v *linuxStorageMetadataInput) { v.Nodes[2].ReadOnly = []byte("2\n") }},
		{"missing_boolean", func(v *linuxStorageMetadataInput) { v.Nodes[2].Removable = nil }},
		{"unavailable_relations", func(v *linuxStorageMetadataInput) { v.Nodes[2].Holders = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := linuxStorageTestInput()
			tc.change(&input)
			requireLinuxStorageMetadataError(t, input, errLinuxStorageMetadata)
		})
	}
}

func TestLinuxStorageMetadataRejectsTopology(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*linuxStorageMetadataInput)
	}{
		{"dangling_parent", func(v *linuxStorageMetadataInput) { v.Nodes[1].Parent = "absent" }},
		{"missing_child_reverse", func(v *linuxStorageMetadataInput) { v.Nodes[0].PhysicalChildren = []string{} }},
		{"dangling_holder", func(v *linuxStorageMetadataInput) { v.Nodes[2].Holders = []string{"absent"} }},
		{"missing_holder_reverse", func(v *linuxStorageMetadataInput) { v.Nodes[2].Holders = []string{"a"} }},
		{"self_edge", func(v *linuxStorageMetadataInput) {
			v.Nodes[2].Holders = []string{"b"}
			v.Nodes[2].Slaves = []string{"b"}
		}},
		{"duplicate_relation", func(v *linuxStorageMetadataInput) { v.Nodes[0].PhysicalChildren = []string{"a1", "a1"} }},
		{"invalid_usb_identity", func(v *linuxStorageMetadataInput) { v.Nodes[2].USBAncestors = []string{"usb:../x"} }},
		{"unknown_transport", func(v *linuxStorageMetadataInput) { v.Nodes[2].Transport = "unproved" }},
		{"partition_zero", func(v *linuxStorageMetadataInput) { v.Nodes[1].Partition = []byte("0\n") }},
		{"nonpartition_attribute", func(v *linuxStorageMetadataInput) { v.Nodes[2].Partition = []byte("1\n") }},
		{"mapper_without_dependencies", func(v *linuxStorageMetadataInput) { v.Nodes[2].Kind = "mapper"; v.Nodes[2].Transport = "virtual" }},
		{"dependency_cycle", func(v *linuxStorageMetadataInput) {
			x, y := linuxStorageTestNode("x", "253:0"), linuxStorageTestNode("y", "253:1")
			x.Kind, y.Kind, x.Transport, y.Transport = "mapper", "mapper", "virtual", "virtual"
			x.Holders, x.Slaves, y.Holders, y.Slaves = []string{"y"}, []string{"y"}, []string{"x"}, []string{"x"}
			v.Registry = []string{"x", "y"}
			v.Nodes = []linuxStorageNodeInput{x, y}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := linuxStorageTestInput()
			tc.change(&input)
			requireLinuxStorageMetadataError(t, input, errLinuxStorageMetadata)
		})
	}
}

func TestLinuxStorageMetadataRejectsLoopsAndSwap(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*linuxStorageMetadataInput)
	}{
		{"loop_kind", func(v *linuxStorageMetadataInput) { v.Nodes[2].Kind = "loop" }},
		{"loop_name", func(v *linuxStorageMetadataInput) { v.Nodes[2].Name = "loop0"; v.Registry[2] = "loop0" }},
		{"loop_number", func(v *linuxStorageMetadataInput) { v.Nodes[2].Number = []byte("7:0\n") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := linuxStorageTestInput()
			tc.change(&input)
			requireLinuxStorageMetadataError(t, input, errLinuxStorageLoop)
		})
	}
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"unknown", "", errLinuxStorageMetadata},
		{"wrong_header", "filename Type Size Used Priority\n", errLinuxStorageMetadata},
		{"missing_eof_line", "Filename Type Size Used Priority", errLinuxStorageMetadata},
		{"blank_record", linuxStorageTestSwap + "\n", errLinuxStorageMetadata},
		{"incomplete_record", linuxStorageTestSwap + "/owned-swap file 1024 0\n", errLinuxStorageMetadata},
		{"unknown_type", linuxStorageTestSwap + "/owned-swap unknown 1024 0 -2\n", errLinuxStorageMetadata},
		{"size_overflow", linuxStorageTestSwap + "/owned-swap file 18446744073709551616 0 -2\n", errLinuxStorageMetadata},
		{"used_overflow", linuxStorageTestSwap + "/owned-swap file 1024 1025 -2\n", errLinuxStorageMetadata},
		{"priority_overflow", linuxStorageTestSwap + "/owned-swap file 1024 0 2147483648\n", errLinuxStorageMetadata},
		{"duplicate_record", linuxStorageTestSwap + "/owned-swap file 1024 0 -2\n/owned-swap file 1024 0 -2\n", errLinuxStorageMetadata},
		{"active_file", linuxStorageTestSwap + "/owned-sensitive-name file 1024 0 -2\n", errLinuxStorageSwap},
		{"active_partition", linuxStorageTestSwap + "/dev/zram0 partition 1024 1 100\n", errLinuxStorageSwap},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := linuxStorageTestInput()
			input.Swaps = []byte(tc.body)
			requireLinuxStorageMetadataError(t, input, tc.want)
			if strings.Contains(tc.want.Error(), "owned-") {
				t.Fatal("swap path leaked through diagnostic")
			}
		})
	}
}

func TestLinuxStorageMetadataMountAndBootClosure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*linuxStorageMetadataInput)
	}{
		{"missing_mounts", func(v *linuxStorageMetadataInput) { v.MountInfo = nil }},
		{"ambiguous_root", func(v *linuxStorageMetadataInput) {
			v.MountInfo = append(v.MountInfo, []byte("2 1 0:2 / / ro - ramfs ramfs ro\n")...)
		}},
		{"unknown_memory_filesystem", func(v *linuxStorageMetadataInput) { v.MountInfo = []byte("1 0 0:1 / / rw - unknown unknown rw\n") }},
		{"conflicting_memory_alias", func(v *linuxStorageMetadataInput) {
			v.MountInfo = append(v.MountInfo, []byte("2 1 0:1 / /owned ro - ramfs ramfs ro\n")...)
		}},
		{"unknown_block_mount", func(v *linuxStorageMetadataInput) {
			v.MountInfo = append(v.MountInfo, []byte("2 1 8:99 / /owned ro - ext4 /dev/absent ro\n")...)
		}},
		{"ambiguous_access", func(v *linuxStorageMetadataInput) { v.MountInfo = []byte("1 0 0:1 / / ro,rw - tmpfs tmpfs rw\n") }},
		{"writable_overlay", func(v *linuxStorageMetadataInput) { v.MountInfo = []byte("1 0 0:1 / / rw - overlay overlay rw\n") }},
		{"missing_boot_facts", func(v *linuxStorageMetadataInput) { v.BootBackingNumbers = nil }},
		{"unknown_boot_block", func(v *linuxStorageMetadataInput) { v.BootBackingNumbers = []string{"8:99"} }},
		{"unknown_boot_zero", func(v *linuxStorageMetadataInput) { v.BootBackingNumbers = []string{"0:9"} }},
		{"duplicate_boot", func(v *linuxStorageMetadataInput) { v.BootBackingNumbers = []string{"8:1", "8:1"} }},
		{"noncanonical_boot", func(v *linuxStorageMetadataInput) { v.BootBackingNumbers = []string{"8:1\n"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := linuxStorageTestInput()
			tc.change(&input)
			requireLinuxStorageMetadataError(t, input, errLinuxStorageMetadata)
		})
	}
}

func TestLinuxStorageMetadataFiniteBounds(t *testing.T) {
	makeBoundary := func() linuxStorageMetadataInput {
		input := linuxStorageTestInput()
		input.Registry = []string{}
		input.Nodes = []linuxStorageNodeInput{}
		for i := 0; i < 256; i++ {
			name := fmt.Sprintf("disk%03d", i)
			node := linuxStorageTestNode(name, fmt.Sprintf("8:%d", i))
			node.USBAncestors = []string{"usb:1-1", "usb:1-2", "usb:1-3", "usb:1-4"}
			input.Registry = append(input.Registry, name)
			input.Nodes = append(input.Nodes, node)
		}
		return input
	}
	t.Run("exact_nodes_and_edges", func(t *testing.T) {
		got, err := parseLinuxStorageMetadata(makeBoundary())
		if err != nil || len(got.facts.Nodes) != 256 {
			t.Fatal("exact 256-node 1024-edge boundary rejected")
		}
	})
	t.Run("node_plus_one", func(t *testing.T) {
		input := makeBoundary()
		input.Registry = append(input.Registry, "extra")
		input.Nodes = append(input.Nodes, linuxStorageTestNode("extra", "8:256"))
		requireLinuxStorageMetadataError(t, input, errLinuxStorageMetadata)
	})
	t.Run("edge_plus_one", func(t *testing.T) {
		input := makeBoundary()
		input.Nodes[255].USBAncestors = append(input.Nodes[255].USBAncestors, "usb:1-5")
		requireLinuxStorageMetadataError(t, input, errLinuxStorageMetadata)
	})
	t.Run("mount_bytes_plus_one", func(t *testing.T) {
		input := linuxStorageTestInput()
		input.MountInfo = bytes.Repeat([]byte{'x'}, (4<<20)+1)
		requireLinuxStorageMetadataError(t, input, errLinuxStorageMetadata)
	})
	t.Run("swap_bytes_plus_one", func(t *testing.T) {
		input := linuxStorageTestInput()
		input.Swaps = bytes.Repeat([]byte{'x'}, (64<<10)+1)
		requireLinuxStorageMetadataError(t, input, errLinuxStorageMetadata)
	})
	t.Run("swap_rows_plus_one", func(t *testing.T) {
		input := linuxStorageTestInput()
		body := linuxStorageTestSwap
		for i := 0; i < 257; i++ {
			body += fmt.Sprintf("/owned%d file 1024 0 -2\n", i)
		}
		input.Swaps = []byte(body)
		requireLinuxStorageMetadataError(t, input, errLinuxStorageMetadata)
	})
}

func TestLinuxStorageMetadataFeedsWholeFamilyExclusions(t *testing.T) {
	// Only these synthetic fixtures assert complete ownership for model tests.
	// Production parser results deliberately retain false admission flags.
	for _, tc := range []struct {
		name, reason string
		fixture      bool
		change       func(*linuxStorageMetadataInput)
	}{
		{"mounted_child", "mounted", false, func(v *linuxStorageMetadataInput) {
			v.MountInfo = append(v.MountInfo, []byte("2 1 8:1 / /owned ro - ext4 /dev/a1 ro\n")...)
		}},
		{"boot_partition", "boot", false, func(v *linuxStorageMetadataInput) { v.BootBackingNumbers = []string{"8:1"} }},
		{"removable_child", "removable", false, func(v *linuxStorageMetadataInput) { v.Nodes[1].Removable = []byte("1\n") }},
		{"usb_child", "usb", false, func(v *linuxStorageMetadataInput) { v.Nodes[1].USBAncestors = []string{"usb:1-2"} }},
		{"optical_child", "optical", false, func(v *linuxStorageMetadataInput) { v.Nodes[1].Transport = "optical" }},
		{"reserved_child", "fixture", true, func(v *linuxStorageMetadataInput) {}},
		{"stacked_family", "stacked", false, func(v *linuxStorageMetadataInput) {
			dm := linuxStorageTestNode("dm-0", "253:0")
			dm.Kind, dm.Transport = "mapper", "virtual"
			dm.Slaves = []string{"a"}
			v.Nodes[0].Holders = []string{"dm-0"}
			v.Nodes = append(v.Nodes, dm)
			v.Registry = append(v.Registry, "dm-0")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := linuxStorageTestInput()
			tc.change(&input)
			snapshot, err := parseLinuxStorageMetadata(input)
			if err != nil {
				t.Fatal("complete synthetic family rejected")
			}
			facts := snapshot.discovery()
			facts.Complete, facts.BackingComplete = true, true
			fixtures := []string{}
			if tc.fixture {
				fixtures = []string{"block:a1"}
			}
			current := inventory.Snapshot{SchemaVersion: 1, Components: []inventory.Component{
				{ID: "block:a1", Type: inventory.ComponentBlock, Attributes: []inventory.Attribute{{Name: "device_number", Value: "8:1"}}},
			}}
			admitted, err := storagetest.Admit(context.Background(), facts, []string{"block:a", "block:b"}, fixtures, current)
			if err != nil || len(admitted.Targets) != 2 {
				t.Fatal("model rejected independent family fixture")
			}
			if !reflect.DeepEqual(admitted.Targets[0].Exclusions, []string{tc.reason}) || len(admitted.Targets[0].Ranges) != 0 {
				t.Fatal("excluded family retained a read schedule")
			}
			want := []storagetest.Range{{Offset: 0, Length: 65536}, {Offset: 491520, Length: 65536}, {Offset: 983040, Length: 65536}}
			if len(admitted.Targets[1].Exclusions) != 0 || !reflect.DeepEqual(admitted.Targets[1].Ranges, want) {
				t.Fatal("unrelated family lost its independent literal schedule")
			}
			if snapshot.facts.Complete || snapshot.facts.BackingComplete || len(snapshot.facts.Nodes) != len(input.Nodes) {
				t.Fatal("model fixture promoted or filtered original metadata")
			}
		})
	}
}
