package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/comparison"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func runCompare(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runCompareDestination(ctx, arguments, stdout, stderr, writeDestination, readRetestInput)
}

func runCompareDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer, writeOutput destinationWriter, readInput retestInputReader) int {
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

	if nilRetestInputValue(ctx) || readInput == nil || writeOutput == nil {
		fmt.Fprintln(stderr, "compare input or output handler is unavailable")
		return 1
	}
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "compare cancelled")
		return 1
	}
	before, err := loadVerifiedComparisonReport(ctx, flags.Arg(0), *publicKeyPath, strings.TrimSpace(*keyID), *selfIssued, readInput)
	if err != nil {
		fmt.Fprintln(stderr, "verify before report failed")
		return 1
	}
	after, err := loadVerifiedComparisonReport(ctx, flags.Arg(1), resolvedAfterPath, resolvedAfterID, *selfIssued, readInput)
	if err != nil {
		fmt.Fprintln(stderr, "verify after report failed")
		return 1
	}
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "compare cancelled")
		return 1
	}
	result, err := comparison.Compare(before, after)
	if err != nil {
		fmt.Fprintln(stderr, "compare reports failed")
		return 1
	}
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "compare cancelled")
		return 1
	}
	if err := writeOutput(*outputPath, stdout, func(destination io.Writer) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		encoder := json.NewEncoder(destination)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}); err != nil {
		fmt.Fprintf(stderr, "write comparison: %v\n", err)
		return 1
	}
	return 0
}

func loadVerifiedComparisonReport(ctx context.Context, reportPath, publicKeyPath, keyID string, selfIssued bool, readInput retestInputReader) (comparison.Report, error) {
	if nilRetestInputValue(ctx) || readInput == nil {
		return comparison.Report{}, errRetestInput
	}
	if err := ctx.Err(); err != nil {
		return comparison.Report{}, safeRetestInputError(ctx, err)
	}
	encoded, err := readInput(ctx, reportPath, retestArtifactInput)
	if err != nil {
		return comparison.Report{}, safeRetestInputError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return comparison.Report{}, safeRetestInputError(ctx, err)
	}
	var artifact bundle.Artifact
	if selfIssued {
		artifact, err = bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
	} else {
		publicKey, keyErr := readInput(ctx, publicKeyPath, retestPublicKeyInput)
		if keyErr != nil {
			return comparison.Report{}, safeRetestInputError(ctx, keyErr)
		}
		if err := ctx.Err(); err != nil {
			return comparison.Report{}, safeRetestInputError(ctx, err)
		}
		artifact, err = bundle.Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{keyID: ed25519.PublicKey(publicKey)})
	}
	if err != nil {
		return comparison.Report{}, fmt.Errorf("comparison report verification failed")
	}
	if artifact.Envelope.Product != product.Name {
		return comparison.Report{}, fmt.Errorf("comparison report product is invalid")
	}
	payload, err := decodeComparisonInventory(artifact.InventoryJSON)
	if err != nil {
		return comparison.Report{}, fmt.Errorf("comparison inventory is invalid")
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
	root, err := canonicalJSONFields(encoded, []string{"schema_version", "components", "system_fingerprint", "test_results", "coverage_scope"}, []string{"schema_version", "components", "system_fingerprint"})
	if err != nil {
		return app.InventoryPayload{}, fmt.Errorf("decode inventory: %w", err)
	}
	if raw, present := root["coverage_scope"]; present {
		var scope app.CoverageScope
		if json.Unmarshal(raw, &scope) != nil || scope == "" {
			return app.InventoryPayload{}, fmt.Errorf("decode inventory: invalid coverage scope")
		}
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
			fields, err := canonicalJSONFields(result, []string{"id", "version", "status", "plan_id", "hardware_graph", "evidence", "planned_status"}, []string{"id", "version", "status", "evidence"})
			if err != nil {
				return app.InventoryPayload{}, fmt.Errorf("decode test result: %w", err)
			}
			if raw, present := fields["planned_status"]; present {
				var status app.PlannedStatus
				if json.Unmarshal(raw, &status) != nil || status == "" {
					return app.InventoryPayload{}, fmt.Errorf("decode test result: invalid planned status")
				}
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
	if err := app.ValidateCoverage(payload); err != nil {
		return app.InventoryPayload{}, fmt.Errorf("decode inventory coverage: %w", err)
	}
	return payload, nil
}

func canonicalJSONFields(encoded []byte, allowedNames, requiredNames []string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, fmt.Errorf("expected inventory object")
	}
	fields := make(map[string]json.RawMessage)
	allowed := make(map[string]bool, len(allowedNames))
	for _, name := range allowedNames {
		allowed[name] = true
	}
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || !allowed[name] {
			return nil, fmt.Errorf("noncanonical or unknown inventory member")
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, fmt.Errorf("duplicate inventory member")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		fields[name] = raw
	}
	if last, err := decoder.Token(); err != nil || last != json.Delim('}') {
		return nil, fmt.Errorf("invalid inventory object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing inventory JSON")
	}
	for _, name := range requiredNames {
		if _, ok := fields[name]; !ok {
			return nil, fmt.Errorf("missing object member %q", name)
		}
	}
	return fields, nil
}
