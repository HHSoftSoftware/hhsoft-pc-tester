package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
)

type cleanupFailedWriter struct{}

func (cleanupFailedWriter) Write([]byte) (int, error) {
	return 0, errors.New("injected output failure")
}

func TestCleanupUncertaintyExitSurvivesDiagnosticOutputFailures(t *testing.T) {
	for _, command := range []string{"graphics", "display"} {
		for _, outputMode := range []string{"saved", "existing", "failed-writer"} {
			t.Run(command+"/"+outputMode, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "diagnostic.json")
				arguments := []string{"-output", path}
				var stdout, stderr bytes.Buffer
				var destination io.Writer = &stdout
				if outputMode == "existing" {
					if err := os.WriteFile(path, []byte("retain existing evidence"), 0o600); err != nil {
						t.Fatal(err)
					}
				} else if outputMode == "failed-writer" {
					arguments = nil
					destination = cleanupFailedWriter{}
				}
				input := strings.NewReader("p\n")
				var code int
				if command == "graphics" {
					probe := func(context.Context, string, int, graphics.Runner) graphics.Result {
						return graphics.Result{Schema: graphics.Schema, Status: graphics.StatusInconclusive, CleanupUncertain: true,
							Renderer: graphics.RendererResult{Status: graphics.StatusInconclusive, Detail: "cleanup unconfirmed"},
							Pattern:  graphics.PatternResult{Status: graphics.StatusAborted}}
					}
					code = runGraphicsWith(t.Context(), arguments, destination, &stderr, probe)
				} else {
					probe := func(context.Context, string, time.Duration, graphics.InteractiveRunner) graphics.VisualResult {
						return graphics.VisualResult{Schema: graphics.VisualSchema, Status: graphics.StatusInconclusive, CleanupUncertain: true,
							Patterns: []graphics.DisplayPattern{{Card: "/dev/dri/card0", Status: graphics.StatusPass,
								Connectors: []graphics.DisplayMode{{Connector: "card0-DP-1"}}}}, Warnings: []string{"cleanup unconfirmed"}}
					}
					code = runDisplayWith(t.Context(), arguments, input, destination, &stderr, probe)
				}
				if code != 12 || !strings.Contains(stderr.String(), "stop the session") || input.Len() != 2 {
					t.Fatalf("cleanup failure was downgraded or consumed findings: code=%d input=%d stderr=%s", code, input.Len(), &stderr)
				}
				if strings.Contains(stdout.String(), "Pixels Geometry") {
					t.Fatal("unsafe run prompted for acceptance")
				}
				if outputMode != "failed-writer" {
					raw, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if outputMode == "existing" && string(raw) != "retain existing evidence" {
						t.Fatal("existing evidence was overwritten")
					}
					if outputMode == "saved" && (!bytes.Contains(raw, []byte("cleanup unconfirmed")) || bytes.Contains(raw, []byte("cleanup_uncertain"))) {
						t.Fatal("diagnostics were lost or live execution state changed historical evidence schema")
					}
				}
			})
		}
	}
}
