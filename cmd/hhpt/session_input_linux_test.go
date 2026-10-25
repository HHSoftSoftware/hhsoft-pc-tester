//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func sessionPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close(); _ = reader.Close() })
	return reader, writer
}

func inputFlags(t *testing.T, file *os.File) uintptr {
	t.Helper()
	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var value uintptr
	var callErr syscall.Errno
	if err := raw.Control(func(fd uintptr) { value, _, callErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0) }); err != nil || callErr != 0 {
		t.Fatal("could not inspect owned input flags")
	}
	return value
}

func pendingSessionLine(t *testing.T, console *sessionConsole) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		console.mutex.Lock()
		pending := console.request != nil
		console.mutex.Unlock()
		if pending {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("finding request was not ready")
}

func TestSessionConsoleCancellationJoinsAndPreservesFutureInput(t *testing.T) {
	for _, stop := range []string{"ctrl-c", "parent", "finish"} {
		t.Run(stop, func(t *testing.T) {
			reader, writer := sessionPipe(t)
			flags := inputFlags(t, reader)
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx, input, finish, err := prepareSessionInput(parent, reader)
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			if _, err := input.Read(make([]byte, 1)); err == nil {
				t.Fatal("a competing direct reader was allowed")
			}
			switch stop {
			case "ctrl-c":
				if _, err := writer.Write([]byte{3}); err != nil {
					t.Fatal(err)
				}
			case "parent":
				cancel()
			}
			if stop != "finish" {
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
					t.Fatal("session cancellation did not arrive")
				}
			}
			if err := finish(); err != nil {
				t.Fatal(err)
			}
			if err := finish(); err != nil {
				t.Fatal("input cleanup is not idempotent")
			}
			if inputFlags(t, reader) != flags {
				t.Fatal("caller input flags were not restored")
			}
			if _, err := writer.Write([]byte("future\n")); err != nil {
				t.Fatal(err)
			}
			if err := reader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			future := make([]byte, 7)
			if _, err := io.ReadFull(reader, future); err != nil || string(future) != "future\n" {
				t.Fatal("a reader survived cleanup and consumed future input")
			}
		})
	}
}

func TestSessionConsoleFindingTimeoutDiscardsPartialLine(t *testing.T) {
	reader, writer := sessionPipe(t)
	_, input, finish, err := prepareSessionInput(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	console := input.(*sessionConsole)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	done := make(chan sessionLineResult, 1)
	go func() { line, err := console.ReadFindingLine(ctx, 32); done <- sessionLineResult{line, err} }()
	pendingSessionLine(t, console)
	if _, err := writer.Write([]byte("f")); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.line != "" || !errors.Is(result.err, context.DeadlineExceeded) {
		t.Fatal("partial finding survived its deadline")
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("p\n")); err != nil {
		t.Fatal(err)
	}
	_ = reader.SetReadDeadline(time.Now().Add(time.Second))
	future := make([]byte, 2)
	if _, err := io.ReadFull(reader, future); err != nil || string(future) != "p\n" {
		t.Fatal("timed-out finding reader consumed future input")
	}
}

func TestSessionConsoleFindingLineAndLimit(t *testing.T) {
	for _, value := range []string{"pffffpi\n", "toolong\n"} {
		t.Run(value[:len(value)-1], func(t *testing.T) {
			reader, writer := sessionPipe(t)
			_, input, finish, err := prepareSessionInput(t.Context(), reader)
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			console := input.(*sessionConsole)
			limit := 7
			if value == "toolong\n" {
				limit = 3
			}
			done := make(chan sessionLineResult, 1)
			go func() { line, err := console.ReadFindingLine(t.Context(), limit); done <- sessionLineResult{line, err} }()
			pendingSessionLine(t, console)
			if _, err := writer.Write([]byte(value)); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-done:
				if limit == 7 && (result.err != nil || result.line != "pffffpi") {
					t.Fatal("complete finding was lost")
				}
				if limit == 3 && (result.err == nil || result.line != "") {
					t.Fatal("unbounded finding was accepted")
				}
			case <-time.After(time.Second):
				t.Fatal("finding did not complete")
			}
		})
	}
}

func TestSessionConsoleClosedInputBlocksSigning(t *testing.T) {
	reader, writer := sessionPipe(t)
	ctx, _, finish, err := prepareSessionInput(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	_ = writer.Close()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("input failure did not stop session")
	}
	if err := finish(); err == nil {
		t.Fatal("uncertain input lifetime allowed finalization")
	}
}

func TestSessionConsoleRejectsRegularInputWithoutReading(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("future\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	flags := inputFlags(t, file)
	if _, _, _, err := prepareSessionInput(t.Context(), file); err == nil {
		t.Fatal("regular file accepted as cancellable input")
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 0 || inputFlags(t, file) != flags {
		t.Fatal("rejected input was consumed or modified")
	}
}

func TestSessionConsoleNeverTouchesReplacementDescriptor(t *testing.T) {
	reader, _ := sessionPipe(t)
	ctx, _, finish, err := prepareSessionInput(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	_ = reader.Close()
	replacement, writer := sessionPipe(t)
	flags := inputFlags(t, replacement)
	if _, err := writer.WriteString("replacement\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("closed input was not detected")
	}
	if err := finish(); err == nil {
		t.Fatal("closed input did not block finalization")
	}
	if inputFlags(t, replacement) != flags {
		t.Fatal("replacement descriptor flags changed")
	}
	_ = replacement.SetReadDeadline(time.Now().Add(time.Second))
	data := make([]byte, 12)
	if _, err := io.ReadFull(replacement, data); err != nil || string(data) != "replacement\n" {
		t.Fatal("replacement descriptor input was consumed")
	}
}

func TestSessionConsoleRestoresOwnedTerminalMode(t *testing.T) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal("cannot open isolated pseudo-terminal")
	}
	defer master.Close()
	var unlocked int32
	var number uint32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlocked)))
	if errno != 0 {
		t.Fatal(errno)
	}
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&number)))
	if errno != 0 {
		t.Fatal(errno)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal("cannot open isolated pseudo-terminal slave")
	}
	defer slave.Close()
	var original, during, after syscall.Termios
	if err := ioctlTermios(slave.Fd(), syscall.TCGETS, &original); err != nil {
		t.Fatal(err)
	}
	ctx, _, finish, err := prepareSessionInput(t.Context(), slave)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	raw, err := slave.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var observed error
	if err := raw.Control(func(fd uintptr) { observed = ioctlTermios(fd, syscall.TCGETS, &during) }); err != nil || observed != nil {
		t.Fatal("cannot inspect owned terminal")
	}
	if during.Lflag&(syscall.ICANON|syscall.ECHO|syscall.ISIG) != 0 {
		t.Fatal("terminal cancellation bytes remain blocked by line discipline")
	}
	if _, err := master.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Ctrl-C did not cancel without a foreground process group")
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	if err := ioctlTermios(slave.Fd(), syscall.TCGETS, &after); err != nil {
		t.Fatal(err)
	}
	if after != original {
		t.Fatal("owned terminal mode was not restored")
	}
}
