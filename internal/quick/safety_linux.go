package quick

import (
	"os"
	"syscall"
)

func openThermalAttribute(path string) (*os.File, error) {
	// A changed fixture or device path must not block opening a FIFO.
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
}
