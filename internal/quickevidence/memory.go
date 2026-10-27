package quickevidence

import (
	"encoding/json"
	"reflect"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

// Immutable historical wire: order, tags and omissions define memory-v2 hashes.
type memoryV2 struct {
	Schema               string         `json:"schema"`
	Status               quick.Status   `json:"status"`
	AllocatedBytes       int64          `json:"allocated_bytes"`
	ExaminedBytes        int64          `json:"examined_bytes"`
	Passes               int            `json:"passes"`
	PatternsPerPass      int            `json:"patterns_per_pass"`
	DurationMilliseconds int64          `json:"duration_ms"`
	Safety               memorySafetyV2 `json:"safety"`
	FailureOffset        int64          `json:"failure_offset,omitempty"`
	ExpectedByte         int            `json:"expected_byte,omitempty"`
	ActualByte           int            `json:"actual_byte,omitempty"`
	Detail               string         `json:"detail,omitempty"`
}

type memorySafetyV2 struct {
	GlobalTimeoutMilliseconds  int64  `json:"global_timeout_ms"`
	ThermalLimitMilliCelsius   int    `json:"thermal_limit_millicelsius"`
	ThermalSensors             int    `json:"thermal_sensors"`
	ThermalSamples             int    `json:"thermal_samples"`
	PeakMilliCelsius           int    `json:"peak_millicelsius,omitempty"`
	MonitorStatus              string `json:"monitor_status"`
	MonitorFailure             string `json:"monitor_failure,omitempty"`
	CleanupConfirmed           bool   `json:"cleanup_confirmed"`
	ThermalObservations        int    `json:"thermal_observations"`
	ThermalCoverage            string `json:"thermal_coverage"`
	ThermalRejectedReadings    int    `json:"thermal_rejected_readings"`
	ThermalUnavailableReadings int    `json:"thermal_unavailable_readings"`
	ThermalEnumerationErrors   int    `json:"thermal_enumeration_errors"`
}

func (v memorySafetyV2) current() quick.SafetyEvidence {
	return quick.SafetyEvidence{GlobalTimeoutMilliseconds: v.GlobalTimeoutMilliseconds, ThermalLimitMilliCelsius: v.ThermalLimitMilliCelsius,
		ThermalSensors: v.ThermalSensors, ThermalSamples: v.ThermalSamples, PeakMilliCelsius: v.PeakMilliCelsius,
		MonitorStatus: v.MonitorStatus, MonitorFailure: v.MonitorFailure, CleanupConfirmed: v.CleanupConfirmed,
		ThermalObservations: v.ThermalObservations, ThermalCoverage: v.ThermalCoverage,
		ThermalRejectedReadings: v.ThermalRejectedReadings, ThermalUnavailableReadings: v.ThermalUnavailableReadings,
		ThermalEnumerationErrors: v.ThermalEnumerationErrors}
}

// MemorySummary is copied only after full evidence validation. Historical v2
// has Present=false and never acquires inversion coverage by reinterpretation.
type MemorySummary struct {
	Present                                                        bool
	Suite                                                          string
	AllocatedBytes                                                 int64
	Passes, CompletedPasses, CompletedPhases                       int
	PhaseBytes, UncreditedBytes                                    int64
	ExaminedBytes, WrittenBytes                                    int64
	ObservedMatchedBytes, AttemptedReadBytes, ObservedWrittenBytes int64
	CurrentPass                                                    int
	Phase, Direction                                               string
	Mismatch                                                       *quick.MemoryMismatch
}

func decodeMemory(encoded []byte) (LocalEvidence, []byte, error) {
	var header struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(encoded, &header) != nil {
		return LocalEvidence{}, nil, ErrInvalid
	}
	if header.Schema == quick.MemorySchemaV2 {
		var old memoryV2
		if evidencejson.ValidateShape(encoded, reflect.TypeOf(old)) != nil || json.Unmarshal(encoded, &old) != nil || !validStatus(old.Status) {
			return LocalEvidence{}, nil, ErrInvalid
		}
		if quickCleanupUnconfirmed(old.Safety.current()) {
			return LocalEvidence{}, nil, ErrCleanupUnconfirmed
		}
		if old.ExpectedByte != old.ActualByte && old.Status != quick.StatusFail {
			return LocalEvidence{}, nil, ErrFailureDowngrade
		}
		if !validateMemoryEvidence(old) {
			return LocalEvidence{}, nil, ErrInvalid
		}
		canonical, _ := json.Marshal(old)
		return LocalEvidence{Schema: old.Schema, Status: old.Status, Safety: old.Safety.current(), DurationMilliseconds: old.DurationMilliseconds}, canonical, nil
	}
	var value quick.MemoryResult
	if header.Schema != quick.MemorySchema || evidencejson.ValidateShape(encoded, reflect.TypeOf(value)) != nil || json.Unmarshal(encoded, &value) != nil || !validStatus(value.Status) {
		return LocalEvidence{}, nil, ErrInvalid
	}
	if quickCleanupUnconfirmed(value.Safety) {
		return LocalEvidence{}, nil, ErrCleanupUnconfirmed
	}
	if value.Mismatch != nil && value.Status != quick.StatusFail {
		return LocalEvidence{}, nil, ErrFailureDowngrade
	}
	summary, valid := validateMemoryCoverage(value)
	if !valid || !validQuickSafety(value.Safety, value.Status, value.DurationMilliseconds, 1000) {
		return LocalEvidence{}, nil, ErrInvalid
	}
	canonical, _ := json.Marshal(value)
	return LocalEvidence{Schema: value.Schema, Status: value.Status, Safety: value.Safety,
		DurationMilliseconds: value.DurationMilliseconds, Memory: summary}, canonical, nil
}

func validateMemoryCoverage(v quick.MemoryResult) (MemorySummary, bool) {
	c := v.Coverage
	invalid := MemorySummary{}
	if c.Suite != quick.MemorySuite || v.PatternsPerPass != 3 || v.DurationMilliseconds < 0 ||
		v.AllocatedBytes < 0 || v.Passes < 0 || v.Passes > 8 || c.CompletedPhases < 0 || c.CompletedPhases > 10*v.Passes ||
		c.CompletedPasses != c.CompletedPhases/10 || c.PhaseBytes < 0 || c.UncreditedBytes < 0 || c.WrittenBytes < 0 || v.ExaminedBytes < 0 ||
		(v.Status == quick.StatusFail) != (v.Mismatch != nil) {
		return invalid, false
	}
	summary := MemorySummary{Present: true, Suite: c.Suite, AllocatedBytes: v.AllocatedBytes, Passes: v.Passes,
		CompletedPasses: c.CompletedPasses, CompletedPhases: c.CompletedPhases, PhaseBytes: c.PhaseBytes, UncreditedBytes: c.UncreditedBytes,
		ExaminedBytes: v.ExaminedBytes, WrittenBytes: c.WrittenBytes, Phase: "not_started", Direction: "none"}
	if v.AllocatedBytes == 0 {
		return summary, c.CompletedPhases == 0 && c.PhaseBytes == 0 && c.UncreditedBytes == 0 && c.WrittenBytes == 0 && v.ExaminedBytes == 0 &&
			v.DurationMilliseconds == 0 && v.Mismatch == nil && v.Status != quick.StatusPass &&
			(v.Passes != 0 || v.Safety.MonitorStatus == "not_started" && (v.Status == quick.StatusError || v.Status == quick.StatusAborted))
	}
	if v.AllocatedBytes < 4<<20 || v.AllocatedBytes > 512<<20 || v.Passes == 0 || v.Safety.ThermalObservations == 0 {
		return invalid, false
	}
	// Bounds above prove all products below fit int64, including on 386.
	n, b, u := v.AllocatedBytes, c.PhaseBytes, c.UncreditedBytes
	if c.CompletedPhases == 10*v.Passes {
		summary.CurrentPass, summary.Phase = v.Passes, "complete"
		summary.ObservedMatchedBytes, summary.AttemptedReadBytes, summary.ObservedWrittenBytes = v.ExaminedBytes, v.ExaminedBytes, c.WrittenBytes
		return summary, b == 0 && u == 0 && v.Mismatch == nil && v.ExaminedBytes == 6*n*int64(v.Passes) && c.WrittenBytes == v.ExaminedBytes
	}
	if v.Status == quick.StatusPass || b >= n || u > min(int64(256<<10), n-b) {
		return invalid, false
	}
	j, p := c.CompletedPhases%10, c.CompletedPhases/10
	reads := [...]int64{0, 0, 1, 1, 2, 2, 3, 3, 4, 5, 6}
	writes := [...]int64{0, 1, 1, 2, 2, 3, 3, 4, 5, 6, 6}
	r, w := reads[j+1]-reads[j], writes[j+1]-writes[j]
	if v.ExaminedBytes != n*(6*int64(p)+reads[j])+r*b || c.WrittenBytes != n*(6*int64(p)+writes[j])+w*b {
		return invalid, false
	}
	if v.Mismatch == nil {
		if b%(256<<10) != 0 || u != 0 && u != min(int64(256<<10), n-b) {
			return invalid, false
		}
	} else {
		m := v.Mismatch
		if r == 0 || b+u >= n || u > 0 && (b%(256<<10) != 0 || u >= min(int64(256<<10), n-b)) ||
			m.ExpectedByte < 0 || m.ExpectedByte > 255 || m.ActualByte < 0 || m.ActualByte > 255 || m.ExpectedByte == m.ActualByte {
			return invalid, false
		}
		offset := b + u
		if j == 8 {
			offset = n - 1 - offset
		}
		expected := byte(0xaa)
		switch j {
		case 1:
			expected = byte(p)
		case 3:
			expected = ^byte(p)
		case 5:
			expected = byte(uint64(offset)*0x9e3779b97f4a7c15>>56) ^ byte(p*67)
		case 8:
			expected = 0x55
		}
		if m.Offset != offset || m.ExpectedByte != int(expected) {
			return invalid, false
		}
		copy := *m
		summary.Mismatch = &copy
	}
	phaseNames := [...]string{"pattern0_fill", "pattern0_verify", "pattern1_fill", "pattern1_verify", "pattern2_fill", "pattern2_verify", "inversion_initialize", "inversion_forward", "inversion_reverse", "inversion_final"}
	summary.CurrentPass, summary.Phase, summary.Direction = p, phaseNames[j], "ascending"
	if j == 8 {
		summary.Direction = "descending"
	}
	summary.ObservedMatchedBytes = v.ExaminedBytes + r*u
	summary.AttemptedReadBytes = summary.ObservedMatchedBytes
	if v.Mismatch != nil {
		summary.AttemptedReadBytes++
	}
	summary.ObservedWrittenBytes = c.WrittenBytes + w*u
	return summary, true
}
