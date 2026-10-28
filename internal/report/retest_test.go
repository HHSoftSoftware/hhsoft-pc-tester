package report

import (
	"strings"
	"testing"
)

func TestRetestIDAndDualDigestGrammar(t *testing.T) {
	for _, id := range []string{"12345678-1234-4234-8234-123456789abc", "00000000-0000-4000-9000-000000000000", "ffffffff-ffff-4fff-afff-ffffffffffff", "ffffffff-ffff-4fff-bfff-ffffffffffff"} {
		if !ValidRetestID(id) {
			t.Fatalf("canonical v4 ID rejected: %s", id)
		}
	}
	for _, id := range []string{"", "legacy", " 12345678-1234-4234-8234-123456789abc", "12345678-1234-4234-8234-123456789abc ", "12345678-1234-4234-8234-123456789ABC", "12345678-1234-1234-8234-123456789abc", "12345678-1234-4234-c234-123456789abc", "12345678123442348234123456789abc"} {
		if ValidRetestID(id) {
			t.Fatalf("noncanonical child ID accepted: %q", id)
		}
	}
	good := "sha256:" + strings.Repeat("a", 64)
	if (PreviousReport{ManifestDigest: good, BundleSHA256: good}).Validate() != nil {
		t.Fatal("valid dual digest rejected")
	}
	for _, bad := range []string{"", strings.ToUpper(good), good[:len(good)-1], good + "0", "sha256:" + strings.Repeat("g", 64)} {
		if (PreviousReport{ManifestDigest: bad, BundleSHA256: good}).Validate() == nil || (PreviousReport{ManifestDigest: good, BundleSHA256: bad}).Validate() == nil {
			t.Fatal("one malformed digest accepted")
		}
	}
}
