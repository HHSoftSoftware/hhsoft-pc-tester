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
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func stressFinalizeArgs(h *sessionHarness, output string) []string {
	args := []string{"-plan", h.request.PlanPath, "-fingerprint-key", h.request.FingerprintKeyPath,
		"-key-epoch", h.request.KeyEpoch, "-evidence-dir", h.directory, "-output", output}
	for _, outcome := range h.request.Outcomes {
		args = append(args, "-outcome", outcome)
	}
	return args
}

func verifyStandardHarness(t *testing.T, h *sessionHarness) bundle.Artifact {
	t.Helper()
	if code := h.run(t.Context()); code != 0 || !h.finalized {
		t.Fatalf("positive live standard prerequisite: %d %s", code, &h.stderr)
	}
	raw, err := os.ReadFile(filepath.Join(h.directory, "session.hhpcr"))
	if err != nil {
		t.Fatal("positive report absent")
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal("positive signature and canonical views", err)
	}
	return artifact
}

func TestCPUStressFinalizerRejectsChangedSourcesAfterSignedPrerequisite(t *testing.T) {
	for _, mode := range []string{"missing_gate", "quick_profile", "zero_profile", "old_schema", "wrong_profile",
		"null_source", "typed_digest", "file_digest", "short_interval", "changed_file_bytes",
		"malformed_file", "missing_dispatched_file", "outcome_downgrade", "public_journal", "public_stop"} {
		t.Run(mode, func(t *testing.T) {
			h := newStandardHarness(t, commandStressPass())
			verifyStandardHarness(t, h)
			journal := sessionevidence.CloneJournal(h.journal)
			profile := plan.ProfileStandard
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
				journal.Schema = sessionevidence.SchemaV7
			case "wrong_profile":
				journal.Profile = "quick"
			case "null_source":
				journal.CPUStressSource = nil
			case "typed_digest":
				journal.CPUStressSource.EvidenceTypedSHA256 = "sha256:" + strings.Repeat("1", 64)
			case "file_digest":
				journal.CPUStressSource.EvidenceFileSHA256 = "sha256:" + strings.Repeat("2", 64)
			case "short_interval":
				journal.Workers[1].Timing.FinishedMilliseconds = journal.Workers[1].Timing.StartedMilliseconds
			case "changed_file_bytes":
				path := filepath.Join(h.directory, "cpu-stress.json")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal("fixture")
				}
				if os.WriteFile(path, append(raw, ' '), 0600) != nil {
					t.Fatal("fixture")
				}
			case "malformed_file":
				if os.WriteFile(filepath.Join(h.directory, "cpu-stress.json"), []byte("{"), 0600) != nil {
					t.Fatal("fixture")
				}
			case "missing_dispatched_file":
				if os.Remove(filepath.Join(h.directory, "cpu-stress.json")) != nil {
					t.Fatal("fixture")
				}
				for i := range args {
					if args[i] == "cpu.stress=pass" {
						args[i] = "cpu.stress=aborted"
					}
				}
				journal.Workers[1].Outcome = "aborted"
			case "outcome_downgrade":
				for i := range args {
					if args[i] == "cpu.stress=pass" {
						args[i] = "cpu.stress=error"
					}
				}
			case "public_journal":
				args = append(args, "-session-journal", filepath.Join(h.directory, "session-journal.json"))
			case "public_stop":
				args = append(args, "-run-stop", "completed")
			}
			var out, diagnostic bytes.Buffer
			result := runFinalizeOwnedDestinationWithPreviousForProfile(t.Context(), args, &out, &diagnostic,
				func(context.Context) (inventory.Snapshot, error) { return h.snapshot, nil },
				bytes.NewReader(bytes.Repeat([]byte{0x63}, 128)), time.Now, gate, writeDestination, nil, captureFixtureInput(t), profile)
			if result.Code == 0 {
				t.Fatal("mutated source or absent live admission signed")
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("invalid finalization created report")
			}
		})
	}
}

func TestCPUStressFinalizerCopiesProviderSourceBeforeSigning(t *testing.T) {
	h := newStandardHarness(t, commandStressNoLoad())
	verifyStandardHarness(t, h)
	provider := sessionevidence.CloneJournal(h.journal)
	expected := *provider.CPUStressSource
	output := filepath.Join(h.directory, "copied.hhpcr")
	var out, diagnostic bytes.Buffer
	nowCalled := false
	now := func() time.Time {
		nowCalled = true
		provider.CPUStressSource.EvidenceTypedSHA256 = "sha256:" + strings.Repeat("f", 64)
		return time.Now()
	}
	result := runFinalizeOwnedDestinationWithPreviousForProfile(t.Context(), stressFinalizeArgs(h, output), &out, &diagnostic,
		func(context.Context) (inventory.Snapshot, error) { return h.snapshot, nil },
		bytes.NewReader(bytes.Repeat([]byte{0x64}, 128)), now,
		func(context.Context) (sessionJournal, error) { return provider, nil },
		writeDestination, nil, captureFixtureInput(t), plan.ProfileStandard)
	if result.Code != 0 || !nowCalled || result.Journal == nil || result.Journal.CPUStressSource == nil ||
		*result.Journal.CPUStressSource != expected {
		t.Fatalf("provider alias changed finalization: %d %s", result.Code, &diagnostic)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal("copied report missing")
	}
	if _, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw))); err != nil {
		t.Fatal("provider mutation altered signed report", err)
	}
}

func TestCPUStressCurrentStandardCannotFinalizeThroughPublicWrapper(t *testing.T) {
	h := newStandardHarness(t, commandStressNoLoad())
	verifyStandardHarness(t, h)
	var out, diagnostic bytes.Buffer
	output := filepath.Join(h.directory, "direct.hhpcr")
	result := runFinalizeOwnedDestinationWithPrevious(t.Context(), stressFinalizeArgs(h, output), &out, &diagnostic,
		func(context.Context) (inventory.Snapshot, error) {
			t.Fatal("direct standard reached inventory")
			return inventory.Snapshot{}, nil
		},
		bytes.NewReader(bytes.Repeat([]byte{0x65}, 128)), time.Now, nil, writeDestination, nil, captureFixtureInput(t))
	if result.Code == 0 {
		t.Fatal("public wrapper manufactured standard admission")
	}
}

func TestCPUStressFinalizerSourceNullIsOnlyUnstarted(t *testing.T) {
	h := newStandardHarness(t, commandStressNoLoad())
	verifyStandardHarness(t, h)
	raw, err := os.ReadFile(filepath.Join(h.directory, "cpu-stress.json"))
	if err != nil {
		t.Fatal("source missing")
	}
	journal := sessionevidence.CloneJournal(h.journal)
	if validateSessionWorkerEvidence(&journal, "cpu.stress", raw, nil, "inconclusive", nil) != nil {
		t.Fatal("positive persisted no-load source")
	}
	journal.CPUStressSource = nil
	if validateSessionWorkerEvidence(&journal, "cpu.stress", raw, nil, "inconclusive", nil) == nil {
		t.Fatal("dispatched no-load null accepted")
	}
	// A clone has its own source descriptor. Encoding keeps the required null
	// member even in this deliberately invalid dispatched record.
	copied := sessionevidence.CloneJournal(h.journal)
	copied.CPUStressSource.EvidenceFileSHA256 = "changed"
	if h.journal.CPUStressSource.EvidenceFileSHA256 == "changed" {
		t.Fatal("journal clone aliases source")
	}
	encoded, err := json.Marshal(journal)
	if err != nil || !bytes.Contains(encoded, []byte("\"cpu_stress_source\":null")) {
		t.Fatal("required nullable member omitted")
	}
}
