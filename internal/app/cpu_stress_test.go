package app

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func standardStressPayload(t *testing.T) InventoryPayload {
	t.Helper()
	var p InventoryPayload
	if json.Unmarshal([]byte(stressReportLiteral), &p) != nil {
		t.Fatal("literal report")
	}
	if err := ValidateCoverage(p); err != nil {
		t.Fatal("valid standard report prerequisite", err)
	}
	return p
}

func stressIntegrityChange(t *testing.T, p *InventoryPayload, change func(*sessionevidence.IntegrityEvidence)) {
	t.Helper()
	row := &p.TestResults[len(p.TestResults)-1]
	v, err := sessionevidence.DecodeIntegrity(row.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	change(&v)
	row.Evidence, err = json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCPUStressReportReturnsIndependentObservation(t *testing.T) {
	p := standardStressPayload(t)
	before, _ := json.Marshal(p)
	v, err := ValidatedCPUStress(p)
	if err != nil || v == nil || v.Iterations != 256 || v.Workers[0].LoadChecksPassed != 12 || v.ChecksPassed != 24 {
		t.Fatal("literal observation", err)
	}
	*v.LoadStartedMilliseconds = 99
	*v.Workers[0].ReadyMilliseconds = 99
	v.Workers[0].ChecksPassed = 99
	again, err := ValidatedCPUStress(p)
	after, _ := json.Marshal(p)
	if err != nil || again == nil || *again.LoadStartedMilliseconds != 2 || *again.Workers[0].ReadyMilliseconds != 1 || again.Workers[0].ChecksPassed != 24 || !bytes.Equal(before, after) {
		t.Fatal("accessor aliases report")
	}
	thermal, err := ValidatedSessionThermal(p)
	if err != nil || thermal == nil || thermal.Scope != "Standard-session execution, cleanup, final inventory and final safety gate; report encoding, output and saving are outside this monitoring window." {
		t.Fatal("standard scope", err)
	}
}

func TestCPUStressReportRejectsForgedSourcesAndOpaqueObservations(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*testing.T, *InventoryPayload)
	}{
		{"wrong_row", func(t *testing.T, p *InventoryPayload) { p.TestResults[1].ID = "camera.quick" }},
		{"wrong_version", func(t *testing.T, p *InventoryPayload) { p.TestResults[1].Version = 2 }},
		{"row_status", func(t *testing.T, p *InventoryPayload) { p.TestResults[1].Status = TestFail }},
		{"opaque_pass", func(t *testing.T, p *InventoryPayload) { p.TestResults[1].Evidence = []byte(`{}`) }},
		{"unknown_family", func(t *testing.T, p *InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte("cpu-stress/v1"), []byte("cpu-stress/v9"), 1)
		}},
		{"wrong_typed_hash", func(t *testing.T, p *InventoryPayload) {
			stressIntegrityChange(t, p, func(v *sessionevidence.IntegrityEvidence) {
				v.Session.CPUStressSource.EvidenceTypedSHA256 = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			})
		}},
		{"invalid_file_hash", func(t *testing.T, p *InventoryPayload) {
			stressIntegrityChange(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.CPUStressSource.EvidenceFileSHA256 = "invalid" })
		}},
		{"null_source", func(t *testing.T, p *InventoryPayload) {
			stressIntegrityChange(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.CPUStressSource = nil })
		}},
		{"wrong_source_worker", func(t *testing.T, p *InventoryPayload) {
			stressIntegrityChange(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.CPUStressSource.WorkerID = "cpu.quick" })
		}},
		{"short_worker_interval", func(t *testing.T, p *InventoryPayload) {
			stressIntegrityChange(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.Workers[1].Timing.FinishedMilliseconds-- })
		}},
		{"missing_session", func(t *testing.T, p *InventoryPayload) {
			p.TestResults[len(p.TestResults)-1].Evidence = []byte(`{"status":"pass"}`)
		}},
		{"stripped_coverage", func(t *testing.T, p *InventoryPayload) {
			p.CoverageScope = ""
			p.TestResults = []TestResult{{ID: "cpu.stress", Version: 1, Status: TestPass, Evidence: []byte(`{}`)}}
		}},
		{"direct_capture", func(t *testing.T, p *InventoryPayload) {
			p.CoverageScope = CoverageDirectCapture
			p.TestResults = p.TestResults[:2]
			for i := range p.TestResults {
				p.TestResults[i].PlanID = ""
				p.TestResults[i].HardwareGraph = ""
				p.TestResults[i].PlannedStatus = ""
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := standardStressPayload(t)
			test.change(t, &p)
			if ValidateCoverage(p) == nil {
				t.Fatal("forged report coverage")
			}
			if _, err := ValidatedCPUStress(p); err == nil {
				t.Fatal("forged stress accessor")
			}
			if _, err := ValidatedSessionThermal(p); err == nil {
				t.Fatal("separate thermal path bypass")
			}
		})
	}
}

func TestCPUStressHistoricalNotesAndViewsRemainUnclaimed(t *testing.T) {
	for _, name := range []string{"historical-session-v3.json", "session-v4-unavailable.json", "session-v5-pointer-v2-pass.json", "cpu-v3-direct-pass.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			var p InventoryPayload
			if json.Unmarshal(raw, &p) != nil {
				t.Fatal("historical fixture")
			}
			before, _ := json.Marshal(p)
			if v, err := ValidatedCPUStress(p); err != nil || v != nil {
				t.Fatal("historical report reinterpreted", err)
			}
			after, _ := json.Marshal(p)
			if !bytes.Equal(before, after) {
				t.Fatal("historical mutation")
			}
		})
	}
	for _, test := range []struct {
		planned PlannedStatus
		status  TestStatus
		source  string
	}{{PlannedUnsupported, TestSkip, "unsupported"}, {PlannedExcluded, TestSkip, "excluded"}, {PlannedMissingDriver, TestInconclusive, "missing_driver"}} {
		p := coverageFixture()
		p.TestResults[0].ID = "cpu.stress"
		p.TestResults[0].Version = 1
		p.TestResults[0].PlannedStatus = test.planned
		p.TestResults[0].Status = test.status
		p.TestResults[0].Evidence, _ = json.Marshal(map[string]string{"schema": "hhsoft.pc-tester.execution-note/v1", "status": string(test.status), "source_status": test.source, "detail": "test was not scheduled"})
		if v, err := ValidatedCPUStress(p); err != nil || v != nil {
			t.Fatal("historical unobserved note", err)
		}
	}
}

func TestCPUStressHistoricalFailureNotesHaveExactAdmission(t *testing.T) {
	for _, status := range []TestStatus{TestError, TestAborted} {
		for _, fault := range []string{"frozen_note", "historical_note", "opaque", "wrong_detail", "source_mismatch", "extra_member", "duplicate_status", "wrong_version"} {
			t.Run(string(status)+"/"+fault, func(t *testing.T) {
				p := coverageFixture()
				row := &p.TestResults[0]
				row.ID, row.Version, row.PlannedStatus, row.Status = "cpu.stress", 1, PlannedScheduled, status
				row.Evidence = []byte(`{"schema":"hhsoft.pc-tester.execution-note/v1","status":"` + string(status) + `","source_status":"` + string(status) + `","detail":"bounded evidence was not produced; outcome recorded by the run supervisor"}`)
				if value, err := ValidatedCPUStress(p); err != nil || value != nil {
					t.Fatal("exact historical note prerequisite", err)
				}
				if fault == "frozen_note" {
					return
				}
				p.CoverageScope = ""
				p.TestResults = p.TestResults[:1]
				row = &p.TestResults[0]
				row.PlannedStatus, row.PlanID, row.HardwareGraph = "", "", ""
				if value, err := ValidatedCPUStress(p); err != nil || value != nil {
					t.Fatal("historical absent-coverage note prerequisite", err)
				}
				if fault == "historical_note" {
					return
				}
				switch fault {
				case "opaque":
					row.Evidence = []byte(`{}`)
				case "wrong_detail":
					row.Evidence = bytes.Replace(row.Evidence, []byte("bounded evidence was not produced"), []byte("all work passed"), 1)
				case "source_mismatch":
					row.Evidence = bytes.Replace(row.Evidence, []byte(`"source_status":"`+string(status)+`"`), []byte(`"source_status":"pass"`), 1)
				case "extra_member":
					row.Evidence = bytes.Replace(row.Evidence, []byte(`"schema":`), []byte(`"extra":false,"schema":`), 1)
				case "duplicate_status":
					row.Evidence = bytes.Replace(row.Evidence, []byte(`"status":`), []byte(`"status":"pass","status":`), 1)
				case "wrong_version":
					row.Version = 2
				}
				if ValidateCoverage(p) == nil {
					t.Fatal("opaque or forged failure note admitted")
				}
				if _, err := ValidatedCPUStress(p); err == nil {
					t.Fatal("failure note bypassed stress accessor")
				}
			})
		}
	}
}

const stressReportLiteral = `{"schema_version":1,"components":[{"type":"block","id":"block:sda","attributes":[{"name":"model","value":"Fixture disk"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"stress-fixture","source":"wwid","value":"hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af","confidence":"durable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"stress-fixture","key_id":"hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44","value":"hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449","identified_parts":1,"unidentified_parts":0},"test_results":[{"id":"cpu.quick","version":2,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.cpu/v3","status":"pass","workers":1,"iterations":1,"state_aggregate":17,"known_answer_sha256":"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad","duration_ms":3,"scalar":{"suite":"binary64-normal-exact-v1","vector_count":12,"checks_passed":12,"workers_completed":1,"workers":[{"index":0,"iterations":1,"checks_passed":12}]},"calculation_failures":{"integer":false,"sha256":false,"scalar":false},"safety":{"global_timeout_ms":1000,"thermal_limit_millicelsius":90000,"thermal_sensors":0,"thermal_samples":0,"monitor_status":"completed","cleanup_confirmed":true,"thermal_observations":1,"thermal_coverage":"unavailable","thermal_rejected_readings":0,"thermal_unavailable_readings":0,"thermal_enumeration_errors":0}},"planned_status":"scheduled"},{"id":"cpu.stress","version":1,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.cpu-stress/v1","status":"pass","stop_reason":"completed","requested_load_ms":600000,"local_watchdog_ms":660000,"thermal_policy":"available-observations/v1","worker_selection":"explicit","workers_requested":1,"workers_launched":1,"workers_ready":1,"effective_deadline_ms":660000,"effective_deadline_source":"watchdog","load_started":true,"load_started_ms":2,"load_stopped_ms":600002,"observed_load_ms":600000,"workers_joined_ms":600003,"finished_ms":600004,"iterations":256,"checks_passed":24,"state_aggregate":17,"known_answer_sha256":"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad","scalar_suite":"binary64-normal-exact-v1","scalar_vector_count":12,"calculation_failures":{"integer":false,"sha256":false,"scalar":false},"accounting_valid":true,"stop_facts":{"cleanup_unconfirmed":false,"clock_invalid":false,"counter_overflow":false,"coverage_lost":false,"early_worker_exit":false,"incomplete_load_checks":false,"initial_coverage_partial":false,"initial_coverage_unavailable":false,"insufficient_budget":false,"invalid_observation":false,"launch_incomplete":false,"normal_load_complete":true,"observation_timeout":false,"observer_panic":false,"parent_cancelled":false,"parent_deadline":false,"readiness_incomplete":false,"thermal_limit":false,"watchdog_deadline":false,"worker_panic":false},"safety":{"global_timeout_ms":660000,"thermal_limit_millicelsius":90000,"thermal_sensors":1,"thermal_samples":1,"peak_millicelsius":40000,"monitor_status":"completed","cleanup_confirmed":true,"thermal_observations":1,"thermal_coverage":"available","thermal_rejected_readings":0,"thermal_unavailable_readings":0,"thermal_enumeration_errors":0},"workers":[{"index":0,"started_ms":0,"ready_ms":1,"load_entered_ms":2,"stopped_ms":600002,"exit_reason":"completed","preparation_iterations":1,"preparation_checks_passed":12,"iterations":256,"checks_passed":24,"load_iterations":255,"load_checks_passed":12,"calculation_failures":{"integer":false,"sha256":false,"scalar":false},"failure_phase":"none","mismatch":null}]},"planned_status":"scheduled"},{"id":"memory.quick","version":2,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"audio.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"input.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"input.keyboard.quick","version":2,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"input.pointer.quick","version":2,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"network.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"storage.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"usb.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"graphics.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"display.operator.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"inspection.exterior.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"run.integrity.quick","version":1,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.run-integrity/v1","status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","planned_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","final_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","planned_component_count":1,"final_component_count":1,"excluded_tests":["memory.quick","audio.readiness.quick","input.readiness.quick","input.keyboard.quick","input.pointer.quick","network.readiness.quick","storage.readiness.quick","usb.readiness.quick","graphics.quick","display.operator.quick","inspection.exterior.quick"],"session":{"schema":"hhsoft.pc-tester.quick-session/v8","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","stop_reason":"completed","workers":[{"id":"cpu.quick","state":"completed","outcome":"pass","timing":{"started_ms":2,"finished_ms":5}},{"id":"cpu.stress","state":"completed","outcome":"pass","timing":{"started_ms":6,"finished_ms":600010}},{"id":"memory.quick","state":"not_scheduled","outcome":"excluded"},{"id":"audio.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"input.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"input.keyboard.quick","state":"not_scheduled","outcome":"excluded"},{"id":"input.pointer.quick","state":"not_scheduled","outcome":"excluded"},{"id":"network.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"storage.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"usb.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"graphics.quick","state":"not_scheduled","outcome":"excluded"},{"id":"display.operator.quick","state":"not_scheduled","outcome":"excluded"},{"id":"inspection.exterior.quick","state":"not_scheduled","outcome":"excluded"}],"safety":[{"phase":"preflight","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"before-cpu.quick","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"before-cpu.stress","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"final","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"pre-sign","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}}],"monitor":{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"pass","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":600,"completed_observations":600,"elapsed_ms":600046,"first_completed_ms":1000,"last_completed_ms":600000,"maximum_gap_ms":1000,"cleanup_confirmed":true,"last_observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},"preparation_started_ms":600020,"inventory_prepared_ms":600030,"stop_requested_ms":600040,"safety_joined_ms":600045,"local_thermal_sources":[],"thermal":{"schema":"hhsoft.pc-tester.run-thermal/v1","status":"completed","limit_millicelsius":90000,"interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"stop_timeout_ms":2000,"attempted_observations":602,"completed_observations":602,"periodic_attempts":600,"periodic_completions":600,"elapsed_ms":600048,"first_completed_ms":1,"last_completed_ms":600032,"maximum_gap_ms":1000,"initial":{"requested_ms":0,"attempted":true,"completed":true,"started_ms":0,"completed_ms":1,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"final":{"requested_ms":600031,"attempted":true,"completed":true,"started_ms":600031,"completed_ms":600032,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"aggregate":{"coverage":"unavailable","accepted_observations":602,"available_observations":0,"partial_observations":0,"unavailable_observations":602,"maximum_sensors":0,"valid_readings":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0},"cleanup_confirmed":true,"joined":true,"joined_ms":600047,"last_observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"budget_milliseconds":900000,"elapsed_milliseconds":600050,"profile":"standard","cpu_stress_source":{"worker_id":"cpu.stress","evidence_schema":"hhsoft.pc-tester.cpu-stress/v1","evidence_file_sha256":"sha256:d86dd4c7a897a882a2b6beafed7504499664f8496bb279f104602c201ccd36de","evidence_typed_sha256":"sha256:db80ee5817ade0a530be8ac426698de09d14533404faaaaaaf2df4619cfdf3f5"}}}}],"coverage_scope":"frozen_plan"}`
