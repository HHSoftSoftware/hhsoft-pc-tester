package main

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

type retestInputKind uint8

const (
	retestArtifactInput retestInputKind = iota + 1
	retestPublicKeyInput
	retestFingerprintKeyInput
	retestSigningKeyInput
	retestLaunchSelectionInput
)

var (
	errRetestInput        = errors.New("report input acquisition failed")
	errRetestInputCleanup = errors.New("report input cleanup failed")
)

type retestInputMetadata interface {
	Stat() (os.FileInfo, error)
	Close() error
}

type retestInputReadable interface {
	retestInputMetadata
	Read([]byte) (int, error)
}

type retestInputOperations struct {
	openMetadata func(string) (retestInputMetadata, error)
	openReadable func(string, retestInputMetadata) (retestInputReadable, error)
	sameFile     func(os.FileInfo, os.FileInfo) bool
}

type retestInputStamp struct {
	info     os.FileInfo
	mode     os.FileMode
	size     int64
	modified time.Time
}

func readRetestInput(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
	return readRetestInputWithOperations(ctx, path, kind, realRetestInputOperations())
}

// The same bounded body serves real acquisition and injected fault tests. It
// releases bytes only after readable and metadata ownership has ended.
func readRetestInputWithOperations(ctx context.Context, path string, kind retestInputKind, ops retestInputOperations) (data []byte, failure error) {
	minimum, maximum := int64(22), int64(64<<20)
	switch kind {
	case retestArtifactInput:
	case retestPublicKeyInput, retestFingerprintKeyInput:
		minimum, maximum = 32, 32
	case retestSigningKeyInput:
		minimum, maximum = 64, 64
	case retestLaunchSelectionInput:
		minimum, maximum = 1, plan.MaximumLaunchSelectionBytes
	default:
		return nil, errRetestInput
	}
	if nilRetestInputValue(ctx) || ops.openMetadata == nil || ops.openReadable == nil || ops.sameFile == nil {
		return nil, errRetestInput
	}
	if err := ctx.Err(); err != nil {
		return nil, safeRetestInputError(ctx, err)
	}
	var metadata retestInputMetadata
	var readable retestInputReadable
	defer func() {
		for _, owner := range []retestInputMetadata{readable, metadata} {
			if !nilRetestInputValue(owner) && owner.Close() != nil {
				failure = errors.Join(failure, errRetestInput, errRetestInputCleanup)
			}
		}
		if failure == nil && ctx.Err() != nil {
			failure = safeRetestInputError(ctx, ctx.Err())
		}
		if failure != nil {
			data = nil
		}
	}()

	var err error
	metadata, err = ops.openMetadata(path)
	if nilRetestInputValue(metadata) {
		if metadata != nil || err == nil {
			return nil, errRetestInput
		}
		return nil, safeRetestInputError(ctx, err)
	}
	if err != nil {
		return nil, safeRetestInputError(ctx, err)
	}
	if err = ctx.Err(); err != nil {
		return nil, safeRetestInputError(ctx, err)
	}
	before, err := stampRetestInput(metadata, minimum, maximum)
	if err != nil {
		return nil, safeRetestInputError(ctx, err)
	}
	if err = ctx.Err(); err != nil {
		return nil, safeRetestInputError(ctx, err)
	}
	readable, err = ops.openReadable(path, metadata)
	if nilRetestInputValue(readable) {
		if readable != nil || err == nil {
			return nil, errRetestInput
		}
		return nil, safeRetestInputError(ctx, err)
	}
	if sameRetestInputOwner(metadata, readable) {
		// A malformed provider has returned the same owner twice. Close it once.
		readable = nil
		return nil, errRetestInput
	}
	if err != nil {
		return nil, safeRetestInputError(ctx, err)
	}
	if err = ctx.Err(); err != nil {
		return nil, safeRetestInputError(ctx, err)
	}
	opened, err := stampRetestInput(readable, minimum, maximum)
	if err != nil {
		return nil, safeRetestInputError(ctx, err)
	}
	if !sameRetestInputStamp(before, opened, ops.sameFile) {
		return nil, errRetestInput
	}
	data, err = readRetestInputBytes(ctx, readable, before.size)
	if err != nil {
		return nil, err
	}
	for _, owner := range []retestInputMetadata{readable, metadata} {
		if err = ctx.Err(); err != nil {
			return nil, safeRetestInputError(ctx, err)
		}
		after, statErr := stampRetestInput(owner, minimum, maximum)
		if statErr != nil {
			return nil, safeRetestInputError(ctx, statErr)
		}
		if !sameRetestInputStamp(before, after, ops.sameFile) {
			return nil, errRetestInput
		}
	}
	return data, nil
}

func stampRetestInput(owner retestInputMetadata, minimum, maximum int64) (retestInputStamp, error) {
	info, err := owner.Stat()
	if nilRetestInputValue(info) {
		if info != nil || err == nil {
			return retestInputStamp{}, errRetestInput
		}
		return retestInputStamp{}, err
	}
	if err != nil {
		return retestInputStamp{}, errRetestInput
	}
	stamp := retestInputStamp{info: info, mode: info.Mode(), size: info.Size(), modified: info.ModTime()}
	if !stamp.mode.IsRegular() || stamp.size < minimum || stamp.size > maximum {
		return retestInputStamp{}, errRetestInput
	}
	return stamp, nil
}

func sameRetestInputStamp(before, after retestInputStamp, sameFile func(os.FileInfo, os.FileInfo) bool) bool {
	return before.mode == after.mode && before.size == after.size && before.modified.Equal(after.modified) && sameFile(before.info, after.info)
}

func readRetestInputBytes(ctx context.Context, file retestInputReadable, size int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, safeRetestInputError(ctx, err)
	}
	data := make([]byte, int(size))
	offset := 0
	for offset < len(data) {
		if err := ctx.Err(); err != nil {
			return nil, safeRetestInputError(ctx, err)
		}
		end := offset + (64 << 10)
		if end > len(data) {
			end = len(data)
		}
		n, err := file.Read(data[offset:end])
		if n < 0 || n > end-offset {
			return nil, errRetestInput
		}
		offset += n
		if err == io.EOF {
			if offset != len(data) {
				return nil, errRetestInput
			}
			break
		}
		if err != nil {
			if n != 0 {
				return nil, errRetestInput
			}
			return nil, safeRetestInputError(ctx, err)
		}
		if n == 0 {
			return nil, errRetestInput
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, safeRetestInputError(ctx, err)
	}
	var probe [1]byte
	n, err := file.Read(probe[:])
	if n != 0 {
		return nil, errRetestInput
	}
	if err != io.EOF {
		return nil, safeRetestInputError(ctx, err)
	}
	return data, nil
}

func safeRetestInputError(ctx context.Context, err error) error {
	if errors.Is(err, errRetestInputCleanup) {
		return errors.Join(errRetestInput, errRetestInputCleanup)
	}
	if (err == context.Canceled || err == context.DeadlineExceeded) && ctx.Err() == err {
		return err
	}
	return errRetestInput
}

func nilRetestInputValue(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func sameRetestInputOwner(a, b any) bool {
	x, y := reflect.ValueOf(a), reflect.ValueOf(b)
	return x.Type() == y.Type() && x.Comparable() && x.Interface() == y.Interface()
}
