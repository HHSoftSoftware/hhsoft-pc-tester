package inputtest

import "testing"

func pointerTestCapabilities(buttons ...string) PointerCapabilities {
	return PointerCapabilities{Key: true, Relative: true, RelativeX: true, RelativeY: true, Buttons: buttons}
}

func pointerTestState(buttons ...string) *pointerState {
	capabilities := pointerTestCapabilities(buttons...)
	return newPointerState(&PointerDeviceResult{Interface: "event0", Supported: true, Readable: true, Capabilities: &capabilities})
}

func pointerPacket(state *pointerState, events ...pointerEvent) {
	for _, event := range events {
		state.consume(event)
	}
	state.consume(pointerEvent{0, 0, 0})
}

func pointerCompleteEvents(buttons ...uint16) []pointerEvent {
	events := []pointerEvent{{2, 0, 3}, {2, 1, 2}, {0, 0, 0}, {2, 0, -4}, {2, 1, -6}, {0, 0, 0}}
	for _, button := range buttons {
		events = append(events, pointerEvent{1, button, 1}, pointerEvent{0, 0, 0}, pointerEvent{1, button, 0}, pointerEvent{0, 0, 0})
	}
	return events
}

func TestPointerFramesCommitAtomicallyAndUseNetMovement(t *testing.T) {
	state := pointerTestState("left")
	state.consume(pointerEvent{2, 0, 10})
	state.consume(pointerEvent{2, 1, -8})
	state.consume(pointerEvent{1, 0x110, 1})
	if state.result.Frames != 0 || state.result.XPositive != 0 || state.result.Buttons[0].Presses != 0 {
		t.Fatal("partial packet changed committed evidence")
	}
	state.consume(pointerEvent{0, 0, 0})
	if state.result.Frames != 1 || state.result.XPositive != 1 || state.result.YNegative != 1 || state.result.Buttons[0].Presses != 1 {
		t.Fatal("complete packet observations were lost")
	}
	pointerPacket(state, pointerEvent{2, 0, 12}, pointerEvent{2, 0, -12}, pointerEvent{1, 0x110, 0})
	if state.result.XPositive != 1 || state.result.XNegative != 0 || state.result.Buttons[0].Pairs != 1 {
		t.Fatal("opposite motion in one packet manufactured two directions")
	}
}

func TestPointerRequiresEveryDirectionAndAdvertisedPrimaryButton(t *testing.T) {
	state := pointerTestState("left", "right", "middle")
	for _, event := range pointerCompleteEvents(0x110, 0x111) {
		state.consume(event)
	}
	if state.complete() {
		t.Fatal("untested advertised middle button passed")
	}
	pointerPacket(state, pointerEvent{1, 0x112, 1})
	pointerPacket(state, pointerEvent{1, 0x112, 0})
	if !state.complete() {
		t.Fatal("complete per-interface criteria did not pass")
	}
	for _, button := range state.result.Buttons {
		if button.Presses != 1 || button.Releases != 1 || button.Pairs != 1 {
			t.Fatal("paired observations were miscounted")
		}
	}
	for _, field := range []*uint64{&state.result.XPositive, &state.result.XNegative, &state.result.YPositive, &state.result.YNegative} {
		previous := *field
		*field = 0
		if state.complete() {
			t.Fatal("missing direction passed")
		}
		*field = previous
	}
}

func TestPointerInvalidAndIrrelevantEventsNeverCompleteCriteria(t *testing.T) {
	cases := map[string][]pointerEvent{
		"wheel only":        {{2, 8, 1}, {2, 8, -1}, {0, 0, 0}},
		"repeat only":       {{1, 0x110, 2}, {0, 0, 0}},
		"unmatched release": {{2, 0, 3}, {1, 0x110, 0}, {0, 0, 0}},
		"same packet pair":  {{1, 0x110, 1}, {1, 0x110, 0}, {0, 0, 0}},
		"wrong button":      {{1, 0x111, 1}, {0, 0, 0}},
		"absolute input":    {{3, 0, 30}, {0, 0, 0}},
		"invalid value":     {{1, 0x110, 4}, {0, 0, 0}},
		"dropped records":   {{2, 0, 3}, {0, 3, 0}, {0, 0, 0}},
		"output events":     {{17, 0, 1}, {18, 0, 1}, {0, 0, 0}},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			state := pointerTestState("left")
			for _, event := range events {
				state.consume(event)
			}
			if state.complete() || state.result.Buttons[0].Pairs != 0 || state.result.XPositive != 0 {
				t.Fatal("invalid or irrelevant input manufactured criteria")
			}
			if name == "dropped records" && !state.result.EvidenceLoss {
				t.Fatal("lost records were not disclosed")
			}
		})
	}
	state := pointerTestState("left")
	for index := 0; index <= maximumPointerFrameEvents; index++ {
		state.consume(pointerEvent{2, 8, 1})
	}
	if !state.failed || state.result.Frames != 0 {
		t.Fatal("unbounded incomplete packet accepted")
	}
}

func TestPointerPrimaryButtonPolicyIsExplicitAndCanonical(t *testing.T) {
	for _, buttons := range [][]string{{"left"}, {"left", "middle"}, {"left", "right"}, {"left", "right", "middle"}} {
		if !supportedPointer(pointerTestCapabilities(buttons...)) {
			t.Fatal("advertised primary subset rejected")
		}
	}
	for _, buttons := range [][]string{nil, {"right"}, {"left", "side"}, {"left", "left"}, {"left", "middle", "right"}} {
		if supportedPointer(pointerTestCapabilities(buttons...)) {
			t.Fatal("unsupported or ambiguous primary policy accepted")
		}
	}
}
