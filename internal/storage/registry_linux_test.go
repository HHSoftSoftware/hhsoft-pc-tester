//go:build linux

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRunRegistryFailuresPreserveUnsafeCountsAndWire(t *testing.T) {
	failure := errors.New("untrusted filesystem diagnostic")
	const errorWire = `{"schema":"hhsoft.pc-tester.storage-readiness/v1","status":"error","eligible_devices":0,"readable_devices":0,"writable_mounts":1,"media_bytes":0,"media_content_read":false,"media_content_writes":false,"detail":"cannot read block device registry"}`
	const overflowWire = `{"schema":"hhsoft.pc-tester.storage-readiness/v1","status":"error","eligible_devices":0,"readable_devices":0,"writable_mounts":1,"media_bytes":0,"media_content_read":false,"media_content_writes":false,"detail":"block device registry exceeds the supported limit"}`
	const abortedWire = `{"schema":"hhsoft.pc-tester.storage-readiness/v1","status":"aborted","eligible_devices":0,"readable_devices":0,"writable_mounts":1,"media_bytes":0,"media_content_read":false,"media_content_writes":false,"detail":"cancelled"}`
	for _, test := range []struct {
		name                    string
		step                    registryReadStep
		closeErr                error
		cancelRead, cancelClose bool
		want                    string
	}{
		{"partial read error", registryReadStep{entries: registryEntries("sda"), err: failure}, nil, false, false, errorWire},
		{"read error and cancellation", registryReadStep{entries: registryEntries("sda"), err: failure}, nil, true, false, errorWire},
		{"joined read cancellation", registryReadStep{err: errors.Join(context.Canceled, failure)}, nil, true, false, errorWire},
		{"joined EOF failure", registryReadStep{err: errors.Join(io.EOF, failure)}, nil, true, false, errorWire},
		{"overflow and cancellation", registryReadStep{entries: make([]os.DirEntry, 257), err: io.EOF}, nil, true, false, overflowWire},
		{"no progress and cancellation", registryReadStep{}, nil, true, false, errorWire},
		{"nil entry and cancellation", registryReadStep{entries: []os.DirEntry{nil}, err: io.EOF}, nil, true, false, errorWire},
		{"EOF and cancellation", registryReadStep{entries: registryEntries("sda"), err: io.EOF}, nil, true, false, abortedWire},
		{"cancel during close", registryReadStep{entries: registryEntries("sda"), err: io.EOF}, nil, false, true, abortedWire},
		{"close error with cancellation", registryReadStep{entries: registryEntries("sda"), err: io.EOF}, failure, false, true, errorWire},
		{"joined close cancellation", registryReadStep{err: io.EOF}, errors.Join(context.Canceled, failure), true, true, errorWire},
		{"earlier read error then cancelled close", registryReadStep{err: failure}, nil, false, true, errorWire},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixtureReadinessMedia(t)
			mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 8:0 / /mounted rw - ext4 source rw\n")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancelRead {
				test.step.hook = cancel
			}
			directory := &registryDirectoryFixture{steps: []registryReadStep{test.step}, closeErr: test.closeErr}
			if test.cancelClose {
				directory.closeHook = cancel
			}
			fixture := validNodeOperations()
			operations := fixture.operations()
			opens := 0
			operations.openRegistry = func(path string) (blockRegistryDirectory, error) {
				opens++
				if path != filepath.Join(root, "sys/class/block") {
					t.Fatal("registry opener escaped the configured observation path")
				}
				return directory, nil
			}
			result := runPlatformWith(ctx, Options{Root: root}, operations)
			encoded, err := json.Marshal(result)
			if err != nil || string(encoded) != test.want {
				t.Fatalf("readiness wire or failure priority changed: %s, %v", encoded, err)
			}
			if opens != 1 || directory.closeCalls != 1 || len(directory.requests) != 1 || directory.unexpectedRead || fixture.identityCalls != 0 || fixture.openCalls != 0 || fixture.file.closeCalls != 0 {
				t.Fatalf("directory admission leaked node work: directories=%d/%d identities=%d nodes=%d/%d", opens, directory.closeCalls, fixture.identityCalls, fixture.openCalls, fixture.file.closeCalls)
			}
		})
	}
}

func TestRunRegistryLimitCountsPartitionsAndExcludedInterfaces(t *testing.T) {
	for _, count := range []int{255, 256, 257} {
		t.Run(fmt.Sprintf("all-entries-%d", count), func(t *testing.T) {
			root := fixtureReadinessMedia(t)
			mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), neutralMountInfo)
			for _, row := range []struct{ name, number string }{{"sda1", "8:1"}, {"dm-0", "253:0"}, {"md0", "9:0"}} {
				mustWrite(t, filepath.Join(root, "sys/class/block", row.name, "dev"), row.number+"\n")
			}
			mustWrite(t, filepath.Join(root, "sys/class/block/sda1/partition"), "1\n")
			mustWrite(t, filepath.Join(root, "sys/class/block/sda1/size"), "1\n")
			for index := 0; index < count-4; index++ {
				mustWrite(t, filepath.Join(root, "sys/class/block", fmt.Sprintf("loop%d", index), "dev"), fmt.Sprintf("7:%d\n", index))
			}
			fixture := validNodeOperations()
			operations := fixture.operations()
			var directory *trackedRegistryDirectory
			operations.openRegistry = func(path string) (blockRegistryDirectory, error) {
				opened, err := openBlockRegistry(path)
				if err != nil {
					return nil, err
				}
				directory = &trackedRegistryDirectory{blockRegistryDirectory: opened}
				return directory, nil
			}
			operations.deviceNumber = func(path string) (deviceNumber, bool) {
				if directory == nil || directory.closeCalls != 1 {
					t.Fatal("identity was processed before complete directory closure")
				}
				fixture.identityCalls++
				return readDeviceNumber(path)
			}
			result := runPlatformWith(t.Context(), Options{Root: root}, operations)
			if directory == nil || directory.closeCalls != 1 {
				t.Fatal("real registry was not closed exactly once")
			}
			if count == 257 {
				if result.Status != StatusError || result.Detail != "block device registry exceeds the supported limit" || len(result.Devices) != 0 || result.EligibleDevices != 0 || result.ReadableDevices != 0 || fixture.identityCalls != 0 || fixture.openCalls != 0 || fixture.file.closeCalls != 0 {
					t.Fatalf("excluded entries bypassed the all-entry boundary: %+v", result)
				}
			} else if result.Status != StatusPass || len(result.Devices) != 2 || result.Devices[0].Interface != "sda" || result.Devices[1].Interface != "sda1" || result.EligibleDevices != 1 || result.ReadableDevices != 1 || fixture.identityCalls != count+2 || fixture.openCalls != 1 || fixture.file.closeCalls != 1 {
				t.Fatalf("valid boundary lost registry or media observations: %+v, identities=%d", result, fixture.identityCalls)
			}
			if result.MediaContentRead || result.MediaContentWrites {
				t.Fatal("directory acquisition claimed media I/O")
			}
		})
	}
}

func TestRunRegistrySortingPreservesFirstErrorAndPartialPrefix(t *testing.T) {
	for index, names := range [][]string{{"zzz", "sdb", "sda"}, {"sdb", "sda", "zzz"}, {"sda", "zzz", "sdb"}} {
		t.Run(fmt.Sprintf("permutation-%d", index), func(t *testing.T) {
			root := fixtureReadinessMedia(t)
			mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), neutralMountInfo)
			mustWrite(t, filepath.Join(root, "sys/class/block/sdb/dev"), "8:16\n")
			mustWrite(t, filepath.Join(root, "sys/class/block/sdb/size"), "36028797018963968\n")
			mustWrite(t, filepath.Join(root, "sys/class/block/zzz/dev"), "invalid\n")
			directory := &registryDirectoryFixture{steps: []registryReadStep{{entries: registryEntries(names...), err: io.EOF}}}
			fixture := validNodeOperations()
			operations := fixture.operations()
			operations.openRegistry = registryOpener(directory)
			var observed []string
			operations.deviceNumber = func(path string) (deviceNumber, bool) {
				if directory.closeCalls != 1 {
					t.Fatal("metadata preceded registry closure")
				}
				observed = append(observed, filepath.Base(filepath.Dir(path)))
				return readDeviceNumber(path)
			}
			result := runPlatformWith(t.Context(), Options{Root: root}, operations)
			wantDevices := []DeviceResult{{Interface: "sda", Kind: "media", SizeBytes: 1048576}}
			if result.Status != StatusError || result.Detail != "block device size exceeds the supported limit" || !reflect.DeepEqual(result.Devices, wantDevices) || !reflect.DeepEqual(observed, []string{"sda", "sdb"}) || result.EligibleDevices != 1 || result.MediaBytes != 1048576 || result.ReadableDevices != 0 || fixture.openCalls != 0 || fixture.file.closeCalls != 0 {
				t.Fatalf("directory order changed the first error or retained prefix: %+v, observed=%v", result, observed)
			}
			checkRegistryRequests(t, directory)
		})
	}
}

func TestRunRegistryChecksLastIdentityAtExactLimit(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("duplicate-%t", duplicate), func(t *testing.T) {
			root := fixtureReadinessMedia(t)
			mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), neutralMountInfo)
			entries := registryEntries("zzz", "sda")
			numbers := map[string]deviceNumber{"sda": {major: 8}}
			for index := 253; index >= 0; index-- {
				name := fmt.Sprintf("loop-%03d", index)
				entries = append(entries, registryEntryFixture{name: name})
				numbers[name] = deviceNumber{major: 7, minor: uint32(index)}
			}
			if duplicate {
				numbers["zzz"] = deviceNumber{major: 7}
			}
			directory := &registryDirectoryFixture{steps: []registryReadStep{{entries: entries, err: io.EOF}}}
			fixture := validNodeOperations()
			operations := fixture.operations()
			operations.openRegistry = registryOpener(directory)
			var observed []string
			operations.deviceNumber = func(path string) (deviceNumber, bool) {
				name := filepath.Base(filepath.Dir(path))
				observed = append(observed, name)
				number, ok := numbers[name]
				return number, ok
			}
			result := runPlatformWith(t.Context(), Options{Root: root}, operations)
			want := "block device registry lacks a canonical device number"
			if duplicate {
				want = "block device registry repeats a device number"
			}
			if result.Status != StatusError || result.Detail != want || len(observed) != 256 || observed[255] != "zzz" || !reflect.DeepEqual(result.Devices, []DeviceResult{{Interface: "sda", Kind: "media", SizeBytes: 1048576}}) || fixture.openCalls != 0 || fixture.file.closeCalls != 0 {
				t.Fatalf("last admitted identity was skipped or prefix was lost: %+v, observed=%d", result, len(observed))
			}
			checkRegistryRequests(t, directory)
		})
	}
}

func TestRunRegistryCancellationAtCallerBoundaryIncludesDisklessViews(t *testing.T) {
	for _, names := range [][]string{nil, {"sda"}} {
		t.Run(fmt.Sprintf("entries-%d", len(names)), func(t *testing.T) {
			root := fixtureReadinessMedia(t)
			mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 8:0 / /mounted rw - ext4 source rw\n")
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			directory := &registryDirectoryFixture{steps: []registryReadStep{{entries: registryEntries(names...), err: io.EOF}}}
			ctx := &registryBoundaryContext{Context: parent, ready: func() bool { return directory.closeCalls != 0 }, cancel: cancel, at: 3}
			fixture := validNodeOperations()
			operations := fixture.operations()
			operations.openRegistry = registryOpener(directory)
			result := runPlatformWith(ctx, Options{Root: root}, operations)
			if ctx.calls != 3 || result.Status != StatusAborted || result.Detail != "cancelled" || result.WritableMounts != 1 || len(result.Devices) != 0 || result.EligibleDevices != 0 || result.ReadableDevices != 0 || fixture.identityCalls != 0 || fixture.openCalls != 0 || fixture.file.closeCalls != 0 || directory.closeCalls != 1 {
				t.Fatalf("caller cancellation acquired registry credit: %+v, checks=%d", result, ctx.calls)
			}
		})
	}
}

func TestRunRegistryPreservesDisklessAndMissingDirectorySemantics(t *testing.T) {
	const disklessWire = `{"schema":"hhsoft.pc-tester.storage-readiness/v1","status":"inconclusive","eligible_devices":0,"readable_devices":0,"writable_mounts":0,"media_bytes":0,"media_content_read":false,"media_content_writes":false,"detail":"no supported media-bearing block devices were discovered"}`
	for _, kind := range []string{"empty", "missing", "regular file", "malformed mount"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), neutralMountInfo)
			path := filepath.Join(root, "sys/class/block")
			if kind == "empty" || kind == "malformed mount" {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal("cannot create owned empty registry")
				}
			}
			if kind == "regular file" {
				mustWrite(t, path, "not a directory")
			}
			if kind == "malformed mount" {
				mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), "")
			}
			fixture := validNodeOperations()
			operations := fixture.operations()
			opens := 0
			operations.openRegistry = func(path string) (blockRegistryDirectory, error) {
				opens++
				return openBlockRegistry(path)
			}
			result := runPlatformWith(t.Context(), Options{Root: root}, operations)
			if kind == "empty" {
				encoded, err := json.Marshal(result)
				if err != nil || string(encoded) != disklessWire {
					t.Fatalf("diskless wire changed: %s, %v", encoded, err)
				}
			} else if result.Status != StatusError {
				t.Fatalf("missing observation became an empty registry: %+v", result)
			}
			wantOpens := 1
			if kind == "malformed mount" {
				wantOpens = 0
			}
			if opens != wantOpens || fixture.identityCalls != 0 || fixture.openCalls != 0 || fixture.file.closeCalls != 0 || result.MediaContentRead || result.MediaContentWrites {
				t.Fatal("diskless or invalid registry acquired device observations")
			}
		})
	}
}

func TestRunRegistryRetainsNormalSysfsSymlinkViews(t *testing.T) {
	for _, linkedRoot := range []bool{false, true} {
		t.Run(fmt.Sprintf("linked-root-%t", linkedRoot), func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), neutralMountInfo)
			device := filepath.Join(root, "sys/devices/fixture/block/sda")
			mustWrite(t, filepath.Join(device, "dev"), "8:0\n")
			mustWrite(t, filepath.Join(device, "size"), "2048\n")
			registry := filepath.Join(root, "sys/class/block")
			if linkedRoot {
				registry = filepath.Join(root, "sys/fixture-registry")
			}
			if err := os.MkdirAll(registry, 0700); err != nil {
				t.Fatal("cannot create owned registry")
			}
			if err := os.Symlink(device, filepath.Join(registry, "sda")); err != nil {
				t.Fatal("cannot create owned sysfs-style entry")
			}
			if linkedRoot {
				if err := os.MkdirAll(filepath.Join(root, "sys/class"), 0700); err != nil {
					t.Fatal("cannot create owned class directory")
				}
				if err := os.Symlink(registry, filepath.Join(root, "sys/class/block")); err != nil {
					t.Fatal("cannot create owned registry link")
				}
			}
			fixture := validNodeOperations()
			operations := fixture.operations()
			operations.deviceNumber = readDeviceNumber
			result := runPlatformWith(t.Context(), Options{Root: root}, operations)
			if result.Status != StatusPass || len(result.Devices) != 1 || result.Devices[0].Interface != "sda" || result.ReadableDevices != 1 || fixture.openCalls != 1 || fixture.file.closeCalls != 1 || result.MediaContentRead || result.MediaContentWrites {
				t.Fatalf("valid sysfs symlinks were rejected: %+v", result)
			}
		})
	}
}
