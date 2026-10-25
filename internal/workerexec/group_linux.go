//go:build linux

package workerexec

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Only membership in the owned, still-reserved PGID is inspected. Zombies
// cannot execute or keep pipe descriptors open; their parent owns reaping.
func liveGroupMembers(group int, deadline time.Time) (bool, error) {
	directory, err := os.Open("/proc")
	if err != nil {
		return false, err
	}
	defer directory.Close()
	for {
		names, readErr := directory.Readdirnames(64)
		for _, name := range names {
			if !time.Now().Before(deadline) {
				return false, errors.New("workerexec: process group observation deadline exceeded")
			}
			pid, err := strconv.Atoi(name)
			if err != nil || pid <= 0 || strconv.Itoa(pid) != name {
				continue
			}
			file, err := os.Open(filepath.Join("/proc", name, "stat"))
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
				continue
			}
			if err != nil {
				return false, err
			}
			raw, readStatErr := io.ReadAll(io.LimitReader(file, 8193))
			closeErr := file.Close()
			if errors.Is(readStatErr, os.ErrNotExist) || errors.Is(readStatErr, syscall.ESRCH) {
				continue
			}
			if readStatErr != nil || closeErr != nil {
				return false, errors.Join(readStatErr, closeErr)
			}
			observedGroup, state, err := processGroupState(raw, pid)
			if err != nil {
				return false, err
			}
			if observedGroup == group && state != 'Z' && state != 'X' {
				return true, nil
			}
		}
		if readErr == io.EOF {
			return false, nil
		}
		if readErr != nil {
			return false, readErr
		}
	}
}

func processGroupState(raw []byte, pid int) (int, byte, error) {
	// The command name is parenthesized and can itself contain spaces and ')'.
	end := bytes.LastIndexByte(raw, ')')
	prefix := strconv.Itoa(pid) + " ("
	if len(raw) > 8192 || !bytes.HasPrefix(raw, []byte(prefix)) || end < len(prefix) {
		return 0, 0, errors.New("workerexec: incomplete process group observation")
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 3 || len(fields[0]) != 1 {
		return 0, 0, errors.New("workerexec: incomplete process group observation")
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil || group < 0 || strconv.Itoa(group) != fields[2] {
		return 0, 0, errors.New("workerexec: invalid process group observation")
	}
	return group, fields[0][0], nil
}
