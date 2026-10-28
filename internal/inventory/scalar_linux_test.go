//go:build linux

package inventory

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestInventoryScalarProductionDescriptorFlagsAndNormalLinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "device-value")
	if err := os.WriteFile(target, []byte("linked value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "normal-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	var opened *os.File
	var status, descriptor uintptr
	controls := 0
	data, observed, err := readInventoryScalar(t.Context(), link, func(name string) (inventoryScalar, error) {
		f, openErr := openInventoryScalar(name)
		if f == nil || openErr != nil {
			return f, openErr
		}
		opened = f.(*os.File)
		connection, connectionErr := opened.SyscallConn()
		if connectionErr != nil {
			return f, connectionErr
		}
		var queryErr syscall.Errno
		controlErr := connection.Control(func(fd uintptr) {
			controls++
			status, _, queryErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
			if queryErr != 0 {
				return
			}
			descriptor, _, queryErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0)
		})
		if controlErr != nil {
			return f, controlErr
		}
		if queryErr != 0 {
			return f, queryErr
		}
		return f, nil
	})
	if err != nil || !observed || string(data) != "linked value\n" || controls != 1 {
		t.Fatal("normal link acquisition failed", err)
	}
	if status&syscall.O_ACCMODE != syscall.O_RDONLY || status&syscall.O_NONBLOCK == 0 || descriptor&syscall.FD_CLOEXEC == 0 {
		t.Fatal("scalar descriptor flags do not meet read-only/nonblocking/close-on-exec contract")
	}
	if opened == nil || !errors.Is(opened.Close(), os.ErrClosed) {
		t.Fatal("flag-observed scalar leaked")
	}
	name := filepath.Join(root, "sys", "class", "dmi", "id", "product_name")
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewLinuxCollector(root).Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	assertAttribute(t, snapshot, ComponentSystem, "system", "product_name", "linked value")
}

func TestInventoryScalarProductionFIFORejectedWithoutReading(t *testing.T) {
	name := filepath.Join(t.TempDir(), "owned-fifo")
	if err := syscall.Mkfifo(name, 0600); err != nil {
		t.Fatal(err)
	}
	var opened *os.File
	data, observed, err := readInventoryScalar(t.Context(), name, func(name string) (inventoryScalar, error) {
		f, e := openInventoryScalar(name)
		if f != nil {
			opened = f.(*os.File)
		}
		return f, e
	})
	requireScalarFailure(t, data, observed, err, ErrScalarRead)
	if opened == nil || !errors.Is(opened.Close(), os.ErrClosed) {
		t.Fatal("nonregular scalar handle leaked")
	}
}
