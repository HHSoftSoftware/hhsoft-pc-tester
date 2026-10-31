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
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchpadevidence"
)

const commandRouteQuickWorkers = "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
const commandRouteStandardWorkers = "cpu.quick cpu.stress memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
const commandRouteTouchpadQuickWorkers = "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
const commandRouteTouchpadStandardWorkers = "cpu.quick cpu.stress memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
const commandRouteQuickResults = "audio.readiness.quick cpu.quick display.operator.quick graphics.quick input.keyboard.quick input.pointer.quick input.readiness.quick inspection.exterior.quick memory.quick network.readiness.quick storage.readiness.quick usb.readiness.quick"
const commandRouteStandardResults = "audio.readiness.quick cpu.quick cpu.stress display.operator.quick graphics.quick input.keyboard.quick input.pointer.quick input.readiness.quick inspection.exterior.quick memory.quick network.readiness.quick storage.readiness.quick usb.readiness.quick"
const commandRouteTouchpadQuickResults = "audio.readiness.quick cpu.quick display.operator.quick graphics.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.readiness.quick inspection.exterior.quick memory.quick network.readiness.quick storage.readiness.quick usb.readiness.quick"
const commandRouteTouchpadStandardResults = "audio.readiness.quick cpu.quick cpu.stress display.operator.quick graphics.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.readiness.quick inspection.exterior.quick memory.quick network.readiness.quick storage.readiness.quick usb.readiness.quick"

// These finite wire fixtures use literal historical order and timing, not the
// implementation's worker arrays. They make no actual monitoring or signed-run claim.
const commandRouteJournalPrefix = `","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","stop_reason":"completed","budget_milliseconds":900000,"elapsed_milliseconds":600050,"workers":[`
const commandRouteJournalTail = `],"safety":[{"phase":"preflight","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"final","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"pre-sign","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}}],"monitor":{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"pass","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":600,"completed_observations":600,"elapsed_ms":600046,"first_completed_ms":1000,"last_completed_ms":600000,"maximum_gap_ms":1000,"cleanup_confirmed":true,"last_observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},"preparation_started_ms":600020,"inventory_prepared_ms":600030,"stop_requested_ms":600040,"safety_joined_ms":600045,"thermal":{"schema":"hhsoft.pc-tester.run-thermal/v1","status":"completed","limit_millicelsius":90000,"interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"stop_timeout_ms":2000,"attempted_observations":602,"completed_observations":602,"periodic_attempts":600,"periodic_completions":600,"elapsed_ms":600048,"first_completed_ms":1,"last_completed_ms":600032,"maximum_gap_ms":1000,"initial":{"requested_ms":0,"attempted":true,"completed":true,"started_ms":0,"completed_ms":1,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"final":{"requested_ms":600031,"attempted":true,"completed":true,"started_ms":600031,"completed_ms":600032,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"aggregate":{"coverage":"unavailable","accepted_observations":602,"available_observations":0,"partial_observations":0,"unavailable_observations":602,"maximum_sensors":0,"valid_readings":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0},"cleanup_confirmed":true,"joined":true,"joined_ms":600047,"last_observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"local_thermal_sources":[]`

func commandRouteJournal(t *testing.T, schema, ids string) sessionJournal {
	t.Helper()
	var rows []string
	for _, id := range strings.Fields(ids) {
		rows = append(rows, fmt.Sprintf(`{"id":%q,"state":"not_scheduled","outcome":"excluded"}`, id))
	}
	raw := `{"schema":"` + schema + commandRouteJournalPrefix + strings.Join(rows, ",") + commandRouteJournalTail
	switch schema {
	case "hhsoft.pc-tester.quick-session/v6":
		raw += `,"storage_source":null`
	case "hhsoft.pc-tester.quick-session/v8", "hhsoft.pc-tester.quick-session/v10":
		raw += `,"profile":"standard","cpu_stress_source":null`
	}
	value, err := sessionevidence.Decode([]byte(raw + "}"))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func commandRouteChangeTest(frozen *plan.Plan, change func(*plan.Test)) {
	for i := range frozen.Tests {
		if frozen.Tests[i].ID == "input.touchpad.quick" {
			change(&frozen.Tests[i])
			return
		}
	}
	panic("explicit touchpad fixture missing")
}

func TestTouchpadCommandLayoutKeepsLiteralOldAndNewOrders(t *testing.T) {
	cases := []struct {
		name                     string
		profile                  plan.Profile
		future                   bool
		schema, workers, results string
	}{
		{"quick_v7", plan.ProfileQuick, false, "hhsoft.pc-tester.quick-session/v7", commandRouteQuickWorkers, commandRouteQuickResults},
		{"standard_v8", plan.ProfileStandard, false, "hhsoft.pc-tester.quick-session/v8", commandRouteStandardWorkers, commandRouteStandardResults},
		{"quick_v9", plan.ProfileQuick, true, "hhsoft.pc-tester.quick-session/v9", commandRouteTouchpadQuickWorkers, commandRouteTouchpadQuickResults},
		{"standard_v10", plan.ProfileStandard, true, "hhsoft.pc-tester.quick-session/v10", commandRouteTouchpadStandardWorkers, commandRouteTouchpadStandardResults},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			frozen, _, _ := commandRouteCurrentPlan(t, c.profile)
			if !c.future {
				commandRouteChangeTest(&frozen, func(v *plan.Test) { v.Version = 1; v.Status = plan.TestUnsupported })
			}
			layout, err := commandLayoutForPlan(frozen, c.profile)
			if err != nil || layout.schema != c.schema || layout.touchpad != c.future || !reflect.DeepEqual(layout.workerIDs, strings.Fields(c.workers)) {
				t.Fatalf("literal layout mismatch: %v", err)
			}
			ids := make([]string, len(layout.definitions))
			for i, d := range layout.definitions {
				ids[i] = d.id
				if d.id == "input.touchpad.quick" && (d.filename != "touchpad.json" || d.schema != "hhsoft.pc-tester.touchpad-functional/v1") {
					t.Fatal("fixed touchpad publication changed")
				}
			}
			if !reflect.DeepEqual(ids, strings.Fields(c.results)) {
				t.Fatal("result order changed")
			}
			layout.workerIDs[0] = "mutated"
			layout.definitions[0].id = "mutated"
			again, err := commandLayoutForPlan(frozen, c.profile)
			if err != nil || again.workerIDs[0] != "cpu.quick" || again.definitions[0].id != "audio.readiness.quick" {
				t.Fatal("layout exposes shared storage")
			}
		})
	}
	if sessionevidence.Schema != "hhsoft.pc-tester.quick-session/v9" || sessionJournalSchema != "hhsoft.pc-tester.quick-session/v9" {
		t.Fatal("current quick schema is not v9")
	}
}

func TestTouchpadCommandLayoutRejectsUnadmittedDefinitions(t *testing.T) {
	cases := []struct {
		name   string
		change func(*plan.Plan, *plan.Profile)
	}{
		{"duplicate", func(p *plan.Plan, _ *plan.Profile) { p.Tests = append(p.Tests, p.Tests...) }},
		{"empty_id", func(p *plan.Plan, _ *plan.Profile) { p.Tests[0].ID = "" }},
		{"old_scheduled", func(p *plan.Plan, _ *plan.Profile) { commandRouteChangeTest(p, func(v *plan.Test) { v.Version = 1 }) }},
		{"future_version", func(p *plan.Plan, _ *plan.Profile) { commandRouteChangeTest(p, func(v *plan.Test) { v.Version = 3 }) }},
		{"zero_version", func(p *plan.Plan, _ *plan.Profile) { commandRouteChangeTest(p, func(v *plan.Test) { v.Version = 0 }) }},
		{"capability", func(p *plan.Plan, _ *plan.Profile) {
			commandRouteChangeTest(p, func(v *plan.Test) { v.Capability = "input.pointer" })
		}},
		{"destructive", func(p *plan.Plan, _ *plan.Profile) {
			commandRouteChangeTest(p, func(v *plan.Test) { v.Destructive = true })
		}},
		{"estimate", func(p *plan.Plan, _ *plan.Profile) {
			commandRouteChangeTest(p, func(v *plan.Test) { v.EstimatedSeconds = 29 })
		}},
		{"timeout", func(p *plan.Plan, _ *plan.Profile) {
			commandRouteChangeTest(p, func(v *plan.Test) { v.TimeoutSeconds = 89 })
		}},
		{"status_alias", func(p *plan.Plan, _ *plan.Profile) {
			commandRouteChangeTest(p, func(v *plan.Test) { v.Status = "Scheduled" })
		}},
		{"schema", func(p *plan.Plan, _ *plan.Profile) { p.SchemaVersion = 0 }},
		{"unfrozen", func(p *plan.Plan, _ *plan.Profile) { p.Frozen = false }},
		{"empty_plan_id", func(p *plan.Plan, _ *plan.Profile) { p.ID = "" }},
		{"profile_mismatch", func(_ *plan.Plan, p *plan.Profile) { *p = plan.ProfileStandard }},
		{"extended", func(p *plan.Plan, q *plan.Profile) { p.Profile = plan.ProfileExtended; *q = plan.ProfileExtended }},
		{"destructive_profile", func(p *plan.Plan, q *plan.Profile) { p.Profile = plan.ProfileDestructive; *q = plan.ProfileDestructive }},
		{"profile_alias", func(_ *plan.Plan, p *plan.Profile) { *p = "QUICK" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			frozen, _, _ := commandRouteCurrentPlan(t, plan.ProfileQuick)
			profile := plan.ProfileQuick
			c.change(&frozen, &profile)
			if _, err := commandLayoutForPlan(frozen, profile); err == nil {
				t.Fatal("unadmitted definition selected a route")
			}
		})
	}
	for _, name := range []string{"excluded", "not_present", "unsupported", "missing_driver"} {
		t.Run(name, func(t *testing.T) {
			frozen, _, _ := commandRouteCurrentPlan(t, plan.ProfileQuick)
			commandRouteChangeTest(&frozen, func(v *plan.Test) { v.Status = plan.TestStatus(name) })
			layout, err := commandLayoutForPlan(frozen, plan.ProfileQuick)
			if err != nil || !layout.touchpad || layout.schema != "hhsoft.pc-tester.quick-session/v9" {
				t.Fatal("finite non-scheduled definition lost its versioned layout")
			}
		})
	}
}

func TestTouchpadWorkerPublicationRequiresExactSuccessfulBoundary(t *testing.T) {
	for _, name := range []string{"pass", "inconclusive", "deadline", "aborted", "absent_error", "absent_aborted"} {
		t.Run(name, func(t *testing.T) {
			value := commandRouteValue(t)
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
				raw = commandRouteEncode(t, value)
			}
			out, gotState, err := touchpadWorkerOutcomeEvidence(raw, readErr, 0, interrupted)
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
		{"changed_limit", func(r *[]byte, _ *error, _ *int) { *r = bytes.Replace(*r, []byte("30000"), []byte("30001"), 1) }},
		{"duplicate_claim", func(r *[]byte, _ *error, _ *int) {
			*r = bytes.Replace(*r, []byte(`"schema":`), []byte(`"schema":"hhsoft.pc-tester.touchpad-functional/v1","schema":`), 1)
		}},
		{"schema_alias", func(r *[]byte, _ *error, _ *int) { *r = bytes.Replace(*r, []byte(`"schema":`), []byte(`"Schema":`), 1) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, readErr, code := []byte(commandRouteTouchpadLiteral), error(nil), 0
			c.change(&raw, &readErr, &code)
			if _, _, err := touchpadWorkerOutcomeEvidence(raw, readErr, code, false); err == nil {
				t.Fatal("uncertain publication accepted")
			}
		})
	}
	t.Run("cleanup_exit_overrides_malformed_read", func(t *testing.T) {
		if _, _, err := touchpadWorkerOutcomeEvidence([]byte("{"), errors.New("synthetic read"), workerCleanupUncertainExitCode, false); !errors.Is(err, touchpadevidence.ErrCleanupUnconfirmed) {
			t.Fatal("cleanup exit was masked")
		}
	})
	t.Run("typed_cleanup_overrides_nonzero", func(t *testing.T) {
		v := commandRouteValue(t)
		v.CleanupConfirmed = false
		v.Status = inputtest.StatusError
		v.StopReason, v.Detail = "cleanup_unconfirmed", "cleanup_unconfirmed"
		if _, _, err := touchpadWorkerOutcomeEvidence(commandRouteEncode(t, v), nil, 1, false); !errors.Is(err, touchpadevidence.ErrCleanupUnconfirmed) {
			t.Fatal("typed cleanup was masked")
		}
	})
}

func TestTouchpadWorkerRecordBindsDurationAndCleanup(t *testing.T) {
	valid := func() sessionWorkerRecord {
		return sessionWorkerRecord{ID: "input.touchpad.quick", State: "completed", Outcome: "pass", Timing: &sessionevidence.WorkerTiming{StartedMilliseconds: 10, FinishedMilliseconds: 110}}
	}
	t.Run("exact_interval", func(t *testing.T) {
		journal := sessionJournal{Schema: "hhsoft.pc-tester.quick-session/v9", Workers: []sessionWorkerRecord{valid()}}
		if err := validateSessionWorkerEvidence(&journal, "input.touchpad.quick", []byte(commandRouteTouchpadLiteral), nil, "pass", nil); err != nil {
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
			*r = bytes.Replace(*r, []byte("30000"), []byte("30001"), 1)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, raw, readErr, status, statusErr := valid(), []byte(commandRouteTouchpadLiteral), error(nil), "pass", error(nil)
			c.change(&w, &raw, &readErr, &status, &statusErr)
			if err := validateTouchpadWorkerRecord(w, raw, readErr, status, statusErr); err == nil {
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
			if err := validateTouchpadWorkerRecord(w, nil, os.ErrNotExist, name, nil); err != nil {
				t.Fatal(err)
			}
			if err := validateTouchpadWorkerRecord(w, []byte(commandRouteTouchpadLiteral), os.ErrNotExist, name, nil); err == nil {
				t.Fatal("absence carried bytes")
			}
		})
	}
	t.Run("cleanup_precedes_bad_record", func(t *testing.T) {
		if err := validateTouchpadWorkerRecord(sessionWorkerRecord{}, nil, os.ErrNotExist, "", errors.Join(errors.New("other"), touchpadevidence.ErrCleanupUnconfirmed)); !errors.Is(err, touchpadevidence.ErrCleanupUnconfirmed) {
			t.Fatal("cleanup uncertainty was replaced")
		}
	})
	t.Run("old_schema_cannot_smuggle_touchpad", func(t *testing.T) {
		journal := sessionJournal{Schema: "hhsoft.pc-tester.quick-session/v7", Workers: []sessionWorkerRecord{valid()}}
		if err := validateSessionWorkerEvidence(&journal, "input.touchpad.quick", []byte(commandRouteTouchpadLiteral), nil, "pass", nil); err == nil {
			t.Fatal("old schema admitted touchpad worker")
		}
	})
}

func TestTouchpadCommandHistoricalJournalVersionsStayReadable(t *testing.T) {
	cases := []struct {
		name, schema, ids string
		current           bool
	}{
		{"v4", "hhsoft.pc-tester.quick-session/v4", "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.pointer.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick", false},
		{"v5", "hhsoft.pc-tester.quick-session/v5", "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick", false},
		{"v6", "hhsoft.pc-tester.quick-session/v6", "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick network.readiness.quick storage.readiness.quick storage.quick usb.readiness.quick graphics.quick display.operator.quick", false},
		{"v7", "hhsoft.pc-tester.quick-session/v7", commandRouteQuickWorkers, false},
		{"v8", "hhsoft.pc-tester.quick-session/v8", commandRouteStandardWorkers, false},
		{"v9", "hhsoft.pc-tester.quick-session/v9", commandRouteTouchpadQuickWorkers, true},
		{"v10", "hhsoft.pc-tester.quick-session/v10", commandRouteTouchpadStandardWorkers, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			journal := commandRouteJournal(t, c.schema, c.ids)
			profile := plan.ProfileQuick
			if c.name == "v8" || c.name == "v10" {
				profile = plan.ProfileStandard
			}
			frozen, _, _ := commandRouteCurrentPlan(t, profile)
			if c.name != "v9" && c.name != "v10" {
				commandRouteChangeTest(&frozen, func(v *plan.Test) { v.Version = 1; v.Status = plan.TestUnsupported })
			}
			if currentSessionProfileForPlan(journal, frozen, profile) != c.current {
				t.Fatal("historical/current selection changed")
			}
			if currentSessionProfile(journal, profile) != c.current {
				t.Fatal("current producer accepted a historical schema or lost its current schema")
			}
			encoded, err := journal.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := sessionevidence.Decode(encoded)
			if err != nil || !reflect.DeepEqual(decoded, journal) {
				t.Fatal("historical journal roundtrip changed")
			}
		})
	}
}

func TestTouchpadCheckedJournalBindsFrozenPlanAndReadyMarkers(t *testing.T) {
	cases := []struct {
		name        string
		profile     plan.Profile
		schema, ids string
		future      bool
	}{
		{"quick_v7", plan.ProfileQuick, "hhsoft.pc-tester.quick-session/v7", commandRouteQuickWorkers, false},
		{"standard_v8", plan.ProfileStandard, "hhsoft.pc-tester.quick-session/v8", commandRouteStandardWorkers, false},
		{"quick_v9", plan.ProfileQuick, "hhsoft.pc-tester.quick-session/v9", commandRouteTouchpadQuickWorkers, true},
		{"standard_v10", plan.ProfileStandard, "hhsoft.pc-tester.quick-session/v10", commandRouteTouchpadStandardWorkers, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// This is an internal checked-boundary fixture, not a Plan.Decode or
			// catalog-activation test. No live monitoring authority is manufactured.
			frozen, _, _ := commandRouteCurrentPlan(t, c.profile)
			for i := range frozen.Tests {
				frozen.Tests[i].Status = plan.TestExcluded
			}
			if !c.future {
				commandRouteChangeTest(&frozen, func(v *plan.Test) { v.Version = 1 })
			}
			journal := commandRouteJournal(t, c.schema, c.ids)
			journal.PlanID = frozen.ID
			outcomes := map[string]string{}
			for _, w := range journal.Workers {
				outcomes[w.ID] = w.Outcome
			}
			directory := t.TempDir()
			if !c.future {
				if sessionevidence.ValidateStructure(journal) != nil {
					t.Fatal("historical journal prerequisite is invalid")
				}
				if err := validateSessionJournalForProfile(journal, frozen, outcomes, "completed", directory, c.profile); err == nil {
					t.Fatal("historical journal authorized current finalization")
				}
				return
			}
			if err := validateSessionJournalForProfile(journal, frozen, outcomes, "completed", directory, c.profile); err != nil {
				t.Fatal(err)
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
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), " touchpad=excluded") != c.future || !strings.Contains(string(raw), " exterior_inspection=excluded") || strings.Contains(string(raw), " profile=standard cpu_stress=excluded") != (c.profile == plan.ProfileStandard) {
				t.Fatal("versioned ready marker changed")
			}
			if strings.Count(string(raw), "HHPT_READY ") != 1 || strings.Count(string(raw), "\n") != 1 {
				t.Fatal("marker framing changed")
			}
			bad := sessionevidence.CloneJournal(journal)
			bad.PlanID = "sha256:" + strings.Repeat("2", 64)
			if err := validateSessionJournalForProfile(bad, frozen, outcomes, "completed", directory, c.profile); err == nil {
				t.Fatal("journal plan mismatch accepted")
			}
			if err := validateSessionJournalForProfile(journal, frozen, outcomes, "operator_cancelled", directory, c.profile); err == nil {
				t.Fatal("journal stop mismatch accepted")
			}
			if c.future {
				if err := os.WriteFile(filepath.Join(directory, "touchpad.json"), []byte(commandRouteTouchpadLiteral), 0600); err != nil {
					t.Fatal(err)
				}
				if err := validateSessionJournalForProfile(journal, frozen, outcomes, "completed", directory, c.profile); err == nil {
					t.Fatal("unstarted worker acquired evidence")
				}
			}
		})
	}
}

func TestTouchpadProfileAndJournalClaimsRejectAliases(t *testing.T) {
	for _, name := range []string{"old_schema", "wrong_profile", "quick_has_stress", "standard_lacks_profile", "worker_duplicate", "worker_missing", "outcome_changed", "planned_status_changed", "version_changed"} {
		t.Run(name, func(t *testing.T) {
			profile := plan.ProfileQuick
			schema, ids := "hhsoft.pc-tester.quick-session/v9", commandRouteTouchpadQuickWorkers
			if name == "standard_lacks_profile" {
				profile = plan.ProfileStandard
				schema, ids = "hhsoft.pc-tester.quick-session/v10", commandRouteTouchpadStandardWorkers
			}
			frozen, _, _ := commandRouteCurrentPlan(t, profile)
			for i := range frozen.Tests {
				frozen.Tests[i].Status = plan.TestExcluded
			}
			journal := commandRouteJournal(t, schema, ids)
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
				journal.Schema = "hhsoft.pc-tester.quick-session/v7"
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
				outcomes["input.touchpad.quick"] = "unsupported"
			case "planned_status_changed":
				commandRouteChangeTest(&frozen, func(v *plan.Test) { v.Status = plan.TestUnsupported })
			case "version_changed":
				commandRouteChangeTest(&frozen, func(v *plan.Test) { v.Version = 1 })
			}
			if err := validateSessionJournalForProfile(journal, frozen, outcomes, "completed", t.TempDir(), profile); err == nil {
				t.Fatal("mismatched journal accepted")
			}
		})
	}
}

func TestTouchpadStandardRouteRetainsCPUStressEvidence(t *testing.T) {
	for _, name := range []string{"standard_v8", "standard_v10", "wrong_schema", "wrong_profile", "duplicate_source", "read_error", "nil_timing", "negative_start", "short_interval", "file_digest", "typed_digest"} {
		t.Run(name, func(t *testing.T) {
			value := commandStressPass()
			raw := commandRouteEncode(t, value)
			schema := "hhsoft.pc-tester.quick-session/v10"
			if name == "standard_v8" {
				schema = "hhsoft.pc-tester.quick-session/v8"
			}
			worker := sessionWorkerRecord{ID: "cpu.stress", State: "completed", Outcome: string(value.Status), Timing: &sessionevidence.WorkerTiming{StartedMilliseconds: 0, FinishedMilliseconds: value.FinishedMilliseconds}}
			journal := sessionJournal{Schema: schema, Profile: "standard", Workers: []sessionWorkerRecord{worker}}
			readErr := error(nil)
			switch name {
			case "wrong_schema":
				journal.Schema = "hhsoft.pc-tester.quick-session/v9"
			case "wrong_profile":
				journal.Profile = "quick"
			case "duplicate_source":
				journal.CPUStressSource = &sessionevidence.StressSource{}
			case "read_error":
				readErr = errors.New("synthetic read failure")
			case "nil_timing":
				worker.Timing = nil
			case "negative_start":
				worker.Timing.StartedMilliseconds = -1
			case "short_interval":
				worker.Timing.FinishedMilliseconds--
			}
			err := retainLocalThermalSource(&journal, worker, raw, readErr)
			if name == "standard_v8" || name == "standard_v10" || name == "file_digest" || name == "typed_digest" {
				if err != nil || journal.CPUStressSource == nil {
					t.Fatalf("CPU source not retained: %v", err)
				}
				if name == "file_digest" {
					journal.CPUStressSource.EvidenceFileSHA256 = "sha256:" + strings.Repeat("0", 64)
				}
				if name == "typed_digest" {
					journal.CPUStressSource.EvidenceTypedSHA256 = "sha256:" + strings.Repeat("0", 64)
				}
				err = validateSessionWorkerEvidence(&journal, "cpu.stress", raw, nil, string(value.Status), nil)
				if (err == nil) != (name == "standard_v8" || name == "standard_v10") {
					t.Fatal("CPU source identity/time acceptance changed")
				}
			} else if err == nil {
				t.Fatal("invalid CPU source admitted by Standard route")
			}
		})
	}
}

type commandRouteNoConsoleRead struct{}

func (commandRouteNoConsoleRead) Read([]byte) (int, error) {
	panic("touchpad must not read operator console bytes")
}

func TestTouchpadFixedWorkerDispatchHonorsStopBeforeAcquisition(t *testing.T) {
	for _, name := range []string{"cancelled", "expired", "invalid_timeout", "unknown_worker"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			cancel := func() {}
			worker := sessionWorker{ID: "input.touchpad.quick", Architecture: "x86_64", OutputPath: "-", Timeout: 90 * time.Second}
			switch name {
			case "cancelled":
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "expired":
				ctx, cancel = context.WithDeadline(ctx, time.Unix(0, 0))
			case "invalid_timeout":
				worker.Timeout = 29 * time.Second
			case "unknown_worker":
				worker.ID = "input.touchpad.quick.alias"
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
			value, err := touchpadevidence.Decode(out.Bytes())
			want := "aborted"
			if name == "expired" {
				want = "error"
			}
			if code != 0 || err != nil || string(value.Status) != want || value.LimitMilliseconds != 30000 || len(value.Devices) != 0 || !value.CleanupConfirmed {
				t.Fatalf("pre-stopped fixed worker changed: code=%d err=%v", code, err)
			}
			if strings.Contains(diagnostic.String(), "Touchpad check:") {
				t.Fatal("pre-stopped worker issued acquisition instructions")
			}
		})
	}
}

func TestTouchpadAbsentCandidateRemainsExplicitInCurrentSession(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	if code := h.run(t.Context()); code != 0 || !h.finalized {
		t.Fatalf("current session failed: %d %s", code, h.stderr.String())
	}
	if h.journal.Schema != "hhsoft.pc-tester.quick-session/v9" || len(h.journal.Workers) != 13 ||
		!reflect.DeepEqual(h.calls, strings.Fields(commandRouteQuickWorkers)) {
		t.Fatal("absent candidate changed current execution order")
	}
	row := h.journal.Workers[6]
	if row.ID != "input.touchpad.quick" || row.State != "not_scheduled" || row.Outcome != "not_present" || row.Timing != nil {
		t.Fatal("absent candidate acquired execution credit")
	}
	ready, err := os.ReadFile(filepath.Join(h.directory, "session-ready.txt"))
	if err != nil || !bytes.Contains(ready, []byte(" touchpad=not_present")) ||
		!strings.Contains(h.stdout.String(), "HHPT_TOUCHPAD status=not_present") {
		t.Fatal("current markers lost the absent touchpad disposition")
	}
	if _, err := os.Stat(filepath.Join(h.directory, "touchpad.json")); !os.IsNotExist(err) {
		t.Fatal("absent touchpad created functional evidence")
	}
}
