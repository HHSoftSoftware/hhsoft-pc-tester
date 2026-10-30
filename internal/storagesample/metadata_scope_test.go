package storagesample

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestStorageMetadataScopeHoldsOwners(t *testing.T) {
	f := newMetadataTestFixture(t)
	var retained *storageMetadataView
	calls := 0
	got, err := withLinuxStorageMetadata(context.Background(), f.operations(), func(view *storageMetadataView) error {
		calls++
		retained = view
		if _, ok := any(view).(sampleTargetOwner); ok {
			t.Fatal("metadata view masquerades as a live target owner")
		}
		if len(f.roots) != 2 || f.roots[0].closes != 0 || f.roots[1].closes != 0 {
			t.Fatal("roots were not retained during consumption")
		}
		open := map[string]int{}
		for _, owner := range f.owners {
			if owner.closes == 0 {
				open[owner.kind+":"+owner.path]++
			}
			if owner.closes > 1 {
				t.Fatal("owner closed twice")
			}
		}
		if !reflect.DeepEqual(open, map[string]int{"path:sysfs/class/block": 1, "path:sysfs/class/block/disk/.": 1}) {
			t.Fatal("registry and node anchors were not retained")
		}
		first, e := view.Snapshot()
		if e != nil || len(first.nodes) != 1 || string(first.nodes[0].number) != "8:0\n" ||
			string(first.nodes[0].sectors) != "16777216\n" || first.nodes[0].partition != nil {
			t.Fatal("owned view facts mismatch")
		}
		first.registry[0] = "changed"
		first.nodes[0].number[0] = '9'
		first.nodes[0].sectors[0] = '0'
		first.nodes[0].readOnly[0] = '1'
		first.nodes[0].removable[0] = '1'
		first.nodes[0].logicalBlock[0] = '9'
		first.mountInfo[0] = '9'
		first.swaps[0] = 'X'
		second, e := view.Snapshot()
		if e != nil || second.registry[0] != "disk" || string(second.nodes[0].number) != "8:0\n" ||
			string(second.nodes[0].sectors) != "16777216\n" || string(second.nodes[0].readOnly) != "0\n" ||
			string(second.nodes[0].removable) != "0\n" || string(second.nodes[0].logicalBlock) != "512\n" ||
			string(second.mountInfo) != "1 0 0:1 / / rw - tmpfs tmpfs rw\n" ||
			string(second.swaps) != "Filename Type Size Used Priority\n" {
			t.Fatal("caller alias mutated scoped facts")
		}
		return view.Verify(context.Background())
	})
	if err != nil || calls != 1 || len(got.nodes) != 1 || string(got.nodes[0].number) != "8:0\n" {
		t.Fatal("complete owning scope failed")
	}
	f.allClosed()
	if f.calls["readdir:sysfs/class/block"] != 4 || !reflect.DeepEqual(f.closed[len(f.closed)-2:], []string{"root:proc", "root:sysfs"}) {
		t.Fatal("final full capture or root close order missing")
	}
	count := 0
	for _, n := range f.calls {
		count += n
	}
	stale, e := retained.Snapshot()
	if e == nil || !reflect.DeepEqual(stale, linuxStorageCapture{}) || retained.Verify(context.Background()) == nil {
		t.Fatal("expired view remained usable")
	}
	after := 0
	for _, n := range f.calls {
		after += n
	}
	if count != after {
		t.Fatal("expired view performed provider operations")
	}
}

func TestStorageMetadataScopeRejectsFinalChanges(t *testing.T) {
	for _, which := range []string{"scalar", "registry_object", "registry_membership", "node_object", "active_swap", "close_failure", "cancel", "cancel_at_close"} {
		t.Run(which, func(t *testing.T) {
			f := newMetadataTestFixture(t)
			other := f.info(true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			got, err := withLinuxStorageMetadata(ctx, f.operations(), func(view *storageMetadataView) error {
				calls++
				switch which {
				case "scalar":
					f.objects["sysfs/class/block/disk/ro"].data = []byte("1\n")
				case "registry_object":
					f.objects["sysfs/class/block"].info = other
				case "registry_membership":
					f.objects["sysfs/class/block"].entries = nil
				case "node_object":
					f.objects["sysfs/class/block/disk/."].info = other
				case "active_swap":
					f.objects["proc/swaps"].data = []byte("Filename Type Size Used Priority\n/owned file 4 0 -1\n")
				case "close_failure":
					f.fail["close-root:proc"] = true
				case "cancel":
					cancel()
				case "cancel_at_close":
					f.hook = func(stage, path string) {
						if stage == "close-root" && path == "sysfs" {
							cancel()
						}
					}
				}
				return nil
			})
			if err == nil || calls != 1 || !reflect.DeepEqual(got, linuxStorageCapture{}) {
				t.Fatal("failed final ownership gate retained facts")
			}
			if which == "close_failure" && !errors.Is(err, errStorageMetadataClose) {
				t.Fatal("scope close error lost")
			}
			if (which == "cancel" || which == "cancel_at_close") && !errors.Is(err, errStorageMetadataCanceled) {
				t.Fatal("scope cancellation lost")
			}
			f.allClosed()
		})
	}
}

func TestStorageMetadataScopeRetainsIgnoredVerificationFailure(t *testing.T) {
	f := newMetadataTestFixture(t)
	before := f.objects["sysfs/class/block/disk/."].info
	other := f.info(true)
	got, err := withLinuxStorageMetadata(context.Background(), f.operations(), func(view *storageMetadataView) error {
		f.objects["sysfs/class/block/disk/."].info = other
		if view.Verify(context.Background()) == nil {
			t.Fatal("changed anchor verification succeeded")
		}
		f.objects["sysfs/class/block/disk/."].info = before
		// Restoring an equal observation cannot erase an already observed fault.
		if snapshot, e := view.Snapshot(); e == nil || !reflect.DeepEqual(snapshot, linuxStorageCapture{}) {
			t.Fatal("poisoned view retained facts")
		}
		return nil
	})
	if err == nil || !reflect.DeepEqual(got, linuxStorageCapture{}) || f.calls["readdir:sysfs/class/block"] != 2 {
		t.Fatal("ignored verification failure qualified or continued")
	}
	f.allClosed()
}

func TestStorageMetadataScopeRejectsReentry(t *testing.T) {
	f := newMetadataTestFixture(t)
	rejected := 0
	got, err := withLinuxStorageMetadata(context.Background(), f.operations(), func(view *storageMetadataView) error {
		ctx := &metadataTestContext{Context: context.Background(), observe: func() {
			if snapshot, e := view.Snapshot(); e != nil && reflect.DeepEqual(snapshot, linuxStorageCapture{}) {
				rejected++
			} else {
				t.Fatal("reentrant view access succeeded")
			}
		}}
		if view.Verify(ctx) == nil {
			t.Fatal("reentrant verification silently qualified")
		}
		return nil
	})
	if err == nil || rejected == 0 || !reflect.DeepEqual(got, linuxStorageCapture{}) {
		t.Fatal("reentry did not poison the owning scope")
	}
	f.allClosed()
}

func TestStorageMetadataScopeConsumerFailureCloses(t *testing.T) {
	for _, which := range []string{"consumer_error", "consumer_and_close", "acquired_registry_error"} {
		t.Run(which, func(t *testing.T) {
			f := newMetadataTestFixture(t)
			if which == "consumer_and_close" {
				f.fail["close-root:sysfs"] = true
			}
			if which == "acquired_registry_error" {
				f.fail["open-path:sysfs/class/block"] = true
			}
			calls := 0
			got, err := withLinuxStorageMetadata(context.Background(), f.operations(), func(*storageMetadataView) error {
				calls++
				return metadataTestFault
			})
			if err == nil || !reflect.DeepEqual(got, linuxStorageCapture{}) || strings.Contains(err.Error(), "fixture detail") {
				t.Fatal("consumer error retained facts or leaked diagnostics")
			}
			if which == "acquired_registry_error" {
				if calls != 0 {
					t.Fatal("failed registry acquisition reached consumer")
				}
			} else if calls != 1 || f.calls["readdir:sysfs/class/block"] != 2 {
				t.Fatal("consumer failure continued to final capture")
			}
			if which == "consumer_and_close" && !errors.Is(err, errStorageMetadataClose) {
				t.Fatal("consumer error hid close failure")
			}
			f.allClosed()
		})
	}
}

func TestStorageMetadataScopeInvalidInput(t *testing.T) {
	for _, which := range []string{"nil_context", "typed_nil_context", "nil_operations", "nil_consumer", "canceled"} {
		t.Run(which, func(t *testing.T) {
			f := newMetadataTestFixture(t)
			ctx := context.Context(context.Background())
			operations := f.operations()
			consume := func(*storageMetadataView) error { t.Fatal("invalid input reached consumer"); return nil }
			switch which {
			case "nil_context":
				ctx = nil
			case "typed_nil_context":
				var typed *metadataTestContext
				ctx = typed
			case "nil_operations":
				operations = storageMetadataOperations{}
			case "nil_consumer":
				consume = nil
			case "canceled":
				c, cancel := context.WithCancel(context.Background())
				cancel()
				ctx = c
			}
			got, err := withLinuxStorageMetadata(ctx, operations, consume)
			if err == nil || !reflect.DeepEqual(got, linuxStorageCapture{}) || len(f.roots) != 0 {
				t.Fatal("invalid scope input acquired owners")
			}
		})
	}
}

func TestStorageMetadataScopeCopiesRelationsAndPartitions(t *testing.T) {
	f := newMetadataTestFixture(t)
	f.node("mapped", "253:0")
	f.node("part", "8:1")
	f.objects["sysfs/class/block"].entries = []os.DirEntry{&metadataTestEntry{"part", os.ModeSymlink}, &metadataTestEntry{"mapped", os.ModeSymlink}, &metadataTestEntry{"disk", os.ModeSymlink}}
	f.file("sysfs/class/block/part/partition", "1\n")
	delete(f.objects, "sysfs/class/block/part/removable")
	delete(f.objects, "sysfs/class/block/part/queue/logical_block_size")
	delete(f.objects, "sysfs/class/block/part/slaves")
	f.objects["sysfs/class/block/disk/holders"].entries = []os.DirEntry{&metadataTestEntry{"mapped", os.ModeSymlink}}
	f.objects["sysfs/class/block/mapped/slaves"].entries = []os.DirEntry{&metadataTestEntry{"disk", os.ModeSymlink}}
	f.objects["sysfs/class/block/disk/holders/mapped/."] = f.objects["sysfs/class/block/mapped/."]
	f.objects["sysfs/class/block/mapped/slaves/disk/."] = f.objects["sysfs/class/block/disk/."]
	got, err := withLinuxStorageMetadata(context.Background(), f.operations(), func(view *storageMetadataView) error {
		first, e := view.Snapshot()
		if e != nil || len(first.nodes) != 3 {
			t.Fatal("linked partition view unavailable")
		}
		first.nodes[0].holders[0] = "changed"
		first.nodes[1].slaves[0] = "changed"
		first.nodes[2].partition[0] = '9'
		first.nodes[2].partitionSlavesAbsent = false
		second, e := view.Snapshot()
		if e != nil || !reflect.DeepEqual(second.nodes[0].holders, []string{"mapped"}) ||
			!reflect.DeepEqual(second.nodes[1].slaves, []string{"disk"}) ||
			string(second.nodes[2].partition) != "1\n" || !second.nodes[2].partitionSlavesAbsent {
			t.Fatal("relation or partition alias changed owned view")
		}
		return nil
	})
	if err != nil || len(got.nodes) != 3 {
		t.Fatal("linked partition scope failed")
	}
	f.allClosed()
}
