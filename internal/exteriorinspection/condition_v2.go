package exteriorinspection

import (
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
)

const SchemaV2 = "hhsoft.pc-tester.exterior-inspection/v2"

const (
	UnobservedV2      = "unobserved"
	NoIssueObservedV2 = "no_issue_observed"
	IssuePresentV2    = "issue_present"
	NotApplicableV2   = "not_applicable"
)

// ConditionFindingsV2 has four mandatory fields. N/A is an operator assertion
// permitted only for the hinge criterion, never an inferred hardware property.
type ConditionFindingsV2 struct {
	EnclosureIntegrity     string `json:"enclosure_integrity"`
	HingeMountCondition    string `json:"hinge_mount_condition"`
	PortExteriorCondition  string `json:"port_exterior_condition"`
	SurfaceFinishCondition string `json:"surface_finish_condition"`
}

type ResultV2 struct {
	Schema                  string              `json:"schema"`
	Source                  string              `json:"source"`
	Scope                   string              `json:"scope"`
	Findings                ConditionFindingsV2 `json:"findings"`
	ResponsesCompleted      int                 `json:"responses_completed"`
	StopReason              string              `json:"stop_reason"`
	Status                  string              `json:"status"`
	ObservationMilliseconds int64               `json:"observation_milliseconds"`
}

// ConditionResponseV2 is transient command input. It is never placed in evidence.
type ConditionResponseV2 struct {
	Text     string
	Complete bool
}

func (value ConditionFindingsV2) values() [4]string {
	return [4]string{value.EnclosureIntegrity, value.HingeMountCondition, value.PortExteriorCondition, value.SurfaceFinishCondition}
}

func conditionFindingsV2(values [4]string) ConditionFindingsV2 {
	return ConditionFindingsV2{
		EnclosureIntegrity: values[0], HingeMountCondition: values[1],
		PortExteriorCondition: values[2], SurfaceFinishCondition: values[3],
	}
}

func DecodeV2(encoded []byte) (ResultV2, error) {
	var result ResultV2
	if len(encoded) == 0 || len(encoded) > MaxBytes || !utf8.Valid(encoded) ||
		evidencejson.ValidateShape(encoded, reflect.TypeOf(result)) != nil ||
		json.Unmarshal(encoded, &result) != nil || ValidateV2(result) != nil {
		return ResultV2{}, ErrEvidence
	}
	return result, nil
}

func ValidateV2(result ResultV2) error {
	if result.Schema != SchemaV2 || result.Source != "operator_reported" ||
		result.Scope != "visible_exterior_condition" ||
		result.ObservationMilliseconds < 0 || result.ObservationMilliseconds > MaxObservationMilliseconds ||
		result.ResponsesCompleted < 0 || result.ResponsesCompleted > 4 {
		return ErrEvidence
	}
	issue, completeNoIssue := false, true
	values := result.Findings.values()
	for index, value := range values {
		switch value {
		case UnobservedV2:
			completeNoIssue = false
		case NoIssueObservedV2:
		case IssuePresentV2:
			issue, completeNoIssue = true, false
		case NotApplicableV2:
			if index != 1 {
				return ErrEvidence
			}
		default:
			return ErrEvidence
		}
		if index >= result.ResponsesCompleted && value != UnobservedV2 {
			return ErrEvidence
		}
	}
	if result.StopReason != "completed" && result.ResponsesCompleted == 4 &&
		values[3] != UnobservedV2 && values[3] != IssuePresentV2 {
		return ErrEvidence
	}
	want := "inconclusive"
	switch result.StopReason {
	case "completed":
		if result.ResponsesCompleted != 4 {
			return ErrEvidence
		}
		if completeNoIssue {
			if result.ObservationMilliseconds >= MaxObservationMilliseconds {
				return ErrEvidence
			}
			want = "pass"
		}
		if issue {
			want = "fail"
		}
	case "input_ended":
		if result.ResponsesCompleted == 4 {
			return ErrEvidence
		}
		if issue {
			want = "fail"
		}
	case "operator_cancelled", "deadline":
		want = "aborted"
		if issue {
			want = "fail"
		}
	case "prompt_error":
		if result.ResponsesCompleted != 0 {
			return ErrEvidence
		}
		want = "error"
	case "input_error":
		want = "error"
	default:
		return ErrEvidence
	}
	if result.Status != want {
		return ErrEvidence
	}
	return nil
}

// EvaluateV2 consumes a sequential response prefix and a stop classified by the
// owner. A stop applies to the final attempted response, not earlier decisions.
// A completed issue remains adverse; a stopped clean response gains no authority.
func EvaluateV2(responses []ConditionResponseV2, stopReason string, observationMilliseconds int64) (ResultV2, error) {
	result := ResultV2{
		Schema: SchemaV2, Source: "operator_reported", Scope: "visible_exterior_condition",
		StopReason: stopReason, Status: "inconclusive", ObservationMilliseconds: observationMilliseconds,
	}
	if len(responses) > 4 {
		return ResultV2{}, ErrEvidence
	}
	switch stopReason {
	case "", "operator_cancelled", "deadline", "input_error", "prompt_error":
	default:
		return ResultV2{}, ErrEvidence
	}
	if stopReason == "prompt_error" && (len(responses) > 1 || len(responses) == 1 && responses[0].Complete) {
		return ResultV2{}, ErrEvidence
	}
	values := [4]string{UnobservedV2, UnobservedV2, UnobservedV2, UnobservedV2}
	issue, completeNoIssue := false, true
	for index, response := range responses {
		if !response.Complete {
			if index != len(responses)-1 {
				return ResultV2{}, ErrEvidence
			}
			continue
		}
		result.ResponsesCompleted++
		value := strings.Trim(response.Text, " \t")
		switch {
		case value == "D" || value == "d":
			values[index] = IssuePresentV2
		case stopReason != "" && index == len(responses)-1:
			// A complete but stopped N/A or no-issue answer is not credited.
		case value == "N" || value == "n":
			values[index] = NoIssueObservedV2
		case index == 1 && (value == "A" || value == "a"):
			values[index] = NotApplicableV2
		}
	}
	result.Findings = conditionFindingsV2(values)
	for _, value := range values {
		issue = issue || value == IssuePresentV2
		completeNoIssue = completeNoIssue && value != UnobservedV2 && value != IssuePresentV2
	}
	switch stopReason {
	case "":
		if result.ResponsesCompleted == 4 {
			result.StopReason = "completed"
			if completeNoIssue {
				result.Status = "pass"
			}
		} else {
			result.StopReason = "input_ended"
		}
		if issue {
			result.Status = "fail"
		}
	case "operator_cancelled", "deadline":
		result.Status = "aborted"
		if issue {
			result.Status = "fail"
		}
	case "input_error", "prompt_error":
		result.Status = "error"
	}
	if ValidateV2(result) != nil {
		return ResultV2{}, ErrEvidence
	}
	return result, nil
}
