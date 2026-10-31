package app

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchpadevidence"
)

const touchpadReportID = "input.touchpad.quick"

// ValidatedTouchpad returns independently decoded, owned observations only
// after checking the whole report. It authenticates no unobserved kernel history.
func ValidatedTouchpad(payload InventoryPayload) (*inputtest.TouchpadResult, error) {
	if err := ValidateCoverage(payload); err != nil {
		return nil, err
	}
	for _, row := range payload.TestResults {
		if row.ID == touchpadReportID && HasTouchpadClaim(row.Evidence) {
			value, err := touchpadevidence.Decode(row.Evidence)
			if err != nil {
				return nil, err
			}
			return &value, nil
		}
	}
	return nil, nil
}

func validateTouchpadReport(payload InventoryPayload) error {
	claimed, anySession := false, false
	for _, row := range payload.TestResults {
		anySession = anySession || hasReportClaim(row.Evidence, anySessionClaim)
		if HasTouchpadClaim(row.Evidence) || strings.EqualFold(row.ID, touchpadReportID) && row.Version != 1 {
			if row.ID != touchpadReportID || row.Version != 2 {
				return touchpadevidence.ErrInvalid
			}
			claimed = true
		}
	}
	if !claimed {
		return nil
	}
	if payload.CoverageScope != CoverageFrozenPlan {
		return touchpadevidence.ErrInvalid
	}
	components := make([]inventory.Component, 0, len(payload.Components))
	bindings := make([]sessionevidence.ResultBinding, 0, len(payload.TestResults))
	for _, c := range payload.Components {
		components = append(components, inventory.Component{Type: c.Type, ID: c.ID, Attributes: c.Attributes})
	}
	retained := map[string]bool{}
	var integrity *TestResult
	for i := range payload.TestResults {
		row := &payload.TestResults[i]
		if row.ID == "run.integrity.quick" {
			if integrity != nil || row.Version != 1 {
				return touchpadevidence.ErrInvalid
			}
			integrity = row
		} else {
			if hasReportClaim(row.Evidence, anySessionClaim) {
				return touchpadevidence.ErrInvalid
			}
			bindings = append(bindings, sessionevidence.ResultBinding{ID: row.ID, Version: row.Version,
				PlannedStatus: string(row.PlannedStatus), ReportStatus: string(row.Status), Evidence: row.Evidence})
		}
		if row.ID != touchpadReportID {
			continue
		}
		if row.Version != 2 {
			return touchpadevidence.ErrInvalid
		}
		if HasTouchpadClaim(row.Evidence) {
			value, err := touchpadevidence.Decode(row.Evidence)
			if err != nil {
				return err
			}
			if row.PlannedStatus != PlannedScheduled || string(value.Status) != string(row.Status) ||
				value.LimitMilliseconds != 30000 {
				return touchpadevidence.ErrInvalid
			}
			if err := touchpadevidence.ValidateInventoryBinding(value, components); err != nil {
				return err
			}
			for _, device := range value.Devices {
				if device.Status == inputtest.StatusPass {
					retained[device.Interface] = true
				}
			}
		} else if err := sessionevidence.ValidateTouchpadNote(sessionevidence.ResultBinding{
			ID: row.ID, Version: row.Version, PlannedStatus: string(row.PlannedStatus),
			ReportStatus: string(row.Status), Evidence: row.Evidence}); err != nil {
			return err
		}
	}
	if integrity == nil {
		return touchpadevidence.ErrInvalid
	}
	if !anySession {
		return validateTouchpadStandaloneIntegrity(*integrity, payload.Components, retained, bindings)
	}
	value, err := sessionevidence.DecodeIntegrity(integrity.Evidence)
	if err != nil {
		return err
	}
	if value.Session == nil || !sessionevidence.SupportsTouchpad(value.Session.Schema) ||
		value.Status != string(integrity.Status) || value.PlanID != integrity.PlanID ||
		value.PlannedHardwareGraph != integrity.HardwareGraph || value.FinalComponentCount != len(payload.Components) {
		return touchpadevidence.ErrInvalid
	}
	if err := validateTouchpadExclusions(value, bindings); err != nil {
		return err
	}
	if err := validateTouchpadIntegrityReferences(value, payload.Components, retained); err != nil {
		return err
	}
	return sessionevidence.ValidateBindings(*value.Session, bindings)
}

// Standalone metadata is structurally checked without inventing a journal,
// a monitored interval, a private key, or authority over the original inventory.
func validateTouchpadStandaloneIntegrity(row TestResult, components []ComponentRecord, retained map[string]bool, bindings []sessionevidence.ResultBinding) error {
	var value sessionevidence.IntegrityEvidence
	if row.Version != 1 || evidencejson.ValidateShape(row.Evidence, reflect.TypeOf(value)) != nil ||
		json.Unmarshal(row.Evidence, &value) != nil || value.Schema != sessionevidence.IntegritySchema ||
		value.Session != nil || value.Status != string(row.Status) || value.PlanID != row.PlanID ||
		value.PlannedHardwareGraph != row.HardwareGraph || !hardwareGraphPattern.MatchString(value.FinalHardwareGraph) ||
		value.PlannedComponentCount < 0 || value.FinalComponentCount < 0 || value.FinalComponentCount != len(components) {
		return touchpadevidence.ErrInvalid
	}
	if len(value.RemovedComponents) > value.PlannedComponentCount || len(value.AddedComponents) > value.FinalComponentCount ||
		value.PlannedComponentCount-len(value.RemovedComponents) != value.FinalComponentCount-len(value.AddedComponents) ||
		len(value.ChangedComponents) > min(value.PlannedComponentCount-len(value.RemovedComponents), value.FinalComponentCount-len(value.AddedComponents)) {
		return touchpadevidence.ErrInvalid
	}
	changed := len(value.AddedComponents) != 0 || len(value.RemovedComponents) != 0 || len(value.ChangedComponents) != 0
	if changed == (value.PlannedHardwareGraph == value.FinalHardwareGraph) ||
		value.Status != "pass" && value.Status != "fail" || (value.Status == "fail") != changed {
		return touchpadevidence.ErrInvalid
	}
	if err := validateTouchpadExclusions(value, bindings); err != nil {
		return err
	}
	return validateTouchpadIntegrityReferences(value, components, retained)
}

// An explicit plan exclusion cannot coexist with a scheduled definition-2
// touchpad row. Earlier capability/architecture dispositions may take precedence
// over selection in the planner; this does not reconstruct the private plan.
func validateTouchpadExclusions(value sessionevidence.IntegrityEvidence, bindings []sessionevidence.ResultBinding) error {
	for _, id := range value.ExcludedTests {
		if id != touchpadReportID {
			continue
		}
		for _, row := range bindings {
			if row.ID == touchpadReportID && row.Version == 2 && row.PlannedStatus == "scheduled" {
				return touchpadevidence.ErrInvalid
			}
		}
	}
	return nil
}

// Diff references are finite structural claims, not proof of private history.
// Added/changed references must exist in final inventory; removed ones must not.
// A visibly changed or newly added component cannot retain a frozen-plan pass.
func validateTouchpadIntegrityReferences(value sessionevidence.IntegrityEvidence, components []ComponentRecord, retained map[string]bool) error {
	final := make(map[plan.HardwareComponentRef]ComponentRecord, len(components))
	for _, component := range components {
		key := plan.HardwareComponentRef{Type: component.Type, ID: component.ID}
		if key.Type == "" || strings.TrimSpace(key.ID) == "" || len(key.ID) > 512 {
			return touchpadevidence.ErrInvalid
		}
		if _, exists := final[key]; exists {
			return touchpadevidence.ErrInvalid
		}
		final[key] = component
	}
	seen := make(map[plan.HardwareComponentRef]bool)
	for index, group := range [][]plan.HardwareComponentRef{value.AddedComponents, value.RemovedComponents, value.ChangedComponents} {
		for _, ref := range group {
			if ref.Type == "" || strings.TrimSpace(ref.ID) == "" || len(ref.ID) > 512 || seen[ref] {
				return touchpadevidence.ErrInvalid
			}
			seen[ref] = true
			component, exists := final[ref]
			if (index == 1) == exists {
				return touchpadevidence.ErrInvalid
			}
			if exists && component.Type == inventory.ComponentInput {
				for _, attribute := range component.Attributes {
					if attribute.Name == "interface" && retained[attribute.Value] {
						return touchpadevidence.ErrInvalid
					}
					if attribute.Name == "handlers" {
						for _, name := range strings.Fields(attribute.Value) {
							if retained[name] {
								return touchpadevidence.ErrInvalid
							}
						}
					}
				}
			}
		}
	}
	return nil
}

func touchpadInterface(name string) bool {
	value, ok := strings.CutPrefix(name, "event")
	if !ok || len(value) == 0 || len(value) > 10 {
		return false
	}
	n, err := strconv.ParseUint(value, 10, 32)
	return err == nil && strconv.FormatUint(n, 10) == value
}

// A missing width remains missing. Canonicalizing a bounded hexadecimal word
// does not choose a kernel layout or create a measured capability.
func touchpadBitmapText(text string, width, maximum int) (string, bool) {
	if len(text) == 0 || len(text) > 4096 {
		return "", false
	}
	words := strings.Fields(text)
	wordBits := width
	if wordBits == 0 {
		wordBits = 64
	}
	maxWords := maximum/32 + 1
	if width != 0 {
		maxWords = maximum/width + 1
	}
	if len(words) == 0 || len(words) > maxWords {
		return "", false
	}
	canonical := make([]string, len(words))
	for i, word := range words {
		if len(word) == 0 || len(word) > wordBits/4 {
			return "", false
		}
		for _, c := range word {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return "", false
			}
		}
		value, err := strconv.ParseUint(word, 16, wordBits)
		if err != nil {
			return "", false
		}
		if width != 0 {
			base := (len(words) - 1 - i) * width
			if remaining := maximum - base + 1; remaining < width && value>>uint(remaining) != 0 {
				return "", false
			}
		}
		canonical[i] = strconv.FormatUint(value, 16)
	}
	return strings.Join(canonical, " "), true
}

func touchpadIdentityText(text string) (string, bool) {
	if len(text) != 4 {
		return "", false
	}
	for _, c := range text {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return "", false
		}
	}
	return strings.ToLower(text), true
}

// Only new candidate publication uses this projection. The caller still uses
// the original component for identity material; no source attributes are changed.
func touchpadPublicAttributes(component inventory.Component) ([]inventory.Attribute, bool, error) {
	if component.Type != inventory.ComponentInput {
		return nil, false, nil
	}
	candidate, marked := false, false
	category, absolute := "", false
	for _, a := range component.Attributes {
		if a.Name == "category" {
			if a.Value == "touchpad" {
				candidate = true
			}
			if a.Value == "input" || a.Value == "pointer" {
				category = a.Value
			}
		}
		if (a.Name == "absolute_capabilities" || a.Name == "abs_capabilities") && a.Value != "" {
			absolute = true
		}
		if a.Name == "touchpad_candidate" {
			marked = true
		}
	}
	candidate = candidate || category != "" && absolute
	if !candidate && !marked {
		return nil, false, nil
	}
	if len(component.Attributes) > 128 {
		return nil, false, touchpadevidence.ErrInvalid
	}
	values := make(map[string]string, len(component.Attributes))
	for _, a := range component.Attributes {
		if a.Name == "" {
			return nil, false, touchpadevidence.ErrInvalid
		}
		if _, exists := values[a.Name]; exists {
			return nil, false, touchpadevidence.ErrInvalid
		}
		values[a.Name] = a.Value
	}
	category = values["category"]
	candidate = category == "touchpad" ||
		(category == "input" || category == "pointer") && (values["absolute_capabilities"] != "" || values["abs_capabilities"] != "")
	if marker, exists := values["touchpad_candidate"]; exists && marker != strconv.FormatBool(candidate) {
		return nil, false, touchpadevidence.ErrInvalid
	}
	if !candidate {
		return nil, false, nil
	}
	result := map[string]string{"category": category, "touchpad_candidate": "true"}
	names := map[string]bool{}
	direct, directPresent := values["interface"]
	if directPresent {
		if !touchpadInterface(direct) {
			return nil, false, touchpadevidence.ErrInvalid
		}
		result["interface"], names[direct] = direct, true
	}
	if handlers, exists := values["handlers"]; exists {
		if len(handlers) > 4096 {
			return nil, false, touchpadevidence.ErrInvalid
		}
		seen := map[string]bool{}
		for _, name := range strings.Fields(handlers) {
			if !strings.HasPrefix(name, "event") {
				continue
			}
			if !touchpadInterface(name) || seen[name] || directPresent && name != direct {
				return nil, false, touchpadevidence.ErrInvalid
			}
			seen[name], names[name] = true, true
		}
		ordered := make([]string, 0, len(seen))
		for name := range seen {
			ordered = append(ordered, name)
		}
		sort.Strings(ordered)
		if len(ordered) != 0 {
			result["handlers"] = strings.Join(ordered, " ")
		}
	}
	// Inventory may identify a touchpad without an event interface. Preserve
	// that absence; functional evidence binding still requires an interface.
	if len(names) > 128 {
		return nil, false, touchpadevidence.ErrInvalid
	}
	width := 0
	if values["bitmap_word_bits"] == "32" {
		width = 32
	}
	if values["bitmap_word_bits"] == "64" {
		width = 64
	}
	if width != 0 {
		result["bitmap_word_bits"] = strconv.Itoa(width)
	}
	for _, pair := range [][2]string{{"vendor_id", "vendor"}, {"product_id", "product"}} {
		a, aOK := values[pair[0]]
		b, bOK := values[pair[1]]
		if aOK && bOK {
			x, xOK := touchpadIdentityText(a)
			y, yOK := touchpadIdentityText(b)
			if !xOK || !yOK || x != y {
				return nil, false, touchpadevidence.ErrInvalid
			}
		}
	}
	for _, name := range []string{"bus", "vendor_id", "vendor", "product_id", "product", "version"} {
		if text, ok := touchpadIdentityText(values[name]); ok {
			result[name] = text
		}
	}
	fields := []struct {
		names   []string
		maximum int
	}{
		{[]string{"event_capabilities", "ev_capabilities"}, 63}, {[]string{"key_capabilities"}, 767},
		{[]string{"relative_capabilities", "rel_capabilities"}, 63}, {[]string{"absolute_capabilities", "abs_capabilities"}, 63},
		{[]string{"properties", "prop_capabilities"}, 63}, {[]string{"msc_capabilities"}, 63},
	}
	for _, field := range fields {
		canonical, present := "", false
		for _, name := range field.names {
			text, exists := values[name]
			if !exists {
				continue
			}
			value, ok := touchpadBitmapText(text, width, field.maximum)
			if len(field.names) == 2 {
				if _, alias := values[field.names[0]]; alias {
					if _, alias = values[field.names[1]]; alias && (!ok || present && canonical != value) {
						return nil, false, touchpadevidence.ErrInvalid
					}
				}
			}
			if ok {
				result[name] = value
				canonical, present = value, true
			}
		}
	}
	// Do not erase the only fact selecting an ambiguous ABS candidate.
	if category != "touchpad" && result["absolute_capabilities"] == "" && result["abs_capabilities"] == "" {
		return nil, false, touchpadevidence.ErrInvalid
	}
	keys := make([]string, 0, len(result))
	for name := range result {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	attributes := make([]inventory.Attribute, 0, len(keys))
	for _, name := range keys {
		attributes = append(attributes, inventory.Attribute{Name: name, Value: result[name]})
	}
	return attributes, true, nil
}
