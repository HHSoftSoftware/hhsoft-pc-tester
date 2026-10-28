//go:build !linux

package safemode

import "os"

// Only private fixture observations use this adapter. Public Observe remains
// unsupported outside Linux; these opens confer no kernel safety qualification.
func (root ownedSafetyRoot) OpenPath(path string) (safetyPath, error) {
	return root.OpenFile(path, os.O_RDONLY, 0)
}

func (root ownedSafetyRoot) OpenReadable(path string, _ safetyPath) (safetyFile, error) {
	return root.OpenFile(path, os.O_RDONLY, 0)
}
