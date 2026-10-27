//go:build linux

package inputtest

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func runPointerPlatform(ctx context.Context, options PointerOptions) PointerResult {
	return runPointerWith(ctx, options, pointerBackend{discover: discoverPointerInterfaces, open: openPointerInterface})
}

func openPointerPath(root string, parts []string, flags int) (*os.File, error) {
	return openInputPath(root, parts, flags, false)
}

// The keyboard owner also accounts for temporary metadata/directory handles.
// The pointer caller retains its established error behavior.
func openInputPath(root string, parts []string, flags int, strictCleanup bool) (*os.File, error) {
	directory, err := syscall.Open(filepath.Clean(root), syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for index, part := range parts {
		mode := syscall.O_RDONLY | syscall.O_DIRECTORY
		if index == len(parts)-1 {
			mode = flags
		}
		next, openErr := syscall.Openat(directory, part, mode|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		closeErr := syscall.Close(directory)
		if strictCleanup && closeErr != nil {
			if openErr == nil {
				_ = syscall.Close(next)
			}
			return nil, errKeyboardCleanup
		}
		if openErr != nil {
			return nil, openErr
		}
		directory = next
	}
	return os.NewFile(uintptr(directory), "pointer interface"), nil
}

func discoverPointerInterfaces(ctx context.Context, root string) ([]string, error) {
	if err := pointerContextError(ctx); err != nil {
		return nil, err
	}
	file, err := openPointerPath(root, []string{"proc", "bus", "input", "devices"}, syscall.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	metadata, err := file.Stat()
	if err != nil || !metadata.Mode().IsRegular() {
		return nil, errors.New("invalid pointer registry file")
	}
	encoded, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(encoded) > 1<<20 {
		return nil, errors.New("pointer registry exceeds its read limit")
	}
	return parsePointerInterfaces(strings.NewReader(string(encoded)))
}

func parsePointerInterfaces(reader io.Reader) ([]string, error) {
	return parseFunctionalInputInterfaces(reader, "pointer", 0)
}

func parseFunctionalInputInterfaces(reader io.Reader, wanted string, wordBits int) ([]string, error) {
	var result []string
	seen := map[string]bool{}
	fields := map[string]string{}
	flush := func() error {
		category := inventory.ClassifyInput(fields["name"], fields["handlers"], fields["KEY"], fields["REL"], fields["ABS"], fields["PROP"])
		if wanted == "keyboard" {
			category = inventory.ClassifyInputWithBitmapWidth(fields["name"], fields["handlers"], fields["KEY"], fields["REL"], fields["ABS"], fields["PROP"], wordBits)
		}
		hasEvent := false
		for _, handler := range strings.Fields(fields["handlers"]) {
			if !strings.HasPrefix(handler, "event") {
				continue
			}
			if !pointerInterface(handler) || seen[handler] {
				return errors.New("invalid or duplicate input event interface")
			}
			seen[handler] = true
			hasEvent = true
			if category == wanted {
				result = append(result, handler)
				if len(result) > maximumPointerDevices {
					return errors.New("pointer registry exceeds device limit")
				}
			}
		}
		if category == wanted && !hasEvent {
			return errors.New("pointer registry record has no event interface")
		}
		fields = map[string]string{}
		return nil
	}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		var key, value string
		switch {
		case strings.HasPrefix(line, "N: Name="):
			key, value = "name", strings.Trim(strings.TrimPrefix(line, "N: Name="), "\"")
		case strings.HasPrefix(line, "H: Handlers="):
			key, value = "handlers", strings.TrimPrefix(line, "H: Handlers=")
		case strings.HasPrefix(line, "B: "):
			key, value, _ = strings.Cut(strings.TrimPrefix(line, "B: "), "=")
		}
		if key != "" {
			if _, exists := fields[key]; exists {
				return nil, errors.New("duplicate input registry field")
			}
			fields[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	sort.Strings(result)
	return result, nil
}

func pointerSysfsDevice(root, name string) (string, error) {
	return inputSysfsDevice(root, name, false)
}

func inputSysfsDevice(root, name string, strictCleanup bool) (value string, err error) {
	sysRoot := filepath.Join(filepath.Clean(root), "sys")
	rootMetadata, err := os.Lstat(sysRoot)
	if err != nil || !rootMetadata.IsDir() || rootMetadata.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("invalid pointer sysfs root")
	}
	path, err := filepath.EvalSymlinks(filepath.Join(sysRoot, "class", "input", name, "dev"))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(sysRoot, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("pointer sysfs identity escapes its root")
	}
	file, err := openInputPath(sysRoot, strings.Split(relative, string(filepath.Separator)), syscall.O_RDONLY, strictCleanup)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := file.Close(); strictCleanup && closeErr != nil {
			value, err = "", errKeyboardCleanup
		}
	}()
	metadata, err := file.Stat()
	if err != nil || !metadata.Mode().IsRegular() {
		return "", errors.New("invalid pointer sysfs identity")
	}
	encoded, err := io.ReadAll(io.LimitReader(file, 129))
	if err != nil || len(encoded) > 128 {
		return "", errors.New("invalid pointer device number")
	}
	value = strings.TrimSpace(string(encoded))
	majorText, minorText, found := strings.Cut(value, ":")
	major, majorErr := strconv.ParseUint(majorText, 10, 32)
	minor, minorErr := strconv.ParseUint(minorText, 10, 32)
	if !found || majorErr != nil || minorErr != nil || value != strconv.FormatUint(major, 10)+":"+strconv.FormatUint(minor, 10) {
		return "", errors.New("invalid pointer device number")
	}
	return value, nil
}

func pointerDeviceNumber(metadata os.FileInfo) (string, bool) {
	if metadata.Mode()&os.ModeCharDevice == 0 || metadata.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	stat, ok := metadata.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	device := uint64(stat.Rdev)
	major := (device >> 8 & 0xfff) | (device >> 32 & 0xfffff000)
	minor := (device & 0xff) | (device >> 12 & 0xffffff00)
	if major != 13 || minor < 64 || minor > 95 && minor < 256 {
		return "", false
	}
	return strconv.FormatUint(major, 10) + ":" + strconv.FormatUint(minor, 10), true
}

func openPointerInterface(ctx context.Context, root, name string) (pointerOpened, error) {
	var result pointerOpened
	if !pointerInterface(name) || pointerContextError(ctx) != nil {
		return result, errors.New("invalid pointer interface request")
	}
	expected, err := pointerSysfsDevice(root, name)
	if err != nil {
		return result, err
	}
	file, err := openPointerPath(root, []string{"dev", "input", name}, syscall.O_RDONLY)
	if err != nil {
		return result, err
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = file.Close()
		}
	}()
	metadata, err := file.Stat()
	if err != nil {
		return result, err
	}
	actual, valid := pointerDeviceNumber(metadata)
	if !valid || actual != expected {
		return result, errors.New("pointer descriptor does not match sysfs identity")
	}
	query := func(request uintptr, bytes []byte) (int, error) {
		count, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), request, uintptr(unsafe.Pointer(&bytes[0])))
		if errno != 0 {
			return 0, errno
		}
		return int(count), nil
	}
	identity, capabilities, err := queryPointerMetadata(query)
	if err != nil {
		return result, err
	}
	validate := func() error {
		current, err := os.Lstat(filepath.Join(root, "dev", "input", name))
		if err != nil || !os.SameFile(metadata, current) || current.Mode()&os.ModeCharDevice == 0 || current.Mode()&os.ModeSymlink != 0 {
			return errors.New("pointer device node changed")
		}
		deviceNumber, err := pointerSysfsDevice(root, name)
		if err != nil || deviceNumber != expected {
			return errors.New("pointer sysfs identity changed")
		}
		currentIdentity, currentCapabilities, err := queryPointerMetadata(query)
		if err != nil || identity != currentIdentity || !reflect.DeepEqual(capabilities, currentCapabilities) {
			return errors.New("pointer device identity or capabilities changed")
		}
		return nil
	}
	if err := validate(); err != nil {
		return result, err
	}
	result = pointerOpened{identity: identity, capabilities: capabilities,
		eventSize: int(unsafe.Sizeof(inputEvent{})), typeOffset: int(unsafe.Offsetof(inputEvent{}.Type)),
		validate: validate, close: file.Close,
		read: func(buffer []byte) (int, error) {
			count, err := syscall.Read(int(file.Fd()), buffer)
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				return 0, errPointerIdle
			}
			if errors.Is(err, syscall.EINTR) {
				return 0, errPointerRetry
			}
			return count, err
		}}
	accepted = true
	return result, nil
}

// Read-only Linux input UAPI requests from include/uapi/linux/input.h. No
// grab, clock, key-map, or other device-setting ioctl is issued.
func queryPointerMetadata(query func(uintptr, []byte) (int, error)) (PointerIdentity, PointerCapabilities, error) {
	var identity PointerIdentity
	capabilities := PointerCapabilities{Buttons: []string{}, WheelAxes: []PointerWheelCapability{}}
	request := func(number byte, bytes []byte) (int, error) {
		return query(uintptr(0x80000000|uint32(len(bytes))<<16|uint32('E')<<8|uint32(number)), bytes)
	}
	version := make([]byte, 4)
	if count, err := request(0x01, version); err != nil || count != 0 || binary.NativeEndian.Uint32(version) != 0x010001 {
		return identity, capabilities, errors.New("descriptor is not a supported evdev interface")
	}
	id := make([]byte, 8)
	if count, err := request(0x02, id); err != nil || count != 0 {
		return identity, capabilities, errors.New("cannot establish evdev numeric identity")
	}
	identity = PointerIdentity{binary.NativeEndian.Uint16(id), binary.NativeEndian.Uint16(id[2:]), binary.NativeEndian.Uint16(id[4:]), binary.NativeEndian.Uint16(id[6:])}
	events, keys, relative := make([]byte, 8), make([]byte, 96), make([]byte, 8)
	if count, err := request(0x20, events); err != nil || count < 1 || count > len(events) {
		return identity, capabilities, errors.New("cannot establish evdev event capabilities")
	}
	capabilities.Key, capabilities.Relative, capabilities.Absolute = events[0]&2 != 0, events[0]&4 != 0, events[0]&8 != 0
	if capabilities.Key {
		if count, err := request(0x21, keys); err != nil || count < 35 || count > len(keys) {
			return identity, capabilities, errors.New("cannot establish evdev primary buttons")
		}
		for index, name := range pointerPrimaryButtons {
			code := 0x110 + index
			if keys[code/8]&(1<<uint(code%8)) != 0 {
				capabilities.Buttons = append(capabilities.Buttons, name)
			}
		}
	}
	if capabilities.Relative {
		if count, err := request(0x22, relative); err != nil || count < 2 || count > len(relative) {
			return identity, capabilities, errors.New("cannot establish evdev relative axes")
		}
		capabilities.RelativeX, capabilities.RelativeY = relative[0]&1 != 0, relative[0]&2 != 0
		for _, wheel := range []PointerWheelCapability{
			{Axis: "vertical", Legacy: relative[1]&1 != 0, HighResolution: relative[1]&8 != 0},
			{Axis: "horizontal", Legacy: relative[0]&64 != 0, HighResolution: relative[1]&16 != 0},
		} {
			if wheel.Legacy || wheel.HighResolution {
				capabilities.WheelAxes = append(capabilities.WheelAxes, wheel)
			}
		}
	}
	return identity, capabilities, nil
}
