package storagesample

import (
	"io"
	"reflect"
)

// This registry spans the entire capture, including already closed temporary
// handles. A provider must return a stable comparable concrete owner identity;
// disguising the same resource in different wrappers is outside that contract.
// The system adapter returns distinct *os.File objects and comparable root
// wrappers. No file descriptor number is used as a durable identity.
const storageMetadataMaximumOwners = 10000

type storageMetadataOwnerSet struct{ seen map[io.Closer]bool }

func (set *storageMetadataOwnerSet) claim(owner io.Closer) (bool, error) {
	if nilStorageMetadata(owner) {
		return false, errStorageMetadataAcquire
	}
	value := reflect.ValueOf(owner)
	// Unsupported identity ends capture. Its newly acquired owner is still
	// returned to the caller for the ordinary checked cleanup ledger.
	comparable := value.Comparable()
	if !comparable || owner != owner {
		return false, errStorageMetadataAcquire
	}
	if set.seen[owner] {
		return true, errStorageMetadataAcquire
	}
	if len(set.seen) >= storageMetadataMaximumOwners {
		return false, errStorageMetadataAcquire
	}
	set.seen[owner] = true
	return false, nil
}

type uniqueStorageMetadataRoot struct {
	storageMetadataRoot
	owners *storageMetadataOwnerSet
}

func (root uniqueStorageMetadataRoot) OpenPath(path string) (storageMetadataPath, error) {
	owner, err := root.storageMetadataRoot.OpenPath(path)
	if !nilStorageMetadata(owner) {
		if duplicate, claimErr := root.owners.claim(owner); claimErr != nil {
			if duplicate {
				return nil, claimErr
			}
			return owner, claimErr
		}
	}
	return owner, err
}
func (root uniqueStorageMetadataRoot) OpenReadable(path storageMetadataPath) (storageMetadataFile, error) {
	owner, err := root.storageMetadataRoot.OpenReadable(path)
	if !nilStorageMetadata(owner) {
		if duplicate, claimErr := root.owners.claim(owner); claimErr != nil {
			if duplicate {
				return nil, claimErr
			}
			return owner, claimErr
		}
	}
	return owner, err
}
func uniqueStorageMetadataOperations(operations storageMetadataOperations) storageMetadataOperations {
	owners := &storageMetadataOwnerSet{seen: map[io.Closer]bool{}}
	return storageMetadataOperations{openRoot: func(name string) (storageMetadataRoot, error) {
		root, err := operations.openRoot(name)
		if nilStorageMetadata(root) {
			return root, err
		}
		if duplicate, claimErr := owners.claim(root); claimErr != nil {
			if duplicate {
				return nil, claimErr
			}
			return root, claimErr
		}
		return uniqueStorageMetadataRoot{root, owners}, err
	}}
}
