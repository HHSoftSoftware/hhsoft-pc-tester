package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type inspectionTestBroker struct {
	read        func(context.Context, int) (string, error)
	directReads int
}

func (source *inspectionTestBroker) Read([]byte) (int, error) {
	source.directReads++
	return 0, errors.New("unexpected direct read")
}
func (source *inspectionTestBroker) ReadFindingLine(ctx context.Context, limit int) (string, error) {
	return source.read(ctx, limit)
}

type inspectionReadTrap struct{ calls int }

func (source *inspectionReadTrap) Read([]byte) (int, error) {
	source.calls++
	return 0, io.EOF
}

type inspectionNilContext struct{ context.Context }

func TestInspectionMemoryTerminatorsDoNotReadAhead(t *testing.T) {
	for _, test := range []struct{ name, input, line, remaining string }{
		{"lf", "D\nN\n", "D", "N\n"},
		{"cr", "N\rD\n", "N", "D\n"},
		{"crlf", "I\r\nD\n", "I", "\nD\n"},
		{"empty", "\nN\n", "", "N\n"},
		{"spaces", " \t\rN\n", " \t", "N\n"},
		{"limit", strings.Repeat("x", 4096) + "\nN", strings.Repeat("x", 4096), "N"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := strings.NewReader(test.input)
			line, complete, err := readInspectionLine(t.Context(), source)
			if err != nil || !complete || line != test.line {
				t.Fatalf("line=%q complete=%v err=%v", line, complete, err)
			}
			remaining, err := io.ReadAll(source)
			if err != nil || string(remaining) != test.remaining {
				t.Fatal("input past the first terminator was consumed")
			}
		})
	}
	for _, source := range []io.Reader{bytes.NewReader([]byte("D\n")), bytes.NewBufferString("D\n")} {
		if line, complete, err := readInspectionLine(t.Context(), source); err != nil || !complete || line != "D" {
			t.Fatal("allowed finite memory adapter rejected")
		}
	}
	oversized := strings.NewReader(strings.Repeat("x", 4097) + "\nN")
	if line, complete, err := readInspectionLine(t.Context(), oversized); err == nil || complete || line != "" || oversized.Len() != 2 {
		t.Fatal("oversized response acquired credit or exceeded its bounded probe")
	}
}

func TestInspectionMemoryProtocolFaultsAreReached(t *testing.T) {
	hard := errors.New("private input detail")
	for _, test := range []struct {
		name      string
		count     int
		character byte
		err       error
		complete  bool
		wantErr   error
	}{
		{"negative", -1, '\n', io.EOF, false, errSessionInput},
		{"oversized", 2, '\n', nil, false, errSessionInput},
		{"zero_progress", 0, 0, nil, false, errSessionInput},
		{"empty_eof", 0, 0, io.EOF, false, io.EOF},
		{"partial_eof", 1, 'D', io.EOF, false, io.EOF},
		{"lf_eof", 1, '\n', io.EOF, true, nil},
		{"cr_eof", 1, '\r', io.EOF, true, nil},
		{"hard_lf", 1, '\n', hard, true, hard},
		{"hard_partial", 1, 'D', hard, false, hard},
		{"hard_eof", 1, '\n', errors.Join(hard, io.EOF), true, hard},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			line, complete, err := readInspectionBytes(t.Context(), func(buffer []byte) (int, error) {
				calls++
				if len(buffer) != 1 || calls != 1 {
					t.Fatal("read protocol fault was not isolated to one bounded read")
				}
				buffer[0] = test.character
				return test.count, test.err
			})
			if calls != 1 || line != "" || complete != test.complete ||
				(test.wantErr == nil && err != nil) || test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("calls=%d line=%q complete=%v err=%v", calls, line, complete, err)
			}
		})
	}
	for _, invalidCount := range []int{-1, 2} {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		_, complete, err := readInspectionBytes(ctx, func(buffer []byte) (int, error) {
			calls++
			buffer[0] = '\n'
			cancel()
			return invalidCount, context.Canceled
		})
		if calls != 1 || complete || err != errSessionInput {
			t.Fatal("cancellation erased a reached invalid read count")
		}
	}
}

func TestInspectionBrokerCompletionIsNotInferredFromErrors(t *testing.T) {
	hard := errors.New("private broker detail")
	for _, test := range []struct {
		name, line string
		err        error
		complete   bool
	}{
		{"empty_terminated", "", nil, true},
		{"empty_cancel", "", context.Canceled, false},
		{"empty_hard", "", hard, false},
		{"whitespace_cancel", " \t", context.Canceled, true},
		{"defect_hard", "D", hard, true},
		{"negative_cancel", "N", context.Canceled, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			ctx := context.WithValue(t.Context(), struct{}{}, "request")
			source := &inspectionTestBroker{read: func(actual context.Context, limit int) (string, error) {
				calls++
				if actual != ctx || limit != 4096 {
					t.Fatal("broker did not receive the exact request context and bound")
				}
				return test.line, test.err
			}}
			line, complete, err := readInspectionLine(ctx, source)
			if line != test.line || complete != test.complete || err != test.err || calls != 1 || source.directReads != 0 {
				t.Fatal("broker result completion or fault changed")
			}
		})
	}
	calls := 0
	source := &inspectionTestBroker{read: func(context.Context, int) (string, error) {
		calls++
		return strings.Repeat("D", 4097), context.Canceled
	}}
	if line, complete, err := readInspectionLine(t.Context(), source); calls != 1 || line != "" || complete || err != errSessionInput {
		t.Fatal("broker bound violation acquired completion or lost hard priority")
	}
}

func TestInspectionRejectsMissingAndUnboundedInputs(t *testing.T) {
	var missing *inspectionTestBroker
	var missingContext *inspectionNilContext
	var missingMemory *strings.Reader
	for _, input := range []io.Reader{nil, missing, missingMemory} {
		if line, complete, err := readInspectionLine(t.Context(), input); line != "" || complete || err != errSessionInput {
			t.Fatal("nil input was admitted")
		}
	}
	for _, ctx := range []context.Context{nil, missingContext} {
		source := &inspectionReadTrap{}
		if _, _, err := readInspectionLine(ctx, source); err != errSessionInput || source.calls != 0 {
			t.Fatal("nil context reached input")
		}
	}
	source := &inspectionReadTrap{}
	if _, _, err := readInspectionLine(t.Context(), source); err != errFindingInputUnsupported || source.calls != 0 {
		t.Fatal("unbounded reader was called")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := readInspectionLine(ctx, source); err != context.Canceled || source.calls != 0 {
		t.Fatal("stopped input attempted a read")
	}
	if _, _, err := readInspectionBytes(t.Context(), nil); err != errSessionInput {
		t.Fatal("missing bounded callback was admitted")
	}
}
