package plan

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestLaunchSelectionCanonicalWire(t *testing.T) {
	for _, test := range []struct {
		name      string
		profile   Profile
		excluded  []string
		want      string
		canonical []string
	}{
		{"quick_default", ProfileQuick, nil, "{\"schema_version\":1,\"profile\":\"quick\",\"architecture\":\"x86_64\",\"excluded_tests\":[]}\n", []string{}},
		{"quick_selected", ProfileQuick, []string{"usb.readiness.quick", "cpu.quick"}, "{\"schema_version\":1,\"profile\":\"quick\",\"architecture\":\"x86_64\",\"excluded_tests\":[\"cpu.quick\",\"usb.readiness.quick\"]}\n", []string{"cpu.quick", "usb.readiness.quick"}},
		{"standard_default", ProfileStandard, []string{}, "{\"schema_version\":1,\"profile\":\"standard\",\"architecture\":\"x86_64\",\"excluded_tests\":[]}\n", []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := slices.Clone(test.excluded)
			encoded, err := EncodeLaunchSelection(test.profile, "x86_64", test.excluded)
			if err != nil || string(encoded) != test.want || !reflect.DeepEqual(before, test.excluded) {
				t.Fatal("launch encoding changed literal wire bytes or caller intent")
			}
			got, err := DecodeLaunchSelection([]byte(test.want), test.profile, "x86_64")
			if err != nil || got == nil || !slices.Equal(got, test.canonical) {
				t.Fatal("literal handoff did not preserve canonical exclusions")
			}
		})
	}
	unsorted := []byte(`{"schema_version":1,"profile":"quick","architecture":"x86_64","excluded_tests":["usb.readiness.quick","cpu.quick"]}`)
	before := bytes.Clone(unsorted)
	got, err := DecodeLaunchSelection(unsorted, ProfileQuick, "x86_64")
	if err != nil || !slices.Equal(got, []string{"cpu.quick", "usb.readiness.quick"}) || !bytes.Equal(before, unsorted) {
		t.Fatal("decoding failed to canonicalize without changing input")
	}
	got[0] = "changed"
	again, err := DecodeLaunchSelection(unsorted, ProfileQuick, "x86_64")
	if err != nil || again[0] != "cpu.quick" {
		t.Fatal("decoded selections share mutable state")
	}
}

func TestLaunchSelectionSupportedTargets(t *testing.T) {
	for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
		for _, profile := range []Profile{ProfileQuick, ProfileStandard} {
			t.Run(string(profile)+"_"+architecture, func(t *testing.T) {
				wire := fmt.Sprintf("{\"schema_version\":1,\"profile\":%q,\"architecture\":%q,\"excluded_tests\":[]}\n", profile, architecture)
				got, err := DecodeLaunchSelection([]byte(wire), profile, architecture)
				if err != nil || got == nil || len(got) != 0 {
					t.Fatal("supported target rejected")
				}
				encoded, err := EncodeLaunchSelection(profile, architecture, nil)
				if err != nil || string(encoded) != wire {
					t.Fatal("target encoding differs from literal field order")
				}
			})
		}
	}
}

func TestLaunchSelectionRejectsMalformedDocuments(t *testing.T) {
	const valid = `{"schema_version":1,"profile":"quick","architecture":"x86_64","excluded_tests":[]}`
	replace := func(old, replacement string) string { return strings.Replace(valid, old, replacement, 1) }
	for _, test := range []struct{ name, wire string }{
		{"empty", ""},
		{"top_null", "null"},
		{"top_array", "[]"},
		{"trailing_document", valid + "{}"},
		{"unknown_member", strings.TrimSuffix(valid, "}") + `,"command":"ignored"}`},
		{"duplicate_member", strings.TrimSuffix(valid, "}") + `,"profile":"quick"}`},
		{"escaped_duplicate", strings.TrimSuffix(valid, "}") + `,"\u0070rofile":"quick"}`},
		{"case_alias", replace(`"profile"`, `"Profile"`)},
		{"unicode_alias", replace(`"schema_version"`, `"\u017fchema_version"`)},
		{"missing_version", replace(`"schema_version":1,`, "")},
		{"missing_profile", replace(`"profile":"quick",`, "")},
		{"missing_architecture", replace(`"architecture":"x86_64",`, "")},
		{"missing_exclusions", replace(`,"excluded_tests":[]`, "")},
		{"null_version", replace(`"schema_version":1`, `"schema_version":null`)},
		{"null_profile", replace(`"profile":"quick"`, `"profile":null`)},
		{"null_architecture", replace(`"architecture":"x86_64"`, `"architecture":null`)},
		{"null_exclusions", replace(`"excluded_tests":[]`, `"excluded_tests":null`)},
		{"version_string", replace(`"schema_version":1`, `"schema_version":"1"`)},
		{"version_fraction", replace(`"schema_version":1`, `"schema_version":1.0`)},
		{"future_version", replace(`"schema_version":1`, `"schema_version":2`)},
		{"profile_object", replace(`"profile":"quick"`, `"profile":{}`)},
		{"architecture_number", replace(`"architecture":"x86_64"`, `"architecture":64`)},
		{"exclusions_string", replace(`"excluded_tests":[]`, `"excluded_tests":"cpu.quick"`)},
		{"null_exclusion", replace(`"excluded_tests":[]`, `"excluded_tests":[null]`)},
		{"nonstring_exclusion", replace(`"excluded_tests":[]`, `"excluded_tests":[1]`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := DecodeLaunchSelection([]byte(test.wire), ProfileQuick, "x86_64")
			if err == nil || got != nil {
				t.Fatal("malformed launch document accepted")
			}
		})
	}
}

func TestLaunchSelectionRejectsTargetMismatch(t *testing.T) {
	const quick = `{"schema_version":1,"profile":"quick","architecture":"x86_64","excluded_tests":[]}`
	for _, test := range []struct {
		name, wire   string
		profile      Profile
		architecture string
	}{
		{"profile_mismatch", quick, ProfileStandard, "x86_64"},
		{"architecture_mismatch", quick, ProfileQuick, "x86"},
		{"unsupported_profile", strings.Replace(quick, "quick", "extended", 1), ProfileExtended, "x86_64"},
		{"unknown_architecture", strings.Replace(quick, "x86_64", "amd64", 1), ProfileQuick, "amd64"},
		{"profile_whitespace", strings.Replace(quick, "quick", " quick", 1), ProfileQuick, "x86_64"},
		{"architecture_whitespace", strings.Replace(quick, "x86_64", "x86_64 ", 1), ProfileQuick, "x86_64"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got, err := DecodeLaunchSelection([]byte(test.wire), test.profile, test.architecture); err == nil || got != nil {
				t.Fatal("unbound launch target accepted")
			}
		})
	}
}

func TestLaunchSelectionRejectsUnsafeChoices(t *testing.T) {
	for _, test := range []struct {
		name         string
		profile      Profile
		architecture string
		excluded     []string
	}{
		{"standard_exclusions", ProfileStandard, "x86_64", []string{"cpu.quick"}},
		{"duplicate", ProfileQuick, "x86_64", []string{"cpu.quick", "cpu.quick"}},
		{"unknown", ProfileQuick, "x86_64", []string{"unknown"}},
		{"nonquick", ProfileQuick, "x86_64", []string{"cpu.stress"}},
		{"legacy_graphics", ProfileQuick, "legacy-x86", []string{"graphics.quick"}},
		{"legacy_display", ProfileQuick, "legacy-x86", []string{"display.operator.quick"}},
		{"unsupported_profile", ProfileExtended, "x86_64", nil},
		{"unknown_architecture", ProfileQuick, "amd64", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got, err := EncodeLaunchSelection(test.profile, test.architecture, test.excluded); err == nil || got != nil {
				t.Fatal("encoder admitted an unsafe launch choice")
			}
			// Build the negative decoder fixture independently of the encoder.
			array := "[]"
			if len(test.excluded) != 0 {
				quoted := make([]string, len(test.excluded))
				for i, id := range test.excluded {
					quoted[i] = fmt.Sprintf("%q", id)
				}
				array = "[" + strings.Join(quoted, ",") + "]"
			}
			wire := fmt.Sprintf(`{"schema_version":1,"profile":%q,"architecture":%q,"excluded_tests":%s}`, test.profile, test.architecture, array)
			if got, err := DecodeLaunchSelection([]byte(wire), test.profile, test.architecture); err == nil || got != nil {
				t.Fatal("decoder admitted an unsafe launch choice")
			}
		})
	}
}

func TestLaunchSelectionByteBoundary(t *testing.T) {
	const valid = `{"schema_version":1,"profile":"standard","architecture":"x86_64","excluded_tests":[]}`
	exact := append([]byte(valid), bytes.Repeat([]byte(" "), 16384-len(valid))...)
	if MaximumLaunchSelectionBytes != 16384 {
		t.Fatal("launch bound changed")
	}
	if got, err := DecodeLaunchSelection(exact, ProfileStandard, "x86_64"); err != nil || got == nil || len(got) != 0 {
		t.Fatal("exact byte boundary rejected")
	}
	for _, wire := range [][]byte{append(bytes.Clone(exact), ' '), append([]byte(valid), 0xff)} {
		if got, err := DecodeLaunchSelection(wire, ProfileStandard, "x86_64"); err == nil || got != nil {
			t.Fatal("oversize or invalid UTF-8 accepted")
		}
	}
}

func TestLaunchSelectionKeepsQuickFormatSeparate(t *testing.T) {
	const old = `{"excluded_tests":["usb.readiness.quick","cpu.quick"]}`
	got, err := DecodeSelection([]byte(old), "x86_64")
	if err != nil || !slices.Equal(got, []string{"cpu.quick", "usb.readiness.quick"}) {
		t.Fatal("old quick selection changed")
	}
	if got, err := DecodeLaunchSelection([]byte(old), ProfileQuick, "x86_64"); err == nil || got != nil {
		t.Fatal("old unbound format accepted as launch handoff")
	}
	const launch = `{"schema_version":1,"profile":"quick","architecture":"x86_64","excluded_tests":[]}`
	if got, err := DecodeSelection([]byte(launch), "x86_64"); err == nil || got != nil {
		t.Fatal("new launch format entered old strict quick decoder")
	}
}
