//go:build linux

package inputtest

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func parseDevices(source io.Reader, categories []string) ([]discoveredDevice, error) {
	data, err := readInputBytes(context.Background(), source, maximumInputRegistryBytes, 4096)
	if err != nil {
		return nil, err
	}
	return parseInputRegistry(context.Background(), data, categories)
}

type readinessInfo struct {
	mode        os.FileMode
	raw         syscall.Stat_t
	sysOverride any
	override    bool
}

func (info *readinessInfo) Name() string       { return "owned" }
func (info *readinessInfo) Size() int64        { return 0 }
func (info *readinessInfo) Mode() os.FileMode  { return info.mode }
func (info *readinessInfo) ModTime() time.Time { return time.Time{} }
func (info *readinessInfo) IsDir() bool        { return info.mode.IsDir() }
func (info *readinessInfo) Sys() any {
	if info.override {
		return info.sysOverride
	}
	return &info.raw
}

type readinessFile struct {
	fixture              *readinessFixture
	name, role           string
	info                 os.FileInfo
	read                 io.Reader
	readFunc             func([]byte) (int, error)
	statFunc             func() (os.FileInfo, error)
	closeFunc            func() error
	closes, reads, stats int
}

func (file *readinessFile) Fd() uintptr { return 41 }
func (file *readinessFile) Stat() (os.FileInfo, error) {
	file.fixture.touch("stat", file.name)
	file.stats++
	if file.statFunc != nil {
		return file.statFunc()
	}
	return file.info, nil
}
func (file *readinessFile) Read(p []byte) (int, error) {
	file.fixture.touch("read", file.name)
	file.reads++
	if file.readFunc != nil {
		return file.readFunc(p)
	}
	if file.read != nil {
		return file.read.Read(p)
	}
	return 0, syscall.EAGAIN
}
func (file *readinessFile) Close() error {
	file.fixture.log = append(file.fixture.log, "close:"+file.role+":"+file.name)
	file.closes++
	if file.closes != 1 {
		file.fixture.t.Fatal("handle closed more than once")
	}
	if file.closeFunc != nil {
		return file.closeFunc()
	}
	return nil
}

type readinessRoot struct {
	fixture *readinessFixture
	prefix  string
	token   *readinessFile
}

func (root *readinessRoot) Close() error { return root.token.Close() }
func (root *readinessRoot) Lstat(name string) (os.FileInfo, error) {
	path := root.prefix + name
	root.fixture.touch("lstat", path)
	if root.fixture.lstat != nil {
		return root.fixture.lstat(path)
	}
	info, ok := root.fixture.infos[path]
	if !ok {
		return nil, &os.PathError{Op: "statat", Path: "owned", Err: syscall.ENOENT}
	}
	return info, nil
}
func (root *readinessRoot) OpenPath(name string, flags int) (inputPathFile, error) {
	f := root.fixture
	path := root.prefix + name
	f.touch("path", path)
	if flags != inputOpenPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC {
		f.t.Fatal("path open gained readable/write/follow flags")
	}
	file := &readinessFile{fixture: f, name: path, role: "path", info: f.infos[path]}
	var result inputPathFile = file
	var err error
	if f.openPath != nil {
		result, err = f.openPath(path, file)
	}
	if !nilInputValue(result) {
		f.acquired = append(f.acquired, result.(*readinessFile))
	}
	return result, err
}
func (root *readinessRoot) OpenRoot(name string) (inputReadinessRoot, error) {
	f := root.fixture
	f.touch("root", "sys")
	if root.prefix != "" || name != "sys" {
		f.t.Fatal("unexpected secondary root")
	}
	file := &readinessFile{fixture: f, name: "sys", role: "root"}
	child := &readinessRoot{fixture: f, prefix: "sys/", token: file}
	var result inputReadinessRoot = child
	var err error
	if f.openSys != nil {
		result, err = f.openSys(child)
	}
	if !nilInputValue(result) {
		f.acquired = append(f.acquired, file)
	}
	return result, err
}

type readinessFixture struct {
	t          *testing.T
	infos      map[string]os.FileInfo
	data       map[string]string
	acquired   []*readinessFile
	log        []string
	stop       bool
	clock      time.Time
	root       *readinessRoot
	lstat      func(string) (os.FileInfo, error)
	openPath   func(string, *readinessFile) (inputPathFile, error)
	openReader func(string, *readinessFile) (inputReadableFile, error)
	openSys    func(*readinessRoot) (inputReadinessRoot, error)
	onNow      func()
}

func (f *readinessFixture) touch(op, path string) {
	if f.stop {
		f.t.Fatalf("operation after stop: %s %s", op, path)
	}
	f.log = append(f.log, op+":"+path)
}
func (f *readinessFixture) operations() inputReadinessOperations {
	return inputReadinessOperations{
		openRoot: func(path string) (inputReadinessRoot, error) {
			f.touch("root", "fixture")
			if path != "/fixture" {
				f.t.Fatal("unexpected probe root")
			}
			f.acquired = append(f.acquired, f.root.token)
			return f.root, nil
		},
		openReadable: func(path inputPathFile) (inputReadableFile, error) {
			p, ok := path.(*readinessFile)
			if !ok || p.closes != 0 || p.role != "path" {
				f.t.Fatal("readable open did not use owned metadata descriptor")
			}
			f.touch("readable", p.name)
			file := &readinessFile{fixture: f, name: p.name, role: "reader", info: p.info}
			if data, ok := f.data[p.name]; ok {
				file.read = strings.NewReader(data)
			}
			var result inputReadableFile = file
			var err error
			if f.openReader != nil {
				result, err = f.openReader(p.name, file)
			}
			if !nilInputValue(result) {
				f.acquired = append(f.acquired, result.(*readinessFile))
			}
			return result, err
		},
		now: func() time.Time {
			if f.onNow != nil {
				f.onNow()
			}
			return f.clock
		},
		wait: func(ctx context.Context, duration time.Duration) { f.clock = f.clock.Add(duration) },
	}
}
func (f *readinessFixture) closed() {
	f.t.Helper()
	if len(f.acquired) == 0 {
		f.t.Fatal("no owned handles exercised")
	}
	for _, file := range f.acquired {
		if file.closes != 1 {
			f.t.Fatalf("acquired handle not closed exactly once: %s %s", file.role, file.name)
		}
	}
}
func (f *readinessFixture) count(prefix string) int {
	n := 0
	for _, op := range f.log {
		if strings.HasPrefix(op, prefix) {
			n++
		}
	}
	return n
}
func (f *readinessFixture) run(ctx context.Context, duration time.Duration) Result {
	return runInputWithOperations(ctx, Options{Root: "/fixture", Duration: duration, Categories: []string{"keyboard"}}, f.operations())
}

func readinessMetadata(inode uint64, character bool, minor uint32) *readinessInfo {
	mode := os.FileMode(0o400)
	rdev := uint64(0)
	if character {
		mode |= os.ModeDevice | os.ModeCharDevice
		rdev = uint64(13)<<8 | uint64(minor&255) | uint64(minor&^255)<<12
	}
	return &readinessInfo{mode: mode, raw: syscall.Stat_t{Dev: 7, Ino: inode, Rdev: rdev}}
}
func newReadinessFixture(t *testing.T, count int) *readinessFixture {
	f := &readinessFixture{t: t, infos: map[string]os.FileInfo{}, data: map[string]string{}, clock: time.Unix(100, 0)}
	f.root = &readinessRoot{fixture: f, token: &readinessFile{fixture: f, name: "fixture", role: "root"}}
	var registry strings.Builder
	for index := 0; index < count; index++ {
		fmt.Fprintf(&registry, "N: Name=\"Fixture Keyboard %d\"\nH: Handlers=kbd event%d\n\n", index, index)
		name := fmt.Sprintf("event%d", index)
		f.infos["dev/input/"+name] = readinessMetadata(uint64(100+index), true, uint32(64+index))
		f.infos["sys/class/input/"+name+"/dev"] = readinessMetadata(uint64(1000+index), false, 0)
		f.data["sys/class/input/"+name+"/dev"] = fmt.Sprintf("13:%d\n", 64+index)
	}
	f.infos["proc/bus/input/devices"] = readinessMetadata(1, false, 0)
	f.data["proc/bus/input/devices"] = registry.String()
	return f
}

func inputFrame(kind, code uint16, value int32) []byte {
	size, offset := int(unsafe.Sizeof(inputEvent{})), int(unsafe.Offsetof(inputEvent{}.Type))
	data := make([]byte, size)
	binary.NativeEndian.PutUint16(data[offset:], kind)
	binary.NativeEndian.PutUint16(data[offset+2:], code)
	binary.NativeEndian.PutUint32(data[offset+4:], uint32(value))
	return data
}

func TestInputReadinessOwnsAndClosesBeforeReadableCredit(t *testing.T) {
	f := newReadinessFixture(t, 2)
	result := f.run(t.Context(), 0)
	f.closed()
	want := []DeviceResult{{Interface: "event0", Category: "keyboard", Name: "Fixture Keyboard 0", Readable: true}, {Interface: "event1", Category: "keyboard", Name: "Fixture Keyboard 1", Readable: true}}
	if result.Status != StatusPass || result.Mode != "readiness" || !reflect.DeepEqual(result.Devices, want) || !reflect.DeepEqual(result.ObservedCategories, []string{"keyboard"}) {
		t.Fatalf("unexpected closed readiness %#v", result)
	}
	firstClosed, nextOpened := -1, -1
	for index, op := range f.log {
		if op == "close:path:dev/input/event0" {
			firstClosed = index
		}
		if op == "path:dev/input/event1" {
			nextOpened = index
		}
	}
	if firstClosed < 0 || nextOpened <= firstClosed {
		t.Fatal("later device opened before earlier closure")
	}
	for _, file := range f.acquired {
		if strings.HasPrefix(file.name, "dev/input/") && file.reads != 0 {
			t.Fatal("readiness consumed input event bytes")
		}
	}
}

type inputBoundaryContext struct {
	context.Context
	calls, after int
}

func (ctx *inputBoundaryContext) Err() error {
	ctx.calls++
	if ctx.calls > ctx.after {
		return context.Canceled
	}
	return nil
}

func TestInputReadinessRegistryBoundsIncludeIrrelevantRecords(t *testing.T) {
	for _, row := range []struct {
		name, data string
		valid      bool
	}{
		{"line_exact", strings.Repeat("x", 65536), true}, {"line_overflow", strings.Repeat("x", 65537), false},
		{"record_exact", strings.Repeat("X\n\n", 2048), true}, {"record_overflow", strings.Repeat("X\n\n", 2049), false},
		{"lines_exact", strings.Repeat("\n", 32768), true}, {"lines_overflow", strings.Repeat("\n", 32769), false},
		{"raw_overflow", strings.Repeat("\n", (1<<20)+1), false},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, err := parseDevices(strings.NewReader(row.data), []string{"keyboard"})
			if (err == nil) != row.valid || len(got) != 0 {
				t.Fatal("registry limit omitted irrelevant input")
			}
		})
	}
	for _, row := range []struct{ name, data, reason string }{
		{"duplicate_final", "N: Name=\"Keyboard\"\nH: Handlers=kbd event0\n\nN: Name=\"Keyboard\"\nH: Handlers=kbd event0", "input device registry contains a duplicate event interface"},
		{"record_limit_final", strings.Repeat("X\n\n", 2048) + "X", "input device registry exceeds the supported record limit"},
	} {
		t.Run(row.name, func(t *testing.T) {
			lines := strings.Count(row.data, "\n") + 1
			ctx := &inputBoundaryContext{Context: t.Context(), after: lines}
			_, err := parseInputRegistry(ctx, []byte(row.data), []string{"keyboard"})
			if err == nil || err.Error() != row.reason || ctx.calls != lines || ctx.Err() != context.Canceled {
				t.Fatal("final hard record fault was downgraded by final cancellation")
			}
		})
	}
}

func TestInputReadinessRegistryOwnershipAndProtocolFailuresStop(t *testing.T) {
	for _, mode := range []string{"path-plus-error", "reader-plus-error", "nil-path", "typed-nil-reader", "nil-info", "wrong-type", "read-error", "partial-error", "zero-progress", "negative-count", "oversize-count", "joined-eof", "reader-close", "path-close"} {
		t.Run(mode, func(t *testing.T) {
			f := newReadinessFixture(t, 1)
			if mode == "nil-info" {
				f.infos["proc/bus/input/devices"] = (*readinessInfo)(nil)
			}
			if mode == "wrong-type" {
				f.infos["proc/bus/input/devices"] = readinessMetadata(1, true, 64)
			}
			f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
				if name != "proc/bus/input/devices" {
					return file, nil
				}
				if mode == "nil-path" {
					return nil, nil
				}
				if mode == "path-plus-error" {
					f.stop = true
					return file, syscall.EIO
				}
				if mode == "path-close" {
					file.closeFunc = func() error { f.stop = true; return syscall.EIO }
				}
				return file, nil
			}
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if name != "proc/bus/input/devices" {
					return file, nil
				}
				if mode == "typed-nil-reader" {
					return (*readinessFile)(nil), nil
				}
				if mode == "reader-plus-error" {
					f.stop = true
					return file, syscall.EIO
				}
				if mode == "reader-close" {
					file.closeFunc = func() error { f.stop = true; return syscall.EIO }
				}
				switch mode {
				case "read-error", "partial-error", "zero-progress", "negative-count", "oversize-count", "joined-eof":
					file.readFunc = func(p []byte) (int, error) {
						f.stop = true
						switch mode {
						case "read-error":
							return 0, syscall.EIO
						case "partial-error":
							p[0] = 'x'
							return 1, syscall.EIO
						case "zero-progress":
							return 0, nil
						case "negative-count":
							return -1, syscall.EIO
						case "oversize-count":
							return len(p) + 1, io.EOF
						default:
							return 0, errors.Join(io.EOF, syscall.EIO)
						}
					}
				}
				return file, nil
			}
			result := f.run(t.Context(), 0)
			f.closed()
			if result.Status != StatusError || len(result.Devices) != 0 || f.count("path:dev/input/") != 0 {
				t.Fatal("registry fault reached event acquisition")
			}
			if strings.Contains(mode, "close") && result.Detail != errInputClose.Error() {
				t.Fatal("registry close uncertainty lost")
			}
		})
	}
}

func TestInputReadinessRejectsWrongOrSubstitutedNodesBeforeCredit(t *testing.T) {
	for _, mode := range []string{"wrong-subsystem", "reserved-minor", "regular-path", "symlink-path", "wrong-number", "path-fd-inode", "path-fd-device", "nil-fd-info", "typed-nil-stat", "reader-inode", "final-reader-inode", "final-path-fd-inode", "final-path-inode", "final-sys-number", "duplicate-sys-number"} {
		t.Run(mode, func(t *testing.T) {
			f := newReadinessFixture(t, 2)
			node := "dev/input/event0"
			if mode == "wrong-subsystem" {
				f.data["sys/class/input/event0/dev"] = "1:3"
				f.infos[node].(*readinessInfo).raw.Rdev = 0x103
			}
			if mode == "reserved-minor" {
				f.data["sys/class/input/event0/dev"] = "13:0"
				f.infos[node].(*readinessInfo).raw.Rdev = 13 << 8
			}
			if mode == "regular-path" {
				f.infos[node] = readinessMetadata(100, false, 0)
			}
			if mode == "symlink-path" {
				f.infos[node].(*readinessInfo).mode = os.ModeSymlink | 0o400
			}
			if mode == "wrong-number" {
				f.infos[node].(*readinessInfo).raw.Rdev = 13<<8 | 66
			}
			if mode == "duplicate-sys-number" {
				f.data["sys/class/input/event1/dev"] = "13:64"
				f.infos["dev/input/event1"].(*readinessInfo).raw.Rdev = 13<<8 | 64
			}
			f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
				if name == node {
					copyInfo := *f.infos[name].(*readinessInfo)
					file.info = &copyInfo
					switch mode {
					case "path-fd-inode":
						copyInfo.raw.Ino++
					case "path-fd-device":
						copyInfo.raw.Dev++
					case "nil-fd-info":
						file.info = (*readinessInfo)(nil)
					case "typed-nil-stat":
						copyInfo.override = true
						copyInfo.sysOverride = (*syscall.Stat_t)(nil)
					}
					if mode == "final-path-fd-inode" {
						file.statFunc = func() (os.FileInfo, error) {
							latest := copyInfo
							if file.stats >= 4 {
								latest.raw.Ino++
							}
							return &latest, nil
						}
					}
				}
				return file, nil
			}
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if name == node {
					if mode == "reader-inode" {
						copyInfo := *file.info.(*readinessInfo)
						copyInfo.raw.Ino++
						file.info = &copyInfo
					}
					if mode == "final-reader-inode" {
						file.statFunc = func() (os.FileInfo, error) {
							latest := *file.info.(*readinessInfo)
							if file.stats >= 2 {
								latest.raw.Ino++
							}
							return &latest, nil
						}
					}
					if mode == "final-path-inode" {
						copyInfo := *f.infos[name].(*readinessInfo)
						copyInfo.raw.Ino++
						f.infos[name] = &copyInfo
					}
					if mode == "final-sys-number" {
						f.data["sys/class/input/event0/dev"] = "13:65"
					}
				}
				return file, nil
			}
			result := f.run(t.Context(), 0)
			f.closed()
			if result.Status != StatusError || f.count("readable:dev/input/event1") != 0 {
				t.Fatal("identity substitution continued or passed")
			}
			if mode != "duplicate-sys-number" && result.Devices[0].Readable {
				t.Fatal("rejected descriptor earned readable credit")
			}
			if mode == "wrong-subsystem" || mode == "reserved-minor" || mode == "regular-path" || mode == "symlink-path" || mode == "wrong-number" || strings.HasPrefix(mode, "path-fd") || mode == "nil-fd-info" || mode == "typed-nil-stat" {
				if f.count("readable:dev/input/") != 0 {
					t.Fatal("unverified O_PATH handle reached readable open")
				}
			}
		})
	}
}

func TestInputReadinessCloseFailureRetainsOnlyCompletedPrefix(t *testing.T) {
	for _, role := range []string{"reader", "path"} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_cancel_%v", role, cancelled), func(t *testing.T) {
				f := newReadinessFixture(t, 3)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				inject := func(name string, file *readinessFile) {
					if name == "dev/input/event1" && file.role == role {
						file.closeFunc = func() error {
							f.stop = true
							if cancelled {
								cancel()
							}
							return syscall.EIO
						}
					}
				}
				f.openPath = func(name string, file *readinessFile) (inputPathFile, error) { inject(name, file); return file, nil }
				f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
					inject(name, file)
					return file, nil
				}
				result := f.run(ctx, 0)
				f.closed()
				want := []DeviceResult{{Interface: "event0", Category: "keyboard", Name: "Fixture Keyboard 0", Readable: true}, {Interface: "event1", Category: "keyboard", Name: "Fixture Keyboard 1"}}
				if result.Status != StatusError || result.Detail != errInputClose.Error() || !reflect.DeepEqual(result.Devices, want) || !reflect.DeepEqual(result.ObservedCategories, []string{"keyboard"}) || f.count("path:sys/class/input/event2") != 0 {
					t.Fatal("uncertain later closure changed exact accepted prefix or opened later node")
				}
			})
		}
	}
}

func TestInputReadinessUnavailableNodesRemainNonPass(t *testing.T) {
	for _, phase := range []string{"statat", "openat", "open"} {
		for _, errno := range []error{syscall.ENOENT, syscall.EACCES, syscall.EPERM, errors.Join(syscall.EACCES, syscall.EIO)} {
			t.Run(phase+"_"+errno.Error(), func(t *testing.T) {
				f := newReadinessFixture(t, 2)
				node := "dev/input/event0"
				fault := &os.PathError{Op: phase, Path: "owned", Err: errno}
				if phase == "statat" {
					f.lstat = func(name string) (os.FileInfo, error) {
						if name == node {
							return nil, fault
						}
						return f.infos[name], nil
					}
				}
				if phase == "openat" {
					f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
						if name == node {
							return nil, fault
						}
						return file, nil
					}
				}
				if phase == "open" {
					f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
						if name == node {
							return nil, fault
						}
						return file, nil
					}
				}
				result := f.run(t.Context(), 0)
				f.closed()
				hard := phase == "open" && errno == syscall.ENOENT
				if _, joined := errno.(interface{ Unwrap() []error }); joined {
					hard = true
				}
				if hard {
					if result.Status != StatusError || f.count("readable:dev/input/event1") != 0 {
						t.Fatal("hard availability failure softened")
					}
				} else if result.Status != StatusInconclusive || len(result.Devices) != 2 || result.Devices[0].Readable || !result.Devices[1].Readable {
					t.Fatal("ordinary unavailable node changed compatibility disposition")
				}
			})
		}
	}
	for _, phase := range []string{"statat", "openat", "open"} {
		t.Run("typed_nil_errno_"+phase, func(t *testing.T) {
			f := newReadinessFixture(t, 2)
			node := "dev/input/event0"
			if phase == "statat" {
				f.lstat = func(name string) (os.FileInfo, error) {
					if name == node {
						return (*readinessInfo)(nil), &os.PathError{Op: "statat", Err: syscall.ENOENT}
					}
					return f.infos[name], nil
				}
			}
			if phase == "openat" {
				f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
					if name == node {
						return (*readinessFile)(nil), &os.PathError{Op: "openat", Err: syscall.ENOENT}
					}
					return file, nil
				}
			}
			if phase == "open" {
				f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
					if name == node {
						return (*readinessFile)(nil), &os.PathError{Op: "open", Err: syscall.EACCES}
					}
					return file, nil
				}
			}
			result := f.run(t.Context(), 0)
			f.closed()
			if result.Status != StatusError || f.count("path:sys/class/input/event1/dev") != 0 {
				t.Fatal("typed nil plus errno became ordinary absence")
			}
		})
	}
}

func TestInputReadinessHandleAndErrorAlwaysTransfersOwnership(t *testing.T) {
	for _, phase := range []string{"event-path", "event-reader", "sys-root", "root"} {
		for _, closeFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_close_%v", phase, closeFails), func(t *testing.T) {
				f := newReadinessFixture(t, 2)
				fault := &os.PathError{Op: "open", Err: syscall.EACCES}
				closeError := func() error {
					if closeFails {
						return syscall.EIO
					}
					return nil
				}
				f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
					if phase == "event-path" && name == "dev/input/event0" {
						file.closeFunc = closeError
						f.stop = true
						return file, fault
					}
					return file, nil
				}
				f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
					if phase == "event-reader" && name == "dev/input/event0" {
						file.closeFunc = closeError
						f.stop = true
						return file, fault
					}
					return file, nil
				}
				f.openSys = func(root *readinessRoot) (inputReadinessRoot, error) {
					if phase == "sys-root" {
						root.token.closeFunc = closeError
						f.stop = true
						return root, fault
					}
					return root, nil
				}
				ops := f.operations()
				if phase == "root" {
					ops.openRoot = func(string) (inputReadinessRoot, error) {
						f.acquired = append(f.acquired, f.root.token)
						f.root.token.closeFunc = closeError
						f.stop = true
						return f.root, fault
					}
				}
				result := runInputWithOperations(t.Context(), Options{Root: "/fixture", Categories: []string{"keyboard"}}, ops)
				f.closed()
				if result.Status != StatusError || f.count("readable:dev/input/event1") != 0 {
					t.Fatal("handle plus error became absence/pass")
				}
				if closeFails && result.Detail != errInputClose.Error() {
					t.Fatal("close failure was hidden by open error")
				}
			})
		}
	}
	for _, phase := range []string{"root", "sys-root", "event-path", "event-reader"} {
		t.Run("typed_nil_"+phase, func(t *testing.T) {
			f := newReadinessFixture(t, 1)
			ops := f.operations()
			switch phase {
			case "root":
				ops.openRoot = func(string) (inputReadinessRoot, error) { return (*readinessRoot)(nil), nil }
			case "sys-root":
				f.openSys = func(*readinessRoot) (inputReadinessRoot, error) { return (*readinessRoot)(nil), nil }
			case "event-path":
				f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
					if name == "dev/input/event0" {
						return (*readinessFile)(nil), nil
					}
					return file, nil
				}
			case "event-reader":
				f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
					if name == "dev/input/event0" {
						return (*readinessFile)(nil), nil
					}
					return file, nil
				}
			}
			result := runInputWithOperations(t.Context(), Options{Root: "/fixture", Categories: []string{"keyboard"}}, ops)
			if phase != "root" {
				f.closed()
			}
			if result.Status != StatusError {
				t.Fatal("typed nil was accepted")
			}
		})
	}
}

func TestInputReadinessMandatorySysfsMetadataCannotBeGuessed(t *testing.T) {
	for _, mode := range []string{"absent", "empty", "malformed", "overflow", "final-overflow", "read-error", "reader-close", "path-close", "permission"} {
		t.Run(mode, func(t *testing.T) {
			f := newReadinessFixture(t, 2)
			attribute := "sys/class/input/event0/dev"
			switch mode {
			case "absent":
				delete(f.infos, attribute)
			case "empty":
				f.data[attribute] = ""
			case "malformed":
				f.data[attribute] = "13:064"
			case "overflow":
				f.data[attribute] = strings.Repeat(" ", 129) + "13:64"
			}
			f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
				if name == attribute && mode == "path-close" {
					file.closeFunc = func() error { f.stop = true; return syscall.EIO }
				}
				return file, nil
			}
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if name == attribute {
					switch mode {
					case "read-error":
						file.readFunc = func([]byte) (int, error) { f.stop = true; return 0, syscall.EIO }
					case "permission":
						return nil, &os.PathError{Op: "open", Err: syscall.EACCES}
					case "reader-close":
						file.closeFunc = func() error { f.stop = true; return syscall.EIO }
					}
				}
				if name == "dev/input/event0" && mode == "final-overflow" {
					f.data[attribute] = strings.Repeat(" ", 129) + "13:64"
				}
				return file, nil
			}
			result := f.run(t.Context(), 0)
			f.closed()
			if result.Status != StatusError || result.Devices[0].Readable || f.count("path:dev/input/event1") != 0 {
				t.Fatal("unproved mandatory identity became readable")
			}
			if mode != "final-overflow" && f.count("readable:dev/input/") != 0 {
				t.Fatal("unproved identity reached event readable open")
			}
		})
	}
}

func TestInputReadinessLateCancellationClosesWithoutFurtherOpens(t *testing.T) {
	for _, phase := range []string{"registry-reader", "registry-path", "empty-registry", "sys-identity", "last-identity", "last-event-reader", "last-event-path", "unavailable-last", "sys-root-close", "root-close", "sys-root-close-error", "root-close-error"} {
		t.Run(phase, func(t *testing.T) {
			f := newReadinessFixture(t, 1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			triggered := false
			if phase == "empty-registry" {
				f.data["proc/bus/input/devices"] = ""
			}
			stop := func() error { triggered = true; cancel(); f.stop = true; return nil }
			inject := func(name string, file *readinessFile) {
				if name == "proc/bus/input/devices" && ((phase == "registry-reader" || phase == "empty-registry") && file.role == "reader" || phase == "registry-path" && file.role == "path") {
					file.closeFunc = stop
				}
				if name == "sys/class/input/event0/dev" && file.role == "reader" && (phase == "sys-identity" || phase == "last-identity" && f.count("readable:dev/input/event0") == 1) {
					file.closeFunc = stop
				}
				if name == "dev/input/event0" && (phase == "last-event-reader" && file.role == "reader" || phase == "last-event-path" && file.role == "path") {
					file.closeFunc = stop
				}
			}
			f.openPath = func(name string, file *readinessFile) (inputPathFile, error) { inject(name, file); return file, nil }
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				inject(name, file)
				return file, nil
			}
			if phase == "unavailable-last" {
				f.lstat = func(name string) (os.FileInfo, error) {
					if name == "dev/input/event0" {
						stop()
						return nil, &os.PathError{Op: "statat", Err: syscall.ENOENT}
					}
					return f.infos[name], nil
				}
			}
			if strings.HasPrefix(phase, "sys-root-close") {
				f.openSys = func(root *readinessRoot) (inputReadinessRoot, error) {
					root.token.closeFunc = func() error {
						stop()
						if strings.HasSuffix(phase, "-error") {
							return syscall.EIO
						}
						return nil
					}
					return root, nil
				}
			}
			if strings.HasPrefix(phase, "root-close") {
				f.root.token.closeFunc = func() error {
					stop()
					if strings.HasSuffix(phase, "-error") {
						return syscall.EIO
					}
					return nil
				}
			}
			result := f.run(ctx, 0)
			f.closed()
			want := StatusAborted
			if strings.HasSuffix(phase, "-error") {
				want = StatusError
			}
			if !triggered || result.Status != want {
				t.Fatal("late cancellation or close failure became stale pass/inconclusive")
			}
			if want == StatusError && result.Detail != errInputClose.Error() {
				t.Fatal("root close failure lost to cancellation")
			}
			if phase == "last-identity" && f.count("readable:dev/input/event0") != 1 {
				t.Fatal("mandatory tail witness fired before event ownership")
			}
		})
	}
}

func TestInputReadinessActivityUsesBoundHandlesAndOnlyAggregates(t *testing.T) {
	for _, fragmented := range []bool{false, true} {
		t.Run(fmt.Sprint(fragmented), func(t *testing.T) {
			f := newReadinessFixture(t, 1)
			data := append(inputFrame(1, 30, 1), inputFrame(1, 30, 0)...)
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if name == "dev/input/event0" {
					left := append([]byte(nil), data...)
					file.readFunc = func(p []byte) (int, error) {
						if len(left) == 0 {
							return 0, syscall.EAGAIN
						}
						take := len(left)
						if fragmented && file.reads == 1 {
							take = 1
						}
						n := copy(p, left[:take])
						left = left[n:]
						return n, nil
					}
				}
				return file, nil
			}
			result := f.run(t.Context(), time.Second)
			f.closed()
			if result.Status != StatusPass || len(result.Devices) != 1 || !result.Devices[0].Readable || result.Devices[0].Events != 2 || result.Devices[0].KeyPresses != 1 || result.Devices[0].KeyReleases != 1 || f.count("readable:dev/input/event0") != 1 {
				t.Fatalf("bound activity did not complete: %#v", result)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"keycode", "\"code\"", "timestamp", "event_payload", "/proc/", "/fixture"} {
				if bytes.Contains(encoded, []byte(forbidden)) {
					t.Fatal("aggregate evidence exposed acquisition payload or path")
				}
			}
		})
	}
}

func TestInputReadinessActivityReadFailuresCannotPass(t *testing.T) {
	for _, kind := range []string{"negative-error", "oversize", "zero-progress", "eof", "partial-error", "data-eagain", "joined-eagain", "incomplete-frame", "dropped", "read-error-cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newReadinessFixture(t, 1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if name == "dev/input/event0" {
					file.readFunc = func(p []byte) (int, error) {
						data := append(inputFrame(1, 30, 1), inputFrame(1, 30, 0)...)
						switch kind {
						case "negative-error":
							return -1, syscall.EAGAIN
						case "oversize":
							return len(p) + 1, nil
						case "zero-progress":
							return 0, nil
						case "eof":
							return 0, io.EOF
						case "partial-error":
							return copy(p, data[:1]), syscall.EIO
						case "data-eagain":
							return copy(p, data), syscall.EAGAIN
						case "joined-eagain":
							return 0, errors.Join(syscall.EAGAIN, syscall.EIO)
						case "incomplete-frame":
							return copy(p, append(data, 1)), nil
						case "dropped":
							return copy(p, inputFrame(0, 3, 0)), nil
						default:
							cancel()
							f.stop = true
							return 0, syscall.EIO
						}
					}
				}
				return file, nil
			}
			result := f.run(ctx, time.Second)
			f.closed()
			if kind == "dropped" {
				if result.Status != StatusInconclusive || !result.Devices[0].EvidenceLoss {
					t.Fatal("SYN_DROPPED was lost")
				}
			} else if result.Status != StatusError || result.Devices[0].Readable {
				t.Fatal("invalid event read earned accepted credit")
			}
		})
	}
}

func TestInputReadinessActivityIdleAndFinalCancellationAreBounded(t *testing.T) {
	for _, kind := range []string{"idle-eagain", "idle-eintr", "last-read", "after-last-frame", "last-close", "close-error-cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newReadinessFixture(t, 1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			triggered := false
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if name == "dev/input/event0" {
					file.readFunc = func(p []byte) (int, error) {
						if kind == "idle-eagain" {
							return 0, syscall.EAGAIN
						}
						if kind == "idle-eintr" {
							return 0, syscall.EINTR
						}
						n := copy(p, append(inputFrame(1, 30, 1), inputFrame(1, 30, 0)...))
						if kind == "last-read" {
							triggered = true
							cancel()
							f.stop = true
						}
						return n, nil
					}
					if kind == "last-close" || kind == "close-error-cancel" {
						file.closeFunc = func() error {
							triggered = true
							cancel()
							f.stop = true
							if kind == "close-error-cancel" {
								return syscall.EIO
							}
							return nil
						}
					}
				}
				return file, nil
			}
			if kind == "after-last-frame" {
				calls := 0
				f.onNow = func() {
					calls++
					if calls == 3 {
						triggered = true
						cancel()
						f.stop = true
					}
				}
			}
			result := f.run(ctx, 40*time.Millisecond)
			f.closed()
			if strings.HasPrefix(kind, "idle-") {
				if result.Status != StatusInconclusive || result.DurationMilliseconds != 40 || !result.Devices[0].Readable {
					t.Fatal("bounded idle activity changed disposition")
				}
			} else if kind == "close-error-cancel" {
				if !triggered || result.Status != StatusError || result.Detail != errInputClose.Error() {
					t.Fatal("activity close failure lost to cancellation")
				}
			} else if !triggered || result.Status != StatusAborted || result.Devices[0].Readable {
				t.Fatal("late cancellation retained activity pass/credit")
			}
			if kind == "after-last-frame" && result.Devices[0].Events != 2 {
				t.Fatal("cancellation witness fired before final complete aggregate")
			}
		})
	}
}

func TestInputReadinessActivityFinalFaultClosesEveryHeldHandle(t *testing.T) {
	f := newReadinessFixture(t, 3)
	f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
		if strings.HasPrefix(name, "dev/input/") {
			file.readFunc = func(p []byte) (int, error) {
				return copy(p, append(inputFrame(1, 30, 1), inputFrame(1, 30, 0)...)), nil
			}
			if name == "dev/input/event1" {
				file.closeFunc = func() error { f.stop = true; return syscall.EIO }
			}
		}
		return file, nil
	}
	result := f.run(t.Context(), time.Second)
	f.closed()
	if result.Status != StatusError || result.Detail != errInputClose.Error() || len(result.Devices) != 3 || !result.Devices[0].Readable || result.Devices[1].Readable || result.Devices[2].Readable || !reflect.DeepEqual(result.ObservedCategories, []string{"keyboard"}) {
		t.Fatal("activity finalization lost accepted prefix or accepted unvalidated suffix")
	}
	for index, device := range result.Devices {
		if device.Events != 2 || device.KeyPresses != 1 || device.KeyReleases != 1 || device.Interface != fmt.Sprintf("event%d", index) {
			t.Fatal("diagnostic aggregate prefix changed")
		}
	}
	if f.count("readable:dev/input/") != 3 || f.count("path:sys/class/input/event2/dev") != 1 {
		t.Fatal("activity reopened a device or continued final metadata opens after close fault")
	}
}

func readinessWrite(t *testing.T, root, name, value string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInputReadinessSystemMetadataAllowsOnlyContainedSysfsLinks(t *testing.T) {
	for _, kind := range []string{"contained-class-link", "escaping-class-link", "leaf-link", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			target := readinessWrite(t, root, "sys/devices/owned/event0/dev", "13:64\n")
			if err := os.MkdirAll(filepath.Join(root, "sys/class/input"), 0o700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, "sys/class/input/event0")
			if kind == "escaping-class-link" {
				target = readinessWrite(t, root, "outside/dev", "13:64\n")
				if err := os.Symlink("../../../outside", link); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink("../../devices/owned/event0", link); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "leaf-link" {
				if err := os.Rename(target, target+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("dev.old", target); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "oversize" {
				if err := os.WriteFile(target, []byte(strings.Repeat(" ", 129)+"13:64"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			operations := systemInputReadinessOperations()
			owned, err := operations.openRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			sys, err := owned.OpenRoot("sys")
			if err != nil {
				owned.Close()
				t.Fatal(err)
			}
			number, readErr := inputRegistryNumber(t.Context(), sys, operations, "event0")
			if err := sys.Close(); err != nil {
				t.Fatal(err)
			}
			if err := owned.Close(); err != nil {
				t.Fatal(err)
			}
			if kind == "contained-class-link" {
				if readErr != nil || number != (inputDeviceNumber{13, 64}) {
					t.Fatal("ordinary contained sysfs link rejected")
				}
			} else if readErr == nil {
				t.Fatal("escaping/symlink/oversized identity accepted")
			}
		})
	}
}

func TestInputReadinessRealProducerNeverOpensFixtureAsDevice(t *testing.T) {
	for _, kind := range []string{"empty", "absent-node", "regular-node", "symlink-node", "wrong-subsystem", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			registry := "N: Name=\"Fixture Keyboard\"\nH: Handlers=kbd event0\n"
			if kind == "empty" {
				registry = ""
			}
			readinessWrite(t, root, "proc/bus/input/devices", registry)
			value := "13:64\n"
			if kind == "wrong-subsystem" {
				value = "1:3\n"
			}
			readinessWrite(t, root, "sys/class/input/event0/dev", value)
			if kind == "regular-node" || kind == "symlink-node" {
				target := readinessWrite(t, root, "dev/input/event0", "owned fixture is not an evdev device")
				if kind == "symlink-node" {
					if err := os.Rename(target, target+".old"); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("event0.old", target); err != nil {
						t.Fatal(err)
					}
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			result := Run(ctx, Options{Root: root, Categories: []string{"keyboard"}})
			want := StatusError
			if kind == "empty" || kind == "absent-node" {
				want = StatusInconclusive
			}
			if kind == "cancelled" {
				want = StatusAborted
			}
			if result.Status != want {
				t.Fatalf("real owned producer disposition: %#v", result)
			}
			for _, device := range result.Devices {
				if device.Readable || device.Events != 0 {
					t.Fatal("owned non-device fixture gained input credit")
				}
			}
		})
	}
}

func TestInputReadinessSystemPinnedReaderNeverFallsBackToReplacedPath(t *testing.T) {
	root := t.TempDir()
	path := readinessWrite(t, root, "metadata", "original")
	ops := systemInputReadinessOperations()
	owned, err := ops.openRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owned.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	realOpen := ops.openReadable
	var pinned inputPathFile
	var readable inputReadableFile
	ops.openReadable = func(handle inputPathFile) (inputReadableFile, error) {
		pinned = handle
		if err := os.Rename(path, path+".old"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
		file, err := realOpen(handle)
		readable = file
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := readInputBytes(t.Context(), file, 128, 32)
		if readErr != nil || string(data) != "original" {
			t.Fatal("pinned descriptor reopened replacement path")
		}
		return file, nil
	}
	observation, err := openInputObservation(t.Context(), owned, ops, "metadata", nil)
	if observation != nil || err != errInputIdentity || pinned == nil || readable == nil {
		t.Fatal("changed path was accepted")
	}
	if _, err := pinned.Stat(); err == nil {
		t.Fatal("rejected metadata descriptor remained open")
	}
	if _, err := readable.Stat(); err == nil {
		t.Fatal("rejected readable descriptor remained open")
	}
}
