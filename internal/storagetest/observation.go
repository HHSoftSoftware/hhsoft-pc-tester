package storagetest

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
)

var ErrObservation = errors.New("storage sample observation is invalid")

// Identity contains public device numbers and geometry, never a content identity.
type Identity struct {
	Major             uint32 `json:"major"`
	Minor             uint32 `json:"minor"`
	CapacityBytes     uint64 `json:"capacity_bytes"`
	LogicalBlockBytes uint32 `json:"logical_block_bytes"`
}

type RangeObservation struct {
	Offset      uint64 `json:"offset"`
	Length      uint32 `json:"length"`
	ReadCount   uint8  `json:"read_count"`
	FirstBytes  uint32 `json:"first_bytes"`
	SecondBytes uint32 `json:"second_bytes"`
	Outcome     string `json:"outcome"`
}

type SampleObservation struct {
	Interface     string             `json:"interface"`
	Identity      Identity           `json:"identity"`
	Status        string             `json:"status"`
	Error         string             `json:"error"`
	Ranges        []RangeObservation `json:"ranges"`
	ReadBytes     uint64             `json:"read_bytes"`
	ComparedBytes uint64             `json:"compared_bytes"`
	MatchedRanges uint8              `json:"matched_ranges"`
}

// ObservationSummary is derived only from structurally valid observations.
// Parent timing, cleanup and final binding still decide whether any credit is
// accepted. A structurally valid observation alone never authorizes a read.
type ObservationSummary struct {
	ReadBytes       uint64
	ComparedBytes   uint64
	MatchedRanges   uint8
	FailureObserved bool
}

// ValidateObservation checks the exact schedule and derives counters from the
// finite ledger. No byte contents, byte values or content hashes are accepted.
func ValidateObservation(target Target, value SampleObservation) (ObservationSummary, error) {
	invalid := ObservationSummary{}
	if !componentID(target.ID, "block:") || !canonicalNumber(target.Number, false) || target.Exclusions == nil || len(target.Exclusions) != 0 || target.Ranges == nil {
		return invalid, ErrObservation
	}
	schedule, err := SampleRanges(target.CapacityBytes, target.LogicalBlockBytes)
	if err != nil || !reflect.DeepEqual(target.Ranges, schedule) {
		return invalid, ErrObservation
	}
	numbers := strings.Split(target.Number, ":")
	major, _ := strconv.ParseUint(numbers[0], 10, 32)
	minor, _ := strconv.ParseUint(numbers[1], 10, 32)
	expected := Identity{uint32(major), uint32(minor), target.CapacityBytes, target.LogicalBlockBytes}
	if value.Interface != strings.TrimPrefix(target.ID, "block:") || value.Identity != expected || value.Ranges == nil || len(value.Ranges) != len(schedule) {
		return invalid, ErrObservation
	}
	var summary ObservationSummary
	matched := []Range{}
	stopped := false
	fault := ""
	interrupted := false
	for index, observed := range value.Ranges {
		planned := schedule[index]
		if observed.Offset != uint64(planned.Offset) || observed.Length != uint32(planned.Length) || observed.ReadCount > 2 || observed.FirstBytes > observed.Length || observed.SecondBytes > observed.Length {
			return invalid, ErrObservation
		}
		if observed.ReadCount == 0 && (observed.FirstBytes != 0 || observed.SecondBytes != 0) || observed.ReadCount == 1 && observed.SecondBytes != 0 {
			return invalid, ErrObservation
		}
		if stopped && observed.Outcome != "not_started" {
			return invalid, ErrObservation
		}
		switch observed.Outcome {
		case "not_started":
			if observed.ReadCount != 0 {
				return invalid, ErrObservation
			}
			stopped = true
		case "matched", "mismatch":
			if observed.ReadCount != 2 || observed.FirstBytes != observed.Length || observed.SecondBytes != observed.Length {
				return invalid, ErrObservation
			}
			if observed.Outcome == "matched" {
				matched = append(matched, planned)
				summary.MatchedRanges++
			} else {
				fault = "mismatch"
				stopped = true
			}
		case "short_read":
			if !(observed.ReadCount == 1 && observed.FirstBytes < observed.Length || observed.ReadCount == 2 && observed.FirstBytes == observed.Length && observed.SecondBytes < observed.Length) {
				return invalid, ErrObservation
			}
			fault = "short_read"
			stopped = true
		case "read_error":
			if observed.ReadCount != 1 && observed.ReadCount != 2 || observed.ReadCount == 2 && observed.FirstBytes != observed.Length {
				return invalid, ErrObservation
			}
			fault = "read_error"
			stopped = true
		case "interrupted":
			// Successful full returns before comparison are not read faults. An
			// incomplete return must keep its short_read/read_error outcome.
			if observed.ReadCount != 1 && observed.ReadCount != 2 || observed.FirstBytes != observed.Length || observed.ReadCount == 2 && observed.SecondBytes != observed.Length {
				return invalid, ErrObservation
			}
			interrupted = true
			stopped = true
		default:
			return invalid, ErrObservation
		}
		summary.ReadBytes += uint64(observed.FirstBytes) + uint64(observed.SecondBytes)
	}
	coverage, err := RangeCoverage(matched)
	if err != nil {
		return invalid, ErrObservation
	}
	summary.ComparedBytes = uint64(coverage)
	summary.FailureObserved = fault != ""
	if summary.ReadBytes > MaximumDeviceReadBytes || value.ReadBytes != summary.ReadBytes || value.ComparedBytes != summary.ComparedBytes || value.MatchedRanges != summary.MatchedRanges {
		return invalid, ErrObservation
	}
	if fault != "" {
		if value.Status != "fail" || value.Error != fault {
			return invalid, ErrObservation
		}
	} else if int(summary.MatchedRanges) == len(schedule) {
		// A stop after an accepted comparison preserves that comparison. Its
		// later lifecycle disposition belongs to the completion/parent record.
		if value.Status != "pass" || value.Error != "none" || interrupted {
			return invalid, ErrObservation
		}
	} else {
		switch value.Error {
		case "cancelled":
			if value.Status != "aborted" {
				return invalid, ErrObservation
			}
		case "open_error", "target_busy":
			if value.Status != "error" || summary.ReadBytes != 0 || interrupted {
				return invalid, ErrObservation
			}
			for _, item := range value.Ranges {
				if item.ReadCount != 0 {
					return invalid, ErrObservation
				}
			}
		case "deadline", "identity_changed", "safety_changed", "discovery_failed", "registry_invalid", "topology_invalid", "metadata_limit", "safety_view_invalid", "swap_active", "fixture_changed", "clock_invalid":
			if value.Status != "error" {
				return invalid, ErrObservation
			}
		default:
			return invalid, ErrObservation
		}
	}
	return summary, nil
}
