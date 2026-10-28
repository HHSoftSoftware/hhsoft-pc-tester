//go:build linux

package networktest

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode"
)

const maximumNetworkDriverLinkBytes = 4096

type networkOperations struct {
	openMetadata func(string) (networkMetadataFile, error)
	openRegistry func(string) (networkRegistryDirectory, error)
	stat         func(string) (os.FileInfo, error)
	readlink     func(string, []byte) (int, error)
}

func realNetworkOperations() networkOperations {
	return networkOperations{
		openMetadata: func(path string) (networkMetadataFile, error) {
			return os.OpenFile(path, os.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
		},
		openRegistry: func(path string) (networkRegistryDirectory, error) {
			return os.OpenFile(path, os.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NONBLOCK|syscall.O_DIRECTORY, 0)
		},
		stat: os.Stat,
		readlink: func(path string, buffer []byte) (int, error) {
			n, err := syscall.Readlink(path, buffer)
			// Linux syscall errors return -1 rather than a count of bytes.
			// Normalize only that actual ABI result at the OS boundary.
			if n == -1 && err != nil {
				n = 0
			}
			return n, err
		},
	}
}

func runPlatform(ctx context.Context, options Options) Result {
	return runPlatformWith(ctx, options, realNetworkOperations())
}

func runPlatformWith(ctx context.Context, options Options, operations networkOperations) Result {
	result := Result{Schema: Schema, Status: StatusError, RequireOffline: !options.AllowActive}
	if nilNetworkValue(ctx) || operations.openMetadata == nil || operations.openRegistry == nil || operations.stat == nil || operations.readlink == nil {
		result.Detail = "network readiness dependencies are incomplete"
		return result
	}
	failure := func(err error) Result {
		result.Detail = err.Error()
		if err == context.Canceled || err == context.DeadlineExceeded {
			result.Status = StatusAborted
		}
		return result
	}
	if stopped := ctx.Err(); stopped != nil {
		return failure(stopped)
	}
	if options.Root == "" {
		options.Root = "/"
	}
	root := filepath.Clean(options.Root)
	names, err := readNetworkRegistry(ctx, filepath.Join(root, "sys/class/net"), operations.openRegistry)
	if err != nil {
		return failure(err)
	}
	for _, name := range names {
		if stopped := ctx.Err(); stopped != nil {
			return failure(stopped)
		}
		value, include, active, err := inspectInterface(ctx, root, name, operations)
		if err != nil {
			return failure(err)
		}
		if active {
			result.ActiveNonLoopInterfaces++
		}
		if include {
			result.Interfaces = append(result.Interfaces, value)
			result.HardwareInterfaces++
			if value.DriverBound {
				result.DriverBoundInterfaces++
			}
		}
	}
	if stopped := ctx.Err(); stopped != nil {
		return failure(stopped)
	}
	sort.Slice(result.Interfaces, func(i, j int) bool { return result.Interfaces[i].Interface < result.Interfaces[j].Interface })
	if result.RequireOffline && result.ActiveNonLoopInterfaces > 0 {
		result.Detail = "one or more non-loopback network interfaces are administratively active"
		return result
	}
	if result.HardwareInterfaces == 0 {
		result.Status = StatusInconclusive
		result.Detail = "no hardware-backed network interfaces were discovered"
		return result
	}
	if result.DriverBoundInterfaces != result.HardwareInterfaces {
		result.Status = StatusInconclusive
		result.Detail = "one or more hardware network interfaces lack a bound driver"
		return result
	}
	for _, value := range result.Interfaces {
		if value.ReadinessIssue != "" {
			result.Status = StatusInconclusive
			result.Detail = "one or more network interfaces report a readiness issue"
			return result
		}
	}
	if stopped := ctx.Err(); stopped != nil {
		return failure(stopped)
	}
	result.Status = StatusPass
	return result
}

func networkPathPresent(ctx context.Context, path string, stat func(string) (os.FileInfo, error)) (bool, error) {
	if stopped := ctx.Err(); stopped != nil {
		return false, stopped
	}
	info, err := stat(path)
	if err != nil {
		if !nilNetworkValue(info) || !soleNetworkError(err, "stat", os.ErrNotExist, syscall.ENOENT) {
			return false, errNetworkMetadataRead
		}
		if stopped := ctx.Err(); stopped != nil {
			return false, stopped
		}
		return false, nil
	}
	if nilNetworkValue(info) {
		return false, errNetworkMetadataRead
	}
	if stopped := ctx.Err(); stopped != nil {
		return false, stopped
	}
	return true, nil
}

func readNetworkValue(ctx context.Context, path string, optional, offlineValue bool, open func(string) (networkMetadataFile, error)) (string, error) {
	data, err := acquireNetworkMetadata(ctx, path, maximumNetworkScalarBytes, optional, offlineValue, open)
	if err == errNetworkMetadataAbsent {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func readNetworkDriverLink(ctx context.Context, path string, readlink func(string, []byte) (int, error)) (string, error) {
	if stopped := ctx.Err(); stopped != nil {
		return "", stopped
	}
	var buffer [maximumNetworkDriverLinkBytes + 1]byte
	n, err := readlink(path, buffer[:])
	if n < 0 || n > maximumNetworkDriverLinkBytes || err != nil && n != 0 {
		return "", errNetworkMetadataRead
	}
	if err != nil {
		if !soleNetworkError(err, "readlink", os.ErrNotExist, syscall.ENOENT) {
			return "", errNetworkMetadataRead
		}
		if stopped := ctx.Err(); stopped != nil {
			return "", stopped
		}
		return "", nil
	}
	if n == 0 || strings.IndexByte(string(buffer[:n]), 0) != -1 {
		return "", errNetworkMetadataRead
	}
	if stopped := ctx.Err(); stopped != nil {
		return "", stopped
	}
	return sanitizeDriver(filepath.Base(string(buffer[:n]))), nil
}

func inspectInterface(ctx context.Context, root, name string, operations networkOperations) (InterfaceResult, bool, bool, error) {
	if !validInterfaceName(name) {
		return InterfaceResult{}, false, false, errors.New("network registry contains an invalid interface name")
	}
	if name == "lo" {
		return InterfaceResult{}, false, false, nil
	}
	base := filepath.Join(root, "sys/class/net", name)
	flagsValue, err := readNetworkValue(ctx, filepath.Join(base, "flags"), false, false, operations.openMetadata)
	if err != nil {
		return InterfaceResult{}, false, false, err
	}
	flags, flagsErr := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(flagsValue), "0x"), 16, 64)
	if flagsErr != nil {
		return InterfaceResult{}, false, false, errors.New("cannot verify network interface activity")
	}
	active := flags&1 != 0
	hardware, err := networkPathPresent(ctx, filepath.Join(base, "device"), operations.stat)
	if err != nil || !hardware {
		return InterfaceResult{}, false, active, err
	}
	result := InterfaceResult{Interface: name, Kind: "ethernet", AdministrativeUp: active}
	wireless, err := networkPathPresent(ctx, filepath.Join(base, "wireless"), operations.stat)
	if err != nil {
		return InterfaceResult{}, false, active, err
	}
	if !wireless {
		wireless, err = networkPathPresent(ctx, filepath.Join(base, "phy80211"), operations.stat)
		if err != nil {
			return InterfaceResult{}, false, active, err
		}
	}
	if wireless {
		result.Kind = "wifi"
	}
	result.OperationalState, err = readNetworkValue(ctx, filepath.Join(base, "operstate"), true, false, operations.openMetadata)
	if err != nil {
		return InterfaceResult{}, false, active, err
	}
	carrier, err := readNetworkValue(ctx, filepath.Join(base, "carrier"), true, true, operations.openMetadata)
	if err != nil {
		return InterfaceResult{}, false, active, err
	}
	if value, known := parseNetworkUnsigned(carrier); known && value <= 1 {
		result.CarrierDetected, result.Carrier = true, value == 1
	}
	speed, err := readNetworkValue(ctx, filepath.Join(base, "speed"), true, true, operations.openMetadata)
	if err != nil {
		return InterfaceResult{}, false, active, err
	}
	if value, known := parseNetworkUnsigned(speed); known && value <= 1_000_000 {
		result.SpeedMbps = value
	}
	duplex, err := readNetworkValue(ctx, filepath.Join(base, "duplex"), true, true, operations.openMetadata)
	if err != nil {
		return InterfaceResult{}, false, active, err
	}
	duplex = strings.ToLower(duplex)
	if duplex == "full" || duplex == "half" {
		result.Duplex = duplex
	}
	result.Driver, err = readNetworkDriverLink(ctx, filepath.Join(base, "device/driver"), operations.readlink)
	if err != nil {
		return InterfaceResult{}, false, active, err
	}
	result.DriverBound = result.Driver != ""
	if !result.DriverBound {
		data, readErr := acquireNetworkMetadata(ctx, filepath.Join(base, "device/uevent"), maximumNetworkUeventBytes, true, false, operations.openMetadata)
		if readErr != nil && readErr != errNetworkMetadataAbsent {
			return InterfaceResult{}, false, active, readErr
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "DRIVER=") {
				result.Driver = sanitizeDriver(strings.TrimPrefix(line, "DRIVER="))
				result.DriverBound = result.Driver != ""
				break
			}
		}
	}
	if stopped := ctx.Err(); stopped != nil {
		return InterfaceResult{}, false, active, stopped
	}
	return result, true, active, nil
}

func parseNetworkUnsigned(value string) (uint64, bool) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	return parsed, err == nil && parsed != math.MaxUint64
}

func sanitizeDriver(driver string) string {
	driver = strings.TrimSpace(driver)
	if driver == "" || len(driver) > 64 {
		return ""
	}
	if strings.IndexFunc(driver, func(character rune) bool {
		if character > unicode.MaxASCII || !(unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '-') {
			return true
		}
		return false
	}) != -1 {
		return ""
	}
	return driver
}
