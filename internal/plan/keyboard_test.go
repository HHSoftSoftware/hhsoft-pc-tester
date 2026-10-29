package plan

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestKeyboardDefinitionSchedulesEveryKeyboardAndKeepsCatalogSize(t *testing.T) {
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentInput, "input:0", map[string]string{"category": "keyboard", "handlers": "kbd event2"}),
		inventory.NewComponent(inventory.ComponentInput, "input:1", map[string]string{"category": "keyboard", "handlers": "kbd event7"}),
		inventory.NewComponent(inventory.ComponentInput, "input:2", map[string]string{"category": "pointer", "handlers": "mouse0 event9"}),
	})
	for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
		for _, profile := range []Profile{ProfileQuick, ProfileStandard, ProfileExtended, ProfileDestructive} {
			t.Run(architecture+"/"+string(profile), func(t *testing.T) {
				p := buildValidationPlan(t, snapshot, profile, architecture)
				row := findPlanTest(t, &p, "input.keyboard.quick")
				if len(p.Tests) != 22 || row.Version != 2 || row.Status != TestScheduled || row.Destructive || row.TimeoutSeconds != 90 || row.EstimatedSeconds != 30 || !slices.Equal(row.ComponentIDs, []string{"input:0", "input:1"}) {
					t.Fatal("bounded keyboard row or exact coverage changed")
				}
				if _, err := Decode(rehashPlanForTest(t, &p)); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestKeyboardSelectionAndOldCatalogCannotClaimNewExecution(t *testing.T) {
	for _, present := range []bool{false, true} {
		components := []inventory.Component{}
		if present {
			components = append(components, inventory.NewComponent(inventory.ComponentInput, "input:0", map[string]string{"category": "keyboard"}))
		}
		p, err := BuildWithSelection(inventory.NewSnapshot(components), ProfileQuick, "legacy-x86", bytes.Repeat([]byte{11}, 32), "fixture", []string{"input.keyboard.quick"})
		if err != nil {
			t.Fatal(err)
		}
		want := TestNotPresent
		if present {
			want = TestExcluded
		}
		assertTestStatus(t, p, "input.keyboard.quick", want)
		if !slices.Equal(p.ExcludedTests, []string{"input.keyboard.quick"}) {
			t.Fatal("exclusion intent lost")
		}
	}
	p := buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "x86_64")
	catalog, _, err := loadDefinitions(definitionData)
	if err != nil {
		t.Fatal(err)
	}
	for index := range catalog.Tests {
		if catalog.Tests[index].ID == "input.keyboard.quick" {
			catalog.Tests[index].Version = 1
			catalog.Tests[index].Implemented = false
		}
	}
	raw, _ := json.Marshal(catalog)
	_, oldDigest, err := loadDefinitions(raw)
	if err != nil || oldDigest == p.DefinitionsDigest {
		t.Fatal("old catalog fixture unchanged")
	}
	p.DefinitionsDigest = oldDigest
	row := findPlanTest(t, &p, "input.keyboard.quick")
	row.Version = 1
	row.Status = TestUnsupported
	if _, err := Decode(rehashPlanForTest(t, &p)); err == nil {
		t.Fatal("old catalog plan authorized new keyboard execution")
	}
	p = buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "x86_64")
	findPlanTest(t, &p, "input.keyboard.quick").Version = 1
	if _, err := Decode(rehashPlanForTest(t, &p)); err == nil {
		t.Fatal("version-one row accepted in current frozen plan")
	}
}
