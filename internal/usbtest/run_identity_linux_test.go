//go:build linux

package usbtest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type usbRunFixture struct {
	root  string
	ops   usbNodeOperations
	files map[string]*usbNodeFixtureFile
	opens []string
}

func newUSBRunFixture(t *testing.T) *usbRunFixture {
	t.Helper()
	f := &usbRunFixture{root: t.TempDir(), files: make(map[string]*usbNodeFixtureFile), ops: realUSBNodeOperations()}
	for _, row := range []struct {
		name, address, number string
		rdev                  uint64
	}{
		{"usb1", "001", "189:0\n", 0xbd00}, {"1-1", "002", "189:1\n", 0xbd01}, {"1-2", "003", "189:2\n", 0xbd02},
	} {
		kind, ports := "00", "0"
		if row.name == "usb1" {
			kind, ports = "09", "4"
		}
		device := "1"
		if row.address == "002" {
			device = "2"
		}
		if row.address == "003" {
			device = "3"
		}
		mustUSBDevice(t, f.root, row.name, "1", device, kind, "1", ports)
		mustWrite(t, filepath.Join(f.root, "sys/bus/usb/devices", row.name, "dev"), row.number)
		f.files[row.address] = &usbNodeFixtureFile{info: usbNodeInfo(row.rdev, row.rdev)}
	}
	f.ops.lstat = func(path string) (os.FileInfo, error) {
		file := f.files[filepath.Base(path)]
		if file == nil {
			t.Fatal("unexpected node path")
		}
		return file.info, nil
	}
	f.ops.open = func(path string, _ int, _ os.FileMode) (usbNodeFile, error) {
		name := filepath.Base(path)
		f.opens = append(f.opens, name)
		return f.files[name], nil
	}
	return f
}

func TestUSBRunRetainsEarlierResultsAndStopsAfterCloseFailure(t *testing.T) {
	for _, cancelToo := range []bool{false, true} {
		t.Run(map[bool]string{false: "close", true: "close and cancel"}[cancelToo], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := newUSBRunFixture(t)
			f.files["003"].closeErr = os.ErrPermission
			if cancelToo {
				f.files["003"].closeHook = cancel
			}
			got := runPlatformWith(ctx, Options{Root: f.root}, f.ops)
			if got.Status != StatusError || got.Detail != errUSBNodeClose.Error() || got.ReadableDevices != 1 || len(f.opens) != 2 || f.opens[0] != "002" || f.opens[1] != "003" || f.files["002"].closes != 1 || f.files["003"].closes != 1 || f.files["001"].closes != 0 || got.USBTransfersGenerated {
				t.Fatalf("close error was lost or a later node opened: %+v", got)
			}
			if !got.Devices[0].Readable || got.Devices[1].Readable || got.Devices[2].Readable {
				t.Fatal("partial evidence or revoked readiness changed")
			}
		})
	}
}

func TestUSBRunRejectsIncompleteRegistryBeforeAnyNodeOpen(t *testing.T) {
	for _, bad := range []string{"missing dev", "missing vendor", "malformed dev", "duplicate dev", "changed address"} {
		t.Run(bad, func(t *testing.T) {
			f := newUSBRunFixture(t)
			base := filepath.Join(f.root, "sys/bus/usb/devices/1-2")
			switch bad {
			case "missing dev":
				if err := os.Remove(filepath.Join(base, "dev")); err != nil {
					t.Fatal(err)
				}
			case "missing vendor":
				if err := os.Remove(filepath.Join(base, "idVendor")); err != nil {
					t.Fatal(err)
				}
			case "malformed dev":
				mustWrite(t, filepath.Join(base, "dev"), "189:2 trailing\n")
			case "duplicate dev":
				mustWrite(t, filepath.Join(base, "dev"), "189:1\n")
			case "changed address":
				open := f.ops.openMetadata
				f.ops.openMetadata = func(path string) (usbMetadataFile, error) {
					if path == filepath.Join(base, "dev") {
						mustWrite(t, filepath.Join(base, "devnum"), "4\n")
					}
					return open(path)
				}
			}
			got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
			if got.Status != StatusError || got.ReadableDevices != 0 || len(f.opens) != 0 || got.USBTransfersGenerated {
				t.Fatalf("incomplete registry concealed by another device: %+v", got)
			}
		})
	}
}

func TestUSBRunCancellationRevokesLastCreditAndNoDeviceDisposition(t *testing.T) {
	for _, stage := range []string{"first close", "last close", "last identity close", "no-root final identity"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := newUSBRunFixture(t)
			wantOpens := 0
			wantReadable := 0
			switch stage {
			case "first close":
				f.files["002"].closeHook = cancel
				wantOpens = 1
			case "last close":
				f.files["001"].closeHook = cancel
				wantOpens = 3
				wantReadable = 2
			case "no-root final identity":
				if err := os.RemoveAll(filepath.Join(f.root, "sys/bus/usb/devices/usb1")); err != nil {
					t.Fatal(err)
				}
				open := f.ops.openMetadata
				f.ops.openMetadata = func(path string) (usbMetadataFile, error) {
					file, err := open(path)
					if err == nil && path == filepath.Join(f.root, "sys/bus/usb/devices/1-2/authorized") {
						return &usbTrackedMetadata{usbMetadataFile: file, closeHook: cancel}, nil
					}
					return file, err
				}
			case "last identity close":
				open := f.ops.openMetadata
				f.ops.openMetadata = func(path string) (usbMetadataFile, error) {
					file, err := open(path)
					if err == nil && path == filepath.Join(f.root, "sys/bus/usb/devices/usb1/authorized") {
						return &usbTrackedMetadata{usbMetadataFile: file, closeHook: cancel}, nil
					}
					return file, err
				}
			}
			got := runPlatformWith(ctx, Options{Root: f.root}, f.ops)
			if got.Status != StatusAborted || got.ReadableDevices != wantReadable || len(f.opens) != wantOpens {
				t.Fatalf("late cancellation was credited: %+v", got)
			}
		})
	}
	for _, kind := range []string{"empty", "unrelated", "peripheral only", "absent registry"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if kind != "absent registry" {
				if err := os.MkdirAll(filepath.Join(root, "sys/bus/usb/devices"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "unrelated" {
				if err := os.MkdirAll(filepath.Join(root, "sys/bus/usb/devices/1-1:1.0"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "peripheral only" {
				mustUSBDevice(t, root, "1-1", "1", "2", "00", "0", "0")
			}
			got := runPlatform(t.Context(), Options{Root: root})
			want := StatusInconclusive
			if kind == "absent registry" {
				want = StatusError
			}
			if got.Status != want || got.ReadableDevices != 0 || got.USBTransfersGenerated {
				t.Fatal("legacy no-device disposition changed")
			}
		})
	}
}

func TestUSBRunUsesActualInspectorForAllSuccessfulNodes(t *testing.T) {
	f := newUSBRunFixture(t)
	got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
	if got.Status != StatusPass || got.ReadableDevices != 3 || got.AuthorizedDevices != 3 || len(f.opens) != 3 || got.USBTransfersGenerated {
		t.Fatalf("matching registry/node observations rejected: %+v", got)
	}
	for _, file := range f.files {
		if file.stats != 1 || file.closes != 1 {
			t.Fatal("actual inspector did not own each node")
		}
	}
	for _, change := range []func(*usbRunFixture){
		func(f *usbRunFixture) { f.files["003"].info = usbNodeInfo(0x103, 5) },
		func(f *usbRunFixture) { f.files["003"].statErr = os.ErrPermission },
	} {
		f := newUSBRunFixture(t)
		change(f)
		got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
		if got.Status != StatusInconclusive || got.ReadableDevices != 2 || got.Devices[1].Readable {
			t.Fatal("healthy nodes concealed a failed device")
		}
	}
}

func TestUSBRunRejectsDuplicateAddressesBeforeNodeOpen(t *testing.T) {
	for _, mode := range []string{"unique addresses", "duplicate authorized addresses", "duplicate unauthorized address"} {
		t.Run(mode, func(t *testing.T) {
			f := newUSBRunFixture(t)
			paths := []string{"002", "003", "001"}
			// Each descriptor can individually match the registry observed for that
			// visit. Sharing an address is still inconsistent across registry rows.
			observations := []*usbNodeFixtureFile{f.files["002"], f.files["003"], f.files["001"]}
			if mode != "unique addresses" {
				mustWrite(t, filepath.Join(f.root, "sys/bus/usb/devices/1-2/devnum"), "2\n")
				paths[1] = "002"
			}
			if mode == "duplicate unauthorized address" {
				mustWrite(t, filepath.Join(f.root, "sys/bus/usb/devices/1-2/authorized"), "0\n")
			}
			completed, stats := 0, 0
			for _, file := range observations {
				file.closeHook = func() { completed++ }
			}
			f.ops.lstat = func(path string) (os.FileInfo, error) {
				stats++
				if completed >= len(observations) || filepath.Base(path) != paths[completed] {
					t.Fatal("unexpected observed node path")
				}
				return observations[completed].info, nil
			}
			f.ops.open = func(path string, _ int, _ os.FileMode) (usbNodeFile, error) {
				if completed >= len(observations) || filepath.Base(path) != paths[completed] {
					t.Fatal("unexpected opened node path")
				}
				f.opens = append(f.opens, filepath.Base(path))
				return observations[completed], nil
			}
			got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
			if mode == "unique addresses" {
				if got.Status != StatusPass || got.ReadableDevices != 3 || len(f.opens) != 3 || completed != 3 || stats != 6 {
					t.Fatalf("valid observed identities did not exercise the actual inspector: %+v", got)
				}
			} else if got.Status != StatusError || got.Detail != "USB registry repeats a bus and device address" || got.ReadableDevices != 0 || len(f.opens) != 0 || completed != 0 || stats != 0 {
				t.Fatalf("duplicate address reached a node or gained readiness: %+v", got)
			}
			if got.USBTransfersGenerated {
				t.Fatal("fixture generated application USB transfers")
			}
		})
	}
}
