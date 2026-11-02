package bundle

import (
	"fmt"
	"io"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

// The profile fixes complete view semantics. Preserve its bytes when adding a
// future profile; software version alone does not select a view format.
const (
	MemoryCapacityManifestSchema       = "hhsoft.pc-tester.bundle-manifest/v3"
	MemoryCapacityRetestManifestSchema = "hhsoft.pc-tester.bundle-manifest/v4"
	MemoryCapacityViewProfile          = "hhsoft.pc-tester.memory-capacity-view/v1"
)

func validateMemoryCapacityViewSelection(profile string, html, pdf []byte) error {
	if profile == "" {
		return nil
	}
	if profile != MemoryCapacityViewProfile || html == nil && pdf == nil {
		return fmt.Errorf("%w: unsupported or viewless capacity profile", ErrInvalidBundle)
	}
	return nil
}

// A nil key selects external trust. A non-nil previous reference selects the
// linked schema without conferring trust, chronology, or inherited results.
func MemoryCapacityManifestDigestWithPDF(inventoryJSON, html, pdf, publicKey []byte, previous *report.PreviousReport) (string, error) {
	if previous != nil && previous.Validate() != nil {
		return "", fmt.Errorf("%w: invalid previous-report reference", ErrInvalidBundle)
	}
	if err := validateViewsWithProfile(inventoryJSON, html, pdf, product.Name, product.Version, previous, MemoryCapacityViewProfile); err != nil {
		return "", err
	}
	_, encoded, err := buildManifestWithProfile(inventoryJSON, html, pdf, publicKey, previous, MemoryCapacityViewProfile)
	if err != nil {
		return "", err
	}
	return digest(encoded), nil
}

func WriteMemoryCapacityWithPDF(destination io.Writer, inventoryJSON []byte, envelope report.Envelope, html, pdf, publicKey []byte, previous *report.PreviousReport) error {
	if publicKey != nil {
		expected, err := SelfIssuedKeyID(publicKey)
		if err != nil || envelope.KeyID != expected {
			return fmt.Errorf("%w: invalid self-issued key identity", ErrInvalidBundle)
		}
	}
	return writeArchiveWithProfile(destination, inventoryJSON, envelope, html, pdf, publicKey, previous, MemoryCapacityViewProfile)
}
