//go:build !linux && !windows

package main

// These platforms have no qualified metadata-only, pinned-object adapter.
// Reject before acquisition rather than open a pathname after a racy check.
func realRetestInputOperations() retestInputOperations {
	return retestInputOperations{}
}
