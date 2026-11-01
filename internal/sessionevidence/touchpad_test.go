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
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchpadevidence"
)

// These sequences are literal history requirements, not the production selectors.
var touchpadHistory = []struct {
	name, schema, workers, tail string
}{
	{"v4", "hhsoft.pc-tester.quick-session/v4", "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.pointer.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick", ""},
	{"v5", "hhsoft.pc-tester.quick-session/v5", "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick", ""},
	{"v6", "hhsoft.pc-tester.quick-session/v6", "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick network.readiness.quick storage.readiness.quick storage.quick usb.readiness.quick graphics.quick display.operator.quick", `,"storage_source":null`},
	{"v7", "hhsoft.pc-tester.quick-session/v7", "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick", ""},
	{"v8", "hhsoft.pc-tester.quick-session/v8", "cpu.quick cpu.stress memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick", `,"profile":"standard","cpu_stress_source":null`},
	{"v9", "hhsoft.pc-tester.quick-session/v9", "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick", ""},
	{"v10", "hhsoft.pc-tester.quick-session/v10", "cpu.quick cpu.stress memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick", `,"profile":"standard","cpu_stress_source":null`},
}

// The monitoring tail is a fixed valid all-off interval. No worker result is
// obtained from a live device, current catalog, or the production worker list.
func touchpadSessionLiteral(t *testing.T, version string) []byte {
	t.Helper()
	for _, fixture := range touchpadHistory {
		if fixture.name != version {
			continue
		}
		workers := []string{}
		for _, id := range strings.Fields(fixture.workers) {
			workers = append(workers, `{"id":"`+id+`","state":"not_scheduled","outcome":"excluded"}`)
		}
		return []byte(`{"schema":"` + fixture.schema + touchpadSessionPrefix +
			strings.Join(workers, ",") + touchpadSessionTail + fixture.tail + "}")
	}
	t.Fatal("unknown literal version")
	return nil
}

func touchpadNote(status, source, detail string) []byte {
	// Struct encoding is only a fixture convenience; strict mutations below use
	// literal bytes, including duplicates which this helper cannot generate.
	raw, _ := json.Marshal(struct {
		Schema       string `json:"schema"`
		Status       string `json:"status"`
		SourceStatus string `json:"source_status"`
		Detail       string `json:"detail"`
	}{"hhsoft.pc-tester.execution-note/v1", status, source, detail})
	return raw
}

func touchpadAllOff(t *testing.T, version string) (sessionevidence.Journal, []sessionevidence.ResultBinding) {
	t.Helper()
	j, err := sessionevidence.Decode(touchpadSessionLiteral(t, version))
	if err != nil {
		t.Fatal("literal journal prerequisite", err)
	}
	bindings := make([]sessionevidence.ResultBinding, 0, len(j.Workers))
	for _, worker := range j.Workers {
		version := 1
		if worker.ID == "input.keyboard.quick" || worker.ID == "input.touchpad.quick" || worker.ID == "storage.quick" {
			version = 2
		}
		bindings = append(bindings, sessionevidence.ResultBinding{
			ID: worker.ID, Version: version, PlannedStatus: "excluded", ReportStatus: "skip",
			Evidence: touchpadNote("skip", "excluded", "not run; disposition recorded in the frozen plan"),
		})
	}
	if err := sessionevidence.ValidateBindings(j, bindings); err != nil {
		t.Fatal("all-off binding prerequisite", err)
	}
	return j, bindings
}

func touchpadBindingIndex(t *testing.T, j sessionevidence.Journal) int {
	t.Helper()
	for i, worker := range j.Workers {
		if worker.ID == "input.touchpad.quick" {
			return i
		}
	}
	t.Fatal("touchpad worker missing")
	return -1
}

func touchpadObserved(t *testing.T, version string) (sessionevidence.Journal, []sessionevidence.ResultBinding, int) {
	t.Helper()
	j, bindings := touchpadAllOff(t, version)
	i := touchpadBindingIndex(t, j)
	j.Workers[i] = sessionevidence.WorkerRecord{ID: "input.touchpad.quick", State: "completed", Outcome: "pass",
		Timing: &sessionevidence.WorkerTiming{StartedMilliseconds: 10, FinishedMilliseconds: 110}}
	j.Safety = append(j.Safety[:1], append([]sessionevidence.SafetyRecord{
		{Phase: "before-input.touchpad.quick", Result: j.Safety[0].Result},
	}, j.Safety[1:]...)...)
	bindings[i] = sessionevidence.ResultBinding{ID: "input.touchpad.quick", Version: 2,
		PlannedStatus: "scheduled", ReportStatus: "pass", Evidence: []byte(touchpadSessionPass)}
	if err := sessionevidence.ValidateBindings(j, bindings); err != nil {
		t.Fatal("literal touchpad prerequisite", err)
	}
	return j, bindings, i
}

func touchpadEvidenceValue(t *testing.T, raw []byte) inputtest.TouchpadResult {
	t.Helper()
	var value inputtest.TouchpadResult
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func touchpadEncode(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTouchpadSessionLiteralHistoryAndWire(t *testing.T) {
	for _, fixture := range touchpadHistory {
		t.Run(fixture.name, func(t *testing.T) {
			raw := touchpadSessionLiteral(t, fixture.name)
			j, _ := touchpadAllOff(t, fixture.name)
			got := make([]string, len(j.Workers))
			for i, worker := range j.Workers {
				got[i] = worker.ID
			}
			if !reflect.DeepEqual(got, strings.Fields(fixture.workers)) {
				t.Fatal("historical worker order changed")
			}
			encoded := touchpadEncode(t, j)
			if !bytes.Equal(raw, encoded) {
				t.Fatal("literal historical wire changed")
			}
			standard := fixture.name == "v8" || fixture.name == "v10"
			if bytes.Contains(encoded, []byte(`"profile"`)) != standard ||
				bytes.Contains(encoded, []byte(`"cpu_stress_source"`)) != standard ||
				bytes.Contains(encoded, []byte(`"storage_source"`)) != (fixture.name == "v6") {
				t.Fatal("versioned member leaked")
			}
			view := sessionevidence.ThermalViewOf(j)
			wantScope := sessionevidence.Scope
			if standard {
				wantScope = sessionevidence.StandardScope
			}
			if view.Scope != wantScope || view.Finding != "no_limit_observed" ||
				view.Coverage != "unavailable" || view.PeakObserved || !view.CleanupConfirmed {
				t.Fatal("thermal scope/coverage drift")
			}
		})
	}
}

func TestTouchpadSessionCurrentAndHistoricalSelectorsAndOwnedArrays(t *testing.T) {
	if sessionevidence.Schema != "hhsoft.pc-tester.quick-session/v11" {
		t.Fatal("current quick schema changed")
	}
	quick, standard := sessionevidence.WorkerIDs(), sessionevidence.StandardWorkerIDs()
	if !reflect.DeepEqual(quick[:], strings.Fields("cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick")) ||
		!reflect.DeepEqual(standard[:], strings.Fields("cpu.quick cpu.stress memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick")) {
		t.Fatal("active selectors changed")
	}
	v7, v8 := sessionevidence.WorkerIDsV7(), sessionevidence.WorkerIDsV8()
	if !reflect.DeepEqual(v7[:], strings.Fields(touchpadHistory[3].workers)) ||
		!reflect.DeepEqual(v8[:], strings.Fields(touchpadHistory[4].workers)) {
		t.Fatal("frozen historical selector order changed")
	}
	v9, v10 := sessionevidence.WorkerIDsV9(), sessionevidence.WorkerIDsV10()
	if !reflect.DeepEqual(v9[:], strings.Fields(touchpadHistory[5].workers)) ||
		!reflect.DeepEqual(v10[:], strings.Fields(touchpadHistory[6].workers)) {
		t.Fatal("frozen historical versioned selector order")
	}
	quick[0], standard[0], v7[0], v8[0], v9[0], v10[0] = "changed", "changed", "changed", "changed", "changed", "changed"
	if sessionevidence.WorkerIDs()[0] != "cpu.quick" || sessionevidence.StandardWorkerIDs()[0] != "cpu.quick" ||
		sessionevidence.WorkerIDsV7()[0] != "cpu.quick" || sessionevidence.WorkerIDsV8()[0] != "cpu.quick" ||
		sessionevidence.WorkerIDsV9()[0] != "cpu.quick" || sessionevidence.WorkerIDsV10()[0] != "cpu.quick" {
		t.Fatal("selector storage alias")
	}
	for _, c := range []struct {
		name, schema                           string
		strict, standard, inspection, touchpad bool
	}{
		{"v4", "hhsoft.pc-tester.quick-session/v4", true, false, false, false},
		{"v5", "hhsoft.pc-tester.quick-session/v5", true, false, false, false},
		{"v6", "hhsoft.pc-tester.quick-session/v6", true, false, false, false},
		{"v7", "hhsoft.pc-tester.quick-session/v7", true, false, true, false},
		{"v8", "hhsoft.pc-tester.quick-session/v8", true, true, true, false},
		{"v9", "hhsoft.pc-tester.quick-session/v9", true, false, true, true},
		{"v10", "hhsoft.pc-tester.quick-session/v10", true, true, true, true},
		{"future", "hhsoft.pc-tester.quick-session/v14", false, false, false, false},
		{"case_alias", "HHSOFT.PC-TESTER.QUICK-SESSION/V10", false, false, false, false},
		{"numeric_alias", "hhsoft.pc-tester.quick-session/v010", false, false, false, false},
		{"empty", "", false, false, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if sessionevidence.IsStrictSchema(c.schema) != c.strict || sessionevidence.IsStandardSchema(c.schema) != c.standard ||
				sessionevidence.SupportsInspection(c.schema) != c.inspection || sessionevidence.SupportsTouchpad(c.schema) != c.touchpad {
				t.Fatal("open version predicate")
			}
		})
	}
}

func TestTouchpadSessionWireRejectsAmbiguity(t *testing.T) {
	raw := touchpadSessionLiteral(t, "v10")
	for _, c := range []struct{ name, old, replacement string }{
		{"duplicate_schema", `"schema":"hhsoft.pc-tester.quick-session/v10"`, `"schema":"hhsoft.pc-tester.quick-session/v8","schema":"hhsoft.pc-tester.quick-session/v10"`},
		{"aliased_schema_key", `"schema":`, `"Schema":`},
		{"future_schema", "quick-session/v10", "quick-session/v14"},
		{"unknown_member", `"stop_reason":`, `"unknown":false,"stop_reason":`},
		{"duplicate_worker_id", `"id":"input.touchpad.quick"`, `"id":"input.pointer.quick","id":"input.touchpad.quick"`},
		{"null_workers", `"workers":[`, `"workers":null,"unused":[`},
		{"null_safety", `"safety":[`, `"safety":null,"unused":[`},
		{"missing_profile", `,"profile":"standard"`, ""},
		{"null_profile", `"profile":"standard"`, `"profile":null`},
		{"wrong_profile", `"profile":"standard"`, `"profile":"quick"`},
		{"missing_stress_source", `,"cpu_stress_source":null`, ""},
		{"array_stress_source", `"cpu_stress_source":null`, `"cpu_stress_source":[]`},
		{"v9_with_standard_members", "quick-session/v10", "quick-session/v9"},
		{"storage_in_v10", `"profile":"standard"`, `"storage_source":null,"profile":"standard"`},
		{"duplicate_profile", `"profile":"standard"`, `"profile":"standard","profile":"standard"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			changed := bytes.Replace(raw, []byte(c.old), []byte(c.replacement), 1)
			if bytes.Equal(raw, changed) {
				t.Fatal("ineffective literal mutation")
			}
			if _, err := sessionevidence.Decode(changed); err == nil {
				t.Fatal("ambiguous wire accepted")
			}
		})
	}
	t.Run("trailing_document", func(t *testing.T) {
		if _, err := sessionevidence.Decode(append(append([]byte{}, raw...), []byte("{}")...)); err == nil {
			t.Fatal("trailing document")
		}
	})
	t.Run("invalid_utf8", func(t *testing.T) {
		changed := bytes.Replace(raw, []byte("completed"), []byte{0xff}, 1)
		if _, err := sessionevidence.Decode(changed); err == nil {
			t.Fatal("invalid UTF-8")
		}
	})
	t.Run("exact_cap", func(t *testing.T) {
		bounded := append(append([]byte{}, raw...), bytes.Repeat([]byte(" "), (64<<10)-len(raw))...)
		if _, err := sessionevidence.Decode(bounded); err != nil {
			t.Fatal(err)
		}
		if _, err := sessionevidence.Decode(append(bounded, ' ')); err == nil {
			t.Fatal("oversize journal")
		}
	})
}

func TestTouchpadSessionClaimSelectionAndAliases(t *testing.T) {
	for _, c := range []struct {
		name, raw string
		want      bool
	}{
		{"canonical", `{"schema":"hhsoft.pc-tester.touchpad-functional/v1"}`, true},
		{"case_family", `{"schema":"HHSOFT.PC-TESTER.TOUCHPAD-FUNCTIONAL/v1"}`, true},
		{"unknown_version", `{"schema":"hhsoft.pc-tester.touchpad-functional/v999"}`, true},
		{"escaped_family", `{"schema":"hhsoft.pc-tester.touchpad\u002dfunctional/v1"}`, true},
		{"earlier_duplicate", `{"schema":"hhsoft.pc-tester.touchpad-functional/v1","schema":"opaque"}`, true},
		{"nested_claim", `{"nested":["hhsoft.pc-tester.touchpad-functional/v1"]}`, true},
		{"claim_key", `{"hhsoft.pc-tester.touchpad-functional/v1":0}`, true},
		{"malformed", `{"schema":`, true},
		{"bare_id_reference", `{"id":"input.touchpad.quick"}`, false},
		{"other_family", `{"schema":"hhsoft.pc-tester.pointer-functional/v1"}`, false},
		{"empty", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "escaped_family" {
				if !strings.Contains(c.raw, `\u002d`) || c.raw == `{"schema":"hhsoft.pc-tester.touchpad-functional/v1"}` {
					t.Fatal("ineffective escaped JSON fixture")
				}
				var header struct {
					Schema string `json:"schema"`
				}
				if json.Unmarshal([]byte(c.raw), &header) != nil || header.Schema != "hhsoft.pc-tester.touchpad-functional/v1" {
					t.Fatal("escaped fixture changed the decoded claim")
				}
			}
			if sessionevidence.HasTouchpadClaim([]byte(c.raw)) != c.want {
				t.Fatal("strict claim selector mismatch")
			}
		})
	}
	t.Run("bounded_depth", func(t *testing.T) {
		if !sessionevidence.HasTouchpadClaim([]byte(strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65))) {
			t.Fatal("depth bound")
		}
	})
	t.Run("bounded_length", func(t *testing.T) {
		if !sessionevidence.HasTouchpadClaim(bytes.Repeat([]byte(" "), (1<<20)+1)) {
			t.Fatal("length bound")
		}
	})
	t.Run("invalid_utf8", func(t *testing.T) {
		if !sessionevidence.HasTouchpadClaim([]byte{'"', 0xff, '"'}) {
			t.Fatal("UTF-8 boundary")
		}
	})
}

func TestTouchpadSessionOldVersionsDoNotGainFunctionalClaims(t *testing.T) {
	for _, version := range []string{"v4", "v5", "v6", "v7", "v8"} {
		t.Run(version, func(t *testing.T) {
			j, rows := touchpadAllOff(t, version)
			legacy := sessionevidence.ResultBinding{ID: "input.touchpad.quick", Version: 1, PlannedStatus: "unsupported", ReportStatus: "skip",
				Evidence: touchpadNote("skip", "unsupported", "legacy description")}
			rows = append(rows, legacy)
			if err := sessionevidence.ValidateBindings(j, rows); err != nil {
				t.Fatal("old nonfunctional definition-1 row changed", err)
			}
			rows[len(rows)-1].Version = 2
			if sessionevidence.ValidateBindings(j, rows) == nil {
				t.Fatal("definition-2 bypassed old worker graph")
			}
			rows[len(rows)-1] = legacy
			rows[len(rows)-1].Evidence = []byte(touchpadSessionPass)
			if sessionevidence.ValidateBindings(j, rows) == nil {
				t.Fatal("functional claim used old definition")
			}
			rows[len(rows)-1].ID = "other.quick"
			if sessionevidence.ValidateBindings(j, rows) == nil {
				t.Fatal("functional claim hidden under foreign row")
			}
		})
	}
}

func TestTouchpadSessionBindingRejectsContradictions(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*sessionevidence.Journal, *[]sessionevidence.ResultBinding, int)
	}{
		{"missing_row", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) {
			*r = append((*r)[:i], (*r)[i+1:]...)
		}},
		{"duplicate_row", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) { *r = append(*r, (*r)[i]) }},
		{"alias_id", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) {
			(*r)[i].ID = "INPUT.TOUCHPAD.QUICK"
		}},
		{"old_definition", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) { (*r)[i].Version = 1 }},
		{"future_definition", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) { (*r)[i].Version = 3 }},
		{"foreign_functional_row", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) {
			extra := (*r)[i]
			extra.ID = "other.quick"
			*r = append(*r, extra)
		}},
		{"planned_excluded", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) {
			(*r)[i].PlannedStatus = "excluded"
		}},
		{"report_status", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) {
			(*r)[i].ReportStatus = "inconclusive"
		}},
		{"worker_outcome", func(j *sessionevidence.Journal, _ *[]sessionevidence.ResultBinding, i int) {
			j.Workers[i].Outcome = "inconclusive"
		}},
		{"missing_timing", func(j *sessionevidence.Journal, _ *[]sessionevidence.ResultBinding, i int) { j.Workers[i].Timing = nil }},
		{"negative_start", func(j *sessionevidence.Journal, _ *[]sessionevidence.ResultBinding, i int) {
			j.Workers[i].Timing.StartedMilliseconds = -1
		}},
		{"reversed_interval", func(j *sessionevidence.Journal, _ *[]sessionevidence.ResultBinding, i int) {
			j.Workers[i].Timing.FinishedMilliseconds = 9
		}},
		{"duration_over_interval", func(j *sessionevidence.Journal, _ *[]sessionevidence.ResultBinding, i int) {
			j.Workers[i].Timing.FinishedMilliseconds = 109
		}},
		{"beyond_preparation", func(j *sessionevidence.Journal, _ *[]sessionevidence.ResultBinding, i int) {
			j.Workers[i].Timing.FinishedMilliseconds = j.PreparationStartedMilliseconds + 1
		}},
		{"missing_before_safety", func(j *sessionevidence.Journal, _ *[]sessionevidence.ResultBinding, _ int) {
			j.Safety = append(j.Safety[:1], j.Safety[2:]...)
		}},
		{"interrupted_pass", func(j *sessionevidence.Journal, _ *[]sessionevidence.ResultBinding, i int) {
			j.Workers[i].State = "interrupted"
			j.StopReason = "operator_cancelled"
		}},
		{"not_started_observation", func(j *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) {
			j.StopReason = "operator_cancelled"
			j.Workers[i].State = "not_started"
			j.Workers[i].Outcome = "aborted"
			j.Workers[i].Timing = nil
			j.Safety = append(j.Safety[:1], j.Safety[2:]...)
			(*r)[i].ReportStatus = "aborted"
		}},
		{"unscheduled_observation", func(j *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) {
			j.Workers[i].State = "not_scheduled"
			j.Workers[i].Outcome = "excluded"
			j.Workers[i].Timing = nil
			j.Safety = append(j.Safety[:1], j.Safety[2:]...)
			(*r)[i].PlannedStatus = "excluded"
			(*r)[i].ReportStatus = "skip"
		}},
		{"invalid_typed_counts", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) {
			(*r)[i].Evidence = bytes.Replace((*r)[i].Evidence, []byte(`"x_positive":2`), []byte(`"x_positive":5`), 1)
		}},
		{"false_final_neutral", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) {
			(*r)[i].Evidence = bytes.Replace((*r)[i].Evidence, []byte(`"final_neutral":true`), []byte(`"final_neutral":false`), 1)
		}},
		{"earlier_duplicate_schema", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) {
			(*r)[i].Evidence = bytes.Replace((*r)[i].Evidence, []byte(`{"schema":`), []byte(`{"schema":"opaque","schema":`), 1)
		}},
		{"case_schema", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) {
			(*r)[i].Evidence = bytes.Replace((*r)[i].Evidence, []byte("touchpad-functional"), []byte("TOUCHPAD-FUNCTIONAL"), 1)
		}},
		{"unknown_schema_version", func(_ *sessionevidence.Journal, r *[]sessionevidence.ResultBinding, i int) {
			(*r)[i].Evidence = bytes.Replace((*r)[i].Evidence, []byte("touchpad-functional/v1"), []byte("touchpad-functional/v2"), 1)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			j, rows, i := touchpadObserved(t, "v9")
			c.change(&j, &rows, i)
			if sessionevidence.ValidateBindings(j, rows) == nil {
				t.Fatal("contradictory touchpad binding admitted")
			}
		})
	}
}

func TestTouchpadSessionProducerPhasesRemainRepresentable(t *testing.T) {
	for _, c := range []struct {
		name, state, sessionStop string
		change                   func(*inputtest.TouchpadResult)
	}{
		{"pass_exact_interval", "completed", "completed", func(*inputtest.TouchpadResult) {}},
		{"retained_pass_cancel", "interrupted", "operator_cancelled", func(r *inputtest.TouchpadResult) {
			r.Status = inputtest.StatusAborted
			r.StopReason = "operator_cancelled"
			r.Detail = "operator_cancelled"
		}},
		{"retained_pass_deadline", "interrupted", "test_deadline", func(r *inputtest.TouchpadResult) {
			r.Status = inputtest.StatusError
			r.StopReason = "deadline"
			r.Detail = "deadline"
			r.DurationMilliseconds = 1001
		}},
		{"retained_pass_timeout", "completed", "completed", func(r *inputtest.TouchpadResult) {
			r.Status = inputtest.StatusInconclusive
			r.StopReason = "observation_timeout"
			r.Detail = "observation_timeout"
		}},
		{"candidate_cancel", "interrupted", "operator_cancelled", func(r *inputtest.TouchpadResult) {
			r.Status = inputtest.StatusAborted
			r.StopReason = "operator_cancelled"
			r.Detail = "operator_cancelled"
			r.Devices[0].Status = inputtest.StatusInconclusive
			r.Devices[0].FinalNeutral = false
		}},
		{"terminal_revocation", "completed", "worker_stop", func(r *inputtest.TouchpadResult) {
			r.Status = inputtest.StatusError
			r.StopReason = "error"
			r.Detail = "terminal_not_neutral"
			r.Devices[0].Status = inputtest.StatusInconclusive
			r.Devices[0].Detail = "terminal_not_neutral"
			r.Devices[0].FinalNeutral = false
		}},
		{"unsupported_platform", "completed", "completed", func(r *inputtest.TouchpadResult) {
			r.Status = inputtest.StatusInconclusive
			r.StopReason = "unsupported_platform"
			r.Detail = "unsupported_platform"
			r.Devices = []inputtest.TouchpadDeviceResult{}
		}},
		{"no_touchpads", "completed", "completed", func(r *inputtest.TouchpadResult) {
			r.Status = inputtest.StatusInconclusive
			r.StopReason = "no_touchpads"
			r.Detail = "no_touchpads"
			r.Devices = []inputtest.TouchpadDeviceResult{}
		}},
		{"invalid_options", "completed", "worker_stop", func(r *inputtest.TouchpadResult) {
			r.Status = inputtest.StatusError
			r.StopReason = "error"
			r.Detail = "invalid_options"
			r.Devices = []inputtest.TouchpadDeviceResult{}
			r.LimitMilliseconds = 0
			r.DurationMilliseconds = 0
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			j, rows, i := touchpadObserved(t, "v9")
			value := touchpadEvidenceValue(t, rows[i].Evidence)
			c.change(&value)
			rows[i].Evidence = touchpadEncode(t, value)
			rows[i].ReportStatus = string(value.Status)
			j.StopReason = c.sessionStop
			j.Workers[i].State = c.state
			j.Workers[i].Outcome = string(value.Status)
			j.Workers[i].Timing.FinishedMilliseconds = 10 + value.DurationMilliseconds
			if err := sessionevidence.ValidateBindings(j, rows); err != nil {
				t.Fatal("phase-compatible diagnostic rejected", err)
			}
			if value.Status != inputtest.StatusPass && rows[i].ReportStatus == "pass" {
				t.Fatal("diagnostic promoted to pass")
			}
		})
	}
}

func TestTouchpadSessionCleanupUncertaintyCannotBecomeSuccess(t *testing.T) {
	for _, c := range []struct {
		name           string
		row, malformed bool
	}{
		{"owner_close", true, false},
		{"retained_row_after_registry_close", false, false},
		{"malformed_and_uncertain", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			j, rows, i := touchpadObserved(t, "v10")
			value := touchpadEvidenceValue(t, rows[i].Evidence)
			value.Status = inputtest.StatusError
			value.StopReason = "cleanup_unconfirmed"
			value.Detail = "cleanup_unconfirmed"
			value.CleanupConfirmed = false
			if c.row {
				value.Devices[0].Status = inputtest.StatusError
				value.Devices[0].Detail = "cleanup_unconfirmed"
				value.Devices[0].CleanupConfirmed = false
			}
			rows[i].ReportStatus = "error"
			rows[i].Evidence = touchpadEncode(t, value)
			j.Workers[i].Outcome = "error"
			j.StopReason = "worker_stop"
			if c.malformed {
				rows[i].Evidence = append(rows[i].Evidence, ']')
			}
			err := sessionevidence.ValidateBindings(j, rows)
			if err == nil {
				t.Fatal("cleanup uncertainty accepted")
			}
			if !c.malformed && !errors.Is(err, touchpadevidence.ErrCleanupUnconfirmed) {
				t.Fatal("cleanup cause lost", err)
			}
		})
	}
}

func TestTouchpadSessionFiniteUnobservedNotes(t *testing.T) {
	for _, c := range []struct{ name, planned, state, outcome, status string }{
		{"excluded", "excluded", "not_scheduled", "excluded", "skip"},
		{"not_present", "not_present", "not_scheduled", "not_present", "skip"},
		{"unsupported", "unsupported", "not_scheduled", "unsupported", "skip"},
		{"missing_driver", "missing_driver", "not_scheduled", "missing_driver", "inconclusive"},
		{"not_started", "scheduled", "not_started", "aborted", "aborted"},
		{"completed_error", "scheduled", "completed", "error", "error"},
		{"completed_aborted", "scheduled", "completed", "aborted", "aborted"},
		{"interrupted_error", "scheduled", "interrupted", "error", "error"},
		{"interrupted_aborted", "scheduled", "interrupted", "aborted", "aborted"},
	} {
		t.Run(c.name, func(t *testing.T) {
			j, rows := touchpadAllOff(t, "v9")
			i := touchpadBindingIndex(t, j)
			j.Workers[i].State = c.state
			j.Workers[i].Outcome = c.outcome
			detail := "not run; disposition recorded in the frozen plan"
			if c.planned == "scheduled" {
				detail = "bounded evidence was not produced; outcome recorded by the run supervisor"
				j.StopReason = "worker_stop"
			}
			if c.state == "completed" || c.state == "interrupted" {
				j.Workers[i].Timing = &sessionevidence.WorkerTiming{StartedMilliseconds: 10, FinishedMilliseconds: 11}
				j.Safety = append(j.Safety[:1], append([]sessionevidence.SafetyRecord{
					{Phase: "before-input.touchpad.quick", Result: j.Safety[0].Result},
				}, j.Safety[1:]...)...)
			}
			rows[i].PlannedStatus = c.planned
			rows[i].ReportStatus = c.status
			rows[i].Evidence = touchpadNote(c.status, c.outcome, detail)
			if err := sessionevidence.ValidateTouchpadNote(rows[i]); err != nil {
				t.Fatal("finite note rejected", err)
			}
			if err := sessionevidence.ValidateBindings(j, rows); err != nil {
				t.Fatal("note binding rejected", err)
			}
		})
	}
}

func TestTouchpadSessionNotesCannotSubstituteObservations(t *testing.T) {
	good := sessionevidence.ResultBinding{ID: "input.touchpad.quick", Version: 2, PlannedStatus: "excluded", ReportStatus: "skip",
		Evidence: touchpadNote("skip", "excluded", "not run; disposition recorded in the frozen plan")}
	for _, c := range []struct {
		name   string
		change func(*sessionevidence.ResultBinding)
	}{
		{"wrong_id", func(b *sessionevidence.ResultBinding) { b.ID = "input.pointer.quick" }},
		{"case_id", func(b *sessionevidence.ResultBinding) { b.ID = "Input.Touchpad.Quick" }},
		{"definition_one", func(b *sessionevidence.ResultBinding) { b.Version = 1 }},
		{"unknown_disposition", func(b *sessionevidence.ResultBinding) { b.PlannedStatus = "pending" }},
		{"wrong_report_status", func(b *sessionevidence.ResultBinding) { b.ReportStatus = "inconclusive" }},
		{"source_status", func(b *sessionevidence.ResultBinding) {
			b.Evidence = touchpadNote("skip", "unsupported", "not run; disposition recorded in the frozen plan")
		}},
		{"arbitrary_detail", func(b *sessionevidence.ResultBinding) { b.Evidence = touchpadNote("skip", "excluded", "not run") }},
		{"foreign_schema", func(b *sessionevidence.ResultBinding) {
			b.Evidence = bytes.Replace(b.Evidence, []byte("execution-note/v1"), []byte("execution-note/v2"), 1)
		}},
		{"duplicate_detail", func(b *sessionevidence.ResultBinding) {
			b.Evidence = bytes.Replace(b.Evidence, []byte(`"detail":`), []byte(`"detail":"x","detail":`), 1)
		}},
		{"unknown_member", func(b *sessionevidence.ResultBinding) {
			b.Evidence = bytes.Replace(b.Evidence, []byte("{"), []byte(`{"extra":false,`), 1)
		}},
		{"missing_status", func(b *sessionevidence.ResultBinding) {
			b.Evidence = bytes.Replace(b.Evidence, []byte(`"status":"skip",`), nil, 1)
		}},
		{"null_detail", func(b *sessionevidence.ResultBinding) {
			b.Evidence = bytes.Replace(b.Evidence, []byte(`"detail":"not run; disposition recorded in the frozen plan"`), []byte(`"detail":null`), 1)
		}},
		{"hidden_functional_claim", func(b *sessionevidence.ResultBinding) {
			b.Evidence = touchpadNote("skip", "excluded", "hhsoft.pc-tester.touchpad-functional/v1")
		}},
		{"observed_skip", func(b *sessionevidence.ResultBinding) {
			b.PlannedStatus = "scheduled"
			b.Evidence = touchpadNote("skip", "skip", "bounded evidence was not produced; outcome recorded by the run supervisor")
		}},
		{"observed_inconclusive", func(b *sessionevidence.ResultBinding) {
			b.PlannedStatus = "scheduled"
			b.ReportStatus = "inconclusive"
			b.Evidence = touchpadNote("inconclusive", "inconclusive", "bounded evidence was not produced; outcome recorded by the run supervisor")
		}},
		{"observed_pass", func(b *sessionevidence.ResultBinding) {
			b.PlannedStatus = "scheduled"
			b.ReportStatus = "pass"
			b.Evidence = touchpadNote("pass", "pass", "bounded evidence was not produced; outcome recorded by the run supervisor")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := good
			b.Evidence = append([]byte{}, good.Evidence...)
			c.change(&b)
			if sessionevidence.ValidateTouchpadNote(b) == nil {
				t.Fatal("unobserved note overstated")
			}
		})
	}
	t.Run("pass_worker_cannot_use_error_note", func(t *testing.T) {
		j, rows, i := touchpadObserved(t, "v9")
		rows[i].Evidence = touchpadNote("error", "error", "bounded evidence was not produced; outcome recorded by the run supervisor")
		if sessionevidence.ValidateBindings(j, rows) == nil {
			t.Fatal("pass worker used an error note")
		}
	})
}

func TestTouchpadSessionStructureAndVersionedMarshal(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*sessionevidence.Journal)
	}{
		{"missing_worker", func(j *sessionevidence.Journal) { j.Workers = j.Workers[:len(j.Workers)-1] }},
		{"extra_worker", func(j *sessionevidence.Journal) { j.Workers = append(j.Workers, j.Workers[0]) }},
		{"swapped_workers", func(j *sessionevidence.Journal) { j.Workers[5], j.Workers[6] = j.Workers[6], j.Workers[5] }},
		{"duplicate_worker", func(j *sessionevidence.Journal) { j.Workers[6] = j.Workers[5] }},
		{"worker_id_alias", func(j *sessionevidence.Journal) { j.Workers[6].ID = "INPUT.TOUCHPAD.QUICK" }},
		{"unscheduled_timing", func(j *sessionevidence.Journal) { j.Workers[6].Timing = &sessionevidence.WorkerTiming{} }},
		{"borrowed_standard_profile", func(j *sessionevidence.Journal) { j.Profile = "standard" }},
		{"borrowed_stress_source", func(j *sessionevidence.Journal) { j.CPUStressSource = &sessionevidence.StressSource{} }},
		{"borrowed_storage_source", func(j *sessionevidence.Journal) { j.StorageSource = &sessionevidence.StorageSource{} }},
		{"wrong_worker_schema", func(j *sessionevidence.Journal) { j.Schema = sessionevidence.SchemaV10; j.Profile = "standard" }},
		{"old_schema_new_worker", func(j *sessionevidence.Journal) { j.Schema = sessionevidence.SchemaV7 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			j, _ := touchpadAllOff(t, "v9")
			c.change(&j)
			if sessionevidence.ValidateStructure(j) == nil {
				t.Fatal("invalid worker graph admitted")
			}
		})
	}
	for _, version := range []string{"v4", "v5", "v6", "v7", "v9"} {
		t.Run("marshal_"+version, func(t *testing.T) {
			j, _ := touchpadAllOff(t, version)
			j.Profile = "standard"
			if _, err := json.Marshal(j); err == nil {
				t.Fatal("encoder silently dropped Standard members")
			}
		})
	}
}

func TestTouchpadSessionStandardStressAndHistory(t *testing.T) {
	for _, version := range []string{"v8", "v10"} {
		t.Run(version, func(t *testing.T) {
			j, rows := standardStressJournal(t)
			if version == "v10" {
				j.Schema = sessionevidence.SchemaV10
				index := -1
				for i, w := range j.Workers {
					if w.ID == "input.pointer.quick" {
						index = i + 1
					}
				}
				if index < 0 {
					t.Fatal("literal Standard predecessor lacks pointer")
				}
				j.Workers = append(j.Workers[:index], append([]sessionevidence.WorkerRecord{
					{ID: "input.touchpad.quick", State: "not_scheduled", Outcome: "excluded"},
				}, j.Workers[index:]...)...)
				rows = append(rows, sessionevidence.ResultBinding{ID: "input.touchpad.quick", Version: 2, PlannedStatus: "excluded", ReportStatus: "skip",
					Evidence: touchpadNote("skip", "excluded", "not run; disposition recorded in the frozen plan")})
			}
			if err := sessionevidence.ValidateBindings(j, rows); err != nil {
				t.Fatal("retained Standard evidence", err)
			}
			decoded, err := sessionevidence.Decode(touchpadEncode(t, j))
			if err != nil || decoded.CPUStressSource == nil || sessionevidence.ThermalViewOf(decoded).Scope != sessionevidence.StandardScope {
				t.Fatal("Standard source/wire lost", err)
			}
			copy := sessionevidence.CloneJournal(j)
			copy.CPUStressSource.EvidenceTypedSHA256 = "changed"
			copy.Workers[1].Timing.FinishedMilliseconds++
			if copy.CPUStressSource.EvidenceTypedSHA256 == j.CPUStressSource.EvidenceTypedSHA256 ||
				copy.Workers[1].Timing.FinishedMilliseconds == j.Workers[1].Timing.FinishedMilliseconds {
				t.Fatal("Standard source/timing alias")
			}
			bad := sessionevidence.CloneJournal(j)
			bad.CPUStressSource = nil
			if sessionevidence.ValidateBindings(bad, rows) == nil {
				t.Fatal("dispatched Standard source omitted")
			}
			changed := append([]sessionevidence.ResultBinding{}, rows...)
			for i := range changed {
				if changed[i].ID == "cpu.stress" {
					changed[i].Evidence = bytes.Replace(changed[i].Evidence, []byte(`"requested_load_ms":600000`), []byte(`"requested_load_ms":599999`), 1)
				}
			}
			if sessionevidence.ValidateBindings(j, changed) == nil {
				t.Fatal("Standard load requirement weakened")
			}
		})
	}
	t.Run("inspection_stays_version_bound", func(t *testing.T) {
		j, rows := touchpadAllOff(t, "v5")
		rows = append(rows, sessionevidence.ResultBinding{ID: "inspection.exterior.quick", Version: 1, PlannedStatus: "excluded", ReportStatus: "skip",
			Evidence: touchpadNote("skip", "excluded", "not run; disposition recorded in the frozen plan")})
		if sessionevidence.ValidateBindings(j, rows) == nil {
			t.Fatal("inspection entered an old worker version")
		}
	})
}

func TestTouchpadSessionIntegrityEnvelopeRemainsStrict(t *testing.T) {
	for _, version := range []string{"v9", "v10"} {
		t.Run(version, func(t *testing.T) {
			j, _ := touchpadAllOff(t, version)
			raw := []byte(`{"schema":"hhsoft.pc-tester.run-integrity/v1","status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","planned_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","final_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","planned_component_count":1,"final_component_count":1,"session":` + string(touchpadEncode(t, j)) + "}")
			value, err := sessionevidence.DecodeIntegrity(raw)
			if err != nil || value.Session == nil || value.Session.Schema != j.Schema {
				t.Fatal("valid integrity envelope", err)
			}
			// The graph hashes/counts stay declared structural facts; this
			// decoder does not reobserve inventory or acquire a frozen plan.
			changed := bytes.Replace(raw, []byte(`"final_component_count":1`), []byte(`"final_component_count":2`), 1)
			if _, err := sessionevidence.DecodeIntegrity(changed); err == nil {
				t.Fatal("component count contradiction")
			}
			changed = bytes.Replace(raw, []byte(`"status":"pass"`), []byte(`"status":"fail"`), 1)
			if _, err := sessionevidence.DecodeIntegrity(changed); err == nil {
				t.Fatal("unchanged graph called changed")
			}
			changed = bytes.Replace(raw, []byte(`"plan_id":"sha256:1`), []byte(`"plan_id":"sha256:2`), 1)
			if _, err := sessionevidence.DecodeIntegrity(changed); err == nil {
				t.Fatal("session/envelope plan mismatch")
			}
			changed = bytes.Replace(raw, []byte(`"session":`), []byte(`"session":null,"session":`), 1)
			if _, err := sessionevidence.DecodeIntegrity(changed); err == nil {
				t.Fatal("duplicate session")
			}
		})
	}
}

func TestTouchpadSessionDecodeAndViewsOwnStorage(t *testing.T) {
	raw := touchpadSessionLiteral(t, "v9")
	j, err := sessionevidence.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		raw[i] = ' '
	}
	if j.Schema != "hhsoft.pc-tester.quick-session/v9" || j.Workers[6].ID != "input.touchpad.quick" {
		t.Fatal("decoded strings alias input")
	}
	j, rows, i := touchpadObserved(t, "v9")
	before := touchpadEncode(t, j)
	copy := sessionevidence.CloneJournal(j)
	copy.Workers[i].ID = "changed"
	copy.Workers[i].Timing.FinishedMilliseconds++
	copy.Safety[0].Phase = "changed"
	copy.Monitor.LastObservation.Status = "changed"
	copy.Thermal.Initial.Observation.Coverage = "changed"
	copy.Thermal.Final.Observation.Coverage = "changed"
	copy.Thermal.LastObservation.Coverage = "changed"
	if !bytes.Equal(before, touchpadEncode(t, j)) {
		t.Fatal("CloneJournal aliases owned nested values")
	}
	bindingBefore := append([]byte{}, rows[i].Evidence...)
	if err := sessionevidence.ValidateBindings(j, rows); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bindingBefore, rows[i].Evidence) || !bytes.Equal(before, touchpadEncode(t, j)) {
		t.Fatal("pure validation mutated caller")
	}
	// View storage is independent even for a supplied local-source projection.
	// This does not assert that the invented source is a valid thermal claim.
	j.LocalThermalSources = []sessionevidence.LocalThermalSource{{WorkerID: "cpu.quick"}}
	view := sessionevidence.ThermalViewOf(j)
	view.LocalSources[0].WorkerID = "changed"
	if j.LocalThermalSources[0].WorkerID != "cpu.quick" {
		t.Fatal("thermal view aliases source slice")
	}
}

// Literal monitoring values retain the established Standard fixture; worker
// sequences and the inactive touchpad observation are independently fixed above.
const touchpadSessionPrefix = `","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","stop_reason":"completed","budget_milliseconds":900000,"elapsed_milliseconds":600050,"workers":[`
const touchpadSessionTail = `],"safety":[{"phase":"preflight","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"final","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"pre-sign","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}}],"monitor":{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"pass","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":600,"completed_observations":600,"elapsed_ms":600046,"first_completed_ms":1000,"last_completed_ms":600000,"maximum_gap_ms":1000,"cleanup_confirmed":true,"last_observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},"preparation_started_ms":600020,"inventory_prepared_ms":600030,"stop_requested_ms":600040,"safety_joined_ms":600045,"thermal":{"schema":"hhsoft.pc-tester.run-thermal/v1","status":"completed","limit_millicelsius":90000,"interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"stop_timeout_ms":2000,"attempted_observations":602,"completed_observations":602,"periodic_attempts":600,"periodic_completions":600,"elapsed_ms":600048,"first_completed_ms":1,"last_completed_ms":600032,"maximum_gap_ms":1000,"initial":{"requested_ms":0,"attempted":true,"completed":true,"started_ms":0,"completed_ms":1,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"final":{"requested_ms":600031,"attempted":true,"completed":true,"started_ms":600031,"completed_ms":600032,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"aggregate":{"coverage":"unavailable","accepted_observations":602,"available_observations":0,"partial_observations":0,"unavailable_observations":602,"maximum_sensors":0,"valid_readings":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0},"cleanup_confirmed":true,"joined":true,"joined_ms":600047,"last_observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"local_thermal_sources":[]`
const touchpadSessionPass = `{"schema":"hhsoft.pc-tester.touchpad-functional/v1","status":"pass","stop_reason":"completed","limit_ms":1000,"duration_ms":100,"cleanup_confirmed":true,"devices":[{"interface":"event4","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":4660,"product":22136,"version":1},"capabilities":{"protocol_version":65537,"events":[0,1,3,4],"keys":[272,273,274,325,330],"relative":[],"properties":[0,2],"misc":[4,5],"axes":[{"code":0,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1},{"code":1,"minimum":-500,"maximum":500,"fuzz":0,"flat":0,"resolution":1}]},"x_threshold":50,"y_threshold":50,"frames":6,"contact_starts":1,"contact_releases":1,"xy_baselines":1,"qualifying_contacts":1,"x_positive":2,"x_negative":2,"y_positive":2,"y_negative":2,"buttons":[{"button":"left","presses":1,"releases":1,"pairs":1},{"button":"right","presses":1,"releases":1,"pairs":1},{"button":"middle","presses":1,"releases":1,"pairs":1}],"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]}`
