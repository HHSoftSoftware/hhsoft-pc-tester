package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type registryEntryFixture struct {
	name   string
	onName func()
}

func (entry registryEntryFixture) Name() string {
	if entry.onName != nil {
		entry.onName()
	}
	return entry.name
}

func (registryEntryFixture) IsDir() bool                { return false }
func (registryEntryFixture) Type() os.FileMode          { return os.ModeSymlink }
func (registryEntryFixture) Info() (os.FileInfo, error) { return nil, os.ErrInvalid }

func registryEntries(names ...string) []os.DirEntry {
	var entries []os.DirEntry
	for _, name := range names {
		entries = append(entries, registryEntryFixture{name: name})
	}
	return entries
}

type registryReadStep struct {
	entries []os.DirEntry
	err     error
	hook    func()
}

type registryDirectoryFixture struct {
	steps          []registryReadStep
	requests       []int
	closeCalls     int
	closeErr       error
	closeHook      func()
	unexpectedRead bool
}

func (directory *registryDirectoryFixture) ReadDir(count int) ([]os.DirEntry, error) {
	index := len(directory.requests)
	directory.requests = append(directory.requests, count)
	if directory.closeCalls != 0 || index >= len(directory.steps) {
		directory.unexpectedRead = true
		return nil, errors.New("unexpected directory read")
	}
	step := directory.steps[index]
	if step.hook != nil {
		step.hook()
	}
	return step.entries, step.err
}

func (directory *registryDirectoryFixture) Close() error {
	directory.closeCalls++
	if directory.closeHook != nil {
		directory.closeHook()
	}
	return directory.closeErr
}

func registryOpener(directory blockRegistryDirectory) func(string) (blockRegistryDirectory, error) {
	return func(string) (blockRegistryDirectory, error) { return directory, nil }
}

func registryNames(entries []os.DirEntry) []string {
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func checkRegistryRequests(t *testing.T, directory *registryDirectoryFixture) {
	t.Helper()
	retained := 0
	for index, requested := range directory.requests {
		if requested <= 0 || requested > 257-retained {
			t.Fatalf("unbounded directory request: %d after %d entries", requested, retained)
		}
		retained += len(directory.steps[index].entries)
		if retained > 257 {
			t.Fatal("fixture exceeded the allowed overflow sentinel")
		}
	}
	if directory.closeCalls != 1 || directory.unexpectedRead {
		t.Fatalf("directory ownership changed: closes=%d unexpected=%t", directory.closeCalls, directory.unexpectedRead)
	}
}

func TestReadBlockRegistryRequiresCompleteSortedObservation(t *testing.T) {
	for _, test := range []struct {
		name  string
		steps []registryReadStep
		want  []string
	}{
		{"empty EOF", []registryReadStep{{err: io.EOF}}, nil},
		{"entries with EOF", []registryReadStep{{entries: registryEntries("sdb", "dm-0", "sda1", "sda"), err: io.EOF}}, []string{"dm-0", "sda", "sda1", "sdb"}},
		{"short nil batches", []registryReadStep{
			{entries: registryEntries("sdb")},
			{entries: registryEntries("dm-0", "sda1")},
			{entries: registryEntries("sda")},
			{err: io.EOF},
		}, []string{"dm-0", "sda", "sda1", "sdb"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := &registryDirectoryFixture{steps: test.steps}
			entries, err := readBlockRegistry(t.Context(), "registry", registryOpener(directory))
			if err != nil || !reflect.DeepEqual(registryNames(entries), test.want) || len(directory.requests) != len(test.steps) {
				t.Fatalf("incomplete or reordered observation: %v, %v", registryNames(entries), err)
			}
			checkRegistryRequests(t, directory)
		})
	}
}

func TestReadBlockRegistryClosesBeforeInspectingNames(t *testing.T) {
	directory := &registryDirectoryFixture{}
	nameObserved := func() {
		if directory.closeCalls != 1 {
			t.Fatal("names were processed before directory closure")
		}
	}
	directory.steps = []registryReadStep{{entries: []os.DirEntry{
		registryEntryFixture{name: "sdb", onName: nameObserved},
		registryEntryFixture{name: "sda", onName: nameObserved},
	}, err: io.EOF}}
	entries, err := readBlockRegistry(t.Context(), "registry", registryOpener(directory))
	if err != nil || !reflect.DeepEqual(registryNames(entries), []string{"sda", "sdb"}) {
		t.Fatalf("closed observation changed: %v, %v", entries, err)
	}
	checkRegistryRequests(t, directory)
}

func TestReadBlockRegistryChecksCancellationBeforeAndAfterSorting(t *testing.T) {
	for _, phase := range []string{"close", "name"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			names := 0
			observeName := func() {
				names++
				if phase == "name" {
					cancel()
				}
			}
			directory := &registryDirectoryFixture{steps: []registryReadStep{{entries: []os.DirEntry{
				registryEntryFixture{name: "sdb", onName: observeName},
				registryEntryFixture{name: "sda", onName: observeName},
			}, err: io.EOF}}}
			if phase == "close" {
				directory.closeHook = cancel
			}
			entries, err := readBlockRegistry(ctx, "registry", registryOpener(directory))
			if err != context.Canceled || entries != nil || directory.closeCalls != 1 || phase == "close" && names != 0 || phase == "name" && names == 0 {
				t.Fatalf("sorting crossed a cancelled admission: err=%v names=%d closes=%d", err, names, directory.closeCalls)
			}
		})
	}
}

func TestReadBlockRegistryPreservesExactEntryBoundary(t *testing.T) {
	for _, count := range []int{255, 256, 257} {
		for _, fragmented := range []bool{false, true} {
			t.Run(fmt.Sprintf("entries-%d-fragmented-%t", count, fragmented), func(t *testing.T) {
				var all []os.DirEntry
				for index := count - 1; index >= 0; index-- {
					all = append(all, registryEntryFixture{name: fmt.Sprintf("entry-%03d", index)})
				}
				directory := &registryDirectoryFixture{steps: []registryReadStep{{entries: all, err: io.EOF}}}
				if fragmented {
					directory.steps = nil
					for _, entry := range all {
						directory.steps = append(directory.steps, registryReadStep{entries: []os.DirEntry{entry}})
					}
					directory.steps = append(directory.steps, registryReadStep{err: io.EOF})
				}
				entries, err := readBlockRegistry(t.Context(), "registry", registryOpener(directory))
				if count == 257 {
					if err != errBlockRegistryLimit || entries != nil {
						t.Fatalf("overflow became an admitted prefix: %d, %v", len(entries), err)
					}
				} else if err != nil || len(entries) != count || entries[0].Name() != "entry-000" || entries[count-1].Name() != fmt.Sprintf("entry-%03d", count-1) {
					t.Fatalf("complete boundary lost entries: %d, %v", len(entries), err)
				}
				wantReads := 1
				if fragmented {
					wantReads = count + 1
					if count == 257 {
						wantReads = count
					}
				}
				if len(directory.requests) != wantReads {
					t.Fatalf("wrong terminal boundary: reads=%d want=%d", len(directory.requests), wantReads)
				}
				checkRegistryRequests(t, directory)
			})
		}
	}
}

func TestReadBlockRegistryRejectsIncompleteAndMalformedResponses(t *testing.T) {
	failure := errors.New("fixture directory failure")
	for _, test := range []struct {
		name  string
		steps []registryReadStep
	}{
		{"entries with error", []registryReadStep{{entries: registryEntries("sda"), err: failure}}},
		{"EOF joined with error", []registryReadStep{{entries: registryEntries("sda"), err: errors.Join(io.EOF, failure)}}},
		{"later error", []registryReadStep{{entries: registryEntries("sda")}, {err: failure}}},
		{"empty nil", []registryReadStep{{}}},
		{"later empty nil", []registryReadStep{{entries: registryEntries("sda")}, {}}},
		{"nil entry", []registryReadStep{{entries: []os.DirEntry{nil}, err: io.EOF}}},
		{"response exceeds requested bound", []registryReadStep{{entries: make([]os.DirEntry, 258), err: io.EOF}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := &registryDirectoryFixture{steps: test.steps}
			entries, err := readBlockRegistry(t.Context(), "registry", registryOpener(directory))
			if err != errBlockRegistryRead || entries != nil || directory.closeCalls != 1 || len(directory.requests) != len(test.steps) || directory.unexpectedRead {
				t.Fatalf("malformed observation admitted or retried: entries=%d err=%v reads=%d closes=%d", len(entries), err, len(directory.requests), directory.closeCalls)
			}
		})
	}
}

func TestReadBlockRegistryOwnsObtainedHandlesOnEveryFailure(t *testing.T) {
	failure := errors.New("fixture ownership failure")
	for _, test := range []struct {
		name              string
		handle            bool
		openErr, closeErr error
		step              registryReadStep
		want              error
		wantReads         int
	}{
		{"open error", false, failure, nil, registryReadStep{}, errBlockRegistryRead, 0},
		{"unavailable handle", false, nil, nil, registryReadStep{}, errBlockRegistryRead, 0},
		{"handle and open error", true, failure, nil, registryReadStep{}, errBlockRegistryRead, 0},
		{"handle and both errors", true, failure, failure, registryReadStep{}, errBlockRegistryClose, 0},
		{"EOF then close error", true, nil, failure, registryReadStep{err: io.EOF}, errBlockRegistryClose, 1},
		{"read then close error", true, nil, failure, registryReadStep{err: failure}, errBlockRegistryClose, 1},
		{"overflow then close error", true, nil, failure, registryReadStep{entries: make([]os.DirEntry, 257), err: io.EOF}, errBlockRegistryClose, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := &registryDirectoryFixture{steps: []registryReadStep{test.step}, closeErr: test.closeErr}
			opens := 0
			entries, err := readBlockRegistry(t.Context(), "registry", func(string) (blockRegistryDirectory, error) {
				opens++
				if test.handle {
					return directory, test.openErr
				}
				return nil, test.openErr
			})
			wantCloses := 0
			if test.handle {
				wantCloses = 1
			}
			if err != test.want || entries != nil || opens != 1 || directory.closeCalls != wantCloses || len(directory.requests) != test.wantReads {
				t.Fatalf("directory ownership changed: err=%v opens=%d reads=%d closes=%d", err, opens, len(directory.requests), directory.closeCalls)
			}
		})
	}
}

func TestReadBlockRegistryOpenFailureSurvivesCancellation(t *testing.T) {
	failure := errors.New("fixture open failure")
	for _, test := range []struct {
		name     string
		handle   bool
		closeErr error
		want     error
	}{
		{"no handle", false, nil, errBlockRegistryRead},
		{"obtained handle", true, nil, errBlockRegistryRead},
		{"close failure", true, failure, errBlockRegistryClose},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			directory := &registryDirectoryFixture{closeErr: test.closeErr}
			entries, err := readBlockRegistry(ctx, "registry", func(string) (blockRegistryDirectory, error) {
				cancel()
				if test.handle {
					return directory, failure
				}
				return nil, failure
			})
			wantCloses := 0
			if test.handle {
				wantCloses = 1
			}
			if err != test.want || entries != nil || len(directory.requests) != 0 || directory.closeCalls != wantCloses {
				t.Fatalf("open failure lost precedence or ownership: err=%v reads=%d closes=%d", err, len(directory.requests), directory.closeCalls)
			}
		})
	}
}

type registryBoundaryContext struct {
	context.Context
	ready  func() bool
	cancel context.CancelFunc
	calls  int
	at     int
}

func (ctx *registryBoundaryContext) Err() error {
	if ctx.ready() {
		ctx.calls++
		if ctx.calls == ctx.at {
			ctx.cancel()
		}
	}
	return ctx.Context.Err()
}

func TestReadBlockRegistryHonorsCancellationAndClosesOnce(t *testing.T) {
	for _, phase := range []string{"before open", "inside open", "first batch", "between batches", "final EOF", "inside close"} {
		t.Run(phase, func(t *testing.T) {
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			var ctx context.Context = parent
			directory := &registryDirectoryFixture{steps: []registryReadStep{{entries: registryEntries("sda")}, {err: io.EOF}}}
			if phase == "before open" {
				cancel()
			}
			if phase == "first batch" {
				directory.steps[0].hook = cancel
			}
			if phase == "between batches" {
				ctx = &registryBoundaryContext{Context: parent, ready: func() bool { return len(directory.requests) == 1 }, cancel: cancel, at: 2}
			}
			if phase == "final EOF" {
				directory.steps[1].hook = cancel
			}
			if phase == "inside close" {
				directory.closeHook = cancel
			}
			opens := 0
			entries, err := readBlockRegistry(ctx, "registry", func(string) (blockRegistryDirectory, error) {
				opens++
				if phase == "inside open" {
					cancel()
				}
				return directory, nil
			})
			wantOpens, wantReads := 1, 2
			switch phase {
			case "before open":
				wantOpens, wantReads = 0, 0
			case "inside open":
				wantReads = 0
			case "first batch", "between batches":
				wantReads = 1
			}
			if err != context.Canceled || entries != nil || opens != wantOpens || len(directory.requests) != wantReads || directory.closeCalls != wantOpens {
				t.Fatalf("cancellation retained admission or lost ownership: err=%v opens=%d reads=%d closes=%d", err, opens, len(directory.requests), directory.closeCalls)
			}
		})
	}
}

func TestReadBlockRegistryObservationAndCloseErrorsDominateCancellation(t *testing.T) {
	failure := errors.New("fixture read failure")
	for _, test := range []struct {
		name     string
		step     registryReadStep
		closeErr error
		want     error
	}{
		{"read error", registryReadStep{entries: registryEntries("sda"), err: failure}, nil, errBlockRegistryRead},
		{"joined read cancellation", registryReadStep{err: errors.Join(context.Canceled, failure)}, nil, errBlockRegistryRead},
		{"joined EOF failure", registryReadStep{err: errors.Join(io.EOF, failure)}, nil, errBlockRegistryRead},
		{"overflow", registryReadStep{entries: make([]os.DirEntry, 257), err: io.EOF}, nil, errBlockRegistryLimit},
		{"no progress", registryReadStep{}, nil, errBlockRegistryRead},
		{"nil entry", registryReadStep{entries: []os.DirEntry{nil}, err: io.EOF}, nil, errBlockRegistryRead},
		{"close error", registryReadStep{err: io.EOF}, failure, errBlockRegistryClose},
		{"joined close cancellation", registryReadStep{err: io.EOF}, errors.Join(context.Canceled, failure), errBlockRegistryClose},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			test.step.hook = cancel
			directory := &registryDirectoryFixture{steps: []registryReadStep{test.step}, closeErr: test.closeErr}
			entries, err := readBlockRegistry(ctx, "registry", registryOpener(directory))
			if err != test.want || entries != nil || len(directory.requests) != 1 || directory.closeCalls != 1 {
				t.Fatalf("cancellation masked a known failure: %v, entries=%d closes=%d", err, len(entries), directory.closeCalls)
			}
		})
	}
}

func TestReadBlockRegistryRejectsMissingDependenciesAndExpiredContext(t *testing.T) {
	directory := &registryDirectoryFixture{steps: []registryReadStep{{err: io.EOF}}}
	if _, err := readBlockRegistry(nil, "registry", registryOpener(directory)); err != errBlockRegistryRead {
		t.Fatal("nil context was not rejected")
	}
	if _, err := readBlockRegistry(t.Context(), "registry", nil); err != errBlockRegistryRead {
		t.Fatal("nil opener was not rejected")
	}
	ctx, cancel := context.WithDeadline(t.Context(), time.Unix(0, 0))
	defer cancel()
	opens := 0
	if _, err := readBlockRegistry(ctx, "registry", func(string) (blockRegistryDirectory, error) {
		opens++
		return directory, nil
	}); err != context.DeadlineExceeded || opens != 0 || directory.closeCalls != 0 {
		t.Fatal("expired context acquired a directory")
	}
}

type trackedRegistryDirectory struct {
	blockRegistryDirectory
	requests   []int
	closeCalls int
}

func (directory *trackedRegistryDirectory) ReadDir(count int) ([]os.DirEntry, error) {
	directory.requests = append(directory.requests, count)
	return directory.blockRegistryDirectory.ReadDir(count)
}

func (directory *trackedRegistryDirectory) Close() error {
	directory.closeCalls++
	return directory.blockRegistryDirectory.Close()
}

func TestReadBlockRegistryUsesRealBoundedDirectoryIterator(t *testing.T) {
	for _, count := range []int{0, 255, 256, 257} {
		t.Run(fmt.Sprintf("entries-%d", count), func(t *testing.T) {
			root := t.TempDir()
			for index := count - 1; index >= 0; index-- {
				if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("entry-%03d", index)), 0700); err != nil {
					t.Fatal("cannot create owned directory entry")
				}
			}
			var directory *trackedRegistryDirectory
			entries, err := readBlockRegistry(t.Context(), root, func(path string) (blockRegistryDirectory, error) {
				file, err := os.Open(path)
				if err != nil {
					return nil, err
				}
				directory = &trackedRegistryDirectory{blockRegistryDirectory: file}
				return directory, nil
			})
			if directory == nil || directory.closeCalls != 1 {
				t.Fatal("real directory ownership was not confirmed")
			}
			for _, requested := range directory.requests {
				if requested <= 0 || requested > 257 {
					t.Fatal("real iterator used an unbounded request")
				}
			}
			if count == 257 {
				if err != errBlockRegistryLimit || entries != nil {
					t.Fatal("real oversized directory was admitted")
				}
			} else if err != nil || len(entries) != count {
				t.Fatalf("complete real directory changed: %d, %v", len(entries), err)
			}
		})
	}
}
