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

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/usbtest"
)

type cancellingProcCommandSource struct {
	*inventory.RootPathSource
	cancel  context.CancelFunc
	reached int
	target  string
}

func (s *cancellingProcCommandSource) ReadFile(name string) ([]byte, error) {
	data, err := s.RootPathSource.ReadFile(name)
	if name == s.target {
		s.reached++
		s.cancel()
	}
	return data, err
}

// These fixtures cause real bulk acquisition failures in each primary role.
func commandProcFailure(t *testing.T, mode, primary string, limit int, calls *int) (snapshotCollector, error) {
	t.Helper()
	root := t.TempDir()
	name := filepath.Join(root, filepath.FromSlash(primary))
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	var want error
	switch mode {
	case "overflow":
		if err := os.WriteFile(name, bytes.Repeat([]byte{'x'}, limit+1), 0600); err != nil {
			t.Fatal(err)
		}
		want = inventory.ErrBulkLimit
	case "nonregular":
		if err := os.Mkdir(name, 0700); err != nil {
			t.Fatal(err)
		}
		want = inventory.ErrBulkRead
	case "cancel":
		if err := os.WriteFile(name, []byte("uncredited system\n"), 0600); err != nil {
			t.Fatal(err)
		}
		want = context.Canceled
	default:
		t.Fatal("unknown bulk failure fixture")
	}
	return func(ctx context.Context) (inventory.Snapshot, error) {
		(*calls)++
		collector := inventory.NewLinuxCollector(root)
		if mode == "cancel" {
			owned, cancel := context.WithCancel(ctx)
			defer cancel()
			source := &cancellingProcCommandSource{RootPathSource: inventory.NewRootPathSource(root), cancel: cancel, target: primary}
			snapshot, err := inventory.NewLinuxCollectorWithSource(source).Collect(owned)
			if err != want || source.reached != 1 || snapshot.SchemaVersion != 0 || snapshot.Components != nil {
				t.Fatal("real bulk cancellation fixture did not fail at its boundary", err)
			}
			return snapshot, err
		}
		snapshot, err := collector.Collect(ctx)
		if err != want || snapshot.SchemaVersion != 0 || snapshot.Components != nil {
			t.Fatal("real bulk failure fixture did not fail closed", err)
		}
		return snapshot, err
	}, want
}

func requireProcCommandBoundary(t *testing.T, command string, factory func(*int) (snapshotCollector, error)) {
	t.Helper()
	calls, workers, clocks := 0, 0, 0
	collect, want := factory(&calls)
	root := t.TempDir()
	fingerprint := writeTestKey(t, root, "fingerprint.key", bytes.Repeat([]byte{0x62}, 32))
	output := filepath.Join(root, "output.json")
	args := []string{"-fingerprint-key", fingerprint, "-output", output}
	var stdout, stderr bytes.Buffer
	var code int
	switch command {
	case "inventory":
		code = runInventoryWithInput(t.Context(), args, &stdout, &stderr, collect, writeDestination, captureFixtureInput(t))
	case "plan":
		code = runPlanWithInput(t.Context(), append(args, "-architecture", "x86_64"), &stdout, &stderr, collect, writeDestination, captureFixtureInput(t))
	case "capture":
		// Public deterministic signer for an owned fixture, never a credential.
		signer := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x63}, ed25519.SeedSize))
		signing := writeTestKey(t, root, "signing.key", signer)
		args = append(args, "-signing-key", signing, "-key-id", "fixture-inventory", "-session-id", "proc-fixture", "-graphics-pattern-frames", "12", "-memory-bytes", "4194304", "-memory-passes", "1")
		code = runCaptureTestWith(t, t.Context(), args, &stdout, &stderr, collect,
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
	case "finalize":
		args, _, _, output = selectedCPUFinalization(t, "excluded")
		code = runFinalizeTestWith(t, t.Context(), args, &stdout, &stderr, collect, bytes.NewReader(bytes.Repeat([]byte{0x64}, 64)), func() time.Time { clocks++; return time.Unix(100, 0) })
	}
	if calls != 1 || code != 1 || stdout.Len() != 0 || workers != 0 || clocks != 0 || !strings.Contains(stderr.String(), want.Error()) {
		t.Fatalf("primary observation failure crossed command boundary: code=%d calls=%d workers=%d clocks=%d stderr=%s", code, calls, workers, clocks, &stderr)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed primary collection created output or a report")
	}
}

func TestInventoryProcFailuresReachAllRolesAndCommandBoundaries(t *testing.T) {
	for _, primary := range []struct {
		name  string
		limit int
	}{{"proc/meminfo", 262144}, {"proc/cpuinfo", 16777216}, {"proc/bus/input/devices", 8388608}} {
		for _, command := range []string{"inventory", "plan", "capture", "finalize"} {
			for _, mode := range []string{"overflow", "nonregular", "cancel"} {
				t.Run(primary.name+"/"+command+"/"+mode, func(t *testing.T) {
					requireProcCommandBoundary(t, command, func(calls *int) (snapshotCollector, error) {
						return commandProcFailure(t, mode, primary.name, primary.limit, calls)
					})
				})
			}
		}
	}
}

// This composes the reader's independently tested cleanup category through the
// command boundary. It deliberately does not claim to induce an OS Close error.
func TestInventoryProcComposedCloseFailureRejectsNonemptySnapshots(t *testing.T) {
	for _, command := range []string{"inventory", "plan", "capture", "finalize"} {
		t.Run(command, func(t *testing.T) {
			requireProcCommandBoundary(t, command, func(calls *int) (snapshotCollector, error) {
				return func(context.Context) (inventory.Snapshot, error) {
					(*calls)++
					return inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentMemory, "memory:system", map[string]string{"total_kib": "4096"})}), inventory.ErrBulkClose
				}, inventory.ErrBulkClose
			})
		})
	}
}
