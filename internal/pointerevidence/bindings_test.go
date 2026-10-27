package pointerevidence

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func pointerInventory(width int, relative string) inventory.Component {
	keys := "10000 0 0 0 0"
	if width == 32 {
		keys = "10000 0 0 0 0 0 0 0 0"
	}
	return inventory.NewComponent(inventory.ComponentInput, "input:fixture", map[string]string{
		"category": "pointer", "handlers": "mouse0 event2", "bus": "0003", "vendor": "1234", "product": "5678", "version": "0102",
		"bitmap_word_bits": fmt.Sprint(width), "ev_capabilities": "7", "rel_capabilities": relative, "key_capabilities": keys})
}

func setAttribute(component *inventory.Component, name, value string) {
	for index := range component.Attributes {
		if component.Attributes[index].Name == name {
			component.Attributes[index].Value = value
			return
		}
	}
	component.Attributes = append(component.Attributes, inventory.Attribute{Name: name, Value: value})
}

func allWheelPointer() inputtest.PointerResult {
	return validPointer(inputtest.PointerWheelCapability{Axis: "vertical", Legacy: true, HighResolution: true}, inputtest.PointerWheelCapability{Axis: "horizontal", Legacy: true, HighResolution: true})
}

func TestPointerInventoryBindsEveryWheelBitAndKernelWordWidth(t *testing.T) {
	for _, width := range []int{32, 64} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			result := allWheelPointer()
			if ValidateInventoryBinding(result, []inventory.Component{pointerInventory(width, "1943")}) != nil {
				t.Fatal("independently represented kernel bitmap rejected")
			}
			for _, bit := range []int{6, 8, 11, 12} {
				t.Run(fmt.Sprint(bit), func(t *testing.T) {
					if ValidateInventoryBinding(result, []inventory.Component{pointerInventory(width, fmt.Sprintf("%x", 0x1943&^(1<<uint(bit))))}) == nil {
						t.Fatal("missing advertised wheel bit accepted")
					}
					if ValidateInventoryBinding(validPointer(), []inventory.Component{pointerInventory(width, fmt.Sprintf("%x", 3|(1<<uint(bit))))}) == nil {
						t.Fatal("extra inventory wheel bit hidden by no-wheel evidence")
					}
				})
			}
			for _, condition := range []string{"wrong width", "unknown width", "noncanonical width", "identity", "duplicate attribute", "alias disagreement", "bad bitmap", "overflow bitmap", "missing event", "missing x", "missing button", "missing component", "added interface", "duplicate interface", "duplicate component", "contradictory handler"} {
				t.Run(condition, func(t *testing.T) {
					components := []inventory.Component{pointerInventory(width, "1943")}
					switch condition {
					case "wrong width":
						setAttribute(&components[0], "bitmap_word_bits", fmt.Sprint(96-width))
					case "unknown width":
						setAttribute(&components[0], "bitmap_word_bits", "0")
					case "noncanonical width":
						setAttribute(&components[0], "bitmap_word_bits", "+32")
					case "identity":
						setAttribute(&components[0], "vendor", "9999")
					case "duplicate attribute":
						components[0].Attributes = append(components[0].Attributes, components[0].Attributes[0])
					case "alias disagreement":
						setAttribute(&components[0], "relative_capabilities", "3")
					case "bad bitmap":
						setAttribute(&components[0], "rel_capabilities", "garbage")
					case "overflow bitmap":
						setAttribute(&components[0], "rel_capabilities", "10000000000000000")
					case "missing event":
						setAttribute(&components[0], "ev_capabilities", "3")
					case "missing x":
						setAttribute(&components[0], "rel_capabilities", "1942")
					case "missing button":
						setAttribute(&components[0], "key_capabilities", "0")
					case "missing component":
						components = nil
					case "added interface":
						setAttribute(&components[0], "handlers", "mouse0 event2 event3")
					case "duplicate interface":
						setAttribute(&components[0], "handlers", "mouse0 event2 event2")
					case "duplicate component":
						components = append(components, components[0])
					case "contradictory handler":
						setAttribute(&components[0], "interface", "event3")
					}
					if ValidateInventoryBinding(result, components) == nil {
						t.Fatal("unbound or ambiguous passed interface accepted")
					}
				})
			}
		})
	}
}

func TestPointerV1InventoryDoesNotRetroactivelyClaimWheelCoverage(t *testing.T) {
	value, err := Decode([]byte(historicalPointer))
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{32, 64} {
		for _, relative := range []string{"3", "1943"} {
			if ValidateInventoryBinding(value, []inventory.Component{pointerInventory(width, relative)}) != nil {
				t.Fatal("old movement/button pass was reinterpreted as wheel qualification")
			}
		}
	}
	value.Devices[0].Capabilities.WheelAxes = []inputtest.PointerWheelCapability{}
	if ValidateResult(value) == nil {
		t.Fatal("a typed v1 view gained v2 wheel meaning")
	}
}

func bindingPlans(version int) (plan.Test, plan.Plan, plan.Plan, inventory.Snapshot) {
	test := plan.Test{ID: "input.pointer.quick", Version: version, Status: plan.TestScheduled, TimeoutSeconds: 90, ComponentIDs: []string{"input:fixture"}}
	frozen := plan.Plan{Tests: []plan.Test{test}, HardwareGraph: plan.HardwareGraph{Components: []plan.HardwareComponent{{Type: inventory.ComponentInput, ID: "input:fixture", Value: "unchanged-input"}}}}
	raw, _ := json.Marshal(frozen)
	var final plan.Plan
	_ = json.Unmarshal(raw, &final)
	return test, frozen, final, inventory.Snapshot{Components: []inventory.Component{pointerInventory(64, "1943")}}
}

func TestPointerPlanBindingPairsVersionsAndRetainsPartialFailures(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			result := allWheelPointer()
			if version == 1 {
				var err error
				result, err = Decode([]byte(historicalPointer))
				if err != nil {
					t.Fatal(err)
				}
				result.LimitMilliseconds = 90000
			}
			test, frozen, final, snapshot := bindingPlans(version)
			if ValidatePlanBinding(result, test, frozen, final, snapshot) != nil {
				t.Fatal("valid versioned frozen binding rejected")
			}
			for _, condition := range []string{"wrong version", "wrong test", "limit exceeds plan", "forged frozen row", "duplicate frozen row", "duplicate graph", "changed passed input", "removed passed ref", "extra final ref", "missing final row"} {
				t.Run(condition, func(t *testing.T) {
					test, frozen, final, snapshot := bindingPlans(version)
					value := result
					switch condition {
					case "wrong version":
						test.Version = 3 - version
						frozen.Tests[0] = test
					case "wrong test":
						test.ID = "input.keyboard.quick"
						frozen.Tests[0] = test
					case "limit exceeds plan":
						test.TimeoutSeconds = 89
						frozen.Tests[0] = test
					case "forged frozen row":
						frozen.Tests[0].TimeoutSeconds = 91
					case "duplicate frozen row":
						frozen.Tests = append(frozen.Tests, test)
					case "duplicate graph":
						final.HardwareGraph.Components = append(final.HardwareGraph.Components, final.HardwareGraph.Components[0])
					case "changed passed input":
						final.HardwareGraph.Components[0].Value = "changed"
					case "removed passed ref":
						final.Tests[0].ComponentIDs = nil
					case "extra final ref":
						final.Tests[0].ComponentIDs = append(final.Tests[0].ComponentIDs, "input:added")
					case "missing final row":
						final.Tests = nil
					}
					if ValidatePlanBinding(value, test, frozen, final, snapshot) == nil {
						t.Fatal("contradictory frozen binding accepted")
					}
				})
			}
			result.Status = inputtest.StatusError
			result.StopReason = "deadline"
			result.Detail = "deadline"
			if ValidatePlanBinding(result, test, frozen, final, snapshot) != nil {
				t.Fatal("retained device pass lost inside aggregate failure")
			}
			final.HardwareGraph.Components[0].Value = "changed"
			if ValidatePlanBinding(result, test, frozen, final, snapshot) == nil {
				t.Fatal("aggregate error bypassed retained-pass identity binding")
			}
			result.Devices[0].Status = inputtest.StatusInconclusive
			if ValidatePlanBinding(result, test, frozen, final, snapshot) != nil {
				t.Fatal("honest no-pass failure was forced into successful hardware binding")
			}
			test.TimeoutSeconds = 89
			frozen.Tests[0] = test
			if ValidatePlanBinding(result, test, frozen, final, snapshot) == nil {
				t.Fatal("no-pass result bypassed configured plan limit")
			}
		})
	}
}
