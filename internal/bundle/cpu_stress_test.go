package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func stressBundlePayload(t *testing.T) app.InventoryPayload {
	t.Helper()
	var p app.InventoryPayload
	if json.Unmarshal([]byte(stressReportLiteral), &p) != nil {
		t.Fatal("literal report")
	}
	if err := app.ValidateCoverage(p); err != nil {
		t.Fatal("valid signed prerequisite", err)
	}
	return p
}

func TestCPUStressIndependentSignedManifestProfilesAndViews(t *testing.T) {
	p := stressBundlePayload(t)
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	parentRaw, err := json.Marshal(viewPayload(t))
	if err != nil {
		t.Fatal(err)
	}
	parent, _, parentKeys := independentlySignedRetest(t, parentRaw, nil, nil, nil, false, nil, func(e *report.Envelope) { e.SessionID = "11111111-1111-4111-8111-111111111111" })
	verifiedParent, err := Verify(bytes.NewReader(parent), int64(len(parent)), parentKeys)
	if err != nil {
		t.Fatal("independent parent prerequisite", err)
	}
	reference := report.PreviousReport{ManifestDigest: verifiedParent.Envelope.ManifestDigest, BundleSHA256: fmt.Sprintf("sha256:%x", verifiedParent.BundleSHA256)}
	for _, linked := range []bool{false, true} {
		for _, self := range []bool{false, true} {
			for _, views := range []string{"none", "html", "pdf", "both"} {
				t.Run(fmt.Sprintf("linked=%t/self=%t/%s", linked, self, views), func(t *testing.T) {
					var previous *report.PreviousReport
					if linked {
						previous = &reference
					}
					var html, pdf []byte
					if views == "html" || views == "both" {
						if previous == nil {
							html, err = render.InventoryHTMLForVersion(p, "0.0.1-fixture")
						} else {
							html, err = render.InventoryRetestHTMLForVersion(p, "0.0.1-fixture", *previous)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					if views == "pdf" || views == "both" {
						if previous == nil {
							pdf, err = render.InventoryPDFForVersion(p, "0.0.1-fixture")
						} else {
							pdf, err = render.InventoryRetestPDFForVersion(p, "0.0.1-fixture", *previous)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					archive, envelope, keys := independentlySignedRetest(t, raw, html, pdf, previous, self, nil, nil)
					artifact, err := verifyViewFixture(archive, keys, self)
					wantSchema := "hhsoft.pc-tester.bundle-manifest/v1"
					if linked {
						wantSchema = "hhsoft.pc-tester.bundle-manifest/v2"
					}
					if err != nil || artifact.Manifest.Schema != wantSchema || envelope.Schema != "hhsoft.pc-tester.report-envelope/v1" || !bytes.Equal(artifact.InventoryJSON, raw) || !bytes.Equal(artifact.RenderedReport, html) || !bytes.Equal(artifact.RenderedPDF, pdf) {
						t.Fatal("signed profile/view mismatch", err)
					}
				})
			}
		}
	}
}

func TestCPUStressResignedNoViewForgeriesCannotUseLegacyFastPath(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*app.InventoryPayload)
	}{
		{"opaque_pass_without_all_claim_metadata", func(p *app.InventoryPayload) {
			p.CoverageScope = ""
			p.TestResults = []app.TestResult{{ID: "cpu.stress", Version: 1, Status: app.TestPass, Evidence: []byte(`{}`)}}
		}},
		{"opaque_failed_without_all_claim_metadata", func(p *app.InventoryPayload) {
			p.CoverageScope = ""
			p.TestResults = []app.TestResult{{ID: "cpu.stress", Version: 1, Status: app.TestFail, Evidence: []byte(`{}`)}}
		}},
		{"opaque_error_without_all_claim_metadata", func(p *app.InventoryPayload) {
			p.CoverageScope = ""
			p.TestResults = []app.TestResult{{ID: "cpu.stress", Version: 1, Status: app.TestError, Evidence: []byte(`{}`)}}
		}},
		{"opaque_aborted_without_all_claim_metadata", func(p *app.InventoryPayload) {
			p.CoverageScope = ""
			p.TestResults = []app.TestResult{{ID: "cpu.stress", Version: 1, Status: app.TestAborted, Evidence: []byte(`{}`)}}
		}},
		{"unknown_schema", func(p *app.InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte("cpu-stress/v1"), []byte("cpu-stress/v9"), 1)
		}},
		{"duplicate_status", func(p *app.InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte(`"status":"pass"`), []byte(`"status":"fail","status":"pass"`), 1)
		}},
		{"missing_source", func(p *app.InventoryPayload) {
			last := len(p.TestResults) - 1
			p.TestResults[last].Evidence = bytes.Replace(p.TestResults[last].Evidence, []byte(`"cpu_stress_source":`), []byte(`"omitted_stress_source":`), 1)
		}},
		{"cleanup", func(p *app.InventoryPayload) {
			p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte(`"cleanup_confirmed":true`), []byte(`"cleanup_confirmed":false`), 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := stressBundlePayload(t)
			raw, _ := json.Marshal(p)
			good, _, keys := independentlySignedRetest(t, raw, nil, nil, nil, false, nil, nil)
			if _, err := Verify(bytes.NewReader(good), int64(len(good)), keys); err != nil {
				t.Fatal("signed positive prerequisite", err)
			}
			test.change(&p)
			badRaw, _ := json.Marshal(p)
			if bytes.Equal(raw, badRaw) {
				t.Fatal("ineffective signed fault")
			}
			bad, envelope, badKeys := independentlySignedRetest(t, badRaw, nil, nil, nil, false, nil, nil)
			if err := report.Verify(envelope, badKeys); err != nil {
				t.Fatal("signature must remain valid", err)
			}
			if _, err := Verify(bytes.NewReader(bad), int64(len(bad)), badKeys); err == nil {
				t.Fatal("re-signed semantic forgery admitted")
			}
		})
	}
}

const stressReportLiteral = `{"schema_version":1,"components":[{"type":"block","id":"block:sda","attributes":[{"name":"model","value":"Fixture disk"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"stress-fixture","source":"wwid","value":"hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af","confidence":"durable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"stress-fixture","key_id":"hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44","value":"hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449","identified_parts":1,"unidentified_parts":0},"test_results":[{"id":"cpu.quick","version":2,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.cpu/v3","status":"pass","workers":1,"iterations":1,"state_aggregate":17,"known_answer_sha256":"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad","duration_ms":3,"scalar":{"suite":"binary64-normal-exact-v1","vector_count":12,"checks_passed":12,"workers_completed":1,"workers":[{"index":0,"iterations":1,"checks_passed":12}]},"calculation_failures":{"integer":false,"sha256":false,"scalar":false},"safety":{"global_timeout_ms":1000,"thermal_limit_millicelsius":90000,"thermal_sensors":0,"thermal_samples":0,"monitor_status":"completed","cleanup_confirmed":true,"thermal_observations":1,"thermal_coverage":"unavailable","thermal_rejected_readings":0,"thermal_unavailable_readings":0,"thermal_enumeration_errors":0}},"planned_status":"scheduled"},{"id":"cpu.stress","version":1,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.cpu-stress/v1","status":"pass","stop_reason":"completed","requested_load_ms":600000,"local_watchdog_ms":660000,"thermal_policy":"available-observations/v1","worker_selection":"explicit","workers_requested":1,"workers_launched":1,"workers_ready":1,"effective_deadline_ms":660000,"effective_deadline_source":"watchdog","load_started":true,"load_started_ms":2,"load_stopped_ms":600002,"observed_load_ms":600000,"workers_joined_ms":600003,"finished_ms":600004,"iterations":256,"checks_passed":24,"state_aggregate":17,"known_answer_sha256":"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad","scalar_suite":"binary64-normal-exact-v1","scalar_vector_count":12,"calculation_failures":{"integer":false,"sha256":false,"scalar":false},"accounting_valid":true,"stop_facts":{"cleanup_unconfirmed":false,"clock_invalid":false,"counter_overflow":false,"coverage_lost":false,"early_worker_exit":false,"incomplete_load_checks":false,"initial_coverage_partial":false,"initial_coverage_unavailable":false,"insufficient_budget":false,"invalid_observation":false,"launch_incomplete":false,"normal_load_complete":true,"observation_timeout":false,"observer_panic":false,"parent_cancelled":false,"parent_deadline":false,"readiness_incomplete":false,"thermal_limit":false,"watchdog_deadline":false,"worker_panic":false},"safety":{"global_timeout_ms":660000,"thermal_limit_millicelsius":90000,"thermal_sensors":1,"thermal_samples":1,"peak_millicelsius":40000,"monitor_status":"completed","cleanup_confirmed":true,"thermal_observations":1,"thermal_coverage":"available","thermal_rejected_readings":0,"thermal_unavailable_readings":0,"thermal_enumeration_errors":0},"workers":[{"index":0,"started_ms":0,"ready_ms":1,"load_entered_ms":2,"stopped_ms":600002,"exit_reason":"completed","preparation_iterations":1,"preparation_checks_passed":12,"iterations":256,"checks_passed":24,"load_iterations":255,"load_checks_passed":12,"calculation_failures":{"integer":false,"sha256":false,"scalar":false},"failure_phase":"none","mismatch":null}]},"planned_status":"scheduled"},{"id":"memory.quick","version":2,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"audio.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"input.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"input.keyboard.quick","version":2,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"input.pointer.quick","version":2,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"network.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"storage.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"usb.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"graphics.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"display.operator.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"inspection.exterior.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"run.integrity.quick","version":1,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.run-integrity/v1","status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","planned_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","final_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","planned_component_count":1,"final_component_count":1,"excluded_tests":["memory.quick","audio.readiness.quick","input.readiness.quick","input.keyboard.quick","input.pointer.quick","network.readiness.quick","storage.readiness.quick","usb.readiness.quick","graphics.quick","display.operator.quick","inspection.exterior.quick"],"session":{"schema":"hhsoft.pc-tester.quick-session/v8","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","stop_reason":"completed","workers":[{"id":"cpu.quick","state":"completed","outcome":"pass","timing":{"started_ms":2,"finished_ms":5}},{"id":"cpu.stress","state":"completed","outcome":"pass","timing":{"started_ms":6,"finished_ms":600010}},{"id":"memory.quick","state":"not_scheduled","outcome":"excluded"},{"id":"audio.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"input.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"input.keyboard.quick","state":"not_scheduled","outcome":"excluded"},{"id":"input.pointer.quick","state":"not_scheduled","outcome":"excluded"},{"id":"network.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"storage.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"usb.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"graphics.quick","state":"not_scheduled","outcome":"excluded"},{"id":"display.operator.quick","state":"not_scheduled","outcome":"excluded"},{"id":"inspection.exterior.quick","state":"not_scheduled","outcome":"excluded"}],"safety":[{"phase":"preflight","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"before-cpu.quick","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"before-cpu.stress","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"final","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"pre-sign","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}}],"monitor":{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"pass","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":600,"completed_observations":600,"elapsed_ms":600046,"first_completed_ms":1000,"last_completed_ms":600000,"maximum_gap_ms":1000,"cleanup_confirmed":true,"last_observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},"preparation_started_ms":600020,"inventory_prepared_ms":600030,"stop_requested_ms":600040,"safety_joined_ms":600045,"local_thermal_sources":[],"thermal":{"schema":"hhsoft.pc-tester.run-thermal/v1","status":"completed","limit_millicelsius":90000,"interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"stop_timeout_ms":2000,"attempted_observations":602,"completed_observations":602,"periodic_attempts":600,"periodic_completions":600,"elapsed_ms":600048,"first_completed_ms":1,"last_completed_ms":600032,"maximum_gap_ms":1000,"initial":{"requested_ms":0,"attempted":true,"completed":true,"started_ms":0,"completed_ms":1,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"final":{"requested_ms":600031,"attempted":true,"completed":true,"started_ms":600031,"completed_ms":600032,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"aggregate":{"coverage":"unavailable","accepted_observations":602,"available_observations":0,"partial_observations":0,"unavailable_observations":602,"maximum_sensors":0,"valid_readings":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0},"cleanup_confirmed":true,"joined":true,"joined_ms":600047,"last_observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"budget_milliseconds":900000,"elapsed_milliseconds":600050,"profile":"standard","cpu_stress_source":{"worker_id":"cpu.stress","evidence_schema":"hhsoft.pc-tester.cpu-stress/v1","evidence_file_sha256":"sha256:d86dd4c7a897a882a2b6beafed7504499664f8496bb279f104602c201ccd36de","evidence_typed_sha256":"sha256:db80ee5817ade0a530be8ac426698de09d14533404faaaaaaf2df4619cfdf3f5"}}}}],"coverage_scope":"frozen_plan"}`

const stressAdverseReportLiteral = `{"schema_version":1,"components":[{"type":"block","id":"block:sda","attributes":[{"name":"model","value":"Fixture disk"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"stress-fixture","source":"wwid","value":"hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af","confidence":"durable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"stress-fixture","key_id":"hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44","value":"hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449","identified_parts":1,"unidentified_parts":0},"test_results":[{"id":"cpu.quick","version":2,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.cpu/v3","status":"pass","workers":1,"iterations":1,"state_aggregate":17,"known_answer_sha256":"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad","duration_ms":3,"scalar":{"suite":"binary64-normal-exact-v1","vector_count":12,"checks_passed":12,"workers_completed":1,"workers":[{"index":0,"iterations":1,"checks_passed":12}]},"calculation_failures":{"integer":false,"sha256":false,"scalar":false},"safety":{"global_timeout_ms":1000,"thermal_limit_millicelsius":90000,"thermal_sensors":0,"thermal_samples":0,"monitor_status":"completed","cleanup_confirmed":true,"thermal_observations":1,"thermal_coverage":"unavailable","thermal_rejected_readings":0,"thermal_unavailable_readings":0,"thermal_enumeration_errors":0}},"planned_status":"scheduled"},{"id":"cpu.stress","version":1,"status":"fail","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.cpu-stress/v1","status":"fail","stop_reason":"calculation_mismatch","requested_load_ms":600000,"local_watchdog_ms":660000,"thermal_policy":"available-observations/v1","worker_selection":"explicit","workers_requested":1,"workers_launched":1,"workers_ready":1,"effective_deadline_ms":660000,"effective_deadline_source":"watchdog","load_started":true,"load_started_ms":2,"load_stopped_ms":1000,"observed_load_ms":998,"workers_joined_ms":1001,"finished_ms":1002,"iterations":256,"checks_passed":23,"state_aggregate":17,"known_answer_sha256":"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad","scalar_suite":"binary64-normal-exact-v1","scalar_vector_count":12,"calculation_failures":{"integer":false,"sha256":false,"scalar":true},"accounting_valid":true,"stop_facts":{"cleanup_unconfirmed":false,"clock_invalid":false,"counter_overflow":false,"coverage_lost":false,"early_worker_exit":true,"incomplete_load_checks":true,"initial_coverage_partial":false,"initial_coverage_unavailable":false,"insufficient_budget":false,"invalid_observation":false,"launch_incomplete":false,"normal_load_complete":false,"observation_timeout":true,"observer_panic":false,"parent_cancelled":false,"parent_deadline":false,"readiness_incomplete":false,"thermal_limit":true,"watchdog_deadline":false,"worker_panic":false},"safety":{"global_timeout_ms":660000,"thermal_limit_millicelsius":90000,"thermal_sensors":1,"thermal_samples":1,"peak_millicelsius":90000,"monitor_status":"failed","monitor_failure":"observation_timeout","cleanup_confirmed":true,"thermal_observations":1,"thermal_coverage":"available","thermal_rejected_readings":0,"thermal_unavailable_readings":0,"thermal_enumeration_errors":0},"workers":[{"index":0,"started_ms":0,"ready_ms":1,"load_entered_ms":2,"stopped_ms":1000,"exit_reason":"calculation_mismatch","preparation_iterations":1,"preparation_checks_passed":12,"iterations":256,"checks_passed":23,"load_iterations":255,"load_checks_passed":11,"calculation_failures":{"integer":false,"sha256":false,"scalar":true},"failure_phase":"load","mismatch":{"vector":"div3","expected_bits":"3d70000000000000","actual_bits":"3d70000000000001"}}]},"planned_status":"scheduled"},{"id":"memory.quick","version":2,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"audio.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"input.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"input.keyboard.quick","version":2,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"input.pointer.quick","version":2,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"network.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"storage.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"usb.readiness.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"graphics.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"display.operator.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"inspection.exterior.quick","version":1,"status":"skip","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"excluded","detail":"not run; disposition recorded in the frozen plan"},"planned_status":"excluded"},{"id":"run.integrity.quick","version":1,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.run-integrity/v1","status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","planned_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","final_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","planned_component_count":1,"final_component_count":1,"excluded_tests":["memory.quick","audio.readiness.quick","input.readiness.quick","input.keyboard.quick","input.pointer.quick","network.readiness.quick","storage.readiness.quick","usb.readiness.quick","graphics.quick","display.operator.quick","inspection.exterior.quick"],"session":{"schema":"hhsoft.pc-tester.quick-session/v8","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","stop_reason":"thermal_limit","workers":[{"id":"cpu.quick","state":"completed","outcome":"pass","timing":{"started_ms":2,"finished_ms":5}},{"id":"cpu.stress","state":"completed","outcome":"fail","timing":{"started_ms":6,"finished_ms":1008}},{"id":"memory.quick","state":"not_scheduled","outcome":"excluded"},{"id":"audio.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"input.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"input.keyboard.quick","state":"not_scheduled","outcome":"excluded"},{"id":"input.pointer.quick","state":"not_scheduled","outcome":"excluded"},{"id":"network.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"storage.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"usb.readiness.quick","state":"not_scheduled","outcome":"excluded"},{"id":"graphics.quick","state":"not_scheduled","outcome":"excluded"},{"id":"display.operator.quick","state":"not_scheduled","outcome":"excluded"},{"id":"inspection.exterior.quick","state":"not_scheduled","outcome":"excluded"}],"safety":[{"phase":"preflight","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"before-cpu.quick","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"before-cpu.stress","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"final","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"pre-sign","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}}],"monitor":{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"pass","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":1,"completed_observations":1,"elapsed_ms":1046,"first_completed_ms":1000,"last_completed_ms":1000,"maximum_gap_ms":1000,"cleanup_confirmed":true,"last_observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},"preparation_started_ms":1020,"inventory_prepared_ms":1030,"stop_requested_ms":1040,"safety_joined_ms":1045,"local_thermal_sources":[{"worker_id":"cpu.stress","evidence_schema":"hhsoft.pc-tester.cpu-stress/v1","evidence_file_sha256":"sha256:fd582041bf060a3528107ff9c96cc693c4b3cd0b37b5525ae2ac94d2050737d7","evidence_typed_sha256":"sha256:c1cfa426c4b3472bc47ed01668d45eeaba8695638c040fec5a39e4b0708974b8","peak_millicelsius":90000,"limit_millicelsius":90000,"started_ms":6,"finished_ms":1008}],"thermal":{"schema":"hhsoft.pc-tester.run-thermal/v1","status":"completed","limit_millicelsius":90000,"interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"stop_timeout_ms":2000,"attempted_observations":3,"completed_observations":3,"periodic_attempts":1,"periodic_completions":1,"elapsed_ms":1048,"first_completed_ms":1,"last_completed_ms":1032,"maximum_gap_ms":1000,"initial":{"requested_ms":0,"attempted":true,"completed":true,"started_ms":0,"completed_ms":1,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"final":{"requested_ms":1031,"attempted":true,"completed":true,"started_ms":1031,"completed_ms":1032,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"aggregate":{"coverage":"unavailable","accepted_observations":3,"available_observations":0,"partial_observations":0,"unavailable_observations":3,"maximum_sensors":0,"valid_readings":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0},"cleanup_confirmed":true,"joined":true,"joined_ms":1047,"last_observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"budget_milliseconds":900000,"elapsed_milliseconds":1050,"profile":"standard","cpu_stress_source":{"worker_id":"cpu.stress","evidence_schema":"hhsoft.pc-tester.cpu-stress/v1","evidence_file_sha256":"sha256:fd582041bf060a3528107ff9c96cc693c4b3cd0b37b5525ae2ac94d2050737d7","evidence_typed_sha256":"sha256:c1cfa426c4b3472bc47ed01668d45eeaba8695638c040fec5a39e4b0708974b8"}}}}],"coverage_scope":"frozen_plan"}`

func TestCPUStressSignedAdverseEvidenceKeepsHardCauseAndHeat(t *testing.T) {
	var p app.InventoryPayload
	if json.Unmarshal([]byte(stressAdverseReportLiteral), &p) != nil {
		t.Fatal("adverse literal")
	}
	if err := app.ValidateCoverage(p); err != nil {
		t.Fatal("adverse prerequisite", err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	html, err := render.InventoryHTMLForVersion(p, "0.0.1-fixture")
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := render.InventoryPDFForVersion(p, "0.0.1-fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, views := range []bool{false, true} {
		var h, d []byte
		if views {
			h, d = html, pdf
		}
		encoded, _, keys := independentlySignedRetest(t, raw, h, d, nil, false, nil, nil)
		artifact, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), keys)
		if err != nil {
			t.Fatal(err)
		}
		var admitted app.InventoryPayload
		if json.Unmarshal(artifact.InventoryJSON, &admitted) != nil {
			t.Fatal("verified payload")
		}
		value, err := app.ValidatedCPUStress(admitted)
		if err != nil || value == nil || string(value.Status) != "fail" || !value.CalculationFailures.Scalar || !value.StopFacts.ObservationTimeout || !value.StopFacts.ThermalLimit || value.Safety.MonitorFailure != "observation_timeout" {
			t.Fatal("signed adverse facts lost", err)
		}
	}
}

func TestCPUStressHistoricalFailureNotesRemainVerifiable(t *testing.T) {
	for _, status := range []app.TestStatus{app.TestError, app.TestAborted} {
		t.Run(string(status), func(t *testing.T) {
			p := stressBundlePayload(t)
			p.CoverageScope = ""
			note := []byte(`{"schema":"hhsoft.pc-tester.execution-note/v1","status":"` + string(status) + `","source_status":"` + string(status) + `","detail":"bounded evidence was not produced; outcome recorded by the run supervisor"}`)
			p.TestResults = []app.TestResult{{ID: "cpu.stress", Version: 1, Status: status, Evidence: note}}
			if value, err := app.ValidatedCPUStress(p); err != nil || value != nil {
				t.Fatal("historical note prerequisite", err)
			}
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			encoded, envelope, keys := independentlySignedRetest(t, raw, nil, nil, nil, false, nil, nil)
			if err := report.Verify(envelope, keys); err != nil {
				t.Fatal("historical note signature", err)
			}
			artifact, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), keys)
			if err != nil || !bytes.Equal(artifact.InventoryJSON, raw) || artifact.RenderedReport != nil || artifact.RenderedPDF != nil {
				t.Fatal("historical no-view note changed", err)
			}
		})
	}
}
