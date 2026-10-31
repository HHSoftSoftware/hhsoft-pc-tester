package inventory

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"reflect"
	"sort"
	"strings"
	"syscall"
)

// These categories deliberately exclude caller paths and operating-system text.
var (
	ErrDirectoryRead   = errors.New("inventory: directory enumeration failed")
	ErrDirectoryClose  = errors.New("inventory: directory closure could not be confirmed")
	ErrDirectoryEntry  = errors.New("inventory: invalid directory entry")
	ErrDirectoryBudget = errors.New("inventory: directory acquisition budget exceeded")
	ErrInvalidContext  = errors.New("inventory: collection context is missing")
)

const directoryReadBatch = 64 // A request size, never a total device-count limit.

// These are aggregate acquisition limits, not truncation thresholds. A
// collection that exceeds either limit fails without a partial snapshot.
const (
	maxInventoryDirectoryEntries   = 65536
	maxInventoryDirectoryNameBytes = 8 << 20
)

// The budget belongs to one collection. It bounds collector-owned directory
// records and admitted name bytes, not allocations inside callbacks or opaque
// DirEntry values, scalar/proc data, or a blocked kernel operation.
type inventoryDirectoryBudget struct {
	entries   int
	nameBytes int
}

func newInventoryDirectoryBudget() inventoryDirectoryBudget {
	return inventoryDirectoryBudget{
		entries:   maxInventoryDirectoryEntries,
		nameBytes: maxInventoryDirectoryNameBytes,
	}
}

func (budget *inventoryDirectoryBudget) valid() bool {
	return budget != nil &&
		budget.entries >= 0 && budget.entries <= maxInventoryDirectoryEntries &&
		budget.nameBytes >= 0 && budget.nameBytes <= maxInventoryDirectoryNameBytes
}

type inventoryDirectory interface {
	Stat() (fs.FileInfo, error)
	ReadDir(int) ([]fs.DirEntry, error)
	Close() error
}

// Only a sole, known missing-path cause is optional. A bounded unwrap walk
// rejects cycles, excessive wrapping, all multi-cause errors (even one child),
// Is-only impostors and read/close PathErrors. Error messages are never used.
func soleOpenAbsence(err error) bool {
	for depth := 0; depth < 32 && err != nil; depth++ {
		if nilInventoryValue(err) {
			return false
		}
		if _, multiple := err.(interface{ Unwrap() []error }); multiple {
			return false
		}
		if pathError, ok := err.(*fs.PathError); ok {
			if pathError == nil || pathError.Op != "open" {
				return false
			}
			err = pathError.Err
			continue
		}
		if err == fs.ErrNotExist {
			return true
		}
		if number, ok := err.(syscall.Errno); ok {
			return os.IsNotExist(number)
		}
		wrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = wrapped.Unwrap()
	}
	return false
}

type namedInventoryEntry struct {
	fs.DirEntry
	name string
}

func (entry namedInventoryEntry) Name() string { return entry.name }

func nilInventoryValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func ownDirectoryEntries(ctx context.Context, provided []fs.DirEntry, budget *inventoryDirectoryBudget) ([]fs.DirEntry, error) {
	if ctx == nil {
		return nil, ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !budget.valid() || len(provided) > budget.entries {
		return nil, ErrDirectoryBudget
	}
	// Charge before copying. A legacy provider may already have allocated its
	// aggregate result; that allocation is outside the collector's control.
	budget.entries -= len(provided)
	// Providers may retain or concurrently reuse their immutable backing slice.
	var entries []fs.DirEntry
	if len(provided) != 0 {
		entries = make([]fs.DirEntry, len(provided))
		copy(entries, provided)
	}
	seen := make(map[string]struct{}, len(entries))
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if nilInventoryValue(entry) {
			return nil, ErrDirectoryEntry
		}
		name := entry.Name()
		// Check the byte budget before scanning or retaining the name. Name()
		// itself remains an external callback and has no allocation deadline.
		if len(name) > budget.nameBytes {
			return nil, ErrDirectoryBudget
		}
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
			return nil, ErrDirectoryEntry
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, ErrDirectoryEntry
		}
		budget.nameBytes -= len(name)
		seen[name] = struct{}{}
		// Cache the validated name, so later path construction uses this value.
		entries[index] = namedInventoryEntry{DirEntry: entry, name: name}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// A clean open-time absence retains the PathSource sentinel. All acquired
// handles are closed before data or absence can reach the collection policy.
func readInventoryDirectory(ctx context.Context, name string, open func(string) (inventoryDirectory, error)) ([]fs.DirEntry, error) {
	budget := newInventoryDirectoryBudget()
	return readInventoryDirectoryWithBudget(ctx, name, open, &budget)
}

func readInventoryDirectoryWithBudget(ctx context.Context, name string, open func(string) (inventoryDirectory, error), budget *inventoryDirectoryBudget) (entries []fs.DirEntry, err error) {
	if ctx == nil || open == nil {
		return nil, ErrDirectoryRead
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !budget.valid() {
		return nil, ErrDirectoryBudget
	}
	directory, openErr := open(name)
	if nilInventoryValue(directory) {
		if soleOpenAbsence(openErr) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, fs.ErrNotExist
		}
		return nil, ErrDirectoryRead
	}
	defer func() {
		if directory.Close() != nil {
			entries, err = nil, ErrDirectoryClose
			return
		}
		if err != nil {
			entries = nil
			return
		}
		if stopped := ctx.Err(); stopped != nil {
			entries, err = nil, stopped
			return
		}
		entries, err = ownDirectoryEntries(ctx, entries, budget)
	}()
	if openErr != nil {
		return nil, ErrDirectoryRead
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, statErr := directory.Stat()
	if statErr != nil || nilInventoryValue(info) || !info.IsDir() {
		return nil, ErrDirectoryRead
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := budget.entries - len(entries)
		// One extra record distinguishes an exact-limit EOF from overflow.
		request := min(directoryReadBatch, remaining+1)
		batch, readErr := directory.ReadDir(request)
		if len(batch) > request || readErr != nil && readErr != io.EOF {
			return nil, ErrDirectoryRead
		}
		if len(batch) == 0 && readErr == nil {
			return nil, ErrDirectoryRead
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(batch) > remaining {
			return nil, ErrDirectoryBudget
		}
		// Bound backing-slice capacity as well as length. Old and new slices
		// may coexist during growth; this is not a whole-process heap ceiling.
		required := len(entries) + len(batch)
		if required > cap(entries) {
			capacity := min(budget.entries, max(required, 2*cap(entries)))
			grown := make([]fs.DirEntry, len(entries), capacity)
			copy(grown, entries)
			entries = grown
		}
		// append copies provider-owned batches; no sort or write touches them.
		entries = append(entries, batch...)
		if readErr == io.EOF {
			return entries, nil
		}
	}
}
