package app

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/identity"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

var ErrFingerprintKeyLength = errors.New("fingerprint key must contain exactly 32 bytes")

const maxTestEvidenceBytes = 1 << 20

var (
	testIDPattern        = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,63}$`)
	planIDPattern        = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	hardwareGraphPattern = regexp.MustCompile(`^hmac-sha256:[0-9a-f]{64}$`)
)

type InventoryPayload struct {
	SchemaVersion     int               `json:"schema_version"`
	Components        []ComponentRecord `json:"components"`
	SystemFingerprint SystemFingerprint `json:"system_fingerprint"`
	TestResults       []TestResult      `json:"test_results,omitempty"`
}

type TestStatus string

const (
	TestPass         TestStatus = "pass"
	TestFail         TestStatus = "fail"
	TestSkip         TestStatus = "skip"
	TestInconclusive TestStatus = "inconclusive"
	TestError        TestStatus = "error"
	TestAborted      TestStatus = "aborted"
)

type TestResult struct {
	ID            string          `json:"id"`
	Version       int             `json:"version"`
	Status        TestStatus      `json:"status"`
	PlanID        string          `json:"plan_id,omitempty"`
	HardwareGraph string          `json:"hardware_graph,omitempty"`
	Evidence      json.RawMessage `json:"evidence"`
}

type ComponentRecord struct {
	Type       inventory.ComponentType `json:"type"`
	ID         string                  `json:"id"`
	Attributes []inventory.Attribute   `json:"attributes,omitempty"`
	Identity   ComponentIdentity       `json:"identity"`
}

type ComponentIdentity struct {
	Algorithm  string `json:"algorithm"`
	KeyEpoch   string `json:"key_epoch"`
	Source     string `json:"source,omitempty"`
	Value      string `json:"value,omitempty"`
	Confidence string `json:"confidence"`
}

type SystemFingerprint struct {
	Algorithm         string `json:"algorithm"`
	KeyEpoch          string `json:"key_epoch"`
	KeyID             string `json:"key_id"`
	Value             string `json:"value,omitempty"`
	IdentifiedParts   int    `json:"identified_parts"`
	UnidentifiedParts int    `json:"unidentified_parts"`
}

type identifierCandidate struct {
	attribute  string
	confidence string
}

func BuildInventoryPayload(snapshot inventory.Snapshot, fingerprintKey []byte, keyEpoch string) (InventoryPayload, error) {
	if len(fingerprintKey) != 32 {
		return InventoryPayload{}, ErrFingerprintKeyLength
	}
	keyEpoch = strings.TrimSpace(keyEpoch)
	if keyEpoch == "" {
		keyEpoch = "local-1"
	}

	payload := InventoryPayload{
		SchemaVersion: snapshot.SchemaVersion,
		Components:    make([]ComponentRecord, 0, len(snapshot.Components)),
		SystemFingerprint: SystemFingerprint{
			Algorithm: "hmac-sha256",
			KeyEpoch:  keyEpoch,
		},
	}
	keyID, err := identity.DeploymentKeyID(fingerprintKey)
	if err != nil {
		return InventoryPayload{}, err
	}
	payload.SystemFingerprint.KeyID = prefixed(keyID)
	materials := make([]identity.ComponentMaterial, 0, len(snapshot.Components))
	materialIndexes := make([]int, 0, len(snapshot.Components))
	for _, component := range snapshot.Components {
		record := ComponentRecord{
			Type:       component.Type,
			ID:         component.ID,
			Attributes: publicAttributes(component),
			Identity: ComponentIdentity{
				Algorithm:  "hmac-sha256",
				KeyEpoch:   keyEpoch,
				Confidence: "unavailable",
			},
		}
		material, source, confidence, ok := identityMaterial(component)
		if ok {
			record.Identity.Source = source
			record.Identity.Confidence = confidence
			materials = append(materials, identity.ComponentMaterial{Domain: string(component.Type), Material: []byte(material)})
			materialIndexes = append(materialIndexes, len(payload.Components))
		}
		payload.Components = append(payload.Components, record)
	}

	payload.SystemFingerprint.IdentifiedParts = len(materials)
	payload.SystemFingerprint.UnidentifiedParts = len(payload.Components) - len(materials)
	if len(materials) == 0 {
		return payload, nil
	}
	deploymentIdentity, err := identity.Compute(fingerprintKey, materials)
	if err != nil {
		return InventoryPayload{}, err
	}
	payload.SystemFingerprint.Value = prefixed(deploymentIdentity.SystemHMAC)
	for index, material := range materials {
		componentIdentity, err := identity.Compute(fingerprintKey, []identity.ComponentMaterial{material})
		if err != nil {
			return InventoryPayload{}, err
		}
		payload.Components[materialIndexes[index]].Identity.Value = prefixed(componentIdentity.ComponentPseudonyms[0].Digest)
	}
	return payload, nil
}

func BuildReportPayload(snapshot inventory.Snapshot, fingerprintKey []byte, keyEpoch string, testResults []TestResult) (InventoryPayload, error) {
	payload, err := BuildInventoryPayload(snapshot, fingerprintKey, keyEpoch)
	if err != nil {
		return InventoryPayload{}, err
	}
	seen := make(map[string]struct{}, len(testResults))
	payload.TestResults = append([]TestResult(nil), testResults...)
	for _, result := range payload.TestResults {
		if err := validateTestResult(result); err != nil {
			return InventoryPayload{}, err
		}
		if _, found := seen[result.ID]; found {
			return InventoryPayload{}, fmt.Errorf("invalid test result: duplicate ID %s", result.ID)
		}
		seen[result.ID] = struct{}{}
	}
	sort.Slice(payload.TestResults, func(left, right int) bool {
		return payload.TestResults[left].ID < payload.TestResults[right].ID
	})
	return payload, nil
}

func NewTestResult(id string, version int, status TestStatus, evidence any) (TestResult, error) {
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return TestResult{}, fmt.Errorf("encode test evidence: %w", err)
	}
	return NewEncodedTestResult(id, version, status, encoded)
}

func NewEncodedTestResult(id string, version int, status TestStatus, evidence json.RawMessage) (TestResult, error) {
	encoded := append(json.RawMessage(nil), evidence...)
	result := TestResult{ID: id, Version: version, Status: status, Evidence: encoded}
	if err := validateTestResult(result); err != nil {
		return TestResult{}, err
	}
	return result, nil
}

func BindTestResult(result TestResult, planID, hardwareGraph string) (TestResult, error) {
	result.PlanID = planID
	result.HardwareGraph = hardwareGraph
	if err := validateTestResult(result); err != nil {
		return TestResult{}, err
	}
	return result, nil
}

func validateTestResult(result TestResult) error {
	if !testIDPattern.MatchString(result.ID) || result.Version < 1 || !validTestStatus(result.Status) {
		return errors.New("invalid test result metadata")
	}
	if (result.PlanID == "") != (result.HardwareGraph == "") || result.PlanID != "" && (!planIDPattern.MatchString(result.PlanID) || !hardwareGraphPattern.MatchString(result.HardwareGraph)) {
		return errors.New("invalid test result plan binding")
	}
	if len(result.Evidence) == 0 || len(result.Evidence) > maxTestEvidenceBytes || !json.Valid(result.Evidence) {
		return errors.New("invalid test result evidence")
	}
	return nil
}

func validTestStatus(status TestStatus) bool {
	switch status {
	case TestPass, TestFail, TestSkip, TestInconclusive, TestError, TestAborted:
		return true
	default:
		return false
	}
}

func publicAttributes(component inventory.Component) []inventory.Attribute {
	result := make([]inventory.Attribute, 0, len(component.Attributes))
	for _, attribute := range component.Attributes {
		name := strings.ToLower(attribute.Name)
		if name == "unique_id" || strings.Contains(name, "serial") || strings.Contains(name, "uuid") || strings.Contains(name, "wwid") || strings.Contains(name, "asset_tag") || strings.Contains(name, "mac") || component.Type == inventory.ComponentNetwork && name == "address" {
			continue
		}
		result = append(result, attribute)
	}
	return result
}

func identityMaterial(component inventory.Component) (string, string, string, bool) {
	var candidates []identifierCandidate
	switch component.Type {
	case inventory.ComponentSystem:
		candidates = []identifierCandidate{{"product_uuid", "indicative"}, {"product_serial", "indicative"}, {"chassis_serial", "indicative"}}
	case inventory.ComponentBaseboard:
		candidates = []identifierCandidate{{"serial", "indicative"}, {"asset_tag", "contextual"}}
	case inventory.ComponentBlock:
		if kind, _ := component.Attribute("kind"); kind == "partition" {
			return "", "", "", false
		}
		candidates = []identifierCandidate{{"wwid", "durable"}, {"serial", "indicative"}}
	case inventory.ComponentNetwork:
		candidates = []identifierCandidate{{"address", "indicative"}}
	case inventory.ComponentUSB:
		candidates = []identifierCandidate{{"serial", "indicative"}}
	case inventory.ComponentCPU:
		candidates = []identifierCandidate{{"serial", "indicative"}}
	}
	for _, candidate := range candidates {
		if value, ok := component.Attribute(candidate.attribute); ok && !placeholderIdentifier(value) {
			return candidate.attribute + string([]byte{0}) + strings.ToLower(strings.TrimSpace(value)), candidate.attribute, candidate.confidence, true
		}
	}
	return "", "", "", false
}

func placeholderIdentifier(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" || value == "none" || value == "unknown" || value == "default string" || value == "to be filled by o.e.m." || value == "00000000-0000-0000-0000-000000000000"
}

func prefixed(digest identity.Digest) string {
	return "hmac-sha256:" + hex.EncodeToString(digest[:])
}
