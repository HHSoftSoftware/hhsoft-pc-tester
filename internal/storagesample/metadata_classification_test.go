package storagesample

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func classificationTestRaw(name, number string) linuxStorageCapturedNode {
	return linuxStorageCapturedNode{name: name, number: []byte(number + "\n"), sectors: []byte("16777216\n"),
		readOnly: []byte("0\n"), removable: []byte("0\n"), logicalBlock: []byte("512\n"), holders: []string{}, slaves: []string{}}
}
func classificationTestPath(name, number string) linuxStorageCanonicalPath {
	return linuxStorageCanonicalPath{name: name, number: []byte(number + "\n"), ancestors: []linuxStorageAncestor{
		{path: "/devices", subsystem: "none"}, {path: "/devices/pci0", subsystem: "pci"},
		{path: "/devices/pci0/adapter", subsystem: "virtio"}, {path: "/devices/pci0/adapter/block", subsystem: "none"},
		{path: "/devices/pci0/adapter/block/" + name, subsystem: "block"},
	}}
}
func classificationTestFacts() (linuxStorageCapture, []linuxStorageCanonicalPath) {
	return linuxStorageCapture{registry: []string{"disk"}, nodes: []linuxStorageCapturedNode{classificationTestRaw("disk", "8:0")},
			mountInfo: []byte("1 0 0:1 / / rw - tmpfs tmpfs rw\n"), swaps: []byte("Filename Type Size Used Priority\n")},
		[]linuxStorageCanonicalPath{classificationTestPath("disk", "8:0")}
}
func classificationTestPartition() (linuxStorageCapture, []linuxStorageCanonicalPath) {
	capture, paths := classificationTestFacts()
	capture.nodes[0].logicalBlock = []byte("4096\n")
	capture.nodes[0].removable = []byte("1\n")
	partition := classificationTestRaw("slice", "8:1")
	partition.partition = []byte("1\n")
	partition.sectors = []byte("2097152\n")
	partition.readOnly = []byte("1\n")
	partition.logicalBlock = nil
	partition.removable = nil
	partition.partitionSlavesAbsent = true
	capture.registry = append(capture.registry, "slice")
	capture.nodes = append(capture.nodes, partition)
	child := linuxStorageCanonicalPath{name: "slice", number: []byte("8:1\n"), ancestors: append([]linuxStorageAncestor{}, paths[0].ancestors...)}
	child.ancestors = append(child.ancestors, linuxStorageAncestor{path: "/devices/pci0/adapter/block/disk/slice", subsystem: "block"})
	return capture, append(paths, child)
}

func TestStorageClassificationHeldCapture(t *testing.T) {
	f := newMetadataTestFixture(t)
	var input linuxStorageMetadataInput
	got, err := withLinuxStorageMetadata(context.Background(), f.operations(), func(view *storageMetadataView) error {
		raw, e := view.Snapshot()
		if e != nil {
			return e
		}
		input, e = classifyLinuxStorageCapture(raw, []linuxStorageCanonicalPath{classificationTestPath("disk", "8:0")})
		if e != nil {
			return e
		}
		return view.Verify(context.Background())
	})
	f.allClosed()
	if err != nil || len(got.nodes) != 1 || len(input.Nodes) != 1 || input.Nodes[0].Transport != "virtio" || input.BootBackingNumbers != nil {
		t.Fatal("held capture classification or unknown original boot mismatch")
	}
	if _, e := parseLinuxStorageMetadata(input); e == nil {
		t.Fatal("missing original-boot producer was accepted")
	}
	// This literal is a fixture fact, never an inferred boot value.
	input.BootBackingNumbers = []string{"8:0"}
	snapshot, e := parseLinuxStorageMetadata(input)
	if e != nil {
		t.Fatal("independent fixture boot record did not reach strict parser")
	}
	facts := snapshot.discovery()
	if facts.Complete || facts.BackingComplete || len(facts.Nodes) != 1 || facts.Nodes[0].Geometry.CapacityBytes != 8589934592 || facts.Nodes[0].Transport != "virtio" {
		t.Fatal("classification promoted authority or changed fixture geometry")
	}
}

func TestStorageClassificationPartitionContext(t *testing.T) {
	capture, paths := classificationTestPartition()
	input, err := classifyLinuxStorageCapture(capture, paths)
	if err != nil || len(input.Nodes) != 2 {
		t.Fatal("canonical partition classification failed")
	}
	whole, part := input.Nodes[0], input.Nodes[1]
	if whole.Kind != "physical" || !reflect.DeepEqual(whole.PhysicalChildren, []string{"slice"}) ||
		part.Kind != "partition" || part.Parent != "disk" || part.Transport != "virtio" ||
		string(part.LogicalBlock) != "4096\n" || string(part.Removable) != "1\n" || string(part.Sectors) != "2097152\n" ||
		string(part.ReadOnly) != "1\n" || string(part.Partition) != "1\n" || len(part.PhysicalChildren) != 0 {
		t.Fatal("independent parent and inherited geometry oracle mismatch")
	}
	// Inherited output fields must not alias the parent's output either.
	part.LogicalBlock[0] = '9'
	part.Removable[0] = '0'
	if string(whole.LogicalBlock) != "4096\n" || string(whole.Removable) != "1\n" {
		t.Fatal("partition context aliases parent output")
	}
}

func TestStorageClassificationTransportContext(t *testing.T) {
	for _, tc := range []struct{ name, subsystem, scsi, want string }{
		{"virtio", "virtio", "", "virtio"}, {"xen", "xen", "", "xen"}, {"nvme", "nvme", "", "nvme"}, {"mmc", "mmc", "", "mmc"},
		{"scsi_disk", "scsi", "0\n", "scsi"}, {"scsi_cd", "scsi", "5\n", "optical"}, {"scsi_optical", "scsi", "7\n", "optical"},
		{"scsi_missing", "scsi", "", "unknown"}, {"scsi_other", "scsi", "1\n", "unknown"}, {"unrecognized", "pci", "", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture, paths := classificationTestFacts()
			paths[0].ancestors[2].subsystem = tc.subsystem
			if tc.scsi != "" {
				paths[0].ancestors[2].scsiType = []byte(tc.scsi)
			}
			input, err := classifyLinuxStorageCapture(capture, paths)
			if err != nil || input.Nodes[0].Transport != tc.want || input.Nodes[0].Kind != "physical" {
				t.Fatal("observed context was guessed from path spelling")
			}
		})
	}
}

func TestStorageClassificationUSBContext(t *testing.T) {
	capture, _ := classificationTestFacts()
	row := linuxStorageCanonicalPath{name: "disk", number: []byte("8:0\n"), ancestors: []linuxStorageAncestor{
		{path: "/devices", subsystem: "none"}, {path: "/devices/pci0", subsystem: "pci"},
		{path: "/devices/pci0/usb1", subsystem: "usb", deviceType: "usb_device"},
		{path: "/devices/pci0/usb1/1-2", subsystem: "usb", deviceType: "usb_device"},
		{path: "/devices/pci0/usb1/1-2/1-2:1.0", subsystem: "usb", deviceType: "usb_interface"},
		{path: "/devices/pci0/usb1/1-2/1-2:1.0/adapter", subsystem: "scsi", scsiType: []byte("0\n")},
		{path: "/devices/pci0/usb1/1-2/1-2:1.0/adapter/block", subsystem: "none"},
		{path: "/devices/pci0/usb1/1-2/1-2:1.0/adapter/block/disk", subsystem: "block"},
	}}
	input, err := classifyLinuxStorageCapture(capture, []linuxStorageCanonicalPath{row})
	if err != nil || input.Nodes[0].Transport != "usb" || !reflect.DeepEqual(input.Nodes[0].USBAncestors, []string{"usb:1-2", "usb:usb1"}) {
		t.Fatal("USB device/interface distinction or ancestor order mismatch")
	}
	row.ancestors[3].deviceType = "usb_interface"
	row.ancestors[2].deviceType = "usb_interface"
	if _, err = classifyLinuxStorageCapture(capture, []linuxStorageCanonicalPath{row}); err == nil {
		t.Fatal("interfaces without any observed USB device were accepted")
	}
}

func TestStorageClassificationStackedContext(t *testing.T) {
	capture, paths := classificationTestFacts()
	capture.nodes[0].holders = []string{"mapped"}
	mapped := classificationTestRaw("mapped", "253:0")
	mapped.slaves = []string{"disk"}
	capture.registry = append(capture.registry, "mapped")
	capture.nodes = append(capture.nodes, mapped)
	paths = append(paths, linuxStorageCanonicalPath{name: "mapped", number: []byte("253:0\n"), ancestors: []linuxStorageAncestor{
		{path: "/devices", subsystem: "none"}, {path: "/devices/virtual", subsystem: "none"},
		{path: "/devices/virtual/block", subsystem: "none"}, {path: "/devices/virtual/block/mapped", subsystem: "block"},
	}})
	input, err := classifyLinuxStorageCapture(capture, paths)
	if err != nil || input.Nodes[1].Kind != "mapper" || input.Nodes[1].Transport != "virtual" || !reflect.DeepEqual(input.Nodes[1].Slaves, []string{"disk"}) {
		t.Fatal("observed stacked topology was not retained")
	}
	capture.nodes[0].holders = []string{}
	capture.nodes[1].slaves = []string{}
	if _, err = classifyLinuxStorageCapture(capture, paths); err == nil {
		t.Fatal("unrecognized unstacked virtual device appeared physical")
	}
	capture.nodes[0].holders = []string{"mapped"}
	capture.nodes[0].slaves = []string{"mapped"}
	capture.nodes[1].holders = []string{"disk"}
	capture.nodes[1].slaves = []string{"disk"}
	if _, err = classifyLinuxStorageCapture(capture, paths); err == nil {
		t.Fatal("cyclic stacked dependency accepted")
	}
}

func TestStorageClassificationRejectsCanonicalAmbiguity(t *testing.T) {
	for _, which := range []string{
		"nil_paths", "missing_chain", "missing_prefix", "noncanonical_dot", "outside_devices", "path_limit", "component_limit",
		"wrong_name", "wrong_number", "number_overflow", "missing_subsystem", "fake_devices_subsystem", "wrong_leaf_subsystem",
		"conflicting_transports", "unobserved_usb_type", "unexpected_device_type", "scsi_type_on_other_subsystem", "scsi_type_overflow",
		"unknown_virtual", "loop_name", "loop_major",
	} {
		t.Run(which, func(t *testing.T) {
			capture, paths := classificationTestFacts()
			switch which {
			case "nil_paths":
				paths = nil
			case "missing_chain":
				paths[0].ancestors = nil
			case "missing_prefix":
				paths[0].ancestors = paths[0].ancestors[1:]
			case "noncanonical_dot":
				paths[0].ancestors[4].path = "/devices/pci0/adapter/./disk"
			case "outside_devices":
				paths[0].ancestors[4].path = "/elsewhere/pci0/adapter/block/disk"
			case "path_limit":
				paths[0].ancestors[4].path = "/devices/" + strings.Repeat("x", 4096) + "/disk"
			case "component_limit":
				paths[0].ancestors[4].path = "/devices/" + strings.Repeat("x", 256) + "/adapter/block/disk"
			case "wrong_name":
				paths[0].name = "other"
			case "wrong_number":
				paths[0].number = []byte("8:1\n")
			case "number_overflow":
				paths[0].number = []byte("4294967296:0\n")
			case "missing_subsystem":
				paths[0].ancestors[1].subsystem = ""
			case "fake_devices_subsystem":
				paths[0].ancestors[0].subsystem = "usb"
				paths[0].ancestors[0].deviceType = "usb_device"
			case "wrong_leaf_subsystem":
				paths[0].ancestors[4].subsystem = "pci"
			case "conflicting_transports":
				paths[0].ancestors[1].subsystem = "nvme"
			case "unobserved_usb_type":
				paths[0].ancestors[2].subsystem = "usb"
			case "unexpected_device_type":
				paths[0].ancestors[2].deviceType = "usb_device"
			case "scsi_type_on_other_subsystem":
				paths[0].ancestors[2].scsiType = []byte("0\n")
			case "scsi_type_overflow":
				paths[0].ancestors[2].subsystem = "scsi"
				paths[0].ancestors[2].scsiType = []byte("32\n")
			case "unknown_virtual":
				paths[0].ancestors = []linuxStorageAncestor{{path: "/devices", subsystem: "none"}, {path: "/devices/virtual", subsystem: "none"}, {path: "/devices/virtual/block", subsystem: "none"}, {path: "/devices/virtual/block/disk", subsystem: "block"}}
			case "loop_name":
				capture.registry[0] = "loop0"
				capture.nodes[0].name = "loop0"
				paths[0].name = "loop0"
			case "loop_major":
				capture.nodes[0].number = []byte("7:0\n")
				paths[0].number = []byte("7:0\n")
			}
			got, err := classifyLinuxStorageCapture(capture, paths)
			if err == nil || !reflect.DeepEqual(got, linuxStorageMetadataInput{}) {
				t.Fatal("ambiguous canonical source retained classified facts")
			}
		})
	}
}

func TestStorageClassificationRejectsParentAmbiguity(t *testing.T) {
	for _, which := range []string{"unregistered_parent", "partition_parent", "whole_with_parent", "partition_without_parent", "shared_path_drift", "inherited_attribute_forgery", "missing_parent_geometry", "slave_absence_conflict"} {
		t.Run(which, func(t *testing.T) {
			capture, paths := classificationTestPartition()
			switch which {
			case "unregistered_parent":
				paths = paths[1:]
				capture.registry = capture.registry[1:]
				capture.nodes = capture.nodes[1:]
			case "partition_parent":
				capture.nodes[0].partition = []byte("2\n")
				capture.nodes[0].logicalBlock = nil
				capture.nodes[0].removable = nil
			case "whole_with_parent":
				capture.nodes[1].partition = nil
				capture.nodes[1].partitionSlavesAbsent = false
				capture.nodes[1].logicalBlock = []byte("4096\n")
				capture.nodes[1].removable = []byte("0\n")
			case "partition_without_parent":
				paths[1] = classificationTestPath("slice", "8:1")
			case "shared_path_drift":
				paths[1].ancestors[2].subsystem = "nvme"
			case "inherited_attribute_forgery":
				capture.nodes[1].logicalBlock = []byte("512\n")
			case "missing_parent_geometry":
				capture.nodes[0].logicalBlock = nil
			case "slave_absence_conflict":
				capture.nodes[1].slaves = []string{"disk"}
			}
			got, err := classifyLinuxStorageCapture(capture, paths)
			if err == nil || !reflect.DeepEqual(got, linuxStorageMetadataInput{}) {
				t.Fatal("unbound parent context retained facts")
			}
		})
	}
}

func TestStorageClassificationRejectsRawAmbiguity(t *testing.T) {
	for _, which := range []string{"missing_registry", "missing_node", "unknown_node", "nil_relation", "duplicate_number", "scalar_overflow", "invalid_read_only", "active_swap", "unknown_mount", "bad_mount", "oversized_scalar"} {
		t.Run(which, func(t *testing.T) {
			capture, paths := classificationTestFacts()
			switch which {
			case "missing_registry":
				capture.registry = nil
			case "missing_node":
				capture.nodes = nil
			case "unknown_node":
				capture.nodes[0].name = "other"
			case "nil_relation":
				capture.nodes[0].holders = nil
			case "duplicate_number":
				capture.registry = append(capture.registry, "other")
				capture.nodes = append(capture.nodes, classificationTestRaw("other", "8:0"))
				paths = append(paths, classificationTestPath("other", "8:0"))
			case "scalar_overflow":
				capture.nodes[0].sectors = []byte("36028797018963968\n")
			case "invalid_read_only":
				capture.nodes[0].readOnly = []byte("2\n")
			case "active_swap":
				capture.swaps = []byte("Filename Type Size Used Priority\n/fixture file 4 0 -1\n")
			case "unknown_mount":
				capture.mountInfo = []byte("1 0 8:9 / / rw - ext4 /dev/fixture rw\n")
			case "bad_mount":
				capture.mountInfo = []byte("incomplete\n")
			case "oversized_scalar":
				capture.nodes[0].logicalBlock = []byte(strings.Repeat("1", 257))
			}
			got, err := classifyLinuxStorageCapture(capture, paths)
			if err == nil || !reflect.DeepEqual(got, linuxStorageMetadataInput{}) {
				t.Fatal("malformed raw input retained classified facts")
			}
		})
	}
}

func classificationTestChain(path string, subsystems map[int]string) []linuxStorageAncestor {
	parts := strings.Split(path[1:], "/")
	chain := make([]linuxStorageAncestor, len(parts))
	for i := range parts {
		subsystem := "none"
		if i == len(parts)-1 {
			subsystem = "block"
		}
		if value, ok := subsystems[i]; ok {
			subsystem = value
		}
		chain[i] = linuxStorageAncestor{path: "/" + strings.Join(parts[:i+1], "/"), subsystem: subsystem}
		if subsystem == "usb" {
			chain[i].deviceType = "usb_device"
		}
	}
	return chain
}
func TestStorageClassificationFiniteBounds(t *testing.T) {
	for _, which := range []string{"depth64", "depth65", "ancestors1024", "ancestors1025", "edges1024", "edges1025", "nodes257"} {
		t.Run(which, func(t *testing.T) {
			capture, paths := classificationTestFacts()
			allowed := which == "depth64" || which == "ancestors1024" || which == "edges1024"
			if strings.HasPrefix(which, "depth") {
				depth := 64
				if which == "depth65" {
					depth = 65
				}
				parts := []string{"devices"}
				for i := 1; i < depth-1; i++ {
					parts = append(parts, "level"+strconv.Itoa(i))
				}
				parts = append(parts, "disk")
				paths[0].ancestors = classificationTestChain("/"+strings.Join(parts, "/"), map[int]string{1: "virtio"})
			} else {
				count := 256
				if which == "nodes257" {
					count = 257
				}
				capture.registry = []string{}
				capture.nodes = []linuxStorageCapturedNode{}
				paths = []linuxStorageCanonicalPath{}
				for i := 0; i < count; i++ {
					name := "disk" + strconv.Itoa(i)
					number := "8:" + strconv.Itoa(i)
					capture.registry = append(capture.registry, name)
					capture.nodes = append(capture.nodes, classificationTestRaw(name, number))
					path := "/devices/controller" + strconv.Itoa(i) + "/adapter/block/" + name
					subsystems := map[int]string{1: "pci", 2: "virtio"}
					if i == 255 && which == "ancestors1024" {
						path = "/devices/controller255/block/" + name
						subsystems = map[int]string{1: "virtio"}
					}
					if strings.HasPrefix(which, "edges") {
						path = "/devices/usb1/1-2/1-2.3/1-2.3.4/block/" + name
						subsystems = map[int]string{1: "usb", 2: "usb", 3: "usb", 4: "usb"}
						if i == 0 && which == "edges1025" {
							path = "/devices/usb1/1-2/1-2.3/1-2.3.4/1-2.3.4.5/block/" + name
							subsystems[5] = "usb"
						}
					}
					paths = append(paths, linuxStorageCanonicalPath{name: name, number: []byte(number + "\n"), ancestors: classificationTestChain(path, subsystems)})
				}
			}
			got, err := classifyLinuxStorageCapture(capture, paths)
			if allowed {
				if err != nil || len(got.Nodes) != len(capture.nodes) {
					t.Fatal("literal finite boundary was rejected")
				}
			} else if err == nil || !reflect.DeepEqual(got, linuxStorageMetadataInput{}) {
				t.Fatal("over-bound input retained facts")
			}
		})
	}
}

func TestStorageClassificationOwnsResults(t *testing.T) {
	capture, paths := classificationTestPartition()
	got, err := classifyLinuxStorageCapture(capture, paths)
	if err != nil {
		t.Fatal("classification failed")
	}
	capture.registry[0] = "changed"
	capture.nodes[0].number[0] = '9'
	capture.nodes[0].logicalBlock[0] = '9'
	capture.nodes[0].removable[0] = '0'
	capture.nodes[1].partition[0] = '9'
	capture.mountInfo[0] = '9'
	capture.swaps[0] = 'X'
	paths[0].number[0] = '9'
	paths[0].ancestors[2].subsystem = "usb"
	if got.Registry[0] != "disk" || string(got.Nodes[0].Number) != "8:0\n" || string(got.Nodes[0].LogicalBlock) != "4096\n" ||
		string(got.Nodes[1].LogicalBlock) != "4096\n" || string(got.Nodes[1].Removable) != "1\n" || string(got.Nodes[1].Partition) != "1\n" ||
		!reflect.DeepEqual(got.Nodes[0].PhysicalChildren, []string{"slice"}) || got.Nodes[0].Transport != "virtio" ||
		string(got.MountInfo) != "1 0 0:1 / / rw - tmpfs tmpfs rw\n" || string(got.Swaps) != "Filename Type Size Used Priority\n" {
		t.Fatal("caller mutation changed owned classification")
	}
}

func TestStorageClassificationDisklessRemainsUnadmitted(t *testing.T) {
	capture, unused := classificationTestFacts()
	_ = unused
	capture.registry = []string{}
	capture.nodes = []linuxStorageCapturedNode{}
	input, err := classifyLinuxStorageCapture(capture, []linuxStorageCanonicalPath{})
	if err != nil || input.Registry == nil || input.Nodes == nil || len(input.Nodes) != 0 || input.BootBackingNumbers != nil {
		t.Fatal("known empty registry was confused with unknown boot")
	}
	if _, err = parseLinuxStorageMetadata(input); err == nil {
		t.Fatal("RAM-root alone fabricated a boot-origin record")
	}
}

func TestStorageClassificationUSBPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, mode, want string
		reject           bool
	}{
		{"rom", "rom", "optical", false}, {"magneto_optical", "magneto", "optical", false},
		{"scsi_nvme_conflict", "scsi_nvme", "", true}, {"nvme_virtio_conflict", "nvme_virtio", "", true},
		{"single_nvme", "nvme", "usb", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture, _ := classificationTestFacts()
			row := linuxStorageCanonicalPath{name: "disk", number: []byte("8:0\n"),
				ancestors: classificationTestChain("/devices/usb1/1-2/adapter/bridge/block/disk", map[int]string{1: "usb", 2: "usb", 3: "scsi"})}
			row.ancestors[3].scsiType = []byte("0\n")
			switch tc.mode {
			case "rom":
				row.ancestors[3].scsiType = []byte("5\n")
			case "magneto":
				row.ancestors[3].scsiType = []byte("7\n")
			case "scsi_nvme":
				row.ancestors[4].subsystem = "nvme"
			case "nvme_virtio":
				row.ancestors[3].subsystem = "nvme"
				row.ancestors[3].scsiType = nil
				row.ancestors[4].subsystem = "virtio"
			case "nvme":
				row.ancestors[3].subsystem = "nvme"
				row.ancestors[3].scsiType = nil
			}
			input, err := classifyLinuxStorageCapture(capture, []linuxStorageCanonicalPath{row})
			if tc.reject {
				if err == nil || !reflect.DeepEqual(input, linuxStorageMetadataInput{}) {
					t.Fatal("USB ancestry hid conflicting non-USB transports")
				}
				return
			}
			if err != nil || len(input.Nodes) != 1 || input.Nodes[0].Transport != tc.want ||
				!reflect.DeepEqual(input.Nodes[0].USBAncestors, []string{"usb:1-2", "usb:usb1"}) {
				t.Fatal("optical/USB precedence lost independently expected context or USB ancestry")
			}
		})
	}
}
