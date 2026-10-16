//go:build linux

package workerexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestTimeoutKillsEntireProcessGroup(t *testing.T) {
	runner := newTestRunner(t, time.Second, 1024, 1024)
	started := time.Now()
	result, err := runner.Run(context.Background(), Request{
		Args:        []string{"-test.run=^TestWorkerProcessGroupHelper$"},
		Environment: map[string]string{"WORKEREXEC_TEST_MODE": "group-parent"},
	})
	if !errors.Is(err, context.DeadlineExceeded) || string(result.Stdout) != "group ready\n" || time.Since(started) > 3*time.Second {
		t.Fatalf("result = %#v, elapsed = %v, error = %v", result, time.Since(started), err)
	}
}

func TestCancellationKillsProcessGroupWithHeldStdin(t *testing.T) {
	runner := newTestRunner(t, 5*time.Second, 1024, 1024)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(time.Second, cancel)
	started := time.Now()
	result, err := runner.Run(ctx, Request{
		Args:          []string{"-test.run=^TestWorkerProcessGroupHelper$"},
		Environment:   map[string]string{"WORKEREXEC_TEST_MODE": "group-parent"},
		StdinHoldTime: 4 * time.Second,
	})
	if !errors.Is(err, context.Canceled) || string(result.Stdout) != "group ready\n" || time.Since(started) > 3*time.Second {
		t.Fatalf("result = %#v, elapsed = %v, error = %v", result, time.Since(started), err)
	}
}

func TestWorkerProcessGroupHelper(t *testing.T) {
	switch os.Getenv("WORKEREXEC_TEST_MODE") {
	case "group-parent":
		process, err := os.StartProcess(os.Args[0], []string{os.Args[0], "-test.run=^TestWorkerProcessGroupHelper$"}, &os.ProcAttr{
			Env:   []string{"WORKEREXEC_TEST_MODE=group-child"},
			Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		})
		if err != nil {
			os.Exit(2)
		}
		_ = process.Release()
		fmt.Fprintln(os.Stdout, "group ready")
		time.Sleep(10 * time.Second)
	case "group-child":
		time.Sleep(10 * time.Second)
	default:
		return
	}
	os.Exit(0)
}
