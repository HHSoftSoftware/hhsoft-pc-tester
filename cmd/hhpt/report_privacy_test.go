package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func TestInputUniqueIdentifiersNeverReachSignedReportsOrViews(t *testing.T) {
	const identifier = "synthetic-input-unique-4a7e"
	const model = "FixturePanel"
	for _, provenance := range []string{"proc", "sysfs"} {
		t.Run(provenance, func(t *testing.T) {
			root := t.TempDir()
			write := func(name, contents string) {
				t.Helper()
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if provenance == "proc" {
				write("proc/bus/input/devices", "N: Name=\""+model+"\"\nU: Uniq="+identifier+"\nH: Handlers=event0\nB: ABS=3\nB: PROP=2\n")
			} else {
				write("sys/class/input/event0/device/name", model+"\n")
				write("sys/class/input/event0/device/uniq", identifier+"\n")
				write("sys/class/input/event0/device/capabilities/abs", "3\n")
				write("sys/class/input/event0/device/properties", "2\n")
			}
			collector := inventory.NewLinuxCollector(root)
			snapshot, err := collector.Collect(t.Context())
			if err != nil || len(snapshot.Components) != 1 {
				t.Fatalf("input fixture was not collected: %v, %+v", err, snapshot)
			}
			if value, ok := snapshot.Components[0].Attribute("unique_id"); !ok || value != identifier {
				t.Fatal("fixture did not exercise the raw input identifier")
			}
			key := bytes.Repeat([]byte{0x23}, 32)
			privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x45}, ed25519.SeedSize))
			publicKey := privateKey.Public().(ed25519.PublicKey)
			encoded, err := captureBundle(snapshot, nil, key, "fixture", privateKey, "fixture-signer", "fixture", time.Unix(1, 0))
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := bundle.Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{"fixture-signer": publicKey})
			if err != nil {
				t.Fatal(err)
			}
			assertInputReportPrivacy(t, artifact, identifier, model)

			// Exercise the actual boot finalizer as well as the provisioned signer.
			// Scheduled workers are explicitly unrun; this fixture tests privacy,
			// not hardware success or readiness.
			directory := t.TempDir()
			planPath := writeFinalizeTestPlan(t, directory, snapshot, key)
			planBytes, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			var frozen plan.Plan
			if err := json.Unmarshal(planBytes, &frozen); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(directory, "session.hhpcr")
			arguments := []string{"-fingerprint-key", writeTestKey(t, directory, "fingerprint.key", key),
				"-plan", planPath, "-evidence-dir", directory, "-session-id", "privacy-fixture", "-output", output}
			outcomes := make(map[string]string)
			for _, test := range frozen.Tests {
				status := string(test.Status)
				if test.Status == plan.TestScheduled {
					status = "aborted"
				}
				outcomes[test.ID] = status
			}
			for _, id := range []string{"audio.readiness.quick", "cpu.quick", "display.operator.quick", "graphics.quick",
				"input.readiness.quick", "memory.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick"} {
				if outcomes[id] == "" {
					t.Fatalf("quick workflow result absent from plan: %s", id)
				}
				arguments = append(arguments, "-outcome", id+"="+outcomes[id])
			}
			var stdout, stderr bytes.Buffer
			if code := runFinalizeWith(t.Context(), arguments, &stdout, &stderr, collector.Collect,
				bytes.NewReader(bytes.Repeat([]byte{0x31}, 64)), func() time.Time { return time.Unix(1, 0) }); code != 0 {
				t.Fatalf("finalize returned %d: %s", code, stderr.String())
			}
			encoded, err = os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err = bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
			if err != nil {
				t.Fatal(err)
			}
			assertInputReportPrivacy(t, artifact, identifier, model)
		})
	}
}

func assertInputReportPrivacy(t *testing.T, artifact bundle.Artifact, identifier, model string) {
	t.Helper()
	for name, content := range map[string][]byte{"inventory": artifact.InventoryJSON, "envelope": artifact.Envelope.Payload,
		"html": artifact.RenderedReport, "pdf": artifact.RenderedPDF} {
		if bytes.Contains(content, []byte(identifier)) || bytes.Contains(content, []byte("unique_id")) {
			t.Fatalf("raw input identity reached %s", name)
		}
		if !bytes.Contains(content, []byte(model)) || !bytes.Contains(content, []byte("touchscreen")) {
			t.Fatalf("privacy filtering removed useful model/category from %s", name)
		}
	}
}
