package quick

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type memoryAvailabilityReader struct {
	read   func([]byte) (int, error)
	close  func() error
	closed atomic.Int32
}

func (r *memoryAvailabilityReader) Read(p []byte) (int, error) { return r.read(p) }
func (r *memoryAvailabilityReader) Close() error {
	r.closed.Add(1)
	if r.close != nil {
		return r.close()
	}
	return nil
}

func TestMemoryStressAvailabilityRequiresOneExactValue(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      uint64
		valid     bool
	}{
		{"minimal", "MemAvailable: 16384 kB\n", 16777216, true},
		{"other_fields", "MemTotal: 99999 kB\nMemAvailable:\t32768 kB\nIgnored: words\n", 33554432, true},
		{"no_final_newline", "MemAvailable: 16384 kB", 16777216, true},
		{"missing", "MemTotal: 16384 kB\n", 0, false},
		{"duplicate", "MemAvailable: 16384 kB\nMemAvailable: 16384 kB\n", 0, false},
		{"case_alias", "MemAvailable: 16384 kB\nmemavailable: 1 kB\n", 0, false},
		{"wrong_case", "memavailable: 16384 kB\n", 0, false},
		{"zero", "MemAvailable: 0 kB\n", 0, false},
		{"negative", "MemAvailable: -1 kB\n", 0, false},
		{"signed", "MemAvailable: +16384 kB\n", 0, false},
		{"unit", "MemAvailable: 16384 KB\n", 0, false},
		{"extra", "MemAvailable: 16384 kB extra\n", 0, false},
		{"decimal", "MemAvailable: 16384.0 kB\n", 0, false},
		{"parse_overflow", "MemAvailable: 18446744073709551616 kB\n", 0, false},
		{"byte_overflow", "MemAvailable: 18014398509481984 kB\n", 0, false},
		{"nul", "MemAvailable: 16384 kB\x00\n", 0, false},
		{"carriage_return", "MemAvailable: 16384 kB\r\n", 0, false},
		{"non_ascii", "MemAvailable: 16384 kB\nName: \xff\n", 0, false},
		{"malformed_other", "MemAvailable: 16384 kB\nunkeyed\n", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, ok := parseMemoryStressAvailable([]byte(tc.raw))
			if value != tc.want || ok != tc.valid {
				t.Fatalf("availability %d/%v, want %d/%v", value, ok, tc.want, tc.valid)
			}
		})
	}
}

func TestMemoryStressAvailabilityExactEOFAndClosePrecedence(t *testing.T) {
	valid := "MemAvailable: 16384 kB\n"
	exact := valid + "Ignored:" + strings.Repeat("x", 65536-len(valid)-8)
	if len(exact) != 65536 {
		t.Fatal("exact-limit prerequisite")
	}
	for _, tc := range []struct {
		name, raw    string
		bytes, reads uint64
		failure      string
		eof, good    bool
	}{
		{"small", valid, 23, 2, "", true, true},
		{"exact", exact, 65536, 17, "", true, true},
		{"over", exact + "x", 65537, 17, "too_large", false, false},
		{"empty", "", 0, 1, "invalid", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := strings.NewReader(tc.raw)
			owner := &memoryAvailabilityReader{read: input.Read}
			out := readMemoryStressAvailability(context.Background(), func() (io.ReadCloser, error) { return owner, nil }, time.Now)
			e := out.evidence
			if e.RawBytes != tc.bytes || e.ReadCalls != tc.reads || e.Failure != tc.failure || e.EOF != tc.eof || e.Valid != tc.good || !e.Opened || !e.Started || !e.CloseConfirmed || owner.closed.Load() != 1 {
				t.Fatalf("wrong exact bound/ownership: %+v", e)
			}
			if tc.good && out.available != 16777216 || !tc.good && out.available != 0 {
				t.Fatal("partial availability escaped")
			}
		})
	}
	for _, tc := range []struct {
		name                             string
		readError, openError, closeError error
		panicRead, panicClose            bool
		failure                          string
		closed                           bool
	}{
		{"data_eof", io.EOF, nil, nil, false, false, "", true},
		{"data_error", errors.New("private-read"), nil, nil, false, false, "read_failed", true},
		{"close_error", io.EOF, nil, errors.New("private-close"), false, false, "close_failed", false},
		{"both_errors", errors.New("private-read"), nil, errors.New("private-close"), false, false, "read_failed", false},
		{"returned_handle_error", nil, errors.New("private-open"), nil, false, false, "open_failed", true},
		{"read_panic", nil, nil, nil, true, false, "reader_panic", true},
		{"close_panic", io.EOF, nil, nil, false, true, "close_failed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := &memoryAvailabilityReader{
				read: func(p []byte) (int, error) {
					if tc.panicRead {
						panic("private")
					}
					return copy(p, valid), tc.readError
				},
				close: func() error {
					if tc.panicClose {
						panic("private")
					}
					return tc.closeError
				},
			}
			out := readMemoryStressAvailability(context.Background(), func() (io.ReadCloser, error) { return owner, tc.openError }, time.Now)
			if out.evidence.Failure != tc.failure || out.evidence.CloseConfirmed != tc.closed || owner.closed.Load() != 1 {
				t.Fatalf("close precedence %+v", out.evidence)
			}
			if tc.failure != "" && (out.available != 0 || out.evidence.Valid) {
				t.Fatal("failed acquisition retained availability")
			}
			if tc.name == "data_eof" && (!out.evidence.Valid || out.evidence.RawBytes != 23 || out.evidence.ReadCalls != 1) {
				t.Fatal("data+EOF rejected")
			}
		})
	}
}

func TestMemoryStressAvailabilityRejectsBrokenReaders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		read    func([]byte) (int, error)
		failure string
	}{
		{"zero_progress", func([]byte) (int, error) { return 0, nil }, "read_failed"},
		{"negative_count", func([]byte) (int, error) { return -1, nil }, "read_failed"},
		{"oversized_count", func(p []byte) (int, error) { return len(p) + 1, nil }, "read_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := &memoryAvailabilityReader{read: tc.read}
			out := readMemoryStressAvailability(context.Background(), func() (io.ReadCloser, error) { return owner, nil }, time.Now)
			if out.evidence.Failure != tc.failure || out.evidence.RawBytes != 0 || out.evidence.ReadCalls != 1 || !out.evidence.CloseConfirmed || owner.closed.Load() != 1 {
				t.Fatal("broken reader admitted")
			}
		})
	}
	panicked := readMemoryStressAvailability(context.Background(), func() (io.ReadCloser, error) { panic("private-open") }, time.Now)
	if panicked.evidence.CloseConfirmed || panicked.evidence.Failure != "reader_panic" || panicked.evidence.Opened {
		t.Fatal("unknown opener ownership claimed clean")
	}
	missing := readMemoryStressAvailability(context.Background(), func() (io.ReadCloser, error) { return nil, errors.New("private") }, time.Now)
	if !missing.evidence.CloseConfirmed || missing.evidence.Failure != "open_failed" {
		t.Fatal("absent handle classification")
	}
}

func TestMemoryStressAvailabilityCancelledOwnerCannotRewriteReceipt(t *testing.T) {
	for _, stage := range []string{"read", "close"} {
		t.Run(stage, func(t *testing.T) {
			entered, release, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
			owner := &memoryAvailabilityReader{
				read: func(p []byte) (int, error) {
					if stage == "read" {
						close(entered)
						<-release
					}
					return copy(p, "MemAvailable: 16384 kB\n"), io.EOF
				},
				close: func() error {
					if stage == "close" {
						close(entered)
						<-release
					}
					close(closed)
					return nil
				},
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			clock := &cpuStressClock{read: time.Now}
			clock.origin = clock.now()
			type result struct {
				value  memoryStressAvailabilityCompletion
				joined bool
			}
			done := make(chan result, 1)
			go func() {
				v, j := acquireMemoryStressAvailability(ctx, func() (io.ReadCloser, error) { return owner, nil }, clock)
				done <- result{v, j}
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("owner did not enter")
			}
			cancel()
			var got result
			select {
			case got = <-done:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("cancel did not return")
			}
			before := got.value
			if got.joined || got.value.evidence.CloseConfirmed || got.value.evidence.Valid || got.value.available != 0 {
				close(release)
				t.Fatal("unjoined owner certified")
			}
			close(release)
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("owner did not close after release")
			}
			if owner.closed.Load() != 1 || !reflect.DeepEqual(before, got.value) {
				t.Fatal("late owner rewrote receipt or closed twice")
			}
		})
	}
}
