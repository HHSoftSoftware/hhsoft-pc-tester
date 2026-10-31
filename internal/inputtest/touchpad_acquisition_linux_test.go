//go:build linux

package inputtest

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func touchpadOwnedFixture(t *testing.T) (*readinessFixture, pointerOperations) {
	f := newReadinessFixture(t, 1)
	f.infos["sys"] = &readinessInfo{mode: os.ModeDir}
	f.data["proc/bus/input/devices"] = "N: Name=\"Touchpad\"\nH: Handlers=event0\nB: ABS=3\n\n"
	ops := pointerOperations{input: f.operations(), query: func(reader inputReadableFile, request uintptr, data []byte) (int, error) {
		file, ok := reader.(*readinessFile)
		if !ok || file.name != "dev/input/event0" || file.role != "reader" || file.closes != 0 {
			t.Fatal("query bypassed the owned event reader")
		}
		return touchpadLinuxReply(t, request, data)
	}}
	open := ops.input.openReadable
	ops.input.openReadable = func(path inputPathFile) (inputReadableFile, error) {
		file := path.(*readinessFile)
		if strings.HasPrefix(file.name, "dev/") && (file.stats < 2 || f.count("lstat:"+file.name) < 2) {
			t.Fatal("event readable open preceded node proof")
		}
		return open(path)
	}
	return f, ops
}

func closeTouchpadFixtureOwner(t *testing.T, opened touchpadOpened) error {
	t.Helper()
	if nilInputValue(opened.owner) {
		return nil
	}
	return opened.owner.Close()
}

func TestTouchpadOwnedProofBeforeReadableOpen(t *testing.T) {
	for _, mode := range []string{"wrong_number", "regular", "symlink", "typed_nil", "path_stat", "path_changed", "name_changed", "sys_not_directory"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := touchpadOwnedFixture(t)
			node := "dev/input/event0"
			switch mode {
			case "wrong_number":
				f.infos[node] = readinessMetadata(100, true, 65)
			case "regular":
				f.infos[node] = readinessMetadata(100, false, 0)
			case "symlink":
				f.infos[node] = &readinessInfo{mode: os.ModeSymlink}
			case "typed_nil":
				f.infos[node] = (*readinessInfo)(nil)
			case "sys_not_directory":
				f.infos["sys"] = readinessMetadata(2, false, 0)
			case "path_stat", "path_changed":
				f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
					if name == node {
						if mode == "path_stat" {
							file.statFunc = func() (os.FileInfo, error) { return nil, syscall.EIO }
						} else {
							file.info = readinessMetadata(999, true, 64)
						}
					}
					return file, nil
				}
			case "name_changed":
				f.lstat = func(name string) (os.FileInfo, error) {
					if name == node && f.count("lstat:"+node) > 1 {
						return readinessMetadata(999, true, 64), nil
					}
					return f.infos[name], nil
				}
			}
			opened, err := openTouchpadWithOperations(t.Context(), "/fixture", "event0", ops)
			if err == nil || opened.read != nil || f.count("readable:"+node) != 0 {
				t.Fatal("unproved event was readably opened or accepted")
			}
			if closeTouchpadFixtureOwner(t, opened) != nil {
				t.Fatal("clean rejection lost closure")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestTouchpadOwnedHandlePlusErrorAndTypedNil(t *testing.T) {
	for _, role := range []string{"root", "sys", "metadata_path", "metadata_reader", "event_path", "event_reader"} {
		for _, fault := range []string{"handle_error", "typed_nil", "nil_success"} {
			t.Run(role+"_"+fault, func(t *testing.T) {
				f, ops := touchpadOwnedFixture(t)
				if role == "root" {
					open := ops.input.openRoot
					ops.input.openRoot = func(path string) (inputReadinessRoot, error) {
						if fault == "typed_nil" {
							return (*readinessRoot)(nil), nil
						}
						if fault == "nil_success" {
							return nil, nil
						}
						r, _ := open(path)
						return r, syscall.EIO
					}
				}
				if role == "sys" {
					f.openSys = func(r *readinessRoot) (inputReadinessRoot, error) {
						if fault == "typed_nil" {
							return (*readinessRoot)(nil), nil
						}
						if fault == "nil_success" {
							return nil, nil
						}
						return r, syscall.EIO
					}
				}
				matches := func(name string) bool {
					return strings.HasPrefix(role, "metadata") && strings.HasPrefix(name, "sys/") || strings.HasPrefix(role, "event") && strings.HasPrefix(name, "dev/")
				}
				f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
					if matches(name) && strings.HasSuffix(role, "path") {
						if fault == "typed_nil" {
							return (*readinessFile)(nil), nil
						}
						if fault == "nil_success" {
							return nil, nil
						}
						return file, syscall.EIO
					}
					return file, nil
				}
				f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
					if matches(name) && strings.HasSuffix(role, "reader") {
						if fault == "typed_nil" {
							return (*readinessFile)(nil), nil
						}
						if fault == "nil_success" {
							return nil, nil
						}
						return file, syscall.EIO
					}
					return file, nil
				}
				opened, err := openTouchpadWithOperations(t.Context(), "/fixture", "event0", ops)
				if err != errInputAcquire || opened.read != nil {
					t.Fatal("bad acquisition accepted")
				}
				if closeTouchpadFixtureOwner(t, opened) != nil {
					t.Fatal("clean owner/error closure failed")
				}
				if len(f.acquired) > 0 {
					assertPointerOwnerOrder(t, f)
				} else if role != "root" || fault == "handle_error" {
					t.Fatal("failure seam was not exercised")
				}
			})
		}
	}
}

type touchpadSelfRoot struct{ *readinessRoot }

func (root *touchpadSelfRoot) OpenRoot(string) (inputReadinessRoot, error) { return root, nil }

func TestTouchpadOwnedConcreteAliasesCloseOnce(t *testing.T) {
	for _, mode := range []string{"sys_is_root", "reader_is_path"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := touchpadOwnedFixture(t)
			if mode == "sys_is_root" {
				open := ops.input.openRoot
				ops.input.openRoot = func(path string) (inputReadinessRoot, error) {
					r, err := open(path)
					return &touchpadSelfRoot{r.(*readinessRoot)}, err
				}
			} else {
				open := ops.input.openReadable
				ops.input.openReadable = func(path inputPathFile) (inputReadableFile, error) {
					if path.(*readinessFile).name == "dev/input/event0" {
						return path.(*readinessFile), nil
					}
					return open(path)
				}
			}
			opened, err := openTouchpadWithOperations(t.Context(), "/fixture", "event0", ops)
			if err == nil || opened.read != nil {
				t.Fatal("same concrete owner accepted twice")
			}
			if closeTouchpadFixtureOwner(t, opened) != nil {
				t.Fatal("aliased owner cleanup failed")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestTouchpadOwnedCancellationStopsAcquisition(t *testing.T) {
	for _, phase := range []string{"root", "sys", "metadata_path", "metadata_reader", "event_path", "event_reader", "query"} {
		t.Run(phase, func(t *testing.T) {
			f, ops := touchpadOwnedFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			triggered := 0
			stop := func() { triggered++; cancel(); f.stop = true }
			if phase == "root" {
				open := ops.input.openRoot
				ops.input.openRoot = func(p string) (inputReadinessRoot, error) { r, err := open(p); stop(); return r, err }
			}
			if phase == "sys" {
				f.openSys = func(r *readinessRoot) (inputReadinessRoot, error) { stop(); return r, nil }
			}
			f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
				if phase == "metadata_path" && strings.HasPrefix(name, "sys/") || phase == "event_path" && strings.HasPrefix(name, "dev/") {
					stop()
				}
				return file, nil
			}
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if phase == "metadata_reader" && strings.HasPrefix(name, "sys/") || phase == "event_reader" && strings.HasPrefix(name, "dev/") {
					stop()
				}
				return file, nil
			}
			query := ops.query
			ops.query = func(file inputReadableFile, r uintptr, p []byte) (int, error) {
				n, err := query(file, r, p)
				if phase == "query" {
					stop()
				}
				return n, err
			}
			opened, err := openTouchpadWithOperations(ctx, "/fixture", "event0", ops)
			if triggered != 1 || err != context.Canceled || opened.read != nil {
				t.Fatal("cancellation lost or later acquisition continued")
			}
			if closeTouchpadFixtureOwner(t, opened) != nil {
				t.Fatal("clean cancellation closure failed")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestTouchpadOwnedSnapshotsDetectReplacementAndIgnoreOnlyDynamicValue(t *testing.T) {
	for _, mode := range []string{"reader", "path", "name", "number", "identity", "capability", "axis", "dynamic", "final_after_cancel"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := touchpadOwnedFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			armed := false
			query := ops.query
			ops.query = func(file inputReadableFile, r uintptr, p []byte) (int, error) {
				n, err := query(file, r, p)
				if armed {
					if mode == "identity" && r == 0x80084502 {
						p[2]++
					}
					if mode == "capability" && r == 0x80604521 {
						p[34] |= 2
					}
					if mode == "axis" && r == 0x80184540 {
						binary.NativeEndian.PutUint32(p[8:], 151)
					}
					if mode == "dynamic" && r == 0x80184540 {
						binary.NativeEndian.PutUint32(p, 0x80000000)
					}
				}
				return n, err
			}
			opened, err := openTouchpadWithOperations(ctx, "/fixture", "event0", ops)
			if err != nil {
				t.Fatal("initial owner unavailable")
			}
			armed = true
			for _, file := range f.acquired {
				if file.name == "dev/input/event0" && file.role == mode {
					file.info = readinessMetadata(999, true, 64)
				}
			}
			if mode == "name" {
				f.infos["dev/input/event0"] = readinessMetadata(999, true, 64)
			}
			if mode == "number" {
				f.data["sys/class/input/event0/dev"] = "13:65\n"
			}
			var current touchpadMetadata
			if mode == "final_after_cancel" {
				cancel()
				if _, err := opened.snapshot(); err != context.Canceled {
					t.Fatal("active snapshot ignored cancellation")
				}
				current, err = opened.finalSnapshot()
			} else {
				current, err = opened.snapshot()
			}
			if mode == "dynamic" || mode == "final_after_cancel" {
				if err != nil || !sameTouchpadMetadata(current, opened.metadata) {
					t.Fatal("bounded final proof or dynamic exclusion failed")
				}
			} else if err == nil && sameTouchpadMetadata(current, opened.metadata) {
				t.Fatal("replacement/static drift escaped snapshot contract")
			}
			if closeTouchpadFixtureOwner(t, opened) != nil {
				t.Fatal("snapshot owner closure failed")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestTouchpadOwnedReadMappingAndClosedCallbacks(t *testing.T) {
	for _, mode := range []string{"idle", "retry", "eof", "joined_idle", "partial_error", "negative", "zero_nil", "positive_idle"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := touchpadOwnedFixture(t)
			wantCount, wantErr := 0, error(syscall.EAGAIN)
			switch mode {
			case "retry":
				wantErr = syscall.EINTR
			case "eof":
				wantErr = io.EOF
			case "joined_idle":
				wantErr = errors.Join(syscall.EAGAIN, syscall.EIO)
			case "partial_error":
				wantCount, wantErr = 1, syscall.EIO
			case "negative":
				wantCount, wantErr = -1, nil
			case "zero_nil":
				wantErr = nil
			case "positive_idle":
				wantCount = 1
			}
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if name == "dev/input/event0" {
					file.readFunc = func([]byte) (int, error) { return wantCount, wantErr }
				}
				return file, nil
			}
			opened, err := openTouchpadWithOperations(t.Context(), "/fixture", "event0", ops)
			if err != nil {
				t.Fatal("owner unavailable")
			}
			n, readErr := opened.read(make([]byte, 64))
			expected := wantErr
			if mode == "idle" {
				expected = errTouchpadIdle
			}
			if mode == "retry" {
				expected = errTouchpadRetry
			}
			if n != wantCount || readErr != expected {
				t.Fatal("read mapping erased a hard or partial error")
			}
			if opened.owner.Close() != nil || opened.owner.Close() != nil {
				t.Fatal("single owned close failed")
			}
			before := len(f.log)
			if _, err := opened.snapshot(); err == nil {
				t.Fatal("snapshot after close")
			}
			if _, err := opened.finalSnapshot(); err == nil {
				t.Fatal("final snapshot after close")
			}
			if _, err := opened.neutral(); err == nil {
				t.Fatal("query after close")
			}
			if _, err := opened.read(make([]byte, 8)); err == nil {
				t.Fatal("read after close")
			}
			if len(f.log) != before {
				t.Fatal("closed callback touched provider")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

// Every discovery has its own fresh root. The event source passes through the
// real acquisition and ioctl decoder, then the unchanged production core.
func touchpadOwnedBackend(t *testing.T, ops pointerOperations) touchpadBackend {
	return touchpadBackend{discover: func(ctx context.Context, root string) ([]string, error) {
		f, discovery := touchpadOwnedFixture(t)
		names, err := discoverTouchpadWithOperations(ctx, root, discovery)
		assertPointerOwnerOrder(t, f)
		return names, err
	}, open: func(ctx context.Context, root, name string) (touchpadOpened, error) {
		return openTouchpadWithOperations(ctx, root, name, ops)
	}}
}

func touchpadOwnedScript(t *testing.T, f *readinessFixture, ops *pointerOperations) (*int, *int) {
	data := touchpadFixtureBytes(int(unsafe.Sizeof(inputEvent{})), touchpadTestCompleteEvents())
	queries, idles := 0, 0
	query := ops.query
	ops.query = func(file inputReadableFile, request uintptr, buffer []byte) (int, error) {
		if request == 0x80604518 {
			queries++
			if queries == 1 && idles != 1 || queries == 2 && (len(data) != 0 || idles != 3) || queries > 2 {
				t.Fatal("key-state query outside baseline/terminal phases")
			}
		}
		return query(file, request, buffer)
	}
	f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
		if name == "dev/input/event0" {
			file.readFunc = func(p []byte) (int, error) {
				if queries == 0 || len(data) == 0 {
					idles++
					return 0, syscall.EAGAIN
				}
				n := min(len(p), len(data))
				copy(p, data[:n])
				data = data[n:]
				return n, nil
			}
		}
		return file, nil
	}
	return &queries, &idles
}

func TestTouchpadOwnedCompleteAdapterCoreRoute(t *testing.T) {
	f, ops := touchpadOwnedFixture(t)
	queries, _ := touchpadOwnedScript(t, f, &ops)
	r := runTouchpadWith(t.Context(), TouchpadOptions{Root: "/fixture", Duration: time.Second}, touchpadOwnedBackend(t, ops))
	if r.Status != StatusPass || !r.CleanupConfirmed || len(r.Devices) != 1 || !r.Devices[0].FinalNeutral || r.Devices[0].QualifyingContacts != 1 || *queries != 2 {
		t.Fatal("connected owned adapter route did not reach a closed proof")
	}
	assertPointerOwnerOrder(t, f)
}

func TestTouchpadOwnedUnsupportedAndUnavailableRemainRows(t *testing.T) {
	for _, mode := range []string{"mt", "unknown_property", "absent"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := touchpadOwnedFixture(t)
			query := ops.query
			neutral := 0
			ops.query = func(file inputReadableFile, r uintptr, p []byte) (int, error) {
				n, err := query(file, r, p)
				if r == 0x80604518 {
					neutral++
				}
				if mode == "mt" && r == 0x80084523 {
					p[5] |= 128
				}
				if mode == "unknown_property" && r == 0x80084509 {
					p[3] |= 128
				}
				return n, err
			}
			if mode == "absent" {
				delete(f.infos, "dev/input/event0")
			}
			r := runTouchpadWith(t.Context(), TouchpadOptions{Root: "/fixture", Duration: time.Second}, touchpadOwnedBackend(t, ops))
			if r.Status != StatusInconclusive || !r.CleanupConfirmed || len(r.Devices) != 1 || r.Devices[0].Supported || r.Devices[0].FinalNeutral || neutral != 0 {
				t.Fatal("unsupported/unreadable candidate disappeared or gained credit")
			}
			if r.Devices[0].Readable != (mode != "absent") {
				t.Fatal("readability fabricated")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestTouchpadOwnedTerminalFaultRevokesPass(t *testing.T) {
	for _, mode := range []string{"queued", "retry", "nonneutral", "query_error", "identity_after_query", "close_error"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := touchpadOwnedFixture(t)
			queries, idles := touchpadOwnedScript(t, f, &ops)
			query := ops.query
			ops.query = func(file inputReadableFile, r uintptr, p []byte) (int, error) {
				n, err := query(file, r, p)
				if r == 0x80604518 && *queries == 2 {
					if mode == "nonneutral" {
						p[34] = 1
					}
					if mode == "query_error" {
						err = syscall.EIO
					}
					if mode == "identity_after_query" {
						f.infos["dev/input/event0"] = readinessMetadata(999, true, 64)
					}
				}
				return n, err
			}
			open := f.openReader
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				r, err := open(name, file)
				if name == "dev/input/event0" {
					read := file.readFunc
					file.readFunc = func(p []byte) (int, error) {
						if *idles == 2 {
							if mode == "queued" {
								p[0] = 1
								return 1, nil
							}
							if mode == "retry" {
								return 0, syscall.EINTR
							}
						}
						return read(p)
					}
					if mode == "close_error" {
						file.closeFunc = func() error { return syscall.EIO }
					}
				}
				return r, err
			}
			r := runTouchpadWith(t.Context(), TouchpadOptions{Root: "/fixture", Duration: time.Second}, touchpadOwnedBackend(t, ops))
			if r.Status == StatusPass || len(r.Devices) != 1 || r.Devices[0].Status == StatusPass {
				t.Fatal("terminal fault retained a returned pass")
			}
			if mode == "close_error" {
				if r.CleanupConfirmed || r.Detail != "cleanup_unconfirmed" {
					t.Fatal("close failure did not dominate")
				}
			} else if !r.CleanupConfirmed {
				t.Fatal("clean rejection lost cleanup")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestTouchpadOwnedCloseFailureDominatesEveryAcquiredRole(t *testing.T) {
	for _, role := range []string{"metadata_path", "metadata_reader", "event_path", "event_reader", "sys", "root"} {
		t.Run(role, func(t *testing.T) {
			f, ops := touchpadOwnedFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			touchpadOwnedScript(t, f, &ops)
			triggered := 0
			fail := func() error { triggered++; cancel(); return syscall.EIO }
			f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
				if role == "metadata_path" && strings.HasPrefix(name, "sys/") || role == "event_path" && strings.HasPrefix(name, "dev/") {
					file.closeFunc = fail
				}
				return file, nil
			}
			open := f.openReader
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				r, err := open(name, file)
				if role == "metadata_reader" && strings.HasPrefix(name, "sys/") || role == "event_reader" && strings.HasPrefix(name, "dev/") {
					file.closeFunc = fail
				}
				return r, err
			}
			if role == "sys" {
				f.openSys = func(r *readinessRoot) (inputReadinessRoot, error) { r.token.closeFunc = fail; return r, nil }
			}
			if role == "root" {
				f.root.token.closeFunc = fail
			}
			r := runTouchpadWith(ctx, TouchpadOptions{Root: "/fixture", Duration: time.Second}, touchpadOwnedBackend(t, ops))
			if triggered != 1 || r.Status != StatusError || r.CleanupConfirmed || r.Detail != "cleanup_unconfirmed" {
				t.Fatal("cleanup uncertainty was downgraded by cancellation")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestTouchpadOwnedMalformedActiveInputCannotPass(t *testing.T) {
	for _, mode := range []string{"partial_error", "eof", "syn_dropped", "unknown_sync", "unknown_axis", "partial_eof", "two_contact_transitions"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := touchpadOwnedFixture(t)
			queries, _ := touchpadOwnedScript(t, f, &ops)
			open := f.openReader
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				r, err := open(name, file)
				if name == "dev/input/event0" {
					read := file.readFunc
					delivered := false
					file.readFunc = func(p []byte) (int, error) {
						if *queries == 0 {
							return read(p)
						}
						if delivered {
							return 0, io.EOF
						}
						delivered = true
						if mode == "partial_error" {
							p[0] = 1
							return 1, syscall.EIO
						}
						if mode == "eof" {
							return 0, io.EOF
						}
						if mode == "partial_eof" {
							p[0] = 1
							return 1, nil
						}
						events := []touchpadEvent{{0, 3, 0}}
						if mode == "unknown_sync" {
							events = []touchpadEvent{{0, 2, 0}}
						}
						if mode == "unknown_axis" {
							events = []touchpadEvent{{3, 47, 0}, {0, 0, 0}}
						}
						if mode == "two_contact_transitions" {
							events = []touchpadEvent{{1, 330, 1}, {1, 330, 0}, {0, 0, 0}}
						}
						return copy(p, touchpadFixtureBytes(int(unsafe.Sizeof(inputEvent{})), events)), nil
					}
				}
				return r, err
			}
			r := runTouchpadWith(t.Context(), TouchpadOptions{Root: "/fixture", Duration: time.Second}, touchpadOwnedBackend(t, ops))
			if r.Status != StatusError || !r.CleanupConfirmed || len(r.Devices) != 1 || r.Devices[0].FinalNeutral || *queries != 1 {
				t.Fatal("active error was erased or terminal query resumed observation")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestTouchpadOwnedDiscoveryBoundsAndFailures(t *testing.T) {
	for _, mode := range []string{"healthy", "overflow", "partial_error", "zero_nil", "joined_eof", "reader_close", "root_close"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := touchpadOwnedFixture(t)
			if mode == "overflow" {
				f.data["proc/bus/input/devices"] = strings.Repeat("\n", maximumInputRegistryBytes+1)
			}
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if name == "proc/bus/input/devices" {
					switch mode {
					case "partial_error":
						file.readFunc = func(p []byte) (int, error) { p[0] = 'x'; return 1, syscall.EIO }
					case "zero_nil":
						file.readFunc = func([]byte) (int, error) { return 0, nil }
					case "joined_eof":
						file.readFunc = func([]byte) (int, error) { return 0, errors.Join(io.EOF, syscall.EIO) }
					case "reader_close":
						file.closeFunc = func() error { return syscall.EIO }
					}
				}
				return file, nil
			}
			if mode == "root_close" {
				f.root.token.closeFunc = func() error { return syscall.EIO }
			}
			names, err := discoverTouchpadWithOperations(t.Context(), "/fixture", ops)
			if mode == "healthy" {
				if err != nil || !reflect.DeepEqual(names, []string{"event0"}) {
					t.Fatal("owned registry discovery failed")
				}
			} else if err == nil || names != nil {
				t.Fatal("bounded registry failure returned partial names")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

// A comparable declared struct type can contain an uncomparable dynamic
// interface value. Put that field first so type-only equality panics even when
// the two underlying root owners are distinct and correctly transferred.
type touchpadDynamicRoot struct {
	dynamic any
	inputReadinessRoot
}

func (root touchpadDynamicRoot) OpenRoot(name string) (inputReadinessRoot, error) {
	child, err := root.inputReadinessRoot.OpenRoot(name)
	if nilInputValue(child) {
		return child, err
	}
	return touchpadDynamicRoot{dynamic: []byte{1}, inputReadinessRoot: child}, err
}

func TestTouchpadOwnedDynamicComparabilityTransfersAndCloses(t *testing.T) {
	f, ops := touchpadOwnedFixture(t)
	open := ops.input.openRoot
	ops.input.openRoot = func(path string) (inputReadinessRoot, error) {
		r, err := open(path)
		wrapped := touchpadDynamicRoot{dynamic: []byte{1}, inputReadinessRoot: r}
		if !reflect.TypeOf(wrapped).Comparable() || reflect.ValueOf(wrapped).Comparable() {
			t.Fatal("dynamic-comparability witness is invalid")
		}
		return wrapped, err
	}
	opened, err := openTouchpadWithOperations(t.Context(), "/fixture", "event0", ops)
	if err != nil || nilInputValue(opened.owner) || reflect.ValueOf(opened.owner).Kind() != reflect.Pointer {
		t.Fatal("valid wrapped roots did not transfer an aggregate owner")
	}
	current, err := opened.snapshot()
	if err != nil || !sameTouchpadMetadata(current, opened.metadata) {
		t.Fatal("valid wrapped acquisition lost the connected snapshot")
	}
	if opened.owner.Close() != nil {
		t.Fatal("wrapped owner cleanup failed")
	}
	assertPointerOwnerOrder(t, f)
}
