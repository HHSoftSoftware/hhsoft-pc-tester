//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRetestInputLinuxRejectsLinkAndFIFO(t *testing.T) {
	for _, kind := range []string{"link", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "input")
			if kind == "link" {
				target := filepath.Join(dir, "target")
				if err := os.WriteFile(target, bytes.Repeat([]byte{1}, 32), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("target", path); err != nil {
					t.Fatal(err)
				}
			} else if err := syscall.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			ops := realRetestInputOperations()
			open := ops.openMetadata
			acquired, closed, readable := 0, 0, 0
			ops.openMetadata = func(path string) (retestInputMetadata, error) {
				owner, err := open(path)
				if owner != nil {
					acquired++
					return &retestInputTrackedMetadata{owner, &closed}, err
				}
				return nil, err
			}
			ops.openReadable = func(string, retestInputMetadata) (retestInputReadable, error) {
				readable++
				return nil, errors.New("unexpected readable open")
			}
			data, err := readRetestInputWithOperations(context.Background(), path, retestArtifactInput, ops)
			assertRetestInputFailure(t, data, err, false)
			if acquired != 1 || closed != 1 || readable != 0 {
				t.Fatalf("acquired=%d closed=%d readable=%d", acquired, closed, readable)
			}
		})
	}
}

func TestRetestInputLinuxPinnedObjectAndFlags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input")
	replacement := filepath.Join(dir, "replacement")
	retained := filepath.Join(dir, "retained")
	want := bytes.Repeat([]byte{0x63}, 32)
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, bytes.Repeat([]byte{0xe7}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	ops := realRetestInputOperations()
	openMetadata, openReadable := ops.openMetadata, ops.openReadable
	var metadataFile, readableFile *os.File
	var boundaryErr error
	checkFlags := func(file *os.File, required int) {
		flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_GETFL, 0)
		if errno != 0 || int(flags)&required != required || int(flags)&syscall.O_ACCMODE != syscall.O_RDONLY {
			boundaryErr = errors.New("unexpected access flags")
		}
		flags, _, errno = syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_GETFD, 0)
		if errno != 0 || int(flags)&syscall.FD_CLOEXEC == 0 {
			boundaryErr = errors.New("missing close-on-exec")
		}
	}
	ops.openMetadata = func(path string) (retestInputMetadata, error) {
		owner, err := openMetadata(path)
		if owner != nil {
			metadataFile = owner.(*os.File)
			checkFlags(metadataFile, 0x200000)
		}
		return owner, err
	}
	ops.openReadable = func(path string, owner retestInputMetadata) (retestInputReadable, error) {
		if err := os.Rename(path, retained); err != nil {
			return nil, err
		}
		if err := os.Rename(replacement, path); err != nil {
			return nil, err
		}
		reader, err := openReadable(path, owner)
		if reader != nil {
			readableFile = reader.(*os.File)
			checkFlags(readableFile, syscall.O_NONBLOCK)
		}
		return reader, err
	}
	data, err := readRetestInputWithOperations(context.Background(), path, retestArtifactInput, ops)
	if err != nil || boundaryErr != nil || !bytes.Equal(data, want) {
		t.Fatalf("length=%d error=%v flags=%v", len(data), err, boundaryErr)
	}
	for _, file := range []*os.File{metadataFile, readableFile} {
		if file == nil {
			t.Fatal("missing owner")
		}
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("owner remains open: %v", err)
		}
	}
	current, err := os.ReadFile(path)
	if err != nil || bytes.Equal(current, data) {
		t.Fatal("replacement fixture did not distinguish the bound object")
	}
}
