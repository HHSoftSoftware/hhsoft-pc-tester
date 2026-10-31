package plan

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func touchpadPlanSnapshot(attributes map[string]string, kind inventory.ComponentType) inventory.Snapshot {
	if kind == "" {
		return inventory.NewSnapshot(nil)
	}
	return inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(kind, "input:touchpad-plan-fixture", attributes),
	})
}

func TestTouchpadCatalogUsesDerivedCandidateAuthority(t *testing.T) {
	cases := []struct {
		name       string
		kind       inventory.ComponentType
		attributes map[string]string
		present    bool
	}{
		{"absent", "", nil, false},
		{"category_only", inventory.ComponentInput, map[string]string{"category": "touchpad"}, false},
		{"derived_marker", inventory.ComponentInput, map[string]string{"category": "touchpad", "touchpad_candidate": "true"}, true},
		{"marker_without_category", inventory.ComponentInput, map[string]string{"touchpad_candidate": "true"}, true},
		{"false_marker", inventory.ComponentInput, map[string]string{"category": "touchpad", "touchpad_candidate": "false"}, false},
		{"noncanonical_marker", inventory.ComponentInput, map[string]string{"category": "touchpad", "touchpad_candidate": "TRUE"}, false},
		{"wrong_component_type", inventory.ComponentUSB, map[string]string{"touchpad_candidate": "true"}, false},
		{"unknown_axes_remain_candidates", inventory.ComponentInput, map[string]string{"touchpad_candidate": "true", "absolute_capabilities": "7"}, true},
	}
	for _, example := range cases {
		for _, profile := range []Profile{ProfileQuick, ProfileStandard} {
			for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
				t.Run(example.name+"/"+string(profile)+"/"+architecture, func(t *testing.T) {
					frozen := buildValidationPlan(t, touchpadPlanSnapshot(example.attributes, example.kind), profile, architecture)
					capability := findPlanCapability(t, &frozen, "input.touchpad")
					row := findPlanTest(t, &frozen, "input.touchpad.quick")
					status, capabilityStatus := TestNotPresent, CapabilityAbsent
					if example.present {
						status, capabilityStatus = TestScheduled, CapabilityPresent
					}
					if capability.Status != capabilityStatus || row.Status != status || row.Version != 2 ||
						row.Capability != "input.touchpad" || row.Destructive || row.EstimatedSeconds != 30 || row.TimeoutSeconds != 90 {
						t.Fatalf("unexpected touchpad row: %#v capability=%#v", row, capability)
					}
					if example.present {
						if !slices.Equal(row.ComponentIDs, []string{"input:touchpad-plan-fixture"}) ||
							!slices.Equal(capability.ComponentIDs, row.ComponentIDs) {
							t.Fatal("derived candidate was not bound exactly once")
						}
					} else if len(row.ComponentIDs) != 0 || len(capability.ComponentIDs) != 0 {
						t.Fatal("noncandidate acquired a component binding")
					}
					count := 0
					for _, candidate := range frozen.Tests {
						if candidate.ID == "input.touchpad.quick" {
							count++
						}
					}
					if count != 1 || len(frozen.Tests) != 22 {
						t.Fatal("current catalog changed row identity or count")
					}
					encoded, err := json.Marshal(frozen)
					if err != nil {
						t.Fatal(err)
					}
					decoded, err := Decode(encoded)
					if err != nil || decoded.ID != frozen.ID {
						t.Fatal("real current catalog plan did not round trip")
					}
				})
			}
		}
	}
}

func TestTouchpadCurrentSelectionRetainsUnavailableDispositions(t *testing.T) {
	for _, profile := range []Profile{ProfileQuick, ProfileStandard} {
		for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
			t.Run(string(profile)+"/"+architecture, func(t *testing.T) {
				selectable, err := SelectableQuickTests(architecture)
				if err != nil {
					t.Fatal(err)
				}
				count := 0
				for _, id := range selectable {
					if id == "input.touchpad.quick" {
						count++
					}
				}
				if count != 1 {
					t.Fatal("current touchpad selection is missing or duplicated")
				}
				for _, present := range []bool{false, true} {
					snapshot := inventory.NewSnapshot(nil)
					if present {
						snapshot = touchpadPlanSnapshot(map[string]string{"touchpad_candidate": "true"}, inventory.ComponentInput)
					}
					excluded := []string{"input.touchpad.quick"}
					if profile == ProfileStandard {
						if _, err := BuildWithSelection(snapshot, profile, architecture, make([]byte, 32), "touchpad-selection", excluded); err == nil {
							t.Fatal("standard profile accepted Quick-only exclusions")
						}
						excluded = nil
					}
					frozen, err := BuildWithSelection(snapshot, profile, architecture, make([]byte, 32), "touchpad-selection", excluded)
					if err != nil {
						t.Fatal(err)
					}
					want := TestNotPresent
					if present {
						want = TestScheduled
						if profile == ProfileQuick {
							want = TestExcluded
						}
					}
					if findPlanTest(t, &frozen, "input.touchpad.quick").Status != want ||
						!slices.Equal(frozen.ExcludedTests, excluded) {
						t.Fatal("selection changed the exclusion or unavailable disposition")
					}
					encoded, err := json.Marshal(frozen)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := Decode(encoded); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestTouchpadCatalogRequiresFreshFrozenExecutionPlan(t *testing.T) {
	current := func() Plan {
		return buildValidationPlan(t, touchpadPlanSnapshot(map[string]string{
			"category": "touchpad", "touchpad_candidate": "true",
		}, inventory.ComponentInput), ProfileQuick, "x86_64")
	}
	t.Run("current", func(t *testing.T) {
		value := current()
		if _, err := Decode(rehashPlanForTest(t, &value)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("definition_downgrade", func(t *testing.T) {
		value := current()
		findPlanTest(t, &value, "input.touchpad.quick").Version = 1
		if _, err := Decode(rehashPlanForTest(t, &value)); err == nil {
			t.Fatal("accepted a rehashed version1 execution claim")
		}
	})
	t.Run("stale_unsupported", func(t *testing.T) {
		value := current()
		findPlanTest(t, &value, "input.touchpad.quick").Status = TestUnsupported
		if _, err := Decode(rehashPlanForTest(t, &value)); err == nil {
			t.Fatal("accepted a rehashed stale unsupported disposition")
		}
	})
	t.Run("historical_catalog_digest", func(t *testing.T) {
		value := current()
		catalog, _, err := loadDefinitions(definitionData)
		if err != nil {
			t.Fatal(err)
		}
		for index := range catalog.Capabilities {
			if catalog.Capabilities[index].ID == "input.touchpad" {
				catalog.Capabilities[index].Providers = []componentMatch{{
					Type: inventory.ComponentInput, Attribute: "category", Equals: "touchpad",
				}}
			}
		}
		for index := range catalog.Tests {
			if catalog.Tests[index].ID == "input.touchpad.quick" {
				catalog.Tests[index].Version = 1
				catalog.Tests[index].Implemented = false
			}
		}
		encoded, err := json.Marshal(catalog)
		if err != nil {
			t.Fatal(err)
		}
		_, oldDigest, err := loadDefinitions(encoded)
		if err != nil || oldDigest == value.DefinitionsDigest {
			t.Fatal("historical catalog prerequisite did not change its digest")
		}
		value.DefinitionsDigest = oldDigest
		row := findPlanTest(t, &value, "input.touchpad.quick")
		row.Version, row.Status = 1, TestUnsupported
		if _, err := Decode(rehashPlanForTest(t, &value)); err == nil {
			t.Fatal("historical catalog was accepted for current execution")
		}
	})
}
