package correction

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

// This source is admitted through the real bundle writer and verifier. The
// correction authority is never manufactured from a caller-built Artifact.
func correctionCapacityReport(t *testing.T, payload app.InventoryPayload, linked, self bool) ([]byte, report.Keyring) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	key := correctionFixtureKey("source")
	public := key.Public().(ed25519.PublicKey)
	keyID := "source-fixture"
	var publicMember []byte
	if self {
		keyID, err = bundle.SelfIssuedKeyID(public)
		if err != nil {
			t.Fatal(err)
		}
		publicMember = public
	}
	var previous *report.PreviousReport
	if linked {
		parent, keys := correctionFixtureReport(t, payload, self, correctionFixtureID)
		verified, err := bundle.Verify(bytes.NewReader(parent), int64(len(parent)), keys)
		if err != nil {
			t.Fatal("legacy parent prerequisite")
		}
		previous = &report.PreviousReport{ManifestDigest: verified.Envelope.ManifestDigest, BundleSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(parent))}
	}
	var html, pdf []byte
	if previous == nil {
		html, err = render.InventoryMemoryCapacityHTMLForVersion(payload, product.Version)
		if err == nil {
			pdf, err = render.InventoryMemoryCapacityPDFForVersion(payload, product.Version)
		}
	} else {
		html, err = render.InventoryMemoryCapacityRetestHTMLForVersion(payload, product.Version, *previous)
		if err == nil {
			pdf, err = render.InventoryMemoryCapacityRetestPDFForVersion(payload, product.Version, *previous)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	digest, err := bundle.MemoryCapacityManifestDigestWithPDF(encoded, html, pdf, publicMember, previous)
	if err != nil {
		t.Fatal(err)
	}
	envelope := report.Envelope{Schema: report.EnvelopeSchemaV1, Product: product.Name, Version: product.Version, SessionID: correctionFixtureSession, GeneratedTime: "2026-01-01T00:00:00Z", Payload: encoded, ManifestDigest: digest, KeyID: keyID}
	if err := report.Sign(&envelope, key); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := bundle.WriteMemoryCapacityWithPDF(&out, encoded, envelope, html, pdf, publicMember, previous); err != nil {
		t.Fatal(err)
	}
	keys := report.Keyring{keyID: public}
	var artifact bundle.Artifact
	if self {
		artifact, err = bundle.VerifySelfIssued(bytes.NewReader(out.Bytes()), int64(out.Len()))
	} else {
		artifact, err = bundle.Verify(bytes.NewReader(out.Bytes()), int64(out.Len()), keys)
	}
	if err != nil {
		t.Fatalf("current source full-verification prerequisite: %v", err)
	}
	schema := bundle.MemoryCapacityManifestSchema
	if linked {
		schema = bundle.MemoryCapacityRetestManifestSchema
	}
	if artifact.Manifest.Schema != schema || artifact.Manifest.ViewProfile != bundle.MemoryCapacityViewProfile || !bytes.Equal(artifact.InventoryJSON, encoded) || !bytes.Contains(artifact.RenderedReport, []byte("Memory capacity observations")) {
		t.Fatal("current profile or immutable observations lost")
	}
	if linked && (artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != *previous) {
		t.Fatal("linked source did not preserve exact parent hashes")
	}
	return append([]byte(nil), out.Bytes()...), keys
}

func TestCorrectionMemoryCapacitySourceProfiles(t *testing.T) {
	for _, tc := range []struct {
		name         string
		linked, self bool
	}{{"ordinary_external", false, false}, {"ordinary_self", false, true}, {"linked_external", true, false}, {"linked_self", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			payload := correctionFixturePayload(t)
			before, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			raw, keys := correctionCapacityReport(t, payload, tc.linked, tc.self)
			original := append([]byte(nil), raw...)
			if tc.self {
				keys = nil
			}
			source, err := VerifySource(raw, keys, tc.self)
			if err != nil {
				t.Fatalf("current source was not admitted: %v", err)
			}
			addendum, operator := correctionFixtureAddendum(t, source)
			verified, err := Verify(source, addendum, report.Keyring{"operator-fixture": operator.Public().(ed25519.PublicKey)}, false)
			if err != nil {
				t.Fatal(err)
			}
			html, err := RenderHTML(verified)
			if err != nil || !bytes.Contains(html, []byte("Observed Vendor")) || !bytes.Contains(html, []byte("Operator Vendor")) {
				t.Fatal("current-source correction display disconnected")
			}
			text, err := VerificationText(verified)
			if err != nil || len(text) == 0 {
				t.Fatal("current-source verification output disconnected")
			}
			after, err := json.Marshal(payload)
			if err != nil || !bytes.Equal(before, after) || !bytes.Equal(raw, original) {
				t.Fatal("correction path changed source observations or artifact")
			}
		})
	}
}

func TestCorrectionMemoryCapacityExactSourceBinding(t *testing.T) {
	payload := correctionFixturePayload(t)
	oldRaw, oldKeys := correctionFixtureReport(t, payload, false, correctionFixtureSession)
	newRaw, newKeys := correctionCapacityReport(t, payload, false, false)
	oldArtifact, err := bundle.Verify(bytes.NewReader(oldRaw), int64(len(oldRaw)), oldKeys)
	if err != nil {
		t.Fatal(err)
	}
	newArtifact, err := bundle.Verify(bytes.NewReader(newRaw), int64(len(newRaw)), newKeys)
	if err != nil {
		t.Fatal(err)
	}
	if oldArtifact.Manifest.Schema != bundle.ManifestSchema || newArtifact.Manifest.Schema != bundle.MemoryCapacityManifestSchema || !bytes.Equal(oldArtifact.InventoryJSON, newArtifact.InventoryJSON) || oldArtifact.Envelope.ManifestDigest == newArtifact.Envelope.ManifestDigest || oldArtifact.BundleSHA256 == newArtifact.BundleSHA256 {
		t.Fatal("independent legacy/current same-observation prerequisites failed")
	}
	oldSource, err := VerifySource(oldRaw, oldKeys, false)
	if err != nil {
		t.Fatal(err)
	}
	newSource, err := VerifySource(newRaw, newKeys, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name            string
		original, other Source
	}{{"legacy_addendum_with_current_source", oldSource, newSource}, {"current_addendum_with_legacy_source", newSource, oldSource}} {
		t.Run(tc.name, func(t *testing.T) {
			raw, key := correctionFixtureAddendum(t, tc.original)
			keys := report.Keyring{"operator-fixture": key.Public().(ed25519.PublicKey)}
			if _, err := Verify(tc.original, raw, keys, false); err != nil {
				t.Fatal("valid exact pair prerequisite")
			}
			if _, err := Verify(tc.other, raw, keys, false); err == nil {
				t.Fatal("same inventory improperly substituted for exact signed source artifact")
			}
		})
	}
}
