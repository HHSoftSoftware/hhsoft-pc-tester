//go:build linux

package workerexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCompletedLeaderLeavesNoLiveGroupDescendant(t *testing.T) {
	for _, mode := range []string{"closed-success", "inherited-success", "closed-failure", "inherited-failure"} {
		t.Run(mode, func(t *testing.T) { runDescendantCase(t, mode, "complete") })
	}
}

func TestCancellationAndDeadlineLeaveNoLiveGroupDescendant(t *testing.T) {
	for _, ending := range []string{"cancel", "deadline", "exit-cancel-race"} {
		t.Run(ending, func(t *testing.T) { runDescendantCase(t, "inherited-success", ending) })
	}
}

type descendantIdentity struct {
	pid, group int
	start      string
}

func runDescendantCase(t *testing.T, mode, ending string) {
	t.Helper()
	directory := t.TempDir()
	var child descendantIdentity
	t.Cleanup(func() {
		// A failed assertion stops only these cooperating test helpers. No PID
		// from a file is ever signaled, even if it has since been reused.
		_ = os.WriteFile(filepath.Join(directory, "stop"), nil, 0600)
		_ = os.WriteFile(filepath.Join(directory, "release"), nil, 0600)
		deadline := time.Now().Add(time.Second)
		for child.pid > 0 && testDescendantAlive(t, child) && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if child.pid > 0 && testDescendantAlive(t, child) {
			t.Error("owned test helper did not honor its stop file")
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if ending == "deadline" {
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithTimeout(ctx, 1500*time.Millisecond)
		defer deadlineCancel()
	}
	runner := newTestRunner(t, 5*time.Second, 16, 12)
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	started := time.Now()
	go func() {
		result, err := runner.Run(ctx, Request{
			Args:        []string{"-test.run=^TestWorkerDescendantHelper$"},
			Environment: map[string]string{"WORKEREXEC_DESCENDANT": "parent", "WORKEREXEC_FIXTURE": directory, "WORKEREXEC_DESCENDANT_MODE": mode},
		})
		done <- outcome{result, err}
	}()
	ready := waitTestFile(t, filepath.Join(directory, "parent-ready"))
	leader, err := strconv.Atoi(string(ready))
	if err != nil || leader <= 1 {
		t.Fatal("invalid synthetic parent identity")
	}
	metadata := waitTestFile(t, filepath.Join(directory, "child-ready"))
	if _, err := fmt.Sscanf(string(metadata), "%d %d %s", &child.pid, &child.group, &child.start); err != nil || child.pid <= 1 || child.group != leader || child.start == "" {
		t.Fatal("invalid synthetic child identity or group")
	}
	if !testDescendantAlive(t, child) {
		t.Fatal("descendant fixture exited before the worker terminal event")
	}
	if ending == "complete" || ending == "exit-cancel-race" {
		if err := os.WriteFile(filepath.Join(directory, "release"), nil, 0600); err != nil {
			t.Fatal("could not release owned test parent")
		}
	}
	if ending == "cancel" || ending == "exit-cancel-race" {
		cancel()
	}
	var completed outcome
	select {
	case completed = <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("worker return exceeded its test deadline")
	}
	if errors.Is(completed.err, ErrCleanupUncertain) {
		t.Fatalf("cleanup was not confirmed: %v", completed.err)
	}
	switch ending {
	case "cancel":
		if !errors.Is(completed.err, context.Canceled) {
			t.Fatalf("cancel reason = %v", completed.err)
		}
	case "deadline":
		if !errors.Is(completed.err, context.DeadlineExceeded) {
			t.Fatalf("deadline reason = %v", completed.err)
		}
	case "exit-cancel-race":
		if completed.err != nil && !errors.Is(completed.err, context.Canceled) {
			t.Fatalf("race result = %v", completed.err)
		}
	default:
		if strings.HasSuffix(mode, "failure") {
			if completed.result.ExitCode != 17 || completed.err == nil {
				t.Fatalf("failed worker result = %#v, error = %v", completed.result, completed.err)
			}
		} else if completed.result.ExitCode != 0 || completed.err != nil {
			t.Fatalf("completed worker result = %#v, error = %v", completed.result, completed.err)
		}
	}
	if testDescendantAlive(t, child) {
		t.Fatal("worker returned with a live same-group descendant")
	}
	if string(completed.result.Stdout) != strings.Repeat("O", 16) || string(completed.result.Stderr) != strings.Repeat("E", 12) ||
		!completed.result.StdoutTruncated || !completed.result.StderrTruncated {
		t.Fatalf("bounded evidence was not retained: %#v", completed.result)
	}
	if ending == "complete" && time.Since(started) > 3*time.Second {
		t.Fatal("completed leader waited for the runner timeout or descendant self-exit")
	}
}

func waitTestFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if content, err := os.ReadFile(path); err == nil {
			return content
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("owned test helper did not become ready")
	return nil
}

func testDescendantAlive(t *testing.T, child descendantIdentity) bool {
	t.Helper()
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(child.pid) + "/stat")
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return false
	}
	if err != nil {
		t.Fatal("could not observe owned test child")
	}
	// This independent check uses the immutable start tick as well as PID;
	// unrelated later processes are neither mistaken for helpers nor signaled.
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		t.Fatal("invalid owned test child observation")
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 {
		t.Fatal("incomplete owned test child observation")
	}
	return fields[19] == child.start && fields[0] != "Z" && fields[0] != "X"
}

func TestWorkerDescendantHelper(t *testing.T) {
	role := os.Getenv("WORKEREXEC_DESCENDANT")
	if role == "" {
		return
	}
	directory, mode := os.Getenv("WORKEREXEC_FIXTURE"), os.Getenv("WORKEREXEC_DESCENDANT_MODE")
	if directory == "" {
		os.Exit(2)
	}
	if role == "parent" {
		files := []*os.File{os.Stdin, os.Stdout, os.Stderr}
		if strings.HasPrefix(mode, "closed-") {
			null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
			if err != nil {
				os.Exit(2)
			}
			defer null.Close()
			files = []*os.File{null, null, null}
		}
		process, err := os.StartProcess(os.Args[0], []string{os.Args[0], "-test.run=^TestWorkerDescendantHelper$"}, &os.ProcAttr{
			Env: []string{"WORKEREXEC_DESCENDANT=child", "WORKEREXEC_FIXTURE=" + directory}, Files: files,
		})
		if err != nil {
			os.Exit(2)
		}
		_ = process.Release()
		if !helperWaitFile(directory, "child-ready") {
			os.Exit(2)
		}
		fmt.Fprint(os.Stdout, strings.Repeat("O", 128))
		fmt.Fprint(os.Stderr, strings.Repeat("E", 128))
		if !helperWriteReady(directory, "parent-ready", strconv.Itoa(os.Getpid())) {
			os.Exit(2)
		}
		if !helperWaitFile(directory, "release") {
			os.Exit(2)
		}
		if strings.HasSuffix(mode, "failure") {
			os.Exit(17)
		}
	} else if role == "child" {
		raw, err := os.ReadFile("/proc/self/stat")
		if err != nil {
			os.Exit(2)
		}
		end := strings.LastIndexByte(string(raw), ')')
		fields := strings.Fields(string(raw[end+1:]))
		group, err := syscall.Getpgid(0)
		if err != nil || end < 0 || len(fields) < 20 {
			os.Exit(2)
		}
		if !helperWriteReady(directory, "child-ready", fmt.Sprintf("%d %d %s", os.Getpid(), group, fields[19])) {
			os.Exit(2)
		}
		_ = helperWaitFile(directory, "stop")
	} else {
		os.Exit(2)
	}
	os.Exit(0)
}

func helperWriteReady(directory, name, content string) bool {
	path := filepath.Join(directory, name)
	if os.WriteFile(path+".tmp", []byte(content), 0600) != nil {
		return false
	}
	return os.Rename(path+".tmp", path) == nil
}

func helperWaitFile(directory, name string) bool {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(directory, name)); err == nil {
			return true
		}
		if _, err := os.Stat(filepath.Join(directory, "stop")); err == nil {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}
