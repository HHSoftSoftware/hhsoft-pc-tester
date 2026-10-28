package networktest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
)

type networkEntry struct {
	name  string
	calls int
	hook  func()
}

func (e *networkEntry) Name() string {
	e.calls++
	if e.hook != nil {
		e.hook()
	}
	return e.name
}
func (*networkEntry) IsDir() bool                { panic("entry type is not authority") }
func (*networkEntry) Type() os.FileMode          { panic("entry type is not authority") }
func (*networkEntry) Info() (os.FileInfo, error) { panic("entry links must not be walked") }

type networkDirectory struct {
	entries                       []os.DirEntry
	info                          os.FileInfo
	statErr, closeErr             error
	read                          func(int) ([]os.DirEntry, error)
	closeHook                     func()
	reads, closes, maximumRequest int
}

func (d *networkDirectory) Stat() (os.FileInfo, error) { return d.info, d.statErr }
func (d *networkDirectory) Close() error {
	d.closes++
	if d.closeHook != nil {
		d.closeHook()
	}
	return d.closeErr
}
func (d *networkDirectory) ReadDir(n int) ([]os.DirEntry, error) {
	if n <= 0 || n > 32 {
		panic("unbounded directory request")
	}
	d.reads++
	d.maximumRequest = max(d.maximumRequest, n)
	if d.read != nil {
		return d.read(n)
	}
	if len(d.entries) == 0 {
		return nil, io.EOF
	}
	count := min(n, len(d.entries))
	out := d.entries[:count]
	d.entries = d.entries[count:]
	return out, nil
}
func networkEntries(count int) *networkDirectory {
	d := &networkDirectory{info: networkInfo{mode: os.ModeDir}}
	for i := count - 1; i >= 0; i-- {
		d.entries = append(d.entries, &networkEntry{name: fmt.Sprintf("eth%03d", i)})
	}
	return d
}

func TestNetworkRegistryBoundsNamesBeforeAllocationLimit(t *testing.T) {
	for _, count := range []int{0, 1, 127, 128, 129, 4096} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			d := networkEntries(count)
			all := append([]os.DirEntry(nil), d.entries...)
			names, err := readNetworkRegistry(t.Context(), "owned", func(string) (networkRegistryDirectory, error) { return d, nil })
			if d.closes != 1 || d.maximumRequest > 32 || d.reads > 5 {
				t.Fatal("registry exceeded finite calls or ownership")
			}
			if count > 128 {
				if err != errNetworkRegistryRead || names != nil {
					t.Fatal("overflow truncated to pass")
				}
				return
			}
			if err != nil || len(names) != count {
				t.Fatal("exact boundary rejected")
			}
			for i, name := range names {
				if name != fmt.Sprintf("eth%03d", i) {
					t.Fatal("names not cached and sorted")
				}
			}
			for _, entry := range all {
				if entry.(*networkEntry).calls != 1 {
					t.Fatal("entry name observed twice")
				}
			}
		})
	}
}

func TestNetworkRegistryRejectsProtocolTypesAndDuplicateNames(t *testing.T) {
	for _, name := range []string{"zero progress", "too many", "joined EOF", "wrapped EOF", "partial error", "duplicate", "invalid", "nil entry", "typed nil entry", "stat error", "nil info", "typed nil info", "regular", "handle and error", "nil and nil", "typed nil handle"} {
		t.Run(name, func(t *testing.T) {
			d := networkEntries(1)
			var handle networkRegistryDirectory = d
			var openErr error
			closes := 1
			switch name {
			case "zero progress":
				d.read = func(int) ([]os.DirEntry, error) { return nil, nil }
			case "too many":
				d.read = func(n int) ([]os.DirEntry, error) { return make([]os.DirEntry, n+1), io.EOF }
			case "joined EOF":
				d.read = func(int) ([]os.DirEntry, error) { return nil, errors.Join(io.EOF, os.ErrPermission) }
			case "wrapped EOF":
				d.read = func(int) ([]os.DirEntry, error) { return nil, fmt.Errorf("wrapped: %w", io.EOF) }
			case "partial error":
				d.read = func(int) ([]os.DirEntry, error) { return d.entries, os.ErrPermission }
			case "duplicate":
				d.entries = []os.DirEntry{&networkEntry{name: "eth0"}, &networkEntry{name: "eth0"}}
			case "invalid":
				d.entries = []os.DirEntry{&networkEntry{name: "../eth0"}}
			case "nil entry":
				d.entries = []os.DirEntry{nil}
			case "typed nil entry":
				var entry *networkEntry
				d.entries = []os.DirEntry{entry}
			case "stat error":
				d.statErr = os.ErrPermission
			case "nil info":
				d.info = nil
			case "typed nil info":
				var info *networkInfo
				d.info = info
			case "regular":
				d.info = networkInfo{}
			case "handle and error":
				openErr = os.ErrNotExist
			case "nil and nil":
				handle = nil
				closes = 0
			case "typed nil handle":
				var directory *networkDirectory
				handle = directory
				closes = 0
			}
			names, err := readNetworkRegistry(t.Context(), "owned", func(string) (networkRegistryDirectory, error) { return handle, openErr })
			if err != errNetworkRegistryRead || names != nil || d.closes != closes {
				t.Fatal("registry fault was concealed")
			}
		})
	}
	for _, name := range []string{"", ".", "..", "eth/0", "eth\\0", "eth\x00", "eth\n"} {
		if validInterfaceName(name) {
			t.Fatal("invalid name admitted")
		}
	}
	for _, name := range []string{"lo", "eth0", "veth.1", "veth:1", "name with space", "網路"} {
		if !validInterfaceName(name) {
			t.Fatal("legacy valid name narrowed")
		}
	}
}

func TestNetworkRegistryCloseAndFinalCancellation(t *testing.T) {
	for _, phase := range []string{"empty close", "last name", "final EOF", "read error cancel", "close error cancel", "handle error close"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			d := networkEntries(1)
			want := context.Canceled
			var openErr error
			switch phase {
			case "empty close":
				d.entries = nil
				d.closeHook = cancel
			case "last name":
				d.entries[0].(*networkEntry).hook = cancel
			case "final EOF":
				d.read = func(int) ([]os.DirEntry, error) { cancel(); return nil, io.EOF }
			case "read error cancel":
				d.read = func(int) ([]os.DirEntry, error) { cancel(); return nil, os.ErrPermission }
				want = errNetworkRegistryRead
			case "close error cancel":
				d.closeHook = cancel
				d.closeErr = os.ErrPermission
				want = errNetworkRegistryClose
			case "handle error close":
				openErr = os.ErrNotExist
				d.closeErr = os.ErrPermission
				want = errNetworkRegistryClose
			}
			names, err := readNetworkRegistry(ctx, "owned", func(string) (networkRegistryDirectory, error) { return d, openErr })
			if err != want || names != nil || d.closes != 1 {
				t.Fatal("late cancellation or close precedence lost")
			}
			if (phase == "last name" || phase == "final EOF" || phase == "read error cancel") && d.reads != 1 {
				t.Fatal("later registry read continued")
			}
		})
	}
}
