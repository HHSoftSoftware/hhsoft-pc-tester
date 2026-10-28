//go:build linux

package safemode

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSafetyAcquisitionLinuxUsesOwnedMetadataBeforeReadableOpen(t *testing.T) {
	path := fixture(t)
	root, err := systemSafetyOperations().openRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	handle, err := root.OpenPath("proc/self/mountinfo")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := handle.Close(); err != nil {
			t.Error(err)
		}
	}()
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, handle.Fd(), syscall.F_GETFL, 0)
	if errno != 0 || int(flags)&safetyOpenPath == 0 || int(flags)&syscall.O_NOFOLLOW == 0 {
		t.Fatal("metadata descriptor is not O_PATH/NOFOLLOW")
	}
	fdflags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, handle.Fd(), syscall.F_GETFD, 0)
	if errno != 0 || int(fdflags)&syscall.FD_CLOEXEC == 0 {
		t.Fatal("metadata descriptor lacks CLOEXEC")
	}
	info, err := handle.Stat()
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("fixture is not regular metadata")
	}
	file, err := root.OpenReadable("proc/self/mountinfo", handle)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	readable, ok := file.(*os.File)
	if !ok {
		t.Fatal("real readable file not used")
	}
	connection, err := readable.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	// File.Fd can change blocking mode. RawConn observes the original owned
	// descriptor without changing the property this witness is checking.
	err = connection.Control(func(fd uintptr) { flags, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0) })
	if err != nil {
		t.Fatal(err)
	}
	if errno != 0 || int(flags)&syscall.O_ACCMODE != os.O_RDONLY || int(flags)&syscall.O_NONBLOCK == 0 {
		t.Fatal("readable descriptor is not read-only/nonblocking")
	}
	err = connection.Control(func(fd uintptr) { fdflags, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0) })
	if err != nil {
		t.Fatal(err)
	}
	if errno != 0 || int(fdflags)&syscall.FD_CLOEXEC == 0 {
		t.Fatal("readable descriptor lacks CLOEXEC")
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		t.Fatal("proc-fd reopening changed the owned object")
	}
}

func TestSafetyAcquisitionLinuxRetainsContainedSysfsLinks(t *testing.T) {
	root := fixture(t)
	writeFixture(t, root, "sys/devices/sda/dev", "8:0\n")
	writeFixture(t, root, "sys/devices/eth0/flags", "0x0\n")
	if err := os.Symlink("../../devices/sda", filepath.Join(root, "sys/class/block/sda")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../devices/eth0", filepath.Join(root, "sys/class/net/eth0")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "proc/self/mountinfo", "1 0 8:0 / /disk ro - ext4 alias rw\n")
	if result := check(t.Context(), root); !result.Safe() {
		t.Fatalf("contained ordinary registry links rejected: %#v", result)
	}
}

func TestSafetyAcquisitionLinuxRejectsEscapesAndFinalSymlinks(t *testing.T) {
	for _, mode := range []string{"absolute-escape", "relative-escape", "final-contained-symlink"} {
		t.Run(mode, func(t *testing.T) {
			h := newSafetyOwnerHarness(t)
			outside := t.TempDir()
			writeFixture(t, outside, "dev", "8:0\n")
			if mode == "final-contained-symlink" {
				writeFixture(t, h.root, "sys/class/block/sda/actual", "8:0\n")
				if err := os.Symlink("actual", filepath.Join(h.root, "sys/class/block/sda/dev")); err != nil {
					t.Fatal(err)
				}
			} else {
				target := outside
				if mode == "relative-escape" {
					var err error
					target, err = filepath.Rel(filepath.Join(h.root, "sys/class/block"), outside)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, filepath.Join(h.root, "sys/class/block/sda")); err != nil {
					t.Fatal(err)
				}
			}
			result := checkWith(h.ctx, h.root, h.operations())
			h.assertClosed()
			if result.Status != "error" || result.StorageObserved || !result.NetworkObserved {
				t.Fatalf("escaped or substituted metadata accepted: %#v", result)
			}
			for _, event := range h.events {
				if event == "open:storage/path:sys/class/block/sda/dev" {
					t.Fatal("unproved final metadata was opened")
				}
			}
		})
	}
}

func TestSafetyAcquisitionLinuxRejectsMetadataAndRegistryTypeChanges(t *testing.T) {
	for _, mode := range []string{"metadata-directory", "registry-file"} {
		t.Run(mode, func(t *testing.T) {
			h := newSafetyOwnerHarness(t)
			if mode == "metadata-directory" {
				path := filepath.Join(h.root, "proc/self/mountinfo")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				path := filepath.Join(h.root, "sys/class/block")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("not a registry"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result := checkWith(h.ctx, h.root, h.operations())
			h.assertClosed()
			if result.Safe() || result.StorageObserved || !result.NetworkObserved || result.Status != "error" {
				t.Fatal("wrong metadata type certified safe")
			}
		})
	}
}
