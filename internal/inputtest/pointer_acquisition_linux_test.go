//go:build linux

package inputtest

import (
	"context"
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

func pointerOwnedFixture(t *testing.T) (*readinessFixture, pointerOperations) {
	f := newReadinessFixture(t, 1)
	f.infos["sys"] = &readinessInfo{mode: os.ModeDir}
	f.data["proc/bus/input/devices"] = "N: Name=\"Fixture Mouse\"\nH: Handlers=mouse0 event0\n"
	ops := pointerOperations{input: f.operations(), query: func(reader inputReadableFile, request uintptr, data []byte) (int, error) {
		file, ok := reader.(*readinessFile)
		if !ok || file.role != "reader" || file.name != "dev/input/event0" || file.closes != 0 {
			t.Fatal("ioctl bypassed verified owned event reader")
		}
		return pointerMetadataFixture(request, data)
	}}
	openReadable := ops.input.openReadable
	ops.input.openReadable = func(path inputPathFile) (inputReadableFile, error) {
		file, ok := path.(*readinessFile)
		if !ok {
			t.Fatal("readable acquisition without fixture owner")
		}
		if strings.HasPrefix(file.name, "dev/") && (file.stats < 2 || f.count("lstat:"+file.name) < 2) {
			t.Fatal("readable event open preceded descriptor and path proof")
		}
		return openReadable(path)
	}
	return f, ops
}

func assertPointerOwnerOrder(t *testing.T, f *readinessFixture) {
	t.Helper()
	f.closed()
	order := map[*readinessFile]int{}
	for index, row := range f.log {
		if !strings.HasPrefix(row, "close:") {
			continue
		}
		found := false
		for _, file := range f.acquired {
			if _, seen := order[file]; !seen && row == "close:"+file.role+":"+file.name {
				order[file] = index
				found = true
				break
			}
		}
		if !found {
			t.Fatal("close without a successfully acquired owner")
		}
	}
	if len(order) != len(f.acquired) {
		t.Fatal("owned close ledger incomplete")
	}
	for index, file := range f.acquired {
		if file.role != "reader" {
			continue
		}
		var path *readinessFile
		for _, previous := range f.acquired[:index] {
			if previous.role == "path" && previous.name == file.name {
				path = previous
			}
		}
		if path == nil || order[file] >= order[path] {
			t.Fatal("readable owner was not closed before its own path owner")
		}
	}
	if f.log[len(f.log)-1] != "close:root:fixture" {
		t.Fatal("outer root was not the final owner closed")
	}
}

func TestPointerAcquisitionRejectsUnprovedNodesBeforeReadableOpen(t *testing.T) {
	for _, mode := range []string{"wrong number", "wrong type", "typed nil metadata", "path stat drift", "path substitution", "path stat error"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := pointerOwnedFixture(t)
			node := "dev/input/event0"
			switch mode {
			case "wrong number":
				f.infos[node] = readinessMetadata(100, true, 65)
			case "wrong type":
				f.infos[node] = readinessMetadata(100, false, 0)
			case "typed nil metadata":
				var info *readinessInfo
				f.infos[node] = info
			case "path stat drift", "path stat error":
				f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
					if name == node {
						if mode == "path stat drift" {
							file.info = readinessMetadata(999, true, 64)
						} else {
							file.statFunc = func() (os.FileInfo, error) { return nil, errors.New("private stat") }
						}
					}
					return file, nil
				}
			case "path substitution":
				calls := 0
				f.lstat = func(name string) (os.FileInfo, error) {
					if name == node {
						calls++
						if calls > 1 {
							return readinessMetadata(999, true, 64), nil
						}
					}
					return f.infos[name], nil
				}
			}
			source, err := openPointerWithOperations(t.Context(), "/fixture", "event0", ops)
			if err == nil || source.close != nil || f.count("readable:"+node) != 0 {
				t.Fatal("unproved event reached a readable open")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestPointerAcquisitionEveryReturnedOwnerClosesOnError(t *testing.T) {
	for _, mode := range []string{"root handle error", "sys handle error", "path handle error", "reader handle error", "root typed nil", "sys typed nil", "path typed nil", "reader typed nil", "path nil success", "reader nil success"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := pointerOwnedFixture(t)
			node := "dev/input/event0"
			switch mode {
			case "root handle error":
				open := ops.input.openRoot
				ops.input.openRoot = func(path string) (inputReadinessRoot, error) {
					r, _ := open(path)
					return r, errors.New("private root")
				}
			case "root typed nil":
				ops.input.openRoot = func(string) (inputReadinessRoot, error) { var r *readinessRoot; return r, nil }
			case "sys handle error", "sys typed nil":
				f.openSys = func(r *readinessRoot) (inputReadinessRoot, error) {
					if mode == "sys typed nil" {
						var empty *readinessRoot
						return empty, nil
					}
					return r, errors.New("private sys")
				}
			case "path handle error", "path typed nil", "path nil success":
				f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
					if name != node {
						return file, nil
					}
					if mode == "path typed nil" {
						var p *readinessFile
						return p, nil
					}
					if mode == "path nil success" {
						return nil, nil
					}
					return file, errors.New("private path")
				}
			case "reader handle error", "reader typed nil", "reader nil success":
				f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
					if name != node {
						return file, nil
					}
					if mode == "reader typed nil" {
						var p *readinessFile
						return p, nil
					}
					if mode == "reader nil success" {
						return nil, nil
					}
					return file, errors.New("private reader")
				}
			}
			source, err := openPointerWithOperations(t.Context(), "/fixture", "event0", ops)
			if err != errInputAcquire || source.close != nil {
				t.Fatalf("provider contract failure was not hard: %v", err)
			}
			if mode == "root typed nil" {
				if len(f.acquired) != 0 {
					t.Fatal("nil root was acquired")
				}
				return
			}
			// A handle-plus-error reader still precedes its path in the close ledger.
			f.closed()
			if f.log[len(f.log)-1] != "close:root:fixture" {
				t.Fatal("root ownership lost")
			}
		})
	}
}

func TestPointerAcquisitionTemporaryAndRejectedCloseErrorsRemainHard(t *testing.T) {
	for _, mode := range []string{"metadata reader", "metadata path", "event reader", "event path", "sys root", "outer root"} {
		for _, rejected := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/accepted", true: "/rejected"}[rejected], func(t *testing.T) {
				f, ops := pointerOwnedFixture(t)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				triggered := false
				fail := func() error { triggered = true; cancel(); f.stop = true; return errors.New("private close") }
				f.openPath = func(name string, file *readinessFile) (inputPathFile, error) {
					if mode == "metadata path" && strings.HasPrefix(name, "sys/") || mode == "event path" && strings.HasPrefix(name, "dev/") {
						file.closeFunc = fail
					}
					return file, nil
				}
				f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
					if mode == "metadata reader" && strings.HasPrefix(name, "sys/") || mode == "event reader" && strings.HasPrefix(name, "dev/") {
						file.closeFunc = fail
					}
					return file, nil
				}
				f.openSys = func(r *readinessRoot) (inputReadinessRoot, error) {
					if mode == "sys root" {
						r.token.closeFunc = fail
					}
					return r, nil
				}
				if mode == "outer root" {
					f.root.token.closeFunc = fail
				}
				if rejected {
					ops.query = func(inputReadableFile, uintptr, []byte) (int, error) { return 0, errors.New("private query") }
				}
				source, err := openPointerWithOperations(ctx, "/fixture", "event0", ops)
				if source.close != nil {
					err = source.close()
				}
				if !triggered || err != errInputClose {
					t.Fatalf("cleanup uncertainty downgraded: %v", err)
				}
				assertPointerOwnerOrder(t, f)
				if strings.HasPrefix(mode, "metadata") && f.count("path:dev/input/event0") != 0 {
					t.Fatal("event acquired after temporary cleanup fault")
				}
			})
		}
	}
}

func TestPointerRegistryProtocolBoundsAndLateCloseCancellation(t *testing.T) {
	for _, mode := range []string{"normal", "exact cap", "overflow", "negative", "oversized count", "zero progress", "partial error", "joined EOF", "cancel read", "cancel close", "hard close cancel", "root close cancel", "empty root close cancel"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := pointerOwnedFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			triggered := false
			if mode == "exact cap" || mode == "overflow" {
				f.data["proc/bus/input/devices"] = strings.Repeat("#\n", maximumInputRegistryBytes/2)
				if mode == "overflow" {
					f.data["proc/bus/input/devices"] += "x"
				}
			}
			if mode == "empty root close cancel" {
				f.data["proc/bus/input/devices"] = ""
			}
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if strings.HasPrefix(mode, "cancel close") || mode == "hard close cancel" {
					file.closeFunc = func() error {
						triggered = true
						cancel()
						f.stop = true
						if mode == "hard close cancel" {
							return errors.New("private close")
						}
						return nil
					}
				}
				switch mode {
				case "negative", "oversized count", "zero progress", "partial error", "joined EOF", "cancel read":
					file.readFunc = func(p []byte) (int, error) {
						triggered = true
						switch mode {
						case "negative":
							cancel()
							return -1, io.EOF
						case "oversized count":
							cancel()
							return len(p) + 1, nil
						case "zero progress":
							return 0, nil
						case "partial error":
							p[0] = 'x'
							cancel()
							return 1, errors.New("private read")
						case "joined EOF":
							return 0, errors.Join(io.EOF, errors.New("private read"))
						default:
							cancel()
							return 0, io.EOF
						}
					}
				}
				return file, nil
			}
			if mode == "root close cancel" || mode == "empty root close cancel" {
				f.root.token.closeFunc = func() error { triggered = true; cancel(); f.stop = true; return nil }
			}
			names, err := discoverPointerWithOperations(ctx, "/fixture", ops)
			switch mode {
			case "normal":
				if err != nil || !reflect.DeepEqual(names, []string{"event0"}) {
					t.Fatal("normal registry failed")
				}
			case "exact cap":
				if err != nil || len(names) != 0 {
					t.Fatal("exact cap plus separate EOF rejected")
				}
			case "cancel read", "cancel close", "root close cancel", "empty root close cancel":
				if !triggered || err != context.Canceled || names != nil {
					t.Fatal("late cancellation lost")
				}
			case "hard close cancel":
				if !triggered || err != errInputClose {
					t.Fatal("hard close was hidden by cancellation")
				}
			default:
				if err != errInputRead || names != nil {
					t.Fatalf("protocol/read failure was not hard: %v", err)
				}
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestPointerOwnedProducerRetainsCompleteCountersOnCloseCancellation(t *testing.T) {
	for _, fault := range []bool{false, true} {
		t.Run(map[bool]string{false: "healthy", true: "close-cancel"}[fault], func(t *testing.T) {
			f, ops := pointerOwnedFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reads := 0
			eventSize := int(unsafe.Sizeof(inputEvent{}))
			data := pointerFixtureBytes(eventSize, pointerCompleteEvents(0x110, 0x111, 0x112))
			f.openReader = func(name string, file *readinessFile) (inputReadableFile, error) {
				if name == "dev/input/event0" {
					file.readFunc = func(p []byte) (int, error) {
						reads++
						if reads == 1 || len(data) == 0 {
							return 0, syscall.EAGAIN
						}
						n := copy(p, data)
						data = data[n:]
						return n, nil
					}
					if fault {
						file.closeFunc = func() error { cancel(); f.stop = true; return errors.New("private close") }
					}
				}
				return file, nil
			}
			backend := pointerBackend{discover: func(context.Context, string) ([]string, error) { return []string{"event0"}, nil }, open: func(ctx context.Context, root, name string) (pointerOpened, error) {
				return openPointerWithOperations(ctx, root, name, ops)
			}}
			result := runPointerWith(ctx, PointerOptions{Root: "/fixture", Duration: time.Second}, backend)
			expected := StatusPass
			if fault {
				expected = StatusError
			}
			if result.Status != expected || len(result.Devices) != 1 || result.Devices[0].Status != StatusPass || !result.Devices[0].Readable || result.Devices[0].Frames != 8 || result.Devices[0].XPositive != 1 || result.Devices[0].XNegative != 1 || result.Devices[0].YPositive != 1 || result.Devices[0].YNegative != 1 || len(result.Devices[0].Buttons) != 3 {
				t.Fatalf("real private producer lost complete observations: %#v", result)
			}
			for _, button := range result.Devices[0].Buttons {
				if button.Presses != 1 || button.Releases != 1 || button.Pairs != 1 {
					t.Fatal("button evidence changed")
				}
			}
			if fault && (result.StopReason != "error" || result.Detail != "pointer descriptor cleanup failed") {
				t.Fatal("late cancel concealed owned close fault")
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}

func TestPointerSelectedSysfsNumberKeepsExactBoundsAndClosePrecedence(t *testing.T) {
	for _, mode := range []string{"exact cap", "overflow", "malformed", "ineligible", "partial error", "joined EOF", "close cancel", "hard close cancel"} {
		t.Run(mode, func(t *testing.T) {
			f, ops := pointerOwnedFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			triggered := false
			name := "sys/class/input/event0/dev"
			switch mode {
			case "exact cap":
				f.data[name] = "13:64" + strings.Repeat(" ", 123)
			case "overflow":
				f.data[name] = "13:64" + strings.Repeat(" ", 124)
			case "malformed":
				f.data[name] = "13:064"
			case "ineligible":
				f.data[name] = "13:96"
			}
			f.openReader = func(path string, file *readinessFile) (inputReadableFile, error) {
				if path == name {
					switch mode {
					case "partial error":
						file.readFunc = func(p []byte) (int, error) {
							triggered = true
							copy(p, "13:64")
							cancel()
							return 5, errors.New("private metadata read")
						}
					case "joined EOF":
						file.readFunc = func([]byte) (int, error) {
							triggered = true
							return 0, errors.Join(io.EOF, errors.New("private metadata read"))
						}
					case "close cancel", "hard close cancel":
						file.closeFunc = func() error {
							triggered = true
							cancel()
							f.stop = true
							if mode == "hard close cancel" {
								return errors.New("private metadata close")
							}
							return nil
						}
					}
				}
				return file, nil
			}
			source, err := openPointerWithOperations(ctx, "/fixture", "event0", ops)
			if mode == "exact cap" {
				if err != nil || source.close == nil {
					t.Fatal("exact128 plus separate EOF rejected")
				}
				if err := source.close(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil || source.close != nil || f.count("path:dev/input/event0") != 0 {
					t.Fatal("invalid or interrupted mandatory identity reached the node")
				}
				if mode == "close cancel" && (!triggered || err != context.Canceled) {
					t.Fatal("late sysfs cancellation lost")
				}
				if mode == "hard close cancel" && (!triggered || err != errInputClose) {
					t.Fatal("mandatory cleanup failure was downgraded")
				}
				if (mode == "partial error" || mode == "joined EOF") && (!triggered || err != errInputRead) {
					t.Fatal("metadata error became an interruption or EOF")
				}
			}
			assertPointerOwnerOrder(t, f)
		})
	}
}
