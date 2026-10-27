package bundle

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
)

func thermalPayload(t *testing.T, name string) app.InventoryPayload {
	t.Helper()
	encoded, err := os.ReadFile("../app/testdata/" + name + ".json")
	if err != nil {
		t.Fatal("synthetic thermal bundle fixture unavailable")
	}
	var payload app.InventoryPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestSignedThermalEvidenceWithAndWithoutViews(t *testing.T) {
	for _, name := range []string{"session-v4-unavailable", "session-v4-global-heat", "session-v4-local-heat", "historical-session-v3"} {
		t.Run(name, func(t *testing.T) {
			payload := thermalPayload(t, name)
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			html, err := render.InventoryHTML(payload)
			if err != nil {
				t.Fatal(err)
			}
			pdf, err := render.InventoryPDF(payload)
			if err != nil {
				t.Fatal(err)
			}
			for _, withViews := range []bool{false, true} {
				var h, p []byte
				if withViews {
					h, p = html, pdf
				}
				for _, selfIssued := range []bool{false, true} {
					archive, _, keys := independentSignedViewArchive(t, encoded, h, p, product.Name, product.Version, selfIssued)
					artifact, err := verifyViewFixture(archive, keys, selfIssued)
					if err != nil || !bytes.Equal(artifact.InventoryJSON, encoded) {
						t.Fatalf("valid thermal bundle rejected: %v", err)
					}
				}
			}
		})
	}
}

func TestThermalSelectionPreservesViewlessHistoricalAbsentEvidence(t *testing.T) {
	encoded := []byte(`{"schema_version":1,"test_results":[{"id":"cpu.quick","version":1,"status":"skip"}]}`)
	if app.HasSessionThermalClaim(encoded) || hasCoverageMetadata(encoded) {
		t.Fatal("historical absent evidence selected new claim validation")
	}
	for _, selfIssued := range []bool{false, true} {
		archive, _, keys := independentSignedViewArchive(t, encoded, nil, nil, product.Name, product.Version, selfIssued)
		if _, err := verifyViewFixture(archive, keys, selfIssued); err != nil {
			t.Fatalf("historical viewless report rejected: %v", err)
		}
	}
}

func TestViewlessThermalClaimsCannotRecoverHistoricalOpaqueValidation(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":1,"test_results":[{"evidence":{"session":{"schema":"hhsoft.pc-tester.quick-session/v4"}}}]}`,
		`{"schema_version":1,"test_results":{"evidence":{"session":{"thermal":null}}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"session":{"Thermal":{}}}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"session":{"therm\u0061l":false}}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"session":{"local_thermal_sourceſ":[]}}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"ſession":{"schema":"hhsoft.pc-tester.quick-session/v4"}}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"session":{"schema":"hhsoft.pc-tester.quick-session/v4","schema":"hhsoft.pc-tester.quick-session/v3"}}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"session":{"schema":"hhsoft.pc-tester.quick-session/v4"},"session":null}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"session":{"schema":"hhsoft.pc-tester.quick-session/v4"}}},"test_results":null}`,
		`{"schema_version":1,"test_results":[{"evidence":[{"schema":"hhsoft.pc-tester.run-thermal/v1"}]}]}`,
		`{"schema_version":1,"thermal":{"schema":"hhsoft.pc-tester.run-thermal/v1"}}`,
	} {
		encoded := []byte(raw)
		if hasCoverageMetadata(encoded) {
			t.Fatal("fixture accidentally selected the older coverage gate")
		}
		for _, selfIssued := range []bool{false, true} {
			archive, envelope, keys := independentSignedViewArchive(t, encoded, nil, nil, product.Name, product.Version, selfIssued)
			if _, err := verifyViewFixture(archive, keys, selfIssued); err == nil {
				t.Fatal("signed malformed thermal claim bypassed strict validation")
			}
			var destination bytes.Buffer
			if WriteWithPDF(&destination, encoded, envelope, nil, nil) == nil {
				t.Fatal("producer accepted malformed thermal claim")
			}
		}
		if _, err := ManifestDigestWithPDF(encoded, nil, nil); err == nil {
			t.Fatal("digest helper accepted malformed thermal claim")
		}
	}
}

func TestSignedThermalBindingRejectsAlteredFullWorkerEvidence(t *testing.T) {
	payload := thermalPayload(t, "session-v4-local-heat")
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct{ old, replacement string }{
		{`"workers":1`, `"workers":2`},
		{`"duration_ms":3`, `"duration_ms":4,"duration_ms":3`},
		{`"duration_ms":3`, `"duration_\u006ds":4,"duration_ms":3`},
		{`"thermal":`, `"Thermal":`},
		{`"local_thermal_sources":`, `"local_thermal_sourceſ":`},
		{`"joined":true`, `"joined":false`},
		{`"session":{`, `"session":{` + strings.Repeat(" ", 64<<10)},
		{`"evidence_typed_sha256":"sha256:d0ade46bdda5d5cde8647afd1a1b93c01d51346d10ffcc28cdf71c91688e46d9"`, `"evidence_typed_sha256":"sha256:` + strings.Repeat("0", 64) + `"`},
	} {
		if !bytes.Contains(encoded, []byte(fixture.old)) {
			t.Fatal("binding mutation did not target fixture")
		}
		malformed := bytes.Replace(encoded, []byte(fixture.old), []byte(fixture.replacement), 1)
		archive, _, keys := independentSignedViewArchive(t, malformed, nil, nil, product.Name, product.Version, false)
		if _, err := verifyViewFixture(archive, keys, false); err == nil {
			t.Fatal("authenticated forged thermal evidence was accepted")
		}
	}
}
