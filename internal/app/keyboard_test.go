package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func keyboardRow(t *testing.T, payload *InventoryPayload) *TestResult {
	t.Helper()
	for index := range payload.TestResults {
		if payload.TestResults[index].ID == "input.keyboard.quick" {
			return &payload.TestResults[index]
		}
	}
	t.Fatal("synthetic keyboard row unavailable")
	return nil
}

func changeKeyboard(t *testing.T, payload *InventoryPayload, change func(*inputtest.KeyboardResult)) {
	t.Helper()
	row := keyboardRow(t, payload)
	var result inputtest.KeyboardResult
	if json.Unmarshal(row.Evidence, &result) != nil {
		t.Fatal("invalid synthetic keyboard evidence")
	}
	change(&result)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	row.Evidence = encoded
}

func TestValidatedKeyboardBindsInventoryAndReturnsIndependentEvidence(t *testing.T) {
	payload := thermalFixture(t, "session-v5-keyboard-pass")
	before, _ := json.Marshal(payload)
	result, err := ValidatedKeyboard(payload)
	if err != nil || result == nil || result.Status != inputtest.StatusPass || len(result.Devices) != 1 || len(payload.TestResults) != 22 {
		t.Fatalf("valid keyboard report rejected: %v", err)
	}
	result.Devices[0].Actions[0].PairCompleted = false
	result.Devices[0].Capabilities.FixedKeys[0] = "unrelated"
	again, err := ValidatedKeyboard(payload)
	after, _ := json.Marshal(payload)
	if err != nil || !again.Devices[0].Actions[0].PairCompleted || again.Devices[0].Capabilities.FixedKeys[0] != "left_shift" || !bytes.Equal(before, after) {
		t.Fatal("typed keyboard view aliased or modified signed evidence")
	}
	for _, name := range []string{"historical-session-v3", "session-v4-unavailable", "session-v4-global-heat", "session-v4-local-heat"} {
		if view, err := ValidatedKeyboard(thermalFixture(t, name)); err != nil || view != nil {
			t.Fatalf("historical keyboard definition changed: %v", err)
		}
	}
}

func TestKeyboardClaimsRequireV5DefinitionAndMatchingFinalInventory(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, *InventoryPayload)
	}{
		{"direct capture", func(t *testing.T, p *InventoryPayload) { p.CoverageScope = CoverageDirectCapture }},
		{"opaque profile", func(t *testing.T, p *InventoryPayload) { p.CoverageScope = "" }},
		{"wrong test", func(t *testing.T, p *InventoryPayload) { keyboardRow(t, p).ID = "input.touchpad.quick" }},
		{"aliased test ID", func(t *testing.T, p *InventoryPayload) { keyboardRow(t, p).ID = "INPUT.KEYBOARD.QUICK" }},
		{"old definition", func(t *testing.T, p *InventoryPayload) { keyboardRow(t, p).Version = 1 }},
		{"future definition", func(t *testing.T, p *InventoryPayload) { keyboardRow(t, p).Version = 3 }},
		{"version relabel", func(t *testing.T, p *InventoryPayload) {
			mutateIntegrity(t, p, func(v *sessionevidence.IntegrityEvidence) { v.Session.Schema = sessionevidence.SchemaV4 })
		}},
		{"missing inventory", func(t *testing.T, p *InventoryPayload) { p.Components = nil }},
		{"missing observed interface", func(t *testing.T, p *InventoryPayload) {
			second := p.Components[0]
			second.ID = "event1"
			second.Attributes = append([]inventory.Attribute(nil), second.Attributes...)
			for i := range second.Attributes {
				if second.Attributes[i].Name == "interface" {
					second.Attributes[i].Value = "event1"
				}
				if second.Attributes[i].Name == "handlers" {
					second.Attributes[i].Value = "kbd event1"
				}
			}
			p.Components = append(p.Components, second)
		}},
		{"changed numeric identity", func(t *testing.T, p *InventoryPayload) {
			changeKeyboard(t, p, func(v *inputtest.KeyboardResult) { v.Devices[0].Identity.Product++ })
		}},
		{"missing fixed key", func(t *testing.T, p *InventoryPayload) {
			for i := range p.Components[0].Attributes {
				if p.Components[0].Attributes[i].Name == "key_capabilities" {
					p.Components[0].Attributes[i].Value = "40000000000"
				}
			}
		}},
		{"unbound bitmap layout", func(t *testing.T, p *InventoryPayload) {
			for i := range p.Components[0].Attributes {
				if p.Components[0].Attributes[i].Name == "bitmap_word_bits" {
					p.Components[0].Attributes[i].Value = "32"
				}
			}
		}},
		{"incomplete pass", func(t *testing.T, p *InventoryPayload) {
			changeKeyboard(t, p, func(v *inputtest.KeyboardResult) {
				v.Devices[0].Actions[3].PairCompleted = false
				v.Devices[0].Actions[3].ReleaseObserved = false
			})
		}},
		{"unsafe cleanup", func(t *testing.T, p *InventoryPayload) {
			changeKeyboard(t, p, func(v *inputtest.KeyboardResult) { v.CleanupConfirmed = false })
		}},
		{"duration outside worker", func(t *testing.T, p *InventoryPayload) {
			changeKeyboard(t, p, func(v *inputtest.KeyboardResult) { v.DurationMilliseconds = 7 })
		}},
		{"typed text field", func(t *testing.T, p *InventoryPayload) {
			row := keyboardRow(t, p)
			row.Evidence = bytes.Replace(row.Evidence, []byte(`"schema":`), []byte(`"typed_text":"synthetic-forbidden-canary","schema":`), 1)
		}},
	}
	for _, fixture := range cases {
		t.Run(fixture.name, func(t *testing.T) {
			payload := thermalFixture(t, "session-v5-keyboard-pass")
			fixture.change(t, &payload)
			if ValidateCoverage(payload) == nil {
				t.Fatal("forged keyboard claim accepted")
			}
			if view, err := ValidatedKeyboard(payload); err == nil || view != nil {
				t.Fatal("forged keyboard claim produced a view")
			}
		})
	}
}

func TestKeyboardPartialPassStillRequiresFinalInventoryBinding(t *testing.T) {
	payload := thermalFixture(t, "session-v5-keyboard-pass")
	keyboardRow(t, &payload).Status = TestAborted
	changeKeyboard(t, &payload, func(result *inputtest.KeyboardResult) {
		result.Status = inputtest.StatusAborted
		result.StopReason = "operator_cancelled"
		result.Detail = "operator_cancelled"
	})
	mutateIntegrity(t, &payload, func(value *sessionevidence.IntegrityEvidence) {
		value.Session.StopReason = "operator_cancelled"
		value.Session.Workers[4].State = "interrupted"
		value.Session.Workers[4].Outcome = "aborted"
	})
	view, err := ValidatedKeyboard(payload)
	if err != nil || view == nil || view.Devices[0].Status != inputtest.StatusPass {
		t.Fatalf("honest retained device pass rejected: %v", err)
	}
	changeKeyboard(t, &payload, func(result *inputtest.KeyboardResult) { result.Devices[0].Identity.Bus++ })
	if ValidateCoverage(payload) == nil {
		t.Fatal("aggregate cancellation hid an unbound device pass")
	}
}

func TestKeyboardRawClaimSelectionCannotLoseDefinitionOrFunctionalEvidence(t *testing.T) {
	for _, raw := range []string{
		`{"schema":"hhsoft.pc-tester.keyboard-functional/v1"}`,
		`{"SCHEMA":"HHSOFT.PC-TESTER.KEYBOARD-FUNCTIONAL/V1"}`,
		`{"schema":"hhsoft.pc-tester.keyboard-functional/v1","schema":"historical"}`,
		`{"evidence":{"schem\u0061":"hhsoft.pc-tester.keyboard-functional/v1"},"evidence":null}`,
		`{"test_results":{"id":"input.keyboard.quick","version":2}}`,
		`{"test_results":[{"id":"input.keyboard.quick","id":"cpu.quick","version":2,"version":1}]}`,
		`{"test_results":[{"ver\u0073ion":2,"ID":"INPUT.KEYBOARD.QUICK"}],"test_results":[]}`,
		`{"id":"input.keyboard.quick","version":3}`,
		`{"id":"input.keyboard.quick","version":2.0}`,
		`{"id":"input.keyboard.quick","version":"2"}`,
		`{"id":"input.keyboard.quick","version":null}`,
		`{"schema":"hhsoft.pc-tester.keyboard-functional/v1"`,
	} {
		if !HasKeyboardClaim([]byte(raw)) {
			t.Fatal("new or hidden keyboard claim lost strict validation")
		}
	}
	for _, raw := range [][]byte{nil, {}, []byte(`{"id":"input.keyboard.quick","version":1}`), []byte(`{"id":"cpu.quick","version":2}`), []byte(`{"schema":"hhsoft.pc-tester.quick-session/v3"}`)} {
		if HasKeyboardClaim(raw) {
			t.Fatal("historical row acquired a keyboard claim")
		}
	}
	if !HasSessionThermalClaim([]byte(`{"session":{"schema":"hhsoft.pc-tester.quick-session/v5"}}`)) {
		t.Fatal("v5 escaped strict thermal validation")
	}
}

func standaloneKeyboardNote(t *testing.T, status PlannedStatus) InventoryPayload {
	t.Helper()
	payload := thermalFixture(t, "session-v4-unavailable")
	mutateIntegrity(t, &payload, func(value *sessionevidence.IntegrityEvidence) { value.Session = nil })
	row := keyboardRow(t, &payload)
	row.Version, row.PlannedStatus, row.Status = 2, status, TestSkip
	if status == PlannedMissingDriver {
		row.Status = TestInconclusive
	}
	row.Evidence, _ = json.Marshal(map[string]string{"schema": "hhsoft.pc-tester.execution-note/v1", "status": string(row.Status), "source_status": string(status), "detail": "test was not scheduled"})
	return payload
}

func TestStandaloneKeyboardNotesRemainStrictAndHaveNoFunctionalView(t *testing.T) {
	for _, status := range []PlannedStatus{PlannedExcluded, PlannedNotPresent, PlannedUnsupported, PlannedMissingDriver} {
		payload := standaloneKeyboardNote(t, status)
		if view, err := ValidatedKeyboard(payload); err != nil || view != nil {
			t.Fatalf("honest standalone nonexecution note rejected: %v", err)
		}
		if view, err := ValidatedSessionThermal(payload); err != nil || view != nil {
			t.Fatal("standalone note acquired a monitored-session claim")
		}
	}
	for _, defect := range []string{"direct capture", "scheduled", "functional token", "unknown field", "hidden field", "historical session", "aliased historical schema", "null session"} {
		t.Run(defect, func(t *testing.T) {
			payload := standaloneKeyboardNote(t, PlannedExcluded)
			row := keyboardRow(t, &payload)
			switch defect {
			case "direct capture":
				payload.CoverageScope = CoverageDirectCapture
			case "scheduled":
				row.PlannedStatus = PlannedScheduled
			case "functional token":
				row.Evidence = bytes.Replace(row.Evidence, []byte("test was not scheduled"), []byte(inputtest.KeyboardSchema), 1)
			case "unknown field":
				row.Evidence = bytes.Replace(row.Evidence, []byte(`"detail":`), []byte(`"devices":[],"detail":`), 1)
			case "hidden field":
				row.Evidence = bytes.Replace(row.Evidence, []byte(`"status":`), []byte(`"stat\u0075s":"pass","status":`), 1)
			case "historical session":
				payload.TestResults[len(payload.TestResults)-1].Evidence = []byte(`{"session":{"schema":"hhsoft.pc-tester.quick-session/v3"}}`)
			case "aliased historical schema":
				payload.TestResults[len(payload.TestResults)-1].Evidence = []byte(`{"schema":"hhsoft.pc-tester.quick-\u017fession/v3"}`)
			case "null session":
				payload.TestResults[len(payload.TestResults)-1].Evidence = []byte(`{"session":null}`)
			}
			if ValidateCoverage(payload) == nil {
				t.Fatal("nonexecution exception accepted new or ambiguous observations")
			}
		})
	}
}

func TestKeyboardKeepsHistoricalFixtureBytes(t *testing.T) {
	for name, pin := range map[string]string{
		"historical-session-v3":  "2abd26937f2c52fb15b9dcbac991e0b201dded9333c8d8a55b942e253a8c7648",
		"session-v4-global-heat": "196e214c0ab86bfc4d37b2e48ebcb324c9a66a9ffb35cd4f4c0502778ae8f4f7",
		"session-v4-local-heat":  "57ef4ac1a2bf04f43bb5d64ebba0fae22ec7aee8ce333dfcc8750db3abf2fc8e",
		"session-v4-unavailable": "105e9bf0b0b62e0cf3a58744e7b0384cac190ef5132f09b85c4c5d993256fc78",
	} {
		encoded, err := os.ReadFile("testdata/" + name + ".json")
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(encoded)) != pin {
			t.Fatal("historical report fixture changed")
		}
	}
}
