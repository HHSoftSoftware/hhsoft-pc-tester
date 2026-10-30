package main

import (
	"errors"
	"io"
	"math"
	"os"
)

type smallRegularReader func(string, int64) ([]byte, error)
type smallRegularOpener func(string, int64) (retestInputReadable, os.FileInfo, error)

var (
	errSmallRegularInput   = errors.New("input must be a bounded direct regular file")
	errSmallRegularRead    = errors.New("file changed while reading")
	errSmallRegularCleanup = errors.New("input cleanup failed")
)

func readSmallRegularFile(path string, maximum int64) ([]byte, error) {
	return readSmallRegularFileWith(path, maximum, openSmallRegularInput)
}

// A returned byte slice belongs to the caller only after the acquired reader
// has closed successfully. Read and cleanup failures retain separate safe causes.
func readSmallRegularFileWith(path string, maximum int64, open smallRegularOpener) (data []byte, failure error) {
	if maximum < 1 || maximum == math.MaxInt64 || open == nil {
		return nil, errSmallRegularInput
	}
	file, metadata, err := open(path, maximum)
	defer func() {
		if !nilRetestInputValue(file) && file.Close() != nil {
			failure = errors.Join(failure, errSmallRegularCleanup)
		}
		if failure != nil {
			data = nil
		}
	}()
	if err != nil {
		return nil, safeSmallRegularInputError(err)
	}
	if nilRetestInputValue(file) || !smallRegularMetadata(metadata, maximum) {
		return nil, errSmallRegularInput
	}
	data, err = io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(data)) != metadata.Size() {
		return nil, errSmallRegularRead
	}
	return data, nil
}

func safeSmallRegularInputError(err error) error {
	if errors.Is(err, errSmallRegularCleanup) {
		return errors.Join(errSmallRegularInput, errSmallRegularCleanup)
	}
	return errSmallRegularInput
}

func smallRegularMetadata(metadata os.FileInfo, maximum int64) bool {
	return !nilRetestInputValue(metadata) && metadata.Mode().IsRegular() && metadata.Size() > 0 && metadata.Size() <= maximum
}

// The Linux adapter reuses the existing metadata-only/pinned-object operations.
// Its metadata owner closes before the readable owner can be transferred. Any
// failure closes both acquired owners once and transfers neither.
func openSmallRegularInputWithOperations(path string, maximum int64, ops retestInputOperations) (file retestInputReadable, info os.FileInfo, failure error) {
	if maximum < 1 || maximum == math.MaxInt64 || ops.openMetadata == nil || ops.openReadable == nil || ops.sameFile == nil {
		return nil, nil, errSmallRegularInput
	}
	var metadata retestInputMetadata
	var readable retestInputReadable
	defer func() {
		if !nilRetestInputValue(metadata) && metadata.Close() != nil {
			failure = errors.Join(failure, errSmallRegularCleanup)
		}
		if failure != nil {
			if !nilRetestInputValue(readable) && readable.Close() != nil {
				failure = errors.Join(failure, errSmallRegularCleanup)
			}
			file, info = nil, nil
		}
	}()
	var err error
	metadata, err = ops.openMetadata(path)
	if err != nil || nilRetestInputValue(metadata) {
		return nil, nil, safeSmallRegularInputError(err)
	}
	before, err := metadata.Stat()
	if err != nil || !smallRegularMetadata(before, maximum) {
		return nil, nil, errSmallRegularInput
	}
	readable, err = ops.openReadable(path, metadata)
	if !nilRetestInputValue(readable) && sameRetestInputOwner(metadata, readable) {
		readable = nil
		return nil, nil, errSmallRegularInput
	}
	if err != nil || nilRetestInputValue(readable) {
		return nil, nil, safeSmallRegularInputError(err)
	}
	after, err := readable.Stat()
	if err != nil || !smallRegularMetadata(after, maximum) || !ops.sameFile(before, after) || before.Size() != after.Size() {
		return nil, nil, errSmallRegularInput
	}
	return readable, after, nil
}

type smallRegularLegacyOperations struct {
	lstat    func(string) (os.FileInfo, error)
	open     func(string) (retestInputReadable, error)
	sameFile func(os.FileInfo, os.FileInfo) bool
}

// Preserve the existing non-Linux lstat/open/identity policy. This does not
// claim the stronger Linux pinned-object acquisition guarantee on other OSes.
func openSmallRegularInputLegacy(path string, maximum int64, ops smallRegularLegacyOperations) (file retestInputReadable, info os.FileInfo, failure error) {
	if maximum < 1 || maximum == math.MaxInt64 || ops.lstat == nil || ops.open == nil || ops.sameFile == nil {
		return nil, nil, errSmallRegularInput
	}
	before, err := ops.lstat(path)
	if err != nil || !smallRegularMetadata(before, maximum) {
		return nil, nil, errSmallRegularInput
	}
	var readable retestInputReadable
	defer func() {
		if failure != nil {
			if !nilRetestInputValue(readable) && readable.Close() != nil {
				failure = errors.Join(failure, errSmallRegularCleanup)
			}
			file, info = nil, nil
		}
	}()
	readable, err = ops.open(path)
	if err != nil || nilRetestInputValue(readable) {
		return nil, nil, safeSmallRegularInputError(err)
	}
	after, err := readable.Stat()
	if err != nil || !smallRegularMetadata(after, maximum) || !ops.sameFile(before, after) || before.Size() != after.Size() {
		return nil, nil, errSmallRegularInput
	}
	return readable, after, nil
}
