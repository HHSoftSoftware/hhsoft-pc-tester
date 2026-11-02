// Package correction verifies separate operator assertions about public display
// metadata. It never rewrites the signed source report or its hardware evidence.
package correction

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

const (
	Schema               = "hhsoft.pc-tester.display-correction/v1"
	RequestSchema        = "hhsoft.pc-tester.display-correction-request/v1"
	MaximumRequestBytes  = 8 << 10
	MaximumArtifactBytes = 16 << 10
	MaximumViewBytes     = 64 << 10
	signatureDomain      = "HHSoft PC Tester display correction addendum v1"
)

var (
	ErrSource  = errors.New("correction: source verification failed")
	ErrInvalid = errors.New("correction: invalid addendum or request")
)

type sourceReference struct {
	ManifestDigest string `json:"manifest_digest"`
	BundleSHA256   string `json:"bundle_sha256"`
	SessionID      string `json:"session_id"`
}

type originalValue struct {
	present bool
	value   string
}

// Source has no exported authority fields. Its zero value cannot authorize a
// correction. Only full verification of acquired, closed input constructs it.
type Source struct {
	verified    bool
	reference   sourceReference
	componentID string
	original    [4]originalValue
	trust       string
}

type change struct {
	Field string `json:"field"`
	Value string `json:"value"`
}
type request struct {
	Schema  string   `json:"schema"`
	Changes []change `json:"changes"`
}
type unsignedRecord struct {
	Schema      string          `json:"schema"`
	ID          string          `json:"id"`
	Source      sourceReference `json:"source"`
	ComponentID string          `json:"component_id"`
	Changes     []change        `json:"changes"`
	KeyID       string          `json:"key_id"`
	PublicKey   string          `json:"public_key"`
}
type record struct {
	Schema      string          `json:"schema"`
	ID          string          `json:"id"`
	Source      sourceReference `json:"source"`
	ComponentID string          `json:"component_id"`
	Changes     []change        `json:"changes"`
	KeyID       string          `json:"key_id"`
	PublicKey   string          `json:"public_key"`
	Signature   string          `json:"signature"`
}

// Verified exposes neither mutable authority nor source inventory/key buffers.
type Verified struct {
	verified bool
	source   Source
	record   record
	digest   string
	trust    string
}

// VerifySource takes the hash from successful canonical bundle verification,
// never from separately hashing a pathname.
func VerifySource(raw []byte, keys report.Keyring, selfIssued bool) (Source, error) {
	if selfIssued && len(keys) != 0 || !selfIssued && len(keys) == 0 {
		return Source{}, ErrSource
	}
	var artifact bundle.Artifact
	var err error
	if selfIssued {
		artifact, err = bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	} else {
		artifact, err = bundle.Verify(bytes.NewReader(raw), int64(len(raw)), keys)
	}
	if err != nil || artifact.Envelope.Product != product.Name ||
		(artifact.Manifest.Schema != bundle.ManifestSchema && artifact.Manifest.Schema != bundle.RetestManifestSchema && artifact.Manifest.Schema != bundle.MemoryCapacityManifestSchema && artifact.Manifest.Schema != bundle.MemoryCapacityRetestManifestSchema) ||
		!boundedText(artifact.Envelope.SessionID, 256, false) {
		return Source{}, ErrSource
	}
	var payload app.InventoryPayload
	decoder := json.NewDecoder(bytes.NewReader(artifact.InventoryJSON))
	decoder.DisallowUnknownFields()
	if !utf8.Valid(artifact.InventoryJSON) || !validJSONStringEscapes(artifact.InventoryJSON) || decoder.Decode(&payload) != nil || decoder.Decode(new(any)) != io.EOF ||
		payload.SchemaVersion != inventory.SnapshotSchemaVersion || app.ValidateCoverage(payload) != nil {
		return Source{}, ErrSource
	}
	// Historical inventory-only bundles can omit views. Require exact typed
	// field spelling/representation here without changing those bundle readers.
	canonical, err := json.Marshal(payload)
	if err != nil || !equivalentObjects(artifact.InventoryJSON, canonical) {
		return Source{}, ErrSource
	}
	result := Source{reference: sourceReference{
		ManifestDigest: artifact.Envelope.ManifestDigest,
		BundleSHA256:   "sha256:" + hex.EncodeToString(artifact.BundleSHA256[:]),
		SessionID:      artifact.Envelope.SessionID,
	}, trust: trustLabel(selfIssued)}
	count := 0
	for _, component := range payload.Components {
		if component.Type != inventory.ComponentSystem {
			continue
		}
		count++
		if count != 1 || !boundedText(component.ID, 256, false) {
			return Source{}, ErrSource
		}
		result.componentID = component.ID
		for _, attribute := range component.Attributes {
			index := fieldIndex(attribute.Name)
			if index < 0 {
				continue
			}
			if result.original[index].present || !boundedText(attribute.Value, 1024, true) {
				return Source{}, ErrSource
			}
			result.original[index] = originalValue{present: true, value: attribute.Value}
		}
	}
	if count != 1 {
		return Source{}, ErrSource
	}
	for _, component := range payload.Components {
		if component.Type != inventory.ComponentSystem && component.ID == result.componentID {
			return Source{}, ErrSource
		}
	}
	result.verified = true
	return result, nil
}

// Create signs a reference and public operator values. Input change order is
// normalized; no observed value, fingerprint or outcome is replaced.
func Create(source Source, requested []byte, id, keyID string, key ed25519.PrivateKey) ([]byte, error) {
	if !source.verified || !report.ValidRetestID(id) || !validKeyID(keyID) || len(key) != ed25519.PrivateKeySize {
		return nil, ErrInvalid
	}
	derived := ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])
	defer clear(derived)
	if !bytes.Equal(derived, key) {
		return nil, ErrInvalid
	}
	var input request
	if decodeBounded(requested, MaximumRequestBytes, &input) != nil || input.Schema != RequestSchema {
		return nil, ErrInvalid
	}
	canonicalRequest, err := json.Marshal(input)
	if err != nil || !equivalentObjects(requested, canonicalRequest) {
		return nil, ErrInvalid
	}
	sort.Slice(input.Changes, func(i, j int) bool { return input.Changes[i].Field < input.Changes[j].Field })
	item := record{Schema: Schema, ID: id, Source: source.reference, ComponentID: source.componentID,
		Changes: input.Changes, KeyID: keyID, PublicKey: base64.StdEncoding.EncodeToString(key[ed25519.SeedSize:])}
	if !validRecord(source, item) {
		return nil, ErrInvalid
	}
	message, err := signingBytes(item)
	if err != nil {
		return nil, ErrInvalid
	}
	item.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, message))
	encoded, err := json.Marshal(item)
	if err != nil || len(encoded) > MaximumArtifactBytes {
		return nil, ErrInvalid
	}
	return encoded, nil
}

// Verify requires independent addendum trust and the exact verified source.
// A named embedded key does not independently establish signer identity.
func Verify(source Source, raw []byte, keys report.Keyring, selfIssued bool) (Verified, error) {
	if !source.verified || selfIssued && len(keys) != 0 || !selfIssued && len(keys) == 0 {
		return Verified{}, ErrInvalid
	}
	var item record
	if decodeBounded(raw, MaximumArtifactBytes, &item) != nil || !validRecord(source, item) {
		return Verified{}, ErrInvalid
	}
	canonical, err := json.Marshal(item)
	if err != nil || !bytes.Equal(raw, canonical) {
		return Verified{}, ErrInvalid
	}
	public, err := canonicalBase64(item.PublicKey, ed25519.PublicKeySize)
	if err != nil {
		return Verified{}, ErrInvalid
	}
	key := public
	if !selfIssued {
		key = keys[item.KeyID]
		if len(key) != ed25519.PublicKeySize || !bytes.Equal(key, public) {
			return Verified{}, ErrInvalid
		}
	}
	signature, err := canonicalBase64(item.Signature, ed25519.SignatureSize)
	if err != nil {
		return Verified{}, ErrInvalid
	}
	message, err := signingBytes(item)
	if err != nil || !ed25519.Verify(key, message, signature) {
		return Verified{}, ErrInvalid
	}
	sum := sha256.Sum256(raw)
	return Verified{verified: true, source: source, record: item, digest: "sha256:" + hex.EncodeToString(sum[:]), trust: trustLabel(selfIssued)}, nil
}

func validRecord(source Source, item record) bool {
	if item.Schema != Schema || !report.ValidRetestID(item.ID) || item.Source != source.reference ||
		item.ComponentID != source.componentID || !validKeyID(item.KeyID) || len(item.Changes) < 1 || len(item.Changes) > 4 {
		return false
	}
	for index, value := range item.Changes {
		field := fieldIndex(value.Field)
		if field < 0 || !validNewValue(value.Value) || index > 0 && item.Changes[index-1].Field >= value.Field {
			return false
		}
		original := source.original[field]
		if original.present && original.value == value.Value {
			return false
		}
	}
	return true
}

func signingBytes(item record) ([]byte, error) {
	encoded, err := json.Marshal(unsignedRecord{item.Schema, item.ID, item.Source, item.ComponentID, item.Changes, item.KeyID, item.PublicKey})
	if err != nil {
		return nil, ErrInvalid
	}
	message := append([]byte(signatureDomain), 0)
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(encoded)))
	message = append(message, size[:]...)
	return append(message, encoded...), nil
}

func canonicalBase64(value string, size int) ([]byte, error) {
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) != size || base64.StdEncoding.EncodeToString(decoded) != value {
		return nil, ErrInvalid
	}
	return decoded, nil
}

func fieldIndex(name string) int {
	switch name {
	case "product_family":
		return 0
	case "product_name":
		return 1
	case "product_version":
		return 2
	case "vendor":
		return 3
	default:
		return -1
	}
}

func boundedText(value string, maximum int, empty bool) bool {
	return (empty || value != "") && len(value) <= maximum && utf8.ValidString(value)
}

func validNewValue(value string) bool {
	if !boundedText(value, 256, false) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func validKeyID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("._:-", character)) {
			return false
		}
	}
	return true
}

func trustLabel(selfIssued bool) string {
	if selfIssued {
		return "explicit self-issued (signer identity not independently established)"
	}
	return "explicit external public key"
}
