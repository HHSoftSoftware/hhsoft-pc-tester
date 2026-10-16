//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func openSaveAuditConsole() (*os.File, error) {
	return openPinnedSaveConsole("/dev/console", os.OpenFile)
}

// Pin and inspect without opening the target device for I/O. Only the Linux
// console character device (major 5, minor 1) may be reopened for audit writes.
func openPinnedSaveConsole(path string, open func(string, int, os.FileMode) (*os.File, error)) (*os.File, error) {
	descriptor, err := syscall.Open(path, openPathFlag|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("cannot pin report-save console audit output")
	}
	pinned := os.NewFile(uintptr(descriptor), "report-save-console")
	if pinned == nil {
		syscall.Close(descriptor)
		return nil, errors.New("cannot pin report-save console audit output")
	}
	defer pinned.Close()
	before, err := pinned.Stat()
	if err != nil || !isSaveConsole(before) {
		return nil, errors.New("report-save audit output is not the direct console character device")
	}
	file, err := open(fmt.Sprintf("/proc/self/fd/%d", descriptor), os.O_WRONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("cannot open the pinned report-save console")
	}
	after, err := file.Stat()
	if err != nil || !isSaveConsole(after) || !os.SameFile(before, after) {
		file.Close()
		return nil, errors.New("report-save console identity changed while opening")
	}
	return file, nil
}

func isSaveConsole(metadata os.FileInfo) bool {
	device, ok := metadata.Sys().(*syscall.Stat_t)
	return ok && metadata.Mode()&os.ModeType == os.ModeDevice|os.ModeCharDevice && uint64(device.Rdev) == 5<<8|1
}
