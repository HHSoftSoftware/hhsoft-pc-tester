//go:build linux || windows

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestFinalizerFingerprintRealKeyCompletionAndPreservation(t *testing.T) {
	for _, size := range []int{31, 32, 33} {
		name := "exact"
		if size == 31 {
			name = "short"
		}
		if size == 33 {
			name = "long"
		}
		t.Run(name, func(t *testing.T) {
			h := newFinalizerFingerprintHarness(t)
			before := bytes.Repeat([]byte{0x35}, size)
			if err := os.WriteFile(h.keyPath, before, 0600); err != nil {
				t.Fatal(err)
			}
			result := sessionFinalizationResult{Code: runFinalizeWith(t.Context(), h.args, &h.stdout, &h.stderr, h.collect, h, h.now)}
			if size == 32 {
				raw, err := os.ReadFile(h.outputPath)
				if err != nil {
					t.Fatal(err)
				}
				h.output.Write(raw)
				h.success(result)
			} else {
				h.stopped(result, 0, "read fingerprint key: input acquisition failed\n")
			}
			after, err := os.ReadFile(h.keyPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("real fingerprint input changed")
			}
		})
	}
}

func TestFinalizerFingerprintLinkedContextGuardsBeforeKey(t *testing.T) {
	for _, mode := range []string{"explicit-parent-typed-nil", "verified-summary-typed-nil", "explicit-parent-nil-reader"} {
		t.Run(mode, func(t *testing.T) {
			h := newFinalizerFingerprintHarness(t)
			h.args = replaceFinalizerFingerprintFlag(h.args, "-session-id", retestChildID)
			parent := commandRetestArtifact(t, "finalizer-parent", true, nil, 0x57)
			path, _ := writeCommandRetest(t, t.TempDir(), "parent", parent)
			options := previousReportFlags{path: path, pathSet: true, trust: reportTrustFlags{selfIssued: true, selfSet: true}}
			summary, err := options.load(t.Context(), readRetestInput)
			if err != nil || summary == nil || !summary.valid() {
				t.Fatal("valid verified predecessor prerequisite")
			}
			var value *retestInputStoppedContext
			var ctx context.Context = value
			var previous *verifiedPreviousReport
			var read retestInputReader = h.read
			diagnostic := "finalize: predecessor input or verification failed\n"
			if mode == "verified-summary-typed-nil" {
				previous = summary
				diagnostic = "finalize: linked retest cancelled\n"
			} else {
				h.args = append(h.args, "-previous-report", path, "-previous-self-issued")
			}
			if mode == "explicit-parent-nil-reader" {
				ctx = t.Context()
				read = nil
			}
			h.stopped(h.run(ctx, read, previous, h.forbiddenGate), 0, diagnostic)
			assertRetestInputLedger(t, h.fixture, map[string]int{}, nil)
		})
	}
}

func TestFinalizerFingerprintExplicitParentInputOrder(t *testing.T) {
	for _, mode := range []string{"valid", "parent-close", "parent-key-close"} {
		t.Run(mode, func(t *testing.T) {
			h := newFinalizerFingerprintHarness(t)
			h.args = replaceFinalizerFingerprintFlag(h.args, "-session-id", retestChildID)
			parent := commandRetestArtifact(t, "provisioned-parent", false, nil, 0x58)
			h.args = append(h.args, "-previous-report", "parent-artifact-marker", "-previous-public-key", "parent-public-marker", "-previous-key-id", parent.keyID)
			wantPaths := []string{"parent-artifact-marker", "parent-public-marker", h.keyPath}
			wantKinds := []retestInputKind{retestArtifactInput, retestPublicKeyInput, retestFingerprintKeyInput}
			var roles []retestInputKind
			var owners []*retestInputFixture
			read := func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
				index := len(roles)
				if index >= len(wantPaths) || path != wantPaths[index] || kind != wantKinds[index] {
					t.Fatal("parent/key order or trust changed")
				}
				for _, owner := range owners {
					assertRetestInputLedger(t, owner, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				}
				roles = append(roles, kind)
				if index == 2 {
					return h.read(ctx, path, kind)
				}
				raw := parent.encoded
				if index == 1 {
					raw = parent.public
				}
				owner := newRetestInputFixture(len(raw))
				owner.readable.data = append([]byte(nil), raw...)
				if mode == "parent-close" && index == 0 || mode == "parent-key-close" && index == 1 {
					owner.readable.closeErr = errors.New("private-predecessor-close-marker")
				}
				owners = append(owners, owner)
				data, err := readRetestInputWithOperations(ctx, path, kind, owner.operations())
				if err != nil && (data != nil || !errors.Is(err, errRetestInputCleanup)) {
					t.Fatal("parent owner error lost")
				}
				return data, err
			}
			result := h.run(t.Context(), read, nil, nil)
			if mode == "valid" {
				artifact := h.success(result)
				if !reflect.DeepEqual(roles, wantKinds) || h.reads != 1 || artifact.Manifest.PreviousReport == nil || *artifact.Manifest.PreviousReport != commandRetestReference(parent) {
					t.Fatal("verified independent trust/reference or fingerprint order changed")
				}
				if artifact.Envelope.SessionID != retestChildID {
					t.Fatal("linked child identity changed")
				}
				assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			} else {
				h.stopped(result, 0, "finalize: predecessor input or verification failed\n")
				want := 1
				if mode == "parent-key-close" {
					want = 2
				}
				if len(roles) != want {
					t.Fatal("later role acquired after predecessor failure")
				}
			}
			for _, owner := range owners {
				assertRetestInputLedger(t, owner, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			}
		})
	}
}

func TestFinalizerFingerprintCancelledFreshAdapterContext(t *testing.T) {
	h := newFinalizerFingerprintHarness(t)
	worker, stopWorker := context.WithCancel(t.Context())
	stopWorker()
	if worker.Err() != context.Canceled {
		t.Fatal("stopped-worker prerequisite")
	}
	finalContext, finishFinal := context.WithTimeout(context.Background(), 30*time.Second)
	defer finishFinal()
	if finalContext.Err() != nil {
		t.Fatal("fresh final context already stopped")
	}
	finishFinal()
	var outcomes []string
	for i := 0; i+1 < len(h.args); i++ {
		if h.args[i] == "-outcome" {
			outcomes = append(outcomes, h.args[i+1])
		}
	}
	request := sessionFinalization{PlanPath: h.planPath, FingerprintKeyPath: h.keyPath, KeyEpoch: "ephemeral-session", EvidenceDirectory: h.directory, OutputPath: h.outputPath, Outcomes: outcomes, Gate: h.forbiddenGate}
	result := finalizeSessionWith(finalContext, request, &h.stdout, &h.stderr, h.collect, h, h.now)
	h.stopped(result, 0, "finalize input acquisition failed\n")
	// The retained real clean-stop test supplies a live fresh context and verifies
	// the signed child/gate. This complementary case stops that context itself.
}
