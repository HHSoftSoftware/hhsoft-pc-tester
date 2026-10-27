package bundle

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func TestSignedKeyboardEvidenceWithAndWithoutViews(t *testing.T) {
	payload := thermalPayload(t, "session-v5-keyboard-pass")
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
				t.Fatalf("signed keyboard report rejected: %v", err)
			}
		}
	}
}

func TestViewlessKeyboardClaimsCannotRecoverOpaqueHistoricalProfile(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":1,"test_results":[{"id":"input.keyboard.quick","version":2,"status":"pass","evidence":{}}]}`,
		`{"schema_version":1,"test_results":{"id":"input.keyboard.quick","version":2}}`,
		`{"schema_version":1,"test_results":[{"id":"INPUT.KEYBOARD.QUICK","version":2,"status":"skip","evidence":{}}]}`,
		`{"schema_version":1,"test_results":[{"id":"input.keyboard.quick","version":"2"}]}`,
		`{"schema_version":1,"test_results":[{"id":"input.keyboard.quick","version":2,"version":1}],"test_results":[]}`,
		`{"schema_version":1,"test_results":[{"id":"input.keyboard.quick","id":"cpu.quick","ver\u0073ion":2}]}`,
		`{"schema_version":1,"test_results":[{"id":"input.keyboard.quick","version":1,"status":"skip","evidence":{"schema":"hhsoft.pc-tester.keyboard-functional/v1"}}]}`,
		`{"schema_version":1,"test_results":{"evidence":{"schema":"hhsoft.pc-tester.keyboard-functional/v1","schema":"historical"}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"SCHEMA":"HHSOFT.PC-TESTER.KEYBOARD-FUNCTIONAL/V1"}}}`,
		`{"schema_version":1,"test_results":{"evidence":{"schema":"hhsoft.pc-tester.quick-session/v5"}}}`,
	} {
		for _, selfIssued := range []bool{false, true} {
			archive, envelope, keys := independentSignedViewArchive(t, []byte(raw), nil, nil, product.Name, product.Version, selfIssued)
			if _, err := verifyViewFixture(archive, keys, selfIssued); err == nil {
				t.Fatal("hidden keyboard claim bypassed viewless validation")
			}
			var destination bytes.Buffer
			if WriteWithPDF(&destination, []byte(raw), envelope, nil, nil) == nil {
				t.Fatal("producer accepted opaque keyboard claim")
			}
			if _, err := ManifestDigestWithPDF([]byte(raw), nil, nil); err == nil {
				t.Fatal("digest accepted opaque keyboard claim")
			}
		}
	}
}

func TestSignedKeyboardReportRejectsRelabelingAndHiddenEvidence(t *testing.T) {
	payload := thermalPayload(t, "session-v5-keyboard-pass")
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ old, replacement string }{
		{`"hhsoft.pc-tester.quick-session/v5"`, `"hhsoft.pc-tester.quick-session/v4"`},
		{`"id":"input.keyboard.quick","version":2`, `"id":"input.keyboard.quick","version":1`},
		{`"cleanup_confirmed":true,"devices":`, `"cleanup_confirmed":false,"cleanup_confirmed":true,"devices":`},
		{`"baseline_neutral":true`, `"baseline_neutral":false`},
		{`"fixed_keys":`, `"fixed_key\u0053":`},
		{`"fixed_keys":`, `"fixed_key\u0073":[],"fixed_keys":`},
		{`"duration_ms":6`, `"duration_ms":7`},
	} {
		if !bytes.Contains(encoded, []byte(change.old)) {
			t.Fatal("keyboard mutation did not target fixture")
		}
		malformed := bytes.Replace(encoded, []byte(change.old), []byte(change.replacement), 1)
		archive, _, keys := independentSignedViewArchive(t, malformed, nil, nil, product.Name, product.Version, false)
		if _, err := verifyViewFixture(archive, keys, false); err == nil {
			t.Fatal("authenticated forged keyboard report accepted")
		}
	}
}

func TestViewlessStandaloneKeyboardNonexecutionNote(t *testing.T) {
	payload := thermalPayload(t, "session-v4-unavailable")
	for index := range payload.TestResults {
		row := &payload.TestResults[index]
		if row.ID == "input.keyboard.quick" {
			row.Version = 2
		}
		if row.ID == "run.integrity.quick" {
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if err != nil {
				t.Fatal(err)
			}
			value.Session = nil
			row.Evidence, _ = json.Marshal(value)
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	archive, _, keys := independentSignedViewArchive(t, encoded, nil, nil, product.Name, product.Version, false)
	if _, err := verifyViewFixture(archive, keys, false); err != nil {
		t.Fatalf("standalone nonexecution note rejected: %v", err)
	}
}
