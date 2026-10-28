package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/usbtest"
)

func TestInventoryAndPlanRejectFailedCollectionBeforeOutput(t *testing.T) {
	for _, command := range []string{"inventory", "plan"} {
		for _, failure := range []error{nil, inventory.ErrDirectoryRead, inventory.ErrDirectoryClose, inventory.ErrScalarRead, inventory.ErrScalarLimit, inventory.ErrScalarClose, context.Canceled} {
			label := "success"
			if failure != nil {
				label = failure.Error()
			}
			t.Run(command+"/"+label, func(t *testing.T) {
				root := t.TempDir()
				key := writeTestKey(t, root, "fingerprint.key", bytes.Repeat([]byte{0x62}, 32))
				output := filepath.Join(root, "result.json")
				calls := 0
				collect := func(context.Context) (inventory.Snapshot, error) {
					calls++
					return inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil)}), failure
				}
				args := []string{"-fingerprint-key", key, "-output", output}
				var stdout, stderr bytes.Buffer
				var code int
				if command == "inventory" {
					code = runInventoryWith(t.Context(), args, &stdout, &stderr, collect)
				} else {
					args = append(args, "-architecture", "x86_64")
					code = runPlanWith(t.Context(), args, &stdout, &stderr, collect)
				}
				if calls != 1 {
					t.Fatalf("valid prerequisites did not reach collection: %d %s", calls, &stderr)
				}
				if failure != nil {
					if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "collect inventory: "+failure.Error()) {
						t.Fatalf("collection failure was hidden: %d %s", code, &stderr)
					}
					if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("failed collection created output")
					}
				} else {
					data, err := os.ReadFile(output)
					if code != 0 || err != nil || len(data) == 0 {
						t.Fatalf("successful control failed: %d %v %s", code, err, &stderr)
					}
				}
			})
		}
	}
}

func TestCaptureRejectsFailedCollectionBeforeWorkersAndSigning(t *testing.T) {
	for _, failure := range []error{nil, inventory.ErrDirectoryRead, inventory.ErrDirectoryClose, inventory.ErrScalarRead, inventory.ErrScalarLimit, inventory.ErrScalarClose, context.Canceled} {
		label := "success"
		if failure != nil {
			label = failure.Error()
		}
		t.Run(label, func(t *testing.T) {
			root := t.TempDir()
			fingerprint := writeTestKey(t, root, "fingerprint.key", bytes.Repeat([]byte{0x62}, 32))
			// Public deterministic fixture signer; never a deployment credential.
			signer := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x63}, ed25519.SeedSize))
			signing := writeTestKey(t, root, "signing.key", signer)
			output := filepath.Join(root, "capture.hhpcr")
			args := []string{"-fingerprint-key", fingerprint, "-signing-key", signing, "-key-id", "fixture-inventory", "-session-id", "collection-fixture", "-output", output, "-graphics-pattern-frames", "12", "-memory-bytes", "4194304", "-memory-passes", "1"}
			calls, workers, clocks := 0, 0, 0
			var stdout, stderr bytes.Buffer
			collect := func(context.Context) (inventory.Snapshot, error) { calls++; return inventory.NewSnapshot(nil), failure }
			code := runCaptureWith(t.Context(), args, &stdout, &stderr, collect,
				func(context.Context, int) graphics.Result { workers++; return completeGraphicsEvidence() },
				func(context.Context, quick.CPUOptions) quick.CPUResult { workers++; return completeCPUEvidence() },
				func(context.Context, quick.MemoryOptions) quick.MemoryResult {
					workers++
					return completeMemoryEvidence()
				},
				func(context.Context, inputtest.Options) inputtest.Result { workers++; return completeInputEvidence() },
				func(context.Context, networktest.Options) networktest.Result {
					workers++
					return completeNetworkEvidence()
				},
				func(context.Context, storage.Options) storage.Result { workers++; return completeStorageEvidence() },
				func(context.Context, usbtest.Options) usbtest.Result { workers++; return completeUSBEvidence() },
				bytes.NewReader(nil), func() time.Time { clocks++; return time.Unix(100, 0) })
			if calls != 1 {
				t.Fatalf("valid capture prerequisites did not reach collection: %d %s", calls, &stderr)
			}
			if failure != nil {
				if code != 1 || workers != 0 || clocks != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "collect inventory: "+failure.Error()) {
					t.Fatalf("failed collection reached later capture work: %d/%d/%d %s", code, workers, clocks, &stderr)
				}
				if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed collection created signed report")
				}
			} else {
				if code != 0 || workers != 7 || clocks != 1 {
					t.Fatalf("positive capture control failed: %d/%d/%d %s", code, workers, clocks, &stderr)
				}
				data, err := os.ReadFile(output)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := bundle.Verify(bytes.NewReader(data), int64(len(data)), report.Keyring{"fixture-inventory": signer.Public().(ed25519.PublicKey)}); err != nil {
					t.Fatal("positive fixture report was not valid", err)
				}
			}
		})
	}
}

func TestFinalizeRejectsFailedCollectionBeforeReportCreation(t *testing.T) {
	for _, failure := range []error{nil, inventory.ErrDirectoryRead, inventory.ErrDirectoryClose, inventory.ErrScalarRead, inventory.ErrScalarLimit, inventory.ErrScalarClose, context.Canceled} {
		label := "success"
		if failure != nil {
			label = failure.Error()
		}
		t.Run(label, func(t *testing.T) {
			args, snapshot, _, output := selectedCPUFinalization(t, "excluded")
			calls := 0
			collect := func(context.Context) (inventory.Snapshot, error) { calls++; return snapshot, failure }
			var stdout, stderr bytes.Buffer
			code := runFinalizeWith(t.Context(), args, &stdout, &stderr, collect, bytes.NewReader(bytes.Repeat([]byte{0x64}, 64)), func() time.Time { return time.Unix(100, 0) })
			if calls != 1 {
				t.Fatalf("valid finalization prerequisites did not reach collection: %d %s", calls, &stderr)
			}
			if failure != nil {
				if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "collect final inventory: "+failure.Error()) {
					t.Fatalf("failed collection reached signing: %d %s", code, &stderr)
				}
				if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed collection created final report")
				}
			} else {
				data, err := os.ReadFile(output)
				if code != 0 || err != nil {
					t.Fatalf("successful finalization failed: %d %v %s", code, err, &stderr)
				}
				if _, err := bundle.VerifySelfIssued(bytes.NewReader(data), int64(len(data))); err != nil {
					t.Fatal("positive finalization control produced invalid report", err)
				}
			}
		})
	}
}
