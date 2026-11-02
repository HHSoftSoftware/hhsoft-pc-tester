//go:build linux

package inventory

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func TestPCIeMaximumLinuxOwnedDescriptorsAndLinks(t *testing.T) {
	for _, tc := range []struct {
		name, field, raw, want string
		parse                  func([]byte) (string, string)
	}{
		{"speed", "max_link_speed", "16.0 GT/s PCIe\n", "16000", parsePCIeMaximumSpeed},
		{"width", "max_link_width", "12\n", "12", parsePCIeMaximumWidth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			base := pcieWriteDevice(t, root, pcieTestAddress, nil)
			target := filepath.Join(root, "owned-value")
			if err := os.WriteFile(target, []byte(tc.raw), 0600); err != nil {
				t.Fatal("fixture write failed")
			}
			link := filepath.Join(base, tc.field)
			if err := os.Symlink(target, link); err != nil {
				t.Fatal("fixture link failed")
			}
			var opened *os.File
			var status, descriptor uintptr
			controls := 0
			data, observed, err := readInventoryScalar(t.Context(), link, func(name string) (inventoryScalar, error) {
				file, openErr := openInventoryScalar(name)
				if file == nil || openErr != nil {
					return file, openErr
				}
				opened = file.(*os.File)
				connection, e := opened.SyscallConn()
				if e != nil {
					return file, e
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
			if err != nil || !observed || controls != 1 || status&syscall.O_ACCMODE != syscall.O_RDONLY || status&syscall.O_NONBLOCK == 0 || descriptor&syscall.FD_CLOEXEC == 0 {
				t.Fatal("capability descriptor ownership flags changed")
			}
			if opened == nil || !errors.Is(opened.Close(), os.ErrClosed) {
				t.Fatal("owned capability descriptor leaked")
			}
			state, value := tc.parse(data)
			if state != "reported" || value != tc.want {
				t.Fatal("owned link literal changed")
			}
			snapshot, diagnostics, err := NewLinuxCollector(root).CollectWithDiagnostics(t.Context())
			if err != nil || diagnostics != (CollectionDiagnostics{}) {
				t.Fatal("fixture-backed full collection failed")
			}
			want := map[string]string{"max_link_speed_state": "unavailable", "max_link_width_state": "unavailable"}
			if tc.name == "speed" {
				want["max_link_speed_state"] = "reported"
				want["max_link_speed_mts"] = "16000"
			} else {
				want["max_link_width_state"] = "reported"
				want["max_link_width_lanes"] = "12"
			}
			if !reflect.DeepEqual(pcieTestAttributes(t, snapshot, pcieTestAddress), want) {
				t.Fatal("real collector did not preserve normal sysfs-style links")
			}
		})
	}
}

func TestPCIeMaximumLinuxSpecialFilesFailWithoutPartialSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		fifo        bool
	}{
		{"speed_fifo", "max_link_speed", true}, {"width_fifo", "max_link_width", true},
		{"speed_directory", "max_link_speed", false}, {"width_directory", "max_link_width", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			fields := map[string][]byte{"max_link_speed": []byte("8.0 GT/s PCIe\n"), "max_link_width": []byte("16\n")}
			delete(fields, tc.field)
			base := pcieWriteDevice(t, root, pcieTestAddress, fields)
			name := filepath.Join(base, tc.field)
			if tc.fifo {
				if err := syscall.Mkfifo(name, 0600); err != nil {
					t.Fatal("fixture FIFO creation failed")
				}
			} else {
				if err := os.Mkdir(name, 0700); err != nil {
					t.Fatal("fixture directory creation failed")
				}
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
				t.Fatal("rejected capability descriptor leaked")
			}
			snapshot, diagnostics, err := NewLinuxCollector(root).CollectWithDiagnostics(t.Context())
			pcieRequireEmpty(t, snapshot, diagnostics, err, ErrScalarRead)
		})
	}
}
