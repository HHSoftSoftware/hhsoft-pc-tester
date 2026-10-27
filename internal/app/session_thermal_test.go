package app

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func thermalFixture(t *testing.T, name string) InventoryPayload {
	t.Helper()
	encoded, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal("synthetic thermal report fixture unavailable")
	}
	var payload InventoryPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func mutateIntegrity(t *testing.T, payload *InventoryPayload, change func(*sessionevidence.IntegrityEvidence)) {
	t.Helper()
	index := len(payload.TestResults) - 1
	value, err := sessionevidence.DecodeIntegrity(payload.TestResults[index].Evidence)
	if err != nil {
		t.Fatal(err)
	}
	change(&value)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	payload.TestResults[index].Evidence = encoded
}

func TestValidatedSessionThermalShowsGapsAndIndependentHeatSources(t *testing.T) {
	for _, fixture := range []struct {
		name, coverage, finding string
		peak                    int
		observed                bool
		local                   int
	}{
		{"session-v4-unavailable", "unavailable", "no_limit_observed", 0, false, 0},
		{"session-v4-global-heat", "available", "limit_reached", 95000, true, 0},
		{"session-v4-local-heat", "available", "limit_reached", 42000, true, 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			payload := thermalFixture(t, fixture.name)
			before, _ := json.Marshal(payload)
			if err := ValidateCoverage(payload); err != nil {
				t.Fatal(err)
			}
			view, err := ValidatedSessionThermal(payload)
			if err != nil || view == nil {
				t.Fatalf("valid thermal report rejected: %v", err)
			}
			if view.Coverage != fixture.coverage || view.Finding != fixture.finding ||
				view.PeakObserved != fixture.observed || view.PeakMilliCelsius != fixture.peak ||
				view.AcceptedObservations != 2 || view.MaximumGapMilliseconds != 21 ||
				view.LimitMilliCelsius != 90000 || !view.CleanupConfirmed || len(view.LocalSources) != fixture.local ||
				len(payload.TestResults) != 22 || view.Scope != sessionevidence.Scope {
				t.Fatal("thermal view changed coverage, heat provenance or report rows")
			}
			if fixture.local != 0 {
				if view.LocalSources[0].PeakMilliCelsius != 95125 || view.LocalSources[0].EvidenceTypedSHA256 !=
					"sha256:d0ade46bdda5d5cde8647afd1a1b93c01d51346d10ffcc28cdf71c91688e46d9" {
					t.Fatal("local heat did not retain its independently encoded typed digest")
				}
				view.LocalSources[0].PeakMilliCelsius = 1
				again, err := ValidatedSessionThermal(payload)
				if err != nil || again.LocalSources[0].PeakMilliCelsius != 95125 {
					t.Fatal("returned view shares mutable source storage")
				}
			}
			after, _ := json.Marshal(payload)
			if !bytes.Equal(before, after) {
				t.Fatal("thermal validation mutated signed evidence")
			}
		})
	}
	view, err := ValidatedSessionThermal(thermalFixture(t, "historical-session-v3"))
	if err != nil || view != nil {
		t.Fatalf("historical journal acquired new interpretation: %v", err)
	}
}

func TestSessionThermalPreservesHistoricalAbsentEvidence(t *testing.T) {
	payload := InventoryPayload{TestResults: []TestResult{{ID: "cpu.quick", Version: 1, Status: TestSkip}}}
	if err := ValidateCoverage(payload); err != nil {
		t.Fatalf("historical absent evidence rejected: %v", err)
	}
	if view, err := ValidatedSessionThermal(payload); err != nil || view != nil {
		t.Fatal("absent historical evidence became a thermal claim")
	}
	payload.TestResults[0].Evidence = []byte(`{"session":{"thermal":`)
	if _, err := ValidatedSessionThermal(payload); err == nil {
		t.Fatal("malformed present thermal evidence was treated as absent")
	}
}

func TestSessionThermalRejectsUnboundOrUnsignableClaims(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		change func(*testing.T, *InventoryPayload)
	}{
		{"scope absent", func(t *testing.T, p *InventoryPayload) { p.CoverageScope = "" }},
		{"direct capture", func(t *testing.T, p *InventoryPayload) { p.CoverageScope = CoverageDirectCapture }},
		{"wrong result ID", func(t *testing.T, p *InventoryPayload) { p.TestResults[len(p.TestResults)-1].ID = "thermal.quick" }},
		{"duplicate integrity", func(t *testing.T, p *InventoryPayload) {
			p.TestResults = append(p.TestResults, p.TestResults[len(p.TestResults)-1])
		}},
		{"wrong result version", func(t *testing.T, p *InventoryPayload) { p.TestResults[len(p.TestResults)-1].Version = 2 }},
		{"changed graph outcome", func(t *testing.T, p *InventoryPayload) { p.TestResults[len(p.TestResults)-1].Status = TestFail }},
		{"missing executed row", func(t *testing.T, p *InventoryPayload) {
			for index, row := range p.TestResults {
				if row.ID == "cpu.quick" {
					p.TestResults = append(p.TestResults[:index], p.TestResults[index+1:]...)
					break
				}
			}
		}},
		{"changed plan", func(t *testing.T, p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) { v.PlanID = "sha256:" + strings.Repeat("9", 64) })
		}},
		{"missing final phase", func(t *testing.T, p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.Thermal.Final.Completed = false })
		}},
		{"unconfirmed join", func(t *testing.T, p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.Thermal.Joined = false })
		}},
		{"missing source", func(t *testing.T, p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) {
				v.Session.LocalThermalSources = []sessionevidence.LocalThermalSource{}
				v.Session.StopReason = "completed"
				v.Session.Workers[0].State = "completed"
			})
		}},
		{"duplicate source", func(t *testing.T, p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) {
				v.Session.LocalThermalSources = append(v.Session.LocalThermalSources, v.Session.LocalThermalSources[0])
			})
		}},
		{"changed typed hash", func(t *testing.T, p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) {
				v.Session.LocalThermalSources[0].EvidenceTypedSHA256 = "sha256:" + strings.Repeat("0", 64)
			})
		}},
		{"invalid file hash", func(t *testing.T, p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) {
				v.Session.LocalThermalSources[0].EvidenceFileSHA256 = "lost"
			})
		}},
		{"changed peak", func(t *testing.T, p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.LocalThermalSources[0].PeakMilliCelsius++ })
		}},
		{"changed threshold", func(t *testing.T, p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.LocalThermalSources[0].LimitMilliCelsius++ })
		}},
		{"changed interval", func(t *testing.T, p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.LocalThermalSources[0].FinishedMilliseconds++ })
		}},
		{"null source slice", func(t *testing.T, p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.LocalThermalSources = nil })
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			payload := thermalFixture(t, "session-v4-local-heat")
			fixture.change(t, &payload)
			if ValidateCoverage(payload) == nil {
				t.Fatal("invalid thermal claim passed coverage validation")
			}
			if view, err := ValidatedSessionThermal(payload); err == nil || view != nil {
				t.Fatal("invalid thermal claim produced a display view")
			}
		})
	}
}

func TestLocalThermalTypedBindingUsesFullEvidenceAndRejectsAliases(t *testing.T) {
	for _, fixture := range []struct {
		name, old, replacement string
	}{
		{"changed nonthermal field", `"workers": 1`, `"workers": 2`},
		{"duplicate field", `"duration_ms": 3`, `"duration_ms": 9, "duration_ms": 3`},
		{"escaped duplicate", `"duration_ms": 3`, `"duration_\u006ds": 9, "duration_ms": 3`},
		{"case alias", `"safety":`, `"Safety":`},
		{"unicode alias", `"safety":`, `"Å¿afety":`},
		{"unknown field", `"safety":`, `"extra":true,"safety":`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			payload := thermalFixture(t, "session-v4-local-heat")
			found := false
			for index := range payload.TestResults {
				if payload.TestResults[index].ID == "cpu.quick" {
					raw := payload.TestResults[index].Evidence
					if !bytes.Contains(raw, []byte(fixture.old)) {
						t.Fatal("fixture mutation did not target evidence")
					}
					payload.TestResults[index].Evidence = bytes.Replace(raw, []byte(fixture.old), []byte(fixture.replacement), 1)
					found = true
				}
			}
			if !found || ValidateCoverage(payload) == nil {
				t.Fatal("changed worker evidence retained its old heat binding")
			}
		})
	}
	// The original file digest cannot be reconstructed from report whitespace.
	// The typed digest deliberately survives reserialization of the full value.
	payload := thermalFixture(t, "session-v4-local-heat")
	for index := range payload.TestResults {
		if payload.TestResults[index].ID == "cpu.quick" {
			var compact bytes.Buffer
			if err := json.Compact(&compact, payload.TestResults[index].Evidence); err != nil {
				t.Fatal(err)
			}
			payload.TestResults[index].Evidence = compact.Bytes()
		}
	}
	if err := ValidateCoverage(payload); err != nil {
		t.Fatal(err)
	}
}

func TestSessionThermalRejectsOversizedNestedJournal(t *testing.T) {
	payload := thermalFixture(t, "session-v4-unavailable")
	index := len(payload.TestResults) - 1
	needle := []byte(`"session": {`)
	if !bytes.Contains(payload.TestResults[index].Evidence, needle) {
		t.Fatal("nested journal fixture not found")
	}
	payload.TestResults[index].Evidence = bytes.Replace(payload.TestResults[index].Evidence, needle,
		append(append([]byte(nil), needle...), bytes.Repeat([]byte(" "), 64<<10)...), 1)
	if ValidateCoverage(payload) == nil {
		t.Fatal("nested journal escaped the shared encoded size bound")
	}
}

func TestSessionThermalCannotLoseLocalHeatBehindPreservedFailure(t *testing.T) {
	payload := thermalFixture(t, "session-v4-local-heat")
	mutateIntegrity(t, &payload, func(value *sessionevidence.IntegrityEvidence) {
		value.Session.LocalThermalSources = []sessionevidence.LocalThermalSource{}
		value.Session.StopReason = "worker_stop"
		value.Session.Workers[0].State, value.Session.Workers[0].Outcome = "completed", "fail"
	})
	for index := range payload.TestResults {
		if payload.TestResults[index].ID == "cpu.quick" {
			payload.TestResults[index].Status = TestFail
			payload.TestResults[index].Evidence = bytes.Replace(payload.TestResults[index].Evidence,
				[]byte(`"status": "aborted"`), []byte(`"status": "fail"`), 1)
		}
	}
	if _, err := sessionevidence.DecodeIntegrity(payload.TestResults[len(payload.TestResults)-1].Evidence); err != nil {
		t.Fatalf("fixture must reach worker binding validation: %v", err)
	}
	if ValidateCoverage(payload) == nil {
		t.Fatal("preserved worker failure hid an omitted thermal source")
	}
}
