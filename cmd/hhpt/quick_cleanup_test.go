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

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

func TestQuickCleanupUncertaintyOverridesDiagnosticOutputFailure(t *testing.T) {
	for _, command := range []string{"cpu", "memory"} {
		for _, status := range []quick.Status{quick.StatusPass, quick.StatusFail, quick.StatusError} {
			for _, mode := range []string{"saved", "existing", "failed-writer"} {
				t.Run(command+"/"+string(status)+"/"+mode, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "diagnostic.json")
					arguments := []string{"-output", path}
					var stdout, stderr bytes.Buffer
					var destination io.Writer = &stdout
					if mode == "existing" {
						if err := os.WriteFile(path, []byte("retained evidence"), 0o600); err != nil {
							t.Fatal(err)
						}
					} else if mode == "failed-writer" {
						arguments, destination = nil, cleanupFailedWriter{}
					}
					var code int
					if command == "cpu" {
						result := completeCPUEvidence()
						result.Status, result.Safety.MonitorStatus, result.Safety.MonitorFailure, result.Safety.CleanupConfirmed = status, "cleanup_unconfirmed", "cleanup_unconfirmed", false
						code = runCPUWith(t.Context(), arguments, destination, &stderr, func(context.Context, quick.CPUOptions) quick.CPUResult { return result })
					} else {
						result := completeMemoryEvidence()
						result.Status, result.Safety.MonitorStatus, result.Safety.MonitorFailure, result.Safety.CleanupConfirmed = status, "cleanup_unconfirmed", "cleanup_unconfirmed", false
						if status == quick.StatusFail {
							setMemoryFixtureFailure(&result, 0)
						}
						code = runMemoryWith(t.Context(), arguments, destination, &stderr, func(context.Context, quick.MemoryOptions) quick.MemoryResult { return result })
					}
					if code != workerCleanupUncertainExitCode || !strings.Contains(stderr.String(), "stop the session") {
						t.Fatalf("cleanup uncertainty lost: code=%d stderr=%s", code, &stderr)
					}
					if mode == "failed-writer" {
						return
					}
					encoded, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if mode == "existing" {
						if string(encoded) != "retained evidence" {
							t.Fatal("existing evidence overwritten")
						}
						return
					}
					var retained struct {
						Status   quick.Status          `json:"status"`
						Safety   quick.SafetyEvidence  `json:"safety"`
						Mismatch *quick.MemoryMismatch `json:"mismatch"`
					}
					if json.Unmarshal(encoded, &retained) != nil || retained.Status != status || retained.Safety.CleanupConfirmed || retained.Safety.MonitorStatus != "cleanup_unconfirmed" || retained.Safety.MonitorFailure != "cleanup_unconfirmed" {
						t.Fatal("serialized diagnostic status or cleanup evidence changed")
					}
					if command == "memory" && status == quick.StatusFail && (retained.Mismatch == nil || retained.Mismatch.ActualByte != 1) {
						t.Fatal("memory mismatch evidence lost")
					}
				})
			}
		}
	}
}

func TestQuickCommandChecksBothCleanupClaimsIndependently(t *testing.T) {
	for _, mode := range []string{"flag", "state"} {
		for _, command := range []string{"cpu", "memory"} {
			t.Run(command+"/"+mode, func(t *testing.T) {
				safety := completeCPUEvidence().Safety
				if mode == "flag" {
					safety.CleanupConfirmed = false
				} else {
					safety.MonitorStatus = "cleanup_unconfirmed"
				}
				var code int
				if command == "cpu" {
					result := completeCPUEvidence()
					result.Safety = safety
					code = runCPUWith(t.Context(), nil, io.Discard, io.Discard, func(context.Context, quick.CPUOptions) quick.CPUResult { return result })
				} else {
					result := completeMemoryEvidence()
					result.Safety = safety
					code = runMemoryWith(t.Context(), nil, io.Discard, io.Discard, func(context.Context, quick.MemoryOptions) quick.MemoryResult { return result })
				}
				if code != 12 {
					t.Fatalf("contradictory cleanup claims returned %d", code)
				}
			})
		}
	}
}
