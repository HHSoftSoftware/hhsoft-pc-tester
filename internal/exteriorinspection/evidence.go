// Package exteriorinspection validates a bounded operator assertion about
// safely visible exterior enclosure surfaces. It does not verify an inspection.
package exteriorinspection

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
)

const (
	Schema                           = "hhsoft.pc-tester.exterior-inspection/v1"
	TestID                           = "inspection.exterior.quick"
	MaxBytes                         = 4096
	MaxObservationMilliseconds int64 = 45000
)

var ErrEvidence = errors.New("invalid exterior inspection evidence")

type Result struct {
	Schema                  string `json:"schema"`
	Source                  string `json:"source"`
	Scope                   string `json:"scope"`
	Criterion               string `json:"criterion"`
	Finding                 string `json:"finding"`
	ResponseComplete        bool   `json:"response_complete"`
	StopReason              string `json:"stop_reason"`
	Status                  string `json:"status"`
	ObservationMilliseconds int64  `json:"observation_milliseconds"`
}

func Decode(encoded []byte) (Result, error) {
	var result Result
	if len(encoded) == 0 || len(encoded) > MaxBytes || !utf8.Valid(encoded) ||
		evidencejson.ValidateShape(encoded, reflect.TypeOf(result)) != nil ||
		json.Unmarshal(encoded, &result) != nil || Validate(result) != nil {
		return Result{}, ErrEvidence
	}
	return result, nil
}

func Validate(result Result) error {
	if result.Schema != Schema || result.Source != "operator_reported" ||
		result.Scope != "whole_device" || result.Criterion != "visible_exterior_enclosure_damage" ||
		result.ObservationMilliseconds < 0 || result.ObservationMilliseconds > MaxObservationMilliseconds {
		return ErrEvidence
	}
	switch result.StopReason {
	case "completed":
		if result.ResponseComplete &&
			(result.Finding == "no_defect_observed" && result.Status == "pass" ||
				result.Finding == "defect_observed" && result.Status == "fail") {
			return nil
		}
	case "not_inspected", "empty_response", "invalid_response":
		if result.ResponseComplete && result.Finding == "not_inspected" && result.Status == "inconclusive" {
			return nil
		}
	case "input_ended":
		if !result.ResponseComplete && result.Finding == "not_inspected" && result.Status == "inconclusive" {
			return nil
		}
	case "operator_cancelled", "deadline":
		if result.Finding == "not_inspected" && result.Status == "aborted" ||
			result.ResponseComplete && result.Finding == "defect_observed" && result.Status == "fail" {
			return nil
		}
	case "input_error":
		if result.Status == "error" && (result.Finding == "not_inspected" ||
			result.ResponseComplete && result.Finding == "defect_observed") {
			return nil
		}
	case "prompt_error":
		if !result.ResponseComplete && result.Finding == "not_inspected" && result.Status == "error" {
			return nil
		}
	}
	return ErrEvidence
}

// Evaluate reduces an observed response and an already classified stop. The
// caller must preserve hard faults before supplying a clean cancellation or
// deadline. Only a completed response may establish an observed defect.
func Evaluate(response string, complete bool, stopReason string, observationMilliseconds int64) (Result, error) {
	result := Result{Schema: Schema, Source: "operator_reported", Scope: "whole_device",
		Criterion: "visible_exterior_enclosure_damage", Finding: "not_inspected",
		ResponseComplete: complete, StopReason: stopReason, Status: "inconclusive",
		ObservationMilliseconds: observationMilliseconds}
	response = strings.Trim(response, " \t")
	defect := complete && (response == "D" || response == "d")
	if defect {
		result.Finding = "defect_observed"
	}
	switch stopReason {
	case "prompt_error":
		result.Finding, result.ResponseComplete, result.Status = "not_inspected", false, "error"
	case "input_error":
		result.Status = "error"
	case "operator_cancelled", "deadline":
		result.Status = "aborted"
		if defect {
			result.Status = "fail"
		}
	case "":
		switch {
		case !complete:
			result.StopReason = "input_ended"
		case defect:
			result.StopReason, result.Status = "completed", "fail"
		case response == "N" || response == "n":
			result.Finding, result.StopReason, result.Status = "no_defect_observed", "completed", "pass"
		case response == "I" || response == "i":
			result.StopReason = "not_inspected"
		case response == "":
			result.StopReason = "empty_response"
		default:
			result.StopReason = "invalid_response"
		}
	default:
		return Result{}, ErrEvidence
	}
	if Validate(result) != nil {
		return Result{}, ErrEvidence
	}
	return result, nil
}
