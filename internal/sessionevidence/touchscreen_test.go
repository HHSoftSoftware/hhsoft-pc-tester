package sessionevidence_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

// Literal sequences are independent expected layouts, never production aliases.
const touchscreenQuickWorkers = "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
const touchscreenStandardWorkers = "cpu.quick cpu.stress memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"

func touchscreenSessionLiteral(t *testing.T, version string) []byte {
	t.Helper()
	var schema, order, tail string
	switch version {
	case "v11":
		schema, order = "hhsoft.pc-tester.quick-session/v11", touchscreenQuickWorkers
	case "v12":
		schema, order = "hhsoft.pc-tester.quick-session/v12", touchscreenStandardWorkers
		tail = `,"profile":"standard","cpu_stress_source":null`
	default:
		t.Fatal("unknown literal touchscreen version")
	}
	workers := []string{}
	for _, id := range strings.Fields(order) {
		workers = append(workers, `{"id":"`+id+`","state":"not_scheduled","outcome":"excluded"}`)
	}
	return []byte(`{"schema":"` + schema + touchpadSessionPrefix + strings.Join(workers, ",") + touchpadSessionTail + tail + "}")
}

func touchscreenSessionAllOff(t *testing.T, version string) (sessionevidence.Journal, []sessionevidence.ResultBinding, int) {
	t.Helper()
	j, err := sessionevidence.Decode(touchscreenSessionLiteral(t, version))
	if err != nil {
		t.Fatal("literal current journal prerequisite", err)
	}
	bindings := make([]sessionevidence.ResultBinding, 0, len(j.Workers))
	index := -1
	for i, worker := range j.Workers {
		version := 1
		if worker.ID == "input.keyboard.quick" || worker.ID == "input.touchpad.quick" || worker.ID == "input.touchscreen.quick" {
			version = 2
		}
		if worker.ID == "input.touchscreen.quick" {
			index = i
		}
		bindings = append(bindings, sessionevidence.ResultBinding{
			ID: worker.ID, Version: version, PlannedStatus: "excluded", ReportStatus: "skip",
			Evidence: touchpadNote("skip", "excluded", "not run; disposition recorded in the frozen plan"),
		})
	}
	if index < 0 || sessionevidence.ValidateBindings(j, bindings) != nil {
		t.Fatal("complete all-off prerequisite")
	}
	return j, bindings, index
}

func touchscreenSessionObserved(t *testing.T, version string) (sessionevidence.Journal, []sessionevidence.ResultBinding, int) {
	t.Helper()
	j, bindings, i := touchscreenSessionAllOff(t, version)
	j.Workers[i] = sessionevidence.WorkerRecord{ID: "input.touchscreen.quick", State: "completed", Outcome: "pass",
		Timing: &sessionevidence.WorkerTiming{StartedMilliseconds: 10, FinishedMilliseconds: 110}}
	j.Safety = append(j.Safety[:1], append([]sessionevidence.SafetyRecord{
		{Phase: "before-input.touchscreen.quick", Result: j.Safety[0].Result},
	}, j.Safety[1:]...)...)
	bindings[i] = sessionevidence.ResultBinding{ID: "input.touchscreen.quick", Version: 2,
		PlannedStatus: "scheduled", ReportStatus: "pass", Evidence: []byte(touchscreenSessionPass)}
	if err := sessionevidence.ValidateBindings(j, bindings); err != nil {
		t.Fatal("observed prerequisite", err)
	}
	return j, bindings, i
}

func TestTouchscreenSessionVersionedSelectorsAndHistoricalPredicates(t *testing.T) {
	quick, standard := sessionevidence.WorkerIDsV11(), sessionevidence.WorkerIDsV12()
	if !reflect.DeepEqual(quick[:], strings.Fields(touchscreenQuickWorkers)) ||
		!reflect.DeepEqual(standard[:], strings.Fields(touchscreenStandardWorkers)) {
		t.Fatal("version-frozen touchscreen order")
	}
	quick[0], standard[0] = "changed", "changed"
	if sessionevidence.WorkerIDsV11()[0] != "cpu.quick" || sessionevidence.WorkerIDsV12()[0] != "cpu.quick" {
		t.Fatal("selector storage alias")
	}
	for _, version := range []string{"v4", "v5", "v6", "v7", "v8", "v9", "v10", "v11", "v12", "v13", "v14", "v011", "V11"} {
		t.Run(version, func(t *testing.T) {
			schema := "hhsoft.pc-tester.quick-session/" + version
			strict := version == "v4" || version == "v5" || version == "v6" || version == "v7" || version == "v8" ||
				version == "v9" || version == "v10" || version == "v11" || version == "v12" || version == "v13"
			standard := version == "v8" || version == "v10" || version == "v12"
			inspection := version == "v7" || version == "v8" || version == "v9" || version == "v10" || version == "v11" || version == "v12" || version == "v13"
			touchpad := version == "v9" || version == "v10" || version == "v11" || version == "v12" || version == "v13"
			touchscreen := version == "v11" || version == "v12" || version == "v13"
			if sessionevidence.IsStrictSchema(schema) != strict || sessionevidence.IsStandardSchema(schema) != standard ||
				sessionevidence.SupportsInspection(schema) != inspection || sessionevidence.SupportsTouchpad(schema) != touchpad ||
				sessionevidence.SupportsTouchscreen(schema) != touchscreen {
				t.Fatal("version predicates drift")
			}
		})
	}
}

func TestTouchscreenSessionLiteralWireAndThermalScope(t *testing.T) {
	for _, version := range []string{"v11", "v12"} {
		t.Run(version, func(t *testing.T) {
			raw := touchscreenSessionLiteral(t, version)
			j, _, _ := touchscreenSessionAllOff(t, version)
			encoded, err := j.MarshalJSON()
			if err != nil || !bytes.Equal(encoded, raw) {
				t.Fatal("literal journal wire changed")
			}
			order, count := touchscreenQuickWorkers, 14
			if version == "v12" {
				order, count = touchscreenStandardWorkers, 15
			}
			got := make([]string, len(j.Workers))
			for i, worker := range j.Workers {
				got[i] = worker.ID
			}
			if len(got) != count || !reflect.DeepEqual(got, strings.Fields(order)) {
				t.Fatal("literal worker layout changed")
			}
			if bytes.Contains(raw, []byte(`"profile"`)) != (version == "v12") ||
				bytes.Contains(raw, []byte(`"cpu_stress_source"`)) != (version == "v12") ||
				bytes.Contains(raw, []byte(`"storage_source"`)) {
				t.Fatal("version-specific field drift")
			}
			view := sessionevidence.ThermalViewOf(j)
			scope := sessionevidence.Scope
			if version == "v12" {
				scope = sessionevidence.StandardScope
			}
			if view.Scope != scope || view.Coverage != "unavailable" || view.PeakObserved || !view.CleanupConfirmed {
				t.Fatal("thermal scope or observation inflation")
			}
		})
	}
}

func TestTouchscreenSessionHistoricalWireRemainsLiteral(t *testing.T) {
	for _, version := range []string{"v4", "v5", "v6", "v7", "v8", "v9", "v10"} {
		t.Run(version, func(t *testing.T) {
			raw := touchpadSessionLiteral(t, version)
			j, _ := touchpadAllOff(t, version)
			encoded, err := j.MarshalJSON()
			if err != nil || !bytes.Equal(encoded, raw) || sessionevidence.SupportsTouchscreen(j.Schema) {
				t.Fatal("historical journal was upgraded")
			}
			for _, worker := range j.Workers {
				if worker.ID == "input.touchscreen.quick" {
					t.Fatal("current worker leaked into history")
				}
			}
		})
	}
}

func TestTouchscreenSessionWireRejectsAliasesAndWrongLayouts(t *testing.T) {
	raw := touchscreenSessionLiteral(t, "v12")
	for _, row := range []struct{ name, old, replacement string }{
		{"future", "quick-session/v12", "quick-session/v14"},
		{"schema_alias", `"schema":`, `"Schema":`},
		{"duplicate_schema", `"schema":`, `"schema":"ignored","schema":`},
		{"unknown", `"plan_id":`, `"raw_coordinate":1,"plan_id":`},
		{"missing_profile", `,"profile":"standard"`, ""},
		{"wrong_profile", `"profile":"standard"`, `"profile":"quick"`},
		{"missing_stress_source", `,"cpu_stress_source":null`, ""},
		{"quick_standard_members", "quick-session/v12", "quick-session/v11"},
		{"old_schema_new_layout", "quick-session/v12", "quick-session/v10"},
		{"worker_case", "input.touchscreen.quick", "INPUT.TOUCHSCREEN.QUICK"},
		{"duplicate_worker", "input.touchscreen.quick", "input.touchpad.quick"},
		{"missing_worker", `,{"id":"input.touchscreen.quick","state":"not_scheduled","outcome":"excluded"}`, ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			changed := bytes.Replace(raw, []byte(row.old), []byte(row.replacement), 1)
			if bytes.Equal(raw, changed) {
				t.Fatal("ineffective mutation")
			}
			if _, err := sessionevidence.Decode(changed); err == nil {
				t.Fatal("wrong version or layout admitted")
			}
		})
	}
}

func TestTouchscreenSessionObservedBindingAndOwnedEvidence(t *testing.T) {
	for _, version := range []string{"v11", "v12"} {
		t.Run(version, func(t *testing.T) {
			j, bindings, i := touchscreenSessionObserved(t, version)
			before := append([]byte(nil), bindings[i].Evidence...)
			if err := sessionevidence.ValidateBindings(j, bindings); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, bindings[i].Evidence) {
				t.Fatal("validation mutated caller evidence")
			}
			encoded, err := j.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := sessionevidence.Decode(encoded)
			if err != nil || !reflect.DeepEqual(j, decoded) || sessionevidence.ValidateBindings(decoded, bindings) != nil {
				t.Fatal("observed round trip lost binding")
			}
		})
	}
}

func TestTouchscreenSessionBindingRejectsIndependentMutations(t *testing.T) {
	for _, name := range []string{"wrong_id", "case_id", "version_one", "version_zero", "version_future", "missing_binding", "duplicate_binding", "wrong_planned_status", "wrong_report_status", "missing_timing", "short_interval", "status_disagreement", "future_evidence", "escaped_duplicate", "unreleased", "cleanup", "generic_absolute"} {
		t.Run(name, func(t *testing.T) {
			j, bindings, i := touchscreenSessionObserved(t, "v11")
			switch name {
			case "wrong_id":
				bindings[i].ID = "input.readiness.quick"
			case "case_id":
				bindings[i].ID = "INPUT.TOUCHSCREEN.QUICK"
			case "version_one":
				bindings[i].Version = 1
			case "version_zero":
				bindings[i].Version = 0
			case "version_future":
				bindings[i].Version = 3
			case "missing_binding":
				bindings = append(bindings[:i], bindings[i+1:]...)
			case "duplicate_binding":
				bindings = append(bindings, bindings[i])
			case "wrong_planned_status":
				bindings[i].PlannedStatus = "excluded"
			case "wrong_report_status":
				bindings[i].ReportStatus = "skip"
			case "missing_timing":
				j.Workers[i].Timing = nil
			case "short_interval":
				j.Workers[i].Timing.FinishedMilliseconds = 109
			case "status_disagreement":
				j.Workers[i].Outcome = "inconclusive"
				bindings[i].ReportStatus = "inconclusive"
			case "future_evidence":
				bindings[i].Evidence = bytes.Replace(bindings[i].Evidence, []byte("touchscreen-functional/v1"), []byte("touchscreen-functional/v2"), 1)
			case "escaped_duplicate":
				bindings[i].Evidence = bytes.Replace(bindings[i].Evidence, []byte(`"schema":`), []byte(`"s\u0063hema":"ignored","schema":`), 1)
			case "unreleased", "cleanup":
				var result inputtest.TouchscreenResult
				if err := json.Unmarshal(bindings[i].Evidence, &result); err != nil {
					t.Fatal(err)
				}
				if name == "unreleased" {
					result.Devices[0].ContactReleases = 0
				} else {
					result.CleanupConfirmed = false
				}
				bindings[i].Evidence = touchpadEncode(t, result)
			case "generic_absolute":
				bindings[i].Evidence = []byte(`{"schema":"hhsoft.pc-tester.input-readiness/v1","status":"pass","absolute_events":1}`)
			}
			if name == "escaped_duplicate" {
				var header struct {
					Schema string `json:"schema"`
				}
				if bytes.Equal(bindings[i].Evidence, []byte(touchscreenSessionPass)) ||
					!bytes.Contains(bindings[i].Evidence, []byte(`s\u0063hema`)) ||
					json.Unmarshal(bindings[i].Evidence, &header) != nil || header.Schema != "hhsoft.pc-tester.touchscreen-functional/v1" {
					t.Fatal("escaped duplicate prerequisite")
				}
			}
			if sessionevidence.ValidateBindings(j, bindings) == nil {
				t.Fatal("unbound or invalid functional result admitted")
			}
		})
	}
}

func TestTouchscreenSessionHistoryRejectsCurrentClaims(t *testing.T) {
	for _, version := range []string{"v4", "v5", "v6", "v7", "v8", "v9", "v10"} {
		t.Run(version, func(t *testing.T) {
			j, bindings := touchpadAllOff(t, version)
			claim := sessionevidence.ResultBinding{ID: "input.touchscreen.quick", Version: 2,
				PlannedStatus: "scheduled", ReportStatus: "pass", Evidence: []byte(touchscreenSessionPass)}
			if sessionevidence.ValidateBindings(j, append(bindings, claim)) == nil {
				t.Fatal("functional claim admitted in old journal")
			}
			claim.PlannedStatus, claim.ReportStatus = "excluded", "skip"
			claim.Evidence = touchpadNote("skip", "excluded", "not run; disposition recorded in the frozen plan")
			if sessionevidence.ValidateBindings(j, append(bindings, claim)) == nil {
				t.Fatal("current definition admitted in old journal")
			}
			claim.Version = 1
			if sessionevidence.ValidateBindings(j, append(bindings, claim)) != nil {
				t.Fatal("legacy inactive definition was promoted or rejected")
			}
		})
	}
}

func TestTouchscreenSessionFiniteNotes(t *testing.T) {
	for _, status := range []string{"excluded", "not_present", "unsupported", "missing_driver", "error", "aborted"} {
		t.Run(status, func(t *testing.T) {
			planned, report, detail := status, "skip", "not run; disposition recorded in the frozen plan"
			if status == "missing_driver" {
				report = "inconclusive"
			}
			if status == "error" || status == "aborted" {
				planned, report, detail = "scheduled", status, "bounded evidence was not produced; outcome recorded by the run supervisor"
			}
			binding := sessionevidence.ResultBinding{ID: "input.touchscreen.quick", Version: 2,
				PlannedStatus: planned, ReportStatus: report, Evidence: touchpadNote(report, status, detail)}
			if err := sessionevidence.ValidateTouchscreenNote(binding); err != nil {
				t.Fatal("valid note prerequisite", err)
			}
			binding.Evidence = touchpadNote(report, status, detail+" extra")
			if !errors.Is(sessionevidence.ValidateTouchscreenNote(binding), sessionevidence.ErrInvalid) {
				t.Fatal("arbitrary note accepted")
			}
		})
	}
}

func TestTouchscreenSessionClaimSelectionUsesDecodedTokens(t *testing.T) {
	for _, row := range []struct {
		name, raw string
		want      bool
	}{
		{"exact", `{"schema":"hhsoft.pc-tester.touchscreen-functional/v1"}`, true},
		{"escaped", `{"schema":"hhsoft.pc-tester.touchs\u0063reen-functional/v1"}`, true},
		{"case", `{"schema":"HHSOFT.PC-TESTER.TOUCHSCREEN-FUNCTIONAL/V1"}`, true},
		{"future", `{"schema":"hhsoft.pc-tester.touchscreen-functional/v99"}`, true},
		{"earlier_duplicate", `{"schema":"hhsoft.pc-tester.touchscreen-functional/v1","schema":"other"}`, true},
		{"nested", `{"nested":["hhsoft.pc-tester.touchscreen-functional/v1"]}`, true},
		{"malformed", `{"schema":`, true},
		{"empty", "", false},
		{"note", `{"schema":"hhsoft.pc-tester.execution-note/v1"}`, false},
		{"readiness", `{"schema":"hhsoft.pc-tester.input-readiness/v1"}`, false},
		{"touchpad", `{"schema":"hhsoft.pc-tester.touchpad-functional/v1"}`, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			if row.name == "escaped" {
				var header struct {
					Schema string `json:"schema"`
				}
				if row.raw == `{"schema":"hhsoft.pc-tester.touchscreen-functional/v1"}` ||
					!strings.Contains(row.raw, `\u0063`) || json.Unmarshal([]byte(row.raw), &header) != nil ||
					header.Schema != "hhsoft.pc-tester.touchscreen-functional/v1" {
					t.Fatal("raw-different decoded-equal claim prerequisite")
				}
			}
			if sessionevidence.HasTouchscreenClaim([]byte(row.raw)) != row.want {
				t.Fatal("claim-selection boundary")
			}
		})
	}
}

const touchscreenSessionPass = `{"schema":"hhsoft.pc-tester.touchscreen-functional/v1","status":"pass","stop_reason":"completed","limit_ms":45000,"duration_ms":100,"cleanup_confirmed":true,"devices":[{"interface":"event2","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":1,"product":2,"version":1},"capabilities":{"protocol_version":65537,"events":[0,1,3],"keys":[330],"relative":[],"properties":[1],"misc":[],"axes":[{"code":0,"minimum":-100,"maximum":100,"fuzz":0,"flat":0,"resolution":0},{"code":1,"minimum":-100,"maximum":100,"fuzz":0,"flat":0,"resolution":0}]},"x_threshold":10,"y_threshold":10,"frames":6,"contact_starts":1,"contact_releases":1,"xy_baselines":1,"qualifying_contacts":1,"x_positive":2,"x_negative":2,"y_positive":2,"y_negative":2,"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]}`
