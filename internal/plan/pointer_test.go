package plan

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestPointerPlanSchedulesEveryPointerWithoutEnablingTouchInput(t *testing.T) {
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentInput, "input:0", map[string]string{"category": "pointer", "handlers": "mouse0 event2"}),
		inventory.NewComponent(inventory.ComponentInput, "input:1", map[string]string{"category": "pointer", "handlers": "mouse1 event5"}),
		inventory.NewComponent(inventory.ComponentInput, "input:2", map[string]string{"category": "keyboard", "handlers": "kbd event1"}),
		inventory.NewComponent(inventory.ComponentInput, "input:3", map[string]string{"category": "touchpad", "handlers": "mouse2 event7"}),
		inventory.NewComponent(inventory.ComponentInput, "input:4", map[string]string{"category": "touchscreen", "handlers": "event8"}),
	})
	for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
		for _, profile := range []Profile{ProfileQuick, ProfileStandard, ProfileExtended, ProfileDestructive} {
			t.Run(architecture+"/"+string(profile), func(t *testing.T) {
				p := buildValidationPlan(t, snapshot, profile, architecture)
				pointer := findPlanTest(t, &p, "input.pointer.quick")
				if pointer.Status != TestScheduled || pointer.Destructive || pointer.Version != 2 ||
					pointer.EstimatedSeconds != 30 || pointer.TimeoutSeconds != 90 ||
					!slices.Equal(pointer.ComponentIDs, []string{"input:0", "input:1"}) {
					t.Fatalf("incorrect pointer plan: %+v", pointer)
				}
				if len(p.Tests) != 21 {
					t.Fatalf("catalog row count changed: %d", len(p.Tests))
				}
				assertTestStatus(t, p, "input.keyboard.quick", TestScheduled)
				for _, id := range []string{"input.touchpad.quick", "input.touchscreen.quick"} {
					assertTestStatus(t, p, id, TestUnsupported)
				}
				if _, err := Decode(rehashPlanForTest(t, &p)); err != nil {
					t.Fatalf("valid pointer plan rejected: %v", err)
				}
			})
		}
	}
}

func TestPointerSelectionRetainsReadinessAndAbsence(t *testing.T) {
	for _, present := range []bool{false, true} {
		snapshot := inventory.NewSnapshot(nil)
		if present {
			snapshot = inventory.NewSnapshot([]inventory.Component{
				inventory.NewComponent(inventory.ComponentInput, "input:0", map[string]string{"category": "pointer", "handlers": "mouse0 event0"}),
			})
		}
		p, err := BuildWithSelection(snapshot, ProfileQuick, "legacy-x86", bytes.Repeat([]byte{11}, 32), "fixture", []string{"input.pointer.quick"})
		if err != nil {
			t.Fatal(err)
		}
		if present {
			assertTestStatus(t, p, "input.pointer.quick", TestExcluded)
			assertTestStatus(t, p, "input.readiness.quick", TestScheduled)
		} else {
			assertTestStatus(t, p, "input.pointer.quick", TestNotPresent)
			assertTestStatus(t, p, "input.readiness.quick", TestNotPresent)
		}
		if !slices.Equal(p.ExcludedTests, []string{"input.pointer.quick"}) {
			t.Fatal("pointer exclusion intent was not frozen")
		}
	}
}

func TestPointerCatalogRequiresFreshFrozenPlan(t *testing.T) {
	current := buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "x86_64")
	findPlanTest(t, &current, "input.pointer.quick").Version = 1
	if _, err := Decode(rehashPlanForTest(t, &current)); err == nil {
		t.Fatal("accepted the movement/buttons-only definition in a current execution plan")
	}
	p := buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "x86_64")
	catalog, _, err := loadDefinitions(definitionData)
	if err != nil {
		t.Fatal(err)
	}
	for index := range catalog.Tests {
		if catalog.Tests[index].ID == "input.pointer.quick" {
			catalog.Tests[index].Implemented = false
		}
	}
	oldDefinitions, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	_, oldDigest, err := loadDefinitions(oldDefinitions)
	if err != nil || oldDigest == p.DefinitionsDigest {
		t.Fatal("prior catalog fixture did not change definitions")
	}
	p.DefinitionsDigest = oldDigest
	findPlanTest(t, &p, "input.pointer.quick").Status = TestUnsupported
	if _, err := Decode(rehashPlanForTest(t, &p)); err == nil {
		t.Fatal("accepted an old catalog plan after the pointer implementation changed")
	}
	p = buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "x86_64")
	findPlanTest(t, &p, "input.pointer.quick").Status = TestUnsupported
	if _, err := Decode(rehashPlanForTest(t, &p)); err == nil {
		t.Fatal("accepted a rehashed pointer plan with a stale unsupported disposition")
	}
}

func TestPointerKernelBitmapLayoutIsFrozenInHardwareGraph(t *testing.T) {
	build := func(width string) Plan {
		return buildValidationPlan(t, inventory.NewSnapshot([]inventory.Component{
			inventory.NewComponent(inventory.ComponentInput, "input:0", map[string]string{
				"category": "pointer", "handlers": "mouse0 event0", "bitmap_word_bits": width,
			}),
		}), ProfileQuick, "x86")
	}
	first, second := build("32"), build("64")
	if first.HardwareGraph.Value == second.HardwareGraph.Value || first.ID == second.ID ||
		first.HardwareGraph.Components[0].Value == second.HardwareGraph.Components[0].Value {
		t.Fatal("kernel bitmap layout did not change the frozen hardware binding")
	}
}
