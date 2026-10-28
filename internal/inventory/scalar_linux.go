//go:build linux

package inventory

import (
	"os"
	"syscall"
)

func openInventoryScalar(name string) (inventoryScalar, error) {
	file, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if file == nil {
		return nil, err
	}
	return file, err
}
