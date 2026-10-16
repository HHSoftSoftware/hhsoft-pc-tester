//go:build linux

package storage

import (
	"bufio"
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
)

const maximumBlockDevices = 256

var blockInterfacePattern = regexp.MustCompile(`^(sd[a-z]+|hd[a-z]+|vd[a-z]+|xvd[a-z]+|sr[0-9]+|fd[0-9]+|mmcblk[0-9]+|nvme[0-9]+(c[0-9]+)?n[0-9]+)$`)

type mountState struct {
	mounted  bool
	writable bool
}

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
	mounts, err := readMounts(filepath.Join(root, "proc/self/mountinfo"))
	if err != nil {
		result.Detail = "cannot verify block mount safety"
		return result
	}
	entries, err := os.ReadDir(filepath.Join(root, "sys/class/block"))
	if err != nil {
		result.Detail = "cannot read block device registry"
		return result
	}
	if len(entries) > maximumBlockDevices {
		result.Detail = "block device registry exceeds the supported limit"
		return result
	}
	writableDevices := make(map[string]struct{})
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
		deviceNumber, ok := readSmallValue(filepath.Join(root, "sys/class/block", name, "dev"))
		if !ok {
			result.Detail = "block device registry lacks a device number"
			return result
		}
		state := mounts[deviceNumber]
		// Mount safety applies to every registered block device, including
		// mappings and loop devices excluded from hardware readiness evidence.
		if state.writable {
			writableDevices[deviceNumber] = struct{}{}
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
	result.WritableMounts = len(writableDevices)
	sort.Slice(result.Devices, func(i, j int) bool { return result.Devices[i].Interface < result.Devices[j].Interface })
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
		device.NodePresent, device.Readable, device.ReadinessIssue = inspectBlockNode(root, device.Interface)
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
	device := DeviceResult{Interface: name, Kind: "partition", Mounted: state.mounted, WritableMount: state.writable}
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

func inspectBlockNode(root, name string) (bool, bool, string) {
	devicePath := filepath.Join(root, "dev", name)
	metadata, err := os.Lstat(devicePath)
	if err != nil {
		return false, false, "block node is absent"
	}
	if metadata.Mode()&os.ModeSymlink != 0 || metadata.Mode()&os.ModeDevice == 0 || metadata.Mode()&os.ModeCharDevice != 0 {
		return true, false, "device path is not a direct block node"
	}
	file, err := os.OpenFile(devicePath, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return true, false, "block node cannot be opened read-only"
	}
	openedMetadata, statErr := file.Stat()
	_ = file.Close()
	if statErr != nil || openedMetadata.Mode()&os.ModeDevice == 0 || openedMetadata.Mode()&os.ModeCharDevice != 0 || !sameDevice(metadata, openedMetadata) {
		return true, false, "opened node did not remain the expected block device"
	}
	return true, true, ""
}

func sameDevice(first, second os.FileInfo) bool {
	firstStat, firstOK := first.Sys().(*syscall.Stat_t)
	secondStat, secondOK := second.Sys().(*syscall.Stat_t)
	return firstOK && secondOK && firstStat.Rdev == secondStat.Rdev
}

func readMounts(path string) (map[string]mountState, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return parseMounts(file)
}

func parseMounts(source io.Reader) (map[string]mountState, error) {
	result := make(map[string]mountState)
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	lines := 0
	for scanner.Scan() {
		lines++
		if lines > 4096 {
			return nil, errors.New("mount registry exceeds the supported limit")
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 7 || !strings.Contains(fields[2], ":") || fields[5] == "" {
			return nil, errors.New("malformed mount registry")
		}
		state := result[fields[2]]
		state.mounted = true
		state.writable = state.writable || listContains(fields[5], "rw")
		result[fields[2]] = state
	}
	return result, scanner.Err()
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

func listContains(value, wanted string) bool {
	for _, item := range strings.Split(value, ",") {
		if item == wanted {
			return true
		}
	}
	return false
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
