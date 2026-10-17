package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
)

func TestSafetyCommandPreservesFailureEvidenceWithoutSafeSummary(t *testing.T) {
	for _, result := range []safemode.Result{
		{Schema: safemode.Schema, Status: "fail", StorageObserved: true, NetworkObserved: true, WritableBlockMounts: 1},
		{Schema: safemode.Schema, Status: "fail", StorageObserved: true, NetworkObserved: true, ActiveNonLoopInterfaces: 1},
		{Schema: safemode.Schema, Status: "error", NetworkObserved: true},
		{Schema: safemode.Schema, Status: "pass", StorageObserved: true}, // Contradictory injected observer.
	} {
		t.Run(result.Status+"/"+result.StorageDetail, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "safety.json")
			var stdout, stderr bytes.Buffer
			code := runSafetyWith(t.Context(), []string{"-output", path, "-summary"}, &stdout, &stderr, func(context.Context) safemode.Result { return result })
			if code != 1 || stdout.Len() != 0 {
				t.Fatalf("unsafe result produced successful summary: code=%d stdout=%q", code, stdout.String())
			}
			encoded, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var recorded safemode.Result
			if err := json.Unmarshal(encoded, &recorded); err != nil || recorded != result {
				t.Fatalf("failure evidence changed or was lost: %#v, %v", recorded, err)
			}
		})
	}
}

func TestSafetySummaryRequiresCompleteObservationsAndExclusiveEvidenceWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "safety.json")
	observe := func(context.Context) safemode.Result {
		return safemode.Result{Schema: safemode.Schema, Status: "pass", StorageObserved: true, NetworkObserved: true}
	}
	var stdout, stderr bytes.Buffer
	args := []string{"-output", path, "-summary"}
	if code := runSafetyWith(t.Context(), args, &stdout, &stderr, observe); code != 0 || stdout.String() != "writable_block_mounts=0 active_non_loop_interfaces=0\n" {
		t.Fatalf("safe observation was not summarized: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := runSafetyWith(t.Context(), args, &stdout, &stderr, observe); code != 1 || stdout.Len() != 0 {
		t.Fatal("existing evidence was overwritten or produced a fresh safe summary")
	}
	if current, err := os.ReadFile(path); err != nil || !bytes.Equal(current, original) {
		t.Fatal("existing safety evidence changed")
	}
	for _, invalid := range [][]string{{"-summary"}, {"-root", "/fixture"}, {"/dev/example"}} {
		if code := runSafetyWith(t.Context(), invalid, &stdout, &stderr, func(context.Context) safemode.Result {
			t.Fatal("invalid invocation inspected host")
			return safemode.Result{}
		}); code != 2 {
			t.Fatalf("invalid invocation accepted: %s", strings.Join(invalid, " "))
		}
	}
}
