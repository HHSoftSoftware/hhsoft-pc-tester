package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
)

type sessionHarness struct {
	directory                string
	arguments                []string
	frozen                   plan.Plan
	dependencies             sessionDependencies
	clock                    time.Time
	calls                    []string
	safetyCalls              int
	inputFinished, finalized bool
	journal                  sessionJournal
	request                  sessionFinalization
	stdout, stderr           bytes.Buffer
}

func newSessionHarness(t *testing.T, architecture string, excluded []string) *sessionHarness {
	t.Helper()
	h := &sessionHarness{directory: t.TempDir(), clock: time.Now()}
	components := []inventory.Component{
		inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil),
		inventory.NewComponent(inventory.ComponentMemory, "memory:system", nil),
		inventory.NewComponent(inventory.ComponentSound, "sound:card0", nil),
		inventory.NewComponent(inventory.ComponentInput, "input:event0", map[string]string{"category": "pointer"}),
		inventory.NewComponent(inventory.ComponentNetwork, "network:eth0", map[string]string{"name": "eth0"}),
		inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"kind": "device"}),
		inventory.NewComponent(inventory.ComponentUSB, "usb:1-1", nil),
		inventory.NewComponent(inventory.ComponentVideo, "video:card0", map[string]string{"category": "display_controller"}),
		inventory.NewComponent(inventory.ComponentVideo, "video:connector0", map[string]string{"category": "display_connector"}),
	}
	var err error
	h.frozen, err = plan.BuildWithSelection(inventory.NewSnapshot(components), plan.ProfileQuick, architecture, bytes.Repeat([]byte{0x31}, 32), "ephemeral-session", excluded)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(h.frozen)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(h.directory, "plan.json")
	if err := os.WriteFile(planPath, encoded, 0600); err != nil {
		t.Fatal("could not write synthetic plan")
	}
	h.arguments = []string{"-plan", planPath, "-fingerprint-key", filepath.Join(h.directory, "synthetic.key"),
		"-evidence-dir", h.directory, "-output", filepath.Join(h.directory, "session.hhpcr")}
	h.dependencies.Now = func() time.Time { return h.clock }
	h.dependencies.ObserveSafety = func(ctx context.Context) safemode.Result {
		if ctx.Err() != nil {
			return safemode.Result{Schema: safemode.Schema, Status: "aborted"}
		}
		h.safetyCalls++
		return safemode.Result{Schema: safemode.Schema, Status: "pass", StorageObserved: true, NetworkObserved: true}
	}
	h.dependencies.FinishInput = func() error { h.inputFinished = true; return nil }
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, _ io.Reader, _, _ io.Writer) int {
		if ctx.Err() != nil {
			t.Fatal("worker started with cancelled context")
		}
		deadline, ok := ctx.Deadline()
		if !ok || deadline.After(h.clock.Add(worker.Timeout)) {
			t.Fatal("worker lacks frozen row deadline")
		}
		h.calls = append(h.calls, worker.ID)
		writeSessionFixture(t, worker.OutputPath, sessionCompleteEvidence(worker.ID))
		return 0
	}
	h.dependencies.Finalize = func(ctx context.Context, request sessionFinalization, stdout, _ io.Writer) int {
		if ctx.Err() != nil || !h.inputFinished {
			t.Fatal("finalization overlapped input or inherited cancellation")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 30*time.Second || time.Until(deadline) <= 0 {
			t.Fatal("finalization lacks its bounded fresh context")
		}
		journal, err := readSessionJournal(request.SafetyJournalPath)
		if err != nil {
			t.Fatal(err)
		}
		outcomes, err := parseOutcomes(request.Outcomes)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateSessionJournal(*journal, h.frozen, outcomes, request.StopReason, h.directory); err != nil {
			t.Fatal(err)
		}
		h.journal, h.request, h.finalized = *journal, request, true
		fmt.Fprintf(stdout, "HHPT_RUN_INTEGRITY status=pass plan=%s planned_components=%d final_components=%d added=0 removed=0 changed=0\n", h.frozen.ID, len(h.frozen.HardwareGraph.Components), len(h.frozen.HardwareGraph.Components))
		if err := os.WriteFile(request.OutputPath, []byte("synthetic signed-report fixture"), 0600); err != nil {
			t.Fatal("could not write synthetic finalized report")
		}
		return 0
	}
	return h
}

func writeSessionFixture(t *testing.T, path string, evidence any) {
	t.Helper()
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal("could not write owned synthetic evidence")
	}
}

func sessionCompleteEvidence(id string) any {
	switch id {
	case "cpu.quick":
		return completeCPUEvidence()
	case "memory.quick":
		return completeMemoryEvidence()
	case "audio.readiness.quick":
		return completeAudioEvidence()
	case "input.readiness.quick":
		return completeInputEvidence()
	case "input.pointer.quick":
		return completePointerEvidence()
	case "network.readiness.quick":
		return completeNetworkEvidence()
	case "storage.readiness.quick":
		return completeStorageEvidence()
	case "usb.readiness.quick":
		return completeUSBEvidence()
	case "graphics.quick":
		return completeGraphicsEvidence()
	case "display.operator.quick":
		return completeDisplayEvidence()
	}
	panic("unknown synthetic session worker")
}

func (h *sessionHarness) run(ctx context.Context) int {
	return runSessionWith(ctx, h.arguments, strings.NewReader(""), &h.stdout, &h.stderr, h.dependencies)
}

func TestSessionPointerIncompleteActionsRemainVisibleAndAllowLaterWorkers(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	runWorker := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, stdout, stderr io.Writer) int {
		if worker.ID != "input.pointer.quick" {
			return runWorker(ctx, worker, input, stdout, stderr)
		}
		h.calls = append(h.calls, worker.ID)
		result := completePointerEvidence()
		result.Status, result.StopReason = inputtest.StatusInconclusive, "observation_timeout"
		result.DurationMilliseconds = result.LimitMilliseconds
		result.Devices[0].Status = inputtest.StatusInconclusive
		result.Devices[0].XNegative = 0
		writeSessionFixture(t, worker.OutputPath, result)
		return 0
	}
	if code := h.run(t.Context()); code != 0 || !h.finalized || len(h.calls) != 10 || h.journal.StopReason != "completed" {
		t.Fatalf("incomplete pointer interrupted unrelated work: code=%d", code)
	}
	if h.journal.Workers[4].Outcome != "inconclusive" || h.journal.Workers[4].State != "completed" ||
		!strings.Contains(h.stdout.String(), "HHPT_POINTER status=inconclusive") {
		t.Fatal("pointer action gaps were hidden")
	}
	marker, err := os.ReadFile(filepath.Join(h.directory, "session-ready.txt"))
	if err != nil || !bytes.Contains(marker, []byte(" pointer=inconclusive ")) {
		t.Fatal("readiness marker lost pointer qualification")
	}
}

func TestSessionCancellationDuringPointerPreservesPartialEvidence(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runWorker := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, stdout, stderr io.Writer) int {
		if worker.ID != "input.pointer.quick" {
			return runWorker(ctx, worker, input, stdout, stderr)
		}
		h.calls = append(h.calls, worker.ID)
		result := completePointerEvidence()
		cancel()
		result.MarkInterrupted(context.Canceled)
		writeSessionFixture(t, worker.OutputPath, result)
		return 0
	}
	if code := h.run(ctx); code != 0 || !h.finalized || len(h.calls) != 5 || h.journal.StopReason != "operator_cancelled" {
		t.Fatalf("pointer cancellation did not finalize the stopped run: code=%d", code)
	}
	if h.journal.Workers[4].State != "interrupted" || h.journal.Workers[4].Outcome != "aborted" || h.journal.Workers[5].State != "not_started" {
		t.Fatal("pointer cancellation started a later worker or discarded its outcome")
	}
}

func TestSessionExecutesFrozenAllowlistAndRetainsSelection(t *testing.T) {
	for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
		t.Run(architecture, func(t *testing.T) {
			h := newSessionHarness(t, architecture, []string{"audio.readiness.quick"})
			if code := h.run(t.Context()); code != 0 {
				t.Fatalf("code=%d stderr=%s", code, &h.stderr)
			}
			want := []string{"cpu.quick", "memory.quick", "input.readiness.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick"}
			if architecture != "legacy-x86" {
				want = append(want, "graphics.quick", "display.operator.quick")
			}
			if !h.finalized || !reflect.DeepEqual(h.calls, want) || h.journal.StopReason != "completed" || h.safetyCalls != len(want)+2 {
				t.Fatalf("calls=%v finalized=%v safety=%d stop=%s", h.calls, h.finalized, h.safetyCalls, h.journal.StopReason)
			}
			if h.journal.Workers[2].State != "not_scheduled" || h.journal.Workers[2].Outcome != "excluded" {
				t.Fatal("excluded choice was changed")
			}
			if architecture == "legacy-x86" && (h.journal.Workers[8].Outcome != "unsupported" || h.journal.Workers[9].Outcome != "unsupported") {
				t.Fatal("legacy unsupported rows were changed")
			}
			if !strings.Contains(h.stdout.String(), "HHPT_SESSION_RESULT reason=completed") || !strings.Contains(h.stdout.String(), "HHPT_AUDIO_READINESS status=excluded") {
				t.Fatal("session progress markers missing")
			}
			marker, err := os.ReadFile(filepath.Join(h.directory, "session-ready.txt"))
			if err != nil || !bytes.Contains(marker, []byte(" audio=excluded cpu=pass memory=pass")) {
				t.Fatal("status-rich readiness summary missing")
			}
		})
	}
}

func TestSessionStopsAndFinalizesWithFreshContext(t *testing.T) {
	for _, ending := range []string{"operator_cancelled", "run_deadline", "test_deadline", "worker_stop", "failure_cancel_race"} {
		t.Run(ending, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h.dependencies.RunWorker = func(_ context.Context, worker sessionWorker, _ io.Reader, _, _ io.Writer) int {
				h.calls = append(h.calls, worker.ID)
				evidence := completeCPUEvidence()
				switch ending {
				case "operator_cancelled":
					evidence.Status = quick.StatusAborted
					cancel()
				case "run_deadline":
					evidence.Status = quick.StatusAborted
					h.clock = h.clock.Add(6 * time.Minute)
				case "test_deadline":
					evidence.Status = quick.StatusAborted
					h.clock = h.clock.Add(21 * time.Second)
				case "worker_stop":
					evidence.Status = quick.StatusFail
				case "failure_cancel_race":
					evidence.Status = quick.StatusFail
					cancel()
				}
				writeSessionFixture(t, worker.OutputPath, evidence)
				return 0
			}
			if code := h.run(ctx); code != 0 {
				t.Fatalf("code=%d stderr=%s", code, &h.stderr)
			}
			wantReason := ending
			if ending == "failure_cancel_race" {
				wantReason = "operator_cancelled"
			}
			if !reflect.DeepEqual(h.calls, []string{"cpu.quick"}) || h.journal.StopReason != wantReason || h.safetyCalls != 3 {
				t.Fatalf("calls=%v reason=%s safety=%d", h.calls, h.journal.StopReason, h.safetyCalls)
			}
			first := h.journal.Workers[0]
			if ending == "worker_stop" || ending == "failure_cancel_race" {
				if first.State != "completed" || first.Outcome != "fail" {
					t.Fatal("completed failure was downgraded")
				}
			} else if first.State != "interrupted" || first.Outcome != "aborted" {
				t.Fatal("interrupted active worker lost its state")
			}
			for _, record := range h.journal.Workers[1:] {
				if record.State != "not_started" || record.Outcome != "aborted" {
					t.Fatal("untouched scheduled worker was reported as executed")
				}
			}
		})
	}
}

func TestSessionNeverSignsAfterUnconfirmedCleanupOrInputFailure(t *testing.T) {
	for _, failure := range []string{"worker-cleanup", "input-finish", "final-safety", "preflight-safety", "readiness-error", "readiness-cancel", "failure-downgrade"} {
		t.Run(failure, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			worker := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, errOut io.Writer) int {
				if failure == "worker-cleanup" {
					h.calls = append(h.calls, w.ID)
					return 12
				}
				if (failure == "readiness-error" || failure == "readiness-cancel") && w.ID == "network.readiness.quick" {
					h.calls = append(h.calls, w.ID)
					result := completeNetworkEvidence()
					result.Status = networktest.StatusError
					result.ActiveNonLoopInterfaces = 1
					if failure == "readiness-cancel" {
						result.Status = networktest.StatusAborted
						cancel()
					}
					writeSessionFixture(t, w.OutputPath, result)
					return 0
				}
				if failure == "failure-downgrade" && w.ID == "graphics.quick" {
					h.calls = append(h.calls, w.ID)
					result := completeGraphicsEvidence()
					result.Status = graphics.StatusInconclusive
					result.Pattern.Status = graphics.StatusFail
					writeSessionFixture(t, w.OutputPath, result)
					return 1
				}
				return worker(ctx, w, in, out, errOut)
			}
			if failure == "input-finish" {
				h.dependencies.FinishInput = func() error { return errors.New("synthetic input join failure") }
			}
			observe := h.dependencies.ObserveSafety
			h.dependencies.ObserveSafety = func(ctx context.Context) safemode.Result {
				result := observe(ctx)
				if failure == "preflight-safety" || failure == "final-safety" && h.inputFinished {
					result.Status = "fail"
					result.WritableBlockMounts = 1
				}
				return result
			}
			code := h.run(ctx)
			if code == 0 || h.finalized {
				t.Fatalf("unsafe session finalized: code=%d", code)
			}
			if failure == "worker-cleanup" && (code != 12 || len(h.calls) != 1) {
				t.Fatal("cleanup uncertainty lost its terminal code")
			}
			if failure == "preflight-safety" || failure == "final-safety" {
				phase := "preflight"
				if failure == "final-safety" {
					phase = "final"
				}
				raw, err := os.ReadFile(filepath.Join(h.directory, "session-safety-"+phase+".json"))
				if err != nil || !bytes.Contains(raw, []byte(`"writable_block_mounts": 1`)) {
					t.Fatal("unsafe diagnostic observation was not retained")
				}
			}
			if _, err := os.Stat(filepath.Join(h.directory, "session-journal.json")); !os.IsNotExist(err) {
				t.Fatal("terminal uncertainty created a signable journal")
			}
		})
	}
}

func TestSessionFreshDestinationsRejectBeforeAnyWorker(t *testing.T) {
	for _, name := range []string{"cpu.json", "graphics.json", "session-journal.json", "session-safety-final.json", "session-ready.txt", "session.hhpcr"} {
		t.Run(name, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			path := filepath.Join(h.directory, name)
			if err := os.WriteFile(path, []byte("retained fixture"), 0600); err != nil {
				t.Fatal("could not create existing fixture")
			}
			if code := h.run(t.Context()); code == 0 || len(h.calls) != 0 || h.safetyCalls != 0 || h.finalized {
				t.Fatal("stale destination did not fail before work")
			}
			raw, err := os.ReadFile(path)
			if err != nil || string(raw) != "retained fixture" {
				t.Fatal("existing evidence was changed")
			}
		})
	}
}

func TestSessionOrdinaryGraphicsErrorRemainsReportable(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	worker := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, errOut io.Writer) int {
		if w.ID == "graphics.quick" {
			h.calls = append(h.calls, w.ID)
			return 1
		}
		return worker(ctx, w, in, out, errOut)
	}
	if code := h.run(t.Context()); code != 0 || !h.finalized || len(h.calls) != 10 || h.journal.Workers[8].Outcome != "error" {
		t.Fatalf("ordinary error behavior changed: code=%d stderr=%s", code, &h.stderr)
	}
}

func TestSessionCannotClaimReadyFromMissingOrAmbiguousIntegrity(t *testing.T) {
	for _, mode := range []string{"missing", "duplicate", "truncated", "changed-plan", "bad-count", "output-failure", "existing-ready"} {
		t.Run(mode, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			finalize := h.dependencies.Finalize
			h.dependencies.Finalize = func(ctx context.Context, request sessionFinalization, stdout, stderr io.Writer) int {
				var captured bytes.Buffer
				code := finalize(ctx, request, &captured, stderr)
				text := captured.String()
				switch mode {
				case "missing":
					text = "finalized\n"
				case "duplicate":
					text += text
				case "truncated":
					text += strings.Repeat("x", 1025)
				case "changed-plan":
					text = strings.ReplaceAll(text, h.frozen.ID, "sha256:"+strings.Repeat("0", 64))
				case "bad-count":
					text = strings.ReplaceAll(text, "final_components=9", "final_components=09")
				case "output-failure":
					stdout.(*sessionFinalOutput).destination = cleanupFailedWriter{}
				case "existing-ready":
					if err := os.WriteFile(filepath.Join(h.directory, "session-ready.txt"), []byte("retain"), 0600); err != nil {
						t.Fatal("could not create synthetic ready race")
					}
				}
				fmt.Fprint(stdout, text)
				return code
			}
			if code := h.run(t.Context()); code == 0 {
				t.Fatal("invalid finalizer metadata produced readiness")
			}
			if _, err := os.Stat(filepath.Join(h.directory, "session.hhpcr")); err != nil {
				t.Fatal("failed summary discarded completed report")
			}
			if mode != "existing-ready" {
				if _, err := os.Stat(filepath.Join(h.directory, "session-ready.txt")); !os.IsNotExist(err) {
					t.Fatal("invalid readiness summary was created")
				}
			}
		})
	}
}

func TestSessionReadinessFindingsCannotBeClearedByLaterSafeSnapshots(t *testing.T) {
	for _, mode := range []string{"network-count", "network-interface", "storage-count", "storage-device"} {
		t.Run(mode, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			worker := h.dependencies.RunWorker
			target := "network.readiness.quick"
			if strings.HasPrefix(mode, "storage") {
				target = "storage.readiness.quick"
			}
			h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, errOut io.Writer) int {
				if w.ID != target {
					return worker(ctx, w, in, out, errOut)
				}
				h.calls = append(h.calls, w.ID)
				if target == "network.readiness.quick" {
					result := completeNetworkEvidence()
					result.Status = networktest.StatusInconclusive
					if mode == "network-count" {
						result.ActiveNonLoopInterfaces = 1
					} else {
						result.Interfaces[0].AdministrativeUp = true
					}
					writeSessionFixture(t, w.OutputPath, result)
				} else {
					result := completeStorageEvidence()
					result.Status = storage.StatusInconclusive
					if mode == "storage-count" {
						result.WritableMounts = 1
					} else {
						result.Devices[0].WritableMount = true
					}
					writeSessionFixture(t, w.OutputPath, result)
				}
				return 0
			}
			if code := h.run(t.Context()); code == 0 || h.finalized || h.calls[len(h.calls)-1] != target || h.safetyCalls != len(h.calls)+1 {
				t.Fatalf("unsafe readiness allowed later work: calls=%v safety=%d finalized=%v", h.calls, h.safetyCalls, h.finalized)
			}
		})
	}
}

func TestSessionExplicitGraphicsFailureStopsDisplayAndRemainsFailure(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	worker := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, errOut io.Writer) int {
		if w.ID != "graphics.quick" {
			return worker(ctx, w, in, out, errOut)
		}
		h.calls = append(h.calls, w.ID)
		result := completeGraphicsEvidence()
		result.Status = graphics.StatusFail
		result.Pattern.Status = graphics.StatusFail
		writeSessionFixture(t, w.OutputPath, result)
		return 0
	}
	if code := h.run(t.Context()); code != 0 || !h.finalized || len(h.calls) != 9 || h.journal.StopReason != "worker_stop" ||
		h.journal.Workers[8].Outcome != "fail" || h.journal.Workers[8].State != "completed" || h.journal.Workers[9].State != "not_started" {
		t.Fatalf("graphics failure was lost: code=%d calls=%v stderr=%s", code, h.calls, &h.stderr)
	}
	marker, err := os.ReadFile(filepath.Join(h.directory, "session-ready.txt"))
	if err != nil || !bytes.Contains(marker, []byte("session_stop=worker_stop")) {
		t.Fatal("readiness marker omitted stop reason")
	}
}

func TestSessionRefusesContradictorySafetyAndMissingInputJoin(t *testing.T) {
	for _, mode := range []string{"storage-detail", "network-detail", "missing-join"} {
		t.Run(mode, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			if mode == "missing-join" {
				h.dependencies.FinishInput = nil
			} else {
				observe := h.dependencies.ObserveSafety
				h.dependencies.ObserveSafety = func(ctx context.Context) safemode.Result {
					result := observe(ctx)
					if mode == "storage-detail" {
						result.StorageDetail = "incomplete synthetic observation"
					} else {
						result.NetworkDetail = "incomplete synthetic observation"
					}
					return result
				}
			}
			if code := h.run(t.Context()); code == 0 || len(h.calls) != 0 || h.finalized {
				t.Fatal("unknown safety or input lifetime started work")
			}
		})
	}
}

func TestSessionAllExcludedStillRequiresFreshFinalSafety(t *testing.T) {
	for _, architecture := range []string{"x86_64", "legacy-x86"} {
		for _, unsafeFinal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/unsafe=%v", architecture, unsafeFinal), func(t *testing.T) {
				ids, err := plan.SelectableQuickTests(architecture)
				if err != nil {
					t.Fatal(err)
				}
				h := newSessionHarness(t, architecture, ids)
				observe := h.dependencies.ObserveSafety
				h.dependencies.ObserveSafety = func(ctx context.Context) safemode.Result {
					result := observe(ctx)
					if h.safetyCalls == 2 {
						if !h.inputFinished {
							t.Fatal("all-off session observed final safety before joining input")
						}
						if unsafeFinal {
							result.Status = "fail"
							result.ActiveNonLoopInterfaces = 1
						}
					}
					return result
				}
				code := h.run(t.Context())
				if len(h.calls) != 0 || h.safetyCalls != 2 || !h.inputFinished {
					t.Fatal("all-off session executed workers or omitted safety/input cleanup")
				}
				if unsafeFinal {
					if code == 0 || h.finalized {
						t.Fatal("all-off selection bypassed unsafe final observation")
					}
					return
				}
				if code != 0 || !h.finalized || len(h.journal.Safety) != 2 {
					t.Fatalf("all-off session failed: code=%d stderr=%s", code, &h.stderr)
				}
				for _, record := range h.journal.Workers {
					if record.State != "not_scheduled" {
						t.Fatal("excluded/unsupported row lost its frozen state")
					}
				}
			})
		}
	}
}
