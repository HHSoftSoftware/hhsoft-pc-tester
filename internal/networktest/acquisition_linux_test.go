//go:build linux

package networktest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type networkRunFixture struct {
	root string
	ops  networkOperations
}

func newNetworkRunFixture(t *testing.T) networkRunFixture {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"eth0", "eth1"} {
		for role, value := range map[string]string{"flags": "0x1002\n", "operstate": "down\n", "carrier": "0\n", "speed": "1000\n", "duplex": "full\n", "device/uevent": "DRIVER=e1000e\n"} {
			mustWrite(t, filepath.Join(root, "sys/class/net", name, role), value)
		}
	}
	return networkRunFixture{root: root, ops: realNetworkOperations()}
}

type networkCloseObserver struct {
	networkMetadataFile
	hook   func()
	closes *int
}

func (f *networkCloseObserver) Close() error {
	err := f.networkMetadataFile.Close()
	if f.closes != nil {
		(*f.closes)++
	}
	if f.hook != nil {
		f.hook()
	}
	return err
}

func TestNetworkAcquisitionHardFaultsStopLaterInterfaces(t *testing.T) {
	for _, role := range []string{"flags", "operstate", "carrier", "speed", "duplex", "device/uevent"} {
		for _, fault := range []string{"overflow", "partial error", "permission", "close", "handle and error", "open invalid", "read invalid"} {
			if fault == "read invalid" && (role == "carrier" || role == "speed" || role == "duplex") {
				continue
			}
			t.Run(role+"/"+fault, func(t *testing.T) {
				f := newNetworkRunFixture(t)
				original := f.ops.openMetadata
				limit := 256
				if role == "device/uevent" {
					limit = 4096
				}
				bad := networkData(strings.Repeat("x", limit+8))
				var handle networkMetadataFile = bad
				var openErr error
				want := errNetworkMetadataRead
				closes := 1
				switch fault {
				case "partial error":
					bad.read = func(p []byte) (int, error) { return copy(p, "0"), syscall.EIO }
				case "permission":
					handle = nil
					openErr = os.ErrPermission
					closes = 0
				case "open invalid":
					handle = nil
					openErr = syscall.EINVAL
					closes = 0
				case "read invalid":
					bad.read = func([]byte) (int, error) { return 0, syscall.EINVAL }
				case "close":
					bad = networkData("0")
					handle = bad
					bad.closeErr = os.ErrPermission
					want = errNetworkMetadataClose
				case "handle and error":
					openErr = os.ErrNotExist
				}
				seen := false
				later := 0
				f.ops.openMetadata = func(path string) (networkMetadataFile, error) {
					if seen {
						later++
					}
					if path == filepath.Join(f.root, "sys/class/net/eth0", role) {
						seen = true
						return handle, openErr
					}
					return original(path)
				}
				got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
				if !seen || got.Status != StatusError || got.Detail != want.Error() || got.HardwareInterfaces != 0 || later != 0 || bad.closes != closes || got.NetworkTrafficGenerated {
					t.Fatal("fault hidden or later interface work continued")
				}
				if fault == "overflow" && bad.bytes != limit+1 {
					t.Fatal("wrong raw overflow witness")
				}
			})
		}
	}
	for _, fault := range []string{"read error after prefix", "close error after prefix", "close and cancel after prefix"} {
		t.Run(fault, func(t *testing.T) {
			f := newNetworkRunFixture(t)
			for role, value := range map[string]string{"flags": "0x1002\n", "operstate": "down\n", "carrier": "0\n", "speed": "1000\n", "duplex": "full\n", "device/uevent": "DRIVER=e1000e\n"} {
				mustWrite(t, filepath.Join(f.root, "sys/class/net/eth2", role), value)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			bad := networkData("full\n")
			want := errNetworkMetadataClose
			if fault == "read error after prefix" {
				bad.read = func([]byte) (int, error) { return 0, syscall.EIO }
				want = errNetworkMetadataRead
			} else {
				bad.closeErr = os.ErrPermission
			}
			if fault == "close and cancel after prefix" {
				bad.closeHook = cancel
			}
			open, stat, readlink := f.ops.openMetadata, f.ops.stat, f.ops.readlink
			seen := false
			later, third, opened, closed := 0, 0, 0, 0
			f.ops.openMetadata = func(path string) (networkMetadataFile, error) {
				if seen {
					later++
				}
				if strings.HasPrefix(path, filepath.Join(f.root, "sys/class/net/eth2")+string(filepath.Separator)) {
					third++
				}
				if path == filepath.Join(f.root, "sys/class/net/eth1/duplex") {
					seen = true
					return bad, nil
				}
				file, err := open(path)
				if nilNetworkValue(file) {
					return file, err
				}
				opened++
				return &networkCloseObserver{networkMetadataFile: file, closes: &closed}, err
			}
			f.ops.stat = func(path string) (os.FileInfo, error) {
				if seen {
					later++
				}
				return stat(path)
			}
			f.ops.readlink = func(path string, p []byte) (int, error) {
				if seen {
					later++
				}
				return readlink(path, p)
			}
			got := runPlatformWith(ctx, Options{Root: f.root}, f.ops)
			prefix := InterfaceResult{Interface: "eth0", Kind: "ethernet", Driver: "e1000e", DriverBound: true, OperationalState: "down", CarrierDetected: true, SpeedMbps: 1000, Duplex: "full"}
			if !seen || got.Status != StatusError || got.Detail != want.Error() || !got.RequireOffline || got.NetworkTrafficGenerated || got.HardwareInterfaces != 1 || got.DriverBoundInterfaces != 1 || got.ActiveNonLoopInterfaces != 0 || len(got.Interfaces) != 1 || got.Interfaces[0] != prefix {
				t.Fatal("retained diagnostic prefix changed or incomplete interface was published")
			}
			if third != 0 || later != 0 || bad.closes != 1 || opened != closed {
				t.Fatal("failure opened a later interface or left an owned descriptor")
			}
		})
	}
}

func TestNetworkAcquisitionOptionalUnknownAndLegacyValueGrammar(t *testing.T) {
	for _, mode := range []string{"absent", "malformed", "offline invalid", "legacy spellings"} {
		t.Run(mode, func(t *testing.T) {
			f := newNetworkRunFixture(t)
			original := f.ops.openMetadata
			calls := map[string]int{}
			closed := 0
			if mode == "malformed" {
				for role, value := range map[string]string{"operstate": " unknown \n", "carrier": "2", "speed": "18446744073709551615", "duplex": "other"} {
					mustWrite(t, filepath.Join(f.root, "sys/class/net/eth0", role), value)
				}
			}
			if mode == "legacy spellings" {
				for role, value := range map[string]string{"flags": " 0X00001002 \n", "carrier": "0001", "speed": "00001000", "duplex": " HALF \n"} {
					mustWrite(t, filepath.Join(f.root, "sys/class/net/eth0", role), value)
				}
			}
			f.ops.openMetadata = func(path string) (networkMetadataFile, error) {
				calls[path]++
				if filepath.Dir(path) == filepath.Join(f.root, "sys/class/net/eth0") {
					role := filepath.Base(path)
					if mode == "absent" && role != "flags" {
						return nil, &os.PathError{Op: "open", Path: path, Err: syscall.ENOENT}
					}
					if mode == "offline invalid" && (role == "carrier" || role == "speed" || role == "duplex") {
						bad := networkData("")
						bad.read = func([]byte) (int, error) { return 0, &os.PathError{Op: "read", Path: path, Err: syscall.EINVAL} }
						bad.closeHook = func() { closed++ }
						return bad, nil
					}
				}
				return original(path)
			}
			got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
			if got.Status != StatusPass || got.HardwareInterfaces != 2 || got.DriverBoundInterfaces != 2 || got.NetworkTrafficGenerated {
				t.Fatal("legitimate optional unknown rejected")
			}
			for _, count := range calls {
				if count != 1 {
					t.Fatal("metadata observed more than once")
				}
			}
			value := got.Interfaces[0]
			if mode == "legacy spellings" {
				if !value.CarrierDetected || !value.Carrier || value.SpeedMbps != 1000 || value.Duplex != "half" {
					t.Fatal("legacy numeric grammar narrowed")
				}
			} else if value.CarrierDetected || value.Carrier || value.SpeedMbps != 0 || value.Duplex != "" {
				t.Fatal("unknown optional value fabricated")
			}
			if mode == "offline invalid" && closed != 3 {
				t.Fatal("unavailable descriptors not all closed")
			}
		})
	}
}

func TestNetworkAcquisitionDriverLinkBoundsAndFallback(t *testing.T) {
	for _, mode := range []string{"exact limit", "overflow", "negative", "negative absence", "oversized", "empty success", "partial error", "permission", "not link", "joined absence", "invalid driver fallback", "absent fallback", "nul"} {
		t.Run(mode, func(t *testing.T) {
			f := newNetworkRunFixture(t)
			original := f.ops.readlink
			reads := 0
			uevent := 0
			open := f.ops.openMetadata
			f.ops.openMetadata = func(path string) (networkMetadataFile, error) {
				if path == filepath.Join(f.root, "sys/class/net/eth0/device/uevent") {
					uevent++
				}
				return open(path)
			}
			f.ops.readlink = func(path string, p []byte) (int, error) {
				if path != filepath.Join(f.root, "sys/class/net/eth0/device/driver") {
					return original(path, p)
				}
				reads++
				if len(p) != 4097 {
					t.Fatal("link buffer is not bounded")
				}
				switch mode {
				case "exact limit":
					suffix := "/e1000"
					return copy(p, strings.Repeat("x", 4096-len(suffix))+suffix), nil
				case "overflow":
					return copy(p, strings.Repeat("x", 4097)), nil
				case "negative":
					return -1, nil
				case "negative absence":
					return -1, syscall.ENOENT
				case "oversized":
					return len(p) + 1, nil
				case "empty success":
					return 0, nil
				case "partial error":
					return copy(p, "driver"), syscall.ENOENT
				case "permission":
					return 0, syscall.EACCES
				case "not link":
					return 0, syscall.EINVAL
				case "joined absence":
					return 0, errors.Join(syscall.ENOENT, syscall.EACCES)
				case "invalid driver fallback":
					return copy(p, "path/invalid driver"), nil
				case "absent fallback":
					return 0, syscall.ENOENT
				case "nul":
					return copy(p, "bad\x00/path/e1000"), nil
				}
				t.Fatal("unknown fixture")
				return 0, nil
			}
			got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
			valid := mode == "exact limit" || mode == "invalid driver fallback" || mode == "absent fallback"
			if reads != 1 || got.NetworkTrafficGenerated {
				t.Fatal("link acquisition missing or traffic claimed")
			}
			if valid {
				if got.Status != StatusPass {
					t.Fatal("valid link/fallback rejected")
				}
				want := 1
				if mode == "exact limit" {
					want = 0
				}
				if uevent != want {
					t.Fatal("unexpected fallback work")
				}
			} else if got.Status != StatusError || got.Detail != errNetworkMetadataRead.Error() || got.HardwareInterfaces != 0 || uevent != 0 {
				t.Fatal("link fault downgraded or fallback continued")
			}
		})
	}
}

func TestNetworkAcquisitionStatFailuresCannotHideInterfaces(t *testing.T) {
	for _, role := range []string{"device", "wireless", "phy80211"} {
		for _, fault := range []string{"permission", "joined absence", "nil info", "info and error"} {
			t.Run(role+"/"+fault, func(t *testing.T) {
				f := newNetworkRunFixture(t)
				original := f.ops.stat
				seen := false
				later := 0
				open := f.ops.openMetadata
				f.ops.openMetadata = func(path string) (networkMetadataFile, error) {
					if seen {
						later++
					}
					return open(path)
				}
				f.ops.stat = func(path string) (os.FileInfo, error) {
					if path != filepath.Join(f.root, "sys/class/net/eth0", role) {
						return original(path)
					}
					seen = true
					switch fault {
					case "permission":
						return nil, os.ErrPermission
					case "joined absence":
						return nil, errors.Join(syscall.ENOENT, syscall.EIO)
					case "nil info":
						return nil, nil
					default:
						return networkInfo{}, syscall.ENOENT
					}
				}
				got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
				if !seen || got.Status != StatusError || got.HardwareInterfaces != 0 || got.Detail != errNetworkMetadataRead.Error() || later != 0 {
					t.Fatal("attribute-presence fault concealed discovery")
				}
			})
		}
	}
	for _, flags := range []string{"", "garbage", "18446744073709551616", "0x"} {
		t.Run("mandatory flags "+flags, func(t *testing.T) {
			f := newNetworkRunFixture(t)
			mustWrite(t, filepath.Join(f.root, "sys/class/net/eth0/flags"), flags)
			got := runPlatformWith(t.Context(), Options{Root: f.root}, f.ops)
			if got.Status != StatusError || got.HardwareInterfaces != 0 {
				t.Fatal("unknown activity earned readiness")
			}
		})
	}
}

func TestNetworkAcquisitionLateCancellationNeverRetainsPass(t *testing.T) {
	for _, phase := range []string{"last uevent close", "last optional absence", "last driver link", "software absence", "empty registry close", "last registry name", "hard close cancel"} {
		t.Run(phase, func(t *testing.T) {
			f := newNetworkRunFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			triggered := false
			later := 0
			owned, closed := 0, 0
			stop := func() { triggered = true; cancel() }
			open := f.ops.openMetadata
			f.ops.openMetadata = func(path string) (networkMetadataFile, error) {
				if triggered {
					later++
				}
				if phase == "last optional absence" && path == filepath.Join(f.root, "sys/class/net/eth1/duplex") {
					stop()
					return nil, syscall.ENOENT
				}
				file, err := open(path)
				if nilNetworkValue(file) {
					return file, err
				}
				owned++
				var hook func()
				if phase == "last uevent close" && path == filepath.Join(f.root, "sys/class/net/eth1/device/uevent") {
					hook = stop
				}
				if phase == "hard close cancel" && path == filepath.Join(f.root, "sys/class/net/eth1/device/uevent") {
					// The real acquired descriptor is still closed exactly once.
					return &networkCloseFailure{networkMetadataFile: file, hook: stop, closes: &closed}, err
				}
				return &networkCloseObserver{networkMetadataFile: file, hook: hook, closes: &closed}, err
			}
			if phase == "last driver link" {
				readlink := f.ops.readlink
				f.ops.readlink = func(path string, p []byte) (int, error) {
					if path == filepath.Join(f.root, "sys/class/net/eth1/device/driver") {
						n := copy(p, "driver/e1000e")
						stop()
						return n, nil
					}
					return readlink(path, p)
				}
			}
			if phase == "software absence" {
				stat := f.ops.stat
				f.ops.stat = func(path string) (os.FileInfo, error) {
					if path == filepath.Join(f.root, "sys/class/net/eth1/device") {
						stop()
						return nil, syscall.ENOENT
					}
					return stat(path)
				}
			}
			if phase == "empty registry close" || phase == "last registry name" {
				d := networkEntries(0)
				if phase == "empty registry close" {
					d.closeHook = stop
				} else {
					d.entries = []os.DirEntry{&networkEntry{name: "lo", hook: stop}}
				}
				f.ops.openRegistry = func(string) (networkRegistryDirectory, error) { return d, nil }
			}
			got := runPlatformWith(ctx, Options{Root: f.root}, f.ops)
			want := StatusAborted
			if phase == "hard close cancel" {
				want = StatusError
			}
			if !triggered || got.Status != want || later != 0 || owned != closed || got.NetworkTrafficGenerated {
				t.Fatal("late cancellation or fault concealed or later work continued")
			}
			if phase == "hard close cancel" && got.Detail != errNetworkMetadataClose.Error() {
				t.Fatal("close failure lost to cancellation")
			}
		})
	}
}

type networkCloseFailure struct {
	networkMetadataFile
	hook   func()
	closes *int
}

func (f *networkCloseFailure) Close() error {
	err := f.networkMetadataFile.Close()
	(*f.closes)++
	f.hook()
	if err != nil {
		return err
	}
	return os.ErrPermission
}

func TestNetworkAcquisitionNormalRegistryAndDriverLinksRemainUsable(t *testing.T) {
	root := t.TempDir()
	device := filepath.Join(root, "sys/devices/owned/net/eth0")
	for role, value := range map[string]string{"flags": "0x1002\n", "operstate": "down\n", "carrier": "0\n", "device/uevent": "DRIVER=fallback\n"} {
		mustWrite(t, filepath.Join(device, role), value)
	}
	if err := os.MkdirAll(filepath.Join(root, "sys/class/net"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(device, filepath.Join(root, "sys/class/net/eth0")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../drivers/virtio_net", filepath.Join(device, "device/driver")); err != nil {
		t.Fatal(err)
	}
	got := Run(t.Context(), Options{Root: root})
	if got.Status != StatusPass || got.HardwareInterfaces != 1 || got.Interfaces[0].Driver != "virtio_net" || got.NetworkTrafficGenerated {
		t.Fatal("ordinary sysfs links were rejected")
	}
	for _, withUevent := range []bool{false, true} {
		name := "missing both"
		if withUevent {
			name = "missing link with uevent"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			base := filepath.Join(root, "sys/class/net/eth0")
			mustWrite(t, filepath.Join(base, "flags"), "0x1002\n")
			if err := os.MkdirAll(filepath.Join(base, "device"), 0o700); err != nil {
				t.Fatal(err)
			}
			if withUevent {
				mustWrite(t, filepath.Join(base, "device/uevent"), "DRIVER=e1000e\n")
			}
			path := filepath.Join(base, "device/driver")
			var buffer [32]byte
			// Exercise the actual syscall and production adapter, not a synthetic
			// zero-count ENOENT which would miss the kernel error ABI.
			if n, err := syscall.Readlink(path, buffer[:]); n != -1 || err != syscall.ENOENT {
				t.Fatal("unexpected Linux readlink error ABI")
			}
			if n, err := realNetworkOperations().readlink(path, buffer[:]); n != 0 || err != syscall.ENOENT {
				t.Fatal("production adapter did not normalize exact syscall error")
			}
			got := Run(t.Context(), Options{Root: root})
			want := StatusInconclusive
			if withUevent {
				want = StatusPass
			}
			if got.Status != want || got.HardwareInterfaces != 1 || got.NetworkTrafficGenerated {
				t.Fatal("actual missing link broke bounded fallback")
			}
			if withUevent {
				if got.DriverBoundInterfaces != 1 || got.Interfaces[0].Driver != "e1000e" {
					t.Fatal("uevent fallback was lost")
				}
			} else if got.DriverBoundInterfaces != 0 || got.Interfaces[0].Driver != "" {
				t.Fatal("missing driver invented binding")
			}
		})
	}
}
