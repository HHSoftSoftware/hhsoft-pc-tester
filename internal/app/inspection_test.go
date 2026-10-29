package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

const reportInspectionPass = "{\"schema\":\"hhsoft.pc-tester.exterior-inspection/v1\",\"source\":\"operator_reported\",\"scope\":\"whole_device\",\"criterion\":\"visible_exterior_enclosure_damage\",\"finding\":\"no_defect_observed\",\"response_complete\":true,\"stop_reason\":\"completed\",\"status\":\"pass\",\"observation_milliseconds\":1}"

func standaloneInspectionFixture() InventoryPayload {
	p := coverageFixture()
	p.TestResults[0].ID = "inspection.exterior.quick"
	p.TestResults[0].Version = 1
	p.TestResults[0].Status = TestPass
	p.TestResults[0].PlannedStatus = PlannedScheduled
	p.TestResults[0].Evidence = json.RawMessage(reportInspectionPass)
	return p
}

func requireInspectionReport(t *testing.T, p InventoryPayload) {
	t.Helper()
	if ValidateCoverage(p) != nil {
		t.Fatal("positive report coverage prerequisite")
	}
	if _, err := ValidatedSessionThermal(p); err != nil {
		t.Fatal("positive separate consumer prerequisite", err)
	}
	if _, err := ValidatedExteriorInspection(p); err != nil {
		t.Fatal("positive copied-view prerequisite", err)
	}
}

func rejectInspectionReport(t *testing.T, p InventoryPayload) {
	t.Helper()
	if ValidateCoverage(p) == nil {
		t.Fatal("coverage accepted invalid inspection")
	}
	if _, err := ValidatedSessionThermal(p); err == nil {
		t.Fatal("separate thermal consumer bypassed inspection")
	}
	if _, err := ValidatedExteriorInspection(p); err == nil {
		t.Fatal("view consumer accepted invalid inspection")
	}
}

func TestInspectionStandaloneReturnsIndependentFiniteObservation(t *testing.T) {
	for _, test := range []struct {
		name, finding, stop, status string
		complete                    bool
	}{
		{"pass", "no_defect_observed", "completed", "pass", true},
		{"defect", "defect_observed", "completed", "fail", true},
		{"hard_defect", "defect_observed", "input_error", "error", true},
		{"late_defect", "defect_observed", "deadline", "fail", true},
		{"incomplete", "not_inspected", "input_ended", "inconclusive", false},
		{"stopped", "not_inspected", "operator_cancelled", "aborted", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := standaloneInspectionFixture()
			want := exteriorinspection.Result{Schema: "hhsoft.pc-tester.exterior-inspection/v1", Source: "operator_reported", Scope: "whole_device", Criterion: "visible_exterior_enclosure_damage", Finding: test.finding, ResponseComplete: test.complete, StopReason: test.stop, Status: test.status, ObservationMilliseconds: 1}
			raw, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			p.TestResults[0].Evidence = raw
			p.TestResults[0].Status = TestStatus(test.status)
			requireInspectionReport(t, p)
			got, err := ValidatedExteriorInspection(p)
			if err != nil || got == nil || *got != want {
				t.Fatal("typed fields changed", err)
			}
			got.Finding = "modified"
			again, err := ValidatedExteriorInspection(p)
			if err != nil || again == nil || *again != want || !bytes.Equal(p.TestResults[0].Evidence, raw) {
				t.Fatal("returned value aliases source")
			}
		})
	}
}

func TestInspectionReportCannotHideWrongDefinitionsOrBindings(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*InventoryPayload)
	}{
		{"wrong_id", func(p *InventoryPayload) { p.TestResults[0].ID = "camera.quick" }},
		{"case_id", func(p *InventoryPayload) { p.TestResults[0].ID = "Inspection.Exterior.Quick" }},
		{"version", func(p *InventoryPayload) { p.TestResults[0].Version = 2 }},
		{"scope", func(p *InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("whole_device"), []byte("component"), 1)
		}},
		{"status", func(p *InventoryPayload) { p.TestResults[0].Status = TestFail }},
		{"missing_evidence", func(p *InventoryPayload) { p.TestResults[0].Evidence = json.RawMessage("{}") }},
		{"unknown_schema", func(p *InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("inspection/v1"), []byte("inspection/v99"), 1)
		}},
		{"case_schema", func(p *InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("exterior-inspection"), []byte("EXTERIOR-INSPECTION"), 1)
		}},
		{"duplicate_schema", func(p *InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("\"schema\":"), []byte("\"schema\":\"opaque\",\"schema\":"), 1)
		}},
		{"direct_capture", func(p *InventoryPayload) {
			p.CoverageScope = CoverageDirectCapture
			for i := range p.TestResults {
				p.TestResults[i].PlanID = ""
				p.TestResults[i].HardwareGraph = ""
				p.TestResults[i].PlannedStatus = ""
			}
		}},
		{"legacy_scope", func(p *InventoryPayload) {
			p.CoverageScope = ""
			for i := range p.TestResults {
				p.TestResults[i].PlannedStatus = ""
			}
		}},
		{"stale_plan", func(p *InventoryPayload) { p.TestResults[0].PlanID = "sha256:" + strings.Repeat("c", 64) }},
		{"stale_graph", func(p *InventoryPayload) { p.TestResults[0].HardwareGraph = "hmac-sha256:" + strings.Repeat("c", 64) }},
		{"duplicate_row", func(p *InventoryPayload) { p.TestResults = append(p.TestResults, p.TestResults[0]) }},
		{"excluded_typed", func(p *InventoryPayload) {
			p.TestResults[0].PlannedStatus = PlannedExcluded
			p.TestResults[0].Status = TestSkip
		}},
		{"malformed", func(p *InventoryPayload) { p.TestResults[0].Evidence = []byte("{") }},
		{"opaque_session", func(p *InventoryPayload) {
			p.TestResults[1].Evidence = []byte("{\"status\":\"pass\",\"session\":{\"schema\":\"hhsoft.pc-tester.quick-session/v3\"}}")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := standaloneInspectionFixture()
			requireInspectionReport(t, p)
			test.change(&p)
			rejectInspectionReport(t, p)
		})
	}
}

func TestInspectionNotesAreNotRenderedAsObservations(t *testing.T) {
	for _, test := range []struct{ name, planned, status, detail string }{
		{"excluded", "excluded", "skip", "not run; disposition recorded in the frozen plan"},
		{"error", "scheduled", "error", "bounded evidence was not produced; outcome recorded by the run supervisor"},
		{"aborted", "scheduled", "aborted", "bounded evidence was not produced; outcome recorded by the run supervisor"},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := standaloneInspectionFixture()
			r := &p.TestResults[0]
			r.PlannedStatus = PlannedStatus(test.planned)
			r.Status = TestStatus(test.status)
			source := test.status
			if test.planned == "excluded" {
				source = "excluded"
			}
			raw, err := json.Marshal(map[string]string{"schema": "hhsoft.pc-tester.execution-note/v1", "status": test.status, "source_status": source, "detail": test.detail})
			if err != nil {
				t.Fatal(err)
			}
			r.Evidence = raw
			requireInspectionReport(t, p)
			if got, err := ValidatedExteriorInspection(p); err != nil || got != nil {
				t.Fatal("note invented an observation")
			}
			r.Evidence = bytes.Replace(raw, []byte(test.detail), []byte("unbounded text"), 1)
			rejectInspectionReport(t, p)
		})
	}
	for _, p := range []InventoryPayload{{}, coverageFixture()} {
		if got, err := ValidatedExteriorInspection(p); err != nil || got != nil {
			t.Fatal("legacy report acquired inspection")
		}
	}
}

func inspectionSessionFixture(t *testing.T) InventoryPayload {
	t.Helper()
	p := thermalFixture(t, "session-v4-unavailable")
	requireInspectionReport(t, p)
	var integrity *TestResult
	for i := range p.TestResults {
		r := &p.TestResults[i]
		if r.ID == "input.keyboard.quick" {
			r.Version = 2
			r.PlannedStatus = PlannedExcluded
			r.Status = TestSkip
			r.Evidence = []byte("{\"schema\":\"hhsoft.pc-tester.execution-note/v1\",\"status\":\"skip\",\"source_status\":\"excluded\",\"detail\":\"not run; disposition recorded in the frozen plan\"}")
		}
		if r.ID == "run.integrity.quick" {
			integrity = r
		}
	}
	if integrity == nil || p.TestResults[len(p.TestResults)-1].ID != "run.integrity.quick" {
		t.Fatal("literal historical integrity must be the final row")
	}
	value, err := sessionevidence.DecodeIntegrity(integrity.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	j := value.Session
	j.Schema = sessionevidence.SchemaV7
	j.Workers = append(j.Workers[:4:4], append([]sessionevidence.WorkerRecord{{ID: "input.keyboard.quick", State: "not_scheduled", Outcome: "excluded"}}, j.Workers[4:]...)...)
	j.Workers = append(j.Workers, sessionevidence.WorkerRecord{ID: "inspection.exterior.quick", State: "completed", Outcome: "pass", Timing: &sessionevidence.WorkerTiming{StartedMilliseconds: 5, FinishedMilliseconds: 6}})
	j.Safety = append(j.Safety[:1:1], append([]sessionevidence.SafetyRecord{{Phase: "before-inspection.exterior.quick", Result: j.Safety[0].Result}}, j.Safety[1:]...)...)
	integrity.Evidence, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	inspection := TestResult{ID: "inspection.exterior.quick", Version: 1, Status: TestPass, PlannedStatus: PlannedScheduled, PlanID: integrity.PlanID, HardwareGraph: integrity.HardwareGraph, Evidence: []byte(reportInspectionPass)}
	integrityRow := *integrity
	p.TestResults = append(p.TestResults[:len(p.TestResults)-1], inspection, integrityRow)
	requireInspectionReport(t, p)
	return p
}

func TestInspectionReportRequiresTheActualV7WorkerBinding(t *testing.T) {
	p := inspectionSessionFixture(t)
	if got, err := ValidatedExteriorInspection(p); err != nil || got == nil || got.Status != "pass" || got.ObservationMilliseconds != 1 {
		t.Fatal("v7 positive binding", err)
	}
	for _, test := range []struct {
		name   string
		change func(*sessionevidence.IntegrityEvidence)
	}{
		{"v4", func(v *sessionevidence.IntegrityEvidence) { v.Session.Schema = sessionevidence.SchemaV4 }},
		{"v5", func(v *sessionevidence.IntegrityEvidence) { v.Session.Schema = sessionevidence.SchemaV5 }},
		{"v6", func(v *sessionevidence.IntegrityEvidence) { v.Session.Schema = sessionevidence.SchemaV6 }},
		{"missing_worker", func(v *sessionevidence.IntegrityEvidence) { v.Session.Workers = v.Session.Workers[:11] }},
		{"outcome", func(v *sessionevidence.IntegrityEvidence) { v.Session.Workers[11].Outcome = "inconclusive" }},
		{"short_interval", func(v *sessionevidence.IntegrityEvidence) { v.Session.Workers[11].Timing.FinishedMilliseconds = 5 }},
		{"unstarted", func(v *sessionevidence.IntegrityEvidence) {
			v.Session.Workers[11].State = "not_started"
			v.Session.Workers[11].Timing = nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := inspectionSessionFixture(t)
			mutateIntegrity(t, &v, test.change)
			rejectInspectionReport(t, v)
		})
	}
	for _, name := range []string{"session-v4-unavailable", "session-v4-global-heat", "session-v4-local-heat"} {
		t.Run(name, func(t *testing.T) {
			v := thermalFixture(t, name)
			requireInspectionReport(t, v)
			var parent TestResult
			for _, r := range v.TestResults {
				if r.ID == "run.integrity.quick" {
					parent = r
				}
			}
			v.TestResults = append(v.TestResults, TestResult{ID: "inspection.exterior.quick", Version: 1, Status: TestPass, PlannedStatus: PlannedScheduled, PlanID: parent.PlanID, HardwareGraph: parent.HardwareGraph, Evidence: []byte(reportInspectionPass)})
			rejectInspectionReport(t, v)
		})
	}
}

func TestInspectionWholeReportClaimDetectionCannotLoseEarlierTokens(t *testing.T) {
	for _, raw := range []string{
		"{\"test_results\":[{\"id\":\"inspection.exterior.quick\",\"id\":\"camera.quick\",\"evidence\":{}}]}",
		"{\"test_results\":[{\"ID\":\"INSPECTION.EXTERIOR.QUICK\"}]}",
		"{\"test_results\":[{\"id\":\"inspection\\u002eexterior.quick\"}]}",
		"{\"evidence\":{\"schema\":\"hhsoft.pc-tester.exterior-inspection/v99\",\"schema\":\"opaque\"}}",
		"{\"test_results\":{\"not_an_array\":{\"id\":\"inspection.exterior.quick\"}}}",
		"{\"schema\":\"hhsoft.pc-tester.exterior-inspection/v1\"",
	} {
		if !HasExteriorInspectionClaim([]byte(raw)) {
			t.Fatal("claim lost before strict report decoding")
		}
	}
}

func inspectionExcludedReportFixture(t *testing.T) InventoryPayload {
	t.Helper()
	p := standaloneInspectionFixture()
	p.TestResults[0].Status = TestSkip
	p.TestResults[0].PlannedStatus = PlannedExcluded
	p.TestResults[0].Evidence = json.RawMessage("{\"schema\":\"hhsoft.pc-tester.execution-note/v1\",\"status\":\"skip\",\"source_status\":\"excluded\",\"detail\":\"not run; disposition recorded in the frozen plan\"}")
	row := &p.TestResults[1]
	value := sessionevidence.IntegrityEvidence{Schema: sessionevidence.IntegritySchema, Status: "pass", PlanID: row.PlanID, PlannedHardwareGraph: row.HardwareGraph, FinalHardwareGraph: row.HardwareGraph, ExcludedTests: []string{"inspection.exterior.quick"}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	row.Evidence = raw
	requireInspectionReport(t, p)
	if got, err := ValidatedExteriorInspection(p); err != nil || got != nil {
		t.Fatal("exclusion reference invented an observation")
	}
	return p
}

func TestInspectionIntegrityExclusionReferencesRequireTheActualExcludedRow(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*InventoryPayload)
	}{
		{"missing_row", func(p *InventoryPayload) { p.TestResults[0] = coverageFixture().TestResults[0] }},
		{"scheduled_row", func(p *InventoryPayload) { p.TestResults[0] = standaloneInspectionFixture().TestResults[0] }},
		{"aliased_reference", func(p *InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte("inspection.exterior.quick"), []byte("Inspection.Exterior.Quick"), 1)
		}},
		{"duplicate_reference", func(p *InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte("[\"inspection.exterior.quick\"]"), []byte("[\"inspection.exterior.quick\",\"inspection.exterior.quick\"]"), 1)
		}},
		{"hidden_alias_reference", func(p *InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte("[\"inspection.exterior.quick\"]"), []byte("[\"inspection.exterior.quick\",\"INSPECTION.EXTERIOR.QUICK\"]"), 1)
		}},
		{"duplicate_exclusion_member", func(p *InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte("\"excluded_tests\":"), []byte("\"excluded_tests\":[],\"excluded_tests\":"), 1)
		}},
		{"aliased_exclusion_member", func(p *InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte("\"excluded_tests\":"), []byte("\"Excluded_Tests\":"), 1)
		}},
		{"wrong_integrity_schema", func(p *InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte("run-integrity/v1"), []byte("run-integrity/v9"), 1)
		}},
		{"wrong_integrity_version", func(p *InventoryPayload) { p.TestResults[1].Version = 2 }},
		{"wrong_evidence_status", func(p *InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte("\"status\":\"pass\""), []byte("\"status\":\"fail\""), 1)
		}},
		{"wrong_evidence_plan", func(p *InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte(p.TestResults[1].PlanID), []byte("sha256:"+strings.Repeat("c", 64)), 1)
		}},
		{"wrong_evidence_graph", func(p *InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte(p.TestResults[1].HardwareGraph), []byte("hmac-sha256:"+strings.Repeat("c", 64)), 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := inspectionExcludedReportFixture(t)
			before, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			test.change(&p)
			after, err := json.Marshal(p)
			if err != nil || bytes.Equal(before, after) {
				t.Fatal("fault did not change valid prerequisite")
			}
			if validateCoverage(p) != nil {
				t.Fatal("fault did not reach inspection-specific validation")
			}
			rejectInspectionReport(t, p)
		})
	}
}

func TestInspectionIntegrityCannotHideSchemaFamilyObservations(t *testing.T) {
	for _, test := range []struct{ name, fragment string }{
		{"direct", "\"hidden\":\"hhsoft.pc-tester.exterior-inspection/v1\","},
		{"unknown_family", "\"hidden\":\"hhsoft.pc-tester.exterior-inspection/v99\","},
		{"case_family", "\"hidden\":\"HHSOFT.PC-TESTER.EXTERIOR-INSPECTION/V1\","},
		{"escaped_family", "\"hidden\":\"hhsoft.pc-tester.exterior\\u002dinspection/v1\","},
		{"duplicate_schema", "\"schema\":\"hhsoft.pc-tester.exterior-inspection/v1\","},
		{"aliased_schema", "\"SCHEMA\":\"hhsoft.pc-tester.exterior-inspection/v1\","},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, session := range []bool{false, true} {
				p := inspectionExcludedReportFixture(t)
				if session {
					p = inspectionSessionFixture(t)
				}
				for i := range p.TestResults {
					if p.TestResults[i].ID != "run.integrity.quick" {
						continue
					}
					raw := p.TestResults[i].Evidence
					p.TestResults[i].Evidence = append([]byte("{"+test.fragment), raw[1:]...)
					if !HasExteriorInspectionClaim(p.TestResults[i].Evidence) || !sessionevidence.HasInspectionSchemaClaim(p.TestResults[i].Evidence) {
						t.Fatal("whole-token family selector lost hidden evidence")
					}
				}
				rejectInspectionReport(t, p)
			}
		})
	}
	// A current session reference cannot manufacture its missing result row.
	p := inspectionSessionFixture(t)
	for i := range p.TestResults {
		if p.TestResults[i].ID == "inspection.exterior.quick" {
			p.TestResults = append(p.TestResults[:i], p.TestResults[i+1:]...)
			break
		}
	}
	if validateCoverage(p) != nil {
		t.Fatal("missing-row counterexample did not reach inspection validation")
	}
	rejectInspectionReport(t, p)
}
