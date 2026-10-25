//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"time"
)

func findingFileReader(ctx context.Context, source *os.File) (func([]byte) (int, error), func() error, error) {
	if err := commandStopReason(ctx); err != nil {
		return nil, nil, err
	}
	if source == nil {
		return nil, nil, errFindingInputUnsupported
	}
	metadata, err := source.Stat()
	if err != nil {
		return nil, nil, err
	}
	if metadata.Mode()&(os.ModeCharDevice|os.ModeNamedPipe|os.ModeSocket) == 0 {
		return nil, nil, errFindingInputUnsupported
	}
	raw, err := source.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	var descriptor, flags int
	var setupErr error
	baseSet := syscall.FdSet{}
	controlErr := raw.Control(func(fd uintptr) {
		descriptor = int(fd)
		if _, setupErr = addDescriptor(&baseSet, descriptor, 0); setupErr != nil {
			return
		}
		value, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
		if errno != 0 {
			setupErr = errno
			return
		}
		flags = int(value)
		if flags&syscall.O_NONBLOCK == 0 {
			_, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_SETFL, uintptr(flags|syscall.O_NONBLOCK))
			if errno != 0 {
				setupErr = errno
			}
		}
	})
	if controlErr != nil {
		return nil, nil, controlErr
	}
	if setupErr != nil {
		return nil, nil, setupErr
	}
	restore := func() error {
		if flags&syscall.O_NONBLOCK != 0 {
			return nil
		}
		var restoreErr error
		controlErr := raw.Control(func(fd uintptr) {
			_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_SETFL, uintptr(flags))
			if errno != 0 {
				restoreErr = errno
			}
		})
		return errors.Join(controlErr, restoreErr)
	}
	read := func(buffer []byte) (int, error) {
		for {
			if err := commandStopReason(ctx); err != nil {
				return 0, err
			}
			remaining := 25 * time.Millisecond
			if deadline, ok := ctx.Deadline(); ok {
				if until := time.Until(deadline); until < remaining {
					remaining = until
				}
			}
			if remaining <= 0 {
				return 0, context.DeadlineExceeded
			}
			readSet := baseSet
			wait := syscall.NsecToTimeval(remaining.Nanoseconds())
			ready, err := syscall.Select(descriptor+1, &readSet, nil, nil, &wait)
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if err != nil {
				return 0, err
			}
			if ready == 0 {
				continue
			}
			if err := commandStopReason(ctx); err != nil {
				return 0, err
			}
			var count int
			var readErr error
			controlErr := raw.Control(func(fd uintptr) { count, readErr = syscall.Read(int(fd), buffer) })
			if controlErr != nil {
				return 0, controlErr
			}
			if errors.Is(readErr, syscall.EINTR) || errors.Is(readErr, syscall.EAGAIN) || errors.Is(readErr, syscall.EWOULDBLOCK) {
				continue
			}
			if count == 0 && readErr == nil {
				readErr = io.EOF
			}
			return count, readErr
		}
	}
	return read, restore, nil
}
