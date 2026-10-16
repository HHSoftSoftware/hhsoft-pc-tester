package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/usbtest"
)

func TestCaptureAndVerifyRoundTrip(t *testing.T) {
	directory := t.TempDir()
	fingerprintKey := bytes.Repeat([]byte{0x23}, 32)
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x45}, ed25519.SeedSize))
	publicKey := privateKey.Public().(ed25519.PublicKey)
	fingerprintKeyPath := writeTestKey(t, directory, "fingerprint.key", fingerprintKey)
	privateKeyPath := writeTestKey(t, directory, "signing.key", privateKey)
	publicKeyPath := writeTestKey(t, directory, "signing.pub", publicKey)
	reportPath := filepath.Join(directory, "capture.hhpcr")
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentSystem, "system0", map[string]string{"product_uuid": "raw-private-uuid", "vendor": "HHSoft Lab"}),
	})
	collectorCalls := 0
	collect := func(context.Context) (inventory.Snapshot, error) {
		collectorCalls++
		return snapshot, nil
	}
	probeCalls := 0
	probe := func(context.Context, int) graphics.Result {
		probeCalls++
		return graphics.Result{Schema: graphics.Schema, Status: graphics.StatusPass}
	}
	probeCPU := func(context.Context, quick.CPUOptions) quick.CPUResult {
		return quick.CPUResult{Schema: quick.CPUSchema, Status: quick.StatusPass, Iterations: 20}
	}
	probeMemory := func(context.Context, quick.MemoryOptions) quick.MemoryResult {
		return quick.MemoryResult{Schema: quick.MemorySchema, Status: quick.StatusPass, ExaminedBytes: 4096}
	}
	probeInput := func(context.Context, inputtest.Options) inputtest.Result {
		return inputtest.Result{Schema: inputtest.Schema, Status: inputtest.StatusPass, Mode: "readiness"}
	}
	probeNetwork := func(context.Context, networktest.Options) networktest.Result {
		return networktest.Result{Schema: networktest.Schema, Status: networktest.StatusPass, HardwareInterfaces: 1, DriverBoundInterfaces: 1}
	}
	probeStorage := func(context.Context, storage.Options) storage.Result {
		return storage.Result{Schema: storage.Schema, Status: storage.StatusPass, EligibleDevices: 1, ReadableDevices: 1}
	}
	probeUSB := func(context.Context, usbtest.Options) usbtest.Result {
		return usbtest.Result{Schema: usbtest.Schema, Status: usbtest.StatusPass, Controllers: 1, AuthorizedDevices: 1, ReadableDevices: 1}
	}
	now := time.Date(2026, time.October, 12, 13, 14, 15, 123456789, time.FixedZone("PDT", -7*60*60))
	arguments := []string{"-fingerprint-key", fingerprintKeyPath, "-key-epoch", "lab-2", "-signing-key", privateKeyPath, "-key-id", "signer-7", "-output", reportPath, "-graphics-pattern-frames", "12"}
	var stdout, stderr bytes.Buffer
	if result := runCaptureWith(context.Background(), arguments, &stdout, &stderr, collect, probe, probeCPU, probeMemory, probeInput, probeNetwork, probeStorage, probeUSB, bytes.NewReader(bytes.Repeat([]byte{0xab}, sessionIDLength)), func() time.Time { return now }); result != 0 {
		t.Fatalf("capture returned %d: %s", result, stderr.String())
	}
	if collectorCalls != 1 || probeCalls != 1 || stdout.Len() != 0 {
		t.Fatalf("collector calls %d, stdout %q", collectorCalls, stdout.String())
	}
	metadata, err := os.Stat(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && metadata.Mode().Perm() != 0o600 {
		t.Fatalf("report permissions are %o", metadata.Mode().Perm())
	}
	source, err := os.Open(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := bundle.Verify(source, metadata.Size(), report.Keyring{"signer-7": publicKey})
	_ = source.Close()
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Envelope.Schema != product.ReportSchema || artifact.Envelope.KeyID != "signer-7" {
		t.Fatalf("unexpected envelope: %+v", artifact.Envelope)
	}
	if artifact.Envelope.SessionID != strings.Repeat("ab", sessionIDLength) {
		t.Fatalf("session ID %q", artifact.Envelope.SessionID)
	}
	if artifact.Envelope.GeneratedTime != "2026-10-12T20:14:15.123456789Z" {
		t.Fatalf("generated time %q", artifact.Envelope.GeneratedTime)
	}
	if bytes.Contains(artifact.InventoryJSON, []byte("raw-private-uuid")) || len(artifact.RenderedReport) == 0 || len(artifact.RenderedPDF) == 0 {
		t.Fatal("capture exposed a raw identifier or omitted a rendered report")
	}
	var payload app.InventoryPayload
	if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.TestResults) != 7 || payload.TestResults[0].ID != "cpu.quick" || payload.TestResults[1].ID != "graphics.quick" || payload.TestResults[2].ID != "input.readiness.quick" || payload.TestResults[3].ID != "memory.quick" || payload.TestResults[4].ID != "network.readiness.quick" || payload.TestResults[5].ID != "storage.readiness.quick" || payload.TestResults[6].ID != "usb.readiness.quick" || payload.TestResults[0].Status != app.TestPass {
		t.Fatalf("test results = %#v", payload.TestResults)
	}

	stdout.Reset()
	stderr.Reset()
	if result := runVerify([]string{"-public-key", publicKeyPath, "-key-id", "signer-7", reportPath}, &stdout, &stderr); result != 0 {
		t.Fatalf("verify returned %d: %s", result, stderr.String())
	}
	if stdout.String() != "verified: report signature and contents are valid\n" {
		t.Fatalf("verify output %q", stdout.String())
	}

	before, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if result := runCaptureWith(context.Background(), arguments, &stdout, &stderr, collect, probe, probeCPU, probeMemory, probeInput, probeNetwork, probeStorage, probeUSB, bytes.NewReader(bytes.Repeat([]byte{0xcd}, sessionIDLength)), time.Now); result != 1 {
		t.Fatalf("duplicate capture returned %d", result)
	}
	after, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("existing report was changed")
	}
}

func TestCaptureRejectsBadKeysBeforeCollection(t *testing.T) {
	directory := t.TempDir()
	shortFingerprintKey := writeTestKey(t, directory, "short.key", bytes.Repeat([]byte{1}, 31))
	badPrivateKey := writeTestKey(t, directory, "bad-private.key", bytes.Repeat([]byte{2}, ed25519.PrivateKeySize))
	outputPath := filepath.Join(directory, "rejected.hhpcr")
	called := false
	collect := func(context.Context) (inventory.Snapshot, error) {
		called = true
		return inventory.Snapshot{}, nil
	}

	arguments := []string{"-fingerprint-key", shortFingerprintKey, "-signing-key", badPrivateKey, "-key-id", "signer", "-output", outputPath}
	probe := func(context.Context, int) graphics.Result {
		called = true
		return graphics.Result{}
	}
	probeCPU := func(context.Context, quick.CPUOptions) quick.CPUResult { called = true; return quick.CPUResult{} }
	probeMemory := func(context.Context, quick.MemoryOptions) quick.MemoryResult {
		called = true
		return quick.MemoryResult{}
	}
	probeInput := func(context.Context, inputtest.Options) inputtest.Result { called = true; return inputtest.Result{} }
	probeNetwork := func(context.Context, networktest.Options) networktest.Result {
		called = true
		return networktest.Result{}
	}
	probeStorage := func(context.Context, storage.Options) storage.Result { called = true; return storage.Result{} }
	probeUSB := func(context.Context, usbtest.Options) usbtest.Result { called = true; return usbtest.Result{} }
	if result := runCaptureWith(context.Background(), arguments, bytes.NewBuffer(nil), bytes.NewBuffer(nil), collect, probe, probeCPU, probeMemory, probeInput, probeNetwork, probeStorage, probeUSB, bytes.NewReader(make([]byte, sessionIDLength)), time.Now); result != 1 {
		t.Fatalf("capture returned %d", result)
	}
	if called {
		t.Fatal("inventory was collected before key validation")
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("output exists after rejected capture: %v", err)
	}
}

func TestVerifyRejectsWrongPublicKey(t *testing.T) {
	snapshot := inventory.NewSnapshot(nil)
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, ed25519.SeedSize))
	encoded, err := captureBundle(snapshot, nil, bytes.Repeat([]byte{4}, 32), "lab", privateKey, "signer", "session", time.Unix(1, 2))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	reportPath := filepath.Join(directory, "signed.hhpcr")
	if err := os.WriteFile(reportPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	wrongPublicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{5}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	publicKeyPath := writeTestKey(t, directory, "wrong.pub", wrongPublicKey)
	var stderr bytes.Buffer
	if result := runVerify([]string{"-public-key", publicKeyPath, "-key-id", "signer", reportPath}, bytes.NewBuffer(nil), &stderr); result != 1 {
		t.Fatalf("verify returned %d", result)
	}
	if !strings.Contains(stderr.String(), "invalid signature") {
		t.Fatalf("unexpected error %q", stderr.String())
	}
}

func writeTestKey(t *testing.T, directory, name string, contents []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
