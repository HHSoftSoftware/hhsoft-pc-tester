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
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/keyboardevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func TestKeyboardCommandKeepsHardCauseThroughCollectAndFinishCancellation(t *testing.T) {
	for _, phase := range []string{"collect", "finish", "finish_error"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			expected := completeKeyboardEvidence()
			expected.Status, expected.StopReason, expected.Detail = inputtest.StatusError, "error", "disconnected"
			fixture := &keyboardHandoffFixture{}
			finishes, collections := 0, 0
			var out, stderr bytes.Buffer
			prepare := func(ctx context.Context, _ io.Reader) (context.Context, io.Reader, func() error, error) {
				return ctx, fixture, func() error {
					finishes++
					cancel()
					if phase == "finish_error" {
						return errSessionInput
					}
					return nil
				}, nil
			}
			collect := func(context.Context, inputtest.KeyboardOptions) inputtest.KeyboardResult {
				collections++
				if phase == "collect" {
					cancel()
				}
				return expected
			}
			code := runKeyboardWith(ctx, nil, strings.NewReader(""), &out, &stderr, collect, prepare)
			wantCode := 0
			if phase == "finish_error" {
				wantCode = 1
				expected.Detail = "console_unavailable"
			}
			if code != wantCode || collections != 1 || finishes != 1 || fixture.reads != 0 {
				t.Fatalf("console ownership or command result changed: %d", code)
			}
			actual, err := keyboardevidence.Decode(out.Bytes())
			if err != nil || !reflect.DeepEqual(actual, expected) {
				t.Fatalf("hard result changed: %#v %v", actual, err)
			}
			if outcome, state, err := sessionWorkerOutcomeEvidence(out.Bytes(), nil, inputtest.KeyboardSchema, 0, true); err != nil || outcome != "error" || state != "interrupted" {
				t.Fatal("session hid a reportable hard error")
			}
			if sessionSafetyWorker("input.keyboard.quick") {
				t.Fatal("keyboard acquired a new safety-worker policy")
			}
			assertKeyboardAcquisitionFinalizerBoundary(t, out.Bytes(), false)
		})
	}
}

func TestKeyboardCommandCleanupPrecedesCancelledHardErrorAndOutputFailure(t *testing.T) {
	for _, writeFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved", true: "output_failure"}[writeFails], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			value := completeKeyboardEvidence()
			value.Status, value.StopReason, value.Detail = inputtest.StatusError, "error", "disconnected"
			finished := false
			output := &keyboardOutputFixture{finished: &finished, fail: writeFails}
			finishes := 0
			var stderr bytes.Buffer
			prepare := func(ctx context.Context, _ io.Reader) (context.Context, io.Reader, func() error, error) {
				return ctx, &keyboardHandoffFixture{}, func() error { finishes++; finished = true; cancel(); return errSessionInputCleanup }, nil
			}
			collect := func(context.Context, inputtest.KeyboardOptions) inputtest.KeyboardResult { cancel(); return value }
			if runKeyboardWith(ctx, nil, strings.NewReader(""), output, &stderr, collect, prepare) != workerCleanupUncertainExitCode || finishes != 1 {
				t.Fatal("cleanup lost exit12 precedence")
			}
			if !writeFails {
				actual, err := keyboardevidence.Decode(output.Bytes())
				if !errors.Is(err, keyboardevidence.ErrCleanupUnconfirmed) || actual.CleanupConfirmed || actual.StopReason != "cleanup_unconfirmed" {
					t.Fatal("cleanup diagnostic invalid")
				}
				assertKeyboardAcquisitionFinalizerBoundary(t, output.Bytes(), true)
			}
		})
	}
}

// This is a consumer composition witness. It does not fabricate the live
// keyboard gate or claim that standalone attempted evidence can be signed.
func assertKeyboardAcquisitionFinalizerBoundary(t *testing.T, raw []byte, cleanup bool) {
	t.Helper()
	value, err := keyboardevidence.Decode(raw)
	if cleanup {
		if !errors.Is(err, keyboardevidence.ErrCleanupUnconfirmed) {
			t.Fatal("cleanup precondition invalid")
		}
	} else if err != nil || value.Status == inputtest.StatusPass {
		t.Fatal("hard-evidence precondition invalid")
	}
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x45}, 32)
	keyPath := writeTestKey(t, directory, "fingerprint.key", key)
	snapshot := inventory.NewSnapshot([]inventory.Component{keyboardInventoryFixture()})
	frozen, err := plan.BuildWithSelection(snapshot, plan.ProfileQuick, "x86_64", key, "ephemeral-session", []string{"input.readiness.quick"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(directory, "plan.json")
	evidencePath := filepath.Join(directory, "keyboard.json")
	if err := os.WriteFile(planPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidencePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded plan.Plan
	if json.Unmarshal(saved, &decoded) != nil {
		t.Fatal("written plan invalid")
	}
	scheduled := 0
	for _, row := range decoded.Tests {
		if row.ID == "input.keyboard.quick" {
			if row.Status != plan.TestScheduled || row.Version != 2 {
				t.Fatal("keyboard not scheduled at definition2")
			}
			scheduled++
		}
	}
	if scheduled != 1 {
		t.Fatal("test cannot reach keyboard evidence admission")
	}
	for _, promote := range []bool{false, true} {
		report := filepath.Join(directory, map[bool]string{false: "honest.hhpcr", true: "promoted.hhpcr"}[promote])
		args := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "keyboard-acquisition", "-output", report}
		for _, definition := range quickResultDefinitions {
			for _, row := range decoded.Tests {
				if row.ID == definition.id {
					outcome := string(row.Status)
					if row.ID == "input.keyboard.quick" {
						outcome = string(value.Status)
						if promote {
							outcome = "pass"
						}
					}
					args = append(args, "-outcome", row.ID+"="+outcome)
				}
			}
		}
		var out, stderr bytes.Buffer
		entropy := &pointerUnexpectedEntropy{}
		collections, gates := 0, 0
		var gate sessionFinalGate
		if promote {
			gate = func(context.Context) (sessionJournal, error) {
				gates++
				t.Fatal("invalid promotion reached live gate")
				return sessionJournal{}, nil
			}
		}
		code := runFinalizeOwned(t.Context(), args, &out, &stderr, func(context.Context) (inventory.Snapshot, error) { collections++; return snapshot, nil }, entropy, time.Now, gate).Code
		wantCode, wantReason := 1, "functional keyboard evidence requires the live current session gate\n"
		if promote {
			wantReason = "validate evidence input.keyboard.quick: schema or status does not match supervisor outcome\n"
		}
		if cleanup {
			wantCode = workerCleanupUncertainExitCode
			wantReason = "validate keyboard evidence: cleanup unconfirmed\n"
		}
		if code != wantCode || stderr.String() != wantReason || collections != 0 || gates != 0 || entropy.calls != 0 || out.Len() != 0 {
			t.Fatalf("wrong finalizer rejection: %d %q", code, stderr.String())
		}
		if _, err := os.Stat(report); !os.IsNotExist(err) {
			t.Fatal("rejected evidence produced a report")
		}
	}
	if saved, err := os.ReadFile(evidencePath); err != nil || !bytes.Equal(saved, raw) {
		t.Fatal("rejected finalization changed evidence")
	}
}

func TestKeyboardPrecancelledCommandStillSkipsConsoleAndWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out, stderr bytes.Buffer
	prepare := func(context.Context, io.Reader) (context.Context, io.Reader, func() error, error) {
		t.Fatal("pre-cancel acquired console")
		return nil, nil, nil, nil
	}
	collect := func(context.Context, inputtest.KeyboardOptions) inputtest.KeyboardResult {
		t.Fatal("pre-cancel started worker")
		return inputtest.KeyboardResult{}
	}
	if runKeyboardWith(ctx, nil, strings.NewReader(""), &out, &stderr, collect, prepare) != 0 {
		t.Fatal("pre-cancel command failed")
	}
	value, err := keyboardevidence.Decode(out.Bytes())
	if err != nil || value.Status != inputtest.StatusAborted || !value.CleanupConfirmed || len(value.Devices) != 0 {
		t.Fatal("initial empty-detail state no longer cancels")
	}
}

func TestKeyboardHardErrorSessionUsesLiveGateWithoutPromotingEvidence(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "reportable_error", true: "error_then_cancel"}[interrupted], func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			run := h.dependencies.RunWorker
			expected := completeKeyboardEvidence()
			expected.Status, expected.StopReason, expected.Detail = inputtest.StatusError, "error", "disconnected"
			finishes := 0
			h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
				if worker.ID != "input.keyboard.quick" {
					return run(ctx, worker, input, out, diagnostic)
				}
				h.calls = append(h.calls, worker.ID)
				collect := func(context.Context, inputtest.KeyboardOptions) inputtest.KeyboardResult {
					if interrupted {
						cancel()
					}
					return expected
				}
				prepare := func(ctx context.Context, _ io.Reader) (context.Context, io.Reader, func() error, error) {
					return ctx, &keyboardHandoffFixture{}, func() error { finishes++; return nil }, nil
				}
				return runKeyboardWith(ctx, []string{"-output", worker.OutputPath}, input, out, diagnostic, collect, prepare)
			}
			useRealSessionFinalizer(t, h, nil, nil)
			wantCalls := 11
			if interrupted {
				wantCalls = 5
			}
			if code := h.run(ctx); code != 0 || !h.finalized || len(h.calls) != wantCalls || finishes != 1 || h.journal.Workers[4].Outcome != "error" {
				t.Fatalf("hard keyboard cause changed session policy: %d %s", code, h.stderr.String())
			}
			if interrupted && (h.journal.Workers[4].State != "interrupted" || h.journal.Workers[5].State != "not_started") {
				t.Fatal("cancellation permitted later workers")
			}
			report, err := os.ReadFile(filepath.Join(h.directory, "session.hhpcr"))
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := bundle.VerifySelfIssued(bytes.NewReader(report), int64(len(report)))
			if err != nil {
				t.Fatal(err)
			}
			var payload app.InventoryPayload
			if json.Unmarshal(artifact.InventoryJSON, &payload) != nil {
				t.Fatal("signed report invalid")
			}
			found := 0
			for _, row := range payload.TestResults {
				if row.ID == "input.keyboard.quick" {
					found++
					value, err := keyboardevidence.Decode(row.Evidence)
					if err != nil || row.Version != 2 || row.Status != app.TestError || !reflect.DeepEqual(value, expected) {
						t.Fatal("live signed evidence changed hard cause or prior pairs")
					}
				}
			}
			if found != 1 {
				t.Fatal("signed keyboard row omitted")
			}
		})
	}
}
