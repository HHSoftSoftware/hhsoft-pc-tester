//go:build linux

package usbtest

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestUSBInitialAndOptionalScalarsBoundOverflowBeforeNodeWork(t *testing.T) {
	for _, field := range []string{"idVendor", "busnum", "devnum", "authorized", "bDeviceClass", "speed", "maxchild"} {
		for _, fault := range []string{"overflow", "protocol", "close"} {
			t.Run(field+"/"+fault, func(t *testing.T) {
				f := newUSBRunFixture(t)
				realOpen := f.ops.openMetadata
				bad := usbMetadata(strings.Repeat("9", 4096))
				seen := false
				later := 0
				want := errUSBMetadataRead
				if fault == "protocol" {
					bad.read = func(p []byte) (int, error) { return len(p) + 1, io.EOF }
				}
				if fault == "close" {
					bad = usbMetadata("1\n")
					bad.closeErr = os.ErrPermission
					want = errUSBMetadataClose
				}
				f.ops.openMetadata = func(path string) (usbMetadataFile, error) {
					if seen {
						later++
					}
					if path == filepath.Join(f.root, "sys/bus/usb/devices/1-1", field) {
						seen = true
						return bad, nil
					}
					return realOpen(path)
				}
				got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
				if !seen || got.Status != StatusError || got.Detail != want.Error() || got.ReadableDevices != 0 || len(f.opens) != 0 || later != 0 || bad.closes != 1 || bad.maximumRequest > 32 {
					t.Fatal("acquisition fault was concealed or later work continued")
				}
				if fault == "overflow" && bad.bytesRead != 257 {
					t.Fatal("overflow witness escaped the raw-byte bound")
				}
			})
		}
	}
}

func TestUSBOptionalAcquisitionFaultsAreNotUnknown(t *testing.T) {
	for _, fault := range []string{"negative count", "oversized count", "no progress", "partial errno", "stat", "nonregular", "close", "handle and error", "nil handle", "permission"} {
		t.Run(fault, func(t *testing.T) {
			f := newUSBRunFixture(t)
			realOpen := f.ops.openMetadata
			bad := usbMetadata("09\n")
			var openErr error
			var handle usbMetadataFile = bad
			switch fault {
			case "negative count":
				bad.read = func([]byte) (int, error) { return -1, nil }
			case "oversized count":
				bad.read = func(p []byte) (int, error) { return len(p) + 1, io.EOF }
			case "no progress":
				bad.read = func([]byte) (int, error) { return 0, nil }
			case "partial errno":
				bad.read = func(p []byte) (int, error) { return copy(p, "09"), os.ErrPermission }
			case "stat":
				bad.statErr = os.ErrPermission
			case "nonregular":
				bad.info = usbInfo{mode: os.ModeNamedPipe}
			case "close":
				bad.closeErr = os.ErrPermission
			case "handle and error":
				openErr = os.ErrNotExist
			case "nil handle":
				handle = nil
			case "permission":
				handle, openErr = nil, os.ErrPermission
			}
			seen, later := false, 0
			f.ops.openMetadata = func(path string) (usbMetadataFile, error) {
				if seen {
					later++
				}
				if path == filepath.Join(f.root, "sys/bus/usb/devices/1-1/bDeviceClass") {
					seen = true
					return handle, openErr
				}
				return realOpen(path)
			}
			got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
			want := errUSBMetadataRead
			if fault == "close" {
				want = errUSBMetadataClose
			}
			closes := 1
			if handle == nil {
				closes = 0
			}
			if !seen || got.Status != StatusError || got.Detail != want.Error() || got.ReadableDevices != 0 || len(f.opens) != 0 || later != 0 || bad.closes != closes {
				t.Fatal("hard optional failure was concealed or ownership escaped")
			}
		})
	}
}

func TestUSBOptionalClassIsReadOnceAndOnlyValidValuesArePublished(t *testing.T) {
	for _, test := range []struct{ value, class, kind string }{
		{"09\n", "0x09", "hub"}, {"0009\n", "0x09", "hub"}, {"0X0009\n", "0x09", "hub"}, {strings.Repeat("0", 254) + "09", "0x09", "hub"}, {"9\n", "0x09", "hub"}, {"0X09\n", "0x09", "hub"}, {"FF\n", "0xff", "device"},
		{"g9\n", "", "device"}, {"009\n", "0x09", "hub"}, {"+9\n", "", "device"}, {"0x\n", "", "device"},
		{"", "", "device"}, {"\x00", "", "device"}, {strings.Repeat("x", 256), "", "device"},
		{strings.Repeat(" ", 256), "", "device"}, {"9" + strings.Repeat(" ", 255), "0x09", "hub"},
	} {
		t.Run(test.value, func(t *testing.T) {
			f := newUSBRunFixture(t)
			path := filepath.Join(f.root, "sys/bus/usb/devices/1-1/bDeviceClass")
			mustWrite(t, path, test.value)
			realOpen := f.ops.openMetadata
			calls := 0
			f.ops.openMetadata = func(name string) (usbMetadataFile, error) {
				if name == path {
					calls++
					if calls > 1 {
						t.Fatal("class was acquired twice")
					}
				}
				return realOpen(name)
			}
			got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
			if got.Status != StatusPass || len(f.opens) != 3 || got.ReadableDevices != 3 || calls != 1 || got.Devices[0].Class != test.class || got.Devices[0].Kind != test.kind {
				t.Fatal("class was fabricated, re-read or rejected without cause")
			}
		})
	}
}

func TestUSBOptionalAbsenceAndMalformedValuesRemainUnknown(t *testing.T) {
	for _, field := range []string{"authorized", "bDeviceClass", "speed", "maxchild"} {
		for _, mode := range []string{"absent", "malformed", "empty"} {
			t.Run(field+"/"+mode, func(t *testing.T) {
				f := newUSBRunFixture(t)
				path := filepath.Join(f.root, "sys/bus/usb/devices/1-1", field)
				if mode == "absent" {
					if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
						t.Fatal(err)
					}
				} else {
					value := "invalid\n"
					if mode == "empty" {
						value = ""
					}
					mustWrite(t, path, value)
				}
				got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
				wantStatus, wantReadable := StatusPass, 3
				if field == "authorized" {
					wantStatus, wantReadable = StatusInconclusive, 2
				}
				if got.Status != wantStatus || got.ReadableDevices != wantReadable || len(f.opens) != wantReadable {
					t.Fatal("optional unknown changed node eligibility")
				}
				row := got.Devices[0]
				if field == "authorized" && (row.AuthorizedKnown || row.Authorized || row.Readable) || field == "bDeviceClass" && (row.Class != "" || row.Kind != "device") || field == "speed" && row.LinkSpeedMbps != "" || field == "maxchild" && row.DownstreamPorts != 0 {
					t.Fatal("unknown optional observation was fabricated")
				}
			})
		}
	}
}

func TestUSBMandatoryInitialObservationsCannotBecomeOptional(t *testing.T) {
	for _, field := range []string{"idVendor", "busnum", "devnum"} {
		t.Run(field+"/absent", func(t *testing.T) {
			f := newUSBRunFixture(t)
			if err := os.Remove(filepath.Join(f.root, "sys/bus/usb/devices/1-1", field)); err != nil {
				t.Fatal(err)
			}
			got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
			if got.Status != StatusError || got.ReadableDevices != 0 || len(f.opens) != 0 {
				t.Fatal("required discovery silently omitted a device")
			}
		})
	}
	for _, field := range []string{"busnum", "devnum"} {
		for _, value := range []string{"0", "256", "01", "+1", "1 trailing", ""} {
			t.Run(field+"/"+value, func(t *testing.T) {
				f := newUSBRunFixture(t)
				mustWrite(t, filepath.Join(f.root, "sys/bus/usb/devices/1-1", field), value+"\n")
				got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
				if got.Status != StatusError || len(f.opens) != 0 || got.ReadableDevices != 0 {
					t.Fatal("unproven initial bus/address accepted")
				}
			})
		}
	}
}

func TestUSBAcquisitionLateCancellationStopsFurtherOpens(t *testing.T) {
	for _, phase := range []string{"initial", "optional", "last optional", "absent optional", "last unauthorized", "empty registry", "registry close", "optional close failure", "optional read failure"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := newUSBRunFixture(t)
			seen, later := false, 0
			want := StatusAborted
			open := f.ops.openMetadata
			if phase == "empty registry" || phase == "registry close" {
				file := usbRegistry(0)
				file.closeHook = func() { seen = true; cancel() }
				if phase == "registry close" {
					file.closeErr = os.ErrPermission
					want = StatusError
				}
				f.ops.openRegistry = func(string) (usbRegistryDirectory, error) { return file, nil }
				f.ops.openMetadata = func(string) (usbMetadataFile, error) {
					t.Fatal("metadata followed cancelled enumeration")
					return nil, nil
				}
			} else {
				target := filepath.Join(f.root, "sys/bus/usb/devices/1-1/bDeviceClass")
				if phase == "initial" {
					target = filepath.Join(f.root, "sys/bus/usb/devices/1-1/busnum")
				}
				if phase == "last optional" || phase == "last unauthorized" {
					target = filepath.Join(f.root, "sys/bus/usb/devices/usb1/maxchild")
				}
				if phase == "last unauthorized" {
					mustWrite(t, filepath.Join(f.root, "sys/bus/usb/devices/usb1/authorized"), "0\n")
				}
				f.ops.openMetadata = func(path string) (usbMetadataFile, error) {
					if seen {
						later++
					}
					if path != target {
						return open(path)
					}
					seen = true
					if phase == "absent optional" {
						cancel()
						return nil, os.ErrNotExist
					}
					file := usbMetadata("1\n")
					file.closeHook = cancel
					if phase == "optional close failure" {
						file.closeErr = os.ErrPermission
						want = StatusError
					}
					if phase == "optional read failure" {
						file.read = func(p []byte) (int, error) { cancel(); return copy(p, "1"), os.ErrPermission }
						want = StatusError
					}
					return file, nil
				}
			}
			got := runPlatformWith(ctx, Options{Root: f.root}, f.ops)
			if !seen || got.Status != want || later != 0 || len(f.opens) != 0 || got.ReadableDevices != 0 {
				t.Fatal("late cancellation/error continued or gained readiness")
			}
		})
	}
	// The initial authorization read must not satisfy a mandatory-tail witness.
	for _, phase := range []string{"last mandatory discovery identity", "no-root last mandatory discovery identity"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := newUSBRunFixture(t)
			selected := "usb1"
			if phase == "no-root last mandatory discovery identity" {
				selected = "1-2"
				if err := os.RemoveAll(filepath.Join(f.root, "sys/bus/usb/devices/usb1")); err != nil {
					t.Fatal(err)
				}
			}
			base := filepath.Join(f.root, "sys/bus/usb/devices", selected)
			open := f.ops.openMetadata
			acquired, closed, devAcquired, devClosed, initialAuthorization, mandatoryAuthorization, later := 0, 0, 0, 0, 0, 0, 0
			triggered := false
			f.ops.openMetadata = func(path string) (usbMetadataFile, error) {
				if triggered {
					later++
				}
				file, err := open(path)
				if nilUSBValue(file) {
					return file, err
				}
				acquired++
				isDev := path == filepath.Join(base, "dev")
				isAuthorization := path == filepath.Join(base, "authorized")
				if isDev {
					devAcquired++
				}
				if isAuthorization && devClosed == 0 {
					initialAuthorization++
				}
				arm := isAuthorization && devAcquired == 1 && devClosed == 1
				return &usbTrackedMetadata{usbMetadataFile: file, closeHook: func() {
					closed++
					if isDev {
						devClosed++
					}
					if arm {
						mandatoryAuthorization++
						triggered = true
						cancel()
					}
				}}, err
			}
			got := runPlatformWith(ctx, Options{Root: f.root}, f.ops)
			if got.Status != StatusAborted || !triggered || devAcquired != 1 || devClosed != 1 || initialAuthorization != 1 || mandatoryAuthorization != 1 || acquired == 0 || acquired != closed || later != 0 || len(f.opens) != 0 || got.ReadableDevices != 0 || got.USBTransfersGenerated {
				t.Fatal("mandatory discovery tail was not witnessed or cancellation permitted later work")
			}
		})
	}
}

func TestUSBRegistryProductionUsesBoundedDirectoryAndOrdinaryLinks(t *testing.T) {
	f := newUSBRunFixture(t)
	registry := filepath.Join(f.root, "sys/bus/usb/devices")
	if err := os.Rename(filepath.Join(registry, "1-1"), filepath.Join(f.root, "linked-device")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.root, "linked-device"), filepath.Join(registry, "1-1")); err != nil {
		t.Fatal(err)
	}
	file, err := f.ops.openRegistry(registry)
	if err != nil || nilUSBValue(file) {
		t.Fatal("owned registry could not be opened")
	}
	native, ok := file.(*os.File)
	if !ok {
		t.Fatal("production opener did not return an owned file")
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, native.Fd(), syscall.F_GETFL, 0)
	fdflags, _, fdErr := syscall.Syscall(syscall.SYS_FCNTL, native.Fd(), syscall.F_GETFD, 0)
	if closeErr := file.Close(); errno != 0 || fdErr != 0 || closeErr != nil || flags&syscall.O_ACCMODE != syscall.O_RDONLY || flags&syscall.O_DIRECTORY == 0 || flags&syscall.O_NONBLOCK == 0 || fdflags&syscall.FD_CLOEXEC == 0 {
		t.Fatal("registry descriptor flags or closure changed")
	}
	got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
	if got.Status != StatusPass || got.ReadableDevices != 3 || len(f.opens) != 3 {
		t.Fatal("ordinary sysfs device link was rejected")
	}
	for _, field := range []string{"bDeviceClass", "speed"} {
		path := filepath.Join(f.root, "linked-device", field)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(f.root, "linked-device/maxchild"), path); err != nil {
			t.Fatal(err)
		}
	}
	got = runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
	if got.Status != StatusError {
		t.Fatal("attribute symlink escaped nofollow policy")
	}
}

func TestUSBRegistryProductionRejectsOverflowAndOwnsDirectory(t *testing.T) {
	for _, count := range []int{0, 2048, 2049} {
		for _, closeFailure := range []bool{false, true} {
			f := newUSBRunFixture(t)
			file := usbRegistry(count)
			if closeFailure {
				file.closeErr = os.ErrPermission
			}
			f.ops.openRegistry = func(path string) (usbRegistryDirectory, error) {
				if path != filepath.Join(f.root, "sys/bus/usb/devices") {
					t.Fatal("unexpected registry path")
				}
				return file, nil
			}
			f.ops.openMetadata = func(string) (usbMetadataFile, error) {
				t.Fatal("unrelated names or failed registry acquired attributes")
				return nil, nil
			}
			got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
			want := StatusInconclusive
			if count > 2048 || closeFailure {
				want = StatusError
			}
			if got.Status != want || file.closes != 1 || len(f.opens) != 0 || got.ReadableDevices != 0 {
				t.Fatal("registry failure concealed or directory not owned")
			}
		}
	}
}

func TestUSBIncludedDeviceBoundIsIndependentOfRawRegistryBound(t *testing.T) {
	for _, count := range []int{512, 513} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			root := t.TempDir()
			values := make(map[string]string)
			nodes := make(map[string]*usbNodeFixtureFile)
			entries := make([]os.DirEntry, 0, count)
			for index := 0; index < count; index++ {
				bus, device := index/255+1, index%255+1
				name, class := fmt.Sprintf("%d-%d", bus, device-1), "00"
				if device == 1 {
					name, class = fmt.Sprintf("usb%d", bus), "09"
				}
				entries = append(entries, &usbRegistryEntry{name: name})
				for field, value := range map[string]string{"idVendor": "1234", "busnum": fmt.Sprint(bus), "devnum": fmt.Sprint(device), "authorized": "1", "bDeviceClass": class, "speed": "480", "maxchild": "0", "dev": fmt.Sprintf("189:%d", index)} {
					values[filepath.Join(root, "sys/bus/usb/devices", name, field)] = value + "\n"
				}
				// Explicit synthetic dev_t encoding is confined to this fixture.
				rdev := uint64(189<<8) | uint64(index&255) | uint64(index&^255)<<12
				nodes[filepath.Join(root, "dev/bus/usb", fmt.Sprintf("%03d", bus), fmt.Sprintf("%03d", device))] = &usbNodeFixtureFile{info: usbNodeInfo(rdev, uint64(index+1))}
			}
			directory := usbRegistry(0)
			offset := 0
			directory.read = func(n int) ([]os.DirEntry, error) {
				end := min(offset+n, len(entries))
				batch := entries[offset:end]
				offset = end
				if offset == len(entries) {
					return batch, io.EOF
				}
				return batch, nil
			}
			ops := realUSBNodeOperations()
			metadata := []*usbMetadataFixture{}
			opens := 0
			ops.openRegistry = func(string) (usbRegistryDirectory, error) { return directory, nil }
			ops.openMetadata = func(path string) (usbMetadataFile, error) {
				value, ok := values[path]
				if !ok {
					t.Fatal("unexpected metadata role")
				}
				file := usbMetadata(value)
				metadata = append(metadata, file)
				return file, nil
			}
			ops.lstat = func(path string) (os.FileInfo, error) {
				file := nodes[path]
				if file == nil {
					t.Fatal("unexpected node identity")
				}
				return file.info, nil
			}
			ops.open = func(path string, _ int, _ os.FileMode) (usbNodeFile, error) {
				opens++
				file := nodes[path]
				if file == nil {
					t.Fatal("unexpected node open")
				}
				return file, nil
			}
			got := runPlatformWith(t.Context(), Options{Root: root}, ops)
			if count == 512 {
				if got.Status != StatusPass || got.ReadableDevices != 512 || opens != 512 {
					t.Fatal("complete registry at the included-device bound rejected")
				}
			} else if got.Status != StatusError || got.Detail != "USB device count exceeds the supported limit" || opens != 0 || got.ReadableDevices != 0 {
				t.Fatal("included-device overflow reached node work")
			}
			if directory.closes != 1 || got.USBTransfersGenerated {
				t.Fatal("registry ownership or transfer scope changed")
			}
			for _, file := range metadata {
				if file.closes != 1 {
					t.Fatal("metadata handle escaped closure")
				}
			}
			for _, file := range nodes {
				want := 0
				if count == 512 {
					want = 1
				}
				if file.closes != want {
					t.Fatal("node ownership count changed")
				}
			}
		})
	}
}

func TestUSBAcquisitionRetainsEarlierDiagnosticsAfterLaterFault(t *testing.T) {
	f := newUSBRunFixture(t)
	open := f.ops.openMetadata
	fault := usbMetadata("4\n")
	fault.closeErr = os.ErrPermission
	f.ops.openMetadata = func(path string) (usbMetadataFile, error) {
		if path == filepath.Join(f.root, "sys/bus/usb/devices/1-2/maxchild") {
			return fault, nil
		}
		return open(path)
	}
	got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
	if got.Status != StatusError || got.Detail != errUSBMetadataClose.Error() || len(got.Devices) != 1 || got.Devices[0].Path != "1-1" || !got.Devices[0].AuthorizedKnown || !got.Devices[0].Authorized || got.Devices[0].Readable || got.AuthorizedDevices != 1 || got.ReadableDevices != 0 || len(f.opens) != 0 || fault.closes != 1 {
		t.Fatal("later discovery failure erased or promoted the earlier diagnostic")
	}
}
