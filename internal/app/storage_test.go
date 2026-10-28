package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storageevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/workerexec"
)

func storageFixture(t *testing.T) InventoryPayload {
	t.Helper()
	return thermalFixture(t, "storage-sample-standalone-pass-v1")
}

func changeStorage(t *testing.T, p *InventoryPayload, change func(*storagetest.Result)) {
	t.Helper()
	for i := range p.TestResults {
		if p.TestResults[i].ID != "storage.quick" {
			continue
		}
		var value storagetest.Result
		if err := json.Unmarshal(p.TestResults[i].Evidence, &value); err != nil {
			t.Fatal(err)
		}
		change(&value)
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		p.TestResults[i].Evidence = raw
		p.TestResults[i].Status = TestStatus(value.Status)
		return
	}
	t.Fatal("fixture has no storage row")
}

// Upgrade only a test journal; production still emits v5 and eleven workers.
func storageV6Fixture(t *testing.T) InventoryPayload {
	t.Helper()
	p := thermalFixture(t, "session-v4-unavailable")
	sample := storageFixture(t)
	p.Components, p.SystemFingerprint = sample.Components, sample.SystemFingerprint
	var result storagetest.Result
	if err := json.Unmarshal(sample.TestResults[0].Evidence, &result); err != nil {
		t.Fatal(err)
	}
	result.Scope, result.PlanID, result.ComponentIDs = "planned", p.TestResults[0].PlanID, []string{"block:sda"}
	raw, err := storageevidence.CanonicalTypedBytes(result)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	for i := range p.TestResults {
		row := &p.TestResults[i]
		if row.ID == "storage.quick" {
			row.Version, row.Status, row.PlannedStatus, row.Evidence = 2, TestPass, PlannedScheduled, raw
		}
		if row.ID == "input.keyboard.quick" {
			row.Version = 2
		}
	}
	mutateIntegrity(t, &p, func(v *sessionevidence.IntegrityEvidence) {
		v.PlannedComponentCount, v.FinalComponentCount = 1, 1
		j := v.Session
		j.Schema = sessionevidence.SchemaV6
		j.Workers = append(j.Workers[:4:4], append([]sessionevidence.WorkerRecord{{ID: "input.keyboard.quick", State: "not_scheduled", Outcome: "excluded"}}, j.Workers[4:]...)...)
		j.Workers = append(j.Workers[:8:8], append([]sessionevidence.WorkerRecord{{ID: "storage.quick", State: "completed", Outcome: "pass", Timing: &sessionevidence.WorkerTiming{StartedMilliseconds: 2, FinishedMilliseconds: 8}}}, j.Workers[8:]...)...)
		j.Safety = append(j.Safety[:1:1], append([]sessionevidence.SafetyRecord{{Phase: "before-storage.quick", Result: j.Safety[0].Result}}, j.Safety[1:]...)...)
		j.StorageSource = &sessionevidence.StorageSource{WorkerID: "storage.quick", EvidenceSchema: storagetest.Schema, EvidenceFileSHA256: "sha256:" + strings.Repeat("b", 64), EvidenceTypedSHA256: "sha256:" + hex.EncodeToString(digest[:])}
	})
	// The retained v4 keyboard row was an unsupported definition, whereas the
	// synthetic v6 worker above is explicitly excluded.
	for i := range p.TestResults {
		if p.TestResults[i].ID == "input.keyboard.quick" {
			p.TestResults[i].PlannedStatus = PlannedExcluded
			p.TestResults[i].Status = TestSkip
			p.TestResults[i].Evidence = []byte(`{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run"}`)
		}
	}
	return p
}

func TestStorageReportUsesCopiedAcceptedAndDiagnosticViews(t *testing.T) {
	p := storageFixture(t)
	before, _ := json.Marshal(p)
	view, err := ValidatedStorage(p)
	if err != nil || view == nil || view.AcceptedReadBytes != 131072 || view.AcceptedComparedBytes != 65536 || view.AcceptedMatchedRanges != 1 || view.DiagnosticReadBytes != 0 || !view.CleanupConfirmed || len(view.Devices) != 1 {
		t.Fatal("literal sample view rejected", err)
	}
	view.Devices[0].Interface = "changed"
	again, err := ValidatedStorage(p)
	after, _ := json.Marshal(p)
	if err != nil || again.Devices[0].Interface != "sda" || !bytes.Equal(before, after) {
		t.Fatal("view aliases or changes signed evidence")
	}
	changeStorage(t, &p, func(r *storagetest.Result) {
		r.Status, r.StopReason, r.Error = "error", "error", "identity_changed"
		r.AcceptedReadBytes, r.AcceptedComparedBytes, r.AcceptedMatchedRanges = 0, 0, 0
		d := &r.Devices[0]
		d.Accepted = false
		d.FinalBindingConfirmed = false
		d.Status = "error"
		d.Error = "identity_changed"
	})
	view, err = ValidatedStorage(p)
	if err != nil || view.AcceptedReadBytes != 0 || view.DiagnosticReadBytes != 131072 || view.DiagnosticComparedBytes != 65536 || view.Devices[0].Accepted {
		t.Fatal("diagnostic pass acquired accepted coverage", err)
	}
	// This device ended in identity_changed, not pass; a vanished final row may
	// remain diagnostic without acquiring accepted coverage or continuity.
	p.Components = nil
	p.SystemFingerprint.UnidentifiedParts = 0
	var graph map[string]any
	if json.Unmarshal(p.TestResults[1].Evidence, &graph) != nil {
		t.Fatal("integrity fixture")
	}
	graph["status"] = "fail"
	graph["final_component_count"] = 0
	graph["final_hardware_graph"] = "hmac-sha256:" + strings.Repeat("4", 64)
	graph["removed_components"] = []map[string]string{{"type": "block", "id": "block:sda"}}
	p.TestResults[1].Status = TestFail
	p.TestResults[1].Evidence, _ = json.Marshal(graph)
	view, err = ValidatedStorage(p)
	if err != nil || view == nil || view.AcceptedReadBytes != 0 || view.DiagnosticReadBytes != 131072 {
		t.Fatal("non-pass disappearance discarded diagnostic evidence", err)
	}
	for _, name := range []string{"session-v4-unavailable", "session-v5-keyboard-pass", "historical-session-v3"} {
		if view, err := ValidatedStorage(thermalFixture(t, name)); err != nil || view != nil {
			t.Fatal("historical report acquired storage sampling", name, err)
		}
	}
}

func TestStorageReportRequiresExactDefinitionScopeAndFinalGeometry(t *testing.T) {
	for name, mutate := range map[string]func(*InventoryPayload){
		"old definition":    func(p *InventoryPayload) { p.TestResults[0].Version = 1 },
		"future definition": func(p *InventoryPayload) { p.TestResults[0].Version = 3 },
		"wrong owner":       func(p *InventoryPayload) { p.TestResults[0].ID = "cpu.quick" },
		"case owner":        func(p *InventoryPayload) { p.TestResults[0].ID = "STORAGE.QUICK" },
		"direct capture":    func(p *InventoryPayload) { p.CoverageScope = CoverageDirectCapture },
		"no scope":          func(p *InventoryPayload) { p.CoverageScope = "" },
		"duplicate row":     func(p *InventoryPayload) { p.TestResults = append(p.TestResults, p.TestResults[0]) },
		"missing component": func(p *InventoryPayload) { p.Components = nil },
		"wrong number":      func(p *InventoryPayload) { p.Components[0].Attributes[0].Value = "8:1" },
		"wrong size":        func(p *InventoryPayload) { p.Components[0].Attributes[4].Value = "129" },
		"wrong block":       func(p *InventoryPayload) { p.Components[0].Attributes[2].Value = "4096" },
		"noncanonical size": func(p *InventoryPayload) { p.Components[0].Attributes[4].Value = "0128" },
		"duplicate attribute": func(p *InventoryPayload) {
			p.Components[0].Attributes = append(p.Components[0].Attributes, p.Components[0].Attributes[0])
		},
		"planned without session": func(p *InventoryPayload) {
			changeStorage(t, p, func(r *storagetest.Result) {
				r.Scope = "planned"
				r.PlanID = p.TestResults[0].PlanID
				r.ComponentIDs = []string{"block:sda"}
			})
		},
		"count forgery":   func(p *InventoryPayload) { changeStorage(t, p, func(r *storagetest.Result) { r.AcceptedReadBytes++ }) },
		"status mismatch": func(p *InventoryPayload) { p.TestResults[0].Status = TestInconclusive },
	} {
		t.Run(name, func(t *testing.T) {
			p := storageFixture(t)
			mutate(&p)
			if ValidateCoverage(p) == nil {
				t.Fatal("invalid sampling report accepted")
			}
		})
	}
}

func TestStorageV6BindsScopeDigestTimingAndRetainsLegacyWorkerEvidence(t *testing.T) {
	p := storageV6Fixture(t)
	if err := ValidateCoverage(p); err != nil {
		t.Fatal("complete planned v6 rejected", err)
	}
	for name, mutate := range map[string]func(*InventoryPayload){
		"typed digest": func(p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) {
				v.Session.StorageSource.EvidenceTypedSHA256 = "sha256:" + strings.Repeat("c", 64)
			})
		},
		"source absent": func(p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.StorageSource = nil })
		},
		"wrong source schema": func(p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) {
				v.Session.StorageSource.EvidenceSchema = "hhsoft.pc-tester.storage-readiness/v1"
			})
		},
		"too short worker": func(p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.Workers[8].Timing.FinishedMilliseconds = 6 })
		},
		"standalone in session": func(p *InventoryPayload) {
			changeStorage(t, p, func(r *storagetest.Result) { r.Scope = "standalone"; r.PlanID = ""; r.ComponentIDs = []string{} })
		},
		"foreign plan": func(p *InventoryPayload) {
			changeStorage(t, p, func(r *storagetest.Result) { r.PlanID = "sha256:" + strings.Repeat("d", 64) })
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := storageV6Fixture(t)
			mutate(&p)
			if ValidateCoverage(p) == nil {
				t.Fatal("v6 storage binding forgery accepted")
			}
		})
	}
	for _, name := range []string{"session-v5-keyboard-pass", "session-v5-cpu-v3-local-heat", "session-v5-memory-v3-local-heat", "session-v5-pointer-v2-pass"} {
		p := thermalFixture(t, name)
		mutateIntegrity(t, &p, func(v *sessionevidence.IntegrityEvidence) {
			j := v.Session
			j.Schema = sessionevidence.SchemaV6
			j.Workers = append(j.Workers[:8:8], append([]sessionevidence.WorkerRecord{{ID: "storage.quick", State: "not_scheduled", Outcome: "unsupported"}}, j.Workers[8:]...)...)
		})
		for i := range p.TestResults {
			if p.TestResults[i].ID == "storage.quick" {
				p.TestResults[i].Version = 2
			}
		}
		if err := ValidateCoverage(p); err != nil {
			t.Fatal("v6 lost retained worker evidence", name, err)
		}
	}
}

func TestStorageCleanupUncertaintyAndClosedInactiveNotes(t *testing.T) {
	p := storageFixture(t)
	changeStorage(t, &p, func(r *storagetest.Result) {
		r.CleanupConfirmed = false
		r.Status = "error"
		r.StopReason = "cleanup_unconfirmed"
		r.Error = "cleanup_unconfirmed"
	})
	if err := ValidateCoverage(p); !errors.Is(err, workerexec.ErrCleanupUncertain) {
		t.Fatal("cleanup uncertainty did not block report", err)
	}
	if _, err := ValidatedStorage(p); !errors.Is(err, workerexec.ErrCleanupUncertain) {
		t.Fatal("summary erased cleanup uncertainty", err)
	}
	for _, status := range []PlannedStatus{PlannedExcluded, PlannedNotPresent, PlannedUnsupported, PlannedMissingDriver} {
		p := storageFixture(t)
		row := &p.TestResults[0]
		row.PlannedStatus = status
		row.Status = TestSkip
		if status == PlannedMissingDriver {
			row.Status = TestInconclusive
		}
		row.Evidence = []byte(`{"schema":"hhsoft.pc-tester.execution-note/v1","status":"` + string(row.Status) + `","source_status":"` + string(status) + `","detail":"not run"}`)
		if view, err := ValidatedStorage(p); err != nil || view != nil {
			t.Fatal("inactive note changed", err)
		}
		row.Evidence = bytes.Replace(row.Evidence, []byte(`"not run"`), []byte(`"hhsoft.pc-tester.storage-sample/v1"`), 1)
		if ValidateCoverage(p) == nil {
			t.Fatal("functional claim hidden in note")
		}
	}
}

func TestStorageV6LateHeatAndNonfunctionalNotesCannotHideEvidence(t *testing.T) {
	failure := storageV6Fixture(t)
	var typedSHA string
	changeStorage(t, &failure, func(r *storagetest.Result) {
		r.Status = "fail"
		r.Error = "mismatch"
		r.FailureObserved = true
		r.AcceptedMatchedRanges = 0
		r.AcceptedComparedBytes = 0
		d := &r.Devices[0]
		d.Status = "fail"
		d.Error = "mismatch"
		d.Observation.Status = "fail"
		d.Observation.Error = "mismatch"
		d.Observation.MatchedRanges = 0
		d.Observation.ComparedBytes = 0
		d.Observation.Ranges[0].Outcome = "mismatch"
		raw, err := storageevidence.CanonicalTypedBytes(*r)
		if err != nil {
			t.Fatal("literal failure invalid", err)
		}
		sum := sha256.Sum256(raw)
		typedSHA = "sha256:" + hex.EncodeToString(sum[:])
	})
	mutateIntegrity(t, &failure, func(v *sessionevidence.IntegrityEvidence) {
		j := v.Session
		j.StopReason = "worker_stop"
		j.Workers[8].Outcome = "fail"
		j.StorageSource.EvidenceTypedSHA256 = typedSHA
	})
	failedView, err := ValidatedStorage(failure)
	if err != nil || failedView == nil || !failedView.FailureObserved || failedView.AcceptedComparedBytes != 0 || failedView.AcceptedReadBytes != 131072 || failedView.AcceptedMatchedRanges != 0 {
		t.Fatal("honest accepted failure or source was discarded", err)
	}
	p := storageV6Fixture(t)
	mutateIntegrity(t, &p, func(v *sessionevidence.IntegrityEvidence) {
		j := v.Session
		j.StopReason = "thermal_limit"
		hot := quick.ThermalSnapshot{Coverage: quick.ThermalAvailable, Sensors: 1, PeakMilliCelsius: 95000}
		j.Thermal.Final.Observation = &hot
		j.Thermal.LastObservation = &hot
		j.Thermal.Aggregate = thermal.Aggregate{Coverage: "partial", AcceptedObservations: 2, AvailableObservations: 1, UnavailableObservations: 1, MaximumSensors: 1, ValidReadings: 1, PeakMilliCelsius: 95000}
		j.Thermal.FirstFinding = &thermal.FindingRecord{Reason: "thermal_limit", Phase: "final", ElapsedMilliseconds: j.Thermal.Final.CompletedMilliseconds, Observation: hot}
	})
	if err := ValidateCoverage(p); err != nil {
		t.Fatal("honest late heat rejected", err)
	}
	view, err := ValidatedSessionThermal(p)
	if err != nil || view == nil || view.Finding != "limit_reached" || view.StopReason != "thermal_limit" {
		t.Fatal("sampling erased late heat", err)
	}
	mutateIntegrity(t, &p, func(v *sessionevidence.IntegrityEvidence) { v.Session.StopReason = "completed" })
	if ValidateCoverage(p) == nil {
		t.Fatal("late heat was relabelled completion")
	}
	for _, outcome := range []string{"error", "aborted"} {
		p := storageV6Fixture(t)
		for i := range p.TestResults {
			if p.TestResults[i].ID == "storage.quick" {
				p.TestResults[i].Status = TestStatus(outcome)
				p.TestResults[i].Evidence = []byte(`{"schema":"hhsoft.pc-tester.execution-note/v1","status":"` + outcome + `","source_status":"` + outcome + `","detail":"no result file"}`)
			}
		}
		mutateIntegrity(t, &p, func(v *sessionevidence.IntegrityEvidence) {
			j := v.Session
			j.StopReason = "worker_stop"
			j.Workers[8].Outcome = outcome
			j.StorageSource = nil
		})
		if view, err := ValidatedStorage(p); err != nil || view != nil {
			t.Fatal("honest absent-file note rejected", err)
		}
		mutateIntegrity(t, &p, func(v *sessionevidence.IntegrityEvidence) {
			v.Session.StorageSource = &sessionevidence.StorageSource{WorkerID: "storage.quick", EvidenceSchema: storagetest.Schema, EvidenceFileSHA256: "sha256:" + strings.Repeat("b", 64), EvidenceTypedSHA256: "sha256:" + strings.Repeat("c", 64)}
		})
		if ValidateCoverage(p) == nil {
			t.Fatal("absent file acquired a source assertion")
		}
	}
}
