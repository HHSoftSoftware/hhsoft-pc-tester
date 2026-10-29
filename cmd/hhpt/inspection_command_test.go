package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
)

func inspectionClock(start time.Time, elapsed time.Duration) func() time.Time {
	calls := 0
	return func() time.Time {
		calls++
		if calls == 1 {
			return start
		}
		return start.Add(elapsed)
	}
}

type inspectionPromptWriter struct {
	write func([]byte) (int, error)
}

func (writer inspectionPromptWriter) Write(data []byte) (int, error) { return writer.write(data) }

func TestInspectionRealDispatchWritesOnlyFiniteEvidence(t *testing.T) {
	for _, test := range []struct{ name, input, finding, reason, status string }{
		{"negative", "N\n", "no_defect_observed", "completed", "pass"},
		{"adverse", "D\n", "defect_observed", "completed", "fail"},
		{"uncertain", "I\n", "not_inspected", "not_inspected", "inconclusive"},
		{"empty", "\n", "not_inspected", "empty_response", "inconclusive"},
		{"invalid", "private operator phrase\n", "not_inspected", "invalid_response", "inconclusive"},
		{"partial", "D", "not_inspected", "input_ended", "inconclusive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(t.Context(), []string{"inspect-exterior"}, strings.NewReader(test.input), &stdout, &stderr)
			got, err := exteriorinspection.Decode(bytes.TrimSpace(stdout.Bytes()))
			if code != 0 || err != nil || got.Finding != test.finding || got.StopReason != test.reason || got.Status != test.status ||
				got.ResponseComplete != (test.name != "partial") || !strings.Contains(stderr.String(), "without opening or moving") ||
				bytes.Contains(stdout.Bytes(), []byte("private operator phrase")) || bytes.Contains(stderr.Bytes(), []byte("private operator phrase")) {
				t.Fatalf("code=%d result=%+v err=%v", code, got, err)
			}
		})
	}
}

func TestInspectionObservationUsesEffectiveDecisionDeadline(t *testing.T) {
	start := time.Now().Add(time.Hour)
	for _, test := range []struct {
		name, response, finding, reason, status string
		elapsed, parent                         time.Duration
		milliseconds                            int64
		hard                                    bool
	}{
		{"before", "N", "no_defect_observed", "completed", "pass", 44999 * time.Millisecond, 0, 44999, false},
		{"exact", "N", "not_inspected", "deadline", "aborted", 45 * time.Second, 0, 45000, false},
		{"late", "N", "not_inspected", "deadline", "aborted", 45001 * time.Millisecond, 0, 45000, false},
		{"parent", "N", "not_inspected", "deadline", "aborted", 12001 * time.Millisecond, 12 * time.Second, 12000, false},
		{"defect", "D", "defect_observed", "deadline", "fail", 46 * time.Second, 0, 45000, false},
		{"defect_hard", "D", "defect_observed", "input_error", "error", 46 * time.Second, 0, 45000, true},
		{"fraction", "N", "no_defect_observed", "completed", "pass", 1999 * time.Microsecond, 0, 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			if test.parent != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, start.Add(test.parent))
				defer cancel()
			}
			calls := 0
			source := &inspectionTestBroker{read: func(actual context.Context, limit int) (string, error) {
				calls++
				wantDeadline := start.Add(45 * time.Second)
				if test.parent != 0 {
					wantDeadline = start.Add(test.parent)
				}
				deadline, ok := actual.Deadline()
				if !ok || !deadline.Equal(wantDeadline) || actual.Err() != nil || limit != 4096 {
					t.Fatal("effective deadline or lagging-context prerequisite missing")
				}
				if test.hard {
					return test.response, errors.New("synthetic hard input")
				}
				return test.response, nil
			}}
			var prompt bytes.Buffer
			got, err := observeInspection(ctx, source, &prompt, 45*time.Second, inspectionClock(start, test.elapsed))
			if err != nil || calls != 1 || got.Finding != test.finding || got.StopReason != test.reason ||
				got.Status != test.status || !got.ResponseComplete || got.ObservationMilliseconds != test.milliseconds {
				t.Fatalf("result=%+v err=%v calls=%d", got, err, calls)
			}
		})
	}
}

func TestInspectionReadFaultWinsOverConcurrentCancellation(t *testing.T) {
	hard := errors.New("synthetic private input failure")
	for _, test := range []struct {
		name, line, reason, status, finding string
		fault                               error
		complete                            bool
	}{
		{"clean_negative", "N", "operator_cancelled", "aborted", "not_inspected", context.Canceled, true},
		{"clean_defect", "D", "operator_cancelled", "fail", "defect_observed", context.Canceled, true},
		{"clean_empty", "", "operator_cancelled", "aborted", "not_inspected", context.Canceled, false},
		{"joined_defect", "D", "input_error", "error", "defect_observed", errors.Join(hard, context.Canceled), true},
		{"hard_negative", "N", "input_error", "error", "not_inspected", hard, true},
		{"hard_empty", "", "input_error", "error", "not_inspected", hard, false},
		{"joined_eof", "D", "input_error", "error", "defect_observed", errors.Join(hard, io.EOF), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			source := &inspectionTestBroker{read: func(context.Context, int) (string, error) {
				calls++
				cancel()
				return test.line, test.fault
			}}
			got, err := observeInspection(ctx, source, io.Discard, time.Second, inspectionClock(time.Now().Add(time.Hour), 17*time.Millisecond))
			if err != nil || calls != 1 || got.Finding != test.finding || got.StopReason != test.reason ||
				got.Status != test.status || got.ResponseComplete != test.complete || got.ObservationMilliseconds != 17 {
				t.Fatalf("result=%+v err=%v", got, err)
			}
		})
	}
	if inspectionStop(context.DeadlineExceeded, context.DeadlineExceeded, false) != "deadline" ||
		inspectionStop(errors.Join(hard, context.DeadlineExceeded), context.DeadlineExceeded, true) != "input_error" ||
		inspectionStop(context.Canceled, nil, false) != "operator_cancelled" {
		t.Fatal("clean returned stops or hard-error priority changed")
	}
}

func TestInspectionUsageAndStoppedContextHaveNoInputEffects(t *testing.T) {
	for _, arguments := range [][]string{{"-timeout", "1ms"}, {"-timeout", "46s"}, {"unexpected"}, {"-unknown"}} {
		var stdout, stderr bytes.Buffer
		calls := 0
		prepare := func(context.Context, io.Reader) (context.Context, io.Reader, func() error, error) {
			calls++
			t.Fatal("usage reached input ownership")
			return nil, nil, nil, nil
		}
		if code := runInspectionCommand(nil, arguments, nil, &stdout, &stderr, false, prepare, writeDestination, time.Now); code != 2 || calls != 0 || stdout.Len() != 0 {
			t.Fatal("usage precedence changed")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout, stderr bytes.Buffer
	calls := 0
	prepare := func(context.Context, io.Reader) (context.Context, io.Reader, func() error, error) {
		calls++
		t.Fatal("pre-stopped command acquired an owner")
		return nil, nil, nil, nil
	}
	code := runInspectionCommand(ctx, nil, strings.NewReader("N\n"), &stdout, &stderr, false, prepare, writeDestination, func() time.Time {
		t.Fatal("pre-stopped command acquired observation time")
		return time.Time{}
	})
	got, err := exteriorinspection.Decode(bytes.TrimSpace(stdout.Bytes()))
	if code != 0 || err != nil || calls != 0 || stderr.Len() != 0 || got.Status != "aborted" || got.ObservationMilliseconds != 0 || got.ResponseComplete {
		t.Fatal("pre-stopped observation acquired credit or prompted")
	}
}

func TestInspectionPromptFailurePreventsReadButChecksCleanup(t *testing.T) {
	for _, short := range []bool{false, true} {
		reads, joins := 0, 0
		source := &inspectionTestBroker{read: func(context.Context, int) (string, error) {
			reads++
			t.Fatal("failed prompt read input")
			return "", nil
		}}
		prepare := func(ctx context.Context, _ io.Reader) (context.Context, io.Reader, func() error, error) {
			return ctx, source, func() error { joins++; return nil }, nil
		}
		prompt := inspectionPromptWriter{write: func(data []byte) (int, error) {
			if short {
				return len(data) - 1, nil
			}
			return 0, errors.New("synthetic prompt failure")
		}}
		var stdout bytes.Buffer
		code := runInspectionCommand(t.Context(), nil, strings.NewReader("N\n"), &stdout, prompt, false, prepare, writeDestination, inspectionClock(time.Now().Add(time.Hour), 17*time.Millisecond))
		got, err := exteriorinspection.Decode(bytes.TrimSpace(stdout.Bytes()))
		if code != 1 || err != nil || reads != 0 || joins != 1 || got.StopReason != "prompt_error" || got.Status != "error" || got.ResponseComplete {
			t.Fatal("prompt fault bypassed finite evidence or owner join")
		}
	}
}

func TestInspectionActualOutputWaitsForOwnerReceipt(t *testing.T) {
	for _, test := range []struct {
		name                  string
		prepareErr, finishErr error
		code                  int
	}{
		{"success", nil, nil, 0},
		{"prepare_error", errSessionInput, nil, 1},
		{"read_owner_failure", nil, errSessionInput, 1},
		{"restore_failure", nil, errSessionInputCleanup, 12},
		{"prepare_cleanup_failure", errSessionInputCleanup, nil, 12},
	} {
		t.Run(test.name, func(t *testing.T) {
			var ledger []string
			joins, writes := 0, 0
			source := &inspectionTestBroker{read: func(context.Context, int) (string, error) {
				ledger = append(ledger, "read")
				return "N", nil
			}}
			prepare := func(ctx context.Context, _ io.Reader) (context.Context, io.Reader, func() error, error) {
				ledger = append(ledger, "prepare")
				return ctx, source, func() error { joins++; ledger = append(ledger, "finish"); return test.finishErr }, test.prepareErr
			}
			var stdout, stderr bytes.Buffer
			write := func(path string, output io.Writer, emit func(io.Writer) error) error {
				writes++
				ledger = append(ledger, "write")
				if joins != 1 || path != "-" || output != &stdout {
					t.Fatal("output preceded ownership completion")
				}
				return writeDestination(path, output, emit)
			}
			code := runInspectionCommand(t.Context(), nil, strings.NewReader("I\n"), &stdout, &stderr, false, prepare, write, inspectionClock(time.Now().Add(time.Hour), 17*time.Millisecond))
			if code != test.code || joins != 1 {
				t.Fatalf("code=%d joins=%d", code, joins)
			}
			if test.code == 0 {
				if !reflect.DeepEqual(ledger, []string{"prepare", "read", "finish", "write"}) || writes != 1 {
					t.Fatalf("owner ordering changed: %v", ledger)
				}
				got, err := exteriorinspection.Decode(bytes.TrimSpace(stdout.Bytes()))
				if err != nil || got.Status != "pass" || got.ObservationMilliseconds != 17 {
					t.Fatal("valid owned evidence changed")
				}
			} else if writes != 0 || stdout.Len() != 0 {
				t.Fatal("failed owner receipt allowed output")
			}
		})
	}
}

type inspectionOutputOwner struct {
	bytes.Buffer
	closed             int
	writeErr, closeErr error
	short              bool
}

func (owner *inspectionOutputOwner) Write(data []byte) (int, error) {
	if owner.writeErr != nil {
		return 0, owner.writeErr
	}
	if owner.short {
		return len(data) - 1, nil
	}
	return owner.Buffer.Write(data)
}
func (owner *inspectionOutputOwner) Close() error { owner.closed++; return owner.closeErr }

func TestInspectionDestinationFailuresDoNotReportSuccess(t *testing.T) {
	for _, name := range []string{"write", "short", "close", "success"} {
		t.Run(name, func(t *testing.T) {
			owner := &inspectionOutputOwner{}
			if name == "write" {
				owner.writeErr = errors.New("write")
			}
			if name == "short" {
				owner.short = true
			}
			if name == "close" {
				owner.closeErr = errors.New("close")
			}
			opens := 0
			write := func(path string, stdout io.Writer, emit func(io.Writer) error) error {
				return writeDestinationWith(path, stdout, emit, func(path string, flags int, mode os.FileMode) (io.WriteCloser, error) {
					opens++
					if path != "inspection.json" || flags != os.O_WRONLY|os.O_CREATE|os.O_EXCL || mode != 0600 {
						t.Fatal("exclusive destination ownership changed")
					}
					return owner, nil
				})
			}
			var stdout, stderr bytes.Buffer
			code := runInspectionCommand(t.Context(), []string{"-output", "inspection.json"}, strings.NewReader("D\n"), &stdout, &stderr, true, nil, write, time.Now)
			if opens != 1 || owner.closed != 1 || stdout.Len() != 0 || (name == "success" && code != 0) || (name != "success" && code != 1) {
				t.Fatal("destination failure or close boundary changed")
			}
		})
	}
	stdout := &inspectionOutputOwner{}
	var stderr bytes.Buffer
	if code := runInspectionOwned(t.Context(), nil, strings.NewReader("D\n"), stdout, &stderr); code != 0 || stdout.closed != 0 {
		t.Fatal("borrowed worker closed caller stdout")
	}
	var encoded map[string]any
	if json.Unmarshal(stdout.Bytes(), &encoded) != nil || encoded["status"] != "fail" {
		t.Fatal("borrowed worker lost adverse result")
	}
}

func TestInspectionMalformedDependenciesCannotAcquireOrWrite(t *testing.T) {
	var nilContext *inspectionNilContext
	var nilInput *strings.Reader
	var nilOutput *bytes.Buffer
	for _, test := range []struct {
		name   string
		ctx    context.Context
		input  io.Reader
		stdout io.Writer
	}{
		{"nil_context", nil, strings.NewReader("N\n"), io.Discard},
		{"typed_context", nilContext, strings.NewReader("N\n"), io.Discard},
		{"nil_input", t.Context(), nil, io.Discard},
		{"typed_input", t.Context(), nilInput, io.Discard},
		{"typed_output", t.Context(), strings.NewReader("N\n"), nilOutput},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			prepare := func(context.Context, io.Reader) (context.Context, io.Reader, func() error, error) {
				calls++
				t.Fatal("malformed dependency reached acquisition")
				return nil, nil, nil, nil
			}
			var stderr bytes.Buffer
			code := runInspectionCommand(test.ctx, nil, test.input, test.stdout, &stderr, false, prepare, writeDestination, time.Now)
			if code != 1 || calls != 0 {
				t.Fatal("malformed dependency admitted")
			}
		})
	}
}

func TestInspectionObservationExcludesPreparationAndJoin(t *testing.T) {
	clock := time.Now().Add(time.Hour)
	start := clock.Add(10 * time.Second)
	joins := 0
	source := &inspectionTestBroker{read: func(context.Context, int) (string, error) {
		clock = start.Add(20 * time.Millisecond)
		return "N", nil
	}}
	prepare := func(ctx context.Context, _ io.Reader) (context.Context, io.Reader, func() error, error) {
		clock = start
		return ctx, source, func() error { joins++; clock = start.Add(70 * time.Second); return nil }, nil
	}
	var stdout, stderr bytes.Buffer
	code := runInspectionCommand(t.Context(), nil, strings.NewReader("N\n"), &stdout, &stderr, false, prepare, writeDestination, func() time.Time { return clock })
	got, err := exteriorinspection.Decode(bytes.TrimSpace(stdout.Bytes()))
	if code != 0 || err != nil || joins != 1 || !clock.Equal(start.Add(70*time.Second)) ||
		got.ObservationMilliseconds != 20 || got.Status != "pass" {
		t.Fatal("preparation/join time inflated or revoked the earlier observation")
	}

	parentDeadline := time.Now().Add(time.Hour)
	ctx, cancel := context.WithDeadline(t.Context(), parentDeadline)
	defer cancel()
	calls := 0
	input := &inspectionTestBroker{read: func(context.Context, int) (string, error) {
		calls++
		t.Fatal("expired effective interval read input")
		return "N", nil
	}}
	stderr.Reset()
	got, err = observeInspection(ctx, input, &stderr, time.Second, func() time.Time { return parentDeadline.Add(time.Millisecond) })
	if err != nil || calls != 0 || stderr.Len() != 0 || got.Status != "aborted" || got.StopReason != "deadline" || got.ObservationMilliseconds != 0 {
		t.Fatal("expired effective interval acquired observation credit")
	}
}

type inspectionCyclicError struct{}

func (inspectionCyclicError) Error() string       { return "cyclic fixture error" }
func (value inspectionCyclicError) Unwrap() error { return value }

func TestInspectionCleanErrorTreesAndHardLeaves(t *testing.T) {
	hard := errors.New("synthetic hard fault")
	for _, test := range []struct {
		name          string
		read, stopped error
		reason        string
	}{
		{"returned_cancel", context.Canceled, nil, "operator_cancelled"},
		{"wrapped_cancel", fmt.Errorf("wrapper: %w", context.Canceled), nil, "operator_cancelled"},
		{"joined_cancel", errors.Join(context.Canceled, context.Canceled), nil, "operator_cancelled"},
		{"joined_stops", errors.Join(context.Canceled, context.DeadlineExceeded), nil, "deadline"},
		{"returned_deadline_outer_cancel", context.DeadlineExceeded, context.Canceled, "deadline"},
		{"returned_cancel_outer_deadline", context.Canceled, context.DeadlineExceeded, "deadline"},
		{"wrapped_outer", nil, fmt.Errorf("outer: %w", context.Canceled), "operator_cancelled"},
		{"eof_cancel", errors.Join(io.EOF, context.Canceled), nil, "operator_cancelled"},
		{"eof_deadline", errors.Join(io.EOF, context.DeadlineExceeded), nil, "deadline"},
		{"wrapped_eof", fmt.Errorf("wrapper: %w", io.EOF), nil, ""},
		{"only_eof", io.EOF, nil, ""},
		{"eof_with_outer_cancel", io.EOF, context.Canceled, "operator_cancelled"},
		{"hard_eof", errors.Join(hard, io.EOF), nil, "input_error"},
		{"hard_cancel", errors.Join(context.Canceled, hard), context.Canceled, "input_error"},
		{"hard_all", fmt.Errorf("wrapper: %w", errors.Join(io.EOF, context.Canceled, hard, context.DeadlineExceeded)), context.DeadlineExceeded, "input_error"},
		{"outer_hard", context.Canceled, hard, "input_error"},
		{"cycle", inspectionCyclicError{}, context.Canceled, "input_error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if reason := inspectionStop(test.read, test.stopped, false); reason != test.reason {
				t.Fatalf("reason=%q want=%q", reason, test.reason)
			}
		})
	}
	for _, test := range []struct {
		name, line, finding, reason, status string
		err                                 error
	}{
		{"returned_cancel", "N", "not_inspected", "operator_cancelled", "aborted", context.Canceled},
		{"wrapped_cancel", "D", "defect_observed", "operator_cancelled", "fail", fmt.Errorf("wrapper: %w", context.Canceled)},
		{"joined_cancel", "N", "not_inspected", "operator_cancelled", "aborted", errors.Join(context.Canceled, context.Canceled)},
		{"joined_deadline", "D", "defect_observed", "deadline", "fail", errors.Join(context.Canceled, context.DeadlineExceeded)},
		{"joined_eof_cancel", "D", "defect_observed", "operator_cancelled", "fail", errors.Join(io.EOF, context.Canceled)},
		{"joined_eof_deadline", "N", "not_inspected", "deadline", "aborted", errors.Join(io.EOF, context.DeadlineExceeded)},
		{"wrapped_eof", "", "not_inspected", "input_ended", "inconclusive", fmt.Errorf("wrapper: %w", io.EOF)},
		{"eof_cancel_hard", "D", "defect_observed", "input_error", "error", errors.Join(io.EOF, context.Canceled, hard)},
		{"wrapped_eof_hard", "", "not_inspected", "input_error", "error", errors.Join(fmt.Errorf("wrapper: %w", io.EOF), hard)},
		{"joined_hard", "D", "defect_observed", "input_error", "error", errors.Join(io.EOF, context.DeadlineExceeded, hard)},
	} {
		t.Run("observer/"+test.name, func(t *testing.T) {
			calls := 0
			source := &inspectionTestBroker{read: func(ctx context.Context, _ int) (string, error) {
				calls++
				if ctx.Err() != nil {
					t.Fatal("live outer-context prerequisite missing")
				}
				return test.line, test.err
			}}
			got, err := observeInspection(t.Context(), source, io.Discard, time.Second, inspectionClock(time.Now().Add(time.Hour), 17*time.Millisecond))
			if calls != 1 || err != nil || got.Finding != test.finding || got.StopReason != test.reason || got.Status != test.status || got.ResponseComplete != (test.line != "") {
				t.Fatalf("returned error tree lost: %+v err=%v", got, err)
			}
		})
	}
}
