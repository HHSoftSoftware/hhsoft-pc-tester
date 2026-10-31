package comparison

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
)

// This independent literal describes a bounded synthetic report, not device qualification.
const comparisonTouchscreenLiteral = `{"schema_version":1,"components":[{"type":"input","id":"event0","attributes":[{"name":"abs_capabilities","value":"3"},{"name":"bitmap_word_bits","value":"64"},{"name":"bus","value":"0003"},{"name":"category","value":"touchscreen"},{"name":"event_capabilities","value":"b"},{"name":"handlers","value":"event0"},{"name":"interface","value":"event0"},{"name":"key_capabilities","value":"400 0 0 0 0 0"},{"name":"msc_capabilities","value":"0"},{"name":"product_id","value":"abcd"},{"name":"properties","value":"2"},{"name":"relative_capabilities","value":"0"},{"name":"touchscreen_candidate","value":"true"},{"name":"vendor_id","value":"1234"},{"name":"version","value":"0001"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"fixture","confidence":"unavailable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"fixture","key_id":"hmac-sha256:3333333333333333333333333333333333333333333333333333333333333333","identified_parts":0,"unidentified_parts":1},"test_results":[{"id":"input.touchscreen.quick","version":2,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.touchscreen-functional/v1","status":"pass","stop_reason":"completed","limit_ms":45000,"duration_ms":1000,"cleanup_confirmed":true,"devices":[{"interface":"event0","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":4660,"product":43981,"version":1},"capabilities":{"protocol_version":65537,"events":[0,1,3],"keys":[330],"relative":[],"properties":[1],"misc":[],"axes":[{"code":0,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1},{"code":1,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1}]},"x_threshold":50,"y_threshold":50,"frames":6,"contact_starts":1,"contact_releases":1,"xy_baselines":1,"qualifying_contacts":1,"x_positive":2,"x_negative":2,"y_positive":2,"y_negative":2,"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]},"planned_status":"scheduled"},{"id":"run.integrity.quick","version":1,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.run-integrity/v1","status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","planned_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","final_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","planned_component_count":1,"final_component_count":1}}],"coverage_scope":"frozen_plan"}`

func comparisonTouchscreenPayload(t *testing.T) app.InventoryPayload {
	t.Helper()
	var p app.InventoryPayload
	if err := json.Unmarshal([]byte(comparisonTouchscreenLiteral), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func comparisonTouchscreenReport(p app.InventoryPayload) Report {
	return Report{Inventory: p, Reference: ReportReference{SessionID: "touchscreen-comparison-fixture", GeneratedTime: "2026-10-26T00:00:00Z",
		SoftwareVersion: "0.1.0-dev.0", SignerKeyID: "fixture", ManifestDigest: "sha256:" + strings.Repeat("1", 64),
		SystemFingerprint: p.SystemFingerprint, ComponentCount: len(p.Components)}}
}

func TestTouchscreenComparisonConsumesValidatedReportsAndOwnsSlices(t *testing.T) {
	for _, name := range []string{"observed", "interrupted_retained_pass", "historical_definition_one"} {
		t.Run(name, func(t *testing.T) {
			before, after := comparisonTouchscreenPayload(t), comparisonTouchscreenPayload(t)
			for _, p := range []*app.InventoryPayload{&before, &after} {
				switch name {
				case "interrupted_retained_pass":
					p.TestResults[0].Status = app.TestAborted
					p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("\"status\":\"pass\""), []byte("\"status\":\"aborted\""), 1)
					p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("\"stop_reason\":\"completed\""), []byte("\"stop_reason\":\"operator_cancelled\",\"detail\":\"operator_cancelled\""), 1)
				case "historical_definition_one":
					p.TestResults[0].Version = 1
					p.TestResults[0].Status = app.TestSkip
					p.TestResults[0].PlannedStatus = app.PlannedUnsupported
					p.TestResults[0].Evidence = []byte("{\"reason\":\"historical unsupported definition\"}")
				}
			}
			result, err := Compare(comparisonTouchscreenReport(before), comparisonTouchscreenReport(after))
			if err != nil {
				t.Fatal("valid report rejected", err)
			}
			encoded, _ := json.Marshal(result)
			before.Components[0].Attributes[0].Value = "caller mutation"
			after.Components[0].Attributes[0].Value = "later mutation"
			again, _ := json.Marshal(result)
			if !bytes.Equal(encoded, again) {
				t.Fatal("comparison retains caller-owned attribute slices")
			}
		})
	}
}

func TestTouchscreenComparisonRejectsForgedEitherSide(t *testing.T) {
	for _, name := range []string{"before_neutral", "after_neutral", "before_cleanup", "after_cleanup", "before_inventory", "after_inventory", "future_definition", "missing_definition", "duplicate_row", "conflicting_integrity"} {
		t.Run(name, func(t *testing.T) {
			before, after := comparisonTouchscreenPayload(t), comparisonTouchscreenPayload(t)
			p := &after
			if strings.HasPrefix(name, "before_") {
				p = &before
			}
			switch name {
			case "before_neutral", "after_neutral":
				p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("\"final_neutral\":true"), []byte("\"final_neutral\":false"), 1)
			case "before_cleanup", "after_cleanup":
				p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("\"cleanup_confirmed\":true"), []byte("\"cleanup_confirmed\":false"), 1)
			case "before_inventory", "after_inventory":
				for i := range p.Components[0].Attributes {
					if p.Components[0].Attributes[i].Name == "vendor_id" {
						p.Components[0].Attributes[i].Value = "0000"
					}
				}
			case "future_definition":
				p.TestResults[0].Version = 99
			case "missing_definition":
				p.TestResults[0].Version = 0
			case "duplicate_row":
				p.TestResults = append(p.TestResults, p.TestResults[0])
			case "conflicting_integrity":
				p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte("\"planned_component_count\":1"), []byte("\"planned_component_count\":2"), 1)
			}
			if _, err := Compare(comparisonTouchscreenReport(before), comparisonTouchscreenReport(after)); err == nil {
				t.Fatal("comparison accepted a contradictory signed claim")
			}
		})
	}
}
