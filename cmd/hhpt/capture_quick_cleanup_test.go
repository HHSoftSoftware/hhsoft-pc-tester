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

func TestCaptureStopsOnUncertainQuickCleanupBeforeLaterWorkOrSigning(t *testing.T) {
	for _, worker := range []string{"cpu", "memory"} {
		for _, status := range []quick.Status{quick.StatusPass, quick.StatusFail, quick.StatusError} {
			for _, claim := range []string{"flag", "state"} {
				t.Run(worker+"/"+string(status)+"/"+claim, func(t *testing.T) {
					directory := t.TempDir()
					fingerprint := writeTestKey(t, directory, "fingerprint.key", bytes.Repeat([]byte{0x42}, 32))
					key := writeTestKey(t, directory, "signing.key", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, ed25519.SeedSize)))
					report := filepath.Join(directory, "rejected.hhpcr")
					arguments := []string{"-fingerprint-key", fingerprint, "-signing-key", key, "-key-id", "fixture-signer", "-session-id", "quick-cleanup-fixture", "-output", report}
					cpu, memory := completeCPUEvidence(), completeMemoryEvidence()
					safety := &cpu.Safety
					cpu.Status = status
					if worker == "memory" {
						cpu.Status = quick.StatusPass
						memory.Status = status
						safety = &memory.Safety
					}
					if claim == "flag" {
						safety.CleanupConfirmed = false
					} else {
						safety.MonitorStatus = "cleanup_unconfirmed"
					}
					memoryCalls, laterCalls, clockCalls := 0, 0, 0
					random := &journalForbiddenRandom{}
					var stdout, stderr bytes.Buffer
					code := runCaptureTestWith(t, t.Context(), arguments, &stdout, &stderr,
						func(context.Context) (inventory.Snapshot, error) { return inventory.NewSnapshot(nil), nil },
						func(context.Context, int) graphics.Result { laterCalls++; return completeGraphicsEvidence() },
						func(context.Context, quick.CPUOptions) quick.CPUResult { return cpu },
						func(context.Context, quick.MemoryOptions) quick.MemoryResult { memoryCalls++; return memory },
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
						random, func() time.Time { clockCalls++; return time.Unix(1, 0) })
					wantMemoryCalls := 0
					if worker == "memory" {
						wantMemoryCalls = 1
					}
					if code != 12 || memoryCalls != wantMemoryCalls || laterCalls != 0 || clockCalls != 0 || random.calls != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "cleanup could not be confirmed") {
						t.Fatalf("uncertain quick cleanup did not stop capture: code=%d memory=%d later=%d clock=%d random=%d stderr=%s", code, memoryCalls, laterCalls, clockCalls, random.calls, &stderr)
					}
					if _, err := os.Stat(report); !os.IsNotExist(err) {
						t.Fatal("capture created a report after uncertain cleanup")
					}
				})
			}
		}
	}
}
