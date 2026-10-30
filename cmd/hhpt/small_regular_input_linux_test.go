//go:build linux

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSmallRegularLinuxRejectsSpecialFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "input")
			if kind == "symlink" {
				target := filepath.Join(directory, "target")
				if err := os.WriteFile(target, []byte("owned-data"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			} else if err := syscall.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			// Qualification must run this real-wrapper case in a bounded process:
			// a regression to a blocking pathname open must not hang the runner.
			data, err := readSmallRegularFile(path, 4096)
			requireSmallInputFailure(t, data, err, false)
		})
	}
}

type smallLinuxMetadataOwner struct {
	retestInputMetadata
	closes *int
	fail   bool
}

func (owner *smallLinuxMetadataOwner) Close() error {
	(*owner.closes)++
	err := owner.retestInputMetadata.Close()
	if owner.fail {
		return errors.Join(err, smallInputPrivateFailure)
	}
	return err
}

type smallLinuxReadableOwner struct {
	retestInputReadable
	closes *int
	fail   bool
}

func (owner *smallLinuxReadableOwner) Close() error {
	(*owner.closes)++
	err := owner.retestInputReadable.Close()
	if owner.fail {
		return errors.Join(err, smallInputPrivateFailure)
	}
	return err
}

func TestSmallRegularLinuxRealOwnersCloseBeforeAdmission(t *testing.T) {
	for _, failed := range []string{"neither", "metadata", "readable", "both"} {
		t.Run(failed, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "owned-document")
			want := []byte(`{"excluded_tests":[]}`)
			if err := os.WriteFile(path, want, 0600); err != nil {
				t.Fatal(err)
			}
			ops := realRetestInputOperations()
			openMetadata, openReadable := ops.openMetadata, ops.openReadable
			metadataCloses, readableCloses := 0, 0
			ops.openMetadata = func(path string) (retestInputMetadata, error) {
				owner, err := openMetadata(path)
				if nilRetestInputValue(owner) {
					return owner, err
				}
				return &smallLinuxMetadataOwner{owner, &metadataCloses, failed == "metadata" || failed == "both"}, err
			}
			ops.openReadable = func(path string, metadata retestInputMetadata) (retestInputReadable, error) {
				owner := metadata.(*smallLinuxMetadataOwner)
				reader, err := openReadable(path, owner.retestInputMetadata)
				if nilRetestInputValue(reader) {
					return reader, err
				}
				return &smallLinuxReadableOwner{reader, &readableCloses, failed == "readable" || failed == "both"}, err
			}
			data, err := smallInputComposedReader(ops)(path, 4096)
			if failed == "neither" {
				if err != nil || !bytes.Equal(data, want) {
					t.Fatalf("data=%q error=%v", data, err)
				}
			} else {
				requireSmallInputFailure(t, data, err, true)
			}
			if metadataCloses != 1 || readableCloses != 1 {
				t.Fatalf("metadata=%d readable=%d", metadataCloses, readableCloses)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSmallRegularLinuxPinsObjectAcrossReplacement(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "input")
	retained := filepath.Join(directory, "retained")
	original, replacement := []byte("original-owned-value"), []byte("replacement-owned-bytes")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	ops := realRetestInputOperations()
	openReadable := ops.openReadable
	called := 0
	ops.openReadable = func(name string, metadata retestInputMetadata) (retestInputReadable, error) {
		called++
		if name != path {
			t.Fatal("unexpected input")
		}
		if err := os.Rename(path, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, replacement, 0600); err != nil {
			t.Fatal(err)
		}
		return openReadable(name, metadata)
	}
	got, err := smallInputComposedReader(ops)(path, 4096)
	if err != nil || !bytes.Equal(got, original) || called != 1 {
		t.Fatalf("data=%q calls=%d error=%v", got, called, err)
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, replacement) {
		t.Fatal("replacement fixture changed")
	}
	if err := os.Remove(retained); err != nil {
		t.Fatal(err)
	}
}
