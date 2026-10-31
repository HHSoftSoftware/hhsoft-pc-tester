package bundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
)

// This independent literal describes a bounded synthetic report, not device qualification.
const bundleTouchscreenLiteral = `{"schema_version":1,"components":[{"type":"input","id":"event0","attributes":[{"name":"abs_capabilities","value":"3"},{"name":"bitmap_word_bits","value":"64"},{"name":"bus","value":"0003"},{"name":"category","value":"touchscreen"},{"name":"event_capabilities","value":"b"},{"name":"handlers","value":"event0"},{"name":"interface","value":"event0"},{"name":"key_capabilities","value":"400 0 0 0 0 0"},{"name":"msc_capabilities","value":"0"},{"name":"product_id","value":"abcd"},{"name":"properties","value":"2"},{"name":"relative_capabilities","value":"0"},{"name":"touchscreen_candidate","value":"true"},{"name":"vendor_id","value":"1234"},{"name":"version","value":"0001"}],"identity":{"algorithm":"hmac-sha256","key_epoch":"fixture","confidence":"unavailable"}}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"fixture","key_id":"hmac-sha256:3333333333333333333333333333333333333333333333333333333333333333","identified_parts":0,"unidentified_parts":1},"test_results":[{"id":"input.touchscreen.quick","version":2,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.touchscreen-functional/v1","status":"pass","stop_reason":"completed","limit_ms":45000,"duration_ms":1000,"cleanup_confirmed":true,"devices":[{"interface":"event0","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":4660,"product":43981,"version":1},"capabilities":{"protocol_version":65537,"events":[0,1,3],"keys":[330],"relative":[],"properties":[1],"misc":[],"axes":[{"code":0,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1},{"code":1,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1}]},"x_threshold":50,"y_threshold":50,"frames":6,"contact_starts":1,"contact_releases":1,"xy_baselines":1,"qualifying_contacts":1,"x_positive":2,"x_negative":2,"y_positive":2,"y_negative":2,"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]},"planned_status":"scheduled"},{"id":"run.integrity.quick","version":1,"status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","evidence":{"schema":"hhsoft.pc-tester.run-integrity/v1","status":"pass","plan_id":"sha256:1111111111111111111111111111111111111111111111111111111111111111","planned_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","final_hardware_graph":"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222","planned_component_count":1,"final_component_count":1}}],"coverage_scope":"frozen_plan"}`

func bundleTouchscreenPayload(t *testing.T) app.InventoryPayload {
	t.Helper()
	var p app.InventoryPayload
	if err := json.Unmarshal([]byte(bundleTouchscreenLiteral), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTouchscreenSignedBundleRequiresConsistentViews(t *testing.T) {
	for _, name := range []string{"both", "html_only", "pdf_only", "viewless", "signed_older_version"} {
		t.Run(name, func(t *testing.T) {
			p := bundleTouchscreenPayload(t)
			encoded, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			version := product.Version
			if name == "signed_older_version" {
				version = "0.0.9"
			}
			html, err := render.InventoryHTMLForVersion(p, version)
			if err != nil {
				t.Fatal(err)
			}
			pdf, err := render.InventoryPDFForVersion(p, version)
			if err != nil {
				t.Fatal(err)
			}
			if name == "html_only" {
				pdf = nil
			}
			if name == "pdf_only" {
				html = nil
			}
			if name == "viewless" {
				html = nil
				pdf = nil
			}
			for _, self := range []bool{false, true} {
				archive, envelope, keys := independentSignedViewArchive(t, encoded, html, pdf, product.Name, version, self)
				artifact, err := verifyViewFixture(archive, keys, self)
				if err != nil {
					t.Fatal("independently signed valid report rejected", err)
				}
				if !bytes.Equal(artifact.InventoryJSON, encoded) || !bytes.Equal(artifact.RenderedReport, html) || !bytes.Equal(artifact.RenderedPDF, pdf) {
					t.Fatal("verified bytes changed")
				}
				var output bytes.Buffer
				var writeErr error
				if self {
					var mismatched bytes.Buffer
					if err := WriteWithPDF(&mismatched, encoded, envelope, html, pdf); !errors.Is(err, ErrDigestMismatch) || mismatched.Len() != 0 {
						t.Fatal("ordinary writer did not reject the self-issued manifest before output", err)
					}
					writeErr = WriteSelfIssuedWithPDF(&output, encoded, envelope, html, pdf, keys[envelope.KeyID])
				} else {
					writeErr = WriteWithPDF(&output, encoded, envelope, html, pdf)
				}
				if writeErr != nil {
					t.Fatal("producer rejected matching trust-mode content", writeErr)
				}
				written, err := verifyViewFixture(output.Bytes(), keys, self)
				if err != nil {
					t.Fatal("producer output failed verification", err)
				}
				if !bytes.Equal(written.InventoryJSON, encoded) || !bytes.Equal(written.RenderedReport, html) || !bytes.Equal(written.RenderedPDF, pdf) || !bytes.Equal(written.SelfIssuedKey, artifact.SelfIssuedKey) {
					t.Fatal("producer changed verified content or trust member")
				}
			}
			if name == "signed_older_version" {
				if version == product.Version {
					t.Fatal("historical-version fixture became current")
				}
				if _, err := ManifestDigestWithPDF(encoded, html, pdf); err == nil {
					t.Fatal("current-version digest helper accepted older rendered views")
				}
			} else if _, err := ManifestDigestWithPDF(encoded, html, pdf); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTouchscreenSignedBundleRejectsOpaqueDefinitionBypasses(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		{"future_definition", []byte("{\"test_results\":[{\"id\":\"input.touchscreen.quick\",\"version\":99}]}")},
		{"zero_definition", []byte("{\"test_results\":[{\"id\":\"input.touchscreen.quick\",\"version\":0}]}")},
		{"missing_version", []byte("{\"test_results\":[{\"id\":\"input.touchscreen.quick\"}]}")},
		{"missing_version_outside_array", []byte("{\"id\":\"input.touchscreen.quick\",\"status\":\"pass\"}")},
		{"string_version", []byte("{\"id\":\"input.touchscreen.quick\",\"version\":\"1\"}")},
		{"float_version", []byte("{\"id\":\"input.touchscreen.quick\",\"version\":1.0}")},
		{"escaped_claim", []byte("{\"schema\":\"hhsoft.pc-tester.touchscreen-\\u0066unctional/v1\"}")},
		{"unknown_family", []byte("{\"schema\":\"hhsoft.pc-tester.touchscreen-functional/v99\"}")},
		{"duplicate_results", []byte("{\"test_results\":[{\"id\":\"input.touchscreen.quick\",\"version\":2}],\"test_results\":[]}")},
		{"case_definition", []byte("{\"ID\":\"INPUT.TOUCHSCREEN.QUICK\",\"version\":1}")},
		{"earlier_definition", []byte("{\"id\":\"input.touchscreen.quick\",\"version\":2,\"version\":1}")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "escaped_claim" && !bytes.Contains(c.raw, []byte("\\u0066")) {
				t.Fatal("escape fixture became canonical")
			}
			for _, self := range []bool{false, true} {
				archive, envelope, keys := independentSignedViewArchive(t, c.raw, nil, nil, product.Name, product.Version, self)
				if _, err := verifyViewFixture(archive, keys, self); err == nil {
					t.Fatal("correct signature bypassed strict evidence validation")
				}
				var output bytes.Buffer
				var writeErr error
				if self {
					writeErr = WriteSelfIssuedWithPDF(&output, c.raw, envelope, nil, nil, keys[envelope.KeyID])
				} else {
					writeErr = WriteWithPDF(&output, c.raw, envelope, nil, nil)
				}
				if writeErr == nil {
					t.Fatal("viewless writer accepted malformed definition")
				}
			}
			if _, err := ManifestDigestWithPDF(c.raw, nil, nil); err == nil {
				t.Fatal("digest accepted opaque claim")
			}
		})
	}
	for _, c := range []struct{ name, raw string }{
		{"historical_definition_one", "{\"id\":\"input.touchscreen.quick\",\"version\":1,\"reason\":\"unsupported\"}"},
		{"id_only_reference", "{\"id\":\"input.touchscreen.quick\"}"},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, self := range []bool{false, true} {
				archive, _, keys := independentSignedViewArchive(t, []byte(c.raw), nil, nil, product.Name, product.Version, self)
				if _, err := verifyViewFixture(archive, keys, self); err != nil {
					t.Fatal("explicit historical opaque exception lost", err)
				}
			}
		})
	}
}

func TestTouchscreenSignedBundleRejectsContradictionAndUnrelatedViews(t *testing.T) {
	for _, name := range []string{"false_neutral", "cleanup", "inventory_binding", "hidden_msc", "duplicate_row", "different_html", "different_pdf", "active_html", "bad_pdf"} {
		t.Run(name, func(t *testing.T) {
			p := bundleTouchscreenPayload(t)
			html, err := render.InventoryHTML(p)
			if err != nil {
				t.Fatal(err)
			}
			pdf, err := render.InventoryPDF(p)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "false_neutral":
				p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("\"final_neutral\":true"), []byte("\"final_neutral\":false"), 1)
			case "cleanup":
				p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("\"cleanup_confirmed\":true"), []byte("\"cleanup_confirmed\":false"), 1)
			case "inventory_binding":
				for i := range p.Components[0].Attributes {
					if p.Components[0].Attributes[i].Name == "vendor_id" {
						p.Components[0].Attributes[i].Value = "0000"
					}
				}
			case "hidden_msc":
				for i := range p.Components[0].Attributes {
					if p.Components[0].Attributes[i].Name == "msc_capabilities" {
						p.Components[0].Attributes[i].Value = "10"
					}
				}
			case "duplicate_row":
				p.TestResults = append(p.TestResults, p.TestResults[0])
			case "different_html":
				html = bytes.Replace(html, []byte("Touchscreen"), []byte("Unrelated"), 1)
			case "different_pdf":
				if !bytes.Contains(pdf, []byte("TOUCHSCREEN")) {
					t.Fatal("PDF replacement anchor absent")
				}
				pdf = bytes.Replace(pdf, []byte("TOUCHSCREEN"), []byte("UNRELATED"), 1)
			case "active_html":
				html = []byte("<!doctype html><script>/* inert fixture */</script>")
			case "bad_pdf":
				pdf = []byte("%PDF-1.4\nunrelated content\n%%EOF\n")
			}
			encoded, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, self := range []bool{false, true} {
				archive, envelope, keys := independentSignedViewArchive(t, encoded, html, pdf, product.Name, product.Version, self)
				if _, err := verifyViewFixture(archive, keys, self); err == nil {
					t.Fatal("signed contradiction accepted")
				}
				var output bytes.Buffer
				var writeErr error
				if self {
					writeErr = WriteSelfIssuedWithPDF(&output, encoded, envelope, html, pdf, keys[envelope.KeyID])
				} else {
					writeErr = WriteWithPDF(&output, encoded, envelope, html, pdf)
				}
				if writeErr == nil {
					t.Fatal("producer accepted contradiction")
				}
			}
			if _, err := ManifestDigestWithPDF(encoded, html, pdf); err == nil {
				t.Fatal("digest helper bypass")
			}
		})
	}
}
