package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

// A supervisor timeout or crash cannot erase a defect the worker already
// recorded. Refuse the contradictory report before collecting or signing it.
func TestFinalizeRejectsWorkerFailureDowngrades(t *testing.T) {
	cpu := completeCPUEvidence()
	cpu.Status, cpu.Detail = quick.StatusFail, "known-answer calculation mismatch"
	memory := completeMemoryEvidence()
	memory.Status, memory.ExaminedBytes, memory.ActualByte = quick.StatusFail, 0, 1
	graphicsResult := completeGraphicsEvidence()
	graphicsResult.Status, graphicsResult.Pattern.Status = graphics.StatusFail, graphics.StatusFail
	for id, evidence := range map[string]any{"cpu.quick": cpu, "memory.quick": memory, "graphics.quick": graphicsResult} {
		for _, outcome := range []string{"pass", "inconclusive", "error", "aborted"} {
			t.Run(id+"/"+outcome, func(t *testing.T) {
				assertFinalizationRejected(t, id, evidence, outcome)
			})
		}
	}
}

func TestMemoryMismatchCannotBeHiddenByWorkerStatus(t *testing.T) {
	for _, offset := range []int64{0, 4095} {
		for _, status := range []quick.Status{quick.StatusPass, quick.StatusInconclusive, quick.StatusError, quick.StatusAborted} {
			t.Run(string(status)+"/offset-"+strconv.FormatInt(offset, 10), func(t *testing.T) {
				memory := completeMemoryEvidence()
				memory.Status, memory.FailureOffset, memory.ExpectedByte, memory.ActualByte = status, offset, 0, 1
				if _, err := newValidatedQuickTestResult("memory.quick", 1, app.TestStatus(status), memory); err == nil {
					t.Fatal("direct capture accepted a memory mismatch without fail")
				}
				assertFinalizationRejected(t, "memory.quick", memory, string(status))
			})
		}
	}
}

func TestGraphicsFailureCannotBeHiddenByWorkerStatus(t *testing.T) {
	for _, source := range []string{"renderer", "pattern"} {
		for _, status := range []graphics.Status{graphics.StatusPass, graphics.StatusInconclusive, graphics.StatusSkip, "error", graphics.StatusAborted} {
			t.Run(source+"/"+string(status), func(t *testing.T) {
				result := completeGraphicsEvidence()
				result.Status = status
				if source == "renderer" {
					result.Renderer.Status = graphics.StatusFail
				} else {
					result.Pattern.Status = graphics.StatusFail
				}
				if _, err := newValidatedQuickTestResult("graphics.quick", 1, app.TestStatus(status), result); err == nil {
					t.Fatal("direct capture accepted a graphics failure without fail")
				}
				assertFinalizationRejected(t, "graphics.quick", result, string(status))
				// An execution failure cannot replace contradictory worker JSON.
				assertFinalizationRejected(t, "graphics.quick", result, "error")
			})
		}
	}
}

func TestFinalizePreservesMatchingQuickFailureAndPartialEvidence(t *testing.T) {
	for _, status := range []quick.Status{quick.StatusFail, quick.StatusError, quick.StatusAborted, quick.StatusInconclusive} {
		cpu := completeCPUEvidence()
		cpu.Status, cpu.Iterations = status, 0
		memory := completeMemoryEvidence()
		memory.Status, memory.ExaminedBytes = status, 0
		if status == quick.StatusFail {
			cpu.Detail = "known-answer calculation mismatch"
			memory.ActualByte = 1 // A mismatch at byte zero is still a failure.
		}
		graphicsResult := completeGraphicsEvidence()
		graphicsResult.Status, graphicsResult.Pattern.Status = graphics.Status(status), graphics.Status(status)
		for id, evidence := range map[string]any{"cpu.quick": cpu, "memory.quick": memory, "graphics.quick": graphicsResult} {
			t.Run(id+"/"+string(status), func(t *testing.T) {
				arguments, snapshot, reportPath := prepareQuickFinalization(t, id, evidence, string(status))
				collect := func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }
				var stderr bytes.Buffer
				if code := runFinalizeWith(t.Context(), arguments, &bytes.Buffer{}, &stderr, collect, bytes.NewReader(bytes.Repeat([]byte{0x37}, 64)), time.Now); code != 0 {
					t.Fatalf("legitimate result rejected: code=%d error=%s", code, stderr.String())
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
				for _, result := range payload.TestResults {
					if result.ID == id {
						if result.Status != app.TestStatus(status) || !bytes.Equal(result.Evidence, encodeEvidenceFixture(t, evidence)) {
							t.Fatal("worker observations or status were replaced")
						}
						return
					}
				}
				t.Fatal("signed report omitted worker evidence")
			})
		}
	}
}

func assertFinalizationRejected(t *testing.T, id string, evidence any, outcome string) {
	t.Helper()
	arguments, snapshot, reportPath := prepareQuickFinalization(t, id, evidence, outcome)
	collected := false
	collect := func(context.Context) (inventory.Snapshot, error) { collected = true; return snapshot, nil }
	var stderr bytes.Buffer
	if code := runFinalizeWith(t.Context(), arguments, &bytes.Buffer{}, &stderr, collect, bytes.NewReader(bytes.Repeat([]byte{0x37}, 64)), time.Now); code != 1 || collected {
		t.Fatalf("failure downgrade reached signing: code=%d collected=%v error=%s", code, collected, stderr.String())
	}
	if !strings.Contains(stderr.String(), "explicit worker failure") {
		t.Fatalf("report rejected for a reason other than preserving the failure: %s", stderr.String())
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatal("report exists after a known-failure downgrade")
	}
}

func prepareQuickFinalization(t *testing.T, id string, evidence any, outcome string) ([]string, inventory.Snapshot, string) {
	t.Helper()
	components := map[string]inventory.Component{
		"cpu.quick":      inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil),
		"memory.quick":   inventory.NewComponent(inventory.ComponentMemory, "memory:system", nil),
		"graphics.quick": inventory.NewComponent(inventory.ComponentVideo, "video:drm:card0", map[string]string{"category": "display_controller"}),
	}
	directory := t.TempDir()
	fingerprintKey := bytes.Repeat([]byte{0x26}, 32)
	fingerprintPath := writeTestKey(t, directory, "fingerprint.key", fingerprintKey)
	snapshot := inventory.NewSnapshot([]inventory.Component{components[id]})
	planPath := writeFinalizeTestPlan(t, directory, snapshot, fingerprintKey)
	reportPath := filepath.Join(directory, "quick.hhpcr")
	arguments := []string{"-fingerprint-key", fingerprintPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "quick-fixture", "-output", reportPath}
	for _, definition := range quickResultDefinitions {
		status := "not_present"
		if definition.id == id {
			status = outcome
			if err := os.WriteFile(filepath.Join(directory, definition.filename), encodeEvidenceFixture(t, evidence), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		arguments = append(arguments, "-outcome", definition.id+"="+status)
	}
	return arguments, snapshot, reportPath
}
