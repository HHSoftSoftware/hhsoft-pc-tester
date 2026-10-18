package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

type zipMember struct {
	name string
	data []byte
}

func TestWriteVerifyRoundTripIsDeterministic(t *testing.T) {
	inventoryJSON, renderedReport, _ := renderedFixture(t, product.Version)
	envelope, publicKey, _ := signedEnvelope(t, inventoryJSON, renderedReport)
	first := writeBundle(t, inventoryJSON, envelope, renderedReport)
	second := writeBundle(t, inventoryJSON, envelope, renderedReport)
	if !bytes.Equal(first, second) {
		t.Fatal("same inputs produced different archive bytes")
	}
	artifact, err := Verify(bytes.NewReader(first), int64(len(first)), report.Keyring{envelope.KeyID: publicKey})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(artifact.InventoryJSON, inventoryJSON) {
		t.Fatal("inventory bytes changed")
	}
	if !bytes.Equal(artifact.RenderedReport, renderedReport) {
		t.Fatal("rendered report bytes changed")
	}
	if artifact.Manifest.Schema != ManifestSchema || len(artifact.Manifest.Files) != 2 {
		t.Fatalf("unexpected manifest: %+v", artifact.Manifest)
	}
	if artifact.BundleSHA256 != sha256.Sum256(first) {
		t.Fatal("verified whole-bundle digest changed")
	}
}

func TestWriteVerifyWithoutRenderedReport(t *testing.T) {
	inventoryJSON := []byte("{\"schema_version\":1}")
	envelope, publicKey, _ := signedEnvelope(t, inventoryJSON, nil)
	encoded := writeBundle(t, inventoryJSON, envelope, nil)
	artifact, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{envelope.KeyID: publicKey})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.RenderedReport != nil || len(artifact.Manifest.Files) != 1 {
		t.Fatal("optional rendered report was present")
	}
}

func TestSelfIssuedRoundTripIsCanonicalAndExplicit(t *testing.T) {
	inventoryJSON := []byte(`{"schema_version":1,"components":[]}`)
	publicKey, privateKey, err := ed25519.GenerateKey(bytes.NewReader(bytes.Repeat([]byte{0x52}, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	keyID, err := SelfIssuedKeyID(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest, err := SelfIssuedManifestDigest(inventoryJSON, nil, publicKey)
	if err != nil {
		t.Fatal(err)
	}
	envelope := report.Envelope{Schema: report.EnvelopeSchemaV1, Product: "HHSoft PC Tester", Version: "1.0.0", SessionID: "self-test", GeneratedTime: "2026-10-13T00:00:00Z", Payload: inventoryJSON, ManifestDigest: manifestDigest, KeyID: keyID}
	if err := report.Sign(&envelope, privateKey); err != nil {
		t.Fatal(err)
	}
	var destination bytes.Buffer
	if err := WriteSelfIssued(&destination, inventoryJSON, envelope, nil, publicKey); err != nil {
		t.Fatal(err)
	}
	encoded := destination.Bytes()
	artifact, err := VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Envelope.KeyID != keyID || !bytes.Equal(artifact.SelfIssuedKey, publicKey) || len(artifact.Manifest.Files) != 2 || artifact.BundleSHA256 != sha256.Sum256(encoded) {
		t.Fatalf("unexpected artifact: %+v", artifact)
	}
	if _, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), nil); !errors.Is(err, ErrInvalidBundle) || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("untrusted verification got %v", err)
	}
}

func TestWriteRejectsMismatchedInputs(t *testing.T) {
	inventoryJSON := []byte("{\"schema_version\":1}")
	envelope, _, _ := signedEnvelope(t, inventoryJSON, nil)
	envelope.ManifestDigest = "sha256:" + string(bytes.Repeat([]byte("0"), 64))
	if err := Write(io.Discard, inventoryJSON, envelope, nil); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("got %v, want digest mismatch", err)
	}
	envelope, _, _ = signedEnvelope(t, []byte("{\"other\":true}"), nil)
	digest, err := ManifestDigest(inventoryJSON, nil)
	if err != nil {
		t.Fatal(err)
	}
	envelope.ManifestDigest = digest
	if err := Write(io.Discard, inventoryJSON, envelope, nil); !errors.Is(err, ErrPayloadMismatch) {
		t.Fatalf("got %v, want payload mismatch", err)
	}
}

func TestWriteRejectsDuplicateJSONMembers(t *testing.T) {
	inventoryJSON := []byte(`{"schema_version":1,"components":[],"components":[{"type":"system"}]}`)
	if _, err := ManifestDigest(inventoryJSON, nil); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("got %v, want invalid bundle", err)
	}
}

func TestManifestRejectsActivePDFView(t *testing.T) {
	pdf := []byte("%PDF-1.4\n/JavaScript\n%%EOF\n")
	if _, err := ManifestDigestWithPDF([]byte(`{"schema_version":1}`), nil, pdf); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("got %v, want invalid bundle", err)
	}
}

func TestVerifyRejectsDuplicateInventoryObjectMembers(t *testing.T) {
	inventoryJSON := []byte(`{"schema_version":1}`)
	envelope, publicKey, _ := signedEnvelope(t, inventoryJSON, nil)
	valid := membersOf(t, writeBundle(t, inventoryJSON, envelope, nil))
	duplicateJSON := []byte(`{"schema_version":1,"components":[],"components":[{"type":"system"}]}`)
	encoded := makeZIP(t, replaceMember(valid, InventoryName, duplicateJSON))
	if _, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{envelope.KeyID: publicKey}); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("got %v, want invalid bundle", err)
	}
}

func TestVerifyRejectsCaseVariantInventoryObjectMembers(t *testing.T) {
	inventoryJSON := []byte(`{"schema_version":1}`)
	envelope, publicKey, _ := signedEnvelope(t, inventoryJSON, nil)
	valid := membersOf(t, writeBundle(t, inventoryJSON, envelope, nil))
	caseVariantJSON := []byte(`{"schema_version":1,"Components":[{"type":"system"}]}`)
	encoded := makeZIP(t, replaceMember(valid, InventoryName, caseVariantJSON))
	if _, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{envelope.KeyID: publicKey}); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("got %v, want invalid bundle", err)
	}
}

func TestVerifyRejectsUnsafeDuplicateMissingAndUnexpectedMembers(t *testing.T) {
	inventoryJSON := []byte("{\"schema_version\":1}")
	envelope, _, _ := signedEnvelope(t, inventoryJSON, nil)
	valid := membersOf(t, writeBundle(t, inventoryJSON, envelope, nil))
	tests := map[string]struct {
		members []zipMember
		want    error
	}{
		"zip slip": {
			members: append(cloneMembers(valid), zipMember{name: "../inventory.json", data: inventoryJSON}),
			want:    ErrUnsafeMember,
		},
		"duplicate member": {
			members: append(cloneMembers(valid), zipMember{name: InventoryName, data: inventoryJSON}),
			want:    ErrDuplicateMember,
		},
		"case collision": {
			members: append(cloneMembers(valid), zipMember{name: "Inventory.json", data: inventoryJSON}),
			want:    ErrDuplicateMember,
		},
		"missing member": {
			members: withoutMember(valid, InventoryName),
			want:    ErrMissingMember,
		},
		"unexpected member": {
			members: append(cloneMembers(valid), zipMember{name: "notes.txt", data: []byte("untrusted")}),
			want:    ErrUnexpectedMember,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			encoded := makeZIP(t, test.members)
			if _, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), nil); !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
}

func TestVerifyRejectsAlteredContent(t *testing.T) {
	inventoryJSON, renderedReport, _ := renderedFixture(t, product.Version)
	envelope, publicKey, _ := signedEnvelope(t, inventoryJSON, renderedReport)
	valid := membersOf(t, writeBundle(t, inventoryJSON, envelope, renderedReport))
	tests := map[string][]zipMember{
		"inventory":       replaceMember(valid, InventoryName, []byte("{\"schema_version\":2}")),
		"rendered report": replaceMember(valid, RenderedReportName, []byte("<title>Changed</title>")),
		"manifest":        replaceMember(valid, ManifestName, append(copyOf(memberData(valid, ManifestName)), ' ')),
	}
	for name, members := range tests {
		t.Run(name, func(t *testing.T) {
			encoded := makeZIP(t, members)
			_, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{envelope.KeyID: publicKey})
			if !errors.Is(err, ErrDigestMismatch) {
				t.Fatalf("got %v, want digest mismatch", err)
			}
		})
	}
}

func TestVerifyRejectsNonCanonicalArchiveBoundary(t *testing.T) {
	inventoryJSON := []byte("{\"schema_version\":1}")
	envelope, publicKey, _ := signedEnvelope(t, inventoryJSON, nil)
	valid := writeBundle(t, inventoryJSON, envelope, nil)
	keyring := report.Keyring{envelope.KeyID: publicKey}
	for name, encoded := range map[string][]byte{
		"trailing bytes": append(copyOf(valid), 'X'),
		"leading bytes":  append([]byte("PK\x03\x04unlisted"), valid...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), keyring); !errors.Is(err, ErrInvalidBundle) {
				t.Fatalf("got %v, want invalid bundle", err)
			}
		})
	}
}

func TestVerifyRejectsInvalidSignature(t *testing.T) {
	inventoryJSON := []byte("{\"schema_version\":1}")
	envelope, publicKey, _ := signedEnvelope(t, inventoryJSON, nil)
	valid := membersOf(t, writeBundle(t, inventoryJSON, envelope, nil))
	envelope.SessionID = "changed"
	encodedEnvelope, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	encoded := makeZIP(t, replaceMember(valid, EnvelopeName, encodedEnvelope))
	_, err = Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{envelope.KeyID: publicKey})
	if !errors.Is(err, report.ErrInvalidSignature) {
		t.Fatalf("got %v, want invalid signature", err)
	}
}

func TestVerifyRejectsUnicodeEnvelopeAlias(t *testing.T) {
	inventoryJSON := []byte("{\"schema_version\":1}")
	envelope, publicKey, _ := signedEnvelope(t, inventoryJSON, nil)
	valid := membersOf(t, writeBundle(t, inventoryJSON, envelope, nil))
	envelopeJSON := memberData(valid, EnvelopeName)
	aliased := bytes.Replace(envelopeJSON, []byte(`"session_id":"test-session"`), []byte(`"session_id":"forged-session","ſession_id":"test-session"`), 1)
	encoded := makeZIP(t, replaceMember(valid, EnvelopeName, aliased))
	if _, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{envelope.KeyID: publicKey}); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("got %v, want invalid bundle", err)
	}
}

func TestVerifyRejectsPayloadDisagreementAndMalformedManifest(t *testing.T) {
	inventoryJSON := []byte("{\"schema_version\":1}")
	envelope, publicKey, privateKey := signedEnvelope(t, inventoryJSON, nil)
	valid := membersOf(t, writeBundle(t, inventoryJSON, envelope, nil))

	differentPayload := []byte("{\"schema_version\":2}")
	envelope.Payload = differentPayload
	if err := report.Sign(&envelope, privateKey); err != nil {
		t.Fatal(err)
	}
	encodedEnvelope, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	encoded := makeZIP(t, replaceMember(valid, EnvelopeName, encodedEnvelope))
	if _, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{envelope.KeyID: publicKey}); !errors.Is(err, ErrPayloadMismatch) {
		t.Fatalf("got %v, want payload mismatch", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(memberData(valid, ManifestName), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Files = append(manifest.Files, manifest.Files[0])
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	envelope, _, _ = signedEnvelopeWithDigest(t, inventoryJSON, digest(manifestJSON), privateKey)
	encodedEnvelope, err = json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	members := replaceMember(valid, ManifestName, manifestJSON)
	members = replaceMember(members, EnvelopeName, encodedEnvelope)
	encoded = makeZIP(t, members)
	if _, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{envelope.KeyID: publicKey}); !errors.Is(err, ErrDuplicateMember) {
		t.Fatalf("got %v, want duplicate manifest member", err)
	}
}

func signedEnvelope(t *testing.T, inventoryJSON, renderedReport []byte) (report.Envelope, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	digest, err := ManifestDigest(inventoryJSON, renderedReport)
	if err != nil {
		t.Fatal(err)
	}
	seed := bytes.Repeat([]byte{7}, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	return signedEnvelopeWithDigest(t, inventoryJSON, digest, privateKey)
}

func signedEnvelopeWithDigest(t *testing.T, inventoryJSON []byte, manifestDigest string, privateKey ed25519.PrivateKey) (report.Envelope, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	envelope := report.Envelope{
		Schema:         "hhsoft.pc-tester.report-envelope/v1",
		Product:        "HHSoft PC Tester",
		Version:        product.Version,
		SessionID:      "test-session",
		GeneratedTime:  "2026-10-12T20:00:00Z",
		Payload:        append(json.RawMessage(nil), inventoryJSON...),
		ManifestDigest: manifestDigest,
		KeyID:          "test-key",
	}
	if err := report.Sign(&envelope, privateKey); err != nil {
		t.Fatal(err)
	}
	return envelope, privateKey.Public().(ed25519.PublicKey), privateKey
}

func writeBundle(t *testing.T, inventoryJSON []byte, envelope report.Envelope, renderedReport []byte) []byte {
	t.Helper()
	var destination bytes.Buffer
	if err := Write(&destination, inventoryJSON, envelope, renderedReport); err != nil {
		t.Fatal(err)
	}
	return destination.Bytes()
}

func membersOf(t *testing.T, encoded []byte) []zipMember {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(encoded), int64(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	result := make([]zipMember, 0, len(archive.File))
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, zipMember{name: file.Name, data: data})
	}
	return result
}

func makeZIP(t *testing.T, members []zipMember) []byte {
	t.Helper()
	var destination bytes.Buffer
	archive := zip.NewWriter(&destination)
	for _, member := range members {
		writer, err := archive.Create(member.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(member.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return destination.Bytes()
}

func withoutMember(members []zipMember, name string) []zipMember {
	result := make([]zipMember, 0, len(members))
	for _, member := range members {
		if member.name != name {
			result = append(result, zipMember{name: member.name, data: copyOf(member.data)})
		}
	}
	return result
}

func replaceMember(members []zipMember, name string, data []byte) []zipMember {
	result := cloneMembers(members)
	for index := range result {
		if result[index].name == name {
			result[index].data = copyOf(data)
		}
	}
	return result
}

func memberData(members []zipMember, name string) []byte {
	for _, member := range members {
		if member.name == name {
			return member.data
		}
	}
	return nil
}

func cloneMembers(members []zipMember) []zipMember {
	result := make([]zipMember, len(members))
	for index, member := range members {
		result[index] = zipMember{name: member.name, data: copyOf(member.data)}
	}
	return result
}

func copyOf(value []byte) []byte {
	return append([]byte(nil), value...)
}
