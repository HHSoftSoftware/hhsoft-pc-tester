package bundle

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
)

func TestStorageBundlesVerifyRealViewsAndViewlessEvidence(t *testing.T) {
	p := thermalPayload(t, "storage-sample-standalone-pass-v1")
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	html, err := render.InventoryHTML(p)
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := render.InventoryPDF(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, views := range []struct{ html, pdf []byte }{{nil, nil}, {html, nil}, {nil, pdf}, {html, pdf}} {
		archive, _, keys := independentSignedViewArchive(t, raw, views.html, views.pdf, product.Name, product.Version, false)
		if _, err := verifyViewFixture(archive, keys, false); err != nil {
			t.Fatal("valid finite storage bundle rejected", err)
		}
		if _, err := ManifestDigestWithPDF(raw, views.html, views.pdf); err != nil {
			t.Fatal("valid storage manifest rejected", err)
		}
	}
	wrong := bytes.Replace(html, []byte("131072"), []byte("999999"), 1)
	if bytes.Equal(html, wrong) {
		t.Fatal("expected accepted count missing from HTML")
	}
	archive, _, keys := independentSignedViewArchive(t, raw, wrong, nil, product.Name, product.Version, false)
	if _, err := verifyViewFixture(archive, keys, false); err == nil {
		t.Fatal("signed but misleading storage view accepted")
	}
}

func TestStorageViewlessBundlesCannotDiscardClaimsOrCleanupErrors(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":1,"test_results":[{"id":"storage.quick","version":2,"version":1}],"test_results":[]}`,
		`{"schema_version":1,"test_results":{"evidence":{"SCHEMA":"HHSOFT.PC-TESTER.STORAGE-SAMPLE/V1"}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"storage_sou\u0072ce":null}}}`,
		`{"schema_version":1,"test_results":[{"ID":"STORAGE.QUICK","ver\u0073ion":2}]}`,
		`{"schema_version":1,"test_results":[{"evidence":{"schema":"hhsoft.pc-tester.storage-sample/v99","schema":"legacy"}}]}`,
	} {
		archive, _, keys := independentSignedViewArchive(t, []byte(raw), nil, nil, product.Name, product.Version, false)
		if _, err := verifyViewFixture(archive, keys, false); err == nil {
			t.Fatal("storage claim became opaque")
		}
		if _, err := ManifestDigestWithPDF([]byte(raw), nil, nil); err == nil {
			t.Fatal("invalid storage claim received manifest")
		}
	}
	p := thermalPayload(t, "storage-sample-standalone-pass-v1")
	var value map[string]any
	if json.Unmarshal(p.TestResults[0].Evidence, &value) != nil {
		t.Fatal("fixture")
	}
	value["cleanup_confirmed"] = false
	value["status"] = "error"
	value["stop_reason"] = "cleanup_unconfirmed"
	value["error"] = "cleanup_unconfirmed"
	p.TestResults[0].Status = "error"
	p.TestResults[0].Evidence, _ = json.Marshal(value)
	raw, _ := json.Marshal(p)
	archive, _, keys := independentSignedViewArchive(t, raw, nil, nil, product.Name, product.Version, false)
	if _, err := verifyViewFixture(archive, keys, false); err == nil {
		t.Fatal("uncertain cleanup verified")
	}
	if _, err := ManifestDigestWithPDF(raw, nil, nil); err == nil {
		t.Fatal("uncertain cleanup reached signing manifest")
	}
}
