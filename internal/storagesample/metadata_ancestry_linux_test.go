//go:build linux

package storagesample

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// This builds complete planning inputs only; it never claims a raw capture or
// opens the generated canonical directories. Native owner tests use real trees.
func ancestryPlanLinuxFixture(t *testing.T, nodes, ancestors, edges int) (*metadataTestFixture, linuxStorageCapture) {
	t.Helper()
	f := emptyAncestryFixture(t)
	capture := linuxStorageCapture{registry: []string{}, nodes: []linuxStorageCapturedNode{}}
	for i := 0; i < nodes; i++ {
		name := "disk" + strconv.Itoa(i)
		capture.registry = append(capture.registry, name)
		capture.nodes = append(capture.nodes, linuxStorageCapturedNode{name: name, holders: []string{}, slaves: []string{}})
		count := (ancestors - 1) / nodes
		if i < (ancestors-1)%nodes {
			count++
		}
		if count < 2 || count > 63 {
			t.Fatal("planning fixture depth is invalid")
		}
		parts := []string{"devices", "branch" + strconv.Itoa(i)}
		for j := 1; j < count-1; j++ {
			parts = append(parts, "p"+strconv.Itoa(j))
		}
		parts = append(parts, name)
		ancestryFixtureLink(f, "sysfs/class/block/"+name, "../../"+strings.Join(parts, "/"))
	}
	remaining := edges
	for i := 0; i < nodes && remaining > 0; i++ {
		for j := i + 1; j < nodes && remaining > 0; j++ {
			capture.nodes[i].holders = append(capture.nodes[i].holders, capture.registry[j])
			capture.nodes[j].slaves = append(capture.nodes[j].slaves, capture.registry[i])
			remaining -= 2
		}
	}
	if remaining != 0 {
		t.Fatal("planning fixture edge count differs")
	}
	return f, capture
}

func TestStorageAncestryLinuxCompletePlanLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		n, a, e int
		good    bool
	}{
		{"ancestor_1024", 17, 1024, 0, true}, {"ancestor_1025", 17, 1025, 0, false},
		{"reserve_10000", 65, 1000, 920, true}, {"reserve_10001", 66, 1000, 904, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, capture := ancestryPlanLinuxFixture(t, tc.n, tc.a, tc.e)
			root, err := f.operations().openRoot("sysfs")
			if err != nil {
				t.Fatal("planning root setup failed")
			}
			plan, err := planStorageMetadataAncestry(context.Background(), root, capture)
			if tc.good {
				if err != nil || len(plan.paths) != tc.a || len(plan.chains) != tc.n {
					t.Fatal("complete boundary plan rejected or truncated")
				}
			} else if err == nil || !reflect.DeepEqual(plan, storageMetadataAncestryPlan{}) {
				t.Fatal("overflow plan admitted")
			}
			if len(f.owners) != 0 {
				t.Fatal("planning acquired ancestry owners")
			}
			if root.Close() != nil {
				t.Fatal("planning root close failed")
			}
			f.allClosed()
		})
	}
}

func TestStorageAncestryLinuxReserveBeforeNewOwners(t *testing.T) {
	f, capture := ancestryPlanLinuxFixture(t, 66, 1000, 904)
	root, err := f.operations().openRoot("sysfs")
	if err != nil {
		t.Fatal("planning root setup failed")
	}
	owners := []io.Closer{}
	state := &storageMetadataAncestry{owners: &owners}
	if err := state.acquire(context.Background(), root, capture, map[string]storageMetadataAnchor{}); err == nil || state.initialized || len(owners) != 0 || len(f.owners) != 0 {
		t.Fatal("failed reservation acquired new ancestry owners")
	}
	if root.Close() != nil {
		t.Fatal("planning root close failed")
	}
	f.allClosed()
}

func ancestryFixtureLink(f *metadataTestFixture, path, target string) {
	f.t.Helper()
	f.next++
	name := filepath.Join(f.directory, "link-"+strconv.Itoa(f.next))
	if os.Symlink(target, name) != nil {
		f.t.Fatal("owned link identity setup failed")
	}
	info, err := os.Lstat(name)
	if err != nil {
		f.t.Fatal("owned link identity observation failed")
	}
	f.objects[path] = &metadataTestObject{info: info, linkTarget: target}
}

func ancestryFixtureSubsystem(f *metadataTestFixture, path, subsystem string) {
	f.t.Helper()
	parts := strings.Split(path, "/")
	kind := "bus"
	if subsystem == "block" {
		kind = "class"
	}
	destination := "sysfs/" + kind + "/" + subsystem
	if f.objects[destination] == nil {
		f.dir(destination)
	}
	ancestryFixtureLink(f, "sysfs/"+path+"/subsystem", strings.Repeat("../", len(parts))+kind+"/"+subsystem)
}

func newAncestryLinuxFixture(t *testing.T, profile string) (*metadataTestFixture, string) {
	t.Helper()
	f := newMetadataTestFixture(t)
	leaf := "devices/pci/scsi/block/disk"
	switch profile {
	case "usb", "usb_optical":
		leaf = "devices/pci/usb1/1-2/1-2:1.0/scsi/block/disk"
	case "nvme":
		leaf = "devices/pci/nvme/block/disk"
	case "virtio":
		leaf = "devices/pci/virtio/block/disk"
	case "unknown":
		leaf = "devices/platform/block/disk"
	case "virtual":
		leaf = "devices/virtual/block/disk"
	}
	parts := strings.Split(leaf, "/")
	for i := range parts {
		path := strings.Join(parts[:i+1], "/")
		if i == len(parts)-1 {
			f.objects["sysfs/"+path] = f.objects["sysfs/class/block/disk/."]
		} else {
			f.dir("sysfs/" + path)
		}
		switch parts[i] {
		case "pci":
			ancestryFixtureSubsystem(f, path, "pci")
		case "scsi":
			ancestryFixtureSubsystem(f, path, "scsi")
			value := "0\n"
			if profile == "usb_optical" {
				value = "5\n"
			}
			f.file("sysfs/"+path+"/type", value)
		case "usb1", "1-2":
			ancestryFixtureSubsystem(f, path, "usb")
			f.file("sysfs/"+path+"/uevent", "DEVTYPE=usb_device\nPRODUCT=1/2/3\n")
		case "1-2:1.0":
			ancestryFixtureSubsystem(f, path, "usb")
			f.file("sysfs/"+path+"/uevent", "DEVTYPE=usb_interface\n")
		case "nvme":
			ancestryFixtureSubsystem(f, path, "nvme")
		case "virtio":
			ancestryFixtureSubsystem(f, path, "virtio")
		case "disk":
			ancestryFixtureSubsystem(f, path, "block")
		}
	}
	f.objects["sysfs/"+leaf+"/dev"] = f.objects["sysfs/class/block/disk/dev"]
	ancestryFixtureLink(f, "sysfs/class/block/disk", "../../"+leaf)
	if profile == "partition" {
		f.dir("sysfs/class/block", "disk", "disk1")
		f.node("disk1", "8:1")
		f.file("sysfs/class/block/disk1/partition", "1\n")
		delete(f.objects, "sysfs/class/block/disk1/removable")
		delete(f.objects, "sysfs/class/block/disk1/queue/logical_block_size")
		delete(f.objects, "sysfs/class/block/disk1/slaves")
		child := leaf + "/disk1"
		f.objects["sysfs/"+child] = f.objects["sysfs/class/block/disk1/."]
		f.objects["sysfs/"+child+"/dev"] = f.objects["sysfs/class/block/disk1/dev"]
		ancestryFixtureSubsystem(f, child, "block")
		ancestryFixtureLink(f, "sysfs/class/block/disk1", "../../"+child)
	}
	return f, leaf
}

func TestStorageAncestryLinuxClosedClassification(t *testing.T) {
	for _, tc := range []struct {
		name, transport string
		nodes           int
	}{
		{"plain", "scsi", 1}, {"partition", "scsi", 2}, {"usb", "usb", 1}, {"usb_optical", "optical", 1},
		{"nvme", "nvme", 1}, {"virtio", "virtio", 1}, {"unknown", "unknown", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := newAncestryLinuxFixture(t, tc.name)
			out, err := captureClassifiedLinuxStorageMetadata(context.Background(), f.operations())
			if err != nil || len(out.Nodes) != tc.nodes || out.Nodes[0].Transport != tc.transport || out.BootBackingNumbers != nil {
				t.Fatal("owned complete classification differs")
			}
			if tc.name == "partition" && (out.Nodes[1].Kind != "partition" || out.Nodes[1].Parent != "disk" || string(out.Nodes[1].LogicalBlock) != "512\n") {
				t.Fatal("partition did not inherit registered whole geometry")
			}
			if strings.HasPrefix(tc.name, "usb") && !reflect.DeepEqual(out.Nodes[0].USBAncestors, []string{"usb:1-2", "usb:usb1"}) {
				t.Fatal("USB exclusion ancestry lost")
			}
			if _, err := parseLinuxStorageMetadata(out); err == nil {
				t.Fatal("unknown boot admitted")
			}
			out.BootBackingNumbers = []string{}
			snapshot, err := parseLinuxStorageMetadata(out)
			if err != nil {
				t.Fatal("explicit fixture boot parse rejected")
			}
			if facts := snapshot.discovery(); facts.Complete || facts.BackingComplete {
				t.Fatal("metadata gained target authority")
			}
			f.allClosed()
		})
	}
}

func TestStorageAncestryLinuxAdmissionFailures(t *testing.T) {
	for _, name := range []string{
		"missing_class", "class_not_link", "class_escape", "class_wrong_leaf", "canonical_missing", "canonical_not_directory",
		"canonical_alias", "canonical_wrong_target", "canonical_wrong_number", "subsystem_none", "subsystem_class_none",
		"subsystem_missing_target", "subsystem_target_not_directory", "subsystem_target_alias", "subsystem_unsafe_target",
		"subsystem_not_link", "scsi_bad_scalar", "scsi_type_overflow", "scsi_read_overflow", "scsi_no_eof", "usb_missing_type", "usb_duplicate_type", "usb_read_overflow", "unsupported_virtual", "loop_number", "active_swap",
	} {
		t.Run(name, func(t *testing.T) {
			profile := "plain"
			if strings.HasPrefix(name, "usb_") {
				profile = "usb"
			}
			if name == "unsupported_virtual" {
				profile = "virtual"
			}
			f, leaf := newAncestryLinuxFixture(t, profile)
			switch name {
			case "missing_class":
				delete(f.objects, "sysfs/class/block/disk")
			case "class_not_link":
				f.file("sysfs/class/block/disk", "not-link")
			case "class_escape":
				ancestryFixtureLink(f, "sysfs/class/block/disk", "../../../outside/disk")
			case "class_wrong_leaf":
				ancestryFixtureLink(f, "sysfs/class/block/disk", "../../devices/pci/scsi/block/other")
			case "canonical_missing":
				delete(f.objects, "sysfs/devices/pci")
			case "canonical_not_directory":
				f.file("sysfs/devices/pci", "not-directory")
			case "canonical_alias":
				f.objects["sysfs/devices/pci"] = f.objects["sysfs/devices"]
			case "canonical_wrong_target":
				f.dir("sysfs/" + leaf)
			case "canonical_wrong_number":
				f.file("sysfs/"+leaf+"/dev", "8:99\n")
			case "subsystem_none":
				f.dir("sysfs/bus/none")
				ancestryFixtureLink(f, "sysfs/devices/pci/subsystem", "../../bus/none")
			case "subsystem_class_none":
				f.dir("sysfs/class/none")
				ancestryFixtureLink(f, "sysfs/devices/pci/subsystem", "../../class/none")
			case "subsystem_missing_target":
				delete(f.objects, "sysfs/bus/scsi")
			case "subsystem_target_not_directory":
				f.file("sysfs/bus/scsi", "not-directory")
			case "subsystem_target_alias":
				f.objects["sysfs/bus/scsi"] = f.objects["sysfs/bus/pci"]
			case "subsystem_unsafe_target":
				ancestryFixtureLink(f, "sysfs/devices/pci/subsystem", "../../../bus/pci")
			case "subsystem_not_link":
				f.file("sysfs/devices/pci/subsystem", "pci")
			case "scsi_bad_scalar":
				f.file("sysfs/devices/pci/scsi/type", "-1\n")
			case "scsi_type_overflow":
				f.file("sysfs/devices/pci/scsi/type", "32\n")
			case "scsi_read_overflow":
				f.file("sysfs/devices/pci/scsi/type", strings.Repeat("0", 257))
			case "scsi_no_eof":
				f.hook = func(stage, path string) {
					if stage == "read" && path == "sysfs/devices/pci/scsi/type" {
						for _, owner := range f.owners {
							if owner.kind == "file" && owner.path == path && owner.closes == 0 {
								owner.offset = 0
							}
						}
					}
				}
			case "usb_missing_type":
				f.file("sysfs/devices/pci/usb1/uevent", "PRODUCT=1/2/3\n")
			case "usb_duplicate_type":
				f.file("sysfs/devices/pci/usb1/uevent", "DEVTYPE=usb_device\nDEVTYPE=usb_device\n")
			case "usb_read_overflow":
				f.file("sysfs/devices/pci/usb1/uevent", "DEVTYPE=usb_device\nX="+strings.Repeat("x", 4096)+"\n")
			case "loop_number":
				f.file("sysfs/class/block/disk/dev", "7:0\n")
			case "active_swap":
				f.file("proc/swaps", "Filename Type Size Used Priority\n/dev/owned-fixture partition 1 0 -2\n")
			}
			out, err := captureClassifiedLinuxStorageMetadata(context.Background(), f.operations())
			if err == nil || !reflect.DeepEqual(out, linuxStorageMetadataInput{}) {
				t.Fatal("unsafe ancestry admitted")
			}
			if strings.Contains(err.Error(), metadataTestFault.Error()) {
				t.Fatal("raw provider error escaped")
			}
			f.allClosed()
		})
	}
}

func TestStorageAncestryLinuxObservedAbsence(t *testing.T) {
	for _, name := range []string{"scsi_type_absent", "subsystem_unavailable", "absence_appears"} {
		t.Run(name, func(t *testing.T) {
			f, _ := newAncestryLinuxFixture(t, "plain")
			switch name {
			case "scsi_type_absent":
				delete(f.objects, "sysfs/devices/pci/scsi/type")
			case "subsystem_unavailable":
				f.fail["lstat:sysfs/devices/subsystem"] = true
			case "absence_appears":
				f.hook = func(stage, path string) {
					if stage == "lstat" && path == "sysfs/devices/subsystem" && f.calls[stage+":"+path] == 2 {
						ancestryFixtureLink(f, path, "../bus/pci")
					}
				}
			}
			out, err := captureClassifiedLinuxStorageMetadata(context.Background(), f.operations())
			if name == "scsi_type_absent" {
				if err != nil || out.Nodes[0].Transport != "unknown" {
					t.Fatal("observed missing SCSI type was guessed")
				}
			} else if err == nil || !reflect.DeepEqual(out, linuxStorageMetadataInput{}) {
				t.Fatal("unavailable or changing absence admitted")
			}
			f.allClosed()
		})
	}
}

func TestStorageAncestryLinuxProviderFailures(t *testing.T) {
	for _, tc := range []struct{ name, stage, path string }{
		{"link_read", "readlink", "class/block/disk"},
		{"ancestor_open", "open-path", "devices/pci"}, {"ancestor_stat", "path-stat", "devices/pci"},
		{"subsystem_open", "open-path", "bus/scsi"}, {"subsystem_read", "readlink", "devices/pci/subsystem"},
		{"scalar_open", "open-path", "devices/pci/scsi/type"}, {"scalar_readable", "open-readable", "devices/pci/scsi/type"},
		{"scalar_read", "read", "devices/pci/scsi/type"}, {"scalar_stat", "file-stat", "devices/pci/scsi/type"},
		{"scalar_close", "close-file", "devices/pci/scsi/type"}, {"scalar_path_close", "close-path", "devices/pci/scsi/type"},
		{"number_read", "read", "devices/pci/scsi/block/disk/dev"}, {"ancestor_close", "close-path", "devices/pci"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := newAncestryLinuxFixture(t, "plain")
			key := tc.stage + ":sysfs/" + tc.path
			f.fail[key] = true
			out, err := captureClassifiedLinuxStorageMetadata(context.Background(), f.operations())
			if f.calls[key] == 0 || err == nil || !reflect.DeepEqual(out, linuxStorageMetadataInput{}) {
				t.Fatal("provider failure not observed or not closed")
			}
			if strings.Contains(err.Error(), metadataTestFault.Error()) {
				t.Fatal("provider diagnostic disclosed")
			}
			f.allClosed()
		})
	}
}

func TestStorageAncestryLinuxCancellationBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, stage, path string }{
		{"link", "readlink", "class/block/disk"}, {"ancestor", "open-path", "devices/pci"},
		{"readable", "open-readable", "devices/pci/scsi/block/disk/dev"}, {"read", "read", "devices/pci/scsi/block/disk/dev"},
		{"reader_close", "close-file", "devices/pci/scsi/block/disk/dev"}, {"anchor_close", "close-path", "devices/pci"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := newAncestryLinuxFixture(t, "plain")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observed := false
			f.hook = func(stage, path string) {
				if stage == tc.stage && path == "sysfs/"+tc.path && !observed {
					observed = true
					cancel()
				}
			}
			out, err := captureClassifiedLinuxStorageMetadata(ctx, f.operations())
			if !observed || err == nil || !reflect.DeepEqual(out, linuxStorageMetadataInput{}) {
				t.Fatal("successful provider cancellation admitted")
			}
			f.allClosed()
		})
	}
}

func TestStorageAncestryLinuxLinkReplacement(t *testing.T) {
	for _, name := range []string{"inode_replaced", "target_changed"} {
		t.Run(name, func(t *testing.T) {
			f, _ := newAncestryLinuxFixture(t, "plain")
			observed := false
			f.hook = func(stage, path string) {
				if stage != "readlink" || path != "sysfs/class/block/disk" || observed {
					return
				}
				observed = true
				if name == "inode_replaced" {
					ancestryFixtureLink(f, path, "../../devices/pci/scsi/block/disk")
				} else {
					f.objects[path].linkTarget = "../../devices/pci/scsi/block/other"
				}
			}
			out, err := captureClassifiedLinuxStorageMetadata(context.Background(), f.operations())
			if !observed || err == nil || !reflect.DeepEqual(out, linuxStorageMetadataInput{}) {
				t.Fatal("changed link observation admitted")
			}
			f.allClosed()
		})
	}
}

func TestStorageAncestryLinuxFinalDrift(t *testing.T) {
	for _, name := range []string{"ancestor_identity", "subsystem_identity", "subsystem_text", "absent_link_appears", "scsi_type", "raw_registry", "raw_number", "final_number_encoding"} {
		t.Run(name, func(t *testing.T) {
			f, leaf := newAncestryLinuxFixture(t, "plain")
			out, err := withLinuxStorageMetadata(context.Background(), f.operations(), func(view *storageMetadataView) error {
				if _, err := view.Classify(context.Background()); err != nil {
					return err
				}
				switch name {
				case "ancestor_identity":
					f.dir("sysfs/devices/pci")
				case "subsystem_identity":
					f.dir("sysfs/bus/scsi")
				case "subsystem_text":
					f.objects["sysfs/devices/pci/subsystem"].linkTarget = "../../bus/virtio"
				case "absent_link_appears":
					ancestryFixtureLink(f, "sysfs/devices/subsystem", "../bus/pci")
				case "scsi_type":
					f.objects["sysfs/devices/pci/scsi/type"].data = []byte("5\n")
				case "raw_registry":
					f.dir("sysfs/class/block", "disk", "new")
				case "raw_number":
					f.objects["sysfs/class/block/disk/dev"].data = []byte("8:1\n")
				case "final_number_encoding":
					f.hook = func(stage, path string) {
						if stage == "open-readable" && path == "sysfs/"+leaf+"/dev" {
							f.objects[path].data = []byte("8:0")
						}
					}
				}
				return nil
			})
			if err == nil || !reflect.DeepEqual(out, linuxStorageCapture{}) {
				t.Fatal("final drift escaped scope")
			}
			f.allClosed()
		})
	}
}

func TestStorageAncestryLinuxRepeatedViewCalls(t *testing.T) {
	f, _ := newAncestryLinuxFixture(t, "usb")
	_, err := withLinuxStorageMetadata(context.Background(), f.operations(), func(view *storageMetadataView) error {
		first, err := view.Classify(context.Background())
		if err != nil {
			return err
		}
		owners := len(f.owners)
		first.Nodes[0].Number[0] = '9'
		first.Nodes[0].USBAncestors[0] = "forged"
		second, err := view.Classify(context.Background())
		if err != nil {
			return err
		}
		if string(second.Nodes[0].Number) != "8:0\n" || second.Nodes[0].USBAncestors[0] != "usb:1-2" {
			t.Fatal("classified result aliases held data")
		}
		if err := view.Verify(context.Background()); err != nil {
			return err
		}
		if len(f.owners) != owners {
			t.Fatal("repeat view call acquired unreserved owners")
		}
		return nil
	})
	if err != nil {
		t.Fatal("repeat view fixture failed")
	}
	f.allClosed()
}

type ancestryAliasRoot struct {
	storageMetadataRoot
	fixture  *metadataTestFixture
	held     storageMetadataPath
	released bool
}

func (root *ancestryAliasRoot) OpenPath(path string) (storageMetadataPath, error) {
	if root.released && path == "devices" {
		for _, owner := range root.fixture.owners {
			if owner.kind == "path" && owner.path == "sysfs/class/block/disk/dev" && owner.closes == 1 {
				return owner, nil
			}
		}
	}
	if !root.released && path == "devices/pci" && root.held != nil {
		return root.held, nil
	}
	owner, err := root.storageMetadataRoot.OpenPath(path)
	if path == "devices" {
		root.held = owner
	}
	return owner, err
}

func TestStorageAncestryLinuxOwnerIdentityReuse(t *testing.T) {
	for _, name := range []string{"held_owner", "released_owner"} {
		t.Run(name, func(t *testing.T) {
			f, _ := newAncestryLinuxFixture(t, "plain")
			prior := f.operations()
			operations := storageMetadataOperations{openRoot: func(kind string) (storageMetadataRoot, error) {
				root, err := prior.openRoot(kind)
				if kind == "sysfs" && err == nil {
					return &ancestryAliasRoot{storageMetadataRoot: root, fixture: f, released: name == "released_owner"}, nil
				}
				return root, err
			}}
			out, err := captureClassifiedLinuxStorageMetadata(context.Background(), operations)
			if err == nil || !reflect.DeepEqual(out, linuxStorageMetadataInput{}) {
				t.Fatal("reused owner identity admitted")
			}
			f.allClosed()
		})
	}
}

type ancestryRealRoot struct {
	systemStorageMetadataRoot
	closes int
}

func (root *ancestryRealRoot) Close() error {
	root.closes++
	return root.systemStorageMetadataRoot.Close()
}

func ancestryRealLinuxOperations(t *testing.T) (storageMetadataOperations, string) {
	t.Helper()
	directory := t.TempDir()
	sys, proc := filepath.Join(directory, "sys"), filepath.Join(directory, "proc")
	leaf := "devices/pci/scsi/block/disk"
	for _, path := range []string{"class/block", "bus/pci", "bus/scsi", leaf + "/queue", leaf + "/holders", leaf + "/slaves"} {
		if os.MkdirAll(filepath.Join(sys, filepath.FromSlash(path)), 0700) != nil {
			t.Fatal("owned sysfs directory setup failed")
		}
	}
	if os.MkdirAll(filepath.Join(proc, "self"), 0700) != nil {
		t.Fatal("owned proc directory setup failed")
	}
	for path, data := range map[string]string{
		leaf + "/dev": "8:0\n", leaf + "/size": "16777216\n", leaf + "/ro": "0\n", leaf + "/removable": "0\n", leaf + "/queue/logical_block_size": "512\n", "devices/pci/scsi/type": "0\n",
	} {
		if os.WriteFile(filepath.Join(sys, filepath.FromSlash(path)), []byte(data), 0600) != nil {
			t.Fatal("owned scalar setup failed")
		}
	}
	for path, target := range map[string]string{
		"class/block/disk": "../../" + leaf, "devices/pci/subsystem": "../../bus/pci", "devices/pci/scsi/subsystem": "../../../bus/scsi", leaf + "/subsystem": "../../../../../class/block",
	} {
		if os.Symlink(target, filepath.Join(sys, filepath.FromSlash(path))) != nil {
			t.Fatal("owned native link setup failed")
		}
	}
	if os.WriteFile(filepath.Join(proc, "self", "mountinfo"), []byte("1 0 0:1 / / rw - tmpfs tmpfs rw\n"), 0600) != nil ||
		os.WriteFile(filepath.Join(proc, "swaps"), []byte("Filename Type Size Used Priority\n"), 0600) != nil {
		t.Fatal("owned proc facts setup failed")
	}
	roots := []*ancestryRealRoot{}
	t.Cleanup(func() {
		for _, root := range roots {
			if root.closes != 1 {
				if root.closes == 0 {
					_ = root.Close()
				}
				t.Error("native root not closed exactly once")
			}
		}
	})
	operations := storageMetadataOperations{openRoot: func(kind string) (storageMetadataRoot, error) {
		path := ""
		switch kind {
		case "sysfs":
			path = sys
		case "proc":
			path = proc
		default:
			return nil, errStorageMetadataAcquire
		}
		root, err := os.OpenRoot(path)
		if err != nil {
			return nil, err
		}
		file, err := root.Open(".")
		if err != nil {
			_ = root.Close()
			return nil, err
		}
		var facts syscall.Statfs_t
		statErr := syscall.Fstatfs(int(file.Fd()), &facts)
		closeErr := file.Close()
		if statErr != nil || closeErr != nil {
			_ = root.Close()
			return nil, errStorageMetadataAcquire
		}
		// Only this test adapter accepts its temporary filesystem. The real
		// constructor retains the closed sysfs/proc filesystem magic values.
		owned := &ancestryRealRoot{systemStorageMetadataRoot: systemStorageMetadataRoot{root, int64(facts.Type)}}
		roots = append(roots, owned)
		return owned, nil
	}}
	return operations, sys
}

func TestStorageAncestryLinuxRealClosedRoute(t *testing.T) {
	for _, name := range []string{"complete", "canonical_replacement", "escaped_class_target"} {
		t.Run(name, func(t *testing.T) {
			operations, sys := ancestryRealLinuxOperations(t)
			var classified linuxStorageMetadataInput
			_, err := withLinuxStorageMetadata(context.Background(), operations, func(view *storageMetadataView) error {
				if name == "escaped_class_target" {
					old := filepath.Join(sys, "class", "block", "disk")
					if os.Rename(old, filepath.Join(sys, "class", "disk-before")) != nil || os.Symlink("../../../outside/disk", old) != nil {
						t.Fatal("owned escaped class setup failed")
					}
				}
				var e error
				classified, e = view.Classify(context.Background())
				if e != nil {
					return e
				}
				if name == "canonical_replacement" {
					old := filepath.Join(sys, "devices", "pci", "scsi", "block", "disk")
					if os.Rename(old, old+"-before") != nil || os.Mkdir(old, 0700) != nil {
						t.Fatal("owned canonical replacement setup failed")
					}
				}
				return nil
			})
			if name == "complete" {
				if err != nil || len(classified.Nodes) != 1 || classified.Nodes[0].Transport != "scsi" || classified.BootBackingNumbers != nil {
					t.Fatal("real contained producer/classifier route failed")
				}
			} else if err == nil {
				t.Fatal("native replacement or escape admitted")
			}
		})
	}
}

func TestStorageAncestryLinuxRootReadlinkContainment(t *testing.T) {
	root, directory := metadataLinuxFixtureRoot(t)
	if os.Mkdir(filepath.Join(directory, "inside"), 0700) != nil || os.Symlink("../target", filepath.Join(directory, "inside", "link")) != nil {
		t.Fatal("owned contained readlink setup failed")
	}
	outside := t.TempDir()
	if os.Symlink("outside-value", filepath.Join(outside, "link")) != nil || os.Symlink(outside, filepath.Join(directory, "escape")) != nil {
		t.Fatal("owned external readlink fixture setup failed")
	}
	target, err := root.Readlink("inside/link")
	if err != nil || target != "../target" {
		t.Fatal("root did not return literal contained link text")
	}
	if _, err := root.Readlink("escape/link"); err == nil {
		t.Fatal("readlink followed an escaping parent")
	}
	if _, err := root.Readlink("../link"); err == nil {
		t.Fatal("readlink accepted a parent escape")
	}
	// Reading a final link itself does not validate its returned target.
	if target, err := root.Readlink("escape"); err != nil || target != outside {
		t.Fatal("final link observation semantics differ")
	}
}
