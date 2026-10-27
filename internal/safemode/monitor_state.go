package safemode

import "github.com/HHSoftSoftware/hhsoft-pc-tester/internal/periodic"

func strictMonitorSafety(result Result) bool {
	return result.Safe() && result.StorageDetail == "" && result.NetworkDetail == ""
}

// Never retain arbitrary observer strings, paths, or panic values in evidence.
func monitorObservation(result Result) Result {
	if result.Schema != Schema {
		result.Status = "error"
	}
	result.Schema = Schema
	switch result.Status {
	case "pass", "fail", "error", "aborted":
	default:
		result.Status = "error"
	}
	if result.StorageDetail != "" {
		result.StorageDetail = "block mount safety could not be established"
	}
	if result.NetworkDetail != "" {
		result.NetworkDetail = "offline network state could not be established"
	}
	return result
}

func assessMonitorObservation(raw Result) periodic.Assessment[Result] {
	assessment := periodic.Assessment[Result]{Value: monitorObservation(raw)}
	// Classify the raw result. Sanitizing an unknown schema first must never
	// turn an invalid observation into an ordinary unsafe observation.
	if !strictMonitorSafety(raw) {
		assessment.FailureReason = "invalid_observation"
		if raw.Schema == Schema && raw.Status == "fail" &&
			(raw.WritableBlockMounts > 0 || raw.ActiveNonLoopInterfaces > 0) {
			assessment.FailureReason = "unsafe_observation"
		}
	}
	return assessment
}

func monitorResult(summary periodic.Summary[Result]) MonitorResult {
	result := MonitorResult{
		Schema: MonitorSchema, Status: "pass",
		IntervalMilliseconds:           summary.IntervalMilliseconds,
		ObservationTimeoutMilliseconds: summary.ObservationTimeoutMilliseconds,
		MaximumAllowedGapMilliseconds:  summary.MaximumAllowedGapMilliseconds,
		AttemptedObservations:          summary.AttemptedObservations,
		CompletedObservations:          summary.CompletedObservations,
		ElapsedMilliseconds:            summary.ElapsedMilliseconds,
		FirstCompletedMilliseconds:     summary.FirstCompletedMilliseconds,
		LastCompletedMilliseconds:      summary.LastCompletedMilliseconds,
		MaximumGapMilliseconds:         summary.MaximumGapMilliseconds,
		CleanupConfirmed:               summary.CleanupConfirmed,
	}
	if summary.FirstFailure != nil {
		failure := summary.FirstFailure
		result.FirstFailure = &MonitorFailure{Reason: failure.Reason, ElapsedMilliseconds: failure.ElapsedMilliseconds}
		if failure.Observation != nil {
			observation := *failure.Observation
			result.FirstFailure.Observation = &observation
		}
		result.Status = "error"
		if failure.Reason == "unsafe_observation" {
			result.Status = "fail"
		}
	}
	if summary.LastObservation != nil {
		observation := *summary.LastObservation
		result.LastObservation = &observation
	}
	return result
}
