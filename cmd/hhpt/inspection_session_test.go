package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
)

type inspectionBorrowedFixture struct {
	t               *testing.T
	ctx             context.Context
	prompt          *bytes.Buffer
	discardErr      error
	discards, reads int
	response        string
	readErr         error
	cancel          context.CancelFunc
}

func (f *inspectionBorrowedFixture) Read([]byte) (int, error) {
	f.t.Fatal("borrowed owner used as an unbounded reader")
	return 0, io.ErrNoProgress
}
func (f *inspectionBorrowedFixture) DiscardUnpromptedInput(ctx context.Context) error {
	f.discards++
	if ctx != f.ctx || f.prompt.Len() != 0 || f.reads != 0 {
		f.t.Fatal("discard did not precede the prompt on the borrowed context")
	}
	if f.discardErr != nil && f.cancel != nil {
		f.cancel()
	}
	return f.discardErr
}
func (f *inspectionBorrowedFixture) ReadFindingLine(ctx context.Context, limit int) (string, error) {
	f.reads++
	deadline, ok := ctx.Deadline()
	if f.discards != 1 || !strings.Contains(f.prompt.String(), inspectionPrompt) || limit != 4096 || !ok || time.Until(deadline) > 45*time.Second {
		f.t.Fatal("real borrowed adapter ordering/bound changed")
	}
	if f.cancel != nil {
		f.cancel()
	}
	return f.response, f.readErr
}

func TestInspectionSessionAdapterConsumesBorrowedInputAfterDiscard(t *testing.T) {
	for _, test := range []struct {
		name, response, status, finding, stop string
		readErr, discardErr                   error
		cancel                                bool
		code, reads                           int
	}{
		{"pass", "N", "pass", "no_defect_observed", "completed", nil, nil, false, 0, 1},
		{"defect", "D", "fail", "defect_observed", "completed", nil, nil, false, 0, 1},
		{"incomplete", "I", "inconclusive", "not_inspected", "not_inspected", nil, nil, false, 0, 1},
		{"cancel_defect", "D", "fail", "defect_observed", "operator_cancelled", context.Canceled, nil, true, 0, 1},
		{"hard_cancel_defect", "D", "error", "defect_observed", "input_error", errors.Join(errors.New("owned hard fault"), context.Canceled), nil, true, 1, 1},
		{"hard_discard", "", "error", "not_inspected", "input_error", nil, errors.Join(errors.New("handoff hard fault"), context.Canceled), true, 1, 0},
		{"clean_discard", "", "aborted", "not_inspected", "operator_cancelled", nil, context.Canceled, true, 0, 0},
		{"uncertain_discard", "", "", "", "", nil, errSessionInputCleanup, false, workerCleanupUncertainExitCode, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var output, prompt bytes.Buffer
			f := &inspectionBorrowedFixture{t: t, ctx: ctx, prompt: &prompt, response: test.response, readErr: test.readErr, discardErr: test.discardErr}
			if test.cancel {
				f.cancel = cancel
			}
			path := filepath.Join(t.TempDir(), "inspection.json")
			worker := sessionWorker{ID: "inspection.exterior.quick", Architecture: "x86_64", OutputPath: path, Timeout: 45 * time.Second}
			code := runSessionWorker(ctx, worker, f, &output, &prompt)
			if code != test.code || f.discards != 1 || f.reads != test.reads {
				t.Fatalf("real dispatch failed: code=%d discard=%d read=%d", code, f.discards, f.reads)
			}
			raw, err := os.ReadFile(path)
			if code == workerCleanupUncertainExitCode {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatal("uncertain handoff emitted evidence")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			value, err := exteriorinspection.Decode(raw)
			if err != nil || value.Status != test.status || value.Finding != test.finding || value.StopReason != test.stop || value.ResponseComplete != (test.reads == 1) {
				t.Fatal("actual dispatch lost typed outcome", err)
			}
			if test.reads == 0 && (value.ObservationMilliseconds != 0 || strings.Contains(prompt.String(), inspectionPrompt)) {
				t.Fatal("handoff failure claimed observation")
			}
			if bytes.Contains(raw, []byte("fault")) || bytes.Contains(raw, []byte(path)) {
				t.Fatal("raw fault or path escaped")
			}
			outcome, state, err := sessionWorkerOutcomeEvidence(raw, nil, exteriorinspection.Schema, code, ctx.Err() != nil)
			wantState := "completed"
			if ctx.Err() != nil && (test.status == "error" || test.status == "aborted") {
				wantState = "interrupted"
			}
			if err != nil || outcome != test.status || state != wantState {
				t.Fatal("session reducer erased real adapter evidence", err)
			}
		})
	}
}

func TestInspectionSessionAndFinalizerRejectPresentBadEvidence(t *testing.T) {
	raw := []byte("{\"schema\":\"hhsoft.pc-tester.exterior-inspection/v1\",\"source\":\"operator_reported\",\"scope\":\"whole_device\",\"criterion\":\"visible_exterior_enclosure_damage\",\"finding\":\"no_defect_observed\",\"response_complete\":true,\"stop_reason\":\"completed\",\"status\":\"pass\",\"observation_milliseconds\":0}")
	if status, err := testEvidenceStatus(raw, exteriorinspection.Schema); err != nil || status != "pass" {
		t.Fatal("independent positive prerequisite")
	}
	for _, test := range []struct {
		name    string
		raw     []byte
		readErr error
		code    int
	}{
		{"malformed", []byte("{"), nil, 1},
		{"scope", bytes.Replace(raw, []byte("whole_device"), []byte("component"), 1), nil, 1},
		{"unknown_version", bytes.Replace(raw, []byte("inspection/v1"), []byte("inspection/v99"), 1), nil, 1},
		{"oversized", append(bytes.Clone(raw), bytes.Repeat([]byte(" "), 4096)...), nil, 1},
		{"existing_unreadable", nil, errors.New("owned read fault"), 1},
		{"wrapped_absence", nil, errors.Join(os.ErrNotExist, errors.New("owned read fault")), 1},
		{"output_failed_after_bytes", raw, nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.readErr == nil && test.name != "output_failed_after_bytes" {
				if _, err := testEvidenceStatus(test.raw, exteriorinspection.Schema); err == nil {
					t.Fatal("typed finalizer accepted bad existing evidence")
				}
			}
			for _, interrupted := range []bool{false, true} {
				if _, _, err := sessionWorkerOutcomeEvidence(test.raw, test.readErr, exteriorinspection.Schema, test.code, interrupted); err == nil {
					t.Fatal("present failure became an ordinary supervisor note")
				}
			}
		})
	}
	for _, interrupted := range []bool{false, true} {
		status, state, err := sessionWorkerOutcomeEvidence(nil, os.ErrNotExist, exteriorinspection.Schema, 1, interrupted)
		wantStatus, wantState := "error", "completed"
		if interrupted {
			wantStatus, wantState = "aborted", "interrupted"
		}
		if err != nil || status != wantStatus || state != wantState {
			t.Fatal("direct absence lost bounded nonexecution")
		}
	}
	var value exteriorinspection.Result
	if json.Unmarshal(raw, &value) != nil {
		t.Fatal("literal prerequisite")
	}
	if _, err := newValidatedQuickTestResult("inspection.exterior.quick", 2, "pass", value); err == nil {
		t.Fatal("wrong inspection definition accepted")
	}
	if _, err := newValidatedQuickTestResult("inspection.exterior.quick", 1, "pass", value); err != nil {
		t.Fatal("current typed definition rejected", err)
	}
}
