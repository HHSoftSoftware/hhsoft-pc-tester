package bundle

import (
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
)

func TestCPUScalarViewlessClaimChecks(t *testing.T) {
	for _, name := range []string{"cpu-v3-direct-pass", "session-v5-cpu-v3-local-heat", "session-v4-local-heat"} {
		p := thermalPayload(t, name)
		encoded, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		archive, _, keys := independentSignedViewArchive(t, encoded, nil, nil, product.Name, product.Version, false)
		if _, err := verifyViewFixture(archive, keys, false); err != nil {
			t.Fatal("valid CPU bundle rejected", err)
		}
	}
	for _, raw := range []string{
		`{"schema_version":1,"test_results":[{"id":"cpu.quick","version":2,"version":1}],"test_results":[]}`,
		`{"schema_version":1,"test_results":{"evidence":{"SCHEMA":"HHSOFT.PC-TESTER.CPU/V3"}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"sc\u0061lar":{}}}}`,
		`{"schema_version":1,"test_results":[{"id":"CPU.QUICK","ver\u0073ion":2}]}`,
	} {
		archive, _, keys := independentSignedViewArchive(t, []byte(raw), nil, nil, product.Name, product.Version, false)
		if _, err := verifyViewFixture(archive, keys, false); err == nil {
			t.Fatal("hidden CPU claim became opaque")
		}
		if _, err := ManifestDigestWithPDF([]byte(raw), nil, nil); err == nil {
			t.Fatal("hidden claim digested")
		}
	}
}
