//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/audio"
)

func TestAudioAcquisitionRealProducerThroughSignedConsumers(t *testing.T) {
	// No device node is created or opened. Close-fault injection is deliberately
	// confined to internal/audio; these are real registry/attribute fixtures.
	for _, mode := range []string{"absent", "missing-node", "attribute-overflow", "attribute-symlink", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			want := audio.StatusInconclusive
			if mode != "absent" {
				if err := os.MkdirAll(filepath.Join(root, "sys/class/sound/card0"), 0o700); err != nil {
					t.Fatal(err)
				}
				attribute := filepath.Join(root, "sys/class/sound/controlC0/dev")
				if err := os.MkdirAll(filepath.Dir(attribute), 0o700); err != nil {
					t.Fatal(err)
				}
				value := "116:0\n"
				if mode == "attribute-overflow" {
					value = strings.Repeat("1", 65)
					want = audio.StatusError
				}
				if mode == "attribute-symlink" {
					if err := os.WriteFile(filepath.Join(filepath.Dir(attribute), "number"), []byte(value), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("number", attribute); err != nil {
						t.Fatal(err)
					}
					want = audio.StatusError
				} else if err := os.WriteFile(attribute, []byte(value), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancelled" {
				cancel()
				want = audio.StatusAborted
			}
			result := audio.Run(ctx, audio.Options{Root: root})
			if result.Status != want || result.MediaStreamsOpened {
				t.Fatalf("real producer %#v", result)
			}
			var stdout, stderr bytes.Buffer
			if code := writeQuickResult(result, "-", &stdout, &stderr, "audio"); code != 0 || stderr.Len() != 0 {
				t.Fatal("actual diagnostic output failed")
			}
			var got audio.Result
			if json.Unmarshal(stdout.Bytes(), &got) != nil || got.Status != want || got.Readiness != result.Readiness {
				t.Fatal("writer changed real producer")
			}
			outcome, _, err := sessionWorkerOutcomeEvidence(stdout.Bytes(), nil, audio.Schema, 0, mode == "cancelled")
			if err != nil || outcome != string(want) {
				t.Fatal("session decoder changed real producer")
			}
			if _, err := newValidatedQuickTestResult("audio.readiness.quick", 1, app.TestStatus(want), got); err != nil {
				t.Fatal(err)
			}
			assertAudioSignedDiagnostic(t, got)
		})
	}
}
