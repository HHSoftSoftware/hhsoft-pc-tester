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
)

const powerCommandRawName = "hid-PRIVATE-AA-BB-CC-DD-EE-FF"
const powerCommandPart = "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af"
const powerCommandSystem = "hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449"
const powerCommandKeyID = "hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44"

type powerCommandFixture struct {
	root, directory, keyPath, planPath string
	snapshot                           inventory.Snapshot
	frozen                             plan.Plan
}

func newPowerCommandFixture(t *testing.T) powerCommandFixture {
	t.Helper()
	f := powerCommandFixture{root: t.TempDir(), directory: t.TempDir()}
	files := map[string]string{"sys/class/block/sda/wwid": "fixture-exterior-disk\n", "sys/class/block/sda/device/model": "Fixture disk\n"}
	for name, value := range map[string]string{"type": "Battery\n", "scope": "System\n", "present": "1\n", "online": "2\n", "status": "Charging\n", "capacity": "50\n", "cycle_count": "0\n", "energy_full": "11\n", "energy_full_design": "10\n", "charge_full": "-1\n", "charge_full_design": "0\n", "energy_now": "17\n", "charge_now": "-19\n", "serial_number": "FORBIDDEN-COMMAND-SERIAL", "manufacturer": "FORBIDDEN-COMMAND-MANUFACTURER", "uevent": "FORBIDDEN-COMMAND-UEVENT"} {
		files["sys/class/power_supply/"+powerCommandRawName+"/"+name] = value
	}
	for name, value := range files {
		path := filepath.Join(f.root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
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
	requirePowerCommandPayload(t, payload, "50", "2")
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
			t.Fatal("fixture accidentally scheduled an active worker")
		}
	}
	f.keyPath = writeTestKey(t, f.directory, "fingerprint.key", bytes.Repeat([]byte{0x39}, 32))
	f.planPath = filepath.Join(f.directory, "plan.json")
	if err := os.WriteFile(f.planPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	assertPowerCommandPrivacy(t, encoded)
	return f
}

func (f powerCommandFixture) arguments(t *testing.T, output string) []string {
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

func powerCommandAttributes(capacity, online string) []inventory.Attribute {
	return []inventory.Attribute{
		{Name: "capacity_percent", Value: capacity}, {Name: "capacity_percent_state", Value: "reported"},
		{Name: "charge_full_design_uah", Value: "0"}, {Name: "charge_full_design_uah_state", Value: "reported"},
		{Name: "charge_full_uah", Value: "-1"}, {Name: "charge_full_uah_state", Value: "reported"},
		{Name: "charge_now_uah", Value: "-19"}, {Name: "charge_now_uah_state", Value: "reported"},
		{Name: "cycle_count_state", Value: "unavailable"},
		{Name: "energy_full_design_uwh", Value: "10"}, {Name: "energy_full_design_uwh_state", Value: "reported"},
		{Name: "energy_full_uwh", Value: "11"}, {Name: "energy_full_uwh_state", Value: "reported"},
		{Name: "energy_now_uwh", Value: "17"}, {Name: "energy_now_uwh_state", Value: "reported"},
		{Name: "online", Value: online}, {Name: "online_state", Value: "reported"},
		{Name: "present", Value: "1"}, {Name: "present_state", Value: "reported"},
		{Name: "scope", Value: "System"}, {Name: "scope_state", Value: "reported"},
		{Name: "status", Value: "Charging"}, {Name: "status_state", Value: "reported"},
		{Name: "supply_type", Value: "Battery"}, {Name: "supply_type_state", Value: "reported"},
	}
}

func powerCommandNowAttributes(capacity, online, energy, energyState, charge, chargeState string) []inventory.Attribute {
	var result []inventory.Attribute
	// The base is the literal fixture oracle, not a producer descriptor table.
	for _, a := range powerCommandAttributes(capacity, online) {
		switch a.Name {
		case "energy_now_uwh":
			if energyState != "reported" {
				continue
			}
			a.Value = energy
		case "energy_now_uwh_state":
			a.Value = energyState
		case "charge_now_uah":
			if chargeState != "reported" {
				continue
			}
			a.Value = charge
		case "charge_now_uah_state":
			a.Value = chargeState
		}
		result = append(result, a)
	}
	return result
}

func requirePowerCommandPayload(t *testing.T, p app.InventoryPayload, capacity, online string) {
	t.Helper()
	requirePowerCommandNowPayload(t, p, capacity, online, "17", "reported", "-19", "reported")
}

func requirePowerCommandNowPayload(t *testing.T, p app.InventoryPayload, capacity, online, energy, energyState, charge, chargeState string) {
	t.Helper()
	wantFingerprint := app.SystemFingerprint{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", KeyID: powerCommandKeyID, Value: powerCommandSystem, IdentifiedParts: 1, UnidentifiedParts: 1}
	want := []app.ComponentRecord{
		{Type: inventory.ComponentBlock, ID: "block:sda", Attributes: []inventory.Attribute{{Name: "kind", Value: "device"}, {Name: "model", Value: "Fixture disk"}, {Name: "name", Value: "sda"}}, Identity: app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Source: "wwid", Value: powerCommandPart, Confidence: "durable"}},
		{Type: inventory.ComponentType("power_supply"), ID: "power_supply:1", Attributes: powerCommandNowAttributes(capacity, online, energy, energyState, charge, chargeState), Identity: app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}},
	}
	if p.SchemaVersion != 1 || p.SystemFingerprint != wantFingerprint || !reflect.DeepEqual(p.Components, want) {
		t.Fatal("actual collector/signed payload differs from literal identity and finite row oracle")
	}
}

func assertPowerCommandPrivacy(t *testing.T, raw []byte) {
	t.Helper()
	for _, secret := range []string{powerCommandRawName, "renamed-PRIVATE-SUPPLY-CANARY", "FORBIDDEN-COMMAND-SERIAL", "FORBIDDEN-COMMAND-MANUFACTURER", "FORBIDDEN-COMMAND-UEVENT", "fixture-exterior-disk"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("private source bytes escaped public command output")
		}
	}
}

func TestPowerSupplyRealFinalizerKeepsCurrentTelemetryAndIndependentParentTrust(t *testing.T) {
	for _, selfParent := range []bool{false, true} {
		t.Run(fmt.Sprintf("self_parent=%t", selfParent), func(t *testing.T) {
			f := newPowerCommandFixture(t)
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
			requirePowerCommandPayload(t, parentPayload, "50", "2")
			parentPath := filepath.Join(f.directory, "parent.hhpcr")
			if err := os.WriteFile(parentPath, parentRaw, 0600); err != nil {
				t.Fatal(err)
			}
			parentKey := writeTestKey(t, f.directory, "parent.pub", public)
			oldDir := filepath.Join(f.root, "sys", "class", "power_supply", powerCommandRawName)
			newDir := filepath.Join(f.root, "sys", "class", "power_supply", "renamed-PRIVATE-SUPPLY-CANARY")
			if err := os.Rename(oldDir, newDir); err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]string{"capacity": "0\n", "online": "0\n", "energy_now": "0\n", "charge_now": "-2147483648\n"} {
				if err := os.WriteFile(filepath.Join(newDir, name), []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
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
				t.Fatal("real finalizer rejected a finite telemetry-only update", code, calls, stderr.String())
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
			requirePowerCommandNowPayload(t, payload, "0", "0", "0", "reported", "-2147483648", "reported")
			integrity := false
			for _, row := range payload.TestResults {
				if strings.Contains(row.ID, "power") || strings.Contains(row.ID, "battery") {
					t.Fatal("inventory created a functional test")
				}
				if row.ID == "run.integrity.quick" {
					integrity = row.Status == app.TestPass && row.PlanID == f.frozen.ID && row.HardwareGraph == f.frozen.HardwareGraph.Value
				}
			}
			if !integrity || payload.CoverageScope != app.CoverageFrozenPlan {
				t.Fatal("volatile samples or private rename changed stable finalization")
			}
			for _, value := range [][]byte{child.InventoryJSON, child.RenderedReport, child.RenderedPDF, stdout.Bytes(), stderr.Bytes()} {
				assertPowerCommandPrivacy(t, value)
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
				t.Fatal("actual pair verification rejected current power report", code, stderr.String())
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

func powerCommandSignedPayload(t *testing.T, payload app.InventoryPayload, session string, second int64) ([]byte, bundle.Artifact, ed25519.PublicKey) {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, 32))
	public := private.Public().(ed25519.PublicKey)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := bundle.ManifestDigestWithPDF(raw, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	envelope := report.Envelope{Schema: report.EnvelopeSchemaV1, Product: product.Name, Version: product.Version, SessionID: session, GeneratedTime: time.Unix(second, 0).UTC().Format(time.RFC3339Nano), Payload: raw, ManifestDigest: digest, KeyID: "power-compare-key"}
	if err := report.Sign(&envelope, private); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := bundle.WriteWithPDF(&archive, raw, envelope, nil, nil); err != nil {
		t.Fatal(err)
	}
	encoded := bytes.Clone(archive.Bytes())
	artifact, err := bundle.Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{"power-compare-key": public})
	if err != nil {
		t.Fatal("signature/canonical archive prerequisite", err)
	}
	if !bytes.Equal(artifact.InventoryJSON, raw) {
		t.Fatal("signed payload changed")
	}
	return encoded, artifact, public
}

func TestPowerSupplyRealCompareUsesSignedReferencesAndRejectsForgedIdentity(t *testing.T) {
	f := newPowerCommandFixture(t)
	before, err := app.BuildInventoryPayload(f.snapshot, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "sys", "class", "power_supply", powerCommandRawName, "capacity"), []byte("0\n"), 0600); err != nil {
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
	requirePowerCommandPayload(t, before, "50", "2")
	requirePowerCommandPayload(t, after, "0", "2")
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
	control := comparison.Component{Type: inventory.ComponentBlock, ID: "block:sda", IdentitySource: "wwid", Identity: powerCommandPart, IdentityConfidence: "durable", Attributes: []inventory.Attribute{{Name: "kind", Value: "device"}, {Name: "model", Value: "Fixture disk"}, {Name: "name", Value: "sda"}}}
	oldPower := comparison.Component{Type: inventory.ComponentType("power_supply"), ID: "power_supply:1", IdentityConfidence: "unavailable", Attributes: powerCommandAttributes("50", "2")}
	newPower := comparison.Component{Type: inventory.ComponentType("power_supply"), ID: "power_supply:1", IdentityConfidence: "unavailable", Attributes: powerCommandAttributes("0", "2")}
	want := comparison.Result{Schema: "hhsoft.pc-tester.hardware-comparison/v1", Policy: "hhsoft.parts-swap.v1", Status: comparison.StatusInconclusive, Comparable: true, Complete: false, Before: ref(l, before), After: ref(r, after), Summary: comparison.Summary{Matched: 1, Unresolved: 1}, Matched: []comparison.Match{{Before: control, After: control}}, Unresolved: []comparison.UnresolvedChange{{Before: []comparison.Component{oldPower}, After: []comparison.Component{newPower}, Reason: "stable identity is unavailable at the same observed location; continuity or replacement cannot be proven"}}}
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
		assertPowerCommandPrivacy(t, out.Bytes())
	}
	positive(t)
	for _, side := range []string{"before", "after"} {
		t.Run(side, func(t *testing.T) {
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
			forged.Components[1].Identity.Source = "serial"
			forged.Components[1].Identity.Value = "hmac-sha256:" + strings.Repeat("f", 64)
			forged.Components[1].Identity.Confidence = "durable"
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

func TestPowerSupplyOwnedCollectionFailureStopsFinalizerSigningAndOutput(t *testing.T) {
	for _, tc := range []struct{ name, input, fault string }{
		{"raw_limit", "charge_full_design", "raw_limit"},
		{"nonregular_scalar", "charge_full_design", "nonregular_scalar"},
		{"energy_now/raw_limit", "energy_now", "raw_limit"},
		{"energy_now/nonregular_scalar", "energy_now", "nonregular_scalar"},
		{"charge_now/raw_limit", "charge_now", "raw_limit"},
		{"charge_now/nonregular_scalar", "charge_now", "nonregular_scalar"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fault := tc.fault
			f := newPowerCommandFixture(t)
			field := filepath.Join(f.root, "sys", "class", "power_supply", powerCommandRawName, tc.input)
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
			assertPowerCommandPrivacy(t, stderr.Bytes())
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("failed inventory created report", err)
			}
		})
	}
}

func TestPowerSupplyNowCurrentOrdinaryCLIUsesAuthenticatedObservations(t *testing.T) {
	for _, tc := range []struct{ name, energy, charge, chargeValue, chargeState string }{
		{"reported", "0", "-2147483648", "-2147483648", "reported"},
		{"malformed_peer", "0", "bad", "", "malformed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPowerCommandFixture(t)
			private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x73}, 32))
			public := private.Public().(ed25519.PublicKey)
			produce := func(snapshot inventory.Snapshot, session string, second int64) ([]byte, bundle.Artifact, app.InventoryPayload) {
				t.Helper()
				raw, err := captureBundle(snapshot, nil, bytes.Repeat([]byte{0x39}, 32), "power-fixture", private, "power-now-key", session, time.Unix(second, 0), app.CoverageDirectCapture)
				if err != nil {
					t.Fatal("real current capture producer", err)
				}
				artifact, err := bundle.Verify(bytes.NewReader(raw), int64(len(raw)), report.Keyring{"power-now-key": public})
				if err != nil || artifact.Envelope.KeyID != "power-now-key" || artifact.Envelope.SessionID != session || artifact.Manifest.ViewProfile != "hhsoft.pc-tester.memory-capacity-view/v1" || artifact.Manifest.PreviousReport != nil || len(artifact.RenderedReport) == 0 || len(artifact.RenderedPDF) == 0 {
					t.Fatal("expected signer and current ordinary profile prerequisite", err)
				}
				var payload app.InventoryPayload
				if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
					t.Fatal(err)
				}
				assertPowerCommandPrivacy(t, artifact.InventoryJSON)
				assertPowerCommandPrivacy(t, artifact.RenderedReport)
				assertPowerCommandPrivacy(t, artifact.RenderedPDF)
				return raw, artifact, payload
			}
			left, l, before := produce(f.snapshot, "power-now-before", 1)
			requirePowerCommandPayload(t, before, "50", "2")
			for name, value := range map[string]string{"energy_now": tc.energy, "charge_now": tc.charge} {
				if err := os.WriteFile(filepath.Join(f.root, "sys", "class", "power_supply", powerCommandRawName, name), []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			current, err := inventory.NewLinuxCollector(f.root).Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			right, r, after := produce(current, "power-now-after", 2)
			requirePowerCommandNowPayload(t, after, "50", "2", "0", "reported", tc.chargeValue, tc.chargeState)
			if before.SystemFingerprint != after.SystemFingerprint || bytes.Equal(l.InventoryJSON, r.InventoryJSON) || l.Envelope.ManifestDigest == r.Envelope.ManifestDigest {
				t.Fatal("observation change lost authenticated bytes or altered identity")
			}
			leftPath, rightPath := filepath.Join(f.directory, "now-before.hhpcr"), filepath.Join(f.directory, "now-after.hhpcr")
			if err := os.WriteFile(leftPath, left, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(rightPath, right, 0600); err != nil {
				t.Fatal(err)
			}
			keyPath := writeTestKey(t, f.directory, "now.pub", public)
			wrong := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x74}, 32)).Public().(ed25519.PublicKey)
			wrongPath := writeTestKey(t, f.directory, "wrong-now.pub", wrong)
			for _, path := range []string{leftPath, rightPath} {
				var out, diagnostic bytes.Buffer
				if code := runVerify(t.Context(), []string{"-public-key", keyPath, "-key-id", "power-now-key", path}, &out, &diagnostic); code != 0 {
					t.Fatal("actual current CLI verification", code, diagnostic.String())
				}
				out.Reset()
				diagnostic.Reset()
				if code := runVerify(t.Context(), []string{"-public-key", wrongPath, "-key-id", "power-now-key", path}, &out, &diagnostic); code == 0 || out.Len() != 0 {
					t.Fatal("same-mode wrong signer admitted current report")
				}
			}
			ref := func(a bundle.Artifact, p app.InventoryPayload) comparison.ReportReference {
				return comparison.ReportReference{SessionID: a.Envelope.SessionID, GeneratedTime: a.Envelope.GeneratedTime, SoftwareVersion: product.Version, SignerKeyID: "power-now-key", ManifestDigest: a.Envelope.ManifestDigest, SystemFingerprint: p.SystemFingerprint, ComponentCount: 2}
			}
			control := comparison.Component{Type: inventory.ComponentBlock, ID: "block:sda", IdentitySource: "wwid", Identity: powerCommandPart, IdentityConfidence: "durable", Attributes: []inventory.Attribute{{Name: "kind", Value: "device"}, {Name: "model", Value: "Fixture disk"}, {Name: "name", Value: "sda"}}}
			oldPower := comparison.Component{Type: inventory.ComponentPowerSupply, ID: "power_supply:1", IdentityConfidence: "unavailable", Attributes: powerCommandAttributes("50", "2")}
			newPower := comparison.Component{Type: inventory.ComponentPowerSupply, ID: "power_supply:1", IdentityConfidence: "unavailable", Attributes: powerCommandNowAttributes("50", "2", "0", "reported", tc.chargeValue, tc.chargeState)}
			want := comparison.Result{Schema: "hhsoft.pc-tester.hardware-comparison/v1", Policy: "hhsoft.parts-swap.v1", Status: comparison.StatusInconclusive, Comparable: true, Complete: false, Before: ref(l, before), After: ref(r, after), Summary: comparison.Summary{Matched: 1, Unresolved: 1}, Matched: []comparison.Match{{Before: control, After: control}}, Unresolved: []comparison.UnresolvedChange{{Before: []comparison.Component{oldPower}, After: []comparison.Component{newPower}, Reason: "stable identity is unavailable at the same observed location; continuity or replacement cannot be proven"}}}
			var out, diagnostic bytes.Buffer
			if code := runCompare(t.Context(), []string{"-public-key", keyPath, "-key-id", "power-now-key", leftPath, rightPath}, &out, &diagnostic); code != 0 {
				t.Fatal("actual current CLI comparison", code, diagnostic.String())
			}
			var got comparison.Result
			if err := json.Unmarshal(out.Bytes(), &got); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("current comparison lost exact claims/references or invented continuity", err)
			}
			assertPowerCommandPrivacy(t, out.Bytes())
			for _, expected := range []struct {
				path  string
				bytes []byte
			}{{leftPath, left}, {rightPath, right}} {
				actual, err := os.ReadFile(expected.path)
				if err != nil || !bytes.Equal(actual, expected.bytes) {
					t.Fatal("readonly consumer mutated current signed input")
				}
			}
		})
	}
}
