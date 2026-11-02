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

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

type finalizeObservationHarness struct {
	*finalizerFingerprintHarness
	diagnostics                             inventory.CollectionDiagnostics
	collector                               inventoryDiagnosticCollector
	diagnostic                              io.Writer
	destination                             destinationWriter
	input                                   retestInputReader
	random                                  io.Reader
	gate                                    sessionFinalGate
	collectCalls, warningCalls, randomBytes int
}

func newFinalizeObservationHarness(t *testing.T) *finalizeObservationHarness {
	t.Helper()
	h := &finalizeObservationHarness{finalizerFingerprintHarness: newFinalizerFingerprintHarness(t)}
	h.collector = func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
		return h.snapshot, h.diagnostics, nil
	}
	h.input, h.destination = h.read, h.write
	h.random = captureInputReadFunc(func(p []byte) (int, error) {
		h.closedBeforeEffect()
		h.effects = append(h.effects, "random")
		h.randomBytes += len(p)
		for i := range p {
			p[i] = 0x62
		}
		return len(p), nil
	})
	h.diagnostic = observationCLIWriter{write: func(p []byte) (int, error) {
		if bytes.HasPrefix(p, []byte("inventory: metadata reads failed ")) {
			h.closedBeforeEffect()
			h.effects = append(h.effects, "warning")
			h.warningCalls++
		}
		return h.stderr.Write(p)
	}}
	return h
}

func (h *finalizeObservationHarness) runObservation(ctx context.Context, legacy bool) sessionFinalizationResult {
	h.t.Helper()
	var collect inventoryDiagnosticCollector
	if h.collector != nil {
		collect = func(ctx context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
			h.closedBeforeEffect()
			h.collectCalls++
			h.effects = append(h.effects, "collect")
			return h.collector(ctx)
		}
	}
	if legacy {
		return runFinalizeOwnedDestinationWithPreviousForProfile(ctx, h.args, &h.stdout, h.diagnostic,
			func(ctx context.Context) (inventory.Snapshot, error) { s, _, err := collect(ctx); return s, err },
			h.random, h.now, h.gate, h.destination, nil, h.input, "")
	}
	return runFinalizeWithDiagnosticInput(ctx, h.args, &h.stdout, h.diagnostic, collect, h.random, h.now, h.gate, h.destination, nil, h.input, "")
}

func requireFinalizeObservationBundle(t *testing.T, h *finalizeObservationHarness, result sessionFinalizationResult) bundle.Artifact {
	t.Helper()
	if result.Code != 0 || h.collectCalls != 1 || h.output.Len() == 0 || !strings.Contains(h.stdout.String(), "finalized: signed self-issued report") {
		t.Fatalf("signed prerequisite failed: code=%d %s", result.Code, &h.stderr)
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(h.output.Bytes()), int64(h.output.Len()))
	if err != nil || len(artifact.RenderedReport) == 0 || len(artifact.RenderedPDF) == 0 {
		t.Fatal("full signed-view verification failed", err)
	}
	expected := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x62}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	expectedID, err := bundle.SelfIssuedKeyID(expected)
	if err != nil || artifact.Envelope.KeyID != expectedID || artifact.Envelope.GeneratedTime != "2026-10-28T01:02:03.000000004Z" {
		t.Fatal("expected self-issued signer or timestamp changed")
	}
	for _, raw := range [][]byte{artifact.InventoryJSON, []byte(artifact.RenderedReport), []byte(artifact.RenderedPDF)} {
		for _, marker := range []string{"SYNTHETIC-", "proc/cpuinfo", "proc/meminfo", "proc/bus/input", "sys/class/dmi"} {
			if bytes.Contains(raw, []byte(marker)) {
				t.Fatal("provider detail entered a signed member")
			}
		}
	}
	finalizeObservationPayload(t, artifact)
	h.closedBeforeEffect()
	return artifact
}

func finalizeObservationWithoutFlag(args []string, name string) []string {
	result := append([]string(nil), args...)
	for i := 0; i+1 < len(result); i++ {
		if result[i] == name {
			return append(result[:i], result[i+2:]...)
		}
	}
	panic("missing synthetic fixture flag")
}

func TestFinalizeObservationCanonicalBundle(t *testing.T) {
	for _, name := range []string{"quiet", "mixed", "maximum", "generated_session"} {
		t.Run(name, func(t *testing.T) {
			h, old := newFinalizeObservationHarness(t), newFinalizeObservationHarness(t)
			want := ""
			switch name {
			case "mixed", "generated_session":
				h.diagnostics = inventory.CollectionDiagnostics{ScalarFailures: 2, CPUFailures: 3, MemoryFailures: 4, InputFailures: 5}
				want = "inventory: metadata reads failed (scalar=2 cpu=3 memory=4 input=5)\n"
			case "maximum":
				h.diagnostics = inventory.CollectionDiagnostics{ScalarFailures: 65536}
				want = "inventory: metadata reads failed (scalar=65536 cpu=0 memory=0 input=0)\n"
			}
			if name == "generated_session" {
				h.args = finalizeObservationWithoutFlag(h.args, "-session-id")
				old.args = finalizeObservationWithoutFlag(old.args, "-session-id")
			}
			oldResult := old.runObservation(t.Context(), true)
			old.success(oldResult) // Retained independent identity/HMAC and evidence literals.
			result := h.runObservation(t.Context(), false)
			requireFinalizeObservationBundle(t, h, result)
			if h.stderr.String() != want || !bytes.Equal(h.output.Bytes(), old.output.Bytes()) || !reflect.DeepEqual(result.Integrity, oldResult.Integrity) {
				t.Fatal("diagnostics changed canonical bundle or graph inputs")
			}
			wantEffects := []string{"collect", "random", "clock", "output"}
			if want != "" {
				wantEffects = []string{"collect", "warning", "random", "clock", "output"}
			}
			wantEntropy := 32
			if name == "generated_session" {
				wantEffects = append([]string{"random"}, wantEffects...)
				wantEntropy = 48
			}
			if !reflect.DeepEqual(h.effects, wantEffects) || h.randomBytes != wantEntropy {
				t.Fatal("finalization effect order changed", h.effects, h.randomBytes)
			}
		})
	}
}

func TestFinalizeObservationLinkedCanonicalBundle(t *testing.T) {
	for _, name := range []string{"self_issued_parent", "trusted_parent"} {
		t.Run(name, func(t *testing.T) {
			parent := commandRetestArtifact(t, "legacy-parent", name == "self_issued_parent", nil, 0x51)
			parentPath, parentKey := writeCommandRetest(t, t.TempDir(), "parent", parent)
			h, old := newFinalizeObservationHarness(t), newFinalizeObservationHarness(t)
			for _, current := range []*finalizeObservationHarness{h, old} {
				current.args = finalizeObservationWithoutFlag(current.args, "-session-id")
				current.args = append(current.args, "-previous-report", parentPath)
				current.args = append(current.args, commandRetestTrust("previous-", parentKey, parent.keyID, name == "self_issued_parent")...)
				current.input = func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
					if kind == retestFingerprintKeyInput {
						return current.read(ctx, path, kind)
					}
					return readRetestInput(ctx, path, kind)
				}
			}
			h.diagnostics = inventory.CollectionDiagnostics{InputFailures: 1}
			oldResult := old.runObservation(t.Context(), true)
			if oldResult.Code != 0 {
				t.Fatalf("legacy linked prerequisite: %s", &old.stderr)
			}
			result := h.runObservation(t.Context(), false)
			artifact := requireFinalizeObservationBundle(t, h, result)
			if artifact.Manifest.Schema != bundle.MemoryCapacityRetestManifestSchema || artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != commandRetestReference(parent) || !report.ValidRetestID(artifact.Envelope.SessionID) || !bytes.Equal(h.output.Bytes(), old.output.Bytes()) {
				t.Fatal("linked identity, trust reference or canonical bytes changed")
			}
			if h.stderr.String() != "inventory: metadata reads failed (scalar=0 cpu=0 memory=0 input=1)\n" || h.randomBytes != 48 {
				t.Fatal("linked warning or entropy order changed")
			}
			after, err := os.ReadFile(parentPath)
			if err != nil || !bytes.Equal(after, parent.encoded) {
				t.Fatal("parent changed")
			}
		})
	}
}

func TestFinalizeObservationSamePassRealProvider(t *testing.T) {
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
			h := newFinalizeObservationHarness(t)
			h.collector = collector.CollectWithDiagnostics
			result := h.runObservation(t.Context(), false)
			requireFinalizeObservationBundle(t, h, result)
			if h.stderr.String() != item.warning || source.callbacks != [3]int{} {
				t.Fatal("provider details or wrong counts escaped")
			}
			for _, path := range []string{"proc/cpuinfo", "proc/meminfo", "proc/bus/input/devices", "sys/class/dmi/id/product_uuid"} {
				if source.reads[path] != 1 {
					t.Fatal("finalizer acquired an observation twice")
				}
			}
			old := newFinalizeObservationHarness(t)
			old.collector = collector.CollectWithDiagnostics
			if old.runObservation(t.Context(), true).Code != 0 || !bytes.Equal(old.output.Bytes(), h.output.Bytes()) {
				t.Fatal("same observations changed signed bytes")
			}
		})
	}
}

func TestFinalizeObservationQuietAndReset(t *testing.T) {
	for _, name := range []string{"absent", "empty", "reset"} {
		t.Run(name, func(t *testing.T) {
			source, root := newPlanObservationSource(t)
			collector := inventory.NewLinuxCollectorWithSource(source)
			if name == "absent" {
				collector = inventory.NewLinuxCollector(t.TempDir())
			}
			if name == "empty" {
				for _, path := range []string{"proc/cpuinfo", "proc/meminfo", "proc/bus/input/devices", "sys/class/dmi/id/product_name"} {
					if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if name == "reset" {
				source.failed["proc/cpuinfo"] = true
				prior := newFinalizeObservationHarness(t)
				prior.collector = collector.CollectWithDiagnostics
				requireFinalizeObservationBundle(t, prior, prior.runObservation(t.Context(), false))
				if prior.stderr.String() != "inventory: metadata reads failed (scalar=0 cpu=1 memory=0 input=0)\n" {
					t.Fatal("reset prerequisite did not warn")
				}
				source.failed = map[string]bool{}
			}
			h := newFinalizeObservationHarness(t)
			h.collector = collector.CollectWithDiagnostics
			requireFinalizeObservationBundle(t, h, h.runObservation(t.Context(), false))
			if h.stderr.Len() != 0 || h.warningCalls != 0 || source.callbacks != [3]int{} {
				t.Fatal("absence/empty reads warned or diagnostics persisted")
			}
		})
	}
}

func TestFinalizeObservationInvalidDiagnostics(t *testing.T) {
	for _, item := range []struct {
		name  string
		value inventory.CollectionDiagnostics
	}{
		{"counter", inventory.CollectionDiagnostics{CPUFailures: 65537}}, {"aggregate", inventory.CollectionDiagnostics{ScalarFailures: 65536, InputFailures: 1}},
		{"uint32_max", inventory.CollectionDiagnostics{MemoryFailures: ^uint32(0)}}, {"invalid_and_cancel", inventory.CollectionDiagnostics{InputFailures: 65537}},
	} {
		t.Run(item.name, func(t *testing.T) {
			h := newFinalizeObservationHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h.collector = func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
				if item.name == "invalid_and_cancel" {
					cancel()
				}
				return h.snapshot, item.value, nil
			}
			result := h.runObservation(ctx, false)
			if result.Code != 1 || h.stderr.String() != "collect final inventory: inventory: invalid observation diagnostics\n" || !reflect.DeepEqual(h.effects, []string{"collect"}) || h.output.Len() != 0 {
				t.Fatal("invalid aggregate lost precedence or reached later effects")
			}
		})
	}
}

func TestFinalizeObservationWarningStopsEffects(t *testing.T) {
	for _, name := range []string{"error", "short", "overreported", "negative", "cancel", "error_cancel", "quiet", "nil_stderr"} {
		t.Run(name, func(t *testing.T) {
			h := newFinalizeObservationHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h.diagnostics = inventory.CollectionDiagnostics{ScalarFailures: 65536}
			var callbacks [3]int
			h.destination = func(path string, out io.Writer, write func(io.Writer) error) error {
				h.effects = append(h.effects, "output")
				return writeDestination(path, out, write)
			}
			h.diagnostic = observationCLIWriter{write: func(p []byte) (int, error) {
				h.stderr.Write(p)
				if bytes.HasPrefix(p, []byte("inventory: metadata reads failed ")) {
					h.warningCalls++
					h.effects = append(h.effects, "warning")
					if string(p) != "inventory: metadata reads failed (scalar=65536 cpu=0 memory=0 input=0)\n" || len(p) > 128 {
						t.Fatal("fixed warning changed")
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
					case "error_cancel":
						cancel()
						return 0, &planObservationProviderError{&callbacks}
					}
				}
				return len(p), nil
			}}
			if name == "nil_stderr" {
				h.diagnostic = nil
			}
			if name == "quiet" {
				h.diagnostics = inventory.CollectionDiagnostics{}
			}
			result := h.runObservation(ctx, false)
			if name == "quiet" {
				if result.Code != 0 || h.warningCalls != 0 {
					t.Fatal("quiet positive failed")
				}
				raw, err := os.ReadFile(h.outputPath)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw))); err != nil {
					t.Fatal(err)
				}
				return
			}
			requirePlanObservationAbsent(t, h.outputPath)
			if name == "nil_stderr" {
				if result.Code != 1 || h.reads != 0 || len(h.effects) != 0 {
					t.Fatal("missing sink admitted effects")
				}
				return
			}
			suffix := "final inventory observation warning could not be written\n"
			if name == "cancel" {
				suffix = "final inventory diagnostics interrupted\n"
			}
			if result.Code != 1 || callbacks != [3]int{} || h.stderr.String() != "inventory: metadata reads failed (scalar=65536 cpu=0 memory=0 input=0)\n"+suffix || !reflect.DeepEqual(h.effects, []string{"collect", "warning"}) || h.stdout.Len() != 0 || h.randomBytes != 0 {
				t.Fatal("warning failure reached gate/signing/destination or formatted error callbacks")
			}
		})
	}
}

type finalObservationContext struct {
	context.Context
	armed  bool
	calls  int
	mutate func()
}

func (c *finalObservationContext) Err() error {
	if c.armed {
		c.calls++
		if c.calls == 1 && c.mutate != nil {
			c.mutate()
		}
	}
	return c.Context.Err()
}

func TestFinalInventoryObservationCopyRepresentation(t *testing.T) {
	for _, item := range []struct {
		name  string
		value inventory.Snapshot
	}{
		{"nil_components", inventory.Snapshot{SchemaVersion: 71}}, {"empty_components", inventory.Snapshot{SchemaVersion: 71, Components: []inventory.Component{}}},
		{"nil_attributes", inventory.Snapshot{SchemaVersion: 71, Components: []inventory.Component{{Type: inventory.ComponentCPU, ID: "  cpu:raw  "}}}},
		{"empty_attributes", inventory.Snapshot{SchemaVersion: 71, Components: []inventory.Component{{Attributes: []inventory.Attribute{}}}}},
		{"raw_duplicate_order", inventory.Snapshot{SchemaVersion: 71, Components: []inventory.Component{{ID: "z", Attributes: []inventory.Attribute{{Name: " z ", Value: "  raw\tvalue  "}, {Name: "a", Value: "first"}, {Name: "a", Value: "second"}}}, {ID: "a"}}}},
	} {
		t.Run(item.name, func(t *testing.T) {
			owned := copyFinalInventoryObservation(item.value)
			if !reflect.DeepEqual(owned, item.value) {
				t.Fatal("copy canonicalized representation")
			}
			if len(item.value.Components) > 0 {
				originalID := item.value.Components[0].ID
				owned.Components[0].ID = "owned-change"
				if item.value.Components[0].ID != originalID {
					t.Fatal("component ownership aliases")
				}
				if len(item.value.Components[0].Attributes) > 0 {
					original := item.value.Components[0].Attributes[0]
					owned.Components[0].Attributes[0].Value = "owned-change"
					if item.value.Components[0].Attributes[0] != original {
						t.Fatal("attribute ownership aliases")
					}
					item.value.Components[0].Attributes[1].Value = "borrowed-change"
					if owned.Components[0].Attributes[1].Value == "borrowed-change" {
						t.Fatal("borrowed mutation escaped")
					}
				}
			}
		})
	}
}

func TestFinalizeObservationFreezeBeforeCallbacks(t *testing.T) {
	for _, name := range []string{"writer_mutation", "context_mutation", "invalid_before_writer", "invalid_before_context"} {
		t.Run(name, func(t *testing.T) {
			h, old := newFinalizeObservationHarness(t), newFinalizeObservationHarness(t)
			if old.runObservation(t.Context(), true).Code != 0 {
				t.Fatal("legacy positive failed")
			}
			h.diagnostics = inventory.CollectionDiagnostics{ScalarFailures: 1}
			layout, err := commandLayoutForPlan(h.frozen, h.frozen.Profile)
			if err != nil || !layout.touchpad {
				t.Fatal("fixture does not exercise touchpad freeze")
			}
			ctx := &finalObservationContext{Context: t.Context()}
			mutate := func() {
				h.snapshot.Components[0].Attributes[0].Value = "SYNTHETIC-ALIAS-MUTATION"
				h.snapshot.Components[0].ID = "changed"
			}
			if strings.HasPrefix(name, "invalid_") {
				h.snapshot.SchemaVersion = 71
			}
			h.collector = func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
				if strings.Contains(name, "context") {
					ctx.armed = true
					ctx.mutate = mutate
				}
				return h.snapshot, h.diagnostics, nil
			}
			writer := h.diagnostic
			h.diagnostic = observationCLIWriter{write: func(p []byte) (int, error) {
				if bytes.HasPrefix(p, []byte("inventory: metadata reads failed ")) && name == "writer_mutation" {
					mutate()
				}
				return writer.Write(p)
			}}
			result := h.runObservation(ctx, false)
			if strings.HasPrefix(name, "invalid_") {
				if result.Code != 1 || h.warningCalls != 0 || ctx.calls != 0 || h.stderr.String() != "freeze touchpad final inputs: invalid owned binding\n" || !reflect.DeepEqual(h.effects, []string{"collect"}) {
					t.Fatal("new callback preceded mandatory freeze rejection")
				}
				return
			}
			requireFinalizeObservationBundle(t, h, result)
			if !bytes.Equal(h.output.Bytes(), old.output.Bytes()) || h.snapshot.Components[0].ID != "changed" {
				t.Fatal("owned signed snapshot changed or mutation prerequisite did not fire")
			}
		})
	}
}

func TestFinalizeObservationOwnedKeyClosure(t *testing.T) {
	for _, name := range []string{"readable", "metadata", "readable_cancel", "metadata_cancel"} {
		t.Run(name, func(t *testing.T) {
			h := newFinalizeObservationHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h.diagnostics = inventory.CollectionDiagnostics{CPUFailures: 1}
			var callbacks [3]int
			owner := h.fixture.readable
			if strings.HasPrefix(name, "metadata") {
				owner = h.fixture.metadata
			}
			owner.closeErr = &planObservationProviderError{&callbacks}
			if strings.HasSuffix(name, "_cancel") {
				owner.hook = func(op string) {
					if op == "close" {
						cancel()
					}
				}
			}
			h.stopped(h.runObservation(ctx, false), 1, "read fingerprint key: input acquisition failed\n")
			if callbacks != [3]int{} || !errors.Is(h.lastError, errRetestInputCleanup) || h.collectCalls != 0 || h.warningCalls != 0 {
				t.Fatal("owned-key failure precedence changed")
			}
			assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
		})
	}
}

func TestFinalizeObservationFatalCollection(t *testing.T) {
	for _, item := range []struct {
		name    string
		failure error
	}{
		{"directory_read", inventory.ErrDirectoryRead}, {"directory_close", inventory.ErrDirectoryClose}, {"scalar_read", inventory.ErrScalarRead}, {"scalar_close", inventory.ErrScalarClose}, {"budget", inventory.ErrAcquisitionBudget}, {"cancel", context.Canceled},
	} {
		t.Run(item.name, func(t *testing.T) {
			h := newFinalizeObservationHarness(t)
			h.collector = func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
				return h.snapshot, inventory.CollectionDiagnostics{ScalarFailures: 65537}, item.failure
			}
			result := h.runObservation(t.Context(), false)
			if result.Code != 1 || h.stderr.String() != "collect final inventory: "+item.failure.Error()+"\n" || !reflect.DeepEqual(h.effects, []string{"collect"}) || h.output.Len() != 0 {
				t.Fatal("fatal collection lost precedence or released partial success")
			}
		})
	}
}

func TestFinalizeObservationAdmissionAndEntropy(t *testing.T) {
	for _, name := range []string{"short_key", "invalid_plan", "wrong_epoch", "invalid_evidence", "entropy_error", "entropy_cancel", "pre_cancel", "nil_collector", "warning_after_session_entropy"} {
		t.Run(name, func(t *testing.T) {
			h := newFinalizeObservationHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h.diagnostics = inventory.CollectionDiagnostics{InputFailures: 1}
			switch name {
			case "short_key":
				h.input = func(context.Context, string, retestInputKind) ([]byte, error) { return h.key[:31], nil }
			case "invalid_plan":
				if err := os.WriteFile(h.planPath, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong_epoch":
				h.args = append(h.args, "-key-epoch", "different")
			case "invalid_evidence":
				if err := os.WriteFile(filepath.Join(h.directory, "cpu.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "pre_cancel":
				cancel()
			case "nil_collector":
				h.collector = nil
			case "entropy_error", "entropy_cancel", "warning_after_session_entropy":
				h.args = finalizeObservationWithoutFlag(h.args, "-session-id")
				original := h.random
				h.random = captureInputReadFunc(func(p []byte) (int, error) {
					if len(p) != 16 {
						t.Fatal("signing entropy reached before successful warning")
					}
					n, err := original.Read(p)
					if name == "entropy_error" {
						return 0, errors.New("synthetic entropy failure")
					}
					if name == "entropy_cancel" {
						cancel()
					}
					return n, err
				})
				if name == "warning_after_session_entropy" {
					h.diagnostic = observationCLIWriter{write: func(p []byte) (int, error) {
						h.stderr.Write(p)
						if bytes.HasPrefix(p, []byte("inventory: metadata reads failed ")) {
							h.effects = append(h.effects, "warning")
							h.warningCalls++
							return len(p) - 1, nil
						}
						return len(p), nil
					}}
				}
			}
			result := h.runObservation(ctx, false)
			if result.Code == 0 || h.output.Len() != 0 || h.stdout.Len() != 0 {
				t.Fatal("failed admission reached success")
			}
			if name == "warning_after_session_entropy" {
				if h.randomBytes != 16 || !reflect.DeepEqual(h.effects, []string{"random", "collect", "warning"}) {
					t.Fatal("optional session entropy or warning boundary moved")
				}
				return
			}
			if h.collectCalls != 0 || h.warningCalls != 0 {
				t.Fatal("earlier admission failure reached observation warning")
			}
			if name == "nil_collector" && h.stderr.String() != "collect final inventory: inventory: invalid observation diagnostics\n" {
				t.Fatal("missing collector diagnostic changed")
			}
		})
	}
}

func TestFinalizeObservationInjectedErrorRemainsLegacy(t *testing.T) {
	h := newFinalizeObservationHarness(t)
	var callbacks [3]int
	h.collector = func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
		return inventory.Snapshot{}, inventory.CollectionDiagnostics{}, &planObservationProviderError{&callbacks}
	}
	if h.runObservation(t.Context(), false).Code != 1 || callbacks != [3]int{1, 0, 0} || h.stderr.String() != "collect final inventory: SYNTHETIC-PROVIDER-DETAIL-MUST-NOT-APPEAR\n" {
		t.Fatal("legacy arbitrary injected collector formatting changed")
	}
	// This deliberately injected collector is not a real provider failure route.
}

func TestFinalizeObservationDestinationFailure(t *testing.T) {
	for _, name := range []string{"callback_error", "close_error", "existing_output"} {
		t.Run(name, func(t *testing.T) {
			h := newFinalizeObservationHarness(t)
			h.diagnostics = inventory.CollectionDiagnostics{MemoryFailures: 1}
			destination := &planObservationDestination{}
			switch name {
			case "callback_error":
				h.destination = func(string, io.Writer, func(io.Writer) error) error {
					h.effects = append(h.effects, "output")
					return errors.New("synthetic destination failure")
				}
			case "close_error":
				h.destination = func(_ string, _ io.Writer, write func(io.Writer) error) error {
					h.effects = append(h.effects, "output")
					return writeDestinationWith(h.outputPath, io.Discard, write, func(string, int, os.FileMode) (io.WriteCloser, error) { return destination, nil })
				}
			case "existing_output":
				if err := os.WriteFile(h.outputPath, []byte("retained"), 0600); err != nil {
					t.Fatal(err)
				}
				h.destination = func(path string, out io.Writer, write func(io.Writer) error) error {
					h.effects = append(h.effects, "output")
					return writeDestination(path, out, write)
				}
			}
			result := h.runObservation(t.Context(), false)
			if result.Code != 1 || h.warningCalls != 1 || h.randomBytes != 32 || strings.Contains(h.stdout.String(), "finalized:") {
				t.Fatal("destination failure reported finalization success")
			}
			if name == "close_error" && (destination.closed != 1 || destination.Len() == 0) {
				t.Fatal("owned destination was not written and closed")
			}
			if name == "existing_output" {
				raw, err := os.ReadFile(h.outputPath)
				if err != nil || string(raw) != "retained" {
					t.Fatal("existing destination changed")
				}
			}
		})
	}
}

// Keep the signed payload type in this test file's explicit consumer contract.
func finalizeObservationPayload(t *testing.T, artifact bundle.Artifact) app.InventoryPayload {
	t.Helper()
	var payload app.InventoryPayload
	if json.Unmarshal(artifact.InventoryJSON, &payload) != nil || payload.CoverageScope != app.CoverageFrozenPlan {
		t.Fatal("invalid frozen-plan payload")
	}
	return payload
}
