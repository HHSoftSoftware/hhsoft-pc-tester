//go:build linux

package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type nodeInfoFixture struct {
	mode os.FileMode
	sys  any
}

func (nodeInfoFixture) Name() string           { return "sda" }
func (nodeInfoFixture) Size() int64            { return 0 }
func (info nodeInfoFixture) Mode() os.FileMode { return info.mode }
func (nodeInfoFixture) ModTime() time.Time     { return time.Time{} }
func (nodeInfoFixture) IsDir() bool            { return false }
func (info nodeInfoFixture) Sys() any          { return info.sys }

func blockInfoFixture(number uint64) os.FileInfo {
	return nodeInfoFixture{mode: os.ModeDevice, sys: &syscall.Stat_t{Rdev: number}}
}

type nodeFileFixture struct {
	info       os.FileInfo
	statErr    error
	closeErr   error
	statHook   func()
	closeHook  func()
	statCalls  int
	closeCalls int
}

func (file *nodeFileFixture) Stat() (os.FileInfo, error) {
	file.statCalls++
	if file.statHook != nil {
		file.statHook()
	}
	return file.info, file.statErr
}

func (file *nodeFileFixture) Close() error {
	file.closeCalls++
	if file.closeHook != nil {
		file.closeHook()
	}
	return file.closeErr
}

type nodeOperationsFixture struct {
	lstatInfo     os.FileInfo
	lstatErr      error
	openErr       error
	file          nodeFileFixture
	identity      deviceNumber
	identityOK    bool
	identityHook  func(int)
	lstatHook     func()
	openHook      func(int)
	identityCalls int
	openCalls     int
}

func validNodeOperations() *nodeOperationsFixture {
	return &nodeOperationsFixture{lstatInfo: blockInfoFixture(0x800),
		file:     nodeFileFixture{info: blockInfoFixture(0x800)},
		identity: deviceNumber{major: 8}, identityOK: true}
}

func (fixture *nodeOperationsFixture) operations() blockNodeOperations {
	return blockNodeOperations{
		lstat: func(string) (os.FileInfo, error) {
			if fixture.lstatHook != nil {
				fixture.lstatHook()
			}
			return fixture.lstatInfo, fixture.lstatErr
		},
		open: func(_ string, flags int, _ os.FileMode) (blockNodeFile, error) {
			fixture.openCalls++
			if fixture.openHook != nil {
				fixture.openHook(flags)
			}
			if fixture.openErr != nil {
				return nil, fixture.openErr
			}
			return &fixture.file, nil
		},
		deviceNumber: func(string) (deviceNumber, bool) {
			fixture.identityCalls++
			if fixture.identityHook != nil {
				fixture.identityHook(fixture.identityCalls)
			}
			return fixture.identity, fixture.identityOK
		},
	}
}

func TestBlockNodeBindsRegistryPathAndDescriptor(t *testing.T) {
	for _, test := range []struct {
		name     string
		change   func(*nodeOperationsFixture)
		wantOpen int
	}{
		{"stable wrong node", func(f *nodeOperationsFixture) {
			f.lstatInfo, f.file.info = blockInfoFixture(0x810), blockInfoFixture(0x810)
		}, 0},
		{"wrong path correct descriptor", func(f *nodeOperationsFixture) { f.lstatInfo = blockInfoFixture(0x810) }, 0},
		{"correct path changed descriptor", func(f *nodeOperationsFixture) { f.file.info = blockInfoFixture(0x810) }, 1},
		{"missing initial registry", func(f *nodeOperationsFixture) { f.identityOK = false }, 0},
		{"changed final registry", func(f *nodeOperationsFixture) {
			f.identityHook = func(n int) {
				if n == 2 {
					f.identity.minor = 16
				}
			}
		}, 1},
		{"missing final registry", func(f *nodeOperationsFixture) {
			f.identityHook = func(n int) {
				if n == 2 {
					f.identityOK = false
				}
			}
		}, 1},
		{"lstat error", func(f *nodeOperationsFixture) { f.lstatErr = os.ErrNotExist }, 0},
		{"open error", func(f *nodeOperationsFixture) { f.openErr = os.ErrPermission }, 1},
		{"fstat error", func(f *nodeOperationsFixture) { f.file.statErr = errors.New("fixture fstat failure") }, 1},
		{"close error", func(f *nodeOperationsFixture) { f.file.closeErr = errors.New("fixture close failure") }, 1},
		{"regular node", func(f *nodeOperationsFixture) { f.lstatInfo = nodeInfoFixture{} }, 0},
		{"character node", func(f *nodeOperationsFixture) {
			f.lstatInfo = nodeInfoFixture{mode: os.ModeDevice | os.ModeCharDevice, sys: &syscall.Stat_t{Rdev: 0x800}}
		}, 0},
		{"symlink node", func(f *nodeOperationsFixture) {
			f.lstatInfo = nodeInfoFixture{mode: os.ModeSymlink, sys: &syscall.Stat_t{Rdev: 0x800}}
		}, 0},
		{"nil node metadata", func(f *nodeOperationsFixture) { f.lstatInfo = nil }, 0},
		{"typed nil stat", func(f *nodeOperationsFixture) {
			var missing *syscall.Stat_t
			f.lstatInfo = nodeInfoFixture{mode: os.ModeDevice, sys: missing}
		}, 0},
		{"descriptor lacks stat", func(f *nodeOperationsFixture) { f.file.info = nodeInfoFixture{mode: os.ModeDevice} }, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := validNodeOperations()
			test.change(fixture)
			result := inspectBlockNode(t.Context(), "/fixture", "sda", deviceNumber{major: 8}, fixture.operations())
			wantClose := test.wantOpen
			if fixture.openErr != nil {
				wantClose = 0
			}
			if result.readable || result.issue == "" || fixture.openCalls != test.wantOpen || fixture.file.closeCalls != wantClose {
				t.Fatalf("invalid node was credited or ownership changed: %+v opens=%d closes=%d", result, fixture.openCalls, fixture.file.closeCalls)
			}
			if (result.cleanupError != nil) != (fixture.file.closeErr != nil) {
				t.Fatal("close uncertainty classification changed")
			}
		})
	}
	fixture := validNodeOperations()
	fixture.openHook = func(flags int) {
		if flags != os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOFOLLOW {
			t.Fatal("node open omitted no-follow or changed read-only flags")
		}
	}
	result := inspectBlockNode(t.Context(), "/fixture", "sda", deviceNumber{major: 8}, fixture.operations())
	if !result.present || !result.readable || result.issue != "" || result.cleanupError != nil || fixture.openCalls != 1 || fixture.file.closeCalls != 1 || fixture.identityCalls != 2 {
		t.Fatalf("matching node did not complete its bounded observation: %+v", result)
	}
}

func TestBlockNodeSymlinkInsertionFailsAtNoFollowOpen(t *testing.T) {
	fixture := validNodeOperations()
	fixture.openHook = func(flags int) {
		if flags&syscall.O_NOFOLLOW != 0 {
			fixture.openErr = syscall.ELOOP
		}
	}
	result := inspectBlockNode(t.Context(), "/fixture", "sda", deviceNumber{major: 8}, fixture.operations())
	if result.readable || fixture.openCalls != 1 || fixture.file.closeCalls != 0 {
		t.Fatal("symlink inserted after Lstat was followed or credited")
	}
}

func TestBlockNodeCancellationNeverOpensLaterOrCreditsLateResults(t *testing.T) {
	for _, phase := range []string{"before", "initial identity", "lstat", "open", "fstat", "final identity", "close"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fixture := validNodeOperations()
			switch phase {
			case "before":
				cancel()
			case "initial identity":
				fixture.identityHook = func(n int) {
					if n == 1 {
						cancel()
					}
				}
			case "lstat":
				fixture.lstatHook = cancel
			case "open":
				fixture.openHook = func(int) { cancel() }
			case "fstat":
				fixture.file.statHook = cancel
			case "final identity":
				fixture.identityHook = func(n int) {
					if n == 2 {
						cancel()
					}
				}
			case "close":
				fixture.file.closeHook = cancel
			}
			result := inspectBlockNode(ctx, "/fixture", "sda", deviceNumber{major: 8}, fixture.operations())
			wantOpen := 1
			if phase == "before" || phase == "initial identity" || phase == "lstat" {
				wantOpen = 0
			}
			if result.readable || result.issue != "cancelled" || fixture.openCalls != wantOpen || fixture.file.closeCalls != wantOpen {
				t.Fatalf("late observation credited or ownership changed: %+v opens=%d closes=%d", result, fixture.openCalls, fixture.file.closeCalls)
			}
		})
	}
}

func TestBlockDeviceNumbersRetainHighBits(t *testing.T) {
	for _, test := range []struct {
		raw  uint64
		want deviceNumber
	}{
		{0, deviceNumber{}}, {0x810, deviceNumber{major: 8, minor: 16}},
		{0x0000100000100801, deviceNumber{major: 4104, minor: 257}},
		{^uint64(0), deviceNumber{major: ^uint32(0), minor: ^uint32(0)}},
	} {
		got, ok := directBlockNumber(blockInfoFixture(test.raw))
		if !ok || got != test.want {
			t.Fatalf("device number lost bits: raw=%x got=%+v want=%+v", test.raw, got, test.want)
		}
	}
}

func TestRegistryIdentityAcceptsOnlyOneOptionalTrailingLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev")
	for _, text := range []string{"8:0", "8:0\n", "4294967295:4294967295\n"} {
		mustWrite(t, path, text)
		if _, ok := readDeviceNumber(path); !ok {
			t.Fatalf("canonical registry identity rejected: %q", text)
		}
	}
	for _, text := range []string{" 8:0", "8:0 ", "8:0\n\n", "8:0\r\n", "8:0\t", "+8:0\n", "08:0\n", "8:00\n", "8:\x000", strings.Repeat("9", 4096)} {
		mustWrite(t, path, text)
		if _, ok := readDeviceNumber(path); ok {
			t.Fatal("noncanonical registry identity accepted")
		}
	}
}

func TestMountRegistryUsesCanonicalDeviceIdentity(t *testing.T) {
	for _, text := range []string{"08:0", "8:00", "+8:0", "8:-1", "4294967296:0", "8:0:1"} {
		if _, err := parseMounts(strings.NewReader("1 0 " + text + " / /target rw - ext4 /dev/sda rw\n")); err == nil {
			t.Fatal("malformed mount device identity could bypass writable lookup")
		}
	}
}

func TestRunRejectsDuplicateRegistryIdentitiesBeforeAnyNodeOpen(t *testing.T) {
	for _, name := range []string{"sdb", "dm-0", "loop0"} {
		for _, mounted := range []bool{false, true} {
			t.Run(name+"/"+map[bool]string{false: "unmounted", true: "writable"}[mounted], func(t *testing.T) {
				root := fixtureReadinessMedia(t)
				mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), neutralMountInfo)
				mustWrite(t, filepath.Join(root, "sys/class/block", name, "dev"), "8:0\n")
				if mounted {
					mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 8:0 / /target rw - ext4 /dev/sda rw\n")
				}
				fixture := validNodeOperations()
				ops := fixture.operations()
				ops.deviceNumber = readDeviceNumber
				result := runPlatformWith(t.Context(), Options{Root: root}, ops)
				if result.Status != StatusError || result.Detail != "block device registry repeats a device number" || fixture.openCalls != 0 || result.MediaContentRead || result.MediaContentWrites {
					t.Fatalf("duplicate registry escaped: %+v", result)
				}
				if mounted && result.WritableMounts != 1 {
					t.Fatal("duplicate erased a writable observation")
				}
			})
		}
	}
}

func TestRunKeepsIdentityAndCleanupFailuresOutOfPass(t *testing.T) {
	for _, state := range []string{"correct", "wrong node", "close failure", "cancel after open", "final registry changed"} {
		t.Run(state, func(t *testing.T) {
			root := fixtureReadinessMedia(t)
			mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), neutralMountInfo)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fixture := validNodeOperations()
			switch state {
			case "wrong node":
				fixture.lstatInfo, fixture.file.info = blockInfoFixture(0x810), blockInfoFixture(0x810)
			case "close failure":
				fixture.file.closeErr = errors.New("fixture close failure")
			case "cancel after open":
				fixture.openHook = func(int) { cancel() }
			case "final registry changed":
				fixture.identityHook = func(n int) {
					if n == 3 {
						fixture.identity.minor = 16
					}
				}
			}
			result := runPlatformWith(ctx, Options{Root: root}, fixture.operations())
			want := StatusInconclusive
			if state == "correct" {
				want = StatusPass
			}
			if state == "close failure" {
				want = StatusError
			}
			if state == "cancel after open" {
				want = StatusAborted
			}
			if result.Status != want || result.MediaContentRead || result.MediaContentWrites || (result.ReadableDevices == 1) != (state == "correct") {
				t.Fatalf("bad readiness outcome: %+v", result)
			}
			if fixture.openCalls != fixture.file.closeCalls {
				t.Fatal("owned descriptor was not closed once")
			}
		})
	}
}

type cancelAfterCloseContext struct {
	context.Context
	cancel       context.CancelFunc
	closed       bool
	afterClose   int
	cancelAtCall int
}

func (ctx *cancelAfterCloseContext) Err() error {
	if ctx.closed {
		ctx.afterClose++
		if ctx.afterClose == ctx.cancelAtCall {
			ctx.cancel()
		}
	}
	return ctx.Context.Err()
}

func TestRunRevokesCurrentReadabilityAtCallerCancellationBoundary(t *testing.T) {
	for _, test := range []struct {
		name       string
		cancelAt   int
		closeError bool
		status     Status
		issue      string
	}{
		{"clean control", 0, false, StatusPass, ""},
		{"cancel after deferred check", 2, false, StatusAborted, "cancelled"},
		{"close failure before caller cancellation", 2, true, StatusError, "block node closure could not be confirmed"},
		{"close failure with cancellation", 1, true, StatusError, "block node closure could not be confirmed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixtureReadinessMedia(t)
			mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), neutralMountInfo)
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx := &cancelAfterCloseContext{Context: parent, cancel: cancel, cancelAtCall: test.cancelAt}
			fixture := validNodeOperations()
			fixture.file.closeHook = func() { ctx.closed = true }
			if test.closeError {
				fixture.file.closeErr = errors.New("fixture close failure")
			}
			result := runPlatformWith(ctx, Options{Root: root}, fixture.operations())
			if result.Status != test.status || len(result.Devices) != 1 || result.Devices[0].ReadinessIssue != test.issue {
				t.Fatalf("caller boundary changed outcome: %+v", result)
			}
			wantReadable := test.status == StatusPass
			if result.Devices[0].Readable != wantReadable || (result.ReadableDevices == 1) != wantReadable {
				t.Fatalf("current-device readability disagreed with accepted count: %+v", result)
			}
			if fixture.openCalls != 1 || fixture.file.closeCalls != 1 || result.MediaContentRead || result.MediaContentWrites {
				t.Fatal("boundary changed ownership or performed media I/O")
			}
			if test.closeError && ctx.afterClose != 1 {
				t.Fatal("cleanup error lost priority to caller cancellation")
			}
			if test.cancelAt == 2 && !test.closeError && ctx.afterClose != 2 {
				t.Fatal("fixture did not isolate cancellation after the deferred final check")
			}
		})
	}
}

func TestRunStillRejectsMissingMountRegistryBeforeNodeObservation(t *testing.T) {
	root := fixtureReadinessMedia(t)
	fixture := validNodeOperations()
	result := runPlatformWith(t.Context(), Options{Root: root}, fixture.operations())
	if result.Status != StatusError || result.Detail != "cannot verify block mount safety" {
		t.Fatalf("missing mount registry was not rejected: %+v", result)
	}
	if fixture.identityCalls != 0 || fixture.openCalls != 0 || fixture.file.closeCalls != 0 || len(result.Devices) != 0 || result.ReadableDevices != 0 || result.MediaContentRead || result.MediaContentWrites {
		t.Fatal("missing mount registry admitted node observations or media I/O")
	}
}
