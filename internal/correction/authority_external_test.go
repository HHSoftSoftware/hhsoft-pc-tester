package correction_test

import (
	"crypto/ed25519"
	"encoding/json"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/correction"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"testing"
)

func TestCorrectionExternalCallerCannotMintAuthority(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	request := []byte(`{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"field":"vendor","value":"Operator Vendor"}]}`)
	if out, err := correction.Create(correction.Source{}, request, "11111111-2222-4333-8444-555555555555", "fixture", key); err == nil || out != nil {
		t.Fatal("zero source granted authority")
	}
	fake, _ := json.Marshal(bundle.Artifact{Manifest: bundle.Manifest{Schema: bundle.ManifestSchema}, InventoryJSON: []byte(`{"schema_version":1}`), Envelope: report.Envelope{Product: "HHSoft PC Tester"}})
	if _, err := correction.VerifySource(fake, report.Keyring{"fixture": key.Public().(ed25519.PublicKey)}, false); err == nil {
		t.Fatal("fabricated artifact object granted authority")
	}
	if _, err := correction.RenderHTML(correction.Verified{}); err == nil {
		t.Fatal("zero verified value rendered")
	}
}
