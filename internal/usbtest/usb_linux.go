//go:build linux

package usbtest

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var usbPathPattern = regexp.MustCompile(`^(usb[0-9]+|[0-9]+-[0-9]+(\.[0-9]+)*)$`)
var usbSpeedPattern = regexp.MustCompile(`^[0-9]{1,6}(\.[0-9]{1,3})?$`)

func runPlatform(ctx context.Context, options Options) Result {
	return runPlatformWith(ctx, options, realUSBNodeOperations())
}

func runPlatformWith(ctx context.Context, options Options, operations usbNodeOperations) Result {
	return runWithNodeInspector(ctx, options, operations, func(ctx context.Context, root string, device DeviceResult, expected usbRegistryIdentity) usbNodeInspection {
		return inspectUSBNode(ctx, root, device, expected, operations)
	})
}

func runWithNodeInspector(ctx context.Context, options Options, operations usbNodeOperations, inspectNode func(context.Context, string, DeviceResult, usbRegistryIdentity) usbNodeInspection) Result {
	result := Result{Schema: Schema, Status: StatusError}
	if nilUSBValue(ctx) || operations.lstat == nil || operations.open == nil || operations.openMetadata == nil || operations.openRegistry == nil || inspectNode == nil {
		result.Detail = "USB readiness dependencies are incomplete"
		return result
	}
	if ctx.Err() != nil {
		result.Status = StatusAborted
		result.Detail = "cancelled"
		return result
	}
	if options.Root == "" {
		options.Root = "/"
	}
	root := filepath.Clean(options.Root)
	entries, err := readUSBRegistry(ctx, filepath.Join(root, "sys/bus/usb/devices"), operations.openRegistry)
	if err != nil {
		result.Detail = err.Error()
		if err == context.Canceled || err == context.DeadlineExceeded {
			result.Status = StatusAborted
		}
		return result
	}
	expected := make(map[string]usbRegistryIdentity)
	identities := make(map[usbDeviceNumber]bool)
	addresses := make(map[[2]uint64]bool)
	for _, name := range entries {
		if ctx.Err() != nil {
			result.Status = StatusAborted
			result.Detail = "cancelled"
			return result
		}
		device, include, inspectErr := inspectRegistryDevice(ctx, root, name, operations.openMetadata)
		if inspectErr != nil {
			result.Detail = inspectErr.Error()
			if inspectErr == context.Canceled || inspectErr == context.DeadlineExceeded {
				result.Status = StatusAborted
			}
			return result
		}
		if !include {
			continue
		}
		result.Devices = append(result.Devices, device)
		address := [2]uint64{device.BusNumber, device.DeviceNumber}
		if addresses[address] {
			result.Detail = "USB registry repeats a bus and device address"
			return result
		}
		addresses[address] = true
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
			identity, identityErr := readUSBRegistryIdentity(ctx, root, device, operations.openMetadata)
			if identityErr != nil {
				result.Devices[len(result.Devices)-1].ReadinessIssue = identityErr.Error()
				result.Detail = identityErr.Error()
				if identityErr == context.Canceled || identityErr == context.DeadlineExceeded {
					result.Status = StatusAborted
				}
				return result
			}
			if identities[identity.number] {
				result.Detail = "USB registry repeats a device identity"
				return result
			}
			identities[identity.number] = true
			expected[device.Path] = identity
		}
	}
	if ctx.Err() != nil {
		result.Status, result.Detail = StatusAborted, "cancelled"
		return result
	}
	sort.Slice(result.Devices, func(i, j int) bool { return result.Devices[i].Path < result.Devices[j].Path })
	if result.Controllers == 0 {
		result.Status = StatusInconclusive
		result.Detail = "no USB root hubs were discovered"
		return result
	}
	for index := range result.Devices {
		if ctx.Err() != nil {
			result.Status, result.Detail = StatusAborted, "cancelled"
			return result
		}
		device := &result.Devices[index]
		if !device.Authorized {
			if !device.AuthorizedKnown {
				device.ReadinessIssue = "USB authorization state is unavailable"
			} else {
				device.ReadinessIssue = "USB device is not authorized"
			}
			continue
		}
		inspection := inspectNode(ctx, root, *device, expected[device.Path])
		device.NodePresent, device.Readable, device.ReadinessIssue = inspection.present, inspection.readable, inspection.issue
		if inspection.err != nil && inspection.err != context.Canceled && inspection.err != context.DeadlineExceeded {
			device.Readable = false
			result.Detail = inspection.err.Error()
			return result
		}
		if ctx.Err() != nil || inspection.err == context.Canceled || inspection.err == context.DeadlineExceeded {
			device.Readable, device.ReadinessIssue = false, "cancelled"
			result.Status, result.Detail = StatusAborted, "cancelled"
			return result
		}
		if device.Readable {
			result.ReadableDevices++
		}
	}
	if ctx.Err() != nil {
		result.Status = StatusAborted
		result.Detail = "cancelled"
		return result
	}
	if result.AuthorizedDevices != len(result.Devices) || result.ReadableDevices != result.AuthorizedDevices {
		result.Status = StatusInconclusive
		result.Detail = "one or more USB devices are unauthorized or not read-only accessible"
		return result
	}
	result.Status = StatusPass
	return result
}

func inspectRegistryDevice(ctx context.Context, root, name string, open func(string) (usbMetadataFile, error)) (DeviceResult, bool, error) {
	if !usbPathPattern.MatchString(name) {
		return DeviceResult{}, false, nil
	}
	base := filepath.Join(root, "sys/bus/usb/devices", name)
	// A matching device entry cannot disappear merely because its required
	// vendor observation is missing or unreadable. Its value is not exported.
	if _, err := readUSBMetadata(ctx, filepath.Join(base, "idVendor"), open); err != nil {
		return DeviceResult{}, false, err
	}
	values := make(map[string]string, 2)
	for _, field := range []string{"busnum", "devnum"} {
		data, err := readUSBMetadata(ctx, filepath.Join(base, field), open)
		if err != nil {
			return DeviceResult{}, false, err
		}
		values[field] = strings.TrimSpace(string(data))
	}
	busNumber, busKnown := usbUnsignedValue(values["busnum"], 255)
	deviceNumber, deviceKnown := usbUnsignedValue(values["devnum"], 255)
	if !busKnown || !deviceKnown || busNumber == 0 || deviceNumber == 0 {
		return DeviceResult{}, false, errors.New("USB registry contains an invalid bus or device number")
	}
	optional := make(map[string]string, 4)
	for _, field := range []string{"authorized", "bDeviceClass", "speed", "maxchild"} {
		data, err := acquireUSBMetadata(ctx, filepath.Join(base, field), open)
		if err == errUSBMetadataAbsent {
			continue
		}
		if err != nil {
			return DeviceResult{}, false, err
		}
		optional[field] = strings.TrimSpace(string(data))
	}
	classText := strings.TrimPrefix(strings.ToLower(optional["bDeviceClass"]), "0x")
	class, classErr := strconv.ParseUint(classText, 16, 8)
	classKnown := classErr == nil
	kind := "device"
	if strings.HasPrefix(name, "usb") {
		kind = "root_hub"
	} else if classKnown && class == 9 {
		kind = "hub"
	}
	result := DeviceResult{Path: name, Kind: kind, BusNumber: busNumber, DeviceNumber: deviceNumber}
	if classKnown {
		result.Class = "0x" + leftPad(strconv.FormatUint(class, 16), 2)
	}
	if authorization, known := usbUnsignedValue(optional["authorized"], 1); known {
		result.AuthorizedKnown, result.Authorized = true, authorization == 1
	}
	if speed := optional["speed"]; usbSpeedPattern.MatchString(speed) {
		result.LinkSpeedMbps = speed
	}
	if ports, portsKnown := usbUnsignedValue(optional["maxchild"], 255); portsKnown {
		result.DownstreamPorts = ports
	}
	return result, true, nil
}

// Preserve the initial observation grammar. Authorized devices still undergo
// the separate canonical identity rechecks before and after their node opens.
func usbUnsignedValue(value string, maximum uint64) (uint64, bool) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	return parsed, err == nil && parsed <= maximum
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
