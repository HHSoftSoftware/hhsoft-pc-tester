package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/comparison"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

type comparisonInputSigned struct {
	encoded  []byte
	public   ed25519.PublicKey
	artifact bundle.Artifact
	self     bool
}

// Verify the signed fixture before fault injection. Expected comparison rows
// are literal assertions below, never a second call to Compare.
func comparisonInputArchive(t *testing.T, payload []byte, self bool, seed byte, previous *report.PreviousReport, productName string) comparisonInputSigned {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	keyID := "comparison-fixture-key"
	var member []byte
	var digest string
	var err error
	if self {
		member = public
		keyID, err = bundle.SelfIssuedKeyID(public)
		if err != nil {
			t.Fatal(err)
		}
	}
	if previous != nil {
		digest, err = bundle.RetestManifestDigestWithPDF(payload, nil, nil, *previous, member)
	} else if self {
		digest, err = bundle.SelfIssuedManifestDigest(payload, nil, public)
	} else {
		digest, err = bundle.ManifestDigest(payload, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	id := "comparison-legacy-session"
	if previous != nil {
		id = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
	}
	envelope := report.Envelope{Schema: report.EnvelopeSchemaV1, Product: productName, Version: product.Version, SessionID: id, GeneratedTime: "2026-10-28T00:00:00Z", Payload: append(json.RawMessage(nil), payload...), ManifestDigest: digest, KeyID: keyID}
	if err := report.Sign(&envelope, private); err != nil {
		t.Fatal(err)
	}
	if err := report.Verify(envelope, report.Keyring{keyID: public}); err != nil {
		t.Fatal("fixture signature precondition", err)
	}
	var out bytes.Buffer
	if previous != nil {
		err = bundle.WriteRetestWithPDF(&out, payload, envelope, nil, nil, *previous, member)
	} else if self {
		err = bundle.WriteSelfIssued(&out, payload, envelope, nil, public)
	} else {
		err = bundle.Write(&out, payload, envelope, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(nil), out.Bytes()...)
	var artifact bundle.Artifact
	if self {
		artifact, err = bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	} else {
		artifact, err = bundle.Verify(bytes.NewReader(raw), int64(len(raw)), report.Keyring{keyID: public})
	}
	if err != nil || artifact.Envelope.Product != productName || artifact.Envelope.SessionID != id || artifact.Envelope.ManifestDigest != digest || !bytes.Equal(artifact.InventoryJSON, payload) {
		t.Fatal("valid signed fixture precondition", err)
	}
	return comparisonInputSigned{raw, public, artifact, self}
}

func comparisonInputReport(t *testing.T, self bool, seed, fingerprint byte, epoch string, previous *report.PreviousReport) comparisonInputSigned {
	t.Helper()
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"serial": "comparison-fixture-drive", "model": "Fixture"})})
	payload, err := app.BuildInventoryPayload(snapshot, bytes.Repeat([]byte{fingerprint}, 32), epoch)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Components) != 1 || payload.SystemFingerprint.IdentifiedParts != 1 || payload.SystemFingerprint.UnidentifiedParts != 0 || payload.Components[0].Identity.Value == "" {
		t.Fatal("identified fixture precondition")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return comparisonInputArchive(t, raw, self, seed, previous, product.Name)
}

type comparisonInputHarness struct {
	t                 *testing.T
	args, order, seen []string
	values            map[string][]byte
	fixtures          []*retestInputFixture
	failAt            int
	wantOwners        map[string]int
	wantCloses        []string
	mutate            func(*retestInputFixture)
	lastError         error
	afterRead         func(int)
}

func newComparisonInputHarness(t *testing.T, before, after comparisonInputSigned, override bool) *comparisonInputHarness {
	t.Helper()
	h := &comparisonInputHarness{t: t, failAt: -1, values: map[string][]byte{"before": before.encoded, "before-key": before.public, "after": after.encoded, "after-key": after.public}}
	if before.self {
		h.args = []string{"-self-issued", "before", "after"}
		h.order = []string{"before", "after"}
	} else {
		h.args = []string{"-public-key", "before-key", "-key-id", before.artifact.Envelope.KeyID}
		afterKey := "before-key"
		if override {
			h.args = append(h.args, "-after-public-key", "after-key", "-after-key-id", after.artifact.Envelope.KeyID)
			afterKey = "after-key"
		}
		h.args = append(h.args, "before", "after")
		h.order = []string{"before", "before-key", "after", afterKey}
	}
	return h
}

func (h *comparisonInputHarness) read(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
	h.t.Helper()
	index := len(h.seen)
	if index >= len(h.order) || h.order[index] != path {
		h.t.Fatal("unexpected role or later acquisition", path)
	}
	wantKind := retestArtifactInput
	if strings.HasSuffix(path, "-key") {
		wantKind = retestPublicKeyInput
	}
	if kind != wantKind {
		h.t.Fatal("wrong production input kind")
	}
	h.seen = append(h.seen, path)
	data := h.values[path]
	fixture := newRetestInputFixture(len(data))
	fixture.readable.data = append([]byte(nil), data...)
	if index == h.failAt && h.mutate != nil {
		h.mutate(fixture)
	}
	h.fixtures = append(h.fixtures, fixture)
	captured, err := readRetestInputWithOperations(ctx, path, kind, fixture.operations())
	wantOwners, wantCloses := map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"}
	if index == h.failAt && h.wantOwners != nil {
		wantOwners, wantCloses = h.wantOwners, h.wantCloses
	}
	assertRetestInputLedger(h.t, fixture, wantOwners, wantCloses)
	if err == nil && !bytes.Equal(captured, data) {
		h.t.Fatal("core returned different captured bytes")
	}
	if err != nil && captured != nil {
		h.t.Fatal("core released bytes on acquisition failure")
	}
	h.lastError = err
	if h.afterRead != nil {
		h.afterRead(index)
	}
	return captured, err
}

func (h *comparisonInputHarness) assertClosed() {
	h.t.Helper()
	for _, fixture := range h.fixtures {
		if !reflect.DeepEqual(fixture.ledger.acquired, fixture.ledger.closed) {
			h.t.Fatal("input owner remains live")
		}
	}
}

func comparisonInputResult(t *testing.T, raw []byte) comparison.Result {
	t.Helper()
	var result comparison.Result
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&result); err != nil {
		t.Fatal(err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		t.Fatal("comparison output has trailing data")
	}
	return result
}

func assertComparisonInputUnchanged(t *testing.T, result comparison.Result, before, after comparisonInputSigned) {
	t.Helper()
	if result.Schema != comparison.Schema || result.Policy != comparison.PolicyID || result.Status != comparison.StatusUnchanged || !result.Comparable || !result.Complete || result.Summary != (comparison.Summary{Matched: 1}) || result.Before.ManifestDigest != before.artifact.Envelope.ManifestDigest || result.After.ManifestDigest != after.artifact.Envelope.ManifestDigest || result.Before.SessionID != before.artifact.Envelope.SessionID || result.After.SessionID != after.artifact.Envelope.SessionID || result.Before.SignerKeyID != before.artifact.Envelope.KeyID || result.After.SignerKeyID != after.artifact.Envelope.KeyID {
		t.Fatalf("unexpected literal unchanged comparison: %+v", result)
	}
	var beforePayload, afterPayload app.InventoryPayload
	if json.Unmarshal(before.artifact.InventoryJSON, &beforePayload) != nil || json.Unmarshal(after.artifact.InventoryJSON, &afterPayload) != nil || len(beforePayload.Components) != 1 || len(afterPayload.Components) != 1 {
		t.Fatal("signed expected-row precondition")
	}
	for _, test := range []struct {
		got     comparison.ReportReference
		fixture comparisonInputSigned
		payload app.InventoryPayload
	}{{result.Before, before, beforePayload}, {result.After, after, afterPayload}} {
		envelope := test.fixture.artifact.Envelope
		want := comparison.ReportReference{SessionID: envelope.SessionID, GeneratedTime: envelope.GeneratedTime, SoftwareVersion: envelope.Version, SignerKeyID: envelope.KeyID, ManifestDigest: envelope.ManifestDigest, SystemFingerprint: test.payload.SystemFingerprint, ComponentCount: 1}
		if !reflect.DeepEqual(test.got, want) {
			t.Fatal("full signed comparison reference differs")
		}
	}
	wantBefore := comparison.Component{Type: inventory.ComponentBlock, ID: "block:sda", IdentitySource: "serial", Identity: beforePayload.Components[0].Identity.Value, IdentityConfidence: "indicative", Attributes: []inventory.Attribute{{Name: "model", Value: "Fixture"}}}
	wantAfter := wantBefore
	wantAfter.Identity = afterPayload.Components[0].Identity.Value
	if !reflect.DeepEqual(result.Matched, []comparison.Match{{Before: wantBefore, After: wantAfter, LocationChange: false}}) || len(result.Replaced) != 0 || len(result.Added) != 0 || len(result.Removed) != 0 || len(result.Unresolved) != 0 {
		t.Fatal("literal retained component rows differ")
	}
}

func TestComparisonInputTrustDefaultsAndClosedBytes(t *testing.T) {
	parent := comparisonInputReport(t, false, 0x61, 0x21, "epoch-1", nil)
	ref := report.PreviousReport{ManifestDigest: parent.artifact.Envelope.ManifestDigest, BundleSHA256: fmt.Sprintf("sha256:%x", parent.artifact.BundleSHA256)}
	for _, test := range []struct {
		name                                  string
		self, linked, override, explicitFalse bool
	}{
		{"trusted-default", false, false, false, false}, {"trusted-distinct", false, false, true, false},
		{"trusted-explicit-false", false, false, false, true}, {"self-issued", true, false, false, false},
		{"trusted-mixed-v1-v2", false, true, true, false}, {"self-issued-mixed-v1-v2", true, true, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := comparisonInputReport(t, test.self, 0x61, 0x21, "epoch-1", nil)
			seed := byte(0x61)
			if test.override || test.self {
				seed = 0x62
			}
			var previous *report.PreviousReport
			if test.linked {
				previous = &ref
			}
			after := comparisonInputReport(t, test.self, seed, 0x21, "epoch-1", previous)
			h := newComparisonInputHarness(t, before, after, test.override)
			if test.explicitFalse {
				h.args = append([]string{"-self-issued=false"}, h.args...)
			}
			var stdout, stderr bytes.Buffer
			calls := 0
			writer := func(path string, out io.Writer, callback func(io.Writer) error) error {
				calls++
				h.assertClosed()
				if !reflect.DeepEqual(h.seen, h.order) {
					t.Fatal("output preceded all input roles")
				}
				return writeDestination(path, out, callback)
			}
			code := runCompareDestination(t.Context(), h.args, &stdout, &stderr, writer, h.read)
			if code != 0 || calls != 1 || stderr.Len() != 0 || !reflect.DeepEqual(h.seen, h.order) {
				t.Fatalf("positive compare failed: %d %s", code, stderr.String())
			}
			assertComparisonInputUnchanged(t, comparisonInputResult(t, stdout.Bytes()), before, after)
		})
	}
}

func TestComparisonInputEveryOwnerCloseFaultStopsOutput(t *testing.T) {
	before := comparisonInputReport(t, false, 0x63, 0x22, "epoch-1", nil)
	after := comparisonInputReport(t, false, 0x64, 0x22, "epoch-1", nil)
	for index, role := range []string{"before-report", "before-key", "after-report", "after-key"} {
		for _, owner := range []string{"metadata", "readable"} {
			for _, cancelled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/cancel=%t", role, owner, cancelled), func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					h := newComparisonInputHarness(t, before, after, true)
					h.failAt = index
					h.mutate = func(f *retestInputFixture) {
						file := f.readable
						if owner == "metadata" {
							file = f.metadata
						}
						file.closeErr = errors.New("private-close-marker")
						if cancelled {
							file.hook = func(op string) {
								if op == "close" {
									cancel()
								}
							}
						}
					}
					var stdout, stderr bytes.Buffer
					writes := 0
					code := runCompareDestination(ctx, h.args, &stdout, &stderr, func(string, io.Writer, func(io.Writer) error) error { writes++; return nil }, h.read)
					want := "verify before report failed\n"
					if index >= 2 {
						want = "verify after report failed\n"
					}
					if code != 1 || writes != 0 || stdout.Len() != 0 || stderr.String() != want || !reflect.DeepEqual(h.seen, h.order[:index+1]) || !errors.Is(h.lastError, errRetestInputCleanup) || !errors.Is(h.lastError, errRetestInput) {
						t.Fatal("Close fault did not preserve exact failure/role boundary")
					}
					h.assertClosed()
				})
			}
		}
	}
	t.Run("default-key-second-acquisition", func(t *testing.T) {
		h := newComparisonInputHarness(t, before, before, false)
		h.failAt = 3
		h.mutate = func(f *retestInputFixture) { f.metadata.closeErr = errors.New("private-default-key-marker") }
		var stdout, stderr bytes.Buffer
		writes := 0
		code := runCompareDestination(t.Context(), h.args, &stdout, &stderr, func(string, io.Writer, func(io.Writer) error) error { writes++; return nil }, h.read)
		if code != 1 || writes != 0 || stdout.Len() != 0 || stderr.String() != "verify after report failed\n" || !reflect.DeepEqual(h.seen, []string{"before", "before-key", "after", "before-key"}) || !errors.Is(h.lastError, errRetestInputCleanup) {
			t.Fatal("default after key was cached or its cleanup ignored")
		}
	})
}

func TestComparisonInputMalformedOwnersAndReadProtocol(t *testing.T) {
	before := comparisonInputReport(t, false, 0x65, 0x23, "epoch-1", nil)
	after := comparisonInputReport(t, false, 0x66, 0x23, "epoch-1", nil)
	for _, test := range []struct {
		name                   string
		mutate                 func(*retestInputFixture)
		metadataOnly, noOwners bool
	}{
		{"metadata-owner-error", func(f *retestInputFixture) { f.metadataErr = errors.New("private-open-marker") }, true, false},
		{"readable-owner-error", func(f *retestInputFixture) { f.readableErr = errors.New("private-open-marker") }, false, false},
		{"nil-metadata", func(f *retestInputFixture) { f.metadataOut = nil }, false, true},
		{"typed-nil-readable", func(f *retestInputFixture) { var owner *retestInputTestFile; f.readableOut = owner }, true, false},
		{"duplicate-owner", func(f *retestInputFixture) { f.readableOut = f.metadata }, true, false},
		{"nonregular-before-readable", func(f *retestInputFixture) { f.metadata.info.mode = os.ModeNamedPipe }, true, false},
		{"oversize-before-readable", func(f *retestInputFixture) { f.metadata.info.size = (64 << 20) + 1 }, true, false},
		{"identity-substitution", func(f *retestInputFixture) { info := *f.readable.info; info.id++; f.readable.info = &info }, false, false},
		{"early-eof", func(f *retestInputFixture) { f.readable.reads = []retestInputTestRead{{1, io.EOF}} }, false, false},
		{"zero-progress", func(f *retestInputFixture) { f.readable.reads = []retestInputTestRead{{0, nil}} }, false, false},
		{"invalid-count", func(f *retestInputFixture) { f.readable.reads = []retestInputTestRead{{-1, nil}} }, false, false},
		{"bytes-hard-error", func(f *retestInputFixture) {
			f.readable.reads = []retestInputTestRead{{1, errors.New("private-read-marker")}}
		}, false, false},
		{"probe-hard-error", func(f *retestInputFixture) {
			f.readable.reads = []retestInputTestRead{{len(f.readable.data), io.EOF}, {0, errors.New("private-probe-marker")}}
		}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newComparisonInputHarness(t, before, after, true)
			h.failAt = 2
			h.mutate = test.mutate
			if test.metadataOnly {
				h.wantOwners = map[string]int{"metadata": 1}
				h.wantCloses = []string{"metadata"}
			}
			if test.noOwners {
				h.wantOwners = map[string]int{}
				h.wantCloses = nil
			}
			var stdout, stderr bytes.Buffer
			writes := 0
			code := runCompareDestination(t.Context(), h.args, &stdout, &stderr, func(string, io.Writer, func(io.Writer) error) error { writes++; return nil }, h.read)
			if code != 1 || writes != 0 || stdout.Len() != 0 || stderr.String() != "verify after report failed\n" || !reflect.DeepEqual(h.seen, h.order[:3]) || !errors.Is(h.lastError, errRetestInput) {
				t.Fatal("malformed acquisition reached verification/output or leaked its error")
			}
			h.assertClosed()
			if test.name == "nonregular-before-readable" || test.name == "oversize-before-readable" {
				for _, call := range h.fixtures[2].ledger.calls {
					if call == "open.readable" {
						t.Fatal("readable acquisition preceded metadata admission")
					}
				}
			}
		})
	}
}

func TestComparisonInputCancellationBoundaries(t *testing.T) {
	before := comparisonInputReport(t, false, 0x67, 0x24, "epoch-1", nil)
	after := comparisonInputReport(t, false, 0x68, 0x24, "epoch-1", nil)
	t.Run("real-dispatch-pre-cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		var stdout, stderr bytes.Buffer
		code := run(ctx, []string{"compare", "-self-issued", "never-open-before", "never-open-after"}, bytes.NewReader(nil), &stdout, &stderr)
		if code != 1 || stdout.Len() != 0 || stderr.String() != "compare cancelled\n" {
			t.Fatal("real compare dispatch lost the stopped context")
		}
	})
	for index, role := range []string{"before-report", "before-key", "after-report", "after-key"} {
		t.Run("closed-capture/"+role, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h := newComparisonInputHarness(t, before, after, true)
			h.afterRead = func(captured int) {
				if captured == index {
					cancel()
				}
			}
			var stdout, stderr bytes.Buffer
			writes := 0
			code := runCompareDestination(ctx, h.args, &stdout, &stderr, func(string, io.Writer, func(io.Writer) error) error { writes++; return nil }, h.read)
			want := "verify before report failed\n"
			if index >= 2 {
				want = "verify after report failed\n"
			}
			if code != 1 || writes != 0 || stdout.Len() != 0 || stderr.String() != want || h.lastError != nil || !reflect.DeepEqual(h.seen, h.order[:index+1]) {
				t.Fatal("cancellation after a successful closed capture reached a later role")
			}
			h.assertClosed()
		})
	}
	for _, mode := range []string{"metadata-acquire-cancel", "readable-acquire-cancel", "metadata-open-hard-cancel", "metadata-stat-hard-cancel", "read-cancel", "read-hard-cancel", "exact-context-read", "final-metadata-cancel", "metadata-close-cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h := newComparisonInputHarness(t, before, after, true)
			h.failAt = 2
			if mode == "metadata-close-cancel" {
				h.failAt = 3
			}
			if mode == "metadata-acquire-cancel" || mode == "metadata-open-hard-cancel" || mode == "metadata-stat-hard-cancel" {
				h.wantOwners = map[string]int{"metadata": 1}
				h.wantCloses = []string{"metadata"}
			}
			h.mutate = func(f *retestInputFixture) {
				if mode == "metadata-acquire-cancel" {
					f.metadataHook = cancel
					return
				}
				if mode == "readable-acquire-cancel" {
					f.readableHook = cancel
					return
				}
				if mode == "metadata-open-hard-cancel" {
					f.metadataHook = cancel
					f.metadataErr = errors.Join(context.Canceled, errors.New("private-open-marker"))
					return
				}
				if mode == "metadata-stat-hard-cancel" {
					f.metadata.hook = func(op string) {
						if op == "stat" {
							cancel()
						}
					}
					f.metadata.stats = []retestInputTestStat{{f.metadata.info, errors.New("private-stat-marker")}}
					return
				}
				if mode == "final-metadata-cancel" {
					stats := 0
					f.metadata.hook = func(op string) {
						if op == "stat" {
							stats++
							if stats == 2 {
								cancel()
							}
						}
					}
					return
				}
				if mode == "metadata-close-cancel" {
					f.metadata.hook = func(op string) {
						if op == "close" {
							cancel()
						}
					}
					return
				}
				f.readable.hook = func(op string) {
					if op == "read" {
						cancel()
					}
				}
				if mode == "read-hard-cancel" {
					f.readable.reads = []retestInputTestRead{{1, errors.Join(context.Canceled, errors.New("private-hard-marker"))}}
				}
				if mode == "exact-context-read" {
					f.readable.reads = []retestInputTestRead{{0, context.Canceled}}
				}
			}
			var stdout, stderr bytes.Buffer
			writes := 0
			code := runCompareDestination(ctx, h.args, &stdout, &stderr, func(string, io.Writer, func(io.Writer) error) error { writes++; return nil }, h.read)
			if code != 1 || writes != 0 || stdout.Len() != 0 || stderr.String() != "verify after report failed\n" || !reflect.DeepEqual(h.seen, h.order[:h.failAt+1]) {
				t.Fatal("cancelled input reached a later role or output")
			}
			if strings.Contains(mode, "hard-cancel") {
				if !errors.Is(h.lastError, errRetestInput) || errors.Is(h.lastError, context.Canceled) {
					t.Fatal("hard input failure was rewritten as cancellation")
				}
			} else if !errors.Is(h.lastError, context.Canceled) {
				t.Fatal("pure stopped context was not retained by acquisition")
			}
			h.assertClosed()
		})
	}
	t.Run("output-callback-cancel-after-exclusive-open", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		h := newComparisonInputHarness(t, before, after, true)
		file := &outputFaultFile{limit: -1}
		calls, opens := 0, 0
		writer := func(_ string, out io.Writer, callback func(io.Writer) error) error {
			calls++
			h.assertClosed()
			cancel()
			return writeDestinationWith("owned-output", out, callback, func(string, int, os.FileMode) (io.WriteCloser, error) { opens++; return file, nil })
		}
		var stdout, stderr bytes.Buffer
		code := runCompareDestination(ctx, h.args, &stdout, &stderr, writer, h.read)
		if code != 1 || calls != 1 || opens != 1 || file.writes != 0 || file.closes != 1 || file.Len() != 0 || stdout.Len() != 0 || stderr.String() != "write comparison: context canceled\n" {
			t.Fatal("callback cancellation wrote data or failed to close the output owner")
		}
	})
}

func TestComparisonInputUsageAndRequiredDependencies(t *testing.T) {
	for _, args := range [][]string{
		{"-self-issued", "-public-key", "key", "-key-id", "id", "before", "after"},
		{"-public-key", "key", "-key-id", "id", "-after-public-key", "other", "before", "after"},
		{"-public-key", "key", "-key-id", "id", "-after-key-id", "other", "before", "after"},
		{"-self-issued", "before"}, {"-self-issued", "before", "after", "extra"}, {"before", "after"},
	} {
		var stdout, stderr bytes.Buffer
		reader := func(context.Context, string, retestInputKind) ([]byte, error) {
			t.Fatal("usage failure acquired input")
			return nil, errRetestInput
		}
		writer := func(string, io.Writer, func(io.Writer) error) error {
			t.Fatal("usage failure invoked output")
			return errRetestInput
		}
		if code := runCompareDestination(t.Context(), args, &stdout, &stderr, writer, reader); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatal("usage contract changed")
		}
	}
	var typedNil *retestInputStoppedContext
	for _, ctx := range []context.Context{nil, typedNil} {
		var stdout, stderr bytes.Buffer
		if code := runCompareDestination(ctx, []string{"-self-issued", "before", "after"}, &stdout, &stderr, writeDestination, readRetestInput); code != 1 || stdout.Len() != 0 || stderr.String() != "compare input or output handler is unavailable\n" {
			t.Fatal("invalid context was accepted")
		}
	}
	for _, missingReader := range []bool{false, true} {
		reader := retestInputReader(func(context.Context, string, retestInputKind) ([]byte, error) {
			t.Fatal("missing dependency acquired input")
			return nil, errRetestInput
		})
		writer := destinationWriter(func(string, io.Writer, func(io.Writer) error) error {
			t.Fatal("missing dependency invoked output")
			return errRetestInput
		})
		if missingReader {
			reader = nil
		} else {
			writer = nil
		}
		var stdout, stderr bytes.Buffer
		if code := runCompareDestination(t.Context(), []string{"-self-issued", "before", "after"}, &stdout, &stderr, writer, reader); code != 1 || stdout.Len() != 0 || stderr.String() != "compare input or output handler is unavailable\n" {
			t.Fatal("nil dependency silently selected a fallback")
		}
	}
}

func TestComparisonInputSignedFailuresAreSanitized(t *testing.T) {
	before := comparisonInputReport(t, false, 0x69, 0x25, "epoch-1", nil)
	after := comparisonInputReport(t, false, 0x6a, 0x25, "epoch-1", nil)
	for _, slot := range []string{"before", "after"} {
		for _, failure := range []string{"wrong-key", "foreign-product", "signed-inventory", "noncanonical-archive", "comparison-policy"} {
			t.Run(slot+"/"+failure, func(t *testing.T) {
				h := newComparisonInputHarness(t, before, after, true)
				fixture, seed := before, byte(0x69)
				if slot == "after" {
					fixture, seed = after, 0x6a
				}
				switch failure {
				case "wrong-key":
					key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x6b}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
					h.values[slot+"-key"] = key
				case "foreign-product":
					other := comparisonInputArchive(t, fixture.artifact.InventoryJSON, false, seed, nil, "private-product-marker")
					h.values[slot] = other.encoded
				case "signed-inventory":
					other := comparisonInputArchive(t, []byte(`{"private-payload-marker":true}`), false, seed, nil, product.Name)
					h.values[slot] = other.encoded
				case "noncanonical-archive":
					h.values[slot] = append(append([]byte(nil), fixture.encoded...), []byte("private-archive-marker")...)
				case "comparison-policy":
					var payload app.InventoryPayload
					if json.Unmarshal(fixture.artifact.InventoryJSON, &payload) != nil {
						t.Fatal("signed policy fixture precondition")
					}
					payload.Components[0].Identity.Confidence = "private-confidence-marker"
					encoded, err := json.Marshal(payload)
					if err != nil {
						t.Fatal(err)
					}
					other := comparisonInputArchive(t, encoded, false, seed, nil, product.Name)
					h.values[slot] = other.encoded
				}
				var stdout, stderr bytes.Buffer
				writes := 0
				code := runCompareDestination(t.Context(), h.args, &stdout, &stderr, func(string, io.Writer, func(io.Writer) error) error { writes++; return nil }, h.read)
				count := 2
				if slot == "after" {
					count = 4
				}
				want := "verify " + slot + " report failed\n"
				if failure == "comparison-policy" {
					count = 4
					want = "compare reports failed\n"
				}
				if code != 1 || writes != 0 || stdout.Len() != 0 || stderr.String() != want || !reflect.DeepEqual(h.seen, h.order[:count]) {
					t.Fatal("untrusted input escaped its fixed diagnostic or acquisition boundary")
				}
				h.assertClosed()
			})
		}
	}
}

func TestComparisonInputSignedHMACScopeRemainsIndependent(t *testing.T) {
	before := comparisonInputReport(t, false, 0x72, 0x26, "epoch-1", nil)
	for _, test := range []struct {
		name, epoch string
		fingerprint byte
		reason      string
	}{
		{"different-epoch", "epoch-2", 0x26, "fingerprint key epochs differ; an authorized key service must bridge identities"},
		{"different-key", "epoch-1", 0x27, "fingerprint key identifiers differ; matching epoch labels do not establish comparable identities"},
	} {
		t.Run(test.name, func(t *testing.T) {
			after := comparisonInputReport(t, false, 0x73, test.fingerprint, test.epoch, nil)
			h := newComparisonInputHarness(t, before, after, true)
			var stdout, stderr bytes.Buffer
			if code := runCompareDestination(t.Context(), h.args, &stdout, &stderr, writeDestination, h.read); code != 0 || stderr.Len() != 0 {
				t.Fatalf("signed HMAC comparison failed: %d %s", code, stderr.String())
			}
			result := comparisonInputResult(t, stdout.Bytes())
			if result.Schema != comparison.Schema || result.Policy != comparison.PolicyID || result.Status != comparison.StatusInconclusive || result.Comparable || result.Complete || result.Summary != (comparison.Summary{Unresolved: 1}) || len(result.Unresolved) != 1 || result.Unresolved[0].Reason != test.reason || result.Before.ManifestDigest != before.artifact.Envelope.ManifestDigest || result.After.ManifestDigest != after.artifact.Envelope.ManifestDigest {
				t.Fatalf("identity-key scope was conflated with signature trust: %+v", result)
			}
			h.assertClosed()
		})
	}
}

func TestComparisonInputRealDispatchAndFilePreservation(t *testing.T) {
	before := comparisonInputReport(t, false, 0x74, 0x28, "epoch-1", nil)
	after := comparisonInputReport(t, false, 0x75, 0x28, "epoch-1", nil)
	directory := t.TempDir()
	values := map[string][]byte{"before": before.encoded, "before-key": before.public, "after": after.encoded, "after-key": after.public}
	paths := map[string]string{}
	for name, data := range values {
		paths[name] = filepath.Join(directory, name)
		if err := os.WriteFile(paths[name], data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(directory, "comparison.json")
	args := []string{"compare", "-public-key", paths["before-key"], "-key-id", before.artifact.Envelope.KeyID, "-after-public-key", paths["after-key"], "-after-key-id", after.artifact.Envelope.KeyID, "-output", output, paths["before"], paths["after"]}
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), args, bytes.NewReader(nil), &stdout, &stderr)
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		if code != 1 || stdout.Len() != 0 || stderr.String() != "verify before report failed\n" {
			t.Fatal("unsupported real input adapter appeared successful")
		}
		if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("unsupported input created a destination")
		}
		return
	}
	if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("real owned comparison failed: %d %s", code, stderr.String())
	}
	encoded, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	assertComparisonInputUnchanged(t, comparisonInputResult(t, encoded), before, after)
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), args, bytes.NewReader(nil), &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "write comparison: open output:") {
		t.Fatal("existing output was not rejected by the unchanged exclusive writer")
	}
	retained, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(retained, encoded) {
		t.Fatal("existing comparison was overwritten")
	}
	for name, data := range values {
		retained, err := os.ReadFile(paths[name])
		if err != nil || !bytes.Equal(retained, data) {
			t.Fatal("comparison changed an input")
		}
	}
	stdoutArgs := append(append([]string(nil), args[:len(args)-4]...), paths["before"], paths["after"])
	callerOutput := &outputFaultFile{limit: -1}
	stderr.Reset()
	if code := run(t.Context(), stdoutArgs, bytes.NewReader(nil), callerOutput, &stderr); code != 0 || callerOutput.closes != 0 || stderr.Len() != 0 {
		t.Fatal("stdout ownership or real stdout comparison changed")
	}
	assertComparisonInputUnchanged(t, comparisonInputResult(t, callerOutput.Bytes()), before, after)
}
