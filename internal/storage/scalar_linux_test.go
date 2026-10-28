//go:build linux

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

type trackedScalarFile struct {
	blockScalarFile
	closed *int
}

func (file *trackedScalarFile) Close() error {
	(*file.closed)++
	return file.blockScalarFile.Close()
}

type scalarRunFixture struct {
	root           string
	operations     blockNodeOperations
	nodes          map[string]*nodeFileFixture
	nodeOpens      int
	directoryOpens int
	directory      *trackedRegistryDirectory
	scalarOpens    []string
	scalarAcquired int
	scalarClosed   int
	scalarOverride func(string) (blockScalarFile, error, bool)
}

func scalarValue(value string) *string { return &value }

func newScalarRunFixture(t *testing.T, capacities map[string]*string) *scalarRunFixture {
	t.Helper()
	fixture := &scalarRunFixture{root: t.TempDir(), nodes: make(map[string]*nodeFileFixture)}
	mustWrite(t, filepath.Join(fixture.root, "proc/self/mountinfo"), neutralMountInfo)
	if err := os.MkdirAll(filepath.Join(fixture.root, "sys/class/block"), 0700); err != nil {
		t.Fatal("cannot create owned registry")
	}
	for name, capacity := range capacities {
		var number string
		var rdev uint64
		switch name {
		case "sda":
			number, rdev = "8:0", 0x800
		case "sdb":
			number, rdev = "8:16", 0x810
		case "sda1":
			number, rdev = "8:1", 0x801
		case "dm-0":
			number, rdev = "253:0", 0xfd00
		case "md0":
			number, rdev = "9:0", 0x900
		case "loop0":
			number, rdev = "7:0", 0x700
		default:
			t.Fatal("fixture needs an explicit distinct identity")
		}
		base := filepath.Join(fixture.root, "sys/class/block", name)
		mustWrite(t, filepath.Join(base, "dev"), number+"\n")
		if capacity != nil {
			mustWrite(t, filepath.Join(base, "size"), *capacity)
		}
		if name == "sda1" {
			mustWrite(t, filepath.Join(base, "partition"), "1\n")
		}
		fixture.nodes[name] = &nodeFileFixture{info: blockInfoFixture(rdev)}
	}
	fixture.operations = realBlockNodeOperations()
	fixture.operations.lstat = func(path string) (os.FileInfo, error) {
		file := fixture.nodes[filepath.Base(path)]
		if filepath.Dir(path) != filepath.Join(fixture.root, "dev") || file == nil {
			t.Fatal("node pathname escaped fixture")
		}
		return file.info, nil
	}
	fixture.operations.open = func(path string, flags int, mode os.FileMode) (blockNodeFile, error) {
		file := fixture.nodes[filepath.Base(path)]
		if filepath.Dir(path) != filepath.Join(fixture.root, "dev") || file == nil || flags != os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOFOLLOW || mode != 0 {
			t.Fatal("node open escaped read-only fixture contract")
		}
		fixture.nodeOpens++
		return file, nil
	}
	fixture.operations.openRegistry = func(path string) (blockRegistryDirectory, error) {
		fixture.directoryOpens++
		directory, err := openBlockRegistry(path)
		if err != nil {
			return nil, err
		}
		fixture.directory = &trackedRegistryDirectory{blockRegistryDirectory: directory}
		return fixture.directory, nil
	}
	fixture.operations.openScalar = func(path string) (blockScalarFile, error) {
		if fixture.directory == nil || fixture.directory.closeCalls != 1 {
			t.Fatal("scalar observation preceded directory closure")
		}
		relative, err := filepath.Rel(filepath.Join(fixture.root, "sys/class/block"), path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Fatal("scalar path escaped fixture")
		}
		fixture.scalarOpens = append(fixture.scalarOpens, filepath.ToSlash(relative))
		var file blockScalarFile
		var handled bool
		if fixture.scalarOverride != nil {
			file, err, handled = fixture.scalarOverride(filepath.ToSlash(relative))
		}
		if !handled {
			file, err = openBlockScalar(path)
		}
		if file != nil {
			fixture.scalarAcquired++
			return &trackedScalarFile{blockScalarFile: file, closed: &fixture.scalarClosed}, err
		}
		return nil, err
	}
	return fixture
}

func (fixture *scalarRunFixture) checkOwnership(t *testing.T, result Result, wantNodes int) {
	t.Helper()
	if fixture.directoryOpens != 1 || fixture.directory == nil || fixture.directory.closeCalls != 1 || fixture.scalarAcquired != fixture.scalarClosed || fixture.nodeOpens != wantNodes {
		t.Fatalf("ownership: directory opens=%d, scalar acquired/closed=%d/%d, node opens=%d", fixture.directoryOpens, fixture.scalarAcquired, fixture.scalarClosed, fixture.nodeOpens)
	}
	closed := 0
	for _, node := range fixture.nodes {
		closed += node.closeCalls
	}
	if closed != wantNodes || result.MediaContentRead || result.MediaContentWrites {
		t.Fatal("node closure or media-content contract changed")
	}
}

func TestRunScalarDistinctMediaControlAndLiteralWire(t *testing.T) {
	fixture := newScalarRunFixture(t, map[string]*string{"sda": scalarValue("2048\n"), "sdb": scalarValue("4096\n")})
	result := runPlatformWith(t.Context(), Options{Root: fixture.root}, fixture.operations)
	const want = `{"schema":"hhsoft.pc-tester.storage-readiness/v1","status":"pass","devices":[{"interface":"sda","kind":"media","size_bytes":1048576,"node_present":true,"readable":true},{"interface":"sdb","kind":"media","size_bytes":2097152,"node_present":true,"readable":true}],"eligible_devices":2,"readable_devices":2,"writable_mounts":0,"media_bytes":3145728,"media_content_read":false,"media_content_writes":false}`
	data, err := json.Marshal(result)
	if err != nil || string(data) != want {
		t.Fatalf("distinct-device passing control changed: %s, %v", data, err)
	}
	fixture.checkOwnership(t, result, 2)
	if fixture.nodes["sda"].statCalls != 1 || fixture.nodes["sdb"].statCalls != 1 {
		t.Fatal("both simulated descriptors must be inspected")
	}
}

func TestRunScalarRequiredSizeCannotHideBesideValidMedia(t *testing.T) {
	for _, test := range []struct {
		name        string
		value       *string
		readFailure bool
	}{
		{"missing", nil, false},
		{"empty", scalarValue(""), false},
		{"whitespace", scalarValue(" \t\n"), false},
		{"nonnumeric", scalarValue("synthetic-size-canary"), false},
		{"negative", scalarValue("-1\n"), false},
		{"raw 257 bytes", scalarValue(strings.Repeat(" ", 255) + "0\n"), false},
		{"uint64 overflow", scalarValue("18446744073709551616\n"), false},
		{"unreadable", scalarValue("2048\n"), true},
	} {
		for _, badName := range []string{"sda", "sdb"} {
			t.Run(test.name+"/"+badName, func(t *testing.T) {
				capacities := map[string]*string{"sda": scalarValue("2048\n"), "sdb": scalarValue("2048\n")}
				capacities[badName] = test.value
				fixture := newScalarRunFixture(t, capacities)
				var failed *scalarFileFixture
				if test.readFailure {
					failed = scalarTextFixture("")
					failed.read = func([]byte) (int, error) { return 0, errors.New("synthetic-read-canary") }
					fixture.scalarOverride = func(path string) (blockScalarFile, error, bool) {
						if path == badName+"/size" {
							return failed, nil, true
						}
						return nil, nil, false
					}
				}
				result := runPlatformWith(t.Context(), Options{Root: fixture.root}, fixture.operations)
				var wantDevices []DeviceResult
				wantEligible, wantBytes := 0, uint64(0)
				if badName == "sdb" {
					wantDevices = []DeviceResult{{Interface: "sda", Kind: "media", SizeBytes: 1048576}}
					wantEligible, wantBytes = 1, 1048576
				}
				if result.Status != StatusError || result.Detail != "cannot read block device size" || !reflect.DeepEqual(result.Devices, wantDevices) || result.EligibleDevices != wantEligible || result.MediaBytes != wantBytes || result.ReadableDevices != 0 {
					t.Fatalf("invalid required size was excluded or credited: %+v", result)
				}
				encoded, err := json.Marshal(result)
				if err != nil || strings.Contains(string(encoded), "canary") || strings.Contains(string(encoded), fixture.root) {
					t.Fatal("observation diagnostics escaped fixed text")
				}
				fixture.checkOwnership(t, result, 0)
				if failed != nil && (failed.closeCalls != 1 || len(failed.requests) != 1) {
					t.Fatal("failed required scalar ownership changed")
				}
			})
		}
	}
}

func TestRunScalarZeroAndNumericBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, size      string
		companion       bool
		status          Status
		detail          string
		kind            string
		bytes           uint64
		eligible, nodes int
	}{
		{"zero alone", "0\n", false, StatusInconclusive, "no supported media-bearing block devices were discovered", "empty", 0, 0, 0},
		{"zero beside media", "0\n", true, StatusPass, "", "empty", 1048576, 1, 1},
		{"trim and leading zeros", " \t0002048\r\n", false, StatusPass, "", "media", 1048576, 1, 1},
		{"raw 256 accepted", strings.Repeat(" ", 254) + "0\n", false, StatusInconclusive, "no supported media-bearing block devices were discovered", "empty", 0, 0, 0},
		{"last multiplication value", "36028797018963967", false, StatusPass, "", "media", 18446744073709551104, 1, 1},
		{"multiplication overflow", "36028797018963968", false, StatusError, "block device size exceeds the supported limit", "", 0, 0, 0},
		{"aggregate overflow", "36028797018963967", true, StatusError, "aggregate block capacity exceeds the supported limit", "media", 18446744073709551104, 2, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			capacities := map[string]*string{"sda": scalarValue(test.size)}
			if test.companion {
				capacities["sdb"] = scalarValue("2048")
			}
			fixture := newScalarRunFixture(t, capacities)
			result := runPlatformWith(t.Context(), Options{Root: fixture.root}, fixture.operations)
			if result.Status != test.status || result.Detail != test.detail || result.MediaBytes != test.bytes || result.EligibleDevices != test.eligible || result.ReadableDevices != test.nodes {
				t.Fatalf("numeric policy changed: %+v", result)
			}
			if test.kind == "" {
				if len(result.Devices) != 0 {
					t.Fatal("overflow row was retained")
				}
			} else if len(result.Devices) == 0 || result.Devices[0].Kind != test.kind {
				t.Fatal("zero/media classification changed")
			}
			fixture.checkOwnership(t, result, test.nodes)
			if test.name == "zero alone" {
				const want = `{"schema":"hhsoft.pc-tester.storage-readiness/v1","status":"inconclusive","devices":[{"interface":"sda","kind":"empty"}],"eligible_devices":0,"readable_devices":0,"writable_mounts":0,"media_bytes":0,"media_content_read":false,"media_content_writes":false,"detail":"no supported media-bearing block devices were discovered"}`
				data, err := json.Marshal(result)
				if err != nil || string(data) != want {
					t.Fatalf("zero wire changed: %s, %v", data, err)
				}
			}
		})
	}
}

func TestRunScalarOptionalFieldsAndKernelStateRemainObservable(t *testing.T) {
	for _, test := range []struct {
		name, ro, removable, state string
		missing                    bool
		wantRO, wantRemovable      bool
		wantState                  string
		status                     Status
	}{
		{"absent optional values", "", "", "", true, false, false, "", StatusPass},
		{"normal trimmed state", " 1\n", "1\t", " Running \n", false, true, true, "Running", StatusPass},
		{"exact boolean policy", "01", "true", "standby", false, false, false, "standby", StatusPass},
		{"offline", "0", "0", " OFFLINE \n", false, false, false, "OFFLINE", StatusInconclusive},
		{"dead", "0", "0", "DeAd", false, false, false, "DeAd", StatusInconclusive},
		{"blocked", "0", "0", " blocked ", false, false, false, "blocked", StatusInconclusive},
		{"quiesce", "0", "0", "QUIESCE", false, false, false, "QUIESCE", StatusInconclusive},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newScalarRunFixture(t, map[string]*string{"sda": scalarValue("2048")})
			if !test.missing {
				base := filepath.Join(fixture.root, "sys/class/block/sda")
				mustWrite(t, filepath.Join(base, "ro"), test.ro)
				mustWrite(t, filepath.Join(base, "removable"), test.removable)
				mustWrite(t, filepath.Join(base, "device/state"), test.state)
			}
			result := runPlatformWith(t.Context(), Options{Root: fixture.root}, fixture.operations)
			if len(result.Devices) != 1 || result.Status != test.status {
				t.Fatalf("optional policy changed: %+v", result)
			}
			device := result.Devices[0]
			if device.ReadOnly != test.wantRO || device.Removable != test.wantRemovable || device.State != test.wantState || !device.Readable {
				t.Fatalf("captured metadata changed: %+v", device)
			}
			if test.status == StatusInconclusive && device.ReadinessIssue != "kernel reports an unusable device state" {
				t.Fatal("known bad state was hidden")
			}
			fixture.checkOwnership(t, result, 1)
		})
	}
}

func TestRunScalarRequiredAndOptionalErrorCancellationPolicy(t *testing.T) {
	for _, test := range []struct {
		name, field, value       string
		observationErr, closeErr error
		cancelRead, cancelClose  bool
		status                   Status
		detail                   string
	}{
		{"required read failure and cancellation", "size", "", context.Canceled, nil, true, false, StatusError, "cannot read block device size"},
		{"required complete invalid and cancellation", "size", "bad-size", io.EOF, nil, true, false, StatusError, "cannot read block device size"},
		{"required uint64 overflow and cancelled close", "size", "18446744073709551616", io.EOF, nil, false, true, StatusError, "cannot read block device size"},
		{"required multiplication overflow and cancellation", "size", "36028797018963968", io.EOF, nil, true, false, StatusError, "block device size exceeds the supported limit"},
		{"required valid and cancellation", "size", "2048", io.EOF, nil, true, false, StatusAborted, "cancelled"},
		{"required zero and cancelled close", "size", "0", io.EOF, nil, false, true, StatusAborted, "cancelled"},
		{"optional ro failure active", "ro", "1", io.ErrUnexpectedEOF, nil, false, false, StatusPass, ""},
		{"optional removable failure active", "removable", "1", context.DeadlineExceeded, nil, false, false, StatusPass, ""},
		{"optional state failure active", "device/state", "offline", errors.Join(io.EOF, context.Canceled), nil, false, false, StatusPass, ""},
		{"optional ro failure cancelled", "ro", "1", io.ErrUnexpectedEOF, nil, true, false, StatusAborted, "cancelled"},
		{"optional removable failure cancelled", "removable", "1", context.Canceled, nil, true, false, StatusAborted, "cancelled"},
		{"optional state failure cancelled", "device/state", "offline", io.ErrUnexpectedEOF, nil, false, true, StatusAborted, "cancelled"},
		{"optional ro close failure active", "ro", "1", io.EOF, os.ErrPermission, false, false, StatusError, "block device metadata closure could not be confirmed"},
		{"optional removable close failure cancelled", "removable", "1", io.EOF, context.Canceled, false, true, StatusError, "block device metadata closure could not be confirmed"},
		{"optional state read and close failure cancelled", "device/state", "offline", io.ErrUnexpectedEOF, os.ErrPermission, true, false, StatusError, "block device metadata closure could not be confirmed"},
		{"required read and close failure cancelled", "size", "0", context.Canceled, os.ErrPermission, true, false, StatusError, "block device metadata closure could not be confirmed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newScalarRunFixture(t, map[string]*string{"sda": scalarValue("2048")})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			file := scalarTextFixture("")
			file.closeErr = test.closeErr
			file.read = func(data []byte) (int, error) {
				if test.cancelRead {
					cancel()
				}
				return copy(data, test.value), test.observationErr
			}
			if test.cancelClose {
				file.closeHook = cancel
			}
			fixture.scalarOverride = func(path string) (blockScalarFile, error, bool) {
				if path == "sda/"+test.field {
					return file, nil, true
				}
				return nil, nil, false
			}
			result := runPlatformWith(ctx, Options{Root: fixture.root}, fixture.operations)
			wantNodes := 0
			if test.status == StatusPass {
				wantNodes = 1
			}
			if result.Status != test.status || result.Detail != test.detail || file.closeCalls != 1 {
				t.Fatalf("consumer error/cancellation policy changed: %+v, close=%d", result, file.closeCalls)
			}
			if test.status != StatusPass && (len(result.Devices) != 0 || result.EligibleDevices != 0 || result.MediaBytes != 0 || result.ReadableDevices != 0) {
				t.Fatal("failed or cancelled metadata was credited")
			}
			if test.status == StatusPass && (len(result.Devices) != 1 || result.Devices[0].ReadOnly || result.Devices[0].Removable || result.Devices[0].State != "") {
				t.Fatal("failed optional data became an observed value")
			}
			fixture.checkOwnership(t, result, wantNodes)
		})
	}
}

func TestRunScalarPartitionAndExcludedPolicies(t *testing.T) {
	for _, test := range []struct {
		name     string
		capacity *string
		cancel   bool
		status   Status
		detail   string
	}{
		{"missing", nil, false, StatusInconclusive, "no supported media-bearing block devices were discovered"},
		{"invalid", scalarValue("bad"), false, StatusInconclusive, "no supported media-bearing block devices were discovered"},
		{"parse overflow", scalarValue("18446744073709551616"), false, StatusInconclusive, "no supported media-bearing block devices were discovered"},
		{"known multiplication overflow", scalarValue("36028797018963968"), false, StatusError, "block device size exceeds the supported limit"},
		{"known overflow and cancel", scalarValue("36028797018963968"), true, StatusError, "block device size exceeds the supported limit"},
		{"missing with cancel", nil, true, StatusAborted, "cancelled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newScalarRunFixture(t, map[string]*string{"sda1": test.capacity})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				fixture.scalarOverride = func(path string) (blockScalarFile, error, bool) {
					if path != "sda1/size" {
						return nil, nil, false
					}
					if test.capacity == nil {
						cancel()
						return nil, os.ErrNotExist, true
					}
					file := scalarTextFixture("")
					file.read = func(data []byte) (int, error) { cancel(); return copy(data, *test.capacity), io.EOF }
					return file, nil, true
				}
			}
			result := runPlatformWith(ctx, Options{Root: fixture.root}, fixture.operations)
			if result.Status != test.status || result.Detail != test.detail || result.EligibleDevices != 0 || result.MediaBytes != 0 {
				t.Fatalf("partition policy changed: %+v", result)
			}
			if test.status == StatusInconclusive && !reflect.DeepEqual(result.Devices, []DeviceResult{{Interface: "sda1", Kind: "partition"}}) {
				t.Fatal("unknown partition capacity was not retained as optional")
			}
			if test.status != StatusInconclusive && len(result.Devices) != 0 {
				t.Fatal("invalid partition observation gained row credit")
			}
			fixture.checkOwnership(t, result, 0)
		})
	}
	for _, name := range []string{"dm-0", "md0", "loop0"} {
		t.Run(name, func(t *testing.T) {
			fixture := newScalarRunFixture(t, map[string]*string{name: nil})
			result := runPlatformWith(t.Context(), Options{Root: fixture.root}, fixture.operations)
			if result.Status != StatusInconclusive || len(result.Devices) != 0 || len(fixture.scalarOpens) != 0 {
				t.Fatalf("excluded interface gained scalar or media work: %+v", result)
			}
			fixture.checkOwnership(t, result, 0)
		})
	}
}

// Cancellation becomes visible immediately after a semantic observation's last
// context check returns. No number of internal Err calls is assumed.
type scalarBoundaryContext struct {
	context.Context
	ready  func() bool
	cancel context.CancelFunc
	fired  bool
}

func (ctx *scalarBoundaryContext) Err() error {
	err := ctx.Context.Err()
	if !ctx.fired && ctx.ready() {
		ctx.fired = true
		ctx.cancel()
	}
	return err
}

func TestRunScalarCallerCancellationBeforeAnyCurrentCredit(t *testing.T) {
	for _, test := range []struct {
		name, entry, size string
		excluded, prefix  bool
	}{
		{"positive media", "sdb", "2048", false, true},
		{"final zero", "sdb", "0", false, true},
		{"final partition", "sda1", "1", false, true},
		{"zero only", "sda", "0", false, false},
		{"partition only", "sda1", "1", false, false},
		{"excluded interface", "loop0", "", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			capacities := map[string]*string{test.entry: scalarValue(test.size)}
			if test.excluded {
				capacities[test.entry] = nil
			}
			if test.prefix {
				capacities["sda"] = scalarValue("2048")
			}
			fixture := newScalarRunFixture(t, capacities)
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			complete := false
			ctx := &scalarBoundaryContext{Context: parent, ready: func() bool { return complete }, cancel: cancel}
			if test.excluded {
				fixture.operations.deviceNumber = func(path string) (deviceNumber, bool) {
					number, ok := readDeviceNumber(path)
					if filepath.Base(filepath.Dir(path)) == test.entry {
						complete = true
					}
					return number, ok
				}
			} else {
				fixture.scalarOverride = func(path string) (blockScalarFile, error, bool) {
					if path != test.entry+"/device/state" {
						return nil, nil, false
					}
					file := scalarTextFixture("running")
					file.closeHook = func() { complete = true }
					return file, nil, true
				}
			}
			result := runPlatformWith(ctx, Options{Root: fixture.root}, fixture.operations)
			var want []DeviceResult
			wantEligible, wantBytes := 0, uint64(0)
			if test.prefix {
				want = []DeviceResult{{Interface: "sda", Kind: "media", SizeBytes: 1048576}}
				wantEligible, wantBytes = 1, 1048576
			}
			if !ctx.fired || result.Status != StatusAborted || result.Detail != "cancelled" || !reflect.DeepEqual(result.Devices, want) || result.EligibleDevices != wantEligible || result.MediaBytes != wantBytes || result.ReadableDevices != 0 {
				t.Fatalf("caller credited cancelled current metadata: %+v, fired=%t", result, ctx.fired)
			}
			fixture.checkOwnership(t, result, 0)
		})
	}
}

func TestRunScalarFailureRetainsWritableCountAndLiteralWire(t *testing.T) {
	const errorWire = `{"schema":"hhsoft.pc-tester.storage-readiness/v1","status":"error","eligible_devices":0,"readable_devices":0,"writable_mounts":1,"media_bytes":0,"media_content_read":false,"media_content_writes":false,"detail":"cannot read block device size"}`
	const closeWire = `{"schema":"hhsoft.pc-tester.storage-readiness/v1","status":"error","eligible_devices":0,"readable_devices":0,"writable_mounts":1,"media_bytes":0,"media_content_read":false,"media_content_writes":false,"detail":"block device metadata closure could not be confirmed"}`
	const abortWire = `{"schema":"hhsoft.pc-tester.storage-readiness/v1","status":"aborted","eligible_devices":0,"readable_devices":0,"writable_mounts":1,"media_bytes":0,"media_content_read":false,"media_content_writes":false,"detail":"cancelled"}`
	for _, test := range []struct {
		name, value       string
		readErr, closeErr error
		cancel            bool
		want              string
	}{
		{"invalid", "synthetic-size-canary", io.EOF, nil, false, errorWire},
		{"read error", "", errors.New("synthetic-path-canary"), nil, false, errorWire},
		{"close failure and cancel", "0", io.EOF, errors.New("synthetic-close-canary"), true, closeWire},
		{"cancel clean zero", "0", io.EOF, nil, true, abortWire},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newScalarRunFixture(t, map[string]*string{"sda": scalarValue("2048")})
			mustWrite(t, filepath.Join(fixture.root, "proc/self/mountinfo"), "1 0 8:0 / /mounted rw - ext4 source rw\n")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			file := scalarTextFixture("")
			file.closeErr = test.closeErr
			file.read = func(data []byte) (int, error) {
				if test.cancel {
					cancel()
				}
				return copy(data, test.value), test.readErr
			}
			fixture.scalarOverride = func(path string) (blockScalarFile, error, bool) {
				if path == "sda/size" {
					return file, nil, true
				}
				return nil, nil, false
			}
			result := runPlatformWith(ctx, Options{Root: fixture.root}, fixture.operations)
			data, err := json.Marshal(result)
			if err != nil || string(data) != test.want {
				t.Fatalf("unsafe-count/error wire changed: %s, %v", data, err)
			}
			fixture.checkOwnership(t, result, 0)
		})
	}
}

func TestRunScalarLateFailureKeepsEarlierUnsafeMetadataPrefix(t *testing.T) {
	for _, kind := range []string{"invalid size", "close failure", "clean cancellation"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newScalarRunFixture(t, map[string]*string{"sda": scalarValue("2048"), "sdb": scalarValue("2048")})
			mustWrite(t, filepath.Join(fixture.root, "proc/self/mountinfo"), "1 0 8:0 / /mounted rw - ext4 source rw\n")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			file := scalarTextFixture("bad-size")
			wantStatus, wantDetail := StatusError, "cannot read block device size"
			if kind == "close failure" {
				file.closeErr = os.ErrPermission
				wantDetail = "block device metadata closure could not be confirmed"
			}
			if kind == "clean cancellation" {
				file = scalarTextFixture("0")
				file.closeHook = cancel
				wantStatus, wantDetail = StatusAborted, "cancelled"
			}
			fixture.scalarOverride = func(path string) (blockScalarFile, error, bool) {
				if path == "sdb/size" {
					return file, nil, true
				}
				return nil, nil, false
			}
			result := runPlatformWith(ctx, Options{Root: fixture.root}, fixture.operations)
			want := []DeviceResult{{Interface: "sda", Kind: "media", SizeBytes: 1048576, Mounted: true, WritableMount: true}}
			if result.Status != wantStatus || result.Detail != wantDetail || result.WritableMounts != 1 || !reflect.DeepEqual(result.Devices, want) || result.EligibleDevices != 1 || result.MediaBytes != 1048576 || result.ReadableDevices != 0 {
				t.Fatalf("late failure erased prior unsafe evidence or credited current metadata: %+v", result)
			}
			fixture.checkOwnership(t, result, 0)
		})
	}
}

func TestOpenBlockScalarUsesOwnedRegularFileAndNormalSymlink(t *testing.T) {
	root := t.TempDir()
	value := filepath.Join(root, "value")
	mustWrite(t, value, " 2048\n")
	link := filepath.Join(root, "link")
	if err := os.Symlink(value, link); err != nil {
		t.Fatal("cannot create owned scalar symlink")
	}
	for _, path := range []string{value, link} {
		file, err := openBlockScalar(path)
		if err != nil || file == nil {
			t.Fatal("cannot open owned scalar")
		}
		opened, ok := file.(*os.File)
		if !ok {
			file.Close()
			t.Fatal("scalar opener did not return an owned OS file")
		}
		flags, _, flagsErr := syscall.Syscall(syscall.SYS_FCNTL, opened.Fd(), syscall.F_GETFL, 0)
		descriptorFlags, _, descriptorErr := syscall.Syscall(syscall.SYS_FCNTL, opened.Fd(), syscall.F_GETFD, 0)
		if flagsErr != 0 || descriptorErr != 0 || flags&syscall.O_ACCMODE != syscall.O_RDONLY || flags&syscall.O_NONBLOCK == 0 || descriptorFlags&syscall.FD_CLOEXEC == 0 {
			file.Close()
			t.Fatal("scalar descriptor flags changed")
		}
		data, readErr := readBlockScalar(t.Context(), path, func(string) (blockScalarFile, error) { return file, nil })
		if readErr != nil || string(data) != " 2048\n" {
			t.Fatalf("owned regular scalar or normal link rejected: %q, %v", data, readErr)
		}
		if _, err := opened.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("real scalar descriptor was not closed")
		}
	}
	file, err := openBlockScalar(filepath.Join(root, "missing"))
	if file != nil || err == nil {
		t.Fatal("missing real scalar produced a usable or typed-nil handle")
	}
	data, err := readBlockScalar(t.Context(), root, openBlockScalar)
	if data != nil || err != errBlockScalarRead {
		t.Fatal("directory admitted as scalar metadata")
	}
}

func TestRunScalarDependencyIsRequiredBeforeObservation(t *testing.T) {
	fixture := newScalarRunFixture(t, map[string]*string{"sda": scalarValue("2048")})
	fixture.operations.openScalar = nil
	result := runPlatformWith(t.Context(), Options{Root: fixture.root}, fixture.operations)
	if result.Status != StatusError || result.Detail != "storage readiness dependencies are incomplete" || fixture.directoryOpens != 0 || fixture.nodeOpens != 0 {
		t.Fatalf("missing scalar dependency reached observation: %+v", result)
	}
}
