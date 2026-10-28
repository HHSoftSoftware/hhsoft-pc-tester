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

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

func TestInputAcquisitionOwnedProducerThroughSignedConsumers(t *testing.T) {
	// No device node is created. Nominal and close-fault ownership use private
	// injected handles in inputtest; these real fixtures are non-device paths.
	for _, kind := range []string{"empty", "absent", "regular-node", "identity-overflow", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			write := func(name, value string) {
				t.Helper()
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			registry := "N: Name=\"Fixture Keyboard\"\nH: Handlers=kbd event0\n"
			if kind == "empty" {
				registry = ""
			}
			write("proc/bus/input/devices", registry)
			identity := "13:64\n"
			if kind == "identity-overflow" {
				identity = strings.Repeat(" ", 129) + identity
			}
			write("sys/class/input/event0/dev", identity)
			if kind == "regular-node" {
				write("dev/input/event0", "owned non-device fixture")
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			var stdout, stderr bytes.Buffer
			calls := 0
			code := runInputWith(ctx, nil, &stdout, &stderr, func(ctx context.Context, options inputtest.Options) inputtest.Result {
				calls++
				if options.Root != "/" || options.Duration != 0 {
					t.Fatal("real command defaults changed")
				}
				options.Root = root
				return inputtest.Run(ctx, options)
			})
			var evidence inputtest.Result
			want := inputtest.StatusError
			if kind == "empty" || kind == "absent" {
				want = inputtest.StatusInconclusive
			}
			if kind == "cancelled" {
				want = inputtest.StatusAborted
			}
			if code != 0 || calls != 1 || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &evidence) != nil || evidence.Status != want {
				t.Fatal("real producer/command diagnostic changed")
			}
			for _, device := range evidence.Devices {
				if device.Readable || device.Events != 0 {
					t.Fatal("non-device fixture received readable/activity credit")
				}
			}
			outcome, _, err := sessionWorkerOutcomeEvidence(stdout.Bytes(), nil, inputtest.Schema, code, kind == "cancelled")
			if err != nil || outcome != string(want) {
				t.Fatal("session changed actual producer status")
			}
			assertInputSignedDiagnostic(t, evidence)
		})
	}
}
