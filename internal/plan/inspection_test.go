package plan

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestInspectionWholeDevicePlanNeedsNoInventedHardware(t *testing.T) {
	for _, profile := range []Profile{ProfileQuick, ProfileStandard, ProfileExtended, ProfileDestructive} {
		for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
			t.Run(string(profile)+"/"+architecture, func(t *testing.T) {
				value, err := Build(inventory.NewSnapshot(nil), profile, architecture, bytes.Repeat([]byte{11}, 32), "fixture")
				if err != nil {
					t.Fatal(err)
				}
				capability := findPlanCapability(t, &value, "operator.exterior")
				test := findPlanTest(t, &value, "inspection.exterior.quick")
				if value.SchemaVersion != 3 || capability.Status != CapabilityPresent ||
					capability.Applicability != "whole_device_operator" || len(capability.ComponentIDs) != 0 ||
					test.Version != 2 || test.Capability != "operator.exterior" || test.Applicability != "whole_device_operator" ||
					test.Status != TestScheduled || test.Destructive || len(test.ComponentIDs) != 0 ||
					test.EstimatedSeconds != 20 || test.TimeoutSeconds != 45 || len(value.HardwareGraph.Components) != 0 {
					t.Fatal("manual capability fabricated hardware or changed its finite scope")
				}
				if _, err := Decode(rehashPlanForTest(t, &value)); err != nil {
					t.Fatal(err)
				}
				hardware := findPlanCapability(t, &value, "cpu")
				encoded, err := json.Marshal(hardware)
				if err != nil || bytes.Contains(encoded, []byte("applicability")) || hardware.Status != CapabilityAbsent {
					t.Fatal("existing hardware omission or availability semantics changed")
				}
			})
		}
	}
	for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
		selectable, err := SelectableQuickTests(architecture)
		if err != nil || !slices.Contains(selectable, "inspection.exterior.quick") {
			t.Fatal("manual row is not selectable")
		}
		value, err := BuildWithSelection(inventory.NewSnapshot(nil), ProfileQuick, architecture, bytes.Repeat([]byte{11}, 32), "fixture", []string{"inspection.exterior.quick"})
		if err != nil || findPlanTest(t, &value, "inspection.exterior.quick").Status != TestExcluded ||
			findPlanCapability(t, &value, "operator.exterior").Status != CapabilityPresent {
			t.Fatal("operator exclusion was confused with hardware absence")
		}
	}
}

func TestInspectionPlanScopeCannotBeForgedByRehashing(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *Plan)
	}{
		{"capability_scope_missing", func(t *testing.T, p *Plan) { findPlanCapability(t, p, "operator.exterior").Applicability = "" }},
		{"capability_scope_unknown", func(t *testing.T, p *Plan) { findPlanCapability(t, p, "operator.exterior").Applicability = "hardware" }},
		{"capability_absent", func(t *testing.T, p *Plan) {
			findPlanCapability(t, p, "operator.exterior").Status = CapabilityAbsent
			findPlanTest(t, p, "inspection.exterior.quick").Status = TestNotPresent
		}},
		{"capability_driver", func(t *testing.T, p *Plan) {
			findPlanCapability(t, p, "operator.exterior").Status = CapabilityMissingDriver
			findPlanTest(t, p, "inspection.exterior.quick").Status = TestMissingDriver
		}},
		{"invented_component", func(t *testing.T, p *Plan) { replacePlanComponentRefs(t, p, "operator.exterior", []string{"cpu:0"}) }},
		{"test_scope_missing", func(t *testing.T, p *Plan) { findPlanTest(t, p, "inspection.exterior.quick").Applicability = "" }},
		{"test_scope_unknown", func(t *testing.T, p *Plan) {
			findPlanTest(t, p, "inspection.exterior.quick").Applicability = "component"
		}},
		{"test_capability", func(t *testing.T, p *Plan) { findPlanTest(t, p, "inspection.exterior.quick").Capability = "cpu" }},
		{"test_version", func(t *testing.T, p *Plan) { findPlanTest(t, p, "inspection.exterior.quick").Version = 1 }},
		{"test_unsupported", func(t *testing.T, p *Plan) { findPlanTest(t, p, "inspection.exterior.quick").Status = TestUnsupported }},
		{"test_destructive", func(t *testing.T, p *Plan) { findPlanTest(t, p, "inspection.exterior.quick").Destructive = true }},
		{"test_estimate", func(t *testing.T, p *Plan) { findPlanTest(t, p, "inspection.exterior.quick").EstimatedSeconds = 1 }},
		{"test_timeout", func(t *testing.T, p *Plan) { findPlanTest(t, p, "inspection.exterior.quick").TimeoutSeconds = 46 }},
		{"hardware_capability_scope", func(t *testing.T, p *Plan) { findPlanCapability(t, p, "cpu").Applicability = "whole_device_operator" }},
		{"hardware_test_scope", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").Applicability = "whole_device_operator" }},
		{"stale_plan_version", func(t *testing.T, p *Plan) { p.SchemaVersion = 2 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "x86_64")
			if Validate(value) != nil {
				t.Fatal("valid plan prerequisite missing")
			}
			test.mutate(t, &value)
			encoded := rehashPlanForTest(t, &value)
			if Validate(value) == nil {
				t.Fatal("forged typed scope accepted")
			}
			if _, err := Decode(encoded); err == nil {
				t.Fatal("rehashing authorized a forged scope")
			}
		})
	}
}

func TestInspectionCatalogExceptionIsExact(t *testing.T) {
	for _, name := range []string{"wrong_scope", "operator_hardware", "operator_provider", "hardware_scope", "wrong_test_id", "wrong_capability", "wrong_version", "unimplemented", "destructive", "wrong_profiles", "wrong_estimate", "wrong_timeout", "missing_test"} {
		t.Run(name, func(t *testing.T) {
			catalog, _, err := loadDefinitions(definitionData)
			if err != nil {
				t.Fatal(err)
			}
			capIndex, testIndex := -1, -1
			for i := range catalog.Capabilities {
				if catalog.Capabilities[i].ID == "operator.exterior" {
					capIndex = i
				}
			}
			for i := range catalog.Tests {
				if catalog.Tests[i].ID == "inspection.exterior.quick" {
					testIndex = i
				}
			}
			if capIndex < 0 || testIndex < 0 {
				t.Fatal("literal catalog prerequisites missing")
			}
			capability, test := &catalog.Capabilities[capIndex], &catalog.Tests[testIndex]
			switch name {
			case "wrong_scope":
				capability.Applicability = "operator_component"
			case "operator_hardware":
				capability.Hardware = []componentMatch{{Type: inventory.ComponentCPU}}
			case "operator_provider":
				capability.Providers = []componentMatch{{Type: inventory.ComponentCPU}}
			case "hardware_scope":
				catalog.Capabilities[0].Applicability = "whole_device_operator"
			case "wrong_test_id":
				test.ID = "inspection.unknown"
			case "wrong_capability":
				test.Capability = "cpu"
			case "wrong_version":
				test.Version = 1
			case "unimplemented":
				test.Implemented = false
			case "destructive":
				test.Destructive = true
			case "wrong_profiles":
				test.Profiles = []Profile{ProfileQuick}
			case "wrong_estimate":
				test.EstimatedSeconds = 19
			case "wrong_timeout":
				test.TimeoutSeconds = 44
			case "missing_test":
				catalog.Tests = append(catalog.Tests[:testIndex], catalog.Tests[testIndex+1:]...)
			}
			encoded, err := json.Marshal(catalog)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := loadDefinitions(encoded); err == nil {
				t.Fatal("expanded operator exception admitted")
			}
		})
	}
}

func TestInspectionPlanRejectsNullAndAliasedApplicability(t *testing.T) {
	value := buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "x86_64")
	encoded := rehashPlanForTest(t, &value)
	for _, replacement := range []string{
		`"applicability":null`,
		`"Applicability":"whole_device_operator"`,
		`"applicability":"whole_device_operator","applicability":"whole_device_operator"`,
		`"applicability":32`,
	} {
		altered := bytes.Replace(encoded, []byte(`"applicability":"whole_device_operator"`), []byte(replacement), 1)
		if bytes.Equal(altered, encoded) {
			t.Fatal("scope mutation was not reached")
		}
		if _, err := Decode(altered); err == nil {
			t.Fatal("invalid applicability representation accepted")
		}
	}
	altered := bytes.Replace(encoded, []byte(`"id":"cpu","status":"present"`), []byte(`"id":"cpu","status":"present","applicability":null`), 1)
	if bytes.Equal(altered, encoded) {
		t.Fatal("hardware scope prerequisite missing")
	}
	if _, err := Decode(altered); err == nil {
		t.Fatal("null hardware applicability accepted")
	}
}
