package inventory

import (
	"context"
	"errors"
	"io"
)

// Private acquisition categories are mapped to the caller's fixed error family.
var (
	errInventoryFileRead  = errors.New("inventory: invalid owned file acquisition")
	errInventoryFileLimit = errors.New("inventory: owned file acquisition budget exceeded")
	errInventoryFileClose = errors.New("inventory: owned file closure unconfirmed")
)

// ErrAcquisitionBudget excludes source paths and provider error text.
var ErrAcquisitionBudget = errors.New("inventory: file acquisition budget exceeded")

const (
	maxInventoryFileAttempts     = 65536
	maxInventoryAcquisitionBytes = 64 << 20
)

// This state belongs to one Collect call. It bounds admitted acquisitions,
// not allocations inside callbacks, parser expansion, peak heap use or time
// spent inside a kernel operation.
type inventoryAcquisitionBudget struct {
	attempts int
	bytes    int
}

func newInventoryAcquisitionBudget() inventoryAcquisitionBudget {
	return inventoryAcquisitionBudget{
		attempts: maxInventoryFileAttempts,
		bytes:    maxInventoryAcquisitionBytes,
	}
}

func (budget *inventoryAcquisitionBudget) valid() bool {
	return budget != nil &&
		budget.attempts >= 0 && budget.attempts <= maxInventoryFileAttempts &&
		budget.bytes >= 0 && budget.bytes <= maxInventoryAcquisitionBytes
}

func (budget *inventoryAcquisitionBudget) chargeAttempt() error {
	if !budget.valid() || budget.attempts == 0 {
		return ErrAcquisitionBudget
	}
	budget.attempts--
	return nil
}

func (budget *inventoryAcquisitionBudget) chargeBytes(n int) error {
	if !budget.valid() || n < 0 {
		return ErrAcquisitionBudget
	}
	if n > budget.bytes {
		// The collection will fail. Never refund a discarded prefix or retain
		// a detection byte; keep the remaining counter finite and exhausted.
		budget.bytes = 0
		return ErrAcquisitionBudget
	}
	budget.bytes -= n
	return nil
}

// The collector has already charged the file attempt before reaching this
// reader. A successful read may use the exact remaining allowance; a failed
// owned read can observe at most one extra byte to detect overflow.
func readInventoryBytesWithBudget(ctx context.Context, name string, limit int, open func(string) (inventoryScalar, error), budget *inventoryAcquisitionBudget) (data []byte, observed bool, err error) {
	if nilInventoryValue(ctx) {
		return nil, false, ErrInvalidContext
	}
	switch limit {
	case inventoryScalarLimit, inventoryMemoryLimit, inventoryCPULimit, inventoryInputLimit:
	default:
		return nil, false, errInventoryFileRead
	}
	if open == nil {
		return nil, false, errInventoryFileRead
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if !budget.valid() {
		return nil, false, ErrAcquisitionBudget
	}
	file, openErr := open(name)
	if nilInventoryValue(file) {
		if openErr == nil {
			return nil, false, errInventoryFileRead
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	defer func() {
		// Every acquired handle is owned, including handle-plus-open-error.
		if file.Close() != nil {
			data, observed, err = nil, false, errInventoryFileClose
			return
		}
		if err != nil {
			data, observed = nil, false
			return
		}
		if stopped := ctx.Err(); stopped != nil {
			data, observed, err = nil, false, stopped
			return
		}
		if !observed {
			data = nil
		}
	}()
	if openErr != nil {
		return nil, false, errInventoryFileRead
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	info, statErr := file.Stat()
	if statErr != nil || nilInventoryValue(info) || !info.Mode().IsRegular() {
		return nil, false, errInventoryFileRead
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	// Stat.Size is not a metadata completion or allocation oracle. Retained raw
	// bytes are bounded; the last positive request can include one probe byte.
	allowance := min(limit, budget.bytes)
	data = make([]byte, 0, min(inventoryScalarChunk, allowance))
	var chunk [inventoryScalarChunk]byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		remaining := limit - len(data)
		request := min(len(chunk), remaining+1, budget.bytes+1)
		n, readErr := file.Read(chunk[:request])
		if n < 0 || n > request || n == 0 && readErr == nil {
			return nil, false, errInventoryFileRead
		}
		// Charge valid raw counts before error discard, cancellation or append.
		budgetErr := budget.chargeBytes(n)
		if n > remaining {
			return nil, false, errInventoryFileLimit
		}
		if budgetErr != nil {
			return nil, false, budgetErr
		}
		if readErr != nil && readErr != io.EOF {
			return nil, false, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		// Explicit growth keeps logical capacity within the selected raw budget.
		// Old and new backing arrays may coexist during the copy; this is not
		// a whole-collection allocation ceiling.
		if len(data)+n > cap(data) {
			capacity := min(allowance, max(len(data)+n, cap(data)*2))
			grown := make([]byte, len(data), capacity)
			copy(grown, data)
			data = grown
		}
		data = append(data, chunk[:n]...)
		if readErr == io.EOF {
			return data, true, nil
		}
	}
}
