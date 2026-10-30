package storagesample

import (
	"strconv"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

// prepareSampleObservation validates finite target metadata without performing I/O.
// A fixture mask cannot be overridden by a caller-supplied empty exclusion list.
func prepareSampleObservation(target storagetest.Target) (storagetest.SampleObservation, error) {
	if len(target.Ranges) > 3 || len(target.Number) < 3 || len(target.Number) > 21 || len(target.ID) > 70 || target.FixtureMask != 0 {
		return storagetest.SampleObservation{}, errSampleProtocol
	}
	value := storagetest.SampleObservation{
		Interface: strings.TrimPrefix(target.ID, "block:"),
		Status:    "error", Error: "deadline", Ranges: []storagetest.RangeObservation{},
	}
	numbers := strings.Split(target.Number, ":")
	if len(numbers) != 2 {
		return storagetest.SampleObservation{}, errSampleProtocol
	}
	major, majorErr := strconv.ParseUint(numbers[0], 10, 32)
	minor, minorErr := strconv.ParseUint(numbers[1], 10, 32)
	if majorErr != nil || minorErr != nil {
		return storagetest.SampleObservation{}, errSampleProtocol
	}
	value.Identity = storagetest.Identity{
		Major: uint32(major), Minor: uint32(minor),
		CapacityBytes: target.CapacityBytes, LogicalBlockBytes: target.LogicalBlockBytes,
	}
	for _, planned := range target.Ranges {
		value.Ranges = append(value.Ranges, storagetest.RangeObservation{
			Offset: uint64(planned.Offset), Length: uint32(planned.Length), Outcome: "not_started",
		})
	}
	// The existing independent model rejects malformed identities, exclusions,
	// geometry and any caller-substituted schedule before the first read.
	if _, err := storagetest.ValidateObservation(target, value); err != nil {
		return storagetest.SampleObservation{}, errSampleProtocol
	}
	return value, nil
}

// copyAdmittedSampleTarget isolates a finite, already validated target from
// caller and adapter slices. Call only after prepareSampleObservation succeeds.
func copyAdmittedSampleTarget(target storagetest.Target) storagetest.Target {
	if target.Ranges != nil {
		target.Ranges = append([]storagetest.Range{}, target.Ranges...)
	}
	if target.Exclusions != nil {
		target.Exclusions = append([]string{}, target.Exclusions...)
	}
	return target
}
