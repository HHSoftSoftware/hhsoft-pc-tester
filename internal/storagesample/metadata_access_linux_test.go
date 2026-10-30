//go:build linux

package storagesample

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func metadataLinuxFixtureRoot(t *testing.T) (systemStorageMetadataRoot, string) {
	t.Helper()
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal("owned Linux metadata root failed")
	}
	t.Cleanup(func() {
		if root.Close() != nil {
			t.Error("owned Linux metadata root close failed")
		}
	})
	file, err := root.Open(".")
	if err != nil {
		t.Fatal("owned Linux filesystem observation failed")
	}
	var facts syscall.Statfs_t
	statErr := syscall.Fstatfs(int(file.Fd()), &facts)
	closeErr := file.Close()
	if statErr != nil || closeErr != nil {
		t.Fatal("owned Linux filesystem observation failed")
	}
	// This test adapter accepts only its temporary filesystem. The production
	// constructor admits solely the closed sysfs/proc filesystem constants.
	return systemStorageMetadataRoot{root, int64(facts.Type)}, directory
}

func TestStorageMetadataLinuxDescriptorPolicy(t *testing.T) {
	root, directory := metadataLinuxFixtureRoot(t)
	if os.WriteFile(filepath.Join(directory, "value"), []byte("8:0\n"), 0600) != nil {
		t.Fatal("owned scalar setup failed")
	}
	path, err := root.OpenPath("value")
	if err != nil || nilStorageMetadata(path) {
		t.Fatal("metadata descriptor rejected")
	}
	defer func() {
		if path.Close() != nil {
			t.Error("metadata descriptor close failed")
		}
	}()
	flags, _, callErr := syscall.Syscall(syscall.SYS_FCNTL, path.Fd(), syscall.F_GETFL, 0)
	if callErr != 0 || flags&storageMetadataOpenPath == 0 || flags&syscall.O_NOFOLLOW == 0 {
		t.Fatal("metadata open flags differ")
	}
	fdFlags, _, callErr := syscall.Syscall(syscall.SYS_FCNTL, path.Fd(), syscall.F_GETFD, 0)
	if callErr != 0 || fdFlags&syscall.FD_CLOEXEC == 0 {
		t.Fatal("metadata descriptor inherited")
	}
	reader, err := root.OpenReadable(path)
	if err != nil || nilStorageMetadata(reader) {
		t.Fatal("owned descriptor reopen failed")
	}
	defer func() {
		if reader.Close() != nil {
			t.Error("readable descriptor close failed")
		}
	}()
	file, ok := reader.(*os.File)
	if !ok {
		t.Fatal("unexpected readable owner")
	}
	connection, err := file.SyscallConn()
	if err != nil {
		t.Fatal("readable descriptor inspection failed")
	}
	var access, inherit uintptr
	var accessErr, inheritErr syscall.Errno
	err = connection.Control(func(fd uintptr) {
		access, _, accessErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
		inherit, _, inheritErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0)
	})
	if err != nil || accessErr != 0 || inheritErr != 0 || access&syscall.O_ACCMODE != syscall.O_RDONLY ||
		access&syscall.O_NONBLOCK == 0 || inherit&syscall.FD_CLOEXEC == 0 {
		t.Fatal("readable descriptor flags differ")
	}
	data, err := storageMetadataBytes(context.Background(), reader, 4)
	if err != nil || string(data) != "8:0\n" {
		t.Fatal("owned descriptor content mismatch")
	}
}

func TestStorageMetadataLinuxOwnedPaths(t *testing.T) {
	root, directory := metadataLinuxFixtureRoot(t)
	if os.Mkdir(filepath.Join(directory, "device"), 0700) != nil ||
		os.WriteFile(filepath.Join(directory, "device", "value"), []byte("4\n"), 0600) != nil ||
		os.Symlink("device", filepath.Join(directory, "contained")) != nil ||
		os.Symlink("device/value", filepath.Join(directory, "final")) != nil ||
		syscall.Mkfifo(filepath.Join(directory, "fifo"), 0600) != nil {
		t.Fatal("owned path fixtures failed")
	}
	outside := t.TempDir()
	if os.WriteFile(filepath.Join(outside, "value"), []byte("outside"), 0600) != nil ||
		os.Symlink(outside, filepath.Join(directory, "escape")) != nil {
		t.Fatal("owned escape fixture failed")
	}
	for _, tc := range []struct {
		name, path string
		good       bool
	}{
		{"direct", "device/value", true}, {"contained", "contained/value", true},
		{"final_symlink", "final", false}, {"fifo", "fifo", false}, {"directory", "device", false}, {"escape", "escape/value", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := readStorageMetadata(context.Background(), root, tc.path, 256)
			if tc.good {
				if err != nil || string(data) != "4\n" {
					t.Fatal("owned path rejected")
				}
			} else if err == nil || data != nil {
				t.Fatal("unsafe path admitted")
			}
		})
	}
	t.Run("named_replacement", func(t *testing.T) {
		err := withStorageMetadata(context.Background(), root, "device/value", false, func(file storageMetadataFile) error {
			if _, err := io.ReadAll(io.LimitReader(file, 257)); err != nil {
				return errStorageMetadataAcquire
			}
			if os.Rename(filepath.Join(directory, "device", "value"), filepath.Join(directory, "device", "before")) != nil ||
				os.WriteFile(filepath.Join(directory, "device", "value"), []byte("4\n"), 0600) != nil {
				t.Fatal("owned replacement setup failed")
			}
			return nil
		})
		if err == nil {
			t.Fatal("same bytes at replacement object admitted")
		}
	})
	t.Run("terminal_dot", func(t *testing.T) {
		if os.Mkdir(filepath.Join(directory, "links"), 0700) != nil ||
			os.Symlink("../device", filepath.Join(directory, "links", "target")) != nil {
			t.Fatal("owned relation fixture failed")
		}
		owners := []io.Closer{}
		anchor, err := acquireStorageMetadataAnchor(context.Background(), root, "contained/.", &owners)
		if err == nil {
			err = storageMetadataAlias(context.Background(), root, "links/target/.", anchor)
		}
		if err == nil {
			expected, e := root.Lstat("device/.")
			if e != nil || !os.SameFile(expected, anchor.info) || !anchor.info.IsDir() {
				err = errStorageMetadataAcquire
			}
		}
		closeStorageMetadata(owners, &err)
		if err != nil {
			t.Fatal("terminal dot did not bind canonical target directory")
		}
	})
}

func TestStorageMetadataLinuxFilesystemBinding(t *testing.T) {
	root, directory := metadataLinuxFixtureRoot(t)
	if os.WriteFile(filepath.Join(directory, "value"), []byte("4\n"), 0600) != nil {
		t.Fatal("owned scalar setup failed")
	}
	root.filesystem = 0
	owner, err := root.OpenPath("value")
	if err == nil || nilStorageMetadata(owner) {
		t.Fatal("wrong filesystem did not return rejected acquired owner")
	}
	if owner.Close() != nil {
		t.Fatal("rejected acquired owner close failed")
	}
	owner, err = root.OpenPath("missing")
	if err == nil || !nilStorageMetadata(owner) {
		t.Fatal("missing path invented owner")
	}
	opened, err := systemStorageMetadataOperations().openRoot("unknown")
	if err == nil || !nilStorageMetadata(opened) {
		t.Fatal("unknown system root admitted")
	}
}
