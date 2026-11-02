package bundle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

// These are immutable synthetic pre-change artifacts. Current producers must
// never be used to regenerate their expected archive or view bytes.
//
//go:embed testdata/memory_capacity_legacy/fixtures.json
var capacityLegacyFixtureJSON []byte

type capacityFrozenBytes struct {
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
	Base64 string `json:"base64"`
}
type capacityFrozenMember struct {
	Name  string              `json:"name"`
	Bytes capacityFrozenBytes `json:"bytes"`
}
type capacityFrozenFixture struct {
	Name                 string                 `json:"name"`
	ManifestSchema       string                 `json:"manifest_schema"`
	Trust                string                 `json:"trust"`
	SessionID            string                 `json:"session_id"`
	ManifestDigest       string                 `json:"manifest_digest"`
	VerifiedBundleSHA256 string                 `json:"verified_bundle_sha256"`
	PreviousReport       *report.PreviousReport `json:"previous_report,omitempty"`
	Archive              capacityFrozenBytes    `json:"archive"`
	Members              []capacityFrozenMember `json:"members"`
}
type capacityFrozenSet struct {
	PublicKey capacityFrozenBytes     `json:"public_key"`
	Fixtures  []capacityFrozenFixture `json:"fixtures"`
}

func capacityThaw(t *testing.T, frozen capacityFrozenBytes, maximum int) []byte {
	t.Helper()
	if frozen.Size <= 0 || frozen.Size > maximum || len(frozen.Base64) > 4*((maximum+2)/3) {
		t.Fatal("frozen fixture bounds")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(frozen.Base64)
	if err != nil || len(raw) != frozen.Size || base64.StdEncoding.EncodeToString(raw) != frozen.Base64 ||
		fmt.Sprintf("%x", sha256.Sum256(raw)) != frozen.SHA256 {
		t.Fatal("frozen fixture byte identity")
	}
	return raw
}

func capacityFrozenFixtures(t *testing.T) capacityFrozenSet {
	t.Helper()
	var set capacityFrozenSet
	decoder := json.NewDecoder(bytes.NewReader(capacityLegacyFixtureJSON))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&set) != nil || decoder.Decode(new(any)) != io.EOF || len(set.Fixtures) != 4 {
		t.Fatal("frozen fixture record shape")
	}
	if len(capacityThaw(t, set.PublicKey, 32)) != ed25519.PublicKeySize {
		t.Fatal("frozen public test key")
	}
	names := []string{"ordinary-external", "ordinary-self-issued", "linked-external", "linked-self-issued"}
	for index, name := range names {
		row := set.Fixtures[index]
		if row.Name != name || row.Trust != []string{"external", "self-issued"}[index%2] {
			t.Fatal("frozen fixture identity")
		}
		schema := "hhsoft.pc-tester.bundle-manifest/v1"
		if index >= 2 {
			schema = "hhsoft.pc-tester.bundle-manifest/v2"
		}
		if row.ManifestSchema != schema || (row.PreviousReport != nil) != (index >= 2) {
			t.Fatal("frozen legacy profile")
		}
	}
	return set
}

func capacityFrozenMemberBytes(t *testing.T, row capacityFrozenFixture, name string) []byte {
	t.Helper()
	var value []byte
	for _, member := range row.Members {
		if member.Name != name {
			continue
		}
		if value != nil {
			t.Fatal("duplicate frozen member")
		}
		value = capacityThaw(t, member.Bytes, 64<<10)
	}
	if value == nil {
		t.Fatal("missing frozen member")
	}
	return value
}

func capacityLegacyPayload(t *testing.T) (app.InventoryPayload, []byte) {
	t.Helper()
	fixtures := capacityFrozenFixtures(t)
	raw := capacityFrozenMemberBytes(t, fixtures.Fixtures[0], "inventory.json")
	var payload app.InventoryPayload
	if json.Unmarshal(raw, &payload) != nil || len(payload.Components) != 4 {
		t.Fatal("frozen payload prerequisite")
	}
	return payload, raw
}

func TestMemoryCapacityFrozenLegacyFixtureBytes(t *testing.T) {
	set := capacityFrozenFixtures(t)
	public := ed25519.PublicKey(capacityThaw(t, set.PublicKey, 32))
	keys := report.Keyring{"memory-capacity-legacy-fixture-v1": public}
	names := []string{"ordinary-external", "ordinary-self-issued", "linked-external", "linked-self-issued"}
	for index, name := range names {
		t.Run(name, func(t *testing.T) {
			row := set.Fixtures[index]
			raw := capacityThaw(t, row.Archive, 256<<10)
			self := index%2 == 1
			artifact, err := verifyViewFixture(raw, keys, self)
			if err != nil {
				t.Fatalf("actual legacy archive: %v", err)
			}
			if artifact.Manifest.Schema != row.ManifestSchema || artifact.Manifest.ViewProfile != "" ||
				artifact.Envelope.SessionID != row.SessionID || artifact.Envelope.Version != "0.1.0-dev.0" ||
				artifact.Envelope.ManifestDigest != row.ManifestDigest ||
				fmt.Sprintf("sha256:%x", artifact.BundleSHA256) != row.VerifiedBundleSHA256 ||
				fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) != row.VerifiedBundleSHA256 {
				t.Fatal("legacy verified identity changed")
			}
			actualMembers := membersOf(t, raw)
			if len(actualMembers) != len(row.Members) {
				t.Fatal("legacy member count changed")
			}
			for memberIndex, member := range row.Members {
				if actualMembers[memberIndex].name != member.Name ||
					!bytes.Equal(actualMembers[memberIndex].data, capacityThaw(t, member.Bytes, 64<<10)) {
					t.Fatal("legacy member bytes or order changed")
				}
			}
			inventoryJSON := capacityFrozenMemberBytes(t, row, "inventory.json")
			html := capacityFrozenMemberBytes(t, row, "views/report.html")
			pdf := capacityFrozenMemberBytes(t, row, "views/report.pdf")
			if !bytes.Equal(artifact.InventoryJSON, inventoryJSON) ||
				!bytes.Equal(artifact.RenderedReport, html) || !bytes.Equal(artifact.RenderedPDF, pdf) {
				t.Fatal("legacy inventory or views changed")
			}
			if !bytes.Equal(inventoryJSON, capacityFrozenMemberBytes(t, set.Fixtures[0], "inventory.json")) {
				t.Fatal("fixture payloads differ")
			}
			if row.PreviousReport != nil {
				parentRaw := capacityThaw(t, set.Fixtures[index-2].Archive, 256<<10)
				parent, err := verifyViewFixture(parentRaw, keys, self)
				if err != nil {
					t.Fatal("actual parent prerequisite")
				}
				expected := report.PreviousReport{ManifestDigest: parent.Envelope.ManifestDigest,
					BundleSHA256: fmt.Sprintf("sha256:%x", parent.BundleSHA256)}
				if artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != expected ||
					*row.PreviousReport != expected || artifact.Manifest.RevisionKind != "retest" {
					t.Fatal("legacy parent binding changed")
				}
			} else if artifact.Manifest.PreviousReport != nil || artifact.Manifest.RevisionKind != "" {
				t.Fatal("ordinary fixture gained a relation")
			}
			var payload app.InventoryPayload
			if json.Unmarshal(inventoryJSON, &payload) != nil {
				t.Fatal("frozen inventory decode")
			}
			var oldHTML, oldPDF, newHTML, newPDF []byte
			if row.PreviousReport == nil {
				oldHTML, err = render.InventoryHTMLForVersion(payload, artifact.Envelope.Version)
				if err == nil {
					oldPDF, err = render.InventoryPDFForVersion(payload, artifact.Envelope.Version)
				}
				if err == nil {
					newHTML, err = render.InventoryMemoryCapacityHTMLForVersion(payload, artifact.Envelope.Version)
				}
				if err == nil {
					newPDF, err = render.InventoryMemoryCapacityPDFForVersion(payload, artifact.Envelope.Version)
				}
			} else {
				oldHTML, err = render.InventoryRetestHTMLForVersion(payload, artifact.Envelope.Version, *row.PreviousReport)
				if err == nil {
					oldPDF, err = render.InventoryRetestPDFForVersion(payload, artifact.Envelope.Version, *row.PreviousReport)
				}
				if err == nil {
					newHTML, err = render.InventoryMemoryCapacityRetestHTMLForVersion(payload, artifact.Envelope.Version, *row.PreviousReport)
				}
				if err == nil {
					newPDF, err = render.InventoryMemoryCapacityRetestPDFForVersion(payload, artifact.Envelope.Version, *row.PreviousReport)
				}
			}
			if err != nil || !bytes.Equal(oldHTML, html) || !bytes.Equal(oldPDF, pdf) {
				t.Fatal("pre-change renderer bytes changed")
			}
			if bytes.Equal(newHTML, html) || bytes.Equal(newPDF, pdf) ||
				!bytes.Contains(newHTML, []byte("Memory capacity observations")) ||
				!bytes.Contains(newPDF, []byte("MEMORY CAPACITY OBSERVATIONS")) {
				t.Fatal("current profile did not create distinct capacity views")
			}
			after, err := json.Marshal(payload)
			if err != nil || !bytes.Equal(after, inventoryJSON) {
				t.Fatal("rendering mutated original inventory")
			}
			var encoded bytes.Buffer
			var embedded []byte
			if self {
				embedded = public
			}
			switch {
			case row.PreviousReport != nil:
				err = WriteRetestWithPDF(&encoded, inventoryJSON, artifact.Envelope, html, pdf, *row.PreviousReport, embedded)
			case self:
				err = WriteSelfIssuedWithPDF(&encoded, inventoryJSON, artifact.Envelope, html, pdf, public)
			default:
				err = WriteWithPDF(&encoded, inventoryJSON, artifact.Envelope, html, pdf)
			}
			if err != nil || !bytes.Equal(encoded.Bytes(), raw) {
				t.Fatal("legacy producer archive bytes changed")
			}
		})
	}
}
