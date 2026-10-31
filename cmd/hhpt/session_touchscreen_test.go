package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchscreenevidence"
)

func touchscreenRouteChangeTest(frozen *plan.Plan, change func(*plan.Test)) {
	for i := range frozen.Tests {
		if frozen.Tests[i].ID == "input.touchscreen.quick" {
			change(&frozen.Tests[i])
			return
		}
	}
	panic("explicit touchscreen fixture missing")
}

func TestTouchscreenWorkerPublicationRequiresExactSuccessfulBoundary(t *testing.T) {
	for _, name := range []string{"pass", "inconclusive", "deadline", "aborted", "absent_error", "absent_aborted"} {
		t.Run(name, func(t *testing.T) {
			value := touchscreenRouteValue(t)
			raw := []byte(nil)
			readErr := error(nil)
			interrupted := false
			want, state := "pass", "completed"
			switch name {
			case "inconclusive":
				value.Status, value.StopReason, value.Detail = inputtest.StatusInconclusive, "observation_timeout", "observation_timeout"
				want = "inconclusive"
			case "deadline":
				value.Status, value.StopReason, value.Detail = inputtest.StatusError, "deadline", "deadline"
				want = "error"
			case "aborted":
				value.Status, value.StopReason, value.Detail = inputtest.StatusAborted, "operator_cancelled", "operator_cancelled"
				want, state, interrupted = "aborted", "interrupted", true
			case "absent_error":
				readErr = os.ErrNotExist
				want = "error"
			case "absent_aborted":
				readErr = os.ErrNotExist
				want, state, interrupted = "aborted", "interrupted", true
			}
			if readErr == nil {
				raw = touchscreenRouteEncode(t, value)
			}
			out, gotState, err := touchscreenWorkerOutcomeEvidence(raw, readErr, 0, interrupted)
			if err != nil || out != want || gotState != state {
				t.Fatalf("valid worker boundary rejected: %v", err)
			}
		})
	}
	cases := []struct {
		name   string
		change func(*[]byte, *error, *int)
	}{
		{"nonzero_with_pass", func(_ *[]byte, _ *error, c *int) { *c = 1 }},
		{"negative_exit", func(_ *[]byte, _ *error, c *int) { *c = -1 }},
		{"malformed", func(r *[]byte, _ *error, _ *int) { *r = []byte("{") }},
		{"missing_present", func(r *[]byte, _ *error, _ *int) { *r = nil }},
		{"wrapped_absence", func(r *[]byte, e *error, _ *int) { *r = nil; *e = fmt.Errorf("wrapped: %w", os.ErrNotExist) }},
		{"absence_with_bytes", func(_ *[]byte, e *error, _ *int) { *e = os.ErrNotExist }},
		{"read_error", func(r *[]byte, e *error, _ *int) { *r = nil; *e = errors.New("synthetic read failure") }},
		{"changed_limit", func(r *[]byte, _ *error, _ *int) { *r = bytes.Replace(*r, []byte("45000"), []byte("45001"), 1) }},
		{"duplicate_claim", func(r *[]byte, _ *error, _ *int) {
			*r = bytes.Replace(*r, []byte(`"schema":`), []byte(`"schema":"hhsoft.pc-tester.touchscreen-functional/v1","schema":`), 1)
		}},
		{"schema_alias", func(r *[]byte, _ *error, _ *int) { *r = bytes.Replace(*r, []byte(`"schema":`), []byte(`"Schema":`), 1) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, readErr, code := []byte(touchscreenRouteTouchscreenLiteral), error(nil), 0
			c.change(&raw, &readErr, &code)
			if _, _, err := touchscreenWorkerOutcomeEvidence(raw, readErr, code, false); err == nil {
				t.Fatal("uncertain publication accepted")
			}
		})
	}
	t.Run("cleanup_exit_overrides_malformed_read", func(t *testing.T) {
		if _, _, err := touchscreenWorkerOutcomeEvidence([]byte("{"), errors.New("synthetic read"), workerCleanupUncertainExitCode, false); !errors.Is(err, touchscreenevidence.ErrCleanupUnconfirmed) {
			t.Fatal("cleanup exit was masked")
		}
	})
	t.Run("typed_cleanup_overrides_nonzero", func(t *testing.T) {
		v := touchscreenRouteValue(t)
		v.CleanupConfirmed = false
		v.Status = inputtest.StatusError
		v.StopReason, v.Detail = "cleanup_unconfirmed", "cleanup_unconfirmed"
		if _, _, err := touchscreenWorkerOutcomeEvidence(touchscreenRouteEncode(t, v), nil, 1, false); !errors.Is(err, touchscreenevidence.ErrCleanupUnconfirmed) {
			t.Fatal("typed cleanup was masked")
		}
	})
}

func TestTouchscreenWorkerRecordBindsDurationAndCleanup(t *testing.T) {
	valid := func() sessionWorkerRecord {
		return sessionWorkerRecord{ID: "input.touchscreen.quick", State: "completed", Outcome: "pass", Timing: &sessionevidence.WorkerTiming{StartedMilliseconds: 10, FinishedMilliseconds: 110}}
	}
	t.Run("exact_interval", func(t *testing.T) {
		journal := sessionJournal{Schema: "hhsoft.pc-tester.quick-session/v11", Workers: []sessionWorkerRecord{valid()}}
		if err := validateSessionWorkerEvidence(&journal, "input.touchscreen.quick", []byte(touchscreenRouteTouchscreenLiteral), nil, "pass", nil); err != nil {
			t.Fatal(err)
		}
	})
	cases := []struct {
		name   string
		change func(*sessionWorkerRecord, *[]byte, *error, *string, *error)
	}{
		{"short_interval", func(w *sessionWorkerRecord, _ *[]byte, _ *error, _ *string, _ *error) {
			w.Timing.FinishedMilliseconds = 109
		}},
		{"negative_start", func(w *sessionWorkerRecord, _ *[]byte, _ *error, _ *string, _ *error) {
			w.Timing.StartedMilliseconds = -1
		}},
		{"reversed_interval", func(w *sessionWorkerRecord, _ *[]byte, _ *error, _ *string, _ *error) {
			w.Timing.FinishedMilliseconds = 9
		}},
		{"nil_timing", func(w *sessionWorkerRecord, _ *[]byte, _ *error, _ *string, _ *error) { w.Timing = nil }},
		{"wrong_id", func(w *sessionWorkerRecord, _ *[]byte, _ *error, _ *string, _ *error) { w.ID = "input.pointer.quick" }},
		{"wrong_state", func(w *sessionWorkerRecord, _ *[]byte, _ *error, _ *string, _ *error) { w.State = "running" }},
		{"wrong_outcome", func(w *sessionWorkerRecord, _ *[]byte, _ *error, _ *string, _ *error) { w.Outcome = "inconclusive" }},
		{"wrong_status", func(_ *sessionWorkerRecord, _ *[]byte, _ *error, s *string, _ *error) { *s = "error" }},
		{"status_error", func(_ *sessionWorkerRecord, _ *[]byte, _ *error, _ *string, e *error) {
			*e = errors.New("synthetic status uncertainty")
		}},
		{"wrapped_absence", func(_ *sessionWorkerRecord, r *[]byte, e *error, _ *string, _ *error) {
			*r = nil
			*e = fmt.Errorf("wrapped: %w", os.ErrNotExist)
		}},
		{"absent_pass", func(_ *sessionWorkerRecord, r *[]byte, e *error, _ *string, _ *error) { *r = nil; *e = os.ErrNotExist }},
		{"limit", func(_ *sessionWorkerRecord, r *[]byte, _ *error, _ *string, _ *error) {
			*r = bytes.Replace(*r, []byte("45000"), []byte("45001"), 1)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, raw, readErr, status, statusErr := valid(), []byte(touchscreenRouteTouchscreenLiteral), error(nil), "pass", error(nil)
			c.change(&w, &raw, &readErr, &status, &statusErr)
			if err := validateTouchscreenWorkerRecord(w, raw, readErr, status, statusErr); err == nil {
				t.Fatal("invalid worker binding accepted")
			}
		})
	}
	for _, name := range []string{"not_started", "not_scheduled", "error", "aborted"} {
		t.Run(name, func(t *testing.T) {
			w := valid()
			if name == "not_started" || name == "not_scheduled" {
				w.State = name
				w.Timing = nil
			} else {
				w.Outcome = name
			}
			if err := validateTouchscreenWorkerRecord(w, nil, os.ErrNotExist, name, nil); err != nil {
				t.Fatal(err)
			}
			if err := validateTouchscreenWorkerRecord(w, []byte(touchscreenRouteTouchscreenLiteral), os.ErrNotExist, name, nil); err == nil {
				t.Fatal("absence carried bytes")
			}
		})
	}
	t.Run("cleanup_precedes_bad_record", func(t *testing.T) {
		if err := validateTouchscreenWorkerRecord(sessionWorkerRecord{}, nil, os.ErrNotExist, "", errors.Join(errors.New("other"), touchscreenevidence.ErrCleanupUnconfirmed)); !errors.Is(err, touchscreenevidence.ErrCleanupUnconfirmed) {
			t.Fatal("cleanup uncertainty was replaced")
		}
	})
	t.Run("old_schema_cannot_smuggle_touchscreen", func(t *testing.T) {
		journal := sessionJournal{Schema: "hhsoft.pc-tester.quick-session/v9", Workers: []sessionWorkerRecord{valid()}}
		if err := validateSessionWorkerEvidence(&journal, "input.touchscreen.quick", []byte(touchscreenRouteTouchscreenLiteral), nil, "pass", nil); err == nil {
			t.Fatal("old schema admitted touchscreen worker")
		}
	})
}

func TestTouchscreenProfileAndJournalClaimsRejectAliases(t *testing.T) {
	for _, name := range []string{"old_schema", "wrong_profile", "quick_has_stress", "standard_lacks_profile", "worker_duplicate", "worker_missing", "outcome_changed", "planned_status_changed", "version_changed"} {
		t.Run(name, func(t *testing.T) {
			profile := plan.ProfileQuick
			schema, ids := "hhsoft.pc-tester.quick-session/v11", touchscreenRouteTouchscreenQuickWorkers
			if name == "standard_lacks_profile" {
				profile = plan.ProfileStandard
				schema, ids = "hhsoft.pc-tester.quick-session/v12", touchscreenRouteTouchscreenStandardWorkers
			}
			frozen, _, _ := touchscreenRouteCurrentPlan(t, profile)
			for i := range frozen.Tests {
				frozen.Tests[i].Status = plan.TestExcluded
			}
			journal := touchscreenRouteJournal(t, schema, ids)
			journal.PlanID = frozen.ID
			outcomes := map[string]string{}
			for _, w := range journal.Workers {
				outcomes[w.ID] = w.Outcome
			}
			if err := validateSessionJournalForProfile(journal, frozen, outcomes, "completed", t.TempDir(), profile); err != nil {
				t.Fatal("valid current journal prerequisite rejected", err)
			}
			switch name {
			case "old_schema":
				journal.Schema = "hhsoft.pc-tester.quick-session/v9"
			case "wrong_profile":
				journal.Profile = "standard"
			case "quick_has_stress":
				journal.CPUStressSource = &sessionevidence.StressSource{}
			case "standard_lacks_profile":
				journal.Profile = ""
			case "worker_duplicate":
				journal.Workers[0] = journal.Workers[1]
			case "worker_missing":
				journal.Workers = journal.Workers[:len(journal.Workers)-1]
			case "outcome_changed":
				outcomes["input.touchscreen.quick"] = "unsupported"
			case "planned_status_changed":
				touchscreenRouteChangeTest(&frozen, func(v *plan.Test) { v.Status = plan.TestUnsupported })
			case "version_changed":
				touchscreenRouteChangeTest(&frozen, func(v *plan.Test) { v.Version = 1 })
			}
			if err := validateSessionJournalForProfile(journal, frozen, outcomes, "completed", t.TempDir(), profile); err == nil {
				t.Fatal("mismatched journal accepted")
			}
		})
	}
}

func TestTouchscreenFixedWorkerDispatchHonorsStopBeforeAcquisition(t *testing.T) {
	for _, name := range []string{"cancelled", "expired", "invalid_timeout", "unknown_worker"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			cancel := func() {}
			worker := sessionWorker{ID: "input.touchscreen.quick", Architecture: "x86_64", OutputPath: "-", Timeout: 120 * time.Second}
			switch name {
			case "cancelled":
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "expired":
				ctx, cancel = context.WithDeadline(ctx, time.Unix(0, 0))
			case "invalid_timeout":
				worker.Timeout = 44 * time.Second
			case "unknown_worker":
				worker.ID = "input.touchscreen.quick.alias"
			}
			defer cancel()
			var out, diagnostic bytes.Buffer
			code := runSessionWorker(ctx, worker, commandRouteNoConsoleRead{}, &out, &diagnostic)
			if name == "invalid_timeout" || name == "unknown_worker" {
				if code != 2 || out.Len() != 0 {
					t.Fatal("invalid dispatch reached acquisition/publication")
				}
				return
			}
			value, err := touchscreenevidence.Decode(out.Bytes())
			want := "aborted"
			if name == "expired" {
				want = "error"
			}
			if code != 0 || err != nil || string(value.Status) != want || value.LimitMilliseconds != 45000 || len(value.Devices) != 0 || !value.CleanupConfirmed {
				t.Fatalf("pre-stopped fixed worker changed: code=%d err=%v", code, err)
			}
			if strings.Contains(diagnostic.String(), "Touchscreen check:") {
				t.Fatal("pre-stopped worker issued acquisition instructions")
			}
		})
	}
}

const touchscreenRouteTouchscreenQuickWorkers = "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
const touchscreenRouteTouchscreenStandardWorkers = "cpu.quick cpu.stress memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
const touchscreenRouteTouchscreenQuickResults = "audio.readiness.quick cpu.quick display.operator.quick graphics.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick input.readiness.quick inspection.exterior.quick memory.quick network.readiness.quick storage.readiness.quick usb.readiness.quick"
const touchscreenRouteTouchscreenStandardResults = "audio.readiness.quick cpu.quick cpu.stress display.operator.quick graphics.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick input.readiness.quick inspection.exterior.quick memory.quick network.readiness.quick storage.readiness.quick usb.readiness.quick"

func TestTouchscreenCommandCurrentAndHistoricalLayoutContracts(t *testing.T) {
	for _, row := range []struct {
		name                     string
		profile                  plan.Profile
		schema, workers, results string
		current                  bool
	}{
		{"quick_v9", plan.ProfileQuick, "hhsoft.pc-tester.quick-session/v9", commandRouteTouchpadQuickWorkers, commandRouteTouchpadQuickResults, false},
		{"standard_v10", plan.ProfileStandard, "hhsoft.pc-tester.quick-session/v10", commandRouteTouchpadStandardWorkers, commandRouteTouchpadStandardResults, false},
		{"quick_v11", plan.ProfileQuick, "hhsoft.pc-tester.quick-session/v11", touchscreenRouteTouchscreenQuickWorkers, touchscreenRouteTouchscreenQuickResults, true},
		{"standard_v12", plan.ProfileStandard, "hhsoft.pc-tester.quick-session/v12", touchscreenRouteTouchscreenStandardWorkers, touchscreenRouteTouchscreenStandardResults, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			frozen, _, _ := touchscreenRouteCurrentPlan(t, row.profile)
			if !row.current {
				touchscreenRouteChangeTest(&frozen, func(v *plan.Test) { v.Version = 1; v.Status = plan.TestUnsupported })
			}
			layout, err := commandLayoutForPlan(frozen, row.profile)
			if err != nil || !layout.touchpad || layout.touchscreen != row.current ||
				layout.schema != row.schema || !reflect.DeepEqual(layout.workerIDs, strings.Fields(row.workers)) {
				t.Fatal("literal typed layout changed", err)
			}
			var ids []string
			for _, definition := range layout.definitions {
				ids = append(ids, definition.id)
				if definition.id == "input.touchscreen.quick" &&
					(definition.filename != "touchscreen.json" || definition.schema != "hhsoft.pc-tester.touchscreen-functional/v1") {
					t.Fatal("fixed touchscreen evidence route changed")
				}
			}
			if !reflect.DeepEqual(ids, strings.Fields(row.results)) {
				t.Fatal("literal result order changed")
			}
			journal := touchscreenRouteJournal(t, row.schema, row.workers)
			before, err := journal.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := sessionevidence.Decode(before)
			if err != nil || !reflect.DeepEqual(decoded, journal) {
				t.Fatal("historical/current journal roundtrip changed", err)
			}
			if currentSessionProfile(journal, row.profile) != row.current {
				t.Fatal("historical decoding became current execution admission")
			}
			layout.workerIDs[0] = "mutated"
			layout.definitions[0].id = "mutated"
			again, err := commandLayoutForPlan(frozen, row.profile)
			if err != nil || again.workerIDs[0] != "cpu.quick" || again.definitions[0].id != "audio.readiness.quick" {
				t.Fatal("layout aliases caller data")
			}
		})
	}
	if sessionevidence.Schema != "hhsoft.pc-tester.quick-session/v11" || sessionJournalSchema != "hhsoft.pc-tester.quick-session/v11" {
		t.Fatal("current default schema is not v11")
	}
}

func TestTouchscreenCommandLayoutRejectsUnadmittedDefinition(t *testing.T) {
	for _, name := range []string{"duplicate", "old_scheduled", "future_version", "capability", "estimate", "timeout", "destructive", "without_touchpad"} {
		t.Run(name, func(t *testing.T) {
			frozen, _, _ := touchscreenRouteCurrentPlan(t, plan.ProfileQuick)
			if _, err := commandLayoutForPlan(frozen, plan.ProfileQuick); err != nil {
				t.Fatal("valid current layout prerequisite", err)
			}
			switch name {
			case "duplicate":
				frozen.Tests = append(frozen.Tests, touchscreenRouteTest(t, frozen))
			case "without_touchpad":
				commandRouteChangeTest(&frozen, func(v *plan.Test) { v.Version = 1; v.Status = plan.TestUnsupported })
			default:
				touchscreenRouteChangeTest(&frozen, func(v *plan.Test) {
					switch name {
					case "old_scheduled":
						v.Version = 1
					case "future_version":
						v.Version = 3
					case "capability":
						v.Capability = "input.touchpad"
					case "estimate":
						v.EstimatedSeconds = 44
					case "timeout":
						v.TimeoutSeconds = 119
					case "destructive":
						v.Destructive = true
					}
				})
			}
			if _, err := commandLayoutForPlan(frozen, plan.ProfileQuick); err == nil {
				t.Fatal("unadmitted definition selected a current layout")
			}
		})
	}
}

func TestTouchscreenCheckedJournalAndReadyMarkerRequireCurrentBinding(t *testing.T) {
	for _, row := range []struct {
		name        string
		profile     plan.Profile
		schema, ids string
	}{
		{"quick", plan.ProfileQuick, "hhsoft.pc-tester.quick-session/v11", touchscreenRouteTouchscreenQuickWorkers},
		{"standard", plan.ProfileStandard, "hhsoft.pc-tester.quick-session/v12", touchscreenRouteTouchscreenStandardWorkers},
	} {
		t.Run(row.name, func(t *testing.T) {
			frozen, _, _ := touchscreenRouteCurrentPlan(t, row.profile)
			// Internal journal binding fixture; this does not authorize execution.
			for i := range frozen.Tests {
				frozen.Tests[i].Status = plan.TestExcluded
			}
			journal := touchscreenRouteJournal(t, row.schema, row.ids)
			journal.PlanID = frozen.ID
			outcomes := map[string]string{}
			for _, worker := range journal.Workers {
				outcomes[worker.ID] = worker.Outcome
			}
			directory := t.TempDir()
			if err := validateSessionJournalForProfile(journal, frozen, outcomes, "completed", directory, row.profile); err != nil {
				t.Fatal("valid current journal prerequisite", err)
			}
			count := uint64(len(frozen.HardwareGraph.Components))
			integrity := sessionIntegrity{Status: "pass", PlanID: frozen.ID, PlannedComponents: count, FinalComponents: count}
			output := &sessionFinalOutput{destination: io.Discard}
			fmt.Fprintf(output, "HHPT_RUN_INTEGRITY status=pass plan=%s planned_components=%d final_components=%d added=0 removed=0 changed=0\n", frozen.ID, count, count)
			path := filepath.Join(directory, "ready")
			if err := writeSessionReady(path, "report.hhpcr", "route-fixture", frozen, journal, integrity, output); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil || !bytes.Contains(raw, []byte(" touchpad=excluded")) ||
				!bytes.Contains(raw, []byte(" touchscreen=excluded")) || strings.Count(string(raw), "HHPT_READY ") != 1 ||
				strings.Count(string(raw), "\n") != 1 {
				t.Fatal("complete current ready marker changed", err)
			}
			bad := sessionevidence.CloneJournal(journal)
			bad.PlanID = "sha256:" + strings.Repeat("2", 64)
			if err := validateSessionJournalForProfile(bad, frozen, outcomes, "completed", directory, row.profile); err == nil {
				t.Fatal("journal plan mismatch accepted")
			}
			if err := validateSessionJournalForProfile(journal, frozen, outcomes, "operator_cancelled", directory, row.profile); err == nil {
				t.Fatal("journal stop mismatch accepted")
			}
			if err := os.WriteFile(filepath.Join(directory, "touchscreen.json"), []byte(touchscreenRouteTouchscreenLiteral), 0600); err != nil {
				t.Fatal(err)
			}
			if err := validateSessionJournalForProfile(journal, frozen, outcomes, "completed", directory, row.profile); err == nil {
				t.Fatal("unstarted worker acquired an observed result")
			}
		})
	}
}

func touchscreenRouteJournal(t *testing.T, schema, ids string) sessionJournal {
	t.Helper()
	var rows []string
	for _, id := range strings.Fields(ids) {
		rows = append(rows, fmt.Sprintf(`{"id":%q,"state":"not_scheduled","outcome":"excluded"}`, id))
	}
	raw := `{"schema":"` + schema + commandRouteJournalPrefix + strings.Join(rows, ",") + commandRouteJournalTail
	switch schema {
	case "hhsoft.pc-tester.quick-session/v6":
		raw += `,"storage_source":null`
	case "hhsoft.pc-tester.quick-session/v8", "hhsoft.pc-tester.quick-session/v10", "hhsoft.pc-tester.quick-session/v12":
		raw += `,"profile":"standard","cpu_stress_source":null`
	}
	value, err := sessionevidence.Decode([]byte(raw + "}"))
	if err != nil {
		t.Fatal(err)
	}
	return value
}
