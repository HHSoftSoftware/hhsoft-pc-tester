package inventory

import (
	"context"
	"errors"
	"io/fs"
)

// These fixed categories contain neither provider errors nor metadata paths.
var (
	ErrScalarRead  = errors.New("inventory: scalar acquisition failed")
	ErrScalarLimit = errors.New("inventory: scalar exceeds acquisition budget")
	ErrScalarClose = errors.New("inventory: scalar closure could not be confirmed")
)

// This is a product acquisition budget, not a kernel ABI or instance limit.
const inventoryScalarLimit = 65536
const inventoryScalarChunk = 4096

type inventoryScalar interface {
	Stat() (fs.FileInfo, error)
	Read([]byte) (int, error)
	Close() error
}

// observed distinguishes a completed empty value from unavailable metadata.
// Only exact EOF proves completion. Ordinary open/read observation errors
// discard all bytes; malformed acquisition and cleanup abort the collection.
func readInventoryScalarWithBudget(ctx context.Context, name string, open func(string) (inventoryScalar, error), budget *inventoryAcquisitionBudget) ([]byte, bool, error) {
	data, observed, err := readInventoryBytesWithBudget(ctx, name, inventoryScalarLimit, open, budget)
	switch err {
	case errInventoryFileRead:
		err = ErrScalarRead
	case errInventoryFileLimit:
		err = ErrScalarLimit
	case errInventoryFileClose:
		err = ErrScalarClose
	}
	return data, observed, err
}

func (collector *linuxCollection) readScalar(name string) ([]byte, bool) {
	if !collector.check() {
		return nil, false
	}
	if nilInventoryValue(collector.Source) {
		collector.err = ErrNoPathSource
		return nil, false
	}
	if err := collector.acquisitionBudget.chargeAttempt(); err != nil {
		collector.err = err
		return nil, false
	}
	var data []byte
	var observed bool
	var err error
	if source, exact := collector.Source.(*RootPathSource); exact {
		data, observed, err = source.readScalar(collector.ctx, name, &collector.acquisitionBudget)
	} else {
		// A decorator keeps its ReadFile override. An aggregate provider has
		// already allocated before returning; only its result is bounded here.
		var observationErr error
		data, observationErr = collector.Source.ReadFile(name)
		budgetErr := collector.acquisitionBudget.chargeBytes(len(data))
		if len(data) > inventoryScalarLimit {
			err = ErrScalarLimit
		} else if budgetErr != nil {
			err = budgetErr
		} else {
			observed = observationErr == nil
		}
	}
	if err != nil {
		collector.err = err
		return nil, false
	}
	if !collector.check() || !observed {
		return nil, false
	}
	return data, true
}
