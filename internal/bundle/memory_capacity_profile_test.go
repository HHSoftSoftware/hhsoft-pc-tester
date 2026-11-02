package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

// These literal protocol expectations are independent of production constants.
// The retained independent encoder signs each modified manifest and checks the
// envelope signature before bundle verification is asked to reject its content.
const capacityProfileLiteral = "hhsoft.pc-tester.memory-capacity-view/v1"

type capacityProfileMode struct {
	name         string
	linked, self bool
}
type capacityProfileSpec struct {
	current, self bool
	previous      *report.PreviousReport
	session       string
	mutate        func(*testing.T, string) string
}

func capacityProfileModes() []capacityProfileMode {
	return []capacityProfileMode{
		{"ordinary_external", false, false},
		{"ordinary_self", false, true},
		{"linked_external", true, false},
		{"linked_self", true, true},
	}
}
func capacityProfilePrevious(linked bool) *report.PreviousReport {
	if !linked {
		return nil
	}
	value := retestReference()
	return &value
}
func capacityProfileSchema(current, linked bool) string {
	if current {
		if linked {
			return "hhsoft.pc-tester.bundle-manifest/v4"
		}
		return "hhsoft.pc-tester.bundle-manifest/v3"
	}
	if linked {
		return "hhsoft.pc-tester.bundle-manifest/v2"
	}
	return "hhsoft.pc-tester.bundle-manifest/v1"
}
func capacityProfileReplace(t *testing.T, raw, old, next string) string {
	t.Helper()
	if old == next || strings.Count(raw, old) != 1 {
		t.Fatal("manifest mutation prerequisite")
	}
	return strings.Replace(raw, old, next, 1)
}
func capacityProfileAppend(t *testing.T, raw, field string) string {
	t.Helper()
	if !strings.HasSuffix(raw, "}") {
		t.Fatal("manifest object prerequisite")
	}
	return raw[:len(raw)-1] + "," + field + "}"
}
func capacityProfileSigned(t *testing.T, inventoryJSON, html, pdf []byte, spec capacityProfileSpec) ([]byte, report.Envelope, report.Keyring) {
	t.Helper()
	return independentlySignedRetest(t, inventoryJSON, html, pdf, spec.previous, spec.self, func(raw string) string {
		if spec.current {
			old := fmt.Sprintf("\"schema\":%q", capacityProfileSchema(false, spec.previous != nil))
			next := fmt.Sprintf("\"schema\":%q", capacityProfileSchema(true, spec.previous != nil))
			raw = capacityProfileReplace(t, raw, old, next)
			raw = capacityProfileAppend(t, raw, "\"view_profile\":\""+capacityProfileLiteral+"\"")
		}
		if spec.mutate != nil {
			before := raw
			raw = spec.mutate(t, raw)
			if raw == before || !json.Valid([]byte(raw)) {
				t.Fatal("effective JSON-valid mutation prerequisite")
			}
		}
		return raw
	}, func(envelope *report.Envelope) {
		envelope.Version = product.Version
		envelope.SessionID = "66666666-6666-4666-8666-666666666666"
		if spec.session != "" {
			envelope.SessionID = spec.session
		}
	})
}
func capacityProfileViews(t *testing.T, payload app.InventoryPayload, previous *report.PreviousReport, current bool) ([]byte, []byte) {
	t.Helper()
	var html, pdf []byte
	var err error
	switch {
	case previous != nil && current:
		html, err = render.InventoryMemoryCapacityRetestHTMLForVersion(payload, product.Version, *previous)
		if err == nil {
			pdf, err = render.InventoryMemoryCapacityRetestPDFForVersion(payload, product.Version, *previous)
		}
	case previous != nil:
		html, err = render.InventoryRetestHTMLForVersion(payload, product.Version, *previous)
		if err == nil {
			pdf, err = render.InventoryRetestPDFForVersion(payload, product.Version, *previous)
		}
	case current:
		html, err = render.InventoryMemoryCapacityHTMLForVersion(payload, product.Version)
		if err == nil {
			pdf, err = render.InventoryMemoryCapacityPDFForVersion(payload, product.Version)
		}
	default:
		html, err = render.InventoryHTMLForVersion(payload, product.Version)
		if err == nil {
			pdf, err = render.InventoryPDFForVersion(payload, product.Version)
		}
	}
	if err != nil {
		t.Fatalf("view prerequisite: %v", err)
	}
	return html, pdf
}
func capacityProfileAccepted(t *testing.T, encoded []byte, keys report.Keyring, self bool) Artifact {
	t.Helper()
	artifact, err := verifyViewFixture(encoded, keys, self)
	if err != nil {
		t.Fatalf("fully verified positive prerequisite: %v", err)
	}
	return artifact
}
func capacityProfileRejected(t *testing.T, encoded []byte, keys report.Keyring, self bool) {
	t.Helper()
	if _, err := verifyViewFixture(encoded, keys, self); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("correctly signed invalid profile was not rejected: %v", err)
	}
}

func TestMemoryCapacityCurrentProfileProducers(t *testing.T) {
	cases := []struct {
		name                    string
		linked, self, html, pdf bool
	}{
		{"ordinary_external_both", false, false, true, true},
		{"ordinary_external_html", false, false, true, false},
		{"ordinary_external_pdf", false, false, false, true},
		{"ordinary_self_both", false, true, true, true},
		{"ordinary_self_html", false, true, true, false},
		{"ordinary_self_pdf", false, true, false, true},
		{"linked_external_both", true, false, true, true},
		{"linked_external_html", true, false, true, false},
		{"linked_external_pdf", true, false, false, true},
		{"linked_self_both", true, true, true, true},
		{"linked_self_html", true, true, true, false},
		{"linked_self_pdf", true, true, false, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			payload, inventoryJSON := capacityLegacyPayload(t)
			previous := capacityProfilePrevious(test.linked)
			html, pdf := capacityProfileViews(t, payload, previous, true)
			if !test.html {
				html = nil
			}
			if !test.pdf {
				pdf = nil
			}
			spec := capacityProfileSpec{current: true, self: test.self, previous: previous}
			encoded, envelope, keys := capacityProfileSigned(t, inventoryJSON, html, pdf, spec)
			artifact := capacityProfileAccepted(t, encoded, keys, test.self)
			if artifact.Manifest.Schema != capacityProfileSchema(true, test.linked) ||
				artifact.Manifest.ViewProfile != capacityProfileLiteral ||
				!bytes.Equal(artifact.InventoryJSON, inventoryJSON) ||
				!bytes.Equal(artifact.RenderedReport, html) || !bytes.Equal(artifact.RenderedPDF, pdf) {
				t.Fatal("authenticated current profile or member identity changed")
			}
			if (html != nil && !bytes.Contains(html, []byte("Memory capacity observations"))) ||
				(pdf != nil && !bytes.Contains(pdf, []byte("MEMORY CAPACITY OBSERVATIONS"))) {
				t.Fatal("current view lacks the capacity section")
			}
			var embedded []byte
			if test.self {
				embedded = keys[envelope.KeyID]
			}
			digest, err := MemoryCapacityManifestDigestWithPDF(inventoryJSON, html, pdf, embedded, previous)
			if err != nil || digest != envelope.ManifestDigest {
				t.Fatal("producer manifest differs from independent signed oracle")
			}
			var actual bytes.Buffer
			if err := WriteMemoryCapacityWithPDF(&actual, inventoryJSON, envelope, html, pdf, embedded, previous); err != nil || !bytes.Equal(actual.Bytes(), encoded) {
				t.Fatalf("producer differs from independent canonical archive: %v", err)
			}
			if _, err := Verify(bytes.NewReader(encoded), int64(len(encoded)), nil); err == nil {
				t.Fatal("embedded material or external signature conferred implicit trust")
			}
			if !test.self {
				if _, err := VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded))); err == nil {
					t.Fatal("external bundle acquired an embedded trust mode")
				}
			}
			if _, err := MemoryCapacityManifestDigestWithPDF(inventoryJSON, nil, nil, embedded, previous); err == nil {
				t.Fatal("current digest producer accepted no views")
			}
			var empty bytes.Buffer
			if err := WriteMemoryCapacityWithPDF(&empty, inventoryJSON, envelope, nil, nil, embedded, previous); err == nil || empty.Len() != 0 {
				t.Fatal("current writer accepted or wrote a viewless profile")
			}
		})
	}
}

func TestMemoryCapacityHistoricalViewlessProfiles(t *testing.T) {
	for _, mode := range capacityProfileModes() {
		t.Run(mode.name, func(t *testing.T) {
			_, inventoryJSON := capacityLegacyPayload(t)
			previous := capacityProfilePrevious(mode.linked)
			encoded, _, keys := capacityProfileSigned(t, inventoryJSON, nil, nil, capacityProfileSpec{self: mode.self, previous: previous})
			artifact := capacityProfileAccepted(t, encoded, keys, mode.self)
			if artifact.Manifest.Schema != capacityProfileSchema(false, mode.linked) ||
				artifact.Manifest.ViewProfile != "" || artifact.RenderedReport != nil || artifact.RenderedPDF != nil ||
				!bytes.Equal(artifact.InventoryJSON, inventoryJSON) {
				t.Fatal("historical viewless profile changed")
			}
		})
	}
}

func TestMemoryCapacityCorrectlySignedProfileMutations(t *testing.T) {
	type mutation struct {
		name   string
		change func(*testing.T, string, capacityProfileMode) string
	}
	field := "\"view_profile\":\"" + capacityProfileLiteral + "\""
	cases := []mutation{
		{"missing_profile", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileReplace(t, s, ","+field, "")
		}},
		{"null_profile", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileReplace(t, s, field, "\"view_profile\":null")
		}},
		{"empty_profile", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileReplace(t, s, field, "\"view_profile\":\"\"")
		}},
		{"unknown_profile", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileReplace(t, s, field, "\"view_profile\":\"hhsoft.pc-tester.memory-capacity-view/v2\"")
		}},
		{"number_profile", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileReplace(t, s, field, "\"view_profile\":1")
		}},
		{"boolean_profile", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileReplace(t, s, field, "\"view_profile\":true")
		}},
		{"array_profile", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileReplace(t, s, field, "\"view_profile\":[]")
		}},
		{"object_profile", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileReplace(t, s, field, "\"view_profile\":{}")
		}},
		{"case_key", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileReplace(t, s, "\"view_profile\":", "\"View_Profile\":")
		}},
		{"duplicate_profile", func(t *testing.T, s string, _ capacityProfileMode) string { return capacityProfileAppend(t, s, field) }},
		{"escaped_key", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileReplace(t, s, "\"view_profile\"", "\"view_\\u0070rofile\"")
		}},
		{"escaped_value", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileReplace(t, s, field, "\"view_profile\":\"hhsoft.pc-tester.memory-capacity-view\\u002fv1\"")
		}},
		{"extra_member", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileAppend(t, s, "\"unexpected\":false")
		}},
		{"unknown_schema", func(t *testing.T, s string, m capacityProfileMode) string {
			return capacityProfileReplace(t, s, capacityProfileSchema(true, m.linked), "hhsoft.pc-tester.bundle-manifest/v5")
		}},
		{"legacy_schema_retains_profile", func(t *testing.T, s string, m capacityProfileMode) string {
			return capacityProfileReplace(t, s, capacityProfileSchema(true, m.linked), capacityProfileSchema(false, m.linked))
		}},
		{"missing_schema", func(t *testing.T, s string, m capacityProfileMode) string {
			return capacityProfileReplace(t, s, fmt.Sprintf("\"schema\":%q,", capacityProfileSchema(true, m.linked)), "")
		}},
		{"case_schema", func(t *testing.T, s string, _ capacityProfileMode) string {
			return capacityProfileReplace(t, s, "\"schema\":", "\"Schema\":")
		}},
		{"duplicate_schema", func(t *testing.T, s string, m capacityProfileMode) string {
			return capacityProfileAppend(t, s, fmt.Sprintf("\"schema\":%q", capacityProfileSchema(true, m.linked)))
		}},
		{"profile_not_last", func(t *testing.T, s string, _ capacityProfileMode) string {
			without := capacityProfileReplace(t, s, ","+field, "")
			return "{" + field + "," + without[1:]
		}},
		{"profile_schema_pair_swapped", func(t *testing.T, s string, m capacityProfileMode) string {
			return capacityProfileReplace(t, s, capacityProfileSchema(true, m.linked), capacityProfileSchema(true, !m.linked))
		}},
	}
	for _, mode := range capacityProfileModes() {
		t.Run(mode.name, func(t *testing.T) {
			payload, inventoryJSON := capacityLegacyPayload(t)
			previous := capacityProfilePrevious(mode.linked)
			html, pdf := capacityProfileViews(t, payload, previous, true)
			spec := capacityProfileSpec{current: true, self: mode.self, previous: previous}
			valid, _, keys := capacityProfileSigned(t, inventoryJSON, html, pdf, spec)
			capacityProfileAccepted(t, valid, keys, mode.self)
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					changed := spec
					changed.mutate = func(t *testing.T, s string) string { return test.change(t, s, mode) }
					encoded, _, keys := capacityProfileSigned(t, inventoryJSON, html, pdf, changed)
					capacityProfileRejected(t, encoded, keys, mode.self)
				})
			}
		})
	}
}

func TestMemoryCapacityCorrectlySignedViewMismatches(t *testing.T) {
	cases := []string{"legacy_both", "legacy_html", "legacy_pdf", "no_views", "empty_html", "empty_pdf", "unknown_profile_no_views"}
	for _, mode := range capacityProfileModes() {
		t.Run(mode.name, func(t *testing.T) {
			payload, inventoryJSON := capacityLegacyPayload(t)
			previous := capacityProfilePrevious(mode.linked)
			html, pdf := capacityProfileViews(t, payload, previous, true)
			oldHTML, oldPDF := capacityProfileViews(t, payload, previous, false)
			if bytes.Equal(html, oldHTML) || bytes.Equal(pdf, oldPDF) {
				t.Fatal("distinct profile prerequisite")
			}
			spec := capacityProfileSpec{current: true, self: mode.self, previous: previous}
			valid, _, keys := capacityProfileSigned(t, inventoryJSON, html, pdf, spec)
			capacityProfileAccepted(t, valid, keys, mode.self)
			for _, name := range cases {
				t.Run(name, func(t *testing.T) {
					changedHTML, changedPDF := html, pdf
					changed := spec
					switch name {
					case "legacy_both":
						changedHTML, changedPDF = oldHTML, oldPDF
					case "legacy_html":
						changedHTML = oldHTML
					case "legacy_pdf":
						changedPDF = oldPDF
					case "no_views":
						changedHTML, changedPDF = nil, nil
					case "empty_html":
						changedHTML = []byte{}
					case "empty_pdf":
						changedPDF = []byte{}
					case "unknown_profile_no_views":
						changedHTML, changedPDF = nil, nil
						changed.mutate = func(t *testing.T, s string) string {
							return capacityProfileReplace(t, s, capacityProfileLiteral, "hhsoft.pc-tester.memory-capacity-view/v2")
						}
					default:
						t.Fatal("undeclared mismatch")
					}
					encoded, envelope, keys := capacityProfileSigned(t, inventoryJSON, changedHTML, changedPDF, changed)
					capacityProfileRejected(t, encoded, keys, mode.self)
					if name != "unknown_profile_no_views" {
						var embedded []byte
						if mode.self {
							embedded = keys[envelope.KeyID]
						}
						if _, err := MemoryCapacityManifestDigestWithPDF(inventoryJSON, changedHTML, changedPDF, embedded, previous); err == nil {
							t.Fatal("current digest producer accepted mismatched views")
						}
						var output bytes.Buffer
						if err := WriteMemoryCapacityWithPDF(&output, inventoryJSON, envelope, changedHTML, changedPDF, embedded, previous); err == nil || output.Len() != 0 {
							t.Fatal("current writer accepted or wrote mismatched views")
						}
					}
				})
			}
		})
	}
}

func TestMemoryCapacityLegacyProfileRejectsViewSelection(t *testing.T) {
	cases := []string{"known_profile", "empty_profile", "null_profile", "unknown_profile", "current_content"}
	for _, mode := range capacityProfileModes() {
		t.Run(mode.name, func(t *testing.T) {
			payload, inventoryJSON := capacityLegacyPayload(t)
			previous := capacityProfilePrevious(mode.linked)
			spec := capacityProfileSpec{self: mode.self, previous: previous}
			valid, _, keys := capacityProfileSigned(t, inventoryJSON, nil, nil, spec)
			capacityProfileAccepted(t, valid, keys, mode.self)
			html, pdf := capacityProfileViews(t, payload, previous, true)
			for _, name := range cases {
				t.Run(name, func(t *testing.T) {
					changed := spec
					var selectedHTML, selectedPDF []byte
					field := ""
					switch name {
					case "known_profile":
						field = "\"view_profile\":\"" + capacityProfileLiteral + "\""
					case "empty_profile":
						field = "\"view_profile\":\"\""
					case "null_profile":
						field = "\"view_profile\":null"
					case "unknown_profile":
						field = "\"view_profile\":\"unknown\""
					case "current_content":
						selectedHTML, selectedPDF = html, pdf
					default:
						t.Fatal("undeclared legacy mismatch")
					}
					if field != "" {
						changed.mutate = func(t *testing.T, s string) string { return capacityProfileAppend(t, s, field) }
					}
					encoded, _, keys := capacityProfileSigned(t, inventoryJSON, selectedHTML, selectedPDF, changed)
					capacityProfileRejected(t, encoded, keys, mode.self)
				})
			}
		})
	}
}

func TestMemoryCapacityLinkedOriginalProfileMixes(t *testing.T) {
	originals := []struct {
		name                  string
		current, linked, self bool
	}{
		{"v1_external", false, false, false}, {"v1_self", false, false, true},
		{"v2_external", false, true, false}, {"v2_self", false, true, true},
		{"v3_external", true, false, false}, {"v3_self", true, false, true},
		{"v4_external", true, true, false}, {"v4_self", true, true, true},
	}
	attempts := []struct {
		name          string
		current, self bool
	}{
		{"v2_external", false, false}, {"v2_self", false, true},
		{"v4_external", true, false}, {"v4_self", true, true},
	}
	for _, original := range originals {
		t.Run(original.name, func(t *testing.T) {
			payload, inventoryJSON := capacityLegacyPayload(t)
			previous := capacityProfilePrevious(original.linked)
			html, pdf := capacityProfileViews(t, payload, previous, original.current)
			spec := capacityProfileSpec{current: original.current, self: original.self, previous: previous,
				session: "77777777-7777-4777-8777-777777777777"}
			parent, parentEnvelope, parentKeys := capacityProfileSigned(t, inventoryJSON, html, pdf, spec)
			capacityProfileAccepted(t, parent, parentKeys, original.self)
			reference := report.PreviousReport{ManifestDigest: parentEnvelope.ManifestDigest,
				BundleSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(parent))}
			for _, attempt := range attempts {
				t.Run(attempt.name, func(t *testing.T) {
					nextHTML, nextPDF := capacityProfileViews(t, payload, &reference, attempt.current)
					nextSpec := capacityProfileSpec{current: attempt.current, self: attempt.self, previous: &reference,
						session: "88888888-8888-4888-8888-888888888888"}
					child, _, childKeys := capacityProfileSigned(t, inventoryJSON, nextHTML, nextPDF, nextSpec)
					verified := capacityProfileAccepted(t, child, childKeys, attempt.self)
					if verified.Manifest.Schema != capacityProfileSchema(attempt.current, true) ||
						verified.Manifest.PreviousReport == nil || *verified.Manifest.PreviousReport != reference ||
						verified.Manifest.RevisionKind != "retest" || !bytes.Equal(verified.InventoryJSON, inventoryJSON) {
						t.Fatal("linked attempt changed its own profile, exact reference or observations")
					}
					expected := ""
					if attempt.current {
						expected = capacityProfileLiteral
					}
					if verified.Manifest.ViewProfile != expected {
						t.Fatal("parent profile selected the attempt renderer")
					}
					// The authenticated reference is not a recursive chain or chronology
					// claim. Both concrete archives above were verified independently.
				})
			}
		})
	}
}

func TestMemoryCapacityCurrentRelationFields(t *testing.T) {
	reference := retestReference()
	referenceJSON, err := json.Marshal(reference)
	if err != nil {
		t.Fatal("reference serialization prerequisite")
	}
	previousField := "\"previous_report\":" + string(referenceJSON)
	revisionField := "\"revision_kind\":\"retest\""
	profileField := "\"view_profile\":\"" + capacityProfileLiteral + "\""
	cases := []struct {
		name   string
		linked bool
		change func(*testing.T, string) string
	}{
		{"v4_missing_previous", true, func(t *testing.T, s string) string {
			return capacityProfileReplace(t, s, ","+previousField, "")
		}},
		{"v4_null_previous", true, func(t *testing.T, s string) string {
			return capacityProfileReplace(t, s, previousField, "\"previous_report\":null")
		}},
		{"v4_missing_revision", true, func(t *testing.T, s string) string {
			return capacityProfileReplace(t, s, ","+revisionField, "")
		}},
		{"v4_wrong_revision", true, func(t *testing.T, s string) string {
			return capacityProfileReplace(t, s, revisionField, "\"revision_kind\":\"resume\"")
		}},
		{"v3_forbidden_previous", false, func(t *testing.T, s string) string {
			return capacityProfileReplace(t, s, ","+profileField, ","+previousField+","+profileField)
		}},
		{"v3_forbidden_revision", false, func(t *testing.T, s string) string {
			return capacityProfileReplace(t, s, ","+profileField, ","+revisionField+","+profileField)
		}},
	}
	for _, trust := range []struct {
		name string
		self bool
	}{{"external", false}, {"self", true}} {
		t.Run(trust.name, func(t *testing.T) {
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					payload, inventoryJSON := capacityLegacyPayload(t)
					var previous *report.PreviousReport
					if test.linked {
						previous = &reference
					}
					html, pdf := capacityProfileViews(t, payload, previous, true)
					spec := capacityProfileSpec{current: true, self: trust.self, previous: previous}
					valid, _, keys := capacityProfileSigned(t, inventoryJSON, html, pdf, spec)
					artifact := capacityProfileAccepted(t, valid, keys, trust.self)
					if artifact.Manifest.Schema != capacityProfileSchema(true, test.linked) ||
						artifact.Manifest.ViewProfile != capacityProfileLiteral {
						t.Fatal("current relation profile prerequisite")
					}
					spec.mutate = test.change
					malformed, _, keys := capacityProfileSigned(t, inventoryJSON, html, pdf, spec)
					capacityProfileRejected(t, malformed, keys, trust.self)
				})
			}
		})
	}
}
