package storagetest

import (
	"reflect"
	"testing"
)

func observationFixture() (Target, SampleObservation) {
	target := Target{ID: "block:a", Number: "8:0", CapacityBytes: 98304, LogicalBlockBytes: 4096, Exclusions: []string{}, Ranges: []Range{{0, 65536}, {16384, 65536}, {32768, 65536}}}
	value := SampleObservation{Interface: "a", Identity: Identity{8, 0, 98304, 4096}, Status: "aborted", Error: "cancelled", Ranges: []RangeObservation{{Offset: 0, Length: 65536, Outcome: "not_started"}, {Offset: 16384, Length: 65536, Outcome: "not_started"}, {Offset: 32768, Length: 65536, Outcome: "not_started"}}}
	return target, value
}

func matchedObservation(index int) RangeObservation {
	offsets := [3]uint64{0, 16384, 32768}
	return RangeObservation{Offset: offsets[index], Length: 65536, ReadCount: 2, FirstBytes: 65536, SecondBytes: 65536, Outcome: "matched"}
}

func TestObservationLiteralMatchedOverlap(t *testing.T) {
	target, value := observationFixture()
	value.Status = "pass"
	value.Error = "none"
	value.Ranges = []RangeObservation{matchedObservation(0), matchedObservation(1), matchedObservation(2)}
	value.ReadBytes = 393216
	value.ComparedBytes = 98304
	value.MatchedRanges = 3
	got, err := ValidateObservation(target, value)
	if err != nil || !reflect.DeepEqual(got, ObservationSummary{ReadBytes: 393216, ComparedBytes: 98304, MatchedRanges: 3}) {
		t.Fatalf("literal coverage: %+v %v", got, err)
	}
	value.ComparedBytes = 196608
	if _, err := ValidateObservation(target, value); err != ErrObservation {
		t.Fatal("overlapping windows credited by summation")
	}
}

func TestObservationReadFaultsRetainActualCountsAndStop(t *testing.T) {
	faults := []struct {
		name, outcome string
		reads         uint8
		first, second uint32
		bytes         uint64
	}{
		{"first short", "short_read", 1, 17, 0, 17},
		{"second short", "short_read", 2, 65536, 17, 65553},
		{"first error with data", "read_error", 1, 17, 0, 17},
		{"second error with full data", "read_error", 2, 65536, 65536, 131072},
		{"mismatch", "mismatch", 2, 65536, 65536, 131072},
	}
	for index := 0; index < 3; index++ {
		for _, fault := range faults {
			t.Run(fault.name+string(rune('0'+index)), func(t *testing.T) {
				target, value := observationFixture()
				for prefix := 0; prefix < index; prefix++ {
					value.Ranges[prefix] = matchedObservation(prefix)
				}
				current := &value.Ranges[index]
				current.ReadCount = fault.reads
				current.FirstBytes = fault.first
				current.SecondBytes = fault.second
				current.Outcome = fault.outcome
				value.Status = "fail"
				value.Error = fault.outcome
				value.ReadBytes = []uint64{0, 131072, 262144}[index] + fault.bytes
				value.ComparedBytes = []uint64{0, 65536, 81920}[index]
				value.MatchedRanges = uint8(index)
				got, err := ValidateObservation(target, value)
				if err != nil || !got.FailureObserved || got.ReadBytes != value.ReadBytes || got.ComparedBytes != value.ComparedBytes {
					t.Fatalf("fault ledger: %+v %v", got, err)
				}
				if index < 2 {
					value.Ranges[index+1] = matchedObservation(index + 1)
					if _, err := ValidateObservation(target, value); err != ErrObservation {
						t.Fatal("read resumed after fault")
					}
				}
			})
		}
	}
}

func TestObservationCancellationBoundariesDoNotInventComparisons(t *testing.T) {
	for _, item := range []struct {
		name                string
		prefix              int
		reads               uint8
		wantBytes, coverage uint64
	}{
		{"before first read", 0, 0, 0, 0},
		{"after first full read", 0, 1, 65536, 0},
		{"after second full return before compare", 0, 2, 131072, 0},
		{"after accepted first comparison", 1, 0, 131072, 65536},
		{"next range first full return", 1, 1, 196608, 65536},
	} {
		for _, stop := range []struct{ reason, status string }{{"cancelled", "aborted"}, {"deadline", "error"}, {"identity_changed", "error"}} {
			t.Run(item.name+"/"+stop.reason, func(t *testing.T) {
				target, value := observationFixture()
				for i := 0; i < item.prefix; i++ {
					value.Ranges[i] = matchedObservation(i)
				}
				if item.reads != 0 {
					current := &value.Ranges[item.prefix]
					current.Outcome = "interrupted"
					current.ReadCount = item.reads
					current.FirstBytes = 65536
					if item.reads == 2 {
						current.SecondBytes = 65536
					}
				}
				value.Status = stop.status
				value.Error = stop.reason
				value.ReadBytes = item.wantBytes
				value.ComparedBytes = item.coverage
				value.MatchedRanges = uint8(item.prefix)
				got, err := ValidateObservation(target, value)
				if err != nil || got.FailureObserved || got.ReadBytes != item.wantBytes || got.ComparedBytes != item.coverage {
					t.Fatalf("boundary: %+v %v", got, err)
				}
			})
		}
	}
	// The completion may later report cancellation. It cannot rewrite the last
	// already accepted comparison into an interrupted zero-credit range.
	target, value := observationFixture()
	value.Ranges = []RangeObservation{matchedObservation(0), matchedObservation(1), matchedObservation(2)}
	value.Status = "pass"
	value.Error = "none"
	value.ReadBytes = 393216
	value.ComparedBytes = 98304
	value.MatchedRanges = 3
	if _, err := ValidateObservation(target, value); err != nil {
		t.Fatal(err)
	}
	value.Status = "aborted"
	value.Error = "cancelled"
	if _, err := ValidateObservation(target, value); err != ErrObservation {
		t.Fatal("completed comparison status rewritten by later stop")
	}
}

func TestObservationFailureCannotBeHiddenByLaterStop(t *testing.T) {
	for _, outcome := range []string{"mismatch", "read_error", "short_read"} {
		target, value := observationFixture()
		value.Ranges[0] = matchedObservation(0)
		value.Ranges[0].Outcome = outcome
		if outcome == "short_read" {
			value.Ranges[0].SecondBytes = 17
			value.ReadBytes = 65553
		} else {
			value.ReadBytes = 131072
		}
		value.Status = "fail"
		value.Error = outcome
		if _, err := ValidateObservation(target, value); err != nil {
			t.Fatalf("original %s fault: %v", outcome, err)
		}
		value.Status = "aborted"
		value.Error = "cancelled"
		if _, err := ValidateObservation(target, value); err != ErrObservation {
			t.Fatalf("%s fault erased by cancellation", outcome)
		}
		value.Ranges[0].Outcome = "interrupted"
		if outcome == "short_read" {
			if _, err := ValidateObservation(target, value); err != ErrObservation {
				t.Fatal("partial read masqueraded as interruption")
			}
		}
	}
}

func TestObservationRejectsForgedIdentityScheduleAndCounters(t *testing.T) {
	for _, item := range []struct {
		name   string
		mutate func(*Target, *SampleObservation)
	}{
		{"null exclusions", func(x *Target, v *SampleObservation) { x.Exclusions = nil }},
		{"excluded target", func(x *Target, v *SampleObservation) { x.Exclusions = []string{"mounted"} }},
		{"forged schedule", func(x *Target, v *SampleObservation) { x.Ranges[1].Offset = 4096 }},
		{"wrong interface", func(x *Target, v *SampleObservation) { v.Interface = "b" }},
		{"wrong device", func(x *Target, v *SampleObservation) { v.Identity.Minor = 1 }},
		{"changed capacity", func(x *Target, v *SampleObservation) { v.Identity.CapacityBytes = 1048576 }},
		{"null ranges", func(x *Target, v *SampleObservation) { v.Ranges = nil }},
		{"missing range", func(x *Target, v *SampleObservation) { v.Ranges = v.Ranges[:2] }},
		{"zero work pass", func(x *Target, v *SampleObservation) { v.Status = "pass"; v.Error = "none" }},
		{"invented bytes", func(x *Target, v *SampleObservation) { v.ReadBytes = 1 }},
		{"invented coverage", func(x *Target, v *SampleObservation) { v.ComparedBytes = 1 }},
		{"invented matched range", func(x *Target, v *SampleObservation) { v.MatchedRanges = 1 }},
		{"unknown reason", func(x *Target, v *SampleObservation) { v.Error = "raw device exception" }},
		{"read after not started", func(x *Target, v *SampleObservation) {
			v.Ranges[1] = matchedObservation(1)
			v.ReadBytes = 131072
			v.ComparedBytes = 65536
			v.MatchedRanges = 1
		}},
		{"unattempted bytes", func(x *Target, v *SampleObservation) { v.Ranges[0].FirstBytes = 1; v.ReadBytes = 1 }},
		{"extra attempt", func(x *Target, v *SampleObservation) { v.Ranges[0].ReadCount = 3 }},
		{"interrupted partial return", func(x *Target, v *SampleObservation) {
			v.Ranges[0].Outcome = "interrupted"
			v.Ranges[0].ReadCount = 1
			v.Ranges[0].FirstBytes = 17
			v.ReadBytes = 17
		}},
		{"open error after read", func(x *Target, v *SampleObservation) {
			v.Ranges[0].Outcome = "interrupted"
			v.Ranges[0].ReadCount = 1
			v.Ranges[0].FirstBytes = 65536
			v.ReadBytes = 65536
			v.Status = "error"
			v.Error = "open_error"
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			target, value := observationFixture()
			item.mutate(&target, &value)
			if _, err := ValidateObservation(target, value); err != ErrObservation {
				t.Fatal("forged observation accepted")
			}
		})
	}
}
