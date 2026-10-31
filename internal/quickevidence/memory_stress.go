package quickevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

var ErrInvalidMemoryStress = errors.New("memory stress evidence is incomplete or contradictory")

// DecodeMemoryStress checks complete typed observations. The typed digest does
// not authenticate original file bytes or turn a diagnostic into session authority.
func DecodeMemoryStress(raw []byte) (quick.MemoryStressResult, string, error) {
	var v quick.MemoryStressResult
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) ||
		!cpuStressShape(raw, reflect.TypeOf(v), 0) || json.Unmarshal(raw, &v) != nil {
		return quick.MemoryStressResult{}, "", ErrInvalidMemoryStress
	}
	if quickCleanupUnconfirmed(v.Safety) || v.StopFacts.CleanupUnconfirmed || !v.Availability.CloseConfirmed {
		return quick.MemoryStressResult{}, "", quick.ErrMemoryStressCleanupUnconfirmed
	}
	if !validMemoryStress(v) {
		return quick.MemoryStressResult{}, "", ErrInvalidMemoryStress
	}
	canonical, err := json.Marshal(v)
	if err != nil {
		return quick.MemoryStressResult{}, "", ErrInvalidMemoryStress
	}
	material := append([]byte("hhsoft.pc-tester.memory-stress-evidence/v1\x00"), canonical...)
	digest := sha256.Sum256(material)
	return v, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validMemoryStress(v quick.MemoryStressResult) bool {
	f, c, a := v.StopFacts, v.Coverage, v.Availability
	if v.Schema != quick.MemoryStressSchema || v.Suite != quick.MemorySuite ||
		v.RequestedLoadMilliseconds != 1800000 || v.LocalWatchdogMilliseconds != 1860000 ||
		v.ThermalPolicy != "available-observations/v1" || !v.AccountingValid ||
		f.ClockInvalid || f.CounterOverflow || f.WorkerPanic ||
		v.RequestedBytes != 0 && (v.RequestedBytes < 4<<20 || v.RequestedBytes > 64<<20) ||
		v.EffectiveDeadlineMilliseconds < 0 || v.EffectiveDeadlineMilliseconds > 1860000 ||
		!stressTime(v.FinishedMilliseconds) || !quick.ValidMemoryStressCoverage(v) ||
		!validMemoryStressAvailability(v) || !validMemoryStressSafety(v) {
		return false
	}
	switch v.EffectiveDeadlineSource {
	case "watchdog", "both":
		if v.EffectiveDeadlineMilliseconds != 1860000 {
			return false
		}
	case "parent":
		if v.EffectiveDeadlineMilliseconds >= 1860000 {
			return false
		}
	default:
		return false
	}
	if v.FinishedMilliseconds >= 1860000 && !f.WatchdogDeadline ||
		v.EffectiveDeadlineSource == "parent" && v.FinishedMilliseconds > v.EffectiveDeadlineMilliseconds && !f.ParentDeadline ||
		f.WatchdogDeadline && v.FinishedMilliseconds < 1860000 ||
		(f.ParentDeadline || f.WatchdogDeadline) && v.FinishedMilliseconds < v.EffectiveDeadlineMilliseconds ||
		f.WatchdogDeadline && v.EffectiveDeadlineSource == "parent" && !f.ParentDeadline ||
		f.ParentDeadline && v.EffectiveDeadlineSource == "watchdog" && !f.WatchdogDeadline ||
		v.EffectiveDeadlineSource == "both" && f.ParentDeadline != f.WatchdogDeadline {
		return false
	}
	last := int64(0)
	for _, point := range []*int64{v.AvailabilityFinishedMilliseconds, v.AllocationFinishedMilliseconds,
		v.LoadStartedMilliseconds, v.LoadStoppedMilliseconds, v.TerminalStartedMilliseconds, v.TerminalStoppedMilliseconds} {
		if point != nil {
			if !stressTime(*point) || *point < last || *point > v.FinishedMilliseconds {
				return false
			}
			last = *point
		}
	}
	if (v.AvailabilityFinishedMilliseconds != nil) != a.Started ||
		(v.AllocationFinishedMilliseconds != nil) != (v.AllocatedBytes != 0) ||
		v.AllocatedBytes > 0 && (!a.Valid || a.Failure != "" || v.AvailableBytes/4 < v.AllocatedBytes) {
		return false
	}
	if v.AllocatedBytes > 0 {
		selected := v.RequestedBytes
		if selected == 0 {
			selected = min(uint64(64<<20), v.AvailableBytes/4) / (1 << 20) * (1 << 20)
		}
		if v.AllocatedBytes != selected {
			return false
		}
	}
	if v.LoadStartedMilliseconds == nil {
		if v.LoadStoppedMilliseconds != nil || v.ObservedLoadMilliseconds != 0 ||
			v.TerminalStartedMilliseconds != nil || v.TerminalStoppedMilliseconds != nil ||
			c != (quick.MemoryStressCoverage{}) || v.Mismatch != nil || f.NormalLoadComplete {
			return false
		}
	} else {
		if v.AllocatedBytes == 0 || v.LoadStoppedMilliseconds == nil ||
			v.ObservedLoadMilliseconds != min(int64(1800000), *v.LoadStoppedMilliseconds-*v.LoadStartedMilliseconds) ||
			*v.LoadStartedMilliseconds+1800000 >= v.EffectiveDeadlineMilliseconds {
			return false
		}
	}
	if f.InsufficientBudget && (v.LoadStartedMilliseconds != nil || v.FinishedMilliseconds+1800000 < v.EffectiveDeadlineMilliseconds) {
		return false
	}
	if f.NormalLoadComplete && (v.LoadStartedMilliseconds == nil || v.LoadStoppedMilliseconds == nil ||
		*v.LoadStoppedMilliseconds < *v.LoadStartedMilliseconds+1800000 || v.ObservedLoadMilliseconds != 1800000) {
		return false
	}
	if (v.TerminalStartedMilliseconds == nil) != (v.TerminalStoppedMilliseconds == nil) {
		return false
	}
	if v.TerminalStartedMilliseconds == nil {
		if c.TerminalReadBytes != 0 || c.TerminalUncreditedBytes != 0 {
			return false
		}
	} else if !f.NormalLoadComplete || c.CompletedCycles == 0 || c.CompletedPhases%10 != 0 || c.PhaseBytes != 0 || c.UncreditedBytes != 0 {
		return false
	}
	if !validMemoryStressMismatch(v) {
		return false
	}
	adverse := f.ParentCancelled || f.ParentDeadline || f.WatchdogDeadline || f.ThermalLimit ||
		f.CoverageLost || f.ObservationTimeout || f.ObserverPanic || f.InvalidObservation
	if (c.UncreditedBytes != 0 || c.TerminalUncreditedBytes != 0 || a.Failure == "stopped") && !adverse {
		return false
	}
	if v.Mismatch == nil {
		if c.PhaseBytes%(256<<10) != 0 ||
			c.TerminalReadBytes != v.AllocatedBytes && c.TerminalReadBytes%(256<<10) != 0 ||
			c.UncreditedBytes != 0 && (c.UncreditedBytes != min(uint64(256<<10), v.AllocatedBytes-c.PhaseBytes) || !adverse) ||
			c.TerminalUncreditedBytes != 0 && (c.TerminalUncreditedBytes != min(uint64(256<<10), v.AllocatedBytes-c.TerminalReadBytes) || !adverse) {
			return false
		}
	}
	status, reason := quick.MemoryStressOutcome(v)
	return v.Status == status && v.StopReason == reason
}

func validMemoryStressAvailability(v quick.MemoryStressResult) bool {
	a := v.Availability
	if !a.CloseConfirmed || a.RawBytes > 65537 || a.ReadCalls > 65537 ||
		a.RawBytes > a.ReadCalls*4096 || a.ReadCalls > a.RawBytes+1 {
		return false
	}
	if !a.Started {
		return a == (quick.MemoryStressAvailability{CloseConfirmed: true}) && v.AvailableBytes == 0
	}
	if !a.Opened && (a.RawBytes != 0 || a.ReadCalls != 0 || a.EOF || a.Valid) {
		return false
	}
	if a.Opened && a.ReadCalls == 0 && a.Failure != "open_failed" && a.Failure != "stopped" && a.Failure != "reader_panic" {
		return false
	}
	if a.Valid {
		return a.Opened && a.EOF && a.RawBytes > 0 && a.RawBytes <= 65536 && a.ReadCalls > 0 &&
			v.AvailableBytes > 0 && v.AvailableBytes%1024 == 0 &&
			(a.Failure == "" || a.Failure == "insufficient" && v.AllocatedBytes == 0 &&
				(v.AvailableBytes/4 < 4<<20 || v.RequestedBytes > v.AvailableBytes/4))
	}
	if v.AvailableBytes != 0 || v.AllocatedBytes != 0 {
		return false
	}
	switch a.Failure {
	case "open_failed":
		return a.RawBytes == 0 && a.ReadCalls == 0 && !a.EOF
	case "stopped", "reader_panic":
		return !a.EOF && a.RawBytes <= 65536
	case "read_failed":
		return a.Opened && a.ReadCalls > 0 && !a.EOF && a.RawBytes <= 65536
	case "too_large":
		return a.Opened && !a.EOF && a.RawBytes == 65537 && a.ReadCalls > 0
	case "invalid":
		return a.Opened && a.EOF && a.ReadCalls > 0 && a.RawBytes <= 65536
	default:
		return false
	}
}

func validMemoryStressMismatch(v quick.MemoryStressResult) bool {
	m, c := v.Mismatch, v.Coverage
	if m == nil {
		return true
	}
	if v.LoadStartedMilliseconds == nil || v.AllocatedBytes == 0 || m.Offset >= v.AllocatedBytes ||
		m.ExpectedByte < 0 || m.ExpectedByte > 255 || m.ActualByte < 0 || m.ActualByte > 255 ||
		m.ExpectedByte == m.ActualByte || m.Cycle != c.CompletedCycles {
		return false
	}
	expected := byte(0xaa)
	var position uint64
	switch m.Stage {
	case "cycle":
		if m.Phase != int(c.CompletedPhases%10) || m.Phase != 1 && m.Phase != 3 && m.Phase != 5 && m.Phase != 7 && m.Phase != 8 && m.Phase != 9 {
			return false
		}
		position = c.PhaseBytes + c.UncreditedBytes
		if position >= v.AllocatedBytes {
			return false
		}
		if m.Phase == 8 {
			position = v.AllocatedBytes - 1 - position
			expected = 0x55
		} else if m.Phase == 1 {
			expected = byte(m.Cycle % 256)
		} else if m.Phase == 3 {
			expected = ^byte(m.Cycle % 256)
		} else if m.Phase == 5 {
			expected = byte(m.Offset*0x9e3779b97f4a7c15>>56) ^ byte((m.Cycle%256)*67)
		}
	case "terminal":
		if m.Phase != 9 || v.TerminalStartedMilliseconds == nil {
			return false
		}
		position = c.TerminalReadBytes + c.TerminalUncreditedBytes
	default:
		return false
	}
	return position == m.Offset && int(expected) == m.ExpectedByte
}

func validMemoryStressSafety(v quick.MemoryStressResult) bool {
	s, f := v.Safety, v.StopFacts
	if s.GlobalTimeoutMilliseconds != 1860000 || s.ThermalLimitMilliCelsius != 90000 ||
		s.PeakMilliCelsius < 0 || s.PeakMilliCelsius > 250000 {
		return false
	}
	for _, count := range []int{s.ThermalObservations, s.ThermalSensors, s.ThermalSamples, s.ThermalRejectedReadings, s.ThermalUnavailableReadings, s.ThermalEnumerationErrors} {
		if count < 0 || int64(count) > 2147483647 {
			return false
		}
	}
	o, sensors, samples := int64(s.ThermalObservations), int64(s.ThermalSensors), int64(s.ThermalSamples)
	if samples < sensors || sensors == 0 && (samples != 0 || s.PeakMilliCelsius != 0) || sensors > 0 && o == 0 {
		return false
	}
	if sensors > 0 {
		minimum := samples / sensors
		if samples%sensors != 0 {
			minimum++
		}
		if minimum > o {
			return false
		}
	}
	if o == 0 && (sensors != 0 || samples != 0 || s.PeakMilliCelsius != 0 || s.ThermalRejectedReadings != 0 || s.ThermalUnavailableReadings != 0 || s.ThermalEnumerationErrors != 0 || s.ThermalCoverage != "unavailable") {
		return false
	}
	switch s.ThermalCoverage {
	case "available":
		if sensors == 0 || s.ThermalRejectedReadings != 0 || s.ThermalUnavailableReadings != 0 || s.ThermalEnumerationErrors != 0 || samples-sensors < o-1 {
			return false
		}
	case "unavailable":
		if sensors != 0 || s.ThermalRejectedReadings != 0 || s.ThermalEnumerationErrors != 0 {
			return false
		}
	case "partial":
		if s.ThermalRejectedReadings == 0 && s.ThermalUnavailableReadings == 0 && s.ThermalEnumerationErrors == 0 {
			if sensors == 0 || o < 2 || samples/sensors > o-1 || samples/sensors == o-1 && samples%sensors != 0 {
				return false
			}
		} else if sensors == 0 && s.ThermalRejectedReadings == 0 && s.ThermalEnumerationErrors == 0 {
			return false
		}
	default:
		return false
	}
	if f.ThermalLimit != (samples > 0 && s.PeakMilliCelsius >= 90000) ||
		f.InitialCoveragePartial && (o != 1 || s.ThermalCoverage != "partial") ||
		f.InitialCoverageUnavailable && (o != 1 || s.ThermalCoverage != "unavailable") ||
		f.CoverageLost && (o < 2 || s.ThermalCoverage != "partial") ||
		(v.Availability.Started || v.AllocatedBytes > 0) && (o == 0 || f.InitialCoveragePartial || f.InitialCoverageUnavailable || o == 1 && f.ThermalLimit) {
		return false
	}
	if o > 0 && s.ThermalCoverage == "unavailable" && !f.InitialCoverageUnavailable ||
		s.ThermalCoverage == "partial" && !f.InitialCoveragePartial && !f.CoverageLost {
		return false
	}
	hard := f.ObservationTimeout || f.ObserverPanic || f.InvalidObservation
	coverage := f.CoverageLost || f.InitialCoveragePartial || f.InitialCoverageUnavailable
	switch s.MonitorStatus {
	case "not_started":
		return o == 0 && s.MonitorFailure == "" && v.AllocatedBytes == 0 && v.LoadStartedMilliseconds == nil && !hard && !coverage && !f.ThermalLimit &&
			(f.InsufficientBudget || f.ParentCancelled || f.ParentDeadline || f.WatchdogDeadline)
	case "completed":
		return o > 0 && s.MonitorFailure == "" && s.ThermalCoverage == "available" && !hard && !coverage && !f.ThermalLimit
	case "failed":
		switch s.MonitorFailure {
		case "thermal_limit":
			return f.ThermalLimit
		case "coverage_loss":
			return coverage
		case "observation_timeout":
			return f.ObservationTimeout
		case "observer_panic":
			return f.ObserverPanic
		case "invalid_observation":
			return f.InvalidObservation
		default:
			return false
		}
	default:
		return false
	}
}
