package inventory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type enumerationEntry struct {
	name   string
	onName func()
}

func (e *enumerationEntry) Name() string {
	if e.onName != nil {
		e.onName()
	}
	return e.name
}
func (*enumerationEntry) IsDir() bool       { return true }
func (*enumerationEntry) Type() fs.FileMode { return fs.ModeDir }
func (e *enumerationEntry) Info() (fs.FileInfo, error) {
	return enumerationInfo{name: e.name, directory: true}, nil
}

type enumerationInfo struct {
	name      string
	directory bool
}

func (i enumerationInfo) Name() string { return i.name }
func (enumerationInfo) Size() int64    { return 0 }
func (i enumerationInfo) Mode() fs.FileMode {
	if i.directory {
		return fs.ModeDir | 0700
	}
	return 0600
}
func (enumerationInfo) ModTime() time.Time { return time.Time{} }
func (i enumerationInfo) IsDir() bool      { return i.directory }
func (enumerationInfo) Sys() any           { return nil }

type enumerationBatch struct {
	entries []fs.DirEntry
	err     error
}
type enumerationDirectory struct {
	batches                 []enumerationBatch
	requests                []int
	statErr, closeErr       error
	regular                 bool
	stats, closes           int
	onStat, onRead, onClose func()
}

func (d *enumerationDirectory) Stat() (fs.FileInfo, error) {
	d.stats++
	if d.onStat != nil {
		d.onStat()
	}
	return enumerationInfo{directory: !d.regular}, d.statErr
}
func (d *enumerationDirectory) ReadDir(n int) ([]fs.DirEntry, error) {
	d.requests = append(d.requests, n)
	if d.onRead != nil {
		d.onRead()
	}
	if len(d.batches) == 0 {
		return nil, io.EOF
	}
	b := d.batches[0]
	d.batches = d.batches[1:]
	return b.entries, b.err
}
func (d *enumerationDirectory) Close() error {
	d.closes++
	if d.onClose != nil {
		d.onClose()
	}
	return d.closeErr
}
func enumerationEntries(names ...string) []fs.DirEntry {
	var entries []fs.DirEntry
	for _, name := range names {
		entries = append(entries, &enumerationEntry{name: name})
	}
	return entries
}

type absenceCycle struct{ next error }

func (*absenceCycle) Error() string   { return "fixture cycle" }
func (e *absenceCycle) Unwrap() error { return e.next }

type absenceImpostor struct{}

func (absenceImpostor) Error() string { return "fixture" }
func (absenceImpostor) Is(error) bool { return true }

func TestInventoryDirectorySoleAbsencePolicy(t *testing.T) {
	cycle := &absenceCycle{}
	cycle.next = cycle
	var typedNil *absenceCycle
	deep := error(fs.ErrNotExist)
	for i := 0; i < 40; i++ {
		deep = fmt.Errorf("fixture: %w", deep)
	}
	for _, item := range []struct {
		name string
		err  error
		want bool
	}{
		{"sentinel", fs.ErrNotExist, true}, {"platform", syscall.ENOENT, true},
		{"single wrapper", fmt.Errorf("fixture: %w", fs.ErrNotExist), true},
		{"open path", &fs.PathError{Op: "open", Path: "neutral-fixture", Err: fs.ErrNotExist}, true},
		{"read path", &fs.PathError{Op: "readdir", Path: "neutral-fixture", Err: fs.ErrNotExist}, false},
		{"close path", &fs.PathError{Op: "close", Path: "neutral-fixture", Err: fs.ErrNotExist}, false},
		{"one joined cause", errors.Join(fs.ErrNotExist), false},
		{"absence permission", errors.Join(fs.ErrNotExist, fs.ErrPermission), false},
		{"permission absence", errors.Join(fs.ErrPermission, fs.ErrNotExist), false},
		{"wrapped join", fmt.Errorf("fixture: %w", errors.Join(fs.ErrNotExist, fs.ErrPermission)), false},
		{"permission", fs.ErrPermission, false}, {"message", errors.New("file does not exist"), false},
		{"custom Is", absenceImpostor{}, false}, {"cycle", cycle, false}, {"deep", deep, false}, {"nil", nil, false}, {"typed nil", typedNil, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			if soleOpenAbsence(item.err) != item.want {
				t.Fatal("wrong sole-absence classification")
			}
		})
	}
}

func TestInventoryDirectoryReadsThroughEOFAndOwnsSlices(t *testing.T) {
	for _, item := range []struct {
		name    string
		batches []enumerationBatch
		want    string
		reads   int
	}{
		{"short then more", []enumerationBatch{{enumerationEntries("z"), nil}, {enumerationEntries("a"), io.EOF}}, "a,z", 2},
		{"separate eof", []enumerationBatch{{enumerationEntries("z", "a"), nil}, {nil, io.EOF}}, "a,z", 2},
		{"entries eof", []enumerationBatch{{enumerationEntries("z", "a"), io.EOF}}, "a,z", 1},
		{"permuted batches", []enumerationBatch{{enumerationEntries("a"), nil}, {enumerationEntries("z"), io.EOF}}, "a,z", 2},
		{"empty eof", []enumerationBatch{{nil, io.EOF}}, "", 1},
	} {
		t.Run(item.name, func(t *testing.T) {
			d := &enumerationDirectory{batches: item.batches}
			first := item.batches[0].entries
			var original string
			if len(first) > 0 {
				original = first[0].Name()
			}
			entries, err := readInventoryDirectory(t.Context(), "ignored", func(string) (inventoryDirectory, error) { return d, nil })
			var names []string
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			if err != nil || strings.Join(names, ",") != item.want || len(d.requests) != item.reads || d.closes != 1 || d.stats != 1 {
				t.Fatalf("incomplete enumeration: %v %v", names, err)
			}
			for _, n := range d.requests {
				if n != 64 {
					t.Fatal("request is not the fixed positive batch")
				}
			}
			if len(first) > 0 && first[0].Name() != original {
				t.Fatal("provider backing slice was mutated")
			}
		})
	}
}

func TestInventoryDirectoryExactBatchRequiresNextEOF(t *testing.T) {
	var entries []fs.DirEntry
	for i := 0; i < 64; i++ {
		entries = append(entries, &enumerationEntry{name: fmt.Sprintf("item%03d", i)})
	}
	d := &enumerationDirectory{batches: []enumerationBatch{{entries, nil}, {nil, io.EOF}}}
	got, err := readInventoryDirectory(t.Context(), "ignored", func(string) (inventoryDirectory, error) { return d, nil })
	if err != nil || len(got) != 64 || len(d.requests) != 2 || d.closes != 1 {
		t.Fatal("full batch was treated as completion")
	}
}

func TestInventoryDirectoryFailureAndCleanupPrecedence(t *testing.T) {
	for _, item := range []struct {
		name                                                                                string
		openErr, statErr, readErr, closeErr                                                 error
		noHandle, regular, zero, oversized, cancelOpen, cancelStat, cancelRead, cancelClose bool
		want                                                                                error
	}{
		{name: "missing", openErr: fs.ErrNotExist, noHandle: true, want: fs.ErrNotExist},
		{name: "denied", openErr: fs.ErrPermission, noHandle: true, want: ErrDirectoryRead},
		{name: "joined missing", openErr: errors.Join(fs.ErrNotExist, fs.ErrPermission), noHandle: true, want: ErrDirectoryRead},
		{name: "nil nil", noHandle: true, want: ErrDirectoryRead},
		{name: "handle and absence", openErr: fs.ErrNotExist, want: ErrDirectoryRead},
		{name: "stat absence", statErr: fs.ErrNotExist, want: ErrDirectoryRead},
		{name: "regular file", regular: true, want: ErrDirectoryRead},
		{name: "read absence", readErr: fs.ErrNotExist, want: ErrDirectoryRead},
		{name: "partial io", readErr: io.ErrUnexpectedEOF, want: ErrDirectoryRead},
		{name: "joined eof", readErr: errors.Join(io.EOF, fs.ErrPermission), want: ErrDirectoryRead},
		{name: "zero progress", zero: true, want: ErrDirectoryRead},
		{name: "impossible count", oversized: true, want: ErrDirectoryRead},
		{name: "close missing", closeErr: fs.ErrNotExist, want: ErrDirectoryClose},
		{name: "read and close", readErr: io.ErrUnexpectedEOF, closeErr: fs.ErrPermission, want: ErrDirectoryClose},
		{name: "clean missing cancel", noHandle: true, openErr: fs.ErrNotExist, cancelOpen: true, want: context.Canceled},
		{name: "denied cancel", noHandle: true, openErr: fs.ErrPermission, cancelOpen: true, want: ErrDirectoryRead},
		{name: "open cancel", cancelOpen: true, want: context.Canceled},
		{name: "stat cancel", cancelStat: true, want: context.Canceled},
		{name: "stat failure cancel", statErr: fs.ErrPermission, cancelStat: true, want: ErrDirectoryRead},
		{name: "read cancel", cancelRead: true, want: context.Canceled},
		{name: "read failure cancel", readErr: fs.ErrPermission, cancelRead: true, want: ErrDirectoryRead},
		{name: "close cancel", cancelClose: true, want: context.Canceled},
		{name: "close failure cancel", closeErr: fs.ErrNotExist, cancelClose: true, want: ErrDirectoryClose},
	} {
		t.Run(item.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			batch := enumerationEntries("neutral")
			readErr := item.readErr
			if readErr == nil {
				readErr = io.EOF
			}
			if item.zero {
				batch = nil
				readErr = nil
			}
			if item.oversized {
				batch = make([]fs.DirEntry, 65)
			}
			d := &enumerationDirectory{regular: item.regular, statErr: item.statErr, closeErr: item.closeErr, batches: []enumerationBatch{{batch, readErr}}}
			if item.cancelStat {
				d.onStat = cancel
			}
			if item.cancelRead {
				d.onRead = cancel
			}
			if item.cancelClose {
				d.onClose = cancel
			}
			got, err := readInventoryDirectory(ctx, "never-export-this-root", func(string) (inventoryDirectory, error) {
				if item.cancelOpen {
					cancel()
				}
				if item.noHandle {
					return nil, item.openErr
				}
				return d, item.openErr
			})
			if !errors.Is(err, item.want) || got != nil {
				t.Fatalf("wrong result: %v, entries %d", err, len(got))
			}
			wantClose := 1
			if item.noHandle {
				wantClose = 0
			}
			if d.closes != wantClose {
				t.Fatal("handle not closed exactly once")
			}
			if strings.Contains(err.Error(), "never-export") {
				t.Fatal("error leaked source path")
			}
			if (item.openErr != nil || item.cancelOpen) && !item.noHandle && (d.stats != 0 || len(d.requests) != 0) {
				t.Fatal("work followed an open failure or cancellation")
			}
		})
	}
}

func TestInventoryDirectoryValidatesOnlyOwnedNamesAfterClosure(t *testing.T) {
	var typedNil *enumerationEntry
	for _, item := range []struct {
		name    string
		entries []fs.DirEntry
	}{
		{"nil", []fs.DirEntry{nil}}, {"typed nil", []fs.DirEntry{typedNil}},
		{"duplicate", enumerationEntries("same", "same")}, {"empty", enumerationEntries("")},
		{"dot", enumerationEntries(".")}, {"parent", enumerationEntries("..")},
		{"slash", enumerationEntries("one/two")}, {"backslash", enumerationEntries("one\\two")}, {"nul", enumerationEntries("one\x00two")},
	} {
		t.Run(item.name, func(t *testing.T) {
			d := &enumerationDirectory{batches: []enumerationBatch{{item.entries, io.EOF}}}
			got, err := readInventoryDirectory(t.Context(), "ignored", func(string) (inventoryDirectory, error) { return d, nil })
			if err != ErrDirectoryEntry || got != nil || d.closes != 1 {
				t.Fatal("invalid entry was admitted")
			}
		})
	}
	d := &enumerationDirectory{}
	entry := &enumerationEntry{name: "valid", onName: func() {
		if d.closes != 1 {
			t.Fatal("source name observed before closing directory")
		}
	}}
	d.batches = []enumerationBatch{{[]fs.DirEntry{entry}, io.EOF}}
	got, err := readInventoryDirectory(t.Context(), "ignored", func(string) (inventoryDirectory, error) { return d, nil })
	if err != nil {
		t.Fatal(err)
	}
	entry.name = "../changed"
	if got[0].Name() != "valid" {
		t.Fatal("validated name was not retained")
	}
}

func TestInventoryDirectoryCancellationDuringNameValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := &enumerationDirectory{batches: []enumerationBatch{{[]fs.DirEntry{&enumerationEntry{name: "first", onName: cancel}, &enumerationEntry{name: "second", onName: func() { t.Fatal("validation continued after cancellation") }}}, io.EOF}}}
	entries, err := readInventoryDirectory(ctx, "ignored", func(string) (inventoryDirectory, error) { return d, nil })
	if err != context.Canceled || entries != nil || d.closes != 1 {
		t.Fatal("name cancellation escaped")
	}
}

func TestInventoryDirectoryDependenciesAndInitialCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	open := func(string) (inventoryDirectory, error) { called = true; return nil, nil }
	for _, item := range []struct {
		ctx  context.Context
		open func(string) (inventoryDirectory, error)
		want error
	}{
		{nil, open, ErrDirectoryRead}, {t.Context(), nil, ErrDirectoryRead}, {ctx, open, context.Canceled},
	} {
		got, err := readInventoryDirectory(item.ctx, "ignored", item.open)
		if got != nil || err != item.want {
			t.Fatal("dependency/cancellation guard failed")
		}
	}
	if called {
		t.Fatal("guard reached opener")
	}
	var typedNil *enumerationDirectory
	if _, err := readInventoryDirectory(t.Context(), "ignored", func(string) (inventoryDirectory, error) { return typedNil, nil }); err != ErrDirectoryRead {
		t.Fatal("typed-nil handle accepted")
	}
}

func TestInventoryProductionDirectoryRetainsMoreThan256Entries(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 325; i++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("disk%03d", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	var opened *os.File
	entries, err := readInventoryDirectory(t.Context(), root, func(name string) (inventoryDirectory, error) {
		d, err := openInventoryDirectory(name)
		if d != nil {
			opened = d.(*os.File)
		}
		return d, err
	})
	if err != nil || len(entries) != 325 {
		t.Fatalf("large directory truncated: %d %v", len(entries), err)
	}
	for i, entry := range entries {
		if entry.Name() != fmt.Sprintf("disk%03d", i) {
			t.Fatal("lost or reordered admitted instance")
		}
	}
	if opened == nil {
		t.Fatal("production opener not used")
	}
	// Closed-handle probe: an unclosed handle returns nil and fails.
	if err := opened.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("production descriptor remains open")
	}
	file := filepath.Join(t.TempDir(), "ordinary-file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := readInventoryDirectory(t.Context(), file, openInventoryDirectory); got != nil || err != ErrDirectoryRead {
		t.Fatal("regular file accepted as directory")
	}
}
