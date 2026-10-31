package quickevidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

func memoryEvidenceTime(n int64) *int64 { return &n }

func memoryEvidencePass() quick.MemoryStressResult {
	return quick.MemoryStressResult{
		Schema: "hhsoft.pc-tester.memory-stress/v1", Status: "pass", StopReason: "completed", Suite: "byte-patterns-aa55-inversions-v1",
		RequestedLoadMilliseconds: 1800000, LocalWatchdogMilliseconds: 1860000, ThermalPolicy: "available-observations/v1",
		RequestedBytes: 4194304, AvailableBytes: 16777216, AllocatedBytes: 4194304,
		EffectiveDeadlineMilliseconds: 1860000, EffectiveDeadlineSource: "watchdog",
		AvailabilityFinishedMilliseconds: memoryEvidenceTime(1), AllocationFinishedMilliseconds: memoryEvidenceTime(2),
		LoadStartedMilliseconds: memoryEvidenceTime(3), LoadStoppedMilliseconds: memoryEvidenceTime(1800003), ObservedLoadMilliseconds: 1800000,
		TerminalStartedMilliseconds: memoryEvidenceTime(1800004), TerminalStoppedMilliseconds: memoryEvidenceTime(1800005), FinishedMilliseconds: 1800006,
		AccountingValid: true,
		Availability:    quick.MemoryStressAvailability{Started: true, Opened: true, RawBytes: 23, ReadCalls: 2, EOF: true, Valid: true, CloseConfirmed: true},
		Coverage:        quick.MemoryStressCoverage{CompletedPhases: 20, CompletedCycles: 2, LoadCycles: 1, ReadBytes: 50331648, WrittenBytes: 50331648, TerminalReadBytes: 4194304},
		StopFacts:       quick.MemoryStressStopFacts{NormalLoadComplete: true},
		Safety:          quick.SafetyEvidence{GlobalTimeoutMilliseconds: 1860000, ThermalLimitMilliCelsius: 90000, ThermalSensors: 1, ThermalSamples: 2, PeakMilliCelsius: 42000, MonitorStatus: "completed", CleanupConfirmed: true, ThermalObservations: 2, ThermalCoverage: "available"},
	}
}

func memoryEvidenceNoLoad() quick.MemoryStressResult {
	return quick.MemoryStressResult{
		Schema: "hhsoft.pc-tester.memory-stress/v1", Status: "aborted", StopReason: "parent_cancelled", Suite: "byte-patterns-aa55-inversions-v1",
		RequestedLoadMilliseconds: 1800000, LocalWatchdogMilliseconds: 1860000, ThermalPolicy: "available-observations/v1",
		EffectiveDeadlineMilliseconds: 1860000, EffectiveDeadlineSource: "watchdog", AccountingValid: true,
		Availability: quick.MemoryStressAvailability{CloseConfirmed: true}, StopFacts: quick.MemoryStressStopFacts{ParentCancelled: true},
		Safety: quick.SafetyEvidence{GlobalTimeoutMilliseconds: 1860000, ThermalLimitMilliCelsius: 90000, MonitorStatus: "not_started", CleanupConfirmed: true, ThermalCoverage: "unavailable"},
	}
}

func memoryEvidenceBytes(t *testing.T, v quick.MemoryStressResult) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func requireMemoryEvidence(t *testing.T, v quick.MemoryStressResult) {
	t.Helper()
	if _, _, err := DecodeMemoryStress(memoryEvidenceBytes(t, v)); err != nil {
		t.Fatalf("independent valid prerequisite: %v", err)
	}
}

func TestMemoryStressDecoderAcceptsTypedEquivalentEncoding(t *testing.T) {
	v := memoryEvidencePass()
	raw := memoryEvidenceBytes(t, v)
	got, digest, err := DecodeMemoryStress(raw)
	if err != nil || got.Coverage != v.Coverage || !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		t.Fatal("literal valid evidence rejected")
	}
	if _, err := DecodeLocal("memory.quick", raw); err == nil {
		t.Fatal("standalone stress became memory.quick authority")
	}
	escaped := strings.Replace(string(raw), "\"schema\"", "\"\\u0073chema\"", 1)
	escaped = strings.Replace(escaped, "memory-stress/v1", "memory-stress/v\\u0031", 1)
	if escaped == string(raw) || !strings.Contains(escaped, "\\u0073") || !strings.Contains(escaped, "\\u0031") {
		t.Fatal("escaped fixture ineffective")
	}
	var decoded quick.MemoryStressResult
	if json.Unmarshal([]byte(escaped), &decoded) != nil || decoded.Schema != v.Schema {
		t.Fatal("decoded-equivalence prerequisite")
	}
	_, other, err := DecodeMemoryStress([]byte(escaped))
	if err != nil || other != digest {
		t.Fatal("equivalent typed evidence changed digest")
	}
	_, spaced, err := DecodeMemoryStress(append([]byte(" \n"), append(raw, '\n')...))
	if err != nil || spaced != digest {
		t.Fatal("outer JSON whitespace changed typed digest")
	}
}

func TestMemoryStressDecoderRejectsIncompleteAndAmbiguousJSON(t *testing.T) {
	raw := string(memoryEvidenceBytes(t, memoryEvidencePass()))
	requireMemoryEvidence(t, memoryEvidencePass())
	for _, tc := range []struct {
		name   string
		change func(string) string
	}{
		{"missing", func(s string) string { return strings.Replace(s, "\"allocated_bytes\":4194304,", "", 1) }},
		{"duplicate", func(s string) string {
			return strings.Replace(s, "\"schema\":", "\"schema\":\"hhsoft.pc-tester.memory-stress/v1\",\"schema\":", 1)
		}},
		{"escaped_duplicate", func(s string) string {
			return strings.Replace(s, "\"schema\":", "\"\\u0073chema\":\"hhsoft.pc-tester.memory-stress/v1\",\"schema\":", 1)
		}},
		{"case_alias", func(s string) string { return strings.Replace(s, "\"schema\":", "\"Schema\":", 1) }},
		{"unknown", func(s string) string { return strings.Replace(s, "{", "{\"extra\":0,", 1) }},
		{"nested_duplicate", func(s string) string {
			return strings.Replace(s, "\"load_cycles\":1", "\"load_cycles\":1,\"load_cycles\":1", 1)
		}},
		{"nested_unknown", func(s string) string { return strings.Replace(s, "\"coverage\":{", "\"coverage\":{\"extra\":0,", 1) }},
		{"null_safety", func(s string) string { start := strings.Index(s, "\"safety\":"); return s[:start] + "\"safety\":null}" }},
		{"decimal_integer", func(s string) string {
			return strings.Replace(s, "\"allocated_bytes\":4194304", "\"allocated_bytes\":4194304.0", 1)
		}},
		{"unsigned_negative", func(s string) string {
			return strings.Replace(s, "\"allocated_bytes\":4194304", "\"allocated_bytes\":-1", 1)
		}},
		{"unsigned_overflow", func(s string) string {
			return strings.Replace(s, "\"completed_phases\":20", "\"completed_phases\":18446744073709551616", 1)
		}},
		{"trailing_value", func(s string) string { return s + "{}" }},
		{"invalid_utf8", func(s string) string { return strings.Replace(s, "completed", "comple\xffted", 1) }},
		{"over_cap", func(s string) string { return s + strings.Repeat(" ", 1<<20) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := tc.change(raw)
			if bad == raw {
				t.Fatal("mutation did not reach target")
			}
			if _, _, err := DecodeMemoryStress([]byte(bad)); !errors.Is(err, ErrInvalidMemoryStress) {
				t.Fatalf("ambiguous shape accepted: %v", err)
			}
		})
	}
}

func TestMemoryStressDecoderRejectsForgedCountersAndAuthority(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*quick.MemoryStressResult)
	}{
		{"future_schema", func(v *quick.MemoryStressResult) { v.Schema = "hhsoft.pc-tester.memory-stress/v2" }},
		{"wrong_suite", func(v *quick.MemoryStressResult) { v.Suite = "other" }},
		{"short_load", func(v *quick.MemoryStressResult) { v.ObservedLoadMilliseconds = 1799999 }},
		{"no_verified_load_cycle", func(v *quick.MemoryStressResult) { v.Coverage.LoadCycles = 0 }},
		{"multiple_finishing_cycles", func(v *quick.MemoryStressResult) {
			v.Coverage.CompletedCycles = 3
			v.Coverage.CompletedPhases = 30
			v.Coverage.ReadBytes = 75497472
			v.Coverage.WrittenBytes = 75497472
		}},
		{"excess_load_cycles", func(v *quick.MemoryStressResult) { v.Coverage.LoadCycles = 3 }},
		{"terminal_promoted_to_cycle", func(v *quick.MemoryStressResult) { v.Coverage.CompletedPhases = 21 }},
		{"wrong_read_count", func(v *quick.MemoryStressResult) { v.Coverage.ReadBytes++ }},
		{"wrong_write_count", func(v *quick.MemoryStressResult) { v.Coverage.WrittenBytes++ }},
		{"phase_count_overflow", func(v *quick.MemoryStressResult) {
			v.Coverage.CompletedPhases = math.MaxUint64
			v.Coverage.CompletedCycles = math.MaxUint64 / 10
			v.Coverage.LoadCycles = v.Coverage.CompletedCycles
		}},
		{"short_terminal", func(v *quick.MemoryStressResult) { v.Coverage.TerminalReadBytes = 3932160 }},
		{"uncredited_pass", func(v *quick.MemoryStressResult) {
			v.Coverage.TerminalReadBytes = 3932160
			v.Coverage.TerminalUncreditedBytes = 262144
		}},
		{"over_quarter", func(v *quick.MemoryStressResult) { v.AvailableBytes = 16776192 }},
		{"wrong_requested_size", func(v *quick.MemoryStressResult) { v.RequestedBytes = 4194305 }},
		{"non_kib_availability", func(v *quick.MemoryStressResult) { v.AvailableBytes++ }},
		{"missing_eof", func(v *quick.MemoryStressResult) { v.Availability.EOF = false }},
		{"read_count_impossible", func(v *quick.MemoryStressResult) { v.Availability.ReadCalls = 25 }},
		{"raw_count_impossible", func(v *quick.MemoryStressResult) { v.Availability.RawBytes = 8193; v.Availability.ReadCalls = 2 }},
		{"read_cap", func(v *quick.MemoryStressResult) { v.Availability.RawBytes = 65537 }},
		{"allocation_without_acquisition", func(v *quick.MemoryStressResult) {
			v.Availability = quick.MemoryStressAvailability{CloseConfirmed: true}
			v.AvailabilityFinishedMilliseconds = nil
		}},
		{"thermal_limit_removed", func(v *quick.MemoryStressResult) { v.Safety.PeakMilliCelsius = 95000 }},
		{"thermal_unavailable", func(v *quick.MemoryStressResult) { v.Safety.ThermalCoverage = "unavailable" }},
		{"monitor_timeout_changed", func(v *quick.MemoryStressResult) { v.Safety.GlobalTimeoutMilliseconds = 660000 }},
		{"finish_at_deadline", func(v *quick.MemoryStressResult) { v.FinishedMilliseconds = 1860000 }},
		{"early_terminal", func(v *quick.MemoryStressResult) { v.TerminalStartedMilliseconds = memoryEvidenceTime(1800002) }},
		{"reversed_terminal", func(v *quick.MemoryStressResult) { v.TerminalStoppedMilliseconds = memoryEvidenceTime(1800003) }},
		{"missing_load_start", func(v *quick.MemoryStressResult) { v.LoadStartedMilliseconds = nil }},
		{"false_normal_end", func(v *quick.MemoryStressResult) { v.StopFacts.NormalLoadComplete = false }},
		{"wrong_status", func(v *quick.MemoryStressResult) { v.Status = "fail" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := memoryEvidencePass()
			requireMemoryEvidence(t, v)
			before := memoryEvidenceBytes(t, v)
			tc.change(&v)
			if bytes.Equal(before, memoryEvidenceBytes(t, v)) {
				t.Fatal("mutation ineffective")
			}
			if _, _, err := DecodeMemoryStress(memoryEvidenceBytes(t, v)); !errors.Is(err, ErrInvalidMemoryStress) {
				t.Fatalf("forged relation accepted: %v", err)
			}
		})
	}
}

func TestMemoryStressDecoderBoundsEverySignedTimestampBeforeArithmetic(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*quick.MemoryStressResult, int64)
	}{
		{"effective", func(v *quick.MemoryStressResult, n int64) { v.EffectiveDeadlineMilliseconds = n }},
		{"availability", func(v *quick.MemoryStressResult, n int64) { v.AvailabilityFinishedMilliseconds = &n }},
		{"allocation", func(v *quick.MemoryStressResult, n int64) { v.AllocationFinishedMilliseconds = &n }},
		{"load_start", func(v *quick.MemoryStressResult, n int64) { v.LoadStartedMilliseconds = &n }},
		{"load_stop", func(v *quick.MemoryStressResult, n int64) { v.LoadStoppedMilliseconds = &n }},
		{"observed_load", func(v *quick.MemoryStressResult, n int64) { v.ObservedLoadMilliseconds = n }},
		{"terminal_start", func(v *quick.MemoryStressResult, n int64) { v.TerminalStartedMilliseconds = &n }},
		{"terminal_stop", func(v *quick.MemoryStressResult, n int64) { v.TerminalStoppedMilliseconds = &n }},
		{"finish", func(v *quick.MemoryStressResult, n int64) { v.FinishedMilliseconds = n }},
		{"requested_load", func(v *quick.MemoryStressResult, n int64) { v.RequestedLoadMilliseconds = n }},
		{"watchdog", func(v *quick.MemoryStressResult, n int64) { v.LocalWatchdogMilliseconds = n }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range []int64{math.MinInt64, math.MaxInt64} {
				v := memoryEvidencePass()
				requireMemoryEvidence(t, v)
				tc.set(&v, n)
				if _, _, err := DecodeMemoryStress(memoryEvidenceBytes(t, v)); !errors.Is(err, ErrInvalidMemoryStress) {
					t.Fatalf("signed timestamp %d accepted: %v", n, err)
				}
			}
		})
	}
}

func TestMemoryStressDecoderPreservesNonPassAndLateMismatchFacts(t *testing.T) {
	requireMemoryEvidence(t, memoryEvidenceNoLoad())
	for _, stage := range []string{"forward", "reverse", "terminal"} {
		t.Run(stage, func(t *testing.T) {
			v := memoryEvidencePass()
			v.Status = "fail"
			v.StopReason = "memory_mismatch"
			v.StopFacts.ParentCancelled = true
			if stage == "terminal" {
				v.Coverage.TerminalReadBytes = 0
				v.Mismatch = &quick.MemoryStressMismatch{Stage: "terminal", Cycle: 2, Phase: 9, Offset: 0, ExpectedByte: 170, ActualByte: 171}
			} else {
				v.LoadStoppedMilliseconds = memoryEvidenceTime(4)
				v.ObservedLoadMilliseconds = 1
				v.TerminalStartedMilliseconds = nil
				v.TerminalStoppedMilliseconds = nil
				v.FinishedMilliseconds = 5
				v.StopFacts.NormalLoadComplete = false
				v.Coverage = quick.MemoryStressCoverage{CompletedPhases: 1, WrittenBytes: 4194304}
				v.Mismatch = &quick.MemoryStressMismatch{Stage: "cycle", Cycle: 0, Phase: 1, Offset: 0, ExpectedByte: 0, ActualByte: 1}
				if stage == "reverse" {
					v.Coverage = quick.MemoryStressCoverage{CompletedPhases: 8, PhaseBytes: 3932160, ReadBytes: 20709376, WrittenBytes: 24903680, UncreditedBytes: 262143}
					v.Mismatch = &quick.MemoryStressMismatch{Stage: "cycle", Cycle: 0, Phase: 8, Offset: 0, ExpectedByte: 85, ActualByte: 84}
				}
			}
			requireMemoryEvidence(t, v)
			bad := v
			copyMismatch := *v.Mismatch
			copyMismatch.ExpectedByte ^= 2
			bad.Mismatch = &copyMismatch
			if _, _, err := DecodeMemoryStress(memoryEvidenceBytes(t, bad)); !errors.Is(err, ErrInvalidMemoryStress) {
				t.Fatal("wrong literal mismatch byte accepted")
			}
			bad = v
			bad.Status = "aborted"
			bad.StopReason = "parent_cancelled"
			if _, _, err := DecodeMemoryStress(memoryEvidenceBytes(t, bad)); !errors.Is(err, ErrInvalidMemoryStress) {
				t.Fatal("cancellation erased mismatch")
			}
		})
	}
}

func TestMemoryStressDecoderRequiresThermalAdmissionBeforeAvailability(t *testing.T) {
	initial := memoryEvidenceNoLoad()
	initial.Status = "inconclusive"
	initial.StopReason = "thermal_coverage"
	initial.StopFacts = quick.MemoryStressStopFacts{InitialCoverageUnavailable: true}
	initial.Safety.ThermalObservations = 1
	initial.Safety.MonitorStatus = "failed"
	initial.Safety.MonitorFailure = "coverage_loss"
	requireMemoryEvidence(t, initial)

	hot := memoryEvidenceNoLoad()
	hot.Status = "aborted"
	hot.StopReason = "thermal_limit"
	hot.StopFacts = quick.MemoryStressStopFacts{ThermalLimit: true}
	hot.Safety = quick.SafetyEvidence{GlobalTimeoutMilliseconds: 1860000, ThermalLimitMilliCelsius: 90000, ThermalSensors: 1, ThermalSamples: 1, PeakMilliCelsius: 95000, MonitorStatus: "failed", MonitorFailure: "thermal_limit", CleanupConfirmed: true, ThermalObservations: 1, ThermalCoverage: "available"}
	requireMemoryEvidence(t, hot)
	hot.Availability = quick.MemoryStressAvailability{Started: true, Opened: true, RawBytes: 23, ReadCalls: 2, EOF: true, Valid: true, CloseConfirmed: true}
	hot.AvailableBytes = 16777216
	hot.AvailabilityFinishedMilliseconds = memoryEvidenceTime(1)
	hot.FinishedMilliseconds = 2
	if _, _, err := DecodeMemoryStress(memoryEvidenceBytes(t, hot)); !errors.Is(err, ErrInvalidMemoryStress) {
		t.Fatal("hot initial observation admitted acquisition")
	}
	lateHot := memoryEvidencePass()
	lateHot.Status = "aborted"
	lateHot.StopReason = "thermal_limit"
	lateHot.StopFacts.ThermalLimit = true
	lateHot.Safety.PeakMilliCelsius = 95000
	lateHot.Safety.MonitorStatus = "failed"
	lateHot.Safety.MonitorFailure = "thermal_limit"
	requireMemoryEvidence(t, lateHot)
	stopped := memoryEvidenceNoLoad()
	stopped.Safety = quick.SafetyEvidence{GlobalTimeoutMilliseconds: 1860000, ThermalLimitMilliCelsius: 90000, ThermalSensors: 1, ThermalSamples: 1, PeakMilliCelsius: 42000, MonitorStatus: "completed", CleanupConfirmed: true, ThermalObservations: 1, ThermalCoverage: "available"}
	stopped.Availability = quick.MemoryStressAvailability{Started: true, CloseConfirmed: true, Failure: "stopped"}
	stopped.AvailabilityFinishedMilliseconds = memoryEvidenceTime(1)
	stopped.FinishedMilliseconds = 1
	requireMemoryEvidence(t, stopped)
	stopped.StopFacts = quick.MemoryStressStopFacts{}
	stopped.Status = "error"
	stopped.StopReason = "availability_failed"
	if _, _, err := DecodeMemoryStress(memoryEvidenceBytes(t, stopped)); !errors.Is(err, ErrInvalidMemoryStress) {
		t.Fatal("stopped acquisition without adverse cause accepted")
	}
	for _, mode := range []string{"no_observation", "initial_unavailable"} {
		t.Run(mode, func(t *testing.T) {
			v := initial
			if mode == "no_observation" {
				v = memoryEvidenceNoLoad()
			}
			v.Availability = quick.MemoryStressAvailability{Started: true, Opened: true, RawBytes: 23, ReadCalls: 2, EOF: true, Valid: true, CloseConfirmed: true}
			v.AvailableBytes = 16777216
			v.AvailabilityFinishedMilliseconds = memoryEvidenceTime(1)
			v.FinishedMilliseconds = 2
			if _, _, err := DecodeMemoryStress(memoryEvidenceBytes(t, v)); !errors.Is(err, ErrInvalidMemoryStress) {
				t.Fatal("unadmitted acquisition accepted")
			}
		})
	}
	later := memoryEvidencePass()
	later.Status = "inconclusive"
	later.StopReason = "thermal_coverage"
	later.StopFacts.CoverageLost = true
	later.Safety.ThermalObservations = 3
	later.Safety.ThermalCoverage = "partial"
	later.Safety.MonitorStatus = "failed"
	later.Safety.MonitorFailure = "coverage_loss"
	requireMemoryEvidence(t, later)
}

func TestMemoryStressDecoderCleanupSentinelPrecedesOtherEvidence(t *testing.T) {
	for _, mode := range []string{"monitor", "availability", "fact"} {
		t.Run(mode, func(t *testing.T) {
			v := memoryEvidencePass()
			switch mode {
			case "monitor":
				v.Safety.CleanupConfirmed = false
			case "availability":
				v.Availability.CloseConfirmed = false
			case "fact":
				v.StopFacts.CleanupUnconfirmed = true
			}
			v.Status = "invalid"
			if _, _, err := DecodeMemoryStress(memoryEvidenceBytes(t, v)); !errors.Is(err, quick.ErrMemoryStressCleanupUnconfirmed) {
				t.Fatal("cleanup authority hidden by another defect")
			}
		})
	}
}

func TestMemoryStressDecoderRejectsUncreditedMismatchWithoutAdverseFact(t *testing.T) {
	v := memoryEvidencePass()
	v.Status = "fail"
	v.StopReason = "memory_mismatch"
	v.StopFacts = quick.MemoryStressStopFacts{}
	v.LoadStoppedMilliseconds = memoryEvidenceTime(4)
	v.ObservedLoadMilliseconds = 1
	v.TerminalStartedMilliseconds = nil
	v.TerminalStoppedMilliseconds = nil
	v.FinishedMilliseconds = 5
	v.Coverage = quick.MemoryStressCoverage{CompletedPhases: 1, PhaseBytes: 3, ReadBytes: 3, WrittenBytes: 4194304}
	v.Mismatch = &quick.MemoryStressMismatch{Stage: "cycle", Phase: 1, Offset: 3, ExpectedByte: 0, ActualByte: 1}
	requireMemoryEvidence(t, v)
	v.Coverage.PhaseBytes = 0
	v.Coverage.ReadBytes = 0
	v.Coverage.UncreditedBytes = 3
	if _, _, err := DecodeMemoryStress(memoryEvidenceBytes(t, v)); !errors.Is(err, ErrInvalidMemoryStress) {
		t.Fatal("timely prefix moved to uncredited without a stop")
	}
	v.StopFacts.ParentCancelled = true
	requireMemoryEvidence(t, v)
}
