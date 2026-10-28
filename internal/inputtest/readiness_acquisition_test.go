package inputtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
)

type inputReadFunction func([]byte) (int, error)

func (read inputReadFunction) Read(p []byte) (int, error) { return read(p) }

func TestInputReadinessRawLimitsRequireExactCompletion(t *testing.T) {
	for _, limit := range []int{128, 1 << 20} {
		for _, extra := range []int{-1, 0, 1} {
			t.Run(fmt.Sprintf("limit_%d_extra_%d", limit, extra), func(t *testing.T) {
				remaining := bytes.Repeat([]byte{'x'}, limit+extra)
				read, calls := 0, 0
				got, err := readInputBytes(t.Context(), inputReadFunction(func(p []byte) (int, error) {
					calls++
					if len(p) > 32 || len(p) == 0 {
						t.Fatal("unbounded or empty read request")
					}
					if len(remaining) == 0 {
						return 0, io.EOF
					}
					n := copy(p, remaining[:min(7, len(remaining))])
					remaining = remaining[n:]
					read += n
					return n, nil
				}), limit, 32)
				if extra <= 0 {
					if err != nil || len(got) != limit+extra || calls == 0 || read != limit+extra {
						t.Fatal("complete exact-budget value rejected")
					}
				} else if err != errInputRead || got != nil || read != limit+1 {
					t.Fatal("overflow was truncated or over-read")
				}
			})
		}
	}
	value := strings.Repeat(" ", 128-len("13:64")) + "13:64"
	data, err := readInputBytes(t.Context(), strings.NewReader(value), 128, 32)
	if number, valid := parseInputDeviceNumber(data); err != nil || !valid || number != (inputDeviceNumber{13, 64}) {
		t.Fatal("raw bound confused with trimmed value")
	}
	if _, err := readInputBytes(t.Context(), strings.NewReader(" "+value), 128, 32); err != errInputRead {
		t.Fatal("overflow disappeared during trim")
	}
}

func TestInputReadinessReadProtocolRejectsAmbiguousProgress(t *testing.T) {
	fault := errors.New("fixture read failure")
	for _, kind := range []string{"negative", "oversize", "idle", "partial-error", "error", "joined-eof", "wrapped-eof", "data-joined-eof"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			data, err := readInputBytes(t.Context(), inputReadFunction(func(p []byte) (int, error) {
				calls++
				switch kind {
				case "negative":
					return -1, fault
				case "oversize":
					return len(p) + 1, io.EOF
				case "idle":
					return 0, nil
				case "partial-error":
					p[0] = 'x'
					return 1, fault
				case "joined-eof":
					return 0, errors.Join(io.EOF, fault)
				case "wrapped-eof":
					return 0, fmt.Errorf("wrapped: %w", io.EOF)
				case "data-joined-eof":
					p[0] = 'x'
					return 1, errors.Join(io.EOF)
				default:
					return 0, fault
				}
			}), 128, 32)
			if data != nil || err != errInputRead || calls != 1 {
				t.Fatal("invalid read was accepted or retried")
			}
		})
	}
	var nilReader inputReadFunction
	for _, reader := range []io.Reader{nil, nilReader} {
		if _, err := readInputBytes(t.Context(), reader, 128, 32); err != errInputRead {
			t.Fatal("nil reader accepted")
		}
	}
	if _, err := readInputBytes(nil, strings.NewReader(""), 128, 32); err != errInputRead {
		t.Fatal("nil context accepted")
	}
	for _, limits := range [][2]int{{0, 32}, {(1 << 20) + 1, 32}, {128, 0}, {128, 4097}} {
		if _, err := readInputBytes(t.Context(), strings.NewReader(""), limits[0], limits[1]); err != errInputRead {
			t.Fatal("invalid allocation/read budget accepted")
		}
	}
}

func TestInputReadinessReadCancellationKeepsHardFailure(t *testing.T) {
	for _, hard := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		data, err := readInputBytes(ctx, inputReadFunction(func(p []byte) (int, error) {
			cancel()
			p[0] = 'x'
			if hard {
				return 1, syscall.EIO
			}
			return 1, nil
		}), 128, 32)
		want := error(context.Canceled)
		if hard {
			want = errInputRead
		}
		if data != nil || err != want {
			t.Fatal("cancellation hid acquisition fault or stale data escaped")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	if _, err := readInputBytes(ctx, inputReadFunction(func([]byte) (int, error) { calls++; return 0, io.EOF }), 128, 32); err != context.Canceled || calls != 0 {
		t.Fatal("pre-cancelled acquisition read")
	}
	ctx, cancel = context.WithCancel(t.Context())
	if _, err := readInputBytes(ctx, inputReadFunction(func([]byte) (int, error) { cancel(); return 0, io.EOF }), 128, 32); err != context.Canceled {
		t.Fatal("empty final EOF ignored cancellation")
	}
}

type inputMaskedError struct{}

func (inputMaskedError) Error() string { return "fixture masked errno" }
func (inputMaskedError) Is(error) bool { return true }

func TestInputReadinessAvailabilityRequiresSoleExpectedOperation(t *testing.T) {
	for _, op := range []string{"statat", "openat", "open"} {
		for _, errno := range []error{syscall.ENOENT, syscall.EACCES, syscall.EPERM} {
			err := &os.PathError{Op: op, Path: "owned", Err: errno}
			if !inputAvailabilityError(err, op, true) || !inputAvailabilityError(fmt.Errorf("wrapped: %w", err), op, true) {
				t.Fatal("ordinary sole availability error rejected")
			}
			if inputAvailabilityError(err, "wrong-operation", true) {
				t.Fatal("wrong-operation error softened")
			}
		}
	}
	if inputAvailabilityError(&os.PathError{Op: "open", Err: syscall.ENOENT}, "open", false) {
		t.Fatal("missing pinned proc descriptor softened")
	}
	for _, err := range []error{nil, syscall.EIO, inputMaskedError{}, errors.Join(syscall.ENOENT), errors.Join(syscall.ENOENT, syscall.EIO), &os.PathError{Op: "read", Err: syscall.ENOENT}} {
		if inputAvailabilityError(err, "open", true) {
			t.Fatal("ambiguous/hard errno softened")
		}
	}
	err := error(syscall.ENOENT)
	for range 33 {
		err = fmt.Errorf("wrapped: %w", err)
	}
	if inputAvailabilityError(err, "open", true) {
		t.Fatal("unbounded errno unwrap")
	}
}

func TestInputReadinessDeviceNumbersComeFromCanonicalObservation(t *testing.T) {
	for _, row := range []struct {
		text string
		want inputDeviceNumber
	}{{"13:0\n", inputDeviceNumber{13, 0}}, {" 13:256\n", inputDeviceNumber{13, 256}}, {"4294967295:4294967295", inputDeviceNumber{^uint32(0), ^uint32(0)}}} {
		if got, ok := parseInputDeviceNumber([]byte(row.text)); !ok || got != row.want {
			t.Fatal("observed device number rejected")
		}
	}
	for _, value := range []string{"", "13", "13:01", "+13:64", "13:+64", "13:64:1", "13:64\n1", "0x0d:64", "13:4294967296", "-1:64"} {
		if _, ok := parseInputDeviceNumber([]byte(value)); ok {
			t.Fatal("guessed/noncanonical identity accepted")
		}
	}
}

func TestInputReadinessCancellationCannotRewriteHardDiagnostic(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, status := range []Status{StatusPass, StatusInconclusive, StatusAborted, StatusError} {
		result := Result{Schema: Schema, Status: status, Detail: errInputClose.Error()}
		if !cancelInputResult(ctx, &result) {
			t.Fatal("cancellation not seen")
		}
		if status == StatusError {
			if result.Status != StatusError || result.Detail != errInputClose.Error() {
				t.Fatal("cleanup error downgraded")
			}
		} else if result.Status != StatusAborted {
			t.Fatal("stale non-error status after cancellation")
		}
	}
}

func TestInputReadinessEvdevRangeIsIndependentOfEventIndex(t *testing.T) {
	for _, minor := range []uint32{64, 95, 256, 300, (1 << 20) - 1} {
		if !inputEvdevNumber(inputDeviceNumber{13, minor}) {
			t.Fatal("eligible input-device number rejected")
		}
	}
	for _, number := range []inputDeviceNumber{{0, 64}, {1, 3}, {14, 64}, {13, 0}, {13, 63}, {13, 96}, {13, 255}, {13, 1 << 20}} {
		if inputEvdevNumber(number) {
			t.Fatal("non-evdev device number accepted")
		}
	}
}
