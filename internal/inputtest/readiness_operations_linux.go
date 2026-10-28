//go:build linux

package inputtest

import (
	"context"
	"io"
	"os"
	"strconv"
	"syscall"
	"time"
)

const inputOpenPath = 0x200000

type inputPathFile interface {
	Stat() (os.FileInfo, error)
	Fd() uintptr
	Close() error
}

type inputReadableFile interface {
	io.Reader
	Stat() (os.FileInfo, error)
	Close() error
}

type inputReadinessRoot interface {
	Lstat(string) (os.FileInfo, error)
	OpenPath(string, int) (inputPathFile, error)
	OpenRoot(string) (inputReadinessRoot, error)
	Close() error
}

type inputReadinessOperations struct {
	openRoot     func(string) (inputReadinessRoot, error)
	openReadable func(inputPathFile) (inputReadableFile, error)
	now          func() time.Time
	wait         func(context.Context, time.Duration)
}

type systemInputRoot struct{ *os.Root }
type systemInputReader struct{ *os.File }

func (root systemInputRoot) OpenPath(path string, flags int) (inputPathFile, error) {
	file, err := root.OpenFile(path, flags, 0)
	if file == nil {
		return nil, err
	}
	return file, err
}

func (root systemInputRoot) OpenRoot(path string) (inputReadinessRoot, error) {
	child, err := root.Root.OpenRoot(path)
	if child == nil {
		return nil, err
	}
	return systemInputRoot{child}, err
}

// A direct syscall preserves nonblocking event reads. os.File.Read may poll a
// nonblocking descriptor. Linux syscall errors return -1; normalize only that
// actual errno result, and translate a successful zero-byte read to EOF.
func (file systemInputReader) Read(data []byte) (int, error) {
	n, err := syscall.Read(int(file.Fd()), data)
	if n == -1 && err != nil {
		return 0, err
	}
	if n == 0 && err == nil {
		return 0, io.EOF
	}
	return n, err
}

func systemInputReadinessOperations() inputReadinessOperations {
	return inputReadinessOperations{
		openRoot: func(path string) (inputReadinessRoot, error) {
			root, err := os.OpenRoot(path)
			if root == nil {
				return nil, err
			}
			return systemInputRoot{root}, err
		},
		openReadable: func(path inputPathFile) (inputReadableFile, error) {
			file, err := os.OpenFile("/proc/self/fd/"+strconv.FormatUint(uint64(path.Fd()), 10), os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
			if file == nil {
				return nil, err
			}
			return systemInputReader{file}, err
		},
		now: time.Now,
		wait: func(ctx context.Context, duration time.Duration) {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
		},
	}
}
