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
	"time"
)

var (
	ErrInvalidConfiguration = errors.New("workerexec: invalid configuration")
	ErrInvalidRequest       = errors.New("workerexec: invalid request")
)

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

	stdout := newBoundedBuffer(runner.maxStdoutBytes)
	stderr := newBoundedBuffer(runner.maxStderrBytes)
	command := exec.Command(runner.path, request.Args...)
	command.Dir = runner.directory
	command.Env = environment
	command.Stdout = stdout
	command.Stderr = stderr
	closeStdin := func() {}
	var stdinRead, stdinWrite *os.File
	if request.StdinHoldTime > 0 {
		stdinRead, stdinWrite, err = os.Pipe()
		if err != nil {
			return resultFor(command, stdout, stderr), fmt.Errorf("workerexec: create stdin pipe: %w", err)
		}
		command.Stdin = stdinRead
	}
	configureProcess(command)
	if err := command.Start(); err != nil {
		closePipe(stdinRead, stdinWrite)
		return resultFor(command, stdout, stderr), fmt.Errorf("workerexec: start %q: %w", runner.path, err)
	}
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

	wait := make(chan error, 1)
	go func() {
		wait <- command.Wait()
	}()

	select {
	case err := <-wait:
		return completedResult(command, stdout, stderr, err)
	case <-runContext.Done():
		select {
		case err := <-wait:
			return completedResult(command, stdout, stderr, err)
		default:
		}
		closeStdin()
		terminationErr := terminateProcess(command)
		<-wait
		result := resultFor(command, stdout, stderr)
		if terminationErr != nil {
			return result, fmt.Errorf("workerexec: terminate %q: %v: %w", runner.path, terminationErr, runContext.Err())
		}
		return result, fmt.Errorf("workerexec: terminate %q: %w", runner.path, runContext.Err())
	}
}

func closePipe(read, write *os.File) {
	if read != nil {
		_ = read.Close()
	}
	if write != nil {
		_ = write.Close()
	}
}

func completedResult(command *exec.Cmd, stdout, stderr *boundedBuffer, err error) (Result, error) {
	result := resultFor(command, stdout, stderr)
	if err == nil {
		return result, nil
	}
	return result, fmt.Errorf("workerexec: worker exited with code %d: %w", result.ExitCode, err)
}

func resultFor(command *exec.Cmd, stdout, stderr *boundedBuffer) Result {
	exitCode := -1
	if command.ProcessState != nil {
		exitCode = command.ProcessState.ExitCode()
	}
	return Result{
		Stdout:          stdout.bytes(),
		Stderr:          stderr.bytes(),
		ExitCode:        exitCode,
		StdoutTruncated: stdout.truncated,
		StderrTruncated: stderr.truncated,
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
	limit     int
	buffer    bytes.Buffer
	truncated bool
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
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

func (buffer *boundedBuffer) bytes() []byte {
	return bytes.Clone(buffer.buffer.Bytes())
}
