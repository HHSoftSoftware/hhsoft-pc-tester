package plan

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestSelectionPreservesDefaultsAndFreezesCanonicalIntent(t *testing.T) {
	snapshot := fullValidationSnapshot()
	baseline := buildValidationPlan(t, snapshot, ProfileQuick, "x86_64")
	baselineBytes, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	for _, selection := range [][]string{nil, {}} {
		p, err := BuildWithSelection(snapshot, ProfileQuick, "x86_64", bytes.Repeat([]byte{11}, 32), "fixture", selection)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(p)
		if err != nil || !bytes.Equal(encoded, baselineBytes) || bytes.Contains(encoded, []byte("excluded_tests")) || p.SchemaVersion != 2 {
			t.Fatal("default plan bytes or schema changed")
		}
	}
	requested := []string{"memory.quick", "cpu.quick"}
	p, err := BuildWithSelection(snapshot, ProfileQuick, "x86_64", bytes.Repeat([]byte{11}, 32), "fixture", requested)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(requested, []string{"memory.quick", "cpu.quick"}) || !slices.Equal(p.ExcludedTests, []string{"cpu.quick", "memory.quick"}) {
		t.Fatal("selection was not copied and canonicalized")
	}
	assertTestStatus(t, p, "cpu.quick", TestExcluded)
	assertTestStatus(t, p, "memory.quick", TestExcluded)
	assertTestStatus(t, p, "audio.readiness.quick", TestScheduled)
	if p.ID == baseline.ID || p.HardwareGraph.Value != baseline.HardwareGraph.Value {
		t.Fatal("selection did not change only the plan binding")
	}
	reordered, err := BuildWithSelection(snapshot, ProfileQuick, "x86_64", bytes.Repeat([]byte{11}, 32), "fixture", []string{"cpu.quick", "memory.quick"})
	if err != nil || reordered.ID != p.ID {
		t.Fatal("equivalent selection order changed the plan ID")
	}
}

func TestSelectableQuickTestsReflectSafeImplementedImageCatalog(t *testing.T) {
	wantModern := []string{"audio.readiness.quick", "cpu.quick", "display.operator.quick", "graphics.quick", "input.readiness.quick", "memory.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick"}
	for _, architecture := range []string{"x86_64", "x86", "aarch64", "legacy-x86"} {
		ids, err := SelectableQuickTests(architecture)
		if err != nil {
			t.Fatal(err)
		}
		want := wantModern
		if architecture == "legacy-x86" {
			want = []string{"audio.readiness.quick", "cpu.quick", "input.readiness.quick", "memory.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick"}
		}
		if !slices.Equal(ids, want) {
			t.Fatalf("selectable %s tests = %v", architecture, ids)
		}
		p, err := BuildWithSelection(fullValidationSnapshot(), ProfileQuick, architecture, bytes.Repeat([]byte{11}, 32), "fixture", ids)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			assertTestStatus(t, p, id, TestExcluded)
		}
	}
}

func TestSelectionRetainsUnavailableReasons(t *testing.T) {
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentPCI, "pci:graphics", map[string]string{"class": "0x0300"}),
	})
	p, err := BuildWithSelection(snapshot, ProfileQuick, "x86_64", bytes.Repeat([]byte{11}, 32), "fixture", []string{"cpu.quick", "display.operator.quick"})
	if err != nil {
		t.Fatal(err)
	}
	assertTestStatus(t, p, "cpu.quick", TestNotPresent)
	assertTestStatus(t, p, "display.operator.quick", TestMissingDriver)
	if !slices.Equal(p.ExcludedTests, []string{"cpu.quick", "display.operator.quick"}) {
		t.Fatal("unavailable test exclusion intent was lost")
	}
	if _, err := Decode(rehashPlanForTest(t, &p)); err != nil {
		t.Fatal(err)
	}
}

func TestSelectionRejectsUnknownUnsupportedDuplicateAndNonQuickChoices(t *testing.T) {
	for _, selection := range [][]string{{"cpu.quick", "cpu.quick"}, {"unknown"}, {"audio.quick"}, {"cpu.stress"}, {"storage.destructive"}, {"CPU.QUICK"}, {" cpu.quick"}, {""}} {
		if _, err := BuildWithSelection(fullValidationSnapshot(), ProfileQuick, "x86_64", bytes.Repeat([]byte{11}, 32), "fixture", selection); err == nil {
			t.Fatalf("accepted unsafe or malformed selection %v", selection)
		}
	}
	for _, profile := range []Profile{ProfileStandard, ProfileExtended, ProfileDestructive} {
		if _, err := BuildWithSelection(fullValidationSnapshot(), profile, "x86_64", bytes.Repeat([]byte{11}, 32), "fixture", []string{"cpu.quick"}); err == nil {
			t.Fatalf("accepted selection in %s profile", profile)
		}
	}
	for _, id := range []string{"graphics.quick", "display.operator.quick"} {
		if _, err := BuildWithSelection(fullValidationSnapshot(), ProfileQuick, "legacy-x86", bytes.Repeat([]byte{11}, 32), "fixture", []string{id}); err == nil {
			t.Fatalf("accepted architecture-unsupported selection %s", id)
		}
	}
	for _, architecture := range []string{"", "amd64", "x86_64 ", "unknown"} {
		if _, err := SelectableQuickTests(architecture); err == nil {
			t.Fatalf("offered selections for unsupported architecture %q", architecture)
		}
		if _, err := DecodeSelection([]byte(`{"excluded_tests":[]}`), architecture); err == nil {
			t.Fatalf("accepted selection transport for unsupported architecture %q", architecture)
		}
	}
	if _, err := BuildWithSelection(fullValidationSnapshot(), ProfileQuick, "unknown", bytes.Repeat([]byte{11}, 32), "fixture", []string{"cpu.quick"}); err == nil {
		t.Fatal("accepted frozen selections for unsupported image architecture")
	}
}

func TestDecodeSelectionRejectsAmbiguousOrUnboundedDocuments(t *testing.T) {
	for _, encoded := range []string{
		``, `{}`, `null`, `[]`, `{"excluded_tests":null}`, `{"excluded_tests":"cpu.quick"}`,
		`{"excluded_tests":[null]}`, `{"excluded_tests":["unknown"]}`, `{"excluded_tests":["cpu.quick","cpu.quick"]}`,
		`{"excluded_tests":["audio.quick"]}`, `{"excluded_tests":["storage.destructive"]}`,
		`{"excluded_tests":[],"excluded_tests":["cpu.quick"]}`, `{"EXCLUDED_TESTS":[]}`,
		`{"excluded_tests":[],"profile":"destructive"}`, `{"excluded_tests":[]} {}`,
		`{"excluded_tests":[]}` + strings.Repeat(" ", MaximumSelectionBytes),
	} {
		if _, err := DecodeSelection([]byte(encoded), "x86_64"); err == nil {
			t.Fatalf("accepted invalid selection document %q", encoded[:min(80, len(encoded))])
		}
	}
	empty, err := DecodeSelection([]byte(`{"excluded_tests":[]}`), "x86_64")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("explicit empty selection was not preserved: %v %v", empty, err)
	}
	selected, err := DecodeSelection([]byte("{\n  \"excluded_tests\": [\"memory.quick\", \"cpu.quick\"]\n}\n"), "x86_64")
	if err != nil || !slices.Equal(selected, []string{"cpu.quick", "memory.quick"}) {
		t.Fatalf("valid selection was not canonicalized: %v %v", selected, err)
	}
	if _, err := DecodeSelection([]byte(`{"excluded_tests":["graphics.quick"]}`), "legacy-x86"); err == nil {
		t.Fatal("accepted unsupported legacy selection file")
	}
}

func TestFrozenSelectionRejectsRehashedInconsistentIntent(t *testing.T) {
	for _, mutate := range []func(*Plan){
		func(p *Plan) { p.ExcludedTests = nil },
		func(p *Plan) { p.ExcludedTests = []string{"memory.quick", "cpu.quick"} },
		func(p *Plan) { p.ExcludedTests = []string{"cpu.quick", "cpu.quick", "memory.quick"} },
		func(p *Plan) { p.ExcludedTests = []string{"cpu.quick", "memory.quick", "storage.destructive"} },
		func(p *Plan) { p.Profile = ProfileStandard },
		func(p *Plan) { findPlanTest(t, p, "cpu.quick").Status = TestScheduled },
	} {
		p, err := BuildWithSelection(fullValidationSnapshot(), ProfileQuick, "x86_64", bytes.Repeat([]byte{11}, 32), "fixture", []string{"cpu.quick", "memory.quick"})
		if err != nil {
			t.Fatal(err)
		}
		mutate(&p)
		if _, err := Decode(rehashPlanForTest(t, &p)); err == nil {
			t.Fatal("accepted rehashed inconsistent selection")
		}
	}
	p := buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "x86_64")
	encoded := rehashPlanForTest(t, &p)
	encoded = append(append([]byte(nil), encoded[:len(encoded)-1]...), []byte(`,"excluded_tests":null}`)...)
	if _, err := Decode(encoded); err == nil {
		t.Fatal("accepted explicit null frozen selection")
	}
}
