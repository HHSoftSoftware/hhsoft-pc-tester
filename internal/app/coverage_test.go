package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func coverageFixture() InventoryPayload {
	return InventoryPayload{CoverageScope: CoverageFrozenPlan, TestResults: []TestResult{
		{ID: "camera.quick", Version: 3, Status: TestSkip, PlannedStatus: PlannedUnsupported,
			PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: json.RawMessage(`{"status":"skip"}`)},
		{ID: "run.integrity.quick", Version: 1, Status: TestPass,
			PlanID: "sha256:" + strings.Repeat("a", 64), HardwareGraph: "hmac-sha256:" + strings.Repeat("b", 64), Evidence: json.RawMessage(`{"status":"pass"}`)},
	}}
}

func TestCoverageRejectsContradictoryOrUnknownMetadata(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		change func(*InventoryPayload)
	}{
		{"unknown scope", func(p *InventoryPayload) { p.CoverageScope = "complete" }},
		{"scope case alias", func(p *InventoryPayload) { p.CoverageScope = "Frozen_Plan" }},
		{"unknown planned status", func(p *InventoryPayload) { p.TestResults[0].PlannedStatus = "passed" }},
		{"planned status case alias", func(p *InventoryPayload) { p.TestResults[0].PlannedStatus = "Unsupported" }},
		{"unsupported pass", func(p *InventoryPayload) { p.TestResults[0].Status = TestPass }},
		{"excluded fail", func(p *InventoryPayload) {
			p.TestResults[0].PlannedStatus = PlannedExcluded
			p.TestResults[0].Status = TestFail
		}},
		{"absent error", func(p *InventoryPayload) {
			p.TestResults[0].PlannedStatus = PlannedNotPresent
			p.TestResults[0].Status = TestError
		}},
		{"driver skip", func(p *InventoryPayload) { p.TestResults[0].PlannedStatus = PlannedMissingDriver }},
		{"missing scope", func(p *InventoryPayload) { p.CoverageScope = "" }},
		{"direct capture plan", func(p *InventoryPayload) { p.CoverageScope = CoverageDirectCapture }},
		{"missing planned status", func(p *InventoryPayload) { p.TestResults[0].PlannedStatus = "" }},
		{"missing binding", func(p *InventoryPayload) { p.TestResults[0].PlanID = "" }},
		{"different plan", func(p *InventoryPayload) { p.TestResults[0].PlanID = "sha256:" + strings.Repeat("c", 64) }},
		{"different graph", func(p *InventoryPayload) { p.TestResults[0].HardwareGraph = "hmac-sha256:" + strings.Repeat("c", 64) }},
		{"duplicate result", func(p *InventoryPayload) { p.TestResults = append(p.TestResults, p.TestResults[0]) }},
		{"no planned results", func(p *InventoryPayload) { p.TestResults = p.TestResults[1:] }},
		{"missing integrity", func(p *InventoryPayload) { p.TestResults = p.TestResults[:1] }},
		{"invented integrity plan row", func(p *InventoryPayload) { p.TestResults[1].PlannedStatus = PlannedScheduled }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			payload := coverageFixture()
			fixture.change(&payload)
			if ValidateCoverage(payload) == nil {
				t.Fatal("contradictory report coverage accepted")
			}
		})
	}
}

func TestCoveragePreservesHistoricalAndLimitedCaptureSemantics(t *testing.T) {
	if err := ValidateCoverage(InventoryPayload{}); err != nil {
		t.Fatalf("historical missing metadata rejected: %v", err)
	}
	for _, pair := range []struct {
		planned PlannedStatus
		outcome TestStatus
	}{
		{PlannedScheduled, TestFail}, {PlannedScheduled, TestError}, {PlannedScheduled, TestAborted},
		{PlannedScheduled, TestPass}, {PlannedScheduled, TestSkip}, {PlannedScheduled, TestInconclusive},
		{PlannedExcluded, TestSkip}, {PlannedNotPresent, TestSkip},
		{PlannedUnsupported, TestSkip}, {PlannedMissingDriver, TestInconclusive},
	} {
		payload := coverageFixture()
		payload.TestResults[0].PlannedStatus, payload.TestResults[0].Status = pair.planned, pair.outcome
		if err := ValidateCoverage(payload); err != nil {
			t.Fatalf("valid %s/%s rejected: %v", pair.planned, pair.outcome, err)
		}
	}
	payload := InventoryPayload{CoverageScope: CoverageDirectCapture, TestResults: []TestResult{
		{ID: "cpu.quick", Version: 1, Status: TestFail, Evidence: json.RawMessage(`{"status":"fail"}`)},
	}}
	if err := ValidateCoverage(payload); err != nil {
		t.Fatal(err)
	}
	result := coverageFixture().TestResults[0]
	result.Status = TestPass
	if _, err := BuildReportPayload(inventory.NewSnapshot(nil), make([]byte, 32), "fixture", []TestResult{result}); err == nil {
		t.Fatal("payload builder accepted unsupported planned pass")
	}
}
