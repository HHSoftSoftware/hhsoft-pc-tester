//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func mountAuthorizedReportTarget(ctx context.Context, authorization reporttarget.Authorization) (func() error, error) {
	mountContext, cancelMount := context.WithTimeout(ctx, 15*time.Second)
	defer cancelMount()
	command := exec.CommandContext(mountContext, "/bin/mount", "-t", "auto", "-o", "rw,nodev,nosuid,noexec", authorization.Device.Device, authorization.MountPoint)
	command.Env = []string{"PATH=/sbin:/bin", "LANG=C"}
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, errors.New("authorized report partition could not be mounted with the fixed safe policy")
	}
	return func() error {
		unmountContext, cancelUnmount := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancelUnmount()
		command := exec.CommandContext(unmountContext, "/bin/umount", authorization.MountPoint)
		command.Env = []string{"PATH=/sbin:/bin", "LANG=C"}
		command.Stdin = nil
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		if err := command.Run(); err != nil {
			return errors.New("report partition could not be unmounted after persistence")
		}
		return nil
	}, nil
}
