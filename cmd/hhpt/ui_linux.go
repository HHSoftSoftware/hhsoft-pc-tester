//go:build linux

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func waitForOperatorInteraction(ctx context.Context, timeout time.Duration, input io.Reader) (bool, error) {
	console, ok := input.(*os.File)
	if !ok {
		return false, errors.New("console is not a file")
	}
	restore, err := prepareConsole(console)
	if err != nil {
		return false, fmt.Errorf("prepare console: %w", err)
	}
	defer restore()

	eventPaths, err := interactiveEventPaths("/proc/bus/input/devices", "/dev/input")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("discover input devices: %w", err)
	}
	events := make([]*os.File, 0, len(eventPaths))
	defer func() {
		for _, event := range events {
			_ = event.Close()
		}
	}()
	for _, path := range eventPaths {
		event, openErr := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if openErr != nil {
			return false, fmt.Errorf("open %s: %w", path, openErr)
		}
		events = append(events, event)
	}

	deadline := time.Now().Add(timeout)
	buffer := make([]byte, 256)
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, nil
		}
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		readSet := syscall.FdSet{}
		maximum := addDescriptor(&readSet, int(console.Fd()), 0)
		for _, event := range events {
			maximum = addDescriptor(&readSet, int(event.Fd()), maximum)
		}
		pollTimeout := syscall.NsecToTimeval(remaining.Nanoseconds())
		ready, selectErr := syscall.Select(maximum+1, &readSet, nil, nil, &pollTimeout)
		if selectErr != nil {
			if errors.Is(selectErr, syscall.EINTR) {
				continue
			}
			return false, fmt.Errorf("monitor input: %w", selectErr)
		}
		if ready == 0 {
			continue
		}
		if descriptorIsSet(&readSet, int(console.Fd())) {
			n, readErr := console.Read(buffer)
			if n > 0 {
				return true, nil
			}
			if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, syscall.EINTR) {
				return false, fmt.Errorf("read console: %w", readErr)
			}
		}
		for _, event := range events {
			if descriptorIsSet(&readSet, int(event.Fd())) {
				n, _ := event.Read(buffer)
				if n > 0 {
					return true, nil
				}
			}
		}
	}
}

func prepareConsole(console *os.File) (func(), error) {
	fileDescriptor := console.Fd()
	var original syscall.Termios
	if err := ioctlTermios(fileDescriptor, syscall.TCGETS, &original); err != nil {
		return func() {}, err
	}
	configured := original
	configured.Lflag &^= syscall.ICANON | syscall.ECHO
	configured.Cc[syscall.VMIN] = 1
	configured.Cc[syscall.VTIME] = 0
	if err := ioctlTermios(fileDescriptor, syscall.TCSETS, &configured); err != nil {
		return func() {}, err
	}
	return func() {
		_ = ioctlTermios(fileDescriptor, syscall.TCSETS, &original)
	}, nil
}

func addDescriptor(set *syscall.FdSet, descriptor, maximum int) int {
	bitsPerWord := int(unsafe.Sizeof(set.Bits[0]) * 8)
	set.Bits[descriptor/bitsPerWord] |= 1 << uint(descriptor%bitsPerWord)
	if descriptor > maximum {
		return descriptor
	}
	return maximum
}

func descriptorIsSet(set *syscall.FdSet, descriptor int) bool {
	bitsPerWord := int(unsafe.Sizeof(set.Bits[0]) * 8)
	return set.Bits[descriptor/bitsPerWord]&(1<<uint(descriptor%bitsPerWord)) != 0
}

func ioctlTermios(fileDescriptor uintptr, operation uintptr, value *syscall.Termios) error {
	_, _, callErr := syscall.Syscall(syscall.SYS_IOCTL, fileDescriptor, operation, uintptr(unsafe.Pointer(value)))
	if callErr != 0 {
		return callErr
	}
	return nil
}

func interactiveEventPaths(procPath, inputRoot string) ([]string, error) {
	devices, err := os.Open(procPath)
	if err != nil {
		return nil, err
	}
	defer devices.Close()

	paths := make([]string, 0)
	name := ""
	handlers := ""
	flush := func() {
		if interactiveInput(name, handlers) {
			for _, handler := range strings.Fields(handlers) {
				if strings.HasPrefix(handler, "event") {
					paths = append(paths, filepath.Join(inputRoot, handler))
				}
			}
		}
		name, handlers = "", ""
	}
	scanner := bufio.NewScanner(devices)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "N: Name=") {
			name = strings.Trim(strings.TrimPrefix(line, "N: Name="), "\"")
		}
		if strings.HasPrefix(line, "H: Handlers=") {
			handlers = strings.TrimPrefix(line, "H: Handlers=")
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return paths, nil
}

func interactiveInput(name, handlers string) bool {
	for _, handler := range strings.Fields(handlers) {
		if handler == "kbd" || strings.HasPrefix(handler, "mouse") {
			return true
		}
	}
	name = strings.ToLower(name)
	for _, indicator := range []string{"keyboard", "mouse", "touchpad", "trackpad", "touchscreen", "touch screen", "trackball", "pointing", "tablet", "stylus"} {
		if strings.Contains(name, indicator) {
			return true
		}
	}
	return false
}
