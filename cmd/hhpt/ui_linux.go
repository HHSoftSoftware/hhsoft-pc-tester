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

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
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
	descriptors := []int{int(console.Fd())}
	for _, event := range events {
		descriptors = append(descriptors, int(event.Fd()))
	}
	return waitForInputDescriptors(ctx, timeout, descriptors, syscall.Read)
}

// Keep the real select/read boundary separate from console setup so descriptor
// loss and transient kernel errors can be exercised without device hardware.
func waitForInputDescriptors(ctx context.Context, timeout time.Duration, descriptors []int, read func(int, []byte) (int, error)) (bool, error) {
	if len(descriptors) == 0 {
		return false, errors.New("input monitoring has no descriptors")
	}
	baseSet := syscall.FdSet{}
	maximum := 0
	for _, descriptor := range descriptors {
		var err error
		maximum, err = addDescriptor(&baseSet, descriptor, maximum)
		if err != nil {
			return false, err
		}
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
		readSet := baseSet
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
		for _, descriptor := range descriptors {
			if descriptorIsSet(&readSet, descriptor) {
				n, readErr := read(descriptor, buffer)
				if n > 0 {
					return true, nil
				}
				if errors.Is(readErr, syscall.EINTR) || errors.Is(readErr, syscall.EAGAIN) || errors.Is(readErr, syscall.EWOULDBLOCK) {
					continue
				}
				if readErr == nil {
					readErr = io.EOF
				}
				return false, fmt.Errorf("read monitored input: %w", readErr)
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

func addDescriptor(set *syscall.FdSet, descriptor, maximum int) (int, error) {
	bitsPerWord := int(unsafe.Sizeof(set.Bits[0]) * 8)
	if descriptor < 0 || descriptor >= len(set.Bits)*bitsPerWord {
		return maximum, errors.New("input descriptor exceeds the supported monitoring range")
	}
	set.Bits[descriptor/bitsPerWord] |= 1 << uint(descriptor%bitsPerWord)
	if descriptor > maximum {
		return descriptor, nil
	}
	return maximum, nil
}

func descriptorIsSet(set *syscall.FdSet, descriptor int) bool {
	bitsPerWord := int(unsafe.Sizeof(set.Bits[0]) * 8)
	if descriptor < 0 || descriptor >= len(set.Bits)*bitsPerWord {
		return false
	}
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
	capabilities := make(map[string]string)
	flush := func() {
		if interactiveInput(name, handlers, capabilities) {
			for _, handler := range strings.Fields(handlers) {
				if strings.HasPrefix(handler, "event") {
					paths = append(paths, filepath.Join(inputRoot, handler))
				}
			}
		}
		name, handlers = "", ""
		clear(capabilities)
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
		if strings.HasPrefix(line, "B: ") {
			key, value, found := strings.Cut(strings.TrimPrefix(line, "B: "), "=")
			if found {
				capabilities[key] = value
			}
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return paths, nil
}

func interactiveInput(name, handlers string, capabilities map[string]string) bool {
	// The same capability classification used in inventory recognizes generic
	// device names, including direct touch panels without a mouse handler.
	category := inventory.ClassifyInput(name, handlers, capabilities["KEY"], capabilities["REL"], capabilities["ABS"], capabilities["PROP"])
	switch category {
	case "keyboard", "pointer", "touchpad", "touchscreen", "tablet":
		return true
	}
	for _, handler := range strings.Fields(handlers) {
		if handler == "kbd" {
			return true
		}
	}
	return strings.Contains(strings.ToLower(name), "stylus")
}
