//go:build linux

package workerexec

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNonReapingObservationPreservesOwnedChild(t *testing.T) {
	command := exec.Command(executablePath(t), "-test.run=^TestWorkerHelper$")
	command.Env = []string{"WORKEREXEC_TEST_MODE=sleep"}
	if err := command.Start(); err != nil {
		t.Fatal("could not start owned observation helper")
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	pid := command.Process.Pid
	if ready, err := observeChild(pid); ready || err != nil {
		t.Fatalf("running child ready = %v, error = %v", ready, err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal("could not stop owned observation helper")
	}
	deadline := time.Now().Add(time.Second)
	for {
		ready, err := observeChild(pid)
		if err != nil {
			t.Fatal(err)
		}
		if ready {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatal("owned observation helper did not exit")
		}
		time.Sleep(time.Millisecond)
	}
	if ready, err := observeChild(pid); !ready || err != nil {
		t.Fatalf("repeated observation reaped child: ready = %v, error = %v", ready, err)
	}
	if _, err := syscall.Getpgid(pid); err != nil {
		t.Fatal("unreaped child identity was not retained")
	}
	_ = command.Wait()
	if _, err := observeChild(pid); !errors.Is(err, syscall.ECHILD) {
		t.Fatalf("reaped child observation error = %v", err)
	}
}

func TestGroupSignalsFinishBeforeLeaderReaping(t *testing.T) {
	var sequence []string
	reaping := false
	operations := groupOperations{
		observe: func(int) (bool, error) { sequence = append(sequence, "observe"); return true, nil },
		groupID: func(pid int) (int, error) { sequence = append(sequence, "group"); return pid, nil },
		kill: func(int) error {
			if reaping {
				t.Fatal("numeric group signal after reaper started")
			}
			sequence = append(sequence, "kill")
			return nil
		},
		live: func(int, time.Time) (bool, error) { sequence = append(sequence, "members"); return false, nil },
	}
	completion := waitOwnedGroup(context.Background(), 123, func() { sequence = append(sequence, "stdin") }, operations,
		time.Second, func() <-chan processExit {
			reaping = true
			sequence = append(sequence, "reap")
			done := make(chan processExit, 1)
			done <- processExit{exitCode: 0}
			return done
		})
	want := []string{"observe", "stdin", "observe", "group", "kill", "observe", "members", "reap"}
	if completion.err != nil || !reflect.DeepEqual(sequence, want) {
		t.Fatalf("sequence = %v, error = %v", sequence, completion.err)
	}
}

func TestGroupCleanupRefusesUncertainIdentityAndTermination(t *testing.T) {
	for _, mode := range []string{"initial-ownership", "signal-ownership", "changed-group", "signal-failure", "member-failure", "never-terminates"} {
		t.Run(mode, func(t *testing.T) {
			observations, signals, reaps := 0, 0, 0
			operations := groupOperations{
				observe: func(int) (bool, error) {
					observations++
					if mode == "initial-ownership" || mode == "signal-ownership" && observations == 2 {
						return false, syscall.ECHILD
					}
					return true, nil
				},
				groupID: func(pid int) (int, error) {
					if mode == "changed-group" {
						return pid + 1, nil
					}
					return pid, nil
				},
				kill: func(int) error {
					if reaps != 0 {
						t.Fatal("signal after reaping")
					}
					signals++
					if mode == "signal-failure" {
						return syscall.EPERM
					}
					return nil
				},
				live: func(int, time.Time) (bool, error) {
					if mode == "member-failure" {
						return false, errors.New("synthetic member observation failure")
					}
					return mode == "never-terminates", nil
				},
			}
			started := time.Now()
			completion := waitOwnedGroup(context.Background(), 123, func() {}, operations, 20*time.Millisecond,
				func() <-chan processExit {
					reaps++
					done := make(chan processExit, 1)
					done <- processExit{exitCode: 0}
					return done
				})
			if !errors.Is(completion.err, ErrCleanupUncertain) || reaps != 1 || time.Since(started) > time.Second {
				t.Fatalf("completion = %#v, reaps = %d, elapsed = %v", completion, reaps, time.Since(started))
			}
			if (strings.Contains(mode, "ownership") || mode == "changed-group") && signals != 0 {
				t.Fatalf("unowned group received %d signals", signals)
			}
		})
	}
}

func TestCanceledGroupRetainsReasonAndCleansBeforeReaping(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	signals := 0
	operations := groupOperations{
		observe: func(int) (bool, error) { return signals > 0, nil },
		groupID: func(pid int) (int, error) { return pid, nil },
		kill:    func(int) error { signals++; return nil },
		live:    func(int, time.Time) (bool, error) { return false, nil },
	}
	completion := waitOwnedGroup(ctx, 123, func() {}, operations, time.Second,
		func() <-chan processExit {
			if signals != 1 {
				t.Fatalf("reaper started after %d signals", signals)
			}
			done := make(chan processExit, 1)
			done <- processExit{exitCode: -1, err: errors.New("synthetic killed child")}
			return done
		})
	if !errors.Is(completion.err, context.Canceled) || errors.Is(completion.err, ErrCleanupUncertain) {
		t.Fatalf("completion = %#v", completion)
	}
}

func TestProcessGroupObservationFixtures(t *testing.T) {
	for _, test := range []struct {
		name, raw  string
		pid, group int
		state      byte
	}{
		{"kernel-thread", "2 (kthreadd) S 0 0 0", 2, 0, 'S'},
		{"worker", "123 (worker) R 1 123 123", 123, 123, 'R'},
		{"complex-name", "124 (synthetic ) name\n) Z 123 123 123", 124, 123, 'Z'},
	} {
		t.Run(test.name, func(t *testing.T) {
			group, state, err := processGroupState([]byte(test.raw), test.pid)
			if err != nil || group != test.group || state != test.state {
				t.Fatalf("group = %d, state = %q, error = %v", group, state, err)
			}
		})
	}
	for _, raw := range []string{"", "124 (worker) S 1 123", "123 (worker S 1 123", "123 (worker) S", "123 (worker) SS 1 123",
		"123 (worker) S 1 -1", "123 (worker) S 1 +1", "123 (worker) S 1 00", "123 (worker) S 1 unknown", strings.Repeat("x", 8193)} {
		if _, _, err := processGroupState([]byte(raw), 123); err == nil {
			t.Fatal("malformed synthetic process record was accepted")
		}
	}
}
