//go:build linux

package inputtest

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"unsafe"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func runKeyboardPlatform(ctx context.Context, options KeyboardOptions) KeyboardResult {
	return runKeyboardWith(ctx, options, keyboardBackend{discover: discoverKeyboardInterfaces, open: openKeyboardInterface})
}

func discoverKeyboardInterfaces(ctx context.Context, root string) (interfaces []string, err error) {
	if err := pointerContextError(ctx); err != nil {
		return nil, err
	}
	width := inventory.ObservedInputBitmapWordBits()
	if width != 32 && width != 64 {
		return nil, errors.New("unknown input bitmap layout")
	}
	file, err := openInputPath(root, []string{"proc", "bus", "input", "devices"}, syscall.O_RDONLY, true)
	if err != nil {
		return nil, err
	}
	defer func() {
		if file.Close() != nil {
			interfaces, err = nil, errKeyboardCleanup
		}
	}()
	metadata, err := file.Stat()
	if err != nil || !metadata.Mode().IsRegular() {
		return nil, errors.New("invalid input registry")
	}
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, errors.New("input registry exceeds limit")
	}
	return parseFunctionalInputInterfaces(strings.NewReader(string(raw)), "keyboard", width)
}

type keyboardMetadata struct {
	identity     KeyboardIdentity
	capabilities KeyboardCapabilities
	events, keys []byte
}

func keyboardRequest(query func(uintptr, []byte) (int, error), number byte, data []byte) (int, error) {
	return query(uintptr(0x80000000|uint32(len(data))<<16|uint32('E')<<8|uint32(number)), data)
}

func queryKeyboardMetadata(query func(uintptr, []byte) (int, error)) (keyboardMetadata, error) {
	var result keyboardMetadata
	result.capabilities.FixedKeys = []string{}
	version, identity := make([]byte, 4), make([]byte, 8)
	if count, err := keyboardRequest(query, 0x01, version); err != nil || count != 0 || binary.NativeEndian.Uint32(version) != 0x010001 {
		return result, errors.New("unsupported input interface")
	}
	if count, err := keyboardRequest(query, 0x02, identity); err != nil || count != 0 {
		return result, errors.New("input identity unavailable")
	}
	result.identity = KeyboardIdentity{binary.NativeEndian.Uint16(identity), binary.NativeEndian.Uint16(identity[2:]), binary.NativeEndian.Uint16(identity[4:]), binary.NativeEndian.Uint16(identity[6:])}
	result.events, result.keys = make([]byte, 8), make([]byte, 96)
	if count, err := keyboardRequest(query, 0x20, result.events); err != nil || count < 1 || count > len(result.events) || result.events[0]&1 == 0 {
		return result, errors.New("input event capabilities unavailable")
	}
	result.capabilities.EventKey = result.events[0]&2 != 0
	if result.capabilities.EventKey {
		if count, err := keyboardRequest(query, 0x21, result.keys); err != nil || count < 14 || count > len(result.keys) {
			return result, errors.New("input key capabilities unavailable")
		}
		names := KeyboardActions()
		for index, code := range keyboardCodes {
			if result.keys[code/8]&(1<<uint(code%8)) != 0 {
				result.capabilities.FixedKeys = append(result.capabilities.FixedKeys, names[index])
			}
		}
	}
	return result, nil
}

func queryKeyboardNeutral(query func(uintptr, []byte) (int, error)) (bool, error) {
	keys := make([]byte, 96)
	if count, err := keyboardRequest(query, 0x18, keys); err != nil || count < 14 || count > len(keys) {
		return false, errors.New("input key state unavailable")
	}
	for _, code := range keyboardCodes {
		if keys[code/8]&(1<<uint(code%8)) != 0 {
			return false, nil
		}
	}
	return true, nil
}

func openKeyboardInterface(ctx context.Context, root, name string) (result keyboardOpened, err error) {
	if !pointerInterface(name) || pointerContextError(ctx) != nil {
		return result, errors.New("invalid keyboard interface")
	}
	expected, err := inputSysfsDevice(root, name, true)
	if err != nil {
		return result, err
	}
	file, err := openInputPath(root, []string{"dev", "input", name}, syscall.O_RDONLY, true)
	if err != nil {
		return result, err
	}
	// Transfer close ownership even when subsequent metadata validation fails.
	result.close = file.Close
	metadata, err := file.Stat()
	if err != nil {
		return result, err
	}
	actual, valid := pointerDeviceNumber(metadata)
	if !valid || actual != expected {
		return result, errors.New("keyboard descriptor identity mismatch")
	}
	query := func(request uintptr, data []byte) (int, error) {
		count, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), request, uintptr(unsafe.Pointer(&data[0])))
		if errno != 0 {
			return 0, errno
		}
		return int(count), nil
	}
	observed, err := queryKeyboardMetadata(query)
	if err != nil {
		return result, err
	}
	validate := func() error {
		current, err := os.Lstat(filepath.Join(root, "dev", "input", name))
		if err != nil || !os.SameFile(metadata, current) || current.Mode()&os.ModeCharDevice == 0 || current.Mode()&os.ModeSymlink != 0 {
			return errors.New("keyboard path changed")
		}
		device, err := inputSysfsDevice(root, name, true)
		if err != nil {
			return err
		}
		if device != expected {
			return errors.New("keyboard sysfs changed")
		}
		latest, err := queryKeyboardMetadata(query)
		if err != nil || !reflect.DeepEqual(latest, observed) {
			return errors.New("keyboard identity or capabilities changed")
		}
		return nil
	}
	if err := validate(); err != nil {
		return result, err
	}
	result.identity, result.capabilities = observed.identity, observed.capabilities
	result.eventSize, result.typeOffset = int(unsafe.Sizeof(inputEvent{})), int(unsafe.Offsetof(inputEvent{}.Type))
	result.validate, result.neutral = validate, func() (bool, error) { return queryKeyboardNeutral(query) }
	result.read = func(buffer []byte) (int, error) {
		count, err := syscall.Read(int(file.Fd()), buffer)
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			return 0, errKeyboardIdle
		}
		if errors.Is(err, syscall.EINTR) {
			return 0, errKeyboardRetry
		}
		return count, err
	}
	return result, nil
}
