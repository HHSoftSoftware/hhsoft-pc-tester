package inventory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type collectionSource struct {
	file      func(string) ([]byte, error)
	directory func(string) ([]fs.DirEntry, error)
}

func (s collectionSource) ReadFile(name string) ([]byte, error) {
	if s.file != nil {
		return s.file(name)
	}
	return nil, fs.ErrNotExist
}
func (s collectionSource) ReadDir(name string) ([]fs.DirEntry, error) {
	if s.directory != nil {
		return s.directory(name)
	}
	return nil, fs.ErrNotExist
}

type decoratedCollectionSource struct {
	*RootPathSource
	calls int
}

func (s *decoratedCollectionSource) ReadDir(name string) ([]fs.DirEntry, error) {
	s.calls++
	if name == "sys/class/block" {
		return nil, fs.ErrPermission
	}
	return s.RootPathSource.ReadDir(name)
}
func requireNoCollection(t *testing.T, snapshot Snapshot, err, want error) {
	t.Helper()
	if !errors.Is(err, want) || snapshot.SchemaVersion != 0 || snapshot.Components != nil {
		t.Fatalf("failure escaped as a snapshot: schema=%d count=%d error=%v", snapshot.SchemaVersion, len(snapshot.Components), err)
	}
}

func TestInventoryCollectionRejectsDirectoryFailureWithoutLaterCalls(t *testing.T) {
	for _, target := range []string{"sys/devices/system/cpu", "sys/class/block", "sys/bus/usb/devices"} {
		for _, partial := range []bool{false, true} {
			t.Run(target+fmt.Sprint(partial), func(t *testing.T) {
				failed := false
				source := collectionSource{
					file: func(name string) ([]byte, error) {
						if failed {
							t.Fatal("file acquisition after directory failure")
						}
						if name == "proc/cpuinfo" {
							return []byte("processor: 0\nmodel name: fixture\n"), nil
						}
						return nil, fs.ErrNotExist
					},
					directory: func(name string) ([]fs.DirEntry, error) {
						if failed {
							t.Fatal("directory acquisition after failure")
						}
						if name == target {
							failed = true
							if partial {
								return enumerationEntries("partial"), fs.ErrPermission
							}
							return nil, fs.ErrPermission
						}
						return nil, fs.ErrNotExist
					},
				}
				snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
				requireNoCollection(t, snapshot, err, ErrDirectoryRead)
				if !failed {
					t.Fatal("failure fixture did not reach its registry")
				}
			})
		}
	}
}

func TestInventoryCollectionLegacyAbsenceAndPartialPolicy(t *testing.T) {
	for _, item := range []struct {
		name    string
		entries []fs.DirEntry
		err     error
		want    error
	}{
		{"clean", nil, fs.ErrNotExist, nil}, {"empty", nil, nil, nil},
		{"wrapped clean", nil, fmt.Errorf("fixture: %w", fs.ErrNotExist), nil},
		{"partial absence", enumerationEntries("visible"), fs.ErrNotExist, ErrDirectoryRead},
		{"mixed absence", nil, errors.Join(fs.ErrNotExist, fs.ErrPermission), ErrDirectoryRead},
		{"reverse mixed", nil, errors.Join(fs.ErrPermission, fs.ErrNotExist), ErrDirectoryRead},
		{"one joined", nil, errors.Join(fs.ErrNotExist), ErrDirectoryRead},
		{"read missing", nil, &fs.PathError{Op: "readdir", Path: "neutral", Err: fs.ErrNotExist}, ErrDirectoryRead},
		{"close missing", nil, &fs.PathError{Op: "close", Path: "neutral", Err: fs.ErrNotExist}, ErrDirectoryRead},
	} {
		t.Run(item.name, func(t *testing.T) {
			source := collectionSource{directory: func(name string) ([]fs.DirEntry, error) {
				if name == "sys/class/block" {
					return item.entries, item.err
				}
				return nil, fs.ErrNotExist
			}}
			snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
			if item.want != nil {
				requireNoCollection(t, snapshot, err, item.want)
			} else if err != nil || snapshot.SchemaVersion != 1 || len(snapshot.Components) != 0 {
				t.Fatal("clean absent/diskless snapshot rejected")
			}
		})
	}
}

func TestInventoryCollectionLastSourceCancellation(t *testing.T) {
	for _, mode := range []string{"empty directory", "missing directory", "positive attribute", "missing attribute", "optional error", "hard directory"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			triggered := false
			source := collectionSource{
				directory: func(name string) ([]fs.DirEntry, error) {
					if triggered {
						t.Fatal("directory operation followed cancellation")
					}
					if name != "sys/bus/usb/devices" {
						return nil, fs.ErrNotExist
					}
					switch mode {
					case "empty directory":
						triggered = true
						cancel()
						return nil, nil
					case "missing directory":
						triggered = true
						cancel()
						return nil, fs.ErrNotExist
					case "hard directory":
						triggered = true
						cancel()
						return nil, fs.ErrPermission
					default:
						return enumerationEntries("1-1"), nil
					}
				},
				file: func(name string) ([]byte, error) {
					if triggered {
						t.Fatal("file operation followed cancellation")
					}
					if name == "sys/bus/usb/devices/1-1/idVendor" {
						return []byte("1234\n"), nil
					}
					if name == "sys/bus/usb/devices/1-1/authorized" {
						triggered = true
						cancel()
						if mode == "positive attribute" {
							return []byte("1\n"), nil
						}
						if mode == "optional error" {
							return nil, fs.ErrPermission
						}
						return nil, fs.ErrNotExist
					}
					return nil, fs.ErrNotExist
				},
			}
			snapshot, err := NewLinuxCollectorWithSource(source).Collect(ctx)
			want := error(context.Canceled)
			if mode == "hard directory" {
				want = ErrDirectoryRead
			}
			requireNoCollection(t, snapshot, err, want)
			if !triggered {
				t.Fatal("final acquisition was not exercised")
			}
		})
	}
}

func TestInventoryCollectionInitialCancellationAndMissingDependencies(t *testing.T) {
	source := collectionSource{file: func(string) ([]byte, error) { t.Fatal("read after pre-cancellation"); return nil, nil }, directory: func(string) ([]fs.DirEntry, error) { t.Fatal("enumeration after pre-cancellation"); return nil, nil }}
	collector := NewLinuxCollectorWithSource(source)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	snapshot, err := collector.Collect(ctx)
	requireNoCollection(t, snapshot, err, context.Canceled)
	snapshot, err = collector.Collect(nil)
	requireNoCollection(t, snapshot, err, ErrInvalidContext)
	snapshot, err = NewLinuxCollectorWithSource(nil).Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrNoPathSource)
	var typedNil *RootPathSource
	snapshot, err = NewLinuxCollectorWithSource(typedNil).Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrNoPathSource)
	var absent *LinuxCollector
	snapshot, err = absent.Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrNoPathSource)
}

func TestInventoryCollectionBitmapAndNestedUSBCancellation(t *testing.T) {
	t.Run("bitmap observation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		source := collectionSource{file: func(name string) ([]byte, error) {
			if name == "proc/bus/input/devices" {
				return []byte("N: Name=\"Fixture Keyboard\"\nH: Handlers=kbd event0\n"), nil
			}
			return nil, fs.ErrNotExist
		}, directory: func(name string) ([]fs.DirEntry, error) {
			if ctx.Err() != nil {
				t.Fatal("acquisition after bitmap cancellation")
			}
			return nil, fs.ErrNotExist
		}}
		collector := NewLinuxCollectorWithSource(source)
		observed := 0
		collector.inputBitmapWordBits = func() int { observed++; cancel(); return 64 }
		snapshot, err := collector.Collect(ctx)
		requireNoCollection(t, snapshot, err, context.Canceled)
		if observed != 1 {
			t.Fatal("bitmap boundary was not reached")
		}
	})
	t.Run("USB interface scan", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		interfaces := 0
		source := collectionSource{directory: func(name string) ([]fs.DirEntry, error) {
			if name == "sys/bus/usb/devices" {
				return enumerationEntries("1-1", "1-1:1.0", "1-1:1.1"), nil
			}
			return nil, fs.ErrNotExist
		}, file: func(name string) ([]byte, error) {
			if ctx.Err() != nil {
				t.Fatal("acquisition after interface cancellation")
			}
			if name == "sys/bus/usb/devices/1-1/idVendor" {
				return []byte("1234"), nil
			}
			if strings.HasSuffix(name, "/bInterfaceClass") {
				interfaces++
				cancel()
				return []byte("03"), nil
			}
			return nil, fs.ErrNotExist
		}}
		snapshot, err := NewLinuxCollectorWithSource(source).Collect(ctx)
		requireNoCollection(t, snapshot, err, context.Canceled)
		if interfaces != 1 {
			t.Fatal("nested interface boundary not exercised exactly once")
		}
	})
}

func TestInventoryCollectionOwnsReusableSliceAndRetainsDistinctDevices(t *testing.T) {
	provided := enumerationEntries("eth1", "eth0")
	source := collectionSource{directory: func(name string) ([]fs.DirEntry, error) {
		if name == "sys/class/net" {
			return provided, nil
		}
		return nil, fs.ErrNotExist
	}, file: func(name string) ([]byte, error) {
		switch name {
		case "sys/class/net/eth0/address":
			return []byte("02:00:00:00:00:01"), nil
		case "sys/class/net/eth1/address":
			return []byte("02:00:00:00:00:02"), nil
		case "sys/class/net/eth0/mtu":
			return []byte("1500"), nil
		case "sys/class/net/eth1/mtu":
			return []byte("9000"), nil
		default:
			return nil, fs.ErrPermission // Optional errors retain unknown behavior.
		}
	}}
	collector := NewLinuxCollectorWithSource(source)
	first, err := collector.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := collector.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Components) != 2 || !snapshotsEqual(first, second) || provided[0].Name() != "eth1" || provided[1].Name() != "eth0" {
		t.Fatal("provider mutation or lost device")
	}
	assertAttribute(t, first, ComponentNetwork, "network:eth0", "address", "02:00:00:00:00:01")
	assertAttribute(t, first, ComponentNetwork, "network:eth1", "address", "02:00:00:00:00:02")
	assertAttribute(t, first, ComponentNetwork, "network:eth0", "mtu", "1500")
	assertAttribute(t, first, ComponentNetwork, "network:eth1", "mtu", "9000")
}

func TestInventoryCollectionRetainsEveryInstanceBeyond256(t *testing.T) {
	var provided []fs.DirEntry
	for i := 324; i >= 0; i-- {
		provided = append(provided, &enumerationEntry{name: fmt.Sprintf("disk%03d", i)})
	}
	source := collectionSource{directory: func(name string) ([]fs.DirEntry, error) {
		if name == "sys/class/block" {
			return provided, nil
		}
		return nil, fs.ErrNotExist
	}}
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
	if err != nil || len(snapshot.Components) != 325 || snapshot.SchemaVersion != 1 {
		t.Fatalf("inventory limit or failure: %d %v", len(snapshot.Components), err)
	}
	for i, component := range snapshot.Components {
		name := fmt.Sprintf("disk%03d", i)
		if component.Type != ComponentBlock || component.ID != "block:"+name {
			t.Fatal("missing literal block identity")
		}
		if value, ok := component.Attribute("name"); !ok || value != name {
			t.Fatal("lost device name")
		}
		if value, ok := component.Attribute("kind"); !ok || value != "device" {
			t.Fatal("changed device classification")
		}
	}
	if provided[0].Name() != "disk324" {
		t.Fatal("large provider slice mutated")
	}
}

func TestInventoryCollectionRepeatedFailureDoesNotPersist(t *testing.T) {
	failed := true
	source := collectionSource{directory: func(name string) ([]fs.DirEntry, error) {
		if name == "sys/class/block" && failed {
			return nil, fs.ErrPermission
		}
		return nil, fs.ErrNotExist
	}}
	collector := NewLinuxCollectorWithSource(source)
	snapshot, err := collector.Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrDirectoryRead)
	failed = false
	snapshot, err = collector.Collect(t.Context())
	if err != nil || snapshot.SchemaVersion != 1 {
		t.Fatal("previous error contaminated new collection")
	}
}

func TestInventoryCollectionConcurrentContextsRemainIndependent(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var calls atomic.Int32
	provided := enumerationEntries("eth1", "eth0")
	source := collectionSource{directory: func(name string) ([]fs.DirEntry, error) {
		if name == "sys/class/net" {
			if calls.Add(1) == 1 {
				close(entered)
				<-release
			}
			return provided, nil
		}
		return nil, fs.ErrNotExist
	}}
	collector := NewLinuxCollectorWithSource(source)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type answer struct {
		snapshot Snapshot
		err      error
	}
	first := make(chan answer, 1)
	second := make(chan answer, 1)
	go func() { s, e := collector.Collect(ctx); first <- answer{s, e} }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first collection did not reach source boundary")
	}
	go func() { s, e := collector.Collect(t.Context()); second <- answer{s, e} }()
	select {
	case got := <-second:
		if got.err != nil || len(got.snapshot.Components) != 2 {
			t.Fatal("independent collection inherited blocked state")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second collection shared transient state")
	}
	cancel()
	once.Do(func() { close(release) })
	select {
	case got := <-first:
		requireNoCollection(t, got.snapshot, got.err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled source boundary did not return")
	}
	if provided[0].Name() != "eth1" || provided[1].Name() != "eth0" {
		t.Fatal("concurrent collection modified provider slice")
	}
}

func TestInventoryCollectionCanonicalizationCancellationBoundary(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprint(before), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if before {
				cancel()
			}
			collection := NewLinuxCollectorWithSource(collectionSource{}).newCollection(ctx)
			calls := 0
			snapshot, err := collection.snapshot([]Component{NewComponent(ComponentCPU, "cpu:0", nil)}, func(components []Component) Snapshot {
				calls++
				cancel()
				return NewSnapshot(components)
			})
			requireNoCollection(t, snapshot, err, context.Canceled)
			want := 1
			if before {
				want = 0
			}
			if calls != want {
				t.Fatal("wrong canonicalization acquisition boundary")
			}
		})
	}
}

func TestInventoryCollectionPreservesDecoratedPathSourceDispatch(t *testing.T) {
	source := &decoratedCollectionSource{RootPathSource: NewRootPathSource(t.TempDir())}
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrDirectoryRead)
	if source.calls == 0 {
		t.Fatal("embedded production source bypassed the declared ReadDir override")
	}
}

func TestInventoryRecordIteratorCancelsAfterActualParsedRecord(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			collection := NewLinuxCollectorWithSource(collectionSource{}).newCollection(ctx)
			visited := 0
			for index, record := range collection.parseRecords("\n\nprocessor: 7\nmodel name: First CPU\n\nmodel name: Second CPU\n") {
				visited++
				if visited != 1 || index != 0 || record["processor"] != "7" || record["model name"] != "First CPU" {
					t.Fatal("unexpected parsed record after cancellation")
				}
				cancel()
				if stop {
					break
				}
			}
			if visited != 1 || collection.err != context.Canceled {
				t.Fatal("real parser missed cancellation after yielding, including break")
			}
		})
	}
}

func TestInventoryCPURecordIndicesRemainCompactedAcrossEmptyBlocks(t *testing.T) {
	content := "\n\nprocessor: 7\nmodel name: Explicit\n\n\n\nmodel name: First Fallback\n\n\n\nmodel name: Second Fallback\n\n"
	collection := NewLinuxCollectorWithSource(collectionSource{}).newCollection(t.Context())
	models := []string{"Explicit", "First Fallback", "Second Fallback"}
	count := 0
	for index, record := range collection.parseRecords(content) {
		if index != count || index >= len(models) || record["model name"] != models[index] {
			t.Fatal("CPU iterator exposed a raw or reordered block index")
		}
		count++
	}
	if count != 3 || collection.err != nil {
		t.Fatal("complete CPU record traversal failed")
	}
	source := collectionSource{file: func(name string) ([]byte, error) {
		if name == "proc/cpuinfo" {
			return []byte(content), nil
		}
		return nil, fs.ErrNotExist
	}}
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
	if err != nil || len(snapshot.Components) != 3 {
		t.Fatal("CPU fallback identity control failed")
	}
	for index, id := range []string{"cpu:1", "cpu:2", "cpu:7"} {
		if snapshot.Components[index].ID != id {
			t.Fatal("CPU fallback IDs changed across empty records")
		}
	}
	assertAttribute(t, snapshot, ComponentCPU, "cpu:1", "model", "First Fallback")
	assertAttribute(t, snapshot, ComponentCPU, "cpu:2", "model", "Second Fallback")
	assertAttribute(t, snapshot, ComponentCPU, "cpu:7", "model", "Explicit")
}

func TestInventoryInputRecordIndicesRemainRawAcrossEmptyBlocks(t *testing.T) {
	collection := NewLinuxCollectorWithSource(collectionSource{}).newCollection(t.Context())
	components := collection.parseInputDevices("\n\nN: Name=\"First\"\n\n\n\nN: Name=\"Second\"")
	if collection.err != nil || len(components) != 2 || components[0].ID != "input:1" || components[1].ID != "input:3" {
		t.Fatal("input parser lost its raw block-index contract")
	}
	if value, _ := components[0].Attribute("name"); value != "First" {
		t.Fatal("first input record changed")
	}
	if value, _ := components[1].Attribute("name"); value != "Second" {
		t.Fatal("second input record changed")
	}
}

func TestInventoryProcessingIteratorCancellationWithoutSourceCalls(t *testing.T) {
	for _, item := range []struct {
		name   string
		values []string
	}{
		{"record", []string{"first-record", "second-record", "third-record"}},
		{"line", []string{"processor: 0", "model name: Fixture", "vendor_id: Fixture"}},
		{"field", []string{"Bus=0003", "Vendor=1234", "Product=5678"}},
		{"nonmatching discovery", []string{"unrelated0", "unrelated1", "unrelated2"}},
	} {
		for _, stop := range []bool{false, true} {
			t.Run(item.name+fmt.Sprint(stop), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				sourceCalls := 0
				source := collectionSource{file: func(string) ([]byte, error) { sourceCalls++; return nil, fs.ErrNotExist }}
				collection := NewLinuxCollectorWithSource(source).newCollection(ctx)
				var processed []string
				for index, value := range collectionItems(collection, item.values) {
					processed = append(processed, value)
					if index != 0 {
						t.Fatal("processing advanced after semantic cancellation")
					}
					cancel()
					if stop {
						break
					}
					continue // Models a nonmatching discovery/filter branch with no I/O.
				}
				if len(processed) != 1 || processed[0] != item.values[0] || collection.err != context.Canceled {
					t.Fatal("iterator did not stop at the processing boundary")
				}
				collection.readProc("unused-after-cancel", procCPU)
				if sourceCalls != 0 {
					t.Fatal("selected cancellation permitted a later source acquisition")
				}
			})
		}
	}
}

func TestInventoryProcessingIteratorCompletePermutationAndEarlyBreak(t *testing.T) {
	for _, values := range [][]string{nil, {"first"}, {"first", "second", "third"}, {"third", "first", "second"}} {
		collection := NewLinuxCollectorWithSource(collectionSource{}).newCollection(t.Context())
		var got []string
		for index, value := range collectionItems(collection, values) {
			if index != len(got) {
				t.Fatal("processing index changed")
			}
			got = append(got, value)
		}
		if collection.err != nil || strings.Join(got, "|") != strings.Join(values, "|") || len(got) != len(values) {
			t.Fatal("complete ordered traversal dropped an item")
		}
	}
	collection := NewLinuxCollectorWithSource(collectionSource{}).newCollection(t.Context())
	visited := 0
	for range collectionItems(collection, []string{"first", "second"}) {
		visited++
		break
	}
	if visited != 1 || collection.err != nil {
		t.Fatal("healthy break changed collection state")
	}
	for range collectionItems(collection, []string{"third", "fourth"}) {
		visited++
	}
	if visited != 3 || collection.err != nil {
		t.Fatal("a previous early break poisoned later iteration")
	}
}

func TestInventoryNestedNonmatchingUSBIterationCancellation(t *testing.T) {
	for _, mode := range []string{"complete", "cancel continue", "cancel break"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			collection := NewLinuxCollectorWithSource(collectionSource{}).newCollection(ctx)
			var interfaces []fs.DirEntry
			for index := 0; index < 513; index++ {
				interfaces = append(interfaces, &enumerationEntry{name: fmt.Sprintf("9-9:1.%03d", index)})
			}
			outer, inner, matches := 0, 0, 0
			for _, device := range collectionItems(collection, []string{"1-1", "1-2"}) {
				outer++
				for _, entry := range collectionItems(collection, interfaces) {
					inner++
					if !strings.HasPrefix(entry.Name(), device+":") {
						if mode != "complete" && entry.Name() == "9-9:1.256" {
							cancel()
							if mode == "cancel break" {
								break
							}
						}
						continue
					}
					matches++
				}
				if !collection.check() {
					break
				}
			}
			if matches != 0 {
				t.Fatal("nonmatching interface fixture unexpectedly matched")
			}
			if mode == "complete" {
				if outer != 2 || inner != 1026 || collection.err != nil {
					t.Fatal("complete nested traversal lost nonmatching entries")
				}
			} else if outer != 1 || inner != 257 || collection.err != context.Canceled {
				t.Fatal("nested no-I/O branch continued after cancellation")
			}
		})
	}
}

func TestInventoryLongNonmatchingDiscoveryCancelsOnSemanticEntry(t *testing.T) {
	var entries []fs.DirEntry
	for index := 0; index < 513; index++ {
		entries = append(entries, &enumerationEntry{name: fmt.Sprintf("unrelated%03d", index)})
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	collection := NewLinuxCollectorWithSource(collectionSource{}).newCollection(ctx)
	var visited []string
	for _, entry := range collectionItems(collection, entries) {
		name := entry.Name()
		visited = append(visited, name)
		if !cpuDirectoryPattern.MatchString(name) {
			if name == "unrelated256" {
				cancel()
			}
			continue
		}
		t.Fatal("nonmatching discovery fixture unexpectedly matched")
	}
	if collection.err != context.Canceled || len(visited) != 257 || visited[256] != "unrelated256" {
		t.Fatal("nonmatching scan missed its semantic cancellation boundary")
	}
}
