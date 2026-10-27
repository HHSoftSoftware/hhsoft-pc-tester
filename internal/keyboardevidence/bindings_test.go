package keyboardevidence

import (
	"fmt"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func keyboardInventory(width int) inventory.Component {
	var words [4]uint64
	for _, code := range []int{42, 54, 103, 108, 105, 106} {
		words[code/width] |= 1 << uint(code%width)
	}
	var keys []string
	for index := 108 / width; index >= 0; index-- {
		keys = append(keys, fmt.Sprintf("%x", words[index]))
	}
	return inventory.NewComponent(inventory.ComponentInput, "input:fixture", map[string]string{"category": "keyboard", "handlers": "kbd event2", "bus": "0003", "vendor": "1234", "product": "5678", "version": "0100", "bitmap_word_bits": fmt.Sprint(width), "ev_capabilities": "3", "key_capabilities": strings.Join(keys, " ")})
}

func changeKeyboardAttribute(component *inventory.Component, name, value string) {
	for index := range component.Attributes {
		if component.Attributes[index].Name == name {
			component.Attributes[index].Value = value
			return
		}
	}
	component.Attributes = append(component.Attributes, inventory.Attribute{Name: name, Value: value})
}

func TestKeyboardInventoryBindingsRespectKernelWidthAndAllInterfaces(t *testing.T) {
	for _, width := range []int{32, 64} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			result := validKeyboard(t)
			component := keyboardInventory(width)
			if err := ValidateInventoryBinding(result, []inventory.Component{component}); err != nil {
				t.Fatalf("kernel width %d rejected: %v", width, err)
			}
			for _, condition := range []string{"missing key", "wrong width", "unknown width", "identity", "duplicate attribute", "duplicate interface", "added interface", "missing component", "category", "malformed bitmap", "overflow bitmap", "alias disagreement"} {
				t.Run(condition, func(t *testing.T) {
					component := keyboardInventory(width)
					components := []inventory.Component{component}
					switch condition {
					case "missing key":
						changeKeyboardAttribute(&components[0], "key_capabilities", "0")
					case "wrong width":
						changeKeyboardAttribute(&components[0], "bitmap_word_bits", fmt.Sprint(96-width))
					case "unknown width":
						changeKeyboardAttribute(&components[0], "bitmap_word_bits", "0")
					case "identity":
						changeKeyboardAttribute(&components[0], "vendor", "9999")
					case "duplicate attribute":
						components[0].Attributes = append(components[0].Attributes, components[0].Attributes[0])
					case "duplicate interface":
						changeKeyboardAttribute(&components[0], "handlers", "kbd event2 event2")
					case "added interface":
						changeKeyboardAttribute(&components[0], "handlers", "kbd event2 event3")
					case "missing component":
						components = nil
					case "category":
						changeKeyboardAttribute(&components[0], "category", "pointer")
					case "malformed bitmap":
						changeKeyboardAttribute(&components[0], "key_capabilities", "garbage 0")
					case "overflow bitmap":
						changeKeyboardAttribute(&components[0], "key_capabilities", "10000000000000000 0")
					case "alias disagreement":
						changeKeyboardAttribute(&components[0], "vendor_id", "ffff")
					}
					if ValidateInventoryBinding(result, components) == nil {
						t.Fatal("unbound device pass accepted")
					}
				})
			}
		})
	}
}

func keyboardPlanFixture() (plan.Test, plan.Plan, plan.Plan, inventory.Snapshot) {
	test := plan.Test{ID: "input.keyboard.quick", Version: 2, Status: plan.TestScheduled, Capability: "input.keyboard", ComponentIDs: []string{"input:fixture"}, TimeoutSeconds: 90}
	frozen := plan.Plan{Tests: []plan.Test{test}, HardwareGraph: plan.HardwareGraph{Components: []plan.HardwareComponent{{Type: inventory.ComponentInput, ID: "input:fixture", Value: "bound-keyboard"}}}}
	final := plan.Plan{Tests: []plan.Test{test}, HardwareGraph: plan.HardwareGraph{Components: []plan.HardwareComponent{{Type: inventory.ComponentInput, ID: "input:fixture", Value: "bound-keyboard"}}}}
	return test, frozen, final, inventory.NewSnapshot([]inventory.Component{keyboardInventory(64)})
}

func TestKeyboardPlanBindingRejectsDriftAndAmbiguousMembership(t *testing.T) {
	for _, condition := range []string{"valid", "definition", "unscheduled", "window", "omitted frozen row", "duplicate frozen row", "duplicate final row", "changed graph", "duplicate graph", "removed graph", "removed reference", "added reference", "duplicate component", "changed inventory"} {
		t.Run(condition, func(t *testing.T) {
			result := validKeyboard(t)
			test, frozen, final, snapshot := keyboardPlanFixture()
			switch condition {
			case "definition":
				test.Version = 1
			case "unscheduled":
				test.Status = plan.TestUnsupported
			case "window":
				test.TimeoutSeconds = 10
				frozen.Tests[0] = test
			case "omitted frozen row":
				frozen.Tests = nil
			case "duplicate frozen row":
				frozen.Tests = append(frozen.Tests, test)
			case "duplicate final row":
				final.Tests = append(final.Tests, test)
			case "changed graph":
				final.HardwareGraph.Components[0].Value = "changed"
			case "duplicate graph":
				final.HardwareGraph.Components = append(final.HardwareGraph.Components, final.HardwareGraph.Components[0])
			case "removed graph":
				final.HardwareGraph.Components = nil
			case "removed reference":
				final.Tests[0].ComponentIDs = nil
			case "added reference":
				final.Tests[0].ComponentIDs = []string{"input:fixture", "input:other"}
			case "duplicate component":
				snapshot.Components = append(snapshot.Components, snapshot.Components[0])
			case "changed inventory":
				changeKeyboardAttribute(&snapshot.Components[0], "vendor", "ffff")
			}
			err := ValidatePlanBinding(result, test, frozen, final, snapshot)
			if (err == nil) != (condition == "valid") {
				t.Fatalf("plan membership result for %s: %v", condition, err)
			}
		})
	}
}

func TestKeyboardPartialDevicePassStillRequiresBinding(t *testing.T) {
	result := validKeyboard(t)
	result.Status = inputtest.StatusInconclusive
	result.StopReason = "observation_timeout"
	result.Detail = "observation_timeout"
	component := keyboardInventory(64)
	if ValidateInventoryBinding(result, []inventory.Component{component}) != nil {
		t.Fatal("retained valid device pass rejected")
	}
	changeKeyboardAttribute(&component, "vendor", "ffff")
	if ValidateInventoryBinding(result, []inventory.Component{component}) == nil {
		t.Fatal("aggregate non-pass hid a forged device pass")
	}
	test, frozen, final, snapshot := keyboardPlanFixture()
	final.HardwareGraph.Components[0].Value = "changed"
	if ValidatePlanBinding(result, test, frozen, final, snapshot) == nil {
		t.Fatal("aggregate non-pass hid a changed device graph")
	}
}

func TestKeyboardPartialPassCannotChooseConflictingInventoryAliases(t *testing.T) {
	for _, condition := range []string{"interface mismatch", "overlong bitmap word"} {
		t.Run(condition, func(t *testing.T) {
			result := validKeyboard(t)
			result.Status = inputtest.StatusError
			result.StopReason = "error"
			result.Detail = "incomplete_actions"
			component := keyboardInventory(64)
			if condition == "interface mismatch" {
				changeKeyboardAttribute(&component, "interface", "event9")
			} else {
				for index := range component.Attributes {
					if component.Attributes[index].Name == "key_capabilities" {
						component.Attributes[index].Value = strings.Repeat("0", 17) + component.Attributes[index].Value
					}
				}
			}
			if ValidateInventoryBinding(result, []inventory.Component{component}) == nil {
				t.Fatal("partial status hid ambiguous keyboard inventory")
			}
		})
	}
}
