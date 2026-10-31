package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
)

// These observations occurred at 1, 2, 3 and 4 seconds, and monitoring joined
// at 5 seconds. The expected summary is specified independently of the monitor.
func journalSampledMonitor() safemode.MonitorResult {
	last := journalSafeObservation()
	return safemode.MonitorResult{
		Schema: safemode.MonitorSchema, Status: "pass", IntervalMilliseconds: 1000,
		ObservationTimeoutMilliseconds: 1000, MaximumAllowedGapMilliseconds: 2500,
		ElapsedMilliseconds: 5000, FirstCompletedMilliseconds: 1000, LastCompletedMilliseconds: 4000,
		MaximumGapMilliseconds: 1000, AttemptedObservations: 4, CompletedObservations: 4,
		CleanupConfirmed: true, LastObservation: &last,
	}
}

func TestSessionJournalRejectsMissingOrAmbiguousMonitorShape(t *testing.T) {
	fixture := newJournalFixture(t)
	fixture.journal.Monitor = journalSampledMonitor()
	setJournalFixtureSpan(&fixture.journal, 5000)
	encoded := encodeEvidenceFixture(t, fixture.journal)
	for name, change := range map[string]func([]byte) []byte{
		"missing summary": func(b []byte) []byte {
			var object map[string]json.RawMessage
			_ = json.Unmarshal(b, &object)
			delete(object, "monitor")
			b, _ = json.Marshal(object)
			return b
		},
		"null summary": func(b []byte) []byte {
			var object map[string]json.RawMessage
			_ = json.Unmarshal(b, &object)
			object["monitor"] = json.RawMessage("null")
			b, _ = json.Marshal(object)
			return b
		},
		"array summary": func(b []byte) []byte {
			var object map[string]json.RawMessage
			_ = json.Unmarshal(b, &object)
			object["monitor"] = json.RawMessage("[]")
			b, _ = json.Marshal(object)
			return b
		},
		"duplicate summary": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"monitor":`), []byte(`"monitor":{},"monitor":`), 1)
		},
		"duplicate interval": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"interval_ms":1000`), []byte(`"interval_ms":1,"interval_ms":1000`), 1)
		},
		"escaped duplicate": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"interval_ms":1000`), []byte(`"interv\u0061l_ms":1,"interval_ms":1000`), 1)
		},
		"case alias": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"interval_ms":`), []byte(`"Interval_ms":`), 1)
		},
		"unknown monitor field": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"interval_ms":`), []byte(`"discarded_unsafe_samples":1,"interval_ms":`), 1)
		},
		"missing count": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"completed_observations":4,`), nil, 1)
		},
		"null cleanup": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"cleanup_confirmed":true`), []byte(`"cleanup_confirmed":null`), 1)
		},
		"fractional count": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"completed_observations":4`), []byte(`"completed_observations":4.5`), 1)
		},
		"negative count": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"completed_observations":4`), []byte(`"completed_observations":-1`), 1)
		},
		"ambiguous last observation": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"last_observation":{`), []byte(`"last_observation":{"storage_observed":false,`), 1)
		},
		"null failure pointer": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"cleanup_confirmed":true`), []byte(`"cleanup_confirmed":true,"first_failure":null`), 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			altered := change(encoded)
			if bytes.Equal(altered, encoded) {
				t.Fatal("malformed monitor fixture did not change its target")
			}
			path := filepath.Join(t.TempDir(), "journal.json")
			if err := os.WriteFile(path, altered, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readSessionJournal(path); err == nil {
				t.Fatal("missing or ambiguous monitor summary accepted")
			}
		})
	}
}

func TestFinalizeSessionRejectsUntrustworthyMonitorBeforeSigning(t *testing.T) {
	for name, change := range map[string]func(*safemode.MonitorResult){
		"wrong schema":     func(m *safemode.MonitorResult) { m.Schema = "unknown" },
		"failed status":    func(m *safemode.MonitorResult) { m.Status = "fail" },
		"unknown status":   func(m *safemode.MonitorResult) { m.Status = "inconclusive" },
		"interval changed": func(m *safemode.MonitorResult) { m.IntervalMilliseconds = 999 },
		"timeout changed":  func(m *safemode.MonitorResult) { m.ObservationTimeoutMilliseconds = 1001 },
		"gap relaxed":      func(m *safemode.MonitorResult) { m.MaximumAllowedGapMilliseconds = 2501 },
		"truncated span":   func(m *safemode.MonitorResult) { m.ElapsedMilliseconds = 1000 },
		"negative span":    func(m *safemode.MonitorResult) { m.ElapsedMilliseconds = -1 },
		"unjoined reader":  func(m *safemode.MonitorResult) { m.CleanupConfirmed = false },
		"unfinished sample": func(m *safemode.MonitorResult) {
			m.AttemptedObservations++
		},
		"invented sample": func(m *safemode.MonitorResult) { m.AttemptedObservations-- },
		"overflow count": func(m *safemode.MonitorResult) {
			m.AttemptedObservations, m.CompletedObservations = math.MaxUint64, math.MaxUint64
		},
		"too many samples": func(m *safemode.MonitorResult) {
			m.AttemptedObservations, m.CompletedObservations = 6, 6
		},
		"zero samples over long run": func(m *safemode.MonitorResult) {
			m.AttemptedObservations, m.CompletedObservations = 0, 0
			m.FirstCompletedMilliseconds, m.LastCompletedMilliseconds = 0, 0
			m.LastObservation = nil
		},
		"missing last observation": func(m *safemode.MonitorResult) { m.LastObservation = nil },
		"unsafe latest observation": func(m *safemode.MonitorResult) {
			m.LastObservation.ActiveNonLoopInterfaces = 1
		},
		"unknown latest observation": func(m *safemode.MonitorResult) {
			m.LastObservation.StorageObserved = false
		},
		"contradictory latest observation": func(m *safemode.MonitorResult) {
			m.LastObservation.NetworkDetail = "offline network state could not be established"
		},
		"unsafe hidden by later safe": func(m *safemode.MonitorResult) {
			unsafe := journalSafeObservation()
			unsafe.WritableBlockMounts = 1
			m.FirstFailure = &safemode.MonitorFailure{Reason: "unsafe_observation", ElapsedMilliseconds: 1500, Observation: &unsafe}
		},
		"unknown hidden by later safe": func(m *safemode.MonitorResult) {
			unknown := journalSafeObservation()
			unknown.NetworkObserved = false
			m.FirstFailure = &safemode.MonitorFailure{Reason: "invalid_observation", ElapsedMilliseconds: 1500, Observation: &unknown}
		},
		"late pass after deadline": func(m *safemode.MonitorResult) {
			m.FirstFailure = &safemode.MonitorFailure{Reason: "observation_timeout", ElapsedMilliseconds: 2000, Observation: m.LastObservation}
		},
		"latched coverage gap": func(m *safemode.MonitorResult) {
			m.FirstFailure = &safemode.MonitorFailure{Reason: "sampling_gap", ElapsedMilliseconds: 2501}
		},
		"first sample before cadence": func(m *safemode.MonitorResult) { m.FirstCompletedMilliseconds = 999 },
		"reversed sample times":       func(m *safemode.MonitorResult) { m.FirstCompletedMilliseconds = 4001 },
		"sample after join":           func(m *safemode.MonitorResult) { m.LastCompletedMilliseconds = 5001 },
		"count before required ticks": func(m *safemode.MonitorResult) {
			m.LastCompletedMilliseconds = 3999
			m.MaximumGapMilliseconds = 1001
		},
		"negative gap":      func(m *safemode.MonitorResult) { m.MaximumGapMilliseconds = -1 },
		"excessive gap":     func(m *safemode.MonitorResult) { m.MaximumGapMilliseconds = 2501 },
		"underreported gap": func(m *safemode.MonitorResult) { m.MaximumGapMilliseconds = 999 },
		"uncovered trailing": func(m *safemode.MonitorResult) {
			m.AttemptedObservations, m.CompletedObservations = 2, 2
			m.LastCompletedMilliseconds, m.MaximumGapMilliseconds = 2000, 2000
		},
		"single sample inconsistent times": func(m *safemode.MonitorResult) {
			m.AttemptedObservations, m.CompletedObservations = 1, 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newJournalFixture(t)
			fixture.journal.BudgetMilliseconds = 10000
			setJournalFixtureSpan(&fixture.journal, 5000)
			fixture.journal.Monitor = journalSampledMonitor()
			change(&fixture.journal.Monitor)
			rejectedJournal := filepath.Join(fixture.directory, "rejected-monitor-journal.json")
			if err := writeSessionJournal(rejectedJournal, fixture.journal); err == nil {
				t.Fatal("unsafe monitor journal was written")
			}
			if _, err := os.Stat(rejectedJournal); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("rejected journal writer created an artifact")
			}
			collected, observed := false, false
			random := &journalForbiddenRandom{}
			var stdout, stderr bytes.Buffer
			code := fixture.finalize(t.Context(), fixture.args(t), &stdout, &stderr,
				func(context.Context) (inventory.Snapshot, error) { collected = true; return fixture.snapshot, nil },
				random, time.Now, func(context.Context) safemode.Result { observed = true; return journalSafeObservation() })
			if code != 1 || !collected || !observed || random.calls != 0 || stdout.Len() != 0 {
				t.Fatalf("untrusted monitor reached signing: code=%d collected=%v observed=%v entropy=%d stdout=%d", code, collected, observed, random.calls, stdout.Len())
			}
			if _, err := os.Stat(fixture.reportPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("report exists after monitor rejection")
			}
		})
	}
}

func TestSessionMonitorCoverageRejectsImpossibleBoundaries(t *testing.T) {
	for _, item := range []struct {
		name                      string
		elapsed, first, last, gap int64
		count                     uint64
		observation               bool
	}{
		{name: "no tick despite elapsed cadence", elapsed: 1000, gap: 1000},
		{name: "zero samples with invented observation", elapsed: 100, gap: 100, observation: true},
		{name: "zero samples hide leading gap", elapsed: 100, gap: 99},
		{name: "zero samples invent timestamps", elapsed: 100, first: 1, last: 1, gap: 100},
		{name: "single sample hides leading gap", elapsed: 1800, first: 1200, last: 1200, gap: 1199, count: 1, observation: true},
		{name: "single sample invents gap", elapsed: 1800, first: 1200, last: 1200, gap: 1201, count: 1, observation: true},
		{name: "internal gaps cannot span duration", elapsed: 5500, first: 1000, last: 5000, gap: 1999, count: 3, observation: true},
		{name: "gap larger than any possible segment", elapsed: 3500, first: 2000, last: 3000, gap: 2001, count: 2, observation: true},
	} {
		t.Run(item.name, func(t *testing.T) {
			monitor := journalShortMonitor()
			monitor.ElapsedMilliseconds, monitor.FirstCompletedMilliseconds, monitor.LastCompletedMilliseconds = item.elapsed, item.first, item.last
			monitor.MaximumGapMilliseconds = item.gap
			monitor.AttemptedObservations, monitor.CompletedObservations = item.count, item.count
			if item.observation {
				last := journalSafeObservation()
				monitor.LastObservation = &last
			}
			if err := validateSessionMonitor(monitor, item.elapsed); err == nil {
				t.Fatal("impossible monitor coverage accepted")
			}
		})
	}
}

func TestFinalizeSessionSignsHonestMonitorCoverageAndFreshSafety(t *testing.T) {
	for _, item := range []struct {
		name                      string
		elapsed, first, last, gap int64
		count                     uint64
	}{
		{name: "immediate all-off", elapsed: 0},
		{name: "short no tick", elapsed: 999, gap: 999},
		{name: "one completed sample", elapsed: 2200, first: 1450, last: 1450, gap: 1450, count: 1},
		{name: "four completed samples", elapsed: 5000, first: 1000, last: 4000, gap: 1000, count: 4},
		{name: "queued completion in same millisecond", elapsed: 2100, first: 2000, last: 2000, gap: 2000, count: 2},
	} {
		t.Run(item.name, func(t *testing.T) {
			fixture := newJournalFixture(t)
			fixture.journal.BudgetMilliseconds = 10000
			setJournalFixtureSpan(&fixture.journal, item.elapsed)
			monitor := &fixture.journal.Monitor
			monitor.ElapsedMilliseconds, monitor.FirstCompletedMilliseconds, monitor.LastCompletedMilliseconds = item.elapsed, item.first, item.last
			monitor.MaximumGapMilliseconds = item.gap
			monitor.AttemptedObservations, monitor.CompletedObservations = item.count, item.count
			if item.count > 0 {
				last := journalSafeObservation()
				monitor.LastObservation = &last
			}
			observed := 0
			var stdout, stderr bytes.Buffer
			code := fixture.finalize(t.Context(), fixture.args(t), &stdout, &stderr,
				func(context.Context) (inventory.Snapshot, error) { return fixture.snapshot, nil },
				bytes.NewReader(bytes.Repeat([]byte{0x71}, 64)), time.Now,
				func(context.Context) safemode.Result { observed++; return journalSafeObservation() })
			if code != 0 || observed != 1 {
				t.Fatalf("honest monitoring rejected: code=%d observed=%d: %s", code, observed, stderr.String())
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
			if json.Unmarshal(artifact.InventoryJSON, &payload) != nil || len(payload.TestResults) != 23 {
				t.Fatal("monitor summary changed frozen result coverage")
			}
			for _, result := range payload.TestResults {
				if result.ID != "run.integrity.quick" {
					continue
				}
				var evidence struct {
					Session *sessionJournal `json:"session"`
				}
				if json.Unmarshal(result.Evidence, &evidence) != nil || evidence.Session == nil ||
					evidence.Session.Schema != "hhsoft.pc-tester.quick-session/v11" || evidence.Session.StopReason != "operator_cancelled" ||
					!bytes.Equal(encodeEvidenceFixture(t, evidence.Session.Monitor), encodeEvidenceFixture(t, *monitor)) ||
					evidence.Session.Safety[len(evidence.Session.Safety)-1].Phase != "pre-sign" {
					t.Fatal("signed monitor or fresh safety binding changed")
				}
				return
			}
			t.Fatal("signed session integrity result missing")
		})
	}
}

func setJournalFixtureSpan(j *sessionJournal, elapsed int64) {
	j.ElapsedMilliseconds = elapsed
	j.PreparationStartedMilliseconds = elapsed
	j.InventoryPreparedMilliseconds = elapsed
	j.StopRequestedMilliseconds = elapsed
	j.SafetyJoinedMilliseconds = elapsed
	j.Thermal = sessionThermalSummary(elapsed)
}
