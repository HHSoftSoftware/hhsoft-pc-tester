//go:build !linux

package inventory

import "os"

func openInventoryDirectory(name string) (inventoryDirectory, error) {
	file, err := os.Open(name)
	if file == nil {
		return nil, err
	}
	return file, err
}
