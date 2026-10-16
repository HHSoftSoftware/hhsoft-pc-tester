package bundle

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func FuzzVerifyHostileBundle(f *testing.F) {
	inventoryJSON := []byte(`{"schema_version":1}`)
	manifestDigest, err := ManifestDigest(inventoryJSON, nil)
	if err != nil {
		f.Fatal(err)
	}
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	envelope := report.Envelope{
		Schema:         "hhsoft.pc-tester.report-envelope/v1",
		Product:        "HHSoft PC Tester",
		Version:        "0.1.0",
		SessionID:      "fuzz-session",
		GeneratedTime:  "2026-10-13T00:00:00Z",
		Payload:        inventoryJSON,
		ManifestDigest: manifestDigest,
		KeyID:          "fuzz-key",
	}
	if err := report.Sign(&envelope, privateKey); err != nil {
		f.Fatal(err)
	}
	var valid bytes.Buffer
	if err := Write(&valid, inventoryJSON, envelope, nil); err != nil {
		f.Fatal(err)
	}
	keys := report.Keyring{"fuzz-key": privateKey.Public().(ed25519.PublicKey)}
	if _, err := Verify(bytes.NewReader(valid.Bytes()), int64(valid.Len()), keys); err != nil {
		f.Fatalf("valid seed did not verify: %v", err)
	}

	f.Add(valid.Bytes())
	f.Add([]byte{})
	f.Add([]byte("not a zip archive"))
	f.Add([]byte("PK\x03\x04\x00\x00"))
	f.Fuzz(func(t *testing.T, encoded []byte) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("Verify panicked for %d hostile bytes: %v", len(encoded), recovered)
			}
		}()
		_, _ = Verify(bytes.NewReader(encoded), int64(len(encoded)), keys)
	})
}

func FuzzBundleExactJSONSchemasFailClosed(f *testing.F) {
	f.Add(byte(0), "unknown")
	f.Add(byte(1), "schema")
	f.Add(byte(2), "Signature")
	f.Add(byte(3), "ſession_id")
	f.Fuzz(func(t *testing.T, location byte, fieldName string) {
		fieldJSON, err := json.Marshal(fieldName)
		if err != nil {
			t.Fatal(err)
		}
		extra := fmt.Sprintf(",%s:null", fieldJSON)
		var encoded []byte
		var decode func([]byte) error
		switch location % 3 {
		case 0:
			encoded = []byte(fmt.Sprintf(`{"schema":"%s","files":[]%s}`, ManifestSchema, extra))
			decode = func(value []byte) error {
				_, err := decodeManifest(value)
				return err
			}
		case 1:
			encoded = []byte(fmt.Sprintf(`{"schema":"%s","files":[{"name":"inventory.json","media_type":"application/json","size":1,"sha256":"sha256:00"%s}]}`, ManifestSchema, extra))
			decode = func(value []byte) error {
				_, err := decodeManifest(value)
				return err
			}
		default:
			encoded = []byte(fmt.Sprintf(`{"schema":"hhsoft.pc-tester.report-envelope/v1","product":"HHSoft PC Tester","version":"0.1.0","session_id":"fuzz","generated_time":"2026-10-13T00:00:00Z","payload":{"schema_version":1},"manifest_digest":"sha256:00","key_id":"fuzz-key","signature":"AA=="%s}`, extra))
			decode = func(value []byte) error {
				_, err := decodeEnvelope(value)
				return err
			}
		}
		if err := decode(encoded); err == nil {
			t.Fatalf("exact-schema parser accepted added object field %q", fieldName)
		}
	})
}
