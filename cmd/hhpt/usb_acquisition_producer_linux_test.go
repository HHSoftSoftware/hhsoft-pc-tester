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
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/usbtest"
)

func TestUSBAcquisitionActualProducerRemainsReportable(t *testing.T) {
	for _, mode := range []string{"malformed optional", "absent optional", "unknown authorization", "optional overflow", "mandatory absence", "empty discovery", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			registry := filepath.Join(root, "sys/bus/usb/devices")
			if err := os.MkdirAll(registry, 0700); err != nil {
				t.Fatal(err)
			}
			if mode != "empty discovery" {
				base := filepath.Join(registry, "usb1")
				if err := os.Mkdir(base, 0700); err != nil {
					t.Fatal(err)
				}
				values := map[string]string{"idVendor": "1234\n", "busnum": "1\n", "devnum": "1\n", "authorized": "1\n", "dev": "189:0\n", "bDeviceClass": "09\n", "maxchild": "4\n", "speed": "480\n"}
				switch mode {
				case "malformed optional":
					values["bDeviceClass"], values["speed"], values["maxchild"] = "not-a-class", "not-a-speed", "not-a-count"
				case "absent optional":
					delete(values, "bDeviceClass")
					delete(values, "speed")
					delete(values, "maxchild")
				case "unknown authorization":
					delete(values, "authorized")
				case "optional overflow":
					values["speed"] = strings.Repeat("1", 257)
				case "mandatory absence":
					delete(values, "busnum")
				}
				for name, value := range values {
					if err := os.WriteFile(filepath.Join(base, name), []byte(value), 0600); err != nil {
						t.Fatal(err)
					}
				}
				// A harmless regular fixture cannot receive character-node readiness.
				node := filepath.Join(root, "dev/bus/usb/001/001")
				if err := os.MkdirAll(filepath.Dir(node), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(node, []byte("owned fixture"), 0600); err != nil {
					t.Fatal(err)
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
				t.Fatal("producer diagnostic did not complete output")
			}
			var evidence usbtest.Result
			if err := json.Unmarshal(stdout.Bytes(), &evidence); err != nil {
				t.Fatal(err)
			}
			want := usbtest.StatusInconclusive
			if mode == "optional overflow" || mode == "mandatory absence" {
				want = usbtest.StatusError
			}
			if mode == "cancelled" {
				want = usbtest.StatusAborted
			}
			if evidence.Status != want || evidence.ReadableDevices != 0 || evidence.USBTransfersGenerated {
				t.Fatal("actual acquisition failure gained readiness")
			}
			if mode == "malformed optional" || mode == "absent optional" {
				if len(evidence.Devices) != 1 || evidence.Devices[0].Class != "" || evidence.Devices[0].LinkSpeedMbps != "" || evidence.Devices[0].DownstreamPorts != 0 {
					t.Fatal("unknown optional fields were fabricated")
				}
			}
			outcome, _, err := sessionWorkerOutcomeEvidence(stdout.Bytes(), nil, usbtest.Schema, code, mode == "cancelled")
			if err != nil || outcome != string(want) {
				t.Fatal("session lost the acquisition diagnostic")
			}
			arguments, snapshot, output := prepareUSBFinalization(t, evidence, outcome)
			if code := runFinalizeWith(t.Context(), arguments, &bytes.Buffer{}, &stderr, func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x37}, 64)), time.Now); code != 0 {
				t.Fatal("finalizer rejected reportable acquisition evidence")
			}
			raw, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw))); err != nil {
				t.Fatal("completed diagnostic report did not verify")
			}
			arguments, snapshot, _ = prepareUSBFinalization(t, evidence, "pass")
			collected := false
			if code := runFinalizeWith(t.Context(), arguments, &bytes.Buffer{}, &bytes.Buffer{}, func(context.Context) (inventory.Snapshot, error) { collected = true; return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x37}, 64)), time.Now); code != 1 || collected {
				t.Fatal("diagnostic was promoted to a signed pass")
			}
		})
	}
}
