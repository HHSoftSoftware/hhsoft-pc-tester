package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

type sizeTestDestination struct {
	bytes.Buffer
	calls int
}

func (w *sizeTestDestination) Write(p []byte) (int, error) { w.calls++; return w.Buffer.Write(p) }

func sizeTestEnvelope(t *testing.T, raw []byte, self bool) (report.Envelope, ed25519.PublicKey, []byte) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x29}, 32))
	pub := key.Public().(ed25519.PublicKey)
	var embedded []byte
	keyID := "synthetic-envelope-limit-audit"
	var err error
	if self {
		embedded = pub
		keyID, err = SelfIssuedKeyID(pub)
		if err != nil {
			t.Fatal(err)
		}
	}
	var digest string
	if self {
		digest, err = SelfIssuedManifestDigest(raw, nil, pub)
	} else {
		digest, err = ManifestDigest(raw, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	e := report.Envelope{Schema: report.EnvelopeSchemaV1, Product: product.Name, Version: product.Version, SessionID: "synthetic-envelope-limit", GeneratedTime: "2026-11-10T00:00:00Z", Payload: raw, ManifestDigest: digest, KeyID: keyID}
	if err = report.Sign(&e, key); err != nil {
		t.Fatal(err)
	}
	return e, pub, embedded
}

func verifySizeTestBundle(t *testing.T, raw []byte, e report.Envelope, pub ed25519.PublicKey, self bool) {
	t.Helper()
	var err error
	if self {
		_, err = VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	} else {
		_, err = Verify(bytes.NewReader(raw), int64(len(raw)), report.Keyring{e.KeyID: pub})
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestWriteEnvelopeByteBoundaryBeforeDestination(t *testing.T) {
	const limit = 1 << 20
	for _, self := range []bool{false, true} {
		for _, delta := range []int{-1, 0, 1} {
			t.Run(fmt.Sprintf("self_%v_delta_%d", self, delta), func(t *testing.T) {
				base := []byte(`{"note":""}`)
				e, _, _ := sizeTestEnvelope(t, base, self)
				encoded, err := json.Marshal(e)
				if err != nil {
					t.Fatal(err)
				}
				raw := []byte(`{"note":"` + strings.Repeat("x", limit+delta-len(encoded)) + `"}`)
				e, pub, _ := sizeTestEnvelope(t, raw, self)
				encoded, err = json.Marshal(e)
				if err != nil || len(encoded) != limit+delta {
					t.Fatal("independent envelope boundary fixture", len(encoded), err)
				}
				var dst sizeTestDestination
				if self {
					err = WriteSelfIssued(&dst, raw, e, nil, pub)
				} else {
					err = Write(&dst, raw, e, nil)
				}
				if delta > 0 {
					if !errors.Is(err, ErrInvalidBundle) || dst.calls != 0 || dst.Len() != 0 {
						t.Fatalf("oversized member must fail before any destination call: err=%v calls=%d bytes=%d", err, dst.calls, dst.Len())
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				z, err := zip.NewReader(bytes.NewReader(dst.Bytes()), int64(dst.Len()))
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, m := range z.File {
					if m.Name == EnvelopeName {
						found = true
						if m.UncompressedSize64 != uint64(limit+delta) {
							t.Fatal("wire envelope boundary differs")
						}
					}
				}
				if !found {
					t.Fatal("missing envelope")
				}
				verifySizeTestBundle(t, dst.Bytes(), e, pub, self)
			})
		}
	}
}

// Inventory alone cannot bound an envelope: metadata and JSON escaping add bytes.
func TestWriteEnvelopeMetadataSizeBeforeDestination(t *testing.T) {
	const limit = 1 << 20
	raw := []byte(`{"note":"small"}`)
	for _, self := range []bool{false, true} {
		for _, fixture := range []struct {
			name    string
			session string
			escaped bool
		}{
			{"utf8", strings.Repeat("界", limit/3+1), false},
			{"escaped_utf8", strings.Repeat("<>&界", limit/6-1), true},
		} {
			t.Run(fmt.Sprintf("self_%v_%s", self, fixture.name), func(t *testing.T) {
				if !utf8.ValidString(fixture.session) || (fixture.escaped && len(fixture.session) >= limit) {
					t.Fatal("invalid metadata overflow fixture")
				}
				e, pub, _ := sizeTestEnvelope(t, raw, self)
				e.SessionID = fixture.session
				// Resign changed metadata with the deterministic fixture key.
				key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x29}, 32))
				if err := report.Sign(&e, key); err != nil {
					t.Fatal(err)
				}
				if err := report.Verify(e, report.Keyring{e.KeyID: pub}); err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(e)
				if err != nil || len(raw) > 100 || len(encoded) <= limit {
					t.Fatal("fixture did not isolate marshaled metadata overflow", err)
				}
				var dst sizeTestDestination
				if self {
					err = WriteSelfIssued(&dst, raw, e, nil, pub)
				} else {
					err = Write(&dst, raw, e, nil)
				}
				if !errors.Is(err, ErrInvalidBundle) || dst.calls != 0 || dst.Len() != 0 {
					t.Fatalf("metadata size rejection touched output: err=%v calls=%d bytes=%d", err, dst.calls, dst.Len())
				}
			})
		}
	}
}
