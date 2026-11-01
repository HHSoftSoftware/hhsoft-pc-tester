package correction

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

const correctionFixtureID = "11111111-2222-4333-8444-555555555555"
const correctionFixtureSession = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

func correctionFixtureKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("HHSoft public correction test fixture: " + label))
	return ed25519.NewKeyFromSeed(seed[:])
}

func correctionFixturePayload(t *testing.T) app.InventoryPayload {
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
func correctionFixtureReport(t *testing.T, payload app.InventoryPayload, self bool, session string) ([]byte, report.Keyring) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal("fixture payload failed")
	}
	return correctionFixtureRawReport(t, encoded, self, session)
}

func correctionFixtureRawReport(t *testing.T, encoded []byte, self bool, session string) ([]byte, report.Keyring) {
	t.Helper()
	key := correctionFixtureKey("source")
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

func correctionFixtureSource(t *testing.T) (Source, []byte, report.Keyring) {
	t.Helper()
	raw, keys := correctionFixtureReport(t, correctionFixturePayload(t), false, correctionFixtureSession)
	source, err := VerifySource(raw, keys, false)
	if err != nil {
		t.Fatal("positive source prerequisite failed")
	}
	return source, raw, keys
}

func correctionFixtureAddendum(t *testing.T, source Source) ([]byte, ed25519.PrivateKey) {
	t.Helper()
	key := correctionFixtureKey("operator")
	raw, err := Create(source, []byte(`{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"field":"vendor","value":"Operator Vendor"}]}`), correctionFixtureID, "operator-fixture", key)
	if err != nil {
		t.Fatal("positive addendum prerequisite failed")
	}
	if _, err := Verify(source, raw, report.Keyring{"operator-fixture": key.Public().(ed25519.PublicKey)}, false); err != nil {
		t.Fatal("positive pair prerequisite failed")
	}
	return raw, key
}

func correctionResign(t *testing.T, item record, key ed25519.PrivateKey) []byte {
	t.Helper()
	message, err := signingBytes(item)
	if err != nil {
		t.Fatal("fixture message failed")
	}
	item.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, message))
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal("fixture record failed")
	}
	return raw
}

func TestCorrectionConnectedCanonicalPair(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		sourceSelf, addendumSelf bool
	}{
		{"external_pair", false, false}, {"source_self_issued", true, false},
		{"addendum_self_issued", false, true}, {"both_self_issued", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rawSource, sourceKeys := correctionFixtureReport(t, correctionFixturePayload(t), tc.sourceSelf, correctionFixtureSession)
			original := append([]byte(nil), rawSource...)
			if tc.sourceSelf {
				sourceKeys = nil
			}
			source, err := VerifySource(rawSource, sourceKeys, tc.sourceSelf)
			if err != nil {
				t.Fatal("source failed")
			}
			raw, key := correctionFixtureAddendum(t, source)
			keys := report.Keyring{"operator-fixture": key.Public().(ed25519.PublicKey)}
			if tc.addendumSelf {
				keys = nil
			}
			verified, err := Verify(source, raw, keys, tc.addendumSelf)
			if err != nil {
				t.Fatal("pair failed")
			}
			view, err := RenderHTML(verified)
			if err != nil || !bytes.Contains(view, []byte("Observed Vendor")) || !bytes.Contains(view, []byte("Operator Vendor")) || !bytes.Contains(view, []byte("not new hardware observations")) {
				t.Fatal("comparison did not retain both meanings")
			}
			if !bytes.Equal(rawSource, original) {
				t.Fatal("original report changed")
			}
			if tc.sourceSelf && !bytes.Contains(view, []byte("signer identity not independently established")) {
				t.Fatal("source self-issued warning absent")
			}
		})
	}
}

func TestCorrectionCanonicalSignatureOracle(t *testing.T) {
	source, _, _ := correctionFixtureSource(t)
	raw, key := correctionFixtureAddendum(t, source)
	// This literal independently fixes field order and signature framing.
	public := base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))
	unsigned := `{"schema":"hhsoft.pc-tester.display-correction/v1","id":"11111111-2222-4333-8444-555555555555","source":{"manifest_digest":"` + source.reference.ManifestDigest + `","bundle_sha256":"` + source.reference.BundleSHA256 + `","session_id":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"},"component_id":"system","changes":[{"field":"vendor","value":"Operator Vendor"}],"key_id":"operator-fixture","public_key":"` + public + `"}`
	message := []byte("HHSoft PC Tester display correction addendum v1\x00")
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(unsigned)))
	message = append(message, length[:]...)
	message = append(message, []byte(unsigned)...)
	expected := strings.TrimSuffix(unsigned, "}") + `,"signature":"` + base64.StdEncoding.EncodeToString(ed25519.Sign(key, message)) + `"}`
	if string(raw) != expected {
		t.Fatal("canonical artifact/signature differs from independent literal")
	}
	reordered := []byte("{\n\"changes\":[{\"value\":\"Family\",\"field\":\"product_family\"},{\"value\":\"Operator Vendor\",\"field\":\"vendor\"}],\"schema\":\"hhsoft.pc-tester.display-correction-request/v1\"}")
	a, err := Create(source, reordered, correctionFixtureID, "operator-fixture", key)
	if err != nil {
		t.Fatal("equivalent request encoding failed")
	}
	b, err := Create(source, []byte(`{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"field":"vendor","value":"Operator Vendor"},{"field":"product_family","value":"Family"}]}`), correctionFixtureID, "operator-fixture", key)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("canonicalization depends on request field order")
	}
}

func TestCorrectionRequestFailureBoundaries(t *testing.T) {
	source, _, _ := correctionFixtureSource(t)
	_, key := correctionFixtureAddendum(t, source)
	good := `{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"field":"vendor","value":"Operator Vendor"}]}`
	for _, tc := range []struct{ name, raw string }{
		{"missing_schema", `{"changes":[{"field":"vendor","value":"X"}]}`},
		{"missing_changes", `{"schema":"hhsoft.pc-tester.display-correction-request/v1"}`},
		{"unknown_member", strings.TrimSuffix(good, "}") + `,"reason":"not allowed"}`},
		{"case_alias", strings.Replace(good, `"schema"`, `"Schema"`, 1)},
		{"duplicate_member", strings.Replace(good, `"schema":`, `"schema":"ignored","schema":`, 1)},
		{"escaped_duplicate", strings.Replace(good, `"schema":`, `"\u0073chema":"ignored","schema":`, 1)},
		{"duplicate_target", strings.Replace(good, `}]}`, `},{"field":"vendor","value":"Second"}]}`, 1)},
		{"serial_target", strings.Replace(good, `"vendor"`, `"product_serial"`, 1)},
		{"identity_target", strings.Replace(good, `"vendor"`, `"system_fingerprint"`, 1)},
		{"outcome_target", strings.Replace(good, `"vendor"`, `"test_results"`, 1)},
		{"empty_changes", `{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[]}`},
		{"too_many_changes", `{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[` + strings.Repeat(`{"field":"vendor","value":"X"},`, 4) + `{"field":"vendor","value":"X"}]}`},
		{"empty_value", strings.Replace(good, "Operator Vendor", "", 1)},
		{"no_op", strings.Replace(good, "Operator Vendor", "Observed Vendor", 1)},
		{"leading_space", strings.Replace(good, "Operator Vendor", " Leading", 1)},
		{"control", strings.Replace(good, "Operator Vendor", `Bad\nValue`, 1)},
		{"bidi", strings.Replace(good, "Operator Vendor", `Bad\u202eValue`, 1)},
		{"unpaired_surrogate", strings.Replace(good, "Operator Vendor", `Bad\ud800Value`, 1)},
		{"oversized_value", strings.Replace(good, "Operator Vendor", strings.Repeat("x", 257), 1)},
		{"invalid_utf8", strings.Replace(good, "Operator Vendor", string([]byte{255}), 1)},
		{"oversized_request", good + strings.Repeat(" ", MaximumRequestBytes)},
		{"trailing_object", good + `{}`},
		{"depth_limit", `{"schema":` + strings.Repeat("[", 9) + `0` + strings.Repeat("]", 9) + `}`},
		{"token_limit", `{"changes":[` + strings.Repeat(`{},`, 130) + `{}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out, err := Create(source, []byte(tc.raw), correctionFixtureID, "operator-fixture", key); err == nil || out != nil {
				t.Fatal("invalid request granted output")
			}
		})
	}
}

func TestCorrectionResignedSemanticFailures(t *testing.T) {
	source, _, _ := correctionFixtureSource(t)
	raw, key := correctionFixtureAddendum(t, source)
	keys := report.Keyring{"operator-fixture": key.Public().(ed25519.PublicKey)}
	for _, tc := range []struct {
		name   string
		mutate func(*record)
	}{
		{"bundle_binding", func(r *record) { r.Source.BundleSHA256 = "sha256:" + strings.Repeat("0", 64) }},
		{"manifest_binding", func(r *record) { r.Source.ManifestDigest = "sha256:" + strings.Repeat("0", 64) }},
		{"session_binding", func(r *record) { r.Source.SessionID = "other-session" }},
		{"component_binding", func(r *record) { r.ComponentID = "other-system" }},
		{"wrong_schema", func(r *record) { r.Schema = "hhsoft.pc-tester.report-envelope/v1" }},
		{"invalid_id", func(r *record) { r.ID = "not-a-uuid" }},
		{"private_field", func(r *record) { r.Changes[0].Field = "product_uuid" }},
		{"no_op", func(r *record) { r.Changes[0].Value = "Observed Vendor" }},
		{"empty_changes", func(r *record) { r.Changes = nil }},
		{"duplicate_changes", func(r *record) { r.Changes = append(r.Changes, r.Changes[0]) }},
		{"unsorted_changes", func(r *record) { r.Changes = append(r.Changes, change{"product_family", "Family"}) }},
		{"format_control", func(r *record) { r.Changes[0].Value = "Bad\u202eValue" }},
		{"key_identity", func(r *record) { r.KeyID = "other-key" }},
		{"public_key_binding", func(r *record) {
			r.PublicKey = base64.StdEncoding.EncodeToString(correctionFixtureKey("other").Public().(ed25519.PublicKey))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var item record
			if json.Unmarshal(raw, &item) != nil {
				t.Fatal("fixture decode")
			}
			tc.mutate(&item)
			forged := correctionResign(t, item, key)
			if _, err := Verify(source, forged, keys, false); err == nil {
				t.Fatal("correctly re-signed invalid claim accepted")
			}
		})
	}
}

func TestCorrectionArtifactTrustAndEncodingFailures(t *testing.T) {
	source, _, _ := correctionFixtureSource(t)
	raw, key := correctionFixtureAddendum(t, source)
	keys := report.Keyring{"operator-fixture": key.Public().(ed25519.PublicKey)}
	for _, tc := range []struct {
		name string
		raw  []byte
		keys report.Keyring
		self bool
	}{
		{"missing_trust", raw, nil, false},
		{"mixed_trust", raw, keys, true},
		{"wrong_trusted_key", raw, report.Keyring{"operator-fixture": correctionFixtureKey("wrong").Public().(ed25519.PublicKey)}, false},
		{"tamper", bytes.Replace(raw, []byte("Operator Vendor"), []byte("Other Vendor"), 1), keys, false},
		{"trailing_newline", append(append([]byte(nil), raw...), 10), keys, false},
		{"unknown_member", []byte(strings.TrimSuffix(string(raw), "}") + `,"reason":"extra"}`), keys, false},
		{"duplicate_member", bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"ignored","schema":`), 1), keys, false},
		{"case_alias", bytes.Replace(raw, []byte(`"source":`), []byte(`"Source":`), 1), keys, false},
		{"missing_signature", []byte(strings.Split(string(raw), `,"signature":`)[0] + "}"), keys, false},
		{"oversized_artifact", bytes.Repeat([]byte(" "), MaximumArtifactBytes+1), keys, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Verify(source, tc.raw, tc.keys, tc.self); err == nil {
				t.Fatal("invalid encoding/trust accepted")
			}
		})
	}
	var item record
	if json.Unmarshal(raw, &item) != nil {
		t.Fatal("fixture decode")
	}
	message, err := signingBytes(item)
	if err != nil {
		t.Fatal("fixture message")
	}
	item.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, append([]byte("HHSoft PC Tester report envelope v1"), message...)))
	cross, _ := json.Marshal(item)
	if _, err := Verify(source, cross, keys, false); err == nil {
		t.Fatal("different signature domain accepted")
	}
}

func TestCorrectionSourceAndKeyAuthority(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*app.InventoryPayload)
	}{
		{"no_system", func(p *app.InventoryPayload) { p.Components = nil }},
		{"multiple_systems", func(p *app.InventoryPayload) { p.Components = append(p.Components, p.Components[0]) }},
		{"cross_type_id_reuse", func(p *app.InventoryPayload) {
			p.Components = append(p.Components, app.ComponentRecord{Type: inventory.ComponentCPU, ID: p.Components[0].ID})
		}},
		{"duplicate_attribute", func(p *app.InventoryPayload) {
			p.Components[0].Attributes = append(p.Components[0].Attributes, inventory.Attribute{Name: "vendor", Value: "Ambiguous"})
		}},
		{"oversized_observation", func(p *app.InventoryPayload) {
			p.Components[0].Attributes = append(p.Components[0].Attributes, inventory.Attribute{Name: "product_family", Value: strings.Repeat("x", 1025)})
		}},
		{"oversized_component", func(p *app.InventoryPayload) { p.Components[0].ID = strings.Repeat("x", 257) }},
		{"wrong_payload_schema", func(p *app.InventoryPayload) { p.SchemaVersion = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := correctionFixturePayload(t)
			tc.mutate(&p)
			raw, keys := correctionFixtureReport(t, p, false, correctionFixtureSession)
			if _, err := VerifySource(raw, keys, false); err == nil {
				t.Fatal("unsupported verified source granted authority")
			}
		})
	}
	source, raw, keys := correctionFixtureSource(t)
	if _, err := VerifySource(raw, nil, false); err == nil {
		t.Fatal("source trust inferred")
	}
	if _, err := VerifySource(raw, keys, true); err == nil {
		t.Fatal("mixed source trust accepted")
	}
	key := correctionFixtureKey("operator")
	key[32] ^= 1
	if out, err := Create(source, []byte(`{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"field":"vendor","value":"Changed"}]}`), correctionFixtureID, "operator-fixture", key); err == nil || out != nil {
		t.Fatal("inconsistent key accepted")
	}
	if _, err := RenderHTML(Verified{}); err == nil {
		t.Fatal("zero verified state rendered")
	}
	if _, err := VerificationText(Verified{}); err == nil {
		t.Fatal("zero verified state asserted")
	}
}

func TestCorrectionViewEscapesOriginalAndOperatorValues(t *testing.T) {
	payload := correctionFixturePayload(t)
	payload.Components[0].ID = "system\u202e\n"
	for i := range payload.Components[0].Attributes {
		if payload.Components[0].Attributes[i].Name == "vendor" {
			payload.Components[0].Attributes[i].Value = "<script>old</script>\u202e\n"
		}
	}
	payload.Components[0].Attributes = append(payload.Components[0].Attributes, inventory.Attribute{Name: "product_version", Value: ""})
	raw, keys := correctionFixtureReport(t, payload, false, "source\u202e\n")
	source, err := VerifySource(raw, keys, false)
	if err != nil {
		t.Fatal("signed original-text prerequisite failed")
	}
	request := []byte(`{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"field":"vendor","value":"<img src=x onerror=alert(1)>"},{"field":"product_family","value":"New Family"},{"field":"product_version","value":"New Version"}]}`)
	key := correctionFixtureKey("operator")
	addendum, err := Create(source, request, correctionFixtureID, "operator-fixture", key)
	if err != nil {
		t.Fatal("escaped-text creation failed")
	}
	verified, err := Verify(source, addendum, report.Keyring{"operator-fixture": key.Public().(ed25519.PublicKey)}, false)
	if err != nil {
		t.Fatal("escaped-text verification failed")
	}
	view, err := RenderHTML(verified)
	if err != nil || bytes.Contains(view, []byte("<script>")) || bytes.Contains(view, []byte("<img ")) || bytes.Contains(view, []byte("\u202e")) {
		t.Fatal("active or invisible observed/operator text leaked")
	}
	for _, text := range []string{`\u202e`, `\n`, `(not observed)`, `&#34;&#34;`, `&lt;img`} {
		if !bytes.Contains(view, []byte(text)) {
			t.Fatal("escaped/absent/empty representation missing")
		}
	}
	var limited limitedView
	if n, err := io.WriteString(&limited, strings.Repeat("x", MaximumViewBytes+1)); n != 0 || err == nil || limited.buffer.Len() != 0 {
		t.Fatal("string-writer bypassed view bound")
	}
}

func TestCorrectionVerifiedStateDoesNotAliasInputs(t *testing.T) {
	source, raw, sourceKeys := correctionFixtureSource(t)
	before := sha256.Sum256(raw)
	addendum, key := correctionFixtureAddendum(t, source)
	keyBefore := append([]byte(nil), key...)
	verified, err := Verify(source, addendum, report.Keyring{"operator-fixture": key.Public().(ed25519.PublicKey)}, false)
	if err != nil {
		t.Fatal("pair failed")
	}
	view, err := RenderHTML(verified)
	if err != nil {
		t.Fatal("view failed")
	}
	if _, err := bundle.Verify(bytes.NewReader(raw), int64(len(raw)), sourceKeys); err != nil || sha256.Sum256(raw) != before {
		t.Fatal("original evidence changed")
	}
	if !bytes.Equal(key, keyBefore) {
		t.Fatal("caller signing key changed")
	}
	otherRaw, otherKeys := correctionFixtureReport(t, correctionFixturePayload(t), false, "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff")
	other, err := VerifySource(otherRaw, otherKeys, false)
	if err != nil {
		t.Fatal("other verified source prerequisite")
	}
	if _, err := Verify(other, addendum, report.Keyring{"operator-fixture": key.Public().(ed25519.PublicKey)}, false); err == nil {
		t.Fatal("addendum paired with different valid source")
	}
	clear(raw)
	clear(addendum)
	clear(key)
	again, err := RenderHTML(verified)
	if err != nil || !bytes.Equal(view, again) {
		t.Fatal("verified state aliases acquired input")
	}
}

func TestCorrectionRequestRequiredMembers(t *testing.T) {
	source, _, _ := correctionFixtureSource(t)
	_, key := correctionFixtureAddendum(t, source)
	for _, tc := range []struct{ name, raw string }{
		{"root_null", `null`},
		{"schema_null", `{"schema":null,"changes":[{"field":"vendor","value":"X"}]}`},
		{"changes_null", `{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":null}`},
		{"field_missing", `{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"value":"X"}]}`},
		{"field_null", `{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"field":null,"value":"X"}]}`},
		{"value_missing", `{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"field":"vendor"}]}`},
		{"value_null", `{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"field":"vendor","value":null}]}`},
		{"field_alias", `{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"Field":"vendor","value":"X"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !json.Valid([]byte(tc.raw)) {
				t.Fatal("presence mutation lost valid JSON")
			}
			if out, err := Create(source, []byte(tc.raw), correctionFixtureID, "operator-fixture", key); err == nil || out != nil {
				t.Fatal("missing/null/aliased member accepted")
			}
		})
	}
}

func TestCorrectionUTF8AndEncodedBounds(t *testing.T) {
	source, _, _ := correctionFixtureSource(t)
	key := correctionFixtureKey("operator")
	for _, tc := range []struct {
		name, value string
		pass        bool
	}{
		{"utf8_at_limit", strings.Repeat("\u00e9", 128), true},
		{"utf8_over_limit", strings.Repeat("\u00e9", 129), false},
		{"html_expansion_at_limit", strings.Repeat("<", 256), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requested, _ := json.Marshal(request{RequestSchema, []change{{"vendor", tc.value}, {"product_name", tc.value}, {"product_family", tc.value}, {"product_version", tc.value}}})
			raw, err := Create(source, requested, correctionFixtureID, "operator-fixture", key)
			if (err == nil) != tc.pass {
				t.Fatal("UTF-8 byte bound disagrees")
			}
			if !tc.pass {
				return
			}
			if len(raw) > MaximumArtifactBytes {
				t.Fatal("canonical artifact exceeds cap")
			}
			verified, err := Verify(source, raw, report.Keyring{"operator-fixture": key.Public().(ed25519.PublicKey)}, false)
			if err != nil {
				t.Fatal("bounded canonical artifact rejected")
			}
			view, err := RenderHTML(verified)
			if err != nil || len(view) > MaximumViewBytes {
				t.Fatal("escaped view exceeds cap")
			}
		})
	}
}

func TestCorrectionSourceUnicodeEscapeDistinction(t *testing.T) {
	encoded, err := json.Marshal(correctionFixturePayload(t))
	if err != nil {
		t.Fatal("fixture payload")
	}
	for _, tc := range []struct {
		name, fragment string
		pass           bool
	}{
		{"literal_replacement", "\"\ufffd\"", true}, {"escaped_replacement", `"\ufffd"`, true},
		{"valid_pair", `"\ud83d\ude00"`, true}, {"escaped_backslash", `"\\ud800"`, true},
		{"escaped_quote_backslash", `"quote: \" literal \\ud800"`, true},
		{"lone_high", `"\ud800"`, false}, {"lone_low", `"\udc00"`, false},
		{"high_then_text", `"\ud800x"`, false}, {"high_high", `"\ud800\ud801"`, false},
		{"high_then_literal_low", `"\ud800\\udc00"`, false}, {"pair_then_low", `"\ud83d\ude00\udc00"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutated := bytes.Replace(encoded, []byte(`"Observed Vendor"`), []byte(tc.fragment), 1)
			if bytes.Equal(mutated, encoded) || !json.Valid(mutated) {
				t.Fatal("Unicode fixture failed to change valid JSON")
			}
			raw, keys := correctionFixtureRawReport(t, mutated, false, correctionFixtureSession)
			source, err := VerifySource(raw, keys, false)
			if (err == nil) != tc.pass {
				t.Fatal("source Unicode escape policy differs")
			}
			ordinary, _, _ := correctionFixtureSource(t)
			key := correctionFixtureKey("operator")
			requested := []byte(`{"schema":"hhsoft.pc-tester.display-correction-request/v1","changes":[{"field":"vendor","value":` + tc.fragment + `}]}`)
			output, requestErr := Create(ordinary, requested, correctionFixtureID, "operator-fixture", key)
			if (requestErr == nil) != tc.pass || !tc.pass && output != nil {
				t.Fatal("request Unicode escape policy differs")
			}
			if tc.pass {
				correctionFixtureAddendum(t, source)
			}
		})
	}
}

func TestCorrectionReplacementCharacterRemainsCorrectable(t *testing.T) {
	for _, target := range []string{"session_id", "system_id", "vendor", "product_name", "product_family", "product_version"} {
		t.Run(target, func(t *testing.T) {
			payload := correctionFixturePayload(t)
			session := correctionFixtureSession
			field := "vendor"
			switch target {
			case "session_id":
				session = "session\ufffd"
			case "system_id":
				payload.Components[0].ID = "system\ufffd"
			default:
				field = target
				found := false
				for i := range payload.Components[0].Attributes {
					if payload.Components[0].Attributes[i].Name == target {
						payload.Components[0].Attributes[i].Value = "Observed\ufffd"
						found = true
					}
				}
				if !found {
					payload.Components[0].Attributes = append(payload.Components[0].Attributes, inventory.Attribute{Name: target, Value: "Observed\ufffd"})
				}
			}
			raw, keys := correctionFixtureReport(t, payload, false, session)
			source, err := VerifySource(raw, keys, false)
			if err != nil {
				t.Fatal("literal replacement character rejected")
			}
			requested, _ := json.Marshal(request{RequestSchema, []change{{field, "Operator Value"}}})
			key := correctionFixtureKey("operator")
			addendum, err := Create(source, requested, correctionFixtureID, "operator-fixture", key)
			if err != nil {
				t.Fatal("replacement character correction failed")
			}
			pair, err := Verify(source, addendum, report.Keyring{"operator-fixture": key.Public().(ed25519.PublicKey)}, false)
			if err != nil {
				t.Fatal("replacement pair failed")
			}
			view, err := RenderHTML(pair)
			if err != nil || !bytes.Contains(view, []byte(`\ufffd`)) || bytes.Contains(view, []byte("\ufffd")) {
				t.Fatal("replacement character lost visible representation")
			}
		})
	}
}

// Re-sign the changed inventory and manifest, retaining the canonical archive
// profile. Require an exact identity transformation before the malformed case.
func correctionRepackInventory(t *testing.T, original, inventoryJSON []byte) ([]byte, report.Envelope, report.Keyring) {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(original), int64(len(original)))
	if err != nil {
		t.Fatal("fixture archive")
	}
	members := make(map[string][]byte)
	for _, member := range archive.File {
		reader, err := member.Open()
		if err != nil {
			t.Fatal("fixture member")
		}
		body, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal("fixture member read")
		}
		members[member.Name] = body
	}
	var manifest bundle.Manifest
	var envelope report.Envelope
	if json.Unmarshal(members[bundle.ManifestName], &manifest) != nil || json.Unmarshal(members[bundle.EnvelopeName], &envelope) != nil {
		t.Fatal("fixture metadata")
	}
	members[bundle.InventoryName] = append([]byte(nil), inventoryJSON...)
	sum := sha256.Sum256(inventoryJSON)
	for i := range manifest.Files {
		if manifest.Files[i].Name == bundle.InventoryName {
			manifest.Files[i].Size = int64(len(inventoryJSON))
			manifest.Files[i].SHA256 = "sha256:" + hex.EncodeToString(sum[:])
		}
	}
	members[bundle.ManifestName], err = json.Marshal(manifest)
	if err != nil {
		t.Fatal("fixture manifest encoding")
	}
	sum = sha256.Sum256(members[bundle.ManifestName])
	envelope.ManifestDigest = "sha256:" + hex.EncodeToString(sum[:])
	envelope.Payload = inventoryJSON
	key := correctionFixtureKey("source")
	if report.Sign(&envelope, key) != nil {
		t.Fatal("fixture re-signing")
	}
	members[bundle.EnvelopeName], err = json.Marshal(envelope)
	if err != nil {
		t.Fatal("fixture envelope encoding")
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, member := range archive.File {
		header := &zip.FileHeader{Name: member.Name, Method: zip.Store}
		header.SetMode(0600)
		header.SetModTime(time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC))
		owned, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal("fixture header")
		}
		if _, err := owned.Write(members[member.Name]); err != nil {
			t.Fatal("fixture body")
		}
	}
	if writer.Close() != nil {
		t.Fatal("fixture archive close")
	}
	keys := report.Keyring{envelope.KeyID: key.Public().(ed25519.PublicKey)}
	if report.Verify(envelope, keys) != nil {
		t.Fatal("malicious fixture is not correctly signed")
	}
	return out.Bytes(), envelope, keys
}

func TestCorrectionSourceDuplicateMembersRemainRejected(t *testing.T) {
	_, original, keys := correctionFixtureSource(t)
	verified, err := bundle.Verify(bytes.NewReader(original), int64(len(original)), keys)
	if err != nil {
		t.Fatal("source prerequisite")
	}
	identity, _, _ := correctionRepackInventory(t, original, verified.InventoryJSON)
	if !bytes.Equal(identity, original) {
		t.Fatal("malicious fixture repacker lacks exact positive prerequisite")
	}
	for _, tc := range []struct{ name, old, next string }{
		{"top_level", `"schema_version":1`, `"schema_version":1,"schema_version":1`},
		{"nested", `"name":"vendor"`, `"name":"vendor","name":"vendor"`},
		{"escaped_alias", `"name":"vendor"`, `"name":"vendor","\u006eame":"vendor"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutated := bytes.Replace(verified.InventoryJSON, []byte(tc.old), []byte(tc.next), 1)
			if bytes.Equal(mutated, verified.InventoryJSON) || !json.Valid(mutated) {
				t.Fatal("duplicate fixture prerequisite")
			}
			raw, _, trust := correctionRepackInventory(t, original, mutated)
			if _, err := bundle.Verify(bytes.NewReader(raw), int64(len(raw)), trust); err == nil {
				t.Fatal("retained duplicate-member boundary bypassed")
			}
			if _, err := VerifySource(raw, trust, false); err == nil {
				t.Fatal("duplicate source minted correction authority")
			}
		})
	}
}

func TestCorrectionRetestSourceProfileStaysDistinct(t *testing.T) {
	parent, _, _ := correctionFixtureSource(t)
	payload, err := json.Marshal(correctionFixturePayload(t))
	if err != nil {
		t.Fatal("fixture payload")
	}
	previous := report.PreviousReport{ManifestDigest: parent.reference.ManifestDigest, BundleSHA256: parent.reference.BundleSHA256}
	digest, err := bundle.RetestManifestDigestWithPDF(payload, nil, nil, previous, nil)
	if err != nil {
		t.Fatal("retest manifest")
	}
	key := correctionFixtureKey("source")
	envelope := report.Envelope{Schema: report.EnvelopeSchemaV1, Product: product.Name, Version: product.Version, SessionID: "22222222-3333-4444-8555-666666666666", GeneratedTime: "2026-01-01T00:00:00Z", Payload: payload, ManifestDigest: digest, KeyID: "source-fixture"}
	if report.Sign(&envelope, key) != nil {
		t.Fatal("retest signing")
	}
	var out bytes.Buffer
	if bundle.WriteRetestWithPDF(&out, payload, envelope, nil, nil, previous, nil) != nil {
		t.Fatal("retest writing")
	}
	before := append([]byte(nil), out.Bytes()...)
	trust := report.Keyring{"source-fixture": key.Public().(ed25519.PublicKey)}
	source, err := VerifySource(before, trust, false)
	if err != nil {
		t.Fatal("valid retest source rejected")
	}
	correctionFixtureAddendum(t, source)
	result, err := bundle.Verify(bytes.NewReader(before), int64(len(before)), trust)
	if err != nil || result.Manifest.Schema != bundle.RetestManifestSchema || result.Manifest.PreviousReport == nil || *result.Manifest.PreviousReport != previous || !bytes.Equal(before, out.Bytes()) {
		t.Fatal("original linked profile changed")
	}
}
