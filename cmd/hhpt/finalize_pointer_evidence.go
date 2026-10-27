package main

import (
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/pointerevidence"
)

// Command finalization uses the same strict pointer evidence and binding rules
// as offline report consumers, including retained passes in partial results.
func validatePointerPlanEvidence(encoded []byte, test plan.Test, frozen, final plan.Plan, snapshot inventory.Snapshot) bool {
	value, err := pointerevidence.Decode(encoded)
	return err == nil && pointerevidence.ValidatePlanBinding(value, test, frozen, final, snapshot) == nil
}
