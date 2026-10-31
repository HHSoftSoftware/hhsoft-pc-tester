package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/audio"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func audioAcquisitionDiagnostic(status audio.Status) audio.Result {
	if status == audio.StatusInconclusive {
		return audio.Result{Schema: audio.Schema, Status: status, Readiness: audio.ReadinessNoDevice, Detail: "no ALSA sound cards were discovered"}
	}
	result := completeAudioEvidence()
	result.Status = status
	if status != audio.StatusPass {
		result.Readiness = audio.ReadinessUnknown
		result.Detail = "audio metadata closure could not be confirmed"
	}
	return result
}

func TestAudioAcquisitionWriterAndSessionKeepDiagnosticOutcomes(t *testing.T) {
	// Close-fault producer injection is in internal/audio. Here the literal
	// public result exercises the exact writer used by the real audio command.
	for _, status := range []audio.Status{audio.StatusPass, audio.StatusInconclusive, audio.StatusError, audio.StatusAborted} {
		t.Run(string(status), func(t *testing.T) {
			result := audioAcquisitionDiagnostic(status)
			var stdout, stderr bytes.Buffer
			code := writeQuickResult(result, "-", &stdout, &stderr, "audio")
			var got audio.Result
			if code != 0 || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &got) != nil || got.Status != status ||
				got.Readiness != result.Readiness || got.ReadyEndpoints != result.ReadyEndpoints || got.MediaStreamsOpened {
				t.Fatal("writer changed diagnostic evidence")
			}
			for _, interrupted := range []bool{false, true} {
				outcome, state, err := sessionWorkerOutcomeEvidence(stdout.Bytes(), nil, audio.Schema, code, interrupted)
				want := "completed"
				if interrupted && (status == audio.StatusError || status == audio.StatusAborted) {
					want = "interrupted"
				}
				if err != nil || outcome != string(status) || state != want {
					t.Fatal("session promoted or discarded audio status")
				}
			}
			if _, err := newValidatedQuickTestResult("audio.readiness.quick", 1, app.TestStatus(status), got); err != nil {
				t.Fatal(err)
			}
			if status != audio.StatusPass {
				if _, err := newValidatedQuickTestResult("audio.readiness.quick", 1, app.TestPass, got); err == nil {
					t.Fatal("caller pass overrode diagnostic outcome")
				}
				got.Status = audio.StatusPass
				if _, err := newValidatedQuickTestResult("audio.readiness.quick", 1, app.TestPass, got); err == nil {
					t.Fatal("unknown readiness promoted to pass")
				}
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout, stderr bytes.Buffer
	if code := runAudio(ctx, nil, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatal("cancelled real command did not emit diagnostic")
	}
	var got audio.Result
	if json.Unmarshal(stdout.Bytes(), &got) != nil || got.Status != audio.StatusAborted || got.Readiness != audio.ReadinessUnknown || got.MediaStreamsOpened {
		t.Fatal("real command changed pre-acquisition cancellation")
	}
}

func TestAudioAcquisitionErrorDoesNotInventGlobalCleanupStop(t *testing.T) {
	for _, status := range []audio.Status{audio.StatusError, audio.StatusAborted} {
		t.Run(string(status), func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			original := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, stdout, stderr io.Writer) int {
				if worker.ID != "audio.readiness.quick" {
					return original(ctx, worker, input, stdout, stderr)
				}
				h.calls = append(h.calls, worker.ID)
				return writeQuickResult(audioAcquisitionDiagnostic(status), worker.OutputPath, stdout, stderr, "audio")
			}
			if code := h.run(t.Context()); code != 0 || !h.finalized {
				t.Fatalf("reportable audio error changed session policy: %d %s", code, h.stderr.String())
			}
			seen, later := false, false
			for _, id := range h.calls {
				if id == "audio.readiness.quick" {
					seen = true
				} else if seen {
					later = true
				}
			}
			if !seen || later != (status == audio.StatusError) {
				t.Fatal("audio error/abort changed later-worker policy")
			}
			found := false
			for _, worker := range h.journal.Workers {
				if worker.ID == "audio.readiness.quick" {
					found = true
					if worker.Outcome != string(status) || worker.State != "completed" {
						t.Fatal("audio outcome lost")
					}
				}
			}
			if !found {
				t.Fatal("audio evidence absent")
			}
			wantStop := "completed"
			if status == audio.StatusAborted {
				wantStop = "worker_stop"
			}
			if h.journal.StopReason != wantStop {
				t.Fatal("wrong generic worker-stop disposition")
			}
		})
	}
}

func TestAudioAcquisitionDiagnosticOutputCompletion(t *testing.T) {
	fault := errors.New("injected output failure")
	for _, mode := range []string{"open", "write", "short-write", "close", "write-close"} {
		t.Run(mode, func(t *testing.T) {
			file := &outputFaultFile{limit: -1}
			if mode == "write" || mode == "write-close" {
				file.writeErr = fault
			}
			if mode == "short-write" {
				file.limit = 3
			}
			if mode == "close" || mode == "write-close" {
				file.closeErr = fault
			}
			destination := func(path string, stdout io.Writer, write func(io.Writer) error) error {
				return writeDestinationWith(path, stdout, write, func(name string, flags int, permissions os.FileMode) (io.WriteCloser, error) {
					if name != "owned-audio-output" || flags != os.O_WRONLY|os.O_CREATE|os.O_EXCL || permissions != 0600 {
						t.Fatal("output ownership contract changed")
					}
					if mode == "open" {
						return nil, fault
					}
					return file, nil
				})
			}
			var stdout, stderr bytes.Buffer
			if code := writeQuickResultDestination(audioAcquisitionDiagnostic(audio.StatusError), "owned-audio-output", &stdout, &stderr, "audio", destination); code != 1 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatal("incomplete diagnostic output reported success")
			}
			wantCloses := 1
			if mode == "open" {
				wantCloses = 0
			}
			if file.closes != wantCloses {
				t.Fatal("owned diagnostic output not closed once")
			}
		})
	}
}

func prepareAudioAcquisitionFinalization(t *testing.T, evidence audio.Result, outcome string) ([]string, inventory.Snapshot, string) {
	t.Helper()
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x63}, 32)
	keyPath := writeTestKey(t, directory, "fingerprint.key", key)
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentSound, "sound:card0", map[string]string{"interface": "card0"})})
	planPath := writeFinalizeTestPlan(t, directory, snapshot, key)
	raw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := plan.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, row := range frozen.Tests {
		if row.ID == "audio.readiness.quick" {
			rows++
			if row.Status != plan.TestScheduled || row.Version != 1 {
				t.Fatal("audio fixture is not scheduled")
			}
		}
	}
	if !frozen.Frozen || rows != 1 {
		t.Fatal("audio fixture lacks exactly one frozen scheduled row")
	}
	output := filepath.Join(directory, "audio.hhpcr")
	arguments := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "audio-fixture", "-output", output}
	for _, definition := range currentResultDefinitionsForTest(t, frozen) {
		status := "not_present"
		if definition.id == "inspection.exterior.quick" {
			status = "excluded"
		}
		if definition.id == "audio.readiness.quick" {
			status = outcome
			writeSessionFixture(t, filepath.Join(directory, definition.filename), evidence)
		}
		arguments = append(arguments, "-outcome", definition.id+"="+status)
	}
	return arguments, snapshot, output
}

func assertAudioSignedDiagnostic(t *testing.T, evidence audio.Result) {
	t.Helper()
	arguments, snapshot, output := prepareAudioAcquisitionFinalization(t, evidence, string(evidence.Status))
	var stdout, stderr bytes.Buffer
	collections := 0
	if code := runFinalizeTestWith(t, t.Context(), arguments, &stdout, &stderr, func(context.Context) (inventory.Snapshot, error) { collections++; return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x73}, 64)), time.Now); code != 0 || collections != 1 || stderr.Len() != 0 {
		t.Fatalf("audio finalization failed: %d %s", code, stderr.String())
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var payload app.InventoryPayload
	if json.Unmarshal(verified.InventoryJSON, &payload) != nil {
		t.Fatal("invalid signed payload")
	}
	found := false
	for _, row := range payload.TestResults {
		if row.ID == "audio.readiness.quick" {
			found = true
			if row.Status != app.TestStatus(evidence.Status) || !bytes.Equal(row.Evidence, encodeEvidenceFixture(t, evidence)) {
				t.Fatal("signed diagnostic changed")
			}
		}
	}
	if !found {
		t.Fatal("audio diagnostic omitted")
	}
	if evidence.Status != audio.StatusPass {
		arguments, snapshot, output = prepareAudioAcquisitionFinalization(t, evidence, "pass")
		collected := false
		var rejectedOut, rejectedErr bytes.Buffer
		code := runFinalizeTestWith(t, t.Context(), arguments, &rejectedOut, &rejectedErr, func(context.Context) (inventory.Snapshot, error) { collected = true; return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x73}, 64)), time.Now)
		if code != 1 || collected || rejectedOut.Len() != 0 || rejectedErr.String() != "validate evidence audio.readiness.quick: schema or status does not match supervisor outcome\n" {
			t.Fatalf("wrong rejection boundary: %d %v %q", code, collected, rejectedErr.String())
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("false pass created report")
		}
	}
}

func TestAudioAcquisitionManualFinalizerRetainsDiagnostics(t *testing.T) {
	for _, status := range []audio.Status{audio.StatusPass, audio.StatusError, audio.StatusAborted, audio.StatusInconclusive} {
		t.Run(string(status), func(t *testing.T) { assertAudioSignedDiagnostic(t, audioAcquisitionDiagnostic(status)) })
	}
}
