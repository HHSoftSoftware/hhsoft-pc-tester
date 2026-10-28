//go:build linux || windows

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
)

const retestChildID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

type commandRetestFixture struct {
	encoded  []byte
	artifact bundle.Artifact
	public   ed25519.PublicKey
	keyID    string
}

func commandRetestArtifact(t *testing.T, id string, self bool, previous *report.PreviousReport, seed byte) commandRetestFixture {
	t.Helper()
	return commandRetestArtifactForProduct(t, id, self, previous, seed, product.Name)
}

func commandRetestArtifactForProduct(t *testing.T, id string, self bool, previous *report.PreviousReport, seed byte, productName string) commandRetestFixture {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	keyID := "fixture-provisioned"
	if self {
		var err error
		keyID, err = bundle.SelfIssuedKeyID(public)
		if err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte(`{"fixture":"parent-only-no-inherited-results"}`)
	var publicMember []byte
	if self {
		publicMember = public
	}
	var digest string
	var err error
	if previous != nil {
		digest, err = bundle.RetestManifestDigestWithPDF(payload, nil, nil, *previous, publicMember)
	} else if self {
		digest, err = bundle.SelfIssuedManifestDigest(payload, nil, public)
	} else {
		digest, err = bundle.ManifestDigest(payload, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	envelope := report.Envelope{Schema: report.EnvelopeSchemaV1, Product: productName, Version: product.Version, SessionID: id, GeneratedTime: "2026-10-28T00:00:00Z", Payload: payload, ManifestDigest: digest, KeyID: keyID}
	if err := report.Sign(&envelope, private); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if previous != nil {
		err = bundle.WriteRetestWithPDF(&output, payload, envelope, nil, nil, *previous, publicMember)
	} else if self {
		err = bundle.WriteSelfIssued(&output, payload, envelope, nil, public)
	} else {
		err = bundle.Write(&output, payload, envelope, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	encoded := append([]byte(nil), output.Bytes()...)
	var artifact bundle.Artifact
	if self {
		artifact, err = bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
	} else {
		artifact, err = bundle.Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{keyID: public})
	}
	if err != nil {
		t.Fatalf("valid command fixture precondition: %v", err)
	}
	return commandRetestFixture{encoded, artifact, public, keyID}
}

func commandRetestReference(value commandRetestFixture) report.PreviousReport {
	return report.PreviousReport{ManifestDigest: value.artifact.Envelope.ManifestDigest, BundleSHA256: fmt.Sprintf("sha256:%x", value.artifact.BundleSHA256)}
}

func writeCommandRetest(t *testing.T, directory, name string, value commandRetestFixture) (string, string) {
	t.Helper()
	path := filepath.Join(directory, name+".hhpcr")
	key := filepath.Join(directory, name+".pub")
	if err := os.WriteFile(path, value.encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, value.public, 0600); err != nil {
		t.Fatal(err)
	}
	return path, key
}

func commandRetestTrust(prefix, key, keyID string, self bool) []string {
	if self {
		return []string{"-" + prefix + "self-issued"}
	}
	return []string{"-" + prefix + "public-key", key, "-" + prefix + "key-id", keyID}
}

func TestRetestVerifyIndependentTrustAndOneHop(t *testing.T) {
	for _, childSelf := range []bool{false, true} {
		for _, parentSelf := range []bool{false, true} {
			t.Run(fmt.Sprintf("child-self=%t/parent-self=%t", childSelf, parentSelf), func(t *testing.T) {
				dir := t.TempDir()
				parent := commandRetestArtifact(t, "historical-parent", parentSelf, nil, 0x51)
				reference := commandRetestReference(parent)
				child := commandRetestArtifact(t, retestChildID, childSelf, &reference, 0x52)
				parentPath, parentKey := writeCommandRetest(t, dir, "parent", parent)
				childPath, childKey := writeCommandRetest(t, dir, "child", child)
				args := commandRetestTrust("", childKey, child.keyID, childSelf)
				args = append(args, "-previous-report", parentPath)
				args = append(args, commandRetestTrust("previous-", parentKey, parent.keyID, parentSelf)...)
				args = append(args, childPath)
				var out, errOut bytes.Buffer
				if code := runVerify(t.Context(), args, &out, &errOut); code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "exact supplied parent matched") {
					t.Fatalf("valid independently trusted pair failed: %d %s", code, &errOut)
				}
				if strings.Contains(out.String(), "does not prove operator identity") != childSelf {
					t.Fatal("child trust label inherited from parent")
				}
				for _, role := range []string{"child", "parent"} {
					bad := append([]string(nil), args...)
					// Use explicit wrong-key trust even when the fixture carries an embedded key.
					wrong := writeTestKey(t, dir, "wrong-"+role+".pub", bytes.Repeat([]byte{0x71}, 32))
					if role == "child" {
						bad = commandRetestTrust("", wrong, child.keyID, false)
						bad = append(bad, "-previous-report", parentPath)
						bad = append(bad, commandRetestTrust("previous-", parentKey, parent.keyID, parentSelf)...)
						bad = append(bad, childPath)
					} else {
						bad = commandRetestTrust("", childKey, child.keyID, childSelf)
						bad = append(bad, "-previous-report", parentPath)
						bad = append(bad, commandRetestTrust("previous-", wrong, parent.keyID, false)...)
						bad = append(bad, childPath)
					}
					out.Reset()
					errOut.Reset()
					if code := runVerify(t.Context(), bad, &out, &errOut); code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), role+" input or verification failed") {
						t.Fatal("one report's trust compensated for the other's invalid signature")
					}
				}
				// A child-only v2 still verifies its own signature, but makes no pair claim.
				out.Reset()
				errOut.Reset()
				if code := runVerify(t.Context(), append(commandRetestTrust("", childKey, child.keyID, childSelf), childPath), &out, &errOut); code != 0 || !strings.Contains(out.String(), "relation: unchecked") || strings.Contains(out.String(), "exact supplied parent matched") {
					t.Fatal("child-only reference became a verified history")
				}
			})
		}
	}
	// A linked parent is checked once; no grandparent is loaded or trusted.
	grand := commandRetestArtifact(t, "oldest", true, nil, 0x61)
	gr := commandRetestReference(grand)
	parent := commandRetestArtifact(t, "11111111-1111-4111-8111-111111111111", true, &gr, 0x62)
	pr := commandRetestReference(parent)
	child := commandRetestArtifact(t, retestChildID, true, &pr, 0x63)
	reads := []string{}
	read := func(_ context.Context, path string, kind retestInputKind) ([]byte, error) {
		reads = append(reads, path)
		if kind != retestArtifactInput {
			t.Fatal("unexpected key read")
		}
		switch path {
		case "child":
			return child.encoded, nil
		case "parent":
			return parent.encoded, nil
		}
		t.Fatal("recursive parent acquisition")
		return nil, errRetestInput
	}
	var out, errOut bytes.Buffer
	if code := runVerifyWithInput(t.Context(), []string{"-self-issued", "-previous-self-issued", "-previous-report", "parent", "child"}, &out, &errOut, read); code != 0 || !reflect.DeepEqual(reads, []string{"child", "parent"}) {
		t.Fatalf("one-hop verification failed: %d %s", code, &errOut)
	}
}

func TestRetestVerifyRejectsSameManifestDifferentSignedParent(t *testing.T) {
	parent := commandRetestArtifact(t, "original", false, nil, 0x41)
	for _, substitute := range []commandRetestFixture{commandRetestArtifact(t, "different-id", false, nil, 0x41), commandRetestArtifact(t, "original", false, nil, 0x42)} {
		if substitute.artifact.Envelope.ManifestDigest != parent.artifact.Envelope.ManifestDigest || substitute.artifact.BundleSHA256 == parent.artifact.BundleSHA256 {
			t.Fatal("same-manifest/different-envelope-or-key precondition failed")
		}
		reference := commandRetestReference(parent)
		child := commandRetestArtifact(t, retestChildID, true, &reference, 0x43)
		dir := t.TempDir()
		c, _ := writeCommandRetest(t, dir, "child", child)
		p, k := writeCommandRetest(t, dir, "parent", substitute)
		var out, errOut bytes.Buffer
		if code := runVerify(t.Context(), []string{"-self-issued", "-previous-report", p, "-previous-public-key", k, "-previous-key-id", substitute.keyID, c}, &out, &errOut); code != 1 || out.Len() != 0 || errOut.String() != "verify: exact predecessor relation mismatch\n" {
			t.Fatal("content-only parent substitution accepted or rejected for the wrong reason")
		}
	}
}

func TestRetestVerifyAllConsumedInputFailuresAreSanitized(t *testing.T) {
	parent := commandRetestArtifact(t, "parent", false, nil, 0x51)
	reference := commandRetestReference(parent)
	child := commandRetestArtifact(t, retestChildID, false, &reference, 0x52)
	values := map[string][]byte{"child": child.encoded, "child-key": child.public, "parent": parent.encoded, "parent-key": parent.public}
	order := []string{"child", "child-key", "parent", "parent-key"}
	args := []string{"-public-key", "child-key", "-key-id", child.keyID, "-previous-report", "parent", "-previous-public-key", "parent-key", "-previous-key-id", parent.keyID, "child"}
	for index, role := range order {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancel=%t", role, cancelled), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var reads []string
				read := func(_ context.Context, path string, kind retestInputKind) ([]byte, error) {
					reads = append(reads, path)
					if strings.HasSuffix(path, "key") != (kind == retestPublicKeyInput) {
						t.Fatal("wrong input bounds role")
					}
					if path == role {
						if cancelled {
							cancel()
						}
						return nil, errors.Join(errRetestInputCleanup, errors.New("private-fixture-path-and-key-marker"))
					}
					return append([]byte(nil), values[path]...), nil
				}
				var out, errOut bytes.Buffer
				if code := runVerifyWithInput(ctx, args, &out, &errOut, read); code != 1 || out.Len() != 0 || !reflect.DeepEqual(reads, order[:index+1]) {
					t.Fatal("failed consumed owner permitted success or later reads")
				}
				want := "verify: child input or verification failed\n"
				if index >= 2 {
					want = "verify: parent input or verification failed\n"
				}
				if errOut.String() != want {
					t.Fatal("provider error or private identity escaped safe diagnostics")
				}
			})
		}
	}
	for _, role := range order[:2] {
		t.Run("child-only/"+role, func(t *testing.T) {
			read := func(_ context.Context, path string, _ retestInputKind) ([]byte, error) {
				if path == role {
					return nil, errRetestInputCleanup
				}
				if strings.HasPrefix(path, "parent") {
					t.Fatal("child-only opened parent")
				}
				return values[path], nil
			}
			var out, errOut bytes.Buffer
			if code := runVerifyWithInput(t.Context(), []string{"-public-key", "child-key", "-key-id", child.keyID, "child"}, &out, &errOut, read); code != 1 || out.Len() != 0 {
				t.Fatal("child-only ignored failed input cleanup")
			}
		})
	}
}

func TestRetestVerifyPropagatesEveryOwnedCloseBeforeSuccess(t *testing.T) {
	parent := commandRetestArtifact(t, "parent", false, nil, 0x51)
	reference := commandRetestReference(parent)
	child := commandRetestArtifact(t, retestChildID, false, &reference, 0x52)
	values := map[string][]byte{"child": child.encoded, "child-key": child.public, "parent": parent.encoded, "parent-key": parent.public}
	order := []string{"child", "child-key", "parent", "parent-key"}
	args := []string{"-public-key", "child-key", "-key-id", child.keyID, "-previous-report", "parent", "-previous-public-key", "parent-key", "-previous-key-id", parent.keyID, "child"}
	for _, role := range append([]string{"none"}, order...) {
		for _, owner := range []string{"readable", "metadata"} {
			for _, cancelled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/cancel=%t", role, owner, cancelled), func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					var reads []string
					reader := func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
						reads = append(reads, path)
						data, ok := values[path]
						if !ok {
							t.Fatal("unexpected input role")
						}
						fixture := newRetestInputFixture(len(data))
						fixture.readable.data = append([]byte(nil), data...)
						if path == role {
							file := fixture.readable
							if owner == "metadata" {
								file = fixture.metadata
							}
							file.closeErr = errors.New("private close marker")
							if cancelled {
								file.hook = func(op string) {
									if op == "close" {
										cancel()
									}
								}
							}
						}
						captured, err := readRetestInputWithOperations(ctx, path, kind, fixture.operations())
						assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
						if path == role {
							if captured != nil || !errors.Is(err, errRetestInputCleanup) {
								t.Fatal("real acquisition core failed to preserve checked Close error")
							}
						} else if err != nil || !bytes.Equal(captured, data) {
							t.Fatal("positive acquisition precondition failed")
						}
						return captured, err
					}
					var out, errOut bytes.Buffer
					code := runVerifyWithInput(ctx, args, &out, &errOut, reader)
					if role == "none" {
						if code != 0 || errOut.Len() != 0 || !reflect.DeepEqual(reads, order) {
							t.Fatal("real owned-input positive did not verify")
						}
						return
					}
					index := 0
					for order[index] != role {
						index++
					}
					if code != 1 || out.Len() != 0 || !reflect.DeepEqual(reads, order[:index+1]) {
						t.Fatal("owned close failure permitted later acquisition or success")
					}
					want := "verify: child input or verification failed\n"
					if index >= 2 {
						want = "verify: parent input or verification failed\n"
					}
					if errOut.String() != want {
						t.Fatal("owned provider Close diagnostic escaped")
					}
				})
			}
		}
	}
}

func TestRetestPairRequiresEligibleProductDistinctIDAndBothDigests(t *testing.T) {
	for _, mode := range []string{"foreign-v1", "same-child-id", "manifest-only-mismatch", "bundle-only-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			id := "parent"
			name := product.Name
			if mode == "same-child-id" {
				id = retestChildID
			}
			if mode == "foreign-v1" {
				name = "Foreign Signed Product"
			}
			parent := commandRetestArtifactForProduct(t, id, true, nil, 0x51, name)
			if parent.artifact.Manifest.Schema != bundle.ManifestSchema {
				t.Fatal("fixture is not an independently verified legacy v1 parent")
			}
			reference := commandRetestReference(parent)
			if mode == "manifest-only-mismatch" {
				reference.ManifestDigest = "sha256:" + strings.Repeat("1", 64)
			}
			if mode == "bundle-only-mismatch" {
				reference.BundleSHA256 = "sha256:" + strings.Repeat("2", 64)
			}
			child := commandRetestArtifact(t, retestChildID, true, &reference, 0x52)
			dir := t.TempDir()
			p, _ := writeCommandRetest(t, dir, "parent", parent)
			c, _ := writeCommandRetest(t, dir, "child", child)
			var out, errOut bytes.Buffer
			code := runVerify(t.Context(), []string{"-self-issued", "-previous-self-issued", "-previous-report", p, c}, &out, &errOut)
			want := "verify: exact predecessor relation mismatch\n"
			if mode == "foreign-v1" {
				want = "verify: parent input or verification failed\n"
			}
			if code != 1 || out.Len() != 0 || errOut.String() != want {
				t.Fatalf("pair rejected for wrong reason or accepted: %d %s", code, &errOut)
			}
		})
	}
}

type retestFaultWriter struct{ closed int }

func (writer *retestFaultWriter) Write([]byte) (int, error) {
	return 0, errors.New("fixture stdout failure")
}
func (writer *retestFaultWriter) Close() error { writer.closed++; return nil }

func TestRetestVerifyOptionsCancellationAndOutputCompletion(t *testing.T) {
	for _, args := range [][]string{
		{"-self-issued=false", "child"}, {"-self-issued", "-key-id", "unused", "child"},
		{"-self-issued", "-previous-self-issued", "child"}, {"-self-issued", "-previous-report", "parent", "child"},
		{"-self-issued", "-previous-report", "parent", "-previous-self-issued=false", "child"},
		{"-self-issued", "-previous-report", "parent", "-previous-self-issued", "-previous-public-key", "unused", "child"},
	} {
		var out, errOut bytes.Buffer
		reads := 0
		code := runVerifyWithInput(t.Context(), args, &out, &errOut, func(context.Context, string, retestInputKind) ([]byte, error) { reads++; return nil, errRetestInput })
		if code != 2 || reads != 0 || out.Len() != 0 {
			t.Fatal("invalid trust combination acquired input")
		}
	}
	parent := commandRetestArtifact(t, "legacy", true, nil, 0x51)
	reference := commandRetestReference(parent)
	child := commandRetestArtifact(t, retestChildID, true, &reference, 0x52)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out, errOut bytes.Buffer
	code := runVerifyWithInput(ctx, []string{"-self-issued", "child"}, &out, &errOut, func(context.Context, string, retestInputKind) ([]byte, error) { cancel(); return child.encoded, nil })
	if code != 1 || out.Len() != 0 {
		t.Fatal("late cancellation printed verification success")
	}
	writer := &retestFaultWriter{}
	errOut.Reset()
	code = runVerifyWithInput(t.Context(), []string{"-self-issued", "child"}, writer, &errOut, func(context.Context, string, retestInputKind) ([]byte, error) { return child.encoded, nil })
	if code != 1 || writer.closed != 0 || errOut.String() != "verify: output incomplete\n" {
		t.Fatal("stdout failure was acknowledged or caller output was closed")
	}
}

func retestFinalizeFixture(t *testing.T) ([]string, inventory.Snapshot, string) {
	t.Helper()
	dir := t.TempDir()
	key := bytes.Repeat([]byte{0x21}, 32)
	keyPath := writeTestKey(t, dir, "fingerprint.key", key)
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil)})
	if err := os.WriteFile(filepath.Join(dir, "cpu.json"), encodeEvidenceFixture(t, completeCPUEvidence()), 0600); err != nil {
		t.Fatal(err)
	}
	planPath := writeFinalizeTestPlan(t, dir, snapshot, key)
	// Assert that the positive plan actually schedules the evidence under test.
	encoded, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := plan.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range frozen.Tests {
		if row.ID == "cpu.quick" {
			found = string(row.Status) == "scheduled"
		}
	}
	if !found {
		t.Fatal("fixture lacks the required scheduled CPU row")
	}
	output := filepath.Join(dir, "child.hhpcr")
	args := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", dir, "-output", output}
	for _, definition := range quickResultDefinitions {
		outcome := "not_present"
		if definition.id == "cpu.quick" {
			outcome = "pass"
		}
		args = append(args, "-outcome", definition.id+"="+outcome)
	}
	return args, snapshot, output
}

func TestRetestStandaloneFinalizationSignsNewCurrentEvidence(t *testing.T) {
	args, snapshot, output := retestFinalizeFixture(t)
	parent := commandRetestArtifact(t, "legacy-parent", true, nil, 0x61)
	p, _ := writeCommandRetest(t, t.TempDir(), "parent", parent)
	before := append([]byte(nil), parent.encoded...)
	args = append(args, "-previous-report", p, "-previous-self-issued")
	var out, errOut bytes.Buffer
	code := runFinalizeWith(t.Context(), args, &out, &errOut, func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x31}, 128)), func() time.Time { return time.Unix(10, 20) })
	if code != 0 {
		t.Fatalf("real linked finalizer failed: %d %s", code, &errOut)
	}
	encoded, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	want := commandRetestReference(parent)
	if artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != want || !report.ValidRetestID(artifact.Envelope.SessionID) || artifact.Envelope.SessionID == parent.artifact.Envelope.SessionID || bytes.Contains(artifact.InventoryJSON, []byte("parent-only-no-inherited-results")) {
		t.Fatal("finalizer lost exact parent reference, fresh ID or evidence isolation")
	}
	var payload app.InventoryPayload
	if json.Unmarshal(artifact.InventoryJSON, &payload) != nil {
		t.Fatal("signed current payload invalid")
	}
	currentCPU := false
	for _, row := range payload.TestResults {
		if row.ID == "cpu.quick" {
			currentCPU = row.Status == app.TestPass && row.PlannedStatus == app.PlannedScheduled && bytes.Equal(row.Evidence, encodeEvidenceFixture(t, completeCPUEvidence()))
		}
	}
	if !currentCPU || payload.CoverageScope != app.CoverageFrozenPlan {
		t.Fatal("real current evidence or frozen plan was replaced")
	}
	after, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("parent artifact was modified")
	}
	out.Reset()
	errOut.Reset()
	if code := runVerify(t.Context(), []string{"-self-issued", "-previous-self-issued", "-previous-report", p, output}, &out, &errOut); code != 0 {
		t.Fatal("new report did not survive real pair verification")
	}
	// Re-running cannot replace the new immutable report.
	if code := runFinalizeWith(t.Context(), args, io.Discard, &errOut, func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x32}, 128)), time.Now); code == 0 {
		t.Fatal("existing child report overwritten")
	}
	after, err = os.ReadFile(output)
	if err != nil || !bytes.Equal(after, encoded) {
		t.Fatal("failed retry mutated report")
	}
}

func TestRetestStandaloneIDAndParentFailuresPrecedeSigning(t *testing.T) {
	for _, id := range []string{"", " ", "legacy", "AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE", retestChildID} {
		t.Run("id="+id, func(t *testing.T) {
			args, snapshot, output := retestFinalizeFixture(t)
			parent := commandRetestArtifact(t, retestChildID, true, nil, 0x51)
			p, _ := writeCommandRetest(t, t.TempDir(), "parent", parent)
			args = append(args, "-previous-report", p, "-previous-self-issued", "-session-id", id)
			calls := 0
			var out, errOut bytes.Buffer
			code := runFinalizeWith(t.Context(), args, &out, &errOut, func(context.Context) (inventory.Snapshot, error) { calls++; return snapshot, nil }, bytes.NewReader(nil), time.Now)
			if code != 2 || calls != 0 || out.Len() != 0 || !strings.Contains(errOut.String(), "distinct canonical UUIDv4") {
				t.Fatalf("explicit invalid child ID did not hit its intended guard: %d %s", code, &errOut)
			}
			if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid child ID created output")
			}
		})
	}
	args, snapshot, output := retestFinalizeFixture(t)
	args = append(args, "-previous-report", "private-marker", "-previous-self-issued")
	collect := 0
	var out, errOut bytes.Buffer
	result := runFinalizeOwnedDestinationWithPrevious(t.Context(), args, &out, &errOut, func(context.Context) (inventory.Snapshot, error) { collect++; return snapshot, nil }, bytes.NewReader(nil), time.Now, nil, writeDestination, nil, func(context.Context, string, retestInputKind) ([]byte, error) {
		return nil, errors.Join(errRetestInputCleanup, errors.New("private-marker"))
	})
	if result.Code != 1 || collect != 0 || out.Len() != 0 || errOut.String() != "finalize: predecessor input or verification failed\n" {
		t.Fatal("failed parent reached collection/signing or leaked its diagnostic")
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed parent created output")
	}
}

type retestCancelReader struct {
	reader io.Reader
	cancel context.CancelFunc
	calls  int
	on     int
}

func (reader *retestCancelReader) Read(p []byte) (int, error) {
	reader.calls++
	n, err := reader.reader.Read(p)
	if reader.calls == reader.on {
		reader.cancel()
	}
	return n, err
}

func TestRetestStandaloneCancellationBeforeSigningAndOutput(t *testing.T) {
	for _, point := range []string{"pre-cancelled", "collection", "signing-key", "after-pre-sign-check"} {
		t.Run(point, func(t *testing.T) {
			args, snapshot, output := retestFinalizeFixture(t)
			args = append(args, "-session-id", retestChildID)
			parent := commandRetestArtifact(t, "parent", true, nil, 0x51)
			p, _ := writeCommandRetest(t, t.TempDir(), "parent", parent)
			args = append(args, "-previous-report", p, "-previous-self-issued")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if point == "pre-cancelled" {
				cancel()
			}
			collect := func(context.Context) (inventory.Snapshot, error) {
				if point == "collection" {
					cancel()
				}
				return snapshot, nil
			}
			var random io.Reader = bytes.NewReader(bytes.Repeat([]byte{0x31}, 128))
			if point == "signing-key" {
				random = &retestCancelReader{reader: random, cancel: cancel, on: 1}
			}
			now := func() time.Time {
				if point == "after-pre-sign-check" {
					cancel()
				}
				return time.Unix(10, 20)
			}
			var out, errOut bytes.Buffer
			code := runFinalizeWith(ctx, args, &out, &errOut, collect, random, now)
			if code != 1 || strings.Contains(out.String(), "report written") {
				t.Fatalf("cancelled linked standalone signed output: %d %s", code, &errOut)
			}
			if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("cancelled linked standalone created output")
			}
		})
	}
}

func TestRetestVerifiedSummaryCannotBeReplacedByPathOrJournal(t *testing.T) {
	parent := commandRetestArtifact(t, "parent", true, nil, 0x51)
	p, _ := writeCommandRetest(t, t.TempDir(), "parent", parent)
	options := previousReportFlags{path: p, pathSet: true, trust: reportTrustFlags{selfIssued: true, selfSet: true}}
	summary, err := options.load(t.Context(), readRetestInput)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("changed after checked acquisition"), 0600); err != nil {
		t.Fatal(err)
	}
	args, snapshot, output := retestFinalizeFixture(t)
	args = append(args, "-session-id", retestChildID)
	var out, errOut bytes.Buffer
	reads := 0
	result := runFinalizeOwnedDestinationWithPrevious(t.Context(), args, &out, &errOut, func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x41}, 64)), time.Now, nil, writeDestination, summary, func(context.Context, string, retestInputKind) ([]byte, error) { reads++; return nil, errRetestInput })
	if result.Code != 0 || reads != 0 {
		t.Fatalf("immutable summary reopened a parent path: %d %s", result.Code, &errOut)
	}
	encoded, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
	if err != nil || artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != commandRetestReference(parent) {
		t.Fatal("typed summary changed after source replacement")
	}
	for _, mode := range []string{"empty-summary", "flags-conflict", "journal"} {
		t.Run(mode, func(t *testing.T) {
			args, snapshot, _ := retestFinalizeFixture(t)
			value := summary
			switch mode {
			case "empty-summary":
				value = &verifiedPreviousReport{}
			case "flags-conflict":
				args = append(args, "-previous-report", p, "-previous-self-issued")
			case "journal":
				args = append(args, "-session-journal", "forged.json", "-run-stop", "completed")
			}
			calls := 0
			var out, errOut bytes.Buffer
			result := runFinalizeOwnedDestinationWithPrevious(t.Context(), args, &out, &errOut, func(context.Context) (inventory.Snapshot, error) { calls++; return snapshot, nil }, bytes.NewReader(nil), time.Now, nil, writeDestination, value, readRetestInput)
			if result.Code == 0 || calls != 0 || out.Len() != 0 {
				t.Fatal("caller-created summary/serialized gate authorized signing")
			}
		})
	}
}

func TestRetestRealSessionAdapterUsesLiveGateAndFreshStopContext(t *testing.T) {
	for _, mode := range []string{"all-excluded", "clean-stop", "known-failure", "cleanup-failure", "consumed-gate", "pre-sign-unsafe"} {
		t.Run(mode, func(t *testing.T) {
			ids, err := plan.SelectableQuickTests("x86_64")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "clean-stop" || mode == "known-failure" {
				ids = nil
			}
			h := newSessionHarness(t, "x86_64", ids)
			if err := os.WriteFile(filepath.Join(h.directory, "synthetic.key"), bytes.Repeat([]byte{0x31}, 32), 0600); err != nil {
				t.Fatal(err)
			}
			parent := commandRetestArtifact(t, "legacy-parent", true, nil, 0x51)
			p, _ := writeCommandRetest(t, t.TempDir(), "parent", parent)
			h.arguments = append(h.arguments, "-previous-report", p, "-previous-self-issued")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "clean-stop" {
				worker := h.dependencies.RunWorker
				h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, errOut io.Writer) int {
					code := worker(ctx, w, in, out, errOut)
					cancel()
					return code
				}
			}
			if mode == "known-failure" {
				h.dependencies.RunWorker = func(_ context.Context, w sessionWorker, _ io.Reader, _, _ io.Writer) int {
					h.calls = append(h.calls, w.ID)
					evidence := sessionCompleteEvidence("cpu.quick").(quick.CPUResult)
					setCPUFixtureFailure(&evidence)
					writeSessionFixture(t, w.OutputPath, evidence)
					return 1
				}
			}
			if mode == "cleanup-failure" {
				h.dependencies.FinishInput = func() error {
					h.inputFinished = true
					return errors.Join(errSessionInputCleanup, errors.New("fixture cleanup failed"))
				}
			}
			if mode == "pre-sign-unsafe" {
				observe := h.dependencies.ObserveSafety
				h.dependencies.ObserveSafety = func(ctx context.Context) safemode.Result {
					result := observe(ctx)
					if h.safetyCalls == 3 {
						result.Status = "fail"
						result.ActiveNonLoopInterfaces = 1
					}
					return result
				}
			}
			calls := 0
			h.dependencies.Finalize = func(finalCtx context.Context, request sessionFinalization, out, errOut io.Writer) sessionFinalizationResult {
				calls++
				h.request = request
				if finalCtx.Err() != nil || !h.inputFinished || request.Previous == nil || request.Previous.reference != commandRetestReference(parent) {
					t.Fatal("real session adapter lost fresh integrity context, input cleanup or typed parent")
				}
				if mode == "consumed-gate" {
					if _, err := request.Gate(finalCtx); err != nil {
						t.Fatal(err)
					}
				}
				result := finalizeSessionWith(finalCtx, request, out, errOut, func(context.Context) (inventory.Snapshot, error) { return h.snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x52}, 128)), time.Now)
				if result.Journal != nil {
					h.journal = *result.Journal
				}
				h.finalized = result.Code == 0
				return result
			}
			code := h.run(ctx)
			output := filepath.Join(h.directory, "session.hhpcr")
			if mode == "cleanup-failure" {
				if code != 12 || calls != 0 {
					t.Fatalf("cleanup exit12 lost: %d %s", code, &h.stderr)
				}
				if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("uncertain cleanup signed")
				}
				return
			}
			if mode == "consumed-gate" || mode == "pre-sign-unsafe" {
				if code == 0 || h.finalized {
					t.Fatal("consumed or unsafe live gate authorized signing")
				}
				if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed gate created output")
				}
				return
			}
			if code != 0 || !h.finalized || calls != 1 {
				t.Fatalf("real linked session failed: %d %s", code, &h.stderr)
			}
			encoded, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
			if err != nil {
				t.Fatal(err)
			}
			if artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != commandRetestReference(parent) || !report.ValidRetestID(artifact.Envelope.SessionID) {
				t.Fatal("real adapter did not sign the linked profile")
			}
			if mode == "clean-stop" && (ctx.Err() == nil || h.journal.StopReason == "completed" || len(h.calls) != 1) {
				t.Fatal("clean-stop witness did not stop worker execution")
			}
			if mode == "all-excluded" && len(h.calls) != 0 {
				t.Fatal("all-off run executed a worker")
			}
			if mode == "known-failure" {
				var payload app.InventoryPayload
				if json.Unmarshal(artifact.InventoryJSON, &payload) != nil {
					t.Fatal("invalid signed payload")
				}
				found := false
				for _, row := range payload.TestResults {
					if row.ID == "cpu.quick" {
						found = row.Status == app.TestFail && bytes.Contains(row.Evidence, []byte(`"status":"fail"`))
					}
				}
				if !found || len(h.calls) != 1 {
					t.Fatal("linked session promoted known current failure or ran later workers")
				}
			}
		})
	}
}

func TestRetestParentFailureStopsAnOtherwiseScheduledSession(t *testing.T) {
	for _, mode := range []string{"acquisition", "cleanup-cancel", "signature"} {
		t.Run(mode, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			h.arguments = append(h.arguments, "-previous-report", "fixture-parent", "-previous-self-issued")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reads := 0
			h.dependencies.ReadRetestInput = func(context.Context, string, retestInputKind) ([]byte, error) {
				reads++
				if mode == "signature" {
					return []byte("not an archive"), nil
				}
				if mode == "cleanup-cancel" {
					cancel()
					return nil, errRetestInputCleanup
				}
				return nil, errRetestInput
			}
			if code := h.run(ctx); code != 1 || reads != 1 || len(h.calls) != 0 || h.finalized || h.safetyCalls != 0 || !strings.Contains(h.stderr.String(), "predecessor input or verification failed") {
				t.Fatalf("bad parent reached worker/signer: %d %s", code, &h.stderr)
			}
			if _, err := os.Stat(filepath.Join(h.directory, "session.hhpcr")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("parent failure created a report")
			}
		})
	}
}

func TestRetestManualCurrentFailureCannotBePromotedByParent(t *testing.T) {
	for _, status := range []quick.Status{quick.StatusFail, quick.StatusError, quick.StatusAborted} {
		t.Run(string(status), func(t *testing.T) {
			args, snapshot, output := retestFinalizeFixture(t)
			evidence := completeCPUEvidence()
			evidence.Status = status
			if status == quick.StatusFail {
				setCPUFixtureFailure(&evidence)
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(output), "cpu.json"), encodeEvidenceFixture(t, evidence), 0600); err != nil {
				t.Fatal(err)
			}
			for index, value := range args {
				if value == "cpu.quick=pass" {
					args[index] = "cpu.quick=" + string(status)
				}
			}
			parent := commandRetestArtifact(t, "parent", true, nil, 0x51)
			p, _ := writeCommandRetest(t, t.TempDir(), "parent", parent)
			args = append(args, "-previous-report", p, "-previous-self-issued")
			var out, errOut bytes.Buffer
			if code := runFinalizeWith(t.Context(), args, &out, &errOut, func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x31}, 128)), time.Now); code != 0 {
				t.Fatalf("honest partial report rejected: %d %s", code, &errOut)
			}
			encoded, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
			if err != nil {
				t.Fatal(err)
			}
			var payload app.InventoryPayload
			if json.Unmarshal(artifact.InventoryJSON, &payload) != nil {
				t.Fatal("invalid payload")
			}
			found := false
			for _, row := range payload.TestResults {
				if row.ID == "cpu.quick" {
					found = string(row.Status) == string(status) && bytes.Equal(row.Evidence, encodeEvidenceFixture(t, evidence))
				}
			}
			if !found {
				t.Fatal("current non-pass evidence was replaced by parent or execution note")
			}
		})
	}
}

func TestRetestComparisonAndPersistenceConsumeVerifiedCurrentBundle(t *testing.T) {
	args, snapshot, output := retestFinalizeFixture(t)
	parent := commandRetestArtifact(t, "parent", true, nil, 0x51)
	p, _ := writeCommandRetest(t, t.TempDir(), "parent", parent)
	args = append(args, "-previous-report", p, "-previous-self-issued")
	var out, errOut bytes.Buffer
	if code := runFinalizeWith(t.Context(), args, &out, &errOut, func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x31}, 128)), time.Now); code != 0 {
		t.Fatal(errOut.String())
	}
	encoded, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(encoded)
	// This fixture has no stable CPU identity. A signed linked report must not
	// turn that uncertainty into a continuity claim, even in self-comparison.
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
	if err != nil {
		t.Fatalf("comparison fixture is not verified: %v", err)
	}
	if artifact.Manifest.Schema != bundle.RetestManifestSchema || artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != commandRetestReference(parent) {
		t.Fatal("comparison fixture is not the intended linked report")
	}
	var payload app.InventoryPayload
	if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Components) != 1 || payload.SystemFingerprint.IdentifiedParts != 0 || payload.SystemFingerprint.UnidentifiedParts != 1 || payload.SystemFingerprint.Value != "" {
		t.Fatal("comparison fixture identity counts changed")
	}
	component := payload.Components[0]
	if component.Type != inventory.ComponentCPU || component.ID != "cpu:0" || len(component.Attributes) != 0 || component.Identity.Value != "" || component.Identity.Source != "" || component.Identity.Confidence != "unavailable" {
		t.Fatal("comparison fixture unexpectedly provides stable identity")
	}
	wantReference := comparison.ReportReference{
		SessionID: artifact.Envelope.SessionID, GeneratedTime: artifact.Envelope.GeneratedTime,
		SoftwareVersion: artifact.Envelope.Version, SignerKeyID: artifact.Envelope.KeyID,
		ManifestDigest: artifact.Envelope.ManifestDigest, SystemFingerprint: payload.SystemFingerprint,
		ComponentCount: 1,
	}
	out.Reset()
	errOut.Reset()
	if code := runCompare([]string{"-self-issued", output, output}, &out, &errOut); code != 0 || errOut.Len() != 0 {
		t.Fatalf("verified v2 comparison failed: code=%d stderr=%q", code, errOut.String())
	}
	var compared comparison.Result
	decoder := json.NewDecoder(&out)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&compared); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("comparison did not emit exactly one result")
	}
	if compared.Schema != comparison.Schema || compared.Policy != comparison.PolicyID || compared.Status != comparison.StatusInconclusive || !compared.Comparable || compared.Complete || compared.Summary != (comparison.Summary{Unresolved: 1}) {
		t.Fatalf("linked comparison changed identity-limited policy: %+v", compared)
	}
	if compared.Before != wantReference || compared.After != wantReference {
		t.Fatal("comparison did not retain both exact verified current-report references")
	}
	wantComponent := comparison.Component{Type: inventory.ComponentCPU, ID: "cpu:0", IdentityConfidence: "unavailable"}
	wantUnresolved := []comparison.UnresolvedChange{{
		Before: []comparison.Component{wantComponent}, After: []comparison.Component{wantComponent},
		Reason: "stable identity is unavailable at the same observed location; continuity or replacement cannot be proven",
	}}
	if len(compared.Matched) != 0 || len(compared.Replaced) != 0 || len(compared.Added) != 0 || len(compared.Removed) != 0 || !reflect.DeepEqual(compared.Unresolved, wantUnresolved) {
		t.Fatalf("linked comparison invented continuity or hardware changes: %+v", compared)
	}
	key := bytes.Repeat([]byte{0x21}, 32)
	candidate := reporttarget.Candidate{Device: "/dev/fixture1", Parent: "/dev/fixture", Topology: "/sys/devices/fixture", DeviceMajorMinor: "8:17", ParentMajorMinor: "8:16", SizeBytes: 32 << 20, ParentSizeBytes: 64 << 20, FingerprintKeyID: "hmac-sha256:" + strings.Repeat("1", 64), MediaIdentity: "hmac-sha256:" + strings.Repeat("2", 64), MediaIdentitySource: "serial"}
	authorization, err := reporttarget.NewAuthorization(candidate, "11111111-1111-4111-8111-111111111111", 123, key)
	if err != nil {
		t.Fatal(err)
	}
	encodedAuth, err := json.Marshal(authorization)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "authorization.json")
	if err := os.WriteFile(path, encodedAuth, 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	persist := func(_ context.Context, _ reporttarget.Options, _ reporttarget.Authorization, source *os.File, size int64, name string, digest [sha256.Size]byte) (reporttarget.Receipt, error) {
		calls++
		if source == nil || size != int64(len(encoded)) || digest != want {
			t.Fatal("persist did not receive exact verified v2 bytes")
		}
		return reporttarget.Receipt{Schema: reporttarget.ReceiptSchema, File: name, Bytes: size, SHA256: fmt.Sprintf("sha256:%x", digest), FileSynced: true, ReadBackVerified: true, DirectorySynced: true}, nil
	}
	out.Reset()
	errOut.Reset()
	if code := runPersistWith(t.Context(), []string{"-authorization", path, "-fingerprint-key", args[1], "-self-issued", output}, &out, &errOut, persist); code != 0 || calls != 1 {
		t.Fatalf("verified v2 persistence bridge failed: %d %s", code, &errOut)
	}
}
