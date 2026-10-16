//go:build linux

package usbtest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

const maximumUSBDevices = 512

var usbPathPattern = regexp.MustCompile(`^(usb[0-9]+|[0-9]+-[0-9]+(\.[0-9]+)*)$`)
var usbSpeedPattern = regexp.MustCompile(`^[0-9]{1,6}(\.[0-9]{1,3})?$`)

func runPlatform(ctx context.Context, options Options) Result {
	result := Result{Schema: Schema, Status: StatusError}
	if ctx.Err() != nil {
		result.Status = StatusAborted
		result.Detail = "cancelled"
		return result
	}
	if options.Root == "" {
		options.Root = "/"
	}
	root := filepath.Clean(options.Root)
	entries, err := os.ReadDir(filepath.Join(root, "sys/bus/usb/devices"))
	if err != nil {
		result.Detail = "cannot read USB device registry"
		return result
	}
	if len(entries) > maximumUSBDevices*4 {
		result.Detail = "USB registry exceeds the supported limit"
		return result
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			result.Status = StatusAborted
			result.Detail = "cancelled"
			return result
		}
		device, include, inspectErr := inspectRegistryDevice(root, entry.Name())
		if inspectErr != nil {
			result.Detail = inspectErr.Error()
			return result
		}
		if !include {
			continue
		}
		result.Devices = append(result.Devices, device)
		if len(result.Devices) > maximumUSBDevices {
			result.Detail = "USB device count exceeds the supported limit"
			return result
		}
		if device.Kind == "root_hub" {
			result.Controllers++
			result.RootPorts += device.DownstreamPorts
		}
		if device.Authorized {
			result.AuthorizedDevices++
		}
	}
	sort.Slice(result.Devices, func(i, j int) bool { return result.Devices[i].Path < result.Devices[j].Path })
	if result.Controllers == 0 {
		result.Status = StatusInconclusive
		result.Detail = "no USB root hubs were discovered"
		return result
	}
	for index := range result.Devices {
		device := &result.Devices[index]
		if !device.Authorized {
			if !device.AuthorizedKnown {
				device.ReadinessIssue = "USB authorization state is unavailable"
			} else {
				device.ReadinessIssue = "USB device is not authorized"
			}
			continue
		}
		device.NodePresent, device.Readable, device.ReadinessIssue = inspectUSBNode(root, device.BusNumber, device.DeviceNumber)
		if device.Readable {
			result.ReadableDevices++
		}
	}
	if ctx.Err() != nil {
		result.Status = StatusAborted
		result.Detail = "cancelled"
		return result
	}
	if result.ReadableDevices != result.AuthorizedDevices {
		result.Status = StatusInconclusive
		result.Detail = "one or more USB devices are unauthorized or not read-only accessible"
		return result
	}
	result.Status = StatusPass
	return result
}

func inspectRegistryDevice(root, name string) (DeviceResult, bool, error) {
	if !usbPathPattern.MatchString(name) {
		return DeviceResult{}, false, nil
	}
	base := filepath.Join(root, "sys/bus/usb/devices", name)
	if !fileExists(filepath.Join(base, "idVendor")) {
		return DeviceResult{}, false, nil
	}
	busNumber, busKnown := readUnsigned(filepath.Join(base, "busnum"), 255)
	deviceNumber, deviceKnown := readUnsigned(filepath.Join(base, "devnum"), 255)
	if !busKnown || !deviceKnown || busNumber == 0 || deviceNumber == 0 {
		return DeviceResult{}, false, errors.New("USB registry contains an invalid bus or device number")
	}
	class, _ := readHexadecimal(filepath.Join(base, "bDeviceClass"), 255)
	kind := "device"
	if strings.HasPrefix(name, "usb") {
		kind = "root_hub"
	} else if class == 9 {
		kind = "hub"
	}
	result := DeviceResult{Path: name, Kind: kind, Class: "0x" + strings.ToLower(strings.TrimPrefix(readValue(base, "bDeviceClass"), "0x")), BusNumber: busNumber, DeviceNumber: deviceNumber}
	if result.Class == "0x" {
		result.Class = ""
	}
	if authorized, authorizedKnown := readUnsigned(filepath.Join(base, "authorized"), 1); authorizedKnown {
		result.AuthorizedKnown = true
		result.Authorized = authorized == 1
	}
	if speed := readValue(base, "speed"); usbSpeedPattern.MatchString(speed) {
		result.LinkSpeedMbps = speed
	}
	if ports, portsKnown := readUnsigned(filepath.Join(base, "maxchild"), 255); portsKnown {
		result.DownstreamPorts = ports
	}
	return result, true, nil
}

func inspectUSBNode(root string, busNumber, deviceNumber uint64) (bool, bool, string) {
	devicePath := filepath.Join(root, "dev/bus/usb", leftPadUSBNumber(busNumber), leftPadUSBNumber(deviceNumber))
	metadata, err := os.Lstat(devicePath)
	if err != nil {
		return false, false, "USB device node is absent"
	}
	if metadata.Mode()&os.ModeSymlink != 0 || metadata.Mode()&os.ModeCharDevice == 0 {
		return true, false, "USB device path is not a direct character node"
	}
	file, err := os.OpenFile(devicePath, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return true, false, "USB device node cannot be opened read-only"
	}
	openedMetadata, statErr := file.Stat()
	_ = file.Close()
	if statErr != nil || openedMetadata.Mode()&os.ModeCharDevice == 0 || !sameDevice(metadata, openedMetadata) {
		return true, false, "opened node did not remain the expected USB device"
	}
	return true, true, ""
}

func sameDevice(first, second os.FileInfo) bool {
	firstStat, firstOK := first.Sys().(*syscall.Stat_t)
	secondStat, secondOK := second.Sys().(*syscall.Stat_t)
	return firstOK && secondOK && firstStat.Rdev == secondStat.Rdev
}

func leftPadUSBNumber(value uint64) string {
	return leftPad(strconv.FormatUint(value, 10), 3)
}

func leftPad(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return strings.Repeat("0", width-len(value)) + value
}

func readValue(base, relative string) string {
	value, _ := readSmallValue(filepath.Join(base, relative))
	return value
}

func readUnsigned(path string, maximum uint64) (uint64, bool) {
	value, ok := readSmallValue(path)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	return parsed, err == nil && parsed <= maximum
}

func readHexadecimal(path string, maximum uint64) (uint64, bool) {
	value, ok := readSmallValue(path)
	if !ok {
		return 0, false
	}
	value = strings.TrimPrefix(strings.ToLower(value), "0x")
	parsed, err := strconv.ParseUint(value, 16, 64)
	return parsed, err == nil && parsed <= maximum
}

func readSmallValue(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 256 {
		return "", false
	}
	value := strings.TrimSpace(string(data))
	return value, value != ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
