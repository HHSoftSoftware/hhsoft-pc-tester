package plan

import (
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"testing"
)

func TestMemoryInversionDefinitionKeepsExistingWorkerBudgetAndCatalog(t *testing.T) {
	for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
		p := buildValidationPlan(t, inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentMemory, "memory:system", nil)}), ProfileQuick, architecture)
		row := findPlanTest(t, &p, "memory.quick")
		if len(p.Tests) != 22 || row.Version != 2 || row.Status != TestScheduled || row.Destructive || row.TimeoutSeconds != 30 || row.EstimatedSeconds != 10 {
			t.Fatal("RAM catalog scope or budget changed")
		}
		ids, err := SelectableQuickTests(architecture)
		wanted := 14
		if architecture == "legacy-x86" {
			wanted = 12
		}
		if err != nil || len(ids) != wanted {
			t.Fatal("selectable worker count changed")
		}
		if _, err := Decode(rehashPlanForTest(t, &p)); err != nil {
			t.Fatal(err)
		}
	}
}
