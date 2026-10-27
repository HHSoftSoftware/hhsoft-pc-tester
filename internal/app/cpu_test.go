package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func TestValidatedCPUScalarSharesStrictSummaryAndHistoricalScope(t *testing.T) {
	for _, name := range []string{"cpu-v3-direct-pass", "session-v5-cpu-v3-local-heat"} {
		payload := thermalFixture(t, name)
		view, err := ValidatedCPUScalar(payload)
		if err != nil || view == nil || !view.Present || view.Suite != "binary64-normal-exact-v1" {
			t.Fatalf("valid CPU rejected: %v", err)
		}
		view.ChecksPassed++
		again, err := ValidatedCPUScalar(payload)
		if err != nil || again.ChecksPassed == view.ChecksPassed {
			t.Fatal("mutable summary shared")
		}
	}
	for _, name := range []string{"session-v4-local-heat", "session-v5-keyboard-pass", "session-v5-pointer-v2-pass"} {
		if view, err := ValidatedCPUScalar(thermalFixture(t, name)); err != nil || view != nil {
			t.Fatalf("historical CPU reinterpreted: %v", err)
		}
	}
}

func TestValidatedCPURejectsVersionAndHiddenClaimDowngrades(t *testing.T) {
	for name, change := range map[string]func(*InventoryPayload){
		"definition one":     func(p *InventoryPayload) { p.TestResults[0].Version = 1 },
		"definition unknown": func(p *InventoryPayload) { p.TestResults[0].Version = 3 },
		"wrong row":          func(p *InventoryPayload) { p.TestResults[0].ID = "memory.quick" },
		"aliased row":        func(p *InventoryPayload) { p.TestResults[0].ID = "CPU.QUICK" },
		"no coverage":        func(p *InventoryPayload) { p.CoverageScope = "" },
		"duplicate row":      func(p *InventoryPayload) { p.TestResults = append(p.TestResults, p.TestResults[0]) },
		"old schema with new fields": func(p *InventoryPayload) {
			p.TestResults[0].Evidence = []byte(strings.Replace(string(p.TestResults[0].Evidence), "cpu/v3", "cpu/v2", 1))
		},
		"erased checks": func(p *InventoryPayload) {
			p.TestResults[0].Evidence = []byte(strings.Replace(string(p.TestResults[0].Evidence), `"checks_passed": 12`, `"checks_passed": 0`, 1))
		},
		"status mismatch": func(p *InventoryPayload) { p.TestResults[0].Status = TestInconclusive },
	} {
		t.Run(name, func(t *testing.T) {
			p := thermalFixture(t, "cpu-v3-direct-pass")
			change(&p)
			if ValidateCoverage(p) == nil {
				t.Fatal("CPU confusion accepted")
			}
		})
	}
	old := thermalFixture(t, "session-v4-local-heat")
	for index := range old.TestResults {
		if old.TestResults[index].ID == "cpu.quick" {
			old.TestResults[index].Version = 2
		}
	}
	if ValidateCoverage(old) == nil {
		t.Fatal("v4 accepted current definition")
	}
	current := thermalFixture(t, "session-v5-cpu-v3-local-heat")
	mutateIntegrity(t, &current, func(v *sessionevidence.IntegrityEvidence) {
		v.Session.Schema = sessionevidence.SchemaV4
		v.Session.Workers = append(v.Session.Workers[:4], v.Session.Workers[5:]...)
	})
	if ValidateCoverage(current) == nil {
		t.Fatal("v3 scalar source downgraded to v4")
	}
}

func TestCPUClaimSelectionSeesAliasesAndDuplicates(t *testing.T) {
	for _, raw := range []string{
		`{"id":"cpu.quick","version":2,"version":1}`,
		`{"ID":"CPU.QUICK","ver\u0073ion":2}`,
		`{"schema":"hhsoft.pc-tester.cpu/v3","schema":"historical"}`,
		`{"SCHEMA":"HHSOFT.PC-TESTER.CPU/V3"}`,
		`{"sc\u0061lar":{}}`, `{"calculation_failures":{}}`, `{"scalar":`,
	} {
		if !HasCPUScalarClaim([]byte(raw)) {
			t.Fatal("new claim hidden")
		}
	}
	for _, raw := range []string{`{"id":"cpu.quick","version":1}`, `{"id":"cpu.quick","state":"completed"}`, `{"schema":"hhsoft.pc-tester.cpu/v2"}`} {
		if HasCPUScalarClaim([]byte(raw)) {
			t.Fatal("historical CPU acquired current semantics")
		}
	}
	// Current nonexecuted notes remain valid with no invented scalar coverage.
	p := thermalFixture(t, "session-v5-keyboard-pass")
	for index := range p.TestResults {
		if p.TestResults[index].ID == "cpu.quick" {
			p.TestResults[index].Version = 2
		}
	}
	if ValidateCoverage(p) != nil {
		t.Fatal("valid current excluded note rejected")
	}
	for index := range p.TestResults {
		if p.TestResults[index].ID == "cpu.quick" {
			var note map[string]any
			_ = json.Unmarshal(p.TestResults[index].Evidence, &note)
			note["detail"] = "hhsoft.pc-tester.cpu/v3"
			p.TestResults[index].Evidence, _ = json.Marshal(note)
		}
	}
	if ValidateCoverage(p) == nil {
		t.Fatal("CPU claim smuggled through note")
	}
}
