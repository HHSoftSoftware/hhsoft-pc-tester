package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type outputFaultFile struct {
	bytes.Buffer
	limit      int
	shortFinal bool
	requested  int
	writeErr   error
	closeErr   error
	writes     int
	closes     int
}

func (file *outputFaultFile) Write(contents []byte) (int, error) {
	file.writes++
	file.requested = len(contents)
	written := len(contents)
	if file.limit >= 0 && written > file.limit {
		written = file.limit
	}
	if file.shortFinal && written > 0 {
		written--
	}
	_, _ = file.Buffer.Write(contents[:written])
	return written, file.writeErr
}

func (file *outputFaultFile) Close() error {
	file.closes++
	return file.closeErr
}

func TestDestinationCompletionPreservesWriteAndCloseFailures(t *testing.T) {
	writeFault := errors.New("injected write failure")
	closeFault := errors.New("injected close failure")
	for _, test := range []struct {
		name                          string
		limit                         int
		writeErr, closeErr, wantWrite error
	}{
		{"success", -1, nil, nil, nil},
		{"write", 0, writeFault, nil, writeFault},
		{"partial-write", 3, writeFault, nil, writeFault},
		{"full-write-with-error", -1, writeFault, nil, writeFault},
		{"short-write", 3, nil, nil, io.ErrShortWrite},
		{"zero-write", 0, nil, nil, io.ErrShortWrite},
		{"close", -1, nil, closeFault, nil},
		{"write-and-close", 3, writeFault, closeFault, writeFault},
		{"short-and-close", 3, nil, closeFault, io.ErrShortWrite},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := &outputFaultFile{limit: test.limit, writeErr: test.writeErr, closeErr: test.closeErr}
			opens := 0
			open := func(path string, flags int, mode os.FileMode) (io.WriteCloser, error) {
				opens++
				if path != "owned.json" || flags != os.O_WRONLY|os.O_CREATE|os.O_EXCL || mode != 0600 {
					t.Fatalf("creation changed: %q flags=%d mode=%o", path, flags, mode)
				}
				return file, nil
			}
			err := writeDestinationWith("owned.json", io.Discard, func(destination io.Writer) error {
				return json.NewEncoder(destination).Encode(map[string]int{"value": 7})
			}, open)
			if opens != 1 || file.writes != 1 || file.closes != 1 {
				t.Fatalf("ownership counts open=%d write=%d close=%d", opens, file.writes, file.closes)
			}
			if (test.wantWrite == nil && test.closeErr == nil) != (err == nil) ||
				(test.wantWrite != nil && !errors.Is(err, test.wantWrite)) ||
				(test.closeErr != nil && !errors.Is(err, test.closeErr)) {
				t.Fatalf("lost completion failure: %v", err)
			}
			if test.limit >= 0 && file.Len() != test.limit {
				t.Fatalf("partial output disappeared or was retried: %q", file.Bytes())
			}
		})
	}
}

func TestDestinationOpenFailureNeverWritesAndClosesAcquiredHandle(t *testing.T) {
	openFault := errors.New("injected open failure")
	closeFault := errors.New("injected close failure")
	for _, acquired := range []bool{false, true} {
		file := &outputFaultFile{limit: -1, closeErr: closeFault}
		called := false
		err := writeDestinationWith("owned", io.Discard, func(io.Writer) error { called = true; return nil },
			func(string, int, os.FileMode) (io.WriteCloser, error) {
				if acquired {
					return file, openFault
				}
				return nil, openFault
			})
		wantCloses := 0
		if acquired {
			wantCloses = 1
		}
		if called || file.writes != 0 || file.closes != wantCloses || !errors.Is(err, openFault) || errors.Is(err, closeFault) != acquired {
			t.Fatalf("open ownership acquired=%v close=%d err=%v", acquired, file.closes, err)
		}
	}
	called := false
	if err := writeDestinationWith("owned", io.Discard, func(io.Writer) error { called = true; return nil },
		func(string, int, os.FileMode) (io.WriteCloser, error) { return nil, nil }); err == nil || called {
		t.Fatal("missing owned destination was accepted")
	}
}

func TestDestinationEncodeFailureAndPanicStillCloseOnce(t *testing.T) {
	closeFault := errors.New("injected close failure")
	file := &outputFaultFile{limit: -1, closeErr: closeFault}
	open := func(string, int, os.FileMode) (io.WriteCloser, error) { return file, nil }
	err := writeDestinationWith("owned", io.Discard, func(w io.Writer) error {
		return json.NewEncoder(w).Encode(make(chan int))
	}, open)
	var unsupported *json.UnsupportedTypeError
	if !errors.As(err, &unsupported) || !errors.Is(err, closeFault) || file.writes != 0 || file.closes != 1 {
		t.Fatalf("encoding/close failure lost: %v writes=%d closes=%d", err, file.writes, file.closes)
	}
	file = &outputFaultFile{limit: -1}
	marker := errors.New("callback panic")
	func() {
		defer func() {
			if recover() != marker {
				t.Fatal("callback panic changed")
			}
		}()
		_ = writeDestinationWith("owned", io.Discard, func(io.Writer) error { panic(marker) }, open)
	}()
	if file.closes != 1 {
		t.Fatalf("panic close count=%d", file.closes)
	}
}

func TestDestinationNeverClosesCallerOwnedStdout(t *testing.T) {
	writeFault := errors.New("stdout write failure")
	for _, test := range []struct {
		limit int
		err   error
	}{{-1, nil}, {0, nil}, {2, writeFault}} {
		stdout := &outputFaultFile{limit: test.limit, writeErr: test.err, closeErr: errors.New("must not close stdout")}
		err := writeDestinationWith("-", stdout, func(w io.Writer) error { return writeAll(w, []byte("stdout\n")) },
			func(string, int, os.FileMode) (io.WriteCloser, error) {
				t.Fatal("stdout opened a file")
				return nil, nil
			})
		if stdout.closes != 0 || stdout.writes != 1 || (test.limit == -1) != (err == nil) {
			t.Fatalf("stdout ownership err=%v closes=%d writes=%d", err, stdout.closes, stdout.writes)
		}
		if test.err != nil && !errors.Is(err, test.err) {
			t.Fatal("stdout error changed")
		}
		if test.limit == 0 && !errors.Is(err, io.ErrShortWrite) {
			t.Fatal("zero stdout write accepted")
		}
	}
}

func TestDestinationFinalNewlineShortWriteFails(t *testing.T) {
	file := &outputFaultFile{limit: len("{\"a\":1}")}
	err := writeDestinationWith("owned", io.Discard, func(w io.Writer) error {
		return json.NewEncoder(w).Encode(map[string]int{"a": 1})
	}, func(string, int, os.FileMode) (io.WriteCloser, error) { return file, nil })
	if !errors.Is(err, io.ErrShortWrite) || file.String() != "{\"a\":1}" || file.closes != 1 {
		t.Fatalf("missing final newline was accepted: %q %v", file.String(), err)
	}
}

func TestDestinationExclusiveCreationAndPartialOutputRetention(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "result.json")
	if err := writeDestination(path, io.Discard, func(w io.Writer) error { return writeAll(w, []byte("original\n")) }); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	called := false
	if err := writeDestination(path, io.Discard, func(io.Writer) error { called = true; return nil }); !errors.Is(err, os.ErrExist) || called {
		t.Fatalf("existing file replaced: %v callback=%v", err, called)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "original\n" {
		t.Fatalf("existing bytes changed: %q %v", contents, err)
	}
	partial := filepath.Join(directory, "partial.json")
	failure := errors.New("serialization stopped")
	err = writeDestination(partial, io.Discard, func(w io.Writer) error {
		if err := writeAll(w, []byte("partial")); err != nil {
			return err
		}
		return failure
	})
	contents, readErr := os.ReadFile(partial)
	if !errors.Is(err, failure) || readErr != nil || string(contents) != "partial" {
		t.Fatalf("partial output lost: %q write=%v read=%v", contents, err, readErr)
	}
	if err := os.Remove(partial); err != nil {
		t.Fatalf("destination was not closed: %v", err)
	}
	if err := writeDestination(filepath.Join(directory, "missing", "file"), io.Discard, func(io.Writer) error {
		t.Fatal("failed open reached callback")
		return nil
	}); err == nil {
		t.Fatal("missing parent opened")
	}
}
