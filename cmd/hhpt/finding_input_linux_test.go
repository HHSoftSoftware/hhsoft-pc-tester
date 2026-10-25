//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func findingFlags(t *testing.T, file *os.File, set *int) int {
	t.Helper()
	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var value uintptr
	var callErr syscall.Errno
	err = raw.Control(func(fd uintptr) {
		if set != nil {
			_, _, callErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_SETFL, uintptr(*set))
			if callErr != 0 {
				return
			}
		}
		value, _, callErr = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
	})
	if err != nil || callErr != 0 {
		t.Fatalf("fixture descriptor flags failed: %v %v", err, callErr)
	}
	return int(value)
}

func TestFindingPipeRestoresFlagsAndLeavesLaterInput(t *testing.T) {
	for _, nonblocking := range []bool{false, true} {
		for _, mode := range []string{"success", "timeout", "cancel", "oversize", "eof"} {
			t.Run(mode+map[bool]string{false: "/blocking", true: "/nonblocking"}[nonblocking], func(t *testing.T) {
				reader, writer, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
				flags := findingFlags(t, reader, nil)
				if nonblocking {
					flags |= syscall.O_NONBLOCK
				} else {
					flags &^= syscall.O_NONBLOCK
				}
				findingFlags(t, reader, &flags)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var writerDone chan error
				if mode == "success" {
					if _, err := writer.WriteString("P\nnext\n"); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "oversize" {
					writerDone = make(chan error, 1)
					go func() {
						_, err := writer.WriteString(strings.Repeat("P", maximumFindingLineBytes+1) + "\n")
						writerDone <- err
					}()
				}
				if mode == "eof" {
					if _, err := writer.WriteString("P"); err != nil {
						t.Fatal(err)
					}
					_ = writer.Close()
				}
				if mode == "cancel" {
					timer := time.AfterFunc(10*time.Millisecond, cancel)
					defer timer.Stop()
				}
				line, timedOut, readErr := readFindingLine(ctx, reader, 30*time.Millisecond)
				if writerDone != nil {
					select {
					case err := <-writerDone:
						if err != nil {
							t.Fatal(err)
						}
					case <-time.After(time.Second):
						_ = writer.Close()
						<-writerDone
						t.Fatal("oversized fixture writer did not finish")
					}
				}
				if findingFlags(t, reader, nil) != flags {
					t.Fatal("operator prompt changed caller descriptor flags")
				}
				switch mode {
				case "success":
					if line != "P" || timedOut || readErr != nil {
						t.Fatalf("completed finding rejected: %q %v", line, readErr)
					}
					remaining := make([]byte, 5)
					if _, err := io.ReadFull(reader, remaining); err != nil || string(remaining) != "next\n" {
						t.Fatal("finding read ahead into the next input")
					}
				case "timeout", "cancel":
					if line != "" || mode == "timeout" && (!timedOut || readErr != nil) || mode == "cancel" && !errors.Is(readErr, context.Canceled) {
						t.Fatalf("prompt did not stop: %q timeout=%t err=%v", line, timedOut, readErr)
					}
					if _, err := writer.WriteString("next\n"); err != nil {
						t.Fatal(err)
					}
					line, timedOut, readErr = readFindingLine(t.Context(), reader, time.Second)
					if line != "next" || timedOut || readErr != nil {
						t.Fatalf("old prompt consumed late input: %q %v", line, readErr)
					}
				case "oversize":
					if line != "" || readErr == nil {
						t.Fatal("oversized finding was accepted")
					}
				case "eof":
					if line != "" || !errors.Is(readErr, io.EOF) {
						t.Fatal("partial EOF response was accepted")
					}
				}
			})
		}
	}
}

func TestFindingPipePreCancellationClosedFileAndRestorationOwnership(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := findingFlags(t, reader, nil)
	if _, err := writer.WriteString("P\n"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readFindingLine(ctx, reader, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal("pre-cancelled prompt was not rejected")
	}
	if findingFlags(t, reader, nil) != before {
		t.Fatal("pre-cancellation changed descriptor flags")
	}
	line, _, err := readFindingLine(t.Context(), reader, time.Second)
	if line != "P" || err != nil {
		t.Fatal("pre-cancellation consumed input")
	}
	blocking := before &^ syscall.O_NONBLOCK
	findingFlags(t, reader, &blocking)
	readContext, stopRead := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer stopRead()
	read, restore, err := findingFileReader(readContext, reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	otherReader, otherWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer otherReader.Close()
	defer otherWriter.Close()
	otherFlags := findingFlags(t, otherReader, nil)
	if _, err := otherWriter.WriteString("safe\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := read(make([]byte, 1)); err == nil {
		t.Fatal("closed finding descriptor was read")
	}
	if err := restore(); err == nil {
		t.Fatal("closed finding descriptor was restored through its stale number")
	}
	if findingFlags(t, otherReader, nil) != otherFlags {
		t.Fatal("restoration changed an unrelated descriptor")
	}
	remaining := make([]byte, 5)
	if _, err := io.ReadFull(otherReader, remaining); err != nil || string(remaining) != "safe\n" {
		t.Fatal("closed prompt consumed an unrelated descriptor's bytes")
	}
	if _, _, err := readFindingLine(t.Context(), reader, time.Second); err == nil {
		t.Fatal("closed file accepted")
	}
}
