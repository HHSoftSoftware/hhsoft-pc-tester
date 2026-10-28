package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/usbtest"
)

func usbReadinessDiagnostic(status usbtest.Status) usbtest.Result {
	result := usbtest.Result{Schema: usbtest.Schema, Status: status, Controllers: 1, RootPorts: 4, AuthorizedDevices: 1, Devices: []usbtest.DeviceResult{{Path: "usb1", Kind: "root_hub", BusNumber: 1, DeviceNumber: 1, DownstreamPorts: 4, AuthorizedKnown: true, Authorized: true, NodePresent: true}}}
	if status == usbtest.StatusPass {
		result.ReadableDevices = 1
		result.Devices[0].Readable = true
	} else {
		result.Detail = "USB node observation did not complete"
		result.Devices[0].ReadinessIssue = result.Detail
	}
	return result
}

func TestUSBReadinessCLIAndSessionRetainDiagnosticStatuses(t *testing.T) {
	for _, status := range []usbtest.Status{usbtest.StatusPass, usbtest.StatusError, usbtest.StatusAborted, usbtest.StatusInconclusive} {
		t.Run(string(status), func(t *testing.T) {
			want := usbReadinessDiagnostic(status)
			var stdout, stderr bytes.Buffer
			calls := 0
			code := runUSBWith(t.Context(), nil, &stdout, &stderr, func(ctx context.Context, options usbtest.Options) usbtest.Result {
				calls++
				if ctx.Err() != nil || options.Root != "/" {
					t.Fatal("command changed production options")
				}
				return want
			})
			if code != 0 || calls != 1 || stderr.Len() != 0 {
				t.Fatal("diagnostic serialization did not complete")
			}
			var got usbtest.Result
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Status != status || got.ReadableDevices != want.ReadableDevices || got.USBTransfersGenerated {
				t.Fatal("command promoted diagnostic evidence")
			}
			for _, interrupted := range []bool{false, true} {
				outcome, state, err := sessionWorkerOutcomeEvidence(stdout.Bytes(), nil, usbtest.Schema, code, interrupted)
				wantState := "completed"
				if interrupted && (status == usbtest.StatusAborted || status == usbtest.StatusError) {
					wantState = "interrupted"
				}
				if err != nil || outcome != string(status) || state != wantState {
					t.Fatal("session rewrote retained USB status")
				}
			}
			if status != usbtest.StatusPass {
				got.Status = usbtest.StatusPass
				if _, err := newValidatedQuickTestResult("usb.readiness.quick", 1, app.TestPass, got); err == nil {
					t.Fatal("diagnostic counters were promoted to a pass")
				}
			}
		})
	}
}

func prepareUSBFinalization(t *testing.T, evidence usbtest.Result, outcome string) ([]string, inventory.Snapshot, string) {
	t.Helper()
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x26}, 32)
	keyPath := writeTestKey(t, directory, "fingerprint.key", key)
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentUSB, "usb:usb1", nil)})
	planPath := writeFinalizeTestPlan(t, directory, snapshot, key)
	output := filepath.Join(directory, "usb.hhpcr")
	arguments := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "usb-fixture", "-output", output}
	for _, definition := range quickResultDefinitions {
		status := "not_present"
		if definition.id == "usb.readiness.quick" {
			status = outcome
			writeSessionFixture(t, filepath.Join(directory, definition.filename), evidence)
		}
		arguments = append(arguments, "-outcome", definition.id+"="+status)
	}
	return arguments, snapshot, output
}

func TestUSBReadinessFinalizerPreservesNonPassWithoutPromotion(t *testing.T) {
	for _, status := range []usbtest.Status{usbtest.StatusPass, usbtest.StatusError, usbtest.StatusAborted, usbtest.StatusInconclusive} {
		t.Run(string(status), func(t *testing.T) {
			evidence := usbReadinessDiagnostic(status)
			arguments, snapshot, path := prepareUSBFinalization(t, evidence, string(status))
			var stdout, stderr bytes.Buffer
			if code := runFinalizeWith(t.Context(), arguments, &stdout, &stderr, func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x37}, 64)), time.Now); code != 0 {
				t.Fatalf("reportable USB outcome rejected: %s", stderr.String())
			}
			encoded, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			verified, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
			if err != nil {
				t.Fatal(err)
			}
			var payload app.InventoryPayload
			if err := json.Unmarshal(verified.InventoryJSON, &payload); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range payload.TestResults {
				if row.ID == "usb.readiness.quick" {
					found = true
					if row.Status != app.TestStatus(status) || !bytes.Equal(row.Evidence, encodeEvidenceFixture(t, evidence)) {
						t.Fatal("signed report replaced USB evidence")
					}
				}
			}
			if !found {
				t.Fatal("USB diagnostic omitted")
			}
			if status != usbtest.StatusPass {
				arguments, snapshot, path = prepareUSBFinalization(t, evidence, "pass")
				collected := false
				var rejected bytes.Buffer
				if code := runFinalizeWith(t.Context(), arguments, &bytes.Buffer{}, &rejected, func(context.Context) (inventory.Snapshot, error) { collected = true; return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x37}, 64)), time.Now); code != 1 || collected {
					t.Fatal("claimed pass reached collection/signing")
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("promoted diagnostic created report")
				}
			}
		})
	}
}

func TestUSBReadinessSessionReportsErrorWithoutInventingGlobalCleanupStop(t *testing.T) {
	h := newSessionHarness(t, "x86_64", nil)
	original := h.dependencies.RunWorker
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, stdout, stderr io.Writer) int {
		if worker.ID != "usb.readiness.quick" {
			return original(ctx, worker, input, stdout, stderr)
		}
		h.calls = append(h.calls, worker.ID)
		return runUSBWith(ctx, []string{"-output", worker.OutputPath}, stdout, stderr, func(context.Context, usbtest.Options) usbtest.Result {
			return usbReadinessDiagnostic(usbtest.StatusError)
		})
	}
	if code := runSessionWith(t.Context(), h.arguments, bytes.NewReader(nil), &h.stdout, &h.stderr, h.dependencies); code != 0 || !h.finalized {
		t.Fatalf("reportable USB error changed whole-session policy: %s", h.stderr.String())
	}
	seenUSB, seenLater := false, false
	for _, id := range h.calls {
		if id == "usb.readiness.quick" {
			seenUSB = true
		} else if seenUSB {
			seenLater = true
		}
	}
	if !seenUSB || !seenLater {
		t.Fatal("fixture did not exercise later unrelated workers")
	}
	found := false
	for _, row := range h.journal.Workers {
		if row.ID == "usb.readiness.quick" {
			found = true
			if row.Outcome != "error" {
				t.Fatal("session lost USB error")
			}
		}
	}
	if !found {
		t.Fatal("session omitted USB worker")
	}
}

func TestUSBReadinessConsumerRejectsLiteralFalsePassClaims(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*usbtest.Result)
	}{
		{"wrong readable count", func(r *usbtest.Result) { r.ReadableDevices = 0 }},
		{"unknown authorization", func(r *usbtest.Result) { r.Devices[0].AuthorizedKnown = false }},
		{"retained readiness issue", func(r *usbtest.Result) { r.Devices[0].ReadinessIssue = "node closure could not be confirmed" }},
		{"missing root", func(r *usbtest.Result) { r.Controllers = 0 }},
		{"application transfer", func(r *usbtest.Result) { r.USBTransfersGenerated = true }},
		{"duplicate path and address", func(r *usbtest.Result) {
			r.Devices = append(r.Devices, r.Devices[0])
			r.Controllers = 2
			r.RootPorts = 8
			r.ReadableDevices = 2
			r.AuthorizedDevices = 2
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := usbReadinessDiagnostic(usbtest.StatusPass)
			test.change(&result)
			if _, err := testEvidenceStatus(encodeEvidenceFixture(t, result), usbtest.Schema); err == nil {
				t.Fatal("false pass accepted at independent evidence boundary")
			}
			if _, err := newValidatedQuickTestResult("usb.readiness.quick", 1, app.TestPass, result); err == nil {
				t.Fatal("false pass reached report assembly")
			}
		})
	}
}
