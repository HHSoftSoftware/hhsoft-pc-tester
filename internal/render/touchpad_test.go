package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"strings"
	"testing"
)

// This independent literal describes a bounded synthetic report, not device qualification.
const renderTouchpadLiteral = `{"schema_version":1,"components":[{"type":"input","id":"event0","attributes":[{"name":"abs_capabilities","value":"3"},{"name":"bitmap_word_bits","value":"64"},{"name":"bus","value":"0003"},{"name":"category","value":"touchpad"},{"name":"event_capabilities","value":"b"},{"name":"handlers","value":"event0"},{"name":"interface","value":"event0"},{"name":"key_capabilities","value":"420 10000 0 0 0 0"},{"name":"msc_capabilities","value":"0"},{"name":"product_id","value":"abcd"},{"name":"properties","value":"1"},{"name":"relative_capabilities","value":"0"},{"name":"touchpad_candidate","value":"true"},{"name":"vendor_id","value":"1234"},{"name":"version","value":"0001"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"fixture","confidence":"unavailable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"fixture","key_id":"hmac-sha256:3333333333333333333333333333333333333333333333333333333333333333","identified_parts":0,"unidentified_parts":1},"test_results":[{"id":"input.touchpad.quick","version":2,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.touchpad-functional/v1","status":"pass","stop_reason":"completed","limit_ms":30000,"duration_ms":1000,"cleanup_confirmed":true,"devices":[{"interface":"event0","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":4660,"product":43981,"version":1},"capabilities":{"protocol_version":65537,"events":[0,1,3],"keys":[272,325,330],"relative":[],"properties":[0],"misc":[],"axes":[{"code":0,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1},{"code":1,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1}]},"x_threshold":50,"y_threshold":50,"frames":6,"contact_starts":1,"contact_releases":1,"xy_baselines":1,"qualifying_contacts":1,"x_positive":2,"x_negative":2,"y_positive":2,"y_negative":2,"buttons":[{"button":"left","presses":1,"releases":1,"pairs":1}],"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]},"planned_status":"scheduled"},{"id":"run.integrity.quick","version":1,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.run-integrity/v1","status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","planned_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","final_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","planned_component_count":1,"final_component_count":1}}],"coverage_scope":"frozen_plan"}`

func renderTouchpadPayload(t *testing.T) app.InventoryPayload {
	t.Helper()
	var p app.InventoryPayload
	if err := json.Unmarshal([]byte(renderTouchpadLiteral), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func renderTouchpadChange(t *testing.T, p *app.InventoryPayload, change func(*inputtest.TouchpadResult)) {
	t.Helper()
	var v inputtest.TouchpadResult
	if err := json.Unmarshal(p.TestResults[0].Evidence, &v); err != nil {
		t.Fatal(err)
	}
	change(&v)
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	p.TestResults[0].Evidence = raw
	p.TestResults[0].Status = app.TestStatus(v.Status)
}

func TestTouchpadRenderDisplaysBoundedMeaningNotHardwareDiagnosis(t *testing.T) {
	for _, name := range []string{"completed", "cancel_retains_pass", "prestate", "unsupported_marker", "unreadable", "historical_note"} {
		t.Run(name, func(t *testing.T) {
			p := renderTouchpadPayload(t)
			if name == "historical_note" {
				p.TestResults[0].Version = 1
				p.TestResults[0].Status = app.TestSkip
				p.TestResults[0].PlannedStatus = app.PlannedUnsupported
				p.TestResults[0].Evidence = []byte("{\"reason\":\"historical unsupported definition\"}")
			} else {
				renderTouchpadChange(t, &p, func(v *inputtest.TouchpadResult) {
					switch name {
					case "cancel_retains_pass":
						v.Status = inputtest.StatusAborted
						v.StopReason = "operator_cancelled"
						v.Detail = "operator_cancelled"
					case "prestate", "unsupported_marker", "unreadable":
						old := v.Devices[0]
						d := inputtest.TouchpadDeviceResult{Interface: old.Interface, Status: inputtest.StatusInconclusive, Readable: true, Supported: true, Identity: old.Identity, Capabilities: old.Capabilities, Buttons: []inputtest.PointerButtonResult{}, CleanupConfirmed: true, Detail: "unsupported_record_layout"}
						v.Status = inputtest.StatusError
						v.StopReason = "error"
						v.Detail = "unsupported_record_layout"
						if name == "unsupported_marker" {
							d.Supported = false
							d.Detail = "unsupported_capabilities"
							d.Capabilities.Axes = append(d.Capabilities.Axes, inputtest.TouchpadAxis{Code: 2})
							v.Status = inputtest.StatusInconclusive
							v.Detail = "unsupported_or_unreadable_device"
						}
						if name == "unreadable" {
							d.Readable = false
							d.Supported = false
							d.Identity = nil
							d.Capabilities = nil
							d.Detail = "identity_unavailable"
							v.Status = inputtest.StatusInconclusive
							v.Detail = "unsupported_or_unreadable_device"
						}
						v.Devices[0] = d
					}
				})
			}
			view, err := touchpadReportDocument(p)
			if err != nil {
				t.Fatal(err)
			}
			html, err := InventoryHTML(p)
			if err != nil {
				t.Fatal(err)
			}
			pdf, err := InventoryPDF(p)
			if err != nil {
				t.Fatal(err)
			}
			lines, err := inventoryPDFLines(p, product.Version)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(lines, "\n")
			if name == "historical_note" {
				if view != nil || bytes.Contains(html, []byte("touchpad-heading")) || strings.Contains(text, "GUIDED SINGLE-CONTACT TOUCHPAD OBSERVATIONS") {
					t.Fatal("historical note gained observation view")
				}
				return
			}
			if view == nil || len(view.Devices) != 1 || !bytes.Contains(html, []byte("touchpad-heading")) || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
				t.Fatal("missing typed render")
			}
			if !strings.Contains(view.Scope, "not distance or gesture qualification") || !strings.Contains(view.Scope, "were not tested") {
				t.Fatal("scope overclaims")
			}
			if !strings.Contains(text, "TOUCHPAD") || !strings.Contains(text, "Multitouch") || !strings.Contains(text, "Cleanup") {
				t.Fatal("PDF omitted finite scope/cleanup")
			}
			if name == "completed" {
				if view.Status != "pass" || view.Devices[0].XPositive != 2 || view.Devices[0].ContactReleases != 1 || view.Devices[0].Buttons[0].Name != "left" {
					t.Fatal("literal counts changed")
				}
				if !strings.Contains(view.Devices[0].Configuration, "X=50 Y=50") || !strings.Contains(view.Devices[0].Configuration, "not measured travel") {
					t.Fatal("static range confused with measured travel")
				}
			} else if name == "cancel_retains_pass" {
				if view.Status != "aborted" || view.Devices[0].Status != "pass" || !strings.Contains(view.Finding, "Missing actions") {
					t.Fatal("interruption erased retained row or diagnosed defect")
				}
			} else {
				if !strings.HasPrefix(view.Devices[0].Configuration, "Unavailable") {
					t.Fatal("zero diagnostic marker rendered as measured configuration")
				}
				if name == "unsupported_marker" && !strings.Contains(view.Devices[0].Capabilities, "ABS=[0 1 2]") {
					t.Fatal("unknown marker silently pruned")
				}
			}
		})
	}
}

type touchpadMutationWriter struct {
	bytes.Buffer
	change func()
	err    error
}

func (w *touchpadMutationWriter) Write(raw []byte) (int, error) {
	if w.change != nil {
		change := w.change
		w.change = nil
		change()
	}
	if w.err != nil {
		return 0, w.err
	}
	return w.Buffer.Write(raw)
}

func TestTouchpadRenderFreezesNestedDataBeforeWriterCallbacks(t *testing.T) {
	for _, name := range []string{"attributes", "raw_evidence", "view", "writer_error"} {
		t.Run(name, func(t *testing.T) {
			p := renderTouchpadPayload(t)
			expected, err := InventoryHTML(p)
			if err != nil {
				t.Fatal(err)
			}
			writer := &touchpadMutationWriter{}
			switch name {
			case "attributes":
				writer.change = func() { p.Components[0].Attributes[0].Value = "writer-alias-canary" }
			case "raw_evidence":
				writer.change = func() {
					for i := range p.TestResults[0].Evidence {
						p.TestResults[0].Evidence[i] = ' '
					}
				}
			case "view":
				view, err := touchpadReportDocument(p)
				if err != nil {
					t.Fatal(err)
				}
				view.Devices[0].Buttons[0].Pairs = 999
				view.Devices[0].Capabilities = "writer-alias-canary"
			case "writer_error":
				writer.err = errors.New("fixture output failure")
			}
			err = RenderInventoryHTML(writer, p)
			if name == "writer_error" {
				if !errors.Is(err, writer.err) {
					t.Fatal("writer uncertainty lost")
				}
				return
			}
			if err != nil || !bytes.Equal(expected, writer.Bytes()) {
				t.Fatal("caller mutation crossed output boundary", err)
			}
		})
	}
}

func TestTouchpadRenderRejectsInvalidEvidenceBeforeAnyOutput(t *testing.T) {
	for _, name := range []string{"cleanup", "neutral", "future_version", "unknown_member", "duplicate_member", "large_evidence", "too_many_devices", "mismatched_inventory", "excluded_observation"} {
		t.Run(name, func(t *testing.T) {
			p := renderTouchpadPayload(t)
			switch name {
			case "cleanup":
				renderTouchpadChange(t, &p, func(v *inputtest.TouchpadResult) {
					v.CleanupConfirmed = false
					v.Status = inputtest.StatusError
					v.StopReason = "error"
					v.Detail = "cleanup_unconfirmed"
				})
			case "neutral":
				renderTouchpadChange(t, &p, func(v *inputtest.TouchpadResult) { v.Devices[0].FinalNeutral = false })
			case "future_version":
				p.TestResults[0].Version = 99
			case "unknown_member":
				p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("{"), []byte("{\"coordinates\":[1,2],"), 1)
			case "duplicate_member":
				p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("\"frames\":"), []byte("\"frames\":0,\"frames\":"), 1)
			case "large_evidence":
				p.TestResults[0].Evidence = bytes.Repeat([]byte(" "), 1<<20+1)
			case "too_many_devices":
				renderTouchpadChange(t, &p, func(v *inputtest.TouchpadResult) {
					d := v.Devices[0]
					v.Devices = make([]inputtest.TouchpadDeviceResult, 129)
					for i := range v.Devices {
						v.Devices[i] = d
					}
				})
			case "mismatched_inventory":
				for i := range p.Components[0].Attributes {
					if p.Components[0].Attributes[i].Name == "vendor_id" {
						p.Components[0].Attributes[i].Value = "0000"
					}
				}
			case "excluded_observation":
				p.TestResults[1].Evidence = bytes.Replace(p.TestResults[1].Evidence, []byte("{"), []byte("{\"excluded_tests\":[\"input.touchpad.quick\"],"), 1)
			}
			var output bytes.Buffer
			if RenderInventoryHTML(&output, p) == nil || output.Len() != 0 {
				t.Fatal("invalid observation reached streaming writer")
			}
			if pdf, err := InventoryPDF(p); err == nil || len(pdf) != 0 {
				t.Fatal("invalid observation reached PDF")
			}
		})
	}
}
