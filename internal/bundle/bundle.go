package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

const (
	ManifestName        = "report-manifest.json"
	InventoryName       = "inventory.json"
	EnvelopeName        = "signatures/report-envelope.json"
	RenderedReportName  = "views/report.html"
	RenderedPDFName     = "views/report.pdf"
	SelfIssuedKeyName   = "trust/self-issued-ed25519.pub"
	ManifestSchema      = "hhsoft.pc-tester.bundle-manifest/v1"
	SelfIssuedMediaType = "application/vnd.hhsoft.ed25519-public-key"

	maxManifestBytes  = 64 << 10
	maxInventoryBytes = 16 << 20
	maxEnvelopeBytes  = 1 << 20
	maxRenderedBytes  = 32 << 20
	maxRenderedPDF    = 32 << 20
	maxBundleBytes    = 64 << 20
	maxBundleMembers  = 6
)

var (
	ErrInvalidBundle    = errors.New("bundle: invalid artifact")
	ErrUnsafeMember     = errors.New("bundle: unsafe member")
	ErrDuplicateMember  = errors.New("bundle: duplicate member")
	ErrMissingMember    = errors.New("bundle: missing member")
	ErrUnexpectedMember = errors.New("bundle: unexpected member")
	ErrDigestMismatch   = errors.New("bundle: digest mismatch")
	ErrPayloadMismatch  = errors.New("bundle: envelope payload mismatch")
)

type Manifest struct {
	Schema string         `json:"schema"`
	Files  []ManifestFile `json:"files"`
}

type ManifestFile struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}

type Artifact struct {
	Manifest       Manifest
	InventoryJSON  []byte
	Envelope       report.Envelope
	RenderedReport []byte
	RenderedPDF    []byte
	SelfIssuedKey  []byte
	BundleSHA256   [sha256.Size]byte
}

type sourceFile struct {
	name      string
	mediaType string
	data      []byte
}

func ManifestDigest(inventoryJSON, renderedReport []byte) (string, error) {
	return ManifestDigestWithPDF(inventoryJSON, renderedReport, nil)

}

func ManifestDigestWithPDF(inventoryJSON, renderedReport, renderedPDF []byte) (string, error) {
	if err := validateViews(inventoryJSON, renderedReport, renderedPDF, product.Name, product.Version); err != nil {
		return "", err
	}
	_, encoded, err := buildManifest(inventoryJSON, renderedReport, renderedPDF, nil)
	if err != nil {
		return "", err
	}
	return digest(encoded), nil
}

func SelfIssuedManifestDigest(inventoryJSON, renderedReport, publicKey []byte) (string, error) {
	return SelfIssuedManifestDigestWithPDF(inventoryJSON, renderedReport, nil, publicKey)
}

func SelfIssuedManifestDigestWithPDF(inventoryJSON, renderedReport, renderedPDF, publicKey []byte) (string, error) {
	if err := validateViews(inventoryJSON, renderedReport, renderedPDF, product.Name, product.Version); err != nil {
		return "", err
	}
	_, encoded, err := buildManifest(inventoryJSON, renderedReport, renderedPDF, publicKey)
	if err != nil {
		return "", err
	}
	return digest(encoded), nil
}

func SelfIssuedKeyID(publicKey []byte) (string, error) {
	if len(publicKey) != 32 {
		return "", errors.New("self-issued Ed25519 public key must contain exactly 32 bytes")
	}
	digest := sha256.Sum256(publicKey)
	return "self-ed25519-sha256:" + hex.EncodeToString(digest[:]), nil
}

func Write(destination io.Writer, inventoryJSON []byte, envelope report.Envelope, renderedReport []byte) error {
	return WriteWithPDF(destination, inventoryJSON, envelope, renderedReport, nil)

}

func WriteWithPDF(destination io.Writer, inventoryJSON []byte, envelope report.Envelope, renderedReport, renderedPDF []byte) error {
	return writeArchive(destination, inventoryJSON, envelope, renderedReport, renderedPDF, nil)
}

func WriteSelfIssued(destination io.Writer, inventoryJSON []byte, envelope report.Envelope, renderedReport, publicKey []byte) error {
	return WriteSelfIssuedWithPDF(destination, inventoryJSON, envelope, renderedReport, nil, publicKey)
}

func WriteSelfIssuedWithPDF(destination io.Writer, inventoryJSON []byte, envelope report.Envelope, renderedReport, renderedPDF, publicKey []byte) error {
	expectedKeyID, err := SelfIssuedKeyID(publicKey)
	if err != nil || envelope.KeyID != expectedKeyID {
		return fmt.Errorf("%w: invalid self-issued key identity", ErrInvalidBundle)
	}
	return writeArchive(destination, inventoryJSON, envelope, renderedReport, renderedPDF, publicKey)
}

func writeArchive(destination io.Writer, inventoryJSON []byte, envelope report.Envelope, renderedReport, renderedPDF, selfIssuedKey []byte) error {
	if err := validateViews(inventoryJSON, renderedReport, renderedPDF, envelope.Product, envelope.Version); err != nil {
		return err
	}
	_, manifestJSON, err := buildManifest(inventoryJSON, renderedReport, renderedPDF, selfIssuedKey)
	if err != nil {
		return err
	}
	if envelope.ManifestDigest != digest(manifestJSON) {
		return fmt.Errorf("%w: signed digest does not cover constructed manifest", ErrDigestMismatch)
	}
	if !equivalentJSON(inventoryJSON, envelope.Payload) {
		return ErrPayloadMismatch
	}
	envelopeJSON, err := marshalEnvelope(envelope)
	if err != nil {
		return fmt.Errorf("%w: encode envelope: %v", ErrInvalidBundle, err)
	}
	storedEnvelope, err := decodeEnvelope(envelopeJSON)
	if err != nil || !bytes.Equal(storedEnvelope.SigningBytes(), envelope.SigningBytes()) {
		return fmt.Errorf("%w: payload JSON has leading or trailing whitespace", ErrInvalidBundle)
	}
	if envelope.Signature == "" {
		return fmt.Errorf("%w: envelope is unsigned", ErrInvalidBundle)
	}

	archive := zip.NewWriter(destination)
	members := []sourceFile{
		{name: ManifestName, mediaType: "application/json", data: manifestJSON},
		{name: InventoryName, mediaType: "application/json", data: inventoryJSON},
	}
	if renderedReport != nil {
		members = append(members, sourceFile{name: RenderedReportName, mediaType: "text/html", data: renderedReport})
	}
	if renderedPDF != nil {
		members = append(members, sourceFile{name: RenderedPDFName, mediaType: "application/pdf", data: renderedPDF})
	}
	if selfIssuedKey != nil {
		members = append(members, sourceFile{name: SelfIssuedKeyName, mediaType: SelfIssuedMediaType, data: selfIssuedKey})
	}
	members = append(members, sourceFile{name: EnvelopeName, mediaType: "application/json", data: envelopeJSON})
	for _, member := range members {
		if err := writeMember(archive, member.name, member.data); err != nil {
			_ = archive.Close()
			return fmt.Errorf("%w: write %s: %v", ErrInvalidBundle, member.name, err)
		}
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("%w: close archive: %v", ErrInvalidBundle, err)
	}
	return nil
}

func Verify(source io.ReaderAt, size int64, keys report.Keyring) (Artifact, error) {
	return verify(source, size, keys, false)
}

func VerifySelfIssued(source io.ReaderAt, size int64) (Artifact, error) {
	return verify(source, size, nil, true)
}

func verify(source io.ReaderAt, size int64, keys report.Keyring, trustSelfIssued bool) (Artifact, error) {
	if err := validateArchiveBounds(source, size); err != nil {
		return Artifact{}, err
	}
	archive, err := zip.NewReader(source, size)
	if err != nil {
		return Artifact{}, fmt.Errorf("%w: open zip: %v", ErrInvalidBundle, err)
	}
	members := make(map[string][]byte, len(archive.File))
	foldedNames := make(map[string]string, len(archive.File))
	for _, file := range archive.File {
		if err := validateMemberName(file.Name); err != nil {
			return Artifact{}, err
		}
		folded := strings.ToLower(file.Name)
		if previous, exists := foldedNames[folded]; exists {
			return Artifact{}, fmt.Errorf("%w: %s conflicts with %s", ErrDuplicateMember, file.Name, previous)
		}
		foldedNames[folded] = file.Name
		limit, ok := memberLimit(file.Name)
		if !ok {
			return Artifact{}, fmt.Errorf("%w: %s", ErrUnexpectedMember, file.Name)
		}
		if file.Mode()&os.ModeType != 0 || file.UncompressedSize64 > uint64(limit) {
			return Artifact{}, fmt.Errorf("%w: %s", ErrInvalidBundle, file.Name)
		}
		contents, err := readMember(file, limit)
		if err != nil {
			return Artifact{}, fmt.Errorf("%w: read %s: %v", ErrInvalidBundle, file.Name, err)
		}
		members[file.Name] = contents
	}
	for _, required := range []string{ManifestName, InventoryName, EnvelopeName} {
		if _, ok := members[required]; !ok {
			return Artifact{}, fmt.Errorf("%w: %s", ErrMissingMember, required)
		}
	}
	if err := validateUniqueJSONMembers(members[InventoryName]); err != nil {
		return Artifact{}, fmt.Errorf("%w: inventory JSON: %v", ErrInvalidBundle, err)
	}

	manifest, err := decodeManifest(members[ManifestName])
	if err != nil {
		return Artifact{}, err
	}
	envelope, err := decodeEnvelope(members[EnvelopeName])
	if err != nil {
		return Artifact{}, err
	}
	if envelope.ManifestDigest != digest(members[ManifestName]) {
		return Artifact{}, fmt.Errorf("%w: report manifest", ErrDigestMismatch)
	}
	selfIssuedKey := members[SelfIssuedKeyName]
	if selfIssuedKey != nil {
		expectedKeyID, keyErr := SelfIssuedKeyID(selfIssuedKey)
		if keyErr != nil || envelope.KeyID != expectedKeyID {
			return Artifact{}, fmt.Errorf("%w: self-issued key identity", ErrInvalidBundle)
		}
	}
	if trustSelfIssued {
		if selfIssuedKey == nil {
			return Artifact{}, fmt.Errorf("%w: %s", ErrMissingMember, SelfIssuedKeyName)
		}
		keys = report.Keyring{envelope.KeyID: selfIssuedKey}
	}
	if err := report.Verify(envelope, keys); err != nil {
		return Artifact{}, fmt.Errorf("%w: envelope: %w", ErrInvalidBundle, err)
	}
	if err := verifyManifest(manifest, members); err != nil {
		return Artifact{}, err
	}
	if !equivalentJSON(members[InventoryName], envelope.Payload) {
		return Artifact{}, ErrPayloadMismatch
	}

	artifact := Artifact{
		Manifest:       manifest,
		InventoryJSON:  append([]byte(nil), members[InventoryName]...),
		Envelope:       envelope,
		RenderedReport: append([]byte(nil), members[RenderedReportName]...),
		RenderedPDF:    append([]byte(nil), members[RenderedPDFName]...),
		SelfIssuedKey:  append([]byte(nil), selfIssuedKey...),
	}
	comparison := &canonicalComparisonWriter{source: source, size: size, digest: sha256.New()}
	if err := writeArchive(comparison, artifact.InventoryJSON, artifact.Envelope, artifact.RenderedReport, artifact.RenderedPDF, artifact.SelfIssuedKey); err != nil || comparison.offset != size {
		return Artifact{}, fmt.Errorf("%w: noncanonical archive encoding", ErrInvalidBundle)
	}
	copy(artifact.BundleSHA256[:], comparison.digest.Sum(nil))
	return artifact, nil
}

func validateArchiveBounds(source io.ReaderAt, size int64) error {
	if size < 22 || size > maxBundleBytes {
		return ErrInvalidBundle
	}
	footer := make([]byte, 22)
	if count, err := source.ReadAt(footer, size-int64(len(footer))); err != nil || count != len(footer) || !bytes.Equal(footer[:4], []byte("PK\x05\x06")) {
		return fmt.Errorf("%w: invalid canonical end record", ErrInvalidBundle)
	}
	entries := binary.LittleEndian.Uint16(footer[10:12])
	centralSize := uint64(binary.LittleEndian.Uint32(footer[12:16]))
	centralOffset := uint64(binary.LittleEndian.Uint32(footer[16:20]))
	if binary.LittleEndian.Uint16(footer[4:6]) != 0 || binary.LittleEndian.Uint16(footer[6:8]) != 0 ||
		binary.LittleEndian.Uint16(footer[8:10]) != entries || entries > maxBundleMembers ||
		binary.LittleEndian.Uint16(footer[20:22]) != 0 || centralOffset+centralSize != uint64(size-22) {
		return fmt.Errorf("%w: unsupported or ambiguous archive structure", ErrInvalidBundle)
	}
	start := make([]byte, 4)
	if count, err := source.ReadAt(start, 0); err != nil || count != len(start) || !bytes.Equal(start, []byte("PK\x03\x04")) {
		return fmt.Errorf("%w: archive does not begin with a local record", ErrInvalidBundle)
	}
	return nil
}

type canonicalComparisonWriter struct {
	source io.ReaderAt
	size   int64
	offset int64
	digest hash.Hash
}

func (writer *canonicalComparisonWriter) Write(expected []byte) (int, error) {
	if int64(len(expected)) > writer.size-writer.offset {
		return 0, errors.New("canonical archive exceeds supplied size")
	}
	const maximumChunk = 64 << 10
	compared := 0
	for compared < len(expected) {
		chunk := len(expected) - compared
		if chunk > maximumChunk {
			chunk = maximumChunk
		}
		actual := make([]byte, chunk)
		count, err := writer.source.ReadAt(actual, writer.offset+int64(compared))
		if count != chunk || (err != nil && !errors.Is(err, io.EOF)) || !bytes.Equal(actual, expected[compared:compared+chunk]) {
			return 0, errors.New("archive bytes differ from canonical encoding")
		}
		compared += chunk
	}
	writer.offset += int64(len(expected))
	_, _ = writer.digest.Write(expected)
	return len(expected), nil
}

func buildManifest(inventoryJSON, renderedReport, renderedPDF, selfIssuedKey []byte) (Manifest, []byte, error) {
	if !json.Valid(inventoryJSON) || !utf8.Valid(inventoryJSON) || len(inventoryJSON) > maxInventoryBytes {
		return Manifest{}, nil, fmt.Errorf("%w: inventory JSON", ErrInvalidBundle)
	}
	if err := validateUniqueJSONMembers(inventoryJSON); err != nil {
		return Manifest{}, nil, fmt.Errorf("%w: inventory JSON: %v", ErrInvalidBundle, err)
	}
	files := []sourceFile{{name: InventoryName, mediaType: "application/json", data: inventoryJSON}}
	if renderedReport != nil {
		if len(renderedReport) > maxRenderedBytes || !utf8.Valid(renderedReport) {
			return Manifest{}, nil, fmt.Errorf("%w: rendered report", ErrInvalidBundle)
		}
		files = append(files, sourceFile{name: RenderedReportName, mediaType: "text/html", data: renderedReport})
	}
	if renderedPDF != nil {
		if len(renderedPDF) > maxRenderedPDF {
			return Manifest{}, nil, fmt.Errorf("%w: rendered PDF", ErrInvalidBundle)
		}
		files = append(files, sourceFile{name: RenderedPDFName, mediaType: "application/pdf", data: renderedPDF})
	}
	if selfIssuedKey != nil {
		if len(selfIssuedKey) != 32 {
			return Manifest{}, nil, fmt.Errorf("%w: self-issued public key", ErrInvalidBundle)
		}
		files = append(files, sourceFile{name: SelfIssuedKeyName, mediaType: SelfIssuedMediaType, data: selfIssuedKey})
	}
	manifest := Manifest{Schema: ManifestSchema, Files: make([]ManifestFile, 0, len(files))}
	for _, file := range files {
		manifest.Files = append(manifest.Files, ManifestFile{
			Name:      file.name,
			MediaType: file.mediaType,
			Size:      int64(len(file.data)),
			SHA256:    digest(file.data),
		})
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("%w: encode manifest: %v", ErrInvalidBundle, err)
	}
	return manifest, encoded, nil
}

func decodeManifest(encoded []byte) (Manifest, error) {
	fields, err := exactObjectFields(encoded, []string{"schema", "files"}, []string{"schema", "files"})
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: manifest schema: %v", ErrInvalidBundle, err)
	}
	var files []json.RawMessage
	if err := json.Unmarshal(fields["files"], &files); err != nil {
		return Manifest{}, fmt.Errorf("%w: manifest schema: %v", ErrInvalidBundle, err)
	}
	for _, file := range files {
		if _, err := exactObjectFields(file, []string{"name", "media_type", "size", "sha256"}, []string{"name", "media_type", "size", "sha256"}); err != nil {
			return Manifest{}, fmt.Errorf("%w: manifest schema: %v", ErrInvalidBundle, err)
		}
	}
	var manifest Manifest
	if err := decodeStrict(encoded, &manifest); err != nil || manifest.Schema != ManifestSchema {
		return Manifest{}, fmt.Errorf("%w: manifest schema", ErrInvalidBundle)
	}
	return manifest, nil
}

func decodeEnvelope(encoded []byte) (report.Envelope, error) {
	if _, err := exactObjectFields(encoded, []string{"schema", "product", "version", "session_id", "generated_time", "payload", "manifest_digest", "key_id", "signature"}, []string{"schema", "product", "version", "session_id", "generated_time", "payload", "manifest_digest", "key_id", "signature"}); err != nil {
		return report.Envelope{}, fmt.Errorf("%w: envelope schema: %v", ErrInvalidBundle, err)
	}
	var envelope report.Envelope
	if err := decodeStrict(encoded, &envelope); err != nil {
		return report.Envelope{}, fmt.Errorf("%w: envelope: %v", ErrInvalidBundle, err)
	}
	return envelope, nil
}

func exactObjectFields(encoded []byte, allowedNames, requiredNames []string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(allowedNames))
	for _, name := range allowedNames {
		allowed[name] = true
	}
	for name := range fields {
		if !allowed[name] {
			return nil, fmt.Errorf("noncanonical or unknown object member %q", name)
		}
	}
	for _, name := range requiredNames {
		if _, ok := fields[name]; !ok {
			return nil, fmt.Errorf("missing object member %q", name)
		}
	}
	return fields, nil
}

func marshalEnvelope(envelope report.Envelope) ([]byte, error) {
	if !json.Valid(envelope.Payload) {
		return nil, fmt.Errorf("%w: envelope payload", ErrInvalidBundle)
	}
	var encoded bytes.Buffer
	encoded.WriteString(`{"schema":`)
	writeJSONString(&encoded, envelope.Schema)
	encoded.WriteString(`,"product":`)
	writeJSONString(&encoded, envelope.Product)
	encoded.WriteString(`,"version":`)
	writeJSONString(&encoded, envelope.Version)
	encoded.WriteString(`,"session_id":`)
	writeJSONString(&encoded, envelope.SessionID)
	encoded.WriteString(`,"generated_time":`)
	writeJSONString(&encoded, envelope.GeneratedTime)
	encoded.WriteString(`,"payload":`)
	encoded.Write(envelope.Payload)
	encoded.WriteString(`,"manifest_digest":`)
	writeJSONString(&encoded, envelope.ManifestDigest)
	encoded.WriteString(`,"key_id":`)
	writeJSONString(&encoded, envelope.KeyID)
	encoded.WriteString(`,"signature":`)
	writeJSONString(&encoded, envelope.Signature)
	encoded.WriteByte('}')
	return encoded.Bytes(), nil
}

func writeJSONString(destination *bytes.Buffer, value string) {
	encoded, _ := json.Marshal(value)
	destination.Write(encoded)
}

func decodeStrict(encoded []byte, destination any) error {
	if err := validateUniqueJSONMembers(encoded); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON value")
	}
	return nil
}

func validateUniqueJSONMembers(encoded []byte) error {
	if !json.Valid(encoded) || !utf8.Valid(encoded) {
		return errors.New("invalid JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]string)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object member name is not a string")
			}
			foldedKey := strings.ToLower(key)
			if foldedKey != key {
				return fmt.Errorf("noncanonical object member %q", key)
			}
			if previous, ok := seen[foldedKey]; ok {
				return fmt.Errorf("duplicate or noncanonical case-variant object members %q and %q", previous, key)
			}
			seen[foldedKey] = key
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

func verifyManifest(manifest Manifest, members map[string][]byte) error {
	listed := make(map[string]bool, len(manifest.Files))
	for _, file := range manifest.Files {
		if file.Name != InventoryName && file.Name != RenderedReportName && file.Name != RenderedPDFName && file.Name != SelfIssuedKeyName {
			return fmt.Errorf("%w: manifest lists %s", ErrUnexpectedMember, file.Name)
		}
		if listed[file.Name] {
			return fmt.Errorf("%w: manifest lists %s twice", ErrDuplicateMember, file.Name)
		}
		listed[file.Name] = true
		contents, ok := members[file.Name]
		if !ok {
			return fmt.Errorf("%w: %s", ErrMissingMember, file.Name)
		}
		mediaType := "application/json"
		if file.Name == RenderedReportName {
			mediaType = "text/html"
		} else if file.Name == RenderedPDFName {
			mediaType = "application/pdf"
		} else if file.Name == SelfIssuedKeyName {
			mediaType = SelfIssuedMediaType
		}
		if file.MediaType != mediaType || file.Size != int64(len(contents)) || file.SHA256 != digest(contents) {
			return fmt.Errorf("%w: %s", ErrDigestMismatch, file.Name)
		}
		if file.Name == RenderedReportName && !utf8.Valid(contents) {
			return fmt.Errorf("%w: invalid UTF-8 in %s", ErrInvalidBundle, file.Name)
		}
	}
	if !listed[InventoryName] {
		return fmt.Errorf("%w: manifest entry %s", ErrMissingMember, InventoryName)
	}
	for name := range members {
		if name != ManifestName && name != EnvelopeName && !listed[name] {
			return fmt.Errorf("%w: %s", ErrUnexpectedMember, name)
		}
	}
	return nil
}

func validateMemberName(name string) error {
	if name == "" || !utf8.ValidString(name) || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || path.Clean(name) != name {
		return fmt.Errorf("%w: %q", ErrUnsafeMember, name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." || part == "" {
			return fmt.Errorf("%w: %q", ErrUnsafeMember, name)
		}
	}
	return nil
}

func memberLimit(name string) (int64, bool) {
	switch name {
	case ManifestName:
		return maxManifestBytes, true
	case InventoryName:
		return maxInventoryBytes, true
	case EnvelopeName:
		return maxEnvelopeBytes, true
	case RenderedReportName:
		return maxRenderedBytes, true
	case RenderedPDFName:
		return maxRenderedPDF, true
	case SelfIssuedKeyName:
		return 32, true
	default:
		return 0, false
	}
}

func readMember(file *zip.File, limit int64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	contents, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > limit {
		return nil, errors.New("member exceeds size limit")
	}
	return contents, nil
}

func writeMember(archive *zip.Writer, name string, contents []byte) error {
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o600)
	header.SetModTime(time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC))
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = writer.Write(contents)
	return err
}

func equivalentJSON(first, second []byte) bool {
	var compactFirst bytes.Buffer
	var compactSecond bytes.Buffer
	return json.Compact(&compactFirst, first) == nil && json.Compact(&compactSecond, second) == nil && bytes.Equal(compactFirst.Bytes(), compactSecond.Bytes())
}

func digest(contents []byte) string {
	value := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(value[:])
}
