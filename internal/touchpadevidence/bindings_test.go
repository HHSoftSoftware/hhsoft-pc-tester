package touchpadevidence

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

// Explicit 64-bit and 32-bit kernel word layouts for KEY_LEFT/RIGHT/MIDDLE,
// TOOL_FINGER and TOUCH. These literals are not produced by bitmap().
func inventoryFixture() inventory.Component {
	return inventory.NewComponent(inventory.ComponentInput, "input:touchpad-fixture", map[string]string{
		"category": "touchpad", "touchpad_candidate": "true", "interface": "event4",
		"bus": "0003", "vendor_id": "1234", "product_id": "5678", "version": "0001",
		"bitmap_word_bits": "64", "event_capabilities": "1b",
		"key_capabilities":      "420 70000 0 0 0 0",
		"relative_capabilities": "0", "absolute_capabilities": "3",
		"properties": "5", "msc_capabilities": "30",
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

func TestTouchpadInventoryExactLiteralSets(t *testing.T) {
	cases := []struct {
		name   string
		change func(*inventory.Component)
	}{
		{"word64", func(c *inventory.Component) {}},
		{"word32", func(c *inventory.Component) {
			setAttribute(c, "bitmap_word_bits", "32")
			setAttribute(c, "key_capabilities", "420 0 70000 0 0 0 0 0 0 0 0")
		}},
		{"numeric_identity_aliases", func(c *inventory.Component) { setAttribute(c, "vendor", "1234"); setAttribute(c, "product", "5678") }},
		{"equivalent_bitmap_alias", func(c *inventory.Component) {
			setAttribute(c, "ev_capabilities", "001B")
			setAttribute(c, "abs_capabilities", "0003")
		}},
		{"matching_handler", func(c *inventory.Component) { setAttribute(c, "handlers", "mouse0 event4") }},
		{"handler_only", func(c *inventory.Component) {
			removeAttribute(c, "interface")
			setAttribute(c, "handlers", "mouse0 event4")
		}},
		{"absolute_input_candidate", func(c *inventory.Component) { setAttribute(c, "category", "input") }},
		{"absolute_pointer_candidate", func(c *inventory.Component) { setAttribute(c, "category", "pointer") }},
		{"model_name_irrelevant", func(c *inventory.Component) { setAttribute(c, "name", "Synthetic unrelated model") }},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			c := inventoryFixture()
			item.change(&c)
			if err := ValidateInventoryBinding(passFixture(t), []inventory.Component{c}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTouchpadInventoryRejectsMissingAndAmbiguousProof(t *testing.T) {
	cases := []struct {
		name   string
		change func(*inventory.Component)
	}{
		{"marker_missing", func(c *inventory.Component) { removeAttribute(c, "touchpad_candidate") }},
		{"marker_forged_false", func(c *inventory.Component) { setAttribute(c, "touchpad_candidate", "false") }},
		{"marker_uppercase", func(c *inventory.Component) { setAttribute(c, "touchpad_candidate", "TRUE") }},
		{"model_only_no_candidate", func(c *inventory.Component) {
			setAttribute(c, "category", "keyboard")
			setAttribute(c, "touchpad_candidate", "false")
			setAttribute(c, "name", "Touchpad")
		}},
		{"category_missing", func(c *inventory.Component) { removeAttribute(c, "category") }},
		{"empty_id", func(c *inventory.Component) { c.ID = "" }},
		{"duplicate_attribute", func(c *inventory.Component) { c.Attributes = append(c.Attributes, c.Attributes[0]) }},
		{"identity_alias_conflict", func(c *inventory.Component) { setAttribute(c, "vendor", "1235") }},
		{"identity_short", func(c *inventory.Component) { setAttribute(c, "bus", "3") }},
		{"identity_sign", func(c *inventory.Component) { setAttribute(c, "bus", "+003") }},
		{"identity_prefix", func(c *inventory.Component) { setAttribute(c, "bus", "0x03") }},
		{"identity_changed", func(c *inventory.Component) { setAttribute(c, "version", "0002") }},
		{"width_missing", func(c *inventory.Component) { removeAttribute(c, "bitmap_word_bits") }},
		{"width_guessed", func(c *inventory.Component) { setAttribute(c, "bitmap_word_bits", "16") }},
		{"width_noncanonical", func(c *inventory.Component) { setAttribute(c, "bitmap_word_bits", "064") }},
		{"wrong_word_layout", func(c *inventory.Component) { setAttribute(c, "bitmap_word_bits", "32") }},
		{"events_missing", func(c *inventory.Component) { removeAttribute(c, "event_capabilities") }},
		{"keys_missing", func(c *inventory.Component) { removeAttribute(c, "key_capabilities") }},
		{"relative_missing_not_empty", func(c *inventory.Component) { removeAttribute(c, "relative_capabilities") }},
		{"absolute_missing", func(c *inventory.Component) { removeAttribute(c, "absolute_capabilities") }},
		{"properties_missing", func(c *inventory.Component) { removeAttribute(c, "properties") }},
		{"msc_missing_advertised", func(c *inventory.Component) { removeAttribute(c, "msc_capabilities") }},
		{"msc_unknown_bit", func(c *inventory.Component) { setAttribute(c, "msc_capabilities", "31") }},
		{"events_unknown_bit", func(c *inventory.Component) { setAttribute(c, "event_capabilities", "3b") }},
		{"keys_unknown_bit", func(c *inventory.Component) { setAttribute(c, "key_capabilities", "420 70000 0 0 0 1") }},
		{"relative_unknown_bit", func(c *inventory.Component) { setAttribute(c, "relative_capabilities", "1") }},
		{"absolute_unknown_bit", func(c *inventory.Component) { setAttribute(c, "absolute_capabilities", "7") }},
		{"properties_unknown_bit", func(c *inventory.Component) { setAttribute(c, "properties", "7") }},
		{"event_alias_conflict", func(c *inventory.Component) { setAttribute(c, "ev_capabilities", "b") }},
		{"absolute_alias_conflict", func(c *inventory.Component) { setAttribute(c, "abs_capabilities", "1") }},
		{"bitmap_plus_sign", func(c *inventory.Component) { setAttribute(c, "event_capabilities", "+1b") }},
		{"bitmap_prefix", func(c *inventory.Component) { setAttribute(c, "event_capabilities", "0x1b") }},
		{"bitmap_nonhex", func(c *inventory.Component) { setAttribute(c, "event_capabilities", "1g") }},
		{"bitmap_empty", func(c *inventory.Component) { setAttribute(c, "event_capabilities", "") }},
		{"bitmap_overwide_word", func(c *inventory.Component) { setAttribute(c, "event_capabilities", "0000000000000001b") }},
		{"bitmap_overwide_zero_words", func(c *inventory.Component) { setAttribute(c, "event_capabilities", "0 1b") }},
		{"bitmap_out_of_domain", func(c *inventory.Component) { setAttribute(c, "key_capabilities", "1 "+strings.Repeat("0 ", 12)) }},
		{"bitmap_text_budget", func(c *inventory.Component) { setAttribute(c, "key_capabilities", strings.Repeat(" ", 513)) }},
		{"event_alias_empty_conflict", func(c *inventory.Component) {
			setAttribute(c, "absolute_capabilities", "")
			setAttribute(c, "abs_capabilities", "3")
		}},
		{"interface_leading_zero", func(c *inventory.Component) { setAttribute(c, "interface", "event04") }},
		{"interface_overflow", func(c *inventory.Component) { setAttribute(c, "interface", "event4294967296") }},
		{"interface_handler_conflict", func(c *inventory.Component) { setAttribute(c, "handlers", "event5") }},
		{"duplicate_event_handler", func(c *inventory.Component) { setAttribute(c, "handlers", "event4 event4") }},
		{"malformed_event_handler", func(c *inventory.Component) { setAttribute(c, "handlers", "event04") }},
		{"no_event_interface", func(c *inventory.Component) { removeAttribute(c, "interface"); setAttribute(c, "handlers", "mouse0") }},
		{"handler_budget", func(c *inventory.Component) { setAttribute(c, "handlers", strings.Repeat(" ", 4097)) }},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			c := inventoryFixture()
			item.change(&c)
			if !errors.Is(ValidateInventoryBinding(passFixture(t), []inventory.Component{c}), ErrInvalid) {
				t.Fatal("unbound pass")
			}
		})
	}
}

func TestTouchpadInventoryCandidateAndFinalSet(t *testing.T) {
	t.Run("unknown_absolute_stays_diagnostic_candidate", func(t *testing.T) {
		c := inventory.NewComponent(inventory.ComponentInput, "input:unknown", map[string]string{"category": "input", "absolute_capabilities": "not-a-bitmap", "touchpad_candidate": "true", "interface": "event4"})
		if err := ValidateInventoryBinding(unreadableFixture(t), []inventory.Component{c}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("zero_absolute_still_candidate", func(t *testing.T) {
		c := inventory.NewComponent(inventory.ComponentInput, "input:unknown", map[string]string{"category": "pointer", "absolute_capabilities": "0", "touchpad_candidate": "true", "interface": "event4"})
		if err := ValidateInventoryBinding(unreadableFixture(t), []inventory.Component{c}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("diagnostic_alias_conflict", func(t *testing.T) {
		c := inventoryFixture()
		setAttribute(&c, "abs_capabilities", "7")
		if !errors.Is(ValidateInventoryBinding(unreadableFixture(t), []inventory.Component{c}), ErrInvalid) {
			t.Fatal("alias chosen for diagnostic")
		}
	})
	t.Run("removed_diagnostic_row", func(t *testing.T) {
		if !errors.Is(ValidateInventoryBinding(unreadableFixture(t), nil), ErrInvalid) {
			t.Fatal("removed row signed")
		}
	})
	t.Run("extra_final_candidate_prevents_aggregate_pass", func(t *testing.T) {
		c := inventoryFixture()
		extra := inventoryFixture()
		extra.ID = "input:extra"
		setAttribute(&extra, "interface", "event5")
		if !errors.Is(ValidateInventoryBinding(passFixture(t), []inventory.Component{c, extra}), ErrInvalid) {
			t.Fatal("partial candidate pass")
		}
	})
	t.Run("duplicate_component_id", func(t *testing.T) {
		c := inventoryFixture()
		extra := inventoryFixture()
		setAttribute(&extra, "interface", "event5")
		if !errors.Is(ValidateInventoryBinding(passFixture(t), []inventory.Component{c, extra}), ErrInvalid) {
			t.Fatal("duplicate ID")
		}
	})
	t.Run("duplicate_interface", func(t *testing.T) {
		c := inventoryFixture()
		extra := inventoryFixture()
		extra.ID = "input:extra"
		if !errors.Is(ValidateInventoryBinding(passFixture(t), []inventory.Component{c, extra}), ErrInvalid) {
			t.Fatal("duplicate interface")
		}
	})
	t.Run("noncandidate_cannot_forge_marker", func(t *testing.T) {
		c := inventoryFixture()
		setAttribute(&c, "category", "keyboard")
		if !errors.Is(ValidateInventoryBinding(passFixture(t), []inventory.Component{c}), ErrInvalid) {
			t.Fatal("forged marker")
		}
	})
	t.Run("retained_pass_requires_complete_sets", func(t *testing.T) {
		r := passFixture(t)
		r.Status = inputtest.StatusAborted
		r.StopReason = "operator_cancelled"
		r.Detail = "operator_cancelled"
		c := inventoryFixture()
		setAttribute(&c, "key_capabilities", "420 70000 0 0 0 1")
		if !errors.Is(ValidateInventoryBinding(r, []inventory.Component{c}), ErrInvalid) {
			t.Fatal("retained unbound pass")
		}
	})
	t.Run("empty_misc_requires_observation", func(t *testing.T) {
		r := passFixture(t)
		r.Devices[0].Capabilities.Events = []uint16{0, 1, 3}
		r.Devices[0].Capabilities.Misc = []uint16{}
		c := inventoryFixture()
		setAttribute(&c, "event_capabilities", "b")
		setAttribute(&c, "msc_capabilities", "0")
		if err := ValidateInventoryBinding(r, []inventory.Component{c}); err != nil {
			t.Fatal(err)
		}
		removeAttribute(&c, "msc_capabilities")
		if !errors.Is(ValidateInventoryBinding(r, []inventory.Component{c}), ErrInvalid) {
			t.Fatal("missing inferred empty")
		}
	})
}

// These are explicit future-definition structs for a pure binding API. They
// do not pass through Plan.Decode and do not claim current catalog activation.
func planFixture() (plan.Test, plan.Plan, plan.Plan, inventory.Snapshot) {
	test := plan.Test{ID: "input.touchpad.quick", Version: 2, Status: plan.TestScheduled, Capability: "input.touchpad", ComponentIDs: []string{"input:touchpad-fixture"}, EstimatedSeconds: 30, TimeoutSeconds: 90}
	frozen := plan.Plan{SchemaVersion: 1, Frozen: true, Tests: []plan.Test{test},
		HardwareGraph: plan.HardwareGraph{Algorithm: "hmac-sha256", KeyEpoch: "literal-epoch", Value: strings.Repeat("a", 64), Components: []plan.HardwareComponent{{Type: inventory.ComponentInput, ID: "input:touchpad-fixture", Value: strings.Repeat("b", 64)}}}}
	final := frozen
	final.Tests = append([]plan.Test{}, frozen.Tests...)
	final.Tests[0].ComponentIDs = append([]string{}, test.ComponentIDs...)
	final.HardwareGraph.Components = append([]plan.HardwareComponent{}, frozen.HardwareGraph.Components...)
	return test, frozen, final, inventory.Snapshot{SchemaVersion: inventory.SnapshotSchemaVersion, Components: []inventory.Component{inventoryFixture()}}
}

func TestTouchpadPlanLiteralAndRetainedRows(t *testing.T) {
	t.Run("exact_synthetic_future_plan", func(t *testing.T) {
		test, frozen, final, snapshot := planFixture()
		before := wire(t, passFixture(t))
		r := passFixture(t)
		if err := ValidatePlanBinding(r, test, frozen, final, snapshot); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, wire(t, r)) {
			t.Fatal("validation mutated result")
		}
	})
	t.Run("retained_pass_under_cancel", func(t *testing.T) {
		test, frozen, final, snapshot := planFixture()
		r := passFixture(t)
		r.Status, r.StopReason, r.Detail = inputtest.StatusAborted, "operator_cancelled", "operator_cancelled"
		if err := ValidatePlanBinding(r, test, frozen, final, snapshot); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("diagnostic_without_remaining_candidates", func(t *testing.T) {
		test, frozen, final, snapshot := planFixture()
		r := diagnosticFixture(t)
		r.Devices = []inputtest.TouchpadDeviceResult{}
		r.StopReason, r.Detail = "no_touchpads", "no_touchpads"
		final.Tests[0].Status = plan.TestNotPresent
		final.Tests[0].ComponentIDs = nil
		final.HardwareGraph.Components = nil
		snapshot.Components = nil
		if err := ValidatePlanBinding(r, test, frozen, final, snapshot); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("partial_retained_pass_does_not_upgrade_aggregate", func(t *testing.T) {
		test, frozen, final, snapshot := planFixture()
		r := passFixture(t)
		r.Status, r.StopReason, r.Detail = inputtest.StatusInconclusive, "observation_timeout", "observation_timeout"
		extra := inventoryFixture()
		extra.ID = "input:extra"
		setAttribute(&extra, "interface", "event5")
		snapshot.Components = append(snapshot.Components, extra)
		final.Tests[0].ComponentIDs = append(final.Tests[0].ComponentIDs, extra.ID)
		final.HardwareGraph.Components = append(final.HardwareGraph.Components, plan.HardwareComponent{Type: inventory.ComponentInput, ID: extra.ID, Value: strings.Repeat("c", 64)})
		if err := ValidatePlanBinding(r, test, frozen, final, snapshot); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTouchpadPlanRejectsBindingChanges(t *testing.T) {
	cases := []struct {
		name   string
		change func(*plan.Test, *plan.Plan, *plan.Plan, *inventory.Snapshot)
	}{
		{"wrong_test", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { x.ID = "input.pointer.quick" }},
		{"historical_definition", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { x.Version = 1 }},
		{"unscheduled_initial", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { x.Status = plan.TestUnsupported }},
		{"destructive_test", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { x.Destructive = true }},
		{"timeout_zero", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { x.TimeoutSeconds = 0 }},
		{"timeout_overflow_guard", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { x.TimeoutSeconds = 3601 }},
		{"initial_refs_empty", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { x.ComponentIDs = nil }},
		{"initial_refs_duplicate", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) {
			x.ComponentIDs = append(x.ComponentIDs, x.ComponentIDs[0])
		}},
		{"frozen_missing_test", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { a.Tests = nil }},
		{"frozen_duplicate_test", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { a.Tests = append(a.Tests, a.Tests[0]) }},
		{"frozen_test_not_exact", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { a.Tests[0].EstimatedSeconds++ }},
		{"initial_graph_missing", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { a.HardwareGraph.Components = nil }},
		{"initial_graph_duplicate", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) {
			a.HardwareGraph.Components = append(a.HardwareGraph.Components, a.HardwareGraph.Components[0])
		}},
		{"initial_graph_empty_value", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { a.HardwareGraph.Components[0].Value = "" }},
		{"final_test_missing", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { b.Tests = nil }},
		{"final_test_duplicate", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { b.Tests = append(b.Tests, b.Tests[0]) }},
		{"final_definition_changed", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { b.Tests[0].Version = 1 }},
		{"final_test_unscheduled", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { b.Tests[0].Status = plan.TestUnsupported }},
		{"final_test_destructive", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { b.Tests[0].Destructive = true }},
		{"final_refs_empty", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { b.Tests[0].ComponentIDs = nil }},
		{"final_refs_duplicate", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) {
			b.Tests[0].ComponentIDs = append(b.Tests[0].ComponentIDs, b.Tests[0].ComponentIDs[0])
		}},
		{"final_refs_replaced", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) {
			b.Tests[0].ComponentIDs = []string{"input:replacement"}
		}},
		{"final_graph_missing", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { b.HardwareGraph.Components = nil }},
		{"final_graph_duplicate", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) {
			b.HardwareGraph.Components = append(b.HardwareGraph.Components, b.HardwareGraph.Components[0])
		}},
		{"final_graph_changed", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) {
			b.HardwareGraph.Components[0].Value = strings.Repeat("c", 64)
		}},
		{"graph_epoch_changed", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { b.HardwareGraph.KeyEpoch = "other" }},
		{"graph_algorithm_changed", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { b.HardwareGraph.Algorithm = "sha256" }},
		{"snapshot_schema", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { s.SchemaVersion = 0 }},
		{"snapshot_duplicate_id", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) {
			s.Components = append(s.Components, s.Components[0])
		}},
		{"snapshot_removed", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) { s.Components = nil }},
		{"snapshot_replaced_identity", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) {
			setAttribute(&s.Components[0], "product_id", "5679")
		}},
		{"snapshot_replaced_capabilities", func(x *plan.Test, a, b *plan.Plan, s *inventory.Snapshot) {
			setAttribute(&s.Components[0], "event_capabilities", "3b")
		}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			test, frozen, final, snapshot := planFixture()
			item.change(&test, &frozen, &final, &snapshot)
			r := passFixture(t)
			if !errors.Is(ValidatePlanBinding(r, test, frozen, final, snapshot), ErrInvalid) {
				t.Fatal("aggregate binding change accepted")
			}
			// Retained passed rows require the same source/identity/graph proof.
			r.Status, r.StopReason, r.Detail = inputtest.StatusAborted, "operator_cancelled", "operator_cancelled"
			if !errors.Is(ValidatePlanBinding(r, test, frozen, final, snapshot), ErrInvalid) {
				t.Fatal("retained pass bypassed binding")
			}
		})
	}
	t.Run("limit_exceeds_frozen_timeout", func(t *testing.T) {
		test, frozen, final, snapshot := planFixture()
		test.TimeoutSeconds = 1
		frozen.Tests[0] = test
		final.Tests[0] = test
		r := passFixture(t)
		r.LimitMilliseconds = 1001
		if !errors.Is(ValidatePlanBinding(r, test, frozen, final, snapshot), ErrInvalid) {
			t.Fatal("worker exceeds plan")
		}
	})
	t.Run("exact_candidate_component_set", func(t *testing.T) {
		test, frozen, final, snapshot := planFixture()
		test.ComponentIDs = append(test.ComponentIDs, "input:not-a-candidate")
		frozen.Tests[0] = test
		final.Tests[0] = test
		extra := plan.HardwareComponent{Type: inventory.ComponentInput, ID: "input:not-a-candidate", Value: strings.Repeat("c", 64)}
		frozen.HardwareGraph.Components = append(frozen.HardwareGraph.Components, extra)
		final.HardwareGraph.Components = append(final.HardwareGraph.Components, extra)
		if !errors.Is(ValidatePlanBinding(passFixture(t), test, frozen, final, snapshot), ErrInvalid) {
			t.Fatal("planned set omitted")
		}
	})
}
