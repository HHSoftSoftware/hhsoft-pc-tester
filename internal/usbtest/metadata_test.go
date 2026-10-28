package usbtest

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type usbInfo struct {
	mode os.FileMode
	sys  any
	size int64
}

func (usbInfo) Name() string           { return "owned" }
func (info usbInfo) Size() int64       { return info.size }
func (info usbInfo) Mode() os.FileMode { return info.mode }
func (usbInfo) ModTime() time.Time     { return time.Time{} }
func (info usbInfo) IsDir() bool       { return info.mode.IsDir() }
func (info usbInfo) Sys() any          { return info.sys }

type usbMetadataFixture struct {
	reader                                   io.Reader
	info                                     os.FileInfo
	statErr, closeErr                        error
	read                                     func([]byte) (int, error)
	statHook, closeHook                      func()
	reads, closes, maximumRequest, bytesRead int
}

func (file *usbMetadataFixture) Stat() (os.FileInfo, error) {
	if file.statHook != nil {
		file.statHook()
	}
	return file.info, file.statErr
}

func (file *usbMetadataFixture) Read(p []byte) (int, error) {
	file.reads++
	file.maximumRequest = max(file.maximumRequest, len(p))
	var n int
	var err error
	if file.read != nil {
		n, err = file.read(p)
	} else {
		n, err = file.reader.Read(p)
	}
	if n > 0 {
		file.bytesRead += n
	}
	return n, err
}

func (file *usbMetadataFixture) Close() error {
	file.closes++
	if file.closeHook != nil {
		file.closeHook()
	}
	return file.closeErr
}

func usbMetadata(value string) *usbMetadataFixture {
	return &usbMetadataFixture{reader: strings.NewReader(value), info: usbInfo{}}
}

func TestUSBMetadataUsesFiniteReadAndExactEOF(t *testing.T) {
	for _, size := range []int{0, 1, 255, 256, 257, 4096} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			file := usbMetadata(strings.Repeat("x", size))
			data, err := readUSBMetadata(t.Context(), "owned", func(string) (usbMetadataFile, error) { return file, nil })
			if file.closes != 1 || file.maximumRequest > 32 || file.bytesRead > 257 {
				t.Fatal("reader exceeded ownership or acquisition bounds")
			}
			if size <= 256 {
				if err != nil || string(data) != strings.Repeat("x", size) {
					t.Fatal("exact bounded value rejected")
				}
			} else if err != errUSBMetadataRead || data != nil {
				t.Fatal("overflow received usable bytes")
			}
		})
	}
	file := usbMetadata("")
	file.info = usbInfo{size: 1 << 40}
	file.read = func(p []byte) (int, error) { return copy(p, "189:0\n"), io.EOF }
	data, err := readUSBMetadata(t.Context(), "owned", func(string) (usbMetadataFile, error) { return file, nil })
	if err != nil || string(data) != "189:0\n" || file.reads != 1 || file.closes != 1 {
		t.Fatal("final exact EOF bytes or size-independent completion rejected")
	}
}

func TestUSBMetadataRejectsProtocolsAndUnprovableTypes(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*usbMetadataFixture)
	}{
		{"negative count", func(f *usbMetadataFixture) { f.read = func([]byte) (int, error) { return -1, nil } }},
		{"oversized count", func(f *usbMetadataFixture) { f.read = func(p []byte) (int, error) { return len(p) + 1, io.EOF } }},
		{"no progress", func(f *usbMetadataFixture) { f.read = func([]byte) (int, error) { return 0, nil } }},
		{"partial errno", func(f *usbMetadataFixture) {
			f.read = func(p []byte) (int, error) { p[0] = '1'; return 1, os.ErrPermission }
		}},
		{"joined EOF", func(f *usbMetadataFixture) {
			f.read = func([]byte) (int, error) { return 0, errors.Join(io.EOF, os.ErrPermission) }
		}},
		{"wrapped EOF", func(f *usbMetadataFixture) { f.read = func([]byte) (int, error) { return 0, errors.Join(io.EOF) } }},
		{"stat error", func(f *usbMetadataFixture) { f.statErr = os.ErrPermission }},
		{"nil info", func(f *usbMetadataFixture) { f.info = nil }},
		{"typed nil info", func(f *usbMetadataFixture) { var info *usbInfo; f.info = info }},
		{"directory", func(f *usbMetadataFixture) { f.info = usbInfo{mode: os.ModeDir} }},
		{"character", func(f *usbMetadataFixture) { f.info = usbInfo{mode: os.ModeDevice | os.ModeCharDevice} }},
		{"symlink", func(f *usbMetadataFixture) { f.info = usbInfo{mode: os.ModeSymlink} }},
		{"fifo", func(f *usbMetadataFixture) { f.info = usbInfo{mode: os.ModeNamedPipe} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := usbMetadata("189:0\n")
			test.change(file)
			data, err := readUSBMetadata(t.Context(), "owned", func(string) (usbMetadataFile, error) { return file, nil })
			if err != errUSBMetadataRead || data != nil || file.closes != 1 {
				t.Fatal("invalid metadata or ownership accepted")
			}
		})
	}
}

func TestUSBMetadataCloseOwnsEveryAcquiredHandle(t *testing.T) {
	for _, stage := range []string{"success", "open error", "stat error", "read error", "cancel", "cancel and invalid read", "overflow and cancel"} {
		for _, badClose := range []bool{false, true} {
			t.Run(stage+map[bool]string{false: "/clean", true: "/close error"}[badClose], func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				file := usbMetadata("189:0\n")
				var openErr error
				switch stage {
				case "open error":
					openErr = os.ErrPermission
				case "stat error":
					file.statErr = os.ErrPermission
				case "read error":
					file.read = func([]byte) (int, error) { return 0, os.ErrPermission }
				case "cancel":
					file.closeHook = cancel
				case "cancel and invalid read":
					file.read = func(p []byte) (int, error) { cancel(); return len(p) + 1, io.EOF }
				case "overflow and cancel":
					file.reader = strings.NewReader(strings.Repeat("x", 257))
					file.closeHook = cancel
				}
				if badClose {
					file.closeErr = os.ErrPermission
				}
				data, err := readUSBMetadata(ctx, "owned", func(string) (usbMetadataFile, error) { return file, openErr })
				if file.closes != 1 {
					t.Fatal("owned handle not closed exactly once")
				}
				if badClose {
					if err != errUSBMetadataClose || data != nil {
						t.Fatal("closure failure lost precedence")
					}
				} else if stage == "success" {
					if err != nil || string(data) != "189:0\n" {
						t.Fatal("valid result lost")
					}
				} else if stage == "cancel" {
					if err != context.Canceled || data != nil {
						t.Fatal("post-close cancellation lost")
					}
				} else if err != errUSBMetadataRead || data != nil {
					t.Fatal("hard acquisition failure was downgraded")
				}
			})
		}
	}
}

type usbNilContext struct{ context.Context }

func TestUSBMetadataRejectsNilDependenciesAndStopsBeforeOpen(t *testing.T) {
	var missing *usbMetadataFixture
	for _, file := range []usbMetadataFile{nil, missing} {
		if data, err := readUSBMetadata(t.Context(), "owned", func(string) (usbMetadataFile, error) { return file, nil }); data != nil || err != errUSBMetadataRead {
			t.Fatal("nil file accepted")
		}
	}
	var typedNil *usbNilContext
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, typedNil, ctx} {
		if data, err := readUSBMetadata(ctx, "owned", func(string) (usbMetadataFile, error) { t.Fatal("invalid context opened metadata"); return nil, nil }); data != nil || err == nil {
			t.Fatal("invalid context accepted")
		}
	}
	if _, err := readUSBMetadata(t.Context(), "owned", nil); err != errUSBMetadataRead {
		t.Fatal("nil opener accepted")
	}
}
