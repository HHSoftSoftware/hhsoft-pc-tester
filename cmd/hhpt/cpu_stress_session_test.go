package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func newStandardHarness(t *testing.T, value quick.CPUStressResult) *sessionHarness {
	t.Helper()
	stressFixtureBytes(t, value)
	h := newSessionHarness(t, "x86_64", nil)
	var err error
	h.frozen, err = plan.Build(h.snapshot, plan.ProfileStandard, "x86_64", bytes.Repeat([]byte{0x31}, 32), "ephemeral-session")
	if err != nil {
		t.Fatal(err)
	}
	writeSessionFixture(t, filepath.Join(h.directory, "plan.json"), h.frozen)
	h.arguments = append(h.arguments, "-profile", "standard")
	run := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
		if worker.ID != "cpu.stress" {
			return run(ctx, worker, input, out, diagnostic)
		}
		if ctx.Err() != nil || worker.Timeout != 660*time.Second {
			t.Fatal("stress dispatched outside its budget")
		}
		h.calls = append(h.calls, worker.ID)
		writeSessionFixture(t, worker.OutputPath, value)
		h.clock = h.clock.Add(time.Duration(value.FinishedMilliseconds) * time.Millisecond)
		return 0
	}
	useStandardFinalizer(t, h, nil)
	return h
}

// The real profile-aware finalizer, exclusive file output, live gate, bundle,
// views and verifier are used. Only inventory/clock/observation inputs are fixtures.
func useStandardFinalizer(t *testing.T, h *sessionHarness, change func(*sessionFinalization)) {
	t.Helper()
	if os.WriteFile(filepath.Join(h.directory, "synthetic.key"), bytes.Repeat([]byte{0x31}, 32), 0600) != nil {
		t.Fatal("fixture key")
	}
	h.dependencies.ReadRetestInput = captureFixtureInput(t)
	h.dependencies.Finalize = func(ctx context.Context, request sessionFinalization, out, diagnostic io.Writer) sessionFinalizationResult {
		if change != nil {
			change(&request)
		}
		args := []string{"-plan", request.PlanPath, "-fingerprint-key", request.FingerprintKeyPath,
			"-key-epoch", request.KeyEpoch, "-evidence-dir", request.EvidenceDirectory, "-output", request.OutputPath}
		if request.Previous != nil {
			args = append(args, "-session-id", "c23d32bc-1ccf-4ca7-b977-2a1dadad7af1")
		}
		for _, outcome := range request.Outcomes {
			args = append(args, "-outcome", outcome)
		}
		result := runFinalizeOwnedDestinationWithPreviousForProfile(ctx, args, out, diagnostic,
			func(context.Context) (inventory.Snapshot, error) { return h.snapshot, nil },
			bytes.NewReader(bytes.Repeat([]byte{0x62}, 128)), time.Now, request.Gate,
			writeDestination, request.Previous, captureFixtureInput(t), request.Profile)
		h.request, h.finalized = request, result.Code == 0
		if result.Journal != nil {
			h.journal = sessionevidence.CloneJournal(*result.Journal)
		}
		return result
	}
}

func TestCPUStressStandardSessionRequiresExplicitMatchingProfile(t *testing.T) {
	for _, profile := range []string{"omitted", "quick", "extended", "destructive", "STANDARD"} {
		t.Run(profile, func(t *testing.T) {
			h := newStandardHarness(t, commandStressPass())
			h.arguments = h.arguments[:len(h.arguments)-2]
			if profile != "omitted" {
				h.arguments = append(h.arguments, "-profile", profile)
			}
			starts := 0
			h.dependencies.StartMonitor = func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error) { starts++; return nil, nil }
			if code := h.run(t.Context()); code == 0 || starts != 0 || h.safetyCalls != 0 || len(h.calls) != 0 || h.finalized {
				t.Fatal("profile mismatch reached observers or output")
			}
			if _, err := os.Stat(filepath.Join(h.directory, "session.hhpcr")); !os.IsNotExist(err) {
				t.Fatal("profile mismatch created a report")
			}
		})
	}
	h := newSessionHarness(t, "x86_64", nil)
	h.arguments = append(h.arguments, "-profile", "standard")
	if code := h.run(t.Context()); code == 0 || len(h.calls) != 0 || h.safetyCalls != 0 {
		t.Fatal("standard flag accepted quick plan")
	}
}

func TestCPUStressStandardSessionPreservesOrderAndSource(t *testing.T) {
	h := newStandardHarness(t, commandStressPass())
	if code := h.run(t.Context()); code != 0 || !h.finalized {
		t.Fatalf("standard session failed: %d %s", code, &h.stderr)
	}
	want := []string{"cpu.quick", "cpu.stress", "memory.quick", "audio.readiness.quick", "input.readiness.quick", "input.keyboard.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick", "graphics.quick", "display.operator.quick", "inspection.exterior.quick"}
	if !reflect.DeepEqual(h.calls, want) || h.journal.Schema != sessionevidence.SchemaV8 ||
		h.journal.Profile != "standard" || h.journal.BudgetMilliseconds != 900000 || h.request.Profile != plan.ProfileStandard {
		t.Fatal("standard profile/order/default changed")
	}
	raw, err := os.ReadFile(filepath.Join(h.directory, "cpu-stress.json"))
	if err != nil || h.journal.CPUStressSource == nil || h.journal.CPUStressSource.EvidenceFileSHA256 != sourceFileDigest(raw) {
		t.Fatal("stress source did not retain exact bytes")
	}
	ready, err := os.ReadFile(filepath.Join(h.directory, "session-ready.txt"))
	if err != nil || !bytes.Contains(ready, []byte("profile=standard cpu_stress=pass")) ||
		!bytes.Contains(ready, []byte("exterior_inspection=pass")) {
		t.Fatal("standard readiness labels lost")
	}
}

func TestCPUStressNoLoadNonPassStillBindsSourceAndStopsLaterWorkers(t *testing.T) {
	value := commandStressNoLoad()
	value.EffectiveDeadlineMilliseconds = 19000
	h := newStandardHarness(t, value)
	h.arguments = append(h.arguments, "-timeout", "20s")
	if code := h.run(t.Context()); code != 0 || !h.finalized {
		t.Fatalf("no-load session failed: %d %s", code, &h.stderr)
	}
	if !reflect.DeepEqual(h.calls, []string{"cpu.quick", "cpu.stress"}) || h.journal.StopReason != "worker_stop" ||
		h.journal.CPUStressSource == nil || h.journal.BudgetMilliseconds != 20000 {
		t.Fatal("no-load result became an unstarted note")
	}
	if h.journal.Workers[1].State != "completed" || h.journal.Workers[1].Outcome != "inconclusive" {
		t.Fatal("valid non-pass changed")
	}
	for _, worker := range h.journal.Workers[2:] {
		if worker.State != "not_started" || worker.Outcome != "aborted" || worker.Timing != nil {
			t.Fatal("later workload started")
		}
	}
}

func TestCPUStressUnstartedKeepsNullSourceAndEarlierFailure(t *testing.T) {
	h := newStandardHarness(t, commandStressPass())
	h.dependencies.RunWorker = func(_ context.Context, worker sessionWorker, _ io.Reader, _, _ io.Writer) int {
		h.calls = append(h.calls, worker.ID)
		writeSessionFixture(t, worker.OutputPath, cpuScalarFailureFixture(0, false))
		return 0
	}
	if code := h.run(t.Context()); code != 0 || !h.finalized {
		t.Fatalf("earlier failure failed to finalize: %d %s", code, &h.stderr)
	}
	if !reflect.DeepEqual(h.calls, []string{"cpu.quick"}) || h.journal.CPUStressSource != nil ||
		h.journal.Workers[0].Outcome != "fail" || h.journal.Workers[1].State != "not_started" {
		t.Fatal("earlier failure lost or stress invented")
	}
}

func TestCPUStressSessionDispatchedOutputFailureCannotSign(t *testing.T) {
	for _, mode := range []string{"missing", "malformed", "nonzero", "cleanup", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			h := newStandardHarness(t, commandStressNoLoad())
			run := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
				if worker.ID != "cpu.stress" {
					return run(ctx, worker, input, out, diagnostic)
				}
				h.calls = append(h.calls, worker.ID)
				switch mode {
				case "missing":
					return 0
				case "malformed":
					if os.WriteFile(worker.OutputPath, []byte("{"), 0600) != nil {
						t.Fatal("fixture")
					}
					return 0
				case "oversized":
					if os.WriteFile(worker.OutputPath, bytes.Repeat([]byte(" "), (1<<20)+1), 0600) != nil {
						t.Fatal("fixture")
					}
					return 0
				case "cleanup":
					return 12
				default:
					writeSessionFixture(t, worker.OutputPath, commandStressNoLoad())
					return 1
				}
			}
			want := 1
			if mode == "cleanup" {
				want = 12
			}
			if code := h.run(t.Context()); code != want || h.finalized || len(h.calls) != 2 {
				t.Fatal("dispatch failure signed or continued")
			}
			if _, err := os.Stat(filepath.Join(h.directory, "session.hhpcr")); !os.IsNotExist(err) {
				t.Fatal("invalid stress report exists")
			}
		})
	}
}

func TestCPUStressProfileDefinitionsAreCopiedAndDoNotInferFromOutcomes(t *testing.T) {
	standard, err := executionResultDefinitions(plan.ProfileStandard)
	if err != nil || len(standard) != 13 {
		t.Fatal("standard definitions")
	}
	standard[0].id = "changed"
	quickDefs, err := executionResultDefinitions("")
	if err != nil || len(quickDefs) != 12 || quickDefs[0].id != "audio.readiness.quick" {
		t.Fatal("profile mutated quick definitions")
	}
	values := make([]string, 0, 13)
	for _, definition := range quickDefs {
		values = append(values, definition.id+"=aborted")
	}
	values = append(values, "cpu.stress=aborted")
	if _, err := parseOutcomes(values); err == nil {
		t.Fatal("outcome names selected stress profile")
	}
	good, _ := executionResultDefinitions(plan.ProfileStandard)
	if _, err := parseOutcomesWithDefinitions(values, good); err != nil {
		t.Fatal("explicit definitions rejected")
	}
	if _, err := parseOutcomesWithDefinitions(append(values, "cpu.stress=pass"), good); err == nil {
		t.Fatal("duplicate stress outcome accepted")
	}
}

func TestCPUStressRealDispatchHonorsInsufficientBudgetWithoutLoad(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "stress.json")
	var out, diagnostic bytes.Buffer
	code := runSessionWorker(ctx, sessionWorker{ID: "cpu.stress", Architecture: "x86_64", OutputPath: path, Timeout: 660 * time.Second},
		strings.NewReader(""), &out, &diagnostic)
	if code != 0 {
		t.Fatalf("real short-budget diagnostic failed: %d %s", code, &diagnostic)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("diagnostic missing")
	}
	var value quick.CPUStressResult
	if json.Unmarshal(raw, &value) != nil || value.LoadStarted || value.WorkersLaunched != 0 ||
		value.Status != quick.StatusInconclusive || value.StopReason != "insufficient_budget" || value.Safety.MonitorStatus != "not_started" {
		t.Fatal("short budget launched observation or arithmetic")
	}
}

// These mutations are rejected by plan.Decode before the defensive scheduling
// guard. They demonstrate that no current accepted plan bypasses CPU quick;
// the normal standard-session test separately exercises real dispatch order.
func TestCPUStressStandardPlanCannotOmitQuickPrerequisite(t *testing.T) {
	for _, state := range []plan.TestStatus{plan.TestExcluded, plan.TestUnsupported, plan.TestNotPresent, plan.TestMissingDriver} {
		t.Run(string(state), func(t *testing.T) {
			h := newStandardHarness(t, commandStressPass())
			raw, err := json.Marshal(h.frozen)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := plan.Decode(raw); err != nil {
				t.Fatal("valid prerequisite control rejected")
			}
			changed, scheduled := false, false
			for index := range h.frozen.Tests {
				row := &h.frozen.Tests[index]
				if row.ID == "cpu.quick" {
					row.Status = state
					changed = true
				}
				if row.ID == "cpu.stress" {
					scheduled = row.Status == plan.TestScheduled
				}
			}
			if !changed || !scheduled {
				t.Fatal("prerequisite fixture not reached")
			}
			writeSessionFixture(t, filepath.Join(h.directory, "plan.json"), h.frozen)
			starts := 0
			h.dependencies.StartMonitor = func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error) { starts++; return nil, nil }
			if code := h.run(t.Context()); code == 0 || starts != 0 || h.safetyCalls != 0 || len(h.calls) != 0 || h.finalized {
				t.Fatal("invalid CPU prerequisite reached observation or dispatch")
			}
			for _, name := range []string{"cpu-stress.json", "session-journal.json", "session.hhpcr", "session-ready.txt"} {
				if _, err := os.Stat(filepath.Join(h.directory, name)); !os.IsNotExist(err) {
					t.Fatal("invalid prerequisite created evidence")
				}
			}
		})
	}
	h := newStandardHarness(t, commandStressPass())
	if _, err := plan.BuildWithSelection(h.snapshot, plan.ProfileStandard, "x86_64", bytes.Repeat([]byte{0x31}, 32),
		"ephemeral-session", []string{"cpu.quick"}); err == nil {
		t.Fatal("standard CPU exclusion accepted")
	}
}
