package sessionevidence_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

const conditionSessionPass = "{\"schema\":\"hhsoft.pc-tester.exterior-inspection/v2\",\"source\":\"operator_reported\",\"scope\":\"visible_exterior_condition\",\"findings\":{\"enclosure_integrity\":\"no_issue_observed\",\"hinge_mount_condition\":\"not_applicable\",\"port_exterior_condition\":\"no_issue_observed\",\"surface_finish_condition\":\"no_issue_observed\"},\"responses_completed\":4,\"stop_reason\":\"completed\",\"status\":\"pass\",\"observation_milliseconds\":20}"

func conditionSessionAllOff(t *testing.T, profile string) (sessionevidence.Journal, []sessionevidence.ResultBinding, int) {
	t.Helper()
	var j sessionevidence.Journal
	var rows []sessionevidence.ResultBinding
	switch profile {
	case "quick":
		j, rows, _ = touchscreenSessionAllOff(t, "v11")
		j.Schema = "hhsoft.pc-tester.quick-session/v14"
	case "standard":
		j, rows, _ = touchscreenSessionAllOff(t, "v12")
		j.Schema = "hhsoft.pc-tester.quick-session/v15"
	case "extended":
		j, rows = extendedMemoryAllOff(t)
		j.Schema = "hhsoft.pc-tester.quick-session/v16"
	default:
		t.Fatal("unknown fixture profile")
	}
	index := -1
	for i := range rows {
		if rows[i].ID == "inspection.exterior.quick" {
			if rows[i].Version != 1 {
				t.Fatal("historical definition prerequisite")
			}
			rows[i].Version, index = 2, i
		}
	}
	if index < 0 || sessionevidence.ValidateBindings(j, rows) != nil {
		t.Fatal("current all-off prerequisite")
	}
	return j, rows, index
}

func conditionSessionObserved(t *testing.T, profile string) (sessionevidence.Journal, []sessionevidence.ResultBinding, int) {
	t.Helper()
	j, rows, i := conditionSessionAllOff(t, profile)
	j.Workers[i] = sessionevidence.WorkerRecord{ID: "inspection.exterior.quick", State: "completed", Outcome: "pass",
		Timing: &sessionevidence.WorkerTiming{StartedMilliseconds: 10, FinishedMilliseconds: 30}}
	j.Safety = append(j.Safety[:1:1], append([]sessionevidence.SafetyRecord{
		{Phase: "before-inspection.exterior.quick", Result: j.Safety[0].Result},
	}, j.Safety[1:]...)...)
	rows[i] = sessionevidence.ResultBinding{ID: "inspection.exterior.quick", Version: 2,
		PlannedStatus: "scheduled", ReportStatus: "pass", Evidence: []byte(conditionSessionPass)}
	if err := sessionevidence.ValidateBindings(j, rows); err != nil {
		t.Fatal("literal four-criterion binding prerequisite", err)
	}
	return j, rows, i
}

func TestExteriorConditionJournalCurrentVersionsAndOwnedLayouts(t *testing.T) {
	for _, test := range []struct {
		profile, schema, order string
		count                  int
	}{
		{"quick", "hhsoft.pc-tester.quick-session/v14", touchscreenQuickWorkers, 14},
		{"standard", "hhsoft.pc-tester.quick-session/v15", touchscreenStandardWorkers, 15},
		{"extended", "hhsoft.pc-tester.quick-session/v16", extendedMemoryOrder, 15},
	} {
		t.Run(test.profile, func(t *testing.T) {
			j, rows, _ := conditionSessionObserved(t, test.profile)
			var ids []string
			for _, worker := range j.Workers {
				ids = append(ids, worker.ID)
			}
			if j.Schema != test.schema || len(ids) != test.count || !reflect.DeepEqual(ids, strings.Fields(test.order)) ||
				!sessionevidence.SupportsInspectionV2(j.Schema) ||
				sessionevidence.IsStandardSchema(j.Schema) != (test.profile == "standard") ||
				sessionevidence.IsExtendedSchema(j.Schema) != (test.profile == "extended") {
				t.Fatal("current authority or worker order changed")
			}
			raw, err := json.Marshal(j)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := sessionevidence.Decode(raw)
			if err != nil || !reflect.DeepEqual(j, decoded) || sessionevidence.ValidateBindings(decoded, rows) != nil {
				t.Fatal("current versioned wire lost binding", err)
			}
			if bytes.Contains(raw, []byte("\"cpu_stress_source\"")) != (test.profile == "standard") ||
				bytes.Contains(raw, []byte("\"memory_stress_source\"")) != (test.profile == "extended") {
				t.Fatal("profile-specific source members changed")
			}
		})
	}
	q, s, e := sessionevidence.WorkerIDsV14(), sessionevidence.WorkerIDsV15(), sessionevidence.WorkerIDsV16()
	if q != sessionevidence.WorkerIDs() || s != sessionevidence.StandardWorkerIDs() || e != sessionevidence.ExtendedWorkerIDs() ||
		sessionevidence.Schema != "hhsoft.pc-tester.quick-session/v14" {
		t.Fatal("current aliases")
	}
	q[0], s[0], e[0] = "changed", "changed", "changed"
	if sessionevidence.WorkerIDsV14()[0] != "cpu.quick" || sessionevidence.WorkerIDsV15()[0] != "cpu.quick" ||
		sessionevidence.WorkerIDsV16()[0] != "cpu.quick" {
		t.Fatal("worker storage alias")
	}
	for _, schema := range []string{"hhsoft.pc-tester.quick-session/v11", "hhsoft.pc-tester.quick-session/v12",
		"hhsoft.pc-tester.quick-session/v13", "hhsoft.pc-tester.quick-session/v17", "hhsoft.pc-tester.quick-session/v014",
		"HHSOFT.PC-TESTER.QUICK-SESSION/V14"} {
		if sessionevidence.SupportsInspectionV2(schema) {
			t.Fatal("undeclared v2 authority")
		}
	}
}

func TestExteriorConditionJournalPreservesV11V12V13Evidence(t *testing.T) {
	for _, profile := range []string{"quick", "standard", "extended"} {
		t.Run(profile, func(t *testing.T) {
			var j sessionevidence.Journal
			var rows []sessionevidence.ResultBinding
			if profile == "quick" {
				j, rows, _ = touchscreenSessionAllOff(t, "v11")
			}
			if profile == "standard" {
				j, rows, _ = touchscreenSessionAllOff(t, "v12")
			}
			if profile == "extended" {
				j, rows = extendedMemoryAllOff(t)
			}
			before, err := j.MarshalJSON()
			if err != nil || sessionevidence.ValidateBindings(j, rows) != nil {
				t.Fatal("historical positive prerequisite")
			}
			i := len(rows) - 1
			if rows[i].ID != "inspection.exterior.quick" || rows[i].Version != 1 {
				t.Fatal("literal final worker")
			}
			j.Workers[i] = sessionevidence.WorkerRecord{ID: rows[i].ID, State: "completed", Outcome: "pass",
				Timing: &sessionevidence.WorkerTiming{StartedMilliseconds: 10, FinishedMilliseconds: 30}}
			j.Safety = append(j.Safety[:1:1], append([]sessionevidence.SafetyRecord{
				{Phase: "before-inspection.exterior.quick", Result: j.Safety[0].Result}}, j.Safety[1:]...)...)
			rows[i].PlannedStatus, rows[i].ReportStatus = "scheduled", "pass"
			rows[i].Evidence = []byte("{\"schema\":\"hhsoft.pc-tester.exterior-inspection/v1\",\"source\":\"operator_reported\",\"scope\":\"whole_device\",\"criterion\":\"visible_exterior_enclosure_damage\",\"finding\":\"no_defect_observed\",\"response_complete\":true,\"stop_reason\":\"completed\",\"status\":\"pass\",\"observation_milliseconds\":20}")
			if sessionevidence.ValidateBindings(j, rows) != nil {
				t.Fatal("historical observed v1 rejected")
			}
			rows[i].Version, rows[i].Evidence = 2, []byte(conditionSessionPass)
			if sessionevidence.ValidateBindings(j, rows) == nil {
				t.Fatal("v2 acquired old journal authority")
			}
			old, err := sessionevidence.Decode(before)
			if err != nil {
				t.Fatal(err)
			}
			after, err := old.MarshalJSON()
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("historical wire was rewritten")
			}
		})
	}
}

func TestExteriorConditionJournalBindsFiniteOutcomes(t *testing.T) {
	for _, test := range []struct {
		name, hinge, surface, stop, status, state string
		completed                                 int
	}{
		{"all_observed", "no_issue_observed", "no_issue_observed", "completed", "pass", "completed", 4},
		{"explicit_no_hinge", "not_applicable", "no_issue_observed", "completed", "pass", "completed", 4},
		{"hidden_hinge", "unobserved", "no_issue_observed", "completed", "inconclusive", "completed", 4},
		{"visible_issue", "not_applicable", "issue_present", "completed", "fail", "completed", 4},
		{"late_issue", "not_applicable", "issue_present", "deadline", "fail", "completed", 4},
		{"cancel_between_lines", "not_applicable", "unobserved", "operator_cancelled", "aborted", "interrupted", 2},
		{"prompt_failed", "unobserved", "unobserved", "prompt_error", "error", "completed", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			j, rows, i := conditionSessionObserved(t, "quick")
			value, err := exteriorinspection.DecodeV2([]byte(conditionSessionPass))
			if err != nil {
				t.Fatal(err)
			}
			value.Findings.HingeMountCondition, value.Findings.SurfaceFinishCondition = test.hinge, test.surface
			value.ResponsesCompleted, value.StopReason, value.Status = test.completed, test.stop, test.status
			if test.completed < 4 {
				value.Findings.PortExteriorCondition = "unobserved"
			}
			if test.completed == 0 {
				value.Findings.EnclosureIntegrity = "unobserved"
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := exteriorinspection.DecodeV2(raw); err != nil {
				t.Fatal("independent finite outcome prerequisite", err)
			}
			j.Workers[i].State, j.Workers[i].Outcome = test.state, test.status
			if test.status != "pass" {
				j.StopReason = "worker_stop"
			}
			if test.status == "aborted" {
				j.StopReason = "operator_cancelled"
			}
			rows[i].ReportStatus, rows[i].Evidence = test.status, raw
			if sessionevidence.ValidateBindings(j, rows) != nil {
				t.Fatal("truthful finite observation rejected")
			}
		})
	}
}

func TestExteriorConditionJournalRejectsIndependentContradictions(t *testing.T) {
	for _, name := range []string{"legacy_definition", "future_definition", "legacy_journal", "wrong_id", "case_id",
		"missing_row", "duplicate_row", "wrong_status", "wrong_outcome", "short_interval", "unstarted",
		"excluded", "stripped_criterion", "escaped_duplicate", "foreign_not_applicable", "unobserved_pass", "cleanup"} {
		t.Run(name, func(t *testing.T) {
			j, rows, i := conditionSessionObserved(t, "quick")
			switch name {
			case "legacy_definition":
				rows[i].Version = 1
			case "future_definition":
				rows[i].Version = 3
			case "legacy_journal":
				j.Schema = "hhsoft.pc-tester.quick-session/v11"
			case "wrong_id":
				rows[i].ID = "display.operator.quick"
			case "case_id":
				rows[i].ID = "Inspection.Exterior.Quick"
			case "missing_row":
				rows = append(rows[:i], rows[i+1:]...)
			case "duplicate_row":
				rows = append(rows, rows[i])
			case "wrong_status":
				rows[i].ReportStatus = "inconclusive"
			case "wrong_outcome":
				j.Workers[i].Outcome = "inconclusive"
			case "short_interval":
				j.Workers[i].Timing.FinishedMilliseconds = 29
			case "unstarted":
				j.Workers[i].State, j.Workers[i].Timing = "not_started", nil
			case "excluded":
				rows[i].PlannedStatus = "excluded"
			case "stripped_criterion":
				rows[i].Evidence = bytes.Replace(rows[i].Evidence, []byte(",\"surface_finish_condition\":\"no_issue_observed\""), nil, 1)
				if !json.Valid(rows[i].Evidence) || bytes.Equal(rows[i].Evidence, []byte(conditionSessionPass)) ||
					bytes.Contains(rows[i].Evidence, []byte("surface_finish_condition")) {
					t.Fatal("member-removal prerequisite")
				}
			case "escaped_duplicate":
				rows[i].Evidence = bytes.Replace(rows[i].Evidence, []byte("\"hinge_mount_condition\":"), []byte("\"\\u0068inge_mount_condition\":\"not_applicable\",\"hinge_mount_condition\":"), 1)
				if !bytes.Contains(rows[i].Evidence, []byte("\\u0068")) || !json.Valid(rows[i].Evidence) {
					t.Fatal("escaped mutation prerequisite")
				}
			case "foreign_not_applicable":
				rows[i].Evidence = bytes.Replace(rows[i].Evidence, []byte("\"port_exterior_condition\":\"no_issue_observed\""), []byte("\"port_exterior_condition\":\"not_applicable\""), 1)
			case "unobserved_pass":
				rows[i].Evidence = bytes.Replace(rows[i].Evidence, []byte("\"surface_finish_condition\":\"no_issue_observed\""), []byte("\"surface_finish_condition\":\"unobserved\""), 1)
			case "cleanup":
				j.Monitor.CleanupConfirmed = false
			}
			if sessionevidence.ValidateBindings(j, rows) == nil {
				t.Fatal("unbound or stripped criteria admitted")
			}
		})
	}
}

func TestExteriorConditionJournalNotesStayUnobserved(t *testing.T) {
	for _, profile := range []string{"quick", "standard", "extended"} {
		t.Run(profile, func(t *testing.T) {
			j, rows, i := conditionSessionAllOff(t, profile)
			if sessionevidence.ValidateInspectionConditionNote(rows[i]) != nil || sessionevidence.ValidateBindings(j, rows) != nil {
				t.Fatal("explicit exclusion prerequisite")
			}
			rows[i].Version = 1
			if sessionevidence.ValidateBindings(j, rows) == nil {
				t.Fatal("legacy note acquired current authority")
			}
		})
	}
	for _, name := range []string{"not_present", "unsupported", "missing_driver", "free_detail", "scheduled_pass", "hidden_observation"} {
		t.Run(name, func(t *testing.T) {
			_, rows, i := conditionSessionAllOff(t, "quick")
			row := rows[i]
			switch name {
			case "not_present", "unsupported", "missing_driver":
				row.PlannedStatus = name
				row.Evidence = bytes.Replace(row.Evidence, []byte("excluded"), []byte(name), 1)
			case "free_detail":
				row.Evidence = bytes.Replace(row.Evidence, []byte("not run; disposition recorded in the frozen plan"), []byte("arbitrary observation"), 1)
			case "scheduled_pass":
				row.PlannedStatus, row.ReportStatus = "scheduled", "pass"
				row.Evidence = touchpadNote("pass", "pass", "bounded evidence was not produced; outcome recorded by the run supervisor")
			case "hidden_observation":
				row.Evidence = bytes.Replace(row.Evidence, []byte("not run; disposition recorded in the frozen plan"), []byte("hhsoft.pc-tester.exterior-inspection/v2"), 1)
			}
			if sessionevidence.ValidateInspectionConditionNote(row) == nil {
				t.Fatal("note invented visual authority")
			}
		})
	}
}
