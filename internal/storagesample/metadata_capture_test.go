package storagesample

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func requireMetadataCaptureFailure(t *testing.T, f *metadataTestFixture, ctx context.Context) error {
	t.Helper()
	got, err := captureLinuxStorageMetadata(ctx, f.operations())
	if err == nil || !reflect.DeepEqual(got, linuxStorageCapture{}) || strings.Contains(err.Error(), "fixture detail") {
		t.Fatal("capture failure retained facts or exposed provider diagnostics")
	}
	f.allClosed()
	return err
}

func TestStorageMetadataCaptureOwnsFacts(t *testing.T) {
	f := newMetadataTestFixture(t)
	got, err := captureLinuxStorageMetadata(context.Background(), f.operations())
	want := linuxStorageCapture{registry: []string{"disk"}, nodes: []linuxStorageCapturedNode{{
		name: "disk", number: []byte("8:0\n"), sectors: []byte("16777216\n"), readOnly: []byte("0\n"),
		removable: []byte("0\n"), logicalBlock: []byte("512\n"), holders: []string{}, slaves: []string{},
	}}, mountInfo: []byte("1 0 0:1 / / rw - tmpfs tmpfs rw\n"), swaps: []byte("Filename Type Size Used Priority\n")}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("literal owned capture mismatch")
	}
	f.allClosed()
	if f.calls["readdir:sysfs/class/block"] != 4 || f.calls["read:proc/swaps"] != 4 ||
		!reflect.DeepEqual(f.closed[len(f.closed)-2:], []string{"root:proc", "root:sysfs"}) {
		t.Fatal("two EOF passes or reverse root closure missing")
	}
	f.objects["sysfs/class/block"].entries[0].(*metadataTestEntry).name = "changed"
	for _, object := range f.objects {
		for i := range object.data {
			object.data[i] = 'X'
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("provider alias changed completed capture")
	}
}

func TestStorageMetadataCaptureDisklessAndPartition(t *testing.T) {
	t.Run("diskless", func(t *testing.T) {
		f := newMetadataTestFixture(t)
		f.objects["sysfs/class/block"].entries = []os.DirEntry{}
		got, err := captureLinuxStorageMetadata(context.Background(), f.operations())
		if err != nil || got.registry == nil || got.nodes == nil || len(got.nodes) != 0 {
			t.Fatal("complete diskless observation lost")
		}
		f.allClosed()
	})
	t.Run("partition", func(t *testing.T) {
		f := newMetadataTestFixture(t)
		f.file("sysfs/class/block/disk/partition", "1\n")
		delete(f.objects, "sysfs/class/block/disk/removable")
		delete(f.objects, "sysfs/class/block/disk/queue/logical_block_size")
		delete(f.objects, "sysfs/class/block/disk/slaves")
		got, err := captureLinuxStorageMetadata(context.Background(), f.operations())
		if err != nil || len(got.nodes) != 1 || !got.nodes[0].partitionSlavesAbsent ||
			string(got.nodes[0].partition) != "1\n" || got.nodes[0].logicalBlock != nil ||
			got.nodes[0].removable != nil || got.nodes[0].slaves == nil || len(got.nodes[0].slaves) != 0 {
			t.Fatal("partition absence was lost or inherited geometry invented")
		}
		f.allClosed()
	})
}

func TestStorageMetadataAcquiredOwnerFailures(t *testing.T) {
	for _, tc := range []struct{ name, stage, path string }{
		{"first_root_with_error", "open-root", "sysfs"},
		{"second_root_with_error", "open-root", "proc"},
		{"path_with_error", "open-path", "sysfs/class/block/disk/dev"},
		{"readable_with_error", "open-readable", "sysfs/class/block/disk/dev"},
		{"path_stat", "path-stat", "sysfs/class/block/disk/dev"},
		{"readable_stat", "file-stat", "sysfs/class/block/disk/dev"},
		{"named_stat", "lstat", "sysfs/class/block/disk/dev"},
		{"read", "read", "sysfs/class/block/disk/dev"},
		{"directory", "readdir", "sysfs/class/block"},
		{"path_close", "close-path", "sysfs/class/block/disk/dev"},
		{"readable_close", "close-file", "sysfs/class/block/disk/dev"},
		{"node_close", "close-path", "sysfs/class/block/disk/."},
		{"root_close", "close-root", "sysfs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMetadataTestFixture(t)
			f.fail[tc.stage+":"+tc.path] = true
			err := requireMetadataCaptureFailure(t, f, context.Background())
			if strings.HasPrefix(tc.stage, "close-") && !errors.Is(err, errStorageMetadataClose) {
				t.Fatal("close error was masked")
			}
		})
	}
	t.Run("read_and_close", func(t *testing.T) {
		f := newMetadataTestFixture(t)
		f.fail["read:sysfs/class/block/disk/dev"] = true
		f.fail["close-file:sysfs/class/block/disk/dev"] = true
		f.fail["close-root:proc"] = true
		if !errors.Is(requireMetadataCaptureFailure(t, f, context.Background()), errStorageMetadataClose) {
			t.Fatal("combined close error was masked")
		}
	})
	t.Run("cancel_and_close", func(t *testing.T) {
		f := newMetadataTestFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.fail["close-file:sysfs/class/block/disk/dev"] = true
		f.hook = func(stage, path string) {
			if stage == "read" && path == "sysfs/class/block/disk/dev" {
				cancel()
			}
		}
		err := requireMetadataCaptureFailure(t, f, ctx)
		if !errors.Is(err, errStorageMetadataClose) || !errors.Is(err, errStorageMetadataCanceled) {
			t.Fatal("cancellation erased acquired-owner failure")
		}
	})
}

func TestStorageMetadataCancellationAndNilOwners(t *testing.T) {
	for _, tc := range []struct{ name, stage, path string }{
		{"root", "open-root", "sysfs"}, {"path", "open-path", "sysfs/class/block/disk/dev"},
		{"readable", "open-readable", "sysfs/class/block/disk/dev"}, {"read", "read", "sysfs/class/block/disk/dev"},
		{"directory", "readdir", "sysfs/class/block"}, {"final_close", "close-root", "sysfs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMetadataTestFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.hook = func(stage, path string) {
				if stage == tc.stage && path == tc.path {
					cancel()
				}
			}
			if !errors.Is(requireMetadataCaptureFailure(t, f, ctx), errStorageMetadataCanceled) {
				t.Fatal("cancellation was not observed")
			}
		})
	}
	t.Run("already_canceled", func(t *testing.T) {
		f := newMetadataTestFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		requireMetadataCaptureFailure(t, f, ctx)
		if len(f.roots) != 0 {
			t.Fatal("canceled input acquired root")
		}
	})
	t.Run("typed_nil_context", func(t *testing.T) {
		var ctx *metadataTestContext
		f := newMetadataTestFixture(t)
		requireMetadataCaptureFailure(t, f, ctx)
		if len(f.roots) != 0 {
			t.Fatal("typed nil context reached acquisition")
		}
	})
	t.Run("typed_nil_root", func(t *testing.T) {
		var root *metadataTestRoot
		got, err := captureLinuxStorageMetadata(context.Background(), storageMetadataOperations{openRoot: func(string) (storageMetadataRoot, error) { return root, nil }})
		if err == nil || !reflect.DeepEqual(got, linuxStorageCapture{}) {
			t.Fatal("typed nil root admitted")
		}
	})
}

func TestStorageMetadataIdentityAndSecondPassDrift(t *testing.T) {
	for _, which := range []string{"named_replacement", "descriptor_replacement", "second_scalar", "second_registry", "second_mount", "second_swap"} {
		t.Run(which, func(t *testing.T) {
			f := newMetadataTestFixture(t)
			other := f.info(false)
			f.hook = func(stage, path string) {
				if which == "named_replacement" && stage == "read" && path == "sysfs/class/block/disk/dev" {
					f.objects[path].info = other
				}
				if which == "descriptor_replacement" && stage == "read" && path == "sysfs/class/block/disk/dev" {
					f.statOverride["file:"+path] = other
				}
				if stage == "readdir" && path == "sysfs/class/block" && f.calls[stage+":"+path] == 3 {
					switch which {
					case "second_scalar":
						f.objects["sysfs/class/block/disk/ro"].data = []byte("1\n")
					case "second_registry":
						f.objects[path].entries = []os.DirEntry{}
					case "second_mount":
						f.objects["proc/self/mountinfo"].data = []byte("1 0 0:2 / / rw - tmpfs tmpfs rw\n")
					case "second_swap":
						f.objects["proc/swaps"].data = []byte("Filename Type Size Used Priority\n/owned file 4 0 -1\n")
					}
				}
			}
			requireMetadataCaptureFailure(t, f, context.Background())
		})
	}
}

func TestStorageMetadataRejectsUnknownAndUnsafeFacts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*metadataTestFixture)
	}{
		{"loop_name", func(f *metadataTestFixture) {
			f.objects["sysfs/class/block"].entries = []os.DirEntry{&metadataTestEntry{"loop9", os.ModeSymlink}}
		}},
		{"loop_major", func(f *metadataTestFixture) { f.objects["sysfs/class/block/disk/dev"].data = []byte("7:9\n") }},
		{"bad_number", func(f *metadataTestFixture) { f.objects["sysfs/class/block/disk/dev"].data = []byte("08:0\n") }},
		{"sector_overflow", func(f *metadataTestFixture) {
			f.objects["sysfs/class/block/disk/size"].data = []byte("18446744073709551615\n")
		}},
		{"unknown_readonly", func(f *metadataTestFixture) { f.objects["sysfs/class/block/disk/ro"].data = []byte("2\n") }},
		{"partition_zero", func(f *metadataTestFixture) { f.file("sysfs/class/block/disk/partition", "0\n") }},
		{"missing_geometry", func(f *metadataTestFixture) { delete(f.objects, "sysfs/class/block/disk/queue/logical_block_size") }},
		{"missing_removable", func(f *metadataTestFixture) { delete(f.objects, "sysfs/class/block/disk/removable") }},
		{"missing_holders", func(f *metadataTestFixture) { delete(f.objects, "sysfs/class/block/disk/holders") }},
		{"missing_whole_slaves", func(f *metadataTestFixture) { delete(f.objects, "sysfs/class/block/disk/slaves") }},
		{"unknown_mounted_device", func(f *metadataTestFixture) {
			f.objects["proc/self/mountinfo"].data = []byte("1 0 8:16 / / ro - ext4 /dev/owned ro\n")
		}},
		{"unknown_mount", func(f *metadataTestFixture) { f.objects["proc/self/mountinfo"].data = []byte("incomplete\n") }},
		{"unknown_swap", func(f *metadataTestFixture) {
			f.objects["proc/swaps"].data = []byte("Filename Type Size Used Priority")
		}},
		{"active_swap", func(f *metadataTestFixture) {
			f.objects["proc/swaps"].data = []byte("Filename Type Size Used Priority\n/owned file 4 0 -1\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMetadataTestFixture(t)
			tc.change(f)
			requireMetadataCaptureFailure(t, f, context.Background())
			if tc.name == "loop_major" && f.calls["read:sysfs/class/block/disk/size"] != 0 {
				t.Fatal("loop rejection occurred after other attributes")
			}
		})
	}
}

func TestStorageMetadataRegistryAndRelationIdentity(t *testing.T) {
	for _, which := range []string{"linked", "unknown_target", "wrong_target", "same_number", "same_object", "self", "relation_limit"} {
		t.Run(which, func(t *testing.T) {
			f := newMetadataTestFixture(t)
			f.node("mapped", "253:0")
			f.objects["sysfs/class/block"].entries = []os.DirEntry{&metadataTestEntry{"mapped", os.ModeSymlink}, &metadataTestEntry{"disk", os.ModeSymlink}}
			f.objects["sysfs/class/block/disk/holders"].entries = []os.DirEntry{&metadataTestEntry{"mapped", os.ModeSymlink}}
			f.objects["sysfs/class/block/mapped/slaves"].entries = []os.DirEntry{&metadataTestEntry{"disk", os.ModeSymlink}}
			f.objects["sysfs/class/block/disk/holders/mapped/."] = f.objects["sysfs/class/block/mapped/."]
			f.objects["sysfs/class/block/mapped/slaves/disk/."] = f.objects["sysfs/class/block/disk/."]
			switch which {
			case "unknown_target":
				f.objects["sysfs/class/block/disk/holders"].entries = []os.DirEntry{&metadataTestEntry{"absent", os.ModeSymlink}}
			case "wrong_target":
				f.objects["sysfs/class/block/disk/holders/mapped/."] = &metadataTestObject{info: f.info(true)}
			case "same_number":
				f.objects["sysfs/class/block/mapped/dev"].data = []byte("8:0\n")
			case "same_object":
				f.objects["sysfs/class/block/mapped/."] = f.objects["sysfs/class/block/disk/."]
			case "self":
				f.objects["sysfs/class/block/disk/holders"].entries = []os.DirEntry{&metadataTestEntry{"disk", os.ModeSymlink}}
			case "relation_limit":
				names := make([]os.DirEntry, 1025)
				for i := range names {
					names[i] = &metadataTestEntry{"n" + strconv.Itoa(i), os.ModeSymlink}
				}
				f.objects["sysfs/class/block/disk/holders"].entries = names
			}
			if which == "linked" {
				got, err := captureLinuxStorageMetadata(context.Background(), f.operations())
				if err != nil || !reflect.DeepEqual(got.nodes[0].holders, []string{"mapped"}) || !reflect.DeepEqual(got.nodes[1].slaves, []string{"disk"}) {
					t.Fatal("literal linked identities lost")
				}
				f.allClosed()
			} else {
				requireMetadataCaptureFailure(t, f, context.Background())
			}
		})
	}
}

func TestStorageMetadataReadAndDirectoryLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  []byte
		limit int
		good  bool
	}{
		{"exact", []byte("abcd"), 4, true}, {"overflow", []byte("abcde"), 4, false}, {"empty", []byte{}, 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := storageMetadataBytes(context.Background(), bytes.NewReader(tc.data), tc.limit)
			if tc.good {
				if err != nil || !bytes.Equal(got, tc.data) {
					t.Fatal("bounded EOF mismatch")
				}
			} else if err == nil || got != nil {
				t.Fatal("overflow retained bytes")
			}
		})
	}
	for _, which := range []string{"zero_progress", "negative_count", "excess_count", "read_error"} {
		t.Run(which, func(t *testing.T) {
			reader := metadataReadFunc(func(p []byte) (int, error) {
				switch which {
				case "zero_progress":
					return 0, nil
				case "negative_count":
					return -1, io.EOF
				case "excess_count":
					return len(p) + 1, io.EOF
				default:
					return 1, metadataTestFault
				}
			})
			if got, err := storageMetadataBytes(context.Background(), reader, 4); err == nil || got != nil {
				t.Fatal("invalid reader admitted")
			}
		})
	}
	for _, tc := range []struct {
		name         string
		count, limit int
		good         bool
	}{
		{"nodes_exact", 256, 256, true}, {"nodes_excess", 257, 256, false},
		{"edges_exact", 1024, 1024, true}, {"edges_excess", 1025, 1024, false},
		{"zero_remaining_empty", 0, 0, true}, {"zero_remaining_excess", 1, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			offset := 0
			calls := 0
			file := &metadataNamesFile{read: func(n int) ([]os.DirEntry, error) {
				calls++
				if n < 1 || n > 32 {
					t.Fatal("directory request was not bounded")
				}
				if offset == tc.count {
					return nil, io.EOF
				}
				end := min(tc.count, offset+n)
				rows := []os.DirEntry{}
				for i := offset; i < end; i++ {
					rows = append(rows, &metadataTestEntry{"n" + strconv.Itoa(i), os.ModeSymlink})
				}
				offset = end
				return rows, nil
			}}
			got, err := storageMetadataNames(context.Background(), file, tc.limit)
			if tc.good {
				if err != nil || len(got) != tc.count || calls != tc.count/32+1 {
					t.Fatal("complete enumeration mismatch")
				}
			} else if err == nil || got != nil {
				t.Fatal("excess enumeration admitted")
			}
		})
	}
	for _, which := range []string{"short_without_eof", "directory_zero_progress", "duplicate", "unsafe_name", "nil_entry", "non_link", "excess_batch", "directory_error"} {
		t.Run(which, func(t *testing.T) {
			calls := 0
			file := &metadataNamesFile{read: func(n int) ([]os.DirEntry, error) {
				calls++
				if calls > 1 {
					return nil, metadataTestFault
				}
				row := os.DirEntry(&metadataTestEntry{"disk", os.ModeSymlink})
				switch which {
				case "short_without_eof":
					return []os.DirEntry{row}, nil
				case "directory_zero_progress":
					return nil, nil
				case "duplicate":
					return []os.DirEntry{row, row}, io.EOF
				case "unsafe_name":
					return []os.DirEntry{&metadataTestEntry{"../disk", os.ModeSymlink}}, io.EOF
				case "nil_entry":
					var e *metadataTestEntry
					return []os.DirEntry{e}, io.EOF
				case "non_link":
					return []os.DirEntry{&metadataTestEntry{"disk", 0}}, io.EOF
				case "excess_batch":
					return make([]os.DirEntry, n+1), io.EOF
				default:
					return []os.DirEntry{row}, metadataTestFault
				}
			}}
			if got, err := storageMetadataNames(context.Background(), file, 256); err == nil || got != nil {
				t.Fatal("incomplete directory admitted")
			}
			if which == "short_without_eof" && calls != 2 {
				t.Fatal("short batch was mistaken for EOF")
			}
		})
	}
}

func TestStorageMetadataCopiesBeforeContextCallbacks(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		var borrowed []byte
		ctx := &metadataTestContext{Context: context.Background(), observe: func() {
			for i := range borrowed {
				borrowed[i] = 'X'
			}
		}}
		reader := metadataReadFunc(func(p []byte) (int, error) { copy(p, "abc"); borrowed = p[:3]; return 3, io.EOF })
		got, err := storageMetadataBytes(ctx, reader, 3)
		if err != nil || string(got) != "abc" {
			t.Fatal("context mutated retained read buffer")
		}
	})
	t.Run("directory", func(t *testing.T) {
		entry := &metadataTestEntry{"disk", os.ModeSymlink}
		armed := false
		ctx := &metadataTestContext{Context: context.Background(), observe: func() {
			if armed {
				entry.name = "changed"
			}
		}}
		file := &metadataNamesFile{read: func(int) ([]os.DirEntry, error) { armed = true; return []os.DirEntry{entry}, io.EOF }}
		got, err := storageMetadataNames(ctx, file, 256)
		if err != nil || !reflect.DeepEqual(got, []string{"disk"}) {
			t.Fatal("context mutated retained directory batch")
		}
	})
}
