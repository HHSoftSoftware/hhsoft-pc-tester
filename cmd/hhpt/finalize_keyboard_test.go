package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func completeKeyboardEvidence() inputtest.KeyboardResult {
	actions := []inputtest.KeyboardAction{}
	names := []string{"left_shift", "right_shift", "up", "down", "left", "right"}
	for _, name := range names {
		actions = append(actions, inputtest.KeyboardAction{Name: name, PressObserved: true, ReleaseObserved: true, PairCompleted: true})
	}
	return inputtest.KeyboardResult{Schema: inputtest.KeyboardSchema, Status: inputtest.StatusPass, StopReason: "completed", LimitMilliseconds: 30000, CleanupConfirmed: true,
		Devices: []inputtest.KeyboardDeviceResult{{Interface: "event2", Status: inputtest.StatusPass, Supported: true, Readable: true,
			Identity: &inputtest.KeyboardIdentity{Bus: 3, Vendor: 0x1234, Product: 0x5678, Version: 0x100}, Capabilities: &inputtest.KeyboardCapabilities{EventKey: true, FixedKeys: names},
			Actions: actions, BaselineNeutral: true, FinalNeutral: true, CleanupConfirmed: true}}}
}

func keyboardInventoryFixture() inventory.Component {
	var low, high uint64
	for _, code := range []int{42, 54, 103, 108, 105, 106} {
		if code < 64 {
			low |= 1 << uint(code)
		} else {
			high |= 1 << uint(code-64)
		}
	}
	return inventory.NewComponent(inventory.ComponentInput, "input:keyboard", map[string]string{"category": "keyboard", "handlers": "kbd event2", "bus": "0003", "vendor": "1234", "product": "5678", "version": "0100", "bitmap_word_bits": "64", "ev_capabilities": "3", "key_capabilities": fmt.Sprintf("%x %x", high, low)})
}

func TestFinalizeKeyboardRejectsUnboundRetainedPassAndCleanupBeforeSigning(t *testing.T) {
	for _, condition := range []string{"valid without live gate", "wrong interface", "wrong identity", "aggregate error foreign pass", "partial actions", "uncertain cleanup", "malformed JSON", "unreadable existing", "changed hardware"} {
		t.Run(condition, func(t *testing.T) {
			directory := t.TempDir()
			key := bytes.Repeat([]byte{0x45}, 32)
			keyPath := writeTestKey(t, directory, "fingerprint.key", key)
			initial := inventory.NewSnapshot([]inventory.Component{keyboardInventoryFixture()})
			final := inventory.NewSnapshot([]inventory.Component{keyboardInventoryFixture()})
			frozen, err := plan.BuildWithSelection(initial, plan.ProfileQuick, "x86_64", key, "ephemeral-session", []string{"input.readiness.quick", "inspection.exterior.quick"})
			if err != nil {
				t.Fatal(err)
			}
			value := completeKeyboardEvidence()
			switch condition {
			case "wrong interface":
				value.Devices[0].Interface = "event9"
			case "wrong identity":
				value.Devices[0].Identity.Vendor++
			case "aggregate error foreign pass":
				value.Status = inputtest.StatusError
				value.StopReason = "error"
				value.Detail = "incomplete_actions"
				value.Devices[0].Interface = "event9"
			case "partial actions":
				value.Devices[0].Actions[0].PairCompleted = false
			case "uncertain cleanup":
				value.Status = inputtest.StatusError
				value.StopReason = "cleanup_unconfirmed"
				value.Detail = "cleanup_unconfirmed"
				value.CleanupConfirmed = false
			case "changed hardware":
				final.Components = nil
			}
			encoded, _ := json.Marshal(frozen)
			planPath := filepath.Join(directory, "plan.json")
			if err := os.WriteFile(planPath, encoded, 0600); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(value)
			if condition == "malformed JSON" {
				raw = []byte(`{"schema":"hhsoft.pc-tester.keyboard-functional/v1","status":"error"}`)
				value.Status = inputtest.StatusError
			}
			if condition == "unreadable existing" {
				if err := os.Mkdir(filepath.Join(directory, "keyboard.json"), 0700); err != nil {
					t.Fatal(err)
				}
				value.Status = inputtest.StatusError
			} else if err := os.WriteFile(filepath.Join(directory, "keyboard.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			reportPath := filepath.Join(directory, "report.hhpcr")
			arguments := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "keyboard-fixture", "-output", reportPath}
			for _, definition := range currentResultDefinitionsForTest(t, frozen) {
				for _, row := range frozen.Tests {
					if row.ID == definition.id {
						status := string(row.Status)
						if row.ID == "input.keyboard.quick" {
							status = string(value.Status)
						}
						arguments = append(arguments, "-outcome", row.ID+"="+status)
					}
				}
			}
			var out, diagnostic bytes.Buffer
			entropy := &pointerUnexpectedEntropy{}
			var gate sessionFinalGate
			if condition != "valid without live gate" {
				gate = func(context.Context) (sessionJournal, error) {
					t.Fatal("invalid keyboard reached the live gate")
					return sessionJournal{}, nil
				}
			}
			code := runFinalizeTestOwned(t, t.Context(), arguments, &out, &diagnostic, func(context.Context) (inventory.Snapshot, error) { return final, nil }, entropy, time.Now, gate).Code
			want := 1
			if condition == "uncertain cleanup" {
				want = workerCleanupUncertainExitCode
			}
			if code != want || entropy.calls != 0 || out.Len() != 0 {
				t.Fatalf("invalid keyboard crossed signing boundary: code=%d entropy=%d %s", code, entropy.calls, diagnostic.String())
			}
			if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
				t.Fatal("rejected keyboard report was written")
			}
		})
	}
}
