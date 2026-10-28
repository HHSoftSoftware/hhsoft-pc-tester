package storageevidence

import (
	"encoding/json"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/workerexec"
)

func errorCode(value string) bool {
	switch value {
	case "none", "unsupported_platform", "discovery_failed", "registry_invalid", "topology_invalid", "metadata_limit", "safety_view_invalid", "swap_active", "missing_component", "fixture_changed", "identity_changed", "safety_changed", "target_busy", "open_error", "read_error", "short_read", "mismatch", "close_error", "protocol_error", "spawn_error", "output_error", "clock_invalid", "deadline", "cancelled", "cleanup_unconfirmed":
		return true
	}
	return false
}
func hardwareFailure(value string) bool {
	return value == "read_error" || value == "short_read" || value == "mismatch"
}
func fixtureReference(value string) bool {
	if len(value) > 128 || !(strings.HasPrefix(value, "block:") || strings.HasPrefix(value, "usb:")) {
		return false
	}
	name := value[strings.IndexByte(value, ':')+1:]
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._:-", c)) {
			return false
		}
	}
	return true
}

// ValidateResult recomputes metadata admission, finite observations and accepted
// totals. Precise monotonic timing and kernel ownership remain signed producer
// assertions; these integer timing checks establish only feasible disclosure.
func ValidateResult(result storagetest.Result) error {
	if result.Schema != storagetest.Schema || result.Policy != storagetest.Policy || result.CacheBypass || result.MediaWrites || !errorCode(result.Error) || result.LimitMilliseconds < 1 || result.LimitMilliseconds > 180000 || result.BudgetMilliseconds < 0 || result.BudgetMilliseconds > result.LimitMilliseconds || result.DurationMilliseconds < 0 || result.DurationMilliseconds > 3600000 || result.Devices == nil || len(result.Devices) > storagetest.MaximumDevices {
		return ErrInvalid
	}
	if !sortedIDs(result.ComponentIDs, storagetest.MaximumDevices, blockID) || !sortedIDs(result.FixtureIDs, storagetest.MaximumFixtures, fixtureReference) || !sortedIDs(result.MissingComponents, storagetest.MaximumDevices, blockID) {
		return ErrInvalid
	}
	switch result.Scope {
	case "planned":
		if !digest(result.PlanID, true) {
			return ErrInvalid
		}
	case "standalone":
		if result.PlanID != "" || len(result.ComponentIDs) != 0 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	derived, err := projection(result)
	if err != nil || len(derived) != len(result.Devices) {
		return ErrInvalid
	}
	if result.Error == "unsupported_platform" && (result.DiscoveryAccepted || result.Snapshot != nil) || result.Error == "missing_component" && len(result.MissingComponents) == 0 || result.Error == "close_error" && result.CleanupConfirmed {
		return ErrInvalid
	}
	var reads, compared uint64
	var matched uint32
	failure := ""
	diagnostic := false
	requested := 0
	allPassed := true
	stopped := false
	terminalFault := false
	var finished int64
	for i, device := range result.Devices {
		expected := derived[i]
		if device.Interface != strings.TrimPrefix(expected.target.ID, "block:") || device.Requested != expected.requested || device.Exclusion != expected.exclusion || !errorCode(device.Error) || device.StartedMilliseconds < 0 || device.FinishedMilliseconds < device.StartedMilliseconds || device.FinishedMilliseconds > result.DurationMilliseconds {
			return ErrInvalid
		}
		if expected.requested {
			requested++
			if !device.Accepted || device.Status != "pass" {
				allPassed = false
			}
		}
		if expected.exclusion != "none" {
			if device.Eligibility != "excluded" || device.Status != "excluded" || device.Error != "none" || device.Accepted || device.StartedMilliseconds != 0 || device.FinishedMilliseconds != 0 || !device.CloseConfirmed || device.FinalBindingConfirmed || device.Observation != nil {
				return ErrInvalid
			}
			continue
		}
		if device.Eligibility != "eligible" || !expected.requested {
			return ErrInvalid
		}
		if device.Status == "not_started" {
			if device.Error != "none" || device.Accepted || device.StartedMilliseconds != 0 || device.FinishedMilliseconds != 0 || !device.CloseConfirmed || device.FinalBindingConfirmed || device.Observation != nil {
				return ErrInvalid
			}
			stopped = true
			terminalFault = true
			continue
		}
		if stopped || !result.DiscoveryAccepted || len(result.MissingComponents) != 0 || device.StartedMilliseconds < finished || device.StartedMilliseconds > result.BudgetMilliseconds {
			return ErrInvalid
		}
		finished = device.FinishedMilliseconds
		if !device.CloseConfirmed && result.CleanupConfirmed {
			return ErrInvalid
		}
		if device.Observation == nil {
			if device.Accepted || device.Status != "error" && device.Status != "aborted" || device.Error == "none" || hardwareFailure(device.Error) || (device.Status == "aborted") != (device.Error == "cancelled") {
				return ErrInvalid
			}
			stopped = true
			terminalFault = true
			continue
		}
		observationRaw, err := json.Marshal(device.Observation)
		if err != nil {
			return ErrInvalid
		}
		_, summary, err := DecodeObservation(expected.target, observationRaw)
		if err != nil {
			return ErrInvalid
		}
		if device.Observation.Status == "fail" {
			if device.Status != "fail" || device.Error != device.Observation.Error {
				return ErrInvalid
			}
		} else if device.Observation.Status != "pass" || device.Accepted {
			if device.Status != device.Observation.Status || device.Error != device.Observation.Error {
				return ErrInvalid
			}
		} else if device.Status != "error" && device.Status != "aborted" || device.Error == "none" || hardwareFailure(device.Error) || (device.Status == "aborted") != (device.Error == "cancelled") {
			return ErrInvalid
		}
		if device.Accepted {
			if !device.CloseConfirmed || !device.FinalBindingConfirmed || device.FinishedMilliseconds > result.BudgetMilliseconds || device.FinishedMilliseconds-device.StartedMilliseconds > 10000 || device.Status != "pass" && device.Status != "fail" {
				return ErrInvalid
			}
			reads += summary.ReadBytes
			compared += summary.ComparedBytes
			matched += uint32(summary.MatchedRanges)
			if summary.FailureObserved && failure == "" {
				failure = device.Observation.Error
			}
		} else {
			diagnostic = diagnostic || summary.FailureObserved
			stopped = true
			terminalFault = true
		}
	}
	if terminalFault && result.StopReason == "completed" || reads > storagetest.MaximumTotalReadBytes || result.AcceptedReadBytes != reads || result.AcceptedComparedBytes != compared || result.AcceptedMatchedRanges != matched || result.FailureObserved != (failure != "") || result.DiagnosticFailure != diagnostic {
		return ErrInvalid
	}
	if !result.DiscoveryAccepted && (reads != 0 || compared != 0 || matched != 0) {
		return ErrInvalid
	}
	if len(result.MissingComponents) > 0 && (result.Error != "missing_component" && result.Error != "cleanup_unconfirmed" || result.StopReason != "error" && result.StopReason != "cleanup_unconfirmed") {
		return ErrInvalid
	}
	if !result.CleanupConfirmed {
		if result.Status != "error" || result.StopReason != "cleanup_unconfirmed" || result.Error != "cleanup_unconfirmed" {
			return ErrInvalid
		}
	} else if result.StopReason == "cleanup_unconfirmed" || result.Error == "cleanup_unconfirmed" {
		return ErrInvalid
	} else if failure != "" {
		if result.Status != "fail" || result.Error != failure || result.StopReason != "completed" && result.StopReason != "operator_cancelled" && result.StopReason != "deadline" && result.StopReason != "error" {
			return ErrInvalid
		}
	} else {
		switch result.StopReason {
		case "operator_cancelled":
			if result.Status != "aborted" || result.Error != "cancelled" {
				return ErrInvalid
			}
		case "deadline":
			if result.Status != "error" || result.Error != "deadline" {
				return ErrInvalid
			}
		case "error":
			if result.Status != "error" || result.Error == "none" || result.Error == "cancelled" || result.Error == "deadline" || hardwareFailure(result.Error) {
				return ErrInvalid
			}
		case "completed":
			if result.Error != "none" || !result.DiscoveryAccepted || diagnostic || terminalFault {
				return ErrInvalid
			}
			want := "inconclusive"
			if requested > 0 && allPassed {
				want = "pass"
			}
			if result.Status != want {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	if result.Status == "pass" && result.DurationMilliseconds > result.BudgetMilliseconds {
		return ErrInvalid
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw)+1 > storagetest.MaximumResultBytes {
		return ErrInvalid
	}
	if !result.CleanupConfirmed {
		return workerexec.ErrCleanupUncertain
	}
	return nil
}
