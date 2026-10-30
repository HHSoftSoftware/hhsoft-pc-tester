//go:build linux

package storagesample

import (
	"os"
	"strconv"
	"syscall"
)

const storageMetadataOpenPath = 0x200000
const storageMetadataSysfsMagic int64 = 0x62656572
const storageMetadataProcMagic int64 = 0x9fa0

type systemStorageMetadataRoot struct {
	*os.Root
	filesystem int64
}

func systemStorageMetadataOperations() storageMetadataOperations {
	return storageMetadataOperations{openRoot: func(kind string) (storageMetadataRoot, error) {
		var path string
		var filesystem int64
		switch kind {
		case "sysfs":
			path, filesystem = "/sys", storageMetadataSysfsMagic
		case "proc":
			path, filesystem = "/proc", storageMetadataProcMagic
		default:
			return nil, errStorageMetadataAcquire
		}
		root, err := os.OpenRoot(path)
		if root == nil {
			return nil, err
		}
		return systemStorageMetadataRoot{root, filesystem}, err
	}}
}

func (root systemStorageMetadataRoot) OpenPath(path string) (storageMetadataPath, error) {
	file, err := root.OpenFile(path, storageMetadataOpenPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if file == nil || err != nil {
		return file, err
	}
	var facts syscall.Statfs_t
	if syscall.Fstatfs(int(file.Fd()), &facts) != nil || int64(facts.Type) != root.filesystem {
		// Return the acquired owner even on rejection; the caller closes it.
		return file, errStorageMetadataAcquire
	}
	return file, nil
}

func (root systemStorageMetadataRoot) OpenReadable(handle storageMetadataPath) (storageMetadataFile, error) {
	return os.OpenFile("/proc/self/fd/"+strconv.FormatUint(uint64(handle.Fd()), 10),
		os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
}
