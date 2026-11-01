package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func enableTouchscreenSessionFixture(t *testing.T, h *sessionHarness) {
	t.Helper()
	enableTouchpadSessionFixture(t, h)
	components := append([]inventory.Component(nil), h.snapshot.Components...)
	for _, component := range components {
		if component.ID == "input:touchscreen-fixture" {
			t.Fatal("touchscreen fixture added twice")
		}
	}
	components = append(components, touchscreenRouteComponent())
	h.snapshot = inventory.NewSnapshot(components)
	frozen, err := plan.BuildWithSelection(h.snapshot, h.frozen.Profile, h.frozen.Architecture,
		bytes.Repeat([]byte{0x31}, 32), "ephemeral-session", h.frozen.ExcludedTests)
	if err != nil {
		t.Fatal(err)
	}
	h.frozen, err = plan.Decode(touchscreenRouteEncode(t, frozen))
	if err != nil {
		t.Fatal("touchscreen session fixture failed current plan admission", err)
	}
	row := touchscreenRouteTest(t, h.frozen)
	if row.Version != 2 || !reflect.DeepEqual(row.ComponentIDs, []string{"input:touchscreen-fixture"}) {
		t.Fatal("touchscreen candidate does not have its exact plan binding")
	}
	writeSessionFixture(t, filepath.Join(h.directory, "plan.json"), h.frozen)
	run := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, in io.Reader, out, diagnostic io.Writer) int {
		if worker.ID != "input.touchscreen.quick" {
			return run(ctx, worker, in, out, diagnostic)
		}
		h.calls = append(h.calls, worker.ID)
		value := touchscreenRouteValue(t)
		value.DurationMilliseconds = 0
		writeSessionFixture(t, worker.OutputPath, value)
		return 0
	}
}

func activationSignedTouchscreen(t *testing.T, h *sessionHarness, want string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.directory, "session.hhpcr"))
	if err != nil {
		t.Fatal("current session did not publish its report")
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal("current session report did not verify", err)
	}
	var payload app.InventoryPayload
	if json.Unmarshal(artifact.InventoryJSON, &payload) != nil || len(payload.TestResults) != 23 {
		t.Fatal("current signed catalog is incomplete")
	}
	view, err := app.ValidatedTouchscreen(payload)
	if err != nil || view == nil || string(view.Status) != want || !view.CleanupConfirmed || len(view.Devices) != 1 {
		t.Fatal("signed touchscreen evidence lost its independently validated outcome", err)
	}
	found, integrity := 0, 0
	for _, row := range payload.TestResults {
		if row.ID == "input.touchscreen.quick" {
			found++
			if row.Version != 2 || row.PlannedStatus != app.PlannedScheduled || string(row.Status) != want {
				t.Fatal("signed touchscreen definition or disposition changed")
			}
		}
		if row.ID == "run.integrity.quick" {
			integrity++
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if err != nil || value.Session == nil || !reflect.DeepEqual(*value.Session, h.journal) {
				t.Fatal("signed journal differs from the completed live owner", err)
			}
		}
	}
	if found != 1 || integrity != 1 {
		t.Fatal("signed touchscreen or integrity row missing or duplicated")
	}
	for _, rendered := range [][]byte{artifact.RenderedReport, artifact.RenderedPDF} {
		if !bytes.Contains(bytes.ToLower(rendered), []byte("touchscreen")) {
			t.Fatal("signed view lost the touchscreen section")
		}
	}
	ready, err := os.ReadFile(filepath.Join(h.directory, "session-ready.txt"))
	if err != nil || !bytes.Contains(ready, []byte(" touchscreen="+want)) ||
		!strings.Contains(h.stdout.String(), "HHPT_TOUCHSCREEN status="+want) {
		t.Fatal("current progress/readiness markers lost the touchscreen outcome")
	}
	for _, private := range [][]byte{[]byte(h.directory), bytes.Repeat([]byte{0x31}, 32)} {
		if bytes.Contains(artifact.InventoryJSON, private) {
			t.Fatal("fixture path or key leaked into signed evidence")
		}
	}
}

func TestTouchscreenActivatedSessionSignsCurrentProfile(t *testing.T) {
	for _, row := range []struct {
		name    string
		profile plan.Profile
	}{{"quick", plan.ProfileQuick}, {"standard", plan.ProfileStandard}} {
		t.Run(row.name, func(t *testing.T) {
			profile := row.profile
			h := newSessionHarness(t, "x86_64", nil)
			wantSchema, wantWorkers, count, index := "hhsoft.pc-tester.quick-session/v14", touchscreenRouteTouchscreenQuickWorkers, 14, 7
			if profile == plan.ProfileStandard {
				h = newStandardHarness(t, commandStressPass())
				wantSchema, wantWorkers, count, index = "hhsoft.pc-tester.quick-session/v15", touchscreenRouteTouchscreenStandardWorkers, 15, 8
			} else {
				useRealSessionFinalizer(t, h, nil, nil)
			}
			enableTouchscreenSessionFixture(t, h)
			if touchscreenRouteTest(t, h.frozen).Status != plan.TestScheduled {
				t.Fatal("current prerequisite did not schedule touchscreen")
			}
			if code := h.run(t.Context()); code != 0 || !h.finalized {
				t.Fatalf("current profile did not finalize: %d %s", code, h.stderr.String())
			}
			if h.journal.Schema != wantSchema || len(h.journal.Workers) != count ||
				!reflect.DeepEqual(h.calls, strings.Fields(wantWorkers)) {
				t.Fatal("current profile did not execute the literal complete worker order")
			}
			row := h.journal.Workers[index]
			if row.ID != "input.touchscreen.quick" || row.State != "completed" || row.Outcome != "pass" || row.Timing == nil ||
				!h.inputFinished || !h.journal.Monitor.CleanupConfirmed || !h.journal.Thermal.Joined {
				t.Fatal("touchscreen completion escaped the owned session interval")
			}
			activationSignedTouchscreen(t, h, "pass")
		})
	}
}

func TestTouchscreenActivatedSessionRetainsIncompleteAndCancelledEvidence(t *testing.T) {
	for _, ending := range []string{"inconclusive", "cancelled"} {
		t.Run(ending, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			enableTouchscreenSessionFixture(t, h)
			useRealSessionFinalizer(t, h, nil, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			run := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, in io.Reader, out, diagnostic io.Writer) int {
				if worker.ID != "input.touchscreen.quick" {
					return run(ctx, worker, in, out, diagnostic)
				}
				h.calls = append(h.calls, worker.ID)
				value := touchscreenRouteValue(t)
				value.DurationMilliseconds = 0
				if ending == "cancelled" {
					cancel()
					value.MarkInterrupted(context.Canceled)
				} else {
					value.Status, value.StopReason, value.Detail = inputtest.StatusInconclusive, "observation_timeout", "observation_timeout"
				}
				writeSessionFixture(t, worker.OutputPath, value)
				return 0
			}
			if code := h.run(ctx); code != 0 || !h.finalized {
				t.Fatalf("joined evidence was not finalized: %d %s", code, h.stderr.String())
			}
			wantOutcome, wantState, wantReason, wantCalls := "inconclusive", "completed", "completed", 14
			if ending == "cancelled" {
				wantOutcome, wantState, wantReason, wantCalls = "aborted", "interrupted", "operator_cancelled", 8
			}
			row := h.journal.Workers[7]
			if row.ID != "input.touchscreen.quick" || row.Outcome != wantOutcome || row.State != wantState ||
				row.Timing == nil || h.journal.StopReason != wantReason || len(h.calls) != wantCalls {
				t.Fatal("touchscreen stop or incomplete proof was replaced")
			}
			if ending == "cancelled" {
				for _, later := range h.journal.Workers[8:] {
					if later.State != "not_started" || later.Outcome != "aborted" || later.Timing != nil {
						t.Fatal("cancelled session started later work")
					}
				}
			}
			activationSignedTouchscreen(t, h, wantOutcome)
		})
	}
}

func TestTouchscreenActivatedSessionRefusesUncertainPublication(t *testing.T) {
	for _, fault := range []string{"malformed", "cleanup_exit", "typed_cleanup"} {
		t.Run(fault, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			enableTouchscreenSessionFixture(t, h)
			run := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, in io.Reader, out, diagnostic io.Writer) int {
				if worker.ID != "input.touchscreen.quick" {
					return run(ctx, worker, in, out, diagnostic)
				}
				h.calls = append(h.calls, worker.ID)
				if fault == "typed_cleanup" {
					value := touchscreenRouteValue(t)
					value.DurationMilliseconds = 0
					value.Status, value.StopReason, value.Detail = inputtest.StatusError, "cleanup_unconfirmed", "cleanup_unconfirmed"
					value.CleanupConfirmed = false
					writeSessionFixture(t, worker.OutputPath, value)
					return 0
				}
				if os.WriteFile(worker.OutputPath, []byte("{"), 0600) != nil {
					t.Fatal("write malformed fixture")
				}
				if fault == "cleanup_exit" {
					return workerCleanupUncertainExitCode
				}
				return 0
			}
			want := 1
			if fault != "malformed" {
				want = workerCleanupUncertainExitCode
			}
			if code := h.run(t.Context()); code != want || h.finalized || !h.inputFinished || len(h.calls) != 8 {
				t.Fatal("uncertain touchscreen publication reached finalization or lost cleanup precedence")
			}
			for _, name := range []string{"session.hhpcr", "session-ready.txt"} {
				if _, err := os.Stat(filepath.Join(h.directory, name)); !os.IsNotExist(err) {
					t.Fatal("uncertain publication created accepted output")
				}
			}
		})
	}
}

func TestTouchscreenActivatedFinalizerRejectsChangedCandidateAuthority(t *testing.T) {
	for _, fault := range []string{"identity_changed", "candidate_removed"} {
		t.Run(fault, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			enableTouchscreenSessionFixture(t, h)
			entropy := &pointerUnexpectedEntropy{}
			useRealSessionFinalizer(t, h, func(context.Context) (inventory.Snapshot, error) {
				var components []inventory.Component
				for _, source := range h.snapshot.Components {
					component := source
					component.Attributes = append([]inventory.Attribute(nil), source.Attributes...)
					if component.ID == "input:touchscreen-fixture" {
						if fault == "candidate_removed" {
							continue
						}
						touchscreenRouteSetAttribute(&component, "vendor_id", "4321")
					}
					components = append(components, component)
				}
				return inventory.NewSnapshot(components), nil
			}, entropy)
			if code := h.run(t.Context()); code == 0 || h.finalized || entropy.calls != 0 || !h.inputFinished {
				t.Fatal("changed candidate authority reached signing")
			}
			if !reflect.DeepEqual(h.calls, strings.Fields(touchscreenRouteTouchscreenQuickWorkers)) {
				t.Fatal("negative never reached the completed current worker boundary")
			}
			if _, err := os.Stat(filepath.Join(h.directory, "session.hhpcr")); !os.IsNotExist(err) {
				t.Fatal("changed candidate published a report")
			}
		})
	}
}

func TestTouchscreenActivatedSessionRejectsRehashedStaleExecutionPlan(t *testing.T) {
	for _, fault := range []string{"version1", "unsupported"} {
		t.Run(fault, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			enableTouchscreenSessionFixture(t, h)
			touchscreenRouteChangeTest(&h.frozen, func(row *plan.Test) {
				if fault == "version1" {
					row.Version = 1
				} else {
					row.Status = plan.TestUnsupported
				}
			})
			h.frozen.ID = ""
			raw := touchscreenRouteEncode(t, h.frozen)
			h.frozen.ID = fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
			raw = touchscreenRouteEncode(t, h.frozen)
			if _, err := plan.Decode(raw); err == nil {
				t.Fatal("stale rehashed plan prerequisite was accepted")
			}
			if os.WriteFile(filepath.Join(h.directory, "plan.json"), raw, 0600) != nil {
				t.Fatal("write stale plan fixture")
			}
			starts := 0
			h.dependencies.ObserveSafety = func(context.Context) safemode.Result { starts++; return journalSafeObservation() }
			if code := h.run(t.Context()); code == 0 || starts != 0 || len(h.calls) != 0 || h.finalized {
				t.Fatal("stale execution plan reached session observers or workers")
			}
		})
	}
}
