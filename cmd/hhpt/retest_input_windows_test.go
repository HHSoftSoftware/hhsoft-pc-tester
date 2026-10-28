//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRetestInputWindowsRejectsSpecialNamespacesBeforeOpen(t *testing.T) {
	for index, path := range []string{
		"", `\\.\PhysicalDrive0`, `\\.\C:`, `\\?\GLOBALROOT\Device\HarddiskVolume1\report`,
		`\\?\Volume{00000000-0000-0000-0000-000000000000}\`, `\\?\C:\fixture\report`,
		`\\server\share\report`, `C:\`, `C:\fixture\NUL`, `C:\fixture\CON.txt`,
		`C:\fixture\COM1`, `C:\fixture\LPT1.log`, `C:\fixture\CONIN$`,
		`C:\fixture\report:stream`, "C:\\fixture\\report\x00", `C:\fixture\report.`, `C:\fixture\report `,
		`C:\fixture\report.\..\ordinary`, `C:\fixture\report \..\ordinary`,
		`C:\fixture\NUL\..\ordinary`, `C:\fixture\CON.txt\..\ordinary`,
		`C:/fixture/report.`, `C:/fixture/report `, `C:/fixture/report./../ordinary`,
		`relative\report.`, `relative/report `, `relative\report.\..\ordinary`,
		`//./PhysicalDrive0`, `//?/C:/fixture/ordinary`, `C:\fixture\report:stream\..\ordinary`,
	} {
		t.Run(fmt.Sprintf("case_%d", index), func(t *testing.T) {
			calls := 0
			owner, err := openRetestWindowsMetadata(path, func(string) (syscall.Handle, error) {
				calls++
				return syscall.InvalidHandle, errors.New("unexpected open")
			}, func(syscall.Handle) retestInputReadable {
				calls++
				return nil
			}, func(syscall.Handle) error {
				calls++
				return nil
			})
			if owner != nil || !errors.Is(err, errRetestInput) || calls != 0 {
				t.Fatalf("owner=%v error=%v provider calls=%d", owner != nil, err, calls)
			}
		})
	}
	t.Run("ordinary_file", func(t *testing.T) {
		fixture := newRetestInputFixture(32)
		calls := 0
		owner, err := openRetestWindowsMetadata(`C:\fixture\.report`, func(path string) (syscall.Handle, error) {
			calls++
			if path != `C:\fixture\.report` {
				t.Fatal("different ordinary path")
			}
			return 42, nil
		}, func(syscall.Handle) retestInputReadable {
			fixture.ledger.acquired["readable"]++
			return fixture.readable
		}, func(syscall.Handle) error {
			t.Fatal("unexpected raw close after transfer")
			return nil
		})
		if err != nil || owner == nil || calls != 1 {
			t.Fatalf("owner=%v error=%v calls=%d", owner != nil, err, calls)
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
		assertRetestInputLedger(t, fixture, map[string]int{"readable": 1}, []string{"readable"})
	})
}

func TestRetestInputWindowsRawHandleOwnership(t *testing.T) {
	for _, shape := range []string{"invalid", "invalid_with_error", "nil_wrapper", "typed_nil_wrapper", "close_failure", "owned", "owned_with_error"} {
		t.Run(shape, func(t *testing.T) {
			fixture := newRetestInputFixture(32)
			handle := syscall.Handle(42)
			var failure error
			if shape == "invalid" || shape == "invalid_with_error" {
				handle = syscall.InvalidHandle
			}
			if shape == "owned_with_error" || shape == "invalid_with_error" {
				failure = errors.New("private-open-error")
			}
			wrapped, rawClosed := 0, 0
			owner, err := adoptRetestWindowsHandle(handle, failure, func(actual syscall.Handle) retestInputReadable {
				wrapped++
				if actual != 42 {
					t.Fatal("different raw handle")
				}
				if shape == "nil_wrapper" || shape == "close_failure" {
					return nil
				}
				if shape == "typed_nil_wrapper" {
					var nilOwner *retestInputTestFile
					return nilOwner
				}
				fixture.ledger.acquired["readable"]++
				return fixture.readable
			}, func(actual syscall.Handle) error {
				rawClosed++
				if actual != 42 {
					t.Fatal("different closed raw handle")
				}
				if shape == "close_failure" {
					return errors.New("private-close-error")
				}
				return nil
			})
			if shape == "owned" || shape == "owned_with_error" {
				if owner != fixture.readable || err != failure || wrapped != 1 || rawClosed != 0 {
					t.Fatalf("transfer owner=%v error=%v wrapped=%d rawClosed=%d", owner != nil, err, wrapped, rawClosed)
				}
				if closeErr := owner.Close(); closeErr != nil {
					t.Fatal(closeErr)
				}
				assertRetestInputLedger(t, fixture, map[string]int{"readable": 1}, []string{"readable"})
				return
			}
			if owner != nil || !errors.Is(err, errRetestInput) || errors.Is(err, errRetestInputCleanup) != (shape == "close_failure") {
				t.Fatalf("owner=%v error=%v", owner != nil, err)
			}
			if handle == syscall.InvalidHandle {
				if wrapped != 0 || rawClosed != 0 {
					t.Fatal("invalid handle was used")
				}
			} else if wrapped != 1 || rawClosed != 1 {
				t.Fatal("rejected raw ownership not closed once")
			}
		})
	}
}

func TestRetestInputWindowsPinnedReopenAndSharing(t *testing.T) {
	for _, replacePath := range []bool{false, true} {
		name := "different_existing_path"
		if replacePath {
			name = "original_path_replacement"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "input")
			replacement := filepath.Join(dir, "replacement")
			moved := filepath.Join(dir, "moved")
			want := bytes.Repeat([]byte{0x71}, 32)
			other := bytes.Repeat([]byte{0x92}, 32)
			for name, data := range map[string][]byte{path: want, replacement: other} {
				if err := os.WriteFile(name, data, 0600); err != nil {
					t.Fatal("owned fixture creation failed")
				}
			}
			ops := realRetestInputOperations()
			openMetadata, openReadable := ops.openMetadata, ops.openReadable
			var metadataFile, readableFile *retestWindowsFile
			metadataCalls, readableCalls := 0, 0
			metadataReadDenied, readableWriteDenied, writerClosed := false, false, true
			renamed, replacementCreated, renameDenied := false, false, false
			var fixtureFailure error
			ops.openMetadata = func(name string) (retestInputMetadata, error) {
				metadataCalls++
				owner, err := openMetadata(name)
				if owner != nil {
					metadataFile = owner.(*retestWindowsFile)
					var probe [1]byte
					n, readErr := metadataFile.Read(probe[:])
					metadataReadDenied = n == 0 && readErr != nil
				}
				return owner, err
			}
			ops.openReadable = func(_ string, owner retestInputMetadata) (retestInputReadable, error) {
				readableCalls++
				reopenPath, originalObjectPath := replacement, path
				if replacePath {
					renameErr := os.Rename(path, moved)
					if renameErr == nil {
						renamed = true
						originalObjectPath = moved
						if err := os.WriteFile(path, other, 0600); err != nil {
							fixtureFailure = errors.New("owned replacement creation failed")
						} else {
							replacementCreated = true
							reopenPath = path
						}
					} else if errors.Is(renameErr, syscall.Errno(32)) {
						// ERROR_SHARING_VIOLATION: a rejected rename is not proof
						// of successful pathname substitution on this filesystem.
						renameDenied = true
					} else {
						fixtureFailure = errors.New("unexpected owned rename failure")
					}
				}
				// Both supplied names designate a different owned ordinary file.
				// Only reopening the proven handle can return the original bytes.
				reader, openErr := openReadable(reopenPath, owner)
				if reader != nil {
					readableFile = reader.(*retestWindowsFile)
					writer, writeErr := os.OpenFile(originalObjectPath, os.O_WRONLY, 0)
					readableWriteDenied = writer == nil && errors.Is(writeErr, syscall.Errno(32))
					if writer != nil {
						writerClosed = writer.Close() == nil
					}
				}
				return reader, openErr
			}
			data, err := readRetestInputWithOperations(context.Background(), path, retestArtifactInput, ops)
			if err != nil || fixtureFailure != nil || !bytes.Equal(data, want) || bytes.Equal(data, other) {
				t.Errorf("pinned capture: length=%d acquisition_failed=%v fixture_failed=%v", len(data), err != nil, fixtureFailure != nil)
			}
			if metadataCalls != 1 || readableCalls != 1 || !metadataReadDenied || !readableWriteDenied || !writerClosed {
				t.Errorf("ownership/access: metadata=%d readable=%d metadata_read_denied=%v readable_write_denied=%v writer_closed=%v", metadataCalls, readableCalls, metadataReadDenied, readableWriteDenied, writerClosed)
			}
			for _, owner := range []*retestWindowsFile{metadataFile, readableFile} {
				if owner == nil {
					t.Error("missing acquired owner")
					continue
				}
				// Windows Stat reports the raw handle error, not ErrClosed.
				// Observe the managed owner state without probing a stale handle.
				if owner.File.Fd() != uintptr(syscall.InvalidHandle) {
					t.Error("acquired owner retained a valid handle")
				}
				var closedProbe [1]byte
				if n, err := owner.File.Read(closedProbe[:]); n != 0 || !errors.Is(err, os.ErrClosed) {
					t.Error("managed owner did not reject a read after close")
				}
			}
			if replacePath {
				if renamed {
					if !replacementCreated {
						t.Error("successful rename lacked the distinct replacement")
					} else {
						t.Log("owned original pathname replaced; proven handle retained original bytes")
					}
				} else if renameDenied {
					t.Log("owned rename denied by sharing; distinct existing-path proof still exercised")
				} else {
					t.Error("rename attempt had no classified outcome")
				}
			}
			for _, name := range []string{path, replacement} {
				if err := os.Remove(name); err != nil {
					t.Error("owned fixture removal failed after closure")
				}
			}
			if renamed {
				if err := os.Remove(moved); err != nil {
					t.Error("moved owned fixture removal failed after closure")
				}
			}
		})
	}
}
