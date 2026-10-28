package inventory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type scalarInformation struct {
	mode      fs.FileMode
	size      int64
	sizeCalls *int
}

func (*scalarInformation) Name() string { return "fixture" }
func (s *scalarInformation) Size() int64 {
	if s.sizeCalls != nil {
		(*s.sizeCalls)++
	}
	return s.size
}
func (s *scalarInformation) Mode() fs.FileMode { return s.mode }
func (*scalarInformation) ModTime() time.Time  { return time.Time{} }
func (*scalarInformation) IsDir() bool         { return false }
func (*scalarInformation) Sys() any            { return nil }

type scalarHandle struct {
	info              fs.FileInfo
	statErr, closeErr error
	read              func([]byte) (int, error)
	onStat, onClose   func()
	stats, closes     int
	requests          []int
}

func (f *scalarHandle) Stat() (fs.FileInfo, error) {
	f.stats++
	if f.onStat != nil {
		f.onStat()
	}
	return f.info, f.statErr
}
func (f *scalarHandle) Read(p []byte) (int, error) {
	f.requests = append(f.requests, len(p))
	return f.read(p)
}
func (f *scalarHandle) Close() error {
	f.closes++
	if f.onClose != nil {
		f.onClose()
	}
	return f.closeErr
}

func scalarBytes(value []byte, short int, together bool) *scalarHandle {
	remaining := append([]byte(nil), value...)
	return &scalarHandle{info: &scalarInformation{}, read: func(p []byte) (int, error) {
		if len(remaining) == 0 {
			return 0, io.EOF
		}
		if short > 0 && len(p) > short {
			p = p[:short]
		}
		n := copy(p, remaining)
		remaining = remaining[n:]
		if together && len(remaining) == 0 {
			return n, io.EOF
		}
		return n, nil
	}}
}

func scalarAcquire(ctx context.Context, f *scalarHandle) ([]byte, bool, error) {
	return readInventoryScalar(ctx, "fixture", func(string) (inventoryScalar, error) { return f, nil })
}

func requireScalarFailure(t *testing.T, data []byte, observed bool, err, want error) {
	t.Helper()
	if data != nil || observed || err != want {
		t.Fatalf("unexpected outcome: bytes=%d observed=%v error=%v want=%v", len(data), observed, err, want)
	}
}

func TestInventoryScalarRawBoundaryAndExactEOF(t *testing.T) {
	for _, size := range []int{0, 1, 65535, 65536} {
		for _, together := range []bool{false, true} {
			for _, short := range []int{0, 1, 37} {
				t.Run(fmt.Sprintf("%d/%v/%d", size, together, short), func(t *testing.T) {
					// Alternating bytes and a multibyte suffix expose buffer aliasing;
					// byte accounting is independent of character or trimmed length.
					value := bytes.Repeat([]byte(" ab\t"), (size+3)/4)[:size]
					if size >= 2 {
						copy(value[size-2:], []byte("é"))
					}
					f := scalarBytes(value, short, together)
					data, observed, err := scalarAcquire(t.Context(), f)
					if err != nil || !observed || !bytes.Equal(data, value) || f.closes != 1 || f.stats != 1 {
						t.Fatal("bounded complete raw value changed or lost", err)
					}
					for _, request := range f.requests {
						if request <= 0 || request > 4096 {
							t.Fatal("nonfinite or nonpositive read request")
						}
					}
					if size == 65536 && !together && f.requests[len(f.requests)-1] != 1 {
						t.Fatal("exact limit was accepted without a one-byte EOF probe")
					}
				})
			}
		}
	}
}

func TestInventoryScalarOverflowDominatesTerminalErrorAndCancellation(t *testing.T) {
	for _, terminal := range []error{nil, io.EOF, fs.ErrPermission} {
		for _, canceled := range []bool{false, true} {
			for _, closeFailed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%v/%v/%v", terminal, canceled, closeFailed), func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					remaining, emitted := 65537, 0
					f := &scalarHandle{info: &scalarInformation{}, read: func(p []byte) (int, error) {
						if remaining == 0 {
							t.Fatal("read continued after overflow")
						}
						n := min(len(p), remaining)
						for index := 0; index < n; index++ {
							p[index] = ' '
						}
						remaining -= n
						emitted += n
						if remaining == 0 {
							if len(p) != 1 {
								t.Fatal("overflow probe was not bounded")
							}
							if canceled {
								cancel()
							}
							return n, terminal
						}
						return n, nil
					}}
					want := ErrScalarLimit
					if closeFailed {
						f.closeErr = fs.ErrPermission
						want = ErrScalarClose
					}
					data, observed, err := scalarAcquire(ctx, f)
					requireScalarFailure(t, data, observed, err, want)
					if emitted != 65537 || f.closes != 1 {
						t.Fatal("overflow acquisition or ownership incomplete")
					}
				})
			}
		}
	}
}

func TestInventoryScalarProtocolErrorsAreHardAndCloseOnce(t *testing.T) {
	for _, invalid := range []string{"negative", "too-large", "no-progress"} {
		for _, simultaneous := range []bool{false, true} {
			for _, cleanup := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%v/%v", invalid, simultaneous, cleanup), func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					f := &scalarHandle{info: &scalarInformation{}, read: func(p []byte) (int, error) {
						var err error
						if simultaneous {
							cancel()
							err = fs.ErrPermission
						}
						switch invalid {
						case "negative":
							return -1, err
						case "too-large":
							return len(p) + 1, err
						default:
							return 0, nil
						}
					}}
					want := ErrScalarRead
					if cleanup {
						f.closeErr = fs.ErrPermission
						want = ErrScalarClose
					}
					data, observed, err := scalarAcquire(ctx, f)
					requireScalarFailure(t, data, observed, err, want)
					if f.closes != 1 || len(f.requests) != 1 {
						t.Fatal("malformed reader was retried or not closed once")
					}
				})
			}
		}
	}
}

func TestInventoryScalarObservationFailuresDiscardEveryByte(t *testing.T) {
	for _, observation := range []error{fs.ErrNotExist, fs.ErrPermission, syscall.ENOTSUP, fmt.Errorf("wrapped: %w", io.EOF), errors.Join(io.EOF), errors.Join(io.EOF, fs.ErrPermission)} {
		for _, partial := range []bool{false, true} {
			for _, stopped := range []bool{false, true} {
				for _, cleanup := range []bool{false, true} {
					t.Run(fmt.Sprintf("%v/%v/%v/%v", observation, partial, stopped, cleanup), func(t *testing.T) {
						ctx, cancel := context.WithCancel(t.Context())
						defer cancel()
						f := &scalarHandle{info: &scalarInformation{}, read: func(p []byte) (int, error) {
							if stopped {
								cancel()
							}
							if partial {
								return copy(p, "uncommitted"), observation
							}
							return 0, observation
						}}
						var want error
						if stopped {
							want = context.Canceled
						}
						if cleanup {
							f.closeErr = fs.ErrPermission
							want = ErrScalarClose
						}
						data, observed, err := scalarAcquire(ctx, f)
						requireScalarFailure(t, data, observed, err, want)
						if f.closes != 1 || len(f.requests) != 1 {
							t.Fatal("observation error retried or handle leaked")
						}
					})
				}
			}
		}
	}
	// Earlier short successful reads must also be discarded on a later error.
	f := scalarBytes([]byte("first"), 0, false)
	first := f.read
	calls := 0
	f.read = func(p []byte) (int, error) {
		calls++
		if calls == 1 {
			return first(p)
		}
		return copy(p, "last"), fs.ErrPermission
	}
	data, observed, err := scalarAcquire(t.Context(), f)
	requireScalarFailure(t, data, observed, err, nil)
	if calls != 2 || f.closes != 1 {
		t.Fatal("partial observation fixture did not reach later failure")
	}
}

func TestInventoryScalarStatProofIgnoresSizeAndRejectsUnknownType(t *testing.T) {
	var typedNil *scalarInformation
	for _, info := range []fs.FileInfo{nil, typedNil, &scalarInformation{mode: fs.ModeDir}, &scalarInformation{mode: fs.ModeNamedPipe}, &scalarInformation{mode: fs.ModeDevice}, &scalarInformation{mode: fs.ModeCharDevice | fs.ModeDevice}, &scalarInformation{mode: fs.ModeSocket}, &scalarInformation{mode: fs.ModeSymlink}, &scalarInformation{mode: fs.ModeIrregular}} {
		for _, cleanup := range []bool{false, true} {
			ctx, cancel := context.WithCancel(t.Context())
			f := scalarBytes(nil, 0, true)
			f.info, f.onStat = info, cancel
			want := ErrScalarRead
			if cleanup {
				f.closeErr = fs.ErrPermission
				want = ErrScalarClose
			}
			data, observed, err := scalarAcquire(ctx, f)
			requireScalarFailure(t, data, observed, err, want)
			if len(f.requests) != 0 || f.stats != 1 || f.closes != 1 {
				t.Fatal("unproved descriptor read or leaked")
			}
			cancel()
		}
	}
	for _, size := range []int64{-1, 0, 1 << 62} {
		calls := 0
		f := scalarBytes([]byte("complete"), 2, false)
		f.info = &scalarInformation{size: size, sizeCalls: &calls}
		data, observed, err := scalarAcquire(t.Context(), f)
		if err != nil || !observed || string(data) != "complete" || calls != 0 || f.closes != 1 {
			t.Fatal("Stat.Size influenced acquisition")
		}
	}
	for _, cleanup := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		f := scalarBytes(nil, 0, true)
		f.statErr, f.onStat = fs.ErrPermission, cancel
		want := ErrScalarRead
		if cleanup {
			f.closeErr = fs.ErrPermission
			want = ErrScalarClose
		}
		data, observed, err := scalarAcquire(ctx, f)
		requireScalarFailure(t, data, observed, err, want)
		if len(f.requests) != 0 || f.closes != 1 {
			t.Fatal("failed stat reached read or lost handle")
		}
		cancel()
	}
}

type nilScalarContext struct{ context.Context }

func TestInventoryScalarOpenAndDependencyProtocol(t *testing.T) {
	var typedNil *scalarHandle
	for _, handle := range []inventoryScalar{nil, typedNil} {
		for _, openErr := range []error{nil, fs.ErrNotExist, fs.ErrPermission, syscall.ENOTSUP} {
			for _, stopped := range []bool{false, true} {
				ctx, cancel := context.WithCancel(t.Context())
				data, observed, err := readInventoryScalar(ctx, "fixture", func(string) (inventoryScalar, error) {
					if stopped {
						cancel()
					}
					return handle, openErr
				})
				var want error
				if stopped {
					want = context.Canceled
				}
				if openErr == nil {
					want = ErrScalarRead
				}
				requireScalarFailure(t, data, observed, err, want)
				cancel()
			}
		}
	}
	for _, cleanup := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		f := scalarBytes(nil, 0, true)
		want := ErrScalarRead
		if cleanup {
			f.closeErr = fs.ErrPermission
			want = ErrScalarClose
		}
		data, observed, err := readInventoryScalar(ctx, "fixture", func(string) (inventoryScalar, error) { cancel(); return f, fs.ErrPermission })
		requireScalarFailure(t, data, observed, err, want)
		if f.closes != 1 || f.stats != 0 || len(f.requests) != 0 {
			t.Fatal("open-error handle not closed exactly once before other calls")
		}
		cancel()
	}
	var typedContext *nilScalarContext
	for _, ctx := range []context.Context{nil, typedContext} {
		calls := 0
		data, observed, err := readInventoryScalar(ctx, "fixture", func(string) (inventoryScalar, error) { calls++; return nil, nil })
		requireScalarFailure(t, data, observed, err, ErrInvalidContext)
		if calls != 0 {
			t.Fatal("invalid context reached opener")
		}
	}
	data, observed, err := readInventoryScalar(t.Context(), "fixture", nil)
	requireScalarFailure(t, data, observed, err, ErrScalarRead)
}

func TestInventoryScalarCancellationAtEveryOwnedBoundary(t *testing.T) {
	for _, boundary := range []string{"before-open", "open", "stat", "short-read", "eof", "before-probe", "close"} {
		for _, cleanup := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", boundary, cleanup), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				f := scalarBytes([]byte("value"), 0, true)
				if boundary == "before-open" {
					cancel()
				}
				if boundary == "stat" {
					f.onStat = cancel
				}
				if boundary == "close" {
					f.onClose = cancel
				}
				if boundary == "short-read" {
					f.read = func(p []byte) (int, error) { cancel(); return copy(p, "value"), nil }
				}
				if boundary == "eof" {
					f.read = func(p []byte) (int, error) { cancel(); return copy(p, "value"), io.EOF }
				}
				if boundary == "before-probe" {
					f = scalarBytes(bytes.Repeat([]byte{'x'}, 65536), 0, false)
					original, total := f.read, 0
					f.read = func(p []byte) (int, error) {
						n, err := original(p)
						total += n
						if total == 65536 {
							cancel()
						}
						return n, err
					}
				}
				if cleanup {
					f.closeErr = fs.ErrPermission
				}
				opens := 0
				data, observed, err := readInventoryScalar(ctx, "fixture", func(string) (inventoryScalar, error) {
					opens++
					if boundary == "open" {
						cancel()
					}
					return f, nil
				})
				want := context.Canceled
				if cleanup && boundary != "before-open" {
					want = ErrScalarClose
				}
				requireScalarFailure(t, data, observed, err, want)
				if boundary == "before-open" {
					if opens != 0 || f.closes != 0 {
						t.Fatal("pre-cancel acquired a handle")
					}
				} else if opens != 1 || f.closes != 1 {
					t.Fatal("cancelled acquisition leaked handle")
				}
				if boundary == "before-probe" && len(f.requests) != 16 {
					t.Fatal("cancelled full value attempted another read or invented EOF")
				}
				if (boundary == "open" || boundary == "stat") && len(f.requests) != 0 {
					t.Fatal("cancellation reached later read")
				}
			})
		}
	}
}

func TestInventoryScalarProductionFileAndOwnedClosure(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "value")
	for _, size := range []int{0, 65536, 65537} {
		value := []byte(strings.Repeat(" ", size))
		if err := os.WriteFile(name, value, 0600); err != nil {
			t.Fatal(err)
		}
		var opened *os.File
		data, observed, err := readInventoryScalar(t.Context(), name, func(name string) (inventoryScalar, error) {
			f, e := openInventoryScalar(name)
			if f != nil {
				opened = f.(*os.File)
			}
			return f, e
		})
		if size == 65537 {
			requireScalarFailure(t, data, observed, err, ErrScalarLimit)
		} else if err != nil || !observed || !bytes.Equal(data, value) {
			t.Fatal("real scalar boundary rejected", err)
		}
		// A second Close is a portable closed-handle probe. An unclosed handle
		// would return nil and fail this assertion, including on Windows.
		if opened == nil || !errors.Is(opened.Close(), os.ErrClosed) {
			t.Fatal("real scalar descriptor leaked")
		}
	}
	data, observed, err := readInventoryScalar(t.Context(), root, openInventoryScalar)
	requireScalarFailure(t, data, observed, err, ErrScalarRead)
}

func TestInventoryScalarCompletedValueStillRequiresSuccessfulClose(t *testing.T) {
	f := scalarBytes([]byte("complete but not admitted"), 3, true)
	f.closeErr = errors.New("untrusted provider closure detail")
	data, observed, err := scalarAcquire(t.Context(), f)
	requireScalarFailure(t, data, observed, err, ErrScalarClose)
	if f.closes != 1 || len(f.requests) < 2 {
		t.Fatal("complete acquisition did not reach close exactly once")
	}
}
