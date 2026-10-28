//go:build linux

package inputtest

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func pointerMetadataFixture(request uintptr, bytes []byte) (int, error) {
	switch request {
	case 0x80044501:
		binary.NativeEndian.PutUint32(bytes, 0x010001)
		return 0, nil
	case 0x80084502:
		for index, value := range []uint16{3, 0x1234, 0x5678, 0x0102} {
			binary.NativeEndian.PutUint16(bytes[index*2:], value)
		}
		return 0, nil
	case 0x80084520:
		bytes[0] = 7
	case 0x80604521:
		bytes[34] = 7
	case 0x80084522:
		bytes[0] = 3
	default:
		return 0, errors.New("unexpected or mutating ioctl")
	}
	return len(bytes), nil
}

func TestPointerMetadataUsesOnlyReadOnlyIdentityAndCapabilityRequests(t *testing.T) {
	var requests []uintptr
	identity, capabilities, err := queryPointerMetadata(func(request uintptr, bytes []byte) (int, error) {
		requests = append(requests, request)
		return pointerMetadataFixture(request, bytes)
	})
	if err != nil || identity != (PointerIdentity{3, 0x1234, 0x5678, 0x0102}) || !supportedPointer(capabilities) || !reflect.DeepEqual(capabilities.Buttons, []string{"left", "right", "middle"}) {
		t.Fatal("descriptor-derived identity or advertised criteria were lost")
	}
	if !reflect.DeepEqual(requests, []uintptr{0x80044501, 0x80084502, 0x80084520, 0x80604521, 0x80084522}) {
		t.Fatal("device-mutating or identity-string ioctl requested")
	}
}

func TestPointerMetadataFailuresAndUnsupportedCapabilitiesDoNotPass(t *testing.T) {
	for _, failed := range []uintptr{0x80044501, 0x80084502, 0x80084520, 0x80604521, 0x80084522} {
		_, _, err := queryPointerMetadata(func(request uintptr, bytes []byte) (int, error) {
			if request == failed {
				return 0, syscall.ENOTTY
			}
			return pointerMetadataFixture(request, bytes)
		})
		if err == nil {
			t.Fatal("failed ioctl accepted")
		}
	}
	for _, kind := range []string{"absolute", "missing left", "missing x", "missing y", "no key", "no relative", "wrong version", "short keys", "empty events", "short relative"} {
		t.Run(kind, func(t *testing.T) {
			_, capabilities, err := queryPointerMetadata(func(request uintptr, bytes []byte) (int, error) {
				count, err := pointerMetadataFixture(request, bytes)
				switch {
				case request == 0x80044501 && kind == "wrong version":
					binary.NativeEndian.PutUint32(bytes, 0)
				case request == 0x80084520 && kind == "absolute":
					bytes[0] |= 8
				case request == 0x80084520 && kind == "no key":
					bytes[0] &^= 2
				case request == 0x80084520 && kind == "no relative":
					bytes[0] &^= 4
				case request == 0x80084520 && kind == "empty events":
					count = 0
				case request == 0x80604521 && kind == "missing left":
					bytes[34] = 2
				case request == 0x80604521 && kind == "short keys":
					count = 34
				case request == 0x80084522 && kind == "missing x":
					bytes[0] = 2
				case request == 0x80084522 && kind == "missing y":
					bytes[0] = 1
				case request == 0x80084522 && kind == "short relative":
					count = 0
				}
				return count, err
			})
			if err == nil && supportedPointer(capabilities) {
				t.Fatal("unverified capabilities accepted")
			}
		})
	}
}

func TestPointerRegistryKeepsAllInterfacesAndRejectsAmbiguity(t *testing.T) {
	registry := "N: Name=\"Fixture Mouse\"\nH: Handlers=mouse0 event2 event7\n\nN: Name=\"Fixture Keyboard\"\nH: Handlers=kbd event9\n"
	interfaces, err := parsePointerInterfaces(strings.NewReader(registry))
	if err != nil || !reflect.DeepEqual(interfaces, []string{"event2", "event7"}) {
		t.Fatal("pointer interfaces were silently omitted")
	}
	for _, extra := range []string{
		"\nN: Name=\"Second Mouse\"\nH: Handlers=mouse1\n",
		"\nN: Name=\"Second Mouse\"\nH: Handlers=mouse1 event2\n",
		"\nN: Name=\"Second Mouse\"\nH: Handlers=mouse1 event01\n",
		"\nN: Name=\"Second Mouse\"\nH: Handlers=mouse1 event3\nH: Handlers=mouse2 event4\n",
	} {
		if _, err := parsePointerInterfaces(strings.NewReader(registry + extra)); err == nil {
			t.Fatal("incomplete or ambiguous pointer registry accepted")
		}
	}
	var large strings.Builder
	for index := 0; index <= maximumPointerDevices; index++ {
		fmt.Fprintf(&large, "N: Name=\"Fixture Mouse\"\nH: Handlers=mouse%d event%d\n\n", index, index)
	}
	if _, err := parsePointerInterfaces(strings.NewReader(large.String())); err == nil {
		t.Fatal("pointer registry was silently truncated")
	}
}

func pointerFixtureFile(t *testing.T, root, relative, value string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPointerLinuxEventRecordLayout(t *testing.T) {
	size, offset := int(unsafe.Sizeof(inputEvent{})), int(unsafe.Offsetof(inputEvent{}.Type))
	want := 24
	if runtime.GOARCH == "386" {
		want = 16
	}
	if size != want || offset != want-8 {
		t.Fatal("unsupported Linux event ABI")
	}
	source := completePointerSource(size)
	result := runPointerWith(t.Context(), PointerOptions{Duration: time.Second}, pointerFixtureBackend(source))
	if result.Status != StatusPass {
		t.Fatal("native event layout did not complete synthetic pointer criteria")
	}
}

func TestPointerPathsAreReadOnlyNonblockingAndDoNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	path := pointerFixtureFile(t, root, "dev/input/event0", "fixture bytes remain unchanged")
	file, err := openPointerPath(root, []string{"dev", "input", "event0"}, syscall.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_GETFL, 0)
	fdFlags, _, fdErr := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_GETFD, 0)
	if errno != 0 || fdErr != 0 || flags&syscall.O_ACCMODE != syscall.O_RDONLY || flags&syscall.O_NONBLOCK == 0 || fdFlags&syscall.FD_CLOEXEC == 0 {
		t.Fatal("descriptor lacks read-only, nonblocking, or close-on-exec guard")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(root, "dev/input/event1")); err != nil {
		t.Fatal(err)
	}
	if file, err := openPointerPath(root, []string{"dev", "input", "event1"}, syscall.O_RDONLY); err == nil {
		file.Close()
		t.Fatal("symlink event node followed")
	}
	if err := os.Symlink(filepath.Join(root, "dev"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if file, err := openPointerPath(root, []string{"alias", "input", "event0"}, syscall.O_RDONLY); err == nil {
		file.Close()
		t.Fatal("symlink parent followed")
	}
	value, err := os.ReadFile(path)
	if err != nil || string(value) != "fixture bytes remain unchanged" {
		t.Fatal("read-only fixture was changed")
	}
}

func TestPointerSysfsIdentityIsBoundedAndContained(t *testing.T) {
	root := t.TempDir()
	path := pointerFixtureFile(t, root, "sys/class/input/event0/dev", "13:64\n")
	if value, err := pointerSysfsDevice(root, "event0"); err != nil || value != "13:64" {
		t.Fatal("valid synthetic sysfs identity rejected")
	}
	for _, invalid := range []string{"13:064", "13:+64", "13:64:1", "13:64\n1", strings.Repeat("1", 129)} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := pointerSysfsDevice(root, "event0"); err == nil {
			t.Fatal("malformed or oversized identity accepted")
		}
	}
	outside := pointerFixtureFile(t, root, "outside/dev", "13:65")
	if err := os.MkdirAll(filepath.Join(root, "sys/class/input/event1"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "sys/class/input/event1/dev")); err != nil {
		t.Fatal(err)
	}
	if _, err := pointerSysfsDevice(root, "event1"); err == nil {
		t.Fatal("escaping sysfs identity followed")
	}
	inside := pointerFixtureFile(t, root, "sys/devices/fixture/event2/dev", "13:66")
	if err := os.Symlink(filepath.Join("..", "..", "devices", "fixture", "event2"), filepath.Join(root, "sys/class/input/event2")); err != nil {
		t.Fatal(err)
	}
	if value, err := pointerSysfsDevice(root, "event2"); err != nil || value != "13:66" {
		t.Fatal("contained normal sysfs link rejected")
	}
	for name, target := range map[string]string{"event3": filepath.Dir(inside), "event4": filepath.Join("..", "..", "..", "outside")} {
		if err := os.Symlink(target, filepath.Join(root, "sys/class/input", name)); err != nil {
			t.Fatal(err)
		}
		if _, err := pointerSysfsDevice(root, name); err == nil {
			t.Fatal("absolute or escaping relative sysfs identity followed")
		}
	}
}

func TestPointerRegularDirectoryFIFOAndSymlinkNodesCannotBecomeEvdev(t *testing.T) {
	for _, kind := range []string{"regular", "directory", "fifo", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			pointerFixtureFile(t, root, "sys/class/input/event0/dev", "13:64")
			node := filepath.Join(root, "dev/input/event0")
			if err := os.MkdirAll(filepath.Dir(node), 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "regular":
				pointerFixtureFile(t, root, "dev/input/event0", "not evdev")
			case "directory":
				if err := os.Mkdir(node, 0700); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(node, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := pointerFixtureFile(t, root, "target", "not evdev")
				if err := os.Symlink(target, node); err != nil {
					t.Fatal(err)
				}
			}
			done := make(chan error, 1)
			go func() {
				source, err := openPointerInterface(t.Context(), root, "event0")
				if err == nil {
					source.close()
				}
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("non-evdev descriptor accepted")
				}
			case <-time.After(time.Second):
				// Release a mistakenly blocking FIFO open, then join the helper.
				if kind == "fifo" {
					writer, err := os.OpenFile(node, os.O_WRONLY|syscall.O_NONBLOCK, 0)
					if err == nil {
						writer.Close()
					}
				}
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("fixture open did not stop")
				}
				t.Fatal("nonblocking node rejection exceeded its bound")
			}
		})
	}
}

func TestPointerRegistryReadLimitAndCancelledDiscovery(t *testing.T) {
	root := t.TempDir()
	path := pointerFixtureFile(t, root, "proc/bus/input/devices", "N: Name=\"Fixture Mouse\"\nH: Handlers=mouse0 event0\n")
	interfaces, err := discoverPointerInterfaces(t.Context(), root)
	if err != nil || !reflect.DeepEqual(interfaces, []string{"event0"}) {
		t.Fatal("bounded fixture discovery failed")
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", (1<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverPointerInterfaces(t.Context(), root); err == nil {
		t.Fatal("oversized registry accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := discoverPointerInterfaces(ctx, filepath.Join(root, "missing")); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled discovery inspected paths")
	}
}

type pointerStatFixture struct {
	mode   os.FileMode
	device uint64
}

func (info pointerStatFixture) Name() string       { return "event0" }
func (info pointerStatFixture) Size() int64        { return 0 }
func (info pointerStatFixture) Mode() os.FileMode  { return info.mode }
func (info pointerStatFixture) ModTime() time.Time { return time.Time{} }
func (info pointerStatFixture) IsDir() bool        { return info.mode.IsDir() }
func (info pointerStatFixture) Sys() any {
	value := &syscall.Stat_t{}
	value.Rdev = info.device
	return value
}

func TestPointerDescriptorDeviceNumberUsesActualMajorAndMinor(t *testing.T) {
	for _, example := range []struct {
		major, minor uint64
		valid        bool
	}{{13, 64, true}, {13, 95, true}, {13, 256, true}, {13, 4096, true}, {13, 63, false}, {13, 96, false}, {13, 255, false}, {1, 64, false}} {
		device := example.minor&0xff | example.major&0xfff<<8 | (example.minor&0xffffff00)<<12 | (example.major&0xfffff000)<<32
		value, valid := pointerDeviceNumber(pointerStatFixture{os.ModeDevice | os.ModeCharDevice, device})
		if valid != example.valid || valid && value != fmt.Sprintf("%d:%d", example.major, example.minor) {
			t.Fatal("wrong event-device major/minor accepted")
		}
	}
	if _, valid := pointerDeviceNumber(pointerStatFixture{0, 0x0d40}); valid {
		t.Fatal("regular file metadata accepted as an event node")
	}
}

// Legacy path-only assertions remain keyboard helper coverage. The pointer
// producer now uses the separately tested metadata-only acquisition path.
func openPointerPath(root string, parts []string, flags int) (*os.File, error) {
	return openInputPath(root, parts, flags)
}

func pointerSysfsDevice(root, name string) (value string, err error) {
	operations := systemPointerOperations()
	owner, err := openPointerRoots(context.Background(), root, true, operations)
	if owner != nil {
		defer func() {
			if closeErr := owner.close(); closeErr != nil {
				value, err = "", closeErr
			}
		}()
	}
	if err != nil {
		return "", err
	}
	number, err := inputRegistryNumber(context.Background(), owner.sys, operations.input, name)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", number.major, number.minor), nil
}
