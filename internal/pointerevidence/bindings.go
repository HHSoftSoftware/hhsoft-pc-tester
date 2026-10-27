package pointerevidence

import (
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func attribute(component inventory.Component, names ...string) (string, bool) {
	seen, found, value := map[string]bool{}, false, ""
	for _, item := range component.Attributes {
		if seen[item.Name] {
			return "", false
		}
		seen[item.Name] = true
		if slices.Contains(names, item.Name) {
			if found && value != item.Value {
				return "", false
			}
			found, value = true, item.Value
		}
	}
	return value, found
}

func expectedInterfaces(components []inventory.Component) (map[string]inventory.Component, error) {
	result, ids := map[string]inventory.Component{}, map[string]bool{}
	for _, component := range components {
		if component.Type != inventory.ComponentInput {
			continue
		}
		if ids[component.ID] {
			return nil, ErrInvalid
		}
		ids[component.ID] = true
		category, ok := attribute(component, "category")
		if !ok {
			return nil, ErrInvalid
		}
		if category != "pointer" {
			continue
		}
		interfaces := map[string]bool{}
		direct := ""
		if name, ok := attribute(component, "interface"); ok {
			if !canonicalInterface(name) {
				return nil, ErrInvalid
			}
			interfaces[name] = true
			direct = name
		}
		if handlers, ok := attribute(component, "handlers"); ok {
			seen := map[string]bool{}
			for _, name := range strings.Fields(handlers) {
				if !strings.HasPrefix(name, "event") {
					continue
				}
				if !canonicalInterface(name) || seen[name] || direct != "" && name != direct {
					return nil, ErrInvalid
				}
				seen[name], interfaces[name] = true, true
			}
		}
		if len(interfaces) == 0 {
			return nil, ErrInvalid
		}
		for name := range interfaces {
			if _, exists := result[name]; exists {
				return nil, ErrInvalid
			}
			result[name] = component
		}
	}
	return result, nil
}

func bitmapBit(value string, code, width int) (bool, bool) {
	words := strings.Fields(value)
	if (width != 32 && width != 64) || len(words) == 0 || len(words) > 32 {
		return false, false
	}
	var bit bool
	for index, word := range words {
		if len(word) > width/4 {
			return false, false
		}
		parsed, err := strconv.ParseUint(word, 16, width)
		if err != nil {
			return false, false
		}
		if len(words)-1-index == code/width {
			bit = parsed&(uint64(1)<<uint(code%width)) != 0
		}
	}
	return bit, true
}

func matches(device inputtest.PointerDeviceResult, component inventory.Component, legacy bool) bool {
	if device.Identity == nil || device.Capabilities == nil {
		return false
	}
	identity, capabilities := device.Identity, device.Capabilities
	for _, field := range []struct {
		names  []string
		wanted uint16
	}{
		{[]string{"bus"}, identity.Bus}, {[]string{"vendor_id", "vendor"}, identity.Vendor},
		{[]string{"product_id", "product"}, identity.Product}, {[]string{"version"}, identity.Version},
	} {
		value, ok := attribute(component, field.names...)
		parsed, err := strconv.ParseUint(value, 16, 16)
		if !ok || err != nil || len(value) != 4 || parsed != uint64(field.wanted) {
			return false
		}
	}
	layout, ok := attribute(component, "bitmap_word_bits")
	width := 0
	switch layout {
	case "32":
		width = 32
	case "64":
		width = 64
	}
	if !ok || width == 0 {
		return false
	}
	events, eventOK := attribute(component, "ev_capabilities", "event_capabilities")
	relative, relativeOK := attribute(component, "rel_capabilities", "relative_capabilities")
	keys, keyOK := attribute(component, "key_capabilities")
	if !eventOK || !relativeOK || !keyOK {
		return false
	}
	checks := []struct {
		bitmap string
		bit    int
		want   bool
	}{
		{events, 0, true}, {events, 1, capabilities.Key}, {events, 2, capabilities.Relative}, {events, 3, capabilities.Absolute},
		{relative, 0, capabilities.RelativeX}, {relative, 1, capabilities.RelativeY},
		{keys, 272, slices.Contains(capabilities.Buttons, "left")},
		{keys, 273, slices.Contains(capabilities.Buttons, "right")},
		{keys, 274, slices.Contains(capabilities.Buttons, "middle")},
	}
	if !legacy {
		advertised := map[int]bool{6: false, 8: false, 11: false, 12: false}
		for _, axis := range capabilities.WheelAxes {
			if axis.Axis == "vertical" {
				advertised[8], advertised[11] = axis.Legacy, axis.HighResolution
			}
			if axis.Axis == "horizontal" {
				advertised[6], advertised[12] = axis.Legacy, axis.HighResolution
			}
		}
		for _, bit := range []int{6, 8, 11, 12} {
			checks = append(checks, struct {
				bitmap string
				bit    int
				want   bool
			}{relative, bit, advertised[bit]})
		}
	}
	for _, check := range checks {
		got, valid := bitmapBit(check.bitmap, check.bit, width)
		if !valid || got != check.want {
			return false
		}
	}
	return true
}

func validateExpected(result inputtest.PointerResult, expected map[string]inventory.Component) error {
	if result.Status == inputtest.StatusPass && len(expected) != len(result.Devices) {
		return ErrInvalid
	}
	if !HasPassedDevice(result) {
		return nil
	}
	for _, device := range result.Devices {
		if device.Status != inputtest.StatusPass {
			continue
		}
		component, ok := expected[device.Interface]
		if !ok || !matches(device, component, result.Schema == inputtest.PointerSchemaV1) {
			return ErrInvalid
		}
	}
	return nil
}

func ValidateInventoryBinding(result inputtest.PointerResult, components []inventory.Component) error {
	if err := ValidateResult(result); err != nil {
		return err
	}
	expected, err := expectedInterfaces(components)
	if err != nil {
		return err
	}
	return validateExpected(result, expected)
}

func graphInputs(value plan.Plan) (map[string]string, error) {
	result := map[string]string{}
	for _, component := range value.HardwareGraph.Components {
		if component.Type != inventory.ComponentInput {
			continue
		}
		if _, exists := result[component.ID]; exists {
			return nil, ErrInvalid
		}
		result[component.ID] = component.Value
	}
	return result, nil
}

func ValidatePlanBinding(result inputtest.PointerResult, test plan.Test, frozen, final plan.Plan, snapshot inventory.Snapshot) error {
	if err := ValidateResult(result); err != nil {
		return err
	}
	if test.ID != "input.pointer.quick" || (test.Version != 1 || result.Schema != inputtest.PointerSchemaV1) && (test.Version != 2 || result.Schema != inputtest.PointerSchema) || test.Status != plan.TestScheduled || test.TimeoutSeconds <= 0 || test.TimeoutSeconds > 3600 || result.LimitMilliseconds > int64(test.TimeoutSeconds)*1000 || len(test.ComponentIDs) == 0 {
		return ErrInvalid
	}
	initialGraph, err := graphInputs(frozen)
	if err != nil {
		return err
	}
	finalGraph, err := graphInputs(final)
	if err != nil {
		return err
	}
	found := 0
	for _, candidate := range frozen.Tests {
		if candidate.ID == test.ID {
			found++
			if !reflect.DeepEqual(test, candidate) {
				return ErrInvalid
			}
		}
	}
	if found != 1 {
		return ErrInvalid
	}
	var finalRefs []string
	found = 0
	for _, candidate := range final.Tests {
		if candidate.ID == test.ID {
			found++
			finalRefs = candidate.ComponentIDs
			if result.Status == inputtest.StatusPass && (candidate.Version != test.Version || candidate.Status != plan.TestScheduled) {
				return ErrInvalid
			}
		}
	}
	if found != 1 || result.Status == inputtest.StatusPass && !slices.Equal(test.ComponentIDs, finalRefs) {
		return ErrInvalid
	}
	seenRefs := map[string]bool{}
	for _, id := range test.ComponentIDs {
		if seenRefs[id] {
			return ErrInvalid
		}
		seenRefs[id] = true
	}
	selected, seen := []inventory.Component{}, map[string]bool{}
	for _, component := range snapshot.Components {
		if component.Type != inventory.ComponentInput || !slices.Contains(test.ComponentIDs, component.ID) {
			continue
		}
		if seen[component.ID] {
			return ErrInvalid
		}
		seen[component.ID] = true
		if initialGraph[component.ID] != "" && initialGraph[component.ID] == finalGraph[component.ID] && slices.Contains(finalRefs, component.ID) {
			selected = append(selected, component)
		}
	}
	if result.Status == inputtest.StatusPass && len(selected) != len(test.ComponentIDs) {
		return ErrInvalid
	}
	expected, err := expectedInterfaces(selected)
	if err != nil {
		return err
	}
	return validateExpected(result, expected)
}
