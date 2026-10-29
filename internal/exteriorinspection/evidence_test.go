package exteriorinspection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// These finite tuples are literal expectations, independent of Evaluate.
var inspectionTuples = []struct {
	finding  string
	complete bool
	reason   string
	status   string
}{
	{"no_defect_observed", true, "completed", "pass"},
	{"defect_observed", true, "completed", "fail"},
	{"not_inspected", true, "not_inspected", "inconclusive"},
	{"not_inspected", true, "empty_response", "inconclusive"},
	{"not_inspected", true, "invalid_response", "inconclusive"},
	{"not_inspected", false, "input_ended", "inconclusive"},
	{"not_inspected", false, "operator_cancelled", "aborted"},
	{"not_inspected", true, "operator_cancelled", "aborted"},
	{"defect_observed", true, "operator_cancelled", "fail"},
	{"not_inspected", false, "deadline", "aborted"},
	{"not_inspected", true, "deadline", "aborted"},
	{"defect_observed", true, "deadline", "fail"},
	{"not_inspected", false, "input_error", "error"},
	{"not_inspected", true, "input_error", "error"},
	{"defect_observed", true, "input_error", "error"},
	{"not_inspected", false, "prompt_error", "error"},
}

const inspectionPassJSON = `{"schema":"hhsoft.pc-tester.exterior-inspection/v1","source":"operator_reported","scope":"whole_device","criterion":"visible_exterior_enclosure_damage","finding":"no_defect_observed","response_complete":true,"stop_reason":"completed","status":"pass","observation_milliseconds":17}`

func inspectionLiteral(finding string, complete bool, reason, status string, milliseconds int64) Result {
	return Result{
		Schema: "hhsoft.pc-tester.exterior-inspection/v1", Source: "operator_reported",
		Scope: "whole_device", Criterion: "visible_exterior_enclosure_damage",
		Finding: finding, ResponseComplete: complete, StopReason: reason, Status: status,
		ObservationMilliseconds: milliseconds,
	}
}

func TestInspectionExactFiniteEvidenceTuples(t *testing.T) {
	for index, tuple := range inspectionTuples {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			want := inspectionLiteral(tuple.finding, tuple.complete, tuple.reason, tuple.status, 17)
			encoded, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Decode(encoded)
			if err != nil || got != want || Validate(want) != nil {
				t.Fatalf("literal tuple rejected: got=%+v err=%v", got, err)
			}
		})
	}
}

func TestInspectionRejectsEveryOtherFiniteTuple(t *testing.T) {
	for _, finding := range []string{"no_defect_observed", "defect_observed", "not_inspected"} {
		for _, complete := range []bool{false, true} {
			for _, reason := range []string{"completed", "not_inspected", "empty_response", "input_ended", "invalid_response", "operator_cancelled", "deadline", "input_error", "prompt_error"} {
				for _, status := range []string{"pass", "fail", "inconclusive", "aborted", "error"} {
					allowed := false
					for _, tuple := range inspectionTuples {
						if tuple.finding == finding && tuple.complete == complete && tuple.reason == reason && tuple.status == status {
							allowed = true
						}
					}
					value := inspectionLiteral(finding, complete, reason, status, 17)
					encoded, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					got, decodeErr := Decode(encoded)
					if allowed {
						if Validate(value) != nil || decodeErr != nil || got != value {
							t.Fatalf("admissible literal rejected: %+v", value)
						}
					} else if !errors.Is(Validate(value), ErrEvidence) || !errors.Is(decodeErr, ErrEvidence) || got != (Result{}) {
						t.Fatalf("impossible tuple admitted: %+v", value)
					}
				}
			}
		}
	}
}

func TestInspectionExactJSONShape(t *testing.T) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(inspectionPassJSON), &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"schema", "source", "scope", "criterion", "finding", "response_complete", "stop_reason", "status", "observation_milliseconds"} {
		t.Run(name, func(t *testing.T) {
			for _, variant := range []string{"missing", "null", "case", "duplicate", "alias", "wrong_type"} {
				copyFields := make(map[string]json.RawMessage, len(fields))
				for key, value := range fields {
					copyFields[key] = bytes.Clone(value)
				}
				switch variant {
				case "missing":
					delete(copyFields, name)
				case "null":
					copyFields[name] = json.RawMessage("null")
				case "case":
					copyFields[strings.ToUpper(name)] = copyFields[name]
					delete(copyFields, name)
				case "alias":
					copyFields[strings.ToUpper(name)] = copyFields[name]
				case "wrong_type":
					if name == "response_complete" || name == "observation_milliseconds" {
						copyFields[name] = json.RawMessage(`"17"`)
					} else {
						copyFields[name] = json.RawMessage("false")
					}
				}
				raw, err := json.Marshal(copyFields)
				if err != nil {
					t.Fatal(err)
				}
				if variant == "duplicate" {
					raw = append(raw[:len(raw)-1], []byte(fmt.Sprintf(",%q:%s}", name, fields[name]))...)
				}
				if value, err := Decode(raw); !errors.Is(err, ErrEvidence) || value != (Result{}) {
					t.Fatalf("%s accepted for %s", variant, name)
				}
			}
		})
	}
}

func TestInspectionRejectsUntrustedEncodingsAndValues(t *testing.T) {
	for _, raw := range [][]byte{
		nil, []byte("null"), []byte("[]"), []byte(inspectionPassJSON + "{}"),
		[]byte(strings.Replace(inspectionPassJSON, `"schema":`, `"schema":"hhsoft.pc-tester.exterior-inspection/v2","schema":`, 1)),
		[]byte(strings.Replace(inspectionPassJSON, `"status":"pass"`, `"detail":"raw private response","status":"pass"`, 1)),
		[]byte(strings.Replace(inspectionPassJSON, "observation_milliseconds", "duration_milliseconds", 1)),
		[]byte(strings.Replace(inspectionPassJSON, ":17}", ":17.0}", 1)),
		[]byte(strings.Replace(inspectionPassJSON, ":17}", ":1e1}", 1)),
		[]byte(strings.Replace(inspectionPassJSON, ":17}", ":-1}", 1)),
		[]byte(strings.Replace(inspectionPassJSON, ":17}", ":45001}", 1)),
		[]byte(strings.Replace(inspectionPassJSON, ":17}", ":9223372036854775808}", 1)),
		append([]byte(inspectionPassJSON), 0xff),
		append([]byte(inspectionPassJSON[:len(inspectionPassJSON)-1]), []byte(`,"\u0073chema":"hhsoft.pc-tester.exterior-inspection/v1"}`)...),
	} {
		if value, err := Decode(raw); !errors.Is(err, ErrEvidence) || value != (Result{}) {
			t.Fatalf("untrusted encoding admitted: %q", raw)
		}
	}
	base, err := Decode([]byte(inspectionPassJSON))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"schema", "source", "scope", "criterion", "finding", "status", "reason"} {
		t.Run(name, func(t *testing.T) {
			value := base
			switch name {
			case "schema":
				value.Schema = "hhsoft.pc-tester.exterior-inspection/v2"
			case "source":
				value.Source = "sensor"
			case "scope":
				value.Scope = "component"
			case "criterion":
				value.Criterion = "overall_health"
			case "finding":
				value.Finding = "safe"
			case "status":
				value.Status = "skip"
			case "reason":
				value.StopReason = "cleanup_confirmed"
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := Decode(raw); !errors.Is(err, ErrEvidence) || got != (Result{}) {
				t.Fatalf("unsupported %s accepted", name)
			}
		})
	}
}

func TestInspectionEncodedAndObservationBounds(t *testing.T) {
	for _, milliseconds := range []int64{0, 45000} {
		raw := []byte(strings.Replace(inspectionPassJSON, ":17}", fmt.Sprintf(":%d}", milliseconds), 1))
		got, err := Decode(raw)
		if err != nil || got.ObservationMilliseconds != milliseconds {
			t.Fatalf("observation boundary %d rejected", milliseconds)
		}
	}
	atBound := append([]byte(inspectionPassJSON), bytes.Repeat([]byte(" "), 4096-len(inspectionPassJSON))...)
	if _, err := Decode(atBound); err != nil {
		t.Fatal("exact encoded bound rejected")
	}
	if got, err := Decode(append(atBound, ' ')); !errors.Is(err, ErrEvidence) || got != (Result{}) {
		t.Fatal("oversized encoding admitted")
	}
	invalidInside := bytes.ReplaceAll([]byte(inspectionPassJSON), []byte("operator_reported"), []byte{'o', 0xff})
	if _, err := Decode(invalidInside); !errors.Is(err, ErrEvidence) {
		t.Fatal("invalid UTF-8 inside a string admitted")
	}
}

func TestInspectionReducerLiteralOutcomes(t *testing.T) {
	cases := []struct {
		name, response                string
		complete                      bool
		stop, finding, reason, status string
		wantComplete                  bool
	}{
		{"no_defect", " N\t", true, "", "no_defect_observed", "completed", "pass", true},
		{"defect", "\td ", true, "", "defect_observed", "completed", "fail", true},
		{"uncertain", "i", true, "", "not_inspected", "not_inspected", "inconclusive", true},
		{"empty", "", true, "", "not_inspected", "empty_response", "inconclusive", true},
		{"whitespace", " \t", true, "", "not_inspected", "empty_response", "inconclusive", true},
		{"invalid", "NN", true, "", "not_inspected", "invalid_response", "inconclusive", true},
		{"unicode_space", "\u00a0N", true, "", "not_inspected", "invalid_response", "inconclusive", true},
		{"partial_defect", "D", false, "", "not_inspected", "input_ended", "inconclusive", false},
		{"partial_cancel", "D", false, "operator_cancelled", "not_inspected", "operator_cancelled", "aborted", false},
		{"complete_cancel", "N", true, "operator_cancelled", "not_inspected", "operator_cancelled", "aborted", true},
		{"defect_cancel", "D", true, "operator_cancelled", "defect_observed", "operator_cancelled", "fail", true},
		{"partial_deadline", "N", false, "deadline", "not_inspected", "deadline", "aborted", false},
		{"uncertain_deadline", "I", true, "deadline", "not_inspected", "deadline", "aborted", true},
		{"defect_deadline", "D", true, "deadline", "defect_observed", "deadline", "fail", true},
		{"input_fault", "N", true, "input_error", "not_inspected", "input_error", "error", true},
		{"partial_fault", "D", false, "input_error", "not_inspected", "input_error", "error", false},
		{"defect_fault", "D", true, "input_error", "defect_observed", "input_error", "error", true},
		{"prompt_fault", "D", true, "prompt_error", "not_inspected", "prompt_error", "error", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := Evaluate(test.response, test.complete, test.stop, 17)
			want := inspectionLiteral(test.finding, test.wantComplete, test.reason, test.status, 17)
			if err != nil || got != want {
				t.Fatalf("got=%+v err=%v want=%+v", got, err, want)
			}
		})
	}
	for _, invalidStop := range []string{"completed", "input_ended", "hard private detail", "skip"} {
		if got, err := Evaluate("N", true, invalidStop, 0); !errors.Is(err, ErrEvidence) || got != (Result{}) {
			t.Fatal("invalid stop supplied to reducer accepted")
		}
	}
	for _, milliseconds := range []int64{-1, 45001} {
		if got, err := Evaluate("N", true, "", milliseconds); !errors.Is(err, ErrEvidence) || got != (Result{}) {
			t.Fatal("invalid observation supplied to reducer accepted")
		}
	}
}
