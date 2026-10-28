//go:build linux

package inventory

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestInventoryProductionDirectorySupportsNormalSysfsLinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "devices", "fixture0")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "size"), []byte("4096\n"), 0600); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(root, "sys", "class", "block")
	if err := os.MkdirAll(registry, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(registry, "fixture0")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(registry, filepath.Join(root, "registry-link")); err != nil {
		t.Fatal(err)
	}
	entries, err := readInventoryDirectory(t.Context(), filepath.Join(root, "registry-link"), openInventoryDirectory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "fixture0" {
		t.Fatal("normal directory link rejected")
	}
	snapshot, err := NewLinuxCollector(root).Collect(t.Context())
	if err != nil || len(snapshot.Components) != 1 {
		t.Fatalf("normal class link lost: %v", err)
	}
	assertAttribute(t, snapshot, ComponentBlock, "block:fixture0", "size_sectors", "4096")
}

func TestInventoryProductionDirectoryDescriptorFlags(t *testing.T) {
	root := t.TempDir()
	var opened *os.File
	var statusFlags, descriptorFlags uintptr
	controls := 0
	entries, err := readInventoryDirectory(t.Context(), root, func(name string) (inventoryDirectory, error) {
		directory, openErr := openInventoryDirectory(name)
		if directory == nil || openErr != nil {
			return directory, openErr
		}
		opened = directory.(*os.File)
		connection, connectionErr := opened.SyscallConn()
		if connectionErr != nil {
			return directory, connectionErr
		}
		var queryErr syscall.Errno
		controlErr := connection.Control(func(fd uintptr) {
			controls++
			statusFlags, _, queryErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
			if queryErr != 0 {
				return
			}
			descriptorFlags, _, queryErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0)
		})
		if controlErr != nil {
			return directory, controlErr
		}
		if queryErr != 0 {
			return directory, queryErr
		}
		return directory, nil
	})
	if err != nil || len(entries) != 0 || controls != 1 || opened == nil {
		t.Fatal("owned descriptor flag observation failed")
	}
	if statusFlags&syscall.O_ACCMODE != syscall.O_RDONLY {
		t.Fatal("directory was not opened read-only")
	}
	if statusFlags&syscall.O_NONBLOCK == 0 {
		t.Fatal("directory descriptor lacks nonblocking flag")
	}
	if descriptorFlags&syscall.FD_CLOEXEC == 0 {
		t.Fatal("directory descriptor lacks close-on-exec flag")
	}
	if _, err := opened.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("flag-observed descriptor was not closed")
	}
}
