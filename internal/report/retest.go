package report

import "errors"

// PreviousReport is a signed reference, not proof of trust, asset identity,
// chronology, supersession, or an assertion that earlier outcomes still apply.
type PreviousReport struct {
	ManifestDigest string `json:"manifest_digest"`
	BundleSHA256   string `json:"bundle_sha256"`
}

func (previous PreviousReport) Validate() error {
	if !validSHA256Digest(previous.ManifestDigest) || !validSHA256Digest(previous.BundleSHA256) {
		return errors.New("report: invalid previous-report reference")
	}
	return nil
}

// ValidRetestID is deliberately separate from envelope-v1 validation. Historical
// report identifiers remain valid; only the linked bundle profile requires v4.
func ValidRetestID(value string) bool {
	if len(value) != 36 || value[14] != '4' || (value[19] != '8' && value[19] != '9' && value[19] != 'a' && value[19] != 'b') {
		return false
	}
	for index := 0; index < len(value); index++ {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if value[index] != '-' {
				return false
			}
			continue
		}
		if value[index] < '0' || value[index] > '9' {
			if value[index] < 'a' || value[index] > 'f' {
				return false
			}
		}
	}
	return true
}
