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

func TestFinalizeAccountsForEveryFrozenTestWithoutInventingExecution(t *testing.T) {
	for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
		for _, partial := range []bool{false, true} {
			name := architecture + "/all-selected-off"
			if partial {
				name = architecture + "/missing-hardware-and-driver"
			}
			t.Run(name, func(t *testing.T) {
				directory := t.TempDir()
				key := bytes.Repeat([]byte{0x46}, 32)
				keyPath := writeTestKey(t, directory, "fingerprint.key", key)
				components := []inventory.Component{
					inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil),
					inventory.NewComponent(inventory.ComponentPCI, "pci:graphics", map[string]string{"class": "0x030000"}),
				}
				if !partial {
					components = append(components,
						inventory.NewComponent(inventory.ComponentMemory, "memory:system", nil),
						inventory.NewComponent(inventory.ComponentBlock, "block:test", map[string]string{"kind": "device"}),
						inventory.NewComponent(inventory.ComponentNetwork, "network:test", map[string]string{"name": "eth0"}),
						inventory.NewComponent(inventory.ComponentSound, "sound:test", nil),
						inventory.NewComponent(inventory.ComponentUSB, "usb:test", nil),
						inventory.NewComponent(inventory.ComponentVideo, "video:camera", map[string]string{"category": "video_capture"}),
						inventory.NewComponent(inventory.ComponentVideo, "video:controller", map[string]string{"category": "display_controller"}),
						inventory.NewComponent(inventory.ComponentVideo, "video:connector", map[string]string{"category": "display_connector"}),
					)
					for _, category := range []string{"keyboard", "pointer", "touchpad", "touchscreen"} {
						components = append(components, inventory.NewComponent(inventory.ComponentInput, "input:"+category, map[string]string{"category": category}))
					}
				}
				snapshot := inventory.NewSnapshot(components)
				excluded, err := plan.SelectableQuickTests(architecture)
				if err != nil {
					t.Fatal(err)
				}
				frozen, err := plan.BuildWithSelection(snapshot, plan.ProfileQuick, architecture, key, "ephemeral-session", excluded)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(frozen)
				if err != nil {
					t.Fatal(err)
				}
				planPath, reportPath := filepath.Join(directory, "plan.json"), filepath.Join(directory, "report.hhpcr")
				if err := os.WriteFile(planPath, encoded, 0o600); err != nil {
					t.Fatal(err)
				}
				arguments := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "coverage-fixture", "-output", reportPath}
				planned := make(map[string]plan.Test)
				for _, test := range frozen.Tests {
					planned[test.ID] = test
				}
				for _, worker := range currentResultDefinitionsForTest(t, frozen) {
					arguments = append(arguments, "-outcome", worker.id+"="+string(planned[worker.id].Status))
				}
				var stdout, stderr bytes.Buffer
				if code := runFinalizeTestWith(t, t.Context(), arguments, &stdout, &stderr,
					func(context.Context) (inventory.Snapshot, error) { return snapshot, nil },
					bytes.NewReader(bytes.Repeat([]byte{0x39}, 32)), time.Now); code != 0 {
					t.Fatalf("finalize returned %d: %s", code, stderr.String())
				}
				reportBytes, err := os.ReadFile(reportPath)
				if err != nil {
					t.Fatal(err)
				}
				artifact, err := bundle.VerifySelfIssued(bytes.NewReader(reportBytes), int64(len(reportBytes)))
				if err != nil {
					t.Fatal(err)
				}
				var payload app.InventoryPayload
				if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
					t.Fatal(err)
				}
				wantIDs := []string{"audio.quick", "audio.readiness.quick", "camera.quick", "cpu.quick", "cpu.stress", "display.operator.quick", "graphics.quick", "input.keyboard.quick", "input.pointer.quick", "input.readiness.quick", "input.touchpad.quick", "input.touchscreen.quick", "inspection.exterior.quick", "memory.quick", "memory.stress", "network.quick", "network.readiness.quick", "run.integrity.quick", "storage.destructive", "storage.quick", "storage.readiness.quick", "usb.quick", "usb.readiness.quick"}
				gotIDs := make([]string, 0, len(payload.TestResults))
				for _, result := range payload.TestResults {
					gotIDs = append(gotIDs, result.ID)
					if result.PlanID != frozen.ID || result.HardwareGraph != frozen.HardwareGraph.Value {
						t.Fatal("coverage row lost frozen binding")
					}
					if result.ID == "run.integrity.quick" {
						if result.Status != app.TestPass || result.PlannedStatus != "" {
							t.Fatal("integrity became a synthetic planned test")
						}
						continue
					}
					row := planned[result.ID]
					if result.Version != row.Version || string(result.PlannedStatus) != string(row.Status) {
						t.Fatal("frozen version or planned status changed")
					}
					var note struct{ Schema, Status, SourceStatus string }
					var fields map[string]string
					if err := json.Unmarshal(result.Evidence, &fields); err != nil {
						t.Fatal(err)
					}
					note.Schema, note.Status, note.SourceStatus = fields["schema"], fields["status"], fields["source_status"]
					wantStatus := app.TestSkip
					if row.Status == plan.TestMissingDriver {
						wantStatus = app.TestInconclusive
					}
					if result.Status != wantStatus || note.Status != string(wantStatus) || note.SourceStatus != string(row.Status) || note.Schema != "hhsoft.pc-tester.execution-note/v1" {
						t.Fatal("unexecuted test invented work or lost its exact disposition")
					}
				}
				if payload.CoverageScope != app.CoverageFrozenPlan || !slices.Equal(gotIDs, wantIDs) {
					t.Fatalf("coverage omitted or duplicated catalog rows: %v", gotIDs)
				}
				if !partial {
					for _, id := range []string{"camera.quick", "audio.quick", "storage.quick", "usb.quick", "network.quick"} {
						if planned[id].Status != plan.TestUnsupported {
							t.Fatalf("fixture failed to expose unsupported %s", id)
						}
					}
				}
				for _, view := range [][]byte{artifact.RenderedReport, artifact.RenderedPDF} {
					if !bytes.Contains(view, []byte("Frozen-plan coverage")) || !bytes.Contains(view, []byte("Planned status")) || !bytes.Contains(view, []byte("camera.quick")) {
						t.Fatal("saved view hid unimplemented coverage")
					}
				}
			})
		}
	}
}

func TestFinalizeRejectsScheduledRowsWithoutExecutionAdapters(t *testing.T) {
	frozen, err := plan.BuildWithSelection(inventory.NewSnapshot(nil), plan.ProfileQuick, "x86_64", make([]byte, 32), "fixture", []string{"inspection.exterior.quick"})
	if err != nil {
		t.Fatal(err)
	}
	outcomes := make(map[string]string)
	definitions := currentResultDefinitionsForTest(t, frozen)
	for _, worker := range definitions {
		outcomes[worker.id] = "not_present"
		if worker.id == "inspection.exterior.quick" {
			outcomes[worker.id] = "excluded"
		}
	}
	if err := validatePlanOutcomesWithDefinitions(frozen, outcomes, definitions); err != nil {
		t.Fatal("valid current outcome prerequisite rejected", err)
	}
	for _, id := range []string{"camera.quick", "future.quick"} {
		candidate := frozen
		candidate.Tests = append([]plan.Test(nil), frozen.Tests...)
		found := false
		for index := range candidate.Tests {
			if candidate.Tests[index].ID == id {
				candidate.Tests[index].Status = plan.TestScheduled
				found = true
			}
		}
		if !found {
			candidate.Tests = append(candidate.Tests, plan.Test{ID: id, Version: 1, Status: plan.TestScheduled})
		}
		if err := validatePlanOutcomesWithDefinitions(candidate, outcomes, definitions); err == nil || !strings.Contains(err.Error(), "no execution adapter") {
			t.Fatalf("uncovered scheduled row accepted: %s %v", id, err)
		}
	}
}

func TestExecutionNoteKeepsFrozenVersionAndReason(t *testing.T) {
	result, err := newExecutionNote(plan.Test{ID: "camera.quick", Version: 7, Status: plan.TestUnsupported}, app.TestSkip, "unsupported")
	if err != nil || result.Version != 7 || result.Status != app.TestSkip || !bytes.Contains(result.Evidence, []byte(`"source_status":"unsupported"`)) {
		t.Fatalf("frozen note changed: %+v %v", result, err)
	}
}
