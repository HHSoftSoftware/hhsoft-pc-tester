//go:build !linux

package main

import (
	"errors"
	"os"
)

func openRegularNoSymlink(path string, maximum int64) (*os.File, os.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maximum {
		return nil, nil, errors.New("input must be a bounded direct regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() {
		file.Close()
		return nil, nil, errors.New("input changed while opening")
	}
	return file, after, nil
}
