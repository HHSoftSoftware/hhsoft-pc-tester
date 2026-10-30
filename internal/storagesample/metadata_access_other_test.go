//go:build !linux

package storagesample

import "testing"

func TestStorageMetadataUnsupportedSystem(t *testing.T) {
	root, err := systemStorageMetadataOperations().openRoot("sysfs")
	if !nilStorageMetadata(root) || err != errStorageMetadataPlatform {
		t.Fatal("unsupported system metadata fallback")
	}
}
