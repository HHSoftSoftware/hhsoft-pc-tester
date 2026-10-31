package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func TestFinalizeRejectsRehashedCapabilityHiddenOnUnchangedHardware(t *testing.T) {
	directory := t.TempDir()
	fingerprintKey := bytes.Repeat([]byte{0x42}, 32)
	fingerprintPath := writeTestKey(t, directory, "fingerprint.key", fingerprintKey)
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentVideo, "video:connector:card0-HDMI-A-1", map[string]string{"category": "display_connector"}),
	})
	frozen, err := plan.BuildWithSelection(snapshot, plan.ProfileQuick, "x86_64", fingerprintKey, "ephemeral-session", []string{"inspection.exterior.quick"})
	if err != nil {
		t.Fatal(err)
	}
	originalID := frozen.ID
	for index := range frozen.Capabilities {
		if frozen.Capabilities[index].ID == "display" {
			if frozen.Capabilities[index].Status != plan.CapabilityPresent {
				t.Fatal("fixture lacks an applicable display")
			}
			frozen.Capabilities[index].Status = plan.CapabilityAbsent
			frozen.Capabilities[index].ComponentIDs = nil
		}
	}
	for index := range frozen.Tests {
		if frozen.Tests[index].ID == "display.operator.quick" {
			if frozen.Tests[index].Status != plan.TestScheduled {
				t.Fatal("fixture did not schedule the display test")
			}
			frozen.Tests[index].Status = plan.TestNotPresent
			frozen.Tests[index].ComponentIDs = nil
		}
	}
	frozen.ID = ""
	unsigned, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(unsigned)
	frozen.ID = fmt.Sprintf("sha256:%x", digest)
	encoded, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.ID == originalID {
		t.Fatal("tampering did not change the plan ID")
	}
	if _, err := plan.Decode(encoded); err != nil {
		t.Fatalf("fixture must reach snapshot-based applicability validation: %v", err)
	}
	planPath := filepath.Join(directory, "plan.json")
	if err := os.WriteFile(planPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(directory, "rejected.hhpcr")
	arguments := absentPlanFinalizationArguments(t, directory, fingerprintPath, planPath, reportPath)
	collected := false
	collect := func(context.Context) (inventory.Snapshot, error) { collected = true; return snapshot, nil }
	random := bytes.NewReader(bytes.Repeat([]byte{0x63}, 64))
	var stdout, stderr bytes.Buffer
	if result := runFinalizeTestWith(t, t.Context(), arguments, &stdout, &stderr, collect, random, time.Now); result != 1 {
		t.Fatalf("hidden display was signed: result=%d stderr=%s", result, stderr.String())
	}
	if !collected || !strings.Contains(stderr.String(), "plan does not match unchanged hardware") {
		t.Fatalf("rejection did not compare the rebuilt plan: collected=%t stderr=%s", collected, stderr.String())
	}
	if random.Len() != 64 || stdout.Len() != 0 {
		t.Fatal("rejected plan reached signing or emitted accepted integrity evidence")
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatal("report artifact exists after a forged applicability rejection")
	}
}

func TestFinalizeRejectsContradictoryPlanJSONBeforeSigning(t *testing.T) {
	for _, contradictory := range []string{
		`"status":"scheduled","status":"not_present"`,
		`"status":"scheduled","STATUS":"not_present"`,
	} {
		t.Run(contradictory, func(t *testing.T) {
			directory := t.TempDir()
			fingerprintKey := bytes.Repeat([]byte{0x43}, 32)
			fingerprintPath := writeTestKey(t, directory, "fingerprint.key", fingerprintKey)
			snapshot := inventory.NewSnapshot(nil)
			planPath := writeFinalizeTestPlan(t, directory, snapshot, fingerprintKey)
			encoded, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			encoded = bytes.Replace(encoded, []byte(`"status":"not_present"`), []byte(contradictory), 1)
			var permissive plan.Plan
			if err := json.Unmarshal(encoded, &permissive); err != nil {
				t.Fatal(err)
			}
			if err := plan.Validate(permissive); err != nil {
				t.Fatalf("fixture must retain a valid plan under permissive decoding: %v", err)
			}
			if err := os.WriteFile(planPath, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			reportPath := filepath.Join(directory, "rejected.hhpcr")
			arguments := absentPlanFinalizationArguments(t, directory, fingerprintPath, planPath, reportPath)
			collected := false
			collect := func(context.Context) (inventory.Snapshot, error) { collected = true; return snapshot, nil }
			random := bytes.NewReader(bytes.Repeat([]byte{0x64}, 64))
			var stdout, stderr bytes.Buffer
			if result := runFinalizeTestWith(t, t.Context(), arguments, &stdout, &stderr, collect, random, time.Now); result != 1 {
				t.Fatalf("contradictory plan was signed: result=%d stderr=%s", result, stderr.String())
			}
			if collected || !strings.Contains(stderr.String(), "validate frozen plan: decode plan:") {
				t.Fatalf("rejection did not occur at plan decoding: collected=%t stderr=%s", collected, stderr.String())
			}
			if random.Len() != 64 || stdout.Len() != 0 {
				t.Fatal("contradictory plan reached signing or emitted accepted integrity evidence")
			}
			if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
				t.Fatal("report artifact exists after contradictory plan rejection")
			}
		})
	}
}

func absentPlanFinalizationArguments(t *testing.T, directory, fingerprintPath, planPath, reportPath string) []string {
	t.Helper()
	// The target may be invalid. Use a separate valid current argument baseline.
	baseline, err := plan.BuildWithSelection(inventory.NewSnapshot(nil), plan.ProfileQuick, "x86_64", make([]byte, 32), "fixture", []string{"inspection.exterior.quick"})
	if err != nil {
		t.Fatal(err)
	}
	arguments := []string{"-fingerprint-key", fingerprintPath, "-plan", planPath, "-evidence-dir", directory,
		"-session-id", "plan-validation-fixture", "-output", reportPath}
	definitions := currentResultDefinitionsForTest(t, baseline)
	if len(definitions) != 14 {
		t.Fatal("current argument baseline has the wrong result count")
	}
	values := []string{}
	for _, definition := range definitions {
		status := "not_present"
		if definition.id == "inspection.exterior.quick" {
			status = "excluded"
		}
		values = append(values, definition.id+"="+status)
		arguments = append(arguments, "-outcome", definition.id+"="+status)
	}
	parsed, err := parseOutcomesWithDefinitions(values, definitions)
	if err != nil || validatePlanOutcomesWithDefinitions(baseline, parsed, definitions) != nil ||
		parsed["inspection.exterior.quick"] != "excluded" || parsed["input.touchpad.quick"] != "not_present" || parsed["input.touchscreen.quick"] != "not_present" {
		t.Fatal("valid independent argument baseline failed outcome admission")
	}
	return arguments
}
