// Package safemode checks appliance safety independently of test applicability.
// It reads kernel metadata only: no device nodes, media contents, or sockets.
// Each observation is a bounded snapshot, not continuous monitoring.
package safemode

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
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
	result := Result{Schema: Schema, Status: "error"}
	var storageErr, networkErr error
	result.WritableBlockMounts, storageErr = writableBlockMounts(ctx, root)
	result.StorageObserved = storageErr == nil
	if storageErr != nil {
		result.StorageDetail = "block mount safety could not be established"
	}
	result.ActiveNonLoopInterfaces, networkErr = activeInterfaces(ctx, root)
	result.NetworkObserved = networkErr == nil
	if networkErr != nil {
		result.NetworkDetail = "offline network state could not be established"
	}
	switch {
	case result.WritableBlockMounts > 0 || result.ActiveNonLoopInterfaces > 0:
		result.Status = "fail"
	case ctx.Err() != nil:
		result.Status = "aborted"
	case result.StorageObserved && result.NetworkObserved:
		result.Status = "pass"
	}
	return result
}

var interfaceName = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
var deviceNumber = regexp.MustCompile(`^(0|[1-9][0-9]*):(0|[1-9][0-9]*)$`)

func readMetadata(ctx context.Context, path string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("invalid kernel metadata")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func registry(ctx context.Context, path string) ([]os.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := file.ReadDir(4097)
	if err != nil && !errors.Is(err, io.EOF) || len(entries) > 4096 {
		return nil, errors.New("invalid kernel registry")
	}
	for _, entry := range entries {
		if !interfaceName.MatchString(entry.Name()) || entry.Name() == "." || entry.Name() == ".." {
			return nil, errors.New("invalid kernel interface")
		}
	}
	return entries, nil
}

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

func writableBlockMounts(ctx context.Context, root string) (int, error) {
	entries, err := registry(ctx, filepath.Join(root, "sys/class/block"))
	if err != nil {
		return 0, err
	}
	devices := map[string]bool{}
	for _, entry := range entries {
		data, err := readMetadata(ctx, filepath.Join(root, "sys/class/block", entry.Name(), "dev"), 64)
		number := strings.TrimSpace(string(data))
		if err != nil || !validDeviceNumber(number) || strings.HasPrefix(number, "0:") || devices[number] {
			return 0, errors.New("invalid block device registry")
		}
		devices[number] = true
	}
	data, err := readMetadata(ctx, filepath.Join(root, "proc/self/mountinfo"), 4<<20)
	if err != nil {
		return 0, err
	}
	return parseMounts(data, devices)
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

func parseMounts(data []byte, devices map[string]bool) (int, error) {
	writable, ids := map[string]bool{}, map[string]bool{}
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
			return len(writable), errors.New("invalid mount record")
		}
		id, idErr := strconv.ParseUint(fields[0], 10, 64)
		parent, parentErr := strconv.ParseUint(fields[1], 10, 64)
		if idErr != nil || parentErr != nil || strconv.FormatUint(id, 10) != fields[0] || strconv.FormatUint(parent, 10) != fields[1] || id == 0 || ids[fields[0]] || !strings.HasPrefix(fields[3], "/") || !strings.HasPrefix(fields[4], "/") {
			return len(writable), errors.New("invalid mount identity")
		}
		ids[fields[0]] = true
		rw, err := mountWritable(fields[5])
		if err != nil {
			return len(writable), err
		}
		if _, err := mountWritable(fields[separator+3]); err != nil {
			return len(writable), err
		}
		number := fields[2]
		if strings.HasPrefix(number, "0:") {
			if !virtualFilesystems[fields[separator+1]] {
				return len(writable), errors.New("unknown non-block mount")
			}
			if rw && fields[separator+1] == "overlay" {
				return len(writable), errors.New("writable overlay backing is unknown")
			}
		} else if !devices[number] {
			return len(writable), errors.New("mounted device is absent from registry")
		} else if rw {
			writable[number] = true
		}
	}
	if scanner.Err() != nil || len(ids) == 0 {
		return len(writable), errors.New("mount observations unavailable")
	}
	return len(writable), nil
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

func activeInterfaces(ctx context.Context, root string) (int, error) {
	entries, err := registry(ctx, filepath.Join(root, "sys/class/net"))
	if err != nil {
		return 0, err
	}
	active, loopback := 0, false
	for _, entry := range entries {
		data, err := readMetadata(ctx, filepath.Join(root, "sys/class/net", entry.Name(), "flags"), 64)
		value := strings.TrimSpace(string(data))
		if err != nil || !strings.HasPrefix(value, "0x") {
			return active, errors.New("network flags unavailable")
		}
		flags, err := strconv.ParseUint(strings.TrimPrefix(value, "0x"), 16, 32)
		if err != nil {
			return active, errors.New("invalid network flags")
		}
		if entry.Name() == "lo" {
			if flags&8 == 0 {
				return active, errors.New("invalid loopback interface")
			}
			loopback = true
		} else if flags&1 != 0 {
			active++
		}
	}
	if !loopback {
		return active, errors.New("loopback observation unavailable")
	}
	return active, nil
}
