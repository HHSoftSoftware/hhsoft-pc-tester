package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchpadevidence"
	"reflect"
	"strings"
	"testing"
)

// This independent literal describes a bounded synthetic report, not device qualification.
const appTouchpadLiteral = `{"schema_version":1,"components":[{"type":"input","id":"event0","attributes":[{"name":"abs_capabilities","value":"3"},{"name":"bitmap_word_bits","value":"64"},{"name":"bus","value":"0003"},{"name":"category","value":"touchpad"},{"name":"event_capabilities","value":"b"},{"name":"handlers","value":"event0"},{"name":"interface","value":"event0"},{"name":"key_capabilities","value":"420 10000 0 0 0 0"},{"name":"msc_capabilities","value":"0"},{"name":"product_id","value":"abcd"},{"name":"properties","value":"1"},{"name":"relative_capabilities","value":"0"},{"name":"touchpad_candidate","value":"true"},{"name":"vendor_id","value":"1234"},{"name":"version","value":"0001"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"fixture","confidence":"unavailable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"fixture","key_id":"hmac-sha256:3333333333333333333333333333333333333333333333333333333333333333","identified_parts":0,"unidentified_parts":1},"test_results":[{"id":"input.touchpad.quick","version":2,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.touchpad-functional/v1","status":"pass","stop_reason":"completed","limit_ms":30000,"duration_ms":1000,"cleanup_confirmed":true,"devices":[{"interface":"event0","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":4660,"product":43981,"version":1},"capabilities":{"protocol_version":65537,"events":[0,1,3],"keys":[272,325,330],"relative":[],"properties":[0],"misc":[],"axes":[{"code":0,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1},{"code":1,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1}]},"x_threshold":50,"y_threshold":50,"frames":6,"contact_starts":1,"contact_releases":1,"xy_baselines":1,"qualifying_contacts":1,"x_positive":2,"x_negative":2,"y_positive":2,"y_negative":2,"buttons":[{"button":"left","presses":1,"releases":1,"pairs":1}],"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]},"planned_status":"scheduled"},{"id":"run.integrity.quick","version":1,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.run-integrity/v1","status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","planned_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","final_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","planned_component_count":1,"final_component_count":1}}],"coverage_scope":"frozen_plan"}`

func appTouchpadPayload(t *testing.T) InventoryPayload {
	t.Helper()
	var value InventoryPayload
	if err := json.Unmarshal([]byte(appTouchpadLiteral), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func appTouchpadChange(t *testing.T, payload *InventoryPayload, change func(*inputtest.TouchpadResult)) {
	t.Helper()
	var value inputtest.TouchpadResult
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

func appTouchpadIntegrity(t *testing.T, payload *InventoryPayload, change func(*sessionevidence.IntegrityEvidence)) {
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

func appTouchpadAttribute(payload *InventoryPayload, name, value string) {
	attrs := payload.Components[0].Attributes
	for i := range attrs {
		if attrs[i].Name == name {
			attrs[i].Value = value
			return
		}
	}
	payload.Components[0].Attributes = append(attrs, inventory.Attribute{Name: name, Value: value})
}

func appTouchpadDeleteAttribute(payload *InventoryPayload, name string) {
	attrs := payload.Components[0].Attributes
	for i := range attrs {
		if attrs[i].Name == name {
			payload.Components[0].Attributes = append(attrs[:i:i], attrs[i+1:]...)
			return
		}
	}
	panic("literal fixture attribute absent")
}

func appTouchpadResetState(d inputtest.TouchpadDeviceResult) inputtest.TouchpadDeviceResult {
	return inputtest.TouchpadDeviceResult{Interface: d.Interface, Status: inputtest.StatusInconclusive,
		Readable: d.Readable, Supported: d.Supported, Identity: d.Identity, Capabilities: d.Capabilities,
		Buttons: []inputtest.PointerButtonResult{}, CleanupConfirmed: true}
}

func TestTouchpadAppLiteralReportsAndOwnedObservation(t *testing.T) {
	for _, name := range []string{"standalone", "aborted_retains_pass", "deadline_retains_pass", "prestate_layout", "unsupported_unknown_axis", "unreadable", "no_touchpads"} {
		t.Run(name, func(t *testing.T) {
			payload := appTouchpadPayload(t)
			appTouchpadChange(t, &payload, func(v *inputtest.TouchpadResult) {
				switch name {
				case "aborted_retains_pass":
					v.Status, v.StopReason, v.Detail = inputtest.StatusAborted, "operator_cancelled", "operator_cancelled"
				case "deadline_retains_pass":
					v.Status, v.StopReason, v.Detail = inputtest.StatusError, "deadline", "deadline"
					v.DurationMilliseconds = 30001
				case "prestate_layout":
					v.Devices[0] = appTouchpadResetState(v.Devices[0])
					v.Devices[0].Detail = "unsupported_record_layout"
					v.Status, v.StopReason, v.Detail = inputtest.StatusError, "error", "unsupported_record_layout"
				case "unsupported_unknown_axis":
					d := appTouchpadResetState(v.Devices[0])
					d.Capabilities.Axes = append(d.Capabilities.Axes, inputtest.TouchpadAxis{Code: 2})
					d.Supported, d.Detail = false, "unsupported_capabilities"
					v.Devices[0] = d
					v.Status, v.StopReason, v.Detail = inputtest.StatusInconclusive, "error", "unsupported_or_unreadable_device"
				case "unreadable":
					d := appTouchpadResetState(v.Devices[0])
					d.Identity, d.Capabilities = nil, nil
					d.Readable, d.Supported, d.Detail = false, false, "identity_unavailable"
					v.Devices[0] = d
					v.Status, v.StopReason, v.Detail = inputtest.StatusInconclusive, "error", "unsupported_or_unreadable_device"
				case "no_touchpads":
					v.Devices = []inputtest.TouchpadDeviceResult{}
					v.Status, v.StopReason, v.Detail = inputtest.StatusInconclusive, "no_touchpads", "no_touchpads"
				}
			})
			if name == "no_touchpads" {
				payload.Components = nil
				payload.SystemFingerprint.UnidentifiedParts = 0
				appTouchpadIntegrity(t, &payload, func(v *sessionevidence.IntegrityEvidence) { v.PlannedComponentCount = 0; v.FinalComponentCount = 0 })
			}
			original, _ := json.Marshal(payload)
			value, err := ValidatedTouchpad(payload)
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
				if len(value.Devices[0].Buttons) != 0 {
					value.Devices[0].Buttons[0].Pairs = 99
				}
			}
			after, _ := json.Marshal(payload)
			if !bytes.Equal(original, after) {
				t.Fatal("returned nested observation aliases the signed payload")
			}
			again, err := ValidatedTouchpad(payload)
			if err != nil || again == nil || len(again.Devices) != 0 && again.Devices[0].Interface != "event0" {
				t.Fatal("returned observation was reused")
			}
		})
	}
}

func TestTouchpadAppRejectsContradictoryRows(t *testing.T) {
	cases := []struct {
		name   string
		change func(*InventoryPayload)
	}{
		{"definition_one", func(p *InventoryPayload) { p.TestResults[0].Version = 1 }},
		{"definition_zero", func(p *InventoryPayload) { p.TestResults[0].Version = 0 }},
		{"future_definition", func(p *InventoryPayload) { p.TestResults[0].Version = 99 }},
		{"id_alias", func(p *InventoryPayload) { p.TestResults[0].ID = "INPUT.TOUCHPAD.QUICK" }},
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
			appTouchpadChange(t, p, func(v *inputtest.TouchpadResult) { v.LimitMilliseconds = 30001 })
		}},
		{"missing_final_proof", func(p *InventoryPayload) {
			appTouchpadChange(t, p, func(v *inputtest.TouchpadResult) { v.Devices[0].FinalNeutral = false })
		}},
		{"same_contact_overflow", func(p *InventoryPayload) {
			appTouchpadChange(t, p, func(v *inputtest.TouchpadResult) { v.Devices[0].ContactStarts = 4; v.Devices[0].ContactReleases = 4 })
		}},
		{"axis_frame_budget", func(p *InventoryPayload) {
			appTouchpadChange(t, p, func(v *inputtest.TouchpadResult) { v.Devices[0].XPositive = 3; v.Devices[0].XNegative = 3 })
		}},
		{"public_motion_without_release", func(p *InventoryPayload) {
			appTouchpadChange(t, p, func(v *inputtest.TouchpadResult) { v.Devices[0].ContactReleases = 0 })
		}},
	}
	if _, err := ValidatedTouchpad(appTouchpadPayload(t)); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := appTouchpadPayload(t)
			c.change(&p)
			if ValidateCoverage(p) == nil {
				t.Fatal("contradictory report accepted")
			}
			if v, err := ValidatedTouchpad(p); err == nil || v != nil {
				t.Fatal("invalid observation escaped")
			}
		})
	}
}

func TestTouchpadAppCleanupUncertaintyIsFatal(t *testing.T) {
	for _, name := range []string{"coverage", "touchpad_view", "thermal_view"} {
		t.Run(name, func(t *testing.T) {
			p := appTouchpadPayload(t)
			appTouchpadChange(t, &p, func(v *inputtest.TouchpadResult) {
				v.Status = inputtest.StatusError
				v.StopReason = "cleanup_unconfirmed"
				v.Detail = "cleanup_unconfirmed"
				v.CleanupConfirmed = false
			})
			var err error
			switch name {
			case "coverage":
				err = ValidateCoverage(p)
			case "touchpad_view":
				_, err = ValidatedTouchpad(p)
			case "thermal_view":
				_, err = ValidatedSessionThermal(p)
			}
			if !errors.Is(err, touchpadevidence.ErrCleanupUnconfirmed) {
				t.Fatalf("cleanup uncertainty lost: %v", err)
			}
		})
	}
}

func TestTouchpadAppFinalCandidateBindings(t *testing.T) {
	cases := []struct {
		name   string
		change func(*InventoryPayload)
	}{
		{"missing_candidate", func(p *InventoryPayload) { p.Components = nil }},
		{"missing_marker", func(p *InventoryPayload) { appTouchpadDeleteAttribute(p, "touchpad_candidate") }},
		{"marker_false", func(p *InventoryPayload) { appTouchpadAttribute(p, "touchpad_candidate", "false") }},
		{"duplicate_component", func(p *InventoryPayload) { p.Components = append(p.Components, p.Components[0]) }},
		{"duplicate_attribute", func(p *InventoryPayload) {
			p.Components[0].Attributes = append(p.Components[0].Attributes, p.Components[0].Attributes[0])
		}},
		{"missing_width", func(p *InventoryPayload) { appTouchpadDeleteAttribute(p, "bitmap_word_bits") }},
		{"unknown_width", func(p *InventoryPayload) { appTouchpadAttribute(p, "bitmap_word_bits", "128") }},
		{"width_reinterpretation", func(p *InventoryPayload) { appTouchpadAttribute(p, "bitmap_word_bits", "32") }},
		{"vendor_mismatch", func(p *InventoryPayload) { appTouchpadAttribute(p, "vendor_id", "0000") }},
		{"vendor_alias_conflict", func(p *InventoryPayload) { appTouchpadAttribute(p, "vendor", "0000") }},
		{"absolute_alias_conflict", func(p *InventoryPayload) { appTouchpadAttribute(p, "absolute_capabilities", "7") }},
		{"hidden_key", func(p *InventoryPayload) { appTouchpadAttribute(p, "key_capabilities", "420 10001 0 0 0 0") }},
		{"hidden_relative", func(p *InventoryPayload) { appTouchpadAttribute(p, "relative_capabilities", "1") }},
		{"missing_msc", func(p *InventoryPayload) { appTouchpadDeleteAttribute(p, "msc_capabilities") }},
		{"hidden_msc", func(p *InventoryPayload) { appTouchpadAttribute(p, "msc_capabilities", "10") }},
		{"handler_conflict", func(p *InventoryPayload) { appTouchpadAttribute(p, "handlers", "event1") }},
		{"handler_duplicate", func(p *InventoryPayload) { appTouchpadAttribute(p, "handlers", "event0 event0") }},
		{"noncanonical_interface", func(p *InventoryPayload) { appTouchpadAttribute(p, "interface", "event00") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := appTouchpadPayload(t)
			c.change(&p)
			if ValidateCoverage(p) == nil {
				t.Fatal("unbound candidate accepted")
			}
			appTouchpadChange(t, &p, func(v *inputtest.TouchpadResult) {
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

func appTouchpadChangedInventory(t *testing.T) InventoryPayload {
	t.Helper()
	p := appTouchpadPayload(t)
	p.Components = append(p.Components,
		ComponentRecord{Type: inventory.ComponentCPU, ID: "new", Identity: p.Components[0].Identity},
		ComponentRecord{Type: inventory.ComponentMemory, ID: "kept", Identity: p.Components[0].Identity})
	p.SystemFingerprint.UnidentifiedParts = 3
	appTouchpadIntegrity(t, &p, func(v *sessionevidence.IntegrityEvidence) {
		v.Status = "fail"
		v.FinalHardwareGraph = "hmac-sha256:" + strings.Repeat("4", 64)
		v.PlannedComponentCount = 3
		v.FinalComponentCount = 3
		v.AddedComponents = []plan.HardwareComponentRef{{Type: inventory.ComponentCPU, ID: "new"}}
		v.RemovedComponents = []plan.HardwareComponentRef{{Type: inventory.ComponentCPU, ID: "old"}}
		v.ChangedComponents = []plan.HardwareComponentRef{{Type: inventory.ComponentMemory, ID: "kept"}}
	})
	return p
}

func TestTouchpadAppIntegrityDiffSetsAreStructural(t *testing.T) {
	for _, name := range []string{"unchanged", "changed_unrelated", "two_added_two_removed"} {
		t.Run(name, func(t *testing.T) {
			p := appTouchpadPayload(t)
			if name != "unchanged" {
				p = appTouchpadChangedInventory(t)
			}
			if name == "two_added_two_removed" {
				appTouchpadIntegrity(t, &p, func(v *sessionevidence.IntegrityEvidence) {
					v.AddedComponents = append(v.AddedComponents, plan.HardwareComponentRef{Type: inventory.ComponentMemory, ID: "kept"})
					v.RemovedComponents = append(v.RemovedComponents, plan.HardwareComponentRef{Type: inventory.ComponentMemory, ID: "old"})
					v.ChangedComponents = nil
				})
			}
			if err := ValidateCoverage(p); err != nil {
				t.Fatalf("valid structural delta rejected: %v", err)
			}
		})
	}
	cases := []struct {
		name   string
		change func(*InventoryPayload, *sessionevidence.IntegrityEvidence)
	}{
		{"duplicate_added", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.AddedComponents = append(v.AddedComponents, v.AddedComponents[0])
			v.RemovedComponents = append(v.RemovedComponents, plan.HardwareComponentRef{Type: inventory.ComponentMemory, ID: "old"})
			v.ChangedComponents = nil
		}},
		{"duplicate_removed", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.AddedComponents = append(v.AddedComponents, plan.HardwareComponentRef{Type: inventory.ComponentMemory, ID: "kept"})
			v.RemovedComponents = append(v.RemovedComponents, v.RemovedComponents[0])
			v.ChangedComponents = nil
		}},
		{"root_repeated_two_sets", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.AddedComponents = append(v.AddedComponents, v.AddedComponents[0])
			v.RemovedComponents = append(v.RemovedComponents, v.RemovedComponents[0])
			v.ChangedComponents = nil
		}},
		{"duplicate_changed", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.ChangedComponents = append(v.ChangedComponents, v.ChangedComponents[0])
		}},
		{"empty_added", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) { v.AddedComponents[0].ID = "" }},
		{"empty_removed", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) { v.RemovedComponents[0].ID = "" }},
		{"empty_changed", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) { v.ChangedComponents[0].ID = "" }},
		{"whitespace_id", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) { v.RemovedComponents[0].ID = " " }},
		{"empty_type", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) { v.RemovedComponents[0].Type = "" }},
		{"added_changed_overlap", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.ChangedComponents[0] = v.AddedComponents[0]
		}},
		{"added_removed_overlap", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.RemovedComponents[0] = v.AddedComponents[0]
		}},
		{"removed_changed_overlap", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.RemovedComponents[0] = v.ChangedComponents[0]
		}},
		{"absent_added", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) { v.AddedComponents[0].ID = "missing" }},
		{"absent_changed", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) { v.ChangedComponents[0].ID = "missing" }},
		{"removed_still_present", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.RemovedComponents[0] = plan.HardwareComponentRef{Type: inventory.ComponentInput, ID: "event0"}
		}},
		{"changed_retained_pass", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.ChangedComponents[0] = plan.HardwareComponentRef{Type: inventory.ComponentInput, ID: "event0"}
		}},
		{"added_retained_pass", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.AddedComponents[0] = plan.HardwareComponentRef{Type: inventory.ComponentInput, ID: "event0"}
		}},
		{"duplicate_final_key", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) { p.Components[2] = p.Components[1] }},
		{"final_count_mismatch", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.PlannedComponentCount = 4
			v.FinalComponentCount = 4
		}},
		{"equal_graph_with_delta", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.FinalHardwareGraph = v.PlannedHardwareGraph
		}},
		{"unequal_graph_without_delta", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.AddedComponents = nil
			v.RemovedComponents = nil
			v.ChangedComponents = nil
		}},
		{"count_arithmetic", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.PlannedComponentCount = int(^uint(0) >> 1)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := appTouchpadChangedInventory(t)
			appTouchpadIntegrity(t, &p, func(v *sessionevidence.IntegrityEvidence) { c.change(&p, v) })
			if ValidateCoverage(p) == nil {
				t.Fatal("impossible integrity set accepted")
			}
		})
	}
}

func TestTouchpadAppDefinitionSelectionDoesNotLoseClaims(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"family", `{"schema":"hhsoft.pc-tester.touchpad-functional/v1"}`},
		{"future_family", `{"schema":"hhsoft.pc-tester.touchpad-functional/v999"}`},
		{"unversioned_family", `{"schema":"hhsoft.pc-tester.touchpad-functional"}`},
		{"escaped_family", `{"schema":"hhsoft.pc-tester.touchpad-\u0066unctional/v1"}`},
		{"definition_two", `{"id":"input.touchpad.quick","version":2}`},
		{"future_definition", `{"id":"input.touchpad.quick","version":99}`},
		{"zero_definition", `{"id":"input.touchpad.quick","version":0}`},
		{"missing_version_row", `{"test_results":[{"id":"input.touchpad.quick"}]}`},
		{"missing_version_status", `{"id":"input.touchpad.quick","status":"pass"}`},
		{"missing_version_evidence", `{"id":"input.touchpad.quick","evidence":{}}`},
		{"string_version", `{"id":"input.touchpad.quick","version":"1"}`},
		{"number_spelling", `{"id":"input.touchpad.quick","version":1.0}`},
		{"case_id", `{"ID":"INPUT.TOUCHPAD.QUICK","version":1}`},
		{"case_version", `{"id":"input.touchpad.quick","VERSION":1}`},
		{"earlier_version", `{"id":"input.touchpad.quick","version":2,"version":1}`},
		{"earlier_id", `{"id":"input.touchpad.quick","id":"other","version":2}`},
		{"hidden_results", `{"test_results":[{"id":"input.touchpad.quick","version":2}],"test_results":[]}`},
		{"null_version", `{"id":"input.touchpad.quick","version":null}`},
		{"truncated", `{"id":"input.touchpad.quick","version":`},
		{"trailing", `{} {}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "escaped_family" && !strings.Contains(c.raw, `\u0066`) {
				t.Fatal("escape fixture lost its literal escape")
			}
			if !HasTouchpadClaim([]byte(c.raw)) {
				t.Fatal("strict selector lost a claim")
			}
		})
	}
	for _, name := range []string{"historical_one", "id_only", "worker_reference", "unrelated", "empty"} {
		t.Run(name, func(t *testing.T) {
			raw := ""
			switch name {
			case "historical_one":
				raw = `{"id":"input.touchpad.quick","version":1,"status":"skip","evidence":{"reason":"unsupported"}}`
			case "id_only":
				raw = `{"id":"input.touchpad.quick"}`
			case "worker_reference":
				raw = `{"id":"input.touchpad.quick","state":"not_scheduled","outcome":"excluded"}`
			case "unrelated":
				raw = `{"id":"input.pointer.quick","version":2}`
			}
			if HasTouchpadClaim([]byte(raw)) {
				t.Fatal("explicit historical/reference exception was lost")
			}
		})
	}
}

func TestTouchpadAppCandidateSetIsPhaseAware(t *testing.T) {
	for _, name := range []string{"pass_requires_all", "cancel_retains_bound_pass"} {
		t.Run(name, func(t *testing.T) {
			p := appTouchpadPayload(t)
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
			appTouchpadIntegrity(t, &p, func(v *sessionevidence.IntegrityEvidence) { v.PlannedComponentCount = 2; v.FinalComponentCount = 2 })
			if name == "cancel_retains_bound_pass" {
				appTouchpadChange(t, &p, func(v *inputtest.TouchpadResult) {
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

func appTouchpadComponent(t *testing.T) inventory.Component {
	t.Helper()
	p := appTouchpadPayload(t)
	return inventory.Component{Type: inventory.ComponentInput, ID: "event0", Attributes: append([]inventory.Attribute(nil), p.Components[0].Attributes...)}
}

func appTouchpadSetComponent(c *inventory.Component, name, value string) {
	for i := range c.Attributes {
		if c.Attributes[i].Name == name {
			c.Attributes[i].Value = value
			return
		}
	}
	c.Attributes = append(c.Attributes, inventory.Attribute{Name: name, Value: value})
}

func appTouchpadDeleteComponent(c *inventory.Component, name string) {
	for i := range c.Attributes {
		if c.Attributes[i].Name == name {
			c.Attributes = append(c.Attributes[:i:i], c.Attributes[i+1:]...)
			return
		}
	}
	panic("component fixture attribute absent")
}

func TestTouchpadPublicProjectionIsFiniteAndOwned(t *testing.T) {
	for _, name := range []string{"touchpad", "ambiguous_input", "ambiguous_pointer", "derived_marker", "canonical_aliases", "unknown_width", "unknown_abs_marker"} {
		t.Run(name, func(t *testing.T) {
			c := appTouchpadComponent(t)
			for _, field := range []string{"name", "model", "kernel_path", "unique_id", "coordinates", "msc_payload", "error"} {
				appTouchpadSetComponent(&c, field, "private-canary-"+field)
			}
			switch name {
			case "ambiguous_input":
				appTouchpadSetComponent(&c, "category", "input")
			case "ambiguous_pointer":
				appTouchpadSetComponent(&c, "category", "pointer")
			case "derived_marker":
				appTouchpadDeleteComponent(&c, "touchpad_candidate")
			case "canonical_aliases":
				appTouchpadSetComponent(&c, "vendor_id", "ABCD")
				appTouchpadSetComponent(&c, "vendor", "abcd")
				appTouchpadSetComponent(&c, "event_capabilities", "000B")
				appTouchpadSetComponent(&c, "ev_capabilities", "b")
			case "unknown_width":
				appTouchpadDeleteComponent(&c, "bitmap_word_bits")
			case "unknown_abs_marker":
				appTouchpadSetComponent(&c, "abs_capabilities", "7")
			}
			original, _ := json.Marshal(c)
			attrs, candidate, err := touchpadPublicAttributes(c)
			if err != nil || !candidate {
				t.Fatal("candidate lost", err)
			}
			got := map[string]string{}
			for _, v := range attrs {
				got[v.Name] = v.Value
			}
			if got["touchpad_candidate"] != "true" || got["interface"] != "event0" || got["handlers"] != "event0" {
				t.Fatal("missing finite candidate identity")
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

func TestTouchpadPublicProjectionRejectsAmbiguity(t *testing.T) {
	cases := []struct {
		name   string
		change func(*inventory.Component)
	}{
		{"duplicate_attribute", func(c *inventory.Component) { c.Attributes = append(c.Attributes, c.Attributes[0]) }},
		{"false_marker", func(c *inventory.Component) { appTouchpadSetComponent(c, "touchpad_candidate", "false") }},
		{"malformed_marker", func(c *inventory.Component) { appTouchpadSetComponent(c, "touchpad_candidate", "TRUE") }},
		{"identity_conflict", func(c *inventory.Component) { appTouchpadSetComponent(c, "vendor", "ffff") }},
		{"bitmap_conflict", func(c *inventory.Component) { appTouchpadSetComponent(c, "absolute_capabilities", "7") }},
		{"invalid_alias", func(c *inventory.Component) { appTouchpadSetComponent(c, "absolute_capabilities", "unsafe") }},
		{"handler_conflict", func(c *inventory.Component) { appTouchpadSetComponent(c, "handlers", "event1") }},
		{"handler_duplicate", func(c *inventory.Component) { appTouchpadSetComponent(c, "handlers", "event0 event0") }},
		{"interface_alias", func(c *inventory.Component) { appTouchpadSetComponent(c, "interface", "event00") }},
		{"empty_name", func(c *inventory.Component) {
			c.Attributes = append(c.Attributes, inventory.Attribute{Name: "", Value: "x"})
		}},
		{"too_many_attributes", func(c *inventory.Component) {
			for i := 0; i < 129; i++ {
				c.Attributes = append(c.Attributes, inventory.Attribute{Name: fmt.Sprintf("field%d", i), Value: "x"})
			}
		}},
		{"ambiguous_unsafe_abs", func(c *inventory.Component) {
			appTouchpadSetComponent(c, "category", "input")
			appTouchpadSetComponent(c, "abs_capabilities", "unsafe")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			component := appTouchpadComponent(t)
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

func TestTouchpadPublicProjectionKeepsUnknownsAndOriginalFingerprint(t *testing.T) {
	for _, name := range []string{"missing_msc", "unsafe_single_msc", "unknown_width", "unrelated_component", "original_hmac_owner"} {
		t.Run(name, func(t *testing.T) {
			c := appTouchpadComponent(t)
			switch name {
			case "missing_msc":
				appTouchpadDeleteComponent(&c, "msc_capabilities")
			case "unsafe_single_msc":
				appTouchpadSetComponent(&c, "msc_capabilities", "private-canary")
			case "unknown_width":
				appTouchpadSetComponent(&c, "bitmap_word_bits", "128")
			case "unrelated_component":
				appTouchpadSetComponent(&c, "category", "keyboard")
				appTouchpadDeleteComponent(&c, "touchpad_candidate")
				appTouchpadSetComponent(&c, "model", "retained model")
			}
			system := inventory.NewComponent(inventory.ComponentSystem, "system", map[string]string{"product_uuid": "11111111-2222-3333-4444-555555555555"})
			key := bytes.Repeat([]byte{0x42}, 32)
			p, err := BuildInventoryPayload(inventory.NewSnapshot([]inventory.Component{system, c}), key, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			systemIndex, inputIndex := -1, -1
			for i := range p.Components {
				switch p.Components[i].Type {
				case inventory.ComponentSystem:
					systemIndex = i
				case inventory.ComponentInput:
					inputIndex = i
				}
			}
			if systemIndex < 0 || inputIndex < 0 || p.Components[systemIndex].Identity.Value == "" || p.SystemFingerprint.IdentifiedParts != 1 {
				t.Fatal("original HMAC owner lost")
			}
			got := map[string]string{}
			for _, v := range p.Components[inputIndex].Attributes {
				got[v.Name] = v.Value
			}
			if name == "missing_msc" || name == "unsafe_single_msc" {
				if _, ok := got["msc_capabilities"]; ok {
					t.Fatal("unknown MSC became claimed zero")
				}
			}
			if name == "unknown_width" {
				if _, ok := got["bitmap_word_bits"]; ok {
					t.Fatal("unknown width upgraded")
				}
			}
			if name == "unrelated_component" && got["model"] != "retained model" {
				t.Fatal("unrelated historical projection changed")
			}
			appTouchpadSetComponent(&c, "unique_id", "different-private-canary")
			again, err := BuildInventoryPayload(inventory.NewSnapshot([]inventory.Component{system, c}), key, "fixture")
			if err != nil || again.SystemFingerprint != p.SystemFingerprint || again.Components[systemIndex].Identity != p.Components[systemIndex].Identity {
				t.Fatal("display projection replaced the original identity owner", err)
			}
		})
	}
}

func TestTouchpadAppFiniteDispositionNotes(t *testing.T) {
	for _, name := range []string{"excluded", "not_present", "unsupported", "missing_driver", "error", "aborted"} {
		t.Run(name, func(t *testing.T) {
			p := appTouchpadPayload(t)
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
			if v, err := ValidatedTouchpad(p); err != nil || v != nil {
				t.Fatal("note became an observation", err)
			}
			row.Evidence = bytes.Replace(row.Evidence, []byte(detail), []byte("arbitrary error text"), 1)
			if ValidateCoverage(p) == nil {
				t.Fatal("opaque diagnostic was accepted as a finite note")
			}
		})
	}
}

// Literal historical orders and monitoring intervals are fixture data, not catalog activation.
const appTouchpadJournalPrefix = `","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","stop_reason":"completed","budget_milliseconds":900000,"elapsed_milliseconds":600050,"workers":[`
const appTouchpadJournalTail = `],"safety":[{"phase":"preflight","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"final","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},{"phase":"pre-sign","result":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}}],"monitor":{"schema":"hhsoft.pc-tester.periodic-safety/v1","status":"pass","interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"attempted_observations":600,"completed_observations":600,"elapsed_ms":600046,"first_completed_ms":1000,"last_completed_ms":600000,"maximum_gap_ms":1000,"cleanup_confirmed":true,"last_observation":{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}},"preparation_started_ms":600020,"inventory_prepared_ms":600030,"stop_requested_ms":600040,"safety_joined_ms":600045,"thermal":{"schema":"hhsoft.pc-tester.run-thermal/v1","status":"completed","limit_millicelsius":90000,"interval_ms":1000,"observation_timeout_ms":1000,"maximum_allowed_gap_ms":2500,"stop_timeout_ms":2000,"attempted_observations":602,"completed_observations":602,"periodic_attempts":600,"periodic_completions":600,"elapsed_ms":600048,"first_completed_ms":1,"last_completed_ms":600032,"maximum_gap_ms":1000,"initial":{"requested_ms":0,"attempted":true,"completed":true,"started_ms":0,"completed_ms":1,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"final":{"requested_ms":600031,"attempted":true,"completed":true,"started_ms":600031,"completed_ms":600032,"observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"aggregate":{"coverage":"unavailable","accepted_observations":602,"available_observations":0,"partial_observations":0,"unavailable_observations":602,"maximum_sensors":0,"valid_readings":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0},"cleanup_confirmed":true,"joined":true,"joined_ms":600047,"last_observation":{"coverage":"unavailable","sensors":0,"peak_millicelsius":0,"rejected_readings":0,"unavailable_readings":0,"enumeration_errors":0}},"local_thermal_sources":[]`

func appTouchpadWorkerOrder(version int) string {
	switch version {
	case 4:
		return "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.pointer.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick"
	case 5:
		return "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick"
	case 6:
		return "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick network.readiness.quick storage.readiness.quick storage.quick usb.readiness.quick graphics.quick display.operator.quick"
	case 7:
		return "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
	case 8:
		return "cpu.quick cpu.stress memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
	case 9:
		return "cpu.quick memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
	case 10:
		return "cpu.quick cpu.stress memory.quick audio.readiness.quick input.readiness.quick input.keyboard.quick input.pointer.quick input.touchpad.quick network.readiness.quick storage.readiness.quick usb.readiness.quick graphics.quick display.operator.quick inspection.exterior.quick"
	default:
		panic("undeclared fixture version")
	}
}

func appTouchpadJournalPayload(t *testing.T, version int, observed bool) InventoryPayload {
	t.Helper()
	p := appTouchpadPayload(t)
	touchpad, integrity := p.TestResults[0], p.TestResults[1]
	p.TestResults = nil
	var workers []string
	for _, id := range strings.Fields(appTouchpadWorkerOrder(version)) {
		workers = append(workers, fmt.Sprintf("{\"id\":%q,\"state\":\"not_scheduled\",\"outcome\":\"excluded\"}", id))
		row := TestResult{ID: id, Version: 1, Status: TestSkip, PlanID: touchpad.PlanID, HardwareGraph: touchpad.HardwareGraph, PlannedStatus: PlannedExcluded,
			Evidence: []byte("{\"schema\":\"hhsoft.pc-tester.execution-note/v1\",\"status\":\"skip\",\"source_status\":\"excluded\",\"detail\":\"not run; disposition recorded in the frozen plan\"}")}
		if version != 4 && (id == "cpu.quick" || id == "memory.quick" || id == "input.keyboard.quick" || id == "input.pointer.quick" || id == "storage.quick") {
			row.Version = 2
		}
		if id == "input.touchpad.quick" {
			row.Version = 2
			if observed {
				row = touchpad
			}
		}
		p.TestResults = append(p.TestResults, row)
	}
	raw := "{\"schema\":\"hhsoft.pc-tester.quick-session/v" + fmt.Sprint(version) + appTouchpadJournalPrefix + strings.Join(workers, ",") + appTouchpadJournalTail
	if version == 6 {
		raw += ",\"storage_source\":null"
	}
	if version == 8 || version == 10 {
		raw += ",\"profile\":\"standard\",\"cpu_stress_source\":null"
	}
	journal, err := sessionevidence.Decode([]byte(raw + "}"))
	if err != nil {
		t.Fatal("literal journal", err)
	}
	if observed {
		for i := range journal.Workers {
			if journal.Workers[i].ID == "input.touchpad.quick" {
				journal.Workers[i].State = "completed"
				journal.Workers[i].Outcome = "pass"
				journal.Workers[i].Timing = &sessionevidence.WorkerTiming{StartedMilliseconds: 10, FinishedMilliseconds: 1010}
			}
		}
		before := journal.Safety[0]
		before.Phase = "before-input.touchpad.quick"
		journal.Safety = append(journal.Safety[:1:1], append([]sessionevidence.SafetyRecord{before}, journal.Safety[1:]...)...)
	}
	if version < 9 {
		touchpad.Version = 1
		touchpad.Status = TestSkip
		touchpad.PlannedStatus = PlannedUnsupported
		touchpad.Evidence = []byte("{\"reason\":\"historical unsupported touchpad definition\"}")
		p.TestResults = append(p.TestResults, touchpad)
	}
	encoded, err := journal.MarshalJSON()
	if err != nil {
		t.Fatal("explicit dormant journal", err)
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

func TestTouchpadAppHistoricalAndCurrentSessionVersions(t *testing.T) {
	for _, c := range []struct {
		name     string
		version  int
		observed bool
	}{
		{"historical_v4", 4, false}, {"historical_v5", 5, false}, {"historical_v6", 6, false}, {"historical_v7", 7, false}, {"historical_v8", 8, false},
		{"quick_v9_observed", 9, true}, {"standard_v10_observed", 10, true}, {"quick_v9_note", 9, false}, {"standard_v10_note", 10, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := appTouchpadJournalPayload(t, c.version, c.observed)
			before, _ := json.Marshal(p)
			if err := ValidateCoverage(p); err != nil {
				t.Fatal("literal versioned report rejected", err)
			}
			view, err := ValidatedTouchpad(p)
			if err != nil || (view != nil) != c.observed {
				t.Fatal("historical note became functional or current view disappeared", err)
			}
			thermal, err := ValidatedSessionThermal(p)
			if err != nil || thermal == nil {
				t.Fatal("strict monitoring view lost", err)
			}
			standard := c.version == 8 || c.version == 10
			if strings.HasPrefix(thermal.Scope, "Standard-session") != standard {
				t.Fatal("profile-specific scope changed")
			}
			thermal.StopReason = "mutated"
			if view != nil {
				view.Devices[0].Capabilities.Axes[0].Maximum = 42
			}
			after, _ := json.Marshal(p)
			if !bytes.Equal(before, after) {
				t.Fatal("versioned view aliases signed evidence")
			}
		})
	}
	if sessionevidence.Schema != "hhsoft.pc-tester.quick-session/v11" {
		t.Fatal("current quick schema changed")
	}
}

func TestTouchpadAppSessionRejectsForgedIntervalsAndSources(t *testing.T) {
	cases := []struct {
		name   string
		change func(*InventoryPayload, *sessionevidence.IntegrityEvidence)
	}{
		{"old_session", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.Session.Schema = "hhsoft.pc-tester.quick-session/v7"
		}},
		{"future_session", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.Session.Schema = "hhsoft.pc-tester.quick-session/v14"
		}},
		{"short_interval", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.Session.Workers[6].Timing.FinishedMilliseconds = 1009
		}},
		{"missing_interval", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) { v.Session.Workers[6].Timing = nil }},
		{"worker_outcome", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.Session.Workers[6].Outcome = "inconclusive"
		}},
		{"worker_order", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.Session.Workers[5], v.Session.Workers[6] = v.Session.Workers[6], v.Session.Workers[5]
		}},
		{"wrong_plan", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.PlanID = "sha256:" + strings.Repeat("9", 64)
		}},
		{"wrong_graph", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.PlannedHardwareGraph = "hmac-sha256:" + strings.Repeat("9", 64)
		}},
		{"final_count", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.PlannedComponentCount = 2
			v.FinalComponentCount = 2
		}},
		{"monitor_cleanup", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.Session.Monitor.CleanupConfirmed = false
		}},
		{"thermal_cleanup", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.Session.Thermal.CleanupConfirmed = false
		}},
		{"unrelated_source", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.Session.LocalThermalSources = append(v.Session.LocalThermalSources, sessionevidence.LocalThermalSource{WorkerID: "input.touchpad.quick"})
		}},
		{"wrong_placement", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			p.TestResults[0].Evidence, _ = json.Marshal(v.Session)
		}},
		{"hidden_earlier_worker", func(p *InventoryPayload, v *sessionevidence.IntegrityEvidence) {
			v.Session.Workers = append(v.Session.Workers, v.Session.Workers[6])
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := appTouchpadJournalPayload(t, 9, true)
			if err := ValidateCoverage(p); err != nil {
				t.Fatal("positive prerequisite", err)
			}
			row := &p.TestResults[len(p.TestResults)-1]
			v, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if err != nil {
				t.Fatal(err)
			}
			originalEvidence := append([]byte(nil), row.Evidence...)
			c.change(&p, &v)
			encoded, marshalErr := json.Marshal(v)
			if c.name == "future_session" {
				if !errors.Is(marshalErr, sessionevidence.ErrInvalid) {
					t.Fatal("strict serializer accepted a future session", marshalErr)
				}
				// The strict serializer rejects this fixture; edit only its wire schema
				// so coverage and thermal validation still receive the malformed claim.
				oldSchema := []byte(`"schema":"hhsoft.pc-tester.quick-session/v9"`)
				newSchema := []byte(`"schema":"hhsoft.pc-tester.quick-session/v14"`)
				if bytes.Count(originalEvidence, oldSchema) != 1 {
					t.Fatal("future-session fixture must have exactly one current session schema")
				}
				row.Evidence = bytes.Replace(originalEvidence, oldSchema, newSchema, 1)
				if !json.Valid(row.Evidence) || bytes.Count(row.Evidence, newSchema) != 1 ||
					!bytes.Equal(bytes.Replace(row.Evidence, newSchema, oldSchema, 1), originalEvidence) {
					t.Fatal("future-session wire mutation changed unrelated evidence")
				}
			} else {
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				row.Evidence = encoded
			}
			if ValidateCoverage(p) == nil {
				t.Fatal("forged journal accepted")
			}
			if _, err := ValidatedSessionThermal(p); err == nil {
				t.Fatal("thermal-only entry bypassed touchpad checks")
			}
		})
	}
}

func TestTouchpadAppStandardStressSourceSurvivesV10(t *testing.T) {
	p := standardStressPayload(t)
	integrity := &p.TestResults[len(p.TestResults)-1]
	v, err := sessionevidence.DecodeIntegrity(integrity.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	v.Session.Schema = "hhsoft.pc-tester.quick-session/v10"
	worker := sessionevidence.WorkerRecord{ID: "input.touchpad.quick", State: "not_scheduled", Outcome: "excluded"}
	v.Session.Workers = append(v.Session.Workers[:7:7], append([]sessionevidence.WorkerRecord{worker}, v.Session.Workers[7:]...)...)
	raw, err := v.Session.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(integrity.Evidence, &object) != nil {
		t.Fatal("integrity")
	}
	object["session"] = raw
	integrity.Evidence, err = json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	note := TestResult{ID: "input.touchpad.quick", Version: 2, Status: TestSkip, PlannedStatus: PlannedExcluded, PlanID: integrity.PlanID, HardwareGraph: integrity.HardwareGraph,
		Evidence: []byte("{\"schema\":\"hhsoft.pc-tester.execution-note/v1\",\"status\":\"skip\",\"source_status\":\"excluded\",\"detail\":\"not run; disposition recorded in the frozen plan\"}")}
	p.TestResults = append(p.TestResults, note)
	if value, err := ValidatedCPUStress(p); err != nil || value == nil || value.Iterations != 256 {
		t.Fatal("existing typed stress source lost", err)
	}
	if value, err := ValidatedTouchpad(p); err != nil || value != nil {
		t.Fatal("unobserved touchpad became a functional pass", err)
	}
	object["session"] = bytes.Replace(raw, []byte("\"evidence_typed_sha256\":\"sha256:"), []byte("\"evidence_typed_sha256\":\"invalid:"), 1)
	p.TestResults[len(p.TestResults)-2].Evidence, _ = json.Marshal(object)
	if ValidateCoverage(p) == nil {
		t.Fatal("v10 weakened CPU-stress source binding")
	}
}

func TestTouchpadAppExplicitExclusionCannotClaimScheduledExecution(t *testing.T) {
	for _, name := range []string{"scheduled_pass", "scheduled_cancel", "scheduled_note", "v9_observed", "v10_observed", "excluded_note", "missing_driver_note", "not_present_note", "unsupported_note", "scheduled_without_exclusion", "historical_one"} {
		t.Run(name, func(t *testing.T) {
			p := appTouchpadPayload(t)
			wantError := true
			switch name {
			case "v9_observed":
				p = appTouchpadJournalPayload(t, 9, true)
			case "v10_observed":
				p = appTouchpadJournalPayload(t, 10, true)
			case "scheduled_cancel":
				appTouchpadChange(t, &p, func(v *inputtest.TouchpadResult) {
					v.Status = inputtest.StatusAborted
					v.StopReason = "operator_cancelled"
					v.Detail = "operator_cancelled"
				})
			case "scheduled_note":
				p.TestResults[0].Status = TestAborted
				p.TestResults[0].Evidence = []byte("{\"schema\":\"hhsoft.pc-tester.execution-note/v1\",\"status\":\"aborted\",\"source_status\":\"aborted\",\"detail\":\"bounded evidence was not produced; outcome recorded by the run supervisor\"}")
			case "excluded_note", "missing_driver_note", "not_present_note", "unsupported_note":
				disposition := strings.TrimSuffix(name, "_note")
				status := "skip"
				if disposition == "missing_driver" {
					status = "inconclusive"
				}
				p.TestResults[0].PlannedStatus = PlannedStatus(disposition)
				p.TestResults[0].Status = TestStatus(status)
				p.TestResults[0].Evidence, _ = json.Marshal(map[string]string{"schema": "hhsoft.pc-tester.execution-note/v1", "status": status, "source_status": disposition, "detail": "not run; disposition recorded in the frozen plan"})
				wantError = false
			case "scheduled_without_exclusion":
				wantError = false
			case "historical_one":
				p.TestResults[0].Version = 1
				p.TestResults[0].PlannedStatus = PlannedUnsupported
				p.TestResults[0].Status = TestSkip
				p.TestResults[0].Evidence = []byte("{\"reason\":\"historical unsupported definition\"}")
				wantError = false
			}
			if err := ValidateCoverage(p); err != nil {
				t.Fatal("positive prerequisite", err)
			}
			if name != "scheduled_without_exclusion" {
				for i := range p.TestResults {
					if p.TestResults[i].ID == "run.integrity.quick" {
						var object map[string]json.RawMessage
						if json.Unmarshal(p.TestResults[i].Evidence, &object) != nil {
							t.Fatal("integrity object")
						}
						object["excluded_tests"] = []byte("[\"input.touchpad.quick\"]")
						p.TestResults[i].Evidence, _ = json.Marshal(object)
					}
				}
			}
			err := ValidateCoverage(p)
			if (err != nil) != wantError {
				t.Fatalf("explicit exclusion semantics mismatch: %v", err)
			}
			if _, err = ValidatedSessionThermal(p); (err != nil) != wantError {
				t.Fatal("thermal entry disagrees with exclusion")
			}
		})
	}
}

// Incomplete inventory is reportable without inventing a runnable interface.
func TestTouchpadPublicProjectionPreservesUnaddressableInventory(t *testing.T) {
	for _, name := range []string{"category_only", "non_event_handlers", "capabilities_without_interface", "ambiguous_input"} {
		t.Run(name, func(t *testing.T) {
			component := appTouchpadComponent(t)
			appTouchpadDeleteComponent(&component, "interface")
			appTouchpadDeleteComponent(&component, "handlers")
			switch name {
			case "category_only":
				component.Attributes = []inventory.Attribute{{Name: "category", Value: "touchpad"}}
			case "non_event_handlers":
				appTouchpadSetComponent(&component, "handlers", "mouse0")
			case "ambiguous_input":
				appTouchpadSetComponent(&component, "category", "input")
			}
			for _, field := range []string{"name", "model", "kernel_path", "unique_id", "coordinates"} {
				appTouchpadSetComponent(&component, field, "private-canary-unaddressable")
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
			if attributes["touchpad_candidate"] != "true" {
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
			claimed := appTouchpadPayload(t)
			if err := ValidateCoverage(claimed); err != nil {
				t.Fatal("positive evidence prerequisite", err)
			}
			claimed.Components[0].Attributes = record.Attributes
			if ValidateCoverage(claimed) == nil {
				t.Fatal("unaddressable inventory authenticated functional evidence")
			}
			if view, err := ValidatedTouchpad(claimed); err == nil || view != nil {
				t.Fatal("unaddressable inventory produced a functional view")
			}

			// Missing information is different from a supplied invalid name.
			invalid := component
			invalid.Attributes = append([]inventory.Attribute(nil), component.Attributes...)
			appTouchpadSetComponent(&invalid, "interface", "")
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
