package bundle

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
)

func coverageViewPayload(t *testing.T) app.InventoryPayload {
	payload := viewPayload(t)
	payload.CoverageScope = app.CoverageFrozenPlan
	payload.TestResults = []app.TestResult{
		{ID: "camera.quick", Version: 1, Status: app.TestSkip, PlannedStatus: app.PlannedUnsupported, PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: json.RawMessage(`{"schema":"hhsoft.pc-tester.execution-note/v1","status":"skip","source_status":"unsupported"}`)},
		{ID: "run.integrity.quick", Version: 1, Status: app.TestPass, PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: json.RawMessage(`{"status":"pass"}`)},
	}
	return payload
}

func TestIndependentlySignedCoverageReportsVerifyWithAndWithoutViews(t *testing.T) {
	for _, scope := range []app.CoverageScope{"", app.CoverageDirectCapture, app.CoverageFrozenPlan} {
		payload := coverageViewPayload(t)
		if scope != app.CoverageFrozenPlan {
			payload.TestResults = nil
		}
		payload.CoverageScope = scope
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		html, err := render.InventoryHTML(payload)
		if err != nil {
			t.Fatal(err)
		}
		pdf, err := render.InventoryPDF(payload)
		if err != nil {
			t.Fatal(err)
		}
		for _, withViews := range []bool{false, true} {
			var htmlView, pdfView []byte
			if withViews {
				htmlView, pdfView = html, pdf
			}
			for _, selfIssued := range []bool{false, true} {
				archive, _, keys := independentSignedViewArchive(t, encoded, htmlView, pdfView, product.Name, product.Version, selfIssued)
				artifact, err := verifyViewFixture(archive, keys, selfIssued)
				if err != nil || !bytes.Equal(artifact.InventoryJSON, encoded) || !bytes.Equal(artifact.RenderedReport, htmlView) || !bytes.Equal(artifact.RenderedPDF, pdfView) {
					t.Fatalf("valid %q coverage failed verification: %v", scope, err)
				}
			}
		}
	}
}

func TestSignedCoverageRejectsMalformedAndContradictoryMetadataWithoutViews(t *testing.T) {
	encoded, err := json.Marshal(coverageViewPayload(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"unknown scope", func(b []byte) []byte { return bytes.Replace(b, []byte(`"frozen_plan"`), []byte(`"complete"`), 1) }},
		{"null scope", func(b []byte) []byte { return bytes.Replace(b, []byte(`"frozen_plan"`), []byte(`null`), 1) }},
		{"blank scope", func(b []byte) []byte { return bytes.Replace(b, []byte(`"frozen_plan"`), []byte(`""`), 1) }},
		{"scope case alias", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"coverage_scope"`), []byte(`"Coverage_Scope"`), 1)
		}},
		{"duplicate scope", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"coverage_scope":`), []byte(`"coverage_scope":"complete","coverage_scope":`), 1)
		}},
		{"escaped duplicate scope", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"coverage_scope":`), []byte(`"coverage_\u0073cope":"complete","coverage_scope":`), 1)
		}},
		{"unknown planned status", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"planned_status":"unsupported"`), []byte(`"planned_status":"complete"`), 1)
		}},
		{"null planned status", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"planned_status":"unsupported"`), []byte(`"planned_status":null`), 1)
		}},
		{"blank planned status", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"planned_status":"unsupported"`), []byte(`"planned_status":""`), 1)
		}},
		{"planned status case alias", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"planned_status"`), []byte(`"Planned_Status"`), 1)
		}},
		{"unsupported pass", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"status":"skip"`), []byte(`"status":"pass"`), 1)
		}},
		{"false direct scope", func(b []byte) []byte { return bytes.Replace(b, []byte(`"frozen_plan"`), []byte(`"direct_capture"`), 1) }},
		{"object result container", func([]byte) []byte {
			return []byte(`{"schema_version":1,"test_results":{"planned_status":"complete"}}`)
		}},
		{"object escaped key", func([]byte) []byte {
			return []byte(`{"schema_version":1,"test_results":{"planned_\u0073tatus":"complete"}}`)
		}},
		{"object aliased key", func([]byte) []byte {
			return []byte(`{"schema_version":1,"test_results":{"Planned_Status":"complete"}}`)
		}},
		{"object duplicate container", func([]byte) []byte {
			return []byte(`{"schema_version":1,"test_results":{"planned_status":"complete"},"test_results":null}`)
		}},
		{"mixed result container", func([]byte) []byte {
			return []byte(`{"schema_version":1,"test_results":[{"planned_status":"complete"},1]}`)
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			malformed := fixture.change(encoded)
			for _, selfIssued := range []bool{false, true} {
				archive, envelope, keys := independentSignedViewArchive(t, malformed, nil, nil, product.Name, product.Version, selfIssued)
				if _, err := verifyViewFixture(archive, keys, selfIssued); err == nil {
					t.Fatal("signed invalid coverage accepted without rendered views")
				}
				var destination bytes.Buffer
				if err := WriteWithPDF(&destination, malformed, envelope, nil, nil); err == nil {
					t.Fatal("producer accepted invalid coverage without views")
				}
			}
			if _, err := ManifestDigestWithPDF(malformed, nil, nil); err == nil {
				t.Fatal("digest helper accepted invalid coverage")
			}
		})
	}
	legacy := []byte(`{"schema_version":1}`)
	archive, _, keys := independentSignedViewArchive(t, legacy, nil, nil, product.Name, product.Version, false)
	if _, err := verifyViewFixture(archive, keys, false); err != nil {
		t.Fatalf("historical viewless payload without new claims rejected: %v", err)
	}
}
