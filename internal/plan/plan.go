package plan

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

const SchemaVersion = 3

const WholeDeviceOperator = "whole_device_operator"

const fingerprintKeyBytes = 32

var (
	ErrFingerprintKeyLength = errors.New("fingerprint key must contain exactly 32 bytes")
	validProfiles           = map[Profile]struct{}{ProfileQuick: {}, ProfileStandard: {}, ProfileExtended: {}, ProfileDestructive: {}}
)

//go:embed definitions.json
var definitionData []byte

type Profile string

const (
	ProfileQuick       Profile = "quick"
	ProfileStandard    Profile = "standard"
	ProfileExtended    Profile = "extended"
	ProfileDestructive Profile = "destructive"
)

type CapabilityStatus string

const (
	CapabilityPresent       CapabilityStatus = "present"
	CapabilityAbsent        CapabilityStatus = "absent"
	CapabilityMissingDriver CapabilityStatus = "missing_driver"
)

type TestStatus string

const (
	TestScheduled     TestStatus = "scheduled"
	TestExcluded      TestStatus = "excluded"
	TestNotPresent    TestStatus = "not_present"
	TestUnsupported   TestStatus = "unsupported"
	TestMissingDriver TestStatus = "missing_driver"
)

type HardwareGraph struct {
	Algorithm  string              `json:"algorithm"`
	KeyEpoch   string              `json:"key_epoch"`
	Value      string              `json:"value"`
	Components []HardwareComponent `json:"components"`
}

type HardwareComponent struct {
	Type  inventory.ComponentType `json:"type"`
	ID    string                  `json:"id"`
	Value string                  `json:"value"`
}

type HardwareComponentRef struct {
	Type inventory.ComponentType `json:"type"`
	ID   string                  `json:"id"`
}

type HardwareDelta struct {
	Added   []HardwareComponentRef `json:"added,omitempty"`
	Removed []HardwareComponentRef `json:"removed,omitempty"`
	Changed []HardwareComponentRef `json:"changed,omitempty"`
}

type Capability struct {
	ID            string           `json:"id"`
	Status        CapabilityStatus `json:"status"`
	Applicability string           `json:"applicability,omitempty"`
	ComponentIDs  []string         `json:"component_ids,omitempty"`
}

type Test struct {
	ID               string     `json:"id"`
	Version          int        `json:"version"`
	Status           TestStatus `json:"status"`
	Capability       string     `json:"capability"`
	Applicability    string     `json:"applicability,omitempty"`
	ComponentIDs     []string   `json:"component_ids,omitempty"`
	Destructive      bool       `json:"destructive,omitempty"`
	EstimatedSeconds int        `json:"estimated_seconds"`
	TimeoutSeconds   int        `json:"timeout_seconds"`
}

type Plan struct {
	SchemaVersion     int           `json:"schema_version"`
	ID                string        `json:"id"`
	Frozen            bool          `json:"frozen"`
	Profile           Profile       `json:"profile"`
	Architecture      string        `json:"architecture"`
	HardwareGraph     HardwareGraph `json:"hardware_graph"`
	DefinitionsDigest string        `json:"definitions_digest"`
	Capabilities      []Capability  `json:"capabilities"`
	Tests             []Test        `json:"tests"`
	ExcludedTests     []string      `json:"excluded_tests,omitempty"`
}

type definitions struct {
	Capabilities []capabilityDefinition `json:"capabilities"`
	Tests        []testDefinition       `json:"tests"`
}

type capabilityDefinition struct {
	ID            string           `json:"id"`
	Applicability string           `json:"applicability,omitempty"`
	Hardware      []componentMatch `json:"hardware"`
	Providers     []componentMatch `json:"providers"`
}

type componentMatch struct {
	Type      inventory.ComponentType `json:"type"`
	Attribute string                  `json:"attribute"`
	Equals    string                  `json:"equals"`
	NotEquals string                  `json:"not_equals"`
	Prefix    string                  `json:"prefix"`
}

type testDefinition struct {
	ID               string    `json:"id"`
	Version          int       `json:"version"`
	Implemented      bool      `json:"implemented"`
	Capability       string    `json:"capability"`
	Profiles         []Profile `json:"profiles"`
	Destructive      bool      `json:"destructive"`
	EstimatedSeconds int       `json:"estimated_seconds"`
	TimeoutSeconds   int       `json:"timeout_seconds"`
}

func Build(snapshot inventory.Snapshot, profile Profile, architecture string, fingerprintKey []byte, keyEpoch string) (Plan, error) {
	return BuildWithSelection(snapshot, profile, architecture, fingerprintKey, keyEpoch, nil)
}

func BuildWithSelection(snapshot inventory.Snapshot, profile Profile, architecture string, fingerprintKey []byte, keyEpoch string, excludedTests []string) (Plan, error) {
	if len(fingerprintKey) != fingerprintKeyBytes {
		return Plan{}, ErrFingerprintKeyLength
	}
	if _, ok := validProfiles[profile]; !ok {
		return Plan{}, fmt.Errorf("invalid profile %q", profile)
	}
	architecture = strings.TrimSpace(architecture)
	if architecture == "" {
		return Plan{}, errors.New("architecture is required")
	}
	keyEpoch = strings.TrimSpace(keyEpoch)
	if keyEpoch == "" {
		keyEpoch = "local-1"
	}

	definitions, definitionsDigest, err := loadDefinitions(definitionData)
	if err != nil {
		return Plan{}, err
	}
	excludedTests, err = canonicalExclusions(definitions, profile, architecture, excludedTests)
	if err != nil {
		return Plan{}, err
	}
	canonicalSnapshot := inventory.NewSnapshot(snapshot.Components)
	hardwareGraph, err := ObserveHardwareGraph(canonicalSnapshot, fingerprintKey, keyEpoch)
	if err != nil {
		return Plan{}, err
	}

	result := Plan{
		SchemaVersion:     SchemaVersion,
		Frozen:            true,
		Profile:           profile,
		Architecture:      architecture,
		HardwareGraph:     hardwareGraph,
		DefinitionsDigest: definitionsDigest,
		ExcludedTests:     excludedTests,
	}
	capabilityByID := make(map[string]Capability, len(definitions.Capabilities))
	for _, definition := range definitions.Capabilities {
		capability := resolveCapability(canonicalSnapshot.Components, definition)
		result.Capabilities = append(result.Capabilities, capability)
		capabilityByID[capability.ID] = capability
	}
	for _, definition := range definitions.Tests {
		capability := capabilityByID[definition.Capability]
		test := Test{
			ID: definition.ID, Version: definition.Version, Capability: definition.Capability, Applicability: capability.Applicability,
			ComponentIDs: append([]string(nil), capability.ComponentIDs...), Destructive: definition.Destructive,
			EstimatedSeconds: definition.EstimatedSeconds, TimeoutSeconds: definition.TimeoutSeconds,
		}
		test.Status = testStatus(definition, capability.Status, profile, architecture, excludedTests)
		result.Tests = append(result.Tests, test)
	}
	unsigned := result
	unsigned.ID = ""
	encoded, err := json.Marshal(unsigned)
	if err != nil {
		return Plan{}, fmt.Errorf("encode plan: %w", err)
	}
	digest := sha256.Sum256(encoded)
	result.ID = "sha256:" + hex.EncodeToString(digest[:])
	if err := Validate(result); err != nil {
		return Plan{}, err
	}
	return result, nil
}

func ObserveHardwareGraph(snapshot inventory.Snapshot, fingerprintKey []byte, keyEpoch string) (HardwareGraph, error) {
	if len(fingerprintKey) != fingerprintKeyBytes {
		return HardwareGraph{}, ErrFingerprintKeyLength
	}
	keyEpoch = strings.TrimSpace(keyEpoch)
	if keyEpoch == "" {
		keyEpoch = "local-1"
	}
	stableSnapshot := stableHardwareGraph(inventory.NewSnapshot(snapshot.Components).Components)
	encoded, err := json.Marshal(stableSnapshot)
	if err != nil {
		return HardwareGraph{}, fmt.Errorf("encode hardware graph: %w", err)
	}
	graph := HardwareGraph{Algorithm: "hmac-sha256", KeyEpoch: keyEpoch, Value: keyedDigest(fingerprintKey, "hhpt-plan-hardware-v1\x00", encoded)}
	for _, component := range stableSnapshot.Components {
		encoded, err := json.Marshal(component)
		if err != nil {
			return HardwareGraph{}, fmt.Errorf("encode hardware component: %w", err)
		}
		graph.Components = append(graph.Components, HardwareComponent{Type: component.Type, ID: component.ID, Value: keyedDigest(fingerprintKey, "hhpt-plan-component-v1\x00", encoded)})
	}
	return graph, nil
}

func CompareHardwareGraphs(initial, final HardwareGraph) HardwareDelta {
	initialComponents := make(map[string]HardwareComponent, len(initial.Components))
	finalComponents := make(map[string]HardwareComponent, len(final.Components))
	for _, component := range initial.Components {
		initialComponents[string(component.Type)+"\x00"+component.ID] = component
	}
	for _, component := range final.Components {
		finalComponents[string(component.Type)+"\x00"+component.ID] = component
	}
	var delta HardwareDelta
	for key, component := range initialComponents {
		observed, exists := finalComponents[key]
		if !exists {
			delta.Removed = append(delta.Removed, HardwareComponentRef{Type: component.Type, ID: component.ID})
		} else if observed.Value != component.Value {
			delta.Changed = append(delta.Changed, HardwareComponentRef{Type: component.Type, ID: component.ID})
		}
	}
	for key, component := range finalComponents {
		if _, exists := initialComponents[key]; !exists {
			delta.Added = append(delta.Added, HardwareComponentRef{Type: component.Type, ID: component.ID})
		}
	}
	sortRefs(delta.Added)
	sortRefs(delta.Removed)
	sortRefs(delta.Changed)
	return delta
}

func Decode(encoded []byte) (Plan, error) {
	if !utf8.Valid(encoded) || !json.Valid(encoded) {
		return Plan{}, errors.New("decode plan: invalid JSON")
	}
	if err := validatePlanJSONMembers(json.NewDecoder(bytes.NewReader(encoded)), 0); err != nil {
		return Plan{}, err
	}
	var result Plan
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return Plan{}, fmt.Errorf("decode plan: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Plan{}, errors.New("decode plan: trailing JSON value")
	}
	if err := Validate(result); err != nil {
		return Plan{}, err
	}
	return result, nil
}

func validatePlanJSONMembers(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("decode plan: excessive JSON nesting")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	seen := make(map[string]bool)
	for decoder.More() {
		if delimiter == '{' {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return errors.New("decode plan: duplicate JSON member")
			}
			// Every plan JSON tag is lowercase ASCII. This also rejects
			// Unicode case aliases accepted by encoding/json's field matching.
			for _, character := range key {
				if !(character >= 'a' && character <= 'z' || character == '_') {
					return errors.New("decode plan: noncanonical JSON member")
				}
			}
			seen[key] = true
			if key == "applicability" {
				var applicability *string
				if decoder.Decode(&applicability) != nil || applicability == nil {
					return errors.New("decode plan: applicability must be a string")
				}
				continue
			}
			if key == "excluded_tests" {
				var selected *[]string
				if err := decoder.Decode(&selected); err != nil || selected == nil {
					return errors.New("decode plan: excluded_tests must be an array of test IDs")
				}
				continue
			}
		}
		if err := validatePlanJSONMembers(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

// Validate checks a frozen execution plan against the embedded catalog. It
// validates the hardware bindings' structure, not their keyed authenticity or
// attribute-dependent applicability; those require the original snapshot/key.
// Historical signed-report verification does not use this execution-plan gate.
func Validate(result Plan) error {
	if result.SchemaVersion != SchemaVersion || !result.Frozen {
		return errors.New("invalid frozen plan metadata")
	}
	if _, ok := validProfiles[result.Profile]; !ok || result.Architecture == "" || strings.TrimSpace(result.Architecture) != result.Architecture || len(result.Architecture) > 64 {
		return errors.New("invalid frozen plan target")
	}
	if result.HardwareGraph.Algorithm != "hmac-sha256" || result.HardwareGraph.KeyEpoch == "" || !validDigest(result.HardwareGraph.Value, "hmac-sha256:") || !validDigest(result.DefinitionsDigest, "sha256:") || !validDigest(result.ID, "sha256:") {
		return errors.New("invalid frozen plan binding")
	}
	previous := ""
	componentByID := make(map[string]int, len(result.HardwareGraph.Components))
	for index, component := range result.HardwareGraph.Components {
		key := string(component.Type) + "\x00" + component.ID
		if component.Type == "" || component.ID == "" || key <= previous || !validDigest(component.Value, "hmac-sha256:") {
			return errors.New("invalid frozen plan component binding")
		}
		// Capability references contain IDs only, so an ID shared by different
		// component types would make those references ambiguous.
		if _, exists := componentByID[component.ID]; exists {
			return errors.New("ambiguous frozen plan component ID")
		}
		componentByID[component.ID] = index
		previous = key
	}
	if err := validateCatalog(result, componentByID); err != nil {
		return err
	}
	unsigned := result
	unsigned.ID = ""
	encoded, err := json.Marshal(unsigned)
	if err != nil {
		return fmt.Errorf("validate plan: %w", err)
	}
	digest := sha256.Sum256(encoded)
	if result.ID != "sha256:"+hex.EncodeToString(digest[:]) {
		return errors.New("invalid frozen plan ID")
	}
	return nil
}

func validateCatalog(result Plan, componentByID map[string]int) error {
	catalog, digest, err := loadDefinitions(definitionData)
	if err != nil {
		return err
	}
	if result.DefinitionsDigest != digest {
		return errors.New("frozen plan does not match current test definitions")
	}
	canonical, err := canonicalExclusions(catalog, result.Profile, result.Architecture, result.ExcludedTests)
	if err != nil || !slices.Equal(canonical, result.ExcludedTests) {
		return errors.New("invalid or noncanonical frozen test exclusions")
	}
	if len(result.Capabilities) != len(catalog.Capabilities) || len(result.Tests) != len(catalog.Tests) {
		return errors.New("incomplete frozen plan catalog")
	}
	capabilityByID := make(map[string]Capability, len(result.Capabilities))
	for index, definition := range catalog.Capabilities {
		capability := result.Capabilities[index]
		if capability.ID != definition.ID {
			return errors.New("invalid frozen plan capability order or identity")
		}
		if err := validateCapability(capability, definition, result.HardwareGraph.Components, componentByID); err != nil {
			return fmt.Errorf("invalid frozen plan capability %q: %w", capability.ID, err)
		}
		capabilityByID[capability.ID] = capability
	}
	for index, definition := range catalog.Tests {
		test := result.Tests[index]
		capability := capabilityByID[definition.Capability]
		if test.ID != definition.ID || test.Version != definition.Version || test.Capability != definition.Capability ||
			test.Applicability != capability.Applicability || test.Destructive != definition.Destructive || test.EstimatedSeconds != definition.EstimatedSeconds ||
			test.TimeoutSeconds != definition.TimeoutSeconds || !slices.Equal(test.ComponentIDs, capability.ComponentIDs) ||
			test.Status != testStatus(definition, capability.Status, result.Profile, result.Architecture, result.ExcludedTests) {
			return fmt.Errorf("frozen plan test %q does not match its catalog definition or capability", test.ID)
		}
	}
	return nil
}

func validateCapability(capability Capability, definition capabilityDefinition, components []HardwareComponent, componentByID map[string]int) error {
	if capability.Applicability != definition.Applicability {
		return errors.New("capability applicability does not match its definition")
	}
	if definition.Applicability == WholeDeviceOperator {
		if capability.ID != "operator.exterior" || capability.Status != CapabilityPresent || len(capability.ComponentIDs) != 0 {
			return errors.New("invalid whole-device operator capability")
		}
		return nil
	}
	var matches []componentMatch
	switch capability.Status {
	case CapabilityAbsent:
		if len(capability.ComponentIDs) != 0 {
			return errors.New("absent capability has component references")
		}
	case CapabilityPresent:
		matches = definition.Providers
	case CapabilityMissingDriver:
		matches = definition.Hardware
	default:
		return errors.New("unknown capability status")
	}
	if capability.Status != CapabilityAbsent && len(capability.ComponentIDs) == 0 {
		return errors.New("applicable capability has no component references")
	}
	previous := -1
	referenced := make(map[string]bool, len(capability.ComponentIDs))
	for _, id := range capability.ComponentIDs {
		index, exists := componentByID[id]
		if !exists || index <= previous || !matchesComponentType(components[index].Type, matches) {
			return errors.New("invalid component reference or reference order")
		}
		previous = index
		referenced[id] = true
	}
	// A type-only provider/hardware rule is decidable from the graph. Rules
	// requiring attributes cannot be reconstructed from the component HMAC.
	for _, component := range components {
		if unconditionalComponentType(component.Type, definition.Providers) {
			if capability.Status != CapabilityPresent || !referenced[component.ID] {
				return errors.New("unconditional provider omitted")
			}
		}
		if capability.Status != CapabilityPresent && unconditionalComponentType(component.Type, definition.Hardware) {
			if capability.Status != CapabilityMissingDriver || !referenced[component.ID] {
				return errors.New("unconditional hardware omitted")
			}
		}
	}
	return nil
}

func matchesComponentType(componentType inventory.ComponentType, matches []componentMatch) bool {
	for _, match := range matches {
		if match.Type == componentType {
			return true
		}
	}
	return false
}

func unconditionalComponentType(componentType inventory.ComponentType, matches []componentMatch) bool {
	for _, match := range matches {
		if match.Type == componentType && match.Attribute == "" && match.Equals == "" && match.Prefix == "" {
			return true
		}
	}
	return false
}

func testStatus(definition testDefinition, capabilityStatus CapabilityStatus, profile Profile, architecture string, excludedTests []string) TestStatus {
	switch {
	case capabilityStatus == CapabilityAbsent:
		return TestNotPresent
	case architectureUnsupported(architecture, definition.ID):
		return TestUnsupported
	case capabilityStatus == CapabilityMissingDriver:
		return TestMissingDriver
	case !containsProfile(definition.Profiles, profile):
		return TestExcluded
	case !definition.Implemented:
		return TestUnsupported
	case slices.Contains(excludedTests, definition.ID):
		return TestExcluded
	default:
		return TestScheduled
	}
}

func keyedDigest(key []byte, domain string, encoded []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(domain))
	_, _ = mac.Write(encoded)
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil))
}

func validDigest(value, prefix string) bool {
	if len(value) != len(prefix)+sha256.Size*2 || !strings.HasPrefix(value, prefix) {
		return false
	}
	_, err := hex.DecodeString(value[len(prefix):])
	return err == nil
}

func sortRefs(refs []HardwareComponentRef) {
	sort.Slice(refs, func(left, right int) bool {
		if refs[left].Type != refs[right].Type {
			return refs[left].Type < refs[right].Type
		}
		return refs[left].ID < refs[right].ID
	})
}

func architectureUnsupported(architecture, testID string) bool {
	return architecture == "legacy-x86" && (testID == "graphics.quick" || testID == "display.operator.quick")
}

func stableHardwareGraph(components []inventory.Component) inventory.Snapshot {
	volatile := map[string]struct{}{
		"available_kib": {}, "carrier": {}, "capacity": {}, "duplex": {}, "enabled": {},
		"irq": {}, "mtu": {}, "numa_node": {}, "online": {}, "operational_state": {},
		"read_only": {}, "speed_mbps": {}, "status": {},
	}
	stable := make([]inventory.Component, 0, len(components))
	for _, component := range components {
		attributes := make(map[string]string, len(component.Attributes))
		for _, attribute := range component.Attributes {
			if component.Type == inventory.ComponentPowerSupply && inventory.IsPowerSupplyObservationAttribute(attribute.Name) {
				continue
			}
			if _, excluded := volatile[attribute.Name]; !excluded {
				attributes[attribute.Name] = attribute.Value
			}
		}
		stable = append(stable, inventory.NewComponent(component.Type, component.ID, attributes))
	}
	return inventory.NewSnapshot(stable)
}

func loadDefinitions(data []byte) (definitions, string, error) {
	var result definitions
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return definitions{}, "", fmt.Errorf("decode test definitions: %w", err)
	}
	sort.Slice(result.Capabilities, func(left, right int) bool { return result.Capabilities[left].ID < result.Capabilities[right].ID })
	sort.Slice(result.Tests, func(left, right int) bool { return result.Tests[left].ID < result.Tests[right].ID })
	capabilityIDs := make(map[string]struct{}, len(result.Capabilities))
	for _, definition := range result.Capabilities {
		if definition.ID == "operator.exterior" {
			if definition.Applicability != WholeDeviceOperator || len(definition.Hardware) != 0 || len(definition.Providers) != 0 {
				return definitions{}, "", errors.New("invalid operator capability definition")
			}
		} else if definition.ID == "" || definition.Applicability != "" || len(definition.Providers) == 0 {
			return definitions{}, "", errors.New("invalid capability definition")
		}
		if _, exists := capabilityIDs[definition.ID]; exists {
			return definitions{}, "", fmt.Errorf("duplicate capability %q", definition.ID)
		}
		capabilityIDs[definition.ID] = struct{}{}
	}
	testIDs := make(map[string]struct{}, len(result.Tests))
	for _, definition := range result.Tests {
		if definition.ID == "" || definition.Version < 1 || definition.EstimatedSeconds < 1 || definition.TimeoutSeconds < definition.EstimatedSeconds {
			return definitions{}, "", fmt.Errorf("invalid test definition %q", definition.ID)
		}
		if (definition.ID == "inspection.exterior.quick") != (definition.Capability == "operator.exterior") {
			return definitions{}, "", errors.New("invalid operator test identity")
		}
		if definition.Capability == "operator.exterior" && (definition.Version != 1 || !definition.Implemented || definition.Destructive ||
			definition.EstimatedSeconds != 20 || definition.TimeoutSeconds != 45 ||
			!slices.Equal(definition.Profiles, []Profile{ProfileQuick, ProfileStandard, ProfileExtended, ProfileDestructive})) {
			return definitions{}, "", errors.New("invalid operator test definition")
		}
		if _, exists := testIDs[definition.ID]; exists {
			return definitions{}, "", fmt.Errorf("duplicate test %q", definition.ID)
		}
		testIDs[definition.ID] = struct{}{}
		if _, exists := capabilityIDs[definition.Capability]; !exists {
			return definitions{}, "", fmt.Errorf("test %q references unknown capability", definition.ID)
		}
		for _, profile := range definition.Profiles {
			if _, ok := validProfiles[profile]; !ok {
				return definitions{}, "", fmt.Errorf("test %q has invalid profile", definition.ID)
			}
			if definition.Destructive && profile != ProfileDestructive {
				return definitions{}, "", fmt.Errorf("destructive test %q is in a safe profile", definition.ID)
			}
		}
	}
	if _, present := capabilityIDs["operator.exterior"]; present {
		if _, present := testIDs["inspection.exterior.quick"]; !present {
			return definitions{}, "", errors.New("operator capability has no inspection definition")
		}
	}
	canonical, err := json.Marshal(result)
	if err != nil {
		return definitions{}, "", fmt.Errorf("encode test definitions: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return result, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func resolveCapability(components []inventory.Component, definition capabilityDefinition) Capability {
	if definition.Applicability == WholeDeviceOperator {
		return Capability{ID: definition.ID, Status: CapabilityPresent, Applicability: WholeDeviceOperator}
	}
	providerIDs := matchingComponentIDs(components, definition.Providers)
	if len(providerIDs) > 0 {
		return Capability{ID: definition.ID, Status: CapabilityPresent, ComponentIDs: providerIDs}
	}
	hardwareIDs := matchingComponentIDs(components, definition.Hardware)
	if len(hardwareIDs) > 0 {
		return Capability{ID: definition.ID, Status: CapabilityMissingDriver, ComponentIDs: hardwareIDs}
	}
	return Capability{ID: definition.ID, Status: CapabilityAbsent}
}

func matchingComponentIDs(components []inventory.Component, matches []componentMatch) []string {
	var componentIDs []string
	for _, component := range components {
		for _, match := range matches {
			if component.Type != match.Type {
				continue
			}
			value, found := component.Attribute(match.Attribute)
			if match.Attribute != "" && !found {
				continue
			}
			if (match.Equals != "" && value != match.Equals) || (match.NotEquals != "" && value == match.NotEquals) || (match.Prefix != "" && !strings.HasPrefix(value, match.Prefix)) {
				continue
			}
			componentIDs = append(componentIDs, component.ID)
			break
		}
	}
	return componentIDs
}

func containsProfile(profiles []Profile, wanted Profile) bool {
	for _, profile := range profiles {
		if profile == wanted {
			return true
		}
	}
	return false
}
