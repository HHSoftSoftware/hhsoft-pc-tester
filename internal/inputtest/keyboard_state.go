package inputtest

import "slices"

// Fixed read-only Linux input UAPI controls. No text translation is performed.
var keyboardCodes = [...]uint16{42, 54, 103, 108, 105, 106}

const maximumKeyboardFrameEvents = 4096

type keyboardEvent struct {
	typeCode, code uint16
	value          int32
}
type keyboardState struct {
	result      *KeyboardDeviceResult
	down        [6]bool
	transition  [6]int8
	frameEvents int
	failed      bool
}

func supportedKeyboard(capabilities KeyboardCapabilities) bool {
	names := KeyboardActions()
	return capabilities.EventKey && slices.Equal(capabilities.FixedKeys, names[:])
}

func newKeyboardState(result *KeyboardDeviceResult) *keyboardState {
	result.Actions = make([]KeyboardAction, 6)
	for index, name := range KeyboardActions() {
		result.Actions[index].Name = name
	}
	return &keyboardState{result: result}
}

func (state *keyboardState) reject(detail string, lost bool) {
	state.failed = true
	state.result.Status, state.result.Detail = StatusInconclusive, detail
	state.result.FinalNeutral = false
	state.result.EvidenceLoss = state.result.EvidenceLoss || lost
}

func (state *keyboardState) consume(event keyboardEvent) {
	if state.failed {
		return
	}
	if event.typeCode == 0 {
		switch event.code {
		case 0:
			if event.value != 0 {
				state.reject("malformed_events", false)
				return
			}
			state.commit()
		case 3:
			state.reject("event_loss", true)
		default:
			state.reject("malformed_events", false)
		}
		return
	}
	state.frameEvents++
	if state.frameEvents > maximumKeyboardFrameEvents {
		state.reject("malformed_events", false)
		return
	}
	if event.typeCode != 1 {
		return
	} // Never retain MSC_SCAN or other payloads.
	index := slices.Index(keyboardCodes[:], event.code)
	if index < 0 {
		return
	} // Unrelated keys never become evidence.
	if event.value == 2 {
		return
	} // Autorepeat proves neither transition.
	if event.value != 0 && event.value != 1 || state.transition[index] != 0 {
		state.reject("malformed_events", false)
		return
	}
	state.transition[index] = -1
	if event.value == 1 {
		state.transition[index] = 1
	}
}

func (state *keyboardState) commit() {
	for index, transition := range state.transition {
		if transition == 1 && state.down[index] || transition == -1 && !state.down[index] {
			state.reject("malformed_events", false)
			return
		}
	}
	for index, transition := range state.transition {
		if transition == 1 {
			state.down[index] = true
			state.result.Actions[index].PressObserved = true
		} else if transition == -1 {
			state.down[index] = false
			state.result.Actions[index].ReleaseObserved = true
			state.result.Actions[index].PairCompleted = true
		}
	}
	state.transition, state.frameEvents = [6]int8{}, 0
}

func (state *keyboardState) complete() bool {
	if state.failed || !state.result.BaselineNeutral || state.frameEvents != 0 {
		return false
	}
	for index, action := range state.result.Actions {
		if !action.PairCompleted || state.down[index] {
			return false
		}
	}
	return len(state.result.Actions) == 6
}
