package storagesample

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

func fixtureSampleWorkerReply() sampleWorkerReply {
	target := fixtureSampleTarget(512, 0)
	return sampleWorkerReply{
		Schema: sampleWorkerSchema, Target: target, SnapshotSHA256: strings.Repeat("a", 64),
		Observation: &storagetest.SampleObservation{
			Interface: "fixture0", Identity: storagetest.Identity{Major: 8, Minor: 0, CapacityBytes: 512, LogicalBlockBytes: 512},
			Status: "pass", Error: "none",
			Ranges:    []storagetest.RangeObservation{{Offset: 0, Length: 512, ReadCount: 2, FirstBytes: 512, SecondBytes: 512, Outcome: "matched"}},
			ReadBytes: 1024, ComparedBytes: 512, MatchedRanges: 1,
		},
		Error: "none", CloseConfirmed: true, FinalBindingConfirmed: true,
	}
}

func uncheckedSampleWorkerBytes(t *testing.T, value sampleWorkerReply) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func TestStorageSampleWorkerProtocolIndependentLedger(t *testing.T) {
	value := fixtureSampleWorkerReply()
	raw, err := encodeSampleWorkerReply(value)
	if err != nil {
		t.Fatal(err)
	}
	decoded, summary, err := decodeSampleWorkerReply(value.Target, value.SnapshotSHA256, raw)
	if err != nil || !reflect.DeepEqual(decoded, value) || summary.ReadBytes != 1024 || summary.ComparedBytes != 512 || summary.MatchedRanges != 1 || summary.FailureObserved {
		t.Fatalf("independent fixed ledger changed: %+v, %v", summary, err)
	}
	t.Run("hardware_failure_before_close_error", func(t *testing.T) {
		value := fixtureSampleWorkerReply()
		value.Observation.Status, value.Observation.Error = "fail", "mismatch"
		value.Observation.Ranges[0].Outcome = "mismatch"
		value.Observation.ComparedBytes, value.Observation.MatchedRanges = 0, 0
		value.Error, value.CloseConfirmed = "close_error", false
		raw, err := encodeSampleWorkerReply(value)
		if err != nil {
			t.Fatal(err)
		}
		decoded, summary, err := decodeSampleWorkerReply(value.Target, value.SnapshotSHA256, raw)
		if err != nil || !summary.FailureObserved || summary.ReadBytes != 1024 || decoded.CloseConfirmed {
			t.Fatal("close fault erased the earlier diagnostic")
		}
	})
	t.Run("open_failure_no_observation", func(t *testing.T) {
		value := fixtureSampleWorkerReply()
		value.Observation, value.FinalBindingConfirmed, value.Error = nil, false, "open_error"
		raw, err := encodeSampleWorkerReply(value)
		if err != nil {
			t.Fatal(err)
		}
		decoded, summary, err := decodeSampleWorkerReply(value.Target, value.SnapshotSHA256, raw)
		if err != nil || decoded.Observation != nil || summary != (storagetest.ObservationSummary{}) {
			t.Fatal("open failure gained sampling credit")
		}
	})
}

func TestStorageSampleWorkerProtocolRejectsMutatedClaims(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*sampleWorkerReply)
	}{
		{"schema", func(v *sampleWorkerReply) { v.Schema = "other" }},
		{"snapshot", func(v *sampleWorkerReply) { v.SnapshotSHA256 = strings.Repeat("b", 64) }},
		{"target_number", func(v *sampleWorkerReply) { v.Target.Number = "8:1" }},
		{"target_range", func(v *sampleWorkerReply) { v.Target.Ranges[0].Offset = 512 }},
		{"target_mask", func(v *sampleWorkerReply) { v.Target.FixtureMask = 1 }},
		{"unknown_error", func(v *sampleWorkerReply) { v.Error = "private fixture text" }},
		{"missing_close", func(v *sampleWorkerReply) { v.CloseConfirmed = false }},
		{"contradictory_close", func(v *sampleWorkerReply) { v.Error = "close_error" }},
		{"missing_final_binding", func(v *sampleWorkerReply) { v.FinalBindingConfirmed = false }},
		{"empty_success", func(v *sampleWorkerReply) { v.Observation = nil }},
		{"empty_bound_failure", func(v *sampleWorkerReply) { v.Observation = nil; v.Error = "open_error" }},
		{"invented_reads", func(v *sampleWorkerReply) { v.Observation.ReadBytes++ }},
		{"invented_comparisons", func(v *sampleWorkerReply) { v.Observation.ComparedBytes++ }},
		{"invented_matches", func(v *sampleWorkerReply) { v.Observation.MatchedRanges++ }},
		{"missing_range", func(v *sampleWorkerReply) { v.Observation.Ranges = []storagetest.RangeObservation{} }},
		{"extra_range", func(v *sampleWorkerReply) {
			v.Observation.Ranges = append(v.Observation.Ranges, v.Observation.Ranges[0])
		}},
		{"range_offset", func(v *sampleWorkerReply) { v.Observation.Ranges[0].Offset = 512 }},
		{"short_claimed_match", func(v *sampleWorkerReply) { v.Observation.Ranges[0].FirstBytes = 511 }},
		{"extra_read", func(v *sampleWorkerReply) { v.Observation.Ranges[0].ReadCount = 3 }},
		{"wrong_identity", func(v *sampleWorkerReply) { v.Observation.Identity.Minor = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			expected := fixtureSampleWorkerReply()
			value := fixtureSampleWorkerReply()
			test.mutate(&value)
			_, _, err := decodeSampleWorkerReply(expected.Target, expected.SnapshotSHA256, uncheckedSampleWorkerBytes(t, value))
			if !errors.Is(err, errSampleProtocol) {
				t.Fatal("mutated completion was admitted")
			}
		})
	}
}

func TestStorageSampleWorkerProtocolRejectsNoncanonicalFrames(t *testing.T) {
	value := fixtureSampleWorkerReply()
	good := string(uncheckedSampleWorkerBytes(t, value))
	for _, test := range []struct{ name, raw string }{
		{"missing_lf", strings.TrimSuffix(good, "\n")},
		{"extra_lf", good + "\n"},
		{"trailing_frame", good + good},
		{"trailing_text", good + "fixture"},
		{"extra_field", `{"extra":0,` + good[1:]},
		{"duplicate_field", `{"schema":"hhsoft.pc-tester.storage-sample-worker/v1",` + good[1:]},
		{"case_alias", strings.Replace(good, `"close_confirmed"`, `"Close_Confirmed"`, 1)},
		{"null_boolean", strings.Replace(good, `"close_confirmed":true`, `"close_confirmed":null`, 1)},
		{"missing_boolean", strings.Replace(good, `"close_confirmed":true,`, "", 1)},
		{"unknown_nested_field", strings.Replace(good, `"observation":{`, `"observation":{"content":"fixture",`, 1)},
		{"oversized", strings.Repeat("x", maximumSampleWorkerReply) + "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.raw == good {
				t.Fatal("mutation did not change the frame")
			}
			_, _, err := decodeSampleWorkerReply(value.Target, value.SnapshotSHA256, []byte(test.raw))
			if !errors.Is(err, errSampleProtocol) {
				t.Fatal("noncanonical frame was admitted")
			}
		})
	}
}

func TestStorageSampleWorkerProtocolRejectsInvalidExpectedAuthority(t *testing.T) {
	value := fixtureSampleWorkerReply()
	raw := uncheckedSampleWorkerBytes(t, value)
	t.Run("fixture_mask", func(t *testing.T) {
		target := value.Target
		target.FixtureMask = 1
		if _, _, err := decodeSampleWorkerReply(target, value.SnapshotSHA256, raw); !errors.Is(err, errSampleProtocol) {
			t.Fatal("reserved fixture became expected authority")
		}
	})
	for _, test := range []struct{ name, digest string }{{"short", "a"}, {"uppercase", strings.Repeat("A", 64)}, {"invalid", strings.Repeat("g", 64)}} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := decodeSampleWorkerReply(value.Target, test.digest, raw); !errors.Is(err, errSampleProtocol) {
				t.Fatal("invalid expected source digest accepted")
			}
		})
	}
}
