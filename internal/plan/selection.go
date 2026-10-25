package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"
)

const MaximumSelectionBytes = 16 * 1024

// SelectableQuickTests returns the safe, implemented quick tests supported by
// the image. Applicability is resolved later from the inventoried hardware.
func SelectableQuickTests(architecture string) ([]string, error) {
	if !validSelectionArchitecture(architecture) {
		return nil, errors.New("invalid selection architecture")
	}
	catalog, _, err := loadDefinitions(definitionData)
	if err != nil {
		return nil, err
	}
	return selectableQuickTests(catalog, architecture), nil
}

// DecodeSelection reads the UI/planner transport document. The explicit empty
// array selects defaults; a missing or null array must not silently do so.
func DecodeSelection(encoded []byte, architecture string) ([]string, error) {
	if len(encoded) == 0 || len(encoded) > MaximumSelectionBytes || !utf8.Valid(encoded) || !json.Valid(encoded) || !validSelectionArchitecture(architecture) {
		return nil, errors.New("invalid selection document or architecture")
	}
	if err := validatePlanJSONMembers(json.NewDecoder(bytes.NewReader(encoded)), 0); err != nil {
		return nil, err
	}
	var document struct {
		ExcludedTests *[]string `json:"excluded_tests"`
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || document.ExcludedTests == nil {
		return nil, errors.New("selection requires an excluded_tests array")
	}
	catalog, _, err := loadDefinitions(definitionData)
	if err != nil {
		return nil, err
	}
	return canonicalExclusions(catalog, ProfileQuick, architecture, *document.ExcludedTests)
}

func validSelectionArchitecture(architecture string) bool {
	switch architecture {
	case "x86_64", "x86", "legacy-x86", "aarch64":
		return true
	default:
		return false
	}
}

func selectableQuickTests(catalog definitions, architecture string) []string {
	ids := []string{}
	for _, definition := range catalog.Tests {
		if definition.Implemented && !definition.Destructive && containsProfile(definition.Profiles, ProfileQuick) && !architectureUnsupported(architecture, definition.ID) {
			ids = append(ids, definition.ID)
		}
	}
	return ids
}

func canonicalExclusions(catalog definitions, profile Profile, architecture string, excludedTests []string) ([]string, error) {
	if len(excludedTests) > 0 && profile != ProfileQuick {
		return nil, errors.New("test exclusions are supported only for the quick profile")
	}
	if len(excludedTests) > 0 && !validSelectionArchitecture(architecture) {
		return nil, errors.New("test exclusions require a supported image architecture")
	}
	allowed := make(map[string]bool)
	for _, id := range selectableQuickTests(catalog, architecture) {
		allowed[id] = true
	}
	seen := make(map[string]bool, len(excludedTests))
	canonical := append([]string{}, excludedTests...)
	for _, id := range canonical {
		if !allowed[id] || seen[id] {
			return nil, fmt.Errorf("unknown, unsupported, or duplicate quick test exclusion %q", id)
		}
		seen[id] = true
	}
	sort.Strings(canonical)
	return canonical, nil
}
