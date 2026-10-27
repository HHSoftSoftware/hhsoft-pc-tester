package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func TestKeyboardSummariesShowFixedScopeWithoutTypedText(t *testing.T) {
	payload := thermalReportFixture(t, "session-v5-keyboard-pass")
	for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
		encoded, err := render(payload)
		if err != nil {
			t.Fatal(err)
		}
		for _, phrase := range []string{"left Shift, right Shift, Up, Down, Left, and Right", "typed text is not recorded", "event0", "6 of 6 press-and-release pairs", "Other keys and keyboard functions were not tested"} {
			// The PDF wraps long prose into fixed-width text lines.
			if !strings.Contains(strings.Join(strings.Fields(strings.ReplaceAll(string(encoded), ") Tj\nT*\n(", " ")), " "), phrase) && !bytes.Contains(encoded, []byte(phrase)) {
				t.Fatalf("keyboard summary omitted %q", phrase)
			}
		}
		if bytes.HasPrefix(encoded, []byte("<!doctype html>")) && bytes.Count(encoded, []byte("<pre>")) != 22 {
			t.Fatal("keyboard summary replaced generic evidence rows")
		}
		if bytes.HasPrefix(encoded, []byte("%PDF")) && bytes.Count(encoded, []byte("Evidence:")) != 22 {
			t.Fatal("keyboard summary replaced PDF evidence rows")
		}
	}
}

func TestKeyboardSummaryDoesNotTurnIncompleteCoverageIntoFailureOrPass(t *testing.T) {
	payload := thermalReportFixture(t, "session-v5-keyboard-pass")
	for index := range payload.TestResults {
		row := &payload.TestResults[index]
		if row.ID == "input.keyboard.quick" {
			var result inputtest.KeyboardResult
			if json.Unmarshal(row.Evidence, &result) != nil {
				t.Fatal("synthetic keyboard evidence unavailable")
			}
			result.Status = inputtest.StatusInconclusive
			result.StopReason = "observation_timeout"
			result.Detail = "observation_timeout"
			result.Devices[0].Status = inputtest.StatusInconclusive
			result.Devices[0].Detail = "incomplete_actions"
			result.Devices[0].FinalNeutral = false
			result.Devices[0].Actions[5].PairCompleted = false
			result.Devices[0].Actions[5].ReleaseObserved = false
			result.Devices[0].Actions[5].PressObserved = false
			row.Status = app.TestInconclusive
			row.Evidence, _ = json.Marshal(result)
		}
		if row.ID == "run.integrity.quick" {
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if err != nil {
				t.Fatal(err)
			}
			value.Session.Workers[4].Outcome = "inconclusive"
			row.Evidence, _ = json.Marshal(value)
		}
	}
	view, err := keyboardReportDocument(payload)
	if err != nil || view == nil || view.Status != "inconclusive" || view.Devices[0].Completed != "5 of 6 press-and-release pairs" || view.Devices[0].Missing != "Right" {
		t.Fatalf("incomplete keyboard view changed: %v", err)
	}
	for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
		encoded, err := render(payload)
		if err != nil || !bytes.Contains(encoded, []byte("Incomplete coverage.")) || bytes.Contains(encoded, []byte("All six controls completed")) {
			t.Fatalf("incomplete coverage misrepresented: %v", err)
		}
	}
}

func TestKeyboardMalformedEvidenceCannotReachEitherView(t *testing.T) {
	payload := thermalReportFixture(t, "session-v5-keyboard-pass")
	for index := range payload.TestResults {
		if payload.TestResults[index].ID == "input.keyboard.quick" {
			payload.TestResults[index].Evidence = bytes.Replace(payload.TestResults[index].Evidence, []byte(`"status": "pass"`), []byte(`"typed_text": "synthetic-forbidden-canary", "status": "pass"`), 1)
		}
	}
	for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
		if out, err := render(payload); err == nil || len(out) != 0 {
			t.Fatal("unvalidated keyboard content reached a rendered view")
		}
	}
}
