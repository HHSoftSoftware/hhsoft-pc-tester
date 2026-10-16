package report

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSignAndVerifyRoundTrip(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testEnvelope()
	if err := Sign(&envelope, privateKey); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Envelope
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := Verify(decoded, Keyring{"test-key": publicKey}); err != nil {
		t.Fatal(err)
	}
}

func TestSignRejectsInvalidEnvelopeSemantics(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*Envelope){
		"nil payload":       func(value *Envelope) { value.Payload = nil },
		"wrong schema":      func(value *Envelope) { value.Schema = "hhpt.report.v2" },
		"missing product":   func(value *Envelope) { value.Product = " " },
		"missing version":   func(value *Envelope) { value.Version = "" },
		"missing session":   func(value *Envelope) { value.SessionID = "" },
		"missing timestamp": func(value *Envelope) { value.GeneratedTime = "" },
		"invalid timestamp": func(value *Envelope) { value.GeneratedTime = "2026-13-40T25:61:00Z" },
		"short digest":      func(value *Envelope) { value.ManifestDigest = "sha256:00" },
		"uppercase digest":  func(value *Envelope) { value.ManifestDigest = "sha256:" + strings.Repeat("A", 64) },
		"missing key ID":    func(value *Envelope) { value.KeyID = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := testEnvelope()
			mutate(&value)
			if err := Sign(&value, privateKey); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("got %v, want invalid signature", err)
			}
			if value.Signature != "" {
				t.Fatal("invalid envelope was signed")
			}
		})
	}
	if err := Sign(nil, privateKey); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("nil envelope: got %v, want invalid signature", err)
	}
}

func TestVerifyRejectsSemanticallyInvalidSignedEnvelope(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*Envelope){
		"wrong schema":      func(value *Envelope) { value.Schema = "hhpt.report.v2" },
		"missing product":   func(value *Envelope) { value.Product = "" },
		"missing version":   func(value *Envelope) { value.Version = "" },
		"missing session":   func(value *Envelope) { value.SessionID = "" },
		"invalid timestamp": func(value *Envelope) { value.GeneratedTime = "not-a-time" },
		"invalid digest":    func(value *Envelope) { value.ManifestDigest = "sha256:" + strings.Repeat("g", 64) },
		"missing key ID":    func(value *Envelope) { value.KeyID = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := testEnvelope()
			mutate(&value)
			value.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, value.SigningBytes()))
			if err := Verify(value, Keyring{"test-key": publicKey}); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("got %v, want invalid signature", err)
			}
		})
	}
}

func TestVerifyAllowsHistoricalProductVersion(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	value := testEnvelope()
	value.Version = "0.0.1"
	if err := Sign(&value, privateKey); err != nil {
		t.Fatal(err)
	}
	if err := Verify(value, Keyring{value.KeyID: publicKey}); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testEnvelope()
	if err := Sign(&envelope, privateKey); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*Envelope){
		"payload":         func(value *Envelope) { value.Payload = json.RawMessage(`{"passed":false}`) },
		"schema":          func(value *Envelope) { value.Schema += "-changed" },
		"product":         func(value *Envelope) { value.Product += "-changed" },
		"version":         func(value *Envelope) { value.Version += "-changed" },
		"session id":      func(value *Envelope) { value.SessionID += "-changed" },
		"generated time":  func(value *Envelope) { value.GeneratedTime += "-changed" },
		"manifest digest": func(value *Envelope) { value.ManifestDigest += "-changed" },
		"key id":          func(value *Envelope) { value.KeyID = "other-key" },
		"signature bytes": func(value *Envelope) { value.Signature = value.Signature[:len(value.Signature)-1] + "A" },
		"signature text":  func(value *Envelope) { value.Signature = value.Signature[:4] + "\n" + value.Signature[4:] },
		"invalid payload": func(value *Envelope) { value.Payload = json.RawMessage(`{"passed":`) },
	}
	keys := Keyring{"test-key": publicKey, "other-key": publicKey}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := envelope
			mutate(&changed)
			if err := Verify(changed, keys); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("got %v, want invalid signature", err)
			}
		})
	}
}

func TestVerifySeparatesUnknownKey(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testEnvelope()
	if err := Sign(&envelope, privateKey); err != nil {
		t.Fatal(err)
	}
	envelope.KeyID = "untrusted-key"
	err = Verify(envelope, Keyring{})
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("got %v, want unknown key", err)
	}
	if errors.Is(err, ErrInvalidSignature) {
		t.Fatal("unknown key classified as invalid signature")
	}
}

func TestSigningBytesAreDeterministicAndUnambiguous(t *testing.T) {
	envelope := testEnvelope()
	first := envelope.SigningBytes()
	second := envelope.SigningBytes()
	if !bytes.Equal(first, second) {
		t.Fatal("canonical bytes changed without input changes")
	}
	boundaryShifted := envelope
	boundaryShifted.Product = "HHSoft PC Tester0"
	boundaryShifted.Version = ".1.0"
	if bytes.Equal(first, boundaryShifted.SigningBytes()) {
		t.Fatal("length framing did not preserve field boundaries")
	}
}

func testEnvelope() Envelope {
	return Envelope{
		Schema:         EnvelopeSchemaV1,
		Product:        "HHSoft PC Tester",
		Version:        "0.1.0",
		SessionID:      "018bcfe5-6800-7def-8000-000000000001",
		GeneratedTime:  "2026-10-12T20:00:00Z",
		Payload:        json.RawMessage(`{"passed":true,"tests":2}`),
		ManifestDigest: "sha256:" + strings.Repeat("0", 64),
		KeyID:          "test-key",
	}
}
