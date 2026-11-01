package exteriorinspection

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

const conditionPassJSONV2 = `{"schema":"hhsoft.pc-tester.exterior-inspection/v2","source":"operator_reported","scope":"visible_exterior_condition","findings":{"enclosure_integrity":"no_issue_observed","hinge_mount_condition":"not_applicable","port_exterior_condition":"no_issue_observed","surface_finish_condition":"no_issue_observed"},"responses_completed":4,"stop_reason":"completed","status":"pass","observation_milliseconds":17}`

func TestExteriorConditionV2IndependentFiniteCriteria(t *testing.T) {
	for _, test := range []struct {
		name    string
		answers [4]string
		want    [4]string
		status  string
	}{
		{"four_observed", [4]string{"N", "N", "N", "N"}, [4]string{"no_issue_observed", "no_issue_observed", "no_issue_observed", "no_issue_observed"}, "pass"},
		{"explicit_absent_hinge", [4]string{"N", "A", "N", "N"}, [4]string{"no_issue_observed", "not_applicable", "no_issue_observed", "no_issue_observed"}, "pass"},
		{"hidden_hinge", [4]string{"N", "I", "N", "N"}, [4]string{"no_issue_observed", "unobserved", "no_issue_observed", "no_issue_observed"}, "inconclusive"},
		{"enclosure_issue", [4]string{"D", "N", "N", "N"}, [4]string{"issue_present", "no_issue_observed", "no_issue_observed", "no_issue_observed"}, "fail"},
		{"hinge_issue", [4]string{"N", "D", "N", "N"}, [4]string{"no_issue_observed", "issue_present", "no_issue_observed", "no_issue_observed"}, "fail"},
		{"port_issue", [4]string{"N", "A", "D", "N"}, [4]string{"no_issue_observed", "not_applicable", "issue_present", "no_issue_observed"}, "fail"},
		{"finish_issue", [4]string{"N", "A", "N", "D"}, [4]string{"no_issue_observed", "not_applicable", "no_issue_observed", "issue_present"}, "fail"},
		{"issue_with_unobserved", [4]string{"D", "I", "I", "I"}, [4]string{"issue_present", "unobserved", "unobserved", "unobserved"}, "fail"},
		{"enclosure_na_invalid", [4]string{"A", "A", "N", "N"}, [4]string{"unobserved", "not_applicable", "no_issue_observed", "no_issue_observed"}, "inconclusive"},
		{"port_na_invalid", [4]string{"N", "A", "A", "N"}, [4]string{"no_issue_observed", "not_applicable", "unobserved", "no_issue_observed"}, "inconclusive"},
		{"finish_na_invalid", [4]string{"N", "A", "N", "A"}, [4]string{"no_issue_observed", "not_applicable", "no_issue_observed", "unobserved"}, "inconclusive"},
		{"private_text_unobserved", [4]string{"synthetic-private-canary", "A", "N", "N"}, [4]string{"unobserved", "not_applicable", "no_issue_observed", "no_issue_observed"}, "inconclusive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			responses := make([]ConditionResponseV2, 4)
			for i, a := range test.answers {
				responses[i] = ConditionResponseV2{Text: a, Complete: true}
			}
			got, err := EvaluateV2(responses, "", 17)
			actual := [4]string{got.Findings.EnclosureIntegrity, got.Findings.HingeMountCondition, got.Findings.PortExteriorCondition, got.Findings.SurfaceFinishCondition}
			if err != nil || actual != test.want || got.Status != test.status || got.ResponsesCompleted != 4 ||
				got.StopReason != "completed" || got.Source != "operator_reported" || got.Scope != "visible_exterior_condition" {
				t.Fatalf("result=%+v err=%v", got, err)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeV2(raw)
			if err != nil || decoded != got || bytes.Contains(raw, []byte("synthetic-private-canary")) {
				t.Fatal("finite evidence or privacy invariant changed")
			}
		})
	}
}

func TestExteriorConditionV2StrictShapeAndHistory(t *testing.T) {
	if _, err := DecodeV2([]byte(conditionPassJSONV2)); err != nil {
		t.Fatal("literal valid prerequisite", err)
	}
	for _, test := range []struct{ name, old, replacement string }{
		{"unknown_criterion", `"surface_finish_condition":"no_issue_observed"`, `"extra":"no_issue_observed"`},
		{"missing_criterion", `,"surface_finish_condition":"no_issue_observed"`, ``},
		{"null_criterion", `"hinge_mount_condition":"not_applicable"`, `"hinge_mount_condition":null`},
		{"duplicate_criterion", `"hinge_mount_condition":"not_applicable"`, `"hinge_mount_condition":"not_applicable","hinge_mount_condition":"no_issue_observed"`},
		{"escaped_duplicate", `"hinge_mount_condition":"not_applicable"`, `"hinge_mount_condition":"not_applicable","\u0068inge_mount_condition":"not_applicable"`},
		{"case_alias", `"enclosure_integrity":`, `"Enclosure_integrity":`},
		{"unknown_member", `"source":"operator_reported"`, `"source":"operator_reported","operator_name":"synthetic-private-canary"`},
		{"wrong_source", `"operator_reported"`, `"automated_sensor"`},
		{"legacy_schema", `"hhsoft.pc-tester.exterior-inspection/v2"`, `"hhsoft.pc-tester.exterior-inspection/v1"`},
		{"fraction_count", `"responses_completed":4`, `"responses_completed":4.0`},
		{"string_count", `"responses_completed":4`, `"responses_completed":"4"`},
		{"foreign_na", `"port_exterior_condition":"no_issue_observed"`, `"port_exterior_condition":"not_applicable"`},
		{"stripped_prefix", `"responses_completed":4`, `"responses_completed":3`},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := strings.Replace(conditionPassJSONV2, test.old, test.replacement, 1)
			if raw == conditionPassJSONV2 {
				t.Fatal("mutation did not change bytes")
			}
			if test.name == "missing_criterion" && (!json.Valid([]byte(raw)) || strings.Contains(raw, "surface_finish_condition") || strings.Contains(raw, `"extra"`)) {
				t.Fatal("true member-removal prerequisite")
			}
			if got, err := DecodeV2([]byte(raw)); err == nil || got != (ResultV2{}) {
				t.Fatal("malformed evidence admitted")
			}
		})
	}
	escaped := strings.Replace(conditionPassJSONV2, `"schema":`, `"\u0073chema":`, 1)
	if escaped == conditionPassJSONV2 {
		t.Fatal("escaped prerequisite was ineffective")
	}
	if got, err := DecodeV2([]byte(escaped)); err != nil || got.Schema != SchemaV2 {
		t.Fatal("canonical decoded member rejected")
	}
	legacy, err := Decode([]byte(inspectionPassJSON))
	if err != nil || legacy.Schema != "hhsoft.pc-tester.exterior-inspection/v1" {
		t.Fatal("historical v1 prerequisite")
	}
	if _, err := DecodeV2([]byte(inspectionPassJSON)); err == nil {
		t.Fatal("v1 promoted to v2")
	}
	if _, err := Decode([]byte(conditionPassJSONV2)); err == nil {
		t.Fatal("v2 accepted as historical v1")
	}
	for _, raw := range [][]byte{nil, []byte("null"), []byte("[]"), []byte(conditionPassJSONV2 + "{}"), append([]byte(conditionPassJSONV2), 0xff)} {
		if _, err := DecodeV2(raw); err == nil {
			t.Fatal("invalid wire accepted")
		}
	}
	exact := append([]byte(conditionPassJSONV2), bytes.Repeat([]byte(" "), 4096-len(conditionPassJSONV2))...)
	if _, err := DecodeV2(exact); err != nil {
		t.Fatal("exact bound rejected")
	}
	if _, err := DecodeV2(append(exact, ' ')); err == nil {
		t.Fatal("over-bound evidence accepted")
	}
}

func TestExteriorConditionV2StopsPreserveOnlyAdverseFinalAnswers(t *testing.T) {
	for _, test := range []struct {
		name, last, stop, want, status string
		complete                       bool
	}{
		{"early_issue_later_eof", "", "", "unobserved", "fail", false},
		{"early_issue_later_cancel", "N", "operator_cancelled", "unobserved", "fail", true},
		{"early_issue_later_deadline", "A", "deadline", "unobserved", "fail", true},
		{"late_issue", "D", "deadline", "issue_present", "fail", true},
		{"late_issue_hard", "D", "input_error", "issue_present", "error", true},
		{"partial_issue", "D", "operator_cancelled", "unobserved", "fail", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := EvaluateV2([]ConditionResponseV2{{Text: "D", Complete: true}, {Text: test.last, Complete: test.complete}}, test.stop, 17)
			wantCount := 1
			if test.complete {
				wantCount = 2
			}
			if err != nil || got.Findings.EnclosureIntegrity != "issue_present" || got.Findings.HingeMountCondition != test.want ||
				got.Findings.PortExteriorCondition != "unobserved" || got.Findings.SurfaceFinishCondition != "unobserved" ||
				got.ResponsesCompleted != wantCount || got.Status != test.status {
				t.Fatalf("result=%+v err=%v", got, err)
			}
		})
	}
	for _, responses := range [][]ConditionResponseV2{
		{{Text: "N", Complete: false}, {Text: "N", Complete: true}},
		{{Complete: true}, {Complete: true}, {Complete: true}, {Complete: true}, {Complete: true}},
	} {
		if _, err := EvaluateV2(responses, "", 17); err == nil {
			t.Fatal("invalid response prefix accepted")
		}
	}
	if _, err := EvaluateV2([]ConditionResponseV2{{Text: "D", Complete: true}}, "prompt_error", 17); err == nil {
		t.Fatal("unissued prompt claimed a completed response")
	}
	empty, err := EvaluateV2(nil, "operator_cancelled", 0)
	if err != nil || empty.Status != "aborted" || empty.ResponsesCompleted != 0 {
		t.Fatal("stopped empty prefix gained authority")
	}
}

func TestExteriorConditionV2TimeAndStatusCannotInventPass(t *testing.T) {
	base, err := DecodeV2([]byte(conditionPassJSONV2))
	if err != nil {
		t.Fatal(err)
	}
	for _, milliseconds := range []int64{math.MinInt64, -1, 45000, 45001, math.MaxInt64} {
		value := base
		value.ObservationMilliseconds = milliseconds
		if ValidateV2(value) == nil {
			t.Fatal("late or invalid clean pass admitted")
		}
	}
	for _, milliseconds := range []int64{0, 44999} {
		value := base
		value.ObservationMilliseconds = milliseconds
		if ValidateV2(value) != nil {
			t.Fatal("timely literal pass rejected")
		}
	}
	for _, name := range []string{"unobserved", "early_stop", "partial_count", "issue", "prompt_error"} {
		t.Run(name, func(t *testing.T) {
			value := base
			switch name {
			case "unobserved":
				value.Findings.HingeMountCondition = "unobserved"
			case "early_stop":
				value.StopReason = "deadline"
			case "partial_count":
				value.ResponsesCompleted = 3
			case "issue":
				value.Findings.EnclosureIntegrity = "issue_present"
			case "prompt_error":
				value.StopReason = "prompt_error"
			}
			if ValidateV2(value) == nil {
				t.Fatal("forged pass admitted")
			}
		})
	}
	adverse := base
	adverse.Findings.SurfaceFinishCondition = "issue_present"
	adverse.StopReason = "deadline"
	adverse.Status = "fail"
	adverse.ObservationMilliseconds = 45000
	if ValidateV2(adverse) != nil {
		t.Fatal("late adverse observation disappeared")
	}
}

func TestExteriorConditionV2OnePromptCannotClaimObservations(t *testing.T) {
	if _, err := EvaluateV2([]ConditionResponseV2{{Text: "D", Complete: true}, {}}, "prompt_error", 17); err == nil {
		t.Fatal("single failed prompt claimed earlier observations")
	}
	good, err := EvaluateV2([]ConditionResponseV2{{}}, "prompt_error", 17)
	if err != nil || good.ResponsesCompleted != 0 || good.Status != "error" {
		t.Fatal("valid failed prompt rejected")
	}
	for _, value := range []string{"issue_present", "no_issue_observed"} {
		forged := good
		forged.ResponsesCompleted = 1
		forged.Findings.EnclosureIntegrity = value
		raw, err := json.Marshal(forged)
		if err != nil {
			t.Fatal(err)
		}
		if ValidateV2(forged) == nil {
			t.Fatal("typed failed prompt claimed observations")
		}
		if _, err := DecodeV2(raw); err == nil {
			t.Fatal("wire failed prompt claimed observations")
		}
	}
}
