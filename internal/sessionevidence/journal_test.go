package sessionevidence

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
)

// Initial telemetry completes at 5ms, final at 99ms; both owners join at 100ms.
// This short all-off run still has two observations, despite no periodic tick.
func shortJournal() Journal {
	safe := safemode.Result{Schema: safemode.Schema, Status: "pass", StorageObserved: true, NetworkObserved: true}
	unavailable := quick.ThermalSnapshot{Coverage: quick.ThermalUnavailable}
	j := Journal{Schema: SchemaV5, PlanID: "sha256:" + strings.Repeat("a", 64), StopReason: "completed", BudgetMilliseconds: 1000, ElapsedMilliseconds: 100,
		PreparationStartedMilliseconds: 80, InventoryPreparedMilliseconds: 90, StopRequestedMilliseconds: 100, SafetyJoinedMilliseconds: 100,
		Safety: []SafetyRecord{{"preflight", safe}, {"final", safe}, {"pre-sign", safe}}, LocalThermalSources: []LocalThermalSource{},
		Monitor: safemode.MonitorResult{Schema: safemode.MonitorSchema, Status: "pass", IntervalMilliseconds: 1000, ObservationTimeoutMilliseconds: 1000, MaximumAllowedGapMilliseconds: 2500, ElapsedMilliseconds: 100, MaximumGapMilliseconds: 100, CleanupConfirmed: true},
		Thermal: thermal.Result{Schema: thermal.Schema, Status: "completed", LimitMilliCelsius: 90000, IntervalMilliseconds: 1000, ObservationTimeoutMilliseconds: 1000, MaximumAllowedGapMilliseconds: 2500, StopTimeoutMilliseconds: 2000,
			AttemptedObservations: 2, CompletedObservations: 2, ElapsedMilliseconds: 100, FirstCompletedMilliseconds: 5, LastCompletedMilliseconds: 99, MaximumGapMilliseconds: 94,
			Initial:   thermal.PhaseRecord{Attempted: true, Completed: true, CompletedMilliseconds: 5, Observation: &unavailable},
			Final:     thermal.PhaseRecord{RequestedMilliseconds: 90, Attempted: true, Completed: true, StartedMilliseconds: 95, CompletedMilliseconds: 99, Observation: &unavailable},
			Aggregate: thermal.Aggregate{Coverage: quick.ThermalUnavailable, AcceptedObservations: 2, UnavailableObservations: 2}, CleanupConfirmed: true, Joined: true, JoinedMilliseconds: 100, LastObservation: &unavailable}}
	for _, id := range []string{"cpu.quick", "memory.quick", "audio.readiness.quick", "input.readiness.quick", "input.keyboard.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick", "graphics.quick", "display.operator.quick"} {
		j.Workers = append(j.Workers, WorkerRecord{ID: id, State: "not_scheduled", Outcome: "excluded"})
	}
	return j
}

func shortBindings(j Journal) []ResultBinding {
	rows := make([]ResultBinding, 0, len(j.Workers))
	for _, w := range j.Workers {
		raw, _ := json.Marshal(executionNote{Schema: "hhsoft.pc-tester.execution-note/v1", Status: reportStatus(w.Outcome), SourceStatus: w.Outcome, Detail: "not run"})
		version := 1
		if w.ID == "input.keyboard.quick" {
			version = 2
		}
		rows = append(rows, ResultBinding{ID: w.ID, Version: version, PlannedStatus: "excluded", ReportStatus: reportStatus(w.Outcome), Evidence: raw})
	}
	return rows
}

func TestImmediateAllOffRequiresBothThermalEdgesAndBothJoins(t *testing.T) {
	j := shortJournal()
	if err := ValidateBindings(j, shortBindings(j)); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(j)
	if _, err := Decode(raw); err != nil {
		t.Fatal(err)
	}
	view := ThermalViewOf(j)
	if view.Finding != "no_limit_observed" || view.Coverage != "unavailable" || view.PeakObserved || view.AcceptedObservations != 2 || !view.CleanupConfirmed {
		t.Fatal("unavailable telemetry overstated")
	}
	cases := map[string]func(*Journal){
		"initial omitted":           func(j *Journal) { j.Thermal.Initial.Completed = false },
		"final omitted":             func(j *Journal) { j.Thermal.Final.Completed = false },
		"initial after preparation": func(j *Journal) { j.PreparationStartedMilliseconds = 4 },
		"final before inventory":    func(j *Journal) { j.Thermal.Final.RequestedMilliseconds = 89 },
		"stop before final":         func(j *Journal) { j.StopRequestedMilliseconds = 98 },
		"safety joined early":       func(j *Journal) { j.SafetyJoinedMilliseconds = 99 },
		"thermal joined early":      func(j *Journal) { j.Thermal.JoinedMilliseconds = 99 },
		"thermal not joined":        func(j *Journal) { j.Thermal.Joined = false },
		"unconfirmed cleanup":       func(j *Journal) { j.Thermal.CleanupConfirmed = false },
		"truncated common span":     func(j *Journal) { j.ElapsedMilliseconds = 99 },
		"extra cleanup window":      func(j *Journal) { j.ElapsedMilliseconds = 2101 },
		"invented heat":             func(j *Journal) { j.StopReason = "thermal_limit" },
		"nil local sources":         func(j *Journal) { j.LocalThermalSources = nil },
		"invented timing":           func(j *Journal) { j.Workers[0].Timing = &WorkerTiming{} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			value := CloneJournal(j)
			change(&value)
			if ValidateStructure(value) == nil {
				t.Fatal("contradictory journal accepted")
			}
		})
	}
}

const hotCPU = `{"schema":"hhsoft.pc-tester.cpu/v2","status":"aborted","workers":1,"iterations":0,"state_aggregate":0,"known_answer_sha256":"","duration_ms":10,"safety":{"global_timeout_ms":1000,"thermal_limit_millicelsius":90000,"thermal_sensors":1,"thermal_samples":1,"peak_millicelsius":95000,"monitor_status":"failed","monitor_failure":"thermal_limit","cleanup_confirmed":true,"thermal_observations":1,"thermal_coverage":"available","thermal_rejected_readings":0,"thermal_unavailable_readings":0,"thermal_enumeration_errors":0}}`

func hotJournal(t *testing.T) (Journal, []ResultBinding) {
	t.Helper()
	j := shortJournal()
	j.StopReason = "thermal_limit"
	j.Workers[0] = WorkerRecord{ID: "cpu.quick", State: "completed", Outcome: "aborted", Timing: &WorkerTiming{10, 60}}
	j.Safety = append(j.Safety[:1], append([]SafetyRecord{{"before-cpu.quick", j.Safety[0].Result}}, j.Safety[1:]...)...)
	local, err := quickevidence.DecodeLocal("cpu.quick", []byte(hotCPU))
	if err != nil {
		t.Fatal(err)
	}
	j.LocalThermalSources = []LocalThermalSource{{WorkerID: "cpu.quick", EvidenceSchema: local.Schema, EvidenceFileSHA256: "sha256:" + strings.Repeat("b", 64), EvidenceTypedSHA256: local.TypedSHA256, PeakMilliCelsius: 95000, LimitMilliCelsius: 90000, StartedMilliseconds: 10, FinishedMilliseconds: 60}}
	rows := shortBindings(j)
	rows[0] = ResultBinding{ID: "cpu.quick", PlannedStatus: "scheduled", ReportStatus: "aborted", Evidence: []byte(hotCPU)}
	return j, rows
}

func TestLocalHeatIsBoundToFullTypedEvidenceAndWorkerInterval(t *testing.T) {
	j, rows := hotJournal(t)
	if err := ValidateBindings(j, rows); err != nil {
		t.Fatal(err)
	}
	view := ThermalViewOf(j)
	if view.Finding != "limit_reached" || view.Coverage != "unavailable" || view.PeakObserved || len(view.LocalSources) != 1 {
		t.Fatal("local heat changed global coverage")
	}
	mutations := map[string]func(*Journal, []ResultBinding){
		"rehashed duration exceeds worker": func(j *Journal, r []ResultBinding) {
			r[0].Evidence = bytes.Replace(r[0].Evidence, []byte(`"duration_ms":10`), []byte(`"duration_ms":51`), 1)
			local, err := quickevidence.DecodeLocal("cpu.quick", r[0].Evidence)
			if err != nil {
				t.Fatal(err)
			}
			j.LocalThermalSources[0].EvidenceTypedSHA256 = local.TypedSHA256
		},
		"missing source": func(j *Journal, _ []ResultBinding) {
			j.LocalThermalSources = []LocalThermalSource{}
			j.StopReason = "worker_stop"
		},
		"wrong typed digest": func(j *Journal, _ []ResultBinding) {
			j.LocalThermalSources[0].EvidenceTypedSHA256 = "sha256:" + strings.Repeat("c", 64)
		},
		"invalid file digest": func(j *Journal, _ []ResultBinding) { j.LocalThermalSources[0].EvidenceFileSHA256 = "sha256:bad" },
		"wrong worker":        func(j *Journal, _ []ResultBinding) { j.LocalThermalSources[0].WorkerID = "memory.quick" },
		"wrong interval":      func(j *Journal, _ []ResultBinding) { j.LocalThermalSources[0].FinishedMilliseconds++ },
		"changed full evidence": func(_ *Journal, r []ResultBinding) {
			r[0].Evidence = bytes.Replace(r[0].Evidence, []byte(`"duration_ms":10`), []byte(`"duration_ms":11`), 1)
		},
		"changed peak": func(_ *Journal, r []ResultBinding) {
			r[0].Evidence = bytes.Replace(r[0].Evidence, []byte("95000"), []byte("94000"), 1)
		},
		"duplicate row": func(_ *Journal, r []ResultBinding) { r[1] = r[0] },
		"excluded work": func(_ *Journal, r []ResultBinding) { r[0].PlannedStatus = "excluded" },
		"false pass":    func(_ *Journal, r []ResultBinding) { r[0].ReportStatus = "pass" },
	}
	for name, change := range mutations {
		t.Run(name, func(t *testing.T) {
			value := CloneJournal(j)
			copyRows := append([]ResultBinding(nil), rows...)
			change(&value, copyRows)
			if ValidateBindings(value, copyRows) == nil {
				t.Fatal("false binding accepted")
			}
		})
	}
	// The original file hash is deliberately not reconstructed from report JSON.
	var pretty bytes.Buffer
	_ = json.Indent(&pretty, rows[0].Evidence, "", "  ")
	rows[0].Evidence = pretty.Bytes()
	if ValidateBindings(j, rows) != nil {
		t.Fatal("equivalent typed encoding rejected")
	}
	copyValue := CloneJournal(j)
	copyValue.Workers[0].Timing.StartedMilliseconds = 999
	copyValue.Thermal.Initial.Observation.Coverage = "changed"
	copyValue.Safety[0].Phase = "changed"
	copyValue.LocalThermalSources[0].PeakMilliCelsius = 0
	if j.Workers[0].Timing.StartedMilliseconds != 10 || j.Thermal.Initial.Observation.Coverage != "unavailable" || j.Safety[0].Phase != "preflight" || j.LocalThermalSources[0].PeakMilliCelsius != 95000 {
		t.Fatal("clone aliases live evidence")
	}
}

func TestIntegrityDecoderRejectsContradictoryCountsAndAmbiguousNestedClaims(t *testing.T) {
	j := shortJournal()
	value := IntegrityEvidence{Schema: IntegritySchema, Status: "pass", PlanID: j.PlanID, PlannedHardwareGraph: "hmac-sha256:" + strings.Repeat("d", 64), FinalHardwareGraph: "hmac-sha256:" + strings.Repeat("d", 64), PlannedComponentCount: 3, FinalComponentCount: 3, Session: &j}
	raw, _ := json.Marshal(value)
	if _, err := DecodeIntegrity(raw); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func([]byte) []byte{
		"oversized nested journal": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"session":{`), append([]byte(`"session":{`), bytes.Repeat([]byte{' '}, MaximumBytes)...), 1)
		},
		"count": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"final_component_count":3`), []byte(`"final_component_count":2`), 1)
		},
		"false graph failure": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"status":"pass"`), []byte(`"status":"fail"`), 1)
		},
		"nested duplicate": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"thermal":`), []byte(`"thermal":{},"thermal":`), 1)
		},
		"nested case alias": func(b []byte) []byte { return bytes.Replace(b, []byte(`"thermal":`), []byte(`"Thermal":`), 1) },
		"missing cleanup":   func(b []byte) []byte { return bytes.Replace(b, []byte(`"cleanup_confirmed":true,`), nil, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			altered := change(raw)
			if bytes.Equal(altered, raw) {
				t.Fatal("fixture unchanged")
			}
			if _, err := DecodeIntegrity(altered); err == nil {
				t.Fatal("invalid integrity accepted")
			}
		})
	}
}

func TestJournalCannotExecuteAfterLatchedHeatOrWorkerStop(t *testing.T) {
	j, _ := hotJournal(t)
	for _, outcome := range []string{"fail", "aborted", "error", "inconclusive"} {
		t.Run("after-cpu-"+outcome, func(t *testing.T) {
			value := CloneJournal(j)
			value.Workers[0].Outcome = outcome
			value.Workers[1] = WorkerRecord{ID: "memory.quick", State: "completed", Outcome: "pass", Timing: &WorkerTiming{60, 70}}
			value.Safety = append(value.Safety[:2], append([]SafetyRecord{{"before-memory.quick", value.Safety[0].Result}}, value.Safety[2:]...)...)
			if ValidateStructure(value) == nil {
				t.Fatal("later worker ran after stop")
			}
		})
	}
	value := shortJournal()
	hot := quick.ThermalSnapshot{Coverage: quick.ThermalAvailable, Sensors: 1, PeakMilliCelsius: 95000}
	value.StopReason = "thermal_limit"
	value.Thermal.Initial.Observation = &hot
	value.Thermal.Aggregate = thermal.Aggregate{Coverage: "partial", AcceptedObservations: 2, AvailableObservations: 1, UnavailableObservations: 1, MaximumSensors: 1, ValidReadings: 1, PeakMilliCelsius: 95000}
	value.Thermal.FirstFinding = &thermal.FindingRecord{Reason: "thermal_limit", Phase: "initial", ElapsedMilliseconds: 5, Observation: hot}
	if ValidateStructure(value) != nil {
		t.Fatal("honest initial heat rejected")
	}
	value.Workers[0] = WorkerRecord{ID: "cpu.quick", State: "completed", Outcome: "pass", Timing: &WorkerTiming{5, 60}}
	value.Safety = append(value.Safety[:1], append([]SafetyRecord{{"before-cpu.quick", value.Safety[0].Result}}, value.Safety[1:]...)...)
	if ValidateStructure(value) == nil {
		t.Fatal("initial heat authorized a worker")
	}
}

func TestSharedJournalRejectsInterruptedSafetyReadiness(t *testing.T) {
	for _, id := range []string{"network.readiness.quick", "storage.readiness.quick"} {
		for _, state := range []string{"completed", "interrupted"} {
			for _, outcome := range []string{"error", "aborted"} {
				t.Run(id+"/"+state+"/"+outcome, func(t *testing.T) {
					j := shortJournal()
					j.StopReason = "run_deadline"
					for index := range j.Workers {
						if j.Workers[index].ID == id {
							j.Workers[index] = WorkerRecord{ID: id, State: state, Outcome: outcome, Timing: &WorkerTiming{10, 60}}
						}
					}
					j.Safety = append(j.Safety[:1], append([]SafetyRecord{{"before-" + id, j.Safety[0].Result}}, j.Safety[1:]...)...)
					if ValidateStructure(j) == nil {
						t.Fatal("unsafe executed readiness accepted by shared validator")
					}
				})
			}
		}
	}
}
