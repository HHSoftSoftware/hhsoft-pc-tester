//go:build linux

package inputtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func keyboardOwnedFixture(t *testing.T) (*readinessFixture, pointerOperations) {
	f := newReadinessFixture(t, 1)
	f.infos["sys"] = &readinessInfo{mode: os.ModeDir}
	ops := pointerOperations{input: f.operations(), query: func(reader inputReadableFile, request uintptr, data []byte) (int, error) {
		file, ok := reader.(*readinessFile)
		if !ok || file.name != "dev/input/event0" || file.role != "reader" || file.closes != 0 {
			t.Fatal("query bypassed the acquired event reader")
		}
		return keyboardMetadataFixture(request, data)
	}}
	open := ops.input.openReadable
	ops.input.openReadable = func(path inputPathFile) (inputReadableFile, error) {
		file := path.(*readinessFile)
		if strings.HasPrefix(file.name, "dev/") && (file.stats < 2 || f.count("lstat:"+file.name) < 2) {
			t.Fatal("readable event opened before descriptor and path proof")
		}
		return open(path)
	}
	return f, ops
}

func TestKeyboardOwnedNodeProofPrecedesReadableOpen(t *testing.T) {
	for _, mode := range []string{"number", "regular", "symlink", "typed_nil", "path_stat", "path_changed", "name_changed", "sys_not_directory"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := keyboardOwnedFixture(t)
			node := "dev/input/event0"
			switch mode {
			case "number":
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
			owned, err := openKeyboardWithOperations(t.Context(), "/fixture", "event0", ops)
			if err == nil || owned.close != nil || f.count("readable:"+node) != 0 {
				t.Fatal("unproved node was opened readably or transferred")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestKeyboardOwnedAcquisitionClosesHandlePlusErrorAndRejectsTypedNil(t *testing.T) {
	for _, role := range []string{"root", "sys", "metadata_path", "metadata_reader", "event_path", "event_reader"} {
		for _, fault := range []string{"handle_error", "typed_nil", "nil_success"} {
			t.Run(role+"/"+fault, func(t *testing.T) {
				f, ops := keyboardOwnedFixture(t)
				providerErr := errors.New("unpublished provider identity")
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
						return r, providerErr
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
						return r, providerErr
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
						return file, providerErr
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
						return file, providerErr
					}
					return file, nil
				}
				owned, err := openKeyboardWithOperations(t.Context(), "/fixture", "event0", ops)
				if err != errInputAcquire || owned.close != nil {
					t.Fatalf("bad provider was not rejected: %v", err)
				}
				if len(f.acquired) > 0 {
					assertPointerOwnerOrder(t, f)
				} else if role != "root" || fault == "handle_error" {
					t.Fatal("expected owned handle was not exercised")
				}
			})
		}
	}
}

func TestKeyboardOwnedCloseFailureDominatesCancelAtEveryOwner(t *testing.T) {
	for _, role := range []string{"metadata_path", "metadata_reader", "event_path", "event_reader", "sys", "root"} {
		for _, reject := range []bool{false, true} {
			t.Run(role+map[bool]string{false: "/accepted", true: "/rejected"}[reject], func(t *testing.T) {
				f, ops := keyboardOwnedFixture(t)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				triggered := 0
				fail := func() error { triggered++; cancel(); f.stop = true; return errors.New("unpublished close failure") }
				f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
					if role == "metadata_path" && strings.HasPrefix(name, "sys/") || role == "event_path" && strings.HasPrefix(name, "dev/") {
						file.closeFunc = fail
					}
					return file, nil
				}
				f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
					if role == "metadata_reader" && strings.HasPrefix(name, "sys/") || role == "event_reader" && strings.HasPrefix(name, "dev/") {
						file.closeFunc = fail
					}
					return file, nil
				}
				f.openSys = func(r *readinessRoot) (inputReadinessRoot, error) {
					if role == "sys" {
						r.token.closeFunc = fail
					}
					return r, nil
				}
				if role == "root" {
					f.root.token.closeFunc = fail
				}
				if reject {
					ops.query = func(inputReadableFile, uintptr, []byte) (int, error) { return 0, syscall.EIO }
				}
				owned, err := openKeyboardWithOperations(ctx, "/fixture", "event0", ops)
				if owned.close != nil {
					err = owned.close()
				}
				if triggered != 1 || err != errInputClose {
					t.Fatalf("close uncertainty hidden: %v", err)
				}
				assertPointerOwnerOrder(t, f)
				if strings.HasPrefix(role, "metadata") && f.count("path:dev/input/event0") != 0 {
					t.Fatal("event opened after temporary cleanup fault")
				}
			})
		}
	}
}

func TestKeyboardOwnedCancellationStopsLaterAcquisitionAndClosesEverything(t *testing.T) {
	for _, phase := range []string{"root", "sys", "metadata_path", "metadata_reader", "event_path", "event_reader", "metadata_query"} {
		t.Run(phase, func(t *testing.T) {
			f, ops := keyboardOwnedFixture(t)
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
				if phase == "metadata_query" && r == 0x80604521 {
					stop()
				}
				return n, err
			}
			owned, err := openKeyboardWithOperations(ctx, "/fixture", "event0", ops)
			if triggered != 1 || err != context.Canceled || owned.close != nil {
				t.Fatalf("clean cancellation lost: %v", err)
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestKeyboardUnavailableNodeKeepsCleanOwnershipAndNoReadableClaim(t *testing.T) {
	f, ops := keyboardOwnedFixture(t)
	delete(f.infos, "dev/input/event0")
	owned, err := openKeyboardWithOperations(t.Context(), "/fixture", "event0", ops)
	if err != errInputUnavailable || owned.close != nil || f.count("path:dev/input/event0") != 0 || f.count("readable:dev/input/event0") != 0 {
		t.Fatal("sole node absence lost its bounded distinction")
	}
	assertPointerOwnerOrder(t, f)
}

func TestKeyboardOwnedRegistryProtocolAndExactEOFAreBounded(t *testing.T) {
	for _, mode := range []string{"healthy", "exact_cap", "overflow", "zero", "negative", "oversize", "partial_error", "joined_eof", "root_error", "reader_close_cancel", "root_close_cancel", "clean_close_cancel", "clean_root_close_cancel", "clean_cancel"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := keyboardOwnedFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "exact_cap" || mode == "overflow" {
				f.data["proc/bus/input/devices"] = strings.Repeat("#\n", maximumInputRegistryBytes/2)
				if mode == "overflow" {
					f.data["proc/bus/input/devices"] += "x"
				}
			}
			if mode == "root_error" {
				open := ops.input.openRoot
				ops.input.openRoot = func(p string) (inputReadinessRoot, error) { r, _ := open(p); return r, syscall.EIO }
			}
			if mode == "root_close_cancel" {
				f.root.token.closeFunc = func() error { cancel(); f.stop = true; return syscall.EIO }
			}
			if mode == "clean_root_close_cancel" {
				f.root.token.closeFunc = func() error { cancel(); f.stop = true; return nil }
			}
			f.openReader = func(_ string, file *readinessFile) (inputReadableFile, error) {
				if mode == "reader_close_cancel" {
					file.closeFunc = func() error { cancel(); f.stop = true; return syscall.EIO }
				}
				if mode == "clean_close_cancel" {
					file.closeFunc = func() error { cancel(); f.stop = true; return nil }
				}
				if mode == "zero" || mode == "negative" || mode == "oversize" || mode == "partial_error" || mode == "joined_eof" || mode == "clean_cancel" {
					file.readFunc = func(p []byte) (int, error) {
						cancel()
						switch mode {
						case "negative":
							return -1, io.EOF
						case "oversize":
							return len(p) + 1, nil
						case "partial_error":
							p[0] = 'x'
							return 1, syscall.EIO
						case "joined_eof":
							return 0, errors.Join(io.EOF, syscall.EIO)
						case "clean_cancel":
							return 0, io.EOF
						}
						return 0, nil
					}
				}
				return file, nil
			}
			names, err := discoverKeyboardWithOperations(ctx, "/fixture", ops)
			switch mode {
			case "healthy":
				if err != nil || !reflect.DeepEqual(names, []string{"event0"}) {
					t.Fatal("healthy keyboard registry rejected")
				}
			case "exact_cap":
				if err != nil || len(names) != 0 {
					t.Fatal("exact byte limit with EOF rejected")
				}
			case "root_error":
				if err != errInputAcquire {
					t.Fatal("root provider error hidden")
				}
			case "reader_close_cancel", "root_close_cancel":
				if err != errInputClose {
					t.Fatal("close masked by cancel")
				}
			case "clean_cancel", "clean_close_cancel", "clean_root_close_cancel":
				if err != context.Canceled {
					t.Fatal("clean cancellation hidden")
				}
			default:
				if err != errInputRead {
					t.Fatalf("hard protocol error hidden: %v", err)
				}
			}
			if err != nil && names != nil {
				t.Fatal("failed registry leaked selected names")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestKeyboardCancelledFinalIdentityProofOwnsTemporaryMetadata(t *testing.T) {
	for _, mode := range []string{"healthy", "changed_number", "overflow", "path_error", "reader_error", "typed_nil", "zero", "partial_error", "joined_eof", "path_close", "reader_close", "event_identity", "query_error"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := keyboardOwnedFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			armed := false
			faults := 0
			f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
				if armed && strings.HasPrefix(name, "sys/") {
					switch mode {
					case "path_error":
						faults++
						return file, syscall.EIO
					case "path_close":
						file.closeFunc = func() error { faults++; return syscall.EIO }
					}
				}
				return file, nil
			}
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if armed && strings.HasPrefix(name, "sys/") {
					switch mode {
					case "reader_error":
						faults++
						return file, syscall.EIO
					case "typed_nil":
						faults++
						return (*readinessFile)(nil), nil
					case "reader_close":
						file.closeFunc = func() error { faults++; return syscall.EIO }
					case "zero", "partial_error", "joined_eof":
						file.readFunc = func(p []byte) (int, error) {
							faults++
							switch mode {
							case "partial_error":
								p[0] = '1'
								return 1, syscall.EIO
							case "joined_eof":
								return 0, errors.Join(io.EOF, syscall.EIO)
							}
							return 0, nil
						}
					}
				}
				return file, nil
			}
			query := ops.query
			ops.query = func(file inputReadableFile, r uintptr, p []byte) (int, error) {
				if armed && mode == "query_error" {
					faults++
					return 0, syscall.EIO
				}
				return query(file, r, p)
			}
			owned, err := openKeyboardWithOperations(ctx, "/fixture", "event0", ops)
			if err != nil {
				t.Fatal(err)
			}
			beforeEvents := f.count("path:dev/") + f.count("readable:dev/")
			beforeRoots := f.count("root:")
			armed = true
			cancel()
			if mode == "changed_number" {
				f.data["sys/class/input/event0/dev"] = "13:65"
			}
			if mode == "overflow" {
				f.data["sys/class/input/event0/dev"] = strings.Repeat("1", 129)
			}
			if mode == "event_identity" {
				f.infos["dev/input/event0"] = readinessMetadata(999, true, 64)
			}
			err = owned.finalValidate()
			if (err == nil) != (mode == "healthy") || err == context.Canceled {
				t.Fatalf("closing proof omitted hard fault: %v", err)
			}
			if mode == "path_close" || mode == "reader_close" {
				if err != errInputClose {
					t.Fatal("temporary cleanup not propagated")
				}
			}
			if strings.Contains(mode, "error") || strings.Contains(mode, "close") || mode == "typed_nil" || mode == "zero" || mode == "joined_eof" {
				if faults != 1 {
					t.Fatal("fault boundary not reached exactly once")
				}
			}
			if f.count("path:dev/")+f.count("readable:dev/") != beforeEvents || f.count("root:") != beforeRoots || f.count("read:dev/") != 0 {
				t.Fatal("closing proof opened another event/root or consumed input")
			}
			if err := owned.close(); err != nil {
				t.Fatal(err)
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestKeyboardOwnedProducerPreservesSixPairsAndCleanupPrecedence(t *testing.T) {
	for _, mode := range []string{"healthy", "close_cancel", "hard_read_cancel"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := keyboardOwnedFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			size := int(unsafe.Sizeof(inputEvent{}))
			data := keyboardFixtureBytes(size, keyboardCompleteEvents())
			reads := 0
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if name == "dev/input/event0" {
					file.readFunc = func(p []byte) (int, error) {
						reads++
						if reads == 1 {
							return 0, syscall.EAGAIN
						}
						if len(data) > 0 {
							n := copy(p, data)
							data = data[n:]
							return n, nil
						}
						if mode == "hard_read_cancel" {
							cancel()
							return 0, syscall.EIO
						}
						return 0, syscall.EAGAIN
					}
					if mode == "close_cancel" {
						file.closeFunc = func() error { cancel(); f.stop = true; return syscall.EIO }
					}
				}
				return file, nil
			}
			backend := keyboardBackend{discover: func(ctx context.Context, _ string) ([]string, error) {
				registry, registryOps := keyboardOwnedFixture(t)
				names, err := discoverKeyboardWithOperations(ctx, "/fixture", registryOps)
				assertPointerOwnerOrder(t, registry)
				return names, err
			}, open: func(ctx context.Context, root, name string) (keyboardOpened, error) {
				return openKeyboardWithOperations(ctx, root, name, ops)
			}}
			result := runKeyboardWith(ctx, KeyboardOptions{Root: "/fixture", Duration: time.Second}, backend)
			want := StatusPass
			if mode != "healthy" {
				want = StatusError
			}
			if result.Status != want || len(result.Devices) != 1 || !result.Devices[0].Readable || *result.Devices[0].Identity != (KeyboardIdentity{3, 0x1234, 0x5678, 0x100}) {
				t.Fatalf("actual owned producer changed evidence %#v", result)
			}
			for _, action := range result.Devices[0].Actions {
				if !action.PairCompleted || !action.PressObserved || !action.ReleaseObserved {
					t.Fatal("earlier complete pairs lost")
				}
			}
			if mode == "close_cancel" && (result.CleanupConfirmed || result.StopReason != "cleanup_unconfirmed") {
				t.Fatal("close fault was downgraded")
			}
			if mode == "hard_read_cancel" && (result.StopReason != "error" || result.Detail != "disconnected") {
				t.Fatal("read fault was downgraded")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestKeyboardSelectedNumberProofKeepsExactBoundsAndEvdevRanges(t *testing.T) {
	for _, row := range []struct {
		value string
		minor uint32
		valid bool
	}{
		{"13:64", 64, true}, {"13:95", 95, true}, {"13:256", 256, true}, {"13:1048575", 1048575, true},
		{"12:64", 64, false}, {"14:64", 64, false}, {"13:63", 63, false}, {"13:96", 96, false}, {"13:255", 255, false}, {"13:1048576", 1048576, false},
		{"13:064", 64, false}, {"13:+64", 64, false}, {"13:64:1", 64, false}, {"13:64" + strings.Repeat(" ", 123), 64, true}, {"13:64" + strings.Repeat(" ", 124), 64, false},
	} {
		t.Run(fmt.Sprintf("%d/%d", row.minor, len(row.value)), func(t *testing.T) {
			f, ops := keyboardOwnedFixture(t)
			f.data["sys/class/input/event0/dev"] = row.value
			f.infos["dev/input/event0"] = readinessMetadata(100, true, row.minor)
			owned, err := openKeyboardWithOperations(t.Context(), "/fixture", "event0", ops)
			if (err == nil) != row.valid {
				t.Fatalf("device-number eligibility mismatch: %v", err)
			}
			if owned.close != nil {
				if err := owned.close(); err != nil {
					t.Fatal(err)
				}
			}
			if !row.valid && f.count("readable:dev/input/event0") != 0 {
				t.Fatal("bad registry number reached readable node")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestKeyboardOwnedReadAdapterUsesOnlyExactZeroByteErrnos(t *testing.T) {
	for _, row := range []struct {
		name             string
		count            int
		cause, errorWant error
	}{
		{"idle", 0, syscall.EAGAIN, errKeyboardIdle}, {"retry", 0, syscall.EINTR, errKeyboardRetry},
		{"positive_idle", 1, syscall.EAGAIN, syscall.EAGAIN}, {"positive_retry", 1, syscall.EINTR, syscall.EINTR},
		{"negative", -1, syscall.EINTR, syscall.EINTR}, {"hard", 0, syscall.EIO, syscall.EIO},
		{"joined", 0, errors.Join(syscall.EAGAIN, syscall.EIO), nil}, {"wrapped", 0, fmt.Errorf("wrapped: %w", syscall.EINTR), nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			f, ops := keyboardOwnedFixture(t)
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if name == "dev/input/event0" {
					file.readFunc = func([]byte) (int, error) { return row.count, row.cause }
				}
				return file, nil
			}
			owned, err := openKeyboardWithOperations(t.Context(), "/fixture", "event0", ops)
			if err != nil {
				t.Fatal(err)
			}
			var buffer [32]byte
			n, err := owned.read(buffer[:])
			want := row.errorWant
			if want == nil {
				want = row.cause
			}
			if n != row.count || err != want {
				t.Fatal("adapter erased partial/negative/joined error")
			}
			if err := owned.close(); err != nil {
				t.Fatal(err)
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}
