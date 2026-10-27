package safemode

import (
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/periodic"
)

// These literal bytes were checked against the monitor before extraction.
// They intentionally cover field order, omitted pointers and sanitized values.
var monitorWireCases = map[string]string{
	"short":    `{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"pass","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":0,"completed_observations":0,"elapsed_ms":999,"first_completed_ms":0,"last_completed_ms":0,"maximum_gap_ms":999,"cleanup_confirmed":true}`,
	"sampled":  `{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"pass","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":1,"completed_observations":1,"elapsed_ms":1000,"first_completed_ms":1000,"last_completed_ms":1000,"maximum_gap_ms":1000,"cleanup_confirmed":true,"last_observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}}`,
	"unsafe":   `{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"fail","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":1,"completed_observations":1,"elapsed_ms":1000,"first_completed_ms":1000,"last_completed_ms":1000,"maximum_gap_ms":1000,"cleanup_confirmed":true,"first_failure":{"reason":"unsafe_observation","elapsed_ms":1000,"observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"fail","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":1}},"last_observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"fail","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":1}}`,
	"invalid":  `{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"error","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":1,"completed_observations":1,"elapsed_ms":1000,"first_completed_ms":1000,"last_completed_ms":1000,"maximum_gap_ms":1000,"cleanup_confirmed":true,"first_failure":{"reason":"invalid_observation","elapsed_ms":1000,"observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"error","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":1,"storage_detail":"block mount safety could not be established"}},"last_observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"error","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":1,"storage_detail":"block mount safety could not be established"}}`,
	"late":     `{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"error","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":1,"completed_observations":1,"elapsed_ms":2000,"first_completed_ms":2000,"last_completed_ms":2000,"maximum_gap_ms":2000,"cleanup_confirmed":true,"first_failure":{"reason":"observation_timeout","elapsed_ms":2000},"last_observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}}`,
	"unjoined": `{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"error","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":1,"completed_observations":0,"elapsed_ms":1000,"first_completed_ms":0,"last_completed_ms":0,"maximum_gap_ms":1000,"cleanup_confirmed":false,"first_failure":{"reason":"cleanup_unconfirmed","elapsed_ms":1000}}`,
}

func checkMonitorWire(t *testing.T, name string, result MonitorResult) {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil || string(raw) != monitorWireCases[name] {
		t.Fatalf("%s wire changed: %s; error=%v", name, raw, err)
	}
}

func TestMonitorOriginalWireCompatibility(t *testing.T) {
	for _, name := range []string{"short", "sampled", "unsafe", "invalid", "late", "unjoined"} {
		t.Run(name, func(t *testing.T) {
			summary := periodic.Summary[Result]{IntervalMilliseconds: 1000, ObservationTimeoutMilliseconds: 1000,
				MaximumAllowedGapMilliseconds: 2500, AttemptedObservations: 1, CompletedObservations: 1,
				ElapsedMilliseconds: 1000, FirstCompletedMilliseconds: 1000, LastCompletedMilliseconds: 1000,
				MaximumGapMilliseconds: 1000, CleanupConfirmed: true}
			value := monitorSafeResult()
			summary.LastObservation = &value
			switch name {
			case "short":
				summary.AttemptedObservations, summary.CompletedObservations = 0, 0
				summary.ElapsedMilliseconds, summary.MaximumGapMilliseconds = 999, 999
				summary.FirstCompletedMilliseconds, summary.LastCompletedMilliseconds = 0, 0
				summary.LastObservation = nil
			case "unsafe", "invalid":
				value.Status, value.ActiveNonLoopInterfaces = "fail", 1
				reason := "unsafe_observation"
				if name == "invalid" {
					value.Status = "error"
					value.StorageDetail = "block mount safety could not be established"
					reason = "invalid_observation"
				}
				summary.FirstFailure = &periodic.Failure[Result]{Reason: reason, ElapsedMilliseconds: 1000, Observation: &value}
			case "late":
				summary.ElapsedMilliseconds, summary.FirstCompletedMilliseconds, summary.LastCompletedMilliseconds, summary.MaximumGapMilliseconds = 2000, 2000, 2000, 2000
				summary.FirstFailure = &periodic.Failure[Result]{Reason: "observation_timeout", ElapsedMilliseconds: 2000}
			case "unjoined":
				summary.CompletedObservations = 0
				summary.FirstCompletedMilliseconds, summary.LastCompletedMilliseconds = 0, 0
				summary.CleanupConfirmed = false
				summary.LastObservation = nil
				summary.FirstFailure = &periodic.Failure[Result]{Reason: "cleanup_unconfirmed", ElapsedMilliseconds: 1000}
			}
			result := monitorResult(summary)
			checkMonitorWire(t, name, result)
			if result.FirstFailure != nil && result.FirstFailure.Observation != nil {
				result.FirstFailure.Observation.Status = "caller mutation"
				if result.LastObservation.Status != value.Status || value.Status == "caller mutation" {
					t.Fatal("wire snapshots share caller storage")
				}
			}
		})
	}
}
