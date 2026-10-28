// Package storagetest defines bounded repeated buffered reads of admitted media.
// Matching samples establish only consistency of those kernel reads.
package storagetest

import (
	"errors"
	"math"
)

const (
	MaximumDevices         = 256
	MaximumEdges           = 1024
	MaximumFixtures        = 8
	MaximumSampleBytes     = 64 << 10
	MaximumDeviceReadBytes = 6 * MaximumSampleBytes
	MaximumTotalReadBytes  = MaximumDevices * MaximumDeviceReadBytes
)

var ErrGeometry = errors.New("storage sampling geometry is unsupported")

// Range describes public sample geometry; it never contains media data.
type Range struct {
	Offset int64 `json:"offset"`
	Length int   `json:"length"`
}

// SampleRanges returns the sorted, deduplicated start, middle and end schedule.
// Capacity is measured in bytes and must be representable by a signed offset.
func SampleRanges(capacity uint64, logicalBlock uint32) ([]Range, error) {
	if capacity == 0 || capacity > math.MaxInt64 || logicalBlock < 512 || logicalBlock > MaximumSampleBytes || logicalBlock&(logicalBlock-1) != 0 || capacity%uint64(logicalBlock) != 0 {
		return nil, ErrGeometry
	}
	width := uint64(MaximumSampleBytes)
	if capacity < width {
		width = capacity
	}
	last := capacity - width
	align := uint64(logicalBlock)
	positions := [3]uint64{0, (last / (2 * align)) * align, (last / align) * align}
	ranges := make([]Range, 0, 3)
	for _, offset := range positions {
		if len(ranges) == 0 || ranges[len(ranges)-1].Offset != int64(offset) {
			ranges = append(ranges, Range{Offset: int64(offset), Length: int(width)})
		}
	}
	return ranges, nil
}

// RangeCoverage counts distinct compared bytes, not the sum of overlapping
// windows. The input must contain sorted, distinct starts and bounded lengths;
// callers that accept evidence must also compare it with the admitted schedule.
func RangeCoverage(ranges []Range) (int64, error) {
	if len(ranges) > 3 {
		return 0, ErrGeometry
	}
	var total, end, previous int64
	for index, sample := range ranges {
		if sample.Offset < 0 || sample.Length <= 0 || sample.Length > MaximumSampleBytes || sample.Offset > math.MaxInt64-int64(sample.Length) || index > 0 && sample.Offset <= previous {
			return 0, ErrGeometry
		}
		limit := sample.Offset + int64(sample.Length)
		start := sample.Offset
		if start < end {
			start = end
		}
		if limit > start {
			total += limit - start
		}
		if limit > end {
			end = limit
		}
		previous = sample.Offset
	}
	return total, nil
}
