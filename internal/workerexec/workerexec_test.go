package workerexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRejectsUnsafeConfiguration(t *testing.T) {
	options := Options{Timeout: time.Second, MaxStdoutBytes: 1, MaxStderrBytes: 1}
	executable := executablePath(t)
	dirtyAbsolutePath := executable + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(executable)
	for _, path := range []string{"", "worker", dirtyAbsolutePath} {
		if _, err := New(path, options); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("New(%q) error = %v", path, err)
		}
	}
	if _, err := New(executablePath(t), Options{}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("zero timeout error = %v", err)
	}
}

func TestExecutesWithOnlyExplicitEnvironment(t *testing.T) {
	t.Setenv("WORKEREXEC_INHERITED_SECRET", "must-not-pass")
	runner := newTestRunner(t, 5*time.Second, 1024, 1024)
	result, err := runner.Run(context.Background(), Request{
		Args: []string{"-test.run=^TestWorkerHelper$", "argument value"},
		Environment: map[string]string{
			"WORKEREXEC_TEST_MODE": "environment",
			"VISIBLE":              "explicit value",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || string(result.Stderr) != "" ||
		string(result.Stdout) != "visible=\"explicit value\" inherited=\"\" home=\"\" arg=\"argument value\"\n" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestBoundsCapturedOutput(t *testing.T) {
	runner := newTestRunner(t, 5*time.Second, 4, 3)
	result, err := runner.Run(context.Background(), helperRequest("output"))
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Stdout) != "ABCD" || string(result.Stderr) != "012" || !result.StdoutTruncated || !result.StderrTruncated {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestReturnsExitErrorAndCapturedOutput(t *testing.T) {
	runner := newTestRunner(t, 5*time.Second, 1024, 1024)
	result, err := runner.Run(context.Background(), helperRequest("failure"))
	if err == nil || result.ExitCode != 17 || string(result.Stderr) != "failure detail\n" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestEnforcesTimeout(t *testing.T) {
	runner := newTestRunner(t, 200*time.Millisecond, 1024, 1024)
	started := time.Now()
	result, err := runner.Run(context.Background(), helperRequest("sleep"))
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second || result.ExitCode == 0 {
		t.Fatalf("result = %#v, elapsed = %v, error = %v", result, time.Since(started), err)
	}
}

func TestHonorsCancellation(t *testing.T) {
	runner := newTestRunner(t, 5*time.Second, 1024, 1024)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	result, err := runner.Run(ctx, helperRequest("sleep"))
	if !errors.Is(err, context.Canceled) || result.ExitCode == 0 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestSuppliesEmptyStdinUntilHoldTimeExpires(t *testing.T) {
	runner := newTestRunner(t, 3*time.Second, 1024, 1024)
	request := helperRequest("stdin")
	request.StdinHoldTime = 300 * time.Millisecond
	started := time.Now()
	result, err := runner.Run(context.Background(), request)
	if err != nil || string(result.Stdout) != "stdin bytes=0\n" || time.Since(started) < 250*time.Millisecond || time.Since(started) > 2*time.Second {
		t.Fatalf("result = %#v, elapsed = %v, error = %v", result, time.Since(started), err)
	}
}

func TestCancellationClosesHeldStdinWithoutDeadlock(t *testing.T) {
	runner := newTestRunner(t, 5*time.Second, 1024, 1024)
	request := helperRequest("stdin")
	request.StdinHoldTime = 4 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	started := time.Now()
	_, err := runner.Run(ctx, request)
	if !errors.Is(err, context.Canceled) || time.Since(started) > 2*time.Second {
		t.Fatalf("elapsed = %v, error = %v", time.Since(started), err)
	}
}

func TestRejectsInvalidRequestBeforeExecution(t *testing.T) {
	runner := newTestRunner(t, time.Second, 1024, 1024)
	for _, request := range []Request{
		{Args: []string{"invalid\x00argument"}},
		{Environment: map[string]string{"BAD-NAME": "value"}},
		{Environment: map[string]string{"NAME": "invalid\x00value"}},
		{StdinHoldTime: -time.Second},
		{StdinHoldTime: time.Second},
	} {
		if _, err := runner.Run(context.Background(), request); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("request %#v error = %v", request, err)
		}
	}
}

func TestWorkerHelper(t *testing.T) {
	if os.Getenv("WORKEREXEC_TEST_MODE") == "" {
		return
	}
	switch os.Getenv("WORKEREXEC_TEST_MODE") {
	case "environment":
		fmt.Printf("visible=%q inherited=%q home=%q arg=%q\n", os.Getenv("VISIBLE"), os.Getenv("WORKEREXEC_INHERITED_SECRET"), os.Getenv("HOME"), os.Args[len(os.Args)-1])
	case "output":
		fmt.Fprint(os.Stdout, "ABCDEFG")
		fmt.Fprint(os.Stderr, "012345")
	case "failure":
		fmt.Fprintln(os.Stderr, "failure detail")
		os.Exit(17)
	case "sleep":
		time.Sleep(10 * time.Second)
	case "stdin":
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "stdin: %v\n", err)
			os.Exit(3)
		}
		fmt.Fprintf(os.Stdout, "stdin bytes=%d\n", len(input))
	default:
		fmt.Fprintln(os.Stderr, "unknown worker test mode")
		os.Exit(2)
	}
	os.Exit(0)
}

func newTestRunner(t *testing.T, timeout time.Duration, stdoutLimit, stderrLimit int) *Runner {
	t.Helper()
	runner, err := New(executablePath(t), Options{Timeout: timeout, MaxStdoutBytes: stdoutLimit, MaxStderrBytes: stderrLimit})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func executablePath(t *testing.T) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func helperRequest(mode string) Request {
	return Request{
		Args:        []string{"-test.run=^TestWorkerHelper$"},
		Environment: map[string]string{"WORKEREXEC_TEST_MODE": mode},
	}
}

func TestEnvironmentOrderingIsDeterministic(t *testing.T) {
	environment, err := validatedEnvironment(map[string]string{"ZED": "last", "ALPHA": "first"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(environment, ",") != "ALPHA=first,ZED=last" {
		t.Fatalf("environment = %q", environment)
	}
}
