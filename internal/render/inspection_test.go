package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
)

func inspectionViewFixture(t *testing.T, finding, status, reason string, complete bool, milliseconds int64) app.InventoryPayload {
	t.Helper()
	observation := exteriorinspection.Result{
		Schema: "hhsoft.pc-tester.exterior-inspection/v1", Source: "operator_reported",
		Scope: "whole_device", Criterion: "visible_exterior_enclosure_damage",
		Finding: finding, Status: status, StopReason: reason, ResponseComplete: complete,
		ObservationMilliseconds: milliseconds,
	}
	encoded, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	payload := app.InventoryPayload{SchemaVersion: 1, CoverageScope: app.CoverageFrozenPlan, TestResults: []app.TestResult{
		{ID: "inspection.exterior.quick", Version: 1, Status: app.TestStatus(status), PlannedStatus: app.PlannedScheduled,
			PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: encoded},
		{ID: "run.integrity.quick", Version: 1, Status: app.TestPass,
			PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: json.RawMessage(`{"status":"pass"}`)},
	}}
	if err := app.ValidateCoverage(payload); err != nil {
		t.Fatal("valid standalone inspection prerequisite", err)
	}
	if value, err := app.ValidatedExteriorInspection(payload); err != nil || value == nil || *value != observation {
		t.Fatal("literal inspection prerequisite", err)
	}
	return payload
}

func TestExteriorInspectionViewsPreserveFiniteAssertions(t *testing.T) {
	// Literal semantic cases; the production reducer is not an expected-value oracle.
	for _, test := range []struct {
		name, finding, status, reason string
		complete                      bool
		milliseconds                  int64
	}{
		{"pass", "no_defect_observed", "pass", "completed", true, 0},
		{"defect", "defect_observed", "fail", "completed", true, 25},
		{"not_inspected", "not_inspected", "inconclusive", "not_inspected", true, 25},
		{"empty", "not_inspected", "inconclusive", "empty_response", true, 25},
		{"invalid", "not_inspected", "inconclusive", "invalid_response", true, 25},
		{"input_ended", "not_inspected", "inconclusive", "input_ended", false, 25},
		{"cancel_incomplete", "not_inspected", "aborted", "operator_cancelled", false, 25},
		{"cancel_complete", "not_inspected", "aborted", "operator_cancelled", true, 25},
		{"cancel_defect", "defect_observed", "fail", "operator_cancelled", true, 25},
		{"deadline_incomplete", "not_inspected", "aborted", "deadline", false, 45000},
		{"deadline_complete", "not_inspected", "aborted", "deadline", true, 45000},
		{"deadline_defect", "defect_observed", "fail", "deadline", true, 45000},
		{"input_error_incomplete", "not_inspected", "error", "input_error", false, 25},
		{"input_error_complete", "not_inspected", "error", "input_error", true, 25},
		{"input_error_defect", "defect_observed", "error", "input_error", true, 25},
		{"prompt_error", "not_inspected", "error", "prompt_error", false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := inspectionViewFixture(t, test.finding, test.status, test.reason, test.complete, test.milliseconds)
			before, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			html, err := InventoryHTMLForVersion(payload, "0.0.1-fixture")
			if err != nil {
				t.Fatal(err)
			}
			start := bytes.Index(html, []byte(`<section class="section" aria-labelledby="exterior-inspection-heading">`))
			if start < 0 {
				t.Fatal("missing inspection section")
			}
			end := bytes.Index(html[start:], []byte("</section>"))
			if end < 0 {
				t.Fatal("unterminated inspection section")
			}
			section := string(html[start : start+end])
			for label, value := range map[string]string{
				"Finding": test.finding, "Status": test.status, "Stop reason": test.reason,
				"Response complete": fmt.Sprint(test.complete), "Bounded observation time (ms)": fmt.Sprint(test.milliseconds),
			} {
				if !strings.Contains(section, `scope="row">`+label+`</th><td>`+value+`</td>`) {
					t.Fatalf("inspection field %s lost its exact value", label)
				}
			}
			for _, text := range []string{
				"Operator-reported exterior inspection",
				"Operator-reported observation of safely visible exterior enclosure surfaces. It covers only surfaces inspected without moving or opening the device.",
				"Observation time is capped at the observation deadline; preparation and cleanup are excluded.",
			} {
				if !strings.Contains(section, text) {
					t.Fatalf("inspection wording missing: %s", text)
				}
			}
			pdf, err := InventoryPDFForVersion(payload, "0.0.1-fixture")
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{
				"OPERATOR-REPORTED EXTERIOR INSPECTION", "Finding: " + test.finding, "Status: " + test.status,
				"Response complete: " + fmt.Sprint(test.complete), "Stop reason: " + test.reason,
				`Bounded observation time \050ms\051: ` + fmt.Sprint(test.milliseconds),
				"Operator-reported observation of safely visible exterior enclosure surfaces.",
				"Observation time is capped at the observation deadline; preparation and cleanup",
			} {
				if !bytes.Contains(pdf, []byte(text)) {
					t.Fatalf("PDF observation lost %q", text)
				}
			}
			for _, output := range [][]byte{html, pdf} {
				for _, claim := range []string{"independently verified inspection", "overall health passed", "repair verified", "hardware continuity verified"} {
					if bytes.Contains(output, []byte(claim)) {
						t.Fatal("view invented a physical claim")
					}
				}
			}
			againHTML, err := InventoryHTMLForVersion(payload, "0.0.1-fixture")
			if err != nil {
				t.Fatal(err)
			}
			againPDF, err := InventoryPDFForVersion(payload, "0.0.1-fixture")
			if err != nil {
				t.Fatal(err)
			}
			after, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(html, againHTML) || !bytes.Equal(pdf, againPDF) || !bytes.Equal(before, after) {
				t.Fatal("rendering changed its bytes or source evidence")
			}
		})
	}
}

type inspectionOutputCounter struct{ calls, bytes int }

func (w *inspectionOutputCounter) Write(raw []byte) (int, error) {
	w.calls++
	w.bytes += len(raw)
	return len(raw), nil
}

func TestExteriorInspectionRejectsClaimsBeforeRendering(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*app.InventoryPayload)
	}{
		{"malformed", func(p *app.InventoryPayload) { p.TestResults[0].Evidence = json.RawMessage(`{`) }},
		{"defect_as_pass", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("no_defect_observed"), []byte("defect_observed"), 1)
		}},
		{"row_status", func(p *app.InventoryPayload) { p.TestResults[0].Status = app.TestFail }},
		{"unknown_schema", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("inspection/v1"), []byte("inspection/v2"), 1)
		}},
		{"wrong_id", func(p *app.InventoryPayload) { p.TestResults[0].ID = "camera.quick" }},
		{"case_id", func(p *app.InventoryPayload) { p.TestResults[0].ID = "Inspection.Exterior.Quick" }},
		{"definition", func(p *app.InventoryPayload) { p.TestResults[0].Version = 2 }},
		{"duplicate_schema", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte(`"schema":`), []byte(`"schema":"opaque","schema":`), 1)
		}},
		{"unknown_member", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte(`{`), []byte(`{"name":"<script>untrusted</script>",`), 1)
		}},
		{"null_observation", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte(`"observation_milliseconds":25`), []byte(`"observation_milliseconds":null`), 1)
		}},
		{"negative_observation", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte(`"observation_milliseconds":25`), []byte(`"observation_milliseconds":-1`), 1)
		}},
		{"excess_observation", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte(`"observation_milliseconds":25`), []byte(`"observation_milliseconds":45001`), 1)
		}},
		{"old_duration_member", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte(`observation_milliseconds`), []byte(`duration_milliseconds`), 1)
		}},
		{"different_plan", func(p *app.InventoryPayload) { p.TestResults[0].PlanID = "sha256:" + strings.Repeat("c", 64) }},
		{"duplicate_row", func(p *app.InventoryPayload) { p.TestResults = append(p.TestResults, p.TestResults[0]) }},
		{"missing_integrity", func(p *app.InventoryPayload) { p.TestResults = p.TestResults[:1] }},
		{"invalid_other_row", func(p *app.InventoryPayload) { p.TestResults[1].Status = app.TestSkip }},
		{"missing_scope", func(p *app.InventoryPayload) {
			p.CoverageScope = ""
			for i := range p.TestResults {
				p.TestResults[i].PlannedStatus = ""
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := inspectionViewFixture(t, "no_defect_observed", "pass", "completed", true, 25)
			if _, err := InventoryHTML(payload); err != nil {
				t.Fatal("positive HTML prerequisite", err)
			}
			if _, err := InventoryPDF(payload); err != nil {
				t.Fatal("positive PDF prerequisite", err)
			}
			before, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			test.change(&payload)
			after, err := json.Marshal(payload)
			if err == nil && bytes.Equal(before, after) {
				t.Fatal("mutation did not change its prerequisite")
			}
			var output inspectionOutputCounter
			if err := RenderInventoryHTML(&output, payload); err == nil || output.calls != 0 || output.bytes != 0 {
				t.Fatal("invalid claim reached the output writer")
			}
			if raw, err := InventoryHTML(payload); err == nil || len(raw) != 0 {
				t.Fatal("invalid claim produced HTML")
			}
			if raw, err := InventoryPDF(payload); err == nil || len(raw) != 0 {
				t.Fatal("invalid claim produced PDF")
			}
		})
	}
}

func TestExteriorInspectionNotesDoNotInventAnObservation(t *testing.T) {
	for _, test := range []struct{ name, planned, status, source, detail string }{
		{"excluded", "excluded", "skip", "excluded", "not run; disposition recorded in the frozen plan"},
		{"aborted", "scheduled", "aborted", "aborted", "bounded evidence was not produced; outcome recorded by the run supervisor"},
		{"error", "scheduled", "error", "error", "bounded evidence was not produced; outcome recorded by the run supervisor"},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := inspectionViewFixture(t, "no_defect_observed", "pass", "completed", true, 25)
			encoded, err := json.Marshal(map[string]string{"schema": "hhsoft.pc-tester.execution-note/v1", "status": test.status, "source_status": test.source, "detail": test.detail})
			if err != nil {
				t.Fatal(err)
			}
			payload.TestResults[0].PlannedStatus = app.PlannedStatus(test.planned)
			payload.TestResults[0].Status = app.TestStatus(test.status)
			payload.TestResults[0].Evidence = encoded
			if err := app.ValidateCoverage(payload); err != nil {
				t.Fatal("valid note prerequisite", err)
			}
			if view, err := inspectionReportDocument(payload); err != nil || view != nil {
				t.Fatal("note invented a typed observation", err)
			}
			html, err := InventoryHTML(payload)
			if err != nil {
				t.Fatal(err)
			}
			pdf, err := InventoryPDF(payload)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(html, []byte(`id="exterior-inspection-heading"`)) || bytes.Contains(pdf, []byte("OPERATOR-REPORTED EXTERIOR INSPECTION")) {
				t.Fatal("execution note invented an observation section")
			}
			if !bytes.Contains(html, []byte(test.detail)) || !bytes.Contains(pdf, []byte("inspection.exterior.quick")) {
				t.Fatal("ordinary execution evidence disappeared")
			}
		})
	}
}

func TestExteriorInspectionAbsentRetainsLiteralLegacyBytes(t *testing.T) {
	payload := app.InventoryPayload{SchemaVersion: 1}
	if view, err := inspectionReportDocument(payload); err != nil || view != nil {
		t.Fatal("absent inspection acquired a view", err)
	}
	for _, test := range []struct {
		name, digest string
		size         int
		render       func(app.InventoryPayload, string) ([]byte, error)
	}{
		{"html", "b1930fbb47706d5326913cc470b7bc4746c477b894bf7288ddb677f2b70de775", 3013, InventoryHTMLForVersion},
		{"pdf", "b1582386321add5decc26ff326aa1d84dcb5287436c093dfae68874c1228cf2a", 1166, InventoryPDFForVersion},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := test.render(payload, "0.0.1-fixture")
			if err != nil || len(encoded) != test.size || fmt.Sprintf("%x", sha256.Sum256(encoded)) != test.digest {
				t.Fatal("no-inspection legacy bytes changed", err)
			}
		})
	}
	before := []string{"retained line"}
	lines := pdfLines{values: append([]string(nil), before...)}
	appendInspectionPDF(&lines, nil)
	if !reflect.DeepEqual(lines.values, before) || lines.err != nil {
		t.Fatal("nil PDF observation changed layout")
	}
}
