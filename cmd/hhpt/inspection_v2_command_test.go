package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
)

type conditionBatchBroker struct {
	read        func(context.Context, func() error) ([]exteriorinspection.ConditionResponseV2, error)
	directReads int
}

func (source *conditionBatchBroker) Read([]byte) (int, error) {
	source.directReads++
	return 0, errSessionInput
}
func (source *conditionBatchBroker) ReadInspectionConditions(ctx context.Context, prompt func() error) ([]exteriorinspection.ConditionResponseV2, error) {
	return source.read(ctx, prompt)
}

func TestExteriorConditionRealDispatchAndBufferedDelimiters(t *testing.T) {
	for _, test := range []struct {
		name, input, status, hinge string
		count                      int
	}{
		{"lf", "N\nA\nN\nN\n", "pass", "not_applicable", 4},
		{"crlf", "N\r\nA\r\nN\r\nN\r\n", "pass", "not_applicable", 4},
		{"bare_cr", "N\r", "inconclusive", "unobserved", 0},
		{"hidden_hinge", "N\nI\nN\nN\n", "inconclusive", "unobserved", 4},
		{"adverse_then_partial", "D\nN", "fail", "unobserved", 1},
		{"private_text", "synthetic-private-canary\nA\nN\nN\n", "inconclusive", "not_applicable", 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(t.Context(), []string{"inspect-exterior-condition"}, strings.NewReader(test.input), &stdout, &stderr)
			got, err := exteriorinspection.DecodeV2(bytes.TrimSpace(stdout.Bytes()))
			if code != 0 || err != nil || got.Status != test.status || got.Findings.HingeMountCondition != test.hinge ||
				got.ResponsesCompleted != test.count || bytes.Contains(stdout.Bytes(), []byte("synthetic-private-canary")) ||
				bytes.Contains(stderr.Bytes(), []byte("synthetic-private-canary")) {
				t.Fatalf("code=%d result=%+v err=%v", code, got, err)
			}
			for _, label := range []string{"1. Enclosure integrity", "2. Visible hinge and mount", "3. Visible port exterior", "4. Visible surface finish", "A only when no hinge exists"} {
				if !strings.Contains(stderr.String(), label) {
					t.Fatal("criterion or finite choices missing")
				}
			}
		})
	}
	source := strings.NewReader("N\nA\nN\nN\nEXTRA\n")
	var stdout, stderr bytes.Buffer
	if code := runInspectionConditionOwned(t.Context(), nil, source, &stdout, &stderr); code != 0 {
		t.Fatal("owned memory input failed")
	}
	remaining, err := io.ReadAll(source)
	if err != nil || string(remaining) != "EXTRA\n" {
		t.Fatal("input past four answers was consumed")
	}
}

func TestExteriorConditionSharedDeadlineAndLateAdverseAnswer(t *testing.T) {
	start := time.Now().Add(time.Hour)
	for _, test := range []struct {
		name, last, stop, status, finish string
		elapsed, parent                  time.Duration
		milliseconds                     int64
		hard                             bool
	}{
		{"before", "N", "completed", "pass", "no_issue_observed", 44999 * time.Millisecond, 0, 44999, false},
		{"exact", "N", "deadline", "aborted", "unobserved", 45 * time.Second, 0, 45000, false},
		{"after", "N", "deadline", "aborted", "unobserved", 46 * time.Second, 0, 45000, false},
		{"parent", "N", "deadline", "aborted", "unobserved", 13 * time.Second, 12 * time.Second, 12000, false},
		{"late_issue", "D", "deadline", "fail", "issue_present", 46 * time.Second, 0, 45000, false},
		{"hard_issue", "D", "input_error", "error", "issue_present", 46 * time.Second, 0, 45000, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			wantDeadline := start.Add(45 * time.Second)
			if test.parent != 0 {
				var cancel context.CancelFunc
				wantDeadline = start.Add(test.parent)
				ctx, cancel = context.WithDeadline(ctx, wantDeadline)
				defer cancel()
			}
			calls, prompts := 0, 0
			source := &conditionBatchBroker{read: func(actual context.Context, prompt func() error) ([]exteriorinspection.ConditionResponseV2, error) {
				calls++
				deadline, ok := actual.Deadline()
				if !ok || !deadline.Equal(wantDeadline) || actual.Err() != nil {
					t.Fatal("original effective deadline missing")
				}
				if err := prompt(); err != nil {
					t.Fatal(err)
				}
				prompts++
				responses := []exteriorinspection.ConditionResponseV2{{Text: "N", Complete: true}, {Text: "A", Complete: true}, {Text: "N", Complete: true}, {Text: test.last, Complete: true}}
				if test.hard {
					return responses, errors.Join(context.Canceled, errors.New("synthetic fault"))
				}
				return responses, nil
			}}
			got, err := observeInspectionCondition(ctx, source, io.Discard, 45*time.Second, inspectionClock(start, test.elapsed))
			if err != nil || calls != 1 || prompts != 1 || source.directReads != 0 || got.StopReason != test.stop || got.Status != test.status ||
				got.Findings.SurfaceFinishCondition != test.finish || got.ObservationMilliseconds != test.milliseconds {
				t.Fatalf("result=%+v err=%v", got, err)
			}
		})
	}
}

func TestExteriorConditionBatchFaultsRetainCompletedPrefix(t *testing.T) {
	for _, test := range []struct{ name, input, reason, status string }{
		{"partial_eof", "D\npartial", "input_ended", "fail"},
		{"overlong_second", "D\n" + strings.Repeat("x", 4097) + "\n", "input_error", "error"},
		{"empty_second", "D\n\nN\nN\n", "completed", "fail"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := observeInspectionCondition(t.Context(), strings.NewReader(test.input), io.Discard, time.Second, inspectionClock(time.Now().Add(time.Hour), 17*time.Millisecond))
			if err != nil || got.Findings.EnclosureIntegrity != "issue_present" || got.Findings.HingeMountCondition != "unobserved" ||
				got.Status != test.status || got.StopReason != test.reason {
				t.Fatalf("result=%+v err=%v", got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	data := []byte("D\npartial")
	position := 0
	responses, readErr := readInspectionConditionBytes(ctx, func(destination []byte) (int, error) {
		if len(destination) != 1 {
			t.Fatal("unbounded read")
		}
		destination[0] = data[position]
		position++
		if position == len(data) {
			cancel()
		}
		return 1, nil
	})
	if !errors.Is(readErr, context.Canceled) || position != len(data) || len(responses) != 2 ||
		!responses[0].Complete || responses[0].Text != "D" || responses[1].Complete || responses[1].Text != "" {
		t.Fatal("mid-line cancellation credited partial input or lost prefix")
	}
	got, err := exteriorinspection.EvaluateV2(responses, inspectionStop(readErr, ctx.Err(), false), 17)
	if err != nil || got.Status != "fail" || got.StopReason != "operator_cancelled" {
		t.Fatal("completed issue lost on cancellation")
	}
	for _, count := range []int{-1, 0, 2} {
		calls := 0
		_, err := readInspectionConditionBytes(t.Context(), func(destination []byte) (int, error) { calls++; destination[0] = '\n'; return count, nil })
		if err != errSessionInput || calls != 1 {
			t.Fatal("read protocol fault not terminal")
		}
	}
	source := &conditionBatchBroker{read: func(context.Context, func() error) ([]exteriorinspection.ConditionResponseV2, error) {
		return make([]exteriorinspection.ConditionResponseV2, 5), nil
	}}
	if _, err := observeInspectionCondition(t.Context(), source, io.Discard, time.Second, time.Now); err == nil {
		t.Fatal("fifth batch response admitted")
	}
}

func TestExteriorConditionOwnerJoinPrecedesAllOutput(t *testing.T) {
	for _, test := range []struct {
		name      string
		finishErr error
		code      int
	}{
		{"success", nil, 0}, {"owner_failure", errSessionInput, 1}, {"uncertain_cleanup", errSessionInputCleanup, 12},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []string
			source := &conditionBatchBroker{read: func(ctx context.Context, prompt func() error) ([]exteriorinspection.ConditionResponseV2, error) {
				events = append(events, "batch")
				if err := prompt(); err != nil {
					return nil, errInspectionConditionPrompt
				}
				return []exteriorinspection.ConditionResponseV2{{Text: "N", Complete: true}, {Text: "A", Complete: true}, {Text: "N", Complete: true}, {Text: "N", Complete: true}}, nil
			}}
			prepare := func(ctx context.Context, _ io.Reader) (context.Context, io.Reader, func() error, error) {
				events = append(events, "prepare")
				return ctx, source, func() error { events = append(events, "finish"); return test.finishErr }, nil
			}
			var stdout, stderr bytes.Buffer
			writes := 0
			write := func(path string, destination io.Writer, emit func(io.Writer) error) error {
				writes++
				if !reflect.DeepEqual(events, []string{"prepare", "batch", "finish"}) {
					t.Fatal("write preceded join")
				}
				events = append(events, "write")
				return writeDestination(path, destination, emit)
			}
			code := runInspectionConditionCommand(t.Context(), nil, strings.NewReader(""), &stdout, &stderr, false, prepare, write, inspectionClock(time.Now().Add(time.Hour), 17*time.Millisecond))
			if code != test.code || source.directReads != 0 {
				t.Fatal("owner result changed")
			}
			if test.code == 0 {
				if writes != 1 {
					t.Fatal("missing owned result")
				}
				if got, err := exteriorinspection.DecodeV2(bytes.TrimSpace(stdout.Bytes())); err != nil || got.Status != "pass" {
					t.Fatal("valid result missing")
				}
			} else if writes != 0 || stdout.Len() != 0 {
				t.Fatal("unconfirmed owner allowed output")
			}
		})
	}
}

func TestExteriorConditionPromptAndPreStoppedBoundaries(t *testing.T) {
	for _, short := range []bool{false, true} {
		reads, joins := 0, 0
		source := &conditionBatchBroker{read: func(ctx context.Context, prompt func() error) ([]exteriorinspection.ConditionResponseV2, error) {
			if err := prompt(); err != nil {
				return nil, errInspectionConditionPrompt
			}
			reads++
			return nil, nil
		}}
		prepare := func(ctx context.Context, _ io.Reader) (context.Context, io.Reader, func() error, error) {
			return ctx, source, func() error { joins++; return nil }, nil
		}
		prompt := inspectionPromptWriter{write: func(raw []byte) (int, error) {
			if short {
				return len(raw) - 1, nil
			}
			return 0, errors.New("synthetic prompt fault")
		}}
		var stdout bytes.Buffer
		code := runInspectionConditionCommand(t.Context(), nil, strings.NewReader(""), &stdout, prompt, false, prepare, writeDestination, inspectionClock(time.Now().Add(time.Hour), 17*time.Millisecond))
		got, err := exteriorinspection.DecodeV2(bytes.TrimSpace(stdout.Bytes()))
		if code != 1 || reads != 0 || joins != 1 || err != nil || got.StopReason != "prompt_error" || got.ResponsesCompleted != 0 {
			t.Fatal("prompt failure acquired input or bypassed join")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	prepare := func(context.Context, io.Reader) (context.Context, io.Reader, func() error, error) {
		t.Fatal("pre-stopped command prepared owner")
		return nil, nil, nil, nil
	}
	var stdout, stderr bytes.Buffer
	code := runInspectionConditionCommand(ctx, nil, strings.NewReader("N\nA\nN\nN\n"), &stdout, &stderr, false, prepare, writeDestination, func() time.Time { t.Fatal("pre-stopped command read clock"); return time.Time{} })
	got, err := exteriorinspection.DecodeV2(bytes.TrimSpace(stdout.Bytes()))
	if code != 0 || err != nil || got.Status != "aborted" || stderr.Len() != 0 {
		t.Fatal("stopped command gained authority")
	}
}

func TestExteriorConditionBetweenLineStopsPreservePriorCleanAndNA(t *testing.T) {
	for _, test := range []struct {
		name           string
		stop           error
		reason, status string
	}{
		{"cancel", context.Canceled, "operator_cancelled", "aborted"},
		{"owner_failure", errSessionInput, "input_error", "error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte("N\nA\n")
			position := 0
			responses, err := readInspectionConditionBytes(t.Context(), func(destination []byte) (int, error) {
				if position == len(raw) {
					return 0, test.stop
				}
				destination[0] = raw[position]
				position++
				return 1, nil
			})
			if err != test.stop || len(responses) != 3 || !responses[0].Complete || !responses[1].Complete || responses[2].Complete {
				t.Fatal("between-line stop lost explicit next slot")
			}
			got, err := exteriorinspection.EvaluateV2(responses, inspectionStop(err, nil, false), 17)
			if err != nil || got.Findings.EnclosureIntegrity != "no_issue_observed" || got.Findings.HingeMountCondition != "not_applicable" ||
				got.ResponsesCompleted != 2 || got.StopReason != test.reason || got.Status != test.status {
				t.Fatalf("prior observations erased: %+v %v", got, err)
			}
		})
	}
}
func TestExteriorConditionCoincidentDelimiterFaultDoesNotCreditCleanAnswer(t *testing.T) {
	for _, test := range []struct{ name, last, want string }{
		{"clean", "N", "unobserved"}, {"adverse", "D", "issue_present"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte("N\n" + test.last + "\n")
			position := 0
			fault := errors.New("synthetic delimiter fault")
			responses, err := readInspectionConditionBytes(t.Context(), func(destination []byte) (int, error) {
				destination[0] = raw[position]
				position++
				if position == len(raw) {
					return 1, fault
				}
				return 1, nil
			})
			if err != fault || len(responses) != 2 || !responses[1].Complete {
				t.Fatal("coincident delimiter prerequisite changed")
			}
			got, err := exteriorinspection.EvaluateV2(responses, inspectionStop(err, nil, false), 17)
			if err != nil || got.Status != "error" || got.Findings.EnclosureIntegrity != "no_issue_observed" || got.Findings.HingeMountCondition != test.want {
				t.Fatal("coincident fault acquired clean authority or lost adverse observation")
			}
		})
	}
}
func TestExteriorConditionCRBeforeLFCancellationIsIncomplete(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	raw := []byte("D\nN\r\nfuture\n")
	position := 0
	responses, err := readInspectionConditionBytes(ctx, func(destination []byte) (int, error) {
		destination[0] = raw[position]
		position++
		if position == 4 {
			cancel()
		}
		return 1, nil
	})
	if !errors.Is(err, context.Canceled) || position != 4 || len(responses) != 2 || responses[1].Complete || responses[1].Text != "" {
		t.Fatal("CR without LF acquired completion or lookahead")
	}
	got, err := exteriorinspection.EvaluateV2(responses, "operator_cancelled", 17)
	if err != nil || got.Status != "fail" || got.Findings.EnclosureIntegrity != "issue_present" || got.Findings.HingeMountCondition != "unobserved" {
		t.Fatal("interrupted CRLF lost earlier issue")
	}
}
