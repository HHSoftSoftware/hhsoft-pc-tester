//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func inputPollPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	return reader, writer
}

func TestInputPollerOnlyStartsAfterHealthyInactivity(t *testing.T) {
	console, _ := inputPollPipe(t)
	event, eventWriter := inputPollPipe(t)
	descriptors := []int{int(console.Fd()), int(event.Fd())}
	activity, err := waitForInputDescriptors(t.Context(), 20*time.Millisecond, descriptors, syscall.Read)
	if err != nil || activity {
		t.Fatalf("healthy idle descriptors failed: activity=%t error=%v", activity, err)
	}
	if _, err := eventWriter.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	activity, err = waitForInputDescriptors(t.Context(), time.Second, descriptors, syscall.Read)
	if err != nil || !activity {
		t.Fatalf("real descriptor activity did not cancel: activity=%t error=%v", activity, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	activity, err = waitForInputDescriptors(ctx, time.Second, descriptors, syscall.Read)
	if activity || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled polling allowed automatic start")
	}
}

func TestInputPollerFailsClosedWhenConsoleOrEventDisappears(t *testing.T) {
	for _, disconnected := range []int{0, 1} {
		t.Run([]string{"console", "event"}[disconnected], func(t *testing.T) {
			console, consoleWriter := inputPollPipe(t)
			event, eventWriter := inputPollPipe(t)
			if err := []*os.File{consoleWriter, eventWriter}[disconnected].Close(); err != nil {
				t.Fatal(err)
			}
			activity, err := waitForInputDescriptors(t.Context(), time.Second, []int{int(console.Fd()), int(event.Fd())}, syscall.Read)
			if activity || !errors.Is(err, io.EOF) {
				t.Fatalf("descriptor EOF permitted automatic start: activity=%t error=%v", activity, err)
			}
		})
	}
}

func TestInputPollerDistinguishesTransientReadErrorsFromDeviceLoss(t *testing.T) {
	for _, injected := range []error{syscall.EINTR, syscall.EAGAIN, syscall.EWOULDBLOCK, syscall.ENODEV, syscall.EIO, io.EOF} {
		t.Run(injected.Error(), func(t *testing.T) {
			reader, writer := inputPollPipe(t)
			if _, err := writer.Write([]byte{1}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			// The real select sees a ready pipe. Substitute only its first read
			// result; the transient control must retry and consume the real byte.
			read := func(descriptor int, buffer []byte) (int, error) {
				calls++
				if calls == 1 {
					return 0, injected
				}
				return syscall.Read(descriptor, buffer)
			}
			activity, err := waitForInputDescriptors(t.Context(), time.Second, []int{int(reader.Fd())}, read)
			transient := errors.Is(injected, syscall.EINTR) || errors.Is(injected, syscall.EAGAIN)
			if transient {
				if err != nil || !activity || calls != 2 {
					t.Fatalf("transient read did not retry real activity: calls=%d activity=%t error=%v", calls, activity, err)
				}
			} else if activity || !errors.Is(err, injected) || calls != 1 {
				t.Fatalf("permanent device error permitted inactivity: calls=%d activity=%t error=%v", calls, activity, err)
			}
		})
	}
}

func TestInputPollerRejectsClosedInvalidAndExcessiveDescriptors(t *testing.T) {
	set := syscall.FdSet{}
	capacity := len(set.Bits) * int(unsafe.Sizeof(set.Bits[0])*8)
	for _, descriptors := range [][]int{nil, {-1}, {capacity}, {int(^uint(0) >> 1)}} {
		activity, err := waitForInputDescriptors(t.Context(), time.Second, descriptors, func(int, []byte) (int, error) {
			t.Fatal("invalid descriptor reached a read")
			return 0, nil
		})
		if activity || err == nil {
			t.Fatalf("invalid descriptors were accepted: %v", descriptors)
		}
	}
	maximum, err := addDescriptor(&set, capacity-1, 0)
	if err != nil || maximum != capacity-1 || !descriptorIsSet(&set, capacity-1) || descriptorIsSet(&set, -1) || descriptorIsSet(&set, capacity) {
		t.Fatal("descriptor-set boundary handling is incorrect")
	}
	reader, _ := inputPollPipe(t)
	descriptor := int(reader.Fd())
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	activity, err := waitForInputDescriptors(t.Context(), time.Second, []int{descriptor}, syscall.Read)
	if activity || !errors.Is(err, syscall.EBADF) {
		t.Fatalf("closed descriptor permitted automatic start: activity=%t error=%v", activity, err)
	}
}

func TestAutomaticStartMonitorsGenericNamedTouchCapabilities(t *testing.T) {
	directory := t.TempDir()
	registry := filepath.Join(directory, "devices")
	contents := "N: Name=\"Fixture Panel\"\nH: Handlers=event2\nB: ABS=3\nB: PROP=2\n\n" +
		"N: Name=\"Fixture Surface\"\nH: Handlers=event3\nB: ABS=3\nB: PROP=4\n\n" +
		"N: Name=\"Fixture Sensor\"\nH: Handlers=event4\nB: ABS=3\n\n" +
		"N: Name=\"Fixture Keys\"\nH: Handlers=kbd event5\n\n" +
		"N: Name=\"Fixture Pointer\"\nH: Handlers=mouse0 event6\n"
	if err := os.WriteFile(registry, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := interactiveEventPaths(registry, directory)
	wanted := []string{filepath.Join(directory, "event2"), filepath.Join(directory, "event3"),
		filepath.Join(directory, "event5"), filepath.Join(directory, "event6")}
	if err != nil || !reflect.DeepEqual(paths, wanted) {
		t.Fatalf("generic touch devices were omitted or sensor inherited capabilities: paths=%v error=%v", paths, err)
	}
	// Each selected interface reaches the actual descriptor poller. No hardware
	// input device, input injection ioctl, or production console is involved.
	for _, path := range paths {
		if err := os.WriteFile(path, []byte{1}, 0o600); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		activity, err := waitForInputDescriptors(t.Context(), time.Second, []int{int(file.Fd())}, syscall.Read)
		_ = file.Close()
		if err != nil || !activity {
			t.Fatalf("selected input did not cancel automatic start: %s %v", path, err)
		}
	}
}
