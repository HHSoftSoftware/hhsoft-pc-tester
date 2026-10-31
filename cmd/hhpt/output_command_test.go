package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/usbtest"
)

type outputCommandCase struct {
	name string
	run  func(destinationWriter, io.Writer, io.Writer) int
}

func outputCommandCases(t *testing.T) []outputCommandCase {
	t.Helper()
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x24}, 32)
	keyPath := writeTestKey(t, directory, "fingerprint.key", key)
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x46}, ed25519.SeedSize))
	privatePath := writeTestKey(t, directory, "signing.key", privateKey)
	publicPath := writeTestKey(t, directory, "signing.pub", privateKey.Public().(ed25519.PublicKey))
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentSystem, "system0", map[string]string{"vendor": "Synthetic fixture"}),
		inventory.NewComponent(inventory.ComponentCPU, "cpu:0", map[string]string{"serial": "synthetic-cpu"}),
	})
	collect := func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }
	reportPath := filepath.Join(directory, "comparison-input.hhpcr")
	writeSignedComparisonFixture(t, reportPath, snapshot, key, privateKey, "fixture-signer", "fixture-session")
	planPath := writeFinalizeTestPlan(t, directory, snapshot, key)
	if err := os.WriteFile(filepath.Join(directory, "cpu.json"), encodeEvidenceFixture(t, completeCPUEvidence()), 0600); err != nil {
		t.Fatal(err)
	}
	finalizeArguments := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory,
		"-session-id", "fixture-session", "-output", "owned-output", "-outcome", "audio.readiness.quick=not_present",
		"-outcome", "cpu.quick=pass", "-outcome", "display.operator.quick=not_present", "-outcome", "graphics.quick=not_present",
		"-outcome", "input.keyboard.quick=not_present", "-outcome", "input.pointer.quick=not_present", "-outcome", "input.touchpad.quick=not_present", "-outcome", "input.readiness.quick=not_present",
		"-outcome", "memory.quick=not_present", "-outcome", "network.readiness.quick=not_present", "-outcome", "storage.readiness.quick=not_present",
		"-outcome", "usb.readiness.quick=not_present", "-outcome", "inspection.exterior.quick=excluded"}
	now := func() time.Time { return time.Unix(10, 20) }
	return []outputCommandCase{
		{"inventory", func(write destinationWriter, stdout, stderr io.Writer) int {
			return runInventoryWithInput(t.Context(), []string{"-fingerprint-key", keyPath, "-output", "owned-output"}, stdout, stderr, collect, write, captureFixtureInput(t))
		}},
		{"comparison", func(write destinationWriter, stdout, stderr io.Writer) int {
			return runCompareDestination(t.Context(), []string{"-public-key", publicPath, "-key-id", "fixture-signer", "-output", "owned-output", reportPath, reportPath}, stdout, stderr, write, readRetestInput)
		}},
		{"display", func(write destinationWriter, stdout, stderr io.Writer) int {
			return runDisplayWithDestination(t.Context(), []string{"-output", "owned-output"}, strings.NewReader(""), stdout, stderr,
				func(context.Context, string, time.Duration, graphics.InteractiveRunner) graphics.VisualResult {
					return completeDisplayEvidence()
				}, write)
		}},
		{"finalize", func(write destinationWriter, stdout, stderr io.Writer) int {
			return runFinalizeTestDestination(t, t.Context(), finalizeArguments, stdout, stderr, collect,
				bytes.NewReader(bytes.Repeat([]byte{0x34}, 64)), now, nil, write).Code
		}},
		{"plan", func(write destinationWriter, stdout, stderr io.Writer) int {
			return runPlanWithInput(t.Context(), []string{"-fingerprint-key", keyPath, "-output", "owned-output"}, stdout, stderr, collect, write, captureFixtureInput(t))
		}},
		{"quick-result", func(write destinationWriter, stdout, stderr io.Writer) int {
			return writeQuickResultDestination(completeCPUEvidence(), "owned-output", stdout, stderr, "CPU", write)
		}},
		{"graphics", func(write destinationWriter, stdout, stderr io.Writer) int {
			return runGraphicsWithDestination(t.Context(), []string{"-output", "owned-output"}, stdout, stderr,
				func(context.Context, string, int, graphics.Runner) graphics.Result { return completeGraphicsEvidence() }, write)
		}},
		{"capture", func(write destinationWriter, stdout, stderr io.Writer) int {
			return runCaptureWithDestination(t.Context(), []string{"-fingerprint-key", keyPath, "-signing-key", privatePath, "-key-id", "fixture-signer",
				"-session-id", "fixture-session", "-output", "owned-output", "-memory-bytes", "4194304", "-memory-passes", "1", "-graphics-pattern-frames", "12"},
				stdout, stderr, collect,
				func(context.Context, int) graphics.Result { return completeGraphicsEvidence() },
				func(context.Context, quick.CPUOptions) quick.CPUResult { return completeCPUEvidence() },
				func(context.Context, quick.MemoryOptions) quick.MemoryResult { return completeMemoryEvidence() },
				func(context.Context, inputtest.Options) inputtest.Result { return completeInputEvidence() },
				func(context.Context, networktest.Options) networktest.Result { return completeNetworkEvidence() },
				func(context.Context, storage.Options) storage.Result { return completeStorageEvidence() },
				func(context.Context, usbtest.Options) usbtest.Result { return completeUSBEvidence() }, bytes.NewReader(nil), now, write, captureFixtureInput(t))
		}},
	}
}

func TestEveryDestinationCallerRejectsIncompleteOutput(t *testing.T) {
	openFault := errors.New("injected open failure")
	writeFault := errors.New("injected write failure")
	closeFault := errors.New("injected close failure")
	for _, command := range outputCommandCases(t) {
		t.Run(command.name, func(t *testing.T) {
			for _, test := range []struct {
				name                                   string
				limit                                  int
				openErr, writeErr, closeErr, wantWrite error
				shortFinal                             bool
			}{
				{"success", -1, nil, nil, nil, nil, false},
				{"open", -1, openFault, nil, nil, nil, false},
				{"write", 7, nil, writeFault, nil, writeFault, false},
				{"short-write", 7, nil, nil, nil, io.ErrShortWrite, false},
				{"zero-write", 0, nil, nil, nil, io.ErrShortWrite, false},
				{"last-byte", -1, nil, nil, nil, io.ErrShortWrite, true},
				{"close", -1, nil, nil, closeFault, nil, false},
				{"write-and-close", 7, nil, writeFault, closeFault, writeFault, false},
				{"short-and-close", 7, nil, nil, closeFault, io.ErrShortWrite, false},
			} {
				t.Run(test.name, func(t *testing.T) {
					file := &outputFaultFile{limit: test.limit, shortFinal: test.shortFinal, writeErr: test.writeErr, closeErr: test.closeErr}
					opens, calls := 0, 0
					var completion error
					write := func(path string, stdout io.Writer, callback func(io.Writer) error) error {
						calls++
						completion = writeDestinationWith(path, stdout, callback, func(name string, flags int, mode os.FileMode) (io.WriteCloser, error) {
							opens++
							if name != "owned-output" || flags != os.O_WRONLY|os.O_CREATE|os.O_EXCL || mode != 0600 {
								t.Fatal("caller changed owned output admission")
							}
							if test.openErr != nil {
								return nil, test.openErr
							}
							return file, nil
						})
						return completion
					}
					var stdout, stderr bytes.Buffer
					code := command.run(write, &stdout, &stderr)
					wantCode, wantWrites, wantCloses := 1, 1, 1
					if test.name == "success" {
						wantCode = 0
					}
					if test.openErr != nil {
						wantWrites, wantCloses = 0, 0
					}
					if code != wantCode || calls != 1 || opens != 1 || file.writes != wantWrites || file.closes != wantCloses {
						t.Fatalf("code=%d calls=%d open=%d write=%d close=%d stderr=%s", code, calls, opens, file.writes, file.closes, stderr.String())
					}
					for _, expected := range []error{test.openErr, test.wantWrite, test.closeErr} {
						if expected != nil && (!errors.Is(completion, expected) || command.name != "finalize" && !strings.Contains(stderr.String(), expected.Error())) {
							t.Fatalf("caller lost failure %v: error=%v stderr=%s", expected, completion, stderr.String())
						}
					}
					// The current finalizer preserves typed completion errors but
					// emits only its fixed public diagnostic, never provider text or paths.
					if command.name == "finalize" && wantCode != 0 && stderr.String() != "write touchpad report failed; completed output may remain\n" {
						t.Fatalf("finalizer output diagnostic was not sanitized: %s", stderr.String())
					}
					if test.name == "success" && (file.Len() == 0 || stderr.Len() != 0) {
						t.Fatal("healthy control did not produce clean output")
					}
					if command.name == "finalize" && strings.Contains(stdout.String(), "finalized:") != (wantCode == 0) {
						t.Fatalf("finalizer announced success before completion: %s", stdout.String())
					}
					if test.limit >= 0 && file.Len() != test.limit {
						t.Fatal("caller discarded or retried partial output")
					}
					if test.shortFinal && file.Len() != file.requested-1 {
						t.Fatal("last-byte short-write fixture did not reach the boundary")
					}
				})
			}
		})
	}
}

func TestOutputCompletionCannotDowngradeUncertainWorkerCleanup(t *testing.T) {
	writeFault, closeFault := errors.New("injected write failure"), errors.New("injected close failure")
	cases := []outputCommandCase{
		{"graphics", func(write destinationWriter, stdout, stderr io.Writer) int {
			return runGraphicsWithDestination(t.Context(), []string{"-output", "owned"}, stdout, stderr,
				func(context.Context, string, int, graphics.Runner) graphics.Result {
					result := completeGraphicsEvidence()
					result.CleanupUncertain = true
					return result
				}, write)
		}},
		{"display", func(write destinationWriter, stdout, stderr io.Writer) int {
			return runDisplayWithDestination(t.Context(), []string{"-output", "owned"}, strings.NewReader(""), stdout, stderr,
				func(context.Context, string, time.Duration, graphics.InteractiveRunner) graphics.VisualResult {
					result := completeDisplayEvidence()
					result.CleanupUncertain = true
					return result
				}, write)
		}},
		{"CPU", func(write destinationWriter, stdout, stderr io.Writer) int {
			return runCPUWithDestination(t.Context(), []string{"-output", "owned"}, stdout, stderr,
				func(context.Context, quick.CPUOptions) quick.CPUResult {
					result := completeCPUEvidence()
					result.Safety.CleanupConfirmed = false
					return result
				}, write)
		}},
		{"memory", func(write destinationWriter, stdout, stderr io.Writer) int {
			return runMemoryWithDestination(t.Context(), []string{"-output", "owned"}, stdout, stderr,
				func(context.Context, quick.MemoryOptions) quick.MemoryResult {
					result := completeMemoryEvidence()
					result.Safety.CleanupConfirmed = false
					return result
				}, write)
		}},
	}
	for _, command := range cases {
		t.Run(command.name, func(t *testing.T) {
			file := &outputFaultFile{limit: 3, writeErr: writeFault, closeErr: closeFault}
			write := func(path string, stdout io.Writer, callback func(io.Writer) error) error {
				return writeDestinationWith(path, stdout, callback, func(string, int, os.FileMode) (io.WriteCloser, error) { return file, nil })
			}
			var stdout, stderr bytes.Buffer
			if code := command.run(write, &stdout, &stderr); code != workerCleanupUncertainExitCode || file.closes != 1 {
				t.Fatalf("cleanup result=%d closes=%d stderr=%s", code, file.closes, stderr.String())
			}
			for _, expected := range []string{writeFault.Error(), closeFault.Error(), "cleanup could not be confirmed"} {
				if !strings.Contains(stderr.String(), expected) {
					t.Fatalf("lost diagnostic %q: %s", expected, stderr.String())
				}
			}
		})
	}
}

func TestQuickEncodingFailureDoesNotCreateDestination(t *testing.T) {
	called := false
	var stderr bytes.Buffer
	code := writeQuickResultDestination(make(chan int), "owned", io.Discard, &stderr, "fixture",
		func(string, io.Writer, func(io.Writer) error) error { called = true; return nil })
	if code != 1 || called || !strings.Contains(stderr.String(), "encode fixture result:") {
		t.Fatalf("encoding failure opened output: code=%d called=%v stderr=%s", code, called, stderr.String())
	}
}
