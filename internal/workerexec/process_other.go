//go:build !linux

package workerexec

import (
	"errors"
	"os"
	"os/exec"
)

func configureProcess(command *exec.Cmd) {}

func terminateProcess(command *exec.Cmd) error {
	err := command.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
