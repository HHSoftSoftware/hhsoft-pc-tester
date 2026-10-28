//go:build linux

package audio

import (
	"io"
	"os"
	"strconv"
	"syscall"
)

type audioPathFile interface {
	Stat() (os.FileInfo, error)
	Fd() uintptr
	Close() error
}

type audioReadableFile interface {
	io.Reader
	Stat() (os.FileInfo, error)
	Close() error
}

type audioRoot interface {
	Stat(string) (os.FileInfo, error)
	Lstat(string) (os.FileInfo, error)
	OpenRegistry(string, int) (audioDirectory, error)
	OpenPath(string, int) (audioPathFile, error)
	Close() error
}

type audioOperations struct {
	openRoot     func(string) (audioRoot, error)
	openReadable func(audioPathFile) (audioReadableFile, error)
}

type ownedAudioRoot struct{ *os.Root }

func (root ownedAudioRoot) OpenRegistry(path string, flags int) (audioDirectory, error) {
	return root.OpenFile(path, flags, 0)
}

func (root ownedAudioRoot) OpenPath(path string, flags int) (audioPathFile, error) {
	return root.OpenFile(path, flags, 0)
}

func systemAudioOperations() audioOperations {
	return audioOperations{
		openRoot: func(path string) (audioRoot, error) {
			root, err := os.OpenRoot(path)
			if root == nil {
				return nil, err
			}
			return ownedAudioRoot{root}, err
		},
		openReadable: func(handle audioPathFile) (audioReadableFile, error) {
			return os.OpenFile("/proc/self/fd/"+strconv.FormatUint(uint64(handle.Fd()), 10),
				os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		},
	}
}
