package report

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const EnvelopeSchemaV1 = "hhsoft.pc-tester.report-envelope/v1"

var (
	ErrUnknownKey       = errors.New("report: unknown key")
	ErrInvalidSignature = errors.New("report: invalid signature")
)

type Envelope struct {
	Schema         string          `json:"schema"`
	Product        string          `json:"product"`
	Version        string          `json:"version"`
	SessionID      string          `json:"session_id"`
	GeneratedTime  string          `json:"generated_time"`
	Payload        json.RawMessage `json:"payload"`
	ManifestDigest string          `json:"manifest_digest"`
	KeyID          string          `json:"key_id"`
	Signature      string          `json:"signature"`
}

type Keyring map[string]ed25519.PublicKey

func Sign(envelope *Envelope, privateKey ed25519.PrivateKey) error {
	if len(privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("%w: private key has length %d", ErrInvalidSignature, len(privateKey))
	}
	if envelope == nil {
		return fmt.Errorf("%w: nil envelope", ErrInvalidSignature)
	}
	if err := validateEnvelope(*envelope); err != nil {
		return err
	}
	signature := ed25519.Sign(privateKey, envelope.SigningBytes())
	envelope.Signature = base64.StdEncoding.EncodeToString(signature)
	return nil
}

func Verify(envelope Envelope, keys Keyring) error {
	if err := validateEnvelope(envelope); err != nil {
		return err
	}
	publicKey, ok := keys[envelope.KeyID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, envelope.KeyID)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return ErrInvalidSignature
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrInvalidSignature
	}
	if base64.StdEncoding.EncodeToString(signature) != envelope.Signature {
		return ErrInvalidSignature
	}
	if !json.Valid(envelope.Payload) || !ed25519.Verify(publicKey, envelope.SigningBytes(), signature) {
		return ErrInvalidSignature
	}
	return nil
}

func validateEnvelope(envelope Envelope) error {
	if envelope.Schema != EnvelopeSchemaV1 {
		return fmt.Errorf("%w: unsupported schema", ErrInvalidSignature)
	}
	requiredFields := []struct{ name, value string }{
		{"product", envelope.Product}, {"version", envelope.Version},
		{"session ID", envelope.SessionID}, {"generated time", envelope.GeneratedTime},
		{"manifest digest", envelope.ManifestDigest}, {"key ID", envelope.KeyID},
	}
	for _, field := range requiredFields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: missing %s", ErrInvalidSignature, field.name)
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, envelope.GeneratedTime); err != nil {
		return fmt.Errorf("%w: invalid generated time", ErrInvalidSignature)
	}
	if !validSHA256Digest(envelope.ManifestDigest) {
		return fmt.Errorf("%w: invalid manifest digest", ErrInvalidSignature)
	}
	if !json.Valid(envelope.Payload) {
		return fmt.Errorf("%w: payload is not valid JSON", ErrInvalidSignature)
	}
	return nil
}

func validSHA256Digest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func (envelope Envelope) SigningBytes() []byte {
	fields := [][]byte{
		[]byte("HHSoft PC Tester report envelope v1"),
		[]byte(envelope.Schema),
		[]byte(envelope.Product),
		[]byte(envelope.Version),
		[]byte(envelope.SessionID),
		[]byte(envelope.GeneratedTime),
		envelope.Payload,
		[]byte(envelope.ManifestDigest),
		[]byte(envelope.KeyID),
	}
	canonical := make([]byte, 0, 256+len(envelope.Payload))
	for _, field := range fields {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		canonical = append(canonical, length[:]...)
		canonical = append(canonical, field...)
	}
	return canonical
}
