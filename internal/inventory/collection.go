package inventory

import (
	"context"
	"io/fs"
	"iter"
)

// Configuration is copied; context, failure and acquisition budgets are never
// stored on the shared collector. Concurrent use still requires a source safe
// for concurrent reads.
type linuxCollection struct {
	LinuxCollector
	ctx               context.Context
	err               error
	directoryBudget   inventoryDirectoryBudget
	acquisitionBudget inventoryAcquisitionBudget
}

// The yielded callback is the production range-loop body. Recheck after the
// body even when it breaks, so cancellation on the last processed item cannot
// become a successful early exit. Values remain in their original order.
func collectionItems[T any](collector *linuxCollection, values []T) iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for index, value := range values {
			if !collector.check() {
				return
			}
			keepGoing := yield(index, value)
			if !collector.check() || !keepGoing {
				return
			}
		}
	}
}

func (collector *LinuxCollector) newCollection(ctx context.Context) *linuxCollection {
	return &linuxCollection{
		LinuxCollector:    *collector,
		ctx:               ctx,
		directoryBudget:   newInventoryDirectoryBudget(),
		acquisitionBudget: newInventoryAcquisitionBudget(),
	}
}

func (collector *linuxCollection) check() bool {
	if collector.err != nil {
		return false
	}
	if nilInventoryValue(collector.ctx) {
		collector.err = ErrInvalidContext
		return false
	}
	collector.err = collector.ctx.Err()
	return collector.err == nil
}

func (collector *linuxCollection) readDir(name string) []fs.DirEntry {
	if !collector.check() {
		return nil
	}
	var entries []fs.DirEntry
	var err error
	if source, ok := collector.Source.(*RootPathSource); ok {
		entries, err = source.readDirectoryWithBudget(collector.ctx, name, &collector.directoryBudget)
		if err == fs.ErrNotExist && len(entries) == 0 {
			err = nil
		}
	} else {
		// Legacy PathSource provides one aggregate result. It can cancel only
		// at these call boundaries, not interrupt a blocked source operation.
		// Use its declared ReadDir even if a decorator embeds RootPathSource.
		entries, err = collector.Source.ReadDir(name)
		if err != nil {
			if len(entries) == 0 && soleOpenAbsence(err) {
				err = nil
			} else {
				err = ErrDirectoryRead
			}
		}
		if err == nil {
			entries, err = ownDirectoryEntries(collector.ctx, entries, &collector.directoryBudget)
		}
	}
	if err != nil {
		collector.err = err
		return nil
	}
	if !collector.check() {
		return nil
	}
	return entries
}

func (collector *linuxCollection) snapshot(components []Component, canonicalize func([]Component) Snapshot) (Snapshot, error) {
	if !collector.check() {
		return Snapshot{}, collector.err
	}
	snapshot := canonicalize(components)
	if !collector.check() {
		return Snapshot{}, collector.err
	}
	return snapshot, nil
}
