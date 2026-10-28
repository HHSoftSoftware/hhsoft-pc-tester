//go:build linux

package usbtest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type usbNodeFixtureFile struct {
	info                os.FileInfo
	statErr, closeErr   error
	statHook, closeHook func()
	stats, closes       int
}

func (file *usbNodeFixtureFile) Stat() (os.FileInfo, error) {
	file.stats++
	if file.statHook != nil {
		file.statHook()
	}
	return file.info, file.statErr
}

func (file *usbNodeFixtureFile) Close() error {
	file.closes++
	if file.closeHook != nil {
		file.closeHook()
	}
	return file.closeErr
}

func usbNodeInfo(rdev, inode uint64) os.FileInfo {
	return usbInfo{mode: os.ModeDevice | os.ModeCharDevice, sys: &syscall.Stat_t{Rdev: rdev, Dev: 3, Ino: inode}}
}

type usbNodeFixture struct {
	t                 *testing.T
	root              string
	row               DeviceResult
	expected          usbRegistryIdentity
	before, after     os.FileInfo
	file              *usbNodeFixtureFile
	returned          usbNodeFile
	lstatErr, openErr error
	openHook          func()
	lstats, opens     int
	metadataHook      func(string)
	metadataCloseErr  error
	metadataCloses    int
}

func newUSBNodeFixture(t *testing.T) *usbNodeFixture {
	t.Helper()
	f := &usbNodeFixture{t: t, root: t.TempDir(), row: DeviceResult{Path: "usb1", BusNumber: 1, DeviceNumber: 1, AuthorizedKnown: true, Authorized: true}, expected: usbRegistryIdentity{number: usbDeviceNumber{189, 0}, bus: 1, device: 1}}
	mustUSBDevice(t, f.root, "usb1", "1", "1", "09", "1", "4")
	f.before, f.after = usbNodeInfo(0xbd00, 7), usbNodeInfo(0xbd00, 7)
	f.file = &usbNodeFixtureFile{info: usbNodeInfo(0xbd00, 7)}
	f.returned = f.file
	return f
}

type usbTrackedMetadata struct {
	usbMetadataFile
	closeHook func()
	closeErr  error
}

func (file *usbTrackedMetadata) Close() error {
	err := file.usbMetadataFile.Close()
	if file.closeHook != nil {
		file.closeHook()
	}
	return errors.Join(err, file.closeErr)
}

func (f *usbNodeFixture) operations() usbNodeOperations {
	ops := realUSBNodeOperations()
	openMetadata := ops.openMetadata
	ops.lstat = func(path string) (os.FileInfo, error) {
		if path != filepath.Join(f.root, "dev/bus/usb/001/001") {
			f.t.Fatal("unexpected USBFS address")
		}
		f.lstats++
		if f.lstats == 1 {
			return f.before, f.lstatErr
		}
		return f.after, f.lstatErr
	}
	ops.open = func(path string, flags int, mode os.FileMode) (usbNodeFile, error) {
		if path != filepath.Join(f.root, "dev/bus/usb/001/001") || flags != os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOFOLLOW || mode != 0 {
			f.t.Fatal("unsafe node open contract")
		}
		f.opens++
		if f.openHook != nil {
			f.openHook()
		}
		return f.returned, f.openErr
	}
	ops.openMetadata = func(path string) (usbMetadataFile, error) {
		if f.metadataHook != nil {
			f.metadataHook(path)
		}
		file, err := openMetadata(path)
		if nilUSBValue(file) {
			return nil, err
		}
		return &usbTrackedMetadata{usbMetadataFile: file, closeHook: func() { f.metadataCloses++ }, closeErr: f.metadataCloseErr}, err
	}
	return ops
}

func TestUSBNodeRequiresRegistryPathAndDescriptorIdentity(t *testing.T) {
	for _, test := range []struct {
		name          string
		change        func(*usbNodeFixture)
		opens, closes int
	}{
		{"unrelated character device", func(f *usbNodeFixture) { f.before = usbNodeInfo(0x103, 7); f.file.info = usbNodeInfo(0x103, 7) }, 0, 0},
		{"wrong expected registry", func(f *usbNodeFixture) { f.expected.number.minor = 1 }, 0, 0},
		{"registry disappeared", func(f *usbNodeFixture) {
			if err := os.Remove(filepath.Join(f.root, "sys/bus/usb/devices/usb1/dev")); err != nil {
				t.Fatal(err)
			}
		}, 0, 0},
		{"wrong descriptor", func(f *usbNodeFixture) { f.file.info = usbNodeInfo(0xbd01, 7) }, 1, 1},
		{"replaced filesystem", func(f *usbNodeFixture) {
			f.file.info = usbInfo{mode: os.ModeDevice | os.ModeCharDevice, sys: &syscall.Stat_t{Rdev: 0xbd00, Dev: 4, Ino: 7}}
		}, 1, 1},
		{"replaced inode", func(f *usbNodeFixture) { f.file.info = usbNodeInfo(0xbd00, 8) }, 1, 1},
		{"late path symlink", func(f *usbNodeFixture) { f.after = usbInfo{mode: os.ModeSymlink} }, 1, 1},
		{"late path replacement", func(f *usbNodeFixture) { f.after = usbNodeInfo(0xbd00, 8) }, 1, 1},
		{"late registry replacement", func(f *usbNodeFixture) {
			f.file.statHook = func() { mustWrite(t, filepath.Join(f.root, "sys/bus/usb/devices/usb1/dev"), "189:1\n") }
		}, 1, 1},
		{"late bus replacement", func(f *usbNodeFixture) {
			f.file.statHook = func() { mustWrite(t, filepath.Join(f.root, "sys/bus/usb/devices/usb1/busnum"), "2\n") }
		}, 1, 1},
		{"late address replacement", func(f *usbNodeFixture) {
			f.file.statHook = func() { mustWrite(t, filepath.Join(f.root, "sys/bus/usb/devices/usb1/devnum"), "2\n") }
		}, 1, 1},
		{"late authorization change", func(f *usbNodeFixture) {
			f.file.statHook = func() { mustWrite(t, filepath.Join(f.root, "sys/bus/usb/devices/usb1/authorized"), "0\n") }
		}, 1, 1},
		{"nil info", func(f *usbNodeFixture) { f.before = nil }, 0, 0},
		{"typed nil info", func(f *usbNodeFixture) { var info *usbInfo; f.before = info }, 0, 0},
		{"typed nil syscall metadata", func(f *usbNodeFixture) {
			var stat *syscall.Stat_t
			f.before = usbInfo{mode: os.ModeDevice | os.ModeCharDevice, sys: stat}
		}, 0, 0},
		{"unprovable metadata", func(f *usbNodeFixture) { f.file.info = usbInfo{mode: os.ModeDevice | os.ModeCharDevice} }, 1, 1},
		{"block node", func(f *usbNodeFixture) { f.before = usbInfo{mode: os.ModeDevice, sys: &syscall.Stat_t{Rdev: 0xbd00}} }, 0, 0},
		{"symlink", func(f *usbNodeFixture) { f.before = usbInfo{mode: os.ModeSymlink} }, 0, 0},
		{"lstat error", func(f *usbNodeFixture) { f.lstatErr = os.ErrPermission }, 0, 0},
		{"nofollow insertion", func(f *usbNodeFixture) { f.returned = nil; f.openErr = syscall.ELOOP }, 1, 0},
		{"nil handle", func(f *usbNodeFixture) { f.returned = nil }, 1, 0},
		{"typed nil handle", func(f *usbNodeFixture) { var file *usbNodeFixtureFile; f.returned = file }, 1, 0},
		{"handle plus open error", func(f *usbNodeFixture) { f.openErr = os.ErrPermission }, 1, 1},
		{"stat error", func(f *usbNodeFixture) { f.file.statErr = os.ErrPermission }, 1, 1},
		{"close error", func(f *usbNodeFixture) { f.file.closeErr = os.ErrPermission }, 1, 1},
		{"metadata close error", func(f *usbNodeFixture) { f.metadataCloseErr = os.ErrPermission }, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newUSBNodeFixture(t)
			test.change(f)
			got := inspectUSBNode(t.Context(), f.root, f.row, f.expected, f.operations())
			if got.readable || got.issue == "" || f.opens != test.opens || f.file.closes != test.closes {
				t.Fatalf("node was credited or ownership changed: %+v opens=%d closes=%d", got, f.opens, f.file.closes)
			}
			if f.openErr != nil && f.file.stats != 0 {
				t.Fatal("failed open was inspected")
			}
			if f.file.closeErr != nil && got.err != errUSBNodeClose {
				t.Fatal("node close failure lost precedence")
			}
			if f.metadataCloseErr != nil && got.err != errUSBMetadataClose {
				t.Fatal("metadata close failure lost precedence")
			}
		})
	}
	f := newUSBNodeFixture(t)
	got := inspectUSBNode(t.Context(), f.root, f.row, f.expected, f.operations())
	if !got.present || !got.readable || got.issue != "" || got.err != nil || f.opens != 1 || f.file.closes != 1 || f.metadataCloses != 8 || f.lstats != 2 {
		t.Fatalf("valid minor-zero node rejected: %+v", got)
	}
}

func TestUSBNodeCancellationAndCloseErrorPrecedence(t *testing.T) {
	for _, stage := range []string{"before", "metadata", "open", "open error", "stat", "close"} {
		for _, closeFailure := range []bool{false, true} {
			t.Run(stage+map[bool]string{false: "/clean", true: "/close error"}[closeFailure], func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				f := newUSBNodeFixture(t)
				switch stage {
				case "before":
					cancel()
				case "metadata":
					f.metadataHook = func(string) { cancel() }
				case "open":
					f.openHook = cancel
				case "open error":
					f.openHook = cancel
					f.openErr = os.ErrPermission
				case "stat":
					f.file.statHook = cancel
				case "close":
					f.file.closeHook = cancel
				}
				if closeFailure {
					f.file.closeErr = os.ErrPermission
				}
				got := inspectUSBNode(ctx, f.root, f.row, f.expected, f.operations())
				if got.readable {
					t.Fatal("cancelled node credited")
				}
				if f.file.closes != f.opens || f.opens > 1 {
					t.Fatal("cancelled acquired descriptor leaked or reclosed")
				}
				if closeFailure && f.opens == 1 && got.err != errUSBNodeClose {
					t.Fatal("cancellation concealed close failure")
				}
				if stage == "before" && f.metadataCloses != 0 {
					t.Fatal("pre-cancelled inspector acquired metadata")
				}
			})
		}
	}
}

func TestUSBRegistryIdentityIsExactAndDoesNotBorrowParent(t *testing.T) {
	for _, test := range []struct{ name, value string }{
		{"dev", ""}, {"dev", "189"}, {"dev", "189:00\n"}, {"dev", "189:0\n\n"}, {"dev", "189:0\r\n"}, {"dev", "+189:0"}, {"dev", "0:0"}, {"dev", "4294967296:0"}, {"dev", strings.Repeat("1", 257)},
		{"busnum", "2\n"}, {"busnum", "01\n"}, {"devnum", "2\n"}, {"devnum", "0\n"}, {"authorized", "0\n"},
	} {
		t.Run(test.name+"/"+strings.ReplaceAll(test.value, "\n", "LF"), func(t *testing.T) {
			f := newUSBNodeFixture(t)
			mustWrite(t, filepath.Join(f.root, "sys/bus/usb/devices/usb1", test.name), test.value)
			if _, err := readUSBRegistryIdentity(t.Context(), f.root, f.row, f.operations().openMetadata); err == nil {
				t.Fatal("invalid association accepted")
			}
		})
	}
	f := newUSBNodeFixture(t)
	if err := os.Remove(filepath.Join(f.root, "sys/bus/usb/devices/usb1/dev")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(f.root, "sys/bus/usb/devices/dev"), "189:0\n")
	if _, err := readUSBRegistryIdentity(t.Context(), f.root, f.row, f.operations().openMetadata); err == nil {
		t.Fatal("borrowed parent identity")
	}
}

func TestUSBRegistryIdentityPreservesOrdinarySysfsLinks(t *testing.T) {
	f := newUSBNodeFixture(t)
	old := filepath.Join(f.root, "sys/bus/usb/devices/usb1")
	target := filepath.Join(f.root, "sys/devices/usb1")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(old, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../devices/usb1", old); err != nil {
		t.Fatal(err)
	}
	got, err := readUSBRegistryIdentity(t.Context(), f.root, f.row, f.operations().openMetadata)
	if err != nil || got != f.expected {
		t.Fatal("ordinary registry link rejected")
	}
	if err := os.Remove(filepath.Join(target, "dev")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(target, "replacement"), "189:0\n")
	if err := os.Symlink("replacement", filepath.Join(target, "dev")); err != nil {
		t.Fatal(err)
	}
	if _, err := readUSBRegistryIdentity(t.Context(), f.root, f.row, f.operations().openMetadata); err == nil {
		t.Fatal("symlink attribute accepted")
	}
}

func TestUSBDeviceNumberDecoderRetainsHighBits(t *testing.T) {
	for _, test := range []struct {
		encoded  uint64
		expected usbDeviceNumber
	}{
		{0xbd00, usbDeviceNumber{189, 0}}, {0xbd01, usbDeviceNumber{189, 1}}, {0x12345000678abcef, usbDeviceNumber{0x12345abc, 0x678ef}},
	} {
		got, ok := directUSBNumber(usbNodeInfo(test.encoded, 1))
		if !ok || got != test.expected {
			t.Fatalf("device-number bits changed: %+v", got)
		}
	}
}

func TestUSBRealOpenFlagsOnOwnedHarmlessFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned")
	if err := os.WriteFile(path, []byte("owned metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	ops := realUSBNodeOperations()
	for _, open := range []func() (usbNodeFile, error){
		func() (usbNodeFile, error) {
			return ops.open(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		},
		func() (usbNodeFile, error) { return ops.openMetadata(path) },
	} {
		handle, err := open()
		if err != nil {
			t.Fatal(err)
		}
		file, ok := handle.(*os.File)
		if !ok {
			t.Fatal("real opener did not return owned file")
		}
		connection, err := file.SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		var flags, descriptor uintptr
		var first, second syscall.Errno
		controlErr := connection.Control(func(fd uintptr) {
			flags, _, first = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
			descriptor, _, second = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0)
		})
		closeErr := handle.Close()
		if controlErr != nil || first != 0 || second != 0 || closeErr != nil || flags&syscall.O_ACCMODE != syscall.O_RDONLY || flags&syscall.O_NONBLOCK == 0 || descriptor&syscall.FD_CLOEXEC == 0 {
			t.Fatal("real read-only ownership flags differ")
		}
	}
	link := filepath.Join(filepath.Dir(path), "link")
	if err := os.Symlink("owned", link); err != nil {
		t.Fatal(err)
	}
	file, err := ops.open(link, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if !nilUSBValue(file) {
		_ = file.Close()
		t.Fatal("final node symlink opened")
	}
	if !errors.Is(err, syscall.ELOOP) {
		t.Fatal("no-follow failure was not observed")
	}
}
