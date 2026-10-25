//go:build !linux

package quick

import "os"

func openThermalAttribute(path string) (*os.File, error) {
	return os.Open(path)
}
