package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/pointerevidence"
)

func TestPointerPreflightKeepsExistingMalformedEvidenceFatal(t *testing.T) {
	for _, code := range []int{0, 1} {
		for _, interrupted := range []bool{false, true} {
			for _, raw := range [][]byte{
				[]byte(`{"schema":"hhsoft.pc-tester.pointer-functional/v2","status":"error"}`),
				[]byte(`{"schema":"hhsoft.pc-tester.pointer-functional/v1","status":"error"}`),
				[]byte(`{"schema":"hhsoft.pc-tester.execution-note/v1","status":"error","source_status":"error","detail":"missing"}`),
			} {
				if _, _, err := sessionWorkerOutcomeEvidence(raw, nil, inputtest.PointerSchema, code, interrupted); !errors.Is(err, pointerevidence.ErrInvalid) {
					t.Fatal("existing malformed pointer evidence became a supervisor outcome")
				}
			}
			if _, _, err := sessionWorkerOutcomeEvidence(nil, errors.New("existing evidence unreadable"), inputtest.PointerSchema, code, interrupted); !errors.Is(err, pointerevidence.ErrInvalid) {
				t.Fatal("unreadable existing evidence became a missing-file note")
			}
		}
	}
	if outcome, state, err := sessionWorkerOutcomeEvidence(nil, os.ErrNotExist, inputtest.PointerSchema, 1, false); err != nil || outcome != "error" || state != "completed" {
		t.Fatal("direct absence lost the honest error-note fallback")
	}
}

func TestPointerPreflightRetainsHonestInterruptedWheelEvidence(t *testing.T) {
	value := completePointerEvidence()
	withPointerWheelCriteria(&value)
	value.MarkInterrupted(context.DeadlineExceeded)
	raw := encodeEvidenceFixture(t, value)
	if outcome, state, err := sessionWorkerOutcomeEvidence(raw, nil, inputtest.PointerSchema, 1, true); err != nil || outcome != "error" || state != "interrupted" {
		t.Fatal("valid interrupted wheel observations were discarded")
	}
}
