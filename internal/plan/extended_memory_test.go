package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestExtendedMemoryCatalogPreservesQuickAndStandardBoundaries(t *testing.T) {
	var catalog struct {
		Tests []struct {
			ID          string `json:"id"`
			Implemented bool   `json:"implemented"`
		} `json:"tests"`
	}
	if json.Unmarshal(definitionData, &catalog) != nil || len(catalog.Tests) != 22 {
		t.Fatal("catalog prerequisite")
	}
	implemented := 0
	for _, row := range catalog.Tests {
		if row.Implemented {
			implemented++
		}
	}
	if implemented != 16 || len(catalog.Tests)-implemented != 6 {
		t.Fatal("candidate implementation count")
	}
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil),
		inventory.NewComponent(inventory.ComponentMemory, "memory:system", nil),
	})
	for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
		for _, profile := range []Profile{ProfileQuick, ProfileStandard, ProfileExtended, ProfileDestructive} {
			t.Run(architecture+"_"+string(profile), func(t *testing.T) {
				p := buildValidationPlan(t, snapshot, profile, architecture)
				memory := findPlanTest(t, &p, "memory.stress")
				cpu := findPlanTest(t, &p, "cpu.stress")
				wantMemory, wantCPU := TestExcluded, TestExcluded
				if profile == ProfileExtended {
					wantMemory = TestScheduled
				}
				if profile == ProfileStandard {
					wantCPU = TestScheduled
				}
				if memory.Version != 2 || memory.Status != wantMemory || memory.EstimatedSeconds != 1800 ||
					memory.TimeoutSeconds != 1860 || memory.Destructive || cpu.Status != wantCPU {
					t.Fatal("profile membership or fixed worker budget changed")
				}
				quick := findPlanTest(t, &p, "memory.quick")
				if quick.Version != 2 || quick.TimeoutSeconds != 30 || quick.EstimatedSeconds != 10 {
					t.Fatal("historical quick memory budget changed")
				}
				if _, err := Decode(rehashPlanForTest(t, &p)); err != nil {
					t.Fatal("current independent profile prerequisite", err)
				}
			})
		}
		ids, err := SelectableQuickTests(architecture)
		want := 14
		if architecture == "legacy-x86" {
			want = 12
		}
		if err != nil || len(ids) != want {
			t.Fatal("Extended altered the Quick menu")
		}
	}
	// Presence precedes the changed profile membership: absence is never
	// rewritten as an exclusion or as a completed stress observation.
	for _, profile := range []Profile{ProfileQuick, ProfileStandard, ProfileExtended, ProfileDestructive} {
		p := buildValidationPlan(t, inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil)}), profile, "x86_64")
		if findPlanTest(t, &p, "memory.stress").Status != TestNotPresent {
			t.Fatal("absent memory lost its disposition")
		}
	}
}

func TestExtendedMemoryLaunchDocumentRejectsMismatchedAuthority(t *testing.T) {
	for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
		raw, err := EncodeLaunchSelection(ProfileExtended, architecture, nil)
		want := fmt.Sprintf("{\"schema_version\":1,\"profile\":\"extended\",\"architecture\":%q,\"excluded_tests\":[]}\n", architecture)
		if err != nil || string(raw) != want {
			t.Fatal("literal Extended launch document")
		}
		if selected, err := DecodeLaunchSelection(raw, ProfileExtended, architecture); err != nil || len(selected) != 0 {
			t.Fatal("valid launch prerequisite", err)
		}
		for _, profile := range []Profile{ProfileQuick, ProfileStandard} {
			if _, err := DecodeLaunchSelection(raw, profile, architecture); err == nil {
				t.Fatal("launch document changed the caller's selected profile")
			}
		}
		if _, err := EncodeLaunchSelection(ProfileExtended, architecture, []string{"memory.quick"}); err == nil {
			t.Fatal("Extended accepted Quick exclusions")
		}
	}
	base := []byte("{\"schema_version\":1,\"profile\":\"extended\",\"architecture\":\"x86_64\",\"excluded_tests\":[]}\n")
	for _, test := range []struct{ name, old, next string }{
		{"version", "\"schema_version\":1", "\"schema_version\":2"},
		{"case", "\"extended\"", "\"EXTENDED\""},
		{"duplicate", "\"profile\":\"extended\"", "\"profile\":\"extended\",\"profile\":\"extended\""},
		{"architecture", "\"x86_64\"", "\"aarch64\""},
		{"exclusion", "\"excluded_tests\":[]", "\"excluded_tests\":[\"memory.quick\"]"},
		{"unknown", "\"excluded_tests\":[]", "\"excluded_tests\":[],\"duration\":1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeLaunchSelection(base, ProfileExtended, "x86_64"); err != nil {
				t.Fatal("valid launch prerequisite", err)
			}
			bad := bytes.Replace(base, []byte(test.old), []byte(test.next), 1)
			if bytes.Equal(base, bad) {
				t.Fatal("mutation was ineffective")
			}
			if _, err := DecodeLaunchSelection(bad, ProfileExtended, "x86_64"); err == nil {
				t.Fatal("forged launch authority accepted")
			}
		})
	}
}

func TestExtendedMemoryCurrentPlanRetainsStaleCatalogRejection(t *testing.T) {
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentMemory, "memory:system", nil)})
	for _, kind := range []string{"definition_version", "catalog_digest"} {
		t.Run(kind, func(t *testing.T) {
			p := buildValidationPlan(t, snapshot, ProfileExtended, "x86_64")
			row := findPlanTest(t, &p, "memory.stress")
			if row.Status != TestScheduled || row.Version != 2 {
				t.Fatal("current scheduled prerequisite")
			}
			if _, err := Decode(rehashPlanForTest(t, &p)); err != nil {
				t.Fatal("current execution prerequisite", err)
			}
			if kind == "definition_version" {
				row.Version = 1
			} else {
				p.DefinitionsDigest = "sha256:" + strings.Repeat("0", 64)
			}
			if _, err := Decode(rehashPlanForTest(t, &p)); err == nil {
				t.Fatal("stale authority entered current execution")
			}
		})
	}
}
