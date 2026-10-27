package bundle

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
)

func TestSignedPointerVersionsWithAndWithoutViews(t *testing.T) {
	for _, name := range []string{"pointer-v1-standalone-pass", "pointer-v2-standalone-pass", "session-v5-pointer-v2-pass"} {
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
			for _, views := range []bool{false, true} {
				for _, selfIssued := range []bool{false, true} {
					var h, p []byte
					if views {
						h, p = html, pdf
					}
					archive, _, keys := independentSignedViewArchive(t, encoded, h, p, product.Name, product.Version, selfIssued)
					artifact, err := verifyViewFixture(archive, keys, selfIssued)
					if err != nil || !bytes.Equal(artifact.InventoryJSON, encoded) {
						t.Fatalf("signed pointer evidence changed or rejected: %v", err)
					}
				}
			}
		})
	}
}

func TestViewlessPointerClaimsCannotBecomeOpaque(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":1,"test_results":[{"id":"input.pointer.quick","version":2,"status":"pass","evidence":{}}]}`,
		`{"schema_version":1,"test_results":{"id":"input.pointer.quick","version":2}}`,
		`{"schema_version":1,"test_results":[{"id":"INPUT.POINTER.QUICK","version":2,"status":"skip","evidence":{}}]}`,
		`{"schema_version":1,"test_results":[{"id":"input.pointer.quick","version":2,"version":1}],"test_results":[]}`,
		`{"schema_version":1,"test_results":[{"id":"input.pointer.quick","id":"cpu.quick","ver\u0073ion":2}]}`,
		`{"schema_version":1,"test_results":{"evidence":{"SCHEMA":"HHSOFT.PC-TESTER.POINTER-FUNCTIONAL/V2"}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"schema":"hhsoft.pc-tester.pointer-functional/v2","schema":"historical"}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"schema":"hhsoft.pc-tester.pointer-functional/v1","wheel_axes":[]}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"wheel_axe\u0073":[],"wheels":[]}}}`,
	} {
		for _, selfIssued := range []bool{false, true} {
			archive, envelope, keys := independentSignedViewArchive(t, []byte(raw), nil, nil, product.Name, product.Version, selfIssued)
			if _, err := verifyViewFixture(archive, keys, selfIssued); err == nil {
				t.Fatal("hidden pointer claim bypassed viewless verification")
			}
			var destination bytes.Buffer
			if WriteWithPDF(&destination, []byte(raw), envelope, nil, nil) == nil {
				t.Fatal("producer accepted an opaque pointer claim")
			}
			if _, err := ManifestDigestWithPDF([]byte(raw), nil, nil); err == nil {
				t.Fatal("digest accepted an opaque pointer claim")
			}
		}
	}
}

func TestSignedPointerEvidenceRejectsRelabelingAndWheelForgery(t *testing.T) {
	payload := thermalPayload(t, "pointer-v2-standalone-pass")
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ old, replacement string }{
		{`"id":"input.pointer.quick","version":2`, `"id":"input.pointer.quick","version":1`},
		{`"hhsoft.pc-tester.pointer-functional/v2"`, `"hhsoft.pc-tester.pointer-functional/v1"`},
		{`"wheel_axes":`, `"wheel_ax\u0065s":[],"wheel_axes":`},
		{`"wheels":`, `"WHEELS":`},
		{`"negative_frames":1`, `"negative_frames":0`},
		{`"positive_frames":1`, `"positive_frames":18446744073709551615`},
		{`"rel_capabilities","value":"843"`, `"rel_capabilities","value":"943"`},
	} {
		if !bytes.Contains(encoded, []byte(change.old)) {
			t.Fatal("pointer mutation missed its fixture")
		}
		malformed := bytes.Replace(encoded, []byte(change.old), []byte(change.replacement), 1)
		archive, _, keys := independentSignedViewArchive(t, malformed, nil, nil, product.Name, product.Version, false)
		if _, err := verifyViewFixture(archive, keys, false); err == nil {
			t.Fatal("authenticated forged wheel evidence accepted")
		}
	}
}
