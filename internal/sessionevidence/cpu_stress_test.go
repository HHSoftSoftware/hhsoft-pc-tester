package sessionevidence_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func standardStressJournal(t *testing.T) (sessionevidence.Journal, []sessionevidence.ResultBinding) {
	t.Helper()
	var p app.InventoryPayload
	if json.Unmarshal([]byte(stressReportLiteral), &p) != nil {
		t.Fatal("literal report")
	}
	var j sessionevidence.Journal
	bindings := []sessionevidence.ResultBinding{}
	for _, r := range p.TestResults {
		if r.ID == "run.integrity.quick" {
			v, err := sessionevidence.DecodeIntegrity(r.Evidence)
			if err != nil {
				t.Fatal("integrity prerequisite", err)
			}
			j = *v.Session
			continue
		}
		bindings = append(bindings, sessionevidence.ResultBinding{ID: r.ID, Version: r.Version, PlannedStatus: string(r.PlannedStatus), ReportStatus: string(r.Status), Evidence: r.Evidence})
	}
	if err := sessionevidence.ValidateBindings(j, bindings); err != nil {
		t.Fatal("binding prerequisite", err)
	}
	return j, bindings
}

func TestStandardJournalSourceCloneAndExactWire(t *testing.T) {
	j, _ := standardStressJournal(t)
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"profile":"standard","cpu_stress_source":`)) {
		t.Fatal("v8 field order")
	}
	bounded := append(append([]byte{}, raw...), bytes.Repeat([]byte(" "), (64<<10)-len(raw))...)
	if _, err := sessionevidence.Decode(bounded); err != nil {
		t.Fatal("exact journal cap", err)
	}
	if _, err := sessionevidence.Decode(append(bounded, byte(32))); err == nil {
		t.Fatal("journal cap plus one")
	}
	clone := sessionevidence.CloneJournal(j)
	clone.CPUStressSource.EvidenceTypedSHA256 = "changed"
	clone.Workers[1].Timing.FinishedMilliseconds++
	if j.CPUStressSource.EvidenceTypedSHA256 == "changed" || clone.Workers[1].Timing.FinishedMilliseconds == j.Workers[1].Timing.FinishedMilliseconds {
		t.Fatal("clone aliases source or timing")
	}
	for _, test := range []struct {
		name     string
		old, new string
	}{
		{"wrong_profile", `"profile":"standard"`, `"profile":"quick"`},
		{"missing_source", `,"cpu_stress_source":{`, `,"renamed_source":{`},
		{"duplicate_profile", `"profile":"standard"`, `"profile":"quick","profile":"standard"`},
		{"legacy_schema_with_new_fields", `quick-session/v8`, `quick-session/v7`},
		{"storage_member", `"profile":"standard"`, `"storage_source":null,"profile":"standard"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := bytes.Replace(raw, []byte(test.old), []byte(test.new), 1)
			if bytes.Equal(raw, changed) {
				t.Fatal("ineffective wire fault")
			}
			if _, err := sessionevidence.Decode(changed); err == nil {
				t.Fatal("invalid v8 wire admitted")
			}
		})
	}
}

func TestStandardStressRequiresPassingQuickAndDispatchedSource(t *testing.T) {
	for _, disposition := range []string{"excluded", "unsupported"} {
		t.Run(disposition, func(t *testing.T) {
			j, bindings := standardStressJournal(t)
			j.Workers[0] = sessionevidence.WorkerRecord{ID: "cpu.quick", State: "not_scheduled", Outcome: disposition}
			j.Safety = append(j.Safety[:1], j.Safety[2:]...)
			bindings[0].PlannedStatus = disposition
			bindings[0].ReportStatus = "skip"
			bindings[0].Evidence, _ = json.Marshal(map[string]string{"schema": "hhsoft.pc-tester.execution-note/v1", "status": "skip", "source_status": disposition, "detail": "not run; disposition recorded in the frozen plan"})
			if sessionevidence.ValidateBindings(j, bindings) == nil {
				t.Fatal("stress ran without passing quick predecessor")
			}
		})
	}
	j, bindings := standardStressJournal(t)
	j.StopReason = "worker_stop"
	j.CPUStressSource = nil
	j.Workers[1] = sessionevidence.WorkerRecord{ID: "cpu.stress", State: "not_started", Outcome: "aborted"}
	j.Safety = append(j.Safety[:2], j.Safety[3:]...)
	bindings[1].ReportStatus = "aborted"
	bindings[1].Evidence = []byte(`{"schema":"hhsoft.pc-tester.execution-note/v1","status":"aborted","source_status":"aborted","detail":"bounded evidence was not produced; outcome recorded by the run supervisor"}`)
	if err := sessionevidence.ValidateBindings(j, bindings); err != nil {
		t.Fatal("valid undispatched source-null prerequisite", err)
	}
	j.Workers[1].State = "interrupted"
	j.Workers[1].Timing = &sessionevidence.WorkerTiming{StartedMilliseconds: 6, FinishedMilliseconds: 7}
	j.Safety = append(j.Safety[:2], append([]sessionevidence.SafetyRecord{{Phase: "before-cpu.stress", Result: j.Safety[0].Result}}, j.Safety[2:]...)...)
	if sessionevidence.ValidateBindings(j, bindings) == nil {
		t.Fatal("dispatched error note hid required evidence")
	}
}

const stressReportLiteral = `{"schema_version":1,"components":[{"type":"block","id":"block:sda","attributes":[{"name":"model","value":"Fixture disk"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"stress-fixture","source":"wwid","value":"hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af","confidence":"durable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"stress-fixture","key_id":"hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44","value":"hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449","identified_parts":1,"unidentified_parts":0},"test_results":[{"id":"cpu.quick","version":2,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.cpu/v3","status":"pass","workers":1,"iterations":1,"state_aggregate":17,"known_answer_sha256":"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad","duration_ms":3,"scalar":{"suite":"binary64-normal-exact-v1","vector_count":12,"checks_passed":12,"workers_completed":1,"workers":[{"index":0,"iterations":1,"checks_passed":12}]},"calculation_failures":{"integer":false,"sha256":false,"scalar":false},"safety":{"global_timeout_ms":1000,"thermal_limit_millicelsius":90000,"thermal_sensors":0,"thermal_samples":0,"monitor_status":"completed","cleanup_confirmed":true,"thermal_observations":1,"thermal_coverage":"unavailable","thermal_rejected_readings":0,"thermal_unavailable_readings":0,"thermal_enumeration_errors":0}},"planned_status":"scheduled"},{"id":"cpu.stress","version":1,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.cpu-stress/v1","status":"pass","stop_reason":"completed","requested_load_ms":600000,"local_watchdog_ms":660000,"thermal_policy":"available-observations/v1","worker_selection":"explicit","workers_requested":1,"workers_launched":1,"workers_ready":1,"effective_deadline_ms":660000,"effective_deadline_source":"watchdog","load_started":true,"load_started_ms":2,"load_stopped_ms":600002,"observed_load_ms":600000,"workers_joined_ms":600003,"finished_ms":600004,"iterations":256,"checks_passed":24,"state_aggregate":17,"known_answer_sha256":"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad","scalar_suite":"binary64-normal-exact-v1","scalar_vector_count":12,"calculation_failures":{"integer":false,"sha256":false,"scalar":false},"accounting_valid":true,"stop_facts":{"cleanup_unconfirmed":false,"clock_invalid":false,"counter_overflow":false,"coverage_lost":false,"early_worker_exit":false,"incomplete_load_checks":false,"initial_coverage_partial":false,"initial_coverage_unavailable":false,"insufficient_budget":false,"invalid_observation":false,"launch_incomplete":false,"normal_load_complete":true,"observation_timeout":false,"observer_panic":false,"parent_cancelled":false,"parent_deadline":false,"readiness_incomplete":false,"thermal_limit":false,"watchdog_deadline":false,"worker_panic":false},"safety":{"global_timeout_ms":660000,"thermal_limit_millicelsius":90000,"thermal_sensors":1,"thermal_samples":1,"peak_millicelsius":40000,"monitor_status":"completed","cleanup_confirmed":true,"thermal_observations":1,"thermal_coverage":"available","thermal_rejected_readings":0,"thermal_unavailable_readings":0,"thermal_enumeration_errors":0},"workers":[{"index":0,"started_ms":0,"ready_ms":1,"load_entered_ms":2,"stopped_ms":600002,"exit_reason":"completed","preparation_iterations":1,"preparation_checks_passed":12,"iterations":256,"checks_passed":24,"load_iterations":255,"load_checks_passed":12,"calculation_failures":{"integer":false,"sha256":false,"scalar":false},"failure_phase":"none","mismatch":null}]},"planned_status":"scheduled"},{"id":"memory.quick","version":2,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"audio.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"input.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"input.keyboard.quick","version":2,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"input.pointer.quick","version":2,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"network.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"storage.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"usb.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"graphics.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"display.operator.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"inspection.exterior.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"run.integrity.quick","version":1,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.run-integrity/v1","status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","planned_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","final_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","planned_component_count":1,"final_component_count":1,"excluded_tests":["memory.quick","audio.readiness.quick","input.readiness.quick","input.keyboard.quick","input.pointer.quick","network.readiness.quick","storage.readiness.quick","usb.readiness.quick","graphics.quick","display.operator.quick","inspection.exterior.quick"],"session":{"schema":"hhsoft.pc-tester.quick-session/v8","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","stop_reason":"completed","workers":[{"id":"cpu.quick","state":"completed","outcome":"pass","timing":{"started_ms":2,"finished_ms":5}},{"id":"cpu.stress","state":"completed","outcome":"pass","timing":{"started_ms":6,"finished_ms":600010}},{"id":"memory.quick","state":"not_scheduled","outcome":"excluded"},{"id":"audio.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"input.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"input.keyboard.quick","state":"not_scheduled","outcome":"excluded"},{"id":"input.pointer.quick","state":"not_scheduled","outcome":"excluded"},{"id":"network.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"storage.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"usb.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"graphics.quick","state":"not_scheduled","outcome":"excluded"},{"id":"display.operator.quick","state":"not_scheduled","outcome":"excluded"},{"id":"inspection.exterior.quick","state":"not_scheduled","outcome":"excluded"}],"safety":[{"phase":"preflight","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"before-cpu.quick","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"before-cpu.stress","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"final","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"pre-sign","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}}],"monitor":{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"pass","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":600,"completed_observations":600,"elapsed_ms":600046,"first_completed_ms":1000,"last_completed_ms":600000,"maximum_gap_ms":1000,"cleanup_confirmed":true,"last_observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},"preparation_started_ms":600020,"inventory_prepared_ms":600030,"stop_requested_ms":600040,"safety_joined_ms":600045,"local_thermal_sources":[],"thermal":{"schema":"hhsoft.pc-tester.run-thermal/v1","status":"completed","limit_millicelsius":90000,"interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"stop_timeout_ms":2000,"attempted_observations":602,"completed_observations":602,"periodic_attempts":600,"periodic_completions":600,"elapsed_ms":600048,"first_completed_ms":1,"last_completed_ms":600032,"maximum_gap_ms":1000,"initial":{"requested_ms":0,"attempted":true,"completed":true,"started_ms":0,"completed_ms":1,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"final":{"requested_ms":600031,"attempted":true,"completed":true,"started_ms":600031,"completed_ms":600032,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"aggregate":{"coverage":"unavailable","accepted_observations":602,"available_observations":0,"partial_observations":0,"unavailable_observations":602,"maximum_sensors":0,"valid_readings":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0},"cleanup_confirmed":true,"joined":true,"joined_ms":600047,"last_observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"budget_milliseconds":900000,"elapsed_milliseconds":600050,"profile":"standard","cpu_stress_source":{"worker_id":"cpu.stress","evidence_schema":"hhsoft.pc-tester.cpu-stress/v1","evidence_file_sha256":"sha256:d86dd4c7a897a882a2b6beafed7504499664f8496bb279f104602c201ccd36de","evidence_typed_sha256":"sha256:db80ee5817ade0a530be8ac426698de09d14533404faaaaaaf2df4619cfdf3f5"}}}}],"coverage_scope":"frozen_plan"}`
