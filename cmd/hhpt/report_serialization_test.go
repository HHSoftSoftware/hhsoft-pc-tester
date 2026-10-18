package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func TestCaptureViewsUseSerializedEvidenceForBothSigningProfiles(t *testing.T) {
	fixtures := []struct {
		name     string
		evidence string
	}{
		{"compact", `{"detail":"ordinary"}`},
		{"encoder-newline", "{\"detail\":\"ordinary\"}\n"},
		{"pretty-nested-newline", "{\n  \"observations\": [{\"detail\": \"ordinary\"}]\n}\n"},
		{"literal-HTML-sensitive", `{"detail":"A<B & C>D"}`},
		{"escaped-HTML-sensitive", `{"detail":"A\u003cB \u0026 C\u003eD"}`},
		{"escaped-unicode-and-slash", `{"detail":"\u0041\u2028\u2029\/"}`},
		{"literal-unicode-separators", "{\"detail\":\"A\u2028\u2029/\"}"},
		{"raw-numeric-precision", "{\n \"numbers\": [9007199254740993,18446744073709551615,1.2300,1e+30]\n}\n"},
	}
	for _, selfIssued := range []bool{false, true} {
		profile := "provisioned"
		if selfIssued {
			profile = "self-issued"
		}
		for _, fixture := range fixtures {
			t.Run(profile+"/"+fixture.name, func(t *testing.T) {
				result, err := app.NewEncodedTestResult("fixture.quick", 1, app.TestPass, json.RawMessage(fixture.evidence))
				if err != nil {
					t.Fatal(err)
				}
				privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x48}, ed25519.SeedSize))
				publicKey := privateKey.Public().(ed25519.PublicKey)
				fingerprintKey := bytes.Repeat([]byte{0x28}, 32)
				snapshot := inventory.NewSnapshot(nil)
				generated := time.Unix(10, 20)
				var encoded []byte
				if selfIssued {
					encoded, err = captureSelfIssuedBundle(snapshot, []app.TestResult{result}, fingerprintKey, "fixture", privateKey, publicKey, "fixture-session", generated)
				} else {
					encoded, err = captureBundle(snapshot, []app.TestResult{result}, fingerprintKey, "fixture", privateKey, "fixture-signer", "fixture-session", generated)
				}
				if err != nil {
					t.Fatalf("valid evidence could not produce a signed report: %v", err)
				}
				var artifact bundle.Artifact
				if selfIssued {
					artifact, err = bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
				} else {
					artifact, err = bundle.Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{"fixture-signer": publicKey})
				}
				if err != nil {
					t.Fatalf("independent report verification failed: %v", err)
				}
				var payload app.InventoryPayload
				if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil || len(payload.TestResults) != 1 {
					t.Fatalf("verified report lost its test: %v", err)
				}
				// RawMessage serialization preserves numeric spellings, rather than
				// round-tripping large integers through float64.
				if fixture.name == "raw-numeric-precision" {
					var evidence struct {
						Numbers []json.RawMessage `json:"numbers"`
					}
					if err := json.Unmarshal(payload.TestResults[0].Evidence, &evidence); err != nil {
						t.Fatal(err)
					}
					var spellings []string
					for _, number := range evidence.Numbers {
						spellings = append(spellings, string(number))
					}
					if got := strings.Join(spellings, ","); got != "9007199254740993,18446744073709551615,1.2300,1e+30" {
						t.Fatalf("numeric evidence changed: %s", got)
					}
				}
				directory := t.TempDir()
				path := filepath.Join(directory, "report.hhpcr")
				if err := os.WriteFile(path, encoded, 0o600); err != nil {
					t.Fatal(err)
				}
				arguments := []string{"-self-issued", path}
				if !selfIssued {
					publicPath := writeTestKey(t, directory, "fixture.pub", publicKey)
					arguments = []string{"-public-key", publicPath, "-key-id", "fixture-signer", path}
				}
				var stdout, stderr bytes.Buffer
				if code := runVerify(arguments, &stdout, &stderr); code != 0 {
					t.Fatalf("verification command returned %d: %s", code, stderr.String())
				}
				encoded[len(encoded)-1] ^= 1
				if err := os.WriteFile(path, encoded, 0o600); err != nil {
					t.Fatal(err)
				}
				if code := runVerify(arguments, &stdout, &stderr); code == 0 {
					t.Fatal("tampered report passed verification")
				}
			})
		}
	}
}

func TestFinalizeAcceptsActualWorkerJSONSerialization(t *testing.T) {
	for _, encoder := range []string{"worker-output", "json-encoder"} {
		t.Run(encoder, func(t *testing.T) {
			directory := t.TempDir()
			fingerprintKey := bytes.Repeat([]byte{0x24}, 32)
			fingerprintPath := writeTestKey(t, directory, "fixture.key", fingerprintKey)
			cpu := completeCPUEvidence()
			cpu.Iterations = 9007199254740993
			cpu.Detail = "benign A<B & C>D \u2028 text"
			var worker, workerError bytes.Buffer
			if encoder == "worker-output" {
				if code := writeQuickResult(cpu, "-", &worker, &workerError, "CPU"); code != 0 {
					t.Fatalf("worker serializer returned %d: %s", code, workerError.String())
				}
			} else if err := json.NewEncoder(&worker).Encode(cpu); err != nil {
				t.Fatal(err)
			}
			if !bytes.HasSuffix(worker.Bytes(), []byte("\n")) {
				t.Fatal("fixture omitted the real worker newline")
			}
			if err := os.WriteFile(filepath.Join(directory, "cpu.json"), worker.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			snapshot := inventory.NewSnapshot([]inventory.Component{
				inventory.NewComponent(inventory.ComponentCPU, "cpu:0", map[string]string{"description": "synthetic CPU"}),
			})
			planPath := writeFinalizeTestPlan(t, directory, snapshot, fingerprintKey)
			reportPath := filepath.Join(directory, "session.hhpcr")
			arguments := []string{"-fingerprint-key", fingerprintPath, "-plan", planPath, "-evidence-dir", directory,
				"-session-id", "fixture-session", "-output", reportPath}
			for _, definition := range quickResultDefinitions {
				status := "not_present"
				if definition.id == "cpu.quick" {
					status = "pass"
				}
				arguments = append(arguments, "-outcome", definition.id+"="+status)
			}
			collect := func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }
			var stdout, stderr bytes.Buffer
			if code := runFinalizeWith(context.Background(), arguments, &stdout, &stderr, collect,
				bytes.NewReader(bytes.Repeat([]byte{0x38}, sessionIDLength+32)), func() time.Time { return time.Unix(10, 20) }); code != 0 {
				t.Fatalf("worker output could not finalize: code=%d stderr=%s", code, stderr.String())
			}
			encoded, err := os.ReadFile(reportPath)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
			if err != nil {
				t.Fatal(err)
			}
			var payload app.InventoryPayload
			if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range payload.TestResults {
				if row.ID != "cpu.quick" {
					continue
				}
				var evidence quick.CPUResult
				if err := json.Unmarshal(row.Evidence, &evidence); err != nil || evidence != cpu || row.Status != app.TestPass {
					t.Fatalf("finalization changed worker facts: %+v, %v", evidence, err)
				}
				found = true
			}
			if !found {
				t.Fatal("signed report omitted CPU evidence")
			}
			stdout.Reset()
			stderr.Reset()
			if code := runVerify([]string{"-self-issued", reportPath}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "contents/signature valid") {
				t.Fatalf("verification command rejected finalized report: code=%d stderr=%s", code, stderr.String())
			}
		})
	}
}

func TestCaptureViewsUseSerializedPublicAttributes(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	publicKey := privateKey.Public().(ed25519.PublicKey)
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentCPU, "cpu:0", map[string]string{"description": "synthetic-\xff"}),
	})
	for _, selfIssued := range []bool{false, true} {
		var encoded []byte
		var err error
		if selfIssued {
			encoded, err = captureSelfIssuedBundle(snapshot, nil, make([]byte, 32), "fixture", privateKey, publicKey, "fixture-session", time.Unix(10, 20))
		} else {
			encoded, err = captureBundle(snapshot, nil, make([]byte, 32), "fixture", privateKey, "fixture-signer", "fixture-session", time.Unix(10, 20))
		}
		if err != nil {
			t.Fatalf("serialized attribute cannot be rendered: %v", err)
		}
		var artifact bundle.Artifact
		if selfIssued {
			artifact, err = bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
		} else {
			artifact, err = bundle.Verify(bytes.NewReader(encoded), int64(len(encoded)), report.Keyring{"fixture-signer": publicKey})
		}
		if err != nil {
			t.Fatal(err)
		}
		var payload app.InventoryPayload
		if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
			t.Fatal(err)
		}
		if got := payload.Components[0].Attributes[0].Value; got != "synthetic-\ufffd" {
			t.Fatalf("serialized public attribute changed unexpectedly: %q", got)
		}
	}
}

func TestSerializedInventoryRejectsOversizedInputBeforeViewRendering(t *testing.T) {
	payload := app.InventoryPayload{SchemaVersion: 1, Components: []app.ComponentRecord{{
		ID: "cpu:0", Type: inventory.ComponentCPU,
		Attributes: []inventory.Attribute{{Name: "description", Value: strings.Repeat("x", 16<<20)}},
	}}}
	encoded, html, pdf, err := renderSerializedInventory(payload)
	if err == nil || !strings.Contains(err.Error(), "serialized inventory") || encoded != nil || html != nil || pdf != nil {
		t.Fatal("oversized serialized inventory reached view rendering")
	}
}
