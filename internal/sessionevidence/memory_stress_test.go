package sessionevidence_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
)

const extendedMemoryOrder = "cpu.quick memory.quick memory.stress audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"

func extendedMemoryJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func extendedMemoryHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func extendedMemoryTime(value int64) *int64 { return &value }

func extendedMemoryAllOff(t *testing.T) (sessionevidence.Journal, []sessionevidence.ResultBinding) {
	t.Helper()
	// Reuse a validated unrelated monitor fixture, but build the new worker
	// oracle from a literal order rather than any current production selector.
	j, _, _ := touchscreenSessionAllOff(t, "v11")
	j.Schema, j.Profile, j.BudgetMilliseconds = "hhsoft.pc-tester.quick-session/v13", "extended", 2700000
	j.Workers = nil
	rows := []sessionevidence.ResultBinding{}
	for _, id := range strings.Fields(extendedMemoryOrder) {
		j.Workers = append(j.Workers, sessionevidence.WorkerRecord{ID: id, State: "not_scheduled", Outcome: "excluded"})
		version := 1
		switch id {
		case "memory.stress", "input.keyboard.quick", "input.touchpad.quick", "input.touchscreen.quick":
			version = 2
		}
		rows = append(rows, sessionevidence.ResultBinding{ID: id, Version: version, PlannedStatus: "excluded", ReportStatus: "skip",
			Evidence: touchpadNote("skip", "excluded", "not run; disposition recorded in the frozen plan")})
	}
	if err := sessionevidence.ValidateBindings(j, rows); err != nil {
		t.Fatal("complete all-off Extended prerequisite", err)
	}
	return j, rows
}

func extendedMemoryPass() quick.MemoryStressResult {
	return quick.MemoryStressResult{
		Schema: "hhsoft.pc-tester.memory-stress/v1", Status: "pass", StopReason: "completed", Suite: "byte-patterns-aa55-inversions-v1",
		RequestedLoadMilliseconds: 1800000, LocalWatchdogMilliseconds: 1860000, ThermalPolicy: "available-observations/v1",
		RequestedBytes: 4194304, AvailableBytes: 16777216, AllocatedBytes: 4194304,
		EffectiveDeadlineMilliseconds: 1860000, EffectiveDeadlineSource: "watchdog",
		AvailabilityFinishedMilliseconds: extendedMemoryTime(1), AllocationFinishedMilliseconds: extendedMemoryTime(2),
		LoadStartedMilliseconds: extendedMemoryTime(3), LoadStoppedMilliseconds: extendedMemoryTime(1800003), ObservedLoadMilliseconds: 1800000,
		TerminalStartedMilliseconds: extendedMemoryTime(1800004), TerminalStoppedMilliseconds: extendedMemoryTime(1800005), FinishedMilliseconds: 1800006,
		AccountingValid: true,
		Availability:    quick.MemoryStressAvailability{Started: true, Opened: true, RawBytes: 23, ReadCalls: 2, EOF: true, Valid: true, CloseConfirmed: true},
		Coverage:        quick.MemoryStressCoverage{CompletedPhases: 20, CompletedCycles: 2, LoadCycles: 1, ReadBytes: 50331648, WrittenBytes: 50331648, TerminalReadBytes: 4194304},
		StopFacts:       quick.MemoryStressStopFacts{NormalLoadComplete: true},
		Safety:          quick.SafetyEvidence{GlobalTimeoutMilliseconds: 1860000, ThermalLimitMilliCelsius: 90000, ThermalSensors: 1, ThermalSamples: 2, PeakMilliCelsius: 42000, MonitorStatus: "completed", CleanupConfirmed: true, ThermalObservations: 2, ThermalCoverage: "available"},
	}
}

func extendedMemoryObserved(t *testing.T) (sessionevidence.Journal, []sessionevidence.ResultBinding) {
	t.Helper()
	j, rows := extendedMemoryAllOff(t)
	safe := safemode.Result{Schema: "hhsoft.pc-tester.runtime-safety/v1", Status: "pass", StorageObserved: true, NetworkObserved: true}
	unavailable := quick.ThermalSnapshot{Coverage: "unavailable"}
	j.PreparationStartedMilliseconds, j.InventoryPreparedMilliseconds = 1800020, 1800030
	j.StopRequestedMilliseconds, j.SafetyJoinedMilliseconds, j.ElapsedMilliseconds = 1800040, 1800045, 1800050
	j.Monitor = safemode.MonitorResult{Schema: "hhsoft.pc-tester.periodic-safety/v1", Status: "pass",
		IntervalMilliseconds: 1000, ObservationTimeoutMilliseconds: 1000, MaximumAllowedGapMilliseconds: 2500,
		AttemptedObservations: 1800, CompletedObservations: 1800, ElapsedMilliseconds: 1800046,
		FirstCompletedMilliseconds: 1000, LastCompletedMilliseconds: 1800000, MaximumGapMilliseconds: 1000,
		CleanupConfirmed: true, LastObservation: &safe}
	j.Thermal = thermal.Result{Schema: "hhsoft.pc-tester.run-thermal/v1", Status: "completed",
		LimitMilliCelsius: 90000, IntervalMilliseconds: 1000, ObservationTimeoutMilliseconds: 1000, MaximumAllowedGapMilliseconds: 2500, StopTimeoutMilliseconds: 2000,
		AttemptedObservations: 1802, CompletedObservations: 1802, PeriodicAttempts: 1800, PeriodicCompletions: 1800,
		ElapsedMilliseconds: 1800048, FirstCompletedMilliseconds: 1, LastCompletedMilliseconds: 1800032, MaximumGapMilliseconds: 1000,
		Initial:          thermal.PhaseRecord{Attempted: true, Completed: true, CompletedMilliseconds: 1, Observation: &unavailable},
		Final:            thermal.PhaseRecord{RequestedMilliseconds: 1800031, Attempted: true, Completed: true, StartedMilliseconds: 1800031, CompletedMilliseconds: 1800032, Observation: &unavailable},
		Aggregate:        thermal.Aggregate{Coverage: "unavailable", AcceptedObservations: 1802, UnavailableObservations: 1802},
		CleanupConfirmed: true, Joined: true, JoinedMilliseconds: 1800047, LastObservation: &unavailable}
	j.Safety = []sessionevidence.SafetyRecord{
		{Phase: "preflight", Result: safe}, {Phase: "before-memory.quick", Result: safe}, {Phase: "before-memory.stress", Result: safe},
		{Phase: "final", Result: safe}, {Phase: "pre-sign", Result: safe},
	}
	j.Workers[1] = sessionevidence.WorkerRecord{ID: "memory.quick", State: "completed", Outcome: "pass", Timing: &sessionevidence.WorkerTiming{StartedMilliseconds: 2, FinishedMilliseconds: 5}}
	j.Workers[2] = sessionevidence.WorkerRecord{ID: "memory.stress", State: "completed", Outcome: "pass", Timing: &sessionevidence.WorkerTiming{StartedMilliseconds: 6, FinishedMilliseconds: 1800012}}
	q := quick.MemoryResult{Schema: "hhsoft.pc-tester.memory/v3", Status: "pass", AllocatedBytes: 4194304,
		ExaminedBytes: 25165824, Passes: 1, PatternsPerPass: 3, DurationMilliseconds: 3,
		Safety:   quick.SafetyEvidence{GlobalTimeoutMilliseconds: 30000, ThermalLimitMilliCelsius: 90000, ThermalSensors: 1, ThermalSamples: 1, PeakMilliCelsius: 42000, MonitorStatus: "completed", CleanupConfirmed: true, ThermalObservations: 1, ThermalCoverage: "available"},
		Coverage: quick.MemoryCoverage{Suite: "byte-patterns-aa55-inversions-v1", CompletedPasses: 1, CompletedPhases: 10, WrittenBytes: 25165824}}
	rows[1] = sessionevidence.ResultBinding{ID: "memory.quick", Version: 2, PlannedStatus: "scheduled", ReportStatus: "pass", Evidence: extendedMemoryJSON(t, q)}
	if _, err := quickevidence.DecodeLocal("memory.quick", rows[1].Evidence); err != nil {
		t.Fatal("literal memory.quick prerequisite", err)
	}
	raw := extendedMemoryJSON(t, extendedMemoryPass())
	_, digest, err := quickevidence.DecodeMemoryStress(raw)
	if err != nil {
		t.Fatal("literal standalone stress prerequisite", err)
	}
	j.MemoryStressSource = &sessionevidence.StressSource{WorkerID: "memory.stress", EvidenceSchema: "hhsoft.pc-tester.memory-stress/v1",
		EvidenceFileSHA256: extendedMemoryHash(raw), EvidenceTypedSHA256: digest}
	rows[2] = sessionevidence.ResultBinding{ID: "memory.stress", Version: 2, PlannedStatus: "scheduled", ReportStatus: "pass", Evidence: raw}
	if err := sessionevidence.ValidateBindings(j, rows); err != nil {
		t.Fatal("complete observed Extended prerequisite", err)
	}
	return j, rows
}

func TestExtendedMemoryJournalOwnsLiteralLayoutAndPreservesAliases(t *testing.T) {
	extended := sessionevidence.ExtendedWorkerIDs()
	if sessionevidence.Schema != "hhsoft.pc-tester.quick-session/v11" || len(extended) != 15 ||
		!reflect.DeepEqual(extended[:], strings.Fields(extendedMemoryOrder)) {
		t.Fatal("new profile changed default or literal layout")
	}
	q, s := sessionevidence.WorkerIDs(), sessionevidence.StandardWorkerIDs()
	if !reflect.DeepEqual(q[:], strings.Fields(touchscreenQuickWorkers)) || !reflect.DeepEqual(s[:], strings.Fields(touchscreenStandardWorkers)) {
		t.Fatal("Quick or Standard layout changed")
	}
	extended[2] = "cpu.stress"
	if sessionevidence.ExtendedWorkerIDs()[2] != "memory.stress" || !sessionevidence.IsExtendedSchema("hhsoft.pc-tester.quick-session/v13") ||
		sessionevidence.IsStandardSchema("hhsoft.pc-tester.quick-session/v13") {
		t.Fatal("selector ownership or profile distinction")
	}
	j, _ := extendedMemoryAllOff(t)
	raw := extendedMemoryJSON(t, j)
	if !bytes.HasSuffix(raw, []byte(`"profile":"extended","memory_stress_source":null}`)) ||
		bytes.Contains(raw, []byte("cpu_stress_source")) || bytes.Contains(raw, []byte("storage_source")) {
		t.Fatal("new profile wire fields are not exact")
	}
	if _, err := sessionevidence.Decode(raw); err != nil {
		t.Fatal(err)
	}
	if sessionevidence.ThermalViewOf(j).Scope != "Extended-session execution, cleanup, final inventory and final safety gate; report encoding, output and saving are outside this monitoring window." {
		t.Fatal("Extended monitoring scope overstated")
	}
	for _, version := range []string{"v4", "v5", "v6", "v7", "v8", "v9", "v10", "v11", "v12"} {
		t.Run(version, func(t *testing.T) {
			var old []byte
			if version == "v11" || version == "v12" {
				old = touchscreenSessionLiteral(t, version)
			} else {
				old = touchpadSessionLiteral(t, version)
			}
			prior, err := sessionevidence.Decode(old)
			if err != nil {
				t.Fatal("historical prerequisite", err)
			}
			got := extendedMemoryJSON(t, prior)
			if !bytes.Equal(got, old) || sessionevidence.IsExtendedSchema(prior.Schema) {
				t.Fatal("historical literal wire changed")
			}
			prior.MemoryStressSource = &sessionevidence.StressSource{}
			if _, err := json.Marshal(prior); err == nil {
				t.Fatal("historical schema silently discarded memory authority")
			}
		})
	}
}

func TestExtendedMemoryJournalRejectsIndependentWireAndBudgetMutations(t *testing.T) {
	j, _ := extendedMemoryAllOff(t)
	raw := extendedMemoryJSON(t, j)
	for _, tc := range []struct{ name, old, replacement string }{
		{"missing_source", `,"memory_stress_source":null`, ""},
		{"duplicate_source", `"memory_stress_source":null`, `"memory_stress_source":null,"memory_stress_source":null`},
		{"escaped_duplicate", `"memory_stress_source":null`, `"memory_stress_source":null,"memory_\u0073tress_source":null`},
		{"wrong_profile", `"profile":"extended"`, `"profile":"standard"`},
		{"missing_profile", `,"profile":"extended"`, ""},
		{"case_profile", `"profile":`, `"Profile":`},
		{"cpu_source", `"memory_stress_source":null`, `"memory_stress_source":null,"cpu_stress_source":null`},
		{"future", "quick-session/v13", "quick-session/v14"},
		{"over_budget", `"budget_milliseconds":2700000`, `"budget_milliseconds":2700001`},
		{"wrong_order", `"id":"memory.stress"`, `"id":"cpu.stress"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := bytes.Replace(raw, []byte(tc.old), []byte(tc.replacement), 1)
			if bytes.Equal(bad, raw) {
				t.Fatal("ineffective mutation")
			}
			if _, err := sessionevidence.Decode(bad); err == nil {
				t.Fatal("invalid Extended wire accepted")
			}
		})
	}
	for _, version := range []string{"v11", "v12"} {
		t.Run(version, func(t *testing.T) {
			prior, _, _ := touchscreenSessionAllOff(t, version)
			prior.BudgetMilliseconds = 900000
			if sessionevidence.ValidateStructure(prior) != nil {
				t.Fatal("old exact-limit prerequisite")
			}
			prior.BudgetMilliseconds = 900001
			if sessionevidence.ValidateStructure(prior) == nil {
				t.Fatal("Extended expanded old budget")
			}
		})
	}
}

func TestExtendedMemorySessionBindsTypedEvidenceAndCopiedSource(t *testing.T) {
	j, rows := extendedMemoryObserved(t)
	raw := rows[2].Evidence
	escaped := bytes.Replace(raw, []byte("memory-stress/v1"), []byte(`memory-stress/v\u0031`), 1)
	escaped = append([]byte(" \n"), escaped...)
	if bytes.Equal(raw, escaped) || extendedMemoryHash(raw) == extendedMemoryHash(escaped) {
		t.Fatal("original-byte prerequisite")
	}
	_, digest, err := quickevidence.DecodeMemoryStress(escaped)
	if err != nil || digest != j.MemoryStressSource.EvidenceTypedSHA256 {
		t.Fatal("typed-equivalence prerequisite", err)
	}
	rows[2].Evidence = escaped
	if sessionevidence.ValidateBindings(j, rows) != nil {
		t.Fatal("typed binding depends on original formatting")
	}
	// This layer intentionally retains the owner's original digest. Only the
	// finalizer has the original file bytes; report compaction cannot rehash them.
	if j.MemoryStressSource.EvidenceFileSHA256 != extendedMemoryHash(raw) {
		t.Fatal("source digest rewritten")
	}
	copyJ := sessionevidence.CloneJournal(j)
	copyJ.MemoryStressSource.EvidenceTypedSHA256 = "sha256:" + strings.Repeat("f", 64)
	copyJ.Workers[2].Timing.FinishedMilliseconds = 7
	if j.MemoryStressSource.EvidenceTypedSHA256 == copyJ.MemoryStressSource.EvidenceTypedSHA256 || j.Workers[2].Timing.FinishedMilliseconds != 1800012 {
		t.Fatal("clone retained mutable source or timing")
	}
}

func TestExtendedMemorySessionRejectsIndependentBindingMutations(t *testing.T) {
	for _, name := range []string{"definition", "wrong_id", "wrong_outcome", "interval", "missing_source", "source_worker", "source_schema", "typed_hash", "file_hash_shape", "future_schema", "mixed_cpu", "wrong_memory_family", "missing_quick_pass"} {
		t.Run(name, func(t *testing.T) {
			j, rows := extendedMemoryObserved(t)
			switch name {
			case "definition":
				rows[2].Version = 1
			case "wrong_id":
				rows[2].ID = "MEMORY.STRESS"
			case "wrong_outcome":
				rows[2].ReportStatus = "skip"
			case "interval":
				j.Workers[2].Timing.FinishedMilliseconds--
			case "missing_source":
				j.MemoryStressSource = nil
			case "source_worker":
				j.MemoryStressSource.WorkerID = "cpu.stress"
			case "source_schema":
				j.MemoryStressSource.EvidenceSchema = "hhsoft.pc-tester.memory/v3"
			case "typed_hash":
				j.MemoryStressSource.EvidenceTypedSHA256 = "sha256:" + strings.Repeat("f", 64)
			case "file_hash_shape":
				j.MemoryStressSource.EvidenceFileSHA256 = "invalid"
			case "future_schema":
				rows[2].Evidence = bytes.Replace(rows[2].Evidence, []byte("memory-stress/v1"), []byte("memory-stress/v2"), 1)
			case "mixed_cpu":
				rows[2].Evidence = append([]byte(`{"cpu_stress_source":null,`), rows[2].Evidence[1:]...)
			case "wrong_memory_family":
				rows[1].Evidence = rows[2].Evidence
			case "missing_quick_pass":
				j.Workers[1] = sessionevidence.WorkerRecord{ID: "memory.quick", State: "not_scheduled", Outcome: "excluded"}
				rows[1] = sessionevidence.ResultBinding{ID: "memory.quick", Version: 2, PlannedStatus: "excluded", ReportStatus: "skip",
					Evidence: touchpadNote("skip", "excluded", "not run; disposition recorded in the frozen plan")}
				j.Safety = append(j.Safety[:1:1], j.Safety[2:]...)
			}
			if sessionevidence.ValidateBindings(j, rows) == nil {
				t.Fatal("contradictory memory session accepted")
			}
		})
	}
}

func TestExtendedMemorySessionKeepsNotStartedNotesSeparateFromDispatch(t *testing.T) {
	j, rows := extendedMemoryObserved(t)
	j.MemoryStressSource = nil
	j.Safety = append(j.Safety[:2:2], j.Safety[3:]...)
	j.StopReason = "worker_stop"
	j.Workers[2] = sessionevidence.WorkerRecord{ID: "memory.stress", State: "not_started", Outcome: "aborted"}
	rows[2] = sessionevidence.ResultBinding{ID: "memory.stress", Version: 2, PlannedStatus: "scheduled", ReportStatus: "aborted",
		Evidence: touchpadNote("aborted", "aborted", "bounded evidence was not produced; outcome recorded by the run supervisor")}
	if sessionevidence.ValidateBindings(j, rows) != nil {
		t.Fatal("honest not-started prerequisite rejected")
	}
	j.Workers[2].State = "interrupted"
	j.Workers[2].Timing = &sessionevidence.WorkerTiming{StartedMilliseconds: 10, FinishedMilliseconds: 11}
	j.Safety = append(j.Safety[:2:2], append([]sessionevidence.SafetyRecord{{Phase: "before-memory.stress", Result: j.Safety[0].Result}}, j.Safety[2:]...)...)
	if sessionevidence.ValidateBindings(j, rows) == nil {
		t.Fatal("dispatched worker signed without typed evidence")
	}
}

func TestExtendedMemorySessionPreservesMismatchAndBindsLocalHeat(t *testing.T) {
	for _, mode := range []string{"cancelled_mismatch", "hot_mismatch"} {
		t.Run(mode, func(t *testing.T) {
			j, rows := extendedMemoryObserved(t)
			v := extendedMemoryPass()
			v.Status, v.StopReason = "fail", "memory_mismatch"
			v.StopFacts.ParentCancelled = true
			v.Coverage.TerminalReadBytes = 0
			v.Mismatch = &quick.MemoryStressMismatch{Stage: "terminal", Cycle: 2, Phase: 9, Offset: 0, ExpectedByte: 170, ActualByte: 171}
			j.StopReason = "worker_stop"
			if mode == "hot_mismatch" {
				v.StopFacts.ThermalLimit = true
				v.Safety.PeakMilliCelsius, v.Safety.MonitorStatus, v.Safety.MonitorFailure = 95000, "failed", "thermal_limit"
				j.StopReason = "thermal_limit"
			}
			raw := extendedMemoryJSON(t, v)
			_, digest, err := quickevidence.DecodeMemoryStress(raw)
			if err != nil {
				t.Fatal("late mismatch prerequisite", err)
			}
			j.Workers[2].Outcome, rows[2].ReportStatus, rows[2].Evidence = "fail", "fail", raw
			j.MemoryStressSource.EvidenceFileSHA256, j.MemoryStressSource.EvidenceTypedSHA256 = extendedMemoryHash(raw), digest
			if mode == "hot_mismatch" {
				j.LocalThermalSources = []sessionevidence.LocalThermalSource{{WorkerID: "memory.stress", EvidenceSchema: v.Schema,
					EvidenceFileSHA256: extendedMemoryHash(raw), EvidenceTypedSHA256: digest, PeakMilliCelsius: 95000, LimitMilliCelsius: 90000,
					StartedMilliseconds: 6, FinishedMilliseconds: 1800012}}
			}
			if sessionevidence.ValidateBindings(j, rows) != nil {
				t.Fatal("mismatch erased by adverse stop")
			}
			if mode == "hot_mismatch" {
				j.LocalThermalSources[0].EvidenceFileSHA256 = "sha256:" + strings.Repeat("e", 64)
				if sessionevidence.ValidateBindings(j, rows) == nil {
					t.Fatal("local source detached from original file")
				}
				j.LocalThermalSources = []sessionevidence.LocalThermalSource{}
				j.StopReason = "worker_stop"
				if sessionevidence.ValidateStructure(j) != nil {
					t.Fatal("missing local heat mutation was not structurally valid")
				}
				if sessionevidence.ValidateBindings(j, rows) == nil {
					t.Fatal("hot evidence omitted its local source")
				}
			} else {
				j.LocalThermalSources = []sessionevidence.LocalThermalSource{{WorkerID: "memory.stress", EvidenceSchema: v.Schema,
					EvidenceFileSHA256: extendedMemoryHash(raw), EvidenceTypedSHA256: digest, PeakMilliCelsius: 95000, LimitMilliCelsius: 90000,
					StartedMilliseconds: 6, FinishedMilliseconds: 1800012}}
				j.StopReason = "thermal_limit"
				if sessionevidence.ValidateStructure(j) != nil {
					t.Fatal("invented local heat mutation was not structurally valid")
				}
				if sessionevidence.ValidateBindings(j, rows) == nil {
					t.Fatal("cool evidence acquired a local heat source")
				}
				j.LocalThermalSources = []sessionevidence.LocalThermalSource{}
				j.StopReason = "worker_stop"
				j.Workers[2].Outcome, rows[2].ReportStatus = "aborted", "aborted"
				if sessionevidence.ValidateBindings(j, rows) == nil {
					t.Fatal("cancellation erased a mismatch")
				}
			}
		})
	}
}

func TestExtendedMemoryClaimSelectionDoesNotPromoteNotesOrMixedFamilies(t *testing.T) {
	for _, raw := range []string{
		`{"schema":"hhsoft.pc-tester.memory-stress/v1"}`,
		`{"schema":"hhsoft.pc-tester.memory-\u0073tress/v1"}`,
		`{"schema":"HHSOFT.PC-TESTER.MEMORY-STRESS/V99"}`,
		`{"memory_stress_source":null}`,
		`{"schema":"hhsoft.pc-tester.memory-stress/v1","schema":"note"}`,
	} {
		if !sessionevidence.HasMemoryStressClaim([]byte(raw)) {
			t.Fatal("strict claim hidden")
		}
	}
	if sessionevidence.HasMemoryStressClaim([]byte(`{"test_id":"memory.stress","schema":"hhsoft.pc-tester.execution-note/v1"}`)) {
		t.Fatal("inactive test ID promoted to an observation")
	}
}

func TestExtendedMemoryCurrentProfilesRetainUnscheduledCatalogNotes(t *testing.T) {
	for _, version := range []string{"v11", "v12"} {
		t.Run(version, func(t *testing.T) {
			for _, disposition := range []struct{ planned, status string }{
				{"excluded", "skip"}, {"not_present", "skip"},
				{"unsupported", "skip"}, {"missing_driver", "inconclusive"},
			} {
				for _, definition := range []int{1, 2} {
					j, rows, _ := touchscreenSessionAllOff(t, version)
					rows = append(rows, sessionevidence.ResultBinding{ID: "memory.stress", Version: definition,
						PlannedStatus: disposition.planned, ReportStatus: disposition.status,
						Evidence: touchpadNote(disposition.status, disposition.planned, "not run; disposition recorded in the frozen plan")})
					if sessionevidence.ValidateBindings(j, rows) != nil || j.MemoryStressSource != nil {
						t.Fatalf("unexecuted catalog note rejected: %s definition %d", disposition.planned, definition)
					}
					want := touchscreenQuickWorkers
					if version == "v12" {
						want = touchscreenStandardWorkers
					}
					var ids []string
					for _, worker := range j.Workers {
						ids = append(ids, worker.ID)
					}
					if !reflect.DeepEqual(ids, strings.Fields(want)) {
						t.Fatal("a catalog note changed the literal worker authority")
					}
				}
			}
		})
	}
}

func TestExtendedMemoryCurrentProfilesRejectForgedCatalogNotes(t *testing.T) {
	_, observed := extendedMemoryObserved(t)
	for _, mode := range []string{"case_id", "version_zero", "future_version", "scheduled", "missing_disposition",
		"wrong_report_status", "wrong_source_status", "future_note_schema", "unknown_member", "duplicate_member",
		"case_member", "changed_detail", "observed_stress", "mixed_cpu", "wrong_memory_family"} {
		t.Run(mode, func(t *testing.T) {
			for _, version := range []string{"v11", "v12"} {
				j, rows, _ := touchscreenSessionAllOff(t, version)
				row := sessionevidence.ResultBinding{ID: "memory.stress", Version: 2, PlannedStatus: "excluded", ReportStatus: "skip",
					Evidence: touchpadNote("skip", "excluded", "not run; disposition recorded in the frozen plan")}
				rows = append(rows, row)
				if sessionevidence.ValidateBindings(j, rows) != nil {
					t.Fatal("valid current catalog-note prerequisite")
				}
				switch mode {
				case "case_id":
					row.ID = "MEMORY.STRESS"
				case "version_zero":
					row.Version = 0
				case "future_version":
					row.Version = 3
				case "scheduled":
					row.PlannedStatus, row.ReportStatus = "scheduled", "aborted"
					row.Evidence = touchpadNote("aborted", "aborted", "bounded evidence was not produced; outcome recorded by the run supervisor")
				case "missing_disposition":
					row.PlannedStatus = ""
				case "wrong_report_status":
					row.ReportStatus = "inconclusive"
				case "wrong_source_status":
					row.Evidence = touchpadNote("skip", "not_present", "not run; disposition recorded in the frozen plan")
				case "future_note_schema":
					row.Evidence = bytes.Replace(row.Evidence, []byte("execution-note/v1"), []byte("execution-note/v99"), 1)
				case "unknown_member":
					row.Evidence = append([]byte(`{"unexpected":true,`), row.Evidence[1:]...)
				case "duplicate_member":
					row.Evidence = append([]byte(`{"status":"skip",`), row.Evidence[1:]...)
				case "case_member":
					row.Evidence = bytes.Replace(row.Evidence, []byte(`"source_status"`), []byte(`"Source_status"`), 1)
				case "changed_detail":
					row.Evidence = touchpadNote("skip", "excluded", "memory stress was observed")
				case "observed_stress":
					row.PlannedStatus, row.ReportStatus, row.Evidence = "scheduled", "pass", observed[2].Evidence
				case "mixed_cpu":
					row.Evidence = append([]byte(`{"requested_load_ms":600000,`), row.Evidence[1:]...)
				case "wrong_memory_family":
					row.Evidence = observed[1].Evidence
				}
				if reflect.DeepEqual(row, rows[len(rows)-1]) || !json.Valid(row.Evidence) || sessionevidence.ValidateStructure(j) != nil {
					t.Fatal("effective JSON-valid binding mutation prerequisite")
				}
				rows[len(rows)-1] = row
				if sessionevidence.ValidateBindings(j, rows) == nil {
					t.Fatal("current profile acquired malformed or observed memory authority", version)
				}
			}
		})
	}
}

func TestExtendedMemoryHistoricalVersionsKeepVersionOneCatalogNotes(t *testing.T) {
	for _, version := range []string{"v4", "v5", "v6", "v7", "v8", "v9", "v10"} {
		t.Run(version, func(t *testing.T) {
			j, rows := touchpadAllOff(t, version)
			original := extendedMemoryJSON(t, j)
			rows = append(rows, sessionevidence.ResultBinding{ID: "memory.stress", Version: 1, PlannedStatus: "excluded", ReportStatus: "skip",
				Evidence: touchpadNote("skip", "excluded", "not run; disposition recorded in the frozen plan")})
			if sessionevidence.ValidateBindings(j, rows) != nil {
				t.Fatal("literal historical version-one note prerequisite")
			}
			rows[len(rows)-1].Version = 2
			if sessionevidence.ValidateBindings(j, rows) == nil {
				t.Fatal("current catalog-note exception broadened historical authority")
			}
			if !bytes.Equal(original, extendedMemoryJSON(t, j)) {
				t.Fatal("historical wire changed during binding admission")
			}
		})
	}
}
