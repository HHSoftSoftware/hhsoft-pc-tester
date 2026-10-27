package bundle

import (
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
)

func TestMemoryViewlessBundlesCannotHideNewClaims(t *testing.T) {
	for _, name := range []string{"memory-v3-direct-pass", "session-v5-memory-v3-local-heat", "session-v4-local-heat"} {
		raw, err := json.Marshal(thermalPayload(t, name))
		if err != nil {
			t.Fatal(err)
		}
		archive, _, keys := independentSignedViewArchive(t, raw, nil, nil, product.Name, product.Version, false)
		if _, err := verifyViewFixture(archive, keys, false); err != nil {
			t.Fatal("valid memory bundle rejected", err)
		}
	}
	for _, raw := range []string{
		`{"schema_version":1,"test_results":[{"id":"memory.quick","version":2,"version":1}],"test_results":[]}`,
		`{"schema_version":1,"test_results":{"evidence":{"SCHEMA":"HHSOFT.PC-TESTER.MEMORY/V3"}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"memory_cove\u0072age":{}}}}`,
		`{"schema_version":1,"test_results":[{"id":"MEMORY.QUICK","ver\u0073ion":2}]}`,
	} {
		archive, _, keys := independentSignedViewArchive(t, []byte(raw), nil, nil, product.Name, product.Version, false)
		if _, err := verifyViewFixture(archive, keys, false); err == nil {
			t.Fatal("new memory claim became opaque")
		}
		if _, err := ManifestDigestWithPDF([]byte(raw), nil, nil); err == nil {
			t.Fatal("invalid claim digested")
		}
	}
}
