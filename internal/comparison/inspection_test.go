package comparison

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

const inspectionPartIdentity = "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af"
const inspectionSystemIdentity = "hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449"
const inspectionKeyIdentity = "hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44"

func inspectionComparisonReport(t *testing.T, after bool) Report {
	t.Helper()
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"wwid": "fixture-exterior-disk", "model": "Fixture disk"}),
	})
	payload, err := app.BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x39}, 32), "inspection-fixture")
	if err != nil {
		t.Fatal(err)
	}
	wantFingerprint := app.SystemFingerprint{Algorithm: "hmac-sha256", KeyEpoch: "inspection-fixture", KeyID: inspectionKeyIdentity, Value: inspectionSystemIdentity, IdentifiedParts: 1}
	wantRecord := app.ComponentRecord{Type: inventory.ComponentBlock, ID: "block:sda", Attributes: []inventory.Attribute{{Name: "model", Value: "Fixture disk"}}, Identity: app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "inspection-fixture", Source: "wwid", Value: inspectionPartIdentity, Confidence: "durable"}}
	if payload.SystemFingerprint != wantFingerprint || !reflect.DeepEqual(payload.Components, []app.ComponentRecord{wantRecord}) {
		t.Fatal("identified component and literal HMAC prerequisite failed")
	}
	reference := ReportReference{SessionID: "11111111-1111-4111-8111-111111111111", GeneratedTime: "2026-01-01T00:00:00Z", SoftwareVersion: "0.0.1-fixture", SignerKeyID: "fixture-signer", ManifestDigest: "sha256:" + strings.Repeat("1", 64), SystemFingerprint: wantFingerprint, ComponentCount: 1}
	if after {
		reference.SessionID = "22222222-2222-4222-8222-222222222222"
		reference.GeneratedTime = "2026-01-02T00:00:00Z"
		reference.ManifestDigest = "sha256:" + strings.Repeat("2", 64)
	}
	report := Report{Reference: reference, Inventory: payload}
	if err := validateReport(report); err != nil {
		t.Fatal("complete comparison prerequisite", err)
	}
	return report
}

func addComparisonInspection(t *testing.T, target *Report, finding, status, reason string, complete bool) {
	t.Helper()
	value := exteriorinspection.Result{Schema: "hhsoft.pc-tester.exterior-inspection/v1", Source: "operator_reported", Scope: "whole_device", Criterion: "visible_exterior_enclosure_damage", Finding: finding, Status: status, StopReason: reason, ResponseComplete: complete, ObservationMilliseconds: 25}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	target.Inventory.CoverageScope = app.CoverageFrozenPlan
	target.Inventory.TestResults = []app.TestResult{
		{ID: "inspection.exterior.quick", Version: 1, Status: app.TestStatus(status), PlannedStatus: app.PlannedScheduled, PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: raw},
		{ID: "run.integrity.quick", Version: 1, Status: app.TestPass, PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: json.RawMessage(`{"status":"pass"}`)},
	}
	if err := validateReport(*target); err != nil {
		t.Fatal("valid inspection comparison prerequisite", err)
	}
	if got, err := app.ValidatedExteriorInspection(target.Inventory); err != nil || got == nil || *got != value {
		t.Fatal("literal inspection prerequisite", err)
	}
}

func unchangedInspectionComparison(before, after Report) Result {
	component := Component{Type: inventory.ComponentBlock, ID: "block:sda", IdentitySource: "wwid", Identity: inspectionPartIdentity, IdentityConfidence: "durable", Attributes: []inventory.Attribute{{Name: "model", Value: "Fixture disk"}}}
	return Result{Schema: "hhsoft.pc-tester.hardware-comparison/v1", Policy: "hhsoft.parts-swap.v1", Status: StatusUnchanged, Comparable: true, Complete: true, Before: before.Reference, After: after.Reference, Summary: Summary{Matched: 1}, Matched: []Match{{Before: component, After: component, LocationChange: false}}}
}

func TestCompareExteriorInspectionDoesNotChangeComponentIdentity(t *testing.T) {
	for _, test := range []struct {
		name, finding, status, reason string
		complete                      bool
	}{
		{"absent", "", "", "", false},
		{"favorable", "no_defect_observed", "pass", "completed", true},
		{"adverse", "defect_observed", "fail", "completed", true},
		{"adverse_error", "defect_observed", "error", "input_error", true},
		{"adverse_stopped", "defect_observed", "fail", "operator_cancelled", true},
		{"incomplete", "not_inspected", "inconclusive", "input_ended", false},
		{"aborted", "not_inspected", "aborted", "operator_cancelled", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, after := inspectionComparisonReport(t, false), inspectionComparisonReport(t, true)
			addComparisonInspection(t, &before, "no_defect_observed", "pass", "completed", true)
			if test.name != "absent" {
				addComparisonInspection(t, &after, test.finding, test.status, test.reason, test.complete)
			}
			initial, err := json.Marshal([]Report{before, after})
			if err != nil {
				t.Fatal(err)
			}
			got, err := Compare(before, after)
			if err != nil || !reflect.DeepEqual(got, unchangedInspectionComparison(before, after)) {
				t.Fatal("inspection changed identity classification, exact references or matched fields", err, got)
			}
			final, err := json.Marshal([]Report{before, after})
			if err != nil || !bytes.Equal(initial, final) {
				t.Fatal("comparison mutated its source reports", err)
			}
		})
	}
}

func TestCompareRejectsForgedExteriorInspectionOnEitherSide(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*app.InventoryPayload)
	}{
		{"forged_pass", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("no_defect_observed"), []byte("defect_observed"), 1)
		}},
		{"mismatched_status", func(p *app.InventoryPayload) { p.TestResults[0].Status = app.TestFail }},
		{"wrong_schema", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte("inspection/v1"), []byte("inspection/v9"), 1)
		}},
		{"duplicate_field", func(p *app.InventoryPayload) {
			p.TestResults[0].Evidence = bytes.Replace(p.TestResults[0].Evidence, []byte(`"finding":`), []byte(`"finding":"not_inspected","finding":`), 1)
		}},
		{"missing_plan", func(p *app.InventoryPayload) { p.TestResults[0].PlanID = "" }},
		{"unrelated_integrity", func(p *app.InventoryPayload) { p.TestResults[1].Status = app.TestSkip }},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, side := range []string{"before", "after"} {
				before, after := inspectionComparisonReport(t, false), inspectionComparisonReport(t, true)
				addComparisonInspection(t, &before, "no_defect_observed", "pass", "completed", true)
				addComparisonInspection(t, &after, "no_defect_observed", "pass", "completed", true)
				if got, err := Compare(before, after); err != nil || !reflect.DeepEqual(got, unchangedInspectionComparison(before, after)) {
					t.Fatal("exact comparison prerequisite", err)
				}
				target := &after.Inventory
				if side == "before" {
					target = &before.Inventory
				}
				prior, err := json.Marshal(target)
				if err != nil {
					t.Fatal(err)
				}
				test.mutate(target)
				changed, err := json.Marshal(target)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Equal(prior, changed) {
					t.Fatal("fault mutation was ineffective")
				}
				if got, err := Compare(before, after); err == nil || !reflect.DeepEqual(got, Result{}) {
					t.Fatal("invalid inspection acquired comparison credit", side, err)
				}
			}
		})
	}
}
