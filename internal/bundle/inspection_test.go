package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func inspectionBundlePayload(t *testing.T, finding, status, stop string, complete bool) app.InventoryPayload {
	t.Helper()
	p := viewPayload(t)
	value := exteriorinspection.Result{Schema: "hhsoft.pc-tester.exterior-inspection/v1", Source: "operator_reported", Scope: "whole_device", Criterion: "visible_exterior_enclosure_damage", Finding: finding, Status: status, StopReason: stop, ResponseComplete: complete, ObservationMilliseconds: 1}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	p.CoverageScope = app.CoverageFrozenPlan
	p.TestResults = []app.TestResult{
		{ID: "inspection.exterior.quick", Version: 1, Status: app.TestStatus(status), PlannedStatus: app.PlannedScheduled, PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: raw},
		{ID: "run.integrity.quick", Version: 1, Status: app.TestPass, PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: json.RawMessage("{\"status\":\"pass\"}")},
	}
	if err := app.ValidateCoverage(p); err != nil {
		t.Fatal("valid bundle inspection prerequisite", err)
	}
	got, err := app.ValidatedExteriorInspection(p)
	if err != nil || got == nil || *got != value {
		t.Fatal("literal observation prerequisite", err)
	}
	return p
}

func TestSignedInspectionWithAndWithoutCanonicalViews(t *testing.T) {
	for _, test := range []struct {
		name, finding, status, stop string
		complete                    bool
	}{
		{"pass", "no_defect_observed", "pass", "completed", true},
		{"defect", "defect_observed", "fail", "completed", true},
		{"stopped_defect", "defect_observed", "fail", "operator_cancelled", true},
		{"hard_defect", "defect_observed", "error", "input_error", true},
		{"incomplete", "not_inspected", "inconclusive", "input_ended", false},
		{"cancelled", "not_inspected", "aborted", "operator_cancelled", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := inspectionBundlePayload(t, test.finding, test.status, test.stop, test.complete)
			raw, err := json.Marshal(payload)
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
			for _, views := range []bool{false, true} {
				for _, selfIssued := range []bool{false, true} {
					var h, p []byte
					if views {
						h, p = html, pdf
					}
					archive, _, keys := independentSignedViewArchive(t, raw, h, p, product.Name, product.Version, selfIssued)
					artifact, err := verifyViewFixture(archive, keys, selfIssued)
					if err != nil || !bytes.Equal(artifact.InventoryJSON, raw) || !bytes.Equal(artifact.RenderedReport, h) || !bytes.Equal(artifact.RenderedPDF, p) {
						t.Fatal("independently signed inspection failed", err)
					}
				}
			}
		})
	}
}

func TestSignedInspectionRejectsForgedClaimsWithoutViews(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*app.InventoryPayload)
	}{
		{"promoted_defect", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("no_defect_observed"), []byte("defect_observed"), 1)
		}},
		{"missing_evidence", func(p *app.InventoryPayload) { p.TestResults[0].Evidence = nil }},
		{"wrong_row_status", func(p *app.InventoryPayload) { p.TestResults[0].Status = app.TestFail }},
		{"wrong_id", func(p *app.InventoryPayload) { p.TestResults[0].ID = "camera.quick" }},
		{"wrong_definition", func(p *app.InventoryPayload) { p.TestResults[0].Version = 2 }},
		{"unknown_schema", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("inspection/v1"), []byte("inspection/v99"), 1)
		}},
		{"duplicate_finding", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("\"finding\":"), []byte("\"finding\":\"defect_observed\",\"finding\":"), 1)
		}},
		{"case_field", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("\"finding\":"), []byte("\"Finding\":"), 1)
		}},
		{"excess_observation", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("\"observation_milliseconds\":1"), []byte("\"observation_milliseconds\":45001"), 1)
		}},
		{"unbound_plan", func(p *app.InventoryPayload) { p.TestResults[0].PlanID = "sha256:" + strings.Repeat("c", 64) }},
		{"noncanonical_scope", func(p *app.InventoryPayload) { p.CoverageScope = app.CoverageDirectCapture }},
		{"hidden_integrity_schema", func(p *app.InventoryPayload) {
			p.TestResults[1].Evidence = json.RawMessage("{\"status\":\"pass\",\"hidden\":\"hhsoft.pc-tester.exterior-inspection/v1\"}")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := inspectionBundlePayload(t, "no_defect_observed", "pass", "completed", true)
			before, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			for _, selfIssued := range []bool{false, true} {
				positive, envelope, keys := independentSignedViewArchive(t, before, nil, nil, product.Name, product.Version, selfIssued)
				if _, err := verifyViewFixture(positive, keys, selfIssued); err != nil {
					t.Fatal("signed positive prerequisite", err)
				}
				var output bytes.Buffer
				var writeErr error
				if selfIssued {
					writeErr = WriteSelfIssuedWithPDF(&output, before, envelope, nil, nil, keys[envelope.KeyID])
				} else {
					writeErr = WriteWithPDF(&output, before, envelope, nil, nil)
				}
				if writeErr != nil || !bytes.Equal(output.Bytes(), positive) {
					t.Fatal("producer positive prerequisite", writeErr)
				}
			}
			test.change(&payload)
			raw, err := json.Marshal(payload)
			if err != nil || bytes.Equal(raw, before) {
				t.Fatal("mutation missed valid signed fixture")
			}
			for _, selfIssued := range []bool{false, true} {
				// This helper creates and verifies the signature and canonical ZIP
				// independently of the bundle producer's manifest/view validation.
				archive, envelope, keys := independentSignedViewArchive(t, raw, nil, nil, product.Name, product.Version, selfIssued)
				if _, err := verifyViewFixture(archive, keys, selfIssued); err == nil {
					t.Fatal("authenticated malformed inspection was accepted")
				}
				var output bytes.Buffer
				var writeErr, digestErr error
				if selfIssued {
					writeErr = WriteSelfIssuedWithPDF(&output, raw, envelope, nil, nil, keys[envelope.KeyID])
					_, digestErr = SelfIssuedManifestDigestWithPDF(raw, nil, nil, keys[envelope.KeyID])
				} else {
					writeErr = WriteWithPDF(&output, raw, envelope, nil, nil)
					_, digestErr = ManifestDigestWithPDF(raw, nil, nil)
				}
				if writeErr == nil || digestErr == nil || output.Len() != 0 {
					t.Fatal("producer/digest path accepted malformed inspection")
				}
			}
		})
	}
}

func TestViewlessInspectionTokensCannotRecoverOpaqueHistoricalProfile(t *testing.T) {
	for _, test := range []struct{ name, raw string }{
		{"definition", "{\"schema_version\":1,\"test_results\":[{\"id\":\"inspection.exterior.quick\",\"version\":1,\"status\":\"pass\",\"evidence\":{}}]}"},
		{"case_id", "{\"schema_version\":1,\"test_results\":[{\"id\":\"INSPECTION.EXTERIOR.QUICK\"}]}"},
		{"escaped_id", "{\"schema_version\":1,\"test_results\":[{\"id\":\"inspection\\u002eexterior.quick\"}]}"},
		{"earlier_id", "{\"schema_version\":1,\"test_results\":[{\"id\":\"inspection.exterior.quick\",\"id\":\"camera.quick\"}]}"},
		{"earlier_container", "{\"schema_version\":1,\"test_results\":[{\"id\":\"inspection.exterior.quick\"}],\"test_results\":[]}"},
		{"wrong_container", "{\"schema_version\":1,\"test_results\":{\"nested\":{\"id\":\"inspection.exterior.quick\"}}}"},
		{"unknown_schema", "{\"schema_version\":1,\"test_results\":{\"schema\":\"hhsoft.pc-tester.exterior-inspection/v99\"}}"},
		{"case_schema", "{\"schema_version\":1,\"test_results\":{\"schema\":\"HHSOFT.PC-TESTER.EXTERIOR-INSPECTION/V1\"}}"},
		{"earlier_schema", "{\"schema_version\":1,\"test_results\":{\"schema\":\"hhsoft.pc-tester.exterior-inspection/v1\",\"schema\":\"historical\"}}"},
		{"escaped_schema", "{\"schema_version\":1,\"test_results\":{\"schema\":\"hhsoft.pc-tester.exterior\\u002dinspection/v1\"}}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, selfIssued := range []bool{false, true} {
				legacy := []byte("{\"schema_version\":1,\"historical\":\"opaque\"}")
				positive, _, keys := independentSignedViewArchive(t, legacy, nil, nil, product.Name, product.Version, selfIssued)
				if _, err := verifyViewFixture(positive, keys, selfIssued); err != nil {
					t.Fatal("literal opaque historical prerequisite changed", err)
				}
				raw := []byte(test.raw)
				if !json.Valid(raw) || !app.HasExteriorInspectionClaim(raw) {
					t.Fatal("claim counterexample invalid")
				}
				archive, _, keys := independentSignedViewArchive(t, raw, nil, nil, product.Name, product.Version, selfIssued)
				if _, err := verifyViewFixture(archive, keys, selfIssued); err == nil {
					t.Fatal("hidden inspection recovered opaque profile")
				}
			}
		})
	}
}

func TestInspectionCannotBeAddedToHistoricalSignedSessions(t *testing.T) {
	for _, name := range []string{"historical-session-v3", "session-v4-unavailable", "session-v5-keyboard-pass"} {
		t.Run(name, func(t *testing.T) {
			payload := thermalPayload(t, name)
			prior, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			positive, _, keys := independentSignedViewArchive(t, prior, nil, nil, product.Name, product.Version, false)
			if _, err := verifyViewFixture(positive, keys, false); err != nil {
				t.Fatal("literal historical prerequisite", err)
			}
			inspection := inspectionBundlePayload(t, "no_defect_observed", "pass", "completed", true).TestResults[0]
			found := false
			for _, row := range payload.TestResults {
				if row.ID == "run.integrity.quick" {
					found = true
					inspection.PlanID, inspection.HardwareGraph = row.PlanID, row.HardwareGraph
				}
			}
			if !found {
				t.Fatal("historical fixture lacks actual integrity")
			}
			payload.TestResults = append(payload.TestResults, inspection)
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			for _, selfIssued := range []bool{false, true} {
				archive, _, keys := independentSignedViewArchive(t, raw, nil, nil, product.Name, product.Version, selfIssued)
				if _, err := verifyViewFixture(archive, keys, selfIssued); err == nil {
					t.Fatal("historical session acquired a new inspection claim")
				}
			}
		})
	}
}

func TestSignedInspectionViewsMustMatchAdverseEvidence(t *testing.T) {
	before := inspectionBundlePayload(t, "no_defect_observed", "pass", "completed", true)
	after := inspectionBundlePayload(t, "defect_observed", "fail", "completed", true)
	oldHTML, err := render.InventoryHTML(before)
	if err != nil {
		t.Fatal(err)
	}
	oldPDF, err := render.InventoryPDF(before)
	if err != nil {
		t.Fatal(err)
	}
	newHTML, err := render.InventoryHTML(after)
	if err != nil {
		t.Fatal(err)
	}
	newPDF, err := render.InventoryPDF(after)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(oldHTML, newHTML) || bytes.Equal(oldPDF, newPDF) {
		t.Fatal("adverse fixture did not affect canonical views")
	}
	raw, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	for _, selfIssued := range []bool{false, true} {
		positive, _, keys := independentSignedViewArchive(t, raw, newHTML, newPDF, product.Name, product.Version, selfIssued)
		if _, err := verifyViewFixture(positive, keys, selfIssued); err != nil {
			t.Fatal("adverse canonical view prerequisite", err)
		}
		for _, views := range []struct{ html, pdf []byte }{{oldHTML, newPDF}, {newHTML, oldPDF}, {oldHTML, oldPDF}} {
			archive, _, keys := independentSignedViewArchive(t, raw, views.html, views.pdf, product.Name, product.Version, selfIssued)
			if _, err := verifyViewFixture(archive, keys, selfIssued); err == nil {
				t.Fatal("signed favorable view concealed adverse observation")
			}
		}
	}
}

func TestInspectionOrdinaryAndLinkedProfilesRetainStrictNoViewValidation(t *testing.T) {
	for _, linked := range []bool{false, true} {
		for _, self := range []bool{false, true} {
			for _, views := range []string{"none", "html", "pdf", "both"} {
				t.Run(fmt.Sprintf("linked=%t/self=%t/%s", linked, self, views), func(t *testing.T) {
					payload := inspectionBundlePayload(t, "defect_observed", "fail", "completed", true)
					raw, err := json.Marshal(payload)
					if err != nil {
						t.Fatal(err)
					}
					var previous *report.PreviousReport
					if linked {
						value := retestReference()
						previous = &value
					}
					var html, pdf []byte
					if views == "html" || views == "both" {
						if linked {
							html, err = render.InventoryRetestHTMLForVersion(payload, "0.0.1-fixture", *previous)
						} else {
							html, err = render.InventoryHTMLForVersion(payload, "0.0.1-fixture")
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					if views == "pdf" || views == "both" {
						if linked {
							pdf, err = render.InventoryRetestPDFForVersion(payload, "0.0.1-fixture", *previous)
						} else {
							pdf, err = render.InventoryPDFForVersion(payload, "0.0.1-fixture")
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					archive, envelope, keys := independentlySignedRetest(t, raw, html, pdf, previous, self, nil, nil)
					artifact, err := verifyViewFixture(archive, keys, self)
					if err != nil || !bytes.Equal(artifact.InventoryJSON, raw) || !bytes.Equal(artifact.RenderedReport, html) || !bytes.Equal(artifact.RenderedPDF, pdf) {
						t.Fatal("independent report profile prerequisite", err)
					}
					if linked {
						if artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != *previous {
							t.Fatal("inspection lost exact signed predecessor")
						}
					} else if artifact.Manifest.PreviousReport != nil {
						t.Fatal("ordinary inspection invented predecessor")
					}
					var output bytes.Buffer
					var public []byte
					if self {
						public = keys[envelope.KeyID]
					}
					if linked {
						err = WriteRetestWithPDF(&output, raw, envelope, html, pdf, *previous, public)
					} else if self {
						err = WriteSelfIssuedWithPDF(&output, raw, envelope, html, pdf, public)
					} else {
						err = WriteWithPDF(&output, raw, envelope, html, pdf)
					}
					if err != nil || !bytes.Equal(output.Bytes(), archive) {
						t.Fatal("producer did not preserve independent profile", err)
					}
					// Re-sign malformed evidence with no views, so renderer rejection is
					// not the only path preventing favorable promotion of a defect.
					payload.TestResults[0].Evidence = bytes.Replace(payload.TestResults[0].Evidence, []byte("\"status\":\"fail\""), []byte("\"status\":\"pass\""), 1)
					payload.TestResults[0].Status = app.TestPass
					malformed, err := json.Marshal(payload)
					if err != nil || bytes.Equal(malformed, raw) {
						t.Fatal("adverse mutation missed")
					}
					forged, _, keys := independentlySignedRetest(t, malformed, nil, nil, previous, self, nil, nil)
					if _, err := verifyViewFixture(forged, keys, self); err == nil {
						t.Fatal("viewless signed defect was promoted")
					}
				})
			}
		}
	}
}
