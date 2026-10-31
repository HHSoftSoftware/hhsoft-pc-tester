package touchscreenevidence

import (
	"errors"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func inventoryFixture() inventory.Component {
	return inventory.NewComponent(inventory.ComponentInput, "input:surface", map[string]string{
		"category": "touchscreen", "touchscreen_candidate": "true", "interface": "event2",
		"bus": "0003", "vendor_id": "0001", "product_id": "0002", "version": "0001",
		"bitmap_word_bits": "64", "event_capabilities": "b",
		"key_capabilities": "400 0 0 0 0 0", "relative_capabilities": "0",
		"absolute_capabilities": "3", "properties": "2", "msc_capabilities": "0",
	})
}

func setAttribute(c *inventory.Component, name, value string) {
	for i := range c.Attributes {
		if c.Attributes[i].Name == name {
			c.Attributes[i].Value = value
			return
		}
	}
	c.Attributes = append(c.Attributes, inventory.Attribute{Name: name, Value: value})
}

func removeAttribute(c *inventory.Component, name string) {
	for i := range c.Attributes {
		if c.Attributes[i].Name == name {
			c.Attributes = append(c.Attributes[:i], c.Attributes[i+1:]...)
			return
		}
	}
}

func TestTouchscreenInventoryLiteralWidthAndDirectAliases(t *testing.T) {
	for _, name := range []string{"word64", "word32", "numeric_alias", "bitmap_alias", "handlers_only", "misleading_touchpad", "generic_direct"} {
		t.Run(name, func(t *testing.T) {
			c := inventoryFixture()
			switch name {
			case "word32":
				setAttribute(&c, "bitmap_word_bits", "32")
				setAttribute(&c, "key_capabilities", "400 0 0 0 0 0 0 0 0 0 0")
				setAttribute(&c, "properties", "0 2")
			case "numeric_alias":
				setAttribute(&c, "vendor", "0001")
				setAttribute(&c, "product", "0002")
			case "bitmap_alias":
				setAttribute(&c, "abs_capabilities", "0003")
				setAttribute(&c, "prop_capabilities", "02")
			case "handlers_only":
				removeAttribute(&c, "interface")
				setAttribute(&c, "handlers", "mouse0 event2")
			case "misleading_touchpad":
				setAttribute(&c, "category", "touchpad")
				setAttribute(&c, "touchpad_candidate", "true")
			case "generic_direct":
				setAttribute(&c, "category", "input")
			}
			if err := ValidateInventoryBinding(passFixture(t), []inventory.Component{c}); err != nil {
				t.Fatal("literal DIRECT identity/capability binding rejected")
			}
		})
	}
}

func TestTouchscreenInventoryRejectsIncompleteOrConflictingAuthority(t *testing.T) {
	for _, name := range []string{"missing_marker", "forged_marker", "missing_width", "wrong_width", "overwide_width", "absent_interface", "empty_interface", "interface_alias", "duplicate_handler", "duplicate_attribute", "duplicate_component", "identity_alias", "property_alias", "absolute_alias", "missing_relative", "missing_msc", "unknown_msc", "extra_key", "extra_axis", "pointer_property", "missing_category", "noncandidate"} {
		t.Run(name, func(t *testing.T) {
			c := inventoryFixture()
			if err := ValidateInventoryBinding(passFixture(t), []inventory.Component{c}); err != nil {
				t.Fatal("negative prerequisite is invalid")
			}
			switch name {
			case "missing_marker":
				removeAttribute(&c, "touchscreen_candidate")
			case "forged_marker":
				setAttribute(&c, "touchscreen_candidate", "false")
			case "missing_width":
				removeAttribute(&c, "bitmap_word_bits")
			case "wrong_width":
				setAttribute(&c, "bitmap_word_bits", "32")
			case "overwide_width":
				setAttribute(&c, "bitmap_word_bits", "064")
			case "absent_interface":
				removeAttribute(&c, "interface")
			case "empty_interface":
				setAttribute(&c, "interface", "")
			case "interface_alias":
				setAttribute(&c, "handlers", "event3")
			case "duplicate_handler":
				setAttribute(&c, "handlers", "event2 event2")
			case "duplicate_attribute":
				c.Attributes = append(c.Attributes, c.Attributes[0])
			case "identity_alias":
				setAttribute(&c, "vendor", "0003")
			case "property_alias":
				setAttribute(&c, "prop_capabilities", "1")
			case "absolute_alias":
				setAttribute(&c, "abs_capabilities", "1")
			case "missing_relative":
				removeAttribute(&c, "relative_capabilities")
			case "missing_msc":
				removeAttribute(&c, "msc_capabilities")
			case "unknown_msc":
				setAttribute(&c, "msc_capabilities", "1")
			case "extra_key":
				setAttribute(&c, "key_capabilities", "420 0 0 0 0 0")
			case "extra_axis":
				setAttribute(&c, "absolute_capabilities", "1000003")
			case "pointer_property":
				setAttribute(&c, "properties", "1")
			case "missing_category":
				removeAttribute(&c, "category")
			case "noncandidate":
				setAttribute(&c, "category", "input")
				setAttribute(&c, "properties", "0")
				setAttribute(&c, "touchscreen_candidate", "false")
			}
			components := []inventory.Component{c}
			if name == "duplicate_component" {
				components = append(components, c)
			}
			if !errors.Is(ValidateInventoryBinding(passFixture(t), components), ErrInvalid) {
				t.Fatal("unbound functional pass admitted")
			}
		})
	}
}

func TestTouchscreenInventoryCompleteSetAndRetainedPass(t *testing.T) {
	first, second := inventoryFixture(), inventoryFixture()
	second.ID = "input:second"
	setAttribute(&second, "interface", "event3")
	if !errors.Is(ValidateInventoryBinding(passFixture(t), []inventory.Component{first, second}), ErrInvalid) {
		t.Fatal("partial discovery set became aggregate success")
	}
	r := passFixture(t)
	r.Status, r.StopReason, r.Detail = inputtest.StatusAborted, "operator_cancelled", "operator_cancelled"
	if err := ValidateInventoryBinding(r, []inventory.Component{first, second}); err != nil {
		t.Fatal("bound retained row under a nonpass aggregate was lost")
	}
	setAttribute(&first, "version", "0002")
	if !errors.Is(ValidateInventoryBinding(r, []inventory.Component{first, second}), ErrInvalid) {
		t.Fatal("interruption bypassed retained-pass identity binding")
	}
	// Category-only inventory can be preserved by report projection, but cannot
	// supply a functional interface or an inventory-bound pass.
	categoryOnly := inventory.NewComponent(inventory.ComponentInput, "input:unknown",
		map[string]string{"category": "touchscreen", "touchscreen_candidate": "true"})
	if !errors.Is(ValidateInventoryBinding(passFixture(t), []inventory.Component{categoryOnly}), ErrInvalid) {
		t.Fatal("category-only report metadata became functional authority")
	}
}

// Synthetic arguments exercise this binding API, not Plan.Decode. They do not
// establish catalog execution authority or observed hardware history.
func planFixture() (plan.Test, plan.Plan, plan.Plan, inventory.Snapshot) {
	test := plan.Test{ID: "input.touchscreen.quick", Version: 2, Status: plan.TestScheduled,
		Capability: "input.touchscreen", ComponentIDs: []string{"input:surface"}, EstimatedSeconds: 45, TimeoutSeconds: 120}
	frozen := plan.Plan{SchemaVersion: plan.SchemaVersion, Frozen: true, Tests: []plan.Test{test},
		HardwareGraph: plan.HardwareGraph{Algorithm: "hmac-sha256", KeyEpoch: "literal-epoch", Value: strings.Repeat("a", 64),
			Components: []plan.HardwareComponent{{Type: inventory.ComponentInput, ID: "input:surface", Value: strings.Repeat("b", 64)}}}}
	frozen.Tests[0].ComponentIDs = []string{"input:surface"}
	final := frozen
	final.Tests = append([]plan.Test{}, frozen.Tests...)
	final.Tests[0].ComponentIDs = []string{"input:surface"}
	final.HardwareGraph.Components = append([]plan.HardwareComponent{}, frozen.HardwareGraph.Components...)
	return test, frozen, final, inventory.Snapshot{SchemaVersion: inventory.SnapshotSchemaVersion, Components: []inventory.Component{inventoryFixture()}}
}

func TestTouchscreenPlanBindingPreservesFrozenAuthority(t *testing.T) {
	for _, name := range []string{"valid", "definition", "wrong_test", "unscheduled", "timeout", "frozen_row", "missing_frozen", "duplicate_final", "final_definition", "final_unscheduled", "epoch", "algorithm", "component_graph", "final_refs", "inventory_identity", "inventory_removed"} {
		t.Run(name, func(t *testing.T) {
			test, frozen, final, snapshot := planFixture()
			r := passFixture(t)
			if err := ValidatePlanBinding(r, test, frozen, final, snapshot); err != nil {
				t.Fatal("binding prerequisite invalid")
			}
			switch name {
			case "definition":
				test.Version = 1
			case "wrong_test":
				test.ID = "input.touchpad.quick"
			case "unscheduled":
				test.Status = plan.TestNotPresent
			case "timeout":
				test.TimeoutSeconds = 30
				frozen.Tests[0].TimeoutSeconds = 30
				final.Tests[0].TimeoutSeconds = 30
			case "frozen_row":
				frozen.Tests[0].EstimatedSeconds = 44
			case "missing_frozen":
				frozen.Tests = nil
			case "duplicate_final":
				final.Tests = append(final.Tests, final.Tests[0])
			case "final_definition":
				final.Tests[0].Version = 1
			case "final_unscheduled":
				final.Tests[0].Status = plan.TestUnsupported
			case "epoch":
				final.HardwareGraph.KeyEpoch = "changed-epoch"
			case "algorithm":
				final.HardwareGraph.Algorithm = "sha256"
			case "component_graph":
				final.HardwareGraph.Components[0].Value = strings.Repeat("c", 64)
			case "final_refs":
				final.Tests[0].ComponentIDs = []string{"input:replacement"}
			case "inventory_identity":
				setAttribute(&snapshot.Components[0], "product_id", "0003")
			case "inventory_removed":
				snapshot.Components = nil
			}
			for _, retained := range []bool{false, true} {
				if retained {
					r.Status, r.StopReason, r.Detail = inputtest.StatusAborted, "operator_cancelled", "operator_cancelled"
				}
				err := ValidatePlanBinding(r, test, frozen, final, snapshot)
				if name == "valid" && err != nil || name != "valid" && !errors.Is(err, ErrInvalid) {
					t.Fatal("frozen plan or retained-pass continuity was not enforced")
				}
			}
		})
	}
}
