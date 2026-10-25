//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

type sessionLineResult struct {
	line string
	err  error
}
type sessionLineRequest struct {
	ctx    context.Context
	limit  int
	text   []byte
	result chan sessionLineResult
}

type sessionConsole struct {
	ctx        context.Context
	cancel     context.CancelFunc
	raw        syscall.RawConn
	flags      uintptr
	terminal   *syscall.Termios
	mutex      sync.Mutex
	request    *sessionLineRequest
	failure    error
	stopped    bool
	stop, done chan struct{}
	stopOnce   sync.Once
}

func prepareSessionConsole(ctx context.Context, input io.Reader) (context.Context, io.Reader, func() error, error) {
	file, ok := input.(*os.File)
	if !ok {
		return ctx, input, func() error { return nil }, errSessionInput
	}
	metadata, err := file.Stat()
	if err != nil || metadata.Mode()&(os.ModeCharDevice|os.ModeNamedPipe|os.ModeSocket) == 0 {
		return ctx, input, func() error { return nil }, errSessionInput
	}
	raw, err := file.SyscallConn()
	if err != nil {
		return ctx, input, func() error { return nil }, errSessionInput
	}
	owned, cancel := context.WithCancel(ctx)
	console := &sessionConsole{ctx: owned, cancel: cancel, raw: raw, stop: make(chan struct{}), done: make(chan struct{})}
	var setupErr error
	err = raw.Control(func(fd uintptr) {
		flags, _, callErr := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
		if callErr != 0 {
			setupErr = callErr
			return
		}
		console.flags = flags
		var original syscall.Termios
		termErr := ioctlTermios(fd, syscall.TCGETS, &original)
		if termErr != nil && !errors.Is(termErr, syscall.ENOTTY) {
			setupErr = termErr
			return
		}
		if termErr == nil {
			configured := original
			configured.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.ISIG
			configured.Cc[syscall.VMIN], configured.Cc[syscall.VTIME] = 1, 0
			if setupErr = ioctlTermios(fd, syscall.TCSETS, &configured); setupErr != nil {
				return
			}
			console.terminal = &original
		}
		_, _, callErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_SETFL, flags|syscall.O_NONBLOCK)
		if callErr != 0 {
			setupErr = callErr
			if console.terminal != nil {
				setupErr = errors.Join(setupErr, ioctlTermios(fd, syscall.TCSETS, console.terminal))
			}
		}
	})
	if err != nil || setupErr != nil {
		cancel()
		return ctx, input, func() error { return nil }, errSessionInput
	}
	go console.listen()
	return owned, console, console.finish, nil
}

// Direct reads would create a second input owner. Display uses the explicit
// context-aware line method. Ordinary input outside prompts is discarded.
func (console *sessionConsole) Read([]byte) (int, error) { return 0, errSessionInput }

func (console *sessionConsole) ReadFindingLine(ctx context.Context, limit int) (string, error) {
	if limit < 1 || limit > 4096 {
		return "", errSessionInput
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	request := &sessionLineRequest{ctx: ctx, limit: limit, result: make(chan sessionLineResult, 1)}
	console.mutex.Lock()
	if console.stopped || console.failure != nil || console.request != nil || console.ctx.Err() != nil {
		console.mutex.Unlock()
		return "", errSessionInput
	}
	console.request = request
	console.mutex.Unlock()
	select {
	case result := <-request.result:
		return result.line, result.err
	case <-ctx.Done():
		console.mutex.Lock()
		if console.request == request {
			console.request = nil
		}
		console.mutex.Unlock()
		// Preserve only a whole line received before cancellation, never partial
		// input or a line completed by input arriving after the request ended.
		select {
		case result := <-request.result:
			return result.line, ctx.Err()
		default:
			return "", ctx.Err()
		}
	}
}

func (console *sessionConsole) respond(result sessionLineResult) {
	if console.request != nil {
		console.request.result <- result
		console.request = nil
	}
}

func (console *sessionConsole) listen() {
	defer close(console.done)
	defer console.restore()
	for {
		select {
		case <-console.stop:
			return
		case <-console.ctx.Done():
			return
		default:
		}
		var data [1]byte
		count := 0
		var readErr error
		console.mutex.Lock()
		err := console.raw.Control(func(fd uintptr) { count, readErr = syscall.Read(int(fd), data[:]) })
		if err != nil || readErr != nil && !errors.Is(readErr, syscall.EAGAIN) && !errors.Is(readErr, syscall.EWOULDBLOCK) && !errors.Is(readErr, syscall.EINTR) {
			console.mutex.Unlock()
			console.fail()
			return
		}
		if count > 0 {
			if data[0] == 3 {
				console.mutex.Unlock()
				console.cancel()
				return
			}
			request := console.request
			if request != nil {
				switch {
				case request.ctx.Err() != nil:
					console.respond(sessionLineResult{err: request.ctx.Err()})
				case data[0] == '\n' || data[0] == '\r':
					console.respond(sessionLineResult{line: string(request.text)})
				case len(request.text) == request.limit:
					console.respond(sessionLineResult{err: errSessionInput})
				default:
					request.text = append(request.text, data[0])
				}
			}
			console.mutex.Unlock()
			continue
		}
		console.mutex.Unlock()
		if readErr == nil && count == 0 {
			console.fail()
			return
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-console.stop:
		case <-console.ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (console *sessionConsole) fail() {
	console.mutex.Lock()
	console.failure = errSessionInput
	console.mutex.Unlock()
	console.cancel()
}

func (console *sessionConsole) restore() {
	var restoreErr error
	err := console.raw.Control(func(fd uintptr) {
		_, _, callErr := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_SETFL, console.flags)
		if callErr != 0 {
			restoreErr = callErr
		}
		if console.terminal != nil {
			restoreErr = errors.Join(restoreErr, ioctlTermios(fd, syscall.TCSETS, console.terminal))
		}
	})
	console.mutex.Lock()
	defer console.mutex.Unlock()
	if err != nil || restoreErr != nil {
		console.failure = errSessionInput
	}
	console.stopped = true
	console.respond(sessionLineResult{err: errors.Join(console.ctx.Err(), errSessionInput)})
}

func (console *sessionConsole) finish() error {
	console.stopOnce.Do(func() { close(console.stop) })
	select {
	case <-console.done:
		console.mutex.Lock()
		defer console.mutex.Unlock()
		return console.failure
	case <-time.After(2 * time.Second):
		return errSessionInput
	}
}
