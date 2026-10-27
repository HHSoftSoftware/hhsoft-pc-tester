package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/pointerevidence"
)

func completePointerEvidence() inputtest.PointerResult {
	return inputtest.PointerResult{
		Schema: inputtest.PointerSchema, Status: inputtest.StatusPass, StopReason: "completed",
		LimitMilliseconds: 30000, DurationMilliseconds: 100,
		Devices: []inputtest.PointerDeviceResult{{
			Interface: "event0", Status: inputtest.StatusPass, Supported: true, Readable: true,
			Identity:     &inputtest.PointerIdentity{Bus: 3, Vendor: 0x1234, Product: 0x5678, Version: 0x100},
			Capabilities: &inputtest.PointerCapabilities{Key: true, Relative: true, RelativeX: true, RelativeY: true, Buttons: []string{"left", "right", "middle"}, WheelAxes: []inputtest.PointerWheelCapability{}},
			XPositive:    1, XNegative: 1, YPositive: 1, YNegative: 1, Frames: 4,
			Wheels: []inputtest.PointerWheelResult{},
			Buttons: []inputtest.PointerButtonResult{
				{Button: "left", Presses: 1, Releases: 1, Pairs: 1},
				{Button: "right", Presses: 1, Releases: 1, Pairs: 1},
				{Button: "middle", Presses: 1, Releases: 1, Pairs: 1},
			},
		}},
	}
}

func withPointerWheelCriteria(value *inputtest.PointerResult, snapshots ...*inventory.Snapshot) {
	value.Devices[0].Capabilities.WheelAxes = []inputtest.PointerWheelCapability{
		{Axis: "vertical", Legacy: true, HighResolution: true}, {Axis: "horizontal", Legacy: true},
	}
	value.Devices[0].Wheels = []inputtest.PointerWheelResult{
		{Axis: "vertical", PositiveFrames: 1, NegativeFrames: 1}, {Axis: "horizontal", PositiveFrames: 1, NegativeFrames: 1},
	}
	for _, snapshot := range snapshots {
		attributes := map[string]string{}
		for _, attribute := range snapshot.Components[0].Attributes {
			attributes[attribute.Name] = attribute.Value
		}
		// Independently specified kernel bits: X/Y, coarse horizontal/vertical,
		// and high-resolution vertical. No horizontal high-resolution bit.
		attributes["rel_capabilities"] = "943"
		snapshot.Components[0] = inventory.NewComponent(inventory.ComponentInput, "input:0", attributes)
	}
}

func TestPointerEvidenceRequiresCompleteIndependentInterfaceCoverage(t *testing.T) {
	if status, err := testEvidenceStatus(encodeEvidenceFixture(t, completePointerEvidence()), inputtest.PointerSchema); err != nil || status != "pass" {
		t.Fatalf("valid pointer evidence refused: %s %v", status, err)
	}
	for _, item := range []struct {
		name   string
		change func(*inputtest.PointerResult)
	}{
		{"no interfaces", func(v *inputtest.PointerResult) { v.Devices = nil }},
		{"duplicate interface", func(v *inputtest.PointerResult) { v.Devices = append(v.Devices, v.Devices[0]) }},
		{"noncanonical interface", func(v *inputtest.PointerResult) { v.Devices[0].Interface = "event00" }},
		{"absolute interface", func(v *inputtest.PointerResult) { v.Devices[0].Interface = "/dev/input/event0" }},
		{"missing identity", func(v *inputtest.PointerResult) { v.Devices[0].Identity = nil }},
		{"missing capabilities", func(v *inputtest.PointerResult) { v.Devices[0].Capabilities = nil }},
		{"unsupported device", func(v *inputtest.PointerResult) { v.Devices[0].Supported = false }},
		{"unreadable device", func(v *inputtest.PointerResult) { v.Devices[0].Readable = false }},
		{"mixed absolute device", func(v *inputtest.PointerResult) { v.Devices[0].Capabilities.Absolute = true }},
		{"no key events", func(v *inputtest.PointerResult) { v.Devices[0].Capabilities.Key = false }},
		{"no relative events", func(v *inputtest.PointerResult) { v.Devices[0].Capabilities.Relative = false }},
		{"missing axis", func(v *inputtest.PointerResult) { v.Devices[0].Capabilities.RelativeY = false }},
		{"duplicate advertised button", func(v *inputtest.PointerResult) { v.Devices[0].Capabilities.Buttons = []string{"left", "left"} }},
		{"unknown advertised button", func(v *inputtest.PointerResult) { v.Devices[0].Capabilities.Buttons[2] = "side" }},
		{"missing button coverage", func(v *inputtest.PointerResult) { v.Devices[0].Buttons = v.Devices[0].Buttons[:2] }},
		{"duplicate button coverage", func(v *inputtest.PointerResult) { v.Devices[0].Buttons[1] = v.Devices[0].Buttons[0] }},
		{"unpaired release", func(v *inputtest.PointerResult) { v.Devices[0].Buttons[0].Pairs = 0 }},
		{"button still held", func(v *inputtest.PointerResult) { v.Devices[0].Buttons[0].Presses = 2 }},
		{"invented pair", func(v *inputtest.PointerResult) { v.Devices[0].Buttons[0].Pairs = 2 }},
		{"too many button transitions", func(v *inputtest.PointerResult) { v.Devices[0].Frames = 1 }},
		{"button counter overflow", func(v *inputtest.PointerResult) { v.Devices[0].Buttons[0].Presses = math.MaxUint64 }},
		{"no frames", func(v *inputtest.PointerResult) { v.Devices[0].Frames = 0 }},
		{"missing direction", func(v *inputtest.PointerResult) { v.Devices[0].XNegative = 0 }},
		{"invented motion frames", func(v *inputtest.PointerResult) { v.Devices[0].YPositive = v.Devices[0].Frames }},
		{"motion counter overflow", func(v *inputtest.PointerResult) { v.Devices[0].XNegative = math.MaxUint64 }},
		{"lost events", func(v *inputtest.PointerResult) { v.Devices[0].EvidenceLoss = true }},
		{"incomplete second interface", func(v *inputtest.PointerResult) {
			second := completePointerEvidence().Devices[0]
			second.Interface, second.XPositive = "event1", 0
			v.Devices = append(v.Devices, second)
		}},
		{"nonpass interface", func(v *inputtest.PointerResult) { v.Devices[0].Status = inputtest.StatusInconclusive }},
		{"contradictory device detail", func(v *inputtest.PointerResult) { v.Devices[0].Detail = "incomplete observation" }},
		{"contradictory result detail", func(v *inputtest.PointerResult) { v.Detail = "cancelled observation" }},
		{"cancelled late pass", func(v *inputtest.PointerResult) { v.StopReason = "operator_cancelled" }},
		{"deadline late pass", func(v *inputtest.PointerResult) { v.StopReason = "deadline" }},
		{"expired observation pass", func(v *inputtest.PointerResult) { v.StopReason = "observation_timeout" }},
		{"elapsed beyond limit", func(v *inputtest.PointerResult) { v.DurationMilliseconds = v.LimitMilliseconds + 1 }},
		{"zero limit", func(v *inputtest.PointerResult) { v.LimitMilliseconds = 0 }},
		{"negative elapsed", func(v *inputtest.PointerResult) { v.DurationMilliseconds = -1 }},
	} {
		t.Run(item.name, func(t *testing.T) {
			value := completePointerEvidence()
			item.change(&value)
			if _, err := testEvidenceStatus(encodeEvidenceFixture(t, value), inputtest.PointerSchema); err == nil {
				t.Fatal("accepted missing or contradictory pointer evidence")
			}
		})
	}
}

func TestPointerEvidenceRejectsAmbiguousNestedObservationFields(t *testing.T) {
	valid := encodeEvidenceFixture(t, completePointerEvidence())
	for _, item := range []struct{ old, replacement string }{
		{`"vendor":4660`, `"vendor":1,"vendor":4660`},
		{`"vendor":4660`, `"Vendor":4660`},
		{`"vendor":4660`, `"vendor":null`},
		{`"vendor":4660,`, ``},
		{`"vendor":4660`, `"vendor":4660,"unexpected":true`},
		{`"key":true`, `"key":false,"key":true`},
		{`"key":true`, `"Key":true`},
		{`"relative_x":true,`, ``},
		{`"x_negative":1,`, ``},
		{`"pairs":1`, `"pairs":0,"pairs":1`},
		{`"stop_reason":"completed",`, ``},
	} {
		altered := bytes.Replace(valid, []byte(item.old), []byte(item.replacement), 1)
		if bytes.Equal(altered, valid) {
			t.Fatalf("fixture field not found: %s", item.old)
		}
		if _, err := testEvidenceStatus(altered, inputtest.PointerSchema); err == nil {
			t.Fatalf("accepted ambiguous pointer field: %s", item.old)
		}
	}
}

func TestPointerEvidencePreservesHonestPartialAndStoppedResults(t *testing.T) {
	for _, reason := range []string{"operator_cancelled", "deadline", "observation_timeout", "error"} {
		value := completePointerEvidence()
		value.StopReason = reason
		value.Status = inputtest.StatusError
		if reason == "operator_cancelled" {
			value.Status = inputtest.StatusAborted
		} else if reason == "observation_timeout" {
			value.Status = inputtest.StatusInconclusive
		}
		device := &value.Devices[0]
		device.Status, device.XNegative = value.Status, 0
		device.Buttons[0].Presses, device.Buttons[0].Releases, device.Buttons[0].Pairs = 1, 0, 0
		if status, err := testEvidenceStatus(encodeEvidenceFixture(t, value), inputtest.PointerSchema); err != nil || status != string(value.Status) {
			t.Fatalf("honest partial result rejected: %s %v", reason, err)
		}
	}
	unsupported := inputtest.PointerResult{Schema: inputtest.PointerSchema, Status: inputtest.StatusError, StopReason: "error", Detail: "Linux input devices required"}
	if _, err := testEvidenceStatus(encodeEvidenceFixture(t, unsupported), inputtest.PointerSchema); err != nil {
		t.Fatalf("unsupported-platform error rejected: %v", err)
	}
	beforeCriteria := completePointerEvidence()
	beforeCriteria.Status, beforeCriteria.StopReason = inputtest.StatusInconclusive, "error"
	device := &beforeCriteria.Devices[0]
	device.Status, device.Detail = inputtest.StatusInconclusive, "unsupported event record layout"
	device.Frames, device.XPositive, device.XNegative, device.YPositive, device.YNegative = 0, 0, 0, 0, 0
	device.Buttons = nil
	if _, err := testEvidenceStatus(encodeEvidenceFixture(t, beforeCriteria), inputtest.PointerSchema); err != nil {
		t.Fatalf("observation stopped before criteria initialization was rejected: %v", err)
	}
}

func pointerInventoryFixture(sysfs, kernel32 bool) inventory.Snapshot {
	keys := "70000 0 0 0 0"
	if kernel32 {
		keys = "70000 0 0 0 0 0 0 0 0"
	}
	attributes := map[string]string{"category": "pointer", "bus": "0003", "version": "0100", "key_capabilities": keys, "bitmap_word_bits": "64"}
	if kernel32 {
		attributes["bitmap_word_bits"] = "32"
	}
	id := "input:0"
	if sysfs {
		id = "input:event0"
		attributes["interface"], attributes["vendor_id"], attributes["product_id"] = "event0", "1234", "5678"
		attributes["event_capabilities"], attributes["relative_capabilities"] = "7", "3"
	} else {
		attributes["handlers"], attributes["vendor"], attributes["product"] = "mouse0 event0", "1234", "5678"
		attributes["ev_capabilities"], attributes["rel_capabilities"] = "7", "3"
	}
	return inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentInput, id, attributes)})
}

func TestPointerPassBindsEveryInterfaceToFrozenAndFinalHardware(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	build := func(snapshot inventory.Snapshot) plan.Plan {
		t.Helper()
		p, err := plan.Build(snapshot, plan.ProfileQuick, "x86_64", key, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, sysfs := range []bool{false, true} {
		for _, kernel32 := range []bool{false, true} {
			snapshot := pointerInventoryFixture(sysfs, kernel32)
			frozen := build(snapshot)
			var pointer plan.Test
			for _, test := range frozen.Tests {
				if test.ID == "input.pointer.quick" {
					pointer = test
				}
			}
			if !validatePointerPlanEvidence(encodeEvidenceFixture(t, completePointerEvidence()), pointer, frozen, frozen, snapshot) {
				t.Fatalf("valid kernel bitmap layout rejected: sysfs=%v kernel32=%v", sysfs, kernel32)
			}
		}
	}
	initial := pointerInventoryFixture(false, false)
	frozen := build(initial)
	var pointer plan.Test
	for _, test := range frozen.Tests {
		if test.ID == "input.pointer.quick" {
			pointer = test
		}
	}
	for _, item := range []struct {
		name   string
		change func(*inputtest.PointerResult, *inventory.Snapshot)
	}{
		{"wrong interface", func(v *inputtest.PointerResult, _ *inventory.Snapshot) { v.Devices[0].Interface = "event1" }},
		{"wrong numeric identity", func(v *inputtest.PointerResult, _ *inventory.Snapshot) { v.Devices[0].Identity.Product++ }},
		{"extra interface", func(v *inputtest.PointerResult, _ *inventory.Snapshot) {
			second := completePointerEvidence().Devices[0]
			second.Interface = "event1"
			v.Devices = append(v.Devices, second)
		}},
		{"known interface replaced", func(_ *inputtest.PointerResult, s *inventory.Snapshot) {
			attributes := map[string]string{}
			for _, attribute := range s.Components[0].Attributes {
				attributes[attribute.Name] = attribute.Value
			}
			attributes["product"] = "9999"
			s.Components[0] = inventory.NewComponent(inventory.ComponentInput, s.Components[0].ID, attributes)
		}},
		{"pointer removed", func(_ *inputtest.PointerResult, s *inventory.Snapshot) { s.Components = nil }},
		{"pointer added", func(_ *inputtest.PointerResult, s *inventory.Snapshot) {
			s.Components = append(s.Components, inventory.NewComponent(inventory.ComponentInput, "input:1", map[string]string{"category": "pointer", "handlers": "mouse1 event1"}))
		}},
		{"limit exceeds frozen timeout", func(v *inputtest.PointerResult, _ *inventory.Snapshot) { v.LimitMilliseconds = 90001 }},
	} {
		t.Run(item.name, func(t *testing.T) {
			value, final := completePointerEvidence(), pointerInventoryFixture(false, false)
			item.change(&value, &final)
			if validatePointerPlanEvidence(encodeEvidenceFixture(t, value), pointer, frozen, build(final), final) {
				t.Fatal("stale, incomplete, or mismatched pointer evidence accepted")
			}
		})
	}
}

func TestPointerCapabilityBindingRejectsContradictoryInventory(t *testing.T) {
	for _, item := range []struct{ attribute, value string }{
		{"ev_capabilities", "f"}, {"ev_capabilities", "3"}, {"ev_capabilities", "6"}, {"rel_capabilities", "1"},
		{"key_capabilities", "10000 0 0 0 0"}, {"key_capabilities", "not-hex"},
		{"vendor", "ffff"}, {"vendor", "12345"}, {"vendor_id", "ffff"},
		{"bitmap_word_bits", ""}, {"bitmap_word_bits", "unknown"}, {"bitmap_word_bits", "032"},
	} {
		snapshot := pointerInventoryFixture(false, false)
		attributes := map[string]string{}
		for _, attribute := range snapshot.Components[0].Attributes {
			attributes[attribute.Name] = attribute.Value
		}
		attributes[item.attribute] = item.value
		component := inventory.NewComponent(inventory.ComponentInput, "input:0", attributes)
		if pointerevidence.ValidateInventoryBinding(completePointerEvidence(), []inventory.Component{component}) == nil {
			t.Fatalf("contradictory inventory accepted: %s", item.attribute)
		}
	}
}

func TestPointerCannotChooseWeakerBitmapLayout(t *testing.T) {
	component := inventory.NewComponent(inventory.ComponentInput, "input:0", map[string]string{
		"category": "pointer", "handlers": "mouse0 event0", "bitmap_word_bits": "32",
		"ev_capabilities": "7", "rel_capabilities": "3",
		"bus": "0003", "vendor": "1234", "product": "5678", "version": "0100",
		// In a 32-bit kernel this advertises all primary buttons plus KEY_FILE.
		// The same words misread as 64-bit words appear to advertise only left.
		"key_capabilities": "70000 0 0 0 10000 0 0 0 0",
	})
	value := completePointerEvidence()
	if pointerevidence.ValidateInventoryBinding(value, []inventory.Component{component}) != nil {
		t.Fatal("complete primary-button evidence rejected for the bound kernel layout")
	}
	value.Devices[0].Capabilities.Buttons = []string{"left"}
	value.Devices[0].Buttons = value.Devices[0].Buttons[:1]
	if pointerevidence.ValidateResult(value) != nil {
		t.Fatal("independent weaker-layout fixture is not internally valid")
	}
	if pointerevidence.ValidateInventoryBinding(value, []inventory.Component{component}) == nil {
		t.Fatal("submitted evidence selected a weaker kernel bitmap interpretation")
	}
}

func TestPointerBindingAcceptsEveryIndependentSupportedInterface(t *testing.T) {
	for _, kernel32 := range []bool{false, true} {
		snapshot := pointerInventoryFixture(false, kernel32)
		attributes := map[string]string{}
		for _, attribute := range snapshot.Components[0].Attributes {
			attributes[attribute.Name] = attribute.Value
		}
		attributes["handlers"] = "mouse1 event1"
		attributes["key_capabilities"] = "10000 0 0 0 0"
		if kernel32 {
			attributes["key_capabilities"] = "10000 0 0 0 0 0 0 0 0"
		}
		snapshot.Components = append(snapshot.Components, inventory.NewComponent(inventory.ComponentInput, "input:1", attributes))
		frozen, err := plan.Build(snapshot, plan.ProfileQuick, "x86_64", bytes.Repeat([]byte{0x42}, 32), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		var pointer plan.Test
		for _, row := range frozen.Tests {
			if row.ID == "input.pointer.quick" {
				pointer = row
			}
		}
		value := completePointerEvidence()
		second := completePointerEvidence().Devices[0]
		second.Interface = "event1"
		second.Capabilities.Buttons = []string{"left"}
		second.Buttons = second.Buttons[:1]
		value.Devices = append(value.Devices, second)
		if !validatePointerPlanEvidence(encodeEvidenceFixture(t, value), pointer, frozen, frozen, snapshot) {
			t.Fatal("rejected independent complete observations for three-button and one-button pointers")
		}
		value.Devices[1].Buttons[0].Pairs = 0
		if validatePointerPlanEvidence(encodeEvidenceFixture(t, value), pointer, frozen, frozen, snapshot) {
			t.Fatal("one interface's completed observations concealed another interface's missing paired button")
		}
	}
}

type pointerUnexpectedEntropy struct{ calls int }

func (reader *pointerUnexpectedEntropy) Read([]byte) (int, error) {
	reader.calls++
	return 0, errors.New("unexpected signing attempt")
}

func TestFinalizeRejectsUnboundOrIncompletePointerPassBeforeSigning(t *testing.T) {
	for _, item := range []struct {
		name    string
		change  func(*inputtest.PointerResult, *inventory.Snapshot, *inventory.Snapshot)
		collect bool
	}{
		{"unpaired button", func(v *inputtest.PointerResult, _, _ *inventory.Snapshot) { v.Devices[0].Buttons[0].Pairs = 0 }, false},
		{"missing wheel direction", func(v *inputtest.PointerResult, initial, final *inventory.Snapshot) {
			withPointerWheelCriteria(v, initial, final)
			v.Devices[0].Wheels[1].NegativeFrames = 0
		}, false},
		{"unadvertised wheel claim", func(v *inputtest.PointerResult, _, _ *inventory.Snapshot) {
			withPointerWheelCriteria(v)
		}, true},
		{"omitted advertised wheels", func(v *inputtest.PointerResult, initial, final *inventory.Snapshot) {
			withPointerWheelCriteria(v, initial, final)
			v.Devices[0].Capabilities.WheelAxes = []inputtest.PointerWheelCapability{}
			v.Devices[0].Wheels = []inputtest.PointerWheelResult{}
		}, true},
		{"weaker advertised wheel stream", func(v *inputtest.PointerResult, initial, final *inventory.Snapshot) {
			withPointerWheelCriteria(v, initial, final)
			v.Devices[0].Capabilities.WheelAxes[0].HighResolution = false
		}, true},
		{"retained wheel pass after capability change", func(v *inputtest.PointerResult, initial, _ *inventory.Snapshot) {
			withPointerWheelCriteria(v, initial)
			v.Status, v.StopReason = inputtest.StatusError, "deadline"
		}, true},
		{"null wheel observations", func(v *inputtest.PointerResult, _, _ *inventory.Snapshot) {
			v.Devices[0].Wheels = nil
		}, false},
		{"malformed wheel evidence cannot become an error note", func(v *inputtest.PointerResult, _, _ *inventory.Snapshot) {
			v.Status, v.StopReason = inputtest.StatusError, "error"
			v.Devices[0].Wheels = nil
		}, false},
		{"cancelled late pass", func(v *inputtest.PointerResult, _, _ *inventory.Snapshot) { v.StopReason = "operator_cancelled" }, false},
		{"unsupported absolute device", func(v *inputtest.PointerResult, _, _ *inventory.Snapshot) { v.Devices[0].Capabilities.Absolute = true }, false},
		{"wrong interface", func(v *inputtest.PointerResult, _, _ *inventory.Snapshot) { v.Devices[0].Interface = "event9" }, true},
		{"cancelled aggregate with foreign passed interface", func(v *inputtest.PointerResult, _, _ *inventory.Snapshot) {
			v.Status, v.StopReason = inputtest.StatusAborted, "operator_cancelled"
			v.Devices[0].Interface = "event9"
		}, true},
		{"inconclusive aggregate with weaker passed capabilities", func(v *inputtest.PointerResult, _, _ *inventory.Snapshot) {
			v.Status, v.StopReason = inputtest.StatusInconclusive, "observation_timeout"
			v.Devices[0].Capabilities.Buttons = []string{"left"}
			v.Devices[0].Buttons = v.Devices[0].Buttons[:1]
		}, true},
		{"deadline aggregate with changed completed hardware", func(v *inputtest.PointerResult, _, final *inventory.Snapshot) {
			v.Status, v.StopReason = inputtest.StatusError, "deadline"
			attributes := map[string]string{}
			for _, attribute := range final.Components[0].Attributes {
				attributes[attribute.Name] = attribute.Value
			}
			attributes["key_capabilities"] = "10000 0 0 0 0"
			final.Components[0] = inventory.NewComponent(inventory.ComponentInput, "input:0", attributes)
		}, true},
		{"weaker bitmap interpretation", func(v *inputtest.PointerResult, initial, final *inventory.Snapshot) {
			attributes := map[string]string{}
			for _, attribute := range initial.Components[0].Attributes {
				attributes[attribute.Name] = attribute.Value
			}
			attributes["bitmap_word_bits"] = "32"
			attributes["key_capabilities"] = "70000 0 0 0 10000 0 0 0 0"
			initial.Components[0] = inventory.NewComponent(inventory.ComponentInput, "input:0", attributes)
			final.Components[0] = initial.Components[0]
			v.Devices[0].Capabilities.Buttons = []string{"left"}
			v.Devices[0].Buttons = v.Devices[0].Buttons[:1]
		}, true},
		{"changed pointer identity", func(_ *inputtest.PointerResult, _, final *inventory.Snapshot) {
			attributes := map[string]string{}
			for _, attribute := range final.Components[0].Attributes {
				attributes[attribute.Name] = attribute.Value
			}
			attributes["version"] = "0101"
			final.Components[0] = inventory.NewComponent(inventory.ComponentInput, "input:0", attributes)
		}, true},
		{"missing planned interface", func(_ *inputtest.PointerResult, initial, final *inventory.Snapshot) {
			component := inventory.NewComponent(inventory.ComponentInput, "input:1", map[string]string{"category": "pointer", "handlers": "mouse1 event1"})
			initial.Components = append(initial.Components, component)
			final.Components = append(final.Components, component)
		}, true},
	} {
		t.Run(item.name, func(t *testing.T) {
			directory := t.TempDir()
			key := bytes.Repeat([]byte{0x45}, 32)
			keyPath := writeTestKey(t, directory, "fingerprint.key", key)
			evidence := completePointerEvidence()
			initial, final := pointerInventoryFixture(false, false), pointerInventoryFixture(false, false)
			item.change(&evidence, &initial, &final)
			frozen, err := plan.BuildWithSelection(initial, plan.ProfileQuick, "x86_64", key, "ephemeral-session", []string{"input.readiness.quick"})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(frozen)
			if err != nil {
				t.Fatal(err)
			}
			planPath, reportPath := filepath.Join(directory, "plan.json"), filepath.Join(directory, "report.hhpcr")
			for name, data := range map[string][]byte{planPath: encoded, filepath.Join(directory, "pointer.json"): encodeEvidenceFixture(t, evidence)} {
				if err := os.WriteFile(name, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			arguments := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "pointer-rejection", "-output", reportPath}
			for _, definition := range quickResultDefinitions {
				for _, row := range frozen.Tests {
					if definition.id == row.ID {
						outcome := string(row.Status)
						if row.ID == "input.pointer.quick" {
							outcome = string(evidence.Status)
						}
						arguments = append(arguments, "-outcome", row.ID+"="+outcome)
					}
				}
			}
			collected := false
			collect := func(context.Context) (inventory.Snapshot, error) { collected = true; return final, nil }
			random := &pointerUnexpectedEntropy{}
			var stdout, stderr bytes.Buffer
			if code := runFinalizeWith(t.Context(), arguments, &stdout, &stderr, collect, random, time.Now); code != 1 {
				t.Fatalf("accepted invalid pointer evidence: %d %s", code, stderr.String())
			}
			if collected != item.collect || random.calls != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "validate") {
				t.Fatalf("rejection occurred after the signing boundary: collected=%v entropy=%d stdout=%d stderr=%s", collected, random.calls, stdout.Len(), stderr.String())
			}
			if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
				t.Fatal("report exists after rejected pointer evidence")
			}
		})
	}
}

func TestFinalizeSignsPointerCoverageAndPreservesUnrelatedHotplugFailure(t *testing.T) {
	for _, scenario := range []struct {
		changed, cancelled, removeUnfinished, wheels bool
	}{
		{}, {changed: true}, {cancelled: true}, {cancelled: true, removeUnfinished: true},
		{wheels: true}, {wheels: true, changed: true}, {wheels: true, cancelled: true},
		{wheels: true, cancelled: true, removeUnfinished: true},
	} {
		directory := t.TempDir()
		key := bytes.Repeat([]byte{0x45}, 32)
		keyPath := writeTestKey(t, directory, "fingerprint.key", key)
		initial := pointerInventoryFixture(false, false)
		pointerEvidence := completePointerEvidence()
		if scenario.wheels {
			withPointerWheelCriteria(&pointerEvidence, &initial)
		}
		if scenario.cancelled {
			pointerEvidence.Status, pointerEvidence.StopReason = inputtest.StatusAborted, "operator_cancelled"
		}
		if scenario.removeUnfinished {
			initial.Components = append(initial.Components, inventory.NewComponent(inventory.ComponentInput, "input:1", map[string]string{"category": "pointer", "handlers": "mouse1 event1"}))
			pointerEvidence.Devices = append(pointerEvidence.Devices, inputtest.PointerDeviceResult{Interface: "event1", Status: inputtest.StatusInconclusive, Wheels: []inputtest.PointerWheelResult{}})
		}
		frozen, err := plan.BuildWithSelection(initial, plan.ProfileQuick, "x86_64", key, "ephemeral-session", []string{"input.readiness.quick"})
		if err != nil {
			t.Fatal(err)
		}
		encodedPlan, err := json.Marshal(frozen)
		if err != nil {
			t.Fatal(err)
		}
		planPath, reportPath := filepath.Join(directory, "plan.json"), filepath.Join(directory, "session.hhpcr")
		if err := os.WriteFile(planPath, encodedPlan, 0o600); err != nil {
			t.Fatal(err)
		}
		encodedPointer := encodeEvidenceFixture(t, pointerEvidence)
		if err := os.WriteFile(filepath.Join(directory, "pointer.json"), encodedPointer, 0o600); err != nil {
			t.Fatal(err)
		}
		arguments := []string{"-fingerprint-key", keyPath, "-plan", planPath, "-evidence-dir", directory, "-session-id", "pointer-fixture", "-output", reportPath}
		for _, definition := range quickResultDefinitions {
			for _, row := range frozen.Tests {
				if definition.id == row.ID {
					outcome := string(row.Status)
					if row.ID == "input.pointer.quick" {
						outcome = string(pointerEvidence.Status)
					}
					arguments = append(arguments, "-outcome", row.ID+"="+outcome)
				}
			}
		}
		final := initial
		if scenario.changed {
			final = inventory.NewSnapshot(append(final.Components, inventory.NewComponent(inventory.ComponentUSB, "usb:added", nil)))
		}
		if scenario.removeUnfinished {
			final = inventory.NewSnapshot(final.Components[:1])
		}
		var stdout, stderr bytes.Buffer
		if code := runFinalizeWith(t.Context(), arguments, &stdout, &stderr, func(context.Context) (inventory.Snapshot, error) { return final, nil }, bytes.NewReader(bytes.Repeat([]byte{0x47}, 32)), time.Now); code != 0 {
			t.Fatalf("pointer finalization failed: %d %s", code, stderr.String())
		}
		report, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatal(err)
		}
		artifact, err := bundle.VerifySelfIssued(bytes.NewReader(report), int64(len(report)))
		if err != nil {
			t.Fatal(err)
		}
		var payload app.InventoryPayload
		if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.TestResults) != 22 || payload.CoverageScope != app.CoverageFrozenPlan {
			t.Fatal("pointer execution changed frozen catalog coverage")
		}
		for _, result := range payload.TestResults {
			switch result.ID {
			case "input.pointer.quick":
				if result.Status != app.TestStatus(pointerEvidence.Status) || result.PlannedStatus != app.PlannedScheduled || result.PlanID != frozen.ID || !bytes.Equal(result.Evidence, encodedPointer) {
					t.Fatal("pointer evidence or frozen binding was changed")
				}
			case "input.readiness.quick":
				if result.Status != app.TestSkip || result.PlannedStatus != app.PlannedExcluded {
					t.Fatal("pointer pass changed the excluded readiness disposition")
				}
			case "run.integrity.quick":
				if (result.Status == app.TestFail) != (scenario.changed || scenario.removeUnfinished) {
					t.Fatal("pointer pass erased an unrelated hardware-integrity failure")
				}
			}
		}
		if !strings.Contains(stdout.String(), "HHPT_RUN_INTEGRITY") {
			t.Fatal("finalizer omitted integrity outcome")
		}
	}
}
