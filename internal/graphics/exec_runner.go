package graphics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/workerexec"
)

type ExecRunner struct {
	workingDirectory string
	timeout          time.Duration
	outputLimit      int
}

func NewExecRunner(workingDirectory string, timeout time.Duration, outputLimit int) *ExecRunner {
	return &ExecRunner{
		workingDirectory: workingDirectory,
		timeout:          timeout,
		outputLimit:      outputLimit,
	}
}

func (runner *ExecRunner) Run(ctx context.Context, name string, arguments ...string) (string, error) {
	return runner.run(ctx, 0, name, arguments...)
}

func (runner *ExecRunner) RunInteractive(ctx context.Context, displayTime time.Duration, name string, arguments ...string) (string, error) {
	if displayTime <= 0 || displayTime >= runner.timeout {
		return "", errors.New("invalid interactive graphics duration")
	}
	return runner.run(ctx, displayTime, name, arguments...)
}

func (runner *ExecRunner) run(ctx context.Context, inputHold time.Duration, name string, arguments ...string) (string, error) {
	if runner == nil || runner.timeout <= 0 || runner.outputLimit <= 0 {
		return "", errors.New("invalid graphics command runner")
	}
	if err := os.MkdirAll(runner.workingDirectory, 0o700); err != nil {
		return "", err
	}
	stdoutLimit := runner.outputLimit / 2
	stderrLimit := runner.outputLimit - stdoutLimit
	worker, err := workerexec.New(name, workerexec.Options{Timeout: runner.timeout, MaxStdoutBytes: stdoutLimit, MaxStderrBytes: stderrLimit})
	if err != nil {
		return "", err
	}
	result, err := worker.Run(ctx, workerexec.Request{
		Args:          arguments,
		Environment:   map[string]string{"HOME": runner.workingDirectory, "PATH": "/usr/bin:/bin", "XDG_RUNTIME_DIR": runner.workingDirectory},
		StdinHoldTime: inputHold,
	})
	return commandResult(result, err)
}

func commandResult(result workerexec.Result, err error) (string, error) {
	if result.StdoutTruncated || result.StderrTruncated {
		err = errors.Join(err, errors.New("graphics tool output was truncated"))
	}
	parts := make([]string, 0, 2)
	if stdout := strings.TrimSpace(string(result.Stdout)); stdout != "" {
		parts = append(parts, stdout)
	}
	if stderr := strings.TrimSpace(string(result.Stderr)); stderr != "" {
		parts = append(parts, stderr)
	}
	detail := strings.Join(parts, "\n")
	if err != nil {
		if detail == "" {
			return "", err
		}
		return detail, fmt.Errorf("%w: %s", err, detail)
	}
	return detail, nil
}
