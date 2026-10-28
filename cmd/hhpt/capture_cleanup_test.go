package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/usbtest"
)

func TestCaptureStopsOnUncertainGraphicsCleanupBeforeLaterWorkOrSigning(t *testing.T) {
	directory := t.TempDir()
	fingerprintPath := writeTestKey(t, directory, "fingerprint.key", bytes.Repeat([]byte{0x42}, 32))
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, ed25519.SeedSize))
	signingPath := writeTestKey(t, directory, "signing.key", privateKey)
	reportPath := filepath.Join(directory, "rejected.hhpcr")
	arguments := []string{"-fingerprint-key", fingerprintPath, "-signing-key", signingPath, "-key-id", "fixture-signer", "-session-id", "cleanup-fixture", "-output", reportPath}
	graphicsCalls, laterCalls, clockCalls := 0, 0, 0
	probe := func(context.Context, int) graphics.Result {
		graphicsCalls++
		result := completeGraphicsEvidence()
		// Even otherwise sufficient pass observations cannot erase the runtime
		// supervisor's independent failure to confirm process cleanup.
		result.CleanupUncertain = true
		return result
	}
	var stdout, stderr bytes.Buffer
	code := runCaptureTestWith(t, t.Context(), arguments, &stdout, &stderr,
		func(context.Context) (inventory.Snapshot, error) { return inventory.NewSnapshot(nil), nil }, probe,
		func(context.Context, quick.CPUOptions) quick.CPUResult { return completeCPUEvidence() },
		func(context.Context, quick.MemoryOptions) quick.MemoryResult { return completeMemoryEvidence() },
		func(context.Context, inputtest.Options) inputtest.Result {
			laterCalls++
			return completeInputEvidence()
		},
		func(context.Context, networktest.Options) networktest.Result {
			laterCalls++
			return completeNetworkEvidence()
		},
		func(context.Context, storage.Options) storage.Result { laterCalls++; return completeStorageEvidence() },
		func(context.Context, usbtest.Options) usbtest.Result { laterCalls++; return completeUSBEvidence() },
		bytes.NewReader(nil), func() time.Time { clockCalls++; return time.Unix(1, 0) })
	if code != workerCleanupUncertainExitCode || graphicsCalls != 1 || laterCalls != 0 || clockCalls != 0 || !strings.Contains(stderr.String(), "graphics worker cleanup could not be confirmed") {
		t.Fatalf("uncertain cleanup did not stop capture: code=%d graphics=%d later=%d clock=%d error=%s", code, graphicsCalls, laterCalls, clockCalls, stderr.String())
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatal("capture created an artifact after uncertain worker cleanup")
	}
}
