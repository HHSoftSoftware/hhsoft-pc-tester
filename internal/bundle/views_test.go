package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func TestVerifiedViewsRejectCorrectlySignedActiveOrUnrelatedContent(t *testing.T) {
	encoded, html, pdf := renderedFixture(t, product.Version)
	other := viewPayload(t)
	other.Components[0].Attributes[0].Value = "Different inventory CPU"
	otherHTML, err := render.InventoryHTML(other)
	if err != nil {
		t.Fatal(err)
	}
	otherPDF, err := render.InventoryPDF(other)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name      string
		html, pdf []byte
	}{
		{"script HTML", []byte("<!doctype html><script>/* inert test only */</script>"), pdf},
		{"resource HTML", []byte("<!doctype html><img src=\"https://example.invalid/inert\">"), pdf},
		{"event HTML", []byte("<!doctype html><body onload=\"/* inert test only */\"></body>"), pdf},
		{"unrelated static HTML", otherHTML, pdf},
		{"unrelated static PDF", html, otherPDF},
		{"encoded PDF action", html, actionNamePDF()},
		{"custom static PDF", html, []byte("%PDF-1.4\ncustom static bytes\n%%EOF\n")},
		{"empty HTML", []byte{}, pdf},
		{"empty PDF", html, []byte{}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, selfIssued := range []bool{false, true} {
				archive, envelope, keys := independentSignedViewArchive(t, encoded, fixture.html, fixture.pdf, product.Name, product.Version, selfIssued)
				if _, err := verifyViewFixture(archive, keys, selfIssued); err == nil {
					t.Fatalf("correctly signed %s was accepted (self-issued=%v)", fixture.name, selfIssued)
				}
				var destination bytes.Buffer
				if err := WriteWithPDF(&destination, encoded, envelope, fixture.html, fixture.pdf); err == nil {
					t.Fatal("producer accepted unsafe or unrelated views")
				}
			}
			if _, err := ManifestDigestWithPDF(encoded, fixture.html, fixture.pdf); err == nil {
				t.Fatal("digest helper accepted unsafe or unrelated views")
			}
		})
	}
}

func TestVerifiedViewsUseSignedVersionAndPermitAbsentOptionalViews(t *testing.T) {
	for _, version := range []string{product.Version, "0.0.9"} {
		encoded, html, pdf := renderedFixture(t, version)
		for _, views := range []struct {
			name      string
			html, pdf []byte
		}{
			{"both", html, pdf}, {"HTML only", html, nil}, {"PDF only", nil, pdf}, {"absent", nil, nil},
		} {
			for _, selfIssued := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/self=%v", version, views.name, selfIssued), func(t *testing.T) {
					archive, _, keys := independentSignedViewArchive(t, encoded, views.html, views.pdf, product.Name, version, selfIssued)
					artifact, err := verifyViewFixture(archive, keys, selfIssued)
					if err != nil {
						t.Fatalf("legitimate independently encoded report rejected: %v", err)
					}
					if !bytes.Equal(artifact.RenderedReport, views.html) || !bytes.Equal(artifact.RenderedPDF, views.pdf) {
						t.Fatal("view bytes changed")
					}
				})
			}
		}
	}
	encoded, html, pdf := renderedFixture(t, product.Version)
	for _, metadata := range []struct{ productName, version string }{
		{product.Name, "9.9.9"}, {"Other product", product.Version},
		{product.Name, "1.0\n/Injected"}, {product.Name, strings.Repeat("1", 129)},
	} {
		archive, _, keys := independentSignedViewArchive(t, encoded, html, pdf, metadata.productName, metadata.version, true)
		if _, err := VerifySelfIssued(bytes.NewReader(archive), int64(len(archive))); err == nil {
			t.Fatal("view metadata does not match signed identity")
		}
		_ = keys
	}
}

func TestVerifiedViewsStrictlyDecodeInventoryWithoutIgnoringFields(t *testing.T) {
	encoded, html, pdf := renderedFixture(t, product.Version)
	mutations := map[string][]byte{
		"unknown field":          bytes.Replace(encoded, []byte(`"schema_version":1`), []byte(`"schema_version":1,"extra":true`), 1),
		"Unicode case alias":     bytes.Replace(encoded, []byte(`"schema_version"`), []byte(`"\u017fchema_version"`), 1),
		"missing required field": bytes.Replace(encoded, []byte(`"schema_version":1,`), nil, 1),
		"unsupported schema":     bytes.Replace(encoded, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1),
		"null required field":    bytes.Replace(encoded, []byte(`"identified_parts":0`), []byte(`"identified_parts":null`), 1),
	}
	for name, changed := range mutations {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(changed, encoded) {
				t.Fatal("fixture did not mutate inventory")
			}
			archive, _, _ := independentSignedViewArchive(t, changed, html, pdf, product.Name, product.Version, true)
			if _, err := VerifySelfIssued(bytes.NewReader(archive), int64(len(archive))); err == nil {
				t.Fatal("ignored or ambiguous inventory fields were accepted")
			}
		})
	}
}

func TestVerifiedViewsPreserveSafelyRenderedScriptLikePayloadText(t *testing.T) {
	payload := viewPayload(t)
	payload.Components[0].Attributes[0].Value = `</td><script>inert</script> /OpenAction /JavaScript /Open#41ction (text)`
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
	if bytes.Contains(html, []byte("<script>")) || bytes.Contains(pdf, []byte("/JavaScript")) {
		t.Fatal("renderer failed the benign-text control")
	}
	for _, selfIssued := range []bool{false, true} {
		archive, _, keys := independentSignedViewArchive(t, encoded, html, pdf, product.Name, product.Version, selfIssued)
		if _, err := verifyViewFixture(archive, keys, selfIssued); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVerifiedViewsRejectSmallDeepEvidenceWithoutUnboundedIndentation(t *testing.T) {
	payload := viewPayload(t)
	payload.TestResults = []app.TestResult{{ID: "fixture", Version: 1, Status: app.TestInconclusive,
		Evidence: []byte(strings.Repeat("[", 2000) + "0" + strings.Repeat("]", 2000))}}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) >= 64<<10 {
		t.Fatal("fixture does not exercise small-input expansion")
	}
	for _, selfIssued := range []bool{false, true} {
		archive, _, keys := independentSignedViewArchive(t, encoded, []byte("<!doctype html><title>inert</title>"), nil, product.Name, product.Version, selfIssued)
		if _, err := verifyViewFixture(archive, keys, selfIssued); err == nil {
			t.Fatal("deep evidence was accepted")
		}
	}
}

func renderedFixture(t *testing.T, version string) ([]byte, []byte, []byte) {
	t.Helper()
	payload := viewPayload(t)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	html, err := render.InventoryHTMLForVersion(payload, version)
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := render.InventoryPDFForVersion(payload, version)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, html, pdf
}

func viewPayload(t *testing.T) app.InventoryPayload {
	t.Helper()
	payload, err := app.BuildInventoryPayload(inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentCPU, "cpu:0", map[string]string{"model": "Fixture CPU"}),
	}), bytes.Repeat([]byte{0x42}, 32), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func verifyViewFixture(encoded []byte, keys report.Keyring, selfIssued bool) (Artifact, error) {
	if selfIssued {
		return VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
	}
	return Verify(bytes.NewReader(encoded), int64(len(encoded)), keys)
}

// Deliberately do not call the producer's ManifestDigest, Write, buildManifest,
// marshalEnvelope, or writeMember. Valid signatures and canonical ZIP encoding
// must reach the independent verification boundary even for rejected views.
func independentSignedViewArchive(t *testing.T, inventoryJSON, html, pdf []byte, productName, version string, selfIssued bool) ([]byte, report.Envelope, report.Keyring) {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x57}, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	keyID := "fixture-provisioned"
	files := []struct {
		name, mediaType string
		data            []byte
	}{{InventoryName, "application/json", inventoryJSON}}
	if html != nil {
		files = append(files, struct {
			name, mediaType string
			data            []byte
		}{RenderedReportName, "text/html", html})
	}
	if pdf != nil {
		files = append(files, struct {
			name, mediaType string
			data            []byte
		}{RenderedPDFName, "application/pdf", pdf})
	}
	if selfIssued {
		keyID = fmt.Sprintf("self-ed25519-sha256:%x", sha256.Sum256(public))
		files = append(files, struct {
			name, mediaType string
			data            []byte
		}{SelfIssuedKeyName, SelfIssuedMediaType, public})
	}
	manifest := Manifest{Schema: ManifestSchema}
	for _, file := range files {
		manifest.Files = append(manifest.Files, ManifestFile{Name: file.name, MediaType: file.mediaType, Size: int64(len(file.data)), SHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(file.data))})
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	envelope := report.Envelope{Schema: report.EnvelopeSchemaV1, Product: productName, Version: version, SessionID: "view-fixture", GeneratedTime: "2026-10-17T00:00:00Z", Payload: inventoryJSON, ManifestDigest: fmt.Sprintf("sha256:%x", sha256.Sum256(manifestJSON)), KeyID: keyID}
	if err := report.Sign(&envelope, private); err != nil {
		t.Fatal(err)
	}
	if err := report.Verify(envelope, report.Keyring{keyID: public}); err != nil {
		t.Fatal("fixture signature is invalid")
	}
	envelopeJSON, err := json.Marshal(envelope)
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
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	write(ManifestName, manifestJSON)
	for _, file := range files {
		write(file.name, file.data)
	}
	write(EnvelopeName, envelopeJSON)
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes(), envelope, report.Keyring{keyID: public}
}

func actionNamePDF() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /Open#41ction 3 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		"<< /Type /Action /S /Java#53cript /JS (/* inert test only */) >>",
	}
	var output strings.Builder
	output.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return []byte(output.String())
}
