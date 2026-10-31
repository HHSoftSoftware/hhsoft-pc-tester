package app

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchscreenevidence"
)

const touchscreenReportID = "input.touchscreen.quick"

// ValidatedTouchscreen returns independently decoded, owned observations only
// after checking the whole report. It authenticates no unobserved kernel history.
func ValidatedTouchscreen(payload InventoryPayload) (*inputtest.TouchscreenResult, error) {
	if err := ValidateCoverage(payload); err != nil {
		return nil, err
	}
	for _, row := range payload.TestResults {
		if row.ID == touchscreenReportID && HasTouchscreenClaim(row.Evidence) {
			value, err := touchscreenevidence.Decode(row.Evidence)
			if err != nil {
				return nil, err
			}
			return &value, nil
		}
	}
	return nil, nil
}

func validateTouchscreenReport(payload InventoryPayload) error {
	claimed, anySession := false, false
	for _, row := range payload.TestResults {
		anySession = anySession || hasReportClaim(row.Evidence, anySessionClaim)
		if HasTouchscreenClaim(row.Evidence) || strings.EqualFold(row.ID, touchscreenReportID) && row.Version != 1 {
			if row.ID != touchscreenReportID || row.Version != 2 {
				return touchscreenevidence.ErrInvalid
			}
			claimed = true
		}
	}
	if !claimed {
		return nil
	}
	if payload.CoverageScope != CoverageFrozenPlan {
		return touchscreenevidence.ErrInvalid
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
				return touchscreenevidence.ErrInvalid
			}
			integrity = row
		} else {
			if hasReportClaim(row.Evidence, anySessionClaim) {
				return touchscreenevidence.ErrInvalid
			}
			bindings = append(bindings, sessionevidence.ResultBinding{ID: row.ID, Version: row.Version,
				PlannedStatus: string(row.PlannedStatus), ReportStatus: string(row.Status), Evidence: row.Evidence})
		}
		if row.ID != touchscreenReportID {
			continue
		}
		if row.Version != 2 {
			return touchscreenevidence.ErrInvalid
		}
		if HasTouchscreenClaim(row.Evidence) {
			value, err := touchscreenevidence.Decode(row.Evidence)
			if err != nil {
				return err
			}
			if row.PlannedStatus != PlannedScheduled || string(value.Status) != string(row.Status) ||
				value.LimitMilliseconds != 45000 {
				return touchscreenevidence.ErrInvalid
			}
			if err := touchscreenevidence.ValidateInventoryBinding(value, components); err != nil {
				return err
			}
			for _, device := range value.Devices {
				if device.Status == inputtest.StatusPass {
					retained[device.Interface] = true
				}
			}
		} else if err := sessionevidence.ValidateTouchscreenNote(sessionevidence.ResultBinding{
			ID: row.ID, Version: row.Version, PlannedStatus: string(row.PlannedStatus),
			ReportStatus: string(row.Status), Evidence: row.Evidence}); err != nil {
			return err
		}
	}
	if integrity == nil {
		return touchscreenevidence.ErrInvalid
	}
	if !anySession {
		return validateTouchscreenStandaloneIntegrity(*integrity, payload.Components, retained, bindings)
	}
	value, err := sessionevidence.DecodeIntegrity(integrity.Evidence)
	if err != nil {
		return err
	}
	if value.Session == nil || !sessionevidence.SupportsTouchscreen(value.Session.Schema) ||
		value.Status != string(integrity.Status) || value.PlanID != integrity.PlanID ||
		value.PlannedHardwareGraph != integrity.HardwareGraph || value.FinalComponentCount != len(payload.Components) {
		return touchscreenevidence.ErrInvalid
	}
	if err := validateTouchscreenExclusions(value, bindings); err != nil {
		return err
	}
	if err := validateTouchscreenIntegrityReferences(value, payload.Components, retained); err != nil {
		return err
	}
	return sessionevidence.ValidateBindings(*value.Session, bindings)
}

// Standalone metadata is structurally checked without inventing a journal,
// a monitored interval, a private key, or authority over the original inventory.
func validateTouchscreenStandaloneIntegrity(row TestResult, components []ComponentRecord, retained map[string]bool, bindings []sessionevidence.ResultBinding) error {
	var value sessionevidence.IntegrityEvidence
	if row.Version != 1 || evidencejson.ValidateShape(row.Evidence, reflect.TypeOf(value)) != nil ||
		json.Unmarshal(row.Evidence, &value) != nil || value.Schema != sessionevidence.IntegritySchema ||
		value.Session != nil || value.Status != string(row.Status) || value.PlanID != row.PlanID ||
		value.PlannedHardwareGraph != row.HardwareGraph || !hardwareGraphPattern.MatchString(value.FinalHardwareGraph) ||
		value.PlannedComponentCount < 0 || value.FinalComponentCount < 0 || value.FinalComponentCount != len(components) {
		return touchscreenevidence.ErrInvalid
	}
	if len(value.RemovedComponents) > value.PlannedComponentCount || len(value.AddedComponents) > value.FinalComponentCount ||
		value.PlannedComponentCount-len(value.RemovedComponents) != value.FinalComponentCount-len(value.AddedComponents) ||
		len(value.ChangedComponents) > min(value.PlannedComponentCount-len(value.RemovedComponents), value.FinalComponentCount-len(value.AddedComponents)) {
		return touchscreenevidence.ErrInvalid
	}
	changed := len(value.AddedComponents) != 0 || len(value.RemovedComponents) != 0 || len(value.ChangedComponents) != 0
	if changed == (value.PlannedHardwareGraph == value.FinalHardwareGraph) ||
		value.Status != "pass" && value.Status != "fail" || (value.Status == "fail") != changed {
		return touchscreenevidence.ErrInvalid
	}
	if err := validateTouchscreenExclusions(value, bindings); err != nil {
		return err
	}
	return validateTouchscreenIntegrityReferences(value, components, retained)
}

// An explicit plan exclusion cannot coexist with a scheduled definition-2
// touchscreen row. Earlier capability/architecture dispositions may take precedence
// over selection in the planner; this does not reconstruct the private plan.
func validateTouchscreenExclusions(value sessionevidence.IntegrityEvidence, bindings []sessionevidence.ResultBinding) error {
	for _, id := range value.ExcludedTests {
		if id != touchscreenReportID {
			continue
		}
		for _, row := range bindings {
			if row.ID == touchscreenReportID && row.Version == 2 && row.PlannedStatus == "scheduled" {
				return touchscreenevidence.ErrInvalid
			}
		}
	}
	return nil
}

// Diff references are finite structural claims, not proof of private history.
// Added/changed references must exist in final inventory; removed ones must not.
// A visibly changed or newly added component cannot retain a frozen-plan pass.
func validateTouchscreenIntegrityReferences(value sessionevidence.IntegrityEvidence, components []ComponentRecord, retained map[string]bool) error {
	final := make(map[plan.HardwareComponentRef]ComponentRecord, len(components))
	for _, component := range components {
		key := plan.HardwareComponentRef{Type: component.Type, ID: component.ID}
		if key.Type == "" || strings.TrimSpace(key.ID) == "" || len(key.ID) > 512 {
			return touchscreenevidence.ErrInvalid
		}
		if _, exists := final[key]; exists {
			return touchscreenevidence.ErrInvalid
		}
		final[key] = component
	}
	seen := make(map[plan.HardwareComponentRef]bool)
	for index, group := range [][]plan.HardwareComponentRef{value.AddedComponents, value.RemovedComponents, value.ChangedComponents} {
		for _, ref := range group {
			if ref.Type == "" || strings.TrimSpace(ref.ID) == "" || len(ref.ID) > 512 || seen[ref] {
				return touchscreenevidence.ErrInvalid
			}
			seen[ref] = true
			component, exists := final[ref]
			if (index == 1) == exists {
				return touchscreenevidence.ErrInvalid
			}
			if exists && component.Type == inventory.ComponentInput {
				for _, attribute := range component.Attributes {
					if attribute.Name == "interface" && retained[attribute.Value] {
						return touchscreenevidence.ErrInvalid
					}
					if attribute.Name == "handlers" {
						for _, name := range strings.Fields(attribute.Value) {
							if retained[name] {
								return touchscreenevidence.ErrInvalid
							}
						}
					}
				}
			}
		}
	}
	return nil
}

func touchscreenInterface(name string) bool {
	value, ok := strings.CutPrefix(name, "event")
	if !ok || len(value) == 0 || len(value) > 10 {
		return false
	}
	n, err := strconv.ParseUint(value, 10, 32)
	return err == nil && strconv.FormatUint(n, 10) == value
}

// A missing width remains missing. Canonicalizing a bounded hexadecimal word
// does not choose a kernel layout or create a measured capability.
func touchscreenBitmapText(text string, width, maximum int) (string, bool) {
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
	// Validate every original word and its width before discarding padding.
	// Equal bit sets then have equal aliases without inventing source width.
	for len(canonical) > 1 && canonical[0] == "0" {
		canonical = canonical[1:]
	}
	return strings.Join(canonical, " "), true
}

func touchscreenIdentityText(text string) (string, bool) {
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

// Inventory labels remain useful display text, not functional evidence or proof
// that arbitrary source text is free of personal information. Suppress a known
// input identifier even when another field has copied it into a display label.
func touchscreenPublicLabel(text, uniqueID string) (string, bool) {
	if len(text) == 0 || len(text) > 256 {
		return "", false
	}
	if !utf8.ValidString(text) {
		return "", false
	}
	for _, value := range text {
		if unicode.IsControl(value) {
			return "", false
		}
	}
	if len(uniqueID) != 0 && len(uniqueID) <= len(text) && strings.Contains(text, uniqueID) {
		return "", false
	}
	return text, true
}

// Only new candidate publication uses this projection. The caller still uses
// the original component for identity material; no source attributes are changed.
func touchscreenPublicAttributes(component inventory.Component) ([]inventory.Attribute, bool, error) {
	if component.Type != inventory.ComponentInput {
		return nil, false, nil
	}
	candidate, marked := false, false
	absolute, width32, width64 := false, false, false
	for _, a := range component.Attributes {
		if a.Name == "category" && a.Value == "touchscreen" {
			candidate = true
		}
		if (a.Name == "absolute_capabilities" || a.Name == "abs_capabilities") && a.Value != "" {
			absolute = true
		}
		if a.Name == "bitmap_word_bits" {
			width32 = width32 || a.Value == "32"
			width64 = width64 || a.Value == "64"
		}
		if a.Name == "touchscreen_candidate" {
			marked = true
		}
	}
	// This conservative preselection accepts no width or alias as authority.
	// The bounded map below rejects duplicates and binds the actual width.
	if absolute && !candidate {
		for _, a := range component.Attributes {
			if (a.Name == "properties" || a.Name == "prop_capabilities") && len(a.Value) <= 4096 &&
				(width32 && inventory.TouchscreenCandidate("", "observed", a.Value, 32) ||
					width64 && inventory.TouchscreenCandidate("", "observed", a.Value, 64)) {
				candidate = true
			}
		}
	}
	if !candidate && !marked {
		return nil, false, nil
	}
	possibleCandidate := candidate
	if len(component.Attributes) > 128 {
		return nil, false, touchscreenevidence.ErrInvalid
	}
	values := make(map[string]string, len(component.Attributes))
	for _, a := range component.Attributes {
		if a.Name == "" {
			return nil, false, touchscreenevidence.ErrInvalid
		}
		if _, exists := values[a.Name]; exists {
			return nil, false, touchscreenevidence.ErrInvalid
		}
		values[a.Name] = a.Value
	}
	category := values["category"]
	absoluteText := values["absolute_capabilities"]
	if absoluteText == "" {
		absoluteText = values["abs_capabilities"]
	}
	properties := values["properties"]
	if properties == "" {
		properties = values["prop_capabilities"]
	}
	width := 0
	if values["bitmap_word_bits"] == "32" {
		width = 32
	}
	if values["bitmap_word_bits"] == "64" {
		width = 64
	}
	candidate = category == "touchscreen" || len(properties) <= 4096 &&
		inventory.TouchscreenCandidate(category, absoluteText, properties, width)
	if marker, exists := values["touchscreen_candidate"]; exists && marker != strconv.FormatBool(candidate) {
		return nil, false, touchscreenevidence.ErrInvalid
	}
	if !candidate {
		if possibleCandidate {
			return nil, false, touchscreenevidence.ErrInvalid
		}
		return nil, false, nil
	}
	result := map[string]string{"category": category, "touchscreen_candidate": "true"}
	// A misleading touchpad category can legitimately select both workers.
	// Keep its derived marker without trusting an injected marker value.
	touchpadCandidate := category == "touchpad" ||
		(category == "input" || category == "pointer") && absoluteText != ""
	if marker, exists := values["touchpad_candidate"]; exists && marker != strconv.FormatBool(touchpadCandidate) {
		return nil, false, touchscreenevidence.ErrInvalid
	}
	if touchpadCandidate {
		result["touchpad_candidate"] = "true"
	} else {
		// Preserve useful touchscreen inventory labels without weakening the
		// stricter touchpad or dual-candidate publication contract.
		for _, name := range []string{"name", "model"} {
			if label, ok := touchscreenPublicLabel(values[name], values["unique_id"]); ok {
				result[name] = label
			}
		}
	}
	names := map[string]bool{}
	direct, directPresent := values["interface"]
	if directPresent {
		if !touchscreenInterface(direct) {
			return nil, false, touchscreenevidence.ErrInvalid
		}
		result["interface"], names[direct] = direct, true
	}
	if handlers, exists := values["handlers"]; exists {
		if len(handlers) > 4096 {
			return nil, false, touchscreenevidence.ErrInvalid
		}
		seen := map[string]bool{}
		for _, name := range strings.Fields(handlers) {
			if !strings.HasPrefix(name, "event") {
				continue
			}
			if !touchscreenInterface(name) || seen[name] || directPresent && name != direct {
				return nil, false, touchscreenevidence.ErrInvalid
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
	// Inventory may identify a touchscreen without an event interface. Preserve
	// that absence; functional evidence binding still requires an interface.
	if len(names) > 128 {
		return nil, false, touchscreenevidence.ErrInvalid
	}
	if width != 0 {
		result["bitmap_word_bits"] = strconv.Itoa(width)
	}
	for _, pair := range [][2]string{{"vendor_id", "vendor"}, {"product_id", "product"}} {
		a, aOK := values[pair[0]]
		b, bOK := values[pair[1]]
		if aOK && bOK {
			x, xOK := touchscreenIdentityText(a)
			y, yOK := touchscreenIdentityText(b)
			if !xOK || !yOK || x != y {
				return nil, false, touchscreenevidence.ErrInvalid
			}
		}
	}
	for _, name := range []string{"bus", "vendor_id", "vendor", "product_id", "product", "version"} {
		if text, ok := touchscreenIdentityText(values[name]); ok {
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
			value, ok := touchscreenBitmapText(text, width, field.maximum)
			if len(field.names) == 2 {
				if _, alias := values[field.names[0]]; alias {
					if _, alias = values[field.names[1]]; alias && (!ok || present && canonical != value) {
						return nil, false, touchscreenevidence.ErrInvalid
					}
				}
			}
			if ok {
				result[name] = value
				canonical, present = value, true
			}
		}
	}
	// Publication cannot erase the observations selecting a DIRECT candidate.
	publishedAbsolute := result["absolute_capabilities"]
	if publishedAbsolute == "" {
		publishedAbsolute = result["abs_capabilities"]
	}
	publishedProperties := result["properties"]
	if publishedProperties == "" {
		publishedProperties = result["prop_capabilities"]
	}
	if !inventory.TouchscreenCandidate(category, publishedAbsolute, publishedProperties, width) {
		return nil, false, touchscreenevidence.ErrInvalid
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
