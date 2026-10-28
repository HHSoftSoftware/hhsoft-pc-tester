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

func keyboardMetadataFixture(request uintptr, data []byte) (int, error) {
	switch request {
	case 0x80044501:
		binary.NativeEndian.PutUint32(data, 0x010001)
		return 0, nil
	case 0x80084502:
		for index, value := range []uint16{3, 0x1234, 0x5678, 0x0100} {
			binary.NativeEndian.PutUint16(data[index*2:], value)
		}
		return 0, nil
	case 0x80084520:
		data[0] = 3
	case 0x80604521:
		for _, code := range []int{42, 54, 103, 108, 105, 106} {
			data[code/8] |= 1 << uint(code%8)
		}
	case 0x80604518:
	default:
		return 0, errors.New("unexpected request")
	}
	return len(data), nil
}

func TestKeyboardLinuxQueriesOnlyBoundedReadOnlyNumericState(t *testing.T) {
	var requests []uintptr
	query := func(request uintptr, data []byte) (int, error) {
		requests = append(requests, request)
		return keyboardMetadataFixture(request, data)
	}
	metadata, err := queryKeyboardMetadata(query)
	if err != nil || metadata.identity != (KeyboardIdentity{3, 0x1234, 0x5678, 0x0100}) || !supportedKeyboard(metadata.capabilities) {
		t.Fatal("valid numeric identity/capabilities rejected")
	}
	neutral, err := queryKeyboardNeutral(query)
	if err != nil || !neutral || !reflect.DeepEqual(requests, []uintptr{0x80044501, 0x80084502, 0x80084520, 0x80604521, 0x80604518}) {
		t.Fatal("input was not queried with the closed read-only request set")
	}
	for _, code := range []int{42, 54, 103, 108, 105, 106} {
		neutral, err := queryKeyboardNeutral(func(request uintptr, data []byte) (int, error) {
			count, err := keyboardMetadataFixture(request, data)
			data[code/8] |= 1 << uint(code%8)
			return count, err
		})
		if err != nil || neutral {
			t.Fatalf("held fixed control %d ignored", code)
		}
	}
	neutral, err = queryKeyboardNeutral(func(request uintptr, data []byte) (int, error) {
		count, err := keyboardMetadataFixture(request, data)
		data[30/8] |= 1 << uint(30%8)
		return count, err
	})
	if err != nil || !neutral {
		t.Fatal("unrelated printable key was incorrectly included in fixed scope")
	}
}

func TestKeyboardLinuxMetadataRejectsIncompleteObservation(t *testing.T) {
	for _, condition := range []string{"version error", "id error", "events error", "keys error", "wrong version", "id count", "missing syn", "no key", "short events", "long events", "short keys", "long keys", "missing control"} {
		t.Run(condition, func(t *testing.T) {
			metadata, err := queryKeyboardMetadata(func(request uintptr, data []byte) (int, error) {
				count, err := keyboardMetadataFixture(request, data)
				switch {
				case condition == "version error" && request == 0x80044501, condition == "id error" && request == 0x80084502, condition == "events error" && request == 0x80084520, condition == "keys error" && request == 0x80604521:
					return 0, syscall.ENOTTY
				case condition == "wrong version" && request == 0x80044501:
					binary.NativeEndian.PutUint32(data, 0)
				case condition == "id count" && request == 0x80084502:
					count = 1
				case condition == "missing syn" && request == 0x80084520:
					data[0] &^= 1
				case condition == "no key" && request == 0x80084520:
					data[0] &^= 2
				case condition == "short events" && request == 0x80084520:
					count = 0
				case condition == "long events" && request == 0x80084520:
					count = 9
				case condition == "short keys" && request == 0x80604521:
					count = 13
				case condition == "long keys" && request == 0x80604521:
					count = 97
				case condition == "missing control" && request == 0x80604521:
					data[42/8] &^= 1 << uint(42%8)
				}
				return count, err
			})
			if err == nil && supportedKeyboard(metadata.capabilities) {
				t.Fatal("incomplete capabilities supplied functional support")
			}
		})
	}
	for _, count := range []int{-1, 0, 13, 97} {
		if _, err := queryKeyboardNeutral(func(uintptr, []byte) (int, error) { return count, nil }); err == nil {
			t.Fatal("unbounded or truncated key state accepted")
		}
	}
	if _, err := queryKeyboardNeutral(func(uintptr, []byte) (int, error) { return 0, syscall.ENODEV }); err == nil {
		t.Fatal("disconnected key-state query accepted")
	}
}

func TestKeyboardLinuxRegistryPreservesInterfacesAndKernelWidth(t *testing.T) {
	for _, item := range []struct {
		width  int
		bitmap string
	}{{32, "2001000 0"}, {64, "200100000000000"}} {
		registry := "N: Name=\"Generic HID\"\nH: Handlers=kbd event2 event7\nB: KEY=" + item.bitmap + "\n\nN: Name=\"Fixture Mouse\"\nH: Handlers=mouse0 event9\n"
		interfaces, err := parseFunctionalInputInterfaces(strings.NewReader(registry), "keyboard", item.width)
		if err != nil || !reflect.DeepEqual(interfaces, []string{"event2", "event7"}) {
			t.Fatal("generic keyboard interfaces lost across kernel bitmap widths")
		}
		for _, extra := range []string{"\nN: Name=\"Keyboard\"\nH: Handlers=kbd\n", "\nN: Name=\"Keyboard\"\nH: Handlers=kbd event2\n", "\nN: Name=\"Keyboard\"\nH: Handlers=kbd event01\n", "\nN: Name=\"Keyboard\"\nH: Handlers=kbd event3\nH: Handlers=kbd event4\n"} {
			if _, err := parseFunctionalInputInterfaces(strings.NewReader(registry+extra), "keyboard", item.width); err == nil {
				t.Fatal("ambiguous keyboard registry accepted")
			}
		}
	}
	var many strings.Builder
	for index := 0; index <= 128; index++ {
		fmt.Fprintf(&many, "N: Name=\"Keyboard\"\nH: Handlers=kbd event%d\n\n", index)
	}
	if _, err := parseFunctionalInputInterfaces(strings.NewReader(many.String()), "keyboard", 64); err == nil {
		t.Fatal("too many keyboards silently truncated")
	}
}

func TestKeyboardLinuxRejectsNonEvdevNodesAndRetainsCloseOwnership(t *testing.T) {
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
				pointerFixtureFile(t, root, "dev/input/event0", "unchanged")
			case "directory":
				if err := os.Mkdir(node, 0700); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(node, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := pointerFixtureFile(t, root, "fixture", "unchanged")
				if err := os.Symlink(target, node); err != nil {
					t.Fatal(err)
				}
			}
			owned, err := openKeyboardInterface(t.Context(), root, "event0")
			if owned.close != nil {
				if closeErr := owned.close(); closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			if err == nil {
				t.Fatal("non-evdev node accepted")
			}
			if owned.close != nil {
				t.Fatal("rejected acquisition transferred an already-closed owner")
			}
			if kind == "regular" {
				data, err := os.ReadFile(node)
				if err != nil || string(data) != "unchanged" {
					t.Fatal("read-only node modified")
				}
			}
		})
	}
}

func TestKeyboardLinuxNativeLayoutAndContainedDiscovery(t *testing.T) {
	size, offset := int(unsafe.Sizeof(inputEvent{})), int(unsafe.Offsetof(inputEvent{}.Type))
	want := 24
	if runtime.GOARCH == "386" {
		want = 16
	}
	if size != want || offset != want-8 {
		t.Fatal("unsupported Linux event layout")
	}
	result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: time.Second}, keyboardFixtureBackend(completeKeyboardSource(size)))
	if result.Status != StatusPass {
		t.Fatal("native record layout did not complete inert fixed controls")
	}
	root := t.TempDir()
	path := pointerFixtureFile(t, root, "proc/bus/input/devices", "N: Name=\"Fixture Keyboard\"\nH: Handlers=kbd event2\n")
	interfaces, err := discoverKeyboardInterfaces(t.Context(), root)
	if err != nil || !reflect.DeepEqual(interfaces, []string{"event2"}) {
		t.Fatal("contained inert registry rejected")
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", (1<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverKeyboardInterfaces(t.Context(), root); err == nil {
		t.Fatal("oversized registry accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := discoverKeyboardInterfaces(ctx, root); err == nil {
		t.Fatal("cancelled discovery performed work")
	}
}
