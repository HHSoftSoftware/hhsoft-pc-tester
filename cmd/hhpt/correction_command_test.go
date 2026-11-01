package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/correction"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func correctionCommandKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("HHSoft public correction test fixture: " + label))
	return ed25519.NewKeyFromSeed(seed[:])
}

func correctionCommandPayload(t *testing.T) app.InventoryPayload {
	t.Helper()
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentSystem, "system", map[string]string{
		"vendor": "Observed Vendor", "product_name": "Observed Model", "product_serial": "synthetic-fixture-serial",
	})})
	payload, err := app.BuildInventoryPayload(snapshot, bytes.Repeat([]byte{7}, 32), "fixture-epoch")
	if err != nil {
		t.Fatal("fixture inventory failed")
	}
	return payload
}

// A positive prerequisite is a real signed canonical bundle, not a fabricated
// bundle.Artifact or a manually populated correction Source.
func correctionCommandReport(t *testing.T, payload app.InventoryPayload, self bool, session string) ([]byte, report.Keyring) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal("fixture payload failed")
	}
	return correctionCommandRawReport(t, encoded, self, session)
}

func correctionCommandRawReport(t *testing.T, encoded []byte, self bool, session string) ([]byte, report.Keyring) {
	t.Helper()
	key := correctionCommandKey("source")
	public := key.Public().(ed25519.PublicKey)
	keyID := "source-fixture"
	var digest string
	var err error
	if self {
		keyID, err = bundle.SelfIssuedKeyID(public)
		if err == nil {
			digest, err = bundle.SelfIssuedManifestDigest(encoded, nil, public)
		}
	} else {
		digest, err = bundle.ManifestDigest(encoded, nil)
	}
	if err != nil {
		t.Fatal("fixture manifest failed")
	}
	envelope := report.Envelope{Schema: report.EnvelopeSchemaV1, Product: product.Name, Version: product.Version,
		SessionID: session, GeneratedTime: "2026-01-01T00:00:00Z", Payload: encoded, ManifestDigest: digest, KeyID: keyID}
	if report.Sign(&envelope, key) != nil {
		t.Fatal("fixture signing failed")
	}
	var output bytes.Buffer
	if self {
		err = bundle.WriteSelfIssued(&output, encoded, envelope, nil, public)
	} else {
		err = bundle.Write(&output, encoded, envelope, nil)
	}
	if err != nil {
		t.Fatal("fixture bundle failed")
	}
	keys := report.Keyring{keyID: public}
	if _, err := bundle.Verify(bytes.NewReader(output.Bytes()), int64(output.Len()), keys); err != nil {
		t.Fatal("fixture full verification failed")
	}
	return append([]byte(nil), output.Bytes()...), keys
}

func correctionCommandInputs(t *testing.T) map[string][]byte {
	t.Helper()
	raw, keys := correctionCommandReport(t, correctionCommandPayload(t), false, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	key := correctionCommandKey("operator")
	inputs := map[string][]byte{"source": raw, "source-key": keys["source-fixture"], "signing-key": key, "addendum-key": key.Public().(ed25519.PublicKey),
		"request": []byte(`{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"field":"vendor","value":"Operator Vendor"}]}`)}
	source, err := correction.VerifySource(raw, keys, false)
	if err != nil {
		t.Fatal("source prerequisite")
	}
	inputs["addendum"], err = correction.Create(source, inputs["request"], "11111111-2222-4333-8444-555555555555", "operator-fixture", key)
	if err != nil {
		t.Fatal("addendum prerequisite")
	}
	if _, err := correction.Verify(source, inputs["addendum"], report.Keyring{"operator-fixture": inputs["addendum-key"]}, false); err != nil {
		t.Fatal("pair prerequisite")
	}
	return inputs
}
func correctionCommandReader(inputs map[string][]byte) retestInputReader {
	return func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		raw, ok := inputs[path]
		if !ok {
			return nil, errors.New("fixture input absent")
		}
		return append([]byte(nil), raw...), nil
	}
}
func correctionCreateArguments() []string {
	return []string{"create", "-report", "source", "-source-public-key", "source-key", "-source-key-id", "source-fixture", "-changes", "request", "-signing-key", "signing-key", "-key-id", "operator-fixture", "-output", "-"}
}
func correctionVerifyArguments() []string {
	return []string{"verify", "-report", "source", "-source-public-key", "source-key", "-source-key-id", "source-fixture", "-addendum", "addendum", "-addendum-public-key", "addendum-key", "-addendum-key-id", "operator-fixture"}
}
func correctionSet(args []string, name, value string) []string {
	out := append([]string(nil), args...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == name {
			out[i+1] = value
			return out
		}
	}
	panic("fixture argument absent")
}
func correctionDrop(args []string, name string) []string {
	out := append([]string(nil), args...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == name {
			return append(out[:i], out[i+2:]...)
		}
	}
	panic("fixture argument absent")
}

type correctionWriterFunc func([]byte) (int, error)

func (f correctionWriterFunc) Write(p []byte) (int, error) { return f(p) }

type correctionPrivateError struct{}

func (correctionPrivateError) Error() string { panic("provider Error callback must not run") }
func (correctionPrivateError) Is(error) bool { panic("provider Is callback must not run") }
func (correctionPrivateError) Unwrap() error { panic("provider Unwrap callback must not run") }

type correctionCloseWriter struct {
	bytes.Buffer
	closed int
}

func (w *correctionCloseWriter) Close() error {
	w.closed++
	return errors.New("private close diagnostic")
}

func TestCorrectionCommandConnectedFiles(t *testing.T) {
	inputs := correctionCommandInputs(t)
	dir := t.TempDir()
	paths := make(map[string]string)
	for name, raw := range inputs {
		paths[name] = filepath.Join(dir, name)
		if os.WriteFile(paths[name], raw, 0600) != nil {
			t.Fatal("fixture write")
		}
	}
	original := append([]byte(nil), inputs["source"]...)
	args := correctionCreateArguments()
	for _, pair := range [][2]string{{"-report", "source"}, {"-source-public-key", "source-key"}, {"-changes", "request"}, {"-signing-key", "signing-key"}} {
		args = correctionSet(args, pair[0], paths[pair[1]])
	}
	target := filepath.Join(dir, "created.json")
	args = correctionSet(args, "-output", target)
	var out, diagnostics bytes.Buffer
	if code := run(context.Background(), append([]string{"correction"}, args...), nil, &out, &diagnostics); code != 0 || out.Len() != 0 || diagnostics.Len() != 0 {
		t.Fatal("production create route")
	}
	created, err := os.ReadFile(target)
	if err != nil {
		t.Fatal("created output absent")
	}
	verify := correctionVerifyArguments()
	for _, pair := range [][2]string{{"-report", "source"}, {"-source-public-key", "source-key"}, {"-addendum-public-key", "addendum-key"}} {
		verify = correctionSet(verify, pair[0], paths[pair[1]])
	}
	verify = correctionSet(verify, "-addendum", target)
	if code := run(context.Background(), append([]string{"correction"}, verify...), nil, &out, &diagnostics); code != 0 || !strings.Contains(out.String(), "Correction verified against the exact original report.") {
		t.Fatal("production verify route")
	}
	view := append([]string(nil), verify...)
	view[0] = "view"
	viewPath := filepath.Join(dir, "comparison.html")
	view = append(view, "-output", viewPath)
	if run(context.Background(), append([]string{"correction"}, view...), nil, &out, &diagnostics) != 0 {
		t.Fatal("production view route")
	}
	html, err := os.ReadFile(viewPath)
	if err != nil || !bytes.Contains(html, []byte("Observed Vendor")) || !bytes.Contains(html, []byte("Operator Vendor")) {
		t.Fatal("comparison missing")
	}
	if run(context.Background(), append([]string{"correction"}, args...), nil, &out, &diagnostics) != 1 {
		t.Fatal("output collision accepted")
	}
	after, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(after, created) {
		t.Fatal("collision changed prior output")
	}
	after, err = os.ReadFile(paths["source"])
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("original changed")
	}
	if _, err := bundle.Verify(bytes.NewReader(after), int64(len(after)), report.Keyring{"source-fixture": inputs["source-key"]}); err != nil {
		t.Fatal("original lost verification")
	}
}

func TestCorrectionCommandIndependentTrust(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func([]string) []string
		code int
	}{
		{"missing_source_trust", func(a []string) []string {
			return correctionDrop(correctionDrop(a, "-source-public-key"), "-source-key-id")
		}, 2},
		{"missing_addendum_trust", func(a []string) []string {
			return correctionDrop(correctionDrop(a, "-addendum-public-key"), "-addendum-key-id")
		}, 2},
		{"mixed_source_trust", func(a []string) []string { return append(a, "-source-self-issued") }, 2},
		{"mixed_addendum_trust", func(a []string) []string { return append(a, "-addendum-self-issued") }, 2},
		{"explicit_false", func(a []string) []string {
			return append(correctionDrop(correctionDrop(a, "-addendum-public-key"), "-addendum-key-id"), "-addendum-self-issued=false")
		}, 2},
		{"wrong_source_key", func(a []string) []string { return correctionSet(a, "-source-public-key", "addendum-key") }, 1},
		{"wrong_addendum_key", func(a []string) []string { return correctionSet(a, "-addendum-public-key", "source-key") }, 1},
		{"spaced_source_id", func(a []string) []string { return correctionSet(a, "-source-key-id", " source-fixture ") }, 0},
		{"spaced_addendum_id", func(a []string) []string { return correctionSet(a, "-addendum-key-id", " operator-fixture ") }, 0},
		{"raw_path_preserved", func(a []string) []string { return correctionSet(a, "-source-public-key", " source-key ") }, 0},
		{"explicit_addendum_self_issued", func(a []string) []string {
			return append(correctionDrop(correctionDrop(a, "-addendum-public-key"), "-addendum-key-id"), "-addendum-self-issued")
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs := correctionCommandInputs(t)
			inputs[" source-key "] = inputs["source-key"]
			var out, diagnostics bytes.Buffer
			code := runCorrectionWith(context.Background(), tc.edit(correctionVerifyArguments()), &out, &diagnostics, correctionCommandReader(inputs), writeDestination, nil)
			if code != tc.code || code != 0 && out.Len() != 0 {
				t.Fatal("independent trust result")
			}
			if tc.name == "explicit_addendum_self_issued" && !strings.Contains(out.String(), "signer identity not independently established") {
				t.Fatal("assurance warning absent")
			}
		})
	}
}

func TestCorrectionCommandAcquisitionFailurePrivacy(t *testing.T) {
	for _, tc := range []struct {
		name, path        string
		cancel, withBytes bool
	}{
		{"source_failure", "source", false, false}, {"trust_failure", "source-key", false, false}, {"request_failure", "request", false, false}, {"key_failure", "signing-key", false, false},
		{"bytes_with_cleanup_failure", "request", false, true}, {"cancel_after_source", "source", true, true}, {"cancel_after_request", "request", true, true}, {"cancel_after_key", "signing-key", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs := correctionCommandInputs(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			base := correctionCommandReader(inputs)
			read := func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
				raw, err := base(ctx, path, kind)
				if path != tc.path {
					return raw, err
				}
				if tc.cancel {
					cancel()
					return raw, nil
				}
				if tc.withBytes {
					return raw, correctionPrivateError{}
				}
				return nil, correctionPrivateError{}
			}
			writes := 0
			write := func(string, io.Writer, func(io.Writer) error) error { writes++; return nil }
			var out, diagnostics bytes.Buffer
			code := runCorrectionWith(ctx, correctionCreateArguments(), &out, &diagnostics, read, write, bytes.NewReader(make([]byte, 16)))
			if code != 1 || writes != 0 || out.Len() != 0 || strings.Contains(diagnostics.String(), "private") {
				t.Fatal("failure privacy/ownership boundary")
			}
		})
	}
}

func TestCorrectionCommandOutputAndRandomFailures(t *testing.T) {
	for _, name := range []string{"random_short", "inconsistent_private_key", "short_write", "broken_pipe", "owned_close", "cancel_during_write"} {
		t.Run(name, func(t *testing.T) {
			inputs := correctionCommandInputs(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var out, diagnostics bytes.Buffer
			var writer io.Writer = &out
			var random io.Reader = bytes.NewReader(make([]byte, 16))
			write := destinationWriter(writeDestination)
			var owned *correctionCloseWriter
			switch name {
			case "random_short":
				random = bytes.NewReader(make([]byte, 15))
			case "inconsistent_private_key":
				inputs["signing-key"] = append([]byte(nil), inputs["signing-key"]...)
				inputs["signing-key"][32] ^= 1
			case "short_write":
				writer = correctionWriterFunc(func(p []byte) (int, error) { return len(p) - 1, nil })
			case "broken_pipe":
				writer = correctionWriterFunc(func([]byte) (int, error) { return 0, correctionPrivateError{} })
			case "cancel_during_write":
				writer = correctionWriterFunc(func(p []byte) (int, error) { cancel(); return len(p), nil })
			case "owned_close":
				owned = &correctionCloseWriter{}
				write = func(_ string, stdout io.Writer, body func(io.Writer) error) error {
					return writeDestinationWith("owned", stdout, body, func(string, int, os.FileMode) (io.WriteCloser, error) { return owned, nil })
				}
			}
			if code := runCorrectionWith(ctx, correctionCreateArguments(), writer, &diagnostics, correctionCommandReader(inputs), write, random); code != 1 || strings.Contains(diagnostics.String(), "private") {
				t.Fatal("output/random failure boundary")
			}
			if owned != nil && (owned.closed != 1 || owned.Len() == 0) {
				t.Fatal("owned close was not exercised")
			}
		})
	}
}

func TestCorrectionHelpUsesCheckedOutput(t *testing.T) {
	for _, name := range []string{"success", "short_write", "broken_pipe", "cancel_during_write", "already_cancelled", "nil_context"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var contextValue context.Context = ctx
			var out, diagnostics bytes.Buffer
			var writer io.Writer = &out
			want := 1
			switch name {
			case "success":
				want = 0
			case "short_write":
				writer = correctionWriterFunc(func(p []byte) (int, error) { return len(p) - 1, nil })
			case "broken_pipe":
				writer = correctionWriterFunc(func([]byte) (int, error) { return 0, correctionPrivateError{} })
			case "cancel_during_write":
				writer = correctionWriterFunc(func(p []byte) (int, error) { cancel(); return len(p), nil })
			case "already_cancelled":
				cancel()
			case "nil_context":
				contextValue = nil
			}
			never := func(context.Context, string, retestInputKind) ([]byte, error) {
				t.Fatal("help acquired input")
				return nil, nil
			}
			if code := runCorrectionWith(contextValue, []string{"create", "-h"}, writer, &diagnostics, never, writeDestination, nil); code != want {
				t.Fatal("help write/cancellation boundary")
			}
			if want == 0 && !strings.Contains(out.String(), "either trust choice") {
				t.Fatal("trust help missing")
			}
		})
	}
}

func TestCorrectionInputRoleBounds(t *testing.T) {
	if retestArtifactInput != 1 || retestPublicKeyInput != 2 || retestFingerprintKeyInput != 3 || retestSigningKeyInput != 4 || retestLaunchSelectionInput != 5 || retestCorrectionRequestInput != 6 || retestCorrectionArtifactInput != 7 {
		t.Fatal("input role numbers changed")
	}
	for _, tc := range []struct {
		name string
		kind retestInputKind
		size int
		pass bool
	}{
		{"request_max", retestCorrectionRequestInput, 8192, true}, {"request_over", retestCorrectionRequestInput, 8193, false}, {"request_empty", retestCorrectionRequestInput, 0, false},
		{"artifact_max", retestCorrectionArtifactInput, 16384, true}, {"artifact_over", retestCorrectionArtifactInput, 16385, false}, {"artifact_empty", retestCorrectionArtifactInput, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input")
			raw := bytes.Repeat([]byte("x"), tc.size)
			if os.WriteFile(path, raw, 0600) != nil {
				t.Fatal("fixture write")
			}
			got, err := readRetestInput(context.Background(), path, tc.kind)
			if (err == nil) != tc.pass {
				t.Fatal("input role bound")
			}
			if tc.pass && !bytes.Equal(got, raw) {
				t.Fatal("input bytes changed")
			}
			if !tc.pass && got != nil {
				t.Fatal("partial bytes released")
			}
		})
	}
}
