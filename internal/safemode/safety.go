// Package safemode checks appliance safety independently of test applicability.
// It reads kernel metadata only: no device nodes, media contents, or sockets.
// Each observation is a bounded snapshot, not continuous monitoring.
package safemode

import (
	"bufio"
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

const Schema = "hhsoft.pc-tester.runtime-safety/v1"

type Result struct {
	Schema                  string `json:"schema"`
	Status                  string `json:"status"`
	StorageObserved         bool   `json:"storage_observed"`
	NetworkObserved         bool   `json:"network_observed"`
	WritableBlockMounts     int    `json:"writable_block_mounts"`
	ActiveNonLoopInterfaces int    `json:"active_non_loop_interfaces"`
	StorageDetail           string `json:"storage_detail,omitempty"`
	NetworkDetail           string `json:"network_detail,omitempty"`
}

func (result Result) Safe() bool {
	return result.Schema == Schema && result.Status == "pass" && result.StorageObserved && result.NetworkObserved &&
		result.WritableBlockMounts == 0 && result.ActiveNonLoopInterfaces == 0
}

func check(ctx context.Context, root string) Result {
	return checkWith(ctx, root, systemSafetyOperations())
}

func checkWith(ctx context.Context, root string, operations safetyOperations) Result {
	result := Result{Schema: Schema, Status: "error"}
	storage, storageErr := observeSafetyRoot(ctx, root, operations, writableBlockMounts)
	network, networkErr := observeSafetyRoot(ctx, root, operations, activeInterfaces)
	result.WritableBlockMounts, result.ActiveNonLoopInterfaces = storage, network
	result.StorageObserved, result.NetworkObserved = storageErr == nil, networkErr == nil
	if storageErr != nil {
		result.StorageDetail = "block mount safety could not be established"
	}
	if networkErr != nil {
		result.NetworkDetail = "offline network state could not be established"
	}
	switch {
	case storage > 0 || network > 0:
		result.Status = "fail"
	case storageErr != nil && storageErr != context.Canceled && storageErr != context.DeadlineExceeded ||
		networkErr != nil && networkErr != context.Canceled && networkErr != context.DeadlineExceeded:
		// Hard acquisition errors and cleanup uncertainty outrank cancellation.
	case !nilSafetyValue(ctx) && ctx.Err() != nil:
		result.Status = "aborted"
	case result.StorageObserved && result.NetworkObserved:
		result.Status = "pass"
	}
	return result
}

var interfaceName = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
var deviceNumber = regexp.MustCompile(`^(0|[1-9][0-9]*):(0|[1-9][0-9]*)$`)

func validDeviceNumber(value string) bool {
	if !deviceNumber.MatchString(value) {
		return false
	}
	for _, part := range strings.Split(value, ":") {
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return false
		}
	}
	return true
}

func writableBlockMounts(ctx context.Context, root safetyRoot) (int, error) {
	entries, err := registry(ctx, root, "sys/class/block")
	if err != nil {
		return 0, err
	}
	devices := map[string]bool{}
	for _, name := range entries {
		data, err := readMetadata(ctx, root, "sys/class/block/"+name+"/dev", 64)
		if err != nil {
			return 0, err
		}
		number := strings.TrimSpace(string(data))
		if !validDeviceNumber(number) || strings.HasPrefix(number, "0:") || devices[number] {
			return 0, errors.New("invalid block device registry")
		}
		devices[number] = true
	}
	data, readErr := readMetadata(ctx, root, "proc/self/mountinfo", 4<<20)
	if data == nil {
		return 0, readErr
	}
	writable, parseErr := parseMounts(data, devices)
	if parseErr != nil {
		return writable, errors.Join(readErr, parseErr)
	}
	return writable, readErr
}

// These kernel filesystems use non-block device numbers. Writable overlays are
// separately rejected: their backing mounts may be detached or hidden from
// mountinfo, so their paths cannot establish that writes stay in RAM.
// Unknown zero-major mounts (including synthetic block-filesystem numbers)
// fail closed instead of being mistaken for harmless RAM-backed mounts.
var virtualFilesystems = map[string]bool{
	"rootfs": true, "ramfs": true, "tmpfs": true, "devtmpfs": true,
	"proc": true, "sysfs": true, "devpts": true, "overlay": true,
	"cgroup": true, "cgroup2": true, "pstore": true, "securityfs": true,
	"debugfs": true, "tracefs": true, "configfs": true, "efivarfs": true,
	"fusectl": true, "mqueue": true, "hugetlbfs": true, "binfmt_misc": true,
	"autofs": true, "rpc_pipefs": true, "nsfs": true, "bpf": true,
}

// MountState describes observed aliases of one nonzero-major mount device.
// It does not establish that the device exists in the current block registry.
type MountState struct {
	Mounted  bool
	Writable bool
}

// ParseMounts validates a bounded mountinfo snapshot without accessing the
// filesystem or network. Callers must also reconcile every returned device with
// their complete block registry. On error, previously parsed observations remain
// available for retaining unsafe findings; they cannot establish a safe view.
func ParseMounts(data []byte) (map[string]MountState, error) {
	projection, err := parseMountProjection(data)
	return projection.blockDevices, err
}

type mountProjection struct {
	blockDevices map[string]MountState
}

func parseMountProjection(data []byte) (mountProjection, error) {
	projection := mountProjection{blockDevices: map[string]MountState{}}
	ids, virtualTypes := map[string]bool{}, map[string]string{}
	if len(data) > 4<<20 {
		return projection, errors.New("mount observations exceed the supported limit")
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		separator := -1
		for index, value := range fields {
			if index >= 6 && value == "-" {
				separator = index
				break
			}
		}
		if separator < 6 || len(fields) != separator+4 || len(ids) >= 4096 || !validDeviceNumber(fields[2]) {
			return projection, errors.New("invalid mount record")
		}
		id, idErr := strconv.ParseUint(fields[0], 10, 64)
		parent, parentErr := strconv.ParseUint(fields[1], 10, 64)
		if idErr != nil || parentErr != nil || strconv.FormatUint(id, 10) != fields[0] || strconv.FormatUint(parent, 10) != fields[1] || id == 0 || ids[fields[0]] || !strings.HasPrefix(fields[3], "/") || !strings.HasPrefix(fields[4], "/") {
			return projection, errors.New("invalid mount identity")
		}
		ids[fields[0]] = true
		rw, err := mountWritable(fields[5])
		if err != nil {
			return projection, err
		}
		if _, err := mountWritable(fields[separator+3]); err != nil {
			return projection, err
		}
		number := fields[2]
		if strings.HasPrefix(number, "0:") {
			filesystem := fields[separator+1]
			if !virtualFilesystems[filesystem] {
				return projection, errors.New("unknown non-block mount")
			}
			if rw && filesystem == "overlay" {
				return projection, errors.New("writable overlay backing is unknown")
			}
			// Aliases of one filesystem may differ in paths and access flags,
			// but its canonical device number cannot identify two types.
			if previous, present := virtualTypes[number]; present && previous != filesystem {
				return projection, errors.New("conflicting non-block filesystem identity")
			}
			virtualTypes[number] = filesystem
		} else {
			state := projection.blockDevices[number]
			state.Mounted = true
			state.Writable = state.Writable || rw
			projection.blockDevices[number] = state
		}
	}
	if scanner.Err() != nil || len(ids) == 0 {
		return projection, errors.New("mount observations unavailable")
	}
	return projection, nil
}

func parseMounts(data []byte, devices map[string]bool) (int, error) {
	mounts, err := ParseMounts(data)
	writable := 0
	for number, state := range mounts {
		if devices[number] && state.Writable {
			writable++
		}
	}
	if err != nil {
		return writable, err
	}
	for number := range mounts {
		if !devices[number] {
			return writable, errors.New("mounted device is absent from registry")
		}
	}
	return writable, nil
}

func mountWritable(options string) (bool, error) {
	readOnly, writable := 0, 0
	for _, option := range strings.Split(options, ",") {
		switch option {
		case "ro":
			readOnly++
		case "rw":
			writable++
		case "":
			return false, errors.New("invalid mount options")
		}
	}
	if readOnly+writable != 1 {
		return false, errors.New("ambiguous mount access")
	}
	return writable == 1, nil
}

func activeInterfaces(ctx context.Context, root safetyRoot) (int, error) {
	entries, err := registry(ctx, root, "sys/class/net")
	if err != nil {
		return 0, err
	}
	active, loopback := 0, false
	for _, name := range entries {
		data, readErr := readMetadata(ctx, root, "sys/class/net/"+name+"/flags", 64)
		if data == nil && readErr != nil {
			return active, readErr
		}
		value := strings.TrimSpace(string(data))
		if !strings.HasPrefix(value, "0x") {
			return active, errors.Join(readErr, errors.New("network flags unavailable"))
		}
		flags, err := strconv.ParseUint(strings.TrimPrefix(value, "0x"), 16, 32)
		if err != nil {
			return active, errors.Join(readErr, errors.New("invalid network flags"))
		}
		if name == "lo" {
			if flags&8 == 0 {
				return active, errors.Join(readErr, errors.New("invalid loopback interface"))
			}
			loopback = true
		} else if flags&1 != 0 {
			active++
		}
		if readErr != nil {
			return active, readErr
		}
	}
	if !loopback {
		return active, errors.New("loopback observation unavailable")
	}
	return active, nil
}
