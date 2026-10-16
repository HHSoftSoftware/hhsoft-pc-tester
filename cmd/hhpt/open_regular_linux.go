//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

const openPathFlag = 0x200000

func openRegularNoSymlink(path string, maximum int64) (*os.File, os.FileInfo, error) {
	descriptor, err := syscall.Open(path, openPathFlag|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, errors.New("input must be a bounded direct regular file")
	}
	pinned := os.NewFile(uintptr(descriptor), path)
	if pinned == nil {
		syscall.Close(descriptor)
		return nil, nil, errors.New("cannot pin input file")
	}
	defer pinned.Close()
	before, err := pinned.Stat()
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maximum {
		return nil, nil, errors.New("input must be a bounded direct regular file")
	}
	file, err := os.OpenFile(fmt.Sprintf("/proc/self/fd/%d", descriptor), os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
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
