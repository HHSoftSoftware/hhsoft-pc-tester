//go:build linux

package usbtest

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type usbDeviceNumber struct {
	major, minor uint32
}

type usbRegistryIdentity struct {
	number      usbDeviceNumber
	bus, device uint64
}

type usbNodeFile interface {
	Stat() (os.FileInfo, error)
	Close() error
}

type usbNodeOperations struct {
	lstat        func(string) (os.FileInfo, error)
	open         func(string, int, os.FileMode) (usbNodeFile, error)
	openMetadata func(string) (usbMetadataFile, error)
	openRegistry func(string) (usbRegistryDirectory, error)
}

func realUSBNodeOperations() usbNodeOperations {
	return usbNodeOperations{
		lstat: os.Lstat,
		open: func(path string, flags int, mode os.FileMode) (usbNodeFile, error) {
			return os.OpenFile(path, flags, mode)
		},
		openMetadata: func(path string) (usbMetadataFile, error) {
			return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		},
		openRegistry: func(path string) (usbRegistryDirectory, error) {
			return os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		},
	}
}

// The registry's generic dev attribute is authoritative. Bus/device numbers
// select its USBFS pathname; no inferred bus-to-minor formula is used.
func readUSBRegistryIdentity(ctx context.Context, root string, device DeviceResult, open func(string) (usbMetadataFile, error)) (usbRegistryIdentity, error) {
	identity := usbRegistryIdentity{}
	if !usbPathPattern.MatchString(device.Path) || !device.AuthorizedKnown || !device.Authorized {
		return identity, errUSBMetadataRead
	}
	values := make(map[string]string, 4)
	for _, name := range []string{"dev", "busnum", "devnum", "authorized"} {
		data, err := readUSBMetadata(ctx, filepath.Join(root, "sys/bus/usb/devices", device.Path, name), open)
		if err != nil {
			return identity, err
		}
		values[name] = strings.TrimSuffix(string(data), "\n")
	}
	parts := strings.Split(values["dev"], ":")
	if len(parts) != 2 {
		return identity, errUSBMetadataRead
	}
	major, majorOK := canonicalUSBNumber(parts[0], 32)
	minor, minorOK := canonicalUSBNumber(parts[1], 32)
	bus, busOK := canonicalUSBNumber(values["busnum"], 8)
	number, numberOK := canonicalUSBNumber(values["devnum"], 8)
	if !majorOK || !minorOK || major == 0 || !busOK || !numberOK || bus == 0 || number == 0 ||
		bus != device.BusNumber || number != device.DeviceNumber || values["authorized"] != "1" {
		return identity, errUSBMetadataRead
	}
	return usbRegistryIdentity{number: usbDeviceNumber{uint32(major), uint32(minor)}, bus: bus, device: number}, nil
}

func canonicalUSBNumber(value string, bits int) (uint64, bool) {
	number, err := strconv.ParseUint(value, 10, bits)
	return number, err == nil && strconv.FormatUint(number, 10) == value
}

type usbNodeInspection struct {
	present, readable bool
	issue             string
	err               error
}

func inspectUSBNode(ctx context.Context, root string, device DeviceResult, expected usbRegistryIdentity, operations usbNodeOperations) (result usbNodeInspection) {
	if nilUSBValue(ctx) || operations.lstat == nil || operations.open == nil || operations.openMetadata == nil {
		result.issue, result.err = "USB node dependencies are incomplete", errUSBMetadataRead
		return result
	}
	if ctx.Err() != nil {
		result.issue = "cancelled"
		return result
	}
	identity, err := readUSBRegistryIdentity(ctx, root, device, operations.openMetadata)
	if err != nil || identity != expected {
		result.issue = "USB registry identity changed"
		result.err = err
		return result
	}
	if ctx.Err() != nil {
		result.issue = "cancelled"
		return result
	}
	path := filepath.Join(root, "dev/bus/usb", leftPadUSBNumber(expected.bus), leftPadUSBNumber(expected.device))
	before, statErr := operations.lstat(path)
	if statErr != nil {
		result.issue = "USB device node is absent"
		return result
	}
	result.present = true
	if number, ok := directUSBNumber(before); !ok {
		result.issue = "USB device path is not a direct character node"
		return result
	} else if number != expected.number {
		result.issue = "USB device path is not the expected registry device"
		return result
	}
	if ctx.Err() != nil {
		result.issue = "cancelled"
		return result
	}
	file, openErr := operations.open(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if nilUSBValue(file) {
		result.issue = "USB device node cannot be opened read-only"
		return result
	}
	defer func() {
		if file.Close() != nil {
			result.readable = false
			result.issue, result.err = "USB device node closure could not be confirmed", errUSBNodeClose
			return
		}
		if result.err != nil {
			result.readable = false
			return
		}
		if ctx.Err() != nil {
			result.readable, result.issue = false, "cancelled"
		}
	}()
	if openErr != nil {
		result.issue = "USB device node cannot be opened read-only"
		return result
	}
	if ctx.Err() != nil {
		result.issue = "cancelled"
		return result
	}
	opened, statErr := file.Stat()
	if number, ok := directUSBNumber(opened); statErr != nil || !ok || number != expected.number || !sameUSBNode(before, opened) {
		result.issue = "opened node did not remain the expected USB device"
		return result
	}
	identity, err = readUSBRegistryIdentity(ctx, root, device, operations.openMetadata)
	if err != nil || identity != expected {
		result.issue, result.err = "USB registry identity changed", err
		return result
	}
	if ctx.Err() != nil {
		result.issue = "cancelled"
		return result
	}
	after, statErr := operations.lstat(path)
	if number, ok := directUSBNumber(after); statErr != nil || !ok || number != expected.number || !sameUSBNode(opened, after) {
		result.issue = "USB device path changed during inspection"
		return result
	}
	result.readable = true
	return result
}

func directUSBNumber(info os.FileInfo) (usbDeviceNumber, bool) {
	if nilUSBValue(info) || info.Mode()&os.ModeSymlink != 0 || info.Mode()&(os.ModeDevice|os.ModeCharDevice) != os.ModeDevice|os.ModeCharDevice {
		return usbDeviceNumber{}, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return usbDeviceNumber{}, false
	}
	value := uint64(stat.Rdev)
	return usbDeviceNumber{uint32((value>>8)&0xfff | (value>>32)&0xfffff000), uint32(value&0xff | (value>>12)&0xffffff00)}, true
}

func sameUSBNode(first, second os.FileInfo) bool {
	if _, ok := directUSBNumber(first); !ok {
		return false
	}
	if _, ok := directUSBNumber(second); !ok {
		return false
	}
	a, b := first.Sys().(*syscall.Stat_t), second.Sys().(*syscall.Stat_t)
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Rdev == b.Rdev
}
