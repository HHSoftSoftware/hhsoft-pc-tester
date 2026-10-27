package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

func TestSessionRejectsExistingQuickSafetyBeforeLaterPhases(t *testing.T) {
	cases := []struct {
		name      string
		change    func([]byte) []byte
		directory bool
		wantCode  int
	}{
		{"unjoined", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"cleanup_confirmed":true`), []byte(`"cleanup_confirmed":false`), 1)
		}, false, workerCleanupUncertainExitCode},
		{"uncertain-status", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"monitor_status":"completed"`), []byte(`"monitor_status":"cleanup_unconfirmed"`), 1)
		}, false, workerCleanupUncertainExitCode},
		{"truncated", func(b []byte) []byte { return b[:len(b)-1] }, false, 1},
		{"missing-cleanup", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"cleanup_confirmed":true,`), nil, 1)
		}, false, 1},
		{"contradictory-cleanup", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"cleanup_confirmed":true`), []byte(`"cleanup_confirmed":false,"cleanup_confirmed":true`), 1)
		}, false, 1},
		{"old-schema", historicalQuickSchemaFixture, false, 1},
		{"empty", func([]byte) []byte { return nil }, false, 1},
		{"nonregular", nil, true, 1},
	}
	for _, id := range []string{"cpu.quick", "memory.quick"} {
		for _, item := range cases {
			for _, interrupted := range []bool{false, true} {
				ending := "ordinary-return"
				if interrupted {
					ending = "cancel-race"
				}
				t.Run(id+"/"+item.name+"/"+ending, func(t *testing.T) {
					h := newSessionHarness(t, "x86_64", nil)
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					originalWorker := h.dependencies.RunWorker
					var evidencePath string
					var fixture []byte
					safetyAtReturn := 0
					h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, errOut io.Writer) int {
						if w.ID != id {
							return originalWorker(ctx, w, in, out, errOut)
						}
						h.calls = append(h.calls, w.ID)
						evidencePath = w.OutputPath
						if item.directory {
							if err := os.Mkdir(evidencePath, 0700); err != nil {
								t.Fatal("could not create owned nonregular evidence fixture")
							}
						} else {
							original := encodeEvidenceFixture(t, sessionCompleteEvidence(id))
							fixture = item.change(original)
							if bytes.Equal(fixture, original) {
								t.Fatal("fixture mutation did not exercise the intended failure")
							}
							if err := os.WriteFile(evidencePath, fixture, 0600); err != nil {
								t.Fatal("could not write owned malformed evidence fixture")
							}
						}
						safetyAtReturn = h.safetyCalls
						if interrupted {
							cancel()
						}
						// Evidence remains authoritative even when an adapter returns zero.
						return 0
					}
					h.dependencies.Finalize = func(context.Context, sessionFinalization, io.Writer, io.Writer) sessionFinalizationResult {
						h.finalized = true
						return sessionFinalizationResult{}
					}
					if code := h.run(ctx); code != item.wantCode {
						t.Fatalf("code=%d want=%d stderr=%s", code, item.wantCode, &h.stderr)
					}
					wantCalls := []string{"cpu.quick"}
					if id == "memory.quick" {
						wantCalls = append(wantCalls, id)
					}
					if evidencePath == "" || !reflect.DeepEqual(h.calls, wantCalls) || safetyAtReturn != len(wantCalls)+1 {
						t.Fatalf("intended fault was not reached or later worker ran: calls=%v safety=%d", h.calls, safetyAtReturn)
					}
					if h.safetyCalls != safetyAtReturn || h.finalized {
						t.Fatal("invalid quick safety reached later safety observation or finalization")
					}
					if item.wantCode == workerCleanupUncertainExitCode && !strings.Contains(h.stderr.String(), "reason=cleanup_unconfirmed") {
						t.Fatal("uncertain cleanup lost its terminal diagnostic")
					}
					for _, filename := range []string{"session-safety-final.json", "session-journal.json", "session-ready.txt", "session.hhpcr"} {
						if _, err := os.Lstat(filepath.Join(h.directory, filename)); !os.IsNotExist(err) {
							t.Fatalf("terminal quick safety created later artifact %s", filename)
						}
					}
					if !item.directory {
						retained, err := os.ReadFile(evidencePath)
						if err != nil || !bytes.Equal(retained, fixture) {
							t.Fatal("terminal quick evidence was discarded or changed")
						}
					}
					if _, err := os.Stat(filepath.Join(h.directory, "session-safety-monitor.json")); err != nil {
						t.Fatal("early return omitted owned safety monitor cleanup evidence")
					}
				})
			}
		}
	}
}

func TestSessionQuickAbsentEvidenceRemainsReportable(t *testing.T) {
	for _, id := range []string{"cpu.quick", "memory.quick"} {
		for _, interrupted := range []bool{false, true} {
			ending := "error"
			if interrupted {
				ending = "aborted"
			}
			t.Run(id+"/"+ending, func(t *testing.T) {
				h := newSessionHarness(t, "x86_64", nil)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				originalWorker := h.dependencies.RunWorker
				h.dependencies.RunWorker = func(ctx context.Context, w sessionWorker, in io.Reader, out, errOut io.Writer) int {
					if w.ID != id {
						return originalWorker(ctx, w, in, out, errOut)
					}
					h.calls = append(h.calls, w.ID)
					if interrupted {
						cancel()
					}
					return 1
				}
				if code := h.run(ctx); code != 0 || !h.finalized {
					t.Fatalf("missing evidence note was rejected: code=%d stderr=%s", code, &h.stderr)
				}
				index := 0
				if id == "memory.quick" {
					index = 1
				}
				record := h.journal.Workers[index]
				if record.ID != id || record.Outcome != ending || len(h.calls) != index+1 {
					t.Fatal("missing evidence became a pass or allowed later work")
				}
				if interrupted && record.State != "interrupted" || !interrupted && record.State != "completed" {
					t.Fatal("missing evidence lost its execution state")
				}
			})
		}
	}
}

func TestSessionQuickCompletedFailuresRemainFailures(t *testing.T) {
	for _, id := range []string{"cpu.quick", "memory.quick"} {
		t.Run(id, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "quick.json")
			writeSessionFixture(t, path, quickSafetyFixture(id, quick.StatusFail, completeCPUEvidence().Safety))
			schema := quick.CPUSchema
			if id == "memory.quick" {
				schema = quick.MemorySchema
			}
			encoded, readErr := readOptionalEvidence(path)
			status, state, err := sessionWorkerOutcomeEvidence(encoded, readErr, schema, 1, true)
			if err != nil || status != "fail" || state != "completed" {
				t.Fatalf("completed failure changed after interruption: status=%s state=%s err=%v", status, state, err)
			}
		})
	}
}
