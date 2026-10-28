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

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/pointerevidence"
)

func TestPointerCommandKeepsHardCauseAndHonestRowsAfterCancellation(t *testing.T) {
	for _, detail := range []string{"pointer descriptor cleanup failed", "pointer event stream disconnected or became unreadable", "pointer descriptor acquisition failed"} {
		t.Run(detail, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			expected := completePointerEvidence()
			expected.Status, expected.StopReason, expected.Detail = inputtest.StatusError, "error", detail
			var stdout, stderr bytes.Buffer
			collect := func(context.Context, inputtest.PointerOptions) inputtest.PointerResult { cancel(); return expected }
			if code := runPointerWith(ctx, nil, &stdout, &stderr, collect); code != 0 {
				t.Fatalf("error evidence could not be written: %d", code)
			}
			actual, err := pointerevidence.Decode(stdout.Bytes())
			if err != nil || !reflect.DeepEqual(actual, expected) {
				t.Fatalf("CLI masked hard cause or changed rows: %#v %v", actual, err)
			}
			if outcome, state, err := sessionWorkerOutcomeEvidence(stdout.Bytes(), nil, inputtest.PointerSchema, 0, true); err != nil || outcome != "error" || state != "interrupted" {
				t.Fatal("session replaced valid interrupted hard error")
			}
			if sessionSafetyWorker("input.pointer.quick") {
				t.Fatal("pointer unexpectedly acquired network/storage safety-stop policy")
			}
			assertPointerAcquisitionFinalizer(t, stdout.Bytes(), expected.Status)
		})
	}
}

// Consumer composition deliberately uses a literal hard-error result. Linux
// tests separately feed the real fixture-root producer into this same helper.
func assertPointerAcquisitionFinalizer(t *testing.T, raw []byte, status inputtest.Status) {
	t.Helper()
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x45}, 32)
	keyPath := writeTestKey(t, directory, "fingerprint.key", key)
	expected, err := pointerevidence.Decode(raw)
	if err != nil || expected.Status != status {
		t.Fatal("caller evidence did not decode at its claimed status")
	}
	snapshot := pointerInventoryFixture(false, false)
	frozen, err := plan.BuildWithSelection(snapshot, plan.ProfileQuick, "x86_64", key, "ephemeral-session", []string{"input.readiness.quick"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(directory, "plan.json")
	evidencePath := filepath.Join(directory, "pointer.json")
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
		t.Fatal("written plan did not decode")
	}
	scheduled := 0
	for _, row := range decoded.Tests {
		if row.ID == "input.pointer.quick" {
			if row.Status != plan.TestScheduled || row.Version != 2 {
				t.Fatal("pointer was not scheduled at actual definition2")
			}
			scheduled++
		}
	}
	if scheduled != 1 {
		t.Fatal("fixture cannot reach pointer evidence admission")
	}
	for _, promote := range []bool{false, true} {
		report := filepath.Join(directory, map[bool]string{false: "honest.hhpcr", true: "promoted.hhpcr"}[promote])
		args := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "pointer-acquisition", "-output", report}
		for _, definition := range quickResultDefinitions {
			for _, row := range decoded.Tests {
				if definition.id == row.ID {
					outcome := string(row.Status)
					if row.ID == "input.pointer.quick" {
						outcome = string(status)
						if promote {
							outcome = "pass"
						}
					}
					args = append(args, "-outcome", row.ID+"="+outcome)
				}
			}
		}
		collected := 0
		collect := func(context.Context) (inventory.Snapshot, error) { collected++; return snapshot, nil }
		var out, stderr bytes.Buffer
		if promote {
			entropy := &pointerUnexpectedEntropy{}
			if code := runFinalizeWith(t.Context(), args, &out, &stderr, collect, entropy, time.Now); code != 1 || stderr.String() != "validate evidence input.pointer.quick: schema or status does not match supervisor outcome\n" || collected != 0 || entropy.calls != 0 || out.Len() != 0 {
				t.Fatalf("promotion failed at wrong boundary: %d %q", code, stderr.String())
			}
			if _, err := os.Stat(report); !os.IsNotExist(err) {
				t.Fatal("promoted report exists")
			}
		} else {
			if code := runFinalizeWith(t.Context(), args, &out, &stderr, collect, bytes.NewReader(bytes.Repeat([]byte{0x47}, 32)), time.Now); code != 0 || collected != 1 || stderr.Len() != 0 {
				t.Fatalf("honest evidence rejected: %d %q", code, stderr.String())
			}
			reportBytes, err := os.ReadFile(report)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := bundle.VerifySelfIssued(bytes.NewReader(reportBytes), int64(len(reportBytes)))
			if err != nil {
				t.Fatal(err)
			}
			var payload app.InventoryPayload
			if json.Unmarshal(artifact.InventoryJSON, &payload) != nil {
				t.Fatal("report did not decode")
			}
			found := 0
			for _, row := range payload.TestResults {
				if row.ID == "input.pointer.quick" {
					found++
					signed, err := pointerevidence.Decode(row.Evidence)
					if err != nil || row.Status != app.TestStatus(status) || !reflect.DeepEqual(signed, expected) {
						t.Fatal("signed evidence promoted or changed")
					}
				}
			}
			if found != 1 {
				t.Fatal("signed pointer row missing")
			}
		}
	}
	if retained, err := os.ReadFile(evidencePath); err != nil || !bytes.Equal(retained, raw) {
		t.Fatal("finalizer changed raw pointer evidence")
	}
}

func TestPointerCommandPrecancelSkipsActualProducer(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out bytes.Buffer
	calls := 0
	collect := func(context.Context, inputtest.PointerOptions) inputtest.PointerResult {
		calls++
		return inputtest.PointerResult{}
	}
	if runPointerWith(ctx, nil, &out, io.Discard, collect) != 0 || calls != 0 {
		t.Fatal("pre-cancel touched collector")
	}
	result, err := pointerevidence.Decode(out.Bytes())
	if err != nil || result.Status != inputtest.StatusAborted || strings.Contains(string(out.Bytes()), "private") {
		t.Fatal("precancel evidence invalid")
	}
}
