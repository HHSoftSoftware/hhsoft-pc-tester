package main

import (
	"bytes"
	"context"
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
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

type finalizerFingerprintHarness struct {
	t                                        *testing.T
	args                                     []string
	directory, keyPath, planPath, outputPath string
	key                                      []byte
	snapshot                                 inventory.Snapshot
	frozen                                   plan.Plan
	cpu                                      []byte
	fixture                                  *retestInputFixture
	reads, coreCalls                         int
	lastError                                error
	effects                                  []string
	stdout, stderr, output                   bytes.Buffer
}

func newFinalizerFingerprintHarness(t *testing.T) *finalizerFingerprintHarness {
	t.Helper()
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x35}, 32)
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentCPU, "cpu:0", map[string]string{"serial": "finalizer-fixture-serial"})})
	keyPath := writeTestKey(t, directory, "fingerprint.key", key)
	cpu := encodeEvidenceFixture(t, completeCPUEvidence())
	if err := os.WriteFile(filepath.Join(directory, "cpu.json"), cpu, 0600); err != nil {
		t.Fatal(err)
	}
	planPath := writeFinalizeTestPlan(t, directory, snapshot, key)
	raw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := plan.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	cpuScheduled := false
	for _, row := range frozen.Tests {
		if row.ID == "cpu.quick" {
			cpuScheduled = row.Status == plan.TestScheduled
		}
	}
	if !cpuScheduled || frozen.HardwareGraph.KeyEpoch != "ephemeral-session" {
		t.Fatal("invalid positive frozen-plan prerequisite")
	}
	outputPath := filepath.Join(directory, "final.hhpcr")
	args := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "finalizer-fixture", "-output", outputPath}
	for _, definition := range currentResultDefinitionsForTest(t, frozen) {
		outcome := "not_present"
		if definition.id == "inspection.exterior.quick" {
			outcome = "excluded"
		}
		if definition.id == "cpu.quick" {
			outcome = "pass"
		}
		args = append(args, "-outcome", definition.id+"="+outcome)
	}
	fixture := newRetestInputFixture(32)
	fixture.readable.data = append([]byte(nil), key...)
	return &finalizerFingerprintHarness{t: t, args: args, directory: directory, keyPath: keyPath, planPath: planPath, outputPath: outputPath, key: key, snapshot: snapshot, frozen: frozen, cpu: cpu, fixture: fixture}
}

func (h *finalizerFingerprintHarness) read(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
	h.t.Helper()
	h.reads++
	if h.reads != 1 || path != h.keyPath || kind != retestFingerprintKeyInput {
		h.t.Fatal("wrong or repeated fingerprint acquisition")
	}
	h.coreCalls++
	data, err := readRetestInputWithOperations(ctx, path, kind, h.fixture.operations())
	h.lastError = err
	if err != nil && data != nil {
		h.t.Fatal("failed owned acquisition released bytes")
	}
	return data, err
}

func (h *finalizerFingerprintHarness) closedBeforeEffect() {
	h.t.Helper()
	if h.coreCalls != 0 && !reflect.DeepEqual(h.fixture.ledger.acquired, h.fixture.ledger.closed) {
		h.t.Fatal("effect before input ownership ended")
	}
}

func (h *finalizerFingerprintHarness) collect(context.Context) (inventory.Snapshot, error) {
	h.closedBeforeEffect()
	h.effects = append(h.effects, "collect")
	return h.snapshot, nil
}

func (h *finalizerFingerprintHarness) Read(data []byte) (int, error) {
	h.closedBeforeEffect()
	h.effects = append(h.effects, "random")
	for i := range data {
		data[i] = 0x62
	}
	return len(data), nil
}

func (h *finalizerFingerprintHarness) now() time.Time {
	h.closedBeforeEffect()
	h.effects = append(h.effects, "clock")
	return time.Date(2026, 10, 28, 1, 2, 3, 4, time.UTC)
}

func (h *finalizerFingerprintHarness) write(path string, _ io.Writer, write func(io.Writer) error) error {
	h.closedBeforeEffect()
	h.effects = append(h.effects, "output")
	if path != h.outputPath {
		h.t.Fatal("output destination changed")
	}
	return write(&h.output)
}

func (h *finalizerFingerprintHarness) forbiddenGate(context.Context) (sessionJournal, error) {
	h.effects = append(h.effects, "gate")
	return sessionJournal{}, errors.New("unexpected gate after key rejection")
}

func (h *finalizerFingerprintHarness) run(ctx context.Context, read retestInputReader, previous *verifiedPreviousReport, gate sessionFinalGate) sessionFinalizationResult {
	h.t.Helper()
	return runFinalizeOwnedDestinationWithPrevious(ctx, h.args, &h.stdout, &h.stderr, h.collect, h, h.now, gate, h.write, previous, read)
}

func (h *finalizerFingerprintHarness) stopped(result sessionFinalizationResult, reads int, diagnostic string) {
	h.t.Helper()
	if result.Code != 1 || result.Journal != nil || h.reads != reads || len(h.effects) != 0 || h.stdout.Len() != 0 || h.output.Len() != 0 || h.stderr.String() != diagnostic {
		h.t.Fatalf("input rejection boundary: code=%d reads=%d effects=%v diagnostic=%q", result.Code, h.reads, h.effects, h.stderr.String())
	}
	if _, err := os.Stat(h.outputPath); !errors.Is(err, os.ErrNotExist) {
		h.t.Fatal("rejected acquisition created a report")
	}
	if h.coreCalls != 0 && !reflect.DeepEqual(h.fixture.ledger.acquired, h.fixture.ledger.closed) {
		h.t.Fatal("input rejection leaked an acquired owner")
	}
}

func (h *finalizerFingerprintHarness) success(result sessionFinalizationResult) bundle.Artifact {
	h.t.Helper()
	if result.Code != 0 || h.stderr.Len() != 0 || !strings.Contains(h.stdout.String(), "finalized: signed self-issued report") || h.output.Len() == 0 {
		h.t.Fatalf("valid finalizer prerequisite failed: %d %s", result.Code, &h.stderr)
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(h.output.Bytes()), int64(h.output.Len()))
	if err != nil {
		h.t.Fatal("signed output did not verify", err)
	}
	var payload app.InventoryPayload
	if json.Unmarshal(artifact.InventoryJSON, &payload) != nil {
		h.t.Fatal("invalid signed inventory")
	}
	// Fixed HMAC-SHA256 vectors keep this oracle independent of the identity producer.
	if payload.CoverageScope != app.CoverageFrozenPlan || payload.SystemFingerprint.KeyEpoch != "ephemeral-session" || payload.SystemFingerprint.KeyID != "hmac-sha256:e129e038c0c0c46e52eabbe8389dda6c14308f797644afef6bba8e11f99f9be7" || payload.SystemFingerprint.Value != "hmac-sha256:c25ecf00282419d80605d4e60de7d0806672f9d2c3c8907734da9d0420dc3d7b" || payload.SystemFingerprint.IdentifiedParts != 1 || len(payload.Components) != 1 || payload.Components[0].Identity.Value != "hmac-sha256:fc0ff990771b45a86cc7f7733b5b43ba3c3301854eac68b4c6e935f7a9aaecea" {
		h.t.Fatal("fingerprint, epoch, or coverage changed")
	}
	if artifact.Envelope.GeneratedTime != "2026-10-28T01:02:03.000000004Z" || bytes.Contains(artifact.InventoryJSON, []byte("finalizer-fixture-serial")) || bytes.Contains(artifact.InventoryJSON, []byte(h.keyPath)) || bytes.Contains(artifact.InventoryJSON, h.key) {
		h.t.Fatal("serialized identity/time contract changed")
	}
	cpuPass, integrityPass := false, false
	for _, row := range payload.TestResults {
		if row.ID == "cpu.quick" {
			cpuPass = row.Status == app.TestPass && row.PlannedStatus == app.PlannedScheduled && row.PlanID == h.frozen.ID && row.HardwareGraph == h.frozen.HardwareGraph.Value && bytes.Equal(row.Evidence, h.cpu)
		}
		if row.ID == "run.integrity.quick" {
			integrityPass = row.Status == app.TestPass && row.PlanID == h.frozen.ID
		}
	}
	if !cpuPass || !integrityPass || len(payload.TestResults) != 23 {
		h.t.Fatal("current evidence or frozen-plan binding changed")
	}
	return artifact
}

func replaceFinalizerFingerprintFlag(args []string, name, value string) []string {
	result := append([]string(nil), args...)
	for i := 0; i+1 < len(result); i++ {
		if result[i] == name {
			result[i+1] = value
			return result
		}
	}
	panic("missing fixture flag")
}

func TestFinalizerFingerprintSignedOutputAfterOwnedCompletion(t *testing.T) {
	h := newFinalizerFingerprintHarness(t)
	ctx := context.WithValue(t.Context(), struct{ marker string }{"fingerprint"}, true)
	artifact := h.success(h.run(ctx, func(received context.Context, path string, kind retestInputKind) ([]byte, error) {
		if received != ctx {
			t.Fatal("finalization context replaced")
		}
		return h.read(received, path, kind)
	}, nil, nil))
	if h.reads != 1 || h.lastError != nil || artifact.Manifest.PreviousReport != nil {
		t.Fatal("nominal key or unlinked trust changed")
	}
	assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
	if !reflect.DeepEqual(h.fixture.readable.readSize, []int{32, 1}) {
		t.Fatal("exact fingerprint completion lacks probe")
	}
	if len(h.effects) < 4 || h.effects[0] != "collect" || h.effects[len(h.effects)-2] != "clock" || h.effects[len(h.effects)-1] != "output" {
		t.Fatal("effect order changed", h.effects)
	}
}

func TestFinalizerFingerprintCloseFailuresPrecedeCancellation(t *testing.T) {
	for _, mode := range []string{"readable", "metadata", "readable-cancel", "metadata-cancel"} {
		t.Run(mode, func(t *testing.T) {
			h := newFinalizerFingerprintHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			owner := h.fixture.readable
			if strings.HasPrefix(mode, "metadata") {
				owner = h.fixture.metadata
			}
			owner.closeErr = errors.New("private-provider-marker")
			if strings.HasSuffix(mode, "-cancel") {
				owner.hook = func(operation string) {
					if operation == "close" {
						cancel()
					}
				}
			}
			h.stopped(h.run(ctx, h.read, nil, h.forbiddenGate), 1, "read fingerprint key: input acquisition failed\n")
			if !errors.Is(h.lastError, errRetestInputCleanup) || !errors.Is(h.lastError, errRetestInput) {
				t.Fatal("cleanup uncertainty lost")
			}
			assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
		})
	}
}

func TestFinalizerFingerprintOwnedFaultsStopLaterEffects(t *testing.T) {
	for _, mode := range []string{"metadata-owner-error", "readable-owner-error", "overflow-probe", "read-count-and-cancel"} {
		t.Run(mode, func(t *testing.T) {
			h := newFinalizerFingerprintHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := map[string]int{"metadata": 1, "readable": 1}
			order := []string{"readable", "metadata"}
			switch mode {
			case "metadata-owner-error":
				h.fixture.metadataErr = errors.New("private-metadata-marker")
				want = map[string]int{"metadata": 1}
				order = []string{"metadata"}
			case "readable-owner-error":
				h.fixture.readableErr = errors.New("private-readable-marker")
			case "overflow-probe":
				h.fixture.readable.data = append(h.fixture.readable.data, 0x7f)
			case "read-count-and-cancel":
				h.fixture.readable.reads = []retestInputTestRead{{33, context.Canceled}}
				h.fixture.readable.hook = func(operation string) {
					if operation == "read" {
						cancel()
					}
				}
			}
			h.stopped(h.run(ctx, h.read, nil, h.forbiddenGate), 1, "read fingerprint key: input acquisition failed\n")
			if !errors.Is(h.lastError, errRetestInput) {
				t.Fatal("hard input fault replaced by cancellation")
			}
			assertRetestInputLedger(t, h.fixture, want, order)
			if mode == "overflow-probe" && !reflect.DeepEqual(h.fixture.readable.readSize, []int{32, 1}) {
				t.Fatal("missing bounded overflow probe")
			}
		})
	}
}

func TestFinalizerFingerprintContextAndRequiredReaderAdmission(t *testing.T) {
	for _, mode := range []string{"nil-context", "typed-nil-context", "cancelled", "deadline", "nil-reader"} {
		t.Run(mode, func(t *testing.T) {
			h := newFinalizerFingerprintHarness(t)
			var ctx context.Context = t.Context()
			var read retestInputReader = h.read
			switch mode {
			case "nil-context":
				ctx = nil
			case "typed-nil-context":
				var value *retestInputStoppedContext
				ctx = value
			case "cancelled":
				value, cancel := context.WithCancel(ctx)
				cancel()
				ctx = value
			case "deadline":
				value, cancel := context.WithDeadline(ctx, time.Unix(1, 0))
				defer cancel()
				ctx = value
			case "nil-reader":
				read = nil
			}
			h.stopped(h.run(ctx, read, nil, h.forbiddenGate), 0, "finalize input acquisition failed\n")
			assertRetestInputLedger(t, h.fixture, map[string]int{}, nil)
		})
	}
}

func TestFinalizerFingerprintShapeAndHardErrorBeforeLateCancel(t *testing.T) {
	for _, mode := range []string{"nil", "short", "long", "nil-cancel", "short-cancel", "long-cancel", "hard-cleanup-cancel"} {
		t.Run(mode, func(t *testing.T) {
			h := newFinalizerFingerprintHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			read := func(received context.Context, path string, kind retestInputKind) ([]byte, error) {
				h.reads++
				if received != ctx || path != h.keyPath || kind != retestFingerprintKeyInput {
					t.Fatal("reader request changed")
				}
				if strings.HasSuffix(mode, "-cancel") {
					cancel()
				}
				if mode == "hard-cleanup-cancel" {
					return append([]byte(nil), h.key...), errors.Join(errRetestInputCleanup, errors.New("private-error-marker"))
				}
				if strings.HasPrefix(mode, "short") {
					return bytes.Repeat([]byte{0x35}, 31), nil
				}
				if strings.HasPrefix(mode, "long") {
					return bytes.Repeat([]byte{0x35}, 33), nil
				}
				return nil, nil
			}
			h.stopped(h.run(ctx, read, nil, h.forbiddenGate), 1, "read fingerprint key: input acquisition failed\n")
		})
	}
}

func TestFinalizerFingerprintCancellationAfterClosedCapture(t *testing.T) {
	h := newFinalizerFingerprintHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	read := func(received context.Context, path string, kind retestInputKind) ([]byte, error) {
		data, err := h.read(received, path, kind)
		if err != nil || !bytes.Equal(data, h.key) {
			t.Fatal("closed-success prerequisite failed")
		}
		assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
		cancel()
		return data, nil
	}
	h.stopped(h.run(ctx, read, nil, h.forbiddenGate), 1, "finalize input acquisition failed\n")
	if h.lastError != nil {
		t.Fatal("test exercised core cancellation instead of post-reader barrier")
	}
}

func TestFinalizerFingerprintEarlierPreconditionsPreserved(t *testing.T) {
	for _, mode := range []string{"unknown-flag", "missing-required", "predecessor-trust", "serialized-journal", "invalid-outcome", "invalid-directory"} {
		t.Run(mode, func(t *testing.T) {
			h := newFinalizerFingerprintHarness(t)
			wantCode := 2
			prefix := ""
			switch mode {
			case "unknown-flag":
				h.args = append(h.args, "-unknown-flag")
				prefix = "finalize: invalid arguments\n"
			case "missing-required":
				h.args = replaceFinalizerFingerprintFlag(h.args, "-output", "")
				prefix = "finalize requires "
			case "predecessor-trust":
				h.args = append(h.args, "-previous-report", "parent-without-trust")
				prefix = "finalize: invalid or conflicting predecessor options\n"
			case "serialized-journal":
				h.args = append(h.args, "-session-journal", "forged.json")
				prefix = "session finalization requires the live coordinator gate;"
			case "invalid-outcome":
				h.args = append(h.args, "-outcome", "cpu.quick=bogus")
				prefix = "validate outcomes:"
			case "invalid-directory":
				h.args = replaceFinalizerFingerprintFlag(h.args, "-evidence-dir", filepath.Join(h.directory, "missing"))
				wantCode = 1
				prefix = "validate evidence directory:"
			}
			result := h.run(t.Context(), h.read, nil, h.forbiddenGate)
			if result.Code != wantCode || !strings.HasPrefix(h.stderr.String(), prefix) || h.reads != 0 || len(h.effects) != 0 || h.stdout.Len() != 0 || h.output.Len() != 0 {
				t.Fatalf("earlier guard changed: %s code=%d diagnostic=%q", mode, result.Code, h.stderr.String())
			}
			assertRetestInputLedger(t, h.fixture, map[string]int{}, nil)
		})
	}
}

func TestFinalizerFingerprintFailurePrecedesFrozenPlanRead(t *testing.T) {
	for _, fault := range []bool{false, true} {
		name := "successful-key-reaches-plan"
		if fault {
			name = "failed-key-stops-before-plan"
		}
		t.Run(name, func(t *testing.T) {
			h := newFinalizerFingerprintHarness(t)
			h.args = replaceFinalizerFingerprintFlag(h.args, "-plan", filepath.Join(h.directory, "missing-plan.json"))
			if fault {
				h.fixture.readable.closeErr = errors.New("private-close-marker")
			}
			result := h.run(t.Context(), h.read, nil, h.forbiddenGate)
			if fault {
				h.stopped(result, 1, "read fingerprint key: input acquisition failed\n")
			} else if result.Code != 1 || !strings.HasPrefix(h.stderr.String(), "read frozen plan:") || h.reads != 1 || len(h.effects) != 0 || h.stdout.Len() != 0 || h.output.Len() != 0 {
				t.Fatal("positive key did not reach the distinct plan guard")
			}
			assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
		})
	}
}
