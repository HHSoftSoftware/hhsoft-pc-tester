package workerexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidConfiguration = errors.New("workerexec: invalid configuration")
	ErrInvalidRequest       = errors.New("workerexec: invalid request")
	ErrCleanupUncertain     = errors.New("workerexec: worker cleanup could not be confirmed")
)

const cleanupTimeout = 2 * time.Second

type Options struct {
	Timeout        time.Duration
	MaxStdoutBytes int
	MaxStderrBytes int
}

type Request struct {
	Args          []string
	Environment   map[string]string
	StdinHoldTime time.Duration
}

type Result struct {
	Stdout          []byte
	Stderr          []byte
	ExitCode        int
	StdoutTruncated bool
	StderrTruncated bool
}

type Runner struct {
	path           string
	directory      string
	timeout        time.Duration
	maxStdoutBytes int
	maxStderrBytes int
}

func New(path string, options Options) (*Runner, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("%w: executable path must be clean and absolute", ErrInvalidConfiguration)
	}
	if options.Timeout <= 0 {
		return nil, fmt.Errorf("%w: timeout must be positive", ErrInvalidConfiguration)
	}
	if options.MaxStdoutBytes < 0 || options.MaxStderrBytes < 0 {
		return nil, fmt.Errorf("%w: output limits must not be negative", ErrInvalidConfiguration)
	}
	return &Runner{
		path:           path,
		directory:      filepath.Dir(path),
		timeout:        options.Timeout,
		maxStdoutBytes: options.MaxStdoutBytes,
		maxStderrBytes: options.MaxStderrBytes,
	}, nil
}

func (runner *Runner) Run(ctx context.Context, request Request) (Result, error) {
	if runner == nil {
		return Result{ExitCode: -1}, fmt.Errorf("%w: nil runner", ErrInvalidConfiguration)
	}
	if ctx == nil {
		return Result{ExitCode: -1}, fmt.Errorf("%w: nil context", ErrInvalidRequest)
	}
	if request.StdinHoldTime < 0 || request.StdinHoldTime >= runner.timeout {
		return Result{ExitCode: -1}, fmt.Errorf("%w: stdin hold time must be zero or less than the runner timeout", ErrInvalidRequest)
	}
	environment, err := validatedEnvironment(request.Environment)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	for _, argument := range request.Args {
		if strings.IndexByte(argument, 0) >= 0 {
			return Result{ExitCode: -1}, fmt.Errorf("%w: argument contains NUL", ErrInvalidRequest)
		}
	}

	runContext, cancel := context.WithTimeout(ctx, runner.timeout)
	defer cancel()
	if err := runContext.Err(); err != nil {
		return Result{ExitCode: -1}, fmt.Errorf("workerexec: worker not started: %w", err)
	}

	capture, err := newOutputCapture(runner.maxStdoutBytes, runner.maxStderrBytes)
	if err != nil {
		return Result{ExitCode: -1}, fmt.Errorf("workerexec: create output pipes: %w", err)
	}
	defer capture.close()
	command := exec.Command(runner.path, request.Args...)
	command.Dir = runner.directory
	command.Env = environment
	command.Stdout = capture.stdoutWrite
	command.Stderr = capture.stderrWrite
	closeStdin := func() {}
	var stdinRead, stdinWrite *os.File
	if request.StdinHoldTime > 0 {
		stdinRead, stdinWrite, err = os.Pipe()
		if err != nil {
			return capture.result(-1), fmt.Errorf("workerexec: create stdin pipe: %w", err)
		}
		command.Stdin = stdinRead
	}
	if err := configureProcess(command); err != nil {
		closePipe(stdinRead, stdinWrite)
		return capture.result(-1), fmt.Errorf("workerexec: configure process: %w", err)
	}
	if err := command.Start(); err != nil {
		closePipe(stdinRead, stdinWrite)
		return capture.result(-1), fmt.Errorf("workerexec: start worker: %w", err)
	}
	capture.start()
	if stdinRead != nil {
		_ = stdinRead.Close()
		stdinTimer := time.AfterFunc(request.StdinHoldTime, func() {
			_ = stdinWrite.Close()
		})
		closeStdin = func() {
			stdinTimer.Stop()
			_ = stdinWrite.Close()
		}
		defer closeStdin()
	}

	completed := waitProcess(runContext, command, closeStdin)
	drainErr := capture.finish(completed.cleanupDeadline)
	return capture.result(completed.exitCode), errors.Join(completed.err, drainErr)
}

func closePipe(read, write *os.File) {
	if read != nil {
		_ = read.Close()
	}
	if write != nil {
		_ = write.Close()
	}
}

func validatedEnvironment(values map[string]string) ([]string, error) {
	names := make([]string, 0, len(values))
	for name, value := range values {
		if !validEnvironmentName(name) || strings.IndexByte(value, 0) >= 0 {
			return nil, fmt.Errorf("%w: invalid environment value", ErrInvalidRequest)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	environment := make([]string, 0, len(names))
	for _, name := range names {
		environment = append(environment, name+"="+values[name])
	}
	return environment, nil
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || character == '_' ||
			(index > 0 && character >= '0' && character <= '9') {
			continue
		}
		return false
	}
	return true
}

type boundedBuffer struct {
	mutex     sync.Mutex
	limit     int
	buffer    bytes.Buffer
	truncated bool
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining > len(data) {
		remaining = len(data)
	}
	if remaining > 0 {
		_, _ = buffer.buffer.Write(data[:remaining])
	}
	if remaining < len(data) {
		buffer.truncated = true
	}
	return len(data), nil
}

func (buffer *boundedBuffer) snapshot() ([]byte, bool) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return bytes.Clone(buffer.buffer.Bytes()), buffer.truncated
}
