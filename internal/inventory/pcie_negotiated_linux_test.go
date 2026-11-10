//go:build linux

package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func TestPCIeNegotiatedLinuxOwnedDescriptorsAndLinks(t *testing.T) {
	for _, tc := range []struct{ field, raw, state, value string }{
		{"current_link_speed", "16.0 GT/s PCIe\n", "current_link_speed_state", "16000"},
		{"current_link_width", "12\n", "current_link_width_state", "12"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			root := t.TempDir()
			base := pcieWriteDevice(t, root, pcieTestAddress, nil)
			target := filepath.Join(root, "owned-value")
			if err := os.WriteFile(target, []byte(tc.raw), 0600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(base, tc.field)
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			var opened *os.File
			var status, descriptor uintptr
			controls := 0
			data, observed, err := readInventoryScalar(t.Context(), link, func(name string) (inventoryScalar, error) {
				file, err := openInventoryScalar(name)
				if file == nil || err != nil {
					return file, err
				}
				opened = file.(*os.File)
				connection, err := opened.SyscallConn()
				if err != nil {
					return file, err
				}
				var queryErr syscall.Errno
				controlErr := connection.Control(func(fd uintptr) {
					controls++
					status, _, queryErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
					if queryErr == 0 {
						descriptor, _, queryErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0)
					}
				})
				if controlErr != nil {
					return file, controlErr
				}
				if queryErr != 0 {
					return file, queryErr
				}
				return file, nil
			})
			if err != nil || !observed || string(data) != tc.raw || controls != 1 || status&syscall.O_ACCMODE != syscall.O_RDONLY || status&syscall.O_NONBLOCK == 0 || descriptor&syscall.FD_CLOEXEC == 0 {
				t.Fatal("current descriptor flags or completed bytes changed")
			}
			if opened == nil || !errors.Is(opened.Close(), os.ErrClosed) {
				t.Fatal("current descriptor leaked")
			}
			snapshot, diagnostics, err := NewLinuxCollector(root).CollectWithDiagnostics(t.Context())
			want := map[string]string{"current_link_speed_state": "unavailable", "current_link_width_state": "unavailable"}
			want[tc.state] = "reported"
			if tc.field == "current_link_speed" {
				want["current_link_speed_mts"] = tc.value
			} else {
				want["current_link_width_lanes"] = tc.value
			}
			if err != nil || diagnostics != (CollectionDiagnostics{}) || !reflect.DeepEqual(pcieCurrentAttributes(t, snapshot, pcieTestAddress), want) {
				t.Fatal("real collector did not retain linked current observation")
			}
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			snapshot, diagnostics, err = NewLinuxCollector(root).CollectWithDiagnostics(t.Context())
			if err != nil || diagnostics != (CollectionDiagnostics{}) || len(pcieCurrentAttributes(t, snapshot, pcieTestAddress)) != 0 {
				t.Fatal("disappearance retained a prior current observation")
			}
		})
	}
}

func TestPCIeNegotiatedLinuxSpecialFilesFailClosed(t *testing.T) {
	for _, field := range []string{"current_link_speed", "current_link_width"} {
		for _, fifo := range []bool{false, true} {
			root := t.TempDir()
			fields := map[string][]byte{"current_link_speed": []byte("8.0 GT/s PCIe\n"), "current_link_width": []byte("16\n")}
			delete(fields, field)
			base := pcieWriteDevice(t, root, pcieTestAddress, fields)
			name := filepath.Join(base, field)
			var err error
			if fifo {
				err = syscall.Mkfifo(name, 0600)
			} else {
				err = os.Mkdir(name, 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			var opened *os.File
			data, observed, err := readInventoryScalar(t.Context(), name, func(name string) (inventoryScalar, error) {
				file, e := openInventoryScalar(name)
				if file != nil {
					opened = file.(*os.File)
				}
				return file, e
			})
			requireScalarFailure(t, data, observed, err, ErrScalarRead)
			if opened == nil || !errors.Is(opened.Close(), os.ErrClosed) {
				t.Fatal("rejected current descriptor leaked")
			}
			snapshot, diagnostics, err := NewLinuxCollector(root).CollectWithDiagnostics(t.Context())
			pcieRequireEmpty(t, snapshot, diagnostics, err, ErrScalarRead)
		}
	}
}

type pcieCurrentCloseFile struct {
	*os.File
	cancel context.CancelFunc
	fail   bool
}

func (f pcieCurrentCloseFile) Close() error {
	err := f.File.Close()
	if f.cancel != nil {
		f.cancel()
	}
	if err == nil && f.fail {
		return fsCurrentCloseFailure{}
	}
	return err
}

type fsCurrentCloseFailure struct{}

func (fsCurrentCloseFailure) Error() string { return "fixture close failure" }

func TestPCIeNegotiatedLinuxCloseAndCancellationSuppressOwnedBytes(t *testing.T) {
	for _, field := range []string{"current_link_speed", "current_link_width"} {
		for _, fail := range []bool{false, true} {
			root := t.TempDir()
			name := filepath.Join(pcieWriteDevice(t, root, pcieTestAddress, map[string][]byte{field: []byte("16\n")}), field)
			ctx, cancel := context.WithCancel(t.Context())
			var opened *os.File
			data, observed, err := readInventoryScalar(ctx, name, func(name string) (inventoryScalar, error) {
				file, e := openInventoryScalar(name)
				if file == nil || e != nil {
					return file, e
				}
				opened = file.(*os.File)
				return pcieCurrentCloseFile{File: opened, cancel: cancel, fail: fail}, nil
			})
			cancel()
			want := error(context.Canceled)
			if fail {
				want = ErrScalarClose
			}
			requireScalarFailure(t, data, observed, err, want)
			if opened == nil || !errors.Is(opened.Close(), os.ErrClosed) {
				t.Fatal("cancelled current descriptor leaked")
			}
		}
	}
}
