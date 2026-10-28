//go:build linux

package safemode

import (
	"os"
	"strconv"
	"syscall"
)

const safetyOpenPath = 0x200000

func (root ownedSafetyRoot) OpenPath(path string) (safetyPath, error) {
	return root.OpenFile(path, safetyOpenPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
}

func (root ownedSafetyRoot) OpenReadable(_ string, handle safetyPath) (safetyFile, error) {
	return os.OpenFile("/proc/self/fd/"+strconv.FormatUint(uint64(handle.Fd()), 10),
		os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
}
