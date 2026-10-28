//go:build linux

package storage

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
)

var blockInterfacePattern = regexp.MustCompile(`^(sd[a-z]+|hd[a-z]+|vd[a-z]+|xvd[a-z]+|sr[0-9]+|fd[0-9]+|mmcblk[0-9]+|nvme[0-9]+(c[0-9]+)?n[0-9]+)$`)

type mountState = safemode.MountState

func runPlatform(ctx context.Context, options Options) Result {
	return runPlatformWith(ctx, options, realBlockNodeOperations())
}

type blockNodeFile interface {
	Stat() (os.FileInfo, error)
	Close() error
}

type blockNodeOperations struct {
	lstat        func(string) (os.FileInfo, error)
	open         func(string, int, os.FileMode) (blockNodeFile, error)
	deviceNumber func(string) (deviceNumber, bool)
	openRegistry func(string) (blockRegistryDirectory, error)
}

func realBlockNodeOperations() blockNodeOperations {
	return blockNodeOperations{
		lstat: os.Lstat,
		open: func(path string, flags int, mode os.FileMode) (blockNodeFile, error) {
			return os.OpenFile(path, flags, mode)
		},
		deviceNumber: readDeviceNumber,
		openRegistry: openBlockRegistry,
	}
}

func openBlockRegistry(path string) (blockRegistryDirectory, error) {
	directory, err := os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return directory, nil
}

func runPlatformWith(ctx context.Context, options Options, operations blockNodeOperations) Result {
	result := Result{Schema: Schema, Status: StatusError}
	if ctx == nil || operations.lstat == nil || operations.open == nil || operations.deviceNumber == nil || operations.openRegistry == nil {
		result.Detail = "storage readiness dependencies are incomplete"
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
	mounts, err := readMounts(filepath.Join(root, "proc/self/mountinfo"))
	for _, state := range mounts {
		if state.Writable {
			result.WritableMounts++
		}
	}
	if err != nil {
		result.Detail = "cannot verify block mount safety"
		return result
	}
	entries, err := readBlockRegistry(ctx, filepath.Join(root, "sys/class/block"), operations.openRegistry)
	if err != nil {
		switch err {
		case errBlockRegistryLimit:
			result.Detail = "block device registry exceeds the supported limit"
		case context.Canceled, context.DeadlineExceeded:
			result.Status = StatusAborted
			result.Detail = "cancelled"
		default:
			result.Detail = "cannot read block device registry"
		}
		return result
	}
	if ctx.Err() != nil {
		result.Status = StatusAborted
		result.Detail = "cancelled"
		return result
	}
	registeredDevices := make(map[deviceNumber]struct{})
	registeredMountNumbers := make(map[string]bool)
	expectedDevices := make(map[string]deviceNumber)
	for _, entry := range entries {
		if ctx.Err() != nil {
			result.Status = StatusAborted
			result.Detail = "cancelled"
			return result
		}
		name := entry.Name()
		if !validInterfaceName(name) {
			result.Detail = "block device registry contains an invalid interface"
			return result
		}
		number, ok := operations.deviceNumber(filepath.Join(root, "sys/class/block", name, "dev"))
		if !ok {
			result.Detail = "block device registry lacks a canonical device number"
			return result
		}
		state := mounts[number.String()]
		if _, duplicate := registeredDevices[number]; duplicate {
			result.Detail = "block device registry repeats a device number"
			return result
		}
		registeredDevices[number] = struct{}{}
		registeredMountNumbers[number.String()] = true
		expectedDevices[name] = number
		if ctx.Err() != nil {
			result.Status = StatusAborted
			result.Detail = "cancelled"
			return result
		}
		device, include, deviceErr := inspectRegistryDevice(root, name, state)
		if deviceErr != nil {
			result.Detail = deviceErr.Error()
			return result
		}
		if !include {
			continue
		}
		result.Devices = append(result.Devices, device)
		if device.Kind != "media" {
			continue
		}
		result.EligibleDevices++
		if result.MediaBytes <= math.MaxUint64-device.SizeBytes {
			result.MediaBytes += device.SizeBytes
		} else {
			result.Detail = "aggregate block capacity exceeds the supported limit"
			return result
		}
	}
	sort.Slice(result.Devices, func(i, j int) bool { return result.Devices[i].Interface < result.Devices[j].Interface })
	for number := range mounts {
		if !registeredMountNumbers[number] {
			result.Detail = "mounted block device is absent from the registry"
			return result
		}
	}
	if result.WritableMounts > 0 {
		result.Detail = "one or more block devices have a writable mount"
		return result
	}
	if result.EligibleDevices == 0 {
		result.Status = StatusInconclusive
		result.Detail = "no supported media-bearing block devices were discovered"
		return result
	}
	for index := range result.Devices {
		device := &result.Devices[index]
		if device.Kind != "media" {
			continue
		}
		inspection := inspectBlockNode(ctx, root, device.Interface, expectedDevices[device.Interface], operations)
		device.NodePresent, device.Readable, device.ReadinessIssue = inspection.present, inspection.readable, inspection.issue
		if inspection.cleanupError != nil {
			result.Detail = "block node closure could not be confirmed"
			return result
		}
		if ctx.Err() != nil {
			device.Readable = false
			device.ReadinessIssue = "cancelled"
			result.Status = StatusAborted
			result.Detail = "cancelled"
			return result
		}
		if device.Readable {
			result.ReadableDevices++
		}
		if badDeviceState(device.State) {
			device.ReadinessIssue = "kernel reports an unusable device state"
		}
	}
	if ctx.Err() != nil {
		result.Status = StatusAborted
		result.Detail = "cancelled"
		return result
	}
	if result.ReadableDevices != result.EligibleDevices {
		result.Status = StatusInconclusive
		result.Detail = "one or more media-bearing block devices are not read-only accessible"
		return result
	}
	for _, device := range result.Devices {
		if device.Kind == "media" && device.ReadinessIssue != "" {
			result.Status = StatusInconclusive
			result.Detail = "one or more media-bearing block devices report a readiness issue"
			return result
		}
	}
	result.Status = StatusPass
	return result
}

func inspectRegistryDevice(root, name string, state mountState) (DeviceResult, bool, error) {
	base := filepath.Join(root, "sys/class/block", name)
	partition := fileExists(filepath.Join(base, "partition"))
	physical := blockInterfacePattern.MatchString(name)
	if !physical && !partition {
		return DeviceResult{}, false, nil
	}
	device := DeviceResult{Interface: name, Kind: "partition", Mounted: state.Mounted, WritableMount: state.Writable}
	if !partition {
		device.Kind = "media"
	}
	sizeSectors, sizeKnown := readUnsigned(filepath.Join(base, "size"))
	if sizeKnown {
		if sizeSectors > math.MaxUint64/512 {
			return DeviceResult{}, false, errors.New("block device size exceeds the supported limit")
		}
		device.SizeBytes = sizeSectors * 512
	}
	device.ReadOnly = readBoolean(filepath.Join(base, "ro"))
	device.Removable = readBoolean(filepath.Join(base, "removable"))
	device.State, _ = readSmallValue(filepath.Join(base, "device/state"))
	if device.Kind == "media" && device.SizeBytes == 0 {
		device.Kind = "empty"
	}
	return device, true, nil
}

type blockNodeInspection struct {
	present      bool
	readable     bool
	issue        string
	cleanupError error
}

func inspectBlockNode(ctx context.Context, root, name string, expected deviceNumber, operations blockNodeOperations) (result blockNodeInspection) {
	if ctx.Err() != nil {
		result.issue = "cancelled"
		return result
	}
	registryPath := filepath.Join(root, "sys/class/block", name, "dev")
	number, ok := operations.deviceNumber(registryPath)
	if !ok || number != expected {
		result.issue = "block device registry identity changed"
		return result
	}
	if ctx.Err() != nil {
		result.issue = "cancelled"
		return result
	}
	devicePath := filepath.Join(root, "dev", name)
	metadata, err := operations.lstat(devicePath)
	if err != nil {
		result.issue = "block node is absent"
		return result
	}
	result.present = true
	if number, ok := directBlockNumber(metadata); !ok || number != expected {
		result.issue = "device path is not the expected direct block node"
		return result
	}
	if ctx.Err() != nil {
		result.issue = "cancelled"
		return result
	}
	file, err := operations.open(devicePath, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		result.issue = "block node cannot be opened read-only"
		return result
	}
	if file == nil {
		result.issue = "block node handle is unavailable"
		return result
	}
	defer func() {
		if err := file.Close(); err != nil {
			result.readable = false
			result.issue = "block node closure could not be confirmed"
			result.cleanupError = err
		}
		if ctx.Err() != nil {
			result.readable = false
			if result.cleanupError == nil {
				result.issue = "cancelled"
			}
		}
	}()
	if ctx.Err() != nil {
		result.issue = "cancelled"
		return result
	}
	openedMetadata, statErr := file.Stat()
	if number, ok := directBlockNumber(openedMetadata); statErr != nil || !ok || number != expected {
		result.issue = "opened node did not remain the expected block device"
		return result
	}
	if ctx.Err() != nil {
		result.issue = "cancelled"
		return result
	}
	number, ok = operations.deviceNumber(registryPath)
	if !ok || number != expected {
		result.issue = "block device registry identity changed"
		return result
	}
	if ctx.Err() != nil {
		result.issue = "cancelled"
		return result
	}
	result.readable = true
	return result
}

func directBlockNumber(metadata os.FileInfo) (deviceNumber, bool) {
	if metadata == nil || metadata.Mode()&os.ModeSymlink != 0 || metadata.Mode()&os.ModeDevice == 0 || metadata.Mode()&os.ModeCharDevice != 0 {
		return deviceNumber{}, false
	}
	stat, ok := metadata.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return deviceNumber{}, false
	}
	value := uint64(stat.Rdev)
	return deviceNumber{major: uint32((value>>8)&0xfff | (value >> 32 & 0xfffff000)),
		minor: uint32(value&0xff | (value >> 12 & 0xffffff00))}, true
}

func readDeviceNumber(path string) (deviceNumber, bool) {
	file, err := os.Open(path)
	if err != nil {
		return deviceNumber{}, false
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 23))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > 22 {
		return deviceNumber{}, false
	}
	return parseDeviceNumber(strings.TrimSuffix(string(data), "\n"))
}

func readMounts(path string) (map[string]mountState, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	mounts, parseErr := parseMounts(file)
	return mounts, errors.Join(parseErr, file.Close())
}

func parseMounts(source io.Reader) (map[string]mountState, error) {
	data, readErr := io.ReadAll(io.LimitReader(source, (4<<20)+1))
	mounts, parseErr := safemode.ParseMounts(data)
	return mounts, errors.Join(readErr, parseErr)
}

func validInterfaceName(name string) bool {
	return name != "" && len(name) <= 64 && name != "." && name != ".." && !strings.ContainsAny(name, `/\\`)
}

func badDeviceState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "offline", "dead", "blocked", "quiesce":
		return true
	default:
		return false
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readBoolean(path string) bool {
	value, ok := readSmallValue(path)
	return ok && value == "1"
}

func readUnsigned(path string) (uint64, bool) {
	value, ok := readSmallValue(path)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	return parsed, err == nil
}

func readSmallValue(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 256 {
		return "", false
	}
	value := strings.TrimSpace(string(data))
	return value, value != ""
}
