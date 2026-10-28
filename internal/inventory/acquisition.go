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

func readInventoryBytes(ctx context.Context, name string, limit int, open func(string) (inventoryScalar, error)) (data []byte, observed bool, err error) {
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
	data = make([]byte, 0, min(inventoryScalarChunk, limit))
	var chunk [inventoryScalarChunk]byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		remaining := limit - len(data)
		request := min(len(chunk), remaining+1)
		n, readErr := file.Read(chunk[:request])
		if n < 0 || n > request || n == 0 && readErr == nil {
			return nil, false, errInventoryFileRead
		}
		if n > remaining {
			return nil, false, errInventoryFileLimit
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
			capacity := min(limit, max(len(data)+n, cap(data)*2))
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
