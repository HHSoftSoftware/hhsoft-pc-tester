package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
)

// A valid signature authenticates bytes, not the safety or accuracy of a view.
// Accept only the deterministic renderer's complete output for this inventory
// and signed product/version. No HTML/PDF parser or active-content denylist is
// used. Absent optional views retain the inventory-only bundle profile.
func validateViews(encoded, html, pdf []byte, productName, version string) error {
	if html == nil && pdf == nil && !hasCoverageMetadata(encoded) && !app.HasSessionThermalClaim(encoded) && !app.HasKeyboardClaim(encoded) && !app.HasPointerClaim(encoded) && !app.HasCPUScalarClaim(encoded) {
		return nil
	}
	if productName != product.Name || len(encoded) == 0 || len(encoded) > maxInventoryBytes ||
		len(html) > maxRenderedBytes || len(pdf) > maxRenderedPDF {
		return fmt.Errorf("%w: unsupported report view identity or size", ErrInvalidBundle)
	}
	var payload app.InventoryPayload
	if err := decodeStrict(encoded, &payload); err != nil || payload.SchemaVersion != inventory.SnapshotSchemaVersion {
		return fmt.Errorf("%w: unsupported report view inventory", ErrInvalidBundle)
	}
	if err := app.ValidateCoverage(payload); err != nil {
		return fmt.Errorf("%w: invalid report coverage", ErrInvalidBundle)
	}
	// Exact canonical field spelling and representation also reject Unicode
	// case-fold aliases, missing/noncanonical field representations, and ignored
	// JSON fields. Canonical null slices retain their existing representation.
	canonical, err := json.Marshal(payload)
	if err != nil || !equivalentJSON(encoded, canonical) {
		return fmt.Errorf("%w: noncanonical report view inventory", ErrInvalidBundle)
	}
	if html != nil {
		expected, err := render.InventoryHTMLForVersion(payload, version)
		if err != nil || !bytes.Equal(expected, html) {
			return fmt.Errorf("%w: HTML is not the canonical inventory view", ErrInvalidBundle)
		}
	}
	if pdf != nil {
		expected, err := render.InventoryPDFForVersion(payload, version)
		if err != nil || !bytes.Equal(expected, pdf) {
			return fmt.Errorf("%w: PDF is not the canonical inventory view", ErrInvalidBundle)
		}
	}
	return nil
}

// New coverage claims are validated even in bundles without optional views.
// Older inventory-only payloads keep their historical schema behavior.
func hasCoverageMetadata(encoded []byte) bool {
	found, err := findCoverageMetadata(json.NewDecoder(bytes.NewReader(encoded)), true, false)
	return found || err != nil
}

// Inspect decoded member names, not last-wins struct values. A claim inside a
// malformed test_results container must still select strict validation.
func findCoverageMetadata(decoder *json.Decoder, root, results bool) (bool, error) {
	token, err := decoder.Token()
	if err != nil {
		return false, err
	}
	switch token {
	case json.Delim('{'):
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return false, err
			}
			name, ok := key.(string)
			if !ok {
				return false, fmt.Errorf("invalid coverage member")
			}
			if root && strings.EqualFold(name, "coverage_scope") || results && strings.EqualFold(name, "planned_status") {
				return true, nil
			}
			found, err := findCoverageMetadata(decoder, false, results || root && strings.EqualFold(name, "test_results"))
			if found || err != nil {
				return found, err
			}
		}
		_, err = decoder.Token()
		return false, err
	case json.Delim('['):
		for decoder.More() {
			found, err := findCoverageMetadata(decoder, root, results)
			if found || err != nil {
				return found, err
			}
		}
		_, err = decoder.Token()
		return false, err
	default:
		return false, nil
	}
}
