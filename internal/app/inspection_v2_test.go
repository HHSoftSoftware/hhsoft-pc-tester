package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

const conditionReportPass = "{\"schema\":\"hhsoft.pc-tester.exterior-inspection/v2\",\"source\":\"operator_reported\",\"scope\":\"visible_exterior_condition\",\"findings\":{\"enclosure_integrity\":\"no_issue_observed\",\"hinge_mount_condition\":\"not_applicable\",\"port_exterior_condition\":\"no_issue_observed\",\"surface_finish_condition\":\"no_issue_observed\"},\"responses_completed\":4,\"stop_reason\":\"completed\",\"status\":\"pass\",\"observation_milliseconds\":1}"

func conditionReportFixture(t *testing.T, profile string, current bool) InventoryPayload {
	t.Helper()
	p := inspectionSessionFixture(t)
	integrityIndex := len(p.TestResults) - 1
	if p.TestResults[integrityIndex].ID != "run.integrity.quick" {
		t.Fatal("historical final-row prerequisite")
	}
	outer, err := sessionevidence.DecodeIntegrity(p.TestResults[integrityIndex].Evidence)
	if err != nil || outer.Session == nil {
		t.Fatal("historical journal prerequisite", err)
	}
	j := outer.Session
	var order, oldSchema, newSchema string
	switch profile {
	case "quick":
		order = "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
		oldSchema, newSchema = "hhsoft.pc-tester.quick-session/v11", "hhsoft.pc-tester.quick-session/v14"
	case "standard":
		order = "cpu.quick cpu.stress memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
		oldSchema, newSchema, j.Profile = "hhsoft.pc-tester.quick-session/v12", "hhsoft.pc-tester.quick-session/v15", "standard"
	case "extended":
		order = "cpu.quick memory.quick memory.stress audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
		oldSchema, newSchema, j.Profile = "hhsoft.pc-tester.quick-session/v13", "hhsoft.pc-tester.quick-session/v16", "extended"
	default:
		t.Fatal("unknown profile fixture")
	}
	j.Schema = oldSchema
	if current {
		j.Schema = newSchema
	}
	oldWorkers := map[string]sessionevidence.WorkerRecord{}
	for _, worker := range j.Workers {
		oldWorkers[worker.ID] = worker
	}
	j.Workers = nil
	extra := []TestResult{}
	parent := p.TestResults[integrityIndex]
	for _, id := range strings.Fields(order) {
		if worker, found := oldWorkers[id]; found {
			j.Workers = append(j.Workers, worker)
			continue
		}
		version := 2
		if id == "cpu.stress" {
			version = 1
		}
		j.Workers = append(j.Workers, sessionevidence.WorkerRecord{ID: id, State: "not_scheduled", Outcome: "excluded"})
		added := TestResult{ID: id, Version: version, Status: TestSkip, PlannedStatus: PlannedExcluded,
			PlanID: parent.PlanID, HardwareGraph: parent.HardwareGraph,
			Evidence: []byte("{\"schema\":\"hhsoft.pc-tester.execution-note/v1\",\"status\":\"skip\",\"source_status\":\"excluded\",\"detail\":\"not run; disposition recorded in the frozen plan\"}")}
		replaced := false
		for i := range p.TestResults {
			if p.TestResults[i].ID == id {
				p.TestResults[i], replaced = added, true
			}
		}
		if !replaced {
			extra = append(extra, added)
		}
	}
	if current {
		found := false
		for i := range p.TestResults {
			if p.TestResults[i].ID == "inspection.exterior.quick" {
				p.TestResults[i].Version, p.TestResults[i].Evidence = 2, []byte(conditionReportPass)
				found = true
			}
		}
		if !found {
			t.Fatal("inspection row absent")
		}
	}
	parent.Evidence, err = json.Marshal(outer)
	if err != nil {
		t.Fatal(err)
	}
	p.TestResults = append(append(p.TestResults[:integrityIndex:integrityIndex], extra...), parent)
	if err := ValidateCoverage(p); err != nil {
		t.Fatal("independent complete report prerequisite", err)
	}
	return p
}

func conditionReportRow(t *testing.T, p *InventoryPayload, id string) *TestResult {
	t.Helper()
	for i := range p.TestResults {
		if p.TestResults[i].ID == id {
			return &p.TestResults[i]
		}
	}
	t.Fatal("required report row absent")
	return nil
}

func rejectConditionReport(t *testing.T, p InventoryPayload) {
	t.Helper()
	if ValidateCoverage(p) == nil {
		t.Fatal("coverage accepted invalid condition authority")
	}
	if _, err := ValidatedSessionThermal(p); err == nil {
		t.Fatal("thermal accessor bypassed condition binding")
	}
	if _, err := ValidatedExteriorCondition(p); err == nil {
		t.Fatal("condition accessor accepted invalid report")
	}
	if _, err := ValidatedExteriorInspection(p); err == nil {
		t.Fatal("historical accessor bypassed report validation")
	}
}

func TestExteriorConditionReportCurrentProfilesReturnOwnedCriteria(t *testing.T) {
	for _, profile := range []string{"quick", "standard", "extended"} {
		t.Run(profile, func(t *testing.T) {
			p := conditionReportFixture(t, profile, true)
			before, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ValidatedExteriorCondition(p)
			if err != nil || got == nil || got.Schema != "hhsoft.pc-tester.exterior-inspection/v2" ||
				got.Source != "operator_reported" || got.Scope != "visible_exterior_condition" || got.Status != "pass" ||
				got.ResponsesCompleted != 4 || got.Findings.EnclosureIntegrity != "no_issue_observed" ||
				got.Findings.HingeMountCondition != "not_applicable" || got.Findings.PortExteriorCondition != "no_issue_observed" ||
				got.Findings.SurfaceFinishCondition != "no_issue_observed" {
				t.Fatal("literal finite view changed", err)
			}
			got.Findings.HingeMountCondition = "changed"
			again, err := ValidatedExteriorCondition(p)
			after, marshalErr := json.Marshal(p)
			if err != nil || marshalErr != nil || again == nil || again.Findings.HingeMountCondition != "not_applicable" ||
				!bytes.Equal(before, after) {
				t.Fatal("copied accessor aliases signed source")
			}
			if legacy, err := ValidatedExteriorInspection(p); err != nil || legacy != nil {
				t.Fatal("v2 rendered as historical one-criterion evidence")
			}
		})
	}
}

func TestExteriorConditionReportRetainsLiteralHistoricalV1(t *testing.T) {
	for _, profile := range []string{"quick", "standard", "extended"} {
		t.Run(profile, func(t *testing.T) {
			p := conditionReportFixture(t, profile, false)
			old, err := ValidatedExteriorInspection(p)
			if err != nil || old == nil || old.Schema != "hhsoft.pc-tester.exterior-inspection/v1" ||
				old.Finding != "no_defect_observed" || old.Criterion != "visible_exterior_enclosure_damage" {
				t.Fatal("historical observed report changed", err)
			}
			if current, err := ValidatedExteriorCondition(p); err != nil || current != nil {
				t.Fatal("history acquired new criteria")
			}
			row := conditionReportRow(t, &p, "inspection.exterior.quick")
			row.Version, row.Evidence = 2, []byte(conditionReportPass)
			rejectConditionReport(t, p)
		})
	}
}

func TestExteriorConditionReportRejectsIndependentAuthorityMutations(t *testing.T) {
	for _, name := range []string{"legacy_definition", "future_definition", "legacy_journal", "legacy_payload", "wrong_id",
		"case_id", "status", "duration", "missing_criterion", "escaped_duplicate", "unknown_operator", "unobserved_pass",
		"foreign_not_applicable", "missing_row", "missing_journal", "duplicate_row", "unbound_plan"} {
		t.Run(name, func(t *testing.T) {
			p := conditionReportFixture(t, "quick", true)
			before, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			row := conditionReportRow(t, &p, "inspection.exterior.quick")
			switch name {
			case "legacy_definition":
				row.Version = 1
			case "future_definition":
				row.Version = 3
			case "legacy_journal":
				parent := conditionReportRow(t, &p, "run.integrity.quick")
				parent.Evidence = bytes.Replace(parent.Evidence, []byte("quick-session/v14"), []byte("quick-session/v11"), 1)
			case "legacy_payload":
				row.Evidence = []byte(reportInspectionPass)
			case "wrong_id":
				row.ID = "camera.quick"
			case "case_id":
				row.ID = "Inspection.Exterior.Quick"
			case "status":
				row.Status = TestInconclusive
			case "duration":
				row.Evidence = bytes.Replace(row.Evidence, []byte("\"observation_milliseconds\":1"), []byte("\"observation_milliseconds\":2"), 1)
			case "missing_criterion":
				row.Evidence = bytes.Replace(row.Evidence, []byte(",\"surface_finish_condition\":\"no_issue_observed\""), nil, 1)
				if !json.Valid(row.Evidence) || bytes.Contains(row.Evidence, []byte("surface_finish_condition")) {
					t.Fatal("member-removal prerequisite")
				}
			case "escaped_duplicate":
				row.Evidence = bytes.Replace(row.Evidence, []byte("\"enclosure_integrity\":"), []byte("\"\\u0065nclosure_integrity\":\"no_issue_observed\",\"enclosure_integrity\":"), 1)
				if !json.Valid(row.Evidence) || !bytes.Contains(row.Evidence, []byte("\\u0065")) {
					t.Fatal("escaped-key prerequisite")
				}
			case "unknown_operator":
				row.Evidence = bytes.Replace(row.Evidence, []byte("\"source\":"), []byte("\"operator_identity\":\"synthetic-private-canary\",\"source\":"), 1)
			case "unobserved_pass":
				row.Evidence = bytes.Replace(row.Evidence, []byte("\"hinge_mount_condition\":\"not_applicable\""), []byte("\"hinge_mount_condition\":\"unobserved\""), 1)
			case "foreign_not_applicable":
				row.Evidence = bytes.Replace(row.Evidence, []byte("\"enclosure_integrity\":\"no_issue_observed\""), []byte("\"enclosure_integrity\":\"not_applicable\""), 1)
			case "missing_row", "missing_journal":
				id := "inspection.exterior.quick"
				if name == "missing_journal" {
					id = "run.integrity.quick"
				}
				for i := range p.TestResults {
					if p.TestResults[i].ID == id {
						p.TestResults = append(p.TestResults[:i], p.TestResults[i+1:]...)
						break
					}
				}
			case "duplicate_row":
				p.TestResults = append(p.TestResults, *row)
			case "unbound_plan":
				row.PlanID = "sha256:" + strings.Repeat("e", 64)
			}
			after, err := json.Marshal(p)
			if err != nil || bytes.Equal(before, after) {
				t.Fatal("mutation did not change admitted prerequisite")
			}
			rejectConditionReport(t, p)
		})
	}
}

func TestExteriorConditionReportExcludedNoteCannotInventCriteria(t *testing.T) {
	p := conditionReportFixture(t, "quick", true)
	row := conditionReportRow(t, &p, "inspection.exterior.quick")
	row.PlannedStatus, row.Status = PlannedExcluded, TestSkip
	row.Evidence = []byte("{\"schema\":\"hhsoft.pc-tester.execution-note/v1\",\"status\":\"skip\",\"source_status\":\"excluded\",\"detail\":\"not run; disposition recorded in the frozen plan\"}")
	parent := conditionReportRow(t, &p, "run.integrity.quick")
	outer, err := sessionevidence.DecodeIntegrity(parent.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	for i := range outer.Session.Workers {
		if outer.Session.Workers[i].ID == "inspection.exterior.quick" {
			outer.Session.Workers[i] = sessionevidence.WorkerRecord{ID: "inspection.exterior.quick", State: "not_scheduled", Outcome: "excluded"}
		}
	}
	safety := []sessionevidence.SafetyRecord{}
	for _, observation := range outer.Session.Safety {
		if observation.Phase != "before-inspection.exterior.quick" {
			safety = append(safety, observation)
		}
	}
	outer.Session.Safety = safety
	parent.Evidence, err = json.Marshal(outer)
	if err != nil || ValidateCoverage(p) != nil {
		t.Fatal("truthful current exclusion prerequisite", err)
	}
	if got, err := ValidatedExteriorCondition(p); err != nil || got != nil {
		t.Fatal("exclusion acquired observations")
	}
	row.Evidence = bytes.Replace(row.Evidence, []byte("not run; disposition recorded in the frozen plan"), []byte("operator said all clear"), 1)
	rejectConditionReport(t, p)
	for _, old := range []InventoryPayload{{}, coverageFixture()} {
		if got, err := ValidatedExteriorCondition(old); err != nil || got != nil {
			t.Fatal("unrelated legacy report acquired observations")
		}
	}
}

func conditionNonexecutionReport(t *testing.T, status string, withSession bool) InventoryPayload {
	t.Helper()
	p := standaloneInspectionFixture()
	if withSession {
		p = conditionReportFixture(t, "quick", true)
		parent := conditionReportRow(t, &p, "run.integrity.quick")
		outer, err := sessionevidence.DecodeIntegrity(parent.Evidence)
		if err != nil {
			t.Fatal(err)
		}
		outer.Session.StopReason = "worker_stop"
		for i := range outer.Session.Workers {
			if outer.Session.Workers[i].ID == "inspection.exterior.quick" {
				outer.Session.Workers[i].Outcome = status
				if status == "aborted" {
					outer.Session.Workers[i].State = "interrupted"
					outer.Session.StopReason = "operator_cancelled"
				}
			}
		}
		parent.Evidence, err = json.Marshal(outer)
		if err != nil {
			t.Fatal(err)
		}
	}
	row := conditionReportRow(t, &p, "inspection.exterior.quick")
	row.Version, row.Status, row.PlannedStatus = 2, TestStatus(status), PlannedScheduled
	raw, err := json.Marshal(map[string]string{
		"schema": "hhsoft.pc-tester.execution-note/v1", "status": status, "source_status": status,
		"detail": "bounded evidence was not produced; outcome recorded by the run supervisor",
	})
	if err != nil {
		t.Fatal(err)
	}
	row.Evidence = raw
	if err := ValidateCoverage(p); err != nil {
		t.Fatal("honest fixed nonexecution prerequisite", err)
	}
	return p
}

func TestExteriorConditionNonexecutionNotesPreserveNoObservationAuthority(t *testing.T) {
	for _, row := range []struct {
		name, status string
		session      bool
	}{
		{"error_without_session", "error", false},
		{"aborted_without_session", "aborted", false},
		{"error_with_current_session", "error", true},
		{"aborted_with_current_session", "aborted", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			p := conditionNonexecutionReport(t, row.status, row.session)
			if got, err := ValidatedExteriorCondition(p); err != nil || got != nil {
				t.Fatal("note invented four-criterion observations")
			}
			if old, err := ValidatedExteriorInspection(p); err != nil || old != nil {
				t.Fatal("note invented historical observations")
			}
			thermal, err := ValidatedSessionThermal(p)
			if err != nil || (thermal != nil) != row.session {
				t.Fatal("note invented or removed session authority")
			}
		})
	}
}

func TestExteriorConditionNonexecutionNotesRejectIndependentForgeries(t *testing.T) {
	for _, name := range []string{"success", "fail", "partial", "unknown_detail", "missing_detail", "wrong_source_status",
		"legacy_definition_in_current_session", "current_definition_in_old_session", "worker_outcome",
		"evidence_plus_note", "two_notes", "typed_without_session", "malformed_present"} {
		t.Run(name, func(t *testing.T) {
			withSession := name == "legacy_definition_in_current_session" || name == "current_definition_in_old_session" ||
				name == "worker_outcome" || name == "evidence_plus_note"
			p := conditionNonexecutionReport(t, "error", withSession)
			row := conditionReportRow(t, &p, "inspection.exterior.quick")
			switch name {
			case "success", "fail", "partial":
				status := name
				if name == "success" {
					status = "pass"
				}
				if name == "partial" {
					status = "inconclusive"
				}
				row.Status = TestStatus(status)
				row.Evidence = bytes.ReplaceAll(row.Evidence, []byte("\"error\""), []byte("\""+status+"\""))
			case "unknown_detail":
				row.Evidence = bytes.Replace(row.Evidence, []byte("bounded evidence was not produced; outcome recorded by the run supervisor"), []byte("operator said the enclosure passed"), 1)
			case "missing_detail":
				var note map[string]string
				if json.Unmarshal(row.Evidence, &note) != nil {
					t.Fatal("literal note prerequisite")
				}
				delete(note, "detail")
				var err error
				row.Evidence, err = json.Marshal(note)
				if err != nil {
					t.Fatal(err)
				}
			case "wrong_source_status":
				row.Evidence = bytes.Replace(row.Evidence, []byte("\"source_status\":\"error\""), []byte("\"source_status\":\"aborted\""), 1)
			case "legacy_definition_in_current_session":
				row.Version = 1
			case "current_definition_in_old_session":
				parent := conditionReportRow(t, &p, "run.integrity.quick")
				parent.Evidence = bytes.Replace(parent.Evidence, []byte("quick-session/v14"), []byte("quick-session/v11"), 1)
			case "worker_outcome":
				row.Status = TestAborted
				row.Evidence = bytes.ReplaceAll(row.Evidence, []byte("\"error\""), []byte("\"aborted\""))
			case "evidence_plus_note":
				raw := append([]byte("{\"observation\":"+conditionReportPass+","), row.Evidence[1:]...)
				if !json.Valid(raw) || !HasExteriorInspectionClaim(raw) {
					t.Fatal("valid mixed evidence-note prerequisite")
				}
				row.Evidence = raw
			case "two_notes":
				row.Evidence = append(bytes.Clone(row.Evidence), row.Evidence...)
			case "typed_without_session":
				row.Status, row.Evidence = TestPass, []byte(conditionReportPass)
			case "malformed_present":
				row.Evidence = []byte("{")
			}
			rejectConditionReport(t, p)
		})
	}
}
