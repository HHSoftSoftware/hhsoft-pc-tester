package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
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

type captureObservationHarness struct {
	*captureInputHarness
	collect      inventoryDiagnosticCollector
	diagnostics  inventory.CollectionDiagnostics
	collectCalls int
	diagnostic   io.Writer
	destination  destinationWriter
	random       io.Reader
	stopWorker   string
}

func newCaptureObservationHarness(t *testing.T) *captureObservationHarness {
	t.Helper()
	h := &captureObservationHarness{captureInputHarness: newCaptureInputHarness(t)}
	h.args = append(h.args, "-session-id", "capture-observation")
	h.collect = func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
		return inventory.NewSnapshot([]inventory.Component{
			inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"serial": "capture-fixture-serial", "model": "Fixture"}),
		}), h.diagnostics, nil
	}
	h.diagnostic = observationCLIWriter{write: func(p []byte) (int, error) {
		h.observeWarning(p)
		return h.stderr.Write(p)
	}}
	h.destination = h.write
	h.random = captureInputReadFunc(func(p []byte) (int, error) {
		h.effects = append(h.effects, "random")
		if h.randomHook != nil {
			h.randomHook()
		}
		return copy(p, bytes.Repeat([]byte{0xa7}, 16)), nil
	})
	return h
}

func (h *captureObservationHarness) requireKeysClosed() {
	h.t.Helper()
	for _, f := range h.fixtures {
		if !reflect.DeepEqual(f.ledger.acquired, f.ledger.closed) {
			h.t.Fatal("capture reached a later effect with an owned key still open")
		}
	}
}

func (h *captureObservationHarness) observeWarning(p []byte) {
	h.t.Helper()
	if bytes.HasPrefix(p, []byte("inventory: metadata reads failed ")) {
		h.requireKeysClosed()
		h.effects = append(h.effects, "warning")
	}
}

func (h *captureObservationHarness) setFlag(name, value string) {
	h.t.Helper()
	for i := 0; i+1 < len(h.args); i += 2 {
		if h.args[i] == name {
			h.args[i+1] = value
			return
		}
	}
	h.t.Fatal("missing fixture flag")
}

func (h *captureObservationHarness) runObservation(ctx context.Context, legacy bool) int {
	h.t.Helper()
	collect := h.collect
	if collect != nil {
		collect = func(ctx context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
			h.requireKeysClosed()
			h.effects = append(h.effects, "collect")
			h.collectCalls++
			return h.collect(ctx)
		}
	}
	graphicsProbe := func(context.Context, int) graphics.Result {
		h.effects = append(h.effects, "graphics")
		result := completeGraphicsEvidence()
		if h.stopWorker == "graphics" {
			result.CleanupUncertain = true
		}
		return result
	}
	cpuProbe := func(context.Context, quick.CPUOptions) quick.CPUResult {
		h.effects = append(h.effects, "cpu")
		result := completeCPUEvidence()
		if h.stopWorker == "cpu" {
			result.Safety.CleanupConfirmed = false
		}
		return result
	}
	memoryProbe := func(context.Context, quick.MemoryOptions) quick.MemoryResult {
		h.effects = append(h.effects, "memory")
		result := completeMemoryEvidence()
		if h.stopWorker == "memory" {
			result.Safety.MonitorStatus = "cleanup_unconfirmed"
		}
		return result
	}
	inputProbe := func(context.Context, inputtest.Options) inputtest.Result {
		h.effects = append(h.effects, "input")
		return completeInputEvidence()
	}
	networkProbe := func(context.Context, networktest.Options) networktest.Result {
		h.effects = append(h.effects, "network")
		return completeNetworkEvidence()
	}
	storageProbe := func(context.Context, storage.Options) storage.Result {
		h.effects = append(h.effects, "storage")
		return completeStorageEvidence()
	}
	usbProbe := func(context.Context, usbtest.Options) usbtest.Result {
		h.effects = append(h.effects, "usb")
		return completeUSBEvidence()
	}
	now := func() time.Time {
		h.effects = append(h.effects, "clock")
		return time.Date(2026, 10, 28, 1, 2, 3, 4, time.UTC)
	}
	if legacy {
		return runCaptureWithDestination(ctx, h.args, &h.stdout, h.diagnostic,
			func(ctx context.Context) (inventory.Snapshot, error) {
				snapshot, _, err := collect(ctx)
				return snapshot, err
			}, graphicsProbe, cpuProbe, memoryProbe, inputProbe, networkProbe, storageProbe, usbProbe, h.random, now, h.destination, h.read)
	}
	return runCaptureWithDiagnosticDestination(ctx, h.args, &h.stdout, h.diagnostic, collect,
		graphicsProbe, cpuProbe, memoryProbe, inputProbe, networkProbe, storageProbe, usbProbe, h.random, now, h.destination, h.read)
}

func requireCaptureObservationVerified(t *testing.T, h *captureObservationHarness) {
	t.Helper()
	public := ed25519.PrivateKey(h.keys[1]).Public().(ed25519.PublicKey)
	artifact, err := bundle.Verify(bytes.NewReader(h.output.Bytes()), int64(h.output.Len()), report.Keyring{"capture-fixture-key": public})
	if err != nil || len(artifact.RenderedReport) == 0 || len(artifact.RenderedPDF) == 0 {
		t.Fatal("complete expected-signer verification prerequisite failed", err)
	}
	if artifact.Envelope.KeyID != "capture-fixture-key" || artifact.Envelope.GeneratedTime != "2026-10-28T01:02:03.000000004Z" {
		t.Fatal("signed envelope identity or fixed clock changed")
	}
	for _, exposed := range [][]byte{artifact.InventoryJSON, []byte(artifact.RenderedReport), []byte(artifact.RenderedPDF)} {
		for _, marker := range []string{"SYNTHETIC-", "proc/cpuinfo", "proc/meminfo", "proc/bus/input", "sys/class/dmi"} {
			if bytes.Contains(exposed, []byte(marker)) {
				t.Fatal("provider canary or acquisition path entered a verified report member")
			}
		}
	}
	wrong := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x68}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	if _, err := bundle.Verify(bytes.NewReader(h.output.Bytes()), int64(h.output.Len()), report.Keyring{"capture-fixture-key": wrong}); err == nil {
		t.Fatal("bundle verification did not enforce the expected signer")
	}
}

func TestCaptureObservationCanonicalBundle(t *testing.T) {
	for _, name := range []string{"quiet", "mixed", "maximum", "generated_session"} {
		t.Run(name, func(t *testing.T) {
			h, old := newCaptureObservationHarness(t), newCaptureObservationHarness(t)
			wantWarning := ""
			switch name {
			case "mixed", "generated_session":
				h.diagnostics = inventory.CollectionDiagnostics{ScalarFailures: 2, CPUFailures: 3, MemoryFailures: 4, InputFailures: 5}
				wantWarning = "inventory: metadata reads failed (scalar=2 cpu=3 memory=4 input=5)\n"
			case "maximum":
				h.diagnostics = inventory.CollectionDiagnostics{ScalarFailures: 65536}
				wantWarning = "inventory: metadata reads failed (scalar=65536 cpu=0 memory=0 input=0)\n"
			}
			if name == "generated_session" {
				h.args, old.args = h.args[:len(h.args)-2], old.args[:len(old.args)-2]
			}
			if code := old.runObservation(t.Context(), true); code != 0 || old.stderr.Len() != 0 {
				t.Fatal("legacy signed positive failed")
			}
			if code := h.runObservation(t.Context(), false); code != 0 || h.stderr.String() != wantWarning ||
				h.collectCalls != 1 || h.stdout.Len() != 0 || !bytes.Equal(h.output.Bytes(), old.output.Bytes()) {
				t.Fatal("diagnostics changed legacy signed bundle bytes or one-pass capture")
			}
			want := []string{"collect", "cpu", "memory", "graphics", "input", "network", "storage", "usb", "clock", "output"}
			if wantWarning != "" {
				want = append([]string{"collect", "warning"}, want[1:]...)
			}
			if name == "generated_session" {
				want = append([]string{"random"}, want...)
			}
			if !reflect.DeepEqual(h.effects, want) {
				t.Fatal("key/entropy/warning/worker/signing order changed")
			}
			h.requireKeysClosed()
			requireCaptureObservationVerified(t, h)
			artifact, err := bundle.Verify(bytes.NewReader(h.output.Bytes()), int64(h.output.Len()),
				report.Keyring{"capture-fixture-key": ed25519.PrivateKey(h.keys[1]).Public().(ed25519.PublicKey)})
			if err != nil {
				t.Fatal(err)
			}
			var payload app.InventoryPayload
			if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
				t.Fatal(err)
			}
			// Independent retained HMAC vectors for the literal block fixture.
			if payload.CoverageScope != app.CoverageDirectCapture || payload.SystemFingerprint.KeyEpoch != "capture-epoch" ||
				payload.SystemFingerprint.Value != "hmac-sha256:e732f5268d3a8a586bc8fc60deda0f1d8ecf9abdd75cd6f0edccb63df06c5972" ||
				len(payload.Components) != 1 || payload.Components[0].Identity.Value != "hmac-sha256:9670d998f4774fc641208d0b799b73e0ab771b0a1dfa303d487e93894e874d88" ||
				len(payload.TestResults) != 7 {
				t.Fatal("diagnostics altered fingerprint or result semantics")
			}
		})
	}
}

func TestCaptureObservationSamePassRealProvider(t *testing.T) {
	for _, item := range []struct {
		name    string
		failed  []string
		warning string
	}{
		{"scalar", []string{"sys/class/dmi/id/product_uuid"}, "inventory: metadata reads failed (scalar=1 cpu=0 memory=0 input=0)\n"},
		{"cpu", []string{"proc/cpuinfo"}, "inventory: metadata reads failed (scalar=0 cpu=1 memory=0 input=0)\n"},
		{"memory", []string{"proc/meminfo"}, "inventory: metadata reads failed (scalar=0 cpu=0 memory=1 input=0)\n"},
		{"input", []string{"proc/bus/input/devices"}, "inventory: metadata reads failed (scalar=0 cpu=0 memory=0 input=1)\n"},
		{"all", []string{"sys/class/dmi/id/product_uuid", "proc/cpuinfo", "proc/meminfo", "proc/bus/input/devices"}, "inventory: metadata reads failed (scalar=1 cpu=1 memory=1 input=1)\n"},
	} {
		t.Run(item.name, func(t *testing.T) {
			source, _ := newPlanObservationSource(t)
			for _, path := range item.failed {
				source.failed[path] = true
			}
			collector := inventory.NewLinuxCollectorWithSource(source)
			h := newCaptureObservationHarness(t)
			h.collect = collector.CollectWithDiagnostics
			if code := h.runObservation(t.Context(), false); code != 0 || h.collectCalls != 1 || h.stderr.String() != item.warning {
				t.Fatal("real provider failures did not reach the bounded same-pass capture warning")
			}
			for _, path := range []string{"proc/cpuinfo", "proc/meminfo", "proc/bus/input/devices", "sys/class/dmi/id/product_uuid"} {
				if source.reads[path] != 1 {
					t.Fatal("warning caused repeated acquisition")
				}
			}
			if source.callbacks != [3]int{} || bytes.Contains(h.output.Bytes(), []byte("SYNTHETIC-")) ||
				strings.Contains(h.stderr.String(), "sys/") || strings.Contains(h.stderr.String(), "proc/") {
				t.Fatal("real provider callbacks, discarded bytes or paths escaped")
			}
			requireCaptureObservationVerified(t, h)
			old := newCaptureObservationHarness(t)
			old.collect = collector.CollectWithDiagnostics
			// Separate legacy invocation, not a second acquisition within capture.
			if code := old.runObservation(t.Context(), true); code != 0 || old.stderr.Len() != 0 ||
				!bytes.Equal(h.output.Bytes(), old.output.Bytes()) || source.callbacks != [3]int{} {
				t.Fatal("same observations changed the legacy report or provider isolation")
			}
		})
	}
}

func TestCaptureObservationQuietAndReset(t *testing.T) {
	for _, name := range []string{"absence", "empty", "reset"} {
		t.Run(name, func(t *testing.T) {
			source, root := newPlanObservationSource(t)
			if name == "empty" {
				if err := os.WriteFile(filepath.Join(root, "sys", "class", "dmi", "id", "product_uuid"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			collector := inventory.NewLinuxCollectorWithSource(source)
			if name == "reset" {
				source.failed["sys/class/dmi/id/product_uuid"] = true
				first := newCaptureObservationHarness(t)
				first.collect = collector.CollectWithDiagnostics
				if first.runObservation(t.Context(), false) != 0 || first.stderr.String() != "inventory: metadata reads failed (scalar=1 cpu=0 memory=0 input=0)\n" {
					t.Fatal("counter reset positive prerequisite failed")
				}
				delete(source.failed, "sys/class/dmi/id/product_uuid")
			}
			h := newCaptureObservationHarness(t)
			h.collect = collector.CollectWithDiagnostics
			if h.runObservation(t.Context(), false) != 0 || h.stderr.Len() != 0 || source.callbacks != [3]int{} {
				t.Fatal("quiet observation warned or counters leaked between captures")
			}
			requireCaptureObservationVerified(t, h)
		})
	}
}

func TestCaptureObservationInvalidDiagnostics(t *testing.T) {
	for _, item := range []struct {
		name        string
		diagnostics inventory.CollectionDiagnostics
	}{
		{"one_counter", inventory.CollectionDiagnostics{CPUFailures: 65537}},
		{"aggregate", inventory.CollectionDiagnostics{ScalarFailures: 65536, InputFailures: 1}},
		{"uint32_max", inventory.CollectionDiagnostics{MemoryFailures: ^uint32(0)}},
		{"cancelled_invalid", inventory.CollectionDiagnostics{InputFailures: 65537}},
	} {
		t.Run(item.name, func(t *testing.T) {
			h := newCaptureObservationHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h.collect = func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
				if item.name == "cancelled_invalid" {
					cancel()
				}
				return inventoryPlanFingerprintSnapshot(), item.diagnostics, nil
			}
			if h.runObservation(ctx, false) != 1 || !reflect.DeepEqual(h.effects, []string{"collect"}) ||
				h.output.Len() != 0 || h.stdout.Len() != 0 || h.stderr.String() != "collect inventory: inventory: invalid observation diagnostics\n" {
				t.Fatal("invalid companion reached workers or lost its pre-cancellation precedence")
			}
		})
	}
}

func TestCaptureObservationWarningStopsEffects(t *testing.T) {
	for _, name := range []string{"error", "short", "overreported", "negative", "cancel", "error_cancel", "quiet", "nil_stderr"} {
		t.Run(name, func(t *testing.T) {
			h := newCaptureObservationHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h.diagnostics = inventory.CollectionDiagnostics{ScalarFailures: 65536}
			var callbacks [3]int
			writes := 0
			output := filepath.Join(t.TempDir(), "capture.hhpcr")
			h.setFlag("-output", output)
			h.destination = func(path string, out io.Writer, write func(io.Writer) error) error {
				h.effects = append(h.effects, "output")
				return writeDestination(path, out, write)
			}
			h.diagnostic = observationCLIWriter{write: func(p []byte) (int, error) {
				writes++
				h.observeWarning(p)
				h.stderr.Write(p)
				if writes == 1 {
					if string(p) != "inventory: metadata reads failed (scalar=65536 cpu=0 memory=0 input=0)\n" || len(p) > 128 {
						t.Fatal("warning exceeded or changed its independent literal bound")
					}
					switch name {
					case "error":
						return 0, &planObservationProviderError{&callbacks}
					case "short":
						return len(p) - 1, nil
					case "overreported":
						return len(p) + 1, nil
					case "negative":
						return -1, nil
					case "cancel":
						cancel()
						return len(p), nil
					case "error_cancel":
						cancel()
						return 0, &planObservationProviderError{&callbacks}
					}
				}
				return len(p), nil
			}}
			if name == "quiet" {
				h.diagnostics = inventory.CollectionDiagnostics{}
			}
			if name == "nil_stderr" {
				h.diagnostic = nil
			}
			code := h.runObservation(ctx, false)
			if name == "quiet" {
				if code != 0 || writes != 0 {
					t.Fatal("quiet capture attempted a warning")
				}
				b, err := os.ReadFile(output)
				if err != nil || len(b) == 0 {
					t.Fatal("quiet positive did not create the owned report")
				}
				return
			}
			requirePlanObservationAbsent(t, output)
			if name == "nil_stderr" {
				if code != 1 || len(h.seen) != 0 || len(h.effects) != 0 {
					t.Fatal("missing diagnostics sink reached effects")
				}
				return
			}
			want := "inventory: metadata reads failed (scalar=65536 cpu=0 memory=0 input=0)\ncollect inventory: inventory: observation warning could not be written\n"
			if name == "cancel" {
				want = "inventory: metadata reads failed (scalar=65536 cpu=0 memory=0 input=0)\ncollect inventory: context canceled\n"
			}
			if code != 1 || h.stderr.String() != want || callbacks != [3]int{} ||
				!reflect.DeepEqual(h.effects, []string{"collect", "warning"}) || h.output.Len() != 0 || h.stdout.Len() != 0 {
				t.Fatal("warning failure reached workers/signing/output or leaked writer callbacks")
			}
		})
	}
}

func TestCaptureObservationOwnedKeyClosure(t *testing.T) {
	for _, item := range []struct {
		name             string
		slot             int
		metadata, cancel bool
	}{
		{"fingerprint_readable", 0, false, false}, {"fingerprint_metadata", 0, true, false},
		{"signing_readable", 1, false, false}, {"signing_metadata", 1, true, false},
		{"fingerprint_readable_cancel", 0, false, true}, {"fingerprint_metadata_cancel", 0, true, true},
		{"signing_readable_cancel", 1, false, true}, {"signing_metadata_cancel", 1, true, true},
	} {
		t.Run(item.name, func(t *testing.T) {
			h := newCaptureObservationHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var callbacks [3]int
			h.failAt = item.slot
			h.mutate = func(f *retestInputFixture) {
				file := f.readable
				if item.metadata {
					file = f.metadata
				}
				file.closeErr = &planObservationProviderError{&callbacks}
				if item.cancel {
					file.hook = func(op string) {
						if op == "close" {
							cancel()
						}
					}
				}
			}
			code := h.runObservation(ctx, false)
			h.stopped(code, item.slot+1, captureInputDiagnostic(item.slot))
			if callbacks != [3]int{} || !errors.Is(h.lastError, errRetestInputCleanup) {
				t.Fatal("owned key cleanup precedence changed")
			}
			for _, f := range h.fixtures {
				assertRetestInputLedger(t, f, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			}
		})
	}
}

func TestCaptureObservationAdmissionOrder(t *testing.T) {
	for _, name := range []string{"short_fingerprint", "short_signing", "inconsistent_signer", "fingerprint_cancel", "signing_cancel", "entropy_cancel", "entropy_error", "missing_key", "nil_collector"} {
		t.Run(name, func(t *testing.T) {
			h := newCaptureObservationHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			wantCode, wantKeys := 1, 2
			wantDiagnostic := "capture input acquisition failed\n"
			var wantEffects []string
			switch name {
			case "short_fingerprint":
				h.keys[0] = h.keys[0][:31]
				wantKeys = 1
				wantDiagnostic = captureInputDiagnostic(0)
			case "short_signing":
				h.keys[1] = h.keys[1][:63]
				wantDiagnostic = captureInputDiagnostic(1)
			case "inconsistent_signer":
				h.keys[1][63] ^= 1
				wantDiagnostic = "read signing key: inconsistent Ed25519 seed and public key\n"
			case "fingerprint_cancel", "signing_cancel":
				slot := 1
				if name == "fingerprint_cancel" {
					slot = 0
					wantKeys = 1
				}
				h.afterRead = func(index int) {
					if index == slot {
						cancel()
					}
				}
			case "entropy_cancel":
				h.args = h.args[:len(h.args)-2]
				h.randomHook = cancel
				wantEffects = []string{"random"}
			case "entropy_error":
				h.args = h.args[:len(h.args)-2]
				h.random = captureInputReadFunc(func([]byte) (int, error) {
					h.effects = append(h.effects, "random")
					return 0, errors.New("synthetic entropy failure")
				})
				wantEffects = []string{"random"}
				wantDiagnostic = "generate session ID: synthetic entropy failure\n"
			case "missing_key":
				h.setFlag("-fingerprint-key", "")
				wantCode, wantKeys = 2, 0
				wantDiagnostic = "capture requires -fingerprint-key, -signing-key, -key-id, and -output\n"
			case "nil_collector":
				h.collect = nil
				wantDiagnostic = "collect inventory: inventory: invalid observation diagnostics\n"
			}
			code := h.runObservation(ctx, false)
			h.requireKeysClosed()
			if code != wantCode || len(h.seen) != wantKeys || !reflect.DeepEqual(h.effects, wantEffects) ||
				h.collectCalls != 0 || h.output.Len() != 0 || h.stdout.Len() != 0 || h.stderr.String() != wantDiagnostic {
				t.Fatal("key/entropy admission continued into observation or later effects")
			}
		})
	}
}

func TestCaptureObservationFatalCollection(t *testing.T) {
	for _, name := range []string{"overflow", "nonregular", "cancel", "close", "close_cancel", "successful_cancel"} {
		t.Run(name, func(t *testing.T) {
			h := newCaptureObservationHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := inventory.ErrScalarLimit
			if name == "close" || name == "close_cancel" {
				want = inventory.ErrBulkClose
				h.collect = func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
					if name == "close_cancel" {
						cancel()
					}
					// Inject an established close failure; real owned-close
					// behavior remains covered by inventory reader tests.
					return inventoryPlanFingerprintSnapshot(), inventory.CollectionDiagnostics{CPUFailures: ^uint32(0)}, inventory.ErrBulkClose
				}
			} else if name == "successful_cancel" {
				want = context.Canceled
				h.collect = func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
					cancel()
					return inventoryPlanFingerprintSnapshot(), inventory.CollectionDiagnostics{InputFailures: 1}, nil
				}
			} else {
				source, root := newPlanObservationSource(t)
				target := filepath.Join(root, "sys", "class", "dmi", "id", "product_name")
				var collect inventoryDiagnosticCollector
				switch name {
				case "overflow":
					if err := os.WriteFile(target, bytes.Repeat([]byte{'x'}, 65537), 0600); err != nil {
						t.Fatal(err)
					}
					collect = inventory.NewLinuxCollector(root).CollectWithDiagnostics
				case "nonregular":
					if err := os.Remove(target); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(target, 0700); err != nil {
						t.Fatal(err)
					}
					want = inventory.ErrScalarRead
					collect = inventory.NewLinuxCollector(root).CollectWithDiagnostics
				case "cancel":
					source.cancel = cancel
					want = context.Canceled
					collect = inventory.NewLinuxCollectorWithSource(source).CollectWithDiagnostics
				}
				h.collect = func(ctx context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
					snapshot, diagnostics, err := collect(ctx)
					if err != want || snapshot.SchemaVersion != 0 || snapshot.Components != nil || diagnostics != (inventory.CollectionDiagnostics{}) {
						t.Fatal("real fatal collection retained partial success")
					}
					return snapshot, diagnostics, err
				}
			}
			if h.runObservation(ctx, false) != 1 || !reflect.DeepEqual(h.effects, []string{"collect"}) ||
				h.stderr.String() != "collect inventory: "+want.Error()+"\n" || h.output.Len() != 0 || h.stdout.Len() != 0 {
				t.Fatal("fatal acquisition/cancellation reached a warning or worker")
			}
		})
	}
}

func TestCaptureObservationRetainsCleanupStops(t *testing.T) {
	for _, name := range []string{"cpu", "memory", "graphics"} {
		t.Run(name, func(t *testing.T) {
			h, old := newCaptureObservationHarness(t), newCaptureObservationHarness(t)
			h.diagnostics = inventory.CollectionDiagnostics{InputFailures: 1}
			h.stopWorker, old.stopWorker = name, name
			if old.runObservation(t.Context(), true) != workerCleanupUncertainExitCode {
				t.Fatal("legacy cleanup prerequisite failed")
			}
			if h.runObservation(t.Context(), false) != workerCleanupUncertainExitCode ||
				h.stderr.String() != "inventory: metadata reads failed (scalar=0 cpu=0 memory=0 input=1)\n"+old.stderr.String() ||
				h.output.Len() != 0 || h.stdout.Len() != 0 {
				t.Fatal("diagnostics changed worker cleanup authority")
			}
			want := append([]string{"collect", "warning"}, old.effects[1:]...)
			if !reflect.DeepEqual(h.effects, want) || strings.Contains(strings.Join(h.effects, ","), "clock") {
				t.Fatal("cleanup uncertainty reached later workers/signing")
			}
		})
	}
}

func TestCaptureObservationDestinationClose(t *testing.T) {
	h := newCaptureObservationHarness(t)
	h.diagnostics = inventory.CollectionDiagnostics{InputFailures: 1}
	destination := &planObservationDestination{}
	opens := 0
	h.destination = func(path string, out io.Writer, write func(io.Writer) error) error {
		h.effects = append(h.effects, "output")
		return writeDestinationWith(path, out, write, func(name string, _ int, _ os.FileMode) (io.WriteCloser, error) {
			opens++
			if name != "owned-output" || h.stderr.String() != "inventory: metadata reads failed (scalar=0 cpu=0 memory=0 input=1)\n" {
				t.Fatal("output ownership preceded warning")
			}
			return destination, nil
		})
	}
	if h.runObservation(t.Context(), false) != 1 || opens != 1 || destination.closed != 1 || destination.Len() == 0 || h.stdout.Len() != 0 {
		t.Fatal("owned destination close failure became successful capture")
	}
}

func TestCaptureObservationInjectedErrorRemainsLegacy(t *testing.T) {
	h := newCaptureObservationHarness(t)
	var callbacks [3]int
	h.collect = func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
		return inventoryPlanFingerprintSnapshot(), inventory.CollectionDiagnostics{InputFailures: 1}, &planObservationProviderError{&callbacks}
	}
	// This arbitrary injected collector error is intentionally formatted by the
	// unchanged core. It is not a real PathSource error escaping acquisition.
	if h.runObservation(t.Context(), false) != 1 || callbacks != [3]int{1, 0, 0} ||
		!reflect.DeepEqual(h.effects, []string{"collect"}) || h.output.Len() != 0 ||
		h.stderr.String() != "collect inventory: SYNTHETIC-PROVIDER-DETAIL-MUST-NOT-APPEAR\n" {
		t.Fatal("legacy injected-error formatting contract changed")
	}
}
