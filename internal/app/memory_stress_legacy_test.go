package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func TestMemoryStressHistoricalStandardPayloadRemainsUnobserved(t *testing.T) {
	// This retained literal predates Extended activation. It is deliberately not
	// rebuilt from current plan/catalog aliases.
	payload := standardStressPayload(t)
	foundMemory, foundJournal := false, false
	for _, row := range payload.TestResults {
		switch row.ID {
		case "memory.stress":
			foundMemory = true
		case "run.integrity.quick":
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			foundJournal = err == nil && value.Session != nil && value.Session.Schema == "hhsoft.pc-tester.quick-session/v8" &&
				len(value.Session.Workers) == 13 && value.Session.MemoryStressSource == nil
		}
	}
	if foundMemory || !foundJournal {
		t.Fatal("literal historical Standard prerequisite changed")
	}
	value, err := ValidatedMemoryStress(payload)
	if err != nil || value != nil {
		t.Fatal("historical report acquired an observed memory claim", err)
	}
}

func TestMemoryStressHistoricalFailureNotesRequireExactShape(t *testing.T) {
	note := func(status TestStatus) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"schema":"hhsoft.pc-tester.execution-note/v1","status":%q,"source_status":%q,"detail":"bounded evidence was not produced; outcome recorded by the run supervisor"}`, status, status))
	}
	for _, version := range []int{1, 2} {
		for _, status := range []TestStatus{TestError, TestAborted} {
			row := TestResult{ID: "memory.stress", Version: version, Status: status, Evidence: note(status)}
			if !memoryStressFailureNote(row) || HasMemoryStressClaim(row.Evidence) {
				t.Fatal("literal historical non-observation prerequisite")
			}
		}
	}
	for _, mode := range []string{"version_zero", "future_version", "case_id", "pass", "future_schema",
		"unknown_member", "duplicate_member", "wrong_source_status", "changed_detail", "case_member"} {
		t.Run(mode, func(t *testing.T) {
			row := TestResult{ID: "memory.stress", Version: 1, Status: TestAborted, Evidence: note(TestAborted)}
			original := append([]byte(nil), row.Evidence...)
			switch mode {
			case "version_zero":
				row.Version = 0
			case "future_version":
				row.Version = 3
			case "case_id":
				row.ID = "MEMORY.STRESS"
			case "pass":
				row.Status, row.Evidence = TestPass, note(TestPass)
			case "future_schema":
				row.Evidence = bytes.Replace(row.Evidence, []byte("execution-note/v1"), []byte("execution-note/v99"), 1)
			case "unknown_member":
				row.Evidence = append([]byte(`{"extra":true,`), row.Evidence[1:]...)
			case "duplicate_member":
				row.Evidence = append([]byte(`{"status":"aborted",`), row.Evidence[1:]...)
			case "wrong_source_status":
				row.Evidence = bytes.Replace(row.Evidence, []byte(`"source_status":"aborted"`), []byte(`"source_status":"error"`), 1)
			case "changed_detail":
				row.Evidence = bytes.Replace(row.Evidence, []byte("bounded evidence was not produced"), []byte("observed memory passed"), 1)
			case "case_member":
				row.Evidence = bytes.Replace(row.Evidence, []byte(`"source_status"`), []byte(`"Source_status"`), 1)
			}
			if mode != "version_zero" && mode != "future_version" && mode != "case_id" && bytes.Equal(original, row.Evidence) {
				t.Fatal("note mutation ineffective")
			}
			if memoryStressFailureNote(row) {
				t.Fatal("noncanonical note bypassed strict family selection")
			}
		})
	}
}
