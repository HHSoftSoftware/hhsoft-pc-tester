package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/identity"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchscreenevidence"
)

// Independent finite report bytes; this fixture does not qualify a device.
const appTouchscreenLiteral = `{"schema_version":1,"components":[{"type":"input","id":"event0","attributes":[{"name":"abs_capabilities","value":"3"},{"name":"bitmap_word_bits","value":"64"},{"name":"bus","value":"0003"},{"name":"category","value":"touchscreen"},{"name":"event_capabilities","value":"b"},{"name":"handlers","value":"event0"},{"name":"interface","value":"event0"},{"name":"key_capabilities","value":"400 0 0 0 0 0"},{"name":"msc_capabilities","value":"0"},{"name":"product_id","value":"abcd"},{"name":"properties","value":"2"},{"name":"relative_capabilities","value":"0"},{"name":"touchscreen_candidate","value":"true"},{"name":"vendor_id","value":"1234"},{"name":"version","value":"0001"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"fixture","confidence":"unavailable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"fixture","key_id":"hmac-sha256:3333333333333333333333333333333333333333333333333333333333333333","identified_parts":0,"unidentified_parts":1},"test_results":[{"id":"input.touchscreen.quick","version":2,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.touchscreen-functional/v1","status":"pass","stop_reason":"completed","limit_ms":45000,"duration_ms":1000,"cleanup_confirmed":true,"devices":[{"interface":"event0","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":4660,"product":43981,"version":1},"capabilities":{"protocol_version":65537,"events":[0,1,3],"keys":[330],"relative":[],"properties":[1],"misc":[],"axes":[{"code":0,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1},{"code":1,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1}]},"x_threshold":50,"y_threshold":50,"frames":6,"contact_starts":1,"contact_releases":1,"xy_baselines":1,"qualifying_contacts":1,"x_positive":2,"x_negative":2,"y_positive":2,"y_negative":2,"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]},"planned_status":"scheduled"},{"id":"run.integrity.quick","version":1,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.run-integrity/v1","status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","planned_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","final_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","planned_component_count":1,"final_component_count":1}}],"coverage_scope":"frozen_plan"}`

func appTouchscreenPayload(t *testing.T) InventoryPayload {
	t.Helper()
	var value InventoryPayload
	if err := json.Unmarshal([]byte(appTouchscreenLiteral), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func appTouchscreenChange(t *testing.T, payload *InventoryPayload, change func(*inputtest.TouchscreenResult)) {
	t.Helper()
	var value inputtest.TouchscreenResult
	if err := json.Unmarshal(payload.TestResults[0].Evidence, &value); err != nil {
		t.Fatal(err)
	}
	change(&value)
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	payload.TestResults[0].Evidence, payload.TestResults[0].Status = raw, TestStatus(value.Status)
}

func appTouchscreenIntegrity(t *testing.T, payload *InventoryPayload, change func(*sessionevidence.IntegrityEvidence)) {
	t.Helper()
	var value sessionevidence.IntegrityEvidence
	if err := json.Unmarshal(payload.TestResults[1].Evidence, &value); err != nil {
		t.Fatal(err)
	}
	change(&value)
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	payload.TestResults[1].Evidence, payload.TestResults[1].Status = raw, TestStatus(value.Status)
}

func appTouchscreenAttribute(payload *InventoryPayload, name, value string) {
	attrs := payload.Components[0].Attributes
	for i := range attrs {
		if attrs[i].Name == name {
			attrs[i].Value = value
			return
		}
	}
	payload.Components[0].Attributes = append(attrs, inventory.Attribute{Name: name, Value: value})
}

func appTouchscreenDeleteAttribute(payload *InventoryPayload, name string) {
	attrs := payload.Components[0].Attributes
	for i := range attrs {
		if attrs[i].Name == name {
			payload.Components[0].Attributes = append(attrs[:i:i], attrs[i+1:]...)
			return
		}
	}
	panic("literal fixture attribute absent")
}

func appTouchscreenResetState(d inputtest.TouchscreenDeviceResult) inputtest.TouchscreenDeviceResult {
	return inputtest.TouchscreenDeviceResult{Interface: d.Interface, Status: inputtest.StatusInconclusive,
		Readable: d.Readable, Supported: d.Supported, Identity: d.Identity, Capabilities: d.Capabilities,
		CleanupConfirmed: true}
}

func TestTouchscreenAppLiteralReportsAndOwnedObservation(t *testing.T) {
	for _, name := range []string{"standalone", "aborted_retains_pass", "deadline_retains_pass", "prestate_layout", "unsupported_unknown_axis", "unreadable", "no_touchscreens"} {
		t.Run(name, func(t *testing.T) {
			payload := appTouchscreenPayload(t)
			appTouchscreenChange(t, &payload, func(v *inputtest.TouchscreenResult) {
				switch name {
				case "aborted_retains_pass":
					v.Status, v.StopReason, v.Detail = inputtest.StatusAborted, "operator_cancelled", "operator_cancelled"
				case "deadline_retains_pass":
					v.Status, v.StopReason, v.Detail = inputtest.StatusError, "deadline", "deadline"
					v.DurationMilliseconds = 45001
				case "prestate_layout":
					v.Devices[0] = appTouchscreenResetState(v.Devices[0])
					v.Devices[0].Detail = "unsupported_record_layout"
					v.Status, v.StopReason, v.Detail = inputtest.StatusError, "error", "unsupported_record_layout"
				case "unsupported_unknown_axis":
					d := appTouchscreenResetState(v.Devices[0])
					d.Capabilities.Axes = append(d.Capabilities.Axes, inputtest.TouchscreenAxis{Code: 2})
					d.Supported, d.Detail = false, "unsupported_capabilities"
					v.Devices[0] = d
					v.Status, v.StopReason, v.Detail = inputtest.StatusInconclusive, "error", "unsupported_or_unreadable_device"
				case "unreadable":
					d := appTouchscreenResetState(v.Devices[0])
					d.Identity, d.Capabilities = nil, nil
					d.Readable, d.Supported, d.Detail = false, false, "identity_unavailable"
					v.Devices[0] = d
					v.Status, v.StopReason, v.Detail = inputtest.StatusInconclusive, "error", "unsupported_or_unreadable_device"
				case "no_touchscreens":
					v.Devices = []inputtest.TouchscreenDeviceResult{}
					v.Status, v.StopReason, v.Detail = inputtest.StatusInconclusive, "no_touchscreens", "no_touchscreens"
				}
			})
			if name == "no_touchscreens" {
				payload.Components = nil
				payload.SystemFingerprint.UnidentifiedParts = 0
				appTouchscreenIntegrity(t, &payload, func(v *sessionevidence.IntegrityEvidence) { v.PlannedComponentCount = 0; v.FinalComponentCount = 0 })
			}
			original, _ := json.Marshal(payload)
			value, err := ValidatedTouchscreen(payload)
			if err != nil || value == nil {
				t.Fatalf("literal diagnostic rejected: %v", err)
			}
			if _, err := ValidatedSessionThermal(payload); err != nil {
				t.Fatal(err)
			}
			if len(value.Devices) != 0 {
				value.Devices[0].Interface = "event99"
				if value.Devices[0].Capabilities != nil {
					value.Devices[0].Capabilities.Events[0] = 63
				}
			}
			after, _ := json.Marshal(payload)
			if !bytes.Equal(original, after) {
				t.Fatal("returned nested observation aliases the signed payload")
			}
			again, err := ValidatedTouchscreen(payload)
			if err != nil || again == nil || len(again.Devices) != 0 && again.Devices[0].Interface != "event0" {
				t.Fatal("returned observation was reused")
			}
		})
	}
}

func TestTouchscreenAppRejectsContradictoryRows(t *testing.T) {
	cases := []struct {
		name   string
		change func(*InventoryPayload)
	}{
		{"definition_one", func(p *InventoryPayload) { p.TestResults[0].Version = 1 }},
		{"definition_zero", func(p *InventoryPayload) { p.TestResults[0].Version = 0 }},
		{"future_definition", func(p *InventoryPayload) { p.TestResults[0].Version = 99 }},
		{"id_alias", func(p *InventoryPayload) { p.TestResults[0].ID = "INPUT.TOUCHSCREEN.QUICK" }},
		{"misplaced", func(p *InventoryPayload) { p.TestResults[0].ID = "other.quick" }},
		{"duplicate_result", func(p *InventoryPayload) { p.TestResults = append(p.TestResults, p.TestResults[0]) }},
		{"missing_coverage", func(p *InventoryPayload) { p.CoverageScope = "" }},
		{"direct_coverage", func(p *InventoryPayload) { p.CoverageScope = CoverageDirectCapture }},
		{"row_status", func(p *InventoryPayload) { p.TestResults[0].Status = TestError }},
		{"not_scheduled", func(p *InventoryPayload) {
			p.TestResults[0].PlannedStatus = PlannedExcluded
			p.TestResults[0].Status = TestSkip
		}},
		{"plan_binding", func(p *InventoryPayload) { p.TestResults[0].PlanID = "sha256:" + strings.Repeat("9", 64) }},
		{"graph_binding", func(p *InventoryPayload) { p.TestResults[0].HardwareGraph = "hmac-sha256:" + strings.Repeat("9", 64) }},
		{"missing_integrity", func(p *InventoryPayload) { p.TestResults = p.TestResults[:1] }},
		{"duplicate_integrity", func(p *InventoryPayload) { p.TestResults = append(p.TestResults, p.TestResults[1]) }},
		{"integrity_version", func(p *InventoryPayload) { p.TestResults[1].Version = 2 }},
		{"empty_evidence", func(p *InventoryPayload) { p.TestResults[0].Evidence = nil }},
		{"opaque_note", func(p *InventoryPayload) { p.TestResults[0].Evidence = []byte("{\"status\":\"pass\"}") }},
		{"unknown_field", func(p *InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("{"), []byte("{\"coordinates\":[], "), 1)
		}},
		{"duplicate_member", func(p *InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("\"frames\":"), []byte("\"frames\":0,\"frames\":"), 1)
		}},
		{"trailing_value", func(p *InventoryPayload) {
			p.TestResults[0].Evidence = append(p.TestResults[0].Evidence, []byte(" {}")...)
		}},
		{"wrong_limit", func(p *InventoryPayload) {
			appTouchscreenChange(t, p, func(v *inputtest.TouchscreenResult) { v.LimitMilliseconds = 45001 })
		}},
		{"missing_final_proof", func(p *InventoryPayload) {
			appTouchscreenChange(t, p, func(v *inputtest.TouchscreenResult) { v.Devices[0].FinalNeutral = false })
		}},
		{"same_contact_overflow", func(p *InventoryPayload) {
			appTouchscreenChange(t, p, func(v *inputtest.TouchscreenResult) { v.Devices[0].ContactStarts = 4; v.Devices[0].ContactReleases = 4 })
		}},
		{"axis_frame_budget", func(p *InventoryPayload) {
			appTouchscreenChange(t, p, func(v *inputtest.TouchscreenResult) { v.Devices[0].XPositive = 3; v.Devices[0].XNegative = 3 })
		}},
		{"public_motion_without_release", func(p *InventoryPayload) {
			appTouchscreenChange(t, p, func(v *inputtest.TouchscreenResult) { v.Devices[0].ContactReleases = 0 })
		}},
	}
	if _, err := ValidatedTouchscreen(appTouchscreenPayload(t)); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := appTouchscreenPayload(t)
			c.change(&p)
			if ValidateCoverage(p) == nil {
				t.Fatal("contradictory report accepted")
			}
			if v, err := ValidatedTouchscreen(p); err == nil || v != nil {
				t.Fatal("invalid observation escaped")
			}
		})
	}
}

func TestTouchscreenAppCleanupUncertaintyIsFatal(t *testing.T) {
	for _, name := range []string{"coverage", "touchscreen_view", "thermal_view"} {
		t.Run(name, func(t *testing.T) {
			p := appTouchscreenPayload(t)
			appTouchscreenChange(t, &p, func(v *inputtest.TouchscreenResult) {
				v.Status = inputtest.StatusError
				v.StopReason = "cleanup_unconfirmed"
				v.Detail = "cleanup_unconfirmed"
				v.CleanupConfirmed = false
			})
			var err error
			switch name {
			case "coverage":
				err = ValidateCoverage(p)
			case "touchscreen_view":
				_, err = ValidatedTouchscreen(p)
			case "thermal_view":
				_, err = ValidatedSessionThermal(p)
			}
			if !errors.Is(err, touchscreenevidence.ErrCleanupUnconfirmed) {
				t.Fatalf("cleanup uncertainty lost: %v", err)
			}
		})
	}
}

func TestTouchscreenAppFinalCandidateBindings(t *testing.T) {
	cases := []struct {
		name   string
		change func(*InventoryPayload)
	}{
		{"missing_candidate", func(p *InventoryPayload) { p.Components = nil }},
		{"missing_marker", func(p *InventoryPayload) { appTouchscreenDeleteAttribute(p, "touchscreen_candidate") }},
		{"marker_false", func(p *InventoryPayload) { appTouchscreenAttribute(p, "touchscreen_candidate", "false") }},
		{"duplicate_component", func(p *InventoryPayload) { p.Components = append(p.Components, p.Components[0]) }},
		{"duplicate_attribute", func(p *InventoryPayload) {
			p.Components[0].Attributes = append(p.Components[0].Attributes, p.Components[0].Attributes[0])
		}},
		{"missing_width", func(p *InventoryPayload) { appTouchscreenDeleteAttribute(p, "bitmap_word_bits") }},
		{"unknown_width", func(p *InventoryPayload) { appTouchscreenAttribute(p, "bitmap_word_bits", "128") }},
		{"width_reinterpretation", func(p *InventoryPayload) { appTouchscreenAttribute(p, "bitmap_word_bits", "32") }},
		{"vendor_mismatch", func(p *InventoryPayload) { appTouchscreenAttribute(p, "vendor_id", "0000") }},
		{"vendor_alias_conflict", func(p *InventoryPayload) { appTouchscreenAttribute(p, "vendor", "0000") }},
		{"absolute_alias_conflict", func(p *InventoryPayload) { appTouchscreenAttribute(p, "absolute_capabilities", "7") }},
		{"hidden_key", func(p *InventoryPayload) { appTouchscreenAttribute(p, "key_capabilities", "400 1 0 0 0 0") }},
		{"hidden_relative", func(p *InventoryPayload) { appTouchscreenAttribute(p, "relative_capabilities", "1") }},
		{"missing_msc", func(p *InventoryPayload) { appTouchscreenDeleteAttribute(p, "msc_capabilities") }},
		{"hidden_msc", func(p *InventoryPayload) { appTouchscreenAttribute(p, "msc_capabilities", "10") }},
		{"handler_conflict", func(p *InventoryPayload) { appTouchscreenAttribute(p, "handlers", "event1") }},
		{"handler_duplicate", func(p *InventoryPayload) { appTouchscreenAttribute(p, "handlers", "event0 event0") }},
		{"noncanonical_interface", func(p *InventoryPayload) { appTouchscreenAttribute(p, "interface", "event00") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := appTouchscreenPayload(t)
			c.change(&p)
			if ValidateCoverage(p) == nil {
				t.Fatal("unbound candidate accepted")
			}
			appTouchscreenChange(t, &p, func(v *inputtest.TouchscreenResult) {
				v.Status = inputtest.StatusAborted
				v.StopReason = "operator_cancelled"
				v.Detail = "operator_cancelled"
			})
			if ValidateCoverage(p) == nil {
				t.Fatal("retained pass escaped through aggregate cancellation")
			}
		})
	}
}

func TestTouchscreenAppDefinitionSelectionDoesNotLoseClaims(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"family", `{"schema":"hhsoft.pc-tester.touchscreen-functional/v1"}`},
		{"future_family", `{"schema":"hhsoft.pc-tester.touchscreen-functional/v999"}`},
		{"unversioned_family", `{"schema":"hhsoft.pc-tester.touchscreen-functional"}`},
		{"escaped_family", `{"schema":"hhsoft.pc-tester.touchscreen-\u0066unctional/v1"}`},
		{"definition_two", `{"id":"input.touchscreen.quick","version":2}`},
		{"future_definition", `{"id":"input.touchscreen.quick","version":99}`},
		{"zero_definition", `{"id":"input.touchscreen.quick","version":0}`},
		{"missing_version_row", `{"test_results":[{"id":"input.touchscreen.quick"}]}`},
		{"missing_version_status", `{"id":"input.touchscreen.quick","status":"pass"}`},
		{"missing_version_evidence", `{"id":"input.touchscreen.quick","evidence":{}}`},
		{"string_version", `{"id":"input.touchscreen.quick","version":"1"}`},
		{"number_spelling", `{"id":"input.touchscreen.quick","version":1.0}`},
		{"case_id", `{"ID":"INPUT.TOUCHSCREEN.QUICK","version":1}`},
		{"case_version", `{"id":"input.touchscreen.quick","VERSION":1}`},
		{"earlier_version", `{"id":"input.touchscreen.quick","version":2,"version":1}`},
		{"earlier_id", `{"id":"input.touchscreen.quick","id":"other","version":2}`},
		{"hidden_results", `{"test_results":[{"id":"input.touchscreen.quick","version":2}],"test_results":[]}`},
		{"null_version", `{"id":"input.touchscreen.quick","version":null}`},
		{"truncated", `{"id":"input.touchscreen.quick","version":`},
		{"trailing", `{} {}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "escaped_family" && !strings.Contains(c.raw, `\u0066`) {
				t.Fatal("escape fixture lost its literal escape")
			}
			if !HasTouchscreenClaim([]byte(c.raw)) {
				t.Fatal("strict selector lost a claim")
			}
		})
	}
	for _, name := range []string{"historical_one", "id_only", "worker_reference", "unrelated", "empty"} {
		t.Run(name, func(t *testing.T) {
			raw := ""
			switch name {
			case "historical_one":
				raw = `{"id":"input.touchscreen.quick","version":1,"status":"skip","evidence":{"reason":"unsupported"}}`
			case "id_only":
				raw = `{"id":"input.touchscreen.quick"}`
			case "worker_reference":
				raw = `{"id":"input.touchscreen.quick","state":"not_scheduled","outcome":"excluded"}`
			case "unrelated":
				raw = `{"id":"input.pointer.quick","version":2}`
			}
			if HasTouchscreenClaim([]byte(raw)) {
				t.Fatal("explicit historical/reference exception was lost")
			}
		})
	}
}

func TestTouchscreenAppCandidateSetIsPhaseAware(t *testing.T) {
	for _, name := range []string{"pass_requires_all", "cancel_retains_bound_pass"} {
		t.Run(name, func(t *testing.T) {
			p := appTouchscreenPayload(t)
			c := p.Components[0]
			c.ID = "event1"
			c.Attributes = append([]inventory.Attribute(nil), c.Attributes...)
			for i := range c.Attributes {
				if c.Attributes[i].Name == "interface" || c.Attributes[i].Name == "handlers" {
					c.Attributes[i].Value = "event1"
				}
			}
			p.Components = append(p.Components, c)
			p.SystemFingerprint.UnidentifiedParts = 2
			appTouchscreenIntegrity(t, &p, func(v *sessionevidence.IntegrityEvidence) { v.PlannedComponentCount = 2; v.FinalComponentCount = 2 })
			if name == "cancel_retains_bound_pass" {
				appTouchscreenChange(t, &p, func(v *inputtest.TouchscreenResult) {
					v.Status = inputtest.StatusAborted
					v.StopReason = "operator_cancelled"
					v.Detail = "operator_cancelled"
				})
			}
			err := ValidateCoverage(p)
			if name == "pass_requires_all" && err == nil {
				t.Fatal("aggregate pass omitted a final candidate")
			}
			if name == "cancel_retains_bound_pass" && err != nil {
				t.Fatal("interruption fabricated a missing observation requirement", err)
			}
		})
	}
}

func appTouchscreenComponent(t *testing.T) inventory.Component {
	t.Helper()
	p := appTouchscreenPayload(t)
	return inventory.Component{Type: inventory.ComponentInput, ID: "event0", Attributes: append([]inventory.Attribute(nil), p.Components[0].Attributes...)}
}

func appTouchscreenSetComponent(c *inventory.Component, name, value string) {
	for i := range c.Attributes {
		if c.Attributes[i].Name == name {
			c.Attributes[i].Value = value
			return
		}
	}
	c.Attributes = append(c.Attributes, inventory.Attribute{Name: name, Value: value})
}

func appTouchscreenDeleteComponent(c *inventory.Component, name string) {
	for i := range c.Attributes {
		if c.Attributes[i].Name == name {
			c.Attributes = append(c.Attributes[:i:i], c.Attributes[i+1:]...)
			return
		}
	}
	panic("component fixture attribute absent")
}

func TestTouchscreenPublicProjectionIsFiniteAndOwned(t *testing.T) {
	for _, name := range []string{"touchscreen", "ambiguous_input", "ambiguous_pointer", "derived_marker", "canonical_aliases", "unknown_width", "unknown_abs_marker"} {
		t.Run(name, func(t *testing.T) {
			c := appTouchscreenComponent(t)
			for _, field := range []string{"kernel_path", "unique_id", "coordinates", "msc_payload", "error"} {
				appTouchscreenSetComponent(&c, field, "private-canary-"+field)
			}
			appTouchscreenSetComponent(&c, "name", "Fixture Panel")
			appTouchscreenSetComponent(&c, "model", "Model A")
			switch name {
			case "ambiguous_input":
				appTouchscreenSetComponent(&c, "category", "input")
			case "ambiguous_pointer":
				appTouchscreenSetComponent(&c, "category", "pointer")
			case "derived_marker":
				appTouchscreenDeleteComponent(&c, "touchscreen_candidate")
			case "canonical_aliases":
				appTouchscreenSetComponent(&c, "vendor_id", "ABCD")
				appTouchscreenSetComponent(&c, "vendor", "abcd")
				appTouchscreenSetComponent(&c, "event_capabilities", "000B")
				appTouchscreenSetComponent(&c, "ev_capabilities", "b")
			case "unknown_width":
				appTouchscreenDeleteComponent(&c, "bitmap_word_bits")
			case "unknown_abs_marker":
				appTouchscreenSetComponent(&c, "abs_capabilities", "7")
			}
			original, _ := json.Marshal(c)
			attrs, candidate, err := touchscreenPublicAttributes(c)
			if err != nil || !candidate {
				t.Fatal("candidate lost", err)
			}
			got := map[string]string{}
			for _, v := range attrs {
				got[v.Name] = v.Value
			}
			if got["touchscreen_candidate"] != "true" || got["interface"] != "event0" || got["handlers"] != "event0" {
				t.Fatal("missing finite candidate identity")
			}
			if name == "ambiguous_input" || name == "ambiguous_pointer" {
				if _, exists := got["name"]; exists {
					t.Fatal("dual-candidate projection exported a display name")
				}
				if _, exists := got["model"]; exists {
					t.Fatal("dual-candidate projection exported a model")
				}
			} else if got["name"] != "Fixture Panel" || got["model"] != "Model A" {
				t.Fatal("touchscreen inventory lost bounded useful labels")
			}
			encoded, _ := json.Marshal(attrs)
			if bytes.Contains(encoded, []byte("private-canary")) {
				t.Fatal("unbounded source data exported")
			}
			if name == "unknown_width" {
				if _, ok := got["bitmap_word_bits"]; ok {
					t.Fatal("invented source word width")
				}
			}
			if name == "unknown_abs_marker" && got["abs_capabilities"] != "7" {
				t.Fatal("unknown ABS support marker was pruned")
			}
			if name == "canonical_aliases" && (got["vendor_id"] != "abcd" || got["event_capabilities"] != "b") {
				t.Fatal("numeric aliases did not agree canonically")
			}
			if len(attrs) == 0 {
				t.Fatal("empty projection")
			}
			attrs[0].Value = "mutated"
			after, _ := json.Marshal(c)
			if !bytes.Equal(original, after) {
				t.Fatal("projection aliases original inventory")
			}
			snapshot := inventory.NewSnapshot([]inventory.Component{c})
			payload, err := BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x31}, 32), "fixture")
			if err != nil || len(payload.Components) != 1 {
				t.Fatal(err)
			}
			output, _ := json.Marshal(payload)
			if bytes.Contains(output, []byte("private-canary")) {
				t.Fatal("BuildInventoryPayload bypassed finite projection")
			}
			payload.Components[0].Attributes[0].Value = "second mutation"
			after, _ = json.Marshal(c)
			if !bytes.Equal(original, after) {
				t.Fatal("payload attributes alias original inventory")
			}
		})
	}
}

func TestTouchscreenPublicLabelsBoundDisplayTextAndKnownIdentifiers(t *testing.T) {
	const identifier = "synthetic-known-input-id"
	cases := []struct {
		name      string
		nameText  string
		modelText string
		uniqueID  string
		hasID     bool
		wantName  string
		wantModel string
	}{
		{"absent_identifier", "Fixture Panel", "Model A", "", false, "Fixture Panel", "Model A"},
		{"empty_identifier", "Fixture Panel", "Model A", "", true, "Fixture Panel", "Model A"},
		{"safe_pair", "Fixture Panel", "Model A", identifier, true, "Fixture Panel", "Model A"},
		{"name_exact_identifier", identifier, "Model A", identifier, true, "", "Model A"},
		{"model_exact_identifier", "Fixture Panel", identifier, identifier, true, "Fixture Panel", ""},
		{"name_contains_identifier", "panel " + identifier + " suffix", "Model A", identifier, true, "", "Model A"},
		{"model_contains_identifier", "Fixture Panel", "model " + identifier + " suffix", identifier, true, "Fixture Panel", ""},
		{"both_contain_identifier", "prefix " + identifier, identifier + " suffix", identifier, true, "", ""},
		{"identifier_longer_than_labels", "Fixture Panel", "Model A", strings.Repeat("x", 257), true, "Fixture Panel", "Model A"},
		{"name_exact_256_bytes", strings.Repeat("n", 256), "Model A", identifier, true, strings.Repeat("n", 256), "Model A"},
		{"name_over_256_bytes", strings.Repeat("n", 257), "Model A", identifier, true, "", "Model A"},
		{"model_exact_256_bytes", "Fixture Panel", strings.Repeat("m", 256), identifier, true, "Fixture Panel", strings.Repeat("m", 256)},
		{"model_over_256_bytes", "Fixture Panel", strings.Repeat("m", 257), identifier, true, "Fixture Panel", ""},
		{"utf8_exact_256_bytes", strings.Repeat("\u00e9", 128), "Model A", identifier, true, strings.Repeat("\u00e9", 128), "Model A"},
		{"utf8_over_256_bytes", strings.Repeat("\u00e9", 129), "Model A", identifier, true, "", "Model A"},
		{"invalid_utf8_name", "panel\xff", "Model A", identifier, true, "", "Model A"},
		{"invalid_utf8_model", "Fixture Panel", "model\xc3", identifier, true, "Fixture Panel", ""},
		{"name_ascii_control", "panel\n", "Model A", identifier, true, "", "Model A"},
		{"model_unicode_control", "Fixture Panel", "model\u0085", identifier, true, "Fixture Panel", ""},
		{"name_delete_control", "panel\x7f", "Model A", identifier, true, "", "Model A"},
		{"empty_labels", "", "", identifier, true, "", ""},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			component := appTouchscreenComponent(t)
			appTouchscreenSetComponent(&component, "name", row.nameText)
			appTouchscreenSetComponent(&component, "model", row.modelText)
			if row.hasID {
				appTouchscreenSetComponent(&component, "unique_id", row.uniqueID)
			}
			appTouchscreenSetComponent(&component, "kernel_path", "private-path-canary")
			appTouchscreenSetComponent(&component, "coordinates", "private-coordinate-canary")
			snapshot := inventory.Snapshot{SchemaVersion: inventory.SnapshotSchemaVersion, Components: []inventory.Component{component}}
			before := snapshot
			before.Components = append([]inventory.Component(nil), snapshot.Components...)
			before.Components[0].Attributes = append([]inventory.Attribute(nil), component.Attributes...)
			payload, err := BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x31}, 32), "fixture")
			if err != nil || len(payload.Components) != 1 {
				t.Fatal("label filtering prevented valid inventory projection", err)
			}
			got := map[string]string{}
			for _, attribute := range payload.Components[0].Attributes {
				got[attribute.Name] = attribute.Value
			}
			if got["category"] != "touchscreen" || got["touchscreen_candidate"] != "true" || got["interface"] != "event0" {
				t.Fatal("label handling lost finite inventory metadata")
			}
			for field, expected := range map[string]string{"name": row.wantName, "model": row.wantModel} {
				value, exists := got[field]
				if value != expected || exists != (expected != "") {
					t.Fatalf("incorrect bounded label projection for %s", field)
				}
			}
			for _, field := range []string{"unique_id", "kernel_path", "coordinates"} {
				if _, exists := got[field]; exists {
					t.Fatal("private input field escaped the label projection")
				}
			}
			payload.Components[0].Attributes[0].Value = "changed public copy"
			if !reflect.DeepEqual(snapshot, before) {
				t.Fatal("label projection or public mutation changed original inventory")
			}
		})
	}
}

func TestTouchscreenPublicLabelsRetainOriginalIdentityMaterial(t *testing.T) {
	const inputIdentifier = "synthetic-known-input-identity"
	const systemIdentifier = "synthetic-known-system-identity"
	component := appTouchscreenComponent(t)
	appTouchscreenSetComponent(&component, "unique_id", inputIdentifier)
	appTouchscreenSetComponent(&component, "name", "prefix "+inputIdentifier+" suffix")
	appTouchscreenSetComponent(&component, "model", "Fixture Panel")
	system := inventory.NewComponent(inventory.ComponentSystem, "system:0", map[string]string{"product_uuid": systemIdentifier})
	snapshot := inventory.NewSnapshot([]inventory.Component{component, system})
	before, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x31}, 32)
	expected, err := identity.Compute(key, []identity.ComponentMaterial{{Domain: "system", Material: []byte("product_uuid\x00" + systemIdentifier)}})
	if err != nil {
		t.Fatal("independent supported identity prerequisite failed", err)
	}
	payload, err := BuildInventoryPayload(snapshot, key, "fixture")
	if err != nil || len(payload.Components) != 2 {
		t.Fatal(err)
	}
	inputCount, systemCount := 0, 0
	for _, record := range payload.Components {
		switch record.Type {
		case inventory.ComponentInput:
			inputCount++
			if record.Identity.Source != "" || record.Identity.Value != "" || record.Identity.Confidence != "unavailable" {
				t.Fatal("input label or unique_id acquired unsupported identity authority")
			}
		case inventory.ComponentSystem:
			systemCount++
			if record.Identity.Source != "product_uuid" || record.Identity.Value != fmt.Sprintf("hmac-sha256:%x", expected.ComponentPseudonyms[0].Digest) {
				t.Fatal("projection changed supported component identity material")
			}
		}
	}
	if inputCount != 1 || systemCount != 1 || payload.SystemFingerprint.Value != fmt.Sprintf("hmac-sha256:%x", expected.SystemHMAC) ||
		payload.SystemFingerprint.IdentifiedParts != 1 || payload.SystemFingerprint.UnidentifiedParts != 1 {
		t.Fatal("public label filtering changed original HMAC material or identity coverage")
	}
	public, err := json.Marshal(payload)
	if err != nil || bytes.Contains(public, []byte(inputIdentifier)) || bytes.Contains(public, []byte(systemIdentifier)) ||
		!bytes.Contains(public, []byte("Fixture Panel")) {
		t.Fatal("identity suppression removed the safe label or exported private identity")
	}
	after, err := json.Marshal(snapshot)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("public projection mutated an original identity owner")
	}
}

func TestTouchscreenPublicProjectionRejectsAmbiguity(t *testing.T) {
	cases := []struct {
		name   string
		change func(*inventory.Component)
	}{
		{"duplicate_attribute", func(c *inventory.Component) { c.Attributes = append(c.Attributes, c.Attributes[0]) }},
		{"false_marker", func(c *inventory.Component) { appTouchscreenSetComponent(c, "touchscreen_candidate", "false") }},
		{"malformed_marker", func(c *inventory.Component) { appTouchscreenSetComponent(c, "touchscreen_candidate", "TRUE") }},
		{"identity_conflict", func(c *inventory.Component) { appTouchscreenSetComponent(c, "vendor", "ffff") }},
		{"bitmap_conflict", func(c *inventory.Component) { appTouchscreenSetComponent(c, "absolute_capabilities", "7") }},
		{"invalid_alias", func(c *inventory.Component) { appTouchscreenSetComponent(c, "absolute_capabilities", "unsafe") }},
		{"handler_conflict", func(c *inventory.Component) { appTouchscreenSetComponent(c, "handlers", "event1") }},
		{"handler_duplicate", func(c *inventory.Component) { appTouchscreenSetComponent(c, "handlers", "event0 event0") }},
		{"interface_alias", func(c *inventory.Component) { appTouchscreenSetComponent(c, "interface", "event00") }},
		{"empty_name", func(c *inventory.Component) {
			c.Attributes = append(c.Attributes, inventory.Attribute{Name: "", Value: "x"})
		}},
		{"too_many_attributes", func(c *inventory.Component) {
			for i := 0; i < 129; i++ {
				c.Attributes = append(c.Attributes, inventory.Attribute{Name: fmt.Sprintf("field%d", i), Value: "x"})
			}
		}},
		{"ambiguous_unsafe_abs", func(c *inventory.Component) {
			appTouchscreenSetComponent(c, "category", "input")
			appTouchscreenSetComponent(c, "abs_capabilities", "unsafe")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			component := appTouchscreenComponent(t)
			c.change(&component)
			// Preserve malformed duplicate attributes; NewSnapshot deliberately canonicalizes them.
			snapshot := inventory.Snapshot{SchemaVersion: inventory.SnapshotSchemaVersion, Components: []inventory.Component{component}}
			p, err := BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x31}, 32), "fixture")
			if err == nil || !reflect.DeepEqual(p, InventoryPayload{}) {
				t.Fatal("ambiguous source produced a partial public payload")
			}
		})
	}
}

func TestTouchscreenAppFiniteDispositionNotes(t *testing.T) {
	for _, name := range []string{"excluded", "not_present", "unsupported", "missing_driver", "error", "aborted"} {
		t.Run(name, func(t *testing.T) {
			p := appTouchscreenPayload(t)
			status := name
			planned := name
			detail := "not run; disposition recorded in the frozen plan"
			switch name {
			case "excluded", "not_present", "unsupported":
				status = "skip"
			case "missing_driver":
				status = "inconclusive"
			default:
				planned = "scheduled"
				detail = "bounded evidence was not produced; outcome recorded by the run supervisor"
			}
			row := &p.TestResults[0]
			row.Status = TestStatus(status)
			row.PlannedStatus = PlannedStatus(planned)
			row.Evidence, _ = json.Marshal(map[string]string{"schema": "hhsoft.pc-tester.execution-note/v1", "status": status, "source_status": name, "detail": detail})
			if err := ValidateCoverage(p); err != nil {
				t.Fatal("finite disposition rejected", err)
			}
			if v, err := ValidatedTouchscreen(p); err != nil || v != nil {
				t.Fatal("note became an observation", err)
			}
			row.Evidence = bytes.Replace(row.Evidence, []byte(detail), []byte("arbitrary error text"), 1)
			if ValidateCoverage(p) == nil {
				t.Fatal("opaque diagnostic was accepted as a finite note")
			}
		})
	}
}

func TestTouchscreenPublicProjectionPreservesUnaddressableInventory(t *testing.T) {
	for _, name := range []string{"category_only", "non_event_handlers", "capabilities_without_interface", "ambiguous_input"} {
		t.Run(name, func(t *testing.T) {
			component := appTouchscreenComponent(t)
			appTouchscreenDeleteComponent(&component, "interface")
			appTouchscreenDeleteComponent(&component, "handlers")
			switch name {
			case "category_only":
				component.Attributes = []inventory.Attribute{{Name: "category", Value: "touchscreen"}}
			case "non_event_handlers":
				appTouchscreenSetComponent(&component, "handlers", "mouse0")
			case "ambiguous_input":
				appTouchscreenSetComponent(&component, "category", "input")
			}
			for _, field := range []string{"name", "model", "kernel_path", "unique_id", "coordinates"} {
				appTouchscreenSetComponent(&component, field, "private-canary-unaddressable")
			}
			original, err := json.Marshal(component)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := inventory.NewSnapshot([]inventory.Component{component})
			snapshotBefore, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x31}, 32), "fixture")
			if err != nil || len(payload.Components) != 1 {
				t.Fatal("incomplete inventory prevented report construction", err)
			}
			record := payload.Components[0]
			if record.ID != component.ID || record.Type != component.Type {
				t.Fatal("unaddressable component disappeared")
			}
			attributes := map[string]string{}
			for _, attribute := range record.Attributes {
				attributes[attribute.Name] = attribute.Value
			}
			if attributes["touchscreen_candidate"] != "true" {
				t.Fatal("incomplete candidate classification disappeared")
			}
			if _, exists := attributes["interface"]; exists {
				t.Fatal("invented an event interface")
			}
			if _, exists := attributes["handlers"]; exists {
				t.Fatal("exported a non-event handler")
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encoded, []byte("private-canary")) {
				t.Fatal("incomplete inventory bypassed privacy projection")
			}
			unchanged, err := json.Marshal(component)
			if err != nil || !bytes.Equal(original, unchanged) {
				t.Fatal("projection changed original inventory")
			}
			snapshotAfter, err := json.Marshal(snapshot)
			if err != nil || !bytes.Equal(snapshotBefore, snapshotAfter) {
				t.Fatal("projection changed the supplied snapshot")
			}

			// A valid functional observation still requires matching addressable
			// inventory, even if its public component ID happens to be event0.
			claimed := appTouchscreenPayload(t)
			if err := ValidateCoverage(claimed); err != nil {
				t.Fatal("positive evidence prerequisite", err)
			}
			claimed.Components[0].Attributes = record.Attributes
			if ValidateCoverage(claimed) == nil {
				t.Fatal("unaddressable inventory authenticated functional evidence")
			}
			if view, err := ValidatedTouchscreen(claimed); err == nil || view != nil {
				t.Fatal("unaddressable inventory produced a functional view")
			}

			// Missing information is different from a supplied invalid name.
			invalid := component
			invalid.Attributes = append([]inventory.Attribute(nil), component.Attributes...)
			appTouchscreenSetComponent(&invalid, "interface", "")
			// NewSnapshot removes empty values; retain this malformed attribute
			// directly so public projection, rather than normalization, rejects it.
			malformed := inventory.Snapshot{SchemaVersion: inventory.SnapshotSchemaVersion, Components: []inventory.Component{invalid}}
			emptyInterfaces := 0
			for _, attribute := range malformed.Components[0].Attributes {
				if attribute.Name == "interface" {
					if attribute.Value != "" {
						t.Fatal("malformed fixture lost its empty interface")
					}
					emptyInterfaces++
				}
			}
			if emptyInterfaces != 1 {
				t.Fatal("malformed fixture must retain exactly one empty interface")
			}
			rejected, err := BuildInventoryPayload(malformed, bytes.Repeat([]byte{0x31}, 32), "fixture")
			if err == nil || !reflect.DeepEqual(rejected, InventoryPayload{}) {
				t.Fatal("present empty interface produced a public payload")
			}
		})
	}
}

func TestTouchscreenPublicProjectionCanonicalizesEquivalentBitmaps(t *testing.T) {
	for _, name := range []string{"width32", "width64", "padded_key"} {
		t.Run(name, func(t *testing.T) {
			c := appTouchscreenComponent(t)
			appTouchscreenSetComponent(&c, "prop_capabilities", "2")
			appTouchscreenSetComponent(&c, "absolute_capabilities", "3")
			switch name {
			case "width32":
				appTouchscreenSetComponent(&c, "bitmap_word_bits", "32")
				appTouchscreenSetComponent(&c, "properties", "0 2")
				appTouchscreenSetComponent(&c, "abs_capabilities", "0 3")
				appTouchscreenSetComponent(&c, "key_capabilities", "400 0 0 0 0 0 0 0 0 0 0")
			case "width64":
				appTouchscreenSetComponent(&c, "properties", "0002")
				appTouchscreenSetComponent(&c, "abs_capabilities", "0003")
			case "padded_key":
				appTouchscreenSetComponent(&c, "key_capabilities", "0 400 0 0 0 0 0")
			}
			// The binding independently interprets the full width-aware bitmap.
			before := appTouchscreenPayload(t)
			before.Components[0].Attributes = append([]inventory.Attribute(nil), c.Attributes...)
			if err := ValidateCoverage(before); err != nil {
				t.Fatal("equivalent-set binding prerequisite", err)
			}
			projected, err := BuildInventoryPayload(inventory.NewSnapshot([]inventory.Component{c}), bytes.Repeat([]byte{0x31}, 32), "fixture")
			if err != nil || len(projected.Components) != 1 {
				t.Fatal("equivalent aliases rejected", err)
			}
			got := map[string]string{}
			for _, a := range projected.Components[0].Attributes {
				got[a.Name] = a.Value
			}
			width := "64"
			if name == "width32" {
				width = "32"
			}
			if got["bitmap_word_bits"] != width || got["properties"] != "2" || got["prop_capabilities"] != "2" ||
				got["absolute_capabilities"] != "3" || got["abs_capabilities"] != "3" {
				t.Fatal("projection changed the observed width or bit set")
			}
			before.Components[0].Attributes = projected.Components[0].Attributes
			if err := ValidateCoverage(before); err != nil {
				t.Fatal("canonical projection lost functional binding", err)
			}
		})
	}
}

func TestTouchscreenPublicProjectionValidatesPaddingBeforeCanonicalization(t *testing.T) {
	for _, name := range []string{"conflicting_alias", "invalid_padded_word", "excess_words", "high_bits", "unknown_width"} {
		t.Run(name, func(t *testing.T) {
			c := appTouchscreenComponent(t)
			appTouchscreenSetComponent(&c, "bitmap_word_bits", "32")
			appTouchscreenSetComponent(&c, "properties", "0 2")
			appTouchscreenSetComponent(&c, "prop_capabilities", "2")
			appTouchscreenSetComponent(&c, "key_capabilities", "400 0 0 0 0 0 0 0 0 0 0")
			if _, candidate, err := touchscreenPublicAttributes(c); err != nil || !candidate {
				t.Fatal("valid alias prerequisite", err)
			}
			switch name {
			case "conflicting_alias":
				appTouchscreenSetComponent(&c, "prop_capabilities", "3")
			case "invalid_padded_word":
				appTouchscreenSetComponent(&c, "properties", "unsafe 2")
			case "excess_words":
				appTouchscreenSetComponent(&c, "properties", "0 0 2")
			case "high_bits":
				appTouchscreenSetComponent(&c, "properties", "100000000 2")
			case "unknown_width":
				appTouchscreenSetComponent(&c, "bitmap_word_bits", "128")
				appTouchscreenSetComponent(&c, "category", "input")
			}
			p, err := BuildInventoryPayload(inventory.NewSnapshot([]inventory.Component{c}), bytes.Repeat([]byte{0x31}, 32), "fixture")
			if err == nil || !reflect.DeepEqual(p, InventoryPayload{}) {
				t.Fatal("invalid width or alias produced a partial payload")
			}
		})
	}
}

func TestTouchscreenPublicProjectionRetainsIndependentCandidateMarkers(t *testing.T) {
	for _, name := range []string{"direct_category", "direct_input", "direct_pointer", "misleading_touchpad", "nondirect_touchpad", "spoofed_touchpad_marker", "duplicate_width"} {
		t.Run(name, func(t *testing.T) {
			c := appTouchscreenComponent(t)
			appTouchscreenSetComponent(&c, "name", "Fixture Panel")
			appTouchscreenSetComponent(&c, "model", "Model A")
			wantTouchscreen, wantTouchpad, wantError := true, false, false
			switch name {
			case "direct_input":
				appTouchscreenSetComponent(&c, "category", "input")
				wantTouchpad = true
			case "direct_pointer":
				appTouchscreenSetComponent(&c, "category", "pointer")
				wantTouchpad = true
			case "misleading_touchpad":
				appTouchscreenSetComponent(&c, "category", "touchpad")
				wantTouchpad = true
			case "nondirect_touchpad":
				appTouchscreenSetComponent(&c, "category", "touchpad")
				appTouchscreenSetComponent(&c, "properties", "1")
				appTouchscreenDeleteComponent(&c, "touchscreen_candidate")
				wantTouchscreen, wantTouchpad = false, true
			case "spoofed_touchpad_marker":
				appTouchscreenSetComponent(&c, "category", "touchpad")
				appTouchscreenSetComponent(&c, "touchpad_candidate", "false")
				wantError = true
			case "duplicate_width":
				c.Attributes = append(c.Attributes, inventory.Attribute{Name: "bitmap_word_bits", Value: "32"})
				wantError = true
			}
			original, _ := json.Marshal(c)
			snapshot := inventory.Snapshot{SchemaVersion: inventory.SnapshotSchemaVersion, Components: []inventory.Component{c}}
			p, err := BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x31}, 32), "fixture")
			if wantError {
				if err == nil || !reflect.DeepEqual(p, InventoryPayload{}) {
					t.Fatal("conflicting marker/width escaped")
				}
				return
			}
			if err != nil || len(p.Components) != 1 {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, a := range p.Components[0].Attributes {
				got[a.Name] = a.Value
			}
			if (got["touchscreen_candidate"] == "true") != wantTouchscreen ||
				(got["touchpad_candidate"] == "true") != wantTouchpad {
				t.Fatal("one functional classifier erased or fabricated the other")
			}
			if wantTouchpad {
				if _, exists := got["name"]; exists {
					t.Fatal("touchpad or dual marker exported a display name")
				}
				if _, exists := got["model"]; exists {
					t.Fatal("touchpad or dual marker exported a model")
				}
			} else if got["name"] != "Fixture Panel" || got["model"] != "Model A" {
				t.Fatal("touchscreen-only classification lost useful labels")
			}
			after, _ := json.Marshal(c)
			if !bytes.Equal(original, after) {
				t.Fatal("projection changed caller inventory")
			}
		})
	}
}

// Literal orders remain independent of the current catalog and selector aliases.
func appTouchscreenJournalPayload(t *testing.T, standard, observed bool) InventoryPayload {
	t.Helper()
	p := appTouchscreenPayload(t)
	touchscreen, integrity := p.TestResults[0], p.TestResults[1]
	order := "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
	version := "11"
	if standard {
		order = "cpu.quick cpu.stress memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
		version = "12"
	}
	p.TestResults = nil
	var workers []string
	for _, id := range strings.Fields(order) {
		workers = append(workers, fmt.Sprintf("{\"id\":%q,\"state\":\"not_scheduled\",\"outcome\":\"excluded\"}", id))
		row := TestResult{ID: id, Version: 1, Status: TestSkip, PlanID: touchscreen.PlanID, HardwareGraph: touchscreen.HardwareGraph, PlannedStatus: PlannedExcluded,
			Evidence: []byte("{\"schema\":\"hhsoft.pc-tester.execution-note/v1\",\"status\":\"skip\",\"source_status\":\"excluded\",\"detail\":\"not run; disposition recorded in the frozen plan\"}")}
		if id == "cpu.quick" || id == "memory.quick" || id == "input.keyboard.quick" || id == "input.pointer.quick" ||
			id == "input.touchpad.quick" || id == "input.touchscreen.quick" {
			row.Version = 2
		}
		if id == "input.touchscreen.quick" && observed {
			row = touchscreen
		}
		p.TestResults = append(p.TestResults, row)
	}
	// Retained monitoring bytes are shared test data; the worker oracle is above.
	raw := "{\"schema\":\"hhsoft.pc-tester.quick-session/v" + version + appTouchpadJournalPrefix + strings.Join(workers, ",") + appTouchpadJournalTail
	if standard {
		raw += ",\"profile\":\"standard\",\"cpu_stress_source\":null"
	}
	journal, err := sessionevidence.Decode([]byte(raw + "}"))
	if err != nil {
		t.Fatal("literal new journal", err)
	}
	if observed {
		for i := range journal.Workers {
			if journal.Workers[i].ID == "input.touchscreen.quick" {
				journal.Workers[i].State = "completed"
				journal.Workers[i].Outcome = "pass"
				journal.Workers[i].Timing = &sessionevidence.WorkerTiming{StartedMilliseconds: 10, FinishedMilliseconds: 1010}
			}
		}
		before := journal.Safety[0]
		before.Phase = "before-input.touchscreen.quick"
		journal.Safety = append(journal.Safety[:1:1], append([]sessionevidence.SafetyRecord{before}, journal.Safety[1:]...)...)
	}
	encoded, err := journal.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(integrity.Evidence, &value) != nil {
		t.Fatal("integrity fixture")
	}
	value["session"] = encoded
	integrity.Evidence, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	p.TestResults = append(p.TestResults, integrity)
	return p
}

func TestTouchscreenAppVersionedSessionsPreserveHistoricalReports(t *testing.T) {
	for _, name := range []string{"v4", "v5", "v6", "v7", "v8", "v9", "v10", "v11_observed", "v12_observed", "v11_note", "v12_note"} {
		t.Run(name, func(t *testing.T) {
			var p InventoryPayload
			observed := name == "v11_observed" || name == "v12_observed"
			switch name {
			case "v11_observed", "v11_note":
				p = appTouchscreenJournalPayload(t, false, observed)
			case "v12_observed", "v12_note":
				p = appTouchscreenJournalPayload(t, true, observed)
			default:
				versions := map[string]int{"v4": 4, "v5": 5, "v6": 6, "v7": 7, "v8": 8, "v9": 9, "v10": 10}
				p = appTouchpadJournalPayload(t, versions[name], false)
				row := appTouchscreenPayload(t).TestResults[0]
				row.Version, row.Status, row.PlannedStatus = 1, TestSkip, PlannedUnsupported
				row.Evidence = []byte("{\"reason\":\"historical unsupported touchscreen definition\"}")
				p.TestResults = append(p.TestResults, row)
			}
			before, _ := json.Marshal(p)
			if err := ValidateCoverage(p); err != nil {
				t.Fatal("literal session rejected", err)
			}
			view, err := ValidatedTouchscreen(p)
			if err != nil || (view != nil) != observed {
				t.Fatal("current observation or historical exception lost", err)
			}
			thermal, err := ValidatedSessionThermal(p)
			if err != nil || thermal == nil {
				t.Fatal("strict monitoring lost", err)
			}
			if observed {
				view.Devices[0].Capabilities.Axes[0].Maximum = 99
			}
			after, _ := json.Marshal(p)
			if !bytes.Equal(before, after) {
				t.Fatal("decoded consumer view aliases signed source")
			}
		})
	}
}

func TestTouchscreenAppSessionRejectsForgedIntervalsAndSources(t *testing.T) {
	for _, name := range []string{"old_session", "future_session", "short_interval", "missing_interval", "wrong_outcome", "wrong_order", "monitor_cleanup", "thermal_cleanup", "wrong_placement", "duplicate_worker"} {
		t.Run(name, func(t *testing.T) {
			p := appTouchscreenJournalPayload(t, false, true)
			if err := ValidateCoverage(p); err != nil {
				t.Fatal("positive current-session prerequisite", err)
			}
			row := &p.TestResults[len(p.TestResults)-1]
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "old_session", "future_session":
				oldSchema := []byte(`"schema":"hhsoft.pc-tester.quick-session/v11"`)
				newSchema := []byte(`"schema":"hhsoft.pc-tester.quick-session/v9"`)
				if name == "future_session" {
					newSchema = []byte(`"schema":"hhsoft.pc-tester.quick-session/v13"`)
				}
				if bytes.Count(row.Evidence, oldSchema) != 1 {
					t.Fatal("wire mutation must target one session schema")
				}
				original := append([]byte(nil), row.Evidence...)
				row.Evidence = bytes.Replace(row.Evidence, oldSchema, newSchema, 1)
				if !json.Valid(row.Evidence) || !bytes.Equal(bytes.Replace(row.Evidence, newSchema, oldSchema, 1), original) {
					t.Fatal("wire mutation changed unrelated evidence")
				}
			default:
				switch name {
				case "short_interval":
					value.Session.Workers[7].Timing.FinishedMilliseconds = 1009
				case "missing_interval":
					value.Session.Workers[7].Timing = nil
				case "wrong_outcome":
					value.Session.Workers[7].Outcome = "inconclusive"
				case "wrong_order":
					value.Session.Workers[6], value.Session.Workers[7] = value.Session.Workers[7], value.Session.Workers[6]
				case "monitor_cleanup":
					value.Session.Monitor.CleanupConfirmed = false
				case "thermal_cleanup":
					value.Session.Thermal.CleanupConfirmed = false
				case "wrong_placement":
					p.TestResults[0].Evidence, err = value.Session.MarshalJSON()
					if err != nil {
						t.Fatal(err)
					}
				case "duplicate_worker":
					value.Session.Workers = append(value.Session.Workers, value.Session.Workers[7])
				}
				row.Evidence, err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
			}
			if ValidateCoverage(p) == nil {
				t.Fatal("forged current journal accepted")
			}
			if view, err := ValidatedTouchscreen(p); err == nil || view != nil {
				t.Fatal("forged journal produced observation")
			}
			if _, err := ValidatedSessionThermal(p); err == nil {
				t.Fatal("thermal entry bypassed touchscreen validation")
			}
		})
	}
}
