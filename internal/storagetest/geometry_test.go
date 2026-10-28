package storagetest

import (
	"math"
	"reflect"
	"testing"
)

func TestSampleRangesLiteralGeometry(t *testing.T) {
	cases := []struct {
		name     string
		capacity uint64
		block    uint32
		want     []Range
		coverage int64
	}{
		{"single sector", 512, 512, []Range{{0, 512}}, 512},
		{"one window", 65536, 4096, []Range{{0, 65536}}, 65536},
		{"two overlapping positions", 66048, 512, []Range{{0, 65536}, {512, 65536}}, 66048},
		{"three overlapping positions", 98304, 4096, []Range{{0, 65536}, {16384, 65536}, {32768, 65536}}, 98304},
		{"aligned odd middle", 200192, 512, []Range{{0, 65536}, {67072, 65536}, {134656, 65536}}, 196608},
		{"separated windows", 1048576, 4096, []Range{{0, 65536}, {491520, 65536}, {983040, 65536}}, 196608},
		{"largest supported sector", 196608, 65536, []Range{{0, 65536}, {65536, 65536}, {131072, 65536}}, 196608},
		{"largest aligned signed capacity", 9223372036854775296, 512, []Range{{0, 65536}, {4611686018427354624, 65536}, {9223372036854709760, 65536}}, 196608},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			got, err := SampleRanges(item.capacity, item.block)
			if err != nil || !reflect.DeepEqual(got, item.want) {
				t.Fatalf("ranges %v, error %v; want %v", got, err, item.want)
			}
			coverage, err := RangeCoverage(got)
			if err != nil || coverage != item.coverage {
				t.Fatalf("coverage %d, error %v; want %d", coverage, err, item.coverage)
			}
		})
	}
}

func TestSampleRangesRejectInvalidGeometry(t *testing.T) {
	for _, item := range []struct {
		capacity uint64
		block    uint32
	}{
		{0, 512}, {511, 512}, {513, 512}, {65536, 0}, {65536, 256},
		{65536, 768}, {65536, 131072}, {math.MaxInt64, 512},
		{uint64(math.MaxInt64) + 1, 512}, {math.MaxUint64, 65536},
	} {
		if got, err := SampleRanges(item.capacity, item.block); err != ErrGeometry || got != nil {
			t.Fatalf("accepted capacity %d block %d: %v, %v", item.capacity, item.block, got, err)
		}
	}
}

func TestRangeCoverageLiteralUnionsAndMalformedRanges(t *testing.T) {
	for _, item := range []struct {
		ranges []Range
		want   int64
	}{
		{nil, 0}, {[]Range{}, 0}, {[]Range{{0, 512}, {256, 512}}, 768},
		{[]Range{{0, 65536}, {512, 512}, {1024, 512}}, 65536},
		{[]Range{{512, 512}, {2048, 512}}, 1024},
	} {
		got, err := RangeCoverage(item.ranges)
		if err != nil || got != item.want {
			t.Fatalf("ranges %v: %d %v; want %d", item.ranges, got, err, item.want)
		}
	}
	for _, ranges := range [][]Range{
		{{-1, 512}}, {{0, 0}}, {{0, 65537}}, {{math.MaxInt64, 1}},
		{{0, 512}, {0, 512}}, {{512, 512}, {0, 512}},
		{{0, 1}, {1, 1}, {2, 1}, {3, 1}},
	} {
		if _, err := RangeCoverage(ranges); err != ErrGeometry {
			t.Fatalf("accepted malformed ranges %v", ranges)
		}
	}
}
