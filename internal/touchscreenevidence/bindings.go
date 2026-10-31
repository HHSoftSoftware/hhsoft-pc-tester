package touchscreenevidence

import (
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

type candidate struct {
	component  inventory.Component
	attributes map[string]string
}

func attributes(component inventory.Component) (map[string]string, bool) {
	values := make(map[string]string, len(component.Attributes))
	for _, item := range component.Attributes {
		if item.Name == "" {
			return nil, false
		}
		if _, exists := values[item.Name]; exists {
			return nil, false
		}
		values[item.Name] = item.Value
	}
	// Conflicting aliases must not choose a candidate classification.
	// Numeric equality for identity and bitmap aliases is checked below.
	a, aOK := values["absolute_capabilities"]
	b, bOK := values["abs_capabilities"]
	if aOK && bOK && (a == "") != (b == "") {
		return nil, false
	}
	if !aliasesAgree(values) {
		return nil, false
	}
	return values, true
}

func aliasesAgree(values map[string]string) bool {
	for _, names := range [][2]string{{"vendor_id", "vendor"}, {"product_id", "product"}} {
		a, aOK := values[names[0]]
		b, bOK := values[names[1]]
		if aOK && bOK {
			x, xOK := hexWord(a, 16)
			y, yOK := hexWord(b, 16)
			if !xOK || !yOK || len(a) != 4 || len(b) != 4 || x != y {
				return false
			}
		}
	}
	for _, names := range [][2]string{{"event_capabilities", "ev_capabilities"},
		{"relative_capabilities", "rel_capabilities"}, {"absolute_capabilities", "abs_capabilities"},
		{"properties", "prop_capabilities"}} {
		a, aOK := values[names[0]]
		b, bOK := values[names[1]]
		if aOK && bOK && a != b {
			width := 0
			switch values["bitmap_word_bits"] {
			case "32":
				width = 32
			case "64":
				width = 64
			}
			x, xOK := bitmap(a, width, 63, 64)
			y, yOK := bitmap(b, width, 63, 64)
			if !xOK || !yOK || !slices.Equal(x, y) {
				return false
			}
		}
	}
	return true
}

func candidateInterfaces(components []inventory.Component) (map[string]candidate, error) {
	result, ids := map[string]candidate{}, map[string]bool{}
	for _, component := range components {
		if component.Type != inventory.ComponentInput {
			continue
		}
		if component.ID == "" || ids[component.ID] {
			return nil, ErrInvalid
		}
		ids[component.ID] = true
		values, ok := attributes(component)
		if !ok {
			return nil, ErrInvalid
		}
		category, exists := values["category"]
		if !exists {
			return nil, ErrInvalid
		}
		absolute := values["absolute_capabilities"]
		if absolute == "" {
			absolute = values["abs_capabilities"]
		}
		properties := values["properties"]
		if properties == "" {
			properties = values["prop_capabilities"]
		}
		width := 0
		switch values["bitmap_word_bits"] {
		case "32":
			width = 32
		case "64":
			width = 64
		}
		isCandidate := inventory.TouchscreenCandidate(category, absolute, properties, width)
		marker, marked := values["touchscreen_candidate"]
		if marked && marker != strconv.FormatBool(isCandidate) || isCandidate && !marked {
			return nil, ErrInvalid
		}
		if !isCandidate {
			continue
		}
		names := map[string]bool{}
		direct := ""
		if name, present := values["interface"]; present {
			if !canonicalInterface(name) {
				return nil, ErrInvalid
			}
			direct, names[name] = name, true
		}
		if handlers, present := values["handlers"]; present {
			if len(handlers) > 4096 {
				return nil, ErrInvalid
			}
			seen := map[string]bool{}
			for _, name := range strings.Fields(handlers) {
				if !strings.HasPrefix(name, "event") {
					continue
				}
				if !canonicalInterface(name) || seen[name] || direct != "" && name != direct {
					return nil, ErrInvalid
				}
				seen[name], names[name] = true, true
			}
		}
		if len(names) == 0 || len(names) > maxDevices {
			return nil, ErrInvalid
		}
		for name := range names {
			if _, exists := result[name]; exists || len(result) >= maxDevices {
				return nil, ErrInvalid
			}
			result[name] = candidate{component, values}
		}
	}
	return result, nil
}

func hexWord(text string, bits int) (uint64, bool) {
	if len(text) == 0 || len(text) > bits/4 {
		return 0, false
	}
	for _, digit := range text {
		if !(digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f' || digit >= 'A' && digit <= 'F') {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(text, 16, bits)
	return value, err == nil
}

// Decode every represented bit, not a selected subset. The observed kernel word
// width fixes ordering; missing data and overwide zero words are not guessed.
func bitmap(text string, width, maximum, count int) ([]uint16, bool) {
	if (width != 32 && width != 64) || len(text) == 0 || len(text) > 512 {
		return nil, false
	}
	words := strings.Fields(text)
	if len(words) == 0 || len(words) > (maximum+width)/width {
		return nil, false
	}
	result := []uint16{}
	for i := len(words) - 1; i >= 0; i-- {
		value, ok := hexWord(words[i], width)
		if !ok {
			return nil, false
		}
		offset := (len(words) - 1 - i) * width
		for bit := 0; bit < width; bit++ {
			if value&(uint64(1)<<uint(bit)) != 0 {
				code := offset + bit
				if code > maximum || len(result) >= count {
					return nil, false
				}
				result = append(result, uint16(code))
			}
		}
	}
	return result, true
}

func bitmapAliases(values map[string]string, names []string, width, maximum, count int) ([]uint16, bool) {
	var result []uint16
	found := false
	for _, name := range names {
		text, present := values[name]
		if !present {
			continue
		}
		decoded, ok := bitmap(text, width, maximum, count)
		if !ok || found && !slices.Equal(result, decoded) {
			return nil, false
		}
		result, found = decoded, true
	}
	return result, found
}

func identityAliases(values map[string]string, names []string, wanted uint16) bool {
	found := false
	for _, name := range names {
		text, exists := values[name]
		if !exists {
			continue
		}
		value, ok := hexWord(text, 16)
		if !ok || len(text) != 4 || value != uint64(wanted) {
			return false
		}
		found = true
	}
	return found
}

func matches(d inputtest.TouchscreenDeviceResult, c candidate) bool {
	if d.Identity == nil || d.Capabilities == nil {
		return false
	}
	for _, item := range []struct {
		names []string
		value uint16
	}{
		{[]string{"bus"}, d.Identity.Bus},
		{[]string{"vendor_id", "vendor"}, d.Identity.Vendor},
		{[]string{"product_id", "product"}, d.Identity.Product},
		{[]string{"version"}, d.Identity.Version},
	} {
		if !identityAliases(c.attributes, item.names, item.value) {
			return false
		}
	}
	width := 0
	switch c.attributes["bitmap_word_bits"] {
	case "32":
		width = 32
	case "64":
		width = 64
	default:
		return false
	}
	caps := d.Capabilities
	axes := make([]uint16, len(caps.Axes))
	for i, axis := range caps.Axes {
		axes[i] = axis.Code
	}
	for _, item := range []struct {
		names          []string
		wanted         []uint16
		maximum, count int
	}{
		{[]string{"event_capabilities", "ev_capabilities"}, caps.Events, 63, 8},
		{[]string{"key_capabilities"}, caps.Keys, 767, 8},
		{[]string{"relative_capabilities", "rel_capabilities"}, caps.Relative, 63, 4},
		{[]string{"absolute_capabilities", "abs_capabilities"}, axes, 63, 4},
		{[]string{"properties", "prop_capabilities"}, caps.Properties, 63, 8},
		{[]string{"msc_capabilities"}, caps.Misc, 63, 2},
	} {
		got, ok := bitmapAliases(c.attributes, item.names, width, item.maximum, item.count)
		if !ok || !slices.Equal(got, item.wanted) {
			return false
		}
	}
	return true
}

func validateExpected(result inputtest.TouchscreenResult, expected map[string]candidate) error {
	if result.Status == inputtest.StatusPass && len(expected) != len(result.Devices) {
		return ErrInvalid
	}
	for _, device := range result.Devices {
		component, found := expected[device.Interface]
		if !found || device.Status == inputtest.StatusPass && !matches(device, component) {
			return ErrInvalid
		}
	}
	return nil
}

// ValidateInventoryBinding binds every row to a final discovery candidate and
// every retained pass to complete identity/capability observations. ABS ranges
// are internally checked configuration, not independently inventory-measured.
func ValidateInventoryBinding(result inputtest.TouchscreenResult, components []inventory.Component) error {
	if err := ValidateResult(result); err != nil {
		return err
	}
	expected, err := candidateInterfaces(components)
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
		if component.ID == "" || component.Value == "" {
			return nil, ErrInvalid
		}
		if _, exists := result[component.ID]; exists {
			return nil, ErrInvalid
		}
		result[component.ID] = component.Value
	}
	return result, nil
}

func componentRefs(refs []string, allowEmpty bool) (map[string]bool, error) {
	if !allowEmpty && len(refs) == 0 || len(refs) > maxDevices {
		return nil, ErrInvalid
	}
	result := map[string]bool{}
	for _, id := range refs {
		if id == "" || result[id] {
			return nil, ErrInvalid
		}
		result[id] = true
	}
	return result, nil
}

// ValidatePlanBinding does not decode or upgrade a catalog. The caller supplies
// already checked plans whose original inventory graph was computed before any
// public projection. Matching graph values establish continuity of the supplied
// claims, not authenticity of unseen hardware history.
func ValidatePlanBinding(result inputtest.TouchscreenResult, test plan.Test, frozen, final plan.Plan, snapshot inventory.Snapshot) error {
	if err := ValidateResult(result); err != nil {
		return err
	}
	if test.ID != "input.touchscreen.quick" || test.Version != 2 || test.Status != plan.TestScheduled ||
		test.Destructive || test.TimeoutSeconds <= 0 || test.TimeoutSeconds > 3600 ||
		result.LimitMilliseconds > int64(test.TimeoutSeconds)*1000 ||
		snapshot.SchemaVersion != inventory.SnapshotSchemaVersion {
		return ErrInvalid
	}
	initialRefs, err := componentRefs(test.ComponentIDs, false)
	if err != nil {
		return err
	}
	initialGraph, err := graphInputs(frozen)
	if err != nil {
		return err
	}
	for id := range initialRefs {
		if initialGraph[id] == "" {
			return ErrInvalid
		}
	}
	finalGraph, err := graphInputs(final)
	if err != nil {
		return err
	}
	found := 0
	for _, current := range frozen.Tests {
		if current.ID == test.ID {
			found++
			if !reflect.DeepEqual(current, test) {
				return ErrInvalid
			}
		}
	}
	if found != 1 {
		return ErrInvalid
	}
	var finalTest plan.Test
	found = 0
	for _, current := range final.Tests {
		if current.ID == test.ID {
			found++
			finalTest = current
		}
	}
	if found != 1 {
		return ErrInvalid
	}
	finalRefs, err := componentRefs(finalTest.ComponentIDs, true)
	if err != nil {
		return err
	}
	expected, err := candidateInterfaces(snapshot.Components)
	if err != nil {
		return err
	}
	if err := validateExpected(result, expected); err != nil {
		return err
	}
	hasPass := false
	for _, device := range result.Devices {
		if device.Status == inputtest.StatusPass {
			hasPass = true
			id := expected[device.Interface].component.ID
			if !initialRefs[id] || !finalRefs[id] || initialGraph[id] == "" ||
				initialGraph[id] != finalGraph[id] {
				return ErrInvalid
			}
		}
	}
	if hasPass && (finalTest.Version != 2 || finalTest.Status != plan.TestScheduled || finalTest.Destructive ||
		frozen.HardwareGraph.Algorithm == "" || frozen.HardwareGraph.KeyEpoch == "" ||
		frozen.HardwareGraph.Algorithm != final.HardwareGraph.Algorithm ||
		frozen.HardwareGraph.KeyEpoch != final.HardwareGraph.KeyEpoch) {
		return ErrInvalid
	}
	if result.Status == inputtest.StatusPass {
		if !slices.Equal(test.ComponentIDs, finalTest.ComponentIDs) {
			return ErrInvalid
		}
		candidateIDs := map[string]bool{}
		for _, item := range expected {
			id := item.component.ID
			if !initialRefs[id] || !finalRefs[id] || initialGraph[id] == "" || initialGraph[id] != finalGraph[id] {
				return ErrInvalid
			}
			candidateIDs[id] = true
		}
		if len(candidateIDs) != len(initialRefs) {
			return ErrInvalid
		}
	}
	return nil
}
