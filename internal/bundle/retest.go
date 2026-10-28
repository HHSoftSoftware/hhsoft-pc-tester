package bundle

import (
	"fmt"
	"io"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

// RetestManifestDigestWithPDF authenticates a reference supplied by the signer.
// It does not verify a parent or confer any trust on either report. A nil public
// key selects the ordinary externally verified signing profile.
func RetestManifestDigestWithPDF(inventoryJSON, html, pdf []byte, previous report.PreviousReport, publicKey []byte) (string, error) {
	if previous.Validate() != nil {
		return "", fmt.Errorf("%w: invalid previous-report reference", ErrInvalidBundle)
	}
	if err := validateViewsWithPrevious(inventoryJSON, html, pdf, product.Name, product.Version, &previous); err != nil {
		return "", err
	}
	_, encoded, err := buildManifestWithPrevious(inventoryJSON, html, pdf, publicKey, &previous)
	if err != nil {
		return "", err
	}
	return digest(encoded), nil
}

func WriteRetestWithPDF(destination io.Writer, inventoryJSON []byte, envelope report.Envelope, html, pdf []byte, previous report.PreviousReport, publicKey []byte) error {
	if publicKey != nil {
		expected, err := SelfIssuedKeyID(publicKey)
		if err != nil || envelope.KeyID != expected {
			return fmt.Errorf("%w: invalid self-issued key identity", ErrInvalidBundle)
		}
	}
	return writeArchiveWithPrevious(destination, inventoryJSON, envelope, html, pdf, publicKey, &previous)
}

// This runs independently of optional view validation: viewless v2 reports
// must not bypass the linked profile's product, reference, or UUID rules.
func validateRetestProfile(manifest Manifest, envelope report.Envelope) error {
	switch manifest.Schema {
	case ManifestSchema:
		if manifest.PreviousReport == nil && manifest.RevisionKind == "" {
			return nil
		}
	case RetestManifestSchema:
		if manifest.PreviousReport != nil && manifest.PreviousReport.Validate() == nil &&
			manifest.RevisionKind == "retest" && envelope.Product == product.Name && report.ValidRetestID(envelope.SessionID) {
			return nil
		}
	}
	return fmt.Errorf("%w: invalid linked report profile", ErrInvalidBundle)
}
