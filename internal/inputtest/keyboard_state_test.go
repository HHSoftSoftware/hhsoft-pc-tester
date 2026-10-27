package inputtest

import (
	"encoding/json"
	"strings"
	"testing"
)

func keyboardStateFixture() (*keyboardState, *KeyboardDeviceResult) {
	result := &KeyboardDeviceResult{BaselineNeutral: true}
	return newKeyboardState(result), result
}

func keyboardCompleteEvents() []keyboardEvent {
	// Independent fixed input UAPI values; do not derive fixture coverage from
	// the implementation's key table.
	var events []keyboardEvent
	for _, key := range []uint16{42, 54, 103, 108, 105, 106} {
		events = append(events, keyboardEvent{1, key, 1}, keyboardEvent{0, 0, 0}, keyboardEvent{1, key, 0}, keyboardEvent{0, 0, 0})
	}
	return events
}

func TestKeyboardStateRequiresEachPairAndFinalRelease(t *testing.T) {
	for _, missing := range []int{-1, 0, 1, 2, 3, 4, 5} {
		state, result := keyboardStateFixture()
		for index, event := range keyboardCompleteEvents() {
			if index/4 != missing {
				state.consume(event)
			}
		}
		if state.complete() != (missing == -1) {
			t.Fatalf("missing control %d changed completion incorrectly", missing)
		}
		if missing >= 0 && result.Actions[missing].PairCompleted {
			t.Fatal("another control supplied the missing pair")
		}
	}
	state, _ := keyboardStateFixture()
	for _, event := range keyboardCompleteEvents() {
		state.consume(event)
	}
	state.consume(keyboardEvent{1, 42, 1})
	state.consume(keyboardEvent{0, 0, 0})
	if state.complete() {
		t.Fatal("held control passed despite prior completed pair")
	}
	state.consume(keyboardEvent{1, 42, 0})
	state.consume(keyboardEvent{0, 0, 0})
	if !state.complete() {
		t.Fatal("new final release did not restore neutrality")
	}
}

func TestKeyboardStateRejectsImpossibleAndLostPackets(t *testing.T) {
	cases := map[string][]keyboardEvent{
		"release without press":     {{1, 42, 0}, {0, 0, 0}},
		"duplicate press":           {{1, 42, 1}, {0, 0, 0}, {1, 42, 1}, {0, 0, 0}},
		"two transitions in packet": {{1, 42, 1}, {1, 42, 0}, {0, 0, 0}},
		"invalid value":             {{1, 42, 3}, {0, 0, 0}},
		"negative value":            {{1, 42, -1}, {0, 0, 0}},
		"bad report":                {{0, 0, 1}}, "unknown sync": {{0, 2, 0}},
		"drop": {{0, 3, 0}},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			state, result := keyboardStateFixture()
			for _, event := range events {
				state.consume(event)
			}
			for _, event := range keyboardCompleteEvents() {
				state.consume(event)
			}
			if !state.failed || state.complete() || result.EvidenceLoss != (name == "drop") {
				t.Fatal("invalid packet was recovered into a pass")
			}
		})
	}
	state, result := keyboardStateFixture()
	state.consume(keyboardEvent{1, 42, 1})
	state.consume(keyboardEvent{1, 54, 0})
	state.consume(keyboardEvent{0, 0, 0})
	if !state.failed || result.Actions[0].PressObserved {
		t.Fatal("part of an invalid packet was committed")
	}
}

func TestKeyboardStateDiscardsTextAndRepeatPayloads(t *testing.T) {
	state, result := keyboardStateFixture()
	for _, event := range []keyboardEvent{{4, 4, 0x12345678}, {1, 30, 1}, {1, 30, 0}, {1, 42, 2}, {0, 0, 0}} {
		state.consume(event)
	}
	for _, action := range result.Actions {
		if action.PressObserved || action.ReleaseObserved || action.PairCompleted {
			t.Fatal("text, scan or repeat data supplied fixed-control evidence")
		}
	}
	if state.failed {
		t.Fatal("unrelated ordinary input rejected the worker")
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "305419896") || strings.Contains(string(encoded), "scan") || strings.Contains(string(encoded), "timestamp") {
		t.Fatal("raw input payload escaped into evidence")
	}
	for index := 0; index <= maximumKeyboardFrameEvents; index++ {
		state.consume(keyboardEvent{4, 4, 1})
	}
	if !state.failed {
		t.Fatal("unbounded frame accepted")
	}
	names := KeyboardActions()
	names[0] = "changed"
	if KeyboardActions()[0] != "left_shift" {
		t.Fatal("caller changed fixed scope")
	}
}
