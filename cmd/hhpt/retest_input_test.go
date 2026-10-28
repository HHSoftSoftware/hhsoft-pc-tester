package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type retestInputTestInfo struct {
	id       int
	size     int64
	mode     os.FileMode
	modified time.Time
}

func (info *retestInputTestInfo) Name() string       { return "fixture" }
func (info *retestInputTestInfo) Size() int64        { return info.size }
func (info *retestInputTestInfo) Mode() os.FileMode  { return info.mode }
func (info *retestInputTestInfo) ModTime() time.Time { return info.modified }
func (info *retestInputTestInfo) IsDir() bool        { return info.mode.IsDir() }
func (info *retestInputTestInfo) Sys() any           { return info.id }

type retestInputTestStat struct {
	info os.FileInfo
	err  error
}

type retestInputTestRead struct {
	n   int
	err error
}

type retestInputTestLedger struct {
	acquired map[string]int
	closed   map[string]int
	calls    []string
	closes   []string
}

type retestInputTestFile struct {
	label    string
	ledger   *retestInputTestLedger
	info     *retestInputTestInfo
	stats    []retestInputTestStat
	reads    []retestInputTestRead
	data     []byte
	offset   int
	readSize []int
	closeErr error
	hook     func(string)
}

func (file *retestInputTestFile) call(operation string) {
	file.ledger.calls = append(file.ledger.calls, file.label+"."+operation)
	if file.hook != nil {
		file.hook(operation)
	}
}

func (file *retestInputTestFile) Stat() (os.FileInfo, error) {
	file.call("stat")
	if len(file.stats) != 0 {
		result := file.stats[0]
		file.stats = file.stats[1:]
		return result.info, result.err
	}
	return file.info, nil
}

func (file *retestInputTestFile) Read(data []byte) (int, error) {
	file.call("read")
	file.readSize = append(file.readSize, len(data))
	if len(file.reads) != 0 {
		result := file.reads[0]
		file.reads = file.reads[1:]
		if result.n > 0 {
			n := result.n
			if n > len(data) {
				n = len(data)
			}
			for i := 0; i < n; i++ {
				data[i] = byte((file.offset + i) % 251)
			}
			file.offset += n
		}
		return result.n, result.err
	}
	if file.offset == len(file.data) {
		return 0, io.EOF
	}
	n := copy(data, file.data[file.offset:])
	file.offset += n
	return n, nil
}

func (file *retestInputTestFile) Close() error {
	file.call("close")
	file.ledger.closed[file.label]++
	file.ledger.closes = append(file.ledger.closes, file.label)
	return file.closeErr
}

type retestInputFixture struct {
	ledger       *retestInputTestLedger
	metadata     *retestInputTestFile
	readable     *retestInputTestFile
	metadataOut  retestInputMetadata
	readableOut  retestInputReadable
	metadataErr  error
	readableErr  error
	metadataHook func()
	readableHook func()
}

func newRetestInputFixture(size int) *retestInputFixture {
	ledger := &retestInputTestLedger{acquired: map[string]int{}, closed: map[string]int{}}
	info := &retestInputTestInfo{id: 7, size: int64(size), mode: 0600, modified: time.Unix(123, 456)}
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251)
	}
	metadata := &retestInputTestFile{label: "metadata", ledger: ledger, info: info}
	readable := &retestInputTestFile{label: "readable", ledger: ledger, info: info, data: data}
	return &retestInputFixture{ledger: ledger, metadata: metadata, readable: readable, metadataOut: metadata, readableOut: readable}
}

func (fixture *retestInputFixture) operations() retestInputOperations {
	return retestInputOperations{
		openMetadata: func(string) (retestInputMetadata, error) {
			fixture.ledger.calls = append(fixture.ledger.calls, "open.metadata")
			if fixture.metadataHook != nil {
				fixture.metadataHook()
			}
			if owner, ok := fixture.metadataOut.(*retestInputTestFile); ok && owner != nil {
				fixture.ledger.acquired[owner.label]++
			}
			return fixture.metadataOut, fixture.metadataErr
		},
		openReadable: func(_ string, owner retestInputMetadata) (retestInputReadable, error) {
			fixture.ledger.calls = append(fixture.ledger.calls, "open.readable")
			if owner != fixture.metadataOut {
				panic("incorrect metadata owner")
			}
			if fixture.readableHook != nil {
				fixture.readableHook()
			}
			if owner, ok := fixture.readableOut.(*retestInputTestFile); ok && owner != nil && owner != fixture.metadata {
				fixture.ledger.acquired[owner.label]++
			}
			return fixture.readableOut, fixture.readableErr
		},
		sameFile: func(a, b os.FileInfo) bool {
			x, xok := a.(*retestInputTestInfo)
			y, yok := b.(*retestInputTestInfo)
			return xok && yok && x.id == y.id
		},
	}
}

func assertRetestInputLedger(t *testing.T, fixture *retestInputFixture, acquired map[string]int, closes []string) {
	t.Helper()
	if !reflect.DeepEqual(fixture.ledger.acquired, acquired) || !reflect.DeepEqual(fixture.ledger.closed, acquired) || !reflect.DeepEqual(fixture.ledger.closes, closes) {
		t.Fatalf("ownership acquired=%v closed=%v order=%v; want %v / %v", fixture.ledger.acquired, fixture.ledger.closed, fixture.ledger.closes, acquired, closes)
	}
}

func assertRetestInputFailure(t *testing.T, data []byte, err error, cleanup bool) {
	t.Helper()
	if data != nil || !errors.Is(err, errRetestInput) || errors.Is(err, errRetestInputCleanup) != cleanup {
		t.Fatalf("data=%v error=%v cleanup=%v", data != nil, err, cleanup)
	}
}

func TestRetestInputAdmissionBeforeAcquisition(t *testing.T) {
	var typedNil *retestInputStoppedContext
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name string
		ctx  context.Context
		kind retestInputKind
		err  error
	}{
		{"nil_context", nil, retestArtifactInput, errRetestInput},
		{"typed_nil_context", typedNil, retestArtifactInput, errRetestInput},
		{"invalid_kind", context.Background(), retestInputKind(255), errRetestInput},
		{"cancelled", stopped, retestArtifactInput, context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRetestInputFixture(32)
			data, err := readRetestInputWithOperations(test.ctx, "sensitive-path", test.kind, fixture.operations())
			if data != nil || err != test.err || len(fixture.ledger.calls) != 0 {
				t.Fatalf("data=%v error=%v calls=%v", data != nil, err, fixture.ledger.calls)
			}
			assertRetestInputLedger(t, fixture, map[string]int{}, nil)
		})
	}
	for _, missing := range []string{"metadata", "readable", "identity"} {
		t.Run(missing, func(t *testing.T) {
			fixture := newRetestInputFixture(32)
			ops := fixture.operations()
			switch missing {
			case "metadata":
				ops.openMetadata = nil
			case "readable":
				ops.openReadable = nil
			case "identity":
				ops.sameFile = nil
			}
			data, err := readRetestInputWithOperations(context.Background(), "fixture", retestArtifactInput, ops)
			assertRetestInputFailure(t, data, err, false)
			if len(fixture.ledger.calls) != 0 {
				t.Fatal(fixture.ledger.calls)
			}
		})
	}
}

type retestInputStoppedContext struct{ context.Context }

func TestRetestInputExactCompletionAndOwnedBytes(t *testing.T) {
	for _, test := range []struct {
		name  string
		size  int
		kind  retestInputKind
		reads []retestInputTestRead
	}{
		{"minimum_artifact", 22, retestArtifactInput, nil},
		{"exact_key", 32, retestPublicKeyInput, nil},
		{"final_bytes_and_eof", 32, retestArtifactInput, []retestInputTestRead{{32, io.EOF}}},
		{"partial_progress", 32, retestArtifactInput, []retestInputTestRead{{7, nil}, {25, io.EOF}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRetestInputFixture(test.size)
			fixture.readable.reads = test.reads
			data, err := readRetestInputWithOperations(context.Background(), "fixture", test.kind, fixture.operations())
			if err != nil || !bytes.Equal(data, fixture.readable.data) {
				t.Fatalf("length=%d error=%v", len(data), err)
			}
			fixture.readable.data[0] ^= 0xff
			if data[0] == fixture.readable.data[0] {
				t.Fatal("returned bytes alias the provider")
			}
			assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			if len(fixture.readable.readSize) < 2 || fixture.readable.readSize[len(fixture.readable.readSize)-1] != 1 {
				t.Fatal("successful completion omitted the overflow probe")
			}
			want := []string{"open.metadata", "metadata.stat", "open.readable", "readable.stat"}
			if !reflect.DeepEqual(fixture.ledger.calls[:4], want) {
				t.Fatal(fixture.ledger.calls)
			}
		})
	}
}

func TestRetestInputAcquisitionOwnership(t *testing.T) {
	private := errors.New("private-provider-path-and-key")
	for _, stage := range []string{"metadata", "readable"} {
		for _, shape := range []string{"nil", "nil_error", "typed_nil", "typed_nil_error", "handle_error"} {
			t.Run(stage+"/"+shape, func(t *testing.T) {
				fixture := newRetestInputFixture(32)
				var out *retestInputTestFile
				var failure error
				if strings.HasSuffix(shape, "error") {
					failure = private
				}
				if stage == "metadata" {
					fixture.metadataErr = failure
					switch shape {
					case "nil", "nil_error":
						fixture.metadataOut = nil
					case "typed_nil", "typed_nil_error":
						fixture.metadataOut = out
					}
				} else {
					fixture.readableErr = failure
					switch shape {
					case "nil", "nil_error":
						fixture.readableOut = nil
					case "typed_nil", "typed_nil_error":
						fixture.readableOut = out
					}
				}
				data, err := readRetestInputWithOperations(context.Background(), "private-path", retestArtifactInput, fixture.operations())
				assertRetestInputFailure(t, data, err, false)
				if strings.Contains(err.Error(), "private") || errors.Is(err, private) {
					t.Fatal("provider diagnostics escaped")
				}
				want := map[string]int{}
				var closes []string
				if stage == "readable" || shape == "handle_error" {
					want["metadata"] = 1
					closes = []string{"metadata"}
				}
				if stage == "readable" && shape == "handle_error" {
					want["readable"] = 1
					closes = []string{"readable", "metadata"}
				}
				assertRetestInputLedger(t, fixture, want, closes)
				for _, call := range fixture.ledger.calls {
					if call == "readable.stat" || call == "readable.read" || (stage == "metadata" && call == "open.readable") {
						t.Fatalf("later work: %v", fixture.ledger.calls)
					}
				}
			})
		}
	}
	t.Run("aliased_owner", func(t *testing.T) {
		fixture := newRetestInputFixture(32)
		fixture.readableOut = fixture.metadata
		data, err := readRetestInputWithOperations(context.Background(), "fixture", retestArtifactInput, fixture.operations())
		assertRetestInputFailure(t, data, err, false)
		assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1}, []string{"metadata"})
	})
}

func TestRetestInputMetadataBeforeReadableOpen(t *testing.T) {
	for _, test := range []struct {
		name string
		mode os.FileMode
		size int64
		kind retestInputKind
	}{
		{"directory", os.ModeDir, 32, retestArtifactInput},
		{"symlink", os.ModeSymlink, 32, retestArtifactInput},
		{"fifo", os.ModeNamedPipe, 32, retestArtifactInput},
		{"device", os.ModeDevice | os.ModeCharDevice, 32, retestArtifactInput},
		{"negative", 0600, -1, retestArtifactInput},
		{"short_artifact", 0600, 21, retestArtifactInput},
		{"oversize_artifact", 0600, (64 << 20) + 1, retestArtifactInput},
		{"short_key", 0600, 31, retestPublicKeyInput},
		{"long_key", 0600, 33, retestPublicKeyInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRetestInputFixture(32)
			fixture.metadata.info.mode, fixture.metadata.info.size = test.mode, test.size
			data, err := readRetestInputWithOperations(context.Background(), "fixture", test.kind, fixture.operations())
			assertRetestInputFailure(t, data, err, false)
			assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1}, []string{"metadata"})
			if !reflect.DeepEqual(fixture.ledger.calls, []string{"open.metadata", "metadata.stat", "metadata.close"}) {
				t.Fatal(fixture.ledger.calls)
			}
		})
	}
	for _, shape := range []string{"nil", "nil_error", "typed_nil", "typed_nil_error", "info_error"} {
		t.Run(shape, func(t *testing.T) {
			fixture := newRetestInputFixture(32)
			var typedNil *retestInputTestInfo
			result := retestInputTestStat{}
			if strings.HasSuffix(shape, "error") {
				result.err = errors.New("private-stat")
			}
			if strings.HasPrefix(shape, "typed") {
				result.info = typedNil
			} else if shape == "info_error" {
				result.info = fixture.metadata.info
			}
			fixture.metadata.stats = []retestInputTestStat{result}
			data, err := readRetestInputWithOperations(context.Background(), "fixture", retestArtifactInput, fixture.operations())
			assertRetestInputFailure(t, data, err, false)
			assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1}, []string{"metadata"})
		})
	}
}

func TestRetestInputDescriptorAndFinalStability(t *testing.T) {
	for _, stage := range []string{"opened", "readable_final", "metadata_final"} {
		for _, field := range []string{"identity", "mode", "size", "modified", "error"} {
			t.Run(stage+"/"+field, func(t *testing.T) {
				fixture := newRetestInputFixture(32)
				changed := *fixture.metadata.info
				result := retestInputTestStat{info: &changed}
				switch field {
				case "identity":
					changed.id++
				case "mode":
					changed.mode = 0644
				case "size":
					changed.size++
				case "modified":
					changed.modified = changed.modified.Add(time.Nanosecond)
				case "error":
					result.info, result.err = nil, errors.New("private-final-stat")
				}
				good := retestInputTestStat{info: fixture.metadata.info}
				switch stage {
				case "opened":
					fixture.readable.stats = []retestInputTestStat{result}
				case "readable_final":
					fixture.readable.stats = []retestInputTestStat{good, result}
				case "metadata_final":
					fixture.metadata.stats = []retestInputTestStat{good, result}
				}
				data, err := readRetestInputWithOperations(context.Background(), "fixture", retestArtifactInput, fixture.operations())
				assertRetestInputFailure(t, data, err, false)
				assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				if stage == "opened" && len(fixture.readable.readSize) != 0 {
					t.Fatal("read before descriptor proof")
				}
				metadataStats := 0
				for _, call := range fixture.ledger.calls {
					if call == "metadata.stat" {
						metadataStats++
					}
				}
				if stage != "metadata_final" && metadataStats != 1 {
					t.Fatal("later metadata work after descriptor failure")
				}
			})
		}
	}
}

func TestRetestInputReadProtocol(t *testing.T) {
	private := errors.New("private-read-marker")
	for _, test := range []struct {
		name  string
		reads []retestInputTestRead
	}{
		{"negative", []retestInputTestRead{{-1, private}}},
		{"excessive", []retestInputTestRead{{33, io.EOF}}},
		{"no_progress", []retestInputTestRead{{0, nil}}},
		{"short_eof", []retestInputTestRead{{0, io.EOF}}},
		{"partial_eof", []retestInputTestRead{{31, io.EOF}}},
		{"partial_hard", []retestInputTestRead{{5, private}}},
		{"complete_hard", []retestInputTestRead{{32, private}}},
		{"wrapped_eof", []retestInputTestRead{{32, fmt.Errorf("private-wrap: %w", io.EOF)}}},
		{"joined_eof", []retestInputTestRead{{32, errors.Join(io.EOF, private)}}},
		{"live_cancel", []retestInputTestRead{{0, context.Canceled}}},
		{"wrapped_cancel", []retestInputTestRead{{0, fmt.Errorf("private-wrap: %w", context.Canceled)}}},
		{"probe_progress", []retestInputTestRead{{32, nil}, {1, io.EOF}}},
		{"probe_excessive", []retestInputTestRead{{32, nil}, {2, io.EOF}}},
		{"probe_negative", []retestInputTestRead{{32, nil}, {-1, private}}},
		{"probe_no_progress", []retestInputTestRead{{32, nil}, {0, nil}}},
		{"probe_hard", []retestInputTestRead{{32, nil}, {0, private}}},
		{"probe_joined_eof", []retestInputTestRead{{32, nil}, {0, errors.Join(io.EOF, private)}}},
		{"final_eof_then_extra_byte", []retestInputTestRead{{32, io.EOF}, {1, io.EOF}}},
		{"final_eof_then_hard", []retestInputTestRead{{32, io.EOF}, {0, private}}},
		{"final_eof_then_no_progress", []retestInputTestRead{{32, io.EOF}, {0, nil}}},
		{"final_eof_then_joined_eof", []retestInputTestRead{{32, io.EOF}, {0, errors.Join(io.EOF, private)}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRetestInputFixture(32)
			fixture.readable.reads = test.reads
			data, err := readRetestInputWithOperations(context.Background(), "private-path", retestArtifactInput, fixture.operations())
			assertRetestInputFailure(t, data, err, false)
			assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			if strings.Contains(err.Error(), "private") || errors.Is(err, private) {
				t.Fatal("raw error retained")
			}
			for _, call := range fixture.ledger.calls[4:] {
				if call == "metadata.stat" || call == "readable.stat" {
					t.Fatalf("later metadata work after read failure: %v", fixture.ledger.calls)
				}
			}
		})
	}
}

func TestRetestInputCancellationBoundaries(t *testing.T) {
	for _, stage := range []string{"metadata_open", "metadata_stat", "readable_open", "readable_stat", "last_data", "terminal_eof", "readable_final", "metadata_final", "readable_close", "metadata_close"} {
		t.Run(stage, func(t *testing.T) {
			fixture := newRetestInputFixture(32)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			metaStats, readStats, reads := 0, 0, 0
			fixture.metadataHook = func() {
				if stage == "metadata_open" {
					cancel()
				}
			}
			fixture.readableHook = func() {
				if stage == "readable_open" {
					cancel()
				}
			}
			fixture.metadata.hook = func(operation string) {
				if operation == "stat" {
					metaStats++
				}
				if (stage == "metadata_stat" && operation == "stat" && metaStats == 1) || (stage == "metadata_final" && operation == "stat" && metaStats == 2) || (stage == "metadata_close" && operation == "close") {
					cancel()
				}
			}
			fixture.readable.hook = func(operation string) {
				if operation == "stat" {
					readStats++
				}
				if operation == "read" {
					reads++
				}
				if (stage == "readable_stat" && operation == "stat" && readStats == 1) || (stage == "readable_final" && operation == "stat" && readStats == 2) || (stage == "last_data" && operation == "read" && reads == 1) || (stage == "terminal_eof" && operation == "read" && reads == 2) || (stage == "readable_close" && operation == "close") {
					cancel()
				}
			}
			data, err := readRetestInputWithOperations(ctx, "fixture", retestArtifactInput, fixture.operations())
			if data != nil || err != context.Canceled {
				t.Fatalf("data=%v error=%v", data != nil, err)
			}
			want := map[string]int{"metadata": 1}
			closes := []string{"metadata"}
			if stage != "metadata_open" && stage != "metadata_stat" {
				want["readable"] = 1
				closes = []string{"readable", "metadata"}
			}
			assertRetestInputLedger(t, fixture, want, closes)
			if (stage == "last_data" && reads != 1) || (stage == "readable_stat" && reads != 0) || (stage == "readable_final" && metaStats != 1) {
				t.Fatalf("later work after cancellation: %v", fixture.ledger.calls)
			}
		})
	}
}

func TestRetestInputHardFaultAndCancellationPrecedence(t *testing.T) {
	for _, test := range []struct {
		name string
		n    int
		err  error
		hard bool
	}{
		{"sole_cancel", 0, context.Canceled, false},
		{"cancel_with_bytes", 1, context.Canceled, true},
		{"negative_cancel", -1, context.Canceled, true},
		{"oversize_cancel", 33, context.Canceled, true},
		{"joined_cancel", 0, errors.Join(context.Canceled, errors.New("private-hard")), true},
		{"masked_cancel", 0, fmt.Errorf("private-wrap: %w", context.Canceled), true},
		{"hard", 0, errors.New("private-hard"), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRetestInputFixture(32)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fixture.readable.hook = func(operation string) {
				if operation == "read" {
					cancel()
				}
			}
			fixture.readable.reads = []retestInputTestRead{{test.n, test.err}}
			data, err := readRetestInputWithOperations(ctx, "fixture", retestArtifactInput, fixture.operations())
			if test.hard {
				assertRetestInputFailure(t, data, err, false)
			} else if data != nil || err != context.Canceled {
				t.Fatalf("data=%v error=%v", data != nil, err)
			}
			assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
		})
	}
	t.Run("final_metadata_hard_plus_cancel", func(t *testing.T) {
		fixture := newRetestInputFixture(32)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		fixture.metadata.hook = func(operation string) {
			if operation == "stat" {
				calls++
				if calls == 2 {
					cancel()
				}
			}
		}
		fixture.metadata.stats = []retestInputTestStat{{info: fixture.metadata.info}, {err: errors.New("private-final")}}
		data, err := readRetestInputWithOperations(ctx, "fixture", retestArtifactInput, fixture.operations())
		assertRetestInputFailure(t, data, err, false)
		assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
	})
}

func TestRetestInputCleanupAndSafeErrors(t *testing.T) {
	for _, stage := range []string{"readable", "metadata", "both"} {
		for _, fault := range []string{"none", "read", "cancel", "close_cancel"} {
			t.Run(stage+"/"+fault, func(t *testing.T) {
				fixture := newRetestInputFixture(32)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				private := errors.New("private-close-provider-key-path")
				if stage != "metadata" {
					fixture.readable.closeErr = private
				}
				if stage != "readable" {
					fixture.metadata.closeErr = private
				}
				switch fault {
				case "read":
					fixture.readable.reads = []retestInputTestRead{{4, private}}
				case "cancel":
					fixture.readable.hook = func(operation string) {
						if operation == "read" {
							cancel()
						}
					}
					fixture.readable.reads = []retestInputTestRead{{0, context.Canceled}}
				case "close_cancel":
					fixture.readable.hook = func(operation string) {
						if operation == "close" {
							cancel()
						}
					}
				}
				data, err := readRetestInputWithOperations(ctx, "private-input-path", retestArtifactInput, fixture.operations())
				assertRetestInputFailure(t, data, err, true)
				assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				if strings.Contains(err.Error(), "private") || errors.Is(err, private) {
					t.Fatal("cleanup error leaked provider content")
				}
			})
		}
	}
	t.Run("metadata_rejection_and_close", func(t *testing.T) {
		fixture := newRetestInputFixture(32)
		fixture.metadata.info.mode = os.ModeNamedPipe
		fixture.metadata.closeErr = errors.New("private-close")
		data, err := readRetestInputWithOperations(context.Background(), "fixture", retestArtifactInput, fixture.operations())
		assertRetestInputFailure(t, data, err, true)
		assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1}, []string{"metadata"})
	})
}

func TestRetestInputRejectedOwnerCleanupAndCancellation(t *testing.T) {
	for _, stage := range []string{"metadata_open", "metadata_stat", "readable_open", "readable_stat"} {
		for _, cancellation := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancel_%t", stage, cancellation), func(t *testing.T) {
				fixture := newRetestInputFixture(32)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				private := errors.New("private-provider-marker")
				hook := func() {
					if cancellation {
						cancel()
					}
				}
				fixture.metadata.closeErr = private
				fixture.readable.closeErr = private
				switch stage {
				case "metadata_open":
					fixture.metadataHook, fixture.metadataErr = hook, private
				case "metadata_stat":
					fixture.metadata.stats = []retestInputTestStat{{err: private}}
					fixture.metadata.hook = func(operation string) {
						if operation == "stat" {
							hook()
						}
					}
				case "readable_open":
					fixture.readableHook, fixture.readableErr = hook, private
				case "readable_stat":
					fixture.readable.stats = []retestInputTestStat{{err: private}}
					fixture.readable.hook = func(operation string) {
						if operation == "stat" {
							hook()
						}
					}
				}
				data, err := readRetestInputWithOperations(ctx, "private-path", retestArtifactInput, fixture.operations())
				assertRetestInputFailure(t, data, err, true)
				if strings.Contains(err.Error(), "private") || len(fixture.readable.readSize) != 0 {
					t.Fatal("failure leaked provider content or reached reads")
				}
				want, closes := map[string]int{"metadata": 1}, []string{"metadata"}
				if strings.HasPrefix(stage, "readable") {
					want["readable"] = 1
					closes = []string{"readable", "metadata"}
				}
				assertRetestInputLedger(t, fixture, want, closes)
			})
		}
	}
}

type retestInputDeadlineContext struct {
	context.Context
	stopped bool
}

func (ctx *retestInputDeadlineContext) Err() error {
	if ctx.stopped {
		return context.DeadlineExceeded
	}
	return nil
}

func TestRetestInputExactDeadlineAndMetadataErrorShape(t *testing.T) {
	for _, test := range []struct {
		name string
		n    int
		err  error
		hard bool
	}{
		{"sole_deadline", 0, context.DeadlineExceeded, false},
		{"deadline_with_bytes", 1, context.DeadlineExceeded, true},
		{"deadline_bad_count", -1, context.DeadlineExceeded, true},
		{"joined_deadline", 0, errors.Join(context.DeadlineExceeded, errors.New("private-deadline")), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRetestInputFixture(32)
			ctx := &retestInputDeadlineContext{Context: context.Background()}
			fixture.readable.hook = func(operation string) {
				if operation == "read" {
					ctx.stopped = true
				}
			}
			fixture.readable.reads = []retestInputTestRead{{test.n, test.err}}
			data, err := readRetestInputWithOperations(ctx, "fixture", retestArtifactInput, fixture.operations())
			if test.hard {
				assertRetestInputFailure(t, data, err, false)
			} else if data != nil || err != context.DeadlineExceeded {
				t.Fatalf("data=%v error=%v", data != nil, err)
			}
			assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
		})
	}
	for _, shape := range []string{"nil_cancel", "typed_nil_cancel", "info_cancel", "invalid_info_cancel"} {
		t.Run(shape, func(t *testing.T) {
			fixture := newRetestInputFixture(32)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fixture.metadata.hook = func(operation string) {
				if operation == "stat" {
					cancel()
				}
			}
			result := retestInputTestStat{err: context.Canceled}
			switch shape {
			case "typed_nil_cancel":
				var typedNil *retestInputTestInfo
				result.info = typedNil
			case "info_cancel", "invalid_info_cancel":
				result.info = fixture.metadata.info
				if shape == "invalid_info_cancel" {
					fixture.metadata.info.mode = os.ModeNamedPipe
				}
			}
			fixture.metadata.stats = []retestInputTestStat{result}
			data, err := readRetestInputWithOperations(ctx, "fixture", retestArtifactInput, fixture.operations())
			if shape == "nil_cancel" {
				if data != nil || err != context.Canceled {
					t.Fatalf("data=%v error=%v", data != nil, err)
				}
			} else {
				assertRetestInputFailure(t, data, err, false)
			}
			assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1}, []string{"metadata"})
		})
	}
}

func TestRetestInputMaximumAndChunkBound(t *testing.T) {
	fixture := newRetestInputFixture(32)
	fixture.metadata.info.size = 64 << 20
	// No oversized file or duplicated 64 MiB fixture allocation is needed.
	fixture.readable.data = nil
	fixture.readable.reads = make([]retestInputTestRead, 1025)
	for i := 0; i < 1024; i++ {
		fixture.readable.reads[i] = retestInputTestRead{64 << 10, nil}
	}
	fixture.readable.reads[1024] = retestInputTestRead{0, io.EOF}
	data, err := readRetestInputWithOperations(context.Background(), "fixture", retestArtifactInput, fixture.operations())
	if err != nil || len(data) != 64<<20 || len(fixture.readable.readSize) != 1025 || fixture.readable.readSize[1024] != 1 {
		t.Fatalf("length=%d error=%v read calls=%d", len(data), err, len(fixture.readable.readSize))
	}
	for i, value := range data {
		if value != byte(i%251) {
			t.Fatalf("different byte at %d", i)
		}
	}
	for _, requested := range fixture.readable.readSize {
		if requested > 64<<10 {
			t.Fatalf("unbounded read request: %d", requested)
		}
	}
	assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
}

func TestRetestInputArtifactAndKeyOverflowAfterEOF(t *testing.T) {
	for _, kind := range []retestInputKind{retestArtifactInput, retestPublicKeyInput} {
		for _, end := range []error{nil, io.EOF} {
			t.Run(fmt.Sprintf("kind_%d/final_eof_%t", kind, end == io.EOF), func(t *testing.T) {
				fixture := newRetestInputFixture(32)
				fixture.readable.reads = []retestInputTestRead{{32, end}, {1, io.EOF}}
				data, err := readRetestInputWithOperations(context.Background(), "fixture", kind, fixture.operations())
				assertRetestInputFailure(t, data, err, false)
				assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				if !reflect.DeepEqual(fixture.readable.readSize, []int{32, 1}) {
					t.Fatalf("missing exact growth probe: %v", fixture.readable.readSize)
				}
			})
		}
	}
}
