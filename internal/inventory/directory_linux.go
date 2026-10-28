//go:build linux

package inventory

import (
	"os"
	"syscall"
)

func openInventoryDirectory(name string) (inventoryDirectory, error) {
	file, err := os.OpenFile(name, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if file == nil {
		return nil, err
	}
	return file, err
}
