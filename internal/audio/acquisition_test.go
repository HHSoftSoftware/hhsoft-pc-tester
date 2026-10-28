package audio

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

type acquisitionEntry struct {
	name   string
	names  int
	onName func()
}

func (entry *acquisitionEntry) Name() string {
	entry.names++
	if entry.onName != nil {
		entry.onName()
	}
	return entry.name
}
func (*acquisitionEntry) IsDir() bool                { panic("entry type must not be read") }
func (*acquisitionEntry) Type() os.FileMode          { panic("entry type must not be read") }
func (*acquisitionEntry) Info() (os.FileInfo, error) { panic("entry metadata must not be read") }

type acquisitionDirectory struct {
	read     func(int) ([]os.DirEntry, error)
	close    func() error
	requests []int
	closes   int
}

func (directory *acquisitionDirectory) ReadDir(n int) ([]os.DirEntry, error) {
	directory.requests = append(directory.requests, n)
	return directory.read(n)
}
func (directory *acquisitionDirectory) Close() error {
	directory.closes++
	if directory.close != nil {
		return directory.close()
	}
	return nil
}

type acquisitionReader func([]byte) (int, error)

func (reader acquisitionReader) Read(buffer []byte) (int, error) { return reader(buffer) }

func TestAudioRegistryCachesNamesAndRequiresCompletion(t *testing.T) {
	entries := []*acquisitionEntry{{name: "pcmC0D0p"}, {name: "card0"}, {name: "controlC0"}}
	entries[0].onName = func() {
		if entries[0].names > 1 {
			entries[0].name = "../../changed-on-repeat"
		}
	}
	calls := 0
	directory := &acquisitionDirectory{read: func(n int) ([]os.DirEntry, error) {
		calls++
		if calls == 1 {
			return []os.DirEntry{entries[0], entries[1]}, nil
		}
		if calls == 2 {
			return []os.DirEntry{entries[2]}, nil
		}
		return nil, io.EOF
	}}
	names, err := acquireAudioRegistry(t.Context(), func() (audioDirectory, error) { return directory, nil })
	if err != nil || !reflect.DeepEqual(names, []string{"card0", "controlC0", "pcmC0D0p"}) ||
		!reflect.DeepEqual(directory.requests, []int{32, 32, 32}) || directory.closes != 1 {
		t.Fatalf("completion: names=%v error=%v requests=%v closes=%d", names, err, directory.requests, directory.closes)
	}
	for _, entry := range entries {
		if entry.names != 1 {
			t.Fatalf("name read %d times", entry.names)
		}
	}
}

func TestAudioRegistryLimitUsesOneEntryProbe(t *testing.T) {
	for _, total := range []int{4096, 4097} {
		t.Run(fmt.Sprint(total), func(t *testing.T) {
			position := 0
			var overflow *acquisitionEntry
			directory := &acquisitionDirectory{read: func(n int) ([]os.DirEntry, error) {
				if n < 1 || n > 32 {
					t.Fatalf("unbounded/nonpositive request %d", n)
				}
				if position == total {
					return nil, io.EOF
				}
				var entries []os.DirEntry
				for len(entries) < n && position < total {
					entry := &acquisitionEntry{name: fmt.Sprintf("other-%04d", position)}
					if position == 4096 {
						overflow = entry
					}
					entries = append(entries, entry)
					position++
				}
				return entries, nil
			}}
			names, err := acquireAudioRegistry(t.Context(), func() (audioDirectory, error) { return directory, nil })
			if directory.closes != 1 || len(directory.requests) != 129 || directory.requests[128] != 1 {
				t.Fatalf("ownership/probe: %#v", directory)
			}
			if total == 4096 {
				if err != nil || len(names) != 4096 || names[0] != "other-0000" || names[4095] != "other-4095" {
					t.Fatalf("exact maximum rejected: %d %v", len(names), err)
				}
			} else if err != errAudioRegistryLimit || names != nil || overflow == nil || overflow.names != 0 {
				t.Fatalf("overflow credited: %v %v %#v", names, err, overflow)
			}
		})
	}
}

func TestAudioRegistryRejectsProtocolAndNames(t *testing.T) {
	fault := errors.New("injected read failure")
	var typedNil *acquisitionEntry
	cases := []struct {
		name    string
		entries []os.DirEntry
		err     error
	}{
		{"zero-progress", nil, nil}, {"wrapped-eof", nil, fmt.Errorf("wrapped: %w", io.EOF)},
		{"joined-eof", nil, errors.Join(io.EOF, fault)}, {"data-error", []os.DirEntry{&acquisitionEntry{name: "card0"}}, fault},
		{"nil-entry", []os.DirEntry{nil}, io.EOF}, {"typed-nil-entry", []os.DirEntry{typedNil}, io.EOF},
		{"duplicate", []os.DirEntry{&acquisitionEntry{name: "card0"}, &acquisitionEntry{name: "card0"}}, io.EOF},
	}
	for _, name := range []string{"", ".", "..", "a/b", "a\\b", "a\x00b", strings.Repeat("x", 256), string([]byte{0xff})} {
		cases = append(cases, struct {
			name    string
			entries []os.DirEntry
			err     error
		}{"name-" + fmt.Sprintf("%q", name), []os.DirEntry{&acquisitionEntry{name: name}}, io.EOF})
	}
	excess := make([]os.DirEntry, 33)
	cases = append(cases, struct {
		name    string
		entries []os.DirEntry
		err     error
	}{"over-return", excess, io.EOF})
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := &acquisitionDirectory{read: func(int) ([]os.DirEntry, error) { return test.entries, test.err }}
			names, err := acquireAudioRegistry(t.Context(), func() (audioDirectory, error) { return directory, nil })
			if err != errAudioRegistryRead || names != nil || directory.closes != 1 || len(directory.requests) != 1 {
				t.Fatalf("bad protocol accepted: %v %v %#v", names, err, directory)
			}
		})
	}
}

func TestAudioRegistryAbsenceAndOwnedOpenErrors(t *testing.T) {
	fault := errors.New("injected permission failure")
	calls := 0
	if _, err := acquireAudioRegistry(nil, func() (audioDirectory, error) { calls++; return nil, nil }); err == nil || calls != 0 {
		t.Fatal("nil context reached open")
	}
	if _, err := acquireAudioRegistry(t.Context(), nil); err == nil {
		t.Fatal("nil opener accepted")
	}
	for _, test := range []struct {
		name   string
		err    error
		absent bool
	}{
		{"sole-absence", os.ErrNotExist, true},
		{"openat-path-absence", &os.PathError{Op: "openat", Path: "unused", Err: os.ErrNotExist}, true},
		{"wrapped-openat-absence", fmt.Errorf("wrap: %w", &os.PathError{Op: "openat", Err: os.ErrNotExist}), true},
		{"wrong-open-operation", &os.PathError{Op: "open", Err: os.ErrNotExist}, false},
		{"wrong-operation", &os.PathError{Op: "read", Err: os.ErrNotExist}, false},
		{"mixed-absence", errors.Join(os.ErrNotExist, fault), false},
		{"permission", fault, false}, {"nil-without-error", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			names, err := acquireAudioRegistry(t.Context(), func() (audioDirectory, error) { return nil, test.err })
			if names != nil || (err == nil) != test.absent {
				t.Fatalf("absence disposition: %v %v", names, err)
			}
		})
	}
	var typedNil *acquisitionDirectory
	if _, err := acquireAudioRegistry(t.Context(), func() (audioDirectory, error) { return typedNil, nil }); err == nil {
		t.Fatal("typed nil accepted")
	}
	for _, openErr := range []error{fault, os.ErrNotExist} {
		directory := &acquisitionDirectory{read: func(int) ([]os.DirEntry, error) { t.Fatal("read after open error"); return nil, nil }}
		if _, err := acquireAudioRegistry(t.Context(), func() (audioDirectory, error) { return directory, openErr }); err != errAudioRegistryOpen || directory.closes != 1 || len(directory.requests) != 0 {
			t.Fatalf("owned open error: %v %#v", err, directory)
		}
	}
}

func TestAudioRegistryCancellationAndClosePrecedence(t *testing.T) {
	for _, phase := range []string{"before-open", "read", "name", "close", "read-error-cancel", "close-error-cancel"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			opens := 0
			fault := errors.New("injected close/read failure")
			first := &acquisitionEntry{name: "card0"}
			later := &acquisitionEntry{name: "controlC0"}
			if phase == "name" {
				first.onName = cancel
			}
			directory := &acquisitionDirectory{read: func(int) ([]os.DirEntry, error) {
				if phase == "read" {
					cancel()
				}
				if phase == "read-error-cancel" {
					cancel()
					return nil, fault
				}
				return []os.DirEntry{first, later}, io.EOF
			}, close: func() error {
				if phase == "close" || phase == "close-error-cancel" {
					cancel()
				}
				if phase == "close-error-cancel" {
					return fault
				}
				return nil
			}}
			if phase == "before-open" {
				cancel()
			}
			names, err := acquireAudioRegistry(ctx, func() (audioDirectory, error) { opens++; return directory, nil })
			want := error(context.Canceled)
			if phase == "read-error-cancel" {
				want = errAudioRegistryRead
			}
			if phase == "close-error-cancel" {
				want = errAudioClose
			}
			if err != want || names != nil {
				t.Fatalf("precedence: names=%v err=%v want=%v", names, err, want)
			}
			if phase == "before-open" {
				if opens != 0 || directory.closes != 0 {
					t.Fatal("opened after cancellation")
				}
			} else if opens != 1 || directory.closes != 1 {
				t.Fatal("owned handle not closed exactly once")
			}
			if (phase == "name" || phase == "read") && later.names != 0 {
				t.Fatal("later name read after cancellation")
			}
		})
	}
}

func TestAudioAttributeReadExactBounds(t *testing.T) {
	for _, size := range []int{0, 1, 63, 64, 65} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			source := strings.Repeat("a", size)
			position := 0
			requests := []int{}
			reader := acquisitionReader(func(buffer []byte) (int, error) {
				requests = append(requests, len(buffer))
				if len(buffer) < 1 || len(buffer) > 32 {
					t.Fatalf("bad read size %d", len(buffer))
				}
				if position == len(source) {
					return 0, io.EOF
				}
				n := copy(buffer, source[position:])
				position += n
				return n, nil
			})
			data, err := readAudioBytes(t.Context(), reader)
			if size <= 64 {
				if err != nil || string(data) != source {
					t.Fatalf("complete value: %q %v", data, err)
				}
			} else if err != errAudioRead || data != nil || position != 65 {
				t.Fatalf("overflow: %q %v %d", data, err, position)
			}
			if size >= 64 && requests[len(requests)-1] != 1 {
				t.Fatalf("missing one-byte probe: %v", requests)
			}
		})
	}
	calls := 0
	data, err := readAudioBytes(t.Context(), acquisitionReader(func(buffer []byte) (int, error) {
		calls++
		if calls == 65 {
			return 0, io.EOF
		}
		buffer[0] = 'z'
		return 1, nil
	}))
	if err != nil || string(data) != strings.Repeat("z", 64) || calls != 65 {
		t.Fatalf("bounded short reads: %q %v %d", data, err, calls)
	}
	data, err = readAudioBytes(t.Context(), acquisitionReader(func(buffer []byte) (int, error) { copy(buffer, "116:0\n"); return 6, io.EOF }))
	if err != nil || string(data) != "116:0\n" {
		t.Fatalf("data with exact EOF rejected: %q %v", data, err)
	}
}

func TestAudioAttributeRejectsReadProtocol(t *testing.T) {
	fault := errors.New("injected read failure")
	for _, test := range []struct {
		name string
		n    int
		err  error
	}{
		{"negative", -1, nil}, {"over-return", 33, nil}, {"zero-progress", 0, nil},
		{"read-error", 0, fault}, {"partial-error", 1, fault},
		{"wrapped-eof", 0, fmt.Errorf("wrap: %w", io.EOF)}, {"joined-eof", 0, errors.Join(io.EOF, fault)},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			data, err := readAudioBytes(t.Context(), acquisitionReader(func([]byte) (int, error) { calls++; return test.n, test.err }))
			if err != errAudioRead || data != nil || calls != 1 {
				t.Fatalf("protocol accepted: %q %v %d", data, err, calls)
			}
		})
	}
	var reader acquisitionReader
	if _, err := readAudioBytes(t.Context(), reader); err != errAudioRead {
		t.Fatal("typed nil reader accepted")
	}
}

func TestAudioAttributeReadCancellationPreservesFaults(t *testing.T) {
	for _, phase := range []string{"before", "after-data", "after-data-error"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			if phase == "before" {
				cancel()
			}
			data, err := readAudioBytes(ctx, acquisitionReader(func(buffer []byte) (int, error) {
				calls++
				buffer[0] = 'x'
				cancel()
				if phase == "after-data-error" {
					return 1, errors.New("read failure")
				}
				return 1, io.EOF
			}))
			want := error(context.Canceled)
			if phase == "after-data-error" {
				want = errAudioRead
			}
			if data != nil || err != want || phase == "before" && calls != 0 || phase != "before" && calls != 1 {
				t.Fatalf("fault/cancel: %q %v %d", data, err, calls)
			}
		})
	}
}
