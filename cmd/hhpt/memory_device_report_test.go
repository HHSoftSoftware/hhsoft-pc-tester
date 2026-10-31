//go:build linux || windows

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/comparison"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

const memoryCommandRaw = "sys/firmware/dmi/entries/17-42/raw"
const memoryCommandPart = "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af"
const memoryCommandSystem = "hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449"
const memoryCommandKeyID = "hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44"

type memoryCommandFixture struct {
	root, directory, keyPath, planPath string
	snapshot                           inventory.Snapshot
	frozen                             plan.Plan
}

func memoryCommandRecord(high byte) []byte {
	raw := []byte{17, 21, 0x71, 0x29, 0, 0, 0, 0, 0, 0, 0, 0, 0, high, 9, 0, 0, 0, 34, 0, 0}
	return append(raw, []byte("PRIVATE-SERIAL\x00PRIVATE-LOCATOR\x00\x00")...)
}
func newMemoryCommandFixture(t *testing.T) memoryCommandFixture {
	t.Helper()
	f := memoryCommandFixture{root: t.TempDir(), directory: t.TempDir()}
	files := map[string]string{
		"sys/class/block/sda/wwid": "fixture-exterior-disk\n", "sys/class/block/sda/device/model": "Fixture disk\n",
		memoryCommandRaw:                        string(memoryCommandRecord(0x40)),
		"sys/firmware/dmi/entries/17-42/handle": "FORBIDDEN-HANDLE", "sys/firmware/dmi/tables/DMI": "FORBIDDEN-TABLE",
	}
	for name, value := range files {
		p := filepath.Join(f.root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	f.snapshot, err = inventory.NewLinuxCollector(f.root).Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := app.BuildInventoryPayload(f.snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	requireMemoryCommandPayload(t, payload, "17179869184")
	excluded, err := plan.SelectableQuickTests("x86_64")
	if err != nil {
		t.Fatal(err)
	}
	f.frozen, err = plan.BuildWithSelection(f.snapshot, plan.ProfileQuick, "x86_64", bytes.Repeat([]byte{0x39}, 32), "power-fixture", excluded)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(f.frozen)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range f.frozen.Tests {
		if row.Status == plan.TestScheduled {
			t.Fatal("fixture scheduled an active worker")
		}
	}
	f.keyPath = writeTestKey(t, f.directory, "fingerprint.key", bytes.Repeat([]byte{0x39}, 32))
	f.planPath = filepath.Join(f.directory, "plan.json")
	if err := os.WriteFile(f.planPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	assertMemoryCommandPrivacy(t, encoded)
	return f
}
func (f memoryCommandFixture) arguments(t *testing.T, output string) []string {
	t.Helper()
	args := []string{"-fingerprint-key", f.keyPath, "-key-epoch", "power-fixture", "-plan", f.planPath, "-evidence-dir", f.directory, "-session-id", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "-output", output}
	planned := make(map[string]string)
	for _, row := range f.frozen.Tests {
		planned[row.ID] = string(row.Status)
	}
	for _, worker := range currentResultDefinitionsForTest(t, f.frozen) {
		args = append(args, "-outcome", worker.id+"="+planned[worker.id])
	}
	return args
}
func memoryCommandAttributes(capacity string) []inventory.Attribute {
	return []inventory.Attribute{{Name: "capacity_bytes", Value: capacity}, {Name: "capacity_bytes_state", Value: "reported"}, {Name: "form_factor", Value: "dimm"}, {Name: "form_factor_state", Value: "reported"}, {Name: "memory_type", Value: "ddr5"}, {Name: "memory_type_state", Value: "reported"}, {Name: "observation_source", Value: "smbios_type17"}}
}
func requireMemoryCommandPayload(t *testing.T, p app.InventoryPayload, capacity string) {
	t.Helper()
	fingerprint := app.SystemFingerprint{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", KeyID: memoryCommandKeyID, Value: memoryCommandSystem, IdentifiedParts: 1, UnidentifiedParts: 1}
	rows := []app.ComponentRecord{
		{Type: inventory.ComponentBlock, ID: "block:sda", Attributes: []inventory.Attribute{{Name: "kind", Value: "device"}, {Name: "model", Value: "Fixture disk"}, {Name: "name", Value: "sda"}}, Identity: app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Source: "wwid", Value: memoryCommandPart, Confidence: "durable"}},
		{Type: inventory.ComponentType("memory_device"), ID: "memory_device:1", Attributes: memoryCommandAttributes(capacity), Identity: app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}},
	}
	if p.SchemaVersion != 1 || p.SystemFingerprint != fingerprint || !reflect.DeepEqual(p.Components, rows) {
		t.Fatal("literal finite fields and unavailable identity differ", p.Components)
	}
}
func assertMemoryCommandPrivacy(t *testing.T, raw []byte) {
	t.Helper()
	for _, marker := range []string{"PRIVATE", "FORBIDDEN", "fixture-exterior-disk", "17-42"} {
		if bytes.Contains(raw, []byte(marker)) {
			t.Fatal("private command input escaped")
		}
	}
}
func memoryCommandPositive(t *testing.T, f memoryCommandFixture) {
	t.Helper()
	output := filepath.Join(t.TempDir(), "positive.hhpcr")
	var out, diagnostic bytes.Buffer
	calls := 0
	collect := func(ctx context.Context) (inventory.Snapshot, error) {
		calls++
		return inventory.NewLinuxCollector(f.root).Collect(ctx)
	}
	if code := runFinalizeWith(t.Context(), f.arguments(t, output), &out, &diagnostic, collect, bytes.NewReader(bytes.Repeat([]byte{0x45}, 64)), func() time.Time { return time.Unix(10, 0) }); code != 0 || calls != 1 {
		t.Fatal("positive finalizer prerequisite", code, diagnostic.String())
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var payload app.InventoryPayload
	if json.Unmarshal(artifact.InventoryJSON, &payload) != nil {
		t.Fatal("positive payload")
	}
	requireMemoryCommandPayload(t, payload, "17179869184")
	found := false
	for _, row := range payload.TestResults {
		if row.ID == "run.integrity.quick" {
			found = row.Status == app.TestPass
		}
	}
	if !found || !bytes.Contains(out.Bytes(), []byte("HHPT_RUN_INTEGRITY status=pass")) {
		t.Fatal("unchanged configuration did not pass")
	}
}
func TestMemoryDeviceUnchangedFinalizerHasPassingIntegrity(t *testing.T) {
	memoryCommandPositive(t, newMemoryCommandFixture(t))
}
func TestMemoryDeviceRealFinalizerKeepsConfigurationDeltaAndIndependentParentTrust(t *testing.T) {
	for _, selfParent := range []bool{false, true} {
		t.Run(fmt.Sprintf("self_parent=%t", selfParent), func(t *testing.T) {
			f := newMemoryCommandFixture(t)
			private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x67}, 32))
			public := private.Public().(ed25519.PublicKey)
			var parentRaw []byte
			var err error
			if selfParent {
				parentRaw, err = captureSelfIssuedBundle(f.snapshot, nil, bytes.Repeat([]byte{0x39}, 32), "power-fixture", private, public, "power-parent", time.Unix(1, 0), app.CoverageDirectCapture)
			} else {
				parentRaw, err = captureBundle(f.snapshot, nil, bytes.Repeat([]byte{0x39}, 32), "power-fixture", private, "power-parent-key", "power-parent", time.Unix(1, 0), app.CoverageDirectCapture)
			}
			if err != nil {
				t.Fatal("actual parent producer", err)
			}
			var parent bundle.Artifact
			if selfParent {
				parent, err = bundle.VerifySelfIssued(bytes.NewReader(parentRaw), int64(len(parentRaw)))
			} else {
				parent, err = bundle.Verify(bytes.NewReader(parentRaw), int64(len(parentRaw)), report.Keyring{"power-parent-key": public})
			}
			if err != nil {
				t.Fatal("valid independently verified parent prerequisite", err)
			}
			var parentPayload app.InventoryPayload
			if err := json.Unmarshal(parent.InventoryJSON, &parentPayload); err != nil {
				t.Fatal(err)
			}
			requireMemoryCommandPayload(t, parentPayload, "17179869184")
			parentPath := filepath.Join(f.directory, "parent.hhpcr")
			if err := os.WriteFile(parentPath, parentRaw, 0600); err != nil {
				t.Fatal(err)
			}
			parentKey := writeTestKey(t, f.directory, "parent.pub", public)

			memoryCommandPositive(t, f)
			if err := os.WriteFile(filepath.Join(f.root, filepath.FromSlash(memoryCommandRaw)), memoryCommandRecord(0x20), 0600); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(f.directory, "child.hhpcr")
			args := append(f.arguments(t, output), "-previous-report", parentPath)
			if selfParent {
				args = append(args, "-previous-self-issued")
			} else {
				args = append(args, "-previous-public-key", parentKey, "-previous-key-id", "power-parent-key")
			}
			calls := 0
			collect := func(ctx context.Context) (inventory.Snapshot, error) {
				calls++
				return inventory.NewLinuxCollector(f.root).Collect(ctx)
			}
			var stdout, stderr bytes.Buffer
			if code := runFinalizeWith(t.Context(), args, &stdout, &stderr, collect, bytes.NewReader(bytes.Repeat([]byte{0x45}, 64)), func() time.Time { return time.Unix(10, 0) }); code != 0 || calls != 1 {
				t.Fatal("real finalizer rejected an observed configuration delta", code, calls, stderr.String())
			}
			raw, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			child, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
			if err != nil {
				t.Fatal(err)
			}
			wantParent := report.PreviousReport{ManifestDigest: parent.Envelope.ManifestDigest, BundleSHA256: fmt.Sprintf("sha256:%x", parent.BundleSHA256)}
			if child.Manifest.PreviousReport == nil || *child.Manifest.PreviousReport != wantParent || child.Envelope.SessionID != "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" || len(child.RenderedReport) == 0 || len(child.RenderedPDF) == 0 {
				t.Fatal("finalizer lost current profile or exact authenticated parent")
			}
			var payload app.InventoryPayload
			if err := json.Unmarshal(child.InventoryJSON, &payload); err != nil {
				t.Fatal(err)
			}
			requireMemoryCommandPayload(t, payload, "8589934592")
			integrity := false
			for _, row := range payload.TestResults {
				if strings.Contains(row.ID, "memory_device") {
					t.Fatal("inventory created a functional test")
				}
				if row.ID == "run.integrity.quick" {
					var evidence sessionevidence.IntegrityEvidence
					if err := json.Unmarshal(row.Evidence, &evidence); err != nil {
						t.Fatal(err)
					}
					wantChanged := []plan.HardwareComponentRef{{Type: inventory.ComponentType("memory_device"), ID: "memory_device:1"}}
					integrity = row.Status == app.TestFail && evidence.Status == "fail" && row.PlanID == f.frozen.ID && row.HardwareGraph == f.frozen.HardwareGraph.Value && evidence.PlannedHardwareGraph == "hmac-sha256:f179c2ad6fb61abc70288f1c70166dcc59c689627028dbe9cdd06e5151a94a02" && evidence.FinalHardwareGraph == "hmac-sha256:df39af0f073e62affc61f7c1885dc48dc31a2b1502ba1d04daffb8fd06aad6a1" && evidence.PlannedComponentCount == 2 && evidence.FinalComponentCount == 2 && len(evidence.AddedComponents) == 0 && len(evidence.RemovedComponents) == 0 && reflect.DeepEqual(evidence.ChangedComponents, wantChanged) && evidence.Session == nil
				}
			}
			if !integrity || payload.CoverageScope != app.CoverageFrozenPlan {
				t.Fatal("configuration delta failed to remain signed failed integrity")
			}
			for _, value := range [][]byte{child.InventoryJSON, child.RenderedReport, child.RenderedPDF, stdout.Bytes(), stderr.Bytes()} {
				assertMemoryCommandPrivacy(t, value)
			}
			verifyArgs := []string{"-self-issued", "-previous-report", parentPath}
			if selfParent {
				verifyArgs = append(verifyArgs, "-previous-self-issued")
			} else {
				verifyArgs = append(verifyArgs, "-previous-public-key", parentKey, "-previous-key-id", "power-parent-key")
			}
			verifyArgs = append(verifyArgs, output)
			stdout.Reset()
			stderr.Reset()
			if code := runVerify(t.Context(), verifyArgs, &stdout, &stderr); code != 0 {
				t.Fatal("actual pair verification rejected current memory-device report", code, stderr.String())
			}
			badArgs := []string{"-self-issued", "-previous-report", parentPath}
			if !selfParent {
				wrong := writeTestKey(t, f.directory, "wrong-parent.pub", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x68}, 32)).Public().(ed25519.PublicKey))
				badArgs = append(badArgs, "-previous-public-key", wrong, "-previous-key-id", "power-parent-key")
			}
			badArgs = append(badArgs, output)
			stdout.Reset()
			stderr.Reset()
			if code := runVerify(t.Context(), badArgs, &stdout, &stderr); code == 0 || stdout.Len() != 0 {
				t.Fatal("child trust substituted for independent parent trust")
			}
			after, err := os.ReadFile(parentPath)
			if err != nil || !bytes.Equal(after, parentRaw) {
				t.Fatal("parent was modified", err)
			}
		})
	}
}

func TestMemoryDeviceRealCompareUsesSignedReferencesAndRejectsForgedIdentity(t *testing.T) {
	f := newMemoryCommandFixture(t)
	before, err := app.BuildInventoryPayload(f.snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, filepath.FromSlash(memoryCommandRaw)), memoryCommandRecord(0x20), 0600); err != nil {
		t.Fatal(err)
	}
	current, err := inventory.NewLinuxCollector(f.root).Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	after, err := app.BuildInventoryPayload(current, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	requireMemoryCommandPayload(t, before, "17179869184")
	requireMemoryCommandPayload(t, after, "8589934592")
	left, l, public := powerCommandSignedPayload(t, before, "power-before", 1)
	right, r, _ := powerCommandSignedPayload(t, after, "power-after", 2)
	leftPath, rightPath := filepath.Join(f.directory, "before.hhpcr"), filepath.Join(f.directory, "after.hhpcr")
	if err := os.WriteFile(leftPath, left, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rightPath, right, 0600); err != nil {
		t.Fatal(err)
	}
	keyPath := writeTestKey(t, f.directory, "compare.pub", public)
	args := []string{"-public-key", keyPath, "-key-id", "power-compare-key", leftPath, rightPath}
	ref := func(a bundle.Artifact, p app.InventoryPayload) comparison.ReportReference {
		return comparison.ReportReference{SessionID: a.Envelope.SessionID, GeneratedTime: a.Envelope.GeneratedTime, SoftwareVersion: product.Version, SignerKeyID: "power-compare-key", ManifestDigest: a.Envelope.ManifestDigest, SystemFingerprint: p.SystemFingerprint, ComponentCount: 2}
	}
	control := comparison.Component{Type: inventory.ComponentBlock, ID: "block:sda", IdentitySource: "wwid", Identity: memoryCommandPart, IdentityConfidence: "durable", Attributes: []inventory.Attribute{{Name: "kind", Value: "device"}, {Name: "model", Value: "Fixture disk"}, {Name: "name", Value: "sda"}}}
	oldMemory := comparison.Component{Type: inventory.ComponentType("memory_device"), ID: "memory_device:1", IdentityConfidence: "unavailable", Attributes: memoryCommandAttributes("17179869184")}
	newMemory := comparison.Component{Type: inventory.ComponentType("memory_device"), ID: "memory_device:1", IdentityConfidence: "unavailable", Attributes: memoryCommandAttributes("8589934592")}
	want := comparison.Result{Schema: "hhsoft.pc-tester.hardware-comparison/v1", Policy: "hhsoft.parts-swap.v1", Status: comparison.StatusInconclusive, Comparable: true, Complete: false, Before: ref(l, before), After: ref(r, after), Summary: comparison.Summary{Matched: 1, Unresolved: 1}, Matched: []comparison.Match{{Before: control, After: control}}, Unresolved: []comparison.UnresolvedChange{{Before: []comparison.Component{oldMemory}, After: []comparison.Component{newMemory}, Reason: "stable identity is unavailable at the same observed location; continuity or replacement cannot be proven"}}}
	positive := func(t *testing.T) {
		t.Helper()
		var out, diagnostic bytes.Buffer
		if code := runCompare(t.Context(), args, &out, &diagnostic); code != 0 {
			t.Fatal("real verified comparison prerequisite", code, diagnostic.String())
		}
		var got comparison.Result
		if json.Unmarshal(out.Bytes(), &got) != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("actual signed comparison lost exact rows/references", got)
		}
		assertMemoryCommandPrivacy(t, out.Bytes())
	}
	positive(t)
	for _, side := range []string{"before", "after"} {
		for _, componentIndex := range []int{1} {
			t.Run(fmt.Sprintf("%s/%d", side, componentIndex), func(t *testing.T) {
				positive(t)
				original := after
				path := rightPath
				session := "power-after"
				second := int64(2)
				restore := right
				if side == "before" {
					original = before
					path = leftPath
					session = "power-before"
					second = 1
					restore = left
				}
				raw, _ := json.Marshal(original)
				var forged app.InventoryPayload
				if err := json.Unmarshal(raw, &forged); err != nil {
					t.Fatal(err)
				}
				forged.Components[componentIndex].Identity.Source = "serial"
				forged.Components[componentIndex].Identity.Value = "hmac-sha256:" + strings.Repeat("f", 64)
				forged.Components[componentIndex].Identity.Confidence = "durable"
				forged.SystemFingerprint.IdentifiedParts = 2
				forged.SystemFingerprint.UnidentifiedParts = 0
				bad, _, _ := powerCommandSignedPayload(t, forged, session, second)
				if bytes.Equal(bad, restore) {
					t.Fatal("identity mutation ineffective")
				}
				if err := os.WriteFile(path, bad, 0600); err != nil {
					t.Fatal(err)
				}
				var out, diagnostic bytes.Buffer
				if code := runCompare(t.Context(), args, &out, &diagnostic); code != 1 || out.Len() != 0 || diagnostic.String() != "compare reports failed\n" {
					t.Fatal("correctly signed forged identity reached comparison output", code, diagnostic.String())
				}
				if err := os.WriteFile(path, restore, 0600); err != nil {
					t.Fatal(err)
				}
				positive(t)
			})
		}
	}
}
func TestMemoryDeviceOwnedCollectionFailureStopsFinalizerSigningAndOutput(t *testing.T) {
	for _, fault := range []string{"raw_limit", "nonregular_scalar"} {
		t.Run(fault, func(t *testing.T) {
			f := newMemoryCommandFixture(t)
			memoryCommandPositive(t, f)
			field := filepath.Join(f.root, filepath.FromSlash(memoryCommandRaw))
			if fault == "raw_limit" {
				if err := os.WriteFile(field, bytes.Repeat([]byte{'9'}, 65537), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(field); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(field, 0700); err != nil {
					t.Fatal(err)
				}
			}
			calls, outputs := 0, 0
			collect := func(ctx context.Context) (inventory.Snapshot, error) {
				calls++
				return inventory.NewLinuxCollector(f.root).Collect(ctx)
			}
			write := func(path string, stdout io.Writer, body func(io.Writer) error) error {
				outputs++
				return writeDestination(path, stdout, body)
			}
			random := bytes.NewReader(bytes.Repeat([]byte{0x72}, 64))
			output := filepath.Join(f.directory, "must-not-exist.hhpcr")
			var stdout, stderr bytes.Buffer
			result := runFinalizeOwnedDestination(t.Context(), f.arguments(t, output), &stdout, &stderr, collect, random, func() time.Time { return time.Unix(3, 0) }, nil, write)
			want := "collect final inventory: inventory: scalar exceeds acquisition budget\n"
			if fault == "nonregular_scalar" {
				want = "collect final inventory: inventory: scalar acquisition failed\n"
			}
			if result.Code != 1 || calls != 1 || outputs != 0 || random.Len() != 64 || stdout.Len() != 0 || stderr.String() != want {
				t.Fatal("new scalar failure did not stop real finalization", result.Code, calls, outputs, random.Len(), stderr.String())
			}
			assertMemoryCommandPrivacy(t, stderr.Bytes())
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("failed inventory created report", err)
			}
		})
	}
}
