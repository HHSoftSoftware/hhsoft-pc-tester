package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func TestFinalizePreservesAllSelectionIntentAndHotPlugEvidence(t *testing.T) {
	for _, hotPlug := range []bool{false, true} {
		name, wantIntegrity := "unchanged", app.TestPass
		if hotPlug {
			name, wantIntegrity = "hot-plug", app.TestFail
		}
		t.Run(name, func(t *testing.T) {
			arguments, snapshot, frozen, reportPath := selectedCPUFinalization(t, "excluded")
			if hotPlug {
				snapshot = inventory.NewSnapshot(append(snapshot.Components, inventory.NewComponent(inventory.ComponentUSB, "usb:new", nil)))
			}
			collect := func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }
			var stdout, stderr bytes.Buffer
			if code := runFinalizeTestWith(t, t.Context(), arguments, &stdout, &stderr, collect, bytes.NewReader(bytes.Repeat([]byte{0x51}, 64)), time.Now); code != 0 {
				t.Fatalf("selected plan could not be reconstructed: %d %s", code, stderr.String())
			}
			encoded, err := os.ReadFile(reportPath)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
			if err != nil {
				t.Fatal(err)
			}
			var payload app.InventoryPayload
			if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.CoverageScope != app.CoverageFrozenPlan || len(payload.TestResults) != len(frozen.Tests)+1 {
				t.Fatal("selected or hot-plug report lost complete frozen coverage")
			}
			planned := make(map[string]plan.TestStatus, len(frozen.Tests))
			for _, row := range frozen.Tests {
				planned[row.ID] = row.Status
			}
			cpuFound, usbFound, integrityFound := false, false, false
			for _, result := range payload.TestResults {
				if result.PlanID != frozen.ID || result.HardwareGraph != frozen.HardwareGraph.Value {
					t.Fatal("signed result lost the selected frozen plan binding")
				}
				if result.ID != "run.integrity.quick" && string(result.PlannedStatus) != string(planned[result.ID]) {
					t.Fatal("hot-plug or selection replaced a frozen planned disposition")
				}
				switch result.ID {
				case "cpu.quick":
					cpuFound = true
					if result.Status != app.TestSkip || !bytes.Contains(result.Evidence, []byte(`"source_status":"excluded"`)) {
						t.Fatal("excluded CPU became a pass or lost its exclusion reason")
					}
				case "usb.readiness.quick":
					usbFound = true
					if result.Status != app.TestSkip || !bytes.Contains(result.Evidence, []byte(`"source_status":"not_present"`)) {
						t.Fatal("initially absent selected USB test lost its applicability reason")
					}
				case "run.integrity.quick":
					integrityFound = true
					var evidence struct {
						ExcludedTests []string                    `json:"excluded_tests"`
						Added         []plan.HardwareComponentRef `json:"added_components"`
					}
					if err := json.Unmarshal(result.Evidence, &evidence); err != nil {
						t.Fatal(err)
					}
					if result.Status != wantIntegrity || !slices.Equal(evidence.ExcludedTests, []string{"cpu.quick", "inspection.exterior.quick", "usb.readiness.quick"}) || hotPlug && (len(evidence.Added) != 1 || evidence.Added[0].ID != "usb:new") {
						t.Fatalf("signed integrity omitted selection or hardware change: %+v", evidence)
					}
				}
			}
			if !cpuFound || !usbFound || !integrityFound {
				t.Fatal("signed report omitted an excluded, absent, or integrity record")
			}
		})
	}
}

func TestFinalizeRejectsPassForOperatorExcludedTest(t *testing.T) {
	arguments, snapshot, _, reportPath := selectedCPUFinalization(t, "pass")
	collected := false
	collect := func(context.Context) (inventory.Snapshot, error) { collected = true; return snapshot, nil }
	var stdout, stderr bytes.Buffer
	if code := runFinalizeTestWith(t, t.Context(), arguments, &stdout, &stderr, collect, bytes.NewReader(nil), time.Now); code != 1 || collected || !strings.Contains(stderr.String(), "non-scheduled plan status excluded") {
		t.Fatalf("operator exclusion was reported as pass: %d collected=%t %s", code, collected, stderr.String())
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatal("report exists after an excluded-test pass")
	}
}

func TestFinalizeRejectsStaleEvidenceForOperatorExcludedTest(t *testing.T) {
	arguments, snapshot, _, reportPath := selectedCPUFinalization(t, "excluded")
	if err := os.WriteFile(filepath.Join(filepath.Dir(reportPath), "cpu.json"), encodeEvidenceFixture(t, completeCPUEvidence()), 0o600); err != nil {
		t.Fatal(err)
	}
	collected := false
	collect := func(context.Context) (inventory.Snapshot, error) { collected = true; return snapshot, nil }
	var stdout, stderr bytes.Buffer
	if code := runFinalizeTestWith(t, t.Context(), arguments, &stdout, &stderr, collect, bytes.NewReader(nil), time.Now); code != 1 || collected || !strings.Contains(stderr.String(), "evidence exists for a non-scheduled test") {
		t.Fatalf("excluded test accepted stale worker evidence: %d collected=%t %s", code, collected, stderr.String())
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatal("report exists after stale excluded-test evidence")
	}
}

func selectedCPUFinalization(t *testing.T, cpuOutcome string) ([]string, inventory.Snapshot, plan.Plan, string) {
	t.Helper()
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x45}, 32)
	keyPath := writeTestKey(t, directory, "fingerprint.key", key)
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil)})
	frozen, err := plan.BuildWithSelection(snapshot, plan.ProfileQuick, "x86_64", key, "ephemeral-session", []string{"cpu.quick", "inspection.exterior.quick", "usb.readiness.quick"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(directory, "plan.json")
	if err := os.WriteFile(planPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(directory, "selected.hhpcr")
	arguments := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "selected-fixture", "-output", reportPath}
	for _, definition := range currentResultDefinitionsForTest(t, frozen) {
		status := "not_present"
		if definition.id == "inspection.exterior.quick" {
			status = "excluded"
		}
		if definition.id == "cpu.quick" {
			status = cpuOutcome
		}
		arguments = append(arguments, "-outcome", definition.id+"="+status)
	}
	return arguments, snapshot, frozen, reportPath
}
