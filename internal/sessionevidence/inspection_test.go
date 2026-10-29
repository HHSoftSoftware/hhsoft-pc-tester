package sessionevidence

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
)

const inspectionPass = "{\"schema\":\"hhsoft.pc-tester.exterior-inspection/v1\",\"source\":\"operator_reported\",\"scope\":\"whole_device\",\"criterion\":\"visible_exterior_enclosure_damage\",\"finding\":\"no_defect_observed\",\"response_complete\":true,\"stop_reason\":\"completed\",\"status\":\"pass\",\"observation_milliseconds\":20}"

func inspectionBindingFixture(t *testing.T, raw []byte, outcome, state string) (Journal, []ResultBinding) {
	t.Helper()
	j := shortJournal()
	j.Schema = SchemaV7
	j.Workers = append(j.Workers, WorkerRecord{ID: "inspection.exterior.quick", State: state, Outcome: outcome})
	rows := shortBindings(j)
	planned := "scheduled"
	if state == "not_scheduled" {
		planned = "excluded"
	}
	if state == "completed" || state == "interrupted" {
		j.Workers[11].Timing = &WorkerTiming{10, 30}
		j.Safety = append(j.Safety[:1:1], append([]SafetyRecord{{Phase: "before-inspection.exterior.quick", Result: j.Safety[0].Result}}, j.Safety[1:]...)...)
	}
	if outcome == "fail" || outcome == "error" {
		j.StopReason = "worker_stop"
	}
	if outcome == "aborted" {
		j.StopReason = "operator_cancelled"
	}
	rows[11] = ResultBinding{ID: "inspection.exterior.quick", Version: 1, PlannedStatus: planned, ReportStatus: reportStatus(outcome), Evidence: raw}
	return j, rows
}

func TestInspectionSessionKeepsLiteralHistoricalWorkerContracts(t *testing.T) {
	expected := map[string][]string{
		"hhsoft.pc-tester.quick-session/v4": {"cpu.quick", "memory.quick", "audio.readiness.quick", "input.readiness.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick", "graphics.quick", "display.operator.quick"},
		"hhsoft.pc-tester.quick-session/v5": {"cpu.quick", "memory.quick", "audio.readiness.quick", "input.readiness.quick", "input.keyboard.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick", "graphics.quick", "display.operator.quick"},
		"hhsoft.pc-tester.quick-session/v6": {"cpu.quick", "memory.quick", "audio.readiness.quick", "input.readiness.quick", "input.keyboard.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick", "storage.quick", "usb.readiness.quick", "graphics.quick", "display.operator.quick"},
		"hhsoft.pc-tester.quick-session/v7": {"cpu.quick", "memory.quick", "audio.readiness.quick", "input.readiness.quick", "input.keyboard.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick", "graphics.quick", "display.operator.quick", "inspection.exterior.quick"},
	}
	for schema, ids := range expected {
		t.Run(schema, func(t *testing.T) {
			if !reflect.DeepEqual(workerIDs(schema), ids) {
				t.Fatal("literal worker order changed")
			}
			j := shortJournal()
			j.Schema = schema
			j.Workers = nil
			for _, id := range ids {
				j.Workers = append(j.Workers, WorkerRecord{ID: id, State: "not_scheduled", Outcome: "excluded"})
			}
			raw, err := json.Marshal(j)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Decode(raw)
			if err != nil || !reflect.DeepEqual(got, j) {
				t.Fatal("schema round trip", err)
			}
			if bytes.Contains(raw, []byte("\"storage_source\"")) != (schema == "hhsoft.pc-tester.quick-session/v6") {
				t.Fatal("wire ownership changed")
			}
			if schema != "hhsoft.pc-tester.quick-session/v6" {
				injected := append(append([]byte{}, raw[:len(raw)-1]...), []byte(",\"storage_source\":null}")...)
				if _, err := Decode(injected); err == nil {
					t.Fatal("inactive storage source accepted")
				}
				j.StorageSource = &StorageSource{}
				if _, err := json.Marshal(j); err == nil {
					t.Fatal("inactive source silently discarded")
				}
			}
		})
	}
	active := WorkerIDs()
	if Schema != "hhsoft.pc-tester.quick-session/v7" || !slices.Equal(active[:], expected[Schema]) || slices.Contains(active[:], "storage.quick") {
		t.Fatal("active worker contract changed")
	}
}

func TestInspectionSessionBindsIndependentOutcomesAndNotes(t *testing.T) {
	for _, test := range []struct {
		name, finding, stop, status, state string
		complete                           bool
	}{
		{"pass", "no_defect_observed", "completed", "pass", "completed", true},
		{"defect", "defect_observed", "completed", "fail", "completed", true},
		{"declined", "not_inspected", "not_inspected", "inconclusive", "completed", true},
		{"ended", "not_inspected", "input_ended", "inconclusive", "completed", false},
		{"deadline", "not_inspected", "deadline", "aborted", "interrupted", false},
		{"late_defect", "defect_observed", "deadline", "fail", "completed", true},
		{"cancel_defect", "defect_observed", "operator_cancelled", "fail", "completed", true},
		{"hard_defect", "defect_observed", "input_error", "error", "interrupted", true},
		{"prompt", "not_inspected", "prompt_error", "error", "completed", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := exteriorinspection.Result{Schema: "hhsoft.pc-tester.exterior-inspection/v1", Source: "operator_reported", Scope: "whole_device", Criterion: "visible_exterior_enclosure_damage", Finding: test.finding, ResponseComplete: test.complete, StopReason: test.stop, Status: test.status, ObservationMilliseconds: 20}
			raw, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := exteriorinspection.Decode(raw); err != nil {
				t.Fatal("literal evidence prerequisite", err)
			}
			j, rows := inspectionBindingFixture(t, raw, test.status, test.state)
			if ValidateBindings(j, rows) != nil {
				t.Fatal("valid observation rejected")
			}
		})
	}
	for _, test := range []struct{ name, state, outcome, detail string }{
		{"excluded", "not_scheduled", "excluded", "not run; disposition recorded in the frozen plan"},
		{"not_started", "not_started", "aborted", "bounded evidence was not produced; outcome recorded by the run supervisor"},
		{"missing_error", "completed", "error", "bounded evidence was not produced; outcome recorded by the run supervisor"},
		{"missing_aborted", "interrupted", "aborted", "bounded evidence was not produced; outcome recorded by the run supervisor"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(executionNote{Schema: "hhsoft.pc-tester.execution-note/v1", Status: reportStatus(test.outcome), SourceStatus: test.outcome, Detail: test.detail})
			if err != nil {
				t.Fatal(err)
			}
			j, rows := inspectionBindingFixture(t, raw, test.outcome, test.state)
			if ValidateBindings(j, rows) != nil {
				t.Fatal("valid fixed note rejected")
			}
		})
	}
}

func TestInspectionSessionRejectsIndependentBindingContradictions(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Journal, []ResultBinding)
	}{
		{"duration", func(j *Journal, r []ResultBinding) {
			r[11].Evidence = bytes.Replace(r[11].Evidence, []byte(":20}"), []byte(":21}"), 1)
		}},
		{"status", func(j *Journal, r []ResultBinding) { r[11].ReportStatus = "inconclusive" }},
		{"outcome", func(j *Journal, r []ResultBinding) { j.Workers[11].Outcome = "inconclusive" }},
		{"id", func(j *Journal, r []ResultBinding) { r[11].ID = "inspection.other.quick" }},
		{"case_id", func(j *Journal, r []ResultBinding) { r[11].ID = "Inspection.Exterior.Quick" }},
		{"version", func(j *Journal, r []ResultBinding) { r[11].Version = 2 }},
		{"excluded", func(j *Journal, r []ResultBinding) { r[11].PlannedStatus = "excluded" }},
		{"unstarted", func(j *Journal, r []ResultBinding) { j.Workers[11].State = "not_started"; j.Workers[11].Timing = nil }},
		{"interrupted_pass", func(j *Journal, r []ResultBinding) {
			j.Workers[11].State = "interrupted"
			j.StopReason = "operator_cancelled"
		}},
		{"missing_worker", func(j *Journal, r []ResultBinding) { j.Workers = j.Workers[:11] }},
		{"duplicate_row", func(j *Journal, r []ResultBinding) { r[0] = r[11] }},
		{"extra_field", func(j *Journal, r []ResultBinding) {
			r[11].Evidence = bytes.Replace(r[11].Evidence, []byte("\"source\""), []byte("\"extra\":1,\"source\""), 1)
		}},
		{"scope", func(j *Journal, r []ResultBinding) {
			r[11].Evidence = bytes.Replace(r[11].Evidence, []byte("whole_device"), []byte("hardware"), 1)
		}},
		{"storage", func(j *Journal, r []ResultBinding) { j.StorageSource = &StorageSource{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			j, rows := inspectionBindingFixture(t, []byte(inspectionPass), "pass", "completed")
			if ValidateBindings(j, rows) != nil {
				t.Fatal("positive prerequisite")
			}
			test.change(&j, rows)
			if ValidateBindings(j, rows) == nil {
				t.Fatal("contradiction accepted")
			}
		})
	}
	for _, schema := range []string{SchemaV4, SchemaV5, SchemaV6} {
		t.Run(schema, func(t *testing.T) {
			j := shortJournal()
			if schema == SchemaV4 {
				j.Schema = schema
				j.Workers = append(j.Workers[:4:4], j.Workers[5:]...)
			}
			if schema == SchemaV6 {
				j = storageJournal()
			}
			rows := shortBindings(j)
			if schema == SchemaV6 {
				rows = storageBindings(j)
			}
			if ValidateBindings(j, rows) != nil {
				t.Fatal("historical prerequisite")
			}
			rows = append(rows, ResultBinding{ID: "inspection.exterior.quick", Version: 1, PlannedStatus: "scheduled", ReportStatus: "pass", Evidence: []byte(inspectionPass)})
			if ValidateBindings(j, rows) == nil {
				t.Fatal("historical schema carried inspection")
			}
		})
	}
}

func TestInspectionNotesAndClaimSelectionAreClosed(t *testing.T) {
	valid := ResultBinding{ID: "inspection.exterior.quick", Version: 1, PlannedStatus: "excluded", ReportStatus: "skip", Evidence: []byte("{\"schema\":\"hhsoft.pc-tester.execution-note/v1\",\"status\":\"skip\",\"source_status\":\"excluded\",\"detail\":\"not run; disposition recorded in the frozen plan\"}")}
	if ValidateInspectionNote(valid) != nil {
		t.Fatal("note prerequisite")
	}
	for _, test := range []struct {
		name   string
		change func(*ResultBinding)
	}{
		{"absent", func(r *ResultBinding) {
			r.PlannedStatus = "not_present"
			r.Evidence = bytes.Replace(r.Evidence, []byte("excluded"), []byte("not_present"), 1)
		}},
		{"unsupported", func(r *ResultBinding) {
			r.PlannedStatus = "unsupported"
			r.Evidence = bytes.Replace(r.Evidence, []byte("excluded"), []byte("unsupported"), 1)
		}},
		{"driver", func(r *ResultBinding) {
			r.PlannedStatus = "missing_driver"
			r.ReportStatus = "inconclusive"
			r.Evidence = bytes.ReplaceAll(r.Evidence, []byte("skip"), []byte("inconclusive"))
			r.Evidence = bytes.Replace(r.Evidence, []byte("excluded"), []byte("missing_driver"), 1)
		}},
		{"free_detail", func(r *ResultBinding) {
			r.Evidence = bytes.Replace(r.Evidence, []byte("not run; disposition recorded in the frozen plan"), []byte("arbitrary"), 1)
		}},
		{"hidden_schema", func(r *ResultBinding) {
			r.Evidence = bytes.Replace(r.Evidence, []byte("not run; disposition recorded in the frozen plan"), []byte("HHSOFT.PC-TESTER.EXTERIOR-INSPECTION/v99"), 1)
		}},
		{"extra", func(r *ResultBinding) {
			r.Evidence = bytes.Replace(r.Evidence, []byte("\"status\""), []byte("\"extra\":0,\"status\""), 1)
		}},
		{"missing_detail", func(r *ResultBinding) {
			r.Evidence = bytes.Replace(r.Evidence, []byte(",\"detail\":\"not run; disposition recorded in the frozen plan\""), nil, 1)
		}},
		{"version", func(r *ResultBinding) { r.Version = 2 }},
		{"scheduled_pass", func(r *ResultBinding) {
			r.PlannedStatus = "scheduled"
			r.ReportStatus = "pass"
			r.Evidence = []byte("{\"schema\":\"hhsoft.pc-tester.execution-note/v1\",\"status\":\"pass\",\"source_status\":\"pass\",\"detail\":\"bounded evidence was not produced; outcome recorded by the run supervisor\"}")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := valid
			r.Evidence = bytes.Clone(valid.Evidence)
			test.change(&r)
			if ValidateInspectionNote(r) == nil {
				t.Fatal("forged note accepted")
			}
		})
	}
	for _, raw := range []string{
		"{\"id\":\"inspection.exterior.quick\",\"id\":\"camera.quick\"}",
		"{\"ID\":\"INSPECTION.EXTERIOR.QUICK\"}",
		"{\"id\":\"inspection\\u002eexterior.quick\"}",
		"{\"schema\":\"hhsoft.pc-tester.exterior-inspection/v99\",\"schema\":\"opaque\"}",
		"{\"schema\":\"HHSOFT.PC-TESTER.EXTERIOR-INSPECTION/v1\"}",
		"{\"schema\":\"hhsoft.pc-tester.exterior-inspection\"}",
		"{\"anything\":[{\"id\":\"inspection.exterior.quick\"}]}",
		"{\"schema\":\"hhsoft.pc-tester.exterior-inspection/v1\"",
		"{\"unterminated\":",
	} {
		if !HasInspectionClaim([]byte(raw)) {
			t.Fatal("claim hidden before typed decode")
		}
	}
	for _, raw := range []string{"", "{}", "{\"status\":\"pass\"}", "{\"id\":\"display.operator.quick\"}"} {
		if HasInspectionClaim([]byte(raw)) {
			t.Fatal("unrelated legacy claim selected")
		}
	}
}
