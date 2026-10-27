package comparison

import (
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"testing"
)

func cpuComparisonFixture(t *testing.T, name string) Report {
	value := pointerComparisonFixture(t, "pointer-v1-standalone-pass")
	cpu := pointerComparisonFixture(t, name)
	for _, row := range cpu.Inventory.TestResults {
		if row.ID == "cpu.quick" {
			row.PlanID, row.HardwareGraph, row.PlannedStatus = "", "", ""
			value.Inventory.TestResults = []app.TestResult{row}
		}
	}
	value.Inventory.CoverageScope = app.CoverageDirectCapture
	value.Inventory.Components[0].Type, value.Inventory.Components[0].ID = inventory.ComponentCPU, "cpu:0"
	value.Inventory.Components[0].Attributes = nil
	return value
}

func TestCPUScalarComparisonUsesSharedValidation(t *testing.T) {
	for _, name := range []string{"cpu-v3-direct-pass", "session-v4-local-heat"} {
		before := cpuComparisonFixture(t, name)
		if _, err := Compare(before, before); err != nil {
			t.Fatal("valid CPU comparison rejected", err)
		}
		if name == "session-v4-local-heat" {
			continue
		}
		after := cpuComparisonFixture(t, name)
		for index := range after.Inventory.TestResults {
			if after.Inventory.TestResults[index].ID == "cpu.quick" {
				after.Inventory.TestResults[index].Version = 1
			}
		}
		if _, err := Compare(before, after); err == nil {
			t.Fatal("CPU definition downgrade compared")
		}
	}
}
