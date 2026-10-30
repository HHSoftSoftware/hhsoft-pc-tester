package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

type smallInputInfo struct {
	size int64
	mode os.FileMode
	id   int
}

func (info *smallInputInfo) Name() string       { return "owned-input" }
func (info *smallInputInfo) Size() int64        { return info.size }
func (info *smallInputInfo) Mode() os.FileMode  { return info.mode }
func (info *smallInputInfo) ModTime() time.Time { return time.Unix(1, 0) }
func (info *smallInputInfo) IsDir() bool        { return info.mode.IsDir() }
func (info *smallInputInfo) Sys() any           { return nil }

type smallInputOwner struct {
	info                             os.FileInfo
	reader                           io.Reader
	statError, readError, closeError error
	reads, readBytes, closes         int
	readWithError                    bool
}

func (owner *smallInputOwner) Stat() (os.FileInfo, error) { return owner.info, owner.statError }
func (owner *smallInputOwner) Read(buffer []byte) (int, error) {
	owner.reads++
	if owner.closes != 0 {
		panic("read after close")
	}
	if owner.readError != nil {
		if owner.readWithError {
			n, _ := owner.reader.Read(buffer)
			owner.readBytes += n
			return n, owner.readError
		}
		return 0, owner.readError
	}
	n, err := owner.reader.Read(buffer)
	owner.readBytes += n
	return n, err
}
func (owner *smallInputOwner) Close() error {
	owner.closes++
	if owner.closes != 1 {
		panic("owner closed more than once")
	}
	return owner.closeError
}

var smallInputPrivateFailure = errors.New("synthetic-provider-private-detail")

func smallInputFixture(contents []byte) (*smallInputOwner, *smallInputOwner, retestInputOperations) {
	metadata := &smallInputOwner{info: &smallInputInfo{size: int64(len(contents)), id: 1}}
	readable := &smallInputOwner{info: &smallInputInfo{size: int64(len(contents)), id: 1}, reader: bytes.NewReader(contents)}
	ops := retestInputOperations{
		openMetadata: func(string) (retestInputMetadata, error) { return metadata, nil },
		openReadable: func(string, retestInputMetadata) (retestInputReadable, error) { return readable, nil },
		sameFile:     func(a, b os.FileInfo) bool { return a.(*smallInputInfo).id == b.(*smallInputInfo).id },
	}
	return metadata, readable, ops
}

func smallInputComposedReader(ops retestInputOperations) smallRegularReader {
	return func(path string, maximum int64) ([]byte, error) {
		return readSmallRegularFileWith(path, maximum, func(path string, maximum int64) (retestInputReadable, os.FileInfo, error) {
			return openSmallRegularInputWithOperations(path, maximum, ops)
		})
	}
}

func requireSmallInputFailure(t *testing.T, data []byte, err error, cleanup bool) {
	t.Helper()
	if data != nil || err == nil || errors.Is(err, errSmallRegularCleanup) != cleanup {
		t.Fatalf("data=%d error=%v cleanup=%t", len(data), err, cleanup)
	}
	if strings.Contains(err.Error(), smallInputPrivateFailure.Error()) {
		t.Fatal("provider diagnostic was disclosed")
	}
}

func TestSmallRegularReadCompletionAndBound(t *testing.T) {
	for _, test := range []struct {
		name                                    string
		contents                                string
		readFailure, closeFailure, partialError bool
		pass                                    bool
	}{
		{"exact", "abcdef", false, false, false, true},
		{"short", "abc", false, false, false, false},
		{"overflow", "abcdefg-extra-unread", false, false, false, false},
		{"read_error", "abcdef", true, false, false, false},
		{"read_with_bytes_error", "abcdef", true, false, true, false},
		{"close_error", "abcdef", false, true, false, false},
		{"read_and_close_error", "abcdef", true, true, false, false},
		{"short_and_close_error", "abc", false, true, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner := &smallInputOwner{info: &smallInputInfo{size: 6, id: 1}, reader: strings.NewReader(test.contents), readWithError: test.partialError}
			if test.readFailure {
				owner.readError = smallInputPrivateFailure
			}
			if test.closeFailure {
				owner.closeError = smallInputPrivateFailure
			}
			data, err := readSmallRegularFileWith("owned-input", 6, func(string, int64) (retestInputReadable, os.FileInfo, error) { return owner, owner.info, nil })
			if owner.closes != 1 || owner.readBytes > 7 {
				t.Fatalf("closes=%d consumed=%d", owner.closes, owner.readBytes)
			}
			if test.pass {
				if err != nil || string(data) != "abcdef" {
					t.Fatalf("data=%q error=%v", data, err)
				}
			} else {
				requireSmallInputFailure(t, data, err, test.closeFailure)
				if (test.readFailure || test.contents != "abcdef") && !errors.Is(err, errSmallRegularRead) {
					t.Fatal("read failure was lost behind cleanup")
				}
			}
		})
	}
}

func TestSmallRegularInvalidDependencyAndMetadata(t *testing.T) {
	for _, limit := range []int64{-1, 0, math.MaxInt64} {
		t.Run(map[int64]string{-1: "negative", 0: "zero", math.MaxInt64: "overflow"}[limit], func(t *testing.T) {
			calls := 0
			data, err := readSmallRegularFileWith("owned-input", limit, func(string, int64) (retestInputReadable, os.FileInfo, error) { calls++; return nil, nil, nil })
			requireSmallInputFailure(t, data, err, false)
			if calls != 0 {
				t.Fatal("invalid bound reached acquisition")
			}
		})
	}
	data, err := readSmallRegularFileWith("owned-input", 1, nil)
	requireSmallInputFailure(t, data, err, false)
	for _, name := range []string{"nil_owner", "typed_nil_owner", "nil_metadata", "typed_nil_metadata", "directory", "empty", "oversize", "owner_with_error"} {
		t.Run(name, func(t *testing.T) {
			owner := &smallInputOwner{info: &smallInputInfo{size: 1, id: 1}, reader: strings.NewReader("x")}
			var file retestInputReadable = owner
			var info os.FileInfo = owner.info
			var failure error
			switch name {
			case "nil_owner":
				file = nil
			case "typed_nil_owner":
				file = (*smallInputOwner)(nil)
			case "nil_metadata":
				info = nil
			case "typed_nil_metadata":
				info = (*smallInputInfo)(nil)
			case "directory":
				owner.info.(*smallInputInfo).mode = os.ModeDir
			case "empty":
				owner.info.(*smallInputInfo).size = 0
			case "oversize":
				owner.info.(*smallInputInfo).size = 2
			case "owner_with_error":
				failure = smallInputPrivateFailure
			}
			data, err := readSmallRegularFileWith("owned-input", 1, func(string, int64) (retestInputReadable, os.FileInfo, error) { return file, info, failure })
			requireSmallInputFailure(t, data, err, false)
			wantClose := 1
			if name == "nil_owner" || name == "typed_nil_owner" {
				wantClose = 0
			}
			if owner.closes != wantClose || owner.reads != 0 {
				t.Fatalf("closes=%d reads=%d", owner.closes, owner.reads)
			}
		})
	}
}

func TestSmallRegularPinnedOwnershipFailures(t *testing.T) {
	for _, name := range []string{"success", "metadata_open_error", "metadata_stat_error", "metadata_directory", "readable_open_error", "readable_stat_error", "changed_identity", "changed_size", "same_owner", "metadata_close_error", "readable_close_error", "both_close_error", "read_and_readable_close_error", "typed_nil_metadata", "typed_nil_readable"} {
		t.Run(name, func(t *testing.T) {
			metadata, readable, ops := smallInputFixture([]byte("abcdef"))
			metadataCloses, readableCloses, reads := 1, 1, false
			cleanup := false
			switch name {
			case "success":
				reads = true
			case "metadata_open_error":
				ops.openMetadata = func(string) (retestInputMetadata, error) { return metadata, smallInputPrivateFailure }
				readableCloses = 0
			case "metadata_stat_error":
				metadata.statError = smallInputPrivateFailure
				readableCloses = 0
			case "metadata_directory":
				metadata.info.(*smallInputInfo).mode = os.ModeDir
				readableCloses = 0
			case "readable_open_error":
				ops.openReadable = func(string, retestInputMetadata) (retestInputReadable, error) {
					return readable, smallInputPrivateFailure
				}
			case "readable_stat_error":
				readable.statError = smallInputPrivateFailure
			case "changed_identity":
				readable.info.(*smallInputInfo).id = 2
			case "changed_size":
				readable.info.(*smallInputInfo).size = 5
			case "same_owner":
				ops.openReadable = func(string, retestInputMetadata) (retestInputReadable, error) { return metadata, nil }
				readableCloses = 0
			case "metadata_close_error":
				metadata.closeError = smallInputPrivateFailure
				cleanup = true
			case "readable_close_error":
				readable.closeError = smallInputPrivateFailure
				cleanup = true
				reads = true
			case "both_close_error":
				metadata.closeError = smallInputPrivateFailure
				readable.closeError = smallInputPrivateFailure
				cleanup = true
			case "read_and_readable_close_error":
				readable.readError = smallInputPrivateFailure
				readable.closeError = smallInputPrivateFailure
				cleanup = true
				reads = true
			case "typed_nil_metadata":
				ops.openMetadata = func(string) (retestInputMetadata, error) { return (*smallInputOwner)(nil), nil }
				metadataCloses, readableCloses = 0, 0
			case "typed_nil_readable":
				ops.openReadable = func(string, retestInputMetadata) (retestInputReadable, error) { return (*smallInputOwner)(nil), nil }
				readableCloses = 0
			}
			data, err := smallInputComposedReader(ops)("owned-input", 6)
			if name == "success" {
				if err != nil || string(data) != "abcdef" {
					t.Fatalf("data=%q error=%v", data, err)
				}
			} else {
				requireSmallInputFailure(t, data, err, cleanup)
			}
			if metadata.closes != metadataCloses || readable.closes != readableCloses || (readable.reads > 0) != reads {
				t.Fatalf("metadata=%d readable=%d reads=%d", metadata.closes, readable.closes, readable.reads)
			}
			if metadata.reads != 0 {
				t.Fatal("metadata owner was used for data")
			}
		})
	}
}

func TestSmallRegularLegacyOwnershipFailures(t *testing.T) {
	for _, name := range []string{"success", "lstat_error", "symlink", "empty", "owner_with_error", "stat_error", "identity_changed", "size_changed", "open_cleanup_error", "read_cleanup_error"} {
		t.Run(name, func(t *testing.T) {
			before := &smallInputInfo{size: 6, id: 1}
			reader := &smallInputOwner{info: &smallInputInfo{size: 6, id: 1}, reader: strings.NewReader("abcdef")}
			opened := 0
			ops := smallRegularLegacyOperations{
				lstat:    func(string) (os.FileInfo, error) { return before, nil },
				open:     func(string) (retestInputReadable, error) { opened++; return reader, nil },
				sameFile: func(a, b os.FileInfo) bool { return a.(*smallInputInfo).id == b.(*smallInputInfo).id },
			}
			wantOpen, wantRead, cleanup := 1, false, false
			switch name {
			case "success":
				wantRead = true
			case "lstat_error":
				ops.lstat = func(string) (os.FileInfo, error) { return nil, smallInputPrivateFailure }
				wantOpen = 0
			case "symlink":
				before.mode = os.ModeSymlink
				wantOpen = 0
			case "empty":
				before.size = 0
				wantOpen = 0
			case "owner_with_error":
				ops.open = func(string) (retestInputReadable, error) { opened++; return reader, smallInputPrivateFailure }
			case "stat_error":
				reader.statError = smallInputPrivateFailure
			case "identity_changed":
				reader.info.(*smallInputInfo).id = 2
			case "size_changed":
				reader.info.(*smallInputInfo).size = 5
			case "open_cleanup_error":
				reader.statError, reader.closeError = smallInputPrivateFailure, smallInputPrivateFailure
				cleanup = true
			case "read_cleanup_error":
				reader.readError, reader.closeError = smallInputPrivateFailure, smallInputPrivateFailure
				wantRead, cleanup = true, true
			}
			data, err := readSmallRegularFileWith("owned-input", 6, func(path string, limit int64) (retestInputReadable, os.FileInfo, error) {
				return openSmallRegularInputLegacy(path, limit, ops)
			})
			if name == "success" {
				if err != nil || string(data) != "abcdef" {
					t.Fatalf("data=%q error=%v", data, err)
				}
			} else {
				requireSmallInputFailure(t, data, err, cleanup)
			}
			if opened != wantOpen || reader.closes != wantOpen || (reader.reads > 0) != wantRead {
				t.Fatalf("opened=%d closes=%d reads=%d", opened, reader.closes, reader.reads)
			}
		})
	}
}

func TestSmallRegularRealFilesAndExactBytes(t *testing.T) {
	for _, size := range []int{0, 1, 64 << 10, (64 << 10) + 17} {
		name := map[int]string{0: "empty", 1: "minimum", 64 << 10: "boundary", (64 << 10) + 17: "multiple_chunks"}[size]
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "document.json")
			want := bytes.Repeat([]byte{'x'}, size)
			if err := os.WriteFile(path, want, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readSmallRegularFile(path, int64(max(size, 1)))
			if size == 0 {
				requireSmallInputFailure(t, got, err, false)
			} else if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("bytes=%d error=%v", len(got), err)
			}
			if size > 1 {
				got, err = readSmallRegularFile(path, int64(size-1))
				requireSmallInputFailure(t, got, err, false)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal("owned document was not released:", err)
			}
		})
	}
	for _, path := range []string{t.TempDir(), filepath.Join(t.TempDir(), "absent")} {
		got, err := readSmallRegularFile(path, 4096)
		requireSmallInputFailure(t, got, err, false)
	}
}

func TestPlanSmallDocumentFailureStopsConsumers(t *testing.T) {
	for _, name := range []string{"read_error", "readable_close_error", "metadata_close_error", "read_and_close_error", "nil_reader"} {
		t.Run(name, func(t *testing.T) {
			contents := []byte(`{"excluded_tests":[]}`)
			metadata, readable, ops := smallInputFixture(contents)
			switch name {
			case "read_error":
				readable.readError = smallInputPrivateFailure
			case "readable_close_error":
				readable.closeError = smallInputPrivateFailure
			case "metadata_close_error":
				metadata.closeError = smallInputPrivateFailure
			case "read_and_close_error":
				readable.readError, readable.closeError = smallInputPrivateFailure, smallInputPrivateFailure
			}
			read := smallInputComposedReader(ops)
			if name == "nil_reader" {
				read = nil
			}
			keys, collects, outputs := 0, 0, 0
			var stdout, stderr bytes.Buffer
			code := runPlanWithDocumentInput(t.Context(), []string{"-fingerprint-key", "unused-key", "-selection-file", "owned-selection", "-output", "unused-output"}, &stdout, &stderr,
				func(context.Context) (inventory.Snapshot, error) { collects++; return inventory.NewSnapshot(nil), nil },
				func(string, io.Writer, func(io.Writer) error) error { outputs++; return nil },
				func(context.Context, string, retestInputKind) ([]byte, error) {
					keys++
					return bytes.Repeat([]byte{0x25}, 32), nil
				}, read)
			if code != 1 || keys != 0 || collects != 0 || outputs != 0 || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "read quick-test selection:") || strings.Contains(stderr.String(), smallInputPrivateFailure.Error()) {
				t.Fatalf("code=%d keys=%d collects=%d outputs=%d stderr=%s", code, keys, collects, outputs, stderr.String())
			}
			if name != "nil_reader" && (metadata.closes != 1 || readable.closes != 1) {
				t.Fatal("failed command retained an input owner")
			}
		})
	}
}

func TestPlanSmallDocumentSuccessAndUsagePrecedence(t *testing.T) {
	key := bytes.Repeat([]byte{0x25}, 32)
	collect := func(context.Context) (inventory.Snapshot, error) { return inventory.NewSnapshot(nil), nil }
	readKey := func(context.Context, string, retestInputKind) ([]byte, error) { return append([]byte{}, key...), nil }
	arguments := []string{"-fingerprint-key", "owned-key", "-architecture", "x86_64"}
	var baseline, selected, stderr bytes.Buffer
	if code := runPlanWithInput(t.Context(), arguments, &baseline, &stderr, collect, writeDestination, readKey); code != 0 {
		t.Fatal(code, stderr.String())
	}
	metadata, readable, ops := smallInputFixture([]byte(`{"excluded_tests":[]}`))
	reader := smallInputComposedReader(ops)
	called := 0
	readSelection := func(path string, maximum int64) ([]byte, error) {
		called++
		if path != "owned-selection" || maximum != plan.MaximumSelectionBytes {
			t.Fatal("selection bound or path changed")
		}
		return reader(path, maximum)
	}
	if code := runPlanWithDocumentInput(t.Context(), append(arguments, "-selection-file", "owned-selection"), &selected, &stderr, collect, writeDestination, readKey, readSelection); code != 0 || !bytes.Equal(baseline.Bytes(), selected.Bytes()) || called != 1 || metadata.closes != 1 || readable.closes != 1 {
		t.Fatalf("code=%d same=%t calls=%d stderr=%s", code, bytes.Equal(baseline.Bytes(), selected.Bytes()), called, stderr.String())
	}
	for _, args := range [][]string{{"-selection-file", "owned-selection"}, {"-fingerprint-key", "owned-key", "stray", "-selection-file", "owned-selection"}} {
		var output, diagnostic bytes.Buffer
		code := runPlanWithDocumentInput(t.Context(), args, &output, &diagnostic, collect, writeDestination, readKey,
			func(string, int64) ([]byte, error) { t.Fatal("usage failure reached input"); return nil, nil })
		if code != 2 || output.Len() != 0 {
			t.Fatal("usage precedence changed")
		}
	}
}

func TestPlanSmallDocumentPreservesLaunchSelectionRouting(t *testing.T) {
	for _, mode := range []string{"quick", "standard", "empty_launch", "both", "both_legacy_empty", "malformed_launch"} {
		t.Run(mode, func(t *testing.T) {
			h := newLaunchPlanHarness(t)
			wantCode, wantEvents := 0, "launch,key,collect,output"
			wantProfile := plan.ProfileQuick
			switch mode {
			case "standard":
				h.arguments = append(h.arguments, "-profile", "standard")
				h.launch = []byte("{\"schema_version\":1,\"profile\":\"standard\",\"architecture\":\"x86_64\",\"excluded_tests\":[]}")
				wantProfile = plan.ProfileStandard
			case "empty_launch":
				h.arguments[5] = ""
				wantCode, wantEvents = 2, ""
			case "both":
				h.arguments = append(h.arguments, "-selection-file", "unused-selection")
				wantCode, wantEvents = 2, ""
			case "both_legacy_empty":
				h.arguments = append(h.arguments, "-selection-file", "")
				wantCode, wantEvents = 2, ""
			case "malformed_launch":
				h.launch = []byte("{")
				wantCode, wantEvents = 1, "launch"
			}
			code := runPlanWithDocumentInput(t.Context(), h.arguments, &h.stdout, &h.stderr, h.collect, h.write, h.read,
				func(string, int64) ([]byte, error) {
					t.Fatal("launch selection reached the legacy document reader")
					return nil, errSmallRegularInput
				})
			if code != wantCode || strings.Join(h.events, ",") != wantEvents {
				t.Fatalf("code=%d events=%v", code, h.events)
			}
			if wantCode == 0 {
				value, err := plan.Decode(h.stdout.Bytes())
				if err != nil || h.stderr.Len() != 0 || value.Profile != wantProfile || value.Architecture != "x86_64" || len(value.ExcludedTests) != 0 {
					t.Fatal("checked launch target changed")
				}
			} else if h.stdout.Len() != 0 {
				t.Fatal("rejected launch selection emitted a plan")
			}
		})
	}
}
