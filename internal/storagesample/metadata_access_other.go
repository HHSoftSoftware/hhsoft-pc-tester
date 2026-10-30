//go:build !linux

package storagesample

// Portable ownership tests use injected providers. There is no non-Linux live
// metadata fallback or inferred device topology.
func systemStorageMetadataOperations() storageMetadataOperations {
	return storageMetadataOperations{openRoot: func(string) (storageMetadataRoot, error) {
		return nil, errStorageMetadataPlatform
	}}
}
