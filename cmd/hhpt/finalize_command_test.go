package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func TestFinalizeBuildsSelfIssuedReportFromExistingEvidence(t *testing.T) {
	directory := t.TempDir()
	fingerprintKey := bytes.Repeat([]byte{0x21}, 32)
	fingerprintPath := writeTestKey(t, directory, "fingerprint.key", fingerprintKey)
	cpuEvidence := []byte(`{"schema":"hhsoft.pc-tester.cpu/v1","status":"pass","iterations":42}`)
	if err := os.WriteFile(filepath.Join(directory, "cpu.json"), cpuEvidence, 0o600); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(directory, "session.hhpcr")
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentSystem, "system0", map[string]string{"product_uuid": "private-value"}),
		inventory.NewComponent(inventory.ComponentCPU, "cpu:0", map[string]string{"serial": "cpu-fixture-1"}),
	})
	planPath := writeFinalizeTestPlan(t, directory, snapshot, fingerprintKey)
	arguments := []string{"-fingerprint-key", fingerprintPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "session-1", "-output", reportPath,
		"-outcome", "audio.readiness.quick=not_present", "-outcome", "cpu.quick=pass", "-outcome", "display.operator.quick=not_present", "-outcome", "graphics.quick=not_present", "-outcome", "input.readiness.quick=not_present",
		"-outcome", "memory.quick=not_present", "-outcome", "network.readiness.quick=not_present", "-outcome", "storage.readiness.quick=not_present", "-outcome", "usb.readiness.quick=not_present"}
	collect := func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }
	random := bytes.NewReader(bytes.Repeat([]byte{0x31}, sessionIDLength+32))
	var stdout, stderr bytes.Buffer
	if result := runFinalizeWith(context.Background(), arguments, &stdout, &stderr, collect, random, func() time.Time { return time.Unix(10, 20) }); result != 0 {
		t.Fatalf("finalize returned %d: %s", result, stderr.String())
	}
	if !strings.Contains(stdout.String(), "reduced identity assurance") {
		t.Fatalf("missing assurance label: %q", stdout.String())
	}
	reportFile, err := os.Open(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := reportFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := bundle.VerifySelfIssued(reportFile, metadata.Size())
	_ = reportFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	var payload app.InventoryPayload
	if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.TestResults) != len(quickResultDefinitions)+1 || payload.TestResults[0].Status != app.TestSkip || payload.TestResults[1].Status != app.TestPass || payload.TestResults[2].Status != app.TestSkip || payload.TestResults[7].ID != "run.integrity.quick" || payload.TestResults[7].Status != app.TestPass || payload.TestResults[9].Status != app.TestSkip {
		t.Fatalf("unexpected results: %+v", payload.TestResults)
	}
	if !bytes.Equal(payload.TestResults[1].Evidence, cpuEvidence) || payload.TestResults[1].PlanID == "" || payload.TestResults[1].HardwareGraph == "" || !bytes.Contains(payload.TestResults[3].Evidence, []byte(`"source_status":"not_present"`)) || bytes.Contains(artifact.InventoryJSON, []byte("private-value")) {
		t.Fatal("evidence was changed, synthetic provenance omitted, or identity exposed")
	}
	stdout.Reset()
	stderr.Reset()
	if result := runVerify([]string{"-self-issued", reportPath}, &stdout, &stderr); result != 0 || !strings.Contains(stdout.String(), "does not prove operator identity") {
		t.Fatalf("self-issued verification failed: result=%d stdout=%q stderr=%q", result, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if result := runCompare([]string{"-self-issued", reportPath, reportPath}, &stdout, &stderr); result != 0 || !bytes.Contains(stdout.Bytes(), []byte(`"status": "unchanged"`)) {
		t.Fatalf("self-issued comparison failed: result=%d stdout=%q stderr=%q", result, stdout.String(), stderr.String())
	}
}

func TestFinalizeRejectsEvidenceOutcomeMismatch(t *testing.T) {
	directory := t.TempDir()
	fingerprintKey := bytes.Repeat([]byte{0x21}, 32)
	fingerprintPath := writeTestKey(t, directory, "fingerprint.key", fingerprintKey)
	if err := os.WriteFile(filepath.Join(directory, "cpu.json"), []byte(`{"schema":"hhsoft.pc-tester.cpu/v1","status":"fail"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(directory, "session.hhpcr")
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil)})
	planPath := writeFinalizeTestPlan(t, directory, snapshot, fingerprintKey)
	arguments := []string{"-fingerprint-key", fingerprintPath, "-plan", planPath, "-evidence-dir", directory, "-output", reportPath,
		"-outcome", "audio.readiness.quick=not_present", "-outcome", "cpu.quick=pass", "-outcome", "display.operator.quick=not_present", "-outcome", "graphics.quick=not_present", "-outcome", "input.readiness.quick=not_present", "-outcome", "memory.quick=not_present",
		"-outcome", "network.readiness.quick=not_present", "-outcome", "storage.readiness.quick=not_present", "-outcome", "usb.readiness.quick=not_present"}
	called := false
	collect := func(context.Context) (inventory.Snapshot, error) { called = true; return inventory.Snapshot{}, nil }
	if result := runFinalizeWith(context.Background(), arguments, bytes.NewBuffer(nil), bytes.NewBuffer(nil), collect, bytes.NewReader(bytes.Repeat([]byte{1}, 48)), time.Now); result != 1 {
		t.Fatalf("finalize returned %d", result)
	}
	if called {
		t.Fatal("inventory collected after contradictory evidence")
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatal("report exists after rejected finalization")
	}
}

func TestFinalizeRecordsHotPlugAgainstFrozenPlan(t *testing.T) {
	directory := t.TempDir()
	fingerprintKey := bytes.Repeat([]byte{0x41}, 32)
	fingerprintPath := writeTestKey(t, directory, "fingerprint.key", fingerprintKey)
	initial := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"kind": "device", "model": "Disk A"}),
	})
	final := inventory.NewSnapshot(append(initial.Components,
		inventory.NewComponent(inventory.ComponentUSB, "usb:1-2", map[string]string{"vendor_id": "0001"}),
	))
	planPath := writeFinalizeTestPlan(t, directory, initial, fingerprintKey)
	if err := os.WriteFile(filepath.Join(directory, "storage.json"), []byte(`{"schema":"hhsoft.pc-tester.storage-readiness/v1","status":"pass"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(directory, "session.hhpcr")
	arguments := []string{"-fingerprint-key", fingerprintPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "session-2", "-output", reportPath,
		"-outcome", "audio.readiness.quick=not_present", "-outcome", "cpu.quick=not_present", "-outcome", "display.operator.quick=not_present", "-outcome", "graphics.quick=not_present", "-outcome", "input.readiness.quick=not_present", "-outcome", "memory.quick=not_present",
		"-outcome", "network.readiness.quick=not_present", "-outcome", "storage.readiness.quick=pass", "-outcome", "usb.readiness.quick=not_present"}
	collect := func(context.Context) (inventory.Snapshot, error) { return final, nil }
	var stdout, stderr bytes.Buffer
	if result := runFinalizeWith(context.Background(), arguments, &stdout, &stderr, collect, bytes.NewReader(bytes.Repeat([]byte{0x51}, 48)), time.Now); result != 0 {
		t.Fatalf("finalize returned %d: %s", result, stderr.String())
	}
	if !strings.Contains(stdout.String(), "HHPT_RUN_INTEGRITY status=fail") {
		t.Fatalf("missing hot-plug marker: %q", stdout.String())
	}
	reportFile, err := os.Open(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := reportFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := bundle.VerifySelfIssued(reportFile, metadata.Size())
	_ = reportFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(artifact.InventoryJSON, []byte(`"status":"fail"`)) || !bytes.Contains(artifact.InventoryJSON, []byte(`"id":"usb:1-2"`)) {
		t.Fatalf("signed report omitted hot-plug evidence: %s", artifact.InventoryJSON)
	}
}

func TestFinalizeRejectsPassForNonScheduledTest(t *testing.T) {
	directory := t.TempDir()
	fingerprintKey := bytes.Repeat([]byte{0x61}, 32)
	fingerprintPath := writeTestKey(t, directory, "fingerprint.key", fingerprintKey)
	planPath := writeFinalizeTestPlan(t, directory, inventory.NewSnapshot(nil), fingerprintKey)
	reportPath := filepath.Join(directory, "session.hhpcr")
	arguments := []string{"-fingerprint-key", fingerprintPath, "-plan", planPath, "-evidence-dir", directory, "-output", reportPath,
		"-outcome", "audio.readiness.quick=pass", "-outcome", "cpu.quick=not_present", "-outcome", "display.operator.quick=not_present", "-outcome", "graphics.quick=not_present", "-outcome", "input.readiness.quick=not_present",
		"-outcome", "memory.quick=not_present", "-outcome", "network.readiness.quick=not_present", "-outcome", "storage.readiness.quick=not_present", "-outcome", "usb.readiness.quick=not_present"}
	if result := runFinalizeWith(context.Background(), arguments, bytes.NewBuffer(nil), bytes.NewBuffer(nil), func(context.Context) (inventory.Snapshot, error) { return inventory.Snapshot{}, nil }, bytes.NewReader(bytes.Repeat([]byte{1}, 48)), time.Now); result != 1 {
		t.Fatalf("finalize returned %d", result)
	}
}

func TestFinalizeRecordsWorkerErrorWhenEvidenceIsIncomplete(t *testing.T) {
	directory := t.TempDir()
	fingerprintKey := bytes.Repeat([]byte{0x71}, 32)
	fingerprintPath := writeTestKey(t, directory, "fingerprint.key", fingerprintKey)
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentSound, "sound:card0", map[string]string{"interface": "card0"})})
	planPath := writeFinalizeTestPlan(t, directory, snapshot, fingerprintKey)
	if err := os.WriteFile(filepath.Join(directory, "audio.json"), []byte(`{"schema":"hhsoft.pc-tester.audio-readiness/v1"`), 0o600); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(directory, "session.hhpcr")
	arguments := []string{"-fingerprint-key", fingerprintPath, "-plan", planPath, "-evidence-dir", directory, "-output", reportPath,
		"-outcome", "audio.readiness.quick=error", "-outcome", "cpu.quick=not_present", "-outcome", "display.operator.quick=not_present", "-outcome", "graphics.quick=not_present", "-outcome", "input.readiness.quick=not_present",
		"-outcome", "memory.quick=not_present", "-outcome", "network.readiness.quick=not_present", "-outcome", "storage.readiness.quick=not_present", "-outcome", "usb.readiness.quick=not_present"}
	if result := runFinalizeWith(context.Background(), arguments, bytes.NewBuffer(nil), bytes.NewBuffer(nil), func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{1}, 48)), time.Now); result != 0 {
		t.Fatalf("finalize returned %d", result)
	}
	reportFile, err := os.Open(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := reportFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := bundle.VerifySelfIssued(reportFile, metadata.Size())
	_ = reportFile.Close()
	if err != nil || !bytes.Contains(artifact.InventoryJSON, []byte(`"audio.readiness.quick"`)) || !bytes.Contains(artifact.InventoryJSON, []byte(`"source_status":"error"`)) {
		t.Fatal("signed report omitted the worker evidence failure")
	}
}

func writeFinalizeTestPlan(t *testing.T, directory string, snapshot inventory.Snapshot, fingerprintKey []byte) string {
	t.Helper()
	frozenPlan, err := plan.Build(snapshot, plan.ProfileQuick, "x86_64", fingerprintKey, "ephemeral-session")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(frozenPlan)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "plan.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
