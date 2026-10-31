package inventory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInventoryDirectoryBudgetRecordBoundary(t *testing.T) {
	for _, item := range []struct {
		name     string
		limit    int
		batches  []enumerationBatch
		requests []int
		want     string
		wantErr  error
	}{
		{"zero empty eof", 0, []enumerationBatch{{nil, io.EOF}}, []int{1}, "", nil},
		{"zero overflow", 0, []enumerationBatch{{enumerationEntries("a"), io.EOF}}, []int{1}, "", ErrDirectoryBudget},
		{"exact entries eof", 3, []enumerationBatch{{enumerationEntries("z", "b", "a"), io.EOF}}, []int{4}, "a,b,z", nil},
		{"exact separate eof", 3, []enumerationBatch{{enumerationEntries("z", "b", "a"), nil}, {nil, io.EOF}}, []int{4, 1}, "a,b,z", nil},
		{"overflow with eof", 3, []enumerationBatch{{enumerationEntries("z", "b", "a", "x"), io.EOF}}, []int{4}, "", ErrDirectoryBudget},
		{"overflow after exact", 3, []enumerationBatch{{enumerationEntries("z", "b", "a"), nil}, {enumerationEntries("x"), io.EOF}}, []int{4, 1}, "", ErrDirectoryBudget},
	} {
		t.Run(item.name, func(t *testing.T) {
			d := &enumerationDirectory{batches: item.batches}
			nameCalls := 0
			for _, batch := range item.batches {
				for _, entry := range batch.entries {
					entry.(*enumerationEntry).onName = func() {
						nameCalls++
						if d.closes != 1 {
							t.Fatal("name observed before closure")
						}
					}
				}
			}
			budget := inventoryDirectoryBudget{entries: item.limit, nameBytes: 32}
			got, err := readInventoryDirectoryWithBudget(t.Context(), "private-source", func(string) (inventoryDirectory, error) { return d, nil }, &budget)
			if err != item.wantErr || !reflect.DeepEqual(d.requests, item.requests) || d.stats != 1 || d.closes != 1 {
				t.Fatalf("wrong boundary or owner result: %v %v", err, d.requests)
			}
			if err != nil {
				if got != nil || nameCalls != 0 {
					t.Fatal("overflow retained entries or observed names")
				}
				return
			}
			var names []string
			for _, entry := range got {
				names = append(names, entry.Name())
			}
			if strings.Join(names, ",") != item.want || nameCalls != len(got) || cap(got) > item.limit || budget.entries != item.limit-len(got) {
				t.Fatal("exact boundary lost entries, copied excess capacity, or charged incorrectly")
			}
		})
	}
}

func TestInventoryDirectoryBudgetManyBatches(t *testing.T) {
	provided := make([]fs.DirEntry, 257)
	for i := range provided {
		provided[i] = &enumerationEntry{name: fmt.Sprintf("item%03d", 256-i)}
	}
	d := &enumerationDirectory{}
	for start := 0; start < 256; start += 64 {
		d.batches = append(d.batches, enumerationBatch{provided[start : start+64], nil})
	}
	d.batches = append(d.batches, enumerationBatch{provided[256:], nil}, enumerationBatch{nil, io.EOF})
	budget := inventoryDirectoryBudget{entries: 257, nameBytes: 4096}
	got, err := readInventoryDirectoryWithBudget(t.Context(), "ignored", func(string) (inventoryDirectory, error) { return d, nil }, &budget)
	if err != nil || len(got) != 257 || cap(got) != 257 || d.closes != 1 || !reflect.DeepEqual(d.requests, []int{64, 64, 64, 64, 2, 1}) {
		t.Fatalf("bounded enumeration did not reach exact EOF: %v %v", err, d.requests)
	}
	for i, entry := range got {
		if entry.Name() != fmt.Sprintf("item%03d", i) || provided[i].(*enumerationEntry).name != fmt.Sprintf("item%03d", 256-i) {
			t.Fatal("enumeration lost entries or modified a provider slice")
		}
	}
}

func TestInventoryDirectoryBudgetNameBoundary(t *testing.T) {
	for _, item := range []struct {
		name         string
		names        []string
		limit, calls int
		wantErr      error
	}{
		{"exact bytes", []string{"ab", "cde"}, 5, 2, nil},
		{"aggregate overflow", []string{"ab", "cde"}, 4, 2, ErrDirectoryBudget},
		{"multibyte exact", []string{"é", "a"}, 3, 2, nil},
		{"multibyte overflow", []string{"é"}, 1, 1, ErrDirectoryBudget},
		{"first name overflow", []string{strings.Repeat("x", 65), "unobserved"}, 64, 1, ErrDirectoryBudget},
	} {
		t.Run(item.name, func(t *testing.T) {
			d := &enumerationDirectory{}
			provided := enumerationEntries(item.names...)
			calls := 0
			for _, entry := range provided {
				entry.(*enumerationEntry).onName = func() {
					calls++
					if d.closes != 1 || calls > item.calls {
						t.Fatal("name observed outside the admitted boundary")
					}
				}
			}
			d.batches = []enumerationBatch{{provided, io.EOF}}
			budget := inventoryDirectoryBudget{entries: 2, nameBytes: item.limit}
			got, err := readInventoryDirectoryWithBudget(t.Context(), "ignored", func(string) (inventoryDirectory, error) { return d, nil }, &budget)
			if err != item.wantErr || calls != item.calls || d.closes != 1 {
				t.Fatal("wrong name-byte result")
			}
			if err != nil {
				if got != nil {
					t.Fatal("name overflow returned a partial directory")
				}
			} else if len(got) != len(item.names) || budget.nameBytes != 0 {
				t.Fatal("exact name-byte limit was not fully admitted")
			}
		})
	}
}

func TestInventoryDirectoryBudgetCleanupPrecedence(t *testing.T) {
	for _, item := range []struct {
		name                                string
		entries, names, records             int
		readErr, closeErr                   error
		cancelRead, cancelClose, cancelName bool
		want                                error
	}{
		{name: "record overflow", entries: 1, names: 8, records: 2, want: ErrDirectoryBudget},
		{name: "record overflow close failure", entries: 1, names: 8, records: 2, closeErr: fs.ErrPermission, want: ErrDirectoryClose},
		{name: "malformed read before budget", entries: 1, names: 8, records: 2, readErr: io.ErrUnexpectedEOF, want: ErrDirectoryRead},
		{name: "oversized response", entries: 1, names: 8, records: 3, want: ErrDirectoryRead},
		{name: "read cancellation", entries: 1, names: 8, records: 2, cancelRead: true, want: context.Canceled},
		{name: "read cancellation close failure", entries: 1, names: 8, records: 2, cancelRead: true, closeErr: fs.ErrPermission, want: ErrDirectoryClose},
		{name: "name overflow", entries: 2, names: 1, records: 2, want: ErrDirectoryBudget},
		{name: "name overflow close failure", entries: 2, names: 1, records: 2, closeErr: fs.ErrPermission, want: ErrDirectoryClose},
		{name: "overflow then close cancellation", entries: 1, names: 8, records: 2, cancelClose: true, want: ErrDirectoryBudget},
		{name: "valid read then close cancellation", entries: 1, names: 1, records: 1, cancelClose: true, want: context.Canceled},
		{name: "name cancellation", entries: 1, names: 1, records: 1, cancelName: true, want: context.Canceled},
	} {
		t.Run(item.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			d := &enumerationDirectory{closeErr: item.closeErr}
			var provided []fs.DirEntry
			for i := 0; i < item.records; i++ {
				provided = append(provided, &enumerationEntry{name: string(rune('a' + i)), onName: func() {
					if d.closes != 1 || item.closeErr != nil {
						t.Fatal("name admitted without successful closure")
					}
					if item.cancelName {
						cancel()
					}
				}})
			}
			readErr := item.readErr
			if readErr == nil {
				readErr = io.EOF
			}
			d.batches = []enumerationBatch{{provided, readErr}}
			if item.cancelRead {
				d.onRead = cancel
			}
			if item.cancelClose {
				d.onClose = cancel
			}
			budget := inventoryDirectoryBudget{entries: item.entries, nameBytes: item.names}
			got, err := readInventoryDirectoryWithBudget(ctx, "never-export-source", func(string) (inventoryDirectory, error) { return d, nil }, &budget)
			if got != nil || err != item.want || d.closes != 1 || len(d.requests) != 1 || strings.Contains(err.Error(), "never-export") {
				t.Fatalf("wrong failure/cleanup precedence: %v", err)
			}
		})
	}
}

func TestInventoryCollectionDirectoryBudgetAcrossRegistries(t *testing.T) {
	for _, item := range []struct {
		name           string
		entries, names int
	}{
		{"aggregate records", 257, 4096},
		{"aggregate name bytes", 4096, 257},
	} {
		t.Run(item.name, func(t *testing.T) {
			calls, names := 0, 0
			source := collectionSource{directory: func(string) ([]fs.DirEntry, error) {
				calls++
				return []fs.DirEntry{&enumerationEntry{name: "x", onName: func() { names++ }}}, nil
			}}
			collection := NewLinuxCollectorWithSource(source).newCollection(t.Context())
			collection.directoryBudget = inventoryDirectoryBudget{entries: item.entries, nameBytes: item.names}
			// Each individual directory is harmless; the independently chosen
			// total spans more directories than one 64-entry read batch.
			for i := 0; i < 257; i++ {
				if got := collection.readDir(fmt.Sprintf("registry/%03d", i)); len(got) != 1 || collection.err != nil {
					t.Fatalf("premature aggregate rejection at %d", i)
				}
			}
			if calls != 257 || names != 257 {
				t.Fatal("exact aggregate boundary was not observed")
			}
			if got := collection.readDir("registry/overflow"); got != nil || collection.err != ErrDirectoryBudget {
				t.Fatal("many-directory overflow accepted")
			}
			wantNames := 258
			if item.name == "aggregate records" {
				wantNames = 257
			}
			if calls != 258 || names != wantNames {
				t.Fatal("record overflow reached name callbacks")
			}
			collection.readDir("must-not-read")
			snapshot, err := collection.snapshot([]Component{{}}, func([]Component) Snapshot { t.Fatal("partial inventory canonicalized"); return Snapshot{} })
			requireNoCollection(t, snapshot, err, ErrDirectoryBudget)
			if calls != 258 {
				t.Fatal("source work continued after exhaustion")
			}
		})
	}
}

func TestInventoryCollectionDirectoryBudgetAbsenceAndIsolation(t *testing.T) {
	source := collectionSource{directory: func(name string) ([]fs.DirEntry, error) {
		switch name {
		case "missing":
			return nil, fs.ErrNotExist
		case "empty":
			return nil, nil
		default:
			return enumerationEntries("x"), nil
		}
	}}
	collector := NewLinuxCollectorWithSource(source)
	first, second := collector.newCollection(t.Context()), collector.newCollection(t.Context())
	if first.directoryBudget.entries != 65536 || first.directoryBudget.nameBytes != 8388608 || first.directoryBudget != second.directoryBudget {
		t.Fatal("unexpected default collection budget")
	}
	first.directoryBudget = inventoryDirectoryBudget{entries: 1, nameBytes: 1}
	for i := 0; i < 10; i++ {
		first.readDir("missing")
		first.readDir("empty")
	}
	if got := first.readDir("present"); len(got) != 1 || first.err != nil {
		t.Fatal("absence consumed admission budget")
	}
	first.readDir("missing")
	first.readDir("empty")
	if first.err != nil || first.directoryBudget.entries != 0 || first.directoryBudget.nameBytes != 0 {
		t.Fatal("empty result failed at exact limit")
	}
	if got := first.readDir("overflow"); got != nil || first.err != ErrDirectoryBudget {
		t.Fatal("overflow accepted")
	}
	if got := second.readDir("present"); len(got) != 1 || second.err != nil || second.directoryBudget.entries != 65535 || second.directoryBudget.nameBytes != 8388607 {
		t.Fatal("one collection consumed another collection's budget")
	}
}

func TestInventoryCollectionDirectoryBudgetDefaultAndReset(t *testing.T) {
	// Exercise the real Collect entry point with the literal policy boundary.
	// Non-CPU names remain acquisition records even though CPU parsing ignores them.
	provided := make([]fs.DirEntry, 65536)
	for i := range provided {
		provided[i] = &enumerationEntry{name: fmt.Sprintf("entry%05d", i)}
	}
	overflow, failed := false, false
	cpuReads := 0
	source := collectionSource{
		file: func(string) ([]byte, error) {
			if failed {
				t.Fatal("file acquisition after budget failure")
			}
			return nil, fs.ErrNotExist
		},
		directory: func(name string) ([]fs.DirEntry, error) {
			if failed {
				t.Fatal("directory acquisition after budget failure")
			}
			if name == "sys/devices/system/cpu" {
				cpuReads++
				return provided, nil
			}
			if name == "sys/class/block" && overflow {
				failed = true
				return []fs.DirEntry{&enumerationEntry{name: "extra", onName: func() { t.Fatal("overflow entry admitted") }}}, nil
			}
			return nil, fs.ErrNotExist
		},
	}
	collector := NewLinuxCollectorWithSource(source)
	for i := 0; i < 2; i++ {
		snapshot, err := collector.Collect(t.Context())
		if err != nil || snapshot.SchemaVersion != 1 || len(snapshot.Components) != 0 {
			t.Fatalf("exact default limit failed: %v", err)
		}
	}
	overflow = true
	snapshot, err := collector.Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrDirectoryBudget)
	if !failed {
		t.Fatal("overflow prerequisite was not reached")
	}
	overflow, failed = false, false
	snapshot, err = collector.Collect(t.Context())
	if err != nil || snapshot.SchemaVersion != 1 || len(snapshot.Components) != 0 || cpuReads != 4 {
		t.Fatal("later collection retained an exhausted budget or failure")
	}
}

func TestInventoryRootDirectoryBudgetShared(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"first/a", "first/b", "second/c", "second/d"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(name)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	collector := NewLinuxCollector(root)
	collection := collector.newCollection(t.Context())
	collection.directoryBudget = inventoryDirectoryBudget{entries: 3, nameBytes: 3}
	if got := collection.readDir("first"); len(got) != 2 || collection.err != nil {
		t.Fatal("root exact first directory failed")
	}
	if got := collection.readDir("second"); got != nil || collection.err != ErrDirectoryBudget {
		t.Fatal("root source did not share the collection budget")
	}
	snapshot, err := collection.snapshot([]Component{{}}, func([]Component) Snapshot { t.Fatal("root overflow produced a snapshot"); return Snapshot{} })
	requireNoCollection(t, snapshot, err, ErrDirectoryBudget)
	fresh := collector.newCollection(t.Context())
	fresh.directoryBudget = inventoryDirectoryBudget{entries: 4, nameBytes: 4}
	if len(fresh.readDir("first")) != 2 || len(fresh.readDir("second")) != 2 || fresh.err != nil || fresh.directoryBudget.entries != 0 || fresh.directoryBudget.nameBytes != 0 {
		t.Fatal("root exact total or fresh state failed")
	}
}

type directoryBudgetOverride struct {
	*RootPathSource
	provided []fs.DirEntry
	calls    int
}

func (source *directoryBudgetOverride) ReadDir(string) ([]fs.DirEntry, error) {
	source.calls++
	return source.provided, nil
}

func TestInventoryDirectoryBudgetLegacyOverride(t *testing.T) {
	provided := enumerationEntries("z", "a")
	source := &directoryBudgetOverride{RootPathSource: NewRootPathSource("unused-root"), provided: provided}
	collection := NewLinuxCollectorWithSource(source).newCollection(t.Context())
	collection.directoryBudget = inventoryDirectoryBudget{entries: 2, nameBytes: 2}
	got := collection.readDir("unused-directory")
	if collection.err != nil || len(got) != 2 || got[0].Name() != "a" || got[1].Name() != "z" || source.calls != 1 {
		t.Fatal("declared legacy override bypassed")
	}
	if provided[0].(*enumerationEntry).name != "z" || provided[1].(*enumerationEntry).name != "a" {
		t.Fatal("provider slice was sorted in place")
	}
	provided[1].(*enumerationEntry).name = "../changed"
	if got[0].Name() != "a" {
		t.Fatal("admitted name was not retained")
	}
	source.provided = []fs.DirEntry{&enumerationEntry{name: "unobserved", onName: func() { t.Fatal("over-budget legacy result reached Name") }}}
	if collection.readDir("overflow") != nil || !errors.Is(collection.err, ErrDirectoryBudget) || source.calls != 2 {
		t.Fatal("legacy aggregate was not rejected before owned retention")
	}
	if collection.err.Error() != "inventory: directory acquisition budget exceeded" {
		t.Fatal("budget error exposed source data")
	}
}

func TestInventoryDirectoryBudgetInvalidState(t *testing.T) {
	for _, item := range []struct {
		name   string
		budget *inventoryDirectoryBudget
	}{
		{"nil", nil},
		{"negative records", &inventoryDirectoryBudget{entries: -1, nameBytes: 1}},
		{"negative bytes", &inventoryDirectoryBudget{entries: 1, nameBytes: -1}},
		{"excess records", &inventoryDirectoryBudget{entries: 65537, nameBytes: 1}},
		{"excess bytes", &inventoryDirectoryBudget{entries: 1, nameBytes: 8388609}},
	} {
		t.Run(item.name, func(t *testing.T) {
			got, err := readInventoryDirectoryWithBudget(t.Context(), "ignored", func(string) (inventoryDirectory, error) { t.Fatal("invalid budget reached open"); return nil, nil }, item.budget)
			if got != nil || err != ErrDirectoryBudget {
				t.Fatal("invalid reader budget accepted")
			}
			got, err = ownDirectoryEntries(t.Context(), []fs.DirEntry{&enumerationEntry{name: "unobserved", onName: func() { t.Fatal("invalid budget reached Name") }}}, item.budget)
			if got != nil || err != ErrDirectoryBudget {
				t.Fatal("invalid aggregate budget accepted")
			}
		})
	}
}
