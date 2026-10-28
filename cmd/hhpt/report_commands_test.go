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
	workerCalls := make(map[string]int)
	probe := func(context.Context, int) graphics.Result {
		probeCalls++
		workerCalls["graphics.quick"]++
		return completeGraphicsEvidence()
	}
	probeCPU := func(context.Context, quick.CPUOptions) quick.CPUResult {
		workerCalls["cpu.quick"]++
		return completeCPUEvidence()
	}
	probeMemory := func(ctx context.Context, options quick.MemoryOptions) quick.MemoryResult {
		workerCalls["memory.quick"]++
		if options.Bytes != 4<<20 || options.Passes != 1 || options.GlobalTimeout != 30*time.Second {
			t.Fatal("capture changed RAM options")
		}
		options.Root = directory
		return quick.RunMemory(ctx, options)
	}
	probeInput := func(context.Context, inputtest.Options) inputtest.Result {
		workerCalls["input.readiness.quick"]++
		return completeInputEvidence()
	}
	probeNetwork := func(context.Context, networktest.Options) networktest.Result {
		workerCalls["network.readiness.quick"]++
		return completeNetworkEvidence()
	}
	probeStorage := func(context.Context, storage.Options) storage.Result {
		workerCalls["storage.readiness.quick"]++
		return completeStorageEvidence()
	}
	probeUSB := func(context.Context, usbtest.Options) usbtest.Result {
		workerCalls["usb.readiness.quick"]++
		return completeUSBEvidence()
	}
	now := time.Date(2026, time.October, 12, 13, 14, 15, 123456789, time.FixedZone("PDT", -7*60*60))
	arguments := []string{"-fingerprint-key", fingerprintKeyPath, "-key-epoch", "lab-2", "-signing-key", privateKeyPath, "-key-id", "signer-7", "-output", reportPath, "-graphics-pattern-frames", "12", "-memory-bytes", "4194304", "-memory-passes", "1"}
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
	memory, err := app.ValidatedMemory(payload)
	if err != nil || memory == nil || memory.CompletedPhases != 10 || memory.ExaminedBytes != 25165824 || memory.WrittenBytes != 25165824 || payload.TestResults[3].Version != 2 {
		t.Fatal("direct capture lost actual RAM coverage", err)
	}
	if len(payload.TestResults) != 7 || payload.TestResults[0].ID != "cpu.quick" || payload.TestResults[1].ID != "graphics.quick" || payload.TestResults[2].ID != "input.readiness.quick" || payload.TestResults[3].ID != "memory.quick" || payload.TestResults[4].ID != "network.readiness.quick" || payload.TestResults[5].ID != "storage.readiness.quick" || payload.TestResults[6].ID != "usb.readiness.quick" || payload.TestResults[0].Status != app.TestPass {
		t.Fatalf("test results = %#v", payload.TestResults)
	}
	if payload.CoverageScope != app.CoverageDirectCapture || len(workerCalls) != 7 {
		t.Fatal("capture coverage no longer describes its seven direct checks")
	}
	for _, result := range payload.TestResults {
		if workerCalls[result.ID] != 1 || result.PlanID != "" || result.HardwareGraph != "" || result.PlannedStatus != "" {
			t.Fatal("direct capture omitted/repeated a worker or invented frozen-plan metadata")
		}
	}
	for _, view := range [][]byte{artifact.RenderedReport, artifact.RenderedPDF} {
		if !bytes.Contains(view, []byte("Direct capture: only the listed checks")) || !bytes.Contains(view, []byte("No frozen plan")) {
			t.Fatal("signed direct-capture scope is not readable in the report view")
		}
	}

	stdout.Reset()
	stderr.Reset()
	if result := runVerify(context.Background(), []string{"-public-key", publicKeyPath, "-key-id", "signer-7", reportPath}, &stdout, &stderr); result != 0 {
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

func TestCaptureWillNotSignUnsupportedPassEvidence(t *testing.T) {
	for _, target := range []string{"CPU", "memory", "graphics", "input", "network", "storage", "USB"} {
		t.Run(target, func(t *testing.T) {
			directory := t.TempDir()
			fingerprint := writeTestKey(t, directory, "fingerprint.key", bytes.Repeat([]byte{0x25}, 32))
			private := writeTestKey(t, directory, "signing.key", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x35}, ed25519.SeedSize)))
			output := filepath.Join(directory, "rejected.hhpcr")
			arguments := []string{"-fingerprint-key", fingerprint, "-signing-key", private, "-key-id", "fixture", "-session-id", "fixture", "-output", output}
			collect := func(context.Context) (inventory.Snapshot, error) { return inventory.NewSnapshot(nil), nil }
			cpu := func(context.Context, quick.CPUOptions) quick.CPUResult {
				result := completeCPUEvidence()
				if target == "CPU" {
					result.Iterations = 0
				}
				return result
			}
			memory := func(context.Context, quick.MemoryOptions) quick.MemoryResult {
				result := completeMemoryEvidence()
				if target == "memory" {
					result.ExaminedBytes = 0
				}
				return result
			}
			gpu := func(context.Context, int) graphics.Result {
				result := completeGraphicsEvidence()
				if target == "graphics" {
					result.Renderer.HardwareDetected = false
				}
				return result
			}
			input := func(context.Context, inputtest.Options) inputtest.Result {
				result := completeInputEvidence()
				if target == "input" {
					result.Devices = nil
				}
				return result
			}
			network := func(context.Context, networktest.Options) networktest.Result {
				result := completeNetworkEvidence()
				if target == "network" {
					result.ActiveNonLoopInterfaces = 1
				}
				return result
			}
			disk := func(context.Context, storage.Options) storage.Result {
				result := completeStorageEvidence()
				if target == "storage" {
					result.WritableMounts = 1
				}
				return result
			}
			usb := func(context.Context, usbtest.Options) usbtest.Result {
				result := completeUSBEvidence()
				if target == "USB" {
					result.Devices[0].Authorized = false
				}
				return result
			}
			var stderr bytes.Buffer
			if result := runCaptureWith(context.Background(), arguments, bytes.NewBuffer(nil), &stderr, collect, gpu, cpu, memory, input, network, disk, usb, bytes.NewReader(nil), time.Now); result != 1 {
				t.Fatalf("unsupported %s evidence was signed: result=%d", target, result)
			}
			if !strings.Contains(stderr.String(), "build "+target+" evidence") {
				t.Fatalf("unexpected rejection: %s", stderr.String())
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("report was created for unsupported pass")
			}
		})
	}
}

func TestVerifyRejectsWrongPublicKey(t *testing.T) {
	snapshot := inventory.NewSnapshot(nil)
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, ed25519.SeedSize))
	encoded, err := captureBundle(snapshot, nil, bytes.Repeat([]byte{4}, 32), "lab", privateKey, "signer", "session", time.Unix(1, 2), "")
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
	if result := runVerify(context.Background(), []string{"-public-key", publicKeyPath, "-key-id", "signer", reportPath}, bytes.NewBuffer(nil), &stderr); result != 1 {
		t.Fatalf("verify returned %d", result)
	}
	if !strings.Contains(stderr.String(), "verify: child input or verification failed") {
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
