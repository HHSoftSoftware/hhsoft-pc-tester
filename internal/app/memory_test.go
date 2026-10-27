package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func TestValidatedMemorySharesStrictSummaryAndHistoricalScope(t *testing.T) {
	for _, name := range []string{"memory-v3-direct-pass", "session-v5-memory-v3-local-heat"} {
		p := thermalFixture(t, name)
		view, err := ValidatedMemory(p)
		if err != nil || view == nil || !view.Present || view.Suite != "byte-patterns-aa55-inversions-v1" {
			t.Fatalf("valid RAM rejected: %v", err)
		}
		view.ExaminedBytes++
		again, err := ValidatedMemory(p)
		if err != nil || again.ExaminedBytes == view.ExaminedBytes {
			t.Fatal("mutable summary shared")
		}
	}
	for _, name := range []string{"session-v4-local-heat", "session-v5-keyboard-pass", "session-v5-cpu-v3-local-heat"} {
		if view, err := ValidatedMemory(thermalFixture(t, name)); err != nil || view != nil {
			t.Fatalf("historical memory reinterpreted: %v", err)
		}
	}
}

func TestValidatedMemoryRejectsVersionAndHiddenClaimDowngrades(t *testing.T) {
	for name, change := range map[string]func(*InventoryPayload){
		"definition one":     func(p *InventoryPayload) { p.TestResults[0].Version = 1 },
		"definition unknown": func(p *InventoryPayload) { p.TestResults[0].Version = 3 },
		"wrong row":          func(p *InventoryPayload) { p.TestResults[0].ID = "cpu.quick" },
		"aliased row":        func(p *InventoryPayload) { p.TestResults[0].ID = "MEMORY.QUICK" },
		"no coverage":        func(p *InventoryPayload) { p.CoverageScope = "" },
		"duplicate row":      func(p *InventoryPayload) { p.TestResults = append(p.TestResults, p.TestResults[0]) },
		"old schema with new fields": func(p *InventoryPayload) {
			p.TestResults[0].Evidence = []byte(strings.Replace(string(p.TestResults[0].Evidence), "memory/v3", "memory/v2", 1))
		},
		"erased phase": func(p *InventoryPayload) {
			p.TestResults[0].Evidence = []byte(strings.Replace(string(p.TestResults[0].Evidence), `"completed_phases": 10`, `"completed_phases": 9`, 1))
		},
		"status mismatch": func(p *InventoryPayload) { p.TestResults[0].Status = TestInconclusive },
	} {
		t.Run(name, func(t *testing.T) {
			p := thermalFixture(t, "memory-v3-direct-pass")
			change(&p)
			if ValidateCoverage(p) == nil {
				t.Fatal("memory confusion accepted")
			}
		})
	}
	p := thermalFixture(t, "session-v5-memory-v3-local-heat")
	mutateIntegrity(t, &p, func(v *sessionevidence.IntegrityEvidence) {
		v.Session.Schema = sessionevidence.SchemaV4
		v.Session.Workers = append(v.Session.Workers[:4], v.Session.Workers[5:]...)
	})
	if ValidateCoverage(p) == nil {
		t.Fatal("new memory claim downgraded to v4")
	}
}

func TestMemoryClaimSelectionAndClosedNotes(t *testing.T) {
	for _, raw := range []string{`{"id":"memory.quick","version":2,"version":1}`, `{"ID":"MEMORY.QUICK","ver\u0073ion":2}`, `{"schema":"hhsoft.pc-tester.memory/v3","schema":"historical"}`, `{"SCHEMA":"HHSOFT.PC-TESTER.MEMORY/V3"}`, `{"memory_cove\u0072age":{}}`, `{"memory_coverage":`} {
		if !HasMemoryClaim([]byte(raw)) {
			t.Fatal("new memory claim hidden")
		}
	}
	for _, raw := range []string{`{"id":"memory.quick","version":1}`, `{"id":"memory.quick","state":"completed"}`, `{"schema":"hhsoft.pc-tester.memory/v2"}`} {
		if HasMemoryClaim([]byte(raw)) {
			t.Fatal("historical memory acquired inversion semantics")
		}
	}
	p := thermalFixture(t, "session-v5-keyboard-pass")
	for index := range p.TestResults {
		if p.TestResults[index].ID == "memory.quick" {
			p.TestResults[index].Version = 2
		}
	}
	if err := ValidateCoverage(p); err != nil {
		t.Fatal("current excluded note rejected", err)
	}
	for index := range p.TestResults {
		if p.TestResults[index].ID == "memory.quick" {
			var note map[string]any
			_ = json.Unmarshal(p.TestResults[index].Evidence, &note)
			note["detail"] = "hhsoft.pc-tester.memory/v3"
			p.TestResults[index].Evidence, _ = json.Marshal(note)
		}
	}
	if ValidateCoverage(p) == nil {
		t.Fatal("functional claim hidden in excluded note")
	}
}
