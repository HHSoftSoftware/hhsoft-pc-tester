package storagesample

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestStorageAncestryClassPathGrammar(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		good         bool
	}{
		{"direct", "../../devices/pci0000:00/0000:00:01.0/block/disk", true},
		{"shallow", "../../devices/disk", true},
		{"absolute", "/devices/disk", false},
		{"sys_absolute", "/sys/devices/disk", false},
		{"short_prefix", "../devices/disk", false},
		{"extra_parent", "../../../devices/disk", false},
		{"dot", "../../devices/./disk", false},
		{"parent", "../../devices/x/../disk", false},
		{"empty_component", "../../devices//disk", false},
		{"trailing_separator", "../../devices/disk/", false},
		{"wrong_leaf", "../../devices/other", false},
		{"wrong_root", "../../devices-other/disk", false},
		{"backslash", "../../devices/x\\y/disk", false},
		{"control", "../../devices/x\ndisk", false},
		{"unicode", "../../devices/\u00e9/disk", false},
		{"oversize_component", "../../devices/" + strings.Repeat("x", 256) + "/disk", false},
		{"depth_64", "../../devices/" + strings.Repeat("x/", 62) + "disk", true},
		{"depth_65", "../../devices/" + strings.Repeat("x/", 63) + "disk", false},
		{"target_overflow", "../../devices/" + strings.Repeat("x", 4096) + "/disk", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain, err := storageMetadataClassPath("disk", tc.target)
			if !tc.good {
				if err == nil || chain != nil {
					t.Fatal("unsafe class target admitted")
				}
				return
			}
			if err != nil || len(chain) < 2 || chain[0] != "/devices" || chain[len(chain)-1] != "/"+tc.target[6:] {
				t.Fatal("complete canonical chain differs")
			}
			for i := 1; i < len(chain); i++ {
				if !strings.HasPrefix(chain[i], chain[i-1]+"/") {
					t.Fatal("ancestor prefix omitted")
				}
			}
		})
	}
	if out, err := storageMetadataClassPath("../disk", "../../devices/disk"); err == nil || out != nil {
		t.Fatal("unsafe registry name admitted")
	}
}

func TestStorageAncestrySubsystemGrammar(t *testing.T) {
	for _, tc := range []struct {
		name, target, want string
		good               bool
	}{
		{"bus", "../../../bus/scsi", "/bus/scsi", true},
		{"class", "../../../class/block", "/class/block", true},
		{"unknown_observed", "../../../bus/unrecognized", "/bus/unrecognized", true},
		{"present_none", "../../../bus/none", "", false},
		{"present_class_none", "../../../class/none", "", false},
		{"block_wrong_namespace", "../../../bus/block", "", false},
		{"absolute", "/bus/scsi", "", false},
		{"underflow", "../../bus/scsi", "", false},
		{"escape", "../../../../bus/scsi", "", false},
		{"interior_parent", "../../../bus/x/../scsi", "", false},
		{"interior_dot", "../../../bus/./scsi", "", false},
		{"empty_component", "../../../bus//scsi", "", false},
		{"wrong_namespace", "../../../devices/scsi", "", false},
		{"extra_leaf", "../../../bus/scsi/device", "", false},
		{"unsafe_name", "../../../bus/s:csi", "", false},
		{"backslash", "../../../bus/scsi\\x", "", false},
		{"empty", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, name, err := storageMetadataSubsystemPath("/devices/pci/device", tc.target)
			if !tc.good {
				if err == nil || path != "" || name != "" {
					t.Fatal("unsafe subsystem target admitted")
				}
				return
			}
			if err != nil || path != tc.want || name == "none" || name == "" {
				t.Fatal("observed subsystem differs")
			}
		})
	}
}

func TestStorageAncestryOwnerReservation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		n, e, a int
		good    bool
	}{
		{"empty", 0, 0, 0, true}, {"representative", 64, 128, 256, true},
		{"exact_10000", 65, 920, 1000, true}, {"first_10001", 66, 904, 1000, false},
		{"raw_maximum_extension", 256, 1024, 256, false}, {"all_maxima", 256, 1024, 1024, false},
		{"negative_nodes", -1, 0, 0, false}, {"negative_edges", 0, -1, 0, false}, {"negative_ancestors", 0, 0, -1, false},
		{"node_overflow", 257, 0, 0, false}, {"edge_overflow", 0, 1025, 0, false}, {"ancestor_overflow", 0, 0, 1025, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (storageMetadataAncestryBudget(tc.n, tc.e, tc.a) == nil) != tc.good {
				t.Fatal("owner reservation boundary differs")
			}
		})
	}
	if storageMetadataMaximumOwners != 10000 {
		t.Fatal("existing owner registration cap changed")
	}
}

func TestStorageAncestryUSBFieldProtocol(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"device", "DEVTYPE=usb_device\n", "usb_device"},
		{"interface", "PRODUCT=1/2/3\nDEVTYPE=usb_interface\nTYPE=0/0/0\n", "usb_interface"},
		{"missing", "PRODUCT=1/2/3\n", ""},
		{"duplicate_type", "DEVTYPE=usb_device\nDEVTYPE=usb_device\n", ""},
		{"duplicate_other", "PRODUCT=1\nPRODUCT=2\nDEVTYPE=usb_device\n", ""},
		{"unsupported_type", "DEVTYPE=unknown\n", ""},
		{"empty_type", "DEVTYPE=\n", ""},
		{"missing_eof_newline", "DEVTYPE=usb_device", ""},
		{"empty_record", "DEVTYPE=usb_device\n\n", ""},
		{"bad_key", "devtype=usb_device\n", ""},
		{"no_equals", "DEVTYPE\n", ""},
		{"control", "DEVTYPE=usb_device\r\n", ""},
		{"nul", "DEVTYPE=usb_device\nOTHER=\x00\n", ""},
		{"oversize", "DEVTYPE=usb_device\nOTHER=" + strings.Repeat("x", 4096) + "\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := storageMetadataUSBType([]byte(tc.raw))
			if tc.want == "" {
				if err == nil || got != "" {
					t.Fatal("malformed USB facts admitted")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatal("USB device type differs")
			}
		})
	}
}

func emptyAncestryFixture(t *testing.T) *metadataTestFixture {
	f := newMetadataTestFixture(t)
	f.dir("sysfs/class/block")
	return f
}

func TestStorageAncestryClosedEmptyRoute(t *testing.T) {
	f := emptyAncestryFixture(t)
	out, err := captureClassifiedLinuxStorageMetadata(context.Background(), f.operations())
	if err != nil || out.Registry == nil || len(out.Registry) != 0 || out.Nodes == nil || len(out.Nodes) != 0 || out.BootBackingNumbers != nil {
		t.Fatal("empty descriptive route differs")
	}
	f.allClosed()
	if _, err := parseLinuxStorageMetadata(out); err == nil {
		t.Fatal("unknown boot provenance admitted")
	}
	// Supplying explicit owned fixture boot facts still cannot promote gates.
	out.BootBackingNumbers = []string{}
	snapshot, err := parseLinuxStorageMetadata(out)
	if err != nil {
		t.Fatal("owned empty fixture parse rejected")
	}
	if facts := snapshot.discovery(); facts.Complete || facts.BackingComplete {
		t.Fatal("descriptive result promoted to authority")
	}
	for key := range f.calls {
		if strings.Contains(key, "/dev/") || strings.Contains(key, "open-readable:sysfs/devices") {
			t.Fatal("empty route opened target contents")
		}
	}
}

func TestStorageAncestryViewCopiesAndExpiration(t *testing.T) {
	f := emptyAncestryFixture(t)
	var saved *storageMetadataView
	_, err := withLinuxStorageMetadata(context.Background(), f.operations(), func(view *storageMetadataView) error {
		saved = view
		first, err := view.Classify(context.Background())
		if err != nil {
			return err
		}
		count := len(f.owners)
		original := string(first.MountInfo)
		first.MountInfo[0] = 'X'
		first.Registry = append(first.Registry, "forged")
		second, err := view.Classify(context.Background())
		if err != nil {
			return err
		}
		if string(second.MountInfo) != original || len(second.Registry) != 0 || len(f.owners) != count {
			t.Fatal("repeat classification aliased or reacquired")
		}
		return view.Verify(context.Background())
	})
	if err != nil {
		t.Fatal("closed copy fixture failed")
	}
	f.allClosed()
	before := len(f.owners)
	if out, err := saved.Classify(context.Background()); err == nil || !reflect.DeepEqual(out, linuxStorageMetadataInput{}) || len(f.owners) != before {
		t.Fatal("expired view acquired or returned facts")
	}
	var absent *storageMetadataView
	if _, err := absent.Classify(context.Background()); err == nil {
		t.Fatal("nil view admitted")
	}
}

func TestStorageAncestryIgnoredFailurePoisonsScope(t *testing.T) {
	for _, name := range []string{"missing_class_link", "canceled_context", "nil_context", "reentrant", "concurrent"} {
		t.Run(name, func(t *testing.T) {
			f := emptyAncestryFixture(t)
			if name == "missing_class_link" {
				f = newMetadataTestFixture(t)
			}
			out, err := withLinuxStorageMetadata(context.Background(), f.operations(), func(view *storageMetadataView) error {
				switch name {
				case "missing_class_link":
					_, _ = view.Classify(context.Background())
				case "canceled_context":
					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					_, _ = view.Classify(ctx)
				case "nil_context":
					_, _ = view.Classify(nil)
				case "reentrant":
					called := false
					ctx := &metadataTestContext{Context: context.Background(), observe: func() {
						if !called {
							called = true
							_, _ = view.Classify(context.Background())
						}
					}}
					_, _ = view.Classify(ctx)
				case "concurrent":
					entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
					called := false
					ctx := &metadataTestContext{Context: context.Background(), observe: func() {
						if !called {
							called = true
							close(entered)
							<-release
						}
					}}
					go func() { defer close(done); _, _ = view.Classify(ctx) }()
					<-entered
					_, _ = view.Classify(context.Background())
					close(release)
					<-done
				}
				return nil // Ignoring a failed observation must not recover the scope.
			})
			if err == nil || !reflect.DeepEqual(out, linuxStorageCapture{}) {
				t.Fatal("ignored classification failure escaped")
			}
			if strings.Contains(err.Error(), metadataTestFault.Error()) {
				t.Fatal("provider detail disclosed")
			}
			f.allClosed()
		})
	}
}

func TestStorageAncestryOwnerCapAfterAcquisition(t *testing.T) {
	f := newMetadataTestFixture(t)
	raw, err := f.operations().openRoot("sysfs")
	if err != nil {
		t.Fatal("owned root setup failed")
	}
	set := &storageMetadataOwnerSet{seen: map[io.Closer]bool{}}
	// These are inert identity fixtures, not acquired descriptors.
	for i := 0; i < 10000; i++ {
		set.seen[&metadataTestOwner{}] = true
	}
	root := uniqueStorageMetadataRoot{raw, set}
	owner, err := root.OpenPath("class/block/disk/.")
	if err == nil || nilStorageMetadata(owner) || len(f.owners) != 1 || len(set.seen) != 10000 {
		t.Fatal("post-acquisition cap semantics changed")
	}
	if owner.Close() != nil || raw.Close() != nil {
		t.Fatal("rejected acquired owner did not close")
	}
	f.allClosed()
}
