package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func journalSafeObservation() safemode.Result {
	return safemode.Result{Schema: safemode.Schema, Status: "pass", StorageObserved: true, NetworkObserved: true}
}

func journalShortMonitor() safemode.MonitorResult {
	return safemode.MonitorResult{
		Schema: safemode.MonitorSchema, Status: "pass", IntervalMilliseconds: 1000,
		ObservationTimeoutMilliseconds: 1000, MaximumAllowedGapMilliseconds: 2500,
		ElapsedMilliseconds: 100, MaximumGapMilliseconds: 100, CleanupConfirmed: true,
	}
}

type journalFixture struct {
	t                     *testing.T
	directory, reportPath string
	arguments             []string
	frozen                plan.Plan
	snapshot              inventory.Snapshot
	journal               sessionJournal
}

func newJournalFixture(t *testing.T, selected ...string) *journalFixture {
	t.Helper()
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x57}, 32)
	keyPath := writeTestKey(t, directory, "fingerprint.key", key)
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil),
		inventory.NewComponent(inventory.ComponentMemory, "memory:system", nil),
		inventory.NewComponent(inventory.ComponentNetwork, "network:fixture", map[string]string{"name": "eth0"}),
		inventory.NewComponent(inventory.ComponentBlock, "block:fixture", map[string]string{"kind": "device"}),
		inventory.NewComponent(inventory.ComponentSound, "sound:fixture", nil),
		inventory.NewComponent(inventory.ComponentUSB, "usb:fixture", nil),
		inventory.NewComponent(inventory.ComponentInput, "input:fixture", map[string]string{"category": "keyboard"}),
		inventory.NewComponent(inventory.ComponentVideo, "video:controller", map[string]string{"category": "display_controller"}),
		inventory.NewComponent(inventory.ComponentVideo, "video:connector", map[string]string{"category": "display_connector"}),
	})
	chosen := map[string]bool{}
	for _, id := range selected {
		chosen[id] = true
	}
	var excluded []string
	for _, id := range sessionWorkerIDs {
		if !chosen[id] {
			excluded = append(excluded, id)
		}
	}
	frozen, err := plan.BuildWithSelection(snapshot, plan.ProfileQuick, "x86_64", key, "ephemeral-session", excluded)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(directory, "plan.json")
	if err := os.WriteFile(planPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := &journalFixture{t: t, directory: directory, reportPath: filepath.Join(directory, "report.hhpcr"), frozen: frozen, snapshot: snapshot}
	fixture.arguments = []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory,
		"-session-id", "journal-fixture", "-output", fixture.reportPath}
	fixture.journal = sessionJournal{Schema: sessionJournalSchema, PlanID: frozen.ID, StopReason: "operator_cancelled", BudgetMilliseconds: 1000, ElapsedMilliseconds: 100, Monitor: journalShortMonitor(), PreparationStartedMilliseconds: 100, InventoryPreparedMilliseconds: 100, StopRequestedMilliseconds: 100, SafetyJoinedMilliseconds: 100, Thermal: sessionThermalSummary(100), LocalThermalSources: []sessionevidence.LocalThermalSource{}}
	planned := map[string]plan.TestStatus{}
	for _, row := range frozen.Tests {
		planned[row.ID] = row.Status
	}
	for _, id := range sessionWorkerIDs {
		worker := sessionWorkerRecord{ID: id, State: "not_scheduled", Outcome: string(planned[id])}
		if planned[id] == plan.TestScheduled {
			worker.State, worker.Outcome = "not_started", "aborted"
		}
		fixture.journal.Workers = append(fixture.journal.Workers, worker)
	}
	fixture.safety()
	return fixture
}

func (fixture *journalFixture) safety() {
	fixture.journal.Safety = []sessionSafetyRecord{{Phase: "preflight", Result: journalSafeObservation()}}
	for _, worker := range fixture.journal.Workers {
		if worker.State == "completed" || worker.State == "interrupted" {
			fixture.journal.Safety = append(fixture.journal.Safety, sessionSafetyRecord{Phase: "before-" + worker.ID, Result: journalSafeObservation()})
		}
	}
	fixture.journal.Safety = append(fixture.journal.Safety, sessionSafetyRecord{Phase: "final", Result: journalSafeObservation()}, sessionSafetyRecord{Phase: "pre-sign", Result: journalSafeObservation()})
}

func (fixture *journalFixture) worker(t *testing.T, id, state, outcome string, evidence any) {
	t.Helper()
	for index := range fixture.journal.Workers {
		if fixture.journal.Workers[index].ID == id {
			fixture.journal.Workers[index].State, fixture.journal.Workers[index].Outcome = state, outcome
			if state == "completed" || state == "interrupted" {
				start := int64(index * 20)
				finish := start + 20
				fixture.journal.Workers[index].Timing = &sessionevidence.WorkerTiming{StartedMilliseconds: start, FinishedMilliseconds: finish}
				if finish > fixture.journal.PreparationStartedMilliseconds {
					setJournalFixtureSpan(&fixture.journal, finish+20)
					fixture.journal.Monitor = sessionMonitorSummary(finish + 20)
				}
			}
		}
	}
	if evidence != nil {
		for _, definition := range quickResultDefinitions {
			if definition.id == id {
				if err := os.WriteFile(filepath.Join(fixture.directory, definition.filename), encodeEvidenceFixture(t, evidence), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	fixture.safety()
}

func (fixture *journalFixture) args(t *testing.T) []string {
	t.Helper()
	path := filepath.Join(fixture.directory, "session-journal.json")
	encoded, err := json.Marshal(fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	arguments := append([]string(nil), fixture.arguments...)

	for _, worker := range fixture.journal.Workers {
		arguments = append(arguments, "-outcome", worker.ID+"="+worker.Outcome)
	}
	return arguments
}

func TestSessionJournalRejectsAmbiguousAndIncompleteJSON(t *testing.T) {
	fixture := newJournalFixture(t)
	encoded, _ := json.Marshal(fixture.journal)
	for name, change := range map[string]func([]byte) []byte{
		"duplicate": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"schema":`), []byte(`"schema":"ignored","schema":`), 1)
		},
		"escaped duplicate": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"schema":`), []byte(`"sch\u0065ma":"ignored","schema":`), 1)
		},
		"case alias": func(b []byte) []byte { return bytes.Replace(b, []byte(`"plan_id":`), []byte(`"Plan_ID":`), 1) },
		"unknown":    func(b []byte) []byte { return append([]byte(`{"unknown":true,`), b[1:]...) },
		"null number": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"elapsed_milliseconds":100`), []byte(`"elapsed_milliseconds":null`), 1)
		},
		"missing number": func(b []byte) []byte { return bytes.Replace(b, []byte(`"elapsed_milliseconds":100,`), nil, 1) },
		"null workers": func(b []byte) []byte {
			var value map[string]any
			_ = json.Unmarshal(b, &value)
			value["workers"] = nil
			b, _ = json.Marshal(value)
			return b
		},
		"null safety": func(b []byte) []byte {
			var value map[string]any
			_ = json.Unmarshal(b, &value)
			value["safety"] = nil
			b, _ = json.Marshal(value)
			return b
		},
		"nested duplicate": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"state":`), []byte(`"state":"completed","state":`), 1)
		},
		"missing observation": func(b []byte) []byte { return bytes.Replace(b, []byte(`"storage_observed":true,`), nil, 1) },
		"trailing object":     func(b []byte) []byte { return append(b, []byte(`{}`)...) },
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "journal.json")
			if err := os.WriteFile(path, change(encoded), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readSessionJournal(path); err == nil {
				t.Fatal("ambiguous journal accepted")
			}
		})
	}
}

func TestSessionJournalRejectsContradictoryExecutionAndSafety(t *testing.T) {
	for name, change := range map[string]func(*sessionJournal){
		"schema":                   func(j *sessionJournal) { j.Schema = "unknown" },
		"old-nine-worker-schema":   func(j *sessionJournal) { j.Schema = "hhsoft.pc-tester.quick-session/v1" },
		"old-unmonitored-schema":   func(j *sessionJournal) { j.Schema = "hhsoft.pc-tester.quick-session/v2" },
		"short budget":             func(j *sessionJournal) { j.BudgetMilliseconds = 9 },
		"long budget":              func(j *sessionJournal) { j.BudgetMilliseconds = 900001 },
		"negative elapsed":         func(j *sessionJournal) { j.ElapsedMilliseconds = -1 },
		"long elapsed":             func(j *sessionJournal) { j.ElapsedMilliseconds = 3600001 },
		"cleanup uncertain":        func(j *sessionJournal) { j.StopReason = "cleanup_uncertain" },
		"wrong plan":               func(j *sessionJournal) { j.PlanID = "sha256:" + strings.Repeat("0", 64) },
		"completed but unfinished": func(j *sessionJournal) { j.StopReason = "completed" },
		"duplicate worker":         func(j *sessionJournal) { j.Workers[1] = j.Workers[0] },
		"out of order":             func(j *sessionJournal) { j.Workers[0], j.Workers[1] = j.Workers[1], j.Workers[0] },
		"missing worker":           func(j *sessionJournal) { j.Workers = j.Workers[:8] },
		"missing pointer worker":   func(j *sessionJournal) { j.Workers = append(j.Workers[:4], j.Workers[5:]...) },
		"unknown state":            func(j *sessionJournal) { j.Workers[0].State = "running" },
		"invented not scheduled":   func(j *sessionJournal) { j.Workers[0].State, j.Workers[0].Outcome = "not_scheduled", "excluded" },
		"not started pass":         func(j *sessionJournal) { j.Workers[1].Outcome = "pass" },
		"missing final safety":     func(j *sessionJournal) { j.Safety = j.Safety[:len(j.Safety)-1] },
		"wrong safety phase":       func(j *sessionJournal) { j.Safety[0].Phase = "final" },
		"unsafe history":           func(j *sessionJournal) { j.Safety[0].Result.WritableBlockMounts = 1 },
		"unknown history":          func(j *sessionJournal) { j.Safety[0].Result.NetworkObserved = false },
		"contradictory storage": func(j *sessionJournal) {
			j.Safety[0].Result.StorageDetail = "block mount safety could not be established"
		},
		"contradictory network": func(j *sessionJournal) {
			j.Safety[0].Result.NetworkDetail = "offline network state could not be established"
		},
		"forged pre-sign": func(j *sessionJournal) {
			j.Safety = append(j.Safety, sessionSafetyRecord{Phase: "pre-sign", Result: journalSafeObservation()})
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newJournalFixture(t, "cpu.quick", "memory.quick")
			change(&fixture.journal)
			outcomes := map[string]string{}
			for _, worker := range fixture.journal.Workers {
				outcomes[worker.ID] = worker.Outcome
			}
			if err := validateSessionJournal(fixture.journal, fixture.frozen, outcomes, fixture.journal.StopReason, fixture.directory); err == nil {
				t.Fatal("contradictory journal accepted")
			}
		})
	}
	for _, earlier := range []string{"not_started", "interrupted"} {
		t.Run("started after "+earlier, func(t *testing.T) {
			fixture := newJournalFixture(t, "cpu.quick", "memory.quick")
			fixture.worker(t, "cpu.quick", earlier, "aborted", nil)
			fixture.worker(t, "memory.quick", "completed", "pass", nil)
			if err := validateSessionJournalStructure(fixture.journal); err == nil {
				t.Fatal("worker started after stop")
			}
		})
	}
}

func TestSessionJournalWriteAndReadBoundaries(t *testing.T) {
	fixture := newJournalFixture(t)
	path := filepath.Join(fixture.directory, "journal.json")
	if err := writeSessionJournal(path, fixture.journal); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := writeSessionJournal(path, fixture.journal); err == nil {
		t.Fatal("journal overwritten")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing journal changed")
	}
	if _, err := readSessionJournal(path); err != nil {
		t.Fatal(err)
	}
	if _, err := readSessionJournal(fixture.directory); err == nil {
		t.Fatal("directory journal accepted")
	}
	oversized := filepath.Join(fixture.directory, "oversized.json")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte{' '}, maximumSessionJournalBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSessionJournal(oversized); err == nil {
		t.Fatal("oversized journal accepted")
	}
	fixture.journal.Safety[0].Result.StorageDetail = strings.Repeat("x", maximumSessionJournalBytes)
	output := filepath.Join(fixture.directory, "rejected.json")
	if err := writeSessionJournal(output, fixture.journal); err == nil {
		t.Fatal("oversized journal written")
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected journal created output")
	}
}

func TestFinalizeSessionPreservesFailureAndNeverStartedReadiness(t *testing.T) {
	fixture := newJournalFixture(t, "cpu.quick", "memory.quick", "network.readiness.quick", "storage.readiness.quick")
	cpu := completeCPUEvidence()
	cpu.Status, cpu.Detail = quick.StatusFail, "synthetic calculation mismatch"
	setCPUFixtureFailure(&cpu)
	fixture.worker(t, "cpu.quick", "completed", "fail", cpu)
	fixture.journal.StopReason = "worker_stop"
	arguments := fixture.args(t)
	journalPath := filepath.Join(fixture.directory, "session-journal.json")
	before, _ := os.ReadFile(journalPath)
	observations := 0
	var stdout, stderr bytes.Buffer
	code := fixture.finalize(t.Context(), arguments, &stdout, &stderr,
		func(context.Context) (inventory.Snapshot, error) { return fixture.snapshot, nil },
		bytes.NewReader(bytes.Repeat([]byte{0x51}, 64)), time.Now,
		func(context.Context) safemode.Result { observations++; return journalSafeObservation() })
	if code != 0 || observations != 1 {
		t.Fatalf("session finalization = %d, observations=%d: %s", code, observations, stderr.String())
	}
	after, _ := os.ReadFile(journalPath)
	if !bytes.Equal(before, after) {
		t.Fatal("finalizer rewrote the coordinator journal")
	}
	encoded, err := os.ReadFile(fixture.reportPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	var payload app.InventoryPayload
	if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.TestResults) != 22 {
		t.Fatal("session changed frozen catalog coverage")
	}
	found := false
	for _, result := range payload.TestResults {
		switch result.ID {
		case "cpu.quick":
			if result.Status != app.TestFail || !bytes.Equal(result.Evidence, encodeEvidenceFixture(t, cpu)) {
				t.Fatal("completed failure changed")
			}
		case "memory.quick", "network.readiness.quick", "storage.readiness.quick":
			if result.Status != app.TestAborted || result.PlannedStatus != app.PlannedScheduled {
				t.Fatal("never-started worker outcome changed")
			}
		case "run.integrity.quick":
			var evidence struct {
				Session *sessionJournal `json:"session"`
			}
			if json.Unmarshal(result.Evidence, &evidence) != nil || evidence.Session == nil {
				t.Fatal("signed session missing")
			}
			journal := evidence.Session
			if journal.Schema != "hhsoft.pc-tester.quick-session/v5" || journal.PlanID != fixture.frozen.ID || journal.StopReason != "worker_stop" || len(journal.Safety) != len(fixture.journal.Safety) || journal.Safety[len(journal.Safety)-1].Phase != "pre-sign" || !journal.Safety[len(journal.Safety)-1].Result.Safe() ||
				!bytes.Equal(encodeEvidenceFixture(t, journal.Monitor), encodeEvidenceFixture(t, fixture.journal.Monitor)) {
				t.Fatal("fresh safety or session intent not bound")
			}
			found = true
		default:
			if result.PlannedStatus == app.PlannedExcluded && result.Status != app.TestSkip {
				t.Fatal("excluded result changed")
			}
		}
	}
	if !found {
		t.Fatal("run integrity missing")
	}
}

func TestFinalizeSessionRejectsStaleSafetyBeforeSigning(t *testing.T) {
	for _, mode := range []string{"writable", "active", "unknown", "aborted", "cancelled", "nil", "storage-detail", "network-detail"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newJournalFixture(t)
			fixture.journal.StopReason = "completed"
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var observe func(context.Context) safemode.Result
			if mode != "nil" {
				observe = func(context.Context) safemode.Result {
					result := journalSafeObservation()
					switch mode {
					case "writable":
						result.WritableBlockMounts = 1
					case "active":
						result.ActiveNonLoopInterfaces = 1
					case "unknown":
						result.StorageObserved = false
					case "aborted":
						result.Status = "aborted"
					case "cancelled":
						cancel()
					case "storage-detail":
						result.StorageDetail = "block mount safety could not be established"
					case "network-detail":
						result.NetworkDetail = "offline network state could not be established"
					}
					return result
				}
			}
			random := &journalForbiddenRandom{}
			var stderr bytes.Buffer
			code := fixture.finalize(ctx, fixture.args(t), &bytes.Buffer{}, &stderr,
				func(context.Context) (inventory.Snapshot, error) { return fixture.snapshot, nil }, random, time.Now, observe)
			if code != 1 || random.calls != 0 {
				t.Fatalf("unsafe finalization = %d, signing reads=%d: %s", code, random.calls, stderr.String())
			}
			if _, err := os.Stat(fixture.reportPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unsafe report was created")
			}
		})
	}
}

type journalForbiddenRandom struct{ calls int }

func (reader *journalForbiddenRandom) Read([]byte) (int, error) {
	reader.calls++
	return 0, io.ErrUnexpectedEOF
}

func TestFinalizeSessionRejectsUnsafeOrInterruptedReadiness(t *testing.T) {
	for _, id := range []string{"network.readiness.quick", "storage.readiness.quick"} {
		for _, mode := range []string{"error", "aborted", "interrupted", "unsafe-count", "unsafe-device", "stale-not-started"} {
			t.Run(id+"/"+mode, func(t *testing.T) {
				fixture := newJournalFixture(t, id)
				state, outcome := "completed", "inconclusive"
				if mode == "error" || mode == "aborted" {
					outcome = mode
				}
				if mode == "interrupted" {
					state, outcome = "interrupted", "aborted"
				}
				if mode == "stale-not-started" {
					state, outcome = "not_started", "aborted"
				}
				if id == "network.readiness.quick" {
					evidence := completeNetworkEvidence()
					evidence.Status = "inconclusive"
					if mode == "unsafe-count" {
						evidence.ActiveNonLoopInterfaces = 1
					}
					if mode == "unsafe-device" {
						evidence.Interfaces[0].AdministrativeUp = true
					}
					fixture.worker(t, id, state, outcome, evidence)
				} else {
					evidence := completeStorageEvidence()
					evidence.Status = "inconclusive"
					if mode == "unsafe-count" {
						evidence.WritableMounts = 1
					}
					if mode == "unsafe-device" {
						evidence.Devices[0].WritableMount = true
					}
					fixture.worker(t, id, state, outcome, evidence)
				}
				collected, observed := false, false
				var stderr bytes.Buffer
				code := fixture.finalize(t.Context(), fixture.args(t), &bytes.Buffer{}, &stderr,
					func(context.Context) (inventory.Snapshot, error) { collected = true; return fixture.snapshot, nil },
					&journalForbiddenRandom{}, time.Now, func(context.Context) safemode.Result { observed = true; return journalSafeObservation() })
				if code != 1 {
					t.Fatalf("unsafe readiness reached finalization: %d collected=%v observed=%v: %s", code, collected, observed, stderr.String())
				}
			})
		}
	}
}

func TestFinalizeSessionCannotDowngradeCompletedFailure(t *testing.T) {
	for _, state := range []string{"completed", "interrupted", "not_started"} {
		t.Run(state, func(t *testing.T) {
			fixture := newJournalFixture(t, "cpu.quick")
			cpu := completeCPUEvidence()
			cpu.Status = quick.StatusFail
			setCPUFixtureFailure(&cpu)
			fixture.worker(t, "cpu.quick", state, "aborted", cpu)
			observed := false
			var stderr bytes.Buffer
			code := fixture.finalize(t.Context(), fixture.args(t), &bytes.Buffer{}, &stderr,
				func(context.Context) (inventory.Snapshot, error) {
					t.Fatal("failure downgrade reached collection")
					return fixture.snapshot, nil
				},
				&journalForbiddenRandom{}, time.Now, func(context.Context) safemode.Result { observed = true; return journalSafeObservation() })
			if code != 1 || observed {
				t.Fatalf("failure downgrade accepted: %d %s", code, stderr.String())
			}
		})
	}
}

func TestFinalizeSessionFlagsMustAppearTogether(t *testing.T) {
	for _, extra := range [][]string{{"-run-stop", "completed"}, {"-session-journal", "journal.json"}, {"-session-journal", "", "-run-stop", "completed"}, {"-session-journal", "journal.json", "-run-stop", ""}} {
		arguments := append([]string{"-fingerprint-key", "key", "-plan", "plan", "-output", "report"}, extra...)
		if code := runFinalizeTestWith(t, t.Context(), arguments, &bytes.Buffer{}, &bytes.Buffer{}, nil, nil, nil); code != 2 {
			t.Fatalf("unpaired session flags returned %d", code)
		}
	}
}

func TestSessionJournalRejectsChangedStopReasonAndOutcome(t *testing.T) {
	fixture := newJournalFixture(t, "cpu.quick")
	outcomes := map[string]string{}
	for _, worker := range fixture.journal.Workers {
		outcomes[worker.ID] = worker.Outcome
	}
	if err := validateSessionJournal(fixture.journal, fixture.frozen, outcomes, "run_deadline", fixture.directory); err == nil {
		t.Fatal("mismatched stop reason accepted")
	}
	outcomes["cpu.quick"] = "pass"
	if err := validateSessionJournal(fixture.journal, fixture.frozen, outcomes, fixture.journal.StopReason, fixture.directory); err == nil {
		t.Fatal("mismatched outcome accepted")
	}
	for _, readErr := range []error{nil, errors.New("synthetic evidence replacement")} {
		if err := validateSessionWorkerEvidence(&fixture.journal, "cpu.quick", nil, readErr, "aborted", nil); err == nil {
			t.Fatal("evidence appearing after journal validation was accepted")
		}
	}
}

func TestFinalizeSessionSupportsCleanStopReasonsAndCommandErrors(t *testing.T) {
	for _, reason := range []string{"completed", "operator_cancelled", "run_deadline", "test_deadline", "worker_stop"} {
		t.Run(reason, func(t *testing.T) {
			var selected []string
			if reason != "completed" {
				selected = []string{"cpu.quick", "memory.quick"}
			}
			fixture := newJournalFixture(t, selected...)
			fixture.journal.StopReason = reason
			if reason == "test_deadline" {
				fixture.worker(t, "cpu.quick", "interrupted", "error", nil)
			}
			if reason == "worker_stop" {
				fixture.worker(t, "cpu.quick", "completed", "error", nil)
			}
			var stderr bytes.Buffer
			code := fixture.finalize(t.Context(), fixture.args(t), &bytes.Buffer{}, &stderr,
				func(context.Context) (inventory.Snapshot, error) { return fixture.snapshot, nil },
				bytes.NewReader(bytes.Repeat([]byte{0x61}, 64)), time.Now, func(context.Context) safemode.Result { return journalSafeObservation() })
			if code != 0 {
				t.Fatalf("clean stopped session rejected: %d: %s", code, stderr.String())
			}
		})
	}
}

func TestFinalizeSessionPreservesEveryCompletedFailure(t *testing.T) {
	cpu := completeCPUEvidence()
	cpu.Status = quick.StatusFail
	setCPUFixtureFailure(&cpu)
	memory := completeMemoryEvidence()
	setMemoryFixtureFailure(&memory, 0)
	graphic := completeGraphicsEvidence()
	graphic.Status, graphic.Pattern.Status = "fail", "fail"
	display := completeDisplayEvidence()
	display.Status, display.Findings[0].Criteria["pixels"] = "fail", "fail"
	for id, evidence := range map[string]any{"cpu.quick": cpu, "memory.quick": memory, "graphics.quick": graphic, "display.operator.quick": display} {
		t.Run(id, func(t *testing.T) {
			fixture := newJournalFixture(t, id)
			fixture.worker(t, id, "completed", "fail", evidence)
			fixture.journal.StopReason = "operator_cancelled"
			var stderr bytes.Buffer
			code := fixture.finalize(t.Context(), fixture.args(t), &bytes.Buffer{}, &stderr,
				func(context.Context) (inventory.Snapshot, error) { return fixture.snapshot, nil },
				bytes.NewReader(bytes.Repeat([]byte{0x63}, 64)), time.Now, func(context.Context) safemode.Result { return journalSafeObservation() })
			if code != 0 {
				t.Fatalf("completed failure lost during cancellation: %d: %s", code, stderr.String())
			}
			encoded, err := os.ReadFile(fixture.reportPath)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
			if err != nil {
				t.Fatal(err)
			}
			var payload app.InventoryPayload
			if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
				t.Fatal(err)
			}
			for _, result := range payload.TestResults {
				if result.ID == id {
					if result.Status != app.TestFail || !bytes.Equal(result.Evidence, encodeEvidenceFixture(t, evidence)) {
						t.Fatal("completed failure evidence changed")
					}
					return
				}
			}
			t.Fatal("completed failed worker omitted")
		})
	}
}

func TestFinalizeWithoutJournalDoesNotObserveSafetyOrAddSession(t *testing.T) {
	fixture := newJournalFixture(t)
	arguments := append([]string(nil), fixture.arguments...)
	for _, worker := range fixture.journal.Workers {
		arguments = append(arguments, "-outcome", worker.ID+"="+worker.Outcome)
	}
	var stderr bytes.Buffer
	code := runFinalizeTestWith(t, t.Context(), arguments, &bytes.Buffer{}, &stderr,
		func(context.Context) (inventory.Snapshot, error) { return fixture.snapshot, nil },
		bytes.NewReader(bytes.Repeat([]byte{0x65}, 64)), time.Now)
	if code != 0 {
		t.Fatalf("old direct finalization changed: %d %s", code, stderr.String())
	}
	encoded, err := os.ReadFile(fixture.reportPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(artifact.InventoryJSON, []byte(`"session":`)) {
		t.Fatal("old finalizer emitted new session metadata")
	}
}

func (fixture *journalFixture) finalize(ctx context.Context, args []string, out, errOut io.Writer, collect snapshotCollector, random io.Reader, now func() time.Time, observe func(context.Context) safemode.Result) int {
	return runFinalizeTestOwned(fixture.t, ctx, args, out, errOut, collect, random, now, func(ctx context.Context) (sessionJournal, error) {
		if observe == nil {
			return sessionJournal{}, errors.New("missing live observation")
		}
		value := sessionevidence.CloneJournal(fixture.journal)
		value.Safety[len(value.Safety)-1].Result = observe(ctx)
		if ctx.Err() != nil || !safeSessionObservation(value.Safety[len(value.Safety)-1].Result) {
			return sessionJournal{}, errors.New("unsafe live observation")
		}
		return value, nil
	}).Code
}
