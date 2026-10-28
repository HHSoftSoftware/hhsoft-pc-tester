package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

const retestFixtureID = "12345678-1234-4234-8234-123456789abc"

func retestReference() report.PreviousReport {
	return report.PreviousReport{ManifestDigest: "sha256:" + strings.Repeat("a", 64), BundleSHA256: "sha256:" + strings.Repeat("b", 64)}
}

// This encoder is independent of all bundle producer functions, manifest
// structs and envelope SigningBytes/Sign. The fixed wire fields and framing
// come from the v1/v2 contracts. Its valid signatures prevent a malformed
// profile test from passing merely because its signature was invalid.
func independentlySignedRetest(t *testing.T, inventoryJSON, html, pdf []byte, previous *report.PreviousReport, self bool, mutateManifest func(string) string, mutateEnvelope func(*report.Envelope)) ([]byte, report.Envelope, report.Keyring) {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x69}, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	type file struct {
		Name      string `json:"name"`
		MediaType string `json:"media_type"`
		Size      int64  `json:"size"`
		SHA256    string `json:"sha256"`
	}
	type member struct {
		name, media string
		data        []byte
	}
	members := []member{{"inventory.json", "application/json", inventoryJSON}}
	if html != nil {
		members = append(members, member{"views/report.html", "text/html", html})
	}
	if pdf != nil {
		members = append(members, member{"views/report.pdf", "application/pdf", pdf})
	}
	keyID := "independent-fixture-key"
	if self {
		keyID = fmt.Sprintf("self-ed25519-sha256:%x", sha256.Sum256(public))
		members = append(members, member{SelfIssuedKeyName, SelfIssuedMediaType, public})
	}
	manifest := struct {
		Schema   string                 `json:"schema"`
		Files    []file                 `json:"files"`
		Previous *report.PreviousReport `json:"previous_report,omitempty"`
		Kind     string                 `json:"revision_kind,omitempty"`
	}{Schema: "hhsoft.pc-tester.bundle-manifest/v1"}
	if previous != nil {
		manifest.Schema, manifest.Previous, manifest.Kind = "hhsoft.pc-tester.bundle-manifest/v2", previous, "retest"
	}
	for _, member := range members {
		manifest.Files = append(manifest.Files, file{member.name, member.media, int64(len(member.data)), fmt.Sprintf("sha256:%x", sha256.Sum256(member.data))})
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if mutateManifest != nil {
		manifestBytes = []byte(mutateManifest(string(manifestBytes)))
	}
	envelope := report.Envelope{Schema: "hhsoft.pc-tester.report-envelope/v1", Product: product.Name, Version: "0.0.1-fixture", SessionID: retestFixtureID, GeneratedTime: "2026-10-17T00:00:00Z", Payload: inventoryJSON, ManifestDigest: fmt.Sprintf("sha256:%x", sha256.Sum256(manifestBytes)), KeyID: keyID}
	if mutateEnvelope != nil {
		mutateEnvelope(&envelope)
	}
	var signing bytes.Buffer
	for _, field := range [][]byte{[]byte("HHSoft PC Tester report envelope v1"), []byte(envelope.Schema), []byte(envelope.Product), []byte(envelope.Version), []byte(envelope.SessionID), []byte(envelope.GeneratedTime), envelope.Payload, []byte(envelope.ManifestDigest), []byte(envelope.KeyID)} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(field)))
		signing.Write(size[:])
		signing.Write(field)
	}
	envelope.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, signing.Bytes()))
	keys := report.Keyring{keyID: public}
	if err := report.Verify(envelope, keys); err != nil {
		t.Fatalf("fixture signature precondition: %v", err)
	}
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	write := func(name string, data []byte) {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0o600)
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := writer.Write(data); err != nil || n != len(data) {
			t.Fatal("fixture ZIP write failed")
		}
	}
	write("report-manifest.json", manifestBytes)
	for _, member := range members {
		write(member.name, member.data)
	}
	write("signatures/report-envelope.json", envelopeBytes)
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes(), envelope, keys
}

func TestRetestIndependentProfilesAndLegacyArchiveBytes(t *testing.T) {
	for _, linked := range []bool{false, true} {
		for _, self := range []bool{false, true} {
			for _, views := range []string{"none", "html", "pdf", "both"} {
				t.Run(fmt.Sprintf("linked=%t/self=%t/%s", linked, self, views), func(t *testing.T) {
					payload := viewPayload(t)
					encoded, err := json.Marshal(payload)
					if err != nil {
						t.Fatal(err)
					}
					var html, pdf []byte
					var previous *report.PreviousReport
					if linked {
						value := retestReference()
						previous = &value
					}
					if views == "html" || views == "both" {
						if linked {
							html, err = render.InventoryRetestHTMLForVersion(payload, "0.0.1-fixture", *previous)
						} else {
							html, err = render.InventoryHTMLForVersion(payload, "0.0.1-fixture")
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					if views == "pdf" || views == "both" {
						if linked {
							pdf, err = render.InventoryRetestPDFForVersion(payload, "0.0.1-fixture", *previous)
						} else {
							pdf, err = render.InventoryPDFForVersion(payload, "0.0.1-fixture")
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					fixture, envelope, keys := independentlySignedRetest(t, encoded, html, pdf, previous, self, nil, func(e *report.Envelope) {
						if !linked {
							e.SessionID = "historical-non-UUID"
						}
					})
					artifact, err := verifyViewFixture(fixture, keys, self)
					if err != nil {
						t.Fatalf("independent canonical profile rejected: %v", err)
					}
					if artifact.BundleSHA256 != sha256.Sum256(fixture) {
						t.Fatal("canonical bundle digest changed")
					}
					var actual bytes.Buffer
					var public []byte
					if self {
						public = keys[envelope.KeyID]
					}
					if linked {
						err = WriteRetestWithPDF(&actual, encoded, envelope, html, pdf, *previous, public)
					} else if self {
						err = WriteSelfIssuedWithPDF(&actual, encoded, envelope, html, pdf, public)
					} else {
						err = WriteWithPDF(&actual, encoded, envelope, html, pdf)
					}
					if err != nil || !bytes.Equal(actual.Bytes(), fixture) {
						t.Fatalf("producer changed independent archive bytes: %v", err)
					}
					if !linked && (artifact.Manifest.PreviousReport != nil || artifact.Manifest.RevisionKind != "") {
						t.Fatal("legacy archive acquired a relation")
					}
				})
			}
		}
	}
}

func TestRetestResignedStrictManifestAndViewlessIdentity(t *testing.T) {
	previous := retestReference()
	valid, _, keys := independentlySignedRetest(t, []byte(`{"fixture":"no views or claims"}`), nil, nil, &previous, false, nil, nil)
	if _, err := Verify(bytes.NewReader(valid), int64(len(valid)), keys); err != nil {
		t.Fatalf("viewless positive precondition: %v", err)
	}
	refJSON, _ := json.Marshal(previous)
	field := `"previous_report":` + string(refJSON)
	cases := []struct {
		name     string
		manifest func(string) string
		envelope func(*report.Envelope)
	}{
		{"missing-reference", func(s string) string { return strings.Replace(s, ","+field, "", 1) }, nil},
		{"null-reference", func(s string) string { return strings.Replace(s, string(refJSON), "null", 1) }, nil},
		{"extra-reference-key", func(s string) string {
			return strings.Replace(s, string(refJSON), strings.TrimSuffix(string(refJSON), "}")+`,"extra":true}`, 1)
		}, nil},
		{"missing-bundle-digest", func(s string) string {
			return strings.Replace(s, `,"bundle_sha256":"`+previous.BundleSHA256+`"`, "", 1)
		}, nil},
		{"duplicate-reference", func(s string) string { return strings.Replace(s, field, field+","+field, 1) }, nil},
		{"duplicate-inner-key", func(s string) string {
			return strings.Replace(s, `"manifest_digest":`, `"manifest_digest":"`+previous.ManifestDigest+`","manifest_digest":`, 1)
		}, nil},
		{"case-alias", func(s string) string { return strings.Replace(s, `"previous_report"`, `"Previous_Report"`, 1) }, nil},
		{"escaped-key", func(s string) string { return strings.Replace(s, `"previous_report"`, `"previous_\u0072eport"`, 1) }, nil},
		{"wrong-kind", func(s string) string { return strings.Replace(s, `"retest"`, `"resume"`, 1) }, nil},
		{"missing-kind", func(s string) string { return strings.Replace(s, `,"revision_kind":"retest"`, "", 1) }, nil},
		{"extra-manifest-key", func(s string) string { return strings.TrimSuffix(s, "}") + `,"parent_id":"forbidden"}` }, nil},
		{"unknown-schema", func(s string) string { return strings.Replace(s, "bundle-manifest/v2", "bundle-manifest/v3", 1) }, nil},
		{"v1-with-relation", func(s string) string { return strings.Replace(s, "bundle-manifest/v2", "bundle-manifest/v1", 1) }, nil},
		{"wrong-product-no-view", nil, func(e *report.Envelope) { e.Product = "Other Signed Product" }},
		{"legacy-id-no-view", nil, func(e *report.Envelope) { e.SessionID = "legacy-id" }},
		{"uppercase-id-no-view", nil, func(e *report.Envelope) { e.SessionID = strings.ToUpper(retestFixtureID) }},
		{"v1-uuid-no-view", nil, func(e *report.Envelope) { e.SessionID = "12345678-1234-1234-8234-123456789abc" }},
		{"variant-no-view", nil, func(e *report.Envelope) { e.SessionID = "12345678-1234-4234-7234-123456789abc" }},
	}
	for _, digestField := range []string{"manifest", "bundle"} {
		for _, value := range []string{"", "sha256:" + strings.Repeat("A", 64), "SHA256:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("a", 63), "sha256:" + strings.Repeat("a", 65), "sha256:" + strings.Repeat("g", 64)} {
			field, value := digestField, value
			cases = append(cases, struct {
				name     string
				manifest func(string) string
				envelope func(*report.Envelope)
			}{fmt.Sprintf("%s-digest-%d-%s", field, len(value), value), func(s string) string {
				old := previous.ManifestDigest
				if field == "bundle" {
					old = previous.BundleSHA256
				}
				return strings.Replace(s, old, value, 1)
			}, nil})
		}
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture, _, keys := independentlySignedRetest(t, []byte(`{"fixture":"no views or claims"}`), nil, nil, &previous, false, test.manifest, test.envelope)
			if _, err := Verify(bytes.NewReader(fixture), int64(len(fixture)), keys); err == nil {
				t.Fatal("correctly signed malformed linked profile accepted")
			}
		})
	}
	legacy, _, keys := independentlySignedRetest(t, []byte(`{"legacy":true}`), nil, nil, nil, false, nil, func(e *report.Envelope) { e.Product = "Other Signed Product"; e.SessionID = "legacy-id" })
	if _, err := Verify(bytes.NewReader(legacy), int64(len(legacy)), keys); err != nil {
		t.Fatal("generic v1 validation was tightened")
	}
}

func TestRetestViewsAuthenticateBothDigestsAndRejectActiveSubstitution(t *testing.T) {
	payload := viewPayload(t)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	previous := retestReference()
	html, err := render.InventoryRetestHTMLForVersion(payload, "0.0.1-fixture", previous)
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := render.InventoryRetestPDFForVersion(payload, "0.0.1-fixture", previous)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		html, pdf []byte
	}{
		{"valid", html, pdf},
		{"wrong-html-reference", bytes.Replace(html, []byte(previous.BundleSHA256), []byte(previous.ManifestDigest), 1), pdf},
		{"active-html", append(append([]byte(nil), html...), []byte("<script>fixture()</script>")...), pdf},
		{"extra-pdf", html, append(append([]byte(nil), pdf...), []byte(" /JavaScript")...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, _, keys := independentlySignedRetest(t, encoded, test.html, test.pdf, &previous, false, nil, nil)
			_, err := Verify(bytes.NewReader(fixture), int64(len(fixture)), keys)
			if (err == nil) != (test.name == "valid") {
				t.Fatalf("view verification mismatch: %v", err)
			}
		})
	}
}

func TestRetestCanonicalBundleHashIncludesSignedEnvelope(t *testing.T) {
	first, one, keys := independentlySignedRetest(t, []byte(`{"same":"manifest"}`), nil, nil, nil, false, nil, func(e *report.Envelope) { e.SessionID = "parent-one" })
	second, two, _ := independentlySignedRetest(t, []byte(`{"same":"manifest"}`), nil, nil, nil, false, nil, func(e *report.Envelope) { e.SessionID = "parent-two"; e.GeneratedTime = "2026-10-18T00:00:00Z" })
	if one.ManifestDigest != two.ManifestDigest || bytes.Equal(first, second) {
		t.Fatal("equal manifest/distinct envelope precondition failed")
	}
	a, err := Verify(bytes.NewReader(first), int64(len(first)), keys)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Verify(bytes.NewReader(second), int64(len(second)), keys)
	if err != nil {
		t.Fatal(err)
	}
	if a.BundleSHA256 == b.BundleSHA256 {
		t.Fatal("exact parent identity ignored the signed envelope")
	}
	for _, encoded := range [][]byte{append(append([]byte(nil), first...), 0), first[:len(first)-1]} {
		if _, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), keys); err == nil {
			t.Fatal("noncanonical framing accepted")
		}
	}
}
