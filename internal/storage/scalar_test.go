package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type scalarInfoFixture struct {
	mode os.FileMode
	size int64
}

func (scalarInfoFixture) Name() string           { return "size" }
func (info scalarInfoFixture) Size() int64       { return info.size }
func (info scalarInfoFixture) Mode() os.FileMode { return info.mode }
func (scalarInfoFixture) ModTime() time.Time     { return time.Time{} }
func (info scalarInfoFixture) IsDir() bool       { return info.mode.IsDir() }
func (scalarInfoFixture) Sys() any               { return nil }

type scalarFileFixture struct {
	info       os.FileInfo
	statErr    error
	closeErr   error
	reader     io.Reader
	read       func([]byte) (int, error)
	statHook   func()
	closeHook  func()
	statCalls  int
	closeCalls int
	requests   []int
	delivered  int
}

func scalarTextFixture(value string) *scalarFileFixture {
	return &scalarFileFixture{info: scalarInfoFixture{}, reader: strings.NewReader(value)}
}

func (file *scalarFileFixture) Stat() (os.FileInfo, error) {
	file.statCalls++
	if file.statHook != nil {
		file.statHook()
	}
	return file.info, file.statErr
}

func (file *scalarFileFixture) Read(data []byte) (int, error) {
	file.requests = append(file.requests, len(data))
	var n int
	var err error
	if file.read != nil {
		n, err = file.read(data)
	} else {
		n, err = file.reader.Read(data)
	}
	if n >= 0 && n <= len(data) {
		file.delivered += n
	}
	return n, err
}

func (file *scalarFileFixture) Close() error {
	file.closeCalls++
	if file.closeHook != nil {
		file.closeHook()
	}
	return file.closeErr
}

func TestBlockScalarRawByteBounds(t *testing.T) {
	for _, test := range []struct {
		name         string
		available    int
		wantLength   int
		wantRequests []int
		wantErr      error
	}{
		{"empty", 0, 0, []int{257}, nil},
		{"one", 1, 1, []int{257, 256}, nil},
		{"255", 255, 255, []int{257, 2}, nil},
		{"256 with EOF probe", 256, 256, []int{257, 1}, nil},
		{"257 rejects without probe", 257, 0, []int{257}, errBlockScalarRead},
		{"virtual gigabyte", 1 << 30, 0, []int{257}, errBlockScalarRead},
	} {
		t.Run(test.name, func(t *testing.T) {
			remaining := test.available
			file := scalarTextFixture("")
			file.read = func(data []byte) (int, error) {
				if remaining == 0 {
					return 0, io.EOF
				}
				n := len(data)
				if n > remaining {
					n = remaining
				}
				for index := 0; index < n; index++ {
					data[index] = 'x'
				}
				remaining -= n
				return n, nil
			}
			data, err := readBlockScalar(context.Background(), "fixture", func(string) (blockScalarFile, error) { return file, nil })
			if err != test.wantErr || len(data) != test.wantLength || string(data) != strings.Repeat("x", test.wantLength) {
				t.Fatalf("data length=%d, error=%v; want length=%d, error=%v", len(data), err, test.wantLength, test.wantErr)
			}
			if !reflect.DeepEqual(file.requests, test.wantRequests) || file.delivered > 257 || file.closeCalls != 1 || file.statCalls != 1 {
				t.Fatalf("requests=%v delivered=%d stat=%d close=%d", file.requests, file.delivered, file.statCalls, file.closeCalls)
			}
		})
	}
}

func TestBlockScalarRequiresCompleteEOF(t *testing.T) {
	type step struct {
		text string
		err  error
	}
	failure := errors.New("synthetic scalar read failure")
	for _, test := range []struct {
		name         string
		steps        []step
		want         string
		wantRequests []int
		wantErr      error
	}{
		{"data and EOF", []step{{"2048\n", io.EOF}}, "2048\n", []int{257}, nil},
		{"short batches", []step{{"2", nil}, {"04", nil}, {"8\n", nil}, {"", io.EOF}}, "2048\n", []int{257, 256, 254, 252}, nil},
		{"short then EOF", []step{{"0", nil}, {"", io.EOF}}, "0", []int{257, 256}, nil},
		{"zero progress", []step{{"", nil}}, "", []int{257}, errBlockScalarRead},
		{"partial observation failure", []step{{"2048", failure}}, "", []int{257}, errBlockScalarRead},
		{"joined EOF is failure", []step{{"2048", errors.Join(io.EOF, failure)}}, "", []int{257}, errBlockScalarRead},
		{"underlying cancellation is failure", []step{{"", context.Canceled}}, "", []int{257}, errBlockScalarRead},
		{"underlying deadline is failure", []step{{"0", errors.Join(context.DeadlineExceeded, failure)}}, "", []int{257}, errBlockScalarRead},
		{"exact limit with EOF", []step{{strings.Repeat("x", 256), io.EOF}}, strings.Repeat("x", 256), []int{257}, nil},
		{"oversize with EOF", []step{{strings.Repeat("x", 257), io.EOF}}, "", []int{257}, errBlockScalarRead},
	} {
		t.Run(test.name, func(t *testing.T) {
			index := 0
			file := scalarTextFixture("")
			file.read = func(data []byte) (int, error) {
				if index == len(test.steps) {
					t.Fatal("read after terminal observation")
				}
				next := test.steps[index]
				index++
				if len(next.text) > len(data) {
					t.Fatal("unexpectedly small read request")
				}
				return copy(data, next.text), next.err
			}
			data, err := readBlockScalar(context.Background(), "fixture", func(string) (blockScalarFile, error) { return file, nil })
			if string(data) != test.want || err != test.wantErr || !reflect.DeepEqual(file.requests, test.wantRequests) || file.closeCalls != 1 {
				t.Fatalf("data=%q error=%v requests=%v close=%d", data, err, file.requests, file.closeCalls)
			}
		})
	}
}

func TestBlockScalarRejectsImpossibleReadCounts(t *testing.T) {
	for _, count := range []int{-1, 258} {
		file := scalarTextFixture("")
		file.read = func([]byte) (int, error) { return count, io.EOF }
		data, err := readBlockScalar(context.Background(), "fixture", func(string) (blockScalarFile, error) { return file, nil })
		if data != nil || err != errBlockScalarRead || len(file.requests) != 1 || file.closeCalls != 1 {
			t.Fatalf("count=%d data=%q error=%v reads=%d close=%d", count, data, err, len(file.requests), file.closeCalls)
		}
	}
}

func TestBlockScalarChecksDescriptorBeforeRead(t *testing.T) {
	for _, test := range []struct {
		name    string
		info    os.FileInfo
		statErr error
		wantErr error
	}{
		{"regular zero size", scalarInfoFixture{}, nil, nil},
		{"regular sysfs size", scalarInfoFixture{size: 4096}, nil, nil},
		{"regular huge size", scalarInfoFixture{size: 1<<63 - 1}, nil, nil},
		{"directory", scalarInfoFixture{mode: os.ModeDir}, nil, errBlockScalarRead},
		{"block", scalarInfoFixture{mode: os.ModeDevice}, nil, errBlockScalarRead},
		{"character", scalarInfoFixture{mode: os.ModeDevice | os.ModeCharDevice}, nil, errBlockScalarRead},
		{"fifo", scalarInfoFixture{mode: os.ModeNamedPipe}, nil, errBlockScalarRead},
		{"socket", scalarInfoFixture{mode: os.ModeSocket}, nil, errBlockScalarRead},
		{"symlink descriptor", scalarInfoFixture{mode: os.ModeSymlink}, nil, errBlockScalarRead},
		{"nil metadata", nil, nil, errBlockScalarRead},
		{"stat error", scalarInfoFixture{}, context.Canceled, errBlockScalarRead},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := scalarTextFixture("2048\n")
			file.info, file.statErr = test.info, test.statErr
			data, err := readBlockScalar(context.Background(), "fixture", func(string) (blockScalarFile, error) { return file, nil })
			if err != test.wantErr || file.statCalls != 1 || file.closeCalls != 1 {
				t.Fatalf("error=%v stat=%d close=%d", err, file.statCalls, file.closeCalls)
			}
			if test.wantErr != nil && (data != nil || len(file.requests) != 0) {
				t.Fatalf("rejected descriptor was read: %v", file.requests)
			}
			if test.wantErr == nil && string(data) != "2048\n" {
				t.Fatalf("value=%q", data)
			}
		})
	}
}

func TestBlockScalarAcquisitionOwnership(t *testing.T) {
	failure := errors.New("synthetic open failure")
	for _, test := range []struct {
		name      string
		handle    bool
		openErr   error
		closeErr  error
		wantClose int
		wantErr   error
	}{
		{"nil plus nil", false, nil, nil, 0, errBlockScalarRead},
		{"nil plus error", false, failure, nil, 0, errBlockScalarRead},
		{"handle plus error", true, failure, nil, 1, errBlockScalarRead},
		{"handle error and failed close", true, failure, failure, 1, errBlockScalarClose},
		{"clean value failed close", true, nil, failure, 1, errBlockScalarClose},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := scalarTextFixture("2048")
			file.closeErr = test.closeErr
			data, err := readBlockScalar(context.Background(), "fixture", func(string) (blockScalarFile, error) {
				if test.handle {
					return file, test.openErr
				}
				return nil, test.openErr
			})
			if data != nil || err != test.wantErr || file.closeCalls != test.wantClose {
				t.Fatalf("data=%q error=%v close=%d", data, err, file.closeCalls)
			}
			if test.openErr != nil && (file.statCalls != 0 || len(file.requests) != 0) {
				t.Fatal("observation after failed open")
			}
		})
	}
}

func TestBlockScalarDependenciesAndInitialCancellation(t *testing.T) {
	if _, err := readBlockScalar(nil, "fixture", func(string) (blockScalarFile, error) { t.Fatal("opened without context"); return nil, nil }); err != errBlockScalarRead {
		t.Fatalf("nil context: %v", err)
	}
	if _, err := readBlockScalar(context.Background(), "fixture", nil); err != errBlockScalarRead {
		t.Fatalf("nil opener: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, finish := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer finish()
	for _, ctx := range []context.Context{cancelled, expired} {
		data, err := readBlockScalar(ctx, "fixture", func(string) (blockScalarFile, error) { t.Fatal("opened after stop"); return nil, nil })
		if data != nil || err != errBlockScalarCancelled {
			t.Fatalf("data=%q error=%v", data, err)
		}
	}
}

func TestBlockScalarCancellationBoundaries(t *testing.T) {
	for _, test := range []struct {
		name      string
		phase     string
		wantStat  int
		wantReads int
		want      string
		wantErr   error
	}{
		{"after open", "open", 0, 0, "", errBlockScalarCancelled},
		{"after stat", "stat", 1, 0, "", errBlockScalarCancelled},
		{"after incomplete read", "read", 1, 1, "", errBlockScalarCancelled},
		{"complete EOF retained for consumer", "eof", 1, 1, "bad-size", nil},
		{"clean close retained for consumer", "close", 1, 1, "bad-size", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			file := scalarTextFixture("")
			file.read = func(data []byte) (int, error) {
				if test.phase == "read" {
					cancel()
					return copy(data, "2"), nil
				}
				if test.phase == "eof" {
					cancel()
				}
				return copy(data, "bad-size"), io.EOF
			}
			if test.phase == "stat" {
				file.statHook = cancel
			}
			if test.phase == "close" {
				file.closeHook = cancel
			}
			data, err := readBlockScalar(ctx, "fixture", func(string) (blockScalarFile, error) {
				if test.phase == "open" {
					cancel()
				}
				return file, nil
			})
			if string(data) != test.want || err != test.wantErr || file.statCalls != test.wantStat || len(file.requests) != test.wantReads || file.closeCalls != 1 {
				t.Fatalf("data=%q error=%v stat=%d reads=%d close=%d", data, err, file.statCalls, len(file.requests), file.closeCalls)
			}
		})
	}
}

func TestBlockScalarObservationFailureWinsSimultaneousCancellation(t *testing.T) {
	for _, kind := range []string{"open", "stat", "type", "read", "joined EOF", "overflow", "no progress", "negative count", "large count"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			file := scalarTextFixture("")
			if kind == "stat" {
				file.statErr = context.Canceled
				file.statHook = cancel
			}
			if kind == "type" {
				file.info = scalarInfoFixture{mode: os.ModeDevice}
				file.statHook = cancel
			}
			file.read = func(data []byte) (int, error) {
				cancel()
				switch kind {
				case "read":
					return copy(data, "2048"), context.Canceled
				case "joined EOF":
					return copy(data, "2048"), errors.Join(io.EOF, context.Canceled)
				case "overflow":
					return copy(data, strings.Repeat("x", 257)), io.EOF
				case "no progress":
					return 0, nil
				case "negative count":
					return -1, nil
				case "large count":
					return 258, nil
				default:
					t.Fatal("unexpected read")
					return 0, io.EOF
				}
			}
			data, err := readBlockScalar(ctx, "fixture", func(string) (blockScalarFile, error) {
				if kind == "open" {
					cancel()
					return file, context.Canceled
				}
				return file, nil
			})
			if data != nil || err != errBlockScalarRead || file.closeCalls != 1 {
				t.Fatalf("data=%q error=%v close=%d", data, err, file.closeCalls)
			}
		})
	}
}

func TestBlockScalarCloseFailureDominatesEveryOutcome(t *testing.T) {
	for _, kind := range []string{"success", "open error", "stat error", "read error", "cancel after open", "cancel after EOF", "cancel during close"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			file := scalarTextFixture("2048")
			file.closeErr = errors.Join(errors.New("synthetic close failure"), context.Canceled)
			if kind == "stat error" {
				file.statErr = os.ErrPermission
			}
			if kind == "read error" {
				file.read = func([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
			}
			if kind == "cancel after EOF" {
				file.read = func(data []byte) (int, error) { cancel(); return copy(data, "0"), io.EOF }
			}
			if kind == "cancel during close" {
				file.closeHook = cancel
			}
			data, err := readBlockScalar(ctx, "fixture", func(string) (blockScalarFile, error) {
				if kind == "open error" {
					return file, os.ErrPermission
				}
				if kind == "cancel after open" {
					cancel()
				}
				return file, nil
			})
			if data != nil || err != errBlockScalarClose || file.closeCalls != 1 {
				t.Fatalf("data=%q error=%v close=%d", data, err, file.closeCalls)
			}
		})
	}
}
