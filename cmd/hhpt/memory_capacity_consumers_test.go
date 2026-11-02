//go:build linux || windows

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func memoryCapacityCommandSnapshot() inventory.Snapshot {
	return inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentSystem, "system", map[string]string{"vendor": "Observed Vendor", "product_name": "Observed Model", "product_serial": "synthetic-capacity-fixture"}),
		inventory.NewComponent(inventory.ComponentMemory, "memory:system", map[string]string{"total_kib": "15728640"}),
		inventory.NewComponent(inventory.ComponentMemoryDevice, "memory-device:fixture-a", map[string]string{"observation_source": "smbios_type17", "capacity_bytes_state": "reported", "capacity_bytes": "8589934592", "memory_type_state": "reported", "memory_type": "ddr4"}),
		inventory.NewComponent(inventory.ComponentMemoryDevice, "memory-device:fixture-b", map[string]string{"observation_source": "smbios_type17", "capacity_bytes_state": "reported", "capacity_bytes": "8589934592", "memory_type_state": "reported", "memory_type": "ddr4"}),
	})
}

func memoryCapacityCommandProfile(t *testing.T, artifact bundle.Artifact, current, linked bool) {
	t.Helper()
	schema, profile := bundle.ManifestSchema, ""
	if linked {
		schema = bundle.RetestManifestSchema
	}
	if current {
		schema, profile = bundle.MemoryCapacityManifestSchema, bundle.MemoryCapacityViewProfile
		if linked {
			schema = bundle.MemoryCapacityRetestManifestSchema
		}
	}
	if artifact.Manifest.Schema != schema || artifact.Manifest.ViewProfile != profile {
		t.Fatalf("wrong authenticated fixture profile: %+v", artifact.Manifest)
	}
}

// Legacy cases use the existing explicit viewless constructors. Current cases
// have real current views and a full verification before any consumer sees them.
func memoryCapacityCommandReport(t *testing.T, current, self bool, id string, previous *report.PreviousReport, seed byte) commandRetestFixture {
	t.Helper()
	if !current {
		return commandRetestArtifact(t, id, self, previous, seed)
	}
	payload, err := app.BuildInventoryPayload(memoryCapacityCommandSnapshot(), bytes.Repeat([]byte{0x36}, 32), "capacity-command")
	if err != nil {
		t.Fatal(err)
	}
	encoded, html, pdf, err := renderSerializedInventoryWithPrevious(payload, previous)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
	public := key.Public().(ed25519.PublicKey)
	keyID := "capacity-command-key"
	var publicMember []byte
	if self {
		keyID, err = bundle.SelfIssuedKeyID(public)
		if err != nil {
			t.Fatal(err)
		}
		publicMember = public
	}
	digest, err := bundle.MemoryCapacityManifestDigestWithPDF(encoded, html, pdf, publicMember, previous)
	if err != nil {
		t.Fatal(err)
	}
	envelope := report.Envelope{Schema: report.EnvelopeSchemaV1, Product: product.Name, Version: product.Version, SessionID: id, GeneratedTime: "2026-11-01T00:00:00Z", Payload: encoded, ManifestDigest: digest, KeyID: keyID}
	if err := report.Sign(&envelope, key); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := bundle.WriteMemoryCapacityWithPDF(&output, encoded, envelope, html, pdf, publicMember, previous); err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(nil), output.Bytes()...)
	var artifact bundle.Artifact
	if self {
		artifact, err = bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	} else {
		artifact, err = bundle.Verify(bytes.NewReader(raw), int64(len(raw)), report.Keyring{keyID: public})
	}
	if err != nil {
		t.Fatalf("current full-verification prerequisite: %v", err)
	}
	memoryCapacityCommandProfile(t, artifact, true, previous != nil)
	if !bytes.Equal(artifact.InventoryJSON, encoded) || !bytes.Contains(artifact.RenderedReport, []byte("Memory capacity observations")) {
		t.Fatal("current source or views missing")
	}
	return commandRetestFixture{encoded: raw, artifact: artifact, public: public, keyID: keyID}
}

func TestMemoryCapacityCommandCaptureProfiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		self bool
	}{{"external", false}, {"self", true}} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := memoryCapacityCommandSnapshot()
			fingerprint := bytes.Repeat([]byte{0x36}, 32)
			key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x55}, ed25519.SeedSize))
			public := key.Public().(ed25519.PublicKey)
			var raw []byte
			var err error
			if tc.self {
				raw, err = captureSelfIssuedBundle(snapshot, nil, fingerprint, "capacity-command", key, public, "11111111-1111-4111-8111-111111111111", time.Unix(10, 20), "")
			} else {
				raw, err = captureBundle(snapshot, nil, fingerprint, "capacity-command", key, "capacity-command-key", "11111111-1111-4111-8111-111111111111", time.Unix(10, 20), "")
			}
			if err != nil {
				t.Fatal(err)
			}
			var artifact bundle.Artifact
			if tc.self {
				artifact, err = bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
			} else {
				artifact, err = bundle.Verify(bytes.NewReader(raw), int64(len(raw)), report.Keyring{"capacity-command-key": public})
			}
			if err != nil {
				t.Fatal(err)
			}
			memoryCapacityCommandProfile(t, artifact, true, false)
			expected, err := app.BuildInventoryPayload(snapshot, fingerprint, "capacity-command")
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(expected)
			if err != nil || !bytes.Equal(encoded, artifact.InventoryJSON) {
				t.Fatal("capture changed observed inventory or identities")
			}
			var decoded app.InventoryPayload
			if json.Unmarshal(artifact.InventoryJSON, &decoded) != nil || len(decoded.TestResults) != 0 {
				t.Fatal("capacity difference invented a health outcome")
			}
			for _, view := range [][]byte{artifact.RenderedReport, artifact.RenderedPDF} {
				if !bytes.Contains(view, []byte("17179869184 bytes")) || !bytes.Contains(view, []byte("lower than")) {
					t.Fatal("capacity observation difference missing from current capture")
				}
			}
		})
	}
}

func TestMemoryCapacityCommandLinkedProfileCombinations(t *testing.T) {
	for _, tc := range []struct {
		name                                               string
		parentCurrent, childCurrent, parentSelf, childSelf bool
	}{
		{"legacy_legacy_external_external", false, false, false, false}, {"legacy_current_external_external", false, true, false, false}, {"current_legacy_external_external", true, false, false, false}, {"current_current_external_external", true, true, false, false},
		{"legacy_legacy_external_self", false, false, false, true}, {"legacy_current_external_self", false, true, false, true}, {"current_legacy_external_self", true, false, false, true}, {"current_current_external_self", true, true, false, true},
		{"legacy_legacy_self_external", false, false, true, false}, {"legacy_current_self_external", false, true, true, false}, {"current_legacy_self_external", true, false, true, false}, {"current_current_self_external", true, true, true, false},
		{"legacy_legacy_self_self", false, false, true, true}, {"legacy_current_self_self", false, true, true, true}, {"current_legacy_self_self", true, false, true, true}, {"current_current_self_self", true, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := memoryCapacityCommandReport(t, tc.parentCurrent, tc.parentSelf, "11111111-1111-4111-8111-111111111111", nil, 0x71)
			reference := commandRetestReference(parent)
			child := memoryCapacityCommandReport(t, tc.childCurrent, tc.childSelf, "22222222-2222-4222-8222-222222222222", &reference, 0x72)
			memoryCapacityCommandProfile(t, parent.artifact, tc.parentCurrent, false)
			memoryCapacityCommandProfile(t, child.artifact, tc.childCurrent, true)
			if child.artifact.Manifest.PreviousReport == nil || *child.artifact.Manifest.PreviousReport != reference {
				t.Fatal("child lost exact parent hashes")
			}
			dir := t.TempDir()
			parentPath, parentKey := writeCommandRetest(t, dir, "parent", parent)
			childPath, childKey := writeCommandRetest(t, dir, "child", child)
			args := commandRetestTrust("", childKey, child.keyID, tc.childSelf)
			args = append(args, commandRetestTrust("previous-", parentKey, parent.keyID, tc.parentSelf)...)
			args = append(args, "-previous-report", parentPath, childPath)
			var output, diagnostic bytes.Buffer
			if code := runVerify(t.Context(), args, &output, &diagnostic); code != 0 || diagnostic.Len() != 0 || !strings.Contains(output.String(), "exact supplied parent matched") {
				t.Fatalf("real profile-pair consumer: %d %s", code, &diagnostic)
			}
			other := memoryCapacityCommandReport(t, tc.parentCurrent, tc.parentSelf, "33333333-3333-4333-8333-333333333333", nil, 0x71)
			if other.artifact.Envelope.ManifestDigest != parent.artifact.Envelope.ManifestDigest || other.artifact.BundleSHA256 == parent.artifact.BundleSHA256 {
				t.Fatal("same-manifest/different-artifact negative prerequisite")
			}
			otherPath, _ := writeCommandRetest(t, dir, "different-parent", other)
			bad := correctionSet(args, "-previous-report", otherPath)
			output.Reset()
			diagnostic.Reset()
			if code := runVerify(t.Context(), bad, &output, &diagnostic); code == 0 || output.Len() != 0 {
				t.Fatal("exact signed parent replaced by same-manifest artifact")
			}
			after, err := os.ReadFile(parentPath)
			if err != nil || !bytes.Equal(after, parent.encoded) {
				t.Fatal("parent source modified")
			}
		})
	}
}

func TestMemoryCapacityCommandCurrentParentFinalization(t *testing.T) {
	for _, tc := range []struct {
		name   string
		linked bool
	}{{"parent_v3", false}, {"parent_v4", true}} {
		t.Run(tc.name, func(t *testing.T) {
			args, snapshot, outputPath := retestFinalizeFixture(t)
			var previous *report.PreviousReport
			if tc.linked {
				grandparent := commandRetestArtifact(t, "33333333-3333-4333-8333-333333333333", true, nil, 0x70)
				value := commandRetestReference(grandparent)
				previous = &value
			}
			parent := memoryCapacityCommandReport(t, true, true, "11111111-1111-4111-8111-111111111111", previous, 0x71)
			parentPath, _ := writeCommandRetest(t, t.TempDir(), "parent", parent)
			reference := commandRetestReference(parent)
			args = append(args, "-previous-report", parentPath, "-previous-self-issued")
			var output, diagnostic bytes.Buffer
			code := runFinalizeWith(t.Context(), args, &output, &diagnostic, func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x31}, 128)), func() time.Time { return time.Unix(10, 20) })
			if code != 0 {
				t.Fatalf("real current-parent finalizer: %d %s", code, &diagnostic)
			}
			raw, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
			if err != nil {
				t.Fatal(err)
			}
			memoryCapacityCommandProfile(t, artifact, true, true)
			if artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != reference || bytes.Contains(artifact.InventoryJSON, []byte("memory-device:fixture-a")) || artifact.Envelope.SessionID == parent.artifact.Envelope.SessionID {
				t.Fatal("current parent replaced current observations or identity")
			}
			output.Reset()
			diagnostic.Reset()
			if code := runVerify(t.Context(), []string{"-self-issued", "-previous-self-issued", "-previous-report", parentPath, outputPath}, &output, &diagnostic); code != 0 {
				t.Fatalf("current parent/attempt pair: %d %s", code, &diagnostic)
			}
			after, err := os.ReadFile(parentPath)
			if err != nil || !bytes.Equal(after, parent.encoded) {
				t.Fatal("finalizer mutated prior report")
			}
		})
	}
}

func TestMemoryCapacityCommandCorrectionProfiles(t *testing.T) {
	for _, tc := range []struct {
		name         string
		linked, self bool
	}{{"ordinary_external", false, false}, {"ordinary_self", false, true}, {"linked_external", true, false}, {"linked_self", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			var previous *report.PreviousReport
			if tc.linked {
				parent := commandRetestArtifact(t, "11111111-1111-4111-8111-111111111111", true, nil, 0x71)
				value := commandRetestReference(parent)
				previous = &value
			}
			current := memoryCapacityCommandReport(t, true, tc.self, "22222222-2222-4222-8222-222222222222", previous, 0x72)
			inputs := correctionCommandInputs(t)
			inputs["source"], inputs["source-key"] = current.encoded, current.public
			if bytes.Equal(inputs["source-key"], inputs["addendum-key"]) {
				t.Fatal("independent source/addendum key prerequisite")
			}
			trust := func(args []string) []string {
				if tc.self {
					args = correctionDrop(args, "-source-public-key")
					args = correctionDrop(args, "-source-key-id")
					return append(args, "-source-self-issued")
				}
				return correctionSet(args, "-source-key-id", current.keyID)
			}
			create := trust(correctionCreateArguments())
			var output, diagnostic bytes.Buffer
			reader := correctionCommandReader(inputs)
			if code := runCorrectionWith(t.Context(), create, &output, &diagnostic, reader, writeDestination, bytes.NewReader(bytes.Repeat([]byte{0x43}, 32))); code != 0 || diagnostic.Len() != 0 {
				t.Fatalf("current-source CLI create: %d %s", code, &diagnostic)
			}
			inputs["addendum"] = append([]byte(nil), output.Bytes()...)
			verify := trust(correctionVerifyArguments())
			output.Reset()
			diagnostic.Reset()
			if code := runCorrectionWith(t.Context(), verify, &output, &diagnostic, reader, writeDestination, bytes.NewReader(nil)); code != 0 || !strings.Contains(output.String(), "Correction verified against the exact original report.") {
				t.Fatalf("current-source CLI verify: %d %s", code, &diagnostic)
			}
			view := append([]string(nil), verify...)
			view[0] = "view"
			view = append(view, "-output", "-")
			output.Reset()
			diagnostic.Reset()
			if code := runCorrectionWith(t.Context(), view, &output, &diagnostic, reader, writeDestination, bytes.NewReader(nil)); code != 0 || !bytes.Contains(output.Bytes(), []byte("Observed Vendor")) || !bytes.Contains(output.Bytes(), []byte("Operator Vendor")) {
				t.Fatalf("current-source CLI display: %d %s", code, &diagnostic)
			}
			bad := correctionSet(verify, "-addendum-public-key", "source-key")
			output.Reset()
			diagnostic.Reset()
			if code := runCorrectionWith(t.Context(), bad, &output, &diagnostic, reader, writeDestination, bytes.NewReader(nil)); code == 0 || output.Len() != 0 {
				t.Fatal("source signer silently became addendum authority")
			}
			if !bytes.Equal(inputs["source"], current.encoded) {
				t.Fatal("correction route changed original artifact")
			}
		})
	}
}
