package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
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
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/usbtest"
)

// Unsupported platforms retain the portable consumer/worker oracles using
// owned test-file bytes and injected owners. This does not qualify their real
// input adapter. Linux and Windows continue through the production adapter.
func captureFixtureInput(t *testing.T) retestInputReader {
	t.Helper()
	return func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
		if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
			return readRetestInput(ctx, path, kind)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, errRetestInput
		}
		fixture := newRetestInputFixture(len(raw))
		fixture.readable.data = append([]byte(nil), raw...)
		data, err := readRetestInputWithOperations(ctx, path, kind, fixture.operations())
		if !reflect.DeepEqual(fixture.ledger.acquired, fixture.ledger.closed) {
			t.Fatal("portable test input owner remains open")
		}
		return data, err
	}
}

func runCaptureTestWith(t *testing.T, ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, probeGraphics graphicsCollector, probeCPU cpuCollector, probeMemory memoryCollector, probeInput inputCollector, probeNetwork networkCollector, probeStorage storageCollector, probeUSB usbCollector, random io.Reader, now func() time.Time) int {
	t.Helper()
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		return runCaptureWith(ctx, arguments, stdout, stderr, collect, probeGraphics, probeCPU, probeMemory, probeInput, probeNetwork, probeStorage, probeUSB, random, now)
	}
	return runCaptureWithDestination(ctx, arguments, stdout, stderr, collect, probeGraphics, probeCPU, probeMemory, probeInput, probeNetwork, probeStorage, probeUSB, random, now, writeDestination, captureFixtureInput(t))
}

type captureInputReadFunc func([]byte) (int, error)

func (read captureInputReadFunc) Read(data []byte) (int, error) { return read(data) }

type captureInputHarness struct {
	t          *testing.T
	args       []string
	keys       [][]byte
	seen       []retestInputKind
	fixtures   []*retestInputFixture
	effects    []string
	failAt     int
	mutate     func(*retestInputFixture)
	afterRead  func(int)
	randomHook func()
	lastError  error
	stdout     bytes.Buffer
	stderr     bytes.Buffer
	output     bytes.Buffer
}

func newCaptureInputHarness(t *testing.T) *captureInputHarness {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x67}, ed25519.SeedSize))
	if len(private) != 64 || !bytes.Equal(private, ed25519.NewKeyFromSeed(private.Seed())) {
		t.Fatal("valid private-key fixture prerequisite")
	}
	return &captureInputHarness{
		t: t, failAt: -1,
		args: []string{"-fingerprint-key", "fingerprint-path-marker", "-signing-key", "signing-path-marker", "-key-id", " capture-fixture-key ", "-key-epoch", "capture-epoch", "-output", "owned-output", "-graphics-pattern-frames", "12", "-memory-bytes", "4194304", "-memory-passes", "1"},
		keys: [][]byte{bytes.Repeat([]byte{0x35}, 32), append([]byte(nil), private...)},
	}
}

func (h *captureInputHarness) read(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
	h.t.Helper()
	index := len(h.seen)
	wantPaths := []string{"fingerprint-path-marker", "signing-path-marker"}
	wantKinds := []retestInputKind{retestFingerprintKeyInput, retestSigningKeyInput}
	if index >= 2 || path != wantPaths[index] || kind != wantKinds[index] {
		h.t.Fatal("unexpected key role or later acquisition")
	}
	for _, earlier := range h.fixtures {
		assertRetestInputLedger(h.t, earlier, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
	}
	h.seen = append(h.seen, kind)
	fixture := newRetestInputFixture(len(h.keys[index]))
	fixture.readable.data = append([]byte(nil), h.keys[index]...)
	if index == h.failAt && h.mutate != nil {
		h.mutate(fixture)
	}
	h.fixtures = append(h.fixtures, fixture)
	data, err := readRetestInputWithOperations(ctx, path, kind, fixture.operations())
	if err != nil && data != nil {
		h.t.Fatal("acquisition failure released key bytes")
	}
	if err == nil && !bytes.Equal(data, h.keys[index]) {
		h.t.Fatal("captured key bytes differ")
	}
	h.lastError = err
	if h.afterRead != nil {
		h.afterRead(index)
	}
	return data, err
}

func (h *captureInputHarness) write(_ string, _ io.Writer, write func(io.Writer) error) error {
	h.effects = append(h.effects, "output")
	return write(&h.output)
}

func (h *captureInputHarness) run(ctx context.Context, read retestInputReader, write destinationWriter) int {
	h.t.Helper()
	return runCaptureWithDestination(ctx, h.args, &h.stdout, &h.stderr,
		func(context.Context) (inventory.Snapshot, error) {
			h.effects = append(h.effects, "collect")
			for _, fixture := range h.fixtures {
				assertRetestInputLedger(h.t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			}
			return inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"serial": "capture-fixture-serial", "model": "Fixture"})}), nil
		},
		func(context.Context, int) graphics.Result {
			h.effects = append(h.effects, "graphics")
			return completeGraphicsEvidence()
		},
		func(context.Context, quick.CPUOptions) quick.CPUResult {
			h.effects = append(h.effects, "cpu")
			return completeCPUEvidence()
		},
		func(context.Context, quick.MemoryOptions) quick.MemoryResult {
			h.effects = append(h.effects, "memory")
			return completeMemoryEvidence()
		},
		func(context.Context, inputtest.Options) inputtest.Result {
			h.effects = append(h.effects, "input")
			return completeInputEvidence()
		},
		func(context.Context, networktest.Options) networktest.Result {
			h.effects = append(h.effects, "network")
			return completeNetworkEvidence()
		},
		func(context.Context, storage.Options) storage.Result {
			h.effects = append(h.effects, "storage")
			return completeStorageEvidence()
		},
		func(context.Context, usbtest.Options) usbtest.Result {
			h.effects = append(h.effects, "usb")
			return completeUSBEvidence()
		},
		captureInputReadFunc(func(data []byte) (int, error) {
			h.effects = append(h.effects, "random")
			if h.randomHook != nil {
				h.randomHook()
			}
			return copy(data, bytes.Repeat([]byte{0xa7}, 16)), nil
		}),
		func() time.Time {
			h.effects = append(h.effects, "clock")
			return time.Date(2026, 10, 28, 1, 2, 3, 4, time.UTC)
		}, write, read)
}

func (h *captureInputHarness) stopped(code, keyCalls int, diagnostic string) {
	h.t.Helper()
	if code != 1 || len(h.seen) != keyCalls || len(h.effects) != 0 || h.stdout.Len() != 0 || h.output.Len() != 0 || h.stderr.String() != diagnostic {
		h.t.Fatalf("capture crossed failure boundary: code=%d keys=%d effects=%v stderr=%q", code, len(h.seen), h.effects, h.stderr.String())
	}
	for _, fixture := range h.fixtures {
		if !reflect.DeepEqual(fixture.ledger.acquired, fixture.ledger.closed) {
			h.t.Fatal("acquired key owner was not closed")
		}
	}
}

func captureInputDiagnostic(slot int) string {
	if slot == 0 {
		return "read fingerprint key: input acquisition failed\n"
	}
	return "read signing key: input acquisition failed\n"
}

func TestCaptureInputKindsAndExactCompletion(t *testing.T) {
	for _, test := range []struct {
		name string
		kind retestInputKind
		size int
		pass bool
	}{
		{"fingerprint-short", retestFingerprintKeyInput, 31, false}, {"fingerprint-exact", retestFingerprintKeyInput, 32, true}, {"fingerprint-long", retestFingerprintKeyInput, 33, false},
		{"signing-short", retestSigningKeyInput, 63, false}, {"signing-exact", retestSigningKeyInput, 64, true}, {"signing-long", retestSigningKeyInput, 65, false},
		{"public-exact", retestPublicKeyInput, 32, true}, {"public-not-private", retestPublicKeyInput, 64, false}, {"private-not-public", retestSigningKeyInput, 32, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRetestInputFixture(test.size)
			want := append([]byte(nil), fixture.readable.data...)
			data, err := readRetestInputWithOperations(t.Context(), "fixture", test.kind, fixture.operations())
			if test.pass {
				if err != nil || !bytes.Equal(data, want) || !reflect.DeepEqual(fixture.readable.readSize, []int{test.size, 1}) {
					t.Fatal("exact key/probe contract", err)
				}
				assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				fixture.readable.data[0] ^= 1
				if !bytes.Equal(data, want) {
					t.Fatal("key bytes alias provider storage")
				}
			} else {
				assertRetestInputFailure(t, data, err, false)
				assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1}, []string{"metadata"})
				if len(fixture.readable.readSize) != 0 {
					t.Fatal("wrong size reached readable input")
				}
			}
		})
	}
}

func TestCaptureInputFinalEOFStillRequiresProbe(t *testing.T) {
	for _, key := range []struct {
		kind retestInputKind
		size int
	}{{retestFingerprintKeyInput, 32}, {retestSigningKeyInput, 64}} {
		for _, end := range []error{nil, io.EOF} {
			for _, extra := range []bool{false, true} {
				t.Run(fmt.Sprintf("kind_%d/eof_%t/extra_%t", key.kind, end == io.EOF, extra), func(t *testing.T) {
					fixture := newRetestInputFixture(key.size)
					n := 0
					if extra {
						n = 1
					}
					fixture.readable.reads = []retestInputTestRead{{key.size, end}, {n, io.EOF}}
					data, err := readRetestInputWithOperations(t.Context(), "fixture", key.kind, fixture.operations())
					if extra {
						assertRetestInputFailure(t, data, err, false)
					} else if err != nil || !bytes.Equal(data, fixture.readable.data) {
						t.Fatal("valid exact EOF input", err)
					}
					if !reflect.DeepEqual(fixture.readable.readSize, []int{key.size, 1}) {
						t.Fatal("missing bounded overflow probe")
					}
					assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				})
			}
		}
	}
}

func TestCaptureInputSignedOutputAndClosedOwnership(t *testing.T) {
	for _, supplied := range []bool{false, true} {
		t.Run(fmt.Sprintf("supplied_session_%t", supplied), func(t *testing.T) {
			h := newCaptureInputHarness(t)
			wantID := strings.Repeat("a7", 16)
			wantEffects := []string{"random", "collect", "cpu", "memory", "graphics", "input", "network", "storage", "usb", "clock", "output"}
			if supplied {
				h.args = append(h.args, "-session-id", " capture-supplied-session ")
				wantID = "capture-supplied-session"
				wantEffects = wantEffects[1:]
			}
			if code := h.run(t.Context(), h.read, h.write); code != 0 || h.stderr.Len() != 0 || h.stdout.Len() != 0 || !reflect.DeepEqual(h.effects, wantEffects) || !reflect.DeepEqual(h.seen, []retestInputKind{retestFingerprintKeyInput, retestSigningKeyInput}) {
				t.Fatalf("capture success changed: %d %s %v", code, h.stderr.String(), h.effects)
			}
			public := ed25519.PrivateKey(h.keys[1]).Public().(ed25519.PublicKey)
			artifact, err := bundle.Verify(bytes.NewReader(h.output.Bytes()), int64(h.output.Len()), report.Keyring{"capture-fixture-key": public})
			if err != nil {
				t.Fatal("independent signed archive verification", err)
			}
			if artifact.Envelope.KeyID != "capture-fixture-key" || artifact.Envelope.SessionID != wantID || artifact.Envelope.GeneratedTime != "2026-10-28T01:02:03.000000004Z" || len(artifact.RenderedReport) == 0 || len(artifact.RenderedPDF) == 0 || bytes.Contains(artifact.InventoryJSON, []byte("capture-fixture-serial")) {
				t.Fatal("signed identity/session/views changed")
			}
			var payload app.InventoryPayload
			if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
				t.Fatal(err)
			}
			// Literal fixture HMACs independently derived from the documented domain-separated bytes.
			if payload.CoverageScope != app.CoverageDirectCapture || payload.SystemFingerprint.KeyEpoch != "capture-epoch" || payload.SystemFingerprint.KeyID != "hmac-sha256:e129e038c0c0c46e52eabbe8389dda6c14308f797644afef6bba8e11f99f9be7" || payload.SystemFingerprint.Value != "hmac-sha256:e732f5268d3a8a586bc8fc60deda0f1d8ecf9abdd75cd6f0edccb63df06c5972" || payload.SystemFingerprint.IdentifiedParts != 1 || len(payload.Components) != 1 || payload.Components[0].Identity.Source != "serial" || payload.Components[0].Identity.KeyEpoch != "capture-epoch" || payload.Components[0].Identity.Value != "hmac-sha256:9670d998f4774fc641208d0b799b73e0ab771b0a1dfa303d487e93894e874d88" || len(payload.TestResults) != 7 {
				t.Fatal("capture HMAC/scope contract changed")
			}
			wantRows := []string{"cpu.quick", "graphics.quick", "input.readiness.quick", "memory.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick"}
			for i, row := range payload.TestResults {
				if row.ID != wantRows[i] || row.Status != app.TestPass || row.PlanID != "" || row.PlannedStatus != "" || row.HardwareGraph != "" {
					t.Fatal("direct capture rows changed")
				}
			}
		})
	}
}

func TestCaptureInputCloseFailuresStopAllEffects(t *testing.T) {
	for slot := 0; slot < 2; slot++ {
		for _, owner := range []string{"readable", "metadata"} {
			for _, stopped := range []bool{false, true} {
				t.Run(fmt.Sprintf("role_%d/%s/cancel_%t", slot, owner, stopped), func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					h := newCaptureInputHarness(t)
					h.failAt = slot
					h.mutate = func(f *retestInputFixture) {
						file := f.readable
						if owner == "metadata" {
							file = f.metadata
						}
						file.closeErr = errors.New("provider-close-marker")
						if stopped {
							file.hook = func(op string) {
								if op == "close" {
									cancel()
								}
							}
						}
					}
					code := h.run(ctx, h.read, h.write)
					h.stopped(code, slot+1, captureInputDiagnostic(slot))
					if !errors.Is(h.lastError, errRetestInputCleanup) || !errors.Is(h.lastError, errRetestInput) {
						t.Fatal("close uncertainty was disguised", h.lastError)
					}
					for _, f := range h.fixtures {
						assertRetestInputLedger(t, f, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
					}
				})
			}
		}
	}
}

func TestCaptureInputMalformedProvidersStopAllEffects(t *testing.T) {
	for slot := 0; slot < 2; slot++ {
		for _, test := range []struct {
			name   string
			mutate func(*retestInputFixture)
			owners map[string]int
			closes []string
		}{
			{"metadata-and-error", func(f *retestInputFixture) { f.metadataErr = errors.New("metadata-marker") }, map[string]int{"metadata": 1}, []string{"metadata"}},
			{"typed-nil-metadata", func(f *retestInputFixture) { var file *retestInputTestFile; f.metadataOut = file }, map[string]int{}, nil},
			{"readable-and-error", func(f *retestInputFixture) { f.readableErr = errors.New("readable-marker") }, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"}},
			{"typed-nil-readable", func(f *retestInputFixture) { var file *retestInputTestFile; f.readableOut = file }, map[string]int{"metadata": 1}, []string{"metadata"}},
			{"aliased-owner", func(f *retestInputFixture) { f.readableOut = f.metadata }, map[string]int{"metadata": 1}, []string{"metadata"}},
			{"nonregular", func(f *retestInputFixture) { f.metadata.info.mode = os.ModeNamedPipe }, map[string]int{"metadata": 1}, []string{"metadata"}},
			{"identity-substitution", func(f *retestInputFixture) { changed := *f.readable.info; changed.id++; f.readable.info = &changed }, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"}},
			{"early-eof", func(f *retestInputFixture) { f.readable.reads = []retestInputTestRead{{1, io.EOF}} }, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"}},
			{"zero-progress", func(f *retestInputFixture) { f.readable.reads = []retestInputTestRead{{0, nil}} }, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"}},
			{"invalid-count", func(f *retestInputFixture) { f.readable.reads = []retestInputTestRead{{-1, nil}} }, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"}},
			{"bytes-hard-error", func(f *retestInputFixture) { f.readable.reads = []retestInputTestRead{{1, errors.New("read-marker")}} }, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"}},
			{"probe-error", func(f *retestInputFixture) {
				f.readable.reads = []retestInputTestRead{{len(f.readable.data), io.EOF}, {0, errors.New("probe-marker")}}
			}, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"}},
			{"final-stamp", func(f *retestInputFixture) {
				f.metadata.stats = []retestInputTestStat{{f.metadata.info, nil}, {nil, errors.New("final-stamp-marker")}}
			}, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"}},
		} {
			t.Run(fmt.Sprintf("role_%d/%s", slot, test.name), func(t *testing.T) {
				h := newCaptureInputHarness(t)
				h.failAt, h.mutate = slot, test.mutate
				code := h.run(t.Context(), h.read, h.write)
				h.stopped(code, slot+1, captureInputDiagnostic(slot))
				if !errors.Is(h.lastError, errRetestInput) || errors.Is(h.lastError, errRetestInputCleanup) {
					t.Fatal("provider fault classification changed", h.lastError)
				}
				for i, f := range h.fixtures {
					if i == slot {
						assertRetestInputLedger(t, f, test.owners, test.closes)
					} else {
						assertRetestInputLedger(t, f, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
					}
				}
			})
		}
	}
}

func TestCaptureInputCancellationAndHardErrorPrecedence(t *testing.T) {
	for slot := 0; slot < 2; slot++ {
		for _, boundary := range []string{"metadata-open", "readable-open", "read", "final-stat", "readable-close", "metadata-close"} {
			for _, hard := range []bool{false, true} {
				t.Run(fmt.Sprintf("role_%d/%s/hard_%t", slot, boundary, hard), func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					h := newCaptureInputHarness(t)
					h.failAt = slot
					h.mutate = func(f *retestInputFixture) {
						fault := errors.New("hard-provider-marker")
						switch boundary {
						case "metadata-open":
							f.metadataHook = cancel
							if hard {
								f.metadataErr = fault
							}
						case "readable-open":
							f.readableHook = cancel
							if hard {
								f.readableErr = fault
							}
						case "read":
							f.readable.hook = func(op string) {
								if op == "read" {
									cancel()
								}
							}
							if hard {
								f.readable.reads = []retestInputTestRead{{0, fault}}
							}
						case "final-stat":
							count := 0
							f.metadata.hook = func(op string) {
								if op == "stat" {
									count++
									if count == 2 {
										cancel()
									}
								}
							}
							if hard {
								f.metadata.stats = []retestInputTestStat{{f.metadata.info, nil}, {nil, fault}}
							}
						case "readable-close", "metadata-close":
							file := f.readable
							if boundary == "metadata-close" {
								file = f.metadata
							}
							file.hook = func(op string) {
								if op == "close" {
									cancel()
								}
							}
							if hard {
								file.closeErr = fault
							}
						}
					}
					code := h.run(ctx, h.read, h.write)
					h.stopped(code, slot+1, captureInputDiagnostic(slot))
					cleanup := hard && strings.HasSuffix(boundary, "-close")
					if hard {
						if !errors.Is(h.lastError, errRetestInput) || errors.Is(h.lastError, context.Canceled) || errors.Is(h.lastError, errRetestInputCleanup) != cleanup {
							t.Fatal("hard failure lost to cancellation", h.lastError)
						}
					} else if h.lastError != context.Canceled {
						t.Fatal("pure cancellation classification", h.lastError)
					}
					for i, f := range h.fixtures {
						if i == slot && boundary == "metadata-open" {
							assertRetestInputLedger(t, f, map[string]int{"metadata": 1}, []string{"metadata"})
						} else {
							assertRetestInputLedger(t, f, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
						}
					}
				})
			}
		}
	}
}

func TestCaptureInputConsumerShapeAndPrivateKeyPolicy(t *testing.T) {
	for slot := 0; slot < 2; slot++ {
		for _, shape := range []string{"nil", "short", "long", "bytes-and-error", "inconsistent-private"} {
			if shape == "inconsistent-private" && slot == 0 {
				continue
			}
			for _, stopped := range []bool{false, true} {
				t.Run(fmt.Sprintf("role_%d/%s/cancel_%t", slot, shape, stopped), func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					h := newCaptureInputHarness(t)
					read := func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
						data, err := h.read(ctx, path, kind)
						if err != nil {
							t.Fatal("valid acquisition prerequisite", err)
						}
						if len(h.seen)-1 != slot {
							return data, nil
						}
						if stopped {
							cancel()
						}
						switch shape {
						case "nil":
							return nil, nil
						case "short":
							return data[:len(data)-1], nil
						case "long":
							return append(data, 1), nil
						case "bytes-and-error":
							return data, errors.New("reader-error-marker")
						case "inconsistent-private":
							data[63] ^= 1
							if len(data) != 64 || bytes.Equal(data, ed25519.NewKeyFromSeed(data[:32])) {
								t.Fatal("inconsistent private64 prerequisite")
							}
							return data, nil
						}
						t.Fatal("unknown fixture")
						return nil, errRetestInput
					}
					want := captureInputDiagnostic(slot)
					if shape == "inconsistent-private" {
						want = "read signing key: inconsistent Ed25519 seed and public key\n"
					}
					h.stopped(h.run(ctx, read, h.write), slot+1, want)
					for _, f := range h.fixtures {
						assertRetestInputLedger(t, f, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
					}
				})
			}
		}
	}
}

func TestCaptureInputUsageAndRequiredDependencies(t *testing.T) {
	for _, test := range []string{"missing-flags", "positional", "unbounded-duration", "nil-context", "typed-nil-context", "stopped", "deadline", "nil-reader", "nil-writer"} {
		t.Run(test, func(t *testing.T) {
			h := newCaptureInputHarness(t)
			ctx := context.Context(t.Context())
			read, write := retestInputReader(h.read), destinationWriter(h.write)
			want := 1
			switch test {
			case "missing-flags":
				h.args = nil
				want = 2
			case "positional":
				h.args = append(h.args, "unexpected")
				want = 2
			case "unbounded-duration":
				h.args = append(h.args, "-input-duration", "6m")
				want = 2
			case "nil-context":
				ctx = nil
			case "typed-nil-context":
				var stopped *retestInputStoppedContext
				ctx = stopped
			case "stopped":
				stopped, cancel := context.WithCancel(t.Context())
				cancel()
				ctx = stopped
			case "deadline":
				stopped, cancel := context.WithDeadline(t.Context(), time.Unix(0, 0))
				defer cancel()
				ctx = stopped
			case "nil-reader":
				read = nil
			case "nil-writer":
				write = nil
			}
			code := h.run(ctx, read, write)
			if code != want || len(h.seen) != 0 || len(h.effects) != 0 || h.output.Len() != 0 || h.stdout.Len() != 0 || h.stderr.Len() == 0 {
				t.Fatal("pre-acquisition admission failed", code)
			}
			if want == 1 && h.stderr.String() != "capture input acquisition failed\n" {
				t.Fatal("dependency diagnostic changed")
			}
		})
	}
}

func TestCaptureInputCancellationBeforeLaterEffects(t *testing.T) {
	for _, boundary := range []string{"fingerprint-return", "signing-return", "random-return"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h := newCaptureInputHarness(t)
			count := 2
			if boundary == "random-return" {
				h.randomHook = cancel
			} else {
				slot := 1
				if boundary == "fingerprint-return" {
					slot = 0
					count = 1
				}
				h.afterRead = func(index int) {
					if index == slot {
						cancel()
					}
				}
			}
			code := h.run(ctx, h.read, h.write)
			if code != 1 || len(h.seen) != count || h.stderr.String() != "capture input acquisition failed\n" || h.output.Len() != 0 || h.stdout.Len() != 0 {
				t.Fatal("late key cancellation escaped")
			}
			if boundary == "random-return" {
				if !reflect.DeepEqual(h.effects, []string{"random"}) {
					t.Fatal("collection began after cancellation")
				}
			} else if len(h.effects) != 0 {
				t.Fatal("later effect began after key cancellation")
			}
			for _, f := range h.fixtures {
				assertRetestInputLedger(t, f, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			}
		})
	}
}

func TestCaptureInputStoppedProductionDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	h := newCaptureInputHarness(t)
	args := append([]string{"capture"}, h.args...)
	code := run(ctx, args, bytes.NewReader(nil), &h.stdout, &h.stderr)
	if code != 1 || h.stdout.Len() != 0 || h.stderr.String() != "capture input acquisition failed\n" {
		t.Fatal("actual capture dispatch ignored stopped context", code, h.stderr.String())
	}
}
