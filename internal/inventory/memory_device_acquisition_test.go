package inventory

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

func memoryAcquisitionSource(t *testing.T, entries []fs.DirEntry, read func(string) ([]byte, error)) collectionSource {
	t.Helper()
	return collectionSource{directory: func(name string) ([]fs.DirEntry, error) {
		if name == memoryTestRegistry {
			return entries, nil
		}
		return nil, fs.ErrNotExist
	}, file: func(name string) ([]byte, error) {
		if strings.HasPrefix(name, "sys/firmware/dmi/") {
			if !strings.HasPrefix(name, memoryTestRegistry+"/17-") || !strings.HasSuffix(name, "/raw") {
				t.Fatal("unapproved DMI metadata read", name)
			}
			return read(name)
		}
		if strings.HasPrefix(name, "dev/") || strings.Contains(name, "edac") || strings.Contains(name, "spd") {
			t.Fatal("unapproved memory fallback")
		}
		return nil, fs.ErrNotExist
	}}
}
func TestMemoryDeviceCollectorRoutesOnlyEligibleRawRecords(t *testing.T) {
	large := "17-" + strings.Repeat("9", 80)
	entries := enumerationEntries("18-0", "17-2", "17-10", "17-01", "17-100", large, "17--1")
	want := []string{memoryTestRegistry + "/17-10/raw", memoryTestRegistry + "/17-100/raw", memoryTestRegistry + "/17-2/raw", memoryTestRegistry + "/" + large + "/raw"}
	var seen []string
	source := memoryAcquisitionSource(t, entries, func(name string) ([]byte, error) {
		seen = append(seen, name)
		return memoryTestRecord(21, uint16(len(seen)), 9, 34, 0, []byte{0, 0}), nil
	})
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
	if err != nil || !reflect.DeepEqual(seen, want) || len(snapshot.Components) != 4 {
		t.Fatal("eligible route/order", err, seen)
	}
	for i, c := range snapshot.Components {
		value, _ := c.Attribute("capacity_bytes")
		if c.ID != fmt.Sprintf("memory_device:%d", i+1) || value != []string{"1048576", "2097152", "3145728", "4194304"}[i] {
			t.Fatal("eligible ordinal or selected value")
		}
	}
}
func TestMemoryDeviceObservationErrorsDiscardAllSelectedFields(t *testing.T) {
	for _, variant := range []string{"complete", "empty", "data_error", "at_limit", "overflow", "overflow_cancel"} {
		t.Run(variant, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reached := 0
			source := memoryAcquisitionSource(t, enumerationEntries("17-0"), func(name string) ([]byte, error) {
				if name != memoryTestRaw {
					t.Fatal("route")
				}
				reached++
				switch variant {
				case "complete":
					return memoryTestRecord(21, 16, 9, 34, 0, []byte{0, 0}), nil
				case "empty":
					return nil, nil
				case "data_error":
					return memoryTestRecord(21, 16, 9, 34, 0, []byte("PRIVATE\x00\x00")), fs.ErrPermission
				case "at_limit":
					return append(memoryTestRecord(21, 16, 9, 34, 0, nil), append(bytes.Repeat([]byte{'x'}, 65513), 0, 0)...), nil
				default:
					if variant == "overflow_cancel" {
						cancel()
					}
					return bytes.Repeat([]byte{'x'}, 65537), fs.ErrPermission
				}
			})
			snapshot, err := NewLinuxCollectorWithSource(source).Collect(ctx)
			if reached != 1 {
				t.Fatal("fault was not reached")
			}
			if variant == "overflow" || variant == "overflow_cancel" {
				requireNoCollection(t, snapshot, err, ErrScalarLimit)
				return
			}
			if err != nil || len(snapshot.Components) != 1 {
				t.Fatal("retained eligible row", err)
			}
			state := "reported"
			if variant == "empty" {
				state = "malformed"
			}
			if variant == "data_error" {
				state = "unavailable"
			}
			want := memoryTestAttributes("16777216", state, "dimm", state, "ddr5", state)
			if !reflect.DeepEqual(snapshot.Components[0], NewComponent(ComponentType("memory_device"), "memory_device:1", want)) {
				t.Fatal("ordinary failure retained bytes", snapshot)
			}
		})
	}
}
func TestMemoryDeviceOwnedScalarCloseAndProtocolFaults(t *testing.T) {
	record := memoryTestRecord(21, 16, 9, 34, 0, []byte{0, 0})
	for _, variant := range []string{"complete", "data_error", "later_error", "close_error", "error_close", "cancel_close", "cancel_close_error", "protocol_cancel", "handle_error", "nonregular", "typed_nil_info"} {
		t.Run(variant, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := scalarBytes(record, 0, true)
			var want, openErr error
			reads, stats := 1, 1
			switch variant {
			case "data_error", "error_close":
				f.read = func(p []byte) (int, error) { return copy(p, record), fs.ErrPermission }
			case "later_error":
				calls := 0
				reads = 2
				f.read = func(p []byte) (int, error) {
					calls++
					if calls == 1 {
						return copy(p, record[:1]), nil
					}
					return copy(p, record[1:]), fs.ErrPermission
				}
			case "cancel_close", "cancel_close_error":
				f.onClose = cancel
				want = context.Canceled
			case "protocol_cancel":
				f.read = func(p []byte) (int, error) { cancel(); return len(p) + 1, fs.ErrPermission }
				want = ErrScalarRead
			case "handle_error":
				openErr = fs.ErrPermission
				want = ErrScalarRead
				reads = 0
				stats = 0
			case "nonregular":
				f.info = &scalarInformation{mode: fs.ModeDir}
				want = ErrScalarRead
				reads = 0
			case "typed_nil_info":
				var info *scalarInformation
				f.info = info
				want = ErrScalarRead
				reads = 0
			}
			if variant == "close_error" || variant == "error_close" || variant == "cancel_close_error" {
				f.closeErr = fs.ErrPermission
				want = ErrScalarClose
			}
			opens := 0
			raw, observed, err := readInventoryScalar(ctx, memoryTestRaw, func(name string) (inventoryScalar, error) {
				opens++
				if name != memoryTestRaw {
					t.Fatal("route")
				}
				return f, openErr
			})
			if opens != 1 || f.closes != 1 || f.stats != stats || len(f.requests) != reads {
				t.Fatal("owner boundary not reached")
			}
			for _, n := range f.requests {
				if n != 4096 {
					t.Fatal("request budget")
				}
			}
			if variant != "complete" {
				requireScalarFailure(t, raw, observed, err, want)
				return
			}
			if err != nil || !observed || !reflect.DeepEqual(parseMemoryDeviceRecord(raw), memoryTestAttributes("16777216", "reported", "dimm", "reported", "ddr5", "reported")) {
				t.Fatal("owned positive prerequisite", err)
			}
		})
	}
}
func TestMemoryDeviceOwnedTerminalProbeBoundsWholeRecord(t *testing.T) {
	for _, size := range []int{65536, 65537} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			record := append(memoryTestRecord(21, 16, 9, 34, 0, nil), append(bytes.Repeat([]byte{'x'}, size-23), 0, 0)...)
			f := scalarBytes(record, 0, false)
			raw, observed, err := readInventoryScalar(t.Context(), memoryTestRaw, func(string) (inventoryScalar, error) { return f, nil })
			if f.closes != 1 || f.stats != 1 || len(f.requests) != 17 || f.requests[16] != 1 {
				t.Fatal("terminal probe/owner not reached")
			}
			if size == 65537 {
				requireScalarFailure(t, raw, observed, err, ErrScalarLimit)
				return
			}
			if err != nil || !observed || len(raw) != 65536 || !reflect.DeepEqual(parseMemoryDeviceRecord(raw), memoryTestAttributes("16777216", "reported", "dimm", "reported", "ddr5", "reported")) {
				t.Fatal("complete boundary record lost", err)
			}
		})
	}
}
func TestMemoryDeviceDirectoryFaultsAbortPartialSnapshot(t *testing.T) {
	var typedNil *enumerationEntry
	for _, variant := range []string{"error", "entries_error", "nil", "typed_nil", "duplicate", "unsafe"} {
		t.Run(variant, func(t *testing.T) {
			reads := 0
			source := memoryAcquisitionSource(t, nil, func(string) ([]byte, error) { reads++; return nil, fs.ErrNotExist })
			source.file = func(name string) ([]byte, error) {
				if name == "proc/meminfo" {
					return []byte("MemTotal: 8192 kB\n"), nil
				}
				if strings.HasPrefix(name, memoryTestRegistry+"/") {
					reads++
				}
				return nil, fs.ErrNotExist
			}
			source.directory = func(name string) ([]fs.DirEntry, error) {
				if name != memoryTestRegistry {
					return nil, fs.ErrNotExist
				}
				switch variant {
				case "error":
					return nil, fs.ErrPermission
				case "entries_error":
					return enumerationEntries("17-0"), fs.ErrNotExist
				case "nil":
					return []fs.DirEntry{nil}, nil
				case "typed_nil":
					return []fs.DirEntry{typedNil}, nil
				case "duplicate":
					return enumerationEntries("17-0", "17-0"), nil
				default:
					return enumerationEntries("../17-0"), nil
				}
			}
			snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
			want := ErrDirectoryEntry
			if variant == "error" || variant == "entries_error" {
				want = ErrDirectoryRead
			}
			requireNoCollection(t, snapshot, err, want)
			if reads != 0 {
				t.Fatal("invalid directory reached raw")
			}
		})
	}
}
func TestMemoryDeviceOwnedDirectoryBatchesRetainAllEligibleOrdinals(t *testing.T) {
	const count = 129
	entries := make([]fs.DirEntry, count)
	calls := make([]int, count)
	for i := range entries {
		n := count - 1 - i
		entries[i] = &enumerationEntry{name: fmt.Sprintf("17-%d", n), onName: func() { calls[n]++ }}
	}
	original := append([]fs.DirEntry(nil), entries...)
	owner := &enumerationDirectory{batches: []enumerationBatch{{entries[:64], nil}, {entries[64:128], nil}, {entries[128:], io.EOF}}}
	owned, err := readInventoryDirectory(t.Context(), memoryTestRegistry, func(string) (inventoryDirectory, error) { return owner, nil })
	if err != nil || owner.closes != 1 || owner.stats != 1 || !reflect.DeepEqual(owner.requests, []int{64, 64, 64}) || !reflect.DeepEqual(entries, original) {
		t.Fatal("owned directory boundary", err)
	}
	var reads []string
	source := memoryAcquisitionSource(t, owned, func(name string) ([]byte, error) {
		reads = append(reads, name)
		return memoryTestRecord(21, 16, 9, 34, 0, []byte{0, 0}), nil
	})
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
	if err != nil || len(snapshot.Components) != count || len(reads) != count || !sort.StringsAreSorted(reads) {
		t.Fatal("eligible rows capped or unsorted", err)
	}
	found := map[string]bool{}
	for _, row := range snapshot.Components {
		found[row.ID] = true
		if row.Type != ComponentType("memory_device") {
			t.Fatal("component type")
		}
	}
	for i, n := range calls {
		if n != 1 || !found[fmt.Sprintf("memory_device:%d", i+1)] {
			t.Fatal("name reevaluated or ordinal lost")
		}
	}
	owner = &enumerationDirectory{batches: []enumerationBatch{{enumerationEntries("17-0"), io.EOF}}, closeErr: fs.ErrPermission}
	rejected, err := readInventoryDirectory(t.Context(), memoryTestRegistry, func(string) (inventoryDirectory, error) { return owner, nil })
	if rejected != nil || err != ErrDirectoryClose || owner.closes != 1 || len(owner.requests) != 1 {
		t.Fatal("directory Close failed to discard entries")
	}
}
func TestMemoryDeviceCancellationDropsEarlierRows(t *testing.T) {
	for _, phase := range []string{"before", "last_name", "last_raw", "ordinary_error"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entries := enumerationEntries("17-0", "17-1")
			reached, reads := 0, 0
			if phase == "last_name" {
				entries[1].(*enumerationEntry).onName = func() { reached++; cancel() }
			}
			source := memoryAcquisitionSource(t, entries, func(name string) ([]byte, error) {
				reads++
				if ctx.Err() != nil {
					t.Fatal("read after cancellation")
				}
				if name == memoryTestRegistry+"/17-1/raw" {
					reached++
					cancel()
					if phase == "ordinary_error" {
						return memoryTestRecord(21, 16, 9, 34, 0, []byte{0, 0}), fs.ErrPermission
					}
				}
				return memoryTestRecord(21, 16, 9, 34, 0, []byte{0, 0}), nil
			})
			if phase == "before" {
				cancel()
			}
			snapshot, err := NewLinuxCollectorWithSource(source).Collect(ctx)
			requireNoCollection(t, snapshot, err, context.Canceled)
			if phase == "before" {
				if reached != 0 || reads != 0 {
					t.Fatal("pre-cancel access")
				}
			} else if reached != 1 {
				t.Fatal("cancel boundary not reached")
			}
			if phase == "last_name" && reads != 0 {
				t.Fatal("raw opened before directory completion")
			}
			if (phase == "last_raw" || phase == "ordinary_error") && reads != 2 {
				t.Fatal("last raw not reached")
			}
		})
	}
}
func TestMemoryDeviceConcurrentCollectorsKeepIndependentCancellation(t *testing.T) {
	arrived, release := make(chan struct{}, 2), make(chan struct{})
	source := memoryAcquisitionSource(t, enumerationEntries("17-0"), func(string) ([]byte, error) {
		arrived <- struct{}{}
		<-release
		return memoryTestRecord(21, 16, 9, 34, 0, []byte{0, 0}), nil
	})
	collector := NewLinuxCollectorWithSource(source)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stopped, live Snapshot
	var stopErr, liveErr error
	var done sync.WaitGroup
	done.Add(2)
	go func() { defer done.Done(); stopped, stopErr = collector.Collect(ctx) }()
	go func() { defer done.Done(); live, liveErr = collector.Collect(t.Context()) }()
	<-arrived
	<-arrived
	cancel()
	close(release)
	done.Wait()
	requireNoCollection(t, stopped, stopErr, context.Canceled)
	if liveErr != nil || len(live.Components) != 1 {
		t.Fatal("unrelated collection canceled", liveErr)
	}
	again, err := collector.Collect(t.Context())
	if err != nil || !reflect.DeepEqual(again, live) {
		t.Fatal("collector retained cancellation", err)
	}
}

type memoryDecoratedSource struct {
	*RootPathSource
	reads int
}

func (s *memoryDecoratedSource) ReadFile(name string) ([]byte, error) {
	if strings.HasPrefix(name, memoryTestRegistry+"/") {
		s.reads++
		return []byte("PRIVATE-OVERRIDE"), nil
	}
	return s.RootPathSource.ReadFile(name)
}
func TestMemoryDeviceDecoratorCannotBypassRawOverride(t *testing.T) {
	root := t.TempDir()
	memoryTestWrite(t, root, memoryTestRaw, memoryTestRecord(21, 16, 9, 34, 0, []byte{0, 0}))
	source := &memoryDecoratedSource{RootPathSource: NewRootPathSource(root)}
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
	want := NewSnapshot([]Component{NewComponent(ComponentType("memory_device"), "memory_device:1", memoryTestAttributes("", "malformed", "", "malformed", "", "malformed"))})
	if err != nil || source.reads != 1 || !reflect.DeepEqual(snapshot, want) {
		t.Fatal("underlying favorable bytes bypassed decorator", err)
	}
}
