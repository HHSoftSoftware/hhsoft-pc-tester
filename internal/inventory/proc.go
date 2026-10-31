package inventory

import (
	"context"
	"errors"
)

// Product raw-byte budgets, never a kernel ABI or device-count limit.
const (
	inventoryMemoryLimit = 262144
	inventoryCPULimit    = 16777216
	inventoryInputLimit  = 8388608
)

var (
	ErrBulkRead  = errors.New("inventory: primary observation acquisition failed")
	ErrBulkLimit = errors.New("inventory: primary observation exceeds acquisition budget")
	ErrBulkClose = errors.New("inventory: primary observation closure could not be confirmed")
)

type inventoryProcRole uint8

const (
	procMemory inventoryProcRole = iota + 1
	procCPU
	procInput
)

func inventoryProcLimit(role inventoryProcRole) (int, bool) {
	switch role {
	case procMemory:
		return inventoryMemoryLimit, true
	case procCPU:
		return inventoryCPULimit, true
	case procInput:
		return inventoryInputLimit, true
	default:
		return 0, false
	}
}

func readInventoryProcWithBudget(ctx context.Context, name string, role inventoryProcRole, open func(string) (inventoryScalar, error), budget *inventoryAcquisitionBudget) ([]byte, bool, error) {
	limit, valid := inventoryProcLimit(role)
	if !valid {
		return nil, false, ErrBulkRead
	}
	data, observed, err := readInventoryBytesWithBudget(ctx, name, limit, open, budget)
	switch err {
	case errInventoryFileRead:
		err = ErrBulkRead
	case errInventoryFileLimit:
		err = ErrBulkLimit
	case errInventoryFileClose:
		err = ErrBulkClose
	}
	return data, observed, err
}

func (collector *linuxCollection) readProc(name string, role inventoryProcRole) ([]byte, bool) {
	if !collector.check() {
		return nil, false
	}
	if nilInventoryValue(collector.Source) {
		collector.err = ErrNoPathSource
		return nil, false
	}
	limit, valid := inventoryProcLimit(role)
	if !valid {
		collector.err = ErrBulkRead
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
		data, observed, err = source.readProc(collector.ctx, name, role, &collector.acquisitionBudget)
	} else {
		// Preserve the declared override. This aggregate provider may have
		// allocated before returning; only admission of its result is bounded.
		var observationErr error
		data, observationErr = collector.Source.ReadFile(name)
		budgetErr := collector.acquisitionBudget.chargeBytes(len(data))
		if len(data) > limit {
			err = ErrBulkLimit
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
