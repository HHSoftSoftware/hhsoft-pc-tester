//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/usbtest"
)

func TestUSBReadinessActualProducerDiagnosticsReachReportConsumers(t *testing.T) {
	for _, mode := range []string{"empty", "missing identity", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			registry := filepath.Join(root, "sys/bus/usb/devices")
			if err := os.MkdirAll(registry, 0700); err != nil {
				t.Fatal(err)
			}
			if mode == "missing identity" {
				if err := os.Mkdir(filepath.Join(registry, "usb1"), 0700); err != nil {
					t.Fatal(err)
				}
				for name, value := range map[string]string{"idVendor": "1234\n", "busnum": "1\n", "devnum": "1\n", "bDeviceClass": "09\n", "authorized": "1\n", "maxchild": "4\n"} {
					if err := os.WriteFile(filepath.Join(registry, "usb1", name), []byte(value), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			var stdout, stderr bytes.Buffer
			code := runUSBWith(ctx, nil, &stdout, &stderr, func(ctx context.Context, _ usbtest.Options) usbtest.Result {
				return usbtest.Run(ctx, usbtest.Options{Root: root})
			})
			if code != 0 || stderr.Len() != 0 {
				t.Fatal("actual producer diagnostic was not serialized")
			}
			var result usbtest.Result
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			want := usbtest.StatusInconclusive
			if mode == "missing identity" {
				want = usbtest.StatusError
			}
			if mode == "cancelled" {
				want = usbtest.StatusAborted
			}
			if result.Status != want || result.ReadableDevices != 0 || result.USBTransfersGenerated {
				t.Fatal("actual failed acquisition promoted readiness")
			}
			outcome, _, err := sessionWorkerOutcomeEvidence(stdout.Bytes(), nil, usbtest.Schema, code, mode == "cancelled")
			if err != nil || outcome != string(want) {
				t.Fatal("actual producer status lost at session reader")
			}
			arguments, snapshot, path := prepareUSBFinalization(t, result, outcome)
			if code := runFinalizeWith(t.Context(), arguments, &bytes.Buffer{}, &stderr, func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x37}, 64)), time.Now); code != 0 {
				t.Fatalf("actual diagnostic cannot be finalized: %s", stderr.String())
			}
			encoded, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded))); err != nil {
				t.Fatal(err)
			}
		})
	}
}
