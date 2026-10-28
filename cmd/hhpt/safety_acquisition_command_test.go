package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

// The identical literal is asserted from the real private acquisition owner in
// safemode's producer tests. These are consumer composition tests, not a second
// kernel collector or real machine safety observation.
func incompleteSafetyAcquisitionLiteral(t *testing.T) safemode.Result {
	t.Helper()
	const literal = `{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"error","storage_observed":false,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0,"storage_detail":"block mount safety could not be established"}`
	var result safemode.Result
	if err := json.Unmarshal([]byte(literal), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSafetyAcquisitionCommandRetainsUncertaintyWithoutSummary(t *testing.T) {
	for _, mode := range []string{"uncertain", "unsafe", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			value := incompleteSafetyAcquisitionLiteral(t)
			if mode == "unsafe" {
				value.Status = "fail"
				value.ActiveNonLoopInterfaces = 1
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			path := filepath.Join(t.TempDir(), "safety.json")
			var stdout, stderr bytes.Buffer
			calls := 0
			code := runSafetyWith(ctx, []string{"-output", path, "-summary"}, &stdout, &stderr, func(context.Context) safemode.Result { calls++; return value })
			raw, err := os.ReadFile(path)
			var retained safemode.Result
			if err != nil || json.Unmarshal(raw, &retained) != nil || retained != value || calls != 1 || code != 1 || stdout.Len() != 0 || stderr.String() != "runtime safety: unsafe or incomplete kernel observations; active tests blocked\n" {
				t.Fatalf("incomplete safety command changed: code=%d stderr=%q", code, stderr.String())
			}
		})
	}
}

func TestSafetyAcquisitionSessionRejectsUncertainPhases(t *testing.T) {
	for _, phase := range []string{"preflight", "final", "pre-sign"} {
		t.Run(phase, func(t *testing.T) {
			h := newSessionHarness(t, "x86_64", nil)
			original := h.dependencies.ObserveSafety
			lateCalls := 0
			h.dependencies.ObserveSafety = func(ctx context.Context) safemode.Result {
				if phase == "preflight" {
					return incompleteSafetyAcquisitionLiteral(t)
				}
				if h.inputFinished {
					lateCalls++
					if phase == "final" || lateCalls == 2 {
						return incompleteSafetyAcquisitionLiteral(t)
					}
				}
				return original(ctx)
			}
			code := h.run(t.Context())
			if code != 1 || h.finalized || !strings.Contains(h.stderr.String(), "HHPT_SESSION_UNSAFE phase="+phase+"\n") {
				t.Fatalf("uncertain phase not rejected: %d %s", code, h.stderr.String())
			}
			if phase == "preflight" && len(h.calls) != 0 {
				t.Fatal("worker ran after incomplete preflight")
			}
			if phase == "pre-sign" && lateCalls != 2 {
				t.Fatal("pre-sign case did not reach intended live observation")
			}
			raw, err := os.ReadFile(filepath.Join(h.directory, "session-safety-"+phase+".json"))
			var retained safemode.Result
			if err != nil || json.Unmarshal(raw, &retained) != nil || retained != incompleteSafetyAcquisitionLiteral(t) {
				t.Fatal("uncertain phase diagnostics were not preserved")
			}
			for _, name := range []string{"session.hhpcr", "session-journal.json"} {
				if _, err := os.Stat(filepath.Join(h.directory, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("incomplete safety produced signable output")
				}
			}
		})
	}
}

func TestSafetyAcquisitionFinalizerIndependentlyRejectsPromotedJournal(t *testing.T) {
	for _, mode := range []string{"baseline-safe", "uncertain", "status-promoted", "observed-promoted", "unsafe-count"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newJournalFixture(t)
			args := fixture.args(t)
			if err := validateSessionJournalStructure(fixture.journal); err != nil {
				t.Fatalf("baseline journal invalid before negative: %v", err)
			}
			value := sessionevidence.CloneJournal(fixture.journal)
			evidence := incompleteSafetyAcquisitionLiteral(t)
			switch mode {
			case "baseline-safe":
				evidence = journalSafeObservation()
			case "status-promoted":
				evidence.Status = "pass"
			case "observed-promoted":
				evidence.Status = "pass"
				evidence.StorageObserved = true
			case "unsafe-count":
				evidence.Status = "fail"
				evidence.ActiveNonLoopInterfaces = 1
			}
			value.Safety[len(value.Safety)-1].Result = evidence
			var stdout, stderr bytes.Buffer
			forbidden := &journalForbiddenRandom{}
			var random io.Reader = forbidden
			gates, collections := 0, 0
			if mode == "baseline-safe" {
				random = bytes.NewReader(bytes.Repeat([]byte{0x45}, 64))
			}
			result := runFinalizeOwned(t.Context(), args, &stdout, &stderr, func(context.Context) (inventory.Snapshot, error) { collections++; return fixture.snapshot, nil }, random, time.Now, func(context.Context) (sessionJournal, error) { gates++; return value, nil })
			if mode == "baseline-safe" {
				if result.Code != 0 || gates != 1 || collections != 1 || stderr.Len() != 0 {
					t.Fatalf("safe control did not reach signing: %d %s", result.Code, stderr.String())
				}
				raw, err := os.ReadFile(fixture.reportPath)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw))); err != nil {
					t.Fatal("safe control signature invalid", err)
				}
				return
			}
			if result.Code != 1 || gates != 1 || collections != 1 || forbidden.calls != 0 || stdout.Len() != 0 || stderr.String() != "validate live session journal: session evidence is incomplete or contradictory\n" {
				t.Fatalf("wrong rejection boundary: code=%d gates=%d collect=%d random=%d %s", result.Code, gates, collections, forbidden.calls, stderr.String())
			}
			if _, err := os.Stat(fixture.reportPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("promoted safety journal was signed")
			}
		})
	}
}
