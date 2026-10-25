//go:build !linux

package workerexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

func configureProcess(command *exec.Cmd) error { return nil }

func waitProcess(ctx context.Context, command *exec.Cmd, closeStdin func()) processCompletion {
	done := startReaper(command)
	select {
	case result := <-done:
		closeStdin()
		return processCompletion{processExit: result, cleanupDeadline: time.Now().Add(cleanupTimeout)}
	case <-ctx.Done():
		select {
		case result := <-done:
			closeStdin()
			return processCompletion{processExit: result, cleanupDeadline: time.Now().Add(cleanupTimeout)}
		default:
		}
		closeStdin()
		deadline := time.Now().Add(cleanupTimeout)
		terminationErr := terminateProcess(command)
		cause := ctx.Err()
		if terminationErr != nil {
			cause = errors.Join(cause, ErrCleanupUncertain, terminationErr)
		}
		return awaitReaper(done, deadline, cause)
	}
}

func terminateProcess(command *exec.Cmd) error {
	err := command.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
