package workerexec

import (
	"errors"
	"fmt"
	"os/exec"
	"time"
)

type processExit struct {
	exitCode int
	err      error
}

type processCompletion struct {
	processExit
	cleanupDeadline time.Time
}

// No numeric process-group signal is permitted once this reaper is started.
// The goroutine owns ProcessState, including on an uncertain cleanup return.
func startReaper(command *exec.Cmd) <-chan processExit {
	done := make(chan processExit, 1)
	go func() {
		err := command.Wait()
		code := -1
		if command.ProcessState != nil {
			code = command.ProcessState.ExitCode()
		}
		if err != nil {
			err = fmt.Errorf("workerexec: worker exited with code %d: %w", code, err)
		}
		done <- processExit{exitCode: code, err: err}
	}()
	return done
}

func awaitReaper(done <-chan processExit, deadline time.Time, cause error) processCompletion {
	complete := func(result processExit) processCompletion {
		result.err = errors.Join(cause, result.err)
		return processCompletion{processExit: result, cleanupDeadline: deadline}
	}
	select {
	case result := <-done:
		return complete(result)
	default:
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case result := <-done:
		return complete(result)
	case <-timer.C:
		return processCompletion{processExit: processExit{exitCode: -1,
			err: errors.Join(ErrCleanupUncertain, cause, errors.New("workerexec: child reap deadline exceeded"))}, cleanupDeadline: deadline}
	}
}
