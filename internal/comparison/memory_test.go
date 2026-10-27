package comparison

import (
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestMemoryComparisonRequiresCurrentDefinitionAndEvidence(t *testing.T) {
	before := pointerComparisonFixture(t, "pointer-v1-standalone-pass")
	memory := pointerComparisonFixture(t, "memory-v3-direct-pass")
	before.Inventory.TestResults = memory.Inventory.TestResults
	before.Inventory.CoverageScope = app.CoverageDirectCapture
	before.Inventory.Components[0].Type, before.Inventory.Components[0].ID = inventory.ComponentMemory, "memory:0"
	before.Inventory.Components[0].Attributes = nil
	if _, err := Compare(before, before); err != nil {
		t.Fatal("valid memory comparison rejected", err)
	}
	after := before
	after.Inventory.TestResults = append([]app.TestResult(nil), before.Inventory.TestResults...)
	after.Inventory.TestResults[0].Version = 1
	if _, err := Compare(before, after); err == nil {
		t.Fatal("memory definition downgrade compared")
	}
}
