package graphics

import (
	"context"
	"errors"
)

// MarkInterrupted records a command stop without replacing completed failure
// observations. It is used by both the probe and its whole-command caller.
func (result *Result) MarkInterrupted(err error) {
	if err == nil {
		return
	}
	status, detail := interruptedStatus(err), interruptionDetail(err)
	if result.Status == StatusFail || result.Renderer.Status == StatusFail || result.Pattern.Status == StatusFail {
		result.Status = StatusFail
	} else if result.Status != StatusError {
		result.Status = status
	}
	if result.Renderer.Status == "" {
		result.Renderer = RendererResult{Status: status, Detail: detail}
	}
	if result.Pattern.Status == "" {
		result.Pattern = PatternResult{Status: StatusAborted, Detail: "not started: " + detail}
	}
	result.Warnings = appendInterruption(result.Warnings, detail)
}

func (result *VisualResult) MarkInterrupted(err error) {
	if err == nil {
		return
	}
	status := interruptedStatus(err)
	if result.Status == StatusFail {
		status = StatusFail
	} else if result.Status == StatusError {
		status = StatusError
	}
	for _, pattern := range result.Patterns {
		if pattern.Status == StatusFail {
			status = StatusFail
		}
	}
	for _, finding := range result.Findings {
		for _, observation := range finding.Criteria {
			if observation == StatusFail {
				status = StatusFail
			}
		}
	}
	result.Status = status
	result.Warnings = appendInterruption(result.Warnings, interruptionDetail(err))
}

func interruptedStatus(err error) Status {
	if errors.Is(err, context.DeadlineExceeded) {
		return StatusError
	}
	return StatusAborted
}

func interruptionDetail(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "command deadline exceeded"
	}
	return "command canceled"
}

func appendInterruption(warnings []string, detail string) []string {
	for _, warning := range warnings {
		if warning == detail {
			return warnings
		}
	}
	return append(warnings, detail)
}
