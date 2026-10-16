package comparison

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

const (
	Schema   = "hhsoft.pc-tester.hardware-comparison/v1"
	PolicyID = "hhsoft.parts-swap.v1"
)

type Status string

const (
	StatusUnchanged    Status = "unchanged"
	StatusChanged      Status = "changed"
	StatusInconclusive Status = "inconclusive"
)

type Report struct {
	Reference ReportReference
	Inventory app.InventoryPayload
}

type ReportReference struct {
	SessionID         string                `json:"session_id"`
	GeneratedTime     string                `json:"generated_time"`
	SoftwareVersion   string                `json:"software_version"`
	SignerKeyID       string                `json:"signer_key_id"`
	ManifestDigest    string                `json:"manifest_digest"`
	SystemFingerprint app.SystemFingerprint `json:"system_fingerprint"`
	ComponentCount    int                   `json:"component_count"`
}

type Result struct {
	Schema     string             `json:"schema"`
	Policy     string             `json:"policy"`
	Status     Status             `json:"status"`
	Comparable bool               `json:"comparable"`
	Complete   bool               `json:"complete"`
	Before     ReportReference    `json:"before"`
	After      ReportReference    `json:"after"`
	Summary    Summary            `json:"summary"`
	Matched    []Match            `json:"matched,omitempty"`
	Replaced   []Replacement      `json:"replaced,omitempty"`
	Added      []Component        `json:"added,omitempty"`
	Removed    []Component        `json:"removed,omitempty"`
	Unresolved []UnresolvedChange `json:"unresolved,omitempty"`
}

type Summary struct {
	Matched    int `json:"matched"`
	Moved      int `json:"moved"`
	Replaced   int `json:"replaced"`
	Added      int `json:"added"`
	Removed    int `json:"removed"`
	Unresolved int `json:"unresolved"`
}

type Component struct {
	Type               inventory.ComponentType `json:"type"`
	ID                 string                  `json:"id"`
	IdentitySource     string                  `json:"identity_source,omitempty"`
	Identity           string                  `json:"identity,omitempty"`
	IdentityConfidence string                  `json:"identity_confidence"`
	Attributes         []inventory.Attribute   `json:"attributes,omitempty"`
}

type Match struct {
	Before         Component `json:"before"`
	After          Component `json:"after"`
	LocationChange bool      `json:"location_change"`
}

type Replacement struct {
	Before             Component `json:"before"`
	After              Component `json:"after"`
	LocationConfidence string    `json:"location_confidence"`
	Explanation        string    `json:"explanation"`
}

type UnresolvedChange struct {
	Before []Component `json:"before,omitempty"`
	After  []Component `json:"after,omitempty"`
	Reason string      `json:"reason"`
}

type identityKey struct {
	componentType inventory.ComponentType
	source        string
	value         string
}

type aggregateIdentityKey struct {
	componentType inventory.ComponentType
	value         string
}

type locationKey struct {
	componentType inventory.ComponentType
	id            string
}

type observationKey struct {
	componentType inventory.ComponentType
	attributes    string
}

func Compare(before, after Report) (Result, error) {
	if err := validateReport(before); err != nil {
		return Result{}, fmt.Errorf("before report: %w", err)
	}
	if err := validateReport(after); err != nil {
		return Result{}, fmt.Errorf("after report: %w", err)
	}
	result := Result{Schema: Schema, Policy: PolicyID, Status: StatusInconclusive, Before: before.Reference, After: after.Reference}
	if before.Inventory.SystemFingerprint.Algorithm != after.Inventory.SystemFingerprint.Algorithm {
		addUnresolved(&result, UnresolvedChange{Reason: "identity algorithms differ; component pseudonyms cannot be compared"})
		return result, nil
	}
	if before.Inventory.SystemFingerprint.KeyEpoch != after.Inventory.SystemFingerprint.KeyEpoch {
		addUnresolved(&result, UnresolvedChange{Reason: "fingerprint key epochs differ; an authorized key service must bridge identities"})
		return result, nil
	}
	if before.Inventory.SystemFingerprint.KeyID != after.Inventory.SystemFingerprint.KeyID {
		addUnresolved(&result, UnresolvedChange{Reason: "fingerprint key identifiers differ; matching epoch labels do not establish comparable identities"})
		return result, nil
	}
	result.Comparable = true

	beforeComponents := components(before.Inventory.Components)
	afterComponents := components(after.Inventory.Components)
	if identitySetsEqual(beforeComponents, afterComponents) != (before.Inventory.SystemFingerprint.Value == after.Inventory.SystemFingerprint.Value) {
		return Result{}, errors.New("signed system fingerprint conflicts with the component identity multiset")
	}
	matchedBefore := make([]bool, len(beforeComponents))
	matchedAfter := make([]bool, len(afterComponents))
	ambiguousBefore := make([]bool, len(beforeComponents))
	ambiguousAfter := make([]bool, len(afterComponents))

	identityBefore := indexByIdentity(beforeComponents)
	identityAfter := indexByIdentity(afterComponents)
	for _, key := range sortedIdentityKeys(identityBefore, identityAfter) {
		beforeIndexes := identityBefore[key]
		afterIndexes := identityAfter[key]
		if len(beforeIndexes) > 1 || len(afterIndexes) > 1 {
			mark(ambiguousBefore, beforeIndexes)
			mark(ambiguousAfter, afterIndexes)
			continue
		}
		if len(beforeIndexes) == 1 && len(afterIndexes) == 1 {
			beforeIndex := beforeIndexes[0]
			afterIndex := afterIndexes[0]
			beforeComponent := beforeComponents[beforeIndex]
			afterComponent := afterComponents[afterIndex]
			matchedBefore[beforeIndex] = true
			matchedAfter[afterIndex] = true
			if beforeComponent.IdentityConfidence == "contextual" || (beforeComponent.IdentityConfidence != "durable" && observationKeyFor(beforeComponent) != observationKeyFor(afterComponent)) {
				addUnresolved(&result, UnresolvedChange{Before: []Component{beforeComponent}, After: []Component{afterComponent}, Reason: "non-durable identity conflicts with observable hardware attributes and cannot establish continuity"})
				continue
			}
			moved := beforeComponent.ID != afterComponent.ID
			result.Matched = append(result.Matched, Match{Before: beforeComponent, After: afterComponent, LocationChange: moved})
			result.Summary.Matched++
			if moved {
				result.Summary.Moved++
			}
		}
	}

	locationBefore := indexUnmatchedByLocation(beforeComponents, matchedBefore)
	locationAfter := indexUnmatchedByLocation(afterComponents, matchedAfter)
	for _, location := range sortedLocationKeys(locationBefore, locationAfter) {
		beforeIndex := locationBefore[location]
		afterIndex := locationAfter[location]
		beforeComponent := beforeComponents[beforeIndex]
		afterComponent := afterComponents[afterIndex]
		matchedBefore[beforeIndex] = true
		matchedAfter[afterIndex] = true
		if ambiguousBefore[beforeIndex] || ambiguousAfter[afterIndex] {
			addUnresolved(&result, UnresolvedChange{Before: []Component{beforeComponent}, After: []Component{afterComponent}, Reason: "a repeated component pseudonym prevents attributing the observed location"})
			continue
		}
		if beforeComponent.Identity == "" || afterComponent.Identity == "" {
			addUnresolved(&result, UnresolvedChange{Before: []Component{beforeComponent}, After: []Component{afterComponent}, Reason: "stable identity is unavailable at the same observed location; continuity or replacement cannot be proven"})
			continue
		}
		if beforeComponent.IdentitySource != afterComponent.IdentitySource {
			addUnresolved(&result, UnresolvedChange{Before: []Component{beforeComponent}, After: []Component{afterComponent}, Reason: "identity source changed at the same observed location; replacement cannot be proven"})
			continue
		}
		if beforeComponent.IdentityConfidence != "durable" || afterComponent.IdentityConfidence != "durable" {
			addUnresolved(&result, UnresolvedChange{Before: []Component{beforeComponent}, After: []Component{afterComponent}, Reason: "different non-durable identities occupy the same observed location; physical replacement is not proven"})
			continue
		}
		result.Replaced = append(result.Replaced, Replacement{
			Before: beforeComponent, After: afterComponent, LocationConfidence: locationConfidence(beforeComponent),
			Explanation: "different durable component identities from the same source occupy the same observed location",
		})
		result.Summary.Replaced++
	}

	observationBefore := indexUnmatchedByObservation(beforeComponents, matchedBefore)
	observationAfter := indexUnmatchedByObservation(afterComponents, matchedAfter)
	for _, observation := range sortedObservationKeys(observationBefore, observationAfter) {
		beforeIndexes := observationBefore[observation]
		afterIndexes := observationAfter[observation]
		if !observationNeedsResolution(beforeComponents, beforeIndexes, ambiguousBefore) && !observationNeedsResolution(afterComponents, afterIndexes, ambiguousAfter) {
			continue
		}
		mark(matchedBefore, beforeIndexes)
		mark(matchedAfter, afterIndexes)
		reason := "stable identity is unavailable; matching observable attributes do not prove continuity after a location change"
		if len(beforeIndexes) > 1 || len(afterIndexes) > 1 {
			reason = "stable identity is unavailable among repeated observable records; individual continuity and count changes cannot be attributed"
		}
		addUnresolved(&result, UnresolvedChange{Before: componentList(beforeComponents, beforeIndexes), After: componentList(afterComponents, afterIndexes), Reason: reason})
	}

	for index, component := range beforeComponents {
		if matchedBefore[index] {
			continue
		}
		matchedBefore[index] = true
		if component.Identity != "" && component.IdentityConfidence != "contextual" && !ambiguousBefore[index] {
			result.Removed = append(result.Removed, component)
			result.Summary.Removed++
		} else {
			addUnresolved(&result, UnresolvedChange{Before: []Component{component}, Reason: "stable identity unavailable; removal or re-enumeration cannot be distinguished"})
		}
	}
	for index, component := range afterComponents {
		if matchedAfter[index] {
			continue
		}
		matchedAfter[index] = true
		if component.Identity != "" && component.IdentityConfidence != "contextual" && !ambiguousAfter[index] {
			result.Added = append(result.Added, component)
			result.Summary.Added++
		} else {
			addUnresolved(&result, UnresolvedChange{After: []Component{component}, Reason: "stable identity unavailable; addition or re-enumeration cannot be distinguished"})
		}
	}

	result.Complete = result.Summary.Unresolved == 0
	if result.Summary.Replaced+result.Summary.Added+result.Summary.Removed > 0 {
		result.Status = StatusChanged
	} else if result.Complete {
		result.Status = StatusUnchanged
	}
	return result, nil
}

func validateReport(report Report) error {
	payload := report.Inventory
	if payload.SchemaVersion != inventory.SnapshotSchemaVersion {
		return errors.New("unsupported inventory schema")
	}
	if len(payload.Components) == 0 {
		return errors.New("inventory contains no component evidence")
	}
	if report.Reference.SessionID == "" || report.Reference.GeneratedTime == "" || report.Reference.SoftwareVersion == "" || report.Reference.SignerKeyID == "" || report.Reference.ManifestDigest == "" {
		return errors.New("incomplete signed report reference")
	}
	if report.Reference.ComponentCount != len(payload.Components) || report.Reference.SystemFingerprint != payload.SystemFingerprint {
		return errors.New("signed report reference does not match inventory")
	}
	fingerprint := payload.SystemFingerprint
	if fingerprint.Algorithm != "hmac-sha256" || !validText(fingerprint.KeyEpoch, 128) || !validIdentity(fingerprint.KeyID) || fingerprint.IdentifiedParts < 0 || fingerprint.UnidentifiedParts < 0 {
		return errors.New("invalid system fingerprint metadata")
	}
	if fingerprint.IdentifiedParts+fingerprint.UnidentifiedParts != len(payload.Components) {
		return errors.New("component identity counts do not match inventory")
	}
	if fingerprint.IdentifiedParts == 0 {
		if fingerprint.Value != "" {
			return errors.New("system fingerprint exists without identified components")
		}
	} else if !validIdentity(fingerprint.Value) {
		return errors.New("invalid system fingerprint")
	}
	seen := make(map[locationKey]bool, len(payload.Components))
	identified := 0
	for _, component := range payload.Components {
		key := locationKey{componentType: component.Type, id: component.ID}
		if !validComponentType(component.Type) || !validText(component.ID, 512) || seen[key] {
			return errors.New("invalid or duplicate component key")
		}
		seen[key] = true
		if component.Identity.Algorithm != fingerprint.Algorithm || component.Identity.KeyEpoch != fingerprint.KeyEpoch {
			return errors.New("component identity metadata differs from system fingerprint")
		}
		if component.Identity.Value == "" {
			if component.Identity.Source != "" || component.Identity.Confidence != "unavailable" {
				return errors.New("missing component identity has invalid source or confidence")
			}
		} else {
			expectedConfidence, ok := sourceConfidence(component.Type, component.Identity.Source)
			if !validIdentity(component.Identity.Value) || !ok || component.Identity.Confidence != expectedConfidence {
				return errors.New("invalid component identity")
			}
			identified++
		}
		attributeNames := make(map[string]bool, len(component.Attributes))
		for _, attribute := range component.Attributes {
			if !validText(attribute.Name, 256) || !validText(attribute.Value, 64<<10) || attributeNames[attribute.Name] {
				return errors.New("invalid component attribute")
			}
			attributeNames[attribute.Name] = true
		}
	}
	if identified != fingerprint.IdentifiedParts {
		return errors.New("identified component count is incorrect")
	}
	return nil
}

func validComponentType(componentType inventory.ComponentType) bool {
	switch componentType {
	case inventory.ComponentCPU, inventory.ComponentMemory, inventory.ComponentSystem, inventory.ComponentBaseboard, inventory.ComponentBlock, inventory.ComponentPCI, inventory.ComponentNetwork, inventory.ComponentInput, inventory.ComponentSound, inventory.ComponentVideo, inventory.ComponentUSB:
		return true
	default:
		return false
	}
}

func sourceConfidence(componentType inventory.ComponentType, source string) (string, bool) {
	switch componentType {
	case inventory.ComponentSystem:
		if source == "product_uuid" || source == "product_serial" || source == "chassis_serial" {
			return "indicative", true
		}
	case inventory.ComponentBaseboard:
		if source == "serial" {
			return "indicative", true
		}
		if source == "asset_tag" {
			return "contextual", true
		}
	case inventory.ComponentBlock:
		if source == "wwid" {
			return "durable", true
		}
		if source == "serial" {
			return "indicative", true
		}
	case inventory.ComponentNetwork:
		if source == "address" {
			return "indicative", true
		}
	case inventory.ComponentUSB, inventory.ComponentCPU:
		if source == "serial" {
			return "indicative", true
		}
	}
	return "", false
}

func validText(value string, maximum int) bool {
	if value == "" || len(value) > maximum || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func validIdentity(value string) bool {
	if len(value) != len("hmac-sha256:")+64 || !strings.HasPrefix(value, "hmac-sha256:") {
		return false
	}
	for _, character := range value[len("hmac-sha256:"):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func components(records []app.ComponentRecord) []Component {
	result := make([]Component, 0, len(records))
	for _, record := range records {
		attributes := append([]inventory.Attribute(nil), record.Attributes...)
		sort.Slice(attributes, func(left, right int) bool {
			if attributes[left].Name != attributes[right].Name {
				return attributes[left].Name < attributes[right].Name
			}
			return attributes[left].Value < attributes[right].Value
		})
		result = append(result, Component{Type: record.Type, ID: record.ID, IdentitySource: record.Identity.Source, Identity: record.Identity.Value, IdentityConfidence: record.Identity.Confidence, Attributes: attributes})
	}
	sort.Slice(result, func(left, right int) bool {
		return componentLess(result[left], result[right])
	})
	return result
}

func componentLess(left, right Component) bool {
	for _, difference := range []int{cmp.Compare(left.Type, right.Type), cmp.Compare(left.ID, right.ID), cmp.Compare(left.IdentitySource, right.IdentitySource), cmp.Compare(left.Identity, right.Identity)} {
		if difference != 0 {
			return difference < 0
		}
	}
	for index := 0; index < min(len(left.Attributes), len(right.Attributes)); index++ {
		if difference := cmp.Compare(left.Attributes[index].Name, right.Attributes[index].Name); difference != 0 {
			return difference < 0
		}
		if difference := cmp.Compare(left.Attributes[index].Value, right.Attributes[index].Value); difference != 0 {
			return difference < 0
		}
	}
	return len(left.Attributes) < len(right.Attributes)
}

func indexByIdentity(components []Component) map[identityKey][]int {
	result := make(map[identityKey][]int)
	for index, component := range components {
		if component.Identity != "" {
			key := identityKey{componentType: component.Type, source: component.IdentitySource, value: component.Identity}
			result[key] = append(result[key], index)
		}
	}
	return result
}

func indexUnmatchedByLocation(components []Component, matched []bool) map[locationKey]int {
	result := make(map[locationKey]int)
	for index, component := range components {
		if !matched[index] {
			result[locationKey{componentType: component.Type, id: component.ID}] = index
		}
	}
	return result
}

func indexUnmatchedByObservation(components []Component, matched []bool) map[observationKey][]int {
	result := make(map[observationKey][]int)
	for index, component := range components {
		if matched[index] {
			continue
		}
		key := observationKeyFor(component)
		result[key] = append(result[key], index)
	}
	return result
}

func identitySetsEqual(left, right []Component) bool {
	counts := make(map[aggregateIdentityKey]int)
	for _, component := range left {
		if component.Identity != "" {
			counts[aggregateIdentityKey{componentType: component.Type, value: component.Identity}]++
		}
	}
	for _, component := range right {
		if component.Identity != "" {
			counts[aggregateIdentityKey{componentType: component.Type, value: component.Identity}]--
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func sortedIdentityKeys(left, right map[identityKey][]int) []identityKey {
	keys := make(map[identityKey]bool, len(left)+len(right))
	for key := range left {
		keys[key] = true
	}
	for key := range right {
		keys[key] = true
	}
	result := make([]identityKey, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].componentType != result[right].componentType {
			return result[left].componentType < result[right].componentType
		}
		if result[left].source != result[right].source {
			return result[left].source < result[right].source
		}
		return result[left].value < result[right].value
	})
	return result
}

func sortedLocationKeys(left, right map[locationKey]int) []locationKey {
	result := make([]locationKey, 0)
	for key := range left {
		if _, ok := right[key]; ok {
			result = append(result, key)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].componentType != result[right].componentType {
			return result[left].componentType < result[right].componentType
		}
		return result[left].id < result[right].id
	})
	return result
}

func sortedObservationKeys(left, right map[observationKey][]int) []observationKey {
	result := make([]observationKey, 0)
	for key := range left {
		if _, ok := right[key]; ok {
			result = append(result, key)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].componentType != result[right].componentType {
			return result[left].componentType < result[right].componentType
		}
		return result[left].attributes < result[right].attributes
	})
	return result
}

func mark(matched []bool, indexes []int) {
	for _, index := range indexes {
		matched[index] = true
	}
}

func componentList(components []Component, indexes []int) []Component {
	result := make([]Component, 0, len(indexes))
	for _, index := range indexes {
		result = append(result, components[index])
	}
	return result
}

func observationNeedsResolution(components []Component, indexes []int, ambiguous []bool) bool {
	for _, index := range indexes {
		if ambiguous[index] || components[index].Identity == "" || components[index].IdentityConfidence != "durable" {
			return true
		}
	}
	return false
}

func observationKeyFor(component Component) observationKey {
	attributes := make([]inventory.Attribute, 0, len(component.Attributes))
	for _, attribute := range component.Attributes {
		if !volatileObservationAttribute(component.Type, attribute.Name) {
			attributes = append(attributes, attribute)
		}
	}
	encoded, _ := json.Marshal(attributes)
	return observationKey{componentType: component.Type, attributes: string(encoded)}
}

func volatileObservationAttribute(componentType inventory.ComponentType, name string) bool {
	switch componentType {
	case inventory.ComponentBlock:
		if name == "device_number" || name == "name" || name == "read_only" {
			return true
		}
	case inventory.ComponentNetwork:
		if name == "duplex" || name == "interface_index" || name == "link_index" || name == "name" || name == "speed_mbps" {
			return true
		}
	case inventory.ComponentUSB:
		if name == "authorized" || name == "bus_number" || name == "device_number" || name == "path" || name == "speed_mbps" {
			return true
		}
	}
	switch name {
	case "available_kib", "carrier", "duplex", "handlers", "kernel_path", "mtu", "operational_state", "speed", "state":
		return true
	default:
		return false
	}
}

func addUnresolved(result *Result, change UnresolvedChange) {
	result.Unresolved = append(result.Unresolved, change)
	result.Summary.Unresolved++
}

func locationConfidence(component Component) string {
	switch component.Type {
	case inventory.ComponentSystem, inventory.ComponentBaseboard:
		return "strong-singleton"
	case inventory.ComponentPCI, inventory.ComponentUSB:
		return "physical-topology"
	default:
		return "report-location"
	}
}
