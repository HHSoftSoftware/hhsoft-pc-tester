package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func TestSessionKeyboardIncompleteActionsRemainVisibleAndAllowLaterWorkers(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	run := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
		if worker.ID != "input.keyboard.quick" {
			return run(ctx, worker, input, out, diagnostic)
		}
		h.calls = append(h.calls, worker.ID)
		value := completeKeyboardEvidence()
		value.Status = inputtest.StatusInconclusive
		value.StopReason = "error"
		value.Detail = "incomplete_actions"
		device := &value.Devices[0]
		device.Status = inputtest.StatusInconclusive
		device.Detail = "malformed_events"
		device.FinalNeutral = false
		device.Actions[0].ReleaseObserved = false
		device.Actions[0].PairCompleted = false
		writeSessionFixture(t, worker.OutputPath, value)
		return 0
	}
	if code := h.run(t.Context()); code != 0 || !h.finalized || len(h.calls) != 12 || h.journal.Workers[4].ID != "input.keyboard.quick" || h.journal.Workers[4].Outcome != "inconclusive" {
		t.Fatalf("incomplete keyboard blocked unrelated work: %d %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), "HHPT_KEYBOARD status=inconclusive") {
		t.Fatal("keyboard progress lost")
	}
	marker, err := os.ReadFile(filepath.Join(h.directory, "session-ready.txt"))
	if err != nil || !bytes.Contains(marker, []byte(" keyboard=inconclusive ")) {
		t.Fatal("keyboard readiness disposition lost")
	}
}

func TestSessionKeyboardCleanupAndMalformedEvidenceCannotReachFinalizer(t *testing.T) {
	for _, condition := range []string{"typed cleanup", "exit cleanup", "malformed", "existing directory"} {
		t.Run(condition, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			run := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
				if worker.ID != "input.keyboard.quick" {
					return run(ctx, worker, input, out, diagnostic)
				}
				h.calls = append(h.calls, worker.ID)
				switch condition {
				case "exit cleanup":
					return workerCleanupUncertainExitCode
				case "typed cleanup":
					value := completeKeyboardEvidence()
					value.Status = inputtest.StatusError
					value.StopReason = "cleanup_unconfirmed"
					value.Detail = "cleanup_unconfirmed"
					value.CleanupConfirmed = false
					writeSessionFixture(t, worker.OutputPath, value)
				case "malformed":
					if err := os.WriteFile(worker.OutputPath, []byte(`{"schema":"hhsoft.pc-tester.keyboard-functional/v1","status":"error"}`), 0600); err != nil {
						t.Fatal(err)
					}
				case "existing directory":
					if err := os.Mkdir(worker.OutputPath, 0700); err != nil {
						t.Fatal(err)
					}
				}
				return 1
			}
			want := 1
			if condition == "typed cleanup" || condition == "exit cleanup" {
				want = workerCleanupUncertainExitCode
			}
			if code := h.run(t.Context()); code != want || h.finalized || len(h.calls) != 5 || !h.inputFinished {
				t.Fatalf("unsafe keyboard finalized: %d %s", code, h.stderr.String())
			}
			if _, err := os.Stat(filepath.Join(h.directory, "session-ready.txt")); !os.IsNotExist(err) {
				t.Fatal("unsafe keyboard became ready")
			}
		})
	}
}

func TestSessionKeyboardCancellationRetainsTypedPartialAndStopsLaterWorkers(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	run := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
		if worker.ID != "input.keyboard.quick" {
			return run(ctx, worker, input, out, diagnostic)
		}
		h.calls = append(h.calls, worker.ID)
		value := completeKeyboardEvidence()
		cancel()
		value.MarkInterrupted(context.Canceled)
		writeSessionFixture(t, worker.OutputPath, value)
		return 0
	}
	useRealSessionFinalizer(t, h, nil, nil)
	if code := h.run(ctx); code != 0 || len(h.calls) != 5 || h.journal.Workers[4].Outcome != "aborted" || h.journal.Workers[4].State != "interrupted" || h.journal.Workers[5].State != "not_started" {
		t.Fatalf("keyboard stop lost its safe partial report: %d %s", code, h.stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(h.directory, "session.hhpcr"))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var payload app.InventoryPayload
	if json.Unmarshal(artifact.InventoryJSON, &payload) != nil || len(payload.TestResults) != 23 {
		t.Fatal("keyboard changed complete frozen row coverage")
	}
	found := false
	for _, row := range payload.TestResults {
		if row.ID == "input.keyboard.quick" {
			found = true
			if row.Version != 2 || row.Status != app.TestAborted {
				t.Fatal("keyboard definition or partial status lost")
			}
		}
	}
	if !found {
		t.Fatal("keyboard row omitted")
	}
}

func TestSessionAllOffStillRecordsFourteenWorkersAndRejectsStaleKeyboard(t *testing.T) {
	for _, architecture := range []string{"x86_64", "legacy-x86"} {
		excluded, err := plan.SelectableQuickTests(architecture)
		if err != nil {
			t.Fatal(err)
		}
		h := newSessionHarness(t, architecture, excluded)
		if code := h.run(t.Context()); code != 0 || len(h.calls) != 0 || len(h.journal.Workers) != 14 {
			t.Fatal("all-off v11 omitted worker records or started work")
		}
		h = newSessionHarness(t, architecture, excluded)
		writeSessionFixture(t, filepath.Join(h.directory, "keyboard.json"), completeKeyboardEvidence())
		if code := h.run(t.Context()); code == 0 || h.finalized || h.safetyCalls != 0 {
			t.Fatal("stale excluded keyboard evidence bypassed preflight")
		}
	}
}

func TestSessionFinalizerCannotUseHistoricalV4AsCurrentLiveEvidence(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	entropy := &pointerUnexpectedEntropy{}
	useRealSessionFinalizer(t, h, nil, entropy)
	finalize := h.dependencies.Finalize
	converted := false
	h.dependencies.Finalize = func(ctx context.Context, request sessionFinalization, out, diagnostic io.Writer) sessionFinalizationResult {
		gate := request.Gate
		request.Gate = func(ctx context.Context) (sessionJournal, error) {
			value, err := gate(ctx)
			if err != nil {
				return value, err
			}
			value.Schema = sessionevidence.SchemaV4
			workers := value.Workers[:0]
			for _, worker := range value.Workers {
				if worker.ID != "input.keyboard.quick" && worker.ID != "input.touchpad.quick" && worker.ID != "input.touchscreen.quick" && worker.ID != "inspection.exterior.quick" {
					workers = append(workers, worker)
				}
			}
			value.Workers = workers
			safety := value.Safety[:0]
			for _, record := range value.Safety {
				if record.Phase != "before-input.keyboard.quick" && record.Phase != "before-input.touchpad.quick" && record.Phase != "before-input.touchscreen.quick" && record.Phase != "before-inspection.exterior.quick" {
					safety = append(safety, record)
				}
			}
			value.Safety = safety
			if sessionevidence.ValidateStructure(value) != nil {
				t.Fatal("independent historical structure fixture is not valid")
			}
			converted = true
			return value, nil
		}
		return finalize(ctx, request, out, diagnostic)
	}
	if code := h.run(t.Context()); code == 0 || !converted || entropy.calls != 0 || !strings.Contains(h.stderr.String(), "current session journal schema") {
		t.Fatalf("historical journal authorized current signing: %d %s", code, h.stderr.String())
	}
	if _, err := os.Stat(filepath.Join(h.directory, "session.hhpcr")); !os.IsNotExist(err) {
		t.Fatal("historical live journal produced a report")
	}
}
