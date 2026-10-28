//go:build linux

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

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
)

func TestNetworkAcquisitionActualProducerRetainsConsumerSafety(t *testing.T) {
	for _, mode := range []string{"offline pass", "optional absent", "optional malformed", "missing driver", "optional overflow", "uevent overflow", "mandatory absent", "active", "empty registry", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			registry := filepath.Join(root, "sys/class/net")
			if err := os.MkdirAll(registry, 0o700); err != nil {
				t.Fatal(err)
			}
			want := networktest.StatusPass
			if mode != "empty registry" {
				values := map[string]string{"flags": "0x1002\n", "operstate": "down\n", "carrier": "0\n", "speed": "1000\n", "duplex": "full\n", "device/uevent": "DRIVER=e1000e\n"}
				switch mode {
				case "optional absent":
					delete(values, "carrier")
					delete(values, "speed")
					delete(values, "duplex")
					delete(values, "operstate")
				case "optional malformed":
					values["carrier"], values["speed"], values["duplex"] = "wrong", "wrong", "wrong"
				case "missing driver":
					values["device/uevent"] = "OTHER=value\n"
					want = networktest.StatusInconclusive
				case "optional overflow":
					values["speed"] = strings.Repeat("1", 257)
					want = networktest.StatusError
				case "uevent overflow":
					values["device/uevent"] = strings.Repeat("x", 4097)
					want = networktest.StatusError
				case "mandatory absent":
					delete(values, "flags")
					want = networktest.StatusError
				case "active":
					values["flags"] = "0x1003\n"
					want = networktest.StatusError
				case "cancelled":
					want = networktest.StatusAborted
				}
				for role, value := range values {
					path := filepath.Join(registry, "eth0", role)
					if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				want = networktest.StatusInconclusive
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			probe := func(ctx context.Context, _ networktest.Options) networktest.Result {
				return networktest.Run(ctx, networktest.Options{Root: root})
			}
			var stdout, stderr bytes.Buffer
			code := runNetworkWith(ctx, nil, &stdout, &stderr, probe)
			var evidence networktest.Result
			if code != 0 || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &evidence) != nil || evidence.Status != want || evidence.NetworkTrafficGenerated {
				t.Fatal("actual producer diagnostic changed across CLI")
			}
			if mode == "optional absent" || mode == "optional malformed" {
				v := evidence.Interfaces[0]
				if v.CarrierDetected || v.Carrier || v.SpeedMbps != 0 || v.Duplex != "" {
					t.Fatal("unknown optional values fabricated")
				}
			}
			outcome, _, err := sessionWorkerOutcomeEvidence(stdout.Bytes(), nil, networktest.Schema, code, mode == "cancelled")
			if err != nil || outcome != string(want) {
				t.Fatal("actual producer diagnostic lost in session")
			}
			if _, err := newValidatedQuickTestResult("network.readiness.quick", 1, app.TestStatus(want), evidence); err != nil {
				t.Fatal("reportable diagnostic rejected by final evidence validator")
			}
			if want != networktest.StatusPass {
				if _, err := newValidatedQuickTestResult("network.readiness.quick", 1, app.TestPass, evidence); err == nil {
					t.Fatal("diagnostic promoted to pass")
				}
			}
			if mode != "cancelled" {
				h := newSessionHarness(t, "x86_64", nil)
				original := h.dependencies.RunWorker
				seen, later := false, false
				h.dependencies.RunWorker = func(workerCtx context.Context, worker sessionWorker, input io.Reader, out, errOut io.Writer) int {
					if seen {
						later = true
					}
					if worker.ID != "network.readiness.quick" {
						return original(workerCtx, worker, input, out, errOut)
					}
					seen = true
					h.calls = append(h.calls, worker.ID)
					return runNetworkWith(workerCtx, []string{"-output", worker.OutputPath}, out, errOut, probe)
				}
				code = h.run(t.Context())
				if want == networktest.StatusError {
					if code != 1 || !seen || later || h.finalized {
						t.Fatal("actual acquisition error authorized later workers or signing")
					}
				} else if code != 0 || !seen || !h.finalized {
					t.Fatal("safe actual producer changed existing session disposition")
				}
			}
		})
	}
}
