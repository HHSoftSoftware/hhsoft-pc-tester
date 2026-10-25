//go:build linux

package workerexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureProcess(command *exec.Cmd) error {
	// Verify waitid availability before creating a process whose identity must
	// remain reserved. The current process cannot be its own child.
	if _, err := observeChild(os.Getpid()); !errors.Is(err, syscall.ECHILD) {
		return errors.Join(ErrCleanupUncertain, errors.New("workerexec: non-reaping child observation unavailable"), err)
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	return nil
}

func waitProcess(ctx context.Context, command *exec.Cmd, closeStdin func()) processCompletion {
	operations := groupOperations{observe: observeChild, groupID: syscall.Getpgid,
		kill: killOwnedGroup, live: liveGroupMembers}
	return waitOwnedGroup(ctx, command.Process.Pid, closeStdin, operations, cleanupTimeout,
		func() <-chan processExit { return startReaper(command) })
}

func killOwnedGroup(pid int) error {
	if pid <= 1 {
		return errors.New("workerexec: invalid owned process group")
	}
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

type groupOperations struct {
	observe func(int) (bool, error)
	groupID func(int) (int, error)
	kill    func(int) error
	live    func(int, time.Time) (bool, error)
}

func waitOwnedGroup(ctx context.Context, pid int, closeStdin func(), operations groupOperations, cleanupWindow time.Duration, reap func() <-chan processExit) processCompletion {
	var cause error
	for {
		ready, err := operations.observe(pid)
		if err != nil {
			cause = errors.Join(ErrCleanupUncertain, err)
			break
		}
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			cause = ctx.Err()
		case <-time.After(5 * time.Millisecond):
			continue
		}
		break
	}
	closeStdin()
	deadline := time.Now().Add(cleanupWindow)
	if !errors.Is(cause, ErrCleanupUncertain) {
		cause = errors.Join(cause, cleanOwnedGroup(pid, operations, deadline))
	}
	// All group signals end before this call. Even an exceptional cleanup
	// retains a reaper for the owned child, without extending the caller's wait.
	return awaitReaper(reap(), deadline, cause)
}

func cleanOwnedGroup(pid int, operations groupOperations, deadline time.Time) error {
	for {
		if pid <= 1 || !time.Now().Before(deadline) {
			return errors.Join(ErrCleanupUncertain, errors.New("workerexec: process group cleanup deadline exceeded"))
		}
		// A successful non-reaping observation proves this PID is still our
		// unreaped child, even while it is alive. ECHILD forbids any signal.
		if _, err := operations.observe(pid); err != nil {
			return errors.Join(ErrCleanupUncertain, err)
		}
		group, err := operations.groupID(pid)
		if err != nil || group != pid {
			return errors.Join(ErrCleanupUncertain, errors.New("workerexec: owned process group identity changed"), err)
		}
		if err := operations.kill(pid); err != nil {
			return errors.Join(ErrCleanupUncertain, err)
		}
		ready, err := operations.observe(pid)
		if err != nil {
			return errors.Join(ErrCleanupUncertain, err)
		}
		live, err := operations.live(pid, deadline)
		if err != nil {
			return errors.Join(ErrCleanupUncertain, err)
		}
		if ready && !live {
			return nil
		}
		timer := time.NewTimer(min(5*time.Millisecond, time.Until(deadline)))
		<-timer.C
	}
}
