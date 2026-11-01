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
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func TestExtendedMemoryFinalizerRejectsChangedSourcesAfterSignedPrerequisite(t *testing.T) {
	for _, mode := range []string{"missing_gate", "quick_profile", "zero_profile", "old_schema", "wrong_profile",
		"null_source", "typed_digest", "file_digest", "short_interval", "changed_file_bytes", "malformed_file",
		"missing_dispatched_file", "outcome_downgrade", "public_journal", "public_stop", "cpu_source"} {
		t.Run(mode, func(t *testing.T) {
			h := newExtendedMemoryHarness(t, commandMemoryPass())
			verifyExtendedMemoryHarness(t, h)
			journal := sessionevidence.CloneJournal(h.journal)
			profile := plan.ProfileExtended
			output := filepath.Join(h.directory, "rejected.hhpcr")
			args := stressFinalizeArgs(h, output)
			gate := sessionFinalGate(func(context.Context) (sessionJournal, error) { return journal, nil })
			switch mode {
			case "missing_gate":
				gate = nil
			case "quick_profile":
				profile = plan.ProfileQuick
			case "zero_profile":
				profile = ""
			case "old_schema":
				journal.Schema = "hhsoft.pc-tester.quick-session/v11"
			case "wrong_profile":
				journal.Profile = "standard"
			case "null_source":
				journal.MemoryStressSource = nil
			case "typed_digest":
				journal.MemoryStressSource.EvidenceTypedSHA256 = "sha256:" + strings.Repeat("1", 64)
			case "file_digest":
				journal.MemoryStressSource.EvidenceFileSHA256 = "sha256:" + strings.Repeat("2", 64)
			case "short_interval":
				journal.Workers[2].Timing.FinishedMilliseconds = journal.Workers[2].Timing.StartedMilliseconds
			case "changed_file_bytes":
				path := filepath.Join(h.directory, "memory-stress.json")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal("original source prerequisite")
				}
				changed := append(append([]byte(nil), raw...), ' ')
				_, beforeDigest, beforeErr := quickevidence.DecodeMemoryStress(raw)
				_, afterDigest, afterErr := quickevidence.DecodeMemoryStress(changed)
				if beforeErr != nil || afterErr != nil || beforeDigest != afterDigest || sourceFileDigest(raw) == sourceFileDigest(changed) {
					t.Fatal("semantically equivalent distinct-original prerequisite")
				}
				if os.WriteFile(path, changed, 0600) != nil {
					t.Fatal("owned source mutation")
				}
			case "malformed_file":
				if os.WriteFile(filepath.Join(h.directory, "memory-stress.json"), []byte("{"), 0600) != nil {
					t.Fatal("owned malformed source")
				}
			case "missing_dispatched_file":
				if os.Remove(filepath.Join(h.directory, "memory-stress.json")) != nil {
					t.Fatal("owned missing-source fixture")
				}
				for i := range args {
					if args[i] == "memory.stress=pass" {
						args[i] = "memory.stress=aborted"
					}
				}
				journal.Workers[2].Outcome = "aborted"
			case "outcome_downgrade":
				for i := range args {
					if args[i] == "memory.stress=pass" {
						args[i] = "memory.stress=error"
					}
				}
			case "public_journal":
				args = append(args, "-session-journal", filepath.Join(h.directory, "session-journal.json"))
			case "public_stop":
				args = append(args, "-run-stop", "completed")
			case "cpu_source":
				source := *journal.MemoryStressSource
				source.WorkerID = "cpu.stress"
				journal.CPUStressSource = &source
			}
			if mode == "typed_digest" || mode == "file_digest" || mode == "short_interval" {
				if err := sessionevidence.ValidateStructure(journal); err != nil {
					t.Fatal("binding mutation must retain structural admission", err)
				}
			}
			var out, diagnostic bytes.Buffer
			result := runFinalizeOwnedDestinationWithPreviousForProfile(t.Context(), args, &out, &diagnostic,
				func(context.Context) (inventory.Snapshot, error) { return h.snapshot, nil },
				bytes.NewReader(bytes.Repeat([]byte{0x73}, 128)), time.Now, gate, writeDestination, nil, captureFixtureInput(t), profile)
			if result.Code == 0 {
				t.Fatal("mutated source or absent live authority signed")
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("invalid finalization created report")
			}
		})
	}
}

func TestExtendedMemoryFinalizerAcceptsDistinctOriginalSerializations(t *testing.T) {
	files := make(map[string]string)
	typed := make(map[string]string)
	for _, mode := range []string{"compact", "indented"} {
		t.Run(mode, func(t *testing.T) {
			h := newExtendedMemoryHarness(t, commandMemoryPass())
			run := h.dependencies.RunWorker
			h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
				code := run(ctx, worker, input, out, diagnostic)
				if worker.ID == "memory.stress" && mode == "indented" {
					compact, err := os.ReadFile(worker.OutputPath)
					if err != nil {
						t.Fatal("owned compact source")
					}
					indented, err := json.MarshalIndent(commandMemoryPass(), "", "  ")
					if err != nil {
						t.Fatal(err)
					}
					_, left, leftErr := quickevidence.DecodeMemoryStress(compact)
					_, right, rightErr := quickevidence.DecodeMemoryStress(indented)
					if leftErr != nil || rightErr != nil || bytes.Equal(compact, indented) || left != right {
						t.Fatal("both distinct serializations must independently decode")
					}
					if os.WriteFile(worker.OutputPath, indented, 0600) != nil {
						t.Fatal("owned indented source")
					}
				}
				return code
			}
			artifact := verifyExtendedMemoryHarness(t, h)
			if len(artifact.RenderedReport) == 0 || len(artifact.RenderedPDF) == 0 || h.journal.MemoryStressSource == nil {
				t.Fatal("real owner failed to publish canonical views")
			}
			files[mode], typed[mode] = h.journal.MemoryStressSource.EvidenceFileSHA256, h.journal.MemoryStressSource.EvidenceTypedSHA256
		})
	}
	if len(files) != 2 || files["compact"] == files["indented"] || typed["compact"] != typed["indented"] {
		t.Fatal("original bytes and typed semantics were conflated")
	}
}

func TestExtendedMemoryFinalizerOwnsProviderSourceBeforeSigning(t *testing.T) {
	h := newExtendedMemoryHarness(t, commandMemoryNoLoad())
	verifyExtendedMemoryHarness(t, h)
	provider := sessionevidence.CloneJournal(h.journal)
	expected := *provider.MemoryStressSource
	output := filepath.Join(h.directory, "copied.hhpcr")
	var out, diagnostic bytes.Buffer
	nowCalled := false
	now := func() time.Time {
		nowCalled = true
		provider.MemoryStressSource.EvidenceTypedSHA256 = "sha256:" + strings.Repeat("f", 64)
		return time.Now()
	}
	result := runFinalizeOwnedDestinationWithPreviousForProfile(t.Context(), stressFinalizeArgs(h, output), &out, &diagnostic,
		func(context.Context) (inventory.Snapshot, error) { return h.snapshot, nil },
		bytes.NewReader(bytes.Repeat([]byte{0x74}, 128)), now,
		func(context.Context) (sessionJournal, error) { return provider, nil },
		writeDestination, nil, captureFixtureInput(t), plan.ProfileExtended)
	if result.Code != 0 || !nowCalled || result.Journal == nil || result.Journal.MemoryStressSource == nil ||
		*result.Journal.MemoryStressSource != expected {
		t.Fatalf("provider alias changed signing inputs: %d %s", result.Code, &diagnostic)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal("copied report missing")
	}
	if _, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw))); err != nil {
		t.Fatal("provider mutation altered signed report", err)
	}
}

func TestExtendedMemoryCurrentExecutionCannotUsePublicFinalizerAuthority(t *testing.T) {
	h := newExtendedMemoryHarness(t, commandMemoryNoLoad())
	verifyExtendedMemoryHarness(t, h)
	var out, diagnostic bytes.Buffer
	output := filepath.Join(h.directory, "direct.hhpcr")
	result := runFinalizeOwnedDestinationWithPrevious(t.Context(), stressFinalizeArgs(h, output), &out, &diagnostic,
		func(context.Context) (inventory.Snapshot, error) {
			t.Fatal("public wrapper reached Extended inventory")
			return inventory.Snapshot{}, nil
		}, bytes.NewReader(bytes.Repeat([]byte{0x75}, 128)), time.Now, nil, writeDestination, nil, captureFixtureInput(t))
	if result.Code == 0 {
		t.Fatal("public wrapper manufactured Extended authority")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("public wrapper created Extended output")
	}
}

func TestExtendedMemorySourceNullMeansUnstartedRatherThanMissingEvidence(t *testing.T) {
	h := newExtendedMemoryHarness(t, commandMemoryNoLoad())
	verifyExtendedMemoryHarness(t, h)
	raw, err := os.ReadFile(filepath.Join(h.directory, "memory-stress.json"))
	if err != nil {
		t.Fatal("source prerequisite")
	}
	journal := sessionevidence.CloneJournal(h.journal)
	if err := validateSessionWorkerEvidence(&journal, "memory.stress", raw, nil, "inconclusive", nil); err != nil {
		t.Fatal("valid observed no-load source", err)
	}
	journal.MemoryStressSource = nil
	if validateSessionWorkerEvidence(&journal, "memory.stress", raw, nil, "inconclusive", nil) == nil {
		t.Fatal("dispatched null source was accepted")
	}
	encoded, err := json.Marshal(journal)
	if err != nil || !bytes.Contains(encoded, []byte("\"memory_stress_source\":null")) {
		t.Fatal("required nullable member omitted")
	}
	copy := sessionevidence.CloneJournal(h.journal)
	copy.MemoryStressSource.EvidenceFileSHA256 = "changed"
	if h.journal.MemoryStressSource.EvidenceFileSHA256 == "changed" {
		t.Fatal("clone aliases the original source descriptor")
	}
}

func TestExtendedMemoryFinalizerPreservesExistingOwnedOutput(t *testing.T) {
	h := newExtendedMemoryHarness(t, commandMemoryPass())
	verifyExtendedMemoryHarness(t, h)
	output := filepath.Join(h.directory, "existing.hhpcr")
	if os.WriteFile(output, []byte("preserved"), 0600) != nil {
		t.Fatal("existing owner prerequisite")
	}
	var out, diagnostic bytes.Buffer
	result := runFinalizeOwnedDestinationWithPreviousForProfile(t.Context(), stressFinalizeArgs(h, output), &out, &diagnostic,
		func(context.Context) (inventory.Snapshot, error) { return h.snapshot, nil },
		bytes.NewReader(bytes.Repeat([]byte{0x76}, 128)), time.Now,
		func(context.Context) (sessionJournal, error) { return sessionevidence.CloneJournal(h.journal), nil },
		writeDestination, nil, captureFixtureInput(t), plan.ProfileExtended)
	raw, err := os.ReadFile(output)
	if result.Code == 0 || err != nil || string(raw) != "preserved" {
		t.Fatal("finalizer replaced an existing output")
	}
}
