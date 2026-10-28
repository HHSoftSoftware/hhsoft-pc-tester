package usbtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

type usbRegistryEntry struct {
	name  string
	hook  func()
	calls int
}

func (e *usbRegistryEntry) Name() string {
	e.calls++
	if e.hook != nil {
		e.hook()
	}
	return e.name
}
func (*usbRegistryEntry) IsDir() bool { panic("registry enumeration must not query entry type") }
func (*usbRegistryEntry) Type() os.FileMode {
	panic("registry enumeration must preserve symlink names")
}
func (*usbRegistryEntry) Info() (os.FileInfo, error) {
	panic("registry enumeration must not follow entry links")
}

type usbRegistryFixture struct {
	info                 os.FileInfo
	statErr, closeErr    error
	read                 func(int) ([]os.DirEntry, error)
	statHook, closeHook  func()
	reads, closes, stats int
	requests             []int
}

func (f *usbRegistryFixture) Stat() (os.FileInfo, error) {
	f.stats++
	if f.statHook != nil {
		f.statHook()
	}
	return f.info, f.statErr
}
func (f *usbRegistryFixture) ReadDir(n int) ([]os.DirEntry, error) {
	f.reads++
	f.requests = append(f.requests, n)
	return f.read(n)
}
func (f *usbRegistryFixture) Close() error {
	f.closes++
	if f.closeHook != nil {
		f.closeHook()
	}
	return f.closeErr
}
func usbRegistry(count int) *usbRegistryFixture {
	next := 0
	return &usbRegistryFixture{info: usbInfo{mode: os.ModeDir}, read: func(n int) ([]os.DirEntry, error) {
		if n <= 0 || n > 64 {
			panic("unbounded directory request")
		}
		batch := make([]os.DirEntry, 0, n)
		for next < count && len(batch) < n {
			batch = append(batch, &usbRegistryEntry{name: fmt.Sprintf("entry-%05d", count-next)})
			next++
		}
		if next == count {
			return batch, io.EOF
		}
		return batch, nil
	}}
}

func TestUSBRegistryBoundsAcquisitionBeforeLimit(t *testing.T) {
	for _, count := range []int{0, 1, 63, 64, 2047, 2048, 2049, 10000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			file := usbRegistry(count)
			names, err := readUSBRegistry(t.Context(), "owned", func(string) (usbRegistryDirectory, error) { return file, nil })
			if file.closes != 1 || file.reads > 33 {
				t.Fatal("enumeration escaped finite ownership")
			}
			for _, request := range file.requests {
				if request < 1 || request > 64 {
					t.Fatal("unbounded allocation request")
				}
			}
			if count <= 2048 {
				if err != nil || len(names) != count {
					t.Fatal("complete registry at the bound rejected")
				}
				for i := 1; i < len(names); i++ {
					if names[i-1] >= names[i] {
						t.Fatal("names are not uniquely sorted")
					}
				}
			} else if names != nil || err != errUSBRegistryRead || file.requests[len(file.requests)-1] != 1 {
				t.Fatal("overflow was truncated or read without a one-entry probe")
			}
		})
	}
	// Real ReadDir may deliver the last full batch with nil and EOF separately.
	file := usbRegistry(2048)
	read := file.read
	file.read = func(n int) ([]os.DirEntry, error) {
		batch, err := read(n)
		if len(batch) != 0 {
			return batch, nil
		}
		return batch, err
	}
	names, err := readUSBRegistry(t.Context(), "owned", func(string) (usbRegistryDirectory, error) { return file, nil })
	if err != nil || len(names) != 2048 || file.closes != 1 || file.reads != 33 || file.requests[32] != 1 {
		t.Fatal("separate exact-limit EOF probe changed the result")
	}
}

func TestUSBRegistryRequiresExactCompletionAndValidNames(t *testing.T) {
	var typedNil *usbRegistryEntry
	for _, test := range []struct {
		name string
		read func(int) ([]os.DirEntry, error)
	}{
		{"no progress", func(int) ([]os.DirEntry, error) { return nil, nil }},
		{"oversized batch", func(int) ([]os.DirEntry, error) { return make([]os.DirEntry, 65), io.EOF }},
		{"partial error", func(int) ([]os.DirEntry, error) {
			return []os.DirEntry{&usbRegistryEntry{name: "usb1"}}, os.ErrPermission
		}},
		{"joined EOF", func(int) ([]os.DirEntry, error) { return nil, errors.Join(io.EOF) }},
		{"mixed EOF", func(int) ([]os.DirEntry, error) { return nil, errors.Join(io.EOF, os.ErrPermission) }},
		{"nil entry", func(int) ([]os.DirEntry, error) { return []os.DirEntry{nil}, io.EOF }},
		{"typed nil entry", func(int) ([]os.DirEntry, error) { return []os.DirEntry{typedNil}, io.EOF }},
		{"duplicate", func(int) ([]os.DirEntry, error) {
			return []os.DirEntry{&usbRegistryEntry{name: "usb1"}, &usbRegistryEntry{name: "usb1"}}, io.EOF
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := usbRegistry(0)
			file.read = test.read
			if names, err := readUSBRegistry(t.Context(), "owned", func(string) (usbRegistryDirectory, error) { return file, nil }); names != nil || err != errUSBRegistryRead || file.closes != 1 {
				t.Fatal("incomplete or invalid registry gained usable names")
			}
		})
	}
	for _, name := range []string{"", ".", "..", "a/b", "a\\b", "a\x00b", strings.Repeat("x", 256)} {
		t.Run(fmt.Sprintf("invalid_name_%x", []byte(name)), func(t *testing.T) {
			file := usbRegistry(0)
			file.read = func(int) ([]os.DirEntry, error) { return []os.DirEntry{&usbRegistryEntry{name: name}}, io.EOF }
			if names, err := readUSBRegistry(t.Context(), "owned", func(string) (usbRegistryDirectory, error) { return file, nil }); names != nil || err != errUSBRegistryRead || file.closes != 1 {
				t.Fatal("unsafe name accepted")
			}
		})
	}
	file := usbRegistry(0)
	file.read = func(int) ([]os.DirEntry, error) {
		if file.reads == 1 {
			return []os.DirEntry{&usbRegistryEntry{name: "usb1"}}, nil
		}
		return []os.DirEntry{&usbRegistryEntry{name: "usb1"}}, io.EOF
	}
	if names, err := readUSBRegistry(t.Context(), "owned", func(string) (usbRegistryDirectory, error) { return file, nil }); names != nil || err != errUSBRegistryRead || file.closes != 1 {
		t.Fatal("cross-batch duplicate accepted")
	}
}

func TestUSBRegistryCopiesNamesWithoutFollowingEntries(t *testing.T) {
	a, b := &usbRegistryEntry{name: "usb2"}, &usbRegistryEntry{name: "usb1"}
	batch := []os.DirEntry{a, b}
	file := usbRegistry(0)
	file.read = func(int) ([]os.DirEntry, error) { return batch, io.EOF }
	file.closeHook = func() { a.name = "changed-after-read"; b.name = "changed-too"; batch[0] = nil }
	names, err := readUSBRegistry(t.Context(), "owned", func(string) (usbRegistryDirectory, error) { return file, nil })
	if err != nil || !reflect.DeepEqual(names, []string{"usb1", "usb2"}) || a.calls != 1 || b.calls != 1 || file.closes != 1 {
		t.Fatal("entry provider retained authority over cached names")
	}
}

func TestUSBRegistryCloseAndLateCancellationPrecedence(t *testing.T) {
	for _, phase := range []string{"open", "stat", "read", "name", "empty close", "hard read", "overflow"} {
		for _, closeFailure := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/close_%t", phase, closeFailure), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				file := usbRegistry(0)
				open := func(string) (usbRegistryDirectory, error) {
					if phase == "open" {
						cancel()
					}
					return file, nil
				}
				switch phase {
				case "stat":
					file.statHook = cancel
				case "read":
					file.read = func(int) ([]os.DirEntry, error) { cancel(); return nil, io.EOF }
				case "name":
					file.read = func(int) ([]os.DirEntry, error) {
						return []os.DirEntry{&usbRegistryEntry{name: "usb1", hook: cancel}}, io.EOF
					}
				case "empty close":
					file.closeHook = cancel
				case "hard read":
					file.read = func(int) ([]os.DirEntry, error) { cancel(); return nil, os.ErrPermission }
				case "overflow":
					file.read = func(int) ([]os.DirEntry, error) { cancel(); return make([]os.DirEntry, 65), io.EOF }
				}
				if closeFailure {
					file.closeErr = os.ErrPermission
				}
				names, err := readUSBRegistry(ctx, "owned", open)
				want := error(context.Canceled)
				if phase == "hard read" || phase == "overflow" {
					want = errUSBRegistryRead
				}
				if closeFailure {
					want = errUSBRegistryClose
				}
				if names != nil || err != want || file.closes != 1 {
					t.Fatal("closure/hard error/cancellation precedence changed")
				}
			})
		}
	}
}

func TestUSBRegistryRejectsUnownedAndInvalidDescriptors(t *testing.T) {
	var missing *usbRegistryFixture
	for _, handle := range []usbRegistryDirectory{nil, missing} {
		if names, err := readUSBRegistry(t.Context(), "owned", func(string) (usbRegistryDirectory, error) { return handle, nil }); names != nil || err != errUSBRegistryRead {
			t.Fatal("nil directory accepted")
		}
	}
	for _, phase := range []string{"open error", "stat error", "nil info", "typed nil info", "regular", "directory close"} {
		t.Run(phase, func(t *testing.T) {
			file := usbRegistry(0)
			var openErr error
			switch phase {
			case "open error":
				openErr = os.ErrPermission
			case "stat error":
				file.statErr = os.ErrPermission
			case "nil info":
				file.info = nil
			case "typed nil info":
				var info *usbInfo
				file.info = info
			case "regular":
				file.info = usbInfo{}
			case "directory close":
				file.closeErr = os.ErrPermission
			}
			want := errUSBRegistryRead
			if phase == "directory close" {
				want = errUSBRegistryClose
			}
			if names, err := readUSBRegistry(t.Context(), "owned", func(string) (usbRegistryDirectory, error) { return file, openErr }); names != nil || err != want || file.closes != 1 {
				t.Fatal("invalid descriptor earned names or escaped close")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var nilCtx *usbNilContext
	for _, ctx := range []context.Context{nil, nilCtx, ctx} {
		if names, err := readUSBRegistry(ctx, "owned", func(string) (usbRegistryDirectory, error) {
			t.Fatal("invalid context opened registry")
			return nil, nil
		}); names != nil || err == nil {
			t.Fatal("invalid context accepted")
		}
	}
	if names, err := readUSBRegistry(t.Context(), "owned", nil); names != nil || err != errUSBRegistryRead {
		t.Fatal("missing opener accepted")
	}
}
