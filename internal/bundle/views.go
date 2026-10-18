package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"

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
	if html == nil && pdf == nil {
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
