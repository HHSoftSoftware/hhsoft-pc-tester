package inventory

import (
	"context"
	"errors"
	"io"
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
func readInventoryScalar(ctx context.Context, name string, open func(string) (inventoryScalar, error)) (data []byte, observed bool, err error) {
	if nilInventoryValue(ctx) {
		return nil, false, ErrInvalidContext
	}
	if open == nil {
		return nil, false, ErrScalarRead
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	file, openErr := open(name)
	if nilInventoryValue(file) {
		if openErr == nil {
			return nil, false, ErrScalarRead
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	defer func() {
		// Every acquired handle is owned, including handle-plus-open-error.
		if file.Close() != nil {
			data, observed, err = nil, false, ErrScalarClose
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
		return nil, false, ErrScalarRead
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	info, statErr := file.Stat()
	if statErr != nil || nilInventoryValue(info) || !info.Mode().IsRegular() {
		return nil, false, ErrScalarRead
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	// Stat.Size is not a sysfs completion or allocation oracle. Retained raw
	// bytes are bounded; the last positive request can include one probe byte.
	data = make([]byte, 0, inventoryScalarLimit)
	var chunk [inventoryScalarChunk]byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		remaining := inventoryScalarLimit - len(data)
		request := min(len(chunk), remaining+1)
		n, readErr := file.Read(chunk[:request])
		if n < 0 || n > request || n == 0 && readErr == nil {
			return nil, false, ErrScalarRead
		}
		if n > remaining {
			return nil, false, ErrScalarLimit
		}
		if readErr != nil && readErr != io.EOF {
			return nil, false, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		data = append(data, chunk[:n]...)
		if readErr == io.EOF {
			return data, true, nil
		}
	}
}

func (collector *linuxCollection) readScalar(name string) ([]byte, bool) {
	if !collector.check() {
		return nil, false
	}
	if nilInventoryValue(collector.Source) {
		collector.err = ErrNoPathSource
		return nil, false
	}
	var data []byte
	var observed bool
	var err error
	if source, exact := collector.Source.(*RootPathSource); exact {
		data, observed, err = source.readScalar(collector.ctx, name)
	} else {
		// A decorator keeps its ReadFile override. An aggregate provider has
		// already allocated before returning; only its result is bounded here.
		var observationErr error
		data, observationErr = collector.Source.ReadFile(name)
		if len(data) > inventoryScalarLimit {
			err = ErrScalarLimit
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
