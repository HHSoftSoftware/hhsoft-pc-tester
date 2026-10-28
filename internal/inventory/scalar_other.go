//go:build !linux

package inventory

import "os"

func openInventoryScalar(name string) (inventoryScalar, error) {
	file, err := os.Open(name)
	if file == nil {
		return nil, err
	}
	return file, err
}
