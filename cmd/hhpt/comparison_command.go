package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/comparison"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func runCompare(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	publicKeyPath := flags.String("public-key", "", "public key used for the before report and, by default, the after report")
	keyID := flags.String("key-id", "", "key ID used for the before report and, by default, the after report")
	afterPublicKeyPath := flags.String("after-public-key", "", "public key used for the after report when it differs")
	afterKeyID := flags.String("after-key-id", "", "key ID used for the after report when it differs")
	selfIssued := flags.Bool("self-issued", false, "verify both reports using their embedded ephemeral signers")
	outputPath := flags.String("output", "-", "new JSON comparison file, or - for stdout")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 2 || (*selfIssued && (*publicKeyPath != "" || strings.TrimSpace(*keyID) != "" || *afterPublicKeyPath != "" || strings.TrimSpace(*afterKeyID) != "")) || (!*selfIssued && (*publicKeyPath == "" || strings.TrimSpace(*keyID) == "")) {
		fmt.Fprintln(stderr, "compare requires either -self-issued or a public key/key ID, plus before and after .hhpcr files")
		return 2
	}
	resolvedAfterPath, resolvedAfterID := *afterPublicKeyPath, strings.TrimSpace(*afterKeyID)
	if (resolvedAfterPath == "") != (resolvedAfterID == "") {
		fmt.Fprintln(stderr, "compare requires both -after-public-key and -after-key-id when either is used")
		return 2
	}
	if resolvedAfterPath == "" {
		resolvedAfterPath, resolvedAfterID = *publicKeyPath, strings.TrimSpace(*keyID)
	}

	before, err := loadVerifiedComparisonReport(flags.Arg(0), *publicKeyPath, strings.TrimSpace(*keyID), *selfIssued)
	if err != nil {
		fmt.Fprintf(stderr, "verify before report: %v\n", err)
		return 1
	}
	after, err := loadVerifiedComparisonReport(flags.Arg(1), resolvedAfterPath, resolvedAfterID, *selfIssued)
	if err != nil {
		fmt.Fprintf(stderr, "verify after report: %v\n", err)
		return 1
	}
	result, err := comparison.Compare(before, after)
	if err != nil {
		fmt.Fprintf(stderr, "compare reports: %v\n", err)
		return 1
	}
	destination, closeDestination, err := openDestination(*outputPath, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "open output: %v\n", err)
		return 1
	}
	defer closeDestination()
	encoder := json.NewEncoder(destination)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(stderr, "write comparison: %v\n", err)
		return 1
	}
	return 0
}

func loadVerifiedComparisonReport(reportPath, publicKeyPath, keyID string, selfIssued bool) (comparison.Report, error) {
	source, err := os.Open(reportPath)
	if err != nil {
		return comparison.Report{}, fmt.Errorf("open report: %w", err)
	}
	defer source.Close()
	metadata, err := source.Stat()
	if err != nil {
		return comparison.Report{}, fmt.Errorf("inspect report: %w", err)
	}
	if !metadata.Mode().IsRegular() {
		return comparison.Report{}, fmt.Errorf("inspect report: input is not a regular file")
	}
	var artifact bundle.Artifact
	if selfIssued {
		artifact, err = bundle.VerifySelfIssued(source, metadata.Size())
	} else {
		publicKey, keyErr := readRawKey(publicKeyPath, ed25519.PublicKeySize)
		if keyErr != nil {
			return comparison.Report{}, fmt.Errorf("read public key: %w", keyErr)
		}
		artifact, err = bundle.Verify(source, metadata.Size(), report.Keyring{keyID: ed25519.PublicKey(publicKey)})
	}
	if err != nil {
		return comparison.Report{}, err
	}
	if artifact.Envelope.Product != product.Name {
		return comparison.Report{}, fmt.Errorf("unexpected product %q", artifact.Envelope.Product)
	}
	payload, err := decodeComparisonInventory(artifact.InventoryJSON)
	if err != nil {
		return comparison.Report{}, err
	}
	return comparison.Report{
		Reference: comparison.ReportReference{
			SessionID:         artifact.Envelope.SessionID,
			GeneratedTime:     artifact.Envelope.GeneratedTime,
			SoftwareVersion:   artifact.Envelope.Version,
			SignerKeyID:       artifact.Envelope.KeyID,
			ManifestDigest:    artifact.Envelope.ManifestDigest,
			SystemFingerprint: payload.SystemFingerprint,
			ComponentCount:    len(payload.Components),
		},
		Inventory: payload,
	}, nil
}

func decodeComparisonInventory(encoded []byte) (app.InventoryPayload, error) {
	root, err := canonicalJSONFields(encoded, []string{"schema_version", "components", "system_fingerprint", "test_results"}, []string{"schema_version", "components", "system_fingerprint"})
	if err != nil {
		return app.InventoryPayload{}, fmt.Errorf("decode inventory: %w", err)
	}
	var components []json.RawMessage
	if err := json.Unmarshal(root["components"], &components); err != nil {
		return app.InventoryPayload{}, fmt.Errorf("decode inventory components: %w", err)
	}
	for _, component := range components {
		fields, err := canonicalJSONFields(component, []string{"type", "id", "attributes", "identity"}, []string{"type", "id", "identity"})
		if err != nil {
			return app.InventoryPayload{}, fmt.Errorf("decode inventory component: %w", err)
		}
		if rawAttributes, ok := fields["attributes"]; ok {
			var attributes []json.RawMessage
			if err := json.Unmarshal(rawAttributes, &attributes); err != nil {
				return app.InventoryPayload{}, fmt.Errorf("decode inventory attributes: %w", err)
			}
			for _, attribute := range attributes {
				if _, err := canonicalJSONFields(attribute, []string{"name", "value"}, []string{"name", "value"}); err != nil {
					return app.InventoryPayload{}, fmt.Errorf("decode inventory attribute: %w", err)
				}
			}
		}
		if _, err := canonicalJSONFields(fields["identity"], []string{"algorithm", "key_epoch", "source", "value", "confidence"}, []string{"algorithm", "key_epoch", "confidence"}); err != nil {
			return app.InventoryPayload{}, fmt.Errorf("decode component identity: %w", err)
		}
	}
	if _, err := canonicalJSONFields(root["system_fingerprint"], []string{"algorithm", "key_epoch", "key_id", "value", "identified_parts", "unidentified_parts"}, []string{"algorithm", "key_epoch", "key_id", "identified_parts", "unidentified_parts"}); err != nil {
		return app.InventoryPayload{}, fmt.Errorf("decode system fingerprint: %w", err)
	}
	if rawResults, ok := root["test_results"]; ok {
		var results []json.RawMessage
		if err := json.Unmarshal(rawResults, &results); err != nil {
			return app.InventoryPayload{}, fmt.Errorf("decode test results: %w", err)
		}
		for _, result := range results {
			if _, err := canonicalJSONFields(result, []string{"id", "version", "status", "plan_id", "hardware_graph", "evidence"}, []string{"id", "version", "status", "evidence"}); err != nil {
				return app.InventoryPayload{}, fmt.Errorf("decode test result: %w", err)
			}
		}
	}
	var payload app.InventoryPayload
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return app.InventoryPayload{}, fmt.Errorf("decode inventory: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return app.InventoryPayload{}, fmt.Errorf("decode inventory: trailing JSON value")
	}
	return payload, nil
}

func canonicalJSONFields(encoded []byte, allowedNames, requiredNames []string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(allowedNames))
	for _, name := range allowedNames {
		allowed[name] = true
	}
	for name := range fields {
		if !allowed[name] {
			return nil, fmt.Errorf("noncanonical or unknown object member %q", name)
		}
	}
	for _, name := range requiredNames {
		if _, ok := fields[name]; !ok {
			return nil, fmt.Errorf("missing object member %q", name)
		}
	}
	return fields, nil
}
