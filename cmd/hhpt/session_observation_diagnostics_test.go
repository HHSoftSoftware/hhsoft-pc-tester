package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
)

type sessionObservationHarness struct {
	*sessionHarness
	diagnostics                                                    inventory.CollectionDiagnostics
	collector                                                      inventoryDiagnosticCollector
	mode                                                           string
	legacy                                                         bool
	finalCalls, collectCalls, warningCalls, gateCalls, randomBytes int
	callbacks                                                      [3]int
	safety                                                         *sessionMonitorFixture
	heat                                                           *sessionThermalFixture
	cancelFinal                                                    context.CancelFunc
	probe                                                          *finalObservationContext
	last                                                           sessionFinalizationResult
}

func newSessionObservationHarness(t *testing.T, profile string) *sessionObservationHarness {
	t.Helper()
	var core *sessionHarness
	switch profile {
	case "quick":
		core = newSessionHarness(t, "x86_64", nil)
	case "standard":
		core = newStandardHarness(t, commandStressPass())
	case "extended":
		core = newExtendedMemoryHarness(t, commandMemoryPass())
	default:
		t.Fatal("unknown literal profile")
	}
	h := &sessionObservationHarness{sessionHarness: core, diagnostics: inventory.CollectionDiagnostics{ScalarFailures: 1}}
	if err := os.WriteFile(filepath.Join(h.directory, "synthetic.key"), bytes.Repeat([]byte{0x31}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	h.dependencies.ReadRetestInput = readRetestInput
	h.collector = func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
		return h.snapshot, h.diagnostics, nil
	}
	h.dependencies.FinishInput = func() error {
		h.inputFinished = true
		if h.mode == "input_cleanup" {
			return errors.Join(errSessionInputCleanup, errors.New("synthetic input cleanup failure"))
		}
		return nil
	}
	h.dependencies.StartMonitor = func(started time.Time, _ context.CancelFunc) (sessionSafetyMonitor, error) {
		h.safety = &sessionMonitorFixture{result: func() safemode.MonitorResult {
			if !h.inputFinished {
				t.Error("safety joined before input completion")
			}
			return sessionMonitorSummary(h.clock.Sub(started).Milliseconds())
		}}
		return h.safety, nil
	}
	h.dependencies.StartThermal = func(started time.Time, _, _ context.CancelFunc) (sessionThermalMonitor, error) {
		h.heat = &sessionThermalFixture{clock: func() int64 { return h.clock.Sub(started).Milliseconds() }, finding: make(chan struct{})}
		h.heat.stop = func(ctx context.Context) thermal.Result {
			if !h.inputFinished || ctx.Err() != nil {
				t.Error("monitor cleanup lost its fresh context or input join")
			}
			result := sessionThermalSummary(h.clock.Sub(started).Milliseconds())
			if strings.Contains(h.mode, "_thermal_cleanup") {
				result.Status = "cleanup_unconfirmed"
				result.Joined = false
				result.CleanupConfirmed = false
				result.FirstFailure = &thermal.FailureRecord{Reason: "cleanup_unconfirmed", Phase: "none"}
			}
			return result
		}
		return h.heat, nil
	}
	h.dependencies.Finalize = func(finalCtx context.Context, request sessionFinalization, out, diagnostic io.Writer) sessionFinalizationResult {
		h.finalCalls++
		h.request = request
		if !h.inputFinished || finalCtx.Err() != nil {
			t.Fatal("adapter did not receive joined input and fresh finalization context")
		}
		deadline, ok := finalCtx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 30*time.Second {
			t.Fatal("finalizer lost bounded ownership")
		}
		ctx, cancel := context.WithCancel(finalCtx)
		defer cancel()
		h.cancelFinal = cancel
		var observedCtx context.Context = ctx
		if h.mode == "quiet_context_mutation" {
			h.probe = &finalObservationContext{Context: ctx, mutate: func() { h.snapshot.Components[0].ID = "SYNTHETIC-BORROWED-SNAPSHOT-MUTATION" }}
			observedCtx = h.probe
		}
		gate := request.Gate
		request.Gate = func(ctx context.Context) (sessionJournal, error) {
			h.gateCalls++
			value, err := gate(ctx)
			if h.mode == "changed_gate" && err == nil {
				value.BudgetMilliseconds++
			}
			return value, err
		}
		if h.mode == "consumed_gate" {
			if _, err := request.Gate(observedCtx); err != nil {
				t.Fatal("first gate prerequisite", err)
			}
		}
		if h.mode == "final_cancel" {
			cancel()
		}
		collect := func(ctx context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
			h.collectCalls++
			if !h.inputFinished {
				t.Fatal("final collection overlaps input owner")
			}
			value, diagnostics, err := h.collector(ctx)
			switch h.mode {
			case "invalid":
				diagnostics = inventory.CollectionDiagnostics{ScalarFailures: 65536, CPUFailures: 1}
			case "fatal":
				err = inventory.ErrAcquisitionBudget
			case "collect_cancel":
				cancel()
			case "late_collision":
				if err := os.WriteFile(request.OutputPath, []byte("retained late destination"), 0600); err != nil {
					t.Fatal(err)
				}
			case "quiet_context_mutation":
				h.probe.armed = true
			}
			return value, diagnostics, err
		}
		warning := observationCLIWriter{write: func(p []byte) (int, error) {
			if bytes.HasPrefix(p, []byte("inventory: metadata reads failed ")) {
				h.warningCalls++
				if !h.inputFinished || len(p) > 128 {
					t.Fatal("warning ownership or bound changed")
				}
				if _, err := diagnostic.Write(p); err != nil {
					return 0, err
				}
				switch h.mode {
				case "warning_error", "warning_error_thermal_cleanup":
					return 0, &planObservationProviderError{&h.callbacks}
				case "warning_short":
					return len(p) - 1, nil
				case "warning_cancel", "warning_cancel_thermal_cleanup":
					cancel()
				case "warning_error_cancel", "warning_error_cancel_thermal_cleanup":
					cancel()
					return 0, &planObservationProviderError{&h.callbacks}
				}
				return len(p), nil
			}
			return diagnostic.Write(p)
		}}
		random := captureInputReadFunc(func(p []byte) (int, error) {
			h.randomBytes += len(p)
			if h.mode == "entropy_error" || h.mode == "signing_error" && len(p) == 32 {
				return 0, errors.New("synthetic entropy failure")
			}
			for i := range p {
				p[i] = 0x52
			}
			return len(p), nil
		})
		now := func() time.Time { return time.Date(2026, 10, 28, 1, 2, 3, 4, time.UTC) }
		var result sessionFinalizationResult
		if h.legacy {
			result = finalizeSessionWith(observedCtx, request, out, warning, func(ctx context.Context) (inventory.Snapshot, error) { s, _, err := collect(ctx); return s, err }, random, now)
		} else {
			// This is the same diagnostic adapter reached by real finalizeSession.
			result = finalizeSessionWithDiagnostics(observedCtx, request, out, warning, collect, random, now)
		}
		h.last = result
		h.finalized = result.Code == 0
		if result.Journal != nil {
			h.journal = sessionevidence.CloneJournal(*result.Journal)
		}
		return result
	}
	return h
}

func requireSessionObservationOwners(t *testing.T, h *sessionObservationHarness) {
	t.Helper()
	if !h.inputFinished || h.safety == nil || h.heat == nil || h.safety.stops != 1 || h.heat.stops != 1 {
		t.Fatal("both monitors and input were not joined exactly once")
	}
}

func requireSessionObservationBundle(t *testing.T, h *sessionObservationHarness) ([]byte, bundle.Artifact) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.directory, "session.hhpcr"))
	if err != nil {
		t.Fatal("real adapter did not create a report", err)
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	expected := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x52}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	expectedID, idErr := bundle.SelfIssuedKeyID(expected)
	if err != nil || idErr != nil || artifact.Envelope.KeyID != expectedID || len(artifact.RenderedReport) == 0 || len(artifact.RenderedPDF) == 0 {
		t.Fatal("real session signature, signer or complete views failed", err)
	}
	for _, member := range [][]byte{artifact.InventoryJSON, []byte(artifact.RenderedReport), []byte(artifact.RenderedPDF)} {
		for _, marker := range []string{"SYNTHETIC-", "proc/cpuinfo", "sys/class/dmi"} {
			if bytes.Contains(member, []byte(marker)) {
				t.Fatal("provider or borrowed mutation escaped into report")
			}
		}
	}
	if _, err := os.Stat(filepath.Join(h.directory, "session-ready.txt")); err != nil {
		t.Fatal("successful coordinator lacks readiness")
	}
	requireSessionObservationOwners(t, h)
	return raw, artifact
}

func TestSessionObservationRealAdapterCanonicalProfiles(t *testing.T) {
	for _, name := range []string{"quick", "standard", "extended", "linked"} {
		t.Run(name, func(t *testing.T) {
			profile := name
			if name == "linked" {
				profile = "quick"
			}
			h, old := newSessionObservationHarness(t, profile), newSessionObservationHarness(t, profile)
			clock := time.Now()
			h.clock = clock
			old.clock = clock
			old.legacy = true
			var parent commandRetestFixture
			if name == "linked" {
				parent = commandRetestArtifact(t, "legacy-parent", true, nil, 0x61)
				path, _ := writeCommandRetest(t, t.TempDir(), "parent", parent)
				for _, current := range []*sessionObservationHarness{h, old} {
					current.arguments = append(current.arguments, "-previous-report", path, "-previous-self-issued")
				}
			}
			if code := old.run(t.Context()); code != 0 {
				t.Fatalf("legacy real adapter prerequisite: %d %s", code, &old.stderr)
			}
			oldRaw, _ := requireSessionObservationBundle(t, old)
			if code := h.run(t.Context()); code != 0 {
				t.Fatalf("real diagnostic adapter: %d %s", code, &h.stderr)
			}
			raw, artifact := requireSessionObservationBundle(t, h)
			if !bytes.Equal(raw, oldRaw) || h.finalCalls != 1 || h.collectCalls != 1 || h.warningCalls != 1 || h.gateCalls != 1 || h.randomBytes != 48 || h.last.Integrity.Status != "pass" {
				t.Fatal("diagnostics altered signed current report, graph or one-use gate")
			}
			if h.stderr.String() != "inventory: metadata reads failed (scalar=1 cpu=0 memory=0 input=0)\n" || old.warningCalls != 0 {
				t.Fatal("wrong warning or legacy route changed")
			}
			schema := "hhsoft.pc-tester.quick-session/v14"
			workers := 14
			if profile == "standard" {
				schema = "hhsoft.pc-tester.quick-session/v15"
				workers = 15
			}
			if profile == "extended" {
				schema = "hhsoft.pc-tester.quick-session/v16"
				workers = 15
			}
			wantProfile := profile
			if profile == "quick" {
				wantProfile = ""
			} // Quick retains the existing omitted profile field.
			if h.journal.Schema != schema || h.journal.Profile != wantProfile || len(h.journal.Workers) != workers || len(h.calls) == 0 {
				t.Fatal("current profile/worker/journal prerequisite changed")
			}
			if name == "linked" && (artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != commandRetestReference(parent)) {
				t.Fatal("typed parent reference changed")
			}
			if name != "linked" && artifact.Manifest.PreviousReport != nil {
				t.Fatal("ordinary report gained a parent")
			}
		})
	}
}

func TestSessionObservationFailuresRetainOwnersAndGate(t *testing.T) {
	for _, name := range []string{"warning_error", "warning_short", "warning_cancel", "warning_error_cancel", "warning_error_thermal_cleanup", "warning_cancel_thermal_cleanup", "warning_error_cancel_thermal_cleanup", "invalid", "fatal", "collect_cancel", "input_cleanup", "consumed_gate", "changed_gate", "late_collision", "final_cancel", "entropy_error", "signing_error"} {
		t.Run(name, func(t *testing.T) {
			if name == "changed_gate" {
				// The existing coordinator compares its exact journal after the real
				// finalizer has signed. A semantic-but-different journal retains that
				// signed report, while the coordinator rejects readiness.
				h, old := newSessionObservationHarness(t, "quick"), newSessionObservationHarness(t, "quick")
				clock := time.Now()
				h.clock = clock
				old.clock = clock
				h.mode, old.mode = name, name
				old.legacy = true
				oldCode := old.run(t.Context())
				code := h.run(t.Context())
				if oldCode != 1 || code != 1 {
					t.Fatal("changed journal did not reject coordinator success in both routes")
				}
				var signed [][]byte
				expected := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x52}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
				expectedID, idErr := bundle.SelfIssuedKeyID(expected)
				if idErr != nil {
					t.Fatal(idErr)
				}
				for _, current := range []*sessionObservationHarness{old, h} {
					requireSessionObservationOwners(t, current)
					requirePlanObservationAbsent(t, filepath.Join(current.directory, "session-ready.txt"))
					if current.last.Code != 0 || !current.finalized || current.last.Journal == nil || current.last.Journal.BudgetMilliseconds != 300001 || current.last.Integrity.Status != "pass" {
						t.Fatal("final core success and late coordinator rejection were conflated")
					}
					if current.finalCalls != 1 || current.collectCalls != 1 || current.gateCalls != 1 || current.randomBytes != 48 || current.callbacks != [3]int{} {
						t.Fatal("late journal rejection changed collection, gate, entropy or ownership")
					}
					raw, err := os.ReadFile(filepath.Join(current.directory, "session.hhpcr"))
					if err != nil {
						t.Fatal("late journal rejection lost the retained signed report", err)
					}
					artifact, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
					if err != nil || artifact.Envelope.KeyID != expectedID || artifact.Envelope.GeneratedTime != "2026-10-28T01:02:03.000000004Z" || artifact.Manifest.ViewProfile != bundle.MemoryCapacityViewProfile || len(artifact.RenderedReport) == 0 || len(artifact.RenderedPDF) == 0 {
						t.Fatal("retained report lacks expected signer or canonical views", err)
					}
					if !strings.Contains(current.stdout.String(), "finalized: signed self-issued report") {
						t.Fatal("actual finalizer success was not exercised")
					}
					signed = append(signed, raw)
				}
				if !bytes.Equal(signed[0], signed[1]) || old.warningCalls != 0 || h.warningCalls != 1 || old.stderr.Len() == 0 || h.stderr.String() != "inventory: metadata reads failed (scalar=1 cpu=0 memory=0 input=0)\n"+old.stderr.String() {
					t.Fatal("new warning changed legacy retained bundle or late rejection diagnostics")
				}
				return
			}
			h := newSessionObservationHarness(t, "quick")
			h.mode = name
			code := h.run(t.Context())
			wantCode := 1
			if name == "input_cleanup" || strings.Contains(name, "_thermal_cleanup") {
				wantCode = 12
			}
			if code != wantCode || h.callbacks != [3]int{} {
				t.Fatal("failure/cancellation/cleanup precedence or hostile error callbacks changed", code)
			}
			requireSessionObservationOwners(t, h)
			requirePlanObservationAbsent(t, filepath.Join(h.directory, "session-ready.txt"))
			output := filepath.Join(h.directory, "session.hhpcr")
			if name == "late_collision" {
				raw, err := os.ReadFile(output)
				if err != nil || string(raw) != "retained late destination" {
					t.Fatal("late destination overwritten")
				}
			} else {
				requirePlanObservationAbsent(t, output)
			}
			wantFinal, wantCollect, wantWarning, wantGate, wantEntropy := 1, 1, 1, 0, 16
			switch name {
			case "input_cleanup":
				wantFinal, wantCollect, wantWarning, wantEntropy = 0, 0, 0, 0
			case "final_cancel":
				wantCollect, wantWarning, wantEntropy = 0, 0, 0
			case "entropy_error":
				wantCollect, wantWarning = 0, 0
			case "invalid", "fatal", "collect_cancel":
				wantWarning = 0
			case "consumed_gate":
				wantGate = 2
			case "late_collision", "signing_error":
				wantGate, wantEntropy = 1, 48
			}
			if h.finalCalls != wantFinal || h.collectCalls != wantCollect || h.warningCalls != wantWarning || h.gateCalls != wantGate || h.randomBytes != wantEntropy {
				t.Fatalf("effect boundary: final=%d collect=%d warning=%d gate=%d entropy=%d", h.finalCalls, h.collectCalls, h.warningCalls, h.gateCalls, h.randomBytes)
			}
			if strings.Contains(name, "warning") && !strings.Contains(h.stderr.String(), "inventory: metadata reads failed (scalar=1 cpu=0 memory=0 input=0)\n") {
				t.Fatal("warning prerequisite did not occur")
			}
			if strings.Contains(h.stdout.String(), "finalized:") {
				t.Fatal("failure advertised finalization success")
			}
		})
	}
}

func TestSessionObservationFreshContextAndQuietOwnership(t *testing.T) {
	for _, name := range []string{"clean_stop", "quiet_context_mutation"} {
		t.Run(name, func(t *testing.T) {
			h := newSessionObservationHarness(t, "quick")
			h.mode = name
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if name == "clean_stop" {
				run := h.dependencies.RunWorker
				h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
					code := run(ctx, w, input, out, diagnostic)
					cancel()
					return code
				}
			} else {
				h.diagnostics = inventory.CollectionDiagnostics{}
			}
			if code := h.run(ctx); code != 0 {
				t.Fatalf("real finalization failed: %d %s", code, &h.stderr)
			}
			requireSessionObservationBundle(t, h)
			if h.last.Integrity.Status != "pass" || h.collectCalls != 1 || h.gateCalls != 1 {
				t.Fatal("fresh final binding changed")
			}
			if name == "clean_stop" {
				if ctx.Err() == nil || len(h.calls) != 1 || h.journal.StopReason == "completed" || h.warningCalls != 1 {
					t.Fatal("cancelled run did not finalize honestly with a fresh context")
				}
			} else {
				if h.probe == nil || h.probe.calls == 0 || h.snapshot.Components[0].ID != "SYNTHETIC-BORROWED-SNAPSHOT-MUTATION" || h.warningCalls != 0 || h.stderr.Len() != 0 {
					t.Fatal("quiet-path mutation prerequisite or ownership failed")
				}
			}
		})
	}
}

func TestSessionObservationRealProviderAndReset(t *testing.T) {
	for _, name := range []string{"failed_optional_read", "same_provider_reset"} {
		t.Run(name, func(t *testing.T) {
			source, _ := newPlanObservationSource(t)
			source.failed["sys/class/dmi/id/product_uuid"] = true
			collector := inventory.NewLinuxCollectorWithSource(source)
			// Fixture plan preparation is separate from the one acquisition in each command.
			initial, err := collector.Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			run := func(warning bool) {
				h := newSessionObservationHarness(t, "quick")
				h.snapshot = initial
				h.frozen, err = plan.Build(initial, plan.ProfileQuick, "x86_64", bytes.Repeat([]byte{0x31}, 32), "ephemeral-session")
				if err != nil {
					t.Fatal(err)
				}
				writeSessionFixture(t, filepath.Join(h.directory, "plan.json"), h.frozen)
				h.collector = collector.CollectWithDiagnostics
				source.reads = map[string]int{}
				if code := h.run(t.Context()); code != 0 {
					t.Fatalf("real provider adapter: %d %s", code, &h.stderr)
				}
				requireSessionObservationBundle(t, h)
				want := ""
				if warning {
					want = "inventory: metadata reads failed (scalar=1 cpu=0 memory=0 input=0)\n"
				}
				if h.stderr.String() != want || h.collectCalls != 1 || source.callbacks != [3]int{} {
					t.Fatal("real provider counters or privacy changed")
				}
				for _, path := range []string{"proc/cpuinfo", "proc/meminfo", "proc/bus/input/devices", "sys/class/dmi/id/product_uuid"} {
					if source.reads[path] != 1 {
						t.Fatal("adapter collected twice")
					}
				}
			}
			run(true)
			if name == "same_provider_reset" {
				source.failed = map[string]bool{}
				run(false)
			}
		})
	}
}

func TestSessionObservationEarlierAdmission(t *testing.T) {
	for _, name := range []string{"short_fingerprint", "missing_parent", "wrong_parent_signer"} {
		t.Run(name, func(t *testing.T) {
			h := newSessionObservationHarness(t, "quick")
			switch name {
			case "short_fingerprint":
				if err := os.WriteFile(filepath.Join(h.directory, "synthetic.key"), bytes.Repeat([]byte{0x31}, 31), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing_parent":
				h.arguments = append(h.arguments, "-previous-report", filepath.Join(h.directory, "absent-parent.hhpcr"), "-previous-self-issued")
			case "wrong_parent_signer":
				parent := commandRetestArtifact(t, "legacy-parent", false, nil, 0x51)
				path, key := writeCommandRetest(t, t.TempDir(), "parent", parent)
				wrong := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x53}, 32)).Public().(ed25519.PublicKey)
				if err := os.WriteFile(key, wrong, 0600); err != nil {
					t.Fatal(err)
				}
				h.arguments = append(h.arguments, "-previous-report", path, "-previous-public-key", key, "-previous-key-id", parent.keyID)
			}
			if code := h.run(t.Context()); code == 0 || h.collectCalls != 0 || h.warningCalls != 0 || h.randomBytes != 0 || h.gateCalls != 0 {
				t.Fatal("earlier input admission reached final collection or signing")
			}
			requirePlanObservationAbsent(t, filepath.Join(h.directory, "session.hhpcr"))
			requirePlanObservationAbsent(t, filepath.Join(h.directory, "session-ready.txt"))
			if name == "short_fingerprint" {
				requireSessionObservationOwners(t, h)
				if h.finalCalls != 1 {
					t.Fatal("short fingerprint did not reach actual finalizer admission")
				}
			} else if h.finalCalls != 0 || len(h.calls) != 0 || h.safety != nil || h.heat != nil {
				t.Fatal("invalid predecessor started session work")
			}
		})
	}
}
