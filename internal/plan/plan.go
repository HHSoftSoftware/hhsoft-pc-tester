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
	"sort"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

const SchemaVersion = 2

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
	ID           string           `json:"id"`
	Status       CapabilityStatus `json:"status"`
	ComponentIDs []string         `json:"component_ids,omitempty"`
}

type Test struct {
	ID               string     `json:"id"`
	Version          int        `json:"version"`
	Status           TestStatus `json:"status"`
	Capability       string     `json:"capability"`
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
}

type definitions struct {
	Capabilities []capabilityDefinition `json:"capabilities"`
	Tests        []testDefinition       `json:"tests"`
}

type capabilityDefinition struct {
	ID        string           `json:"id"`
	Hardware  []componentMatch `json:"hardware"`
	Providers []componentMatch `json:"providers"`
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
			ID: definition.ID, Version: definition.Version, Capability: definition.Capability,
			ComponentIDs: append([]string(nil), capability.ComponentIDs...), Destructive: definition.Destructive,
			EstimatedSeconds: definition.EstimatedSeconds, TimeoutSeconds: definition.TimeoutSeconds,
		}
		switch {
		case capability.Status == CapabilityAbsent:
			test.Status = TestNotPresent
		case architectureUnsupported(architecture, definition.ID):
			test.Status = TestUnsupported
		case capability.Status == CapabilityMissingDriver:
			test.Status = TestMissingDriver
		case !containsProfile(definition.Profiles, profile):
			test.Status = TestExcluded
		case !definition.Implemented:
			test.Status = TestUnsupported
		default:
			test.Status = TestScheduled
		}
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
	for _, component := range result.HardwareGraph.Components {
		key := string(component.Type) + "\x00" + component.ID
		if component.Type == "" || component.ID == "" || key <= previous || !validDigest(component.Value, "hmac-sha256:") {
			return errors.New("invalid frozen plan component binding")
		}
		previous = key
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
		if definition.ID == "" || len(definition.Providers) == 0 {
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
	canonical, err := json.Marshal(result)
	if err != nil {
		return definitions{}, "", fmt.Errorf("encode test definitions: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return result, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func resolveCapability(components []inventory.Component, definition capabilityDefinition) Capability {
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
