package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

const MaximumLaunchSelectionBytes = 16384

// EncodeLaunchSelection binds a console choice to its expected planner target.
// The canonical document includes an explicit array and exactly one final LF.
func EncodeLaunchSelection(profile Profile, architecture string, excluded []string) ([]byte, error) {
	canonical, err := canonicalLaunchExclusions(profile, architecture, excluded)
	if err != nil {
		return nil, err
	}
	document := struct {
		SchemaVersion int      `json:"schema_version"`
		Profile       Profile  `json:"profile"`
		Architecture  string   `json:"architecture"`
		ExcludedTests []string `json:"excluded_tests"`
	}{1, profile, architecture, canonical}
	encoded, err := json.Marshal(document)
	if err != nil || len(encoded)+1 > MaximumLaunchSelectionBytes {
		return nil, errors.New("invalid launch selection")
	}
	return append(encoded, '\n'), nil
}

// DecodeLaunchSelection validates the handoff against arguments chosen by the
// launcher. The document cannot select a different profile or architecture.
func DecodeLaunchSelection(data []byte, expectedProfile Profile, expectedArchitecture string) ([]string, error) {
	if len(data) == 0 || len(data) > MaximumLaunchSelectionBytes || !utf8.Valid(data) || !json.Valid(data) {
		return nil, errors.New("invalid launch selection")
	}
	if err := validatePlanJSONMembers(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return nil, errors.New("invalid launch selection")
	}
	var document struct {
		SchemaVersion *int      `json:"schema_version"`
		Profile       *Profile  `json:"profile"`
		Architecture  *string   `json:"architecture"`
		ExcludedTests *[]string `json:"excluded_tests"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || document.SchemaVersion == nil || document.Profile == nil || document.Architecture == nil || document.ExcludedTests == nil {
		return nil, errors.New("invalid launch selection")
	}
	if *document.SchemaVersion != 1 || *document.Profile != expectedProfile || *document.Architecture != expectedArchitecture {
		return nil, errors.New("invalid launch selection")
	}
	return canonicalLaunchExclusions(*document.Profile, *document.Architecture, *document.ExcludedTests)
}

func canonicalLaunchExclusions(profile Profile, architecture string, excluded []string) ([]string, error) {
	if (profile != ProfileQuick && profile != ProfileStandard && profile != ProfileExtended) || !validSelectionArchitecture(architecture) {
		return nil, errors.New("invalid launch selection")
	}
	catalog, _, err := loadDefinitions(definitionData)
	if err != nil {
		return nil, errors.New("invalid launch selection")
	}
	canonical, err := canonicalExclusions(catalog, profile, architecture, excluded)
	if err != nil {
		return nil, errors.New("invalid launch selection")
	}
	return canonical, nil
}
