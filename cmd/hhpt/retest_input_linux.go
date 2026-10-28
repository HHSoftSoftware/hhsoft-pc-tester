//go:build linux

package main

import (
	"fmt"
	"os"
	"syscall"
)

func realRetestInputOperations() retestInputOperations {
	return retestInputOperations{
		openMetadata: func(path string) (retestInputMetadata, error) {
			file, err := os.OpenFile(path, 0x200000|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
			if file == nil {
				return nil, err
			}
			return file, err
		},
		openReadable: func(_ string, metadata retestInputMetadata) (retestInputReadable, error) {
			file, ok := metadata.(*os.File)
			if !ok || file == nil {
				return nil, errRetestInput
			}
			reader, err := os.OpenFile(fmt.Sprintf("/proc/self/fd/%d", file.Fd()), os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
			if reader == nil {
				return nil, err
			}
			return reader, err
		},
		sameFile: os.SameFile,
	}
}
