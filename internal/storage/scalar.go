package storage

import (
	"context"
	"errors"
	"io"
	"os"
)

const maximumBlockScalarBytes = 256

var (
	errBlockScalarRead      = errors.New("cannot read block device metadata")
	errBlockScalarClose     = errors.New("block device metadata closure could not be confirmed")
	errBlockScalarCancelled = errors.New("cancelled")
)

type blockScalarFile interface {
	Read([]byte) (int, error)
	Stat() (os.FileInfo, error)
	Close() error
}

func readBlockScalar(ctx context.Context, path string, open func(string) (blockScalarFile, error)) (data []byte, err error) {
	if ctx == nil || open == nil {
		return nil, errBlockScalarRead
	}
	if ctx.Err() != nil {
		return nil, errBlockScalarCancelled
	}
	file, openErr := open(path)
	if file == nil {
		return nil, errBlockScalarRead
	}
	defer func() {
		if file.Close() != nil {
			data, err = nil, errBlockScalarClose
		}
	}()
	if openErr != nil {
		return nil, errBlockScalarRead
	}
	if ctx.Err() != nil {
		return nil, errBlockScalarCancelled
	}
	info, statErr := file.Stat()
	if statErr != nil || info == nil || !info.Mode().IsRegular() {
		return nil, errBlockScalarRead
	}
	var buffer [maximumBlockScalarBytes + 1]byte
	used := 0
	for {
		if ctx.Err() != nil {
			return nil, errBlockScalarCancelled
		}
		remaining := buffer[used:]
		n, readErr := file.Read(remaining)
		if n < 0 || n > len(remaining) || readErr != nil && readErr != io.EOF {
			return nil, errBlockScalarRead
		}
		used += n
		if used > maximumBlockScalarBytes || n == 0 && readErr == nil {
			return nil, errBlockScalarRead
		}
		if readErr == io.EOF {
			// A complete value must reach its consumer before cancellation is
			// decided: invalid required capacity still takes error precedence.
			// The deferred close must succeed before these bytes are usable.
			return buffer[:used], nil
		}
	}
}
