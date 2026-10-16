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
	"unicode"
)

const maximumNetworkInterfaces = 128

func runPlatform(ctx context.Context, options Options) Result {
	result := Result{Schema: Schema, Status: StatusError, RequireOffline: !options.AllowActive}
	if ctx.Err() != nil {
		result.Status = StatusAborted
		result.Detail = "cancelled"
		return result
	}
	if options.Root == "" {
		options.Root = "/"
	}
	root := filepath.Clean(options.Root)
	entries, err := os.ReadDir(filepath.Join(root, "sys/class/net"))
	if err != nil {
		result.Detail = "cannot read network interface registry"
		return result
	}
	if len(entries) > maximumNetworkInterfaces {
		result.Detail = "network interface registry exceeds the supported limit"
		return result
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			result.Status = StatusAborted
			result.Detail = "cancelled"
			return result
		}
		interfaceResult, include, active, inspectErr := inspectInterface(root, entry.Name())
		if inspectErr != nil {
			result.Detail = inspectErr.Error()
			return result
		}
		if active {
			result.ActiveNonLoopInterfaces++
		}
		if include {
			result.Interfaces = append(result.Interfaces, interfaceResult)
			result.HardwareInterfaces++
			if interfaceResult.DriverBound {
				result.DriverBoundInterfaces++
			}
		}
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
	for _, interfaceResult := range result.Interfaces {
		if interfaceResult.ReadinessIssue != "" {
			result.Status = StatusInconclusive
			result.Detail = "one or more network interfaces report a readiness issue"
			return result
		}
	}
	result.Status = StatusPass
	return result
}

func inspectInterface(root, name string) (InterfaceResult, bool, bool, error) {
	if !validInterfaceName(name) {
		return InterfaceResult{}, false, false, errors.New("network registry contains an invalid interface name")
	}
	if name == "lo" {
		return InterfaceResult{}, false, false, nil
	}
	base := filepath.Join(root, "sys/class/net", name)
	flags, flagsKnown := readHexadecimal(filepath.Join(base, "flags"))
	if !flagsKnown {
		return InterfaceResult{}, false, false, errors.New("cannot verify network interface activity")
	}
	active := flags&1 != 0
	if !fileExists(filepath.Join(base, "device")) {
		return InterfaceResult{}, false, active, nil
	}
	result := InterfaceResult{Interface: name, Kind: "ethernet", AdministrativeUp: active}
	if fileExists(filepath.Join(base, "wireless")) || fileExists(filepath.Join(base, "phy80211")) {
		result.Kind = "wifi"
	}
	result.OperationalState, _ = readSmallValue(filepath.Join(base, "operstate"))
	if carrier, carrierKnown := readUnsigned(filepath.Join(base, "carrier")); carrierKnown && carrier <= 1 {
		result.CarrierDetected = true
		result.Carrier = carrier == 1
	}
	if speed, speedKnown := readUnsigned(filepath.Join(base, "speed")); speedKnown && speed <= 1_000_000 {
		result.SpeedMbps = speed
	}
	if duplex, duplexKnown := readSmallValue(filepath.Join(base, "duplex")); duplexKnown {
		duplex = strings.ToLower(duplex)
		if duplex == "full" || duplex == "half" {
			result.Duplex = duplex
		}
	}
	if driverTarget, driverErr := os.Readlink(filepath.Join(base, "device/driver")); driverErr == nil {
		result.Driver = sanitizeDriver(filepath.Base(driverTarget))
		result.DriverBound = result.Driver != ""
	}
	if !result.DriverBound {
		if driver, driverKnown := readDriverFromUevent(filepath.Join(base, "device/uevent")); driverKnown {
			result.Driver = driver
			result.DriverBound = true
		}
	}
	return result, true, active, nil
}

func readDriverFromUevent(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 4096 {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "DRIVER=") {
			driver := sanitizeDriver(strings.TrimPrefix(line, "DRIVER="))
			return driver, driver != ""
		}
	}
	return "", false
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

func validInterfaceName(name string) bool {
	return name != "" && len(name) <= 64 && name != "." && name != ".." && !strings.ContainsAny(name, `/\\`) && strings.IndexFunc(name, unicode.IsControl) == -1
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readHexadecimal(path string) (uint64, bool) {
	value, ok := readSmallValue(path)
	if !ok {
		return 0, false
	}
	value = strings.TrimPrefix(strings.ToLower(value), "0x")
	parsed, err := strconv.ParseUint(value, 16, 64)
	return parsed, err == nil
}

func readUnsigned(path string) (uint64, bool) {
	value, ok := readSmallValue(path)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	return parsed, err == nil && parsed != math.MaxUint64
}

func readSmallValue(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 256 {
		return "", false
	}
	value := strings.TrimSpace(string(data))
	return value, value != ""
}
