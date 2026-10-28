package networktest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

type networkInfo struct {
	mode os.FileMode
	size int64
}

func (networkInfo) Name() string        { return "owned" }
func (v networkInfo) Size() int64       { return v.size }
func (v networkInfo) Mode() os.FileMode { return v.mode }
func (networkInfo) ModTime() time.Time  { return time.Time{} }
func (v networkInfo) IsDir() bool       { return v.mode.IsDir() }
func (networkInfo) Sys() any            { return nil }

type networkFile struct {
	reader                               io.Reader
	info                                 os.FileInfo
	statErr, closeErr                    error
	read                                 func([]byte) (int, error)
	statHook, closeHook                  func()
	reads, bytes, closes, maximumRequest int
}

func (f *networkFile) Stat() (os.FileInfo, error) {
	if f.statHook != nil {
		f.statHook()
	}
	return f.info, f.statErr
}
func (f *networkFile) Read(p []byte) (int, error) {
	f.reads++
	f.maximumRequest = max(f.maximumRequest, len(p))
	var n int
	var err error
	if f.read != nil {
		n, err = f.read(p)
	} else {
		n, err = f.reader.Read(p)
	}
	if n > 0 {
		f.bytes += n
	}
	return n, err
}
func (f *networkFile) Close() error {
	f.closes++
	if f.closeHook != nil {
		f.closeHook()
	}
	return f.closeErr
}
func networkData(value string) *networkFile {
	return &networkFile{reader: strings.NewReader(value), info: networkInfo{}}
}

func TestNetworkMetadataFiniteBoundsAndExactEOF(t *testing.T) {
	for _, limit := range []int{256, 4096} {
		for _, size := range []int{0, 1, limit - 1, limit, limit + 1, limit * 2} {
			t.Run(fmt.Sprintf("%d/%d", limit, size), func(t *testing.T) {
				f := networkData(strings.Repeat("x", size))
				f.info = networkInfo{size: 1 << 40}
				data, err := acquireNetworkMetadata(t.Context(), "owned", limit, false, false, func(string) (networkMetadataFile, error) { return f, nil })
				if f.closes != 1 || f.maximumRequest > 32 || f.bytes > limit+1 {
					t.Fatal("unbounded request or ownership")
				}
				if size <= limit {
					if err != nil || string(data) != strings.Repeat("x", size) {
						t.Fatal("literal boundary rejected")
					}
				} else if err != errNetworkMetadataRead || data != nil || f.bytes != limit+1 {
					t.Fatal("raw overflow not rejected at probe")
				}
			})
		}
	}
	f := networkData("")
	f.read = func(p []byte) (int, error) { return copy(p, "value"), io.EOF }
	data, err := acquireNetworkMetadata(t.Context(), "owned", 256, false, false, func(string) (networkMetadataFile, error) { return f, nil })
	if err != nil || string(data) != "value" || f.reads != 1 || f.closes != 1 {
		t.Fatal("exact terminal bytes rejected")
	}
	f = networkData("1" + strings.Repeat(" ", 256))
	if data, err = acquireNetworkMetadata(t.Context(), "owned", 256, true, false, func(string) (networkMetadataFile, error) { return f, nil }); err != errNetworkMetadataRead || data != nil {
		t.Fatal("trimming concealed raw overflow")
	}
}

func TestNetworkMetadataRejectsProtocolAndOwnershipFaults(t *testing.T) {
	for _, name := range []string{"negative", "oversized", "zero progress", "partial error", "joined EOF", "wrapped EOF", "stat error", "nil info", "typed nil info", "directory", "fifo", "close error", "handle and error", "nil and nil", "typed nil handle"} {
		t.Run(name, func(t *testing.T) {
			f := networkData("1")
			var handle networkMetadataFile = f
			var openErr error
			want := errNetworkMetadataRead
			closes := 1
			switch name {
			case "negative":
				f.read = func([]byte) (int, error) { return -1, nil }
			case "oversized":
				f.read = func(p []byte) (int, error) { return len(p) + 1, io.EOF }
			case "zero progress":
				f.read = func([]byte) (int, error) { return 0, nil }
			case "partial error":
				f.read = func(p []byte) (int, error) { return copy(p, "1"), os.ErrPermission }
			case "joined EOF":
				f.read = func([]byte) (int, error) { return 0, errors.Join(io.EOF, os.ErrPermission) }
			case "wrapped EOF":
				f.read = func([]byte) (int, error) { return 0, fmt.Errorf("wrapped: %w", io.EOF) }
			case "stat error":
				f.statErr = os.ErrPermission
			case "nil info":
				f.info = nil
			case "typed nil info":
				var v *networkInfo
				f.info = v
			case "directory":
				f.info = networkInfo{mode: os.ModeDir}
			case "fifo":
				f.info = networkInfo{mode: os.ModeNamedPipe}
			case "close error":
				f.closeErr = os.ErrPermission
				want = errNetworkMetadataClose
			case "handle and error":
				openErr = os.ErrNotExist
			case "nil and nil":
				handle = nil
				closes = 0
			case "typed nil handle":
				var v *networkFile
				handle = v
				closes = 0
			}
			data, err := acquireNetworkMetadata(t.Context(), "owned", 256, true, true, func(string) (networkMetadataFile, error) { return handle, openErr })
			if data != nil || err != want || f.closes != closes {
				t.Fatal("fault downgraded or close count wrong")
			}
		})
	}
}

func TestNetworkMetadataAbsenceAndOfflinePolicy(t *testing.T) {
	for _, test := range []struct {
		name              string
		optional, offline bool
		openErr, readErr  error
		prefix            string
		want              error
	}{
		{"optional missing", true, false, syscall.ENOENT, nil, "", errNetworkMetadataAbsent},
		{"mandatory missing", false, false, syscall.ENOENT, nil, "", errNetworkMetadataRead},
		{"permission", true, true, syscall.EACCES, nil, "", errNetworkMetadataRead},
		{"joined absence", true, true, errors.Join(syscall.ENOENT, syscall.EACCES), nil, "", errNetworkMetadataRead},
		{"wrapped absence", true, false, fmt.Errorf("wrapped: %w", syscall.ENOENT), nil, "", errNetworkMetadataAbsent},
		{"wrong open phase", true, false, &os.PathError{Op: "read", Path: "owned", Err: syscall.ENOENT}, nil, "", errNetworkMetadataRead},
		{"open invalid", true, true, syscall.EINVAL, nil, "", errNetworkMetadataRead},
		{"offline invalid", true, true, nil, syscall.EINVAL, "", errNetworkMetadataAbsent},
		{"ordinary invalid", true, false, nil, syscall.EINVAL, "", errNetworkMetadataRead},
		{"partial invalid", true, true, nil, syscall.EINVAL, "1", errNetworkMetadataRead},
		{"joined invalid", true, true, nil, errors.Join(syscall.EINVAL, syscall.EIO), "", errNetworkMetadataRead},
		{"wrong read phase", true, true, nil, &os.PathError{Op: "open", Path: "owned", Err: syscall.EINVAL}, "", errNetworkMetadataRead},
		{"read absent", true, true, nil, syscall.ENOENT, "", errNetworkMetadataRead},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := networkData("")
			f.read = func(p []byte) (int, error) { return copy(p, test.prefix), test.readErr }
			open := func(string) (networkMetadataFile, error) {
				if test.openErr != nil {
					return nil, test.openErr
				}
				return f, nil
			}
			data, err := acquireNetworkMetadata(t.Context(), "owned", 256, test.optional, test.offline, open)
			closes := 1
			if test.openErr != nil {
				closes = 0
			}
			if err != test.want || data != nil || f.closes != closes {
				t.Fatal("wrong absence policy or ownership")
			}
		})
	}
	f := networkData("earlier")
	f.read = func(p []byte) (int, error) {
		if f.reads == 1 {
			return copy(p, "x"), nil
		}
		return 0, syscall.EINVAL
	}
	if _, err := acquireNetworkMetadata(t.Context(), "owned", 256, true, true, func(string) (networkMetadataFile, error) { return f, nil }); err != errNetworkMetadataRead || f.closes != 1 {
		t.Fatal("prior progress was unavailable")
	}
	f = networkData("")
	f.read = func([]byte) (int, error) { return 0, syscall.EINVAL }
	f.closeErr = os.ErrPermission
	if data, err := acquireNetworkMetadata(t.Context(), "owned", 256, true, true, func(string) (networkMetadataFile, error) { return f, nil }); err != errNetworkMetadataClose || data != nil || f.closes != 1 {
		t.Fatal("unavailability concealed close failure")
	}
}

func TestNetworkMetadataCancellationAndClosePrecedence(t *testing.T) {
	for _, phase := range []string{"before open", "after open", "after stat", "after EOF", "after close", "absence close", "hard read cancel", "close cancel"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := networkData("1")
			opens := 0
			want := context.Canceled
			if phase == "before open" {
				cancel()
			}
			if phase == "after stat" {
				f.statHook = cancel
			}
			if phase == "after EOF" {
				f.read = func(p []byte) (int, error) { cancel(); return copy(p, "1"), io.EOF }
			}
			if phase == "after close" {
				f.closeHook = cancel
			}
			if phase == "absence close" {
				f.read = func([]byte) (int, error) { return 0, syscall.EINVAL }
				f.closeHook = cancel
			}
			if phase == "hard read cancel" {
				f.read = func([]byte) (int, error) { cancel(); return 0, syscall.EIO }
				want = errNetworkMetadataRead
			}
			if phase == "close cancel" {
				f.closeErr = os.ErrPermission
				f.closeHook = cancel
				want = errNetworkMetadataClose
			}
			data, err := acquireNetworkMetadata(ctx, "owned", 256, true, true, func(string) (networkMetadataFile, error) {
				opens++
				if phase == "after open" {
					cancel()
				}
				return f, nil
			})
			closes := 1
			if phase == "before open" {
				closes = 0
			}
			if err != want || data != nil || f.closes != closes || opens != closes {
				t.Fatal("cancellation masked fault or leaked credit/handle")
			}
			if (phase == "before open" || phase == "after open" || phase == "after stat") && f.reads != 0 {
				t.Fatal("later read after cancellation")
			}
		})
	}
}
