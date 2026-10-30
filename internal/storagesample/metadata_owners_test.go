package storagesample

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type metadataAliasTestRoot struct {
	storageMetadataRoot
	aliasReadable bool
	reusePath     bool
	first         storageMetadataPath
}

func (r *metadataAliasTestRoot) OpenReadable(path storageMetadataPath) (storageMetadataFile, error) {
	if r.aliasReadable {
		return path.(storageMetadataFile), nil
	}
	return r.storageMetadataRoot.OpenReadable(path)
}
func (r *metadataAliasTestRoot) OpenPath(path string) (storageMetadataPath, error) {
	if r.reusePath && r.first != nil {
		return r.first, nil
	}
	owner, err := r.storageMetadataRoot.OpenPath(path)
	if r.first == nil {
		r.first = owner
	}
	return owner, err
}

type metadataNoncomparableTestRoot struct {
	storageMetadataRoot
	values []int
}
type metadataBoxTestRoot struct{ storageMetadataRoot }

func TestStorageMetadataRejectsOwnerAliases(t *testing.T) {
	for _, which := range []string{"readable_transfer", "closed_path_reacquired", "root_reacquired"} {
		t.Run(which, func(t *testing.T) {
			f := newMetadataTestFixture(t)
			base := f.operations()
			var first storageMetadataRoot
			operations := storageMetadataOperations{openRoot: func(name string) (storageMetadataRoot, error) {
				if which == "root_reacquired" && first != nil {
					return first, nil
				}
				root, err := base.openRoot(name)
				if which == "root_reacquired" {
					first = root
					return root, err
				}
				return &metadataAliasTestRoot{storageMetadataRoot: root, aliasReadable: which == "readable_transfer", reusePath: which == "closed_path_reacquired"}, err
			}}
			got, err := captureLinuxStorageMetadata(context.Background(), operations)
			if err == nil || !reflect.DeepEqual(got, linuxStorageCapture{}) {
				t.Fatal("owner alias admitted")
			}
			f.allClosed()
			if which == "readable_transfer" && (len(f.owners) != 1 || f.calls["readdir:sysfs/class/block"] != 0) {
				t.Fatal("same-owner readable transfer reached consumption or acquired extra owner")
			}
			if which == "closed_path_reacquired" && len(f.owners) != 2 {
				t.Fatal("reacquisition was not rejected after one directory pair")
			}
			if which == "root_reacquired" && (len(f.roots) != 1 || len(f.owners) != 0) {
				t.Fatal("root alias reached metadata acquisition")
			}
		})
	}
}

func TestStorageMetadataUnsupportedOwnerIdentityCloses(t *testing.T) {
	for _, which := range []string{"slice_value", "nested_interface", "close_failure"} {
		t.Run(which, func(t *testing.T) {
			f := newMetadataTestFixture(t)
			base := f.operations()
			if which == "close_failure" {
				f.fail["close-root:sysfs"] = true
			}
			operations := storageMetadataOperations{openRoot: func(name string) (storageMetadataRoot, error) {
				root, err := base.openRoot(name)
				value := metadataNoncomparableTestRoot{root, []int{1}}
				if which == "nested_interface" {
					return metadataBoxTestRoot{value}, err
				}
				return value, err
			}}
			got, err := captureLinuxStorageMetadata(context.Background(), operations)
			if err == nil || !reflect.DeepEqual(got, linuxStorageCapture{}) {
				t.Fatal("unsupported owner identity admitted")
			}
			f.allClosed()
			if len(f.roots) != 1 || len(f.owners) != 0 {
				t.Fatal("unsupported root reached file acquisition")
			}
			if which == "close_failure" && !errors.Is(err, errStorageMetadataClose) {
				t.Fatal("unsupported owner close failure was lost")
			}
		})
	}
}
