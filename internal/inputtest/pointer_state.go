package inputtest

import "math"

const maximumPointerFrameEvents = 4096

var pointerPrimaryButtons = [...]string{"left", "right", "middle"}

type pointerEvent struct {
	typeCode, code uint16
	value          int32
}

type pointerState struct {
	result      *PointerDeviceResult
	down        [3]bool
	transition  [3]int8
	x, y        int64
	frameEvents int
	failed      bool
}

func supportedPointer(capabilities PointerCapabilities) bool {
	if !capabilities.Key || !capabilities.Relative || !capabilities.RelativeX || !capabilities.RelativeY ||
		capabilities.Absolute || len(capabilities.Buttons) == 0 || capabilities.Buttons[0] != "left" {
		return false
	}
	previous := -1
	for _, name := range capabilities.Buttons {
		index := -1
		for candidate, allowed := range pointerPrimaryButtons {
			if name == allowed {
				index = candidate
			}
		}
		if index <= previous {
			return false
		}
		previous = index
	}
	return true
}

func newPointerState(result *PointerDeviceResult) *pointerState {
	for _, button := range result.Capabilities.Buttons {
		result.Buttons = append(result.Buttons, PointerButtonResult{Button: button})
	}
	return &pointerState{result: result}
}

func (state *pointerState) reject(detail string, lost bool) {
	state.failed = true
	state.result.Status = StatusInconclusive
	state.result.Detail = detail
	state.result.EvidenceLoss = state.result.EvidenceLoss || lost
}

func (state *pointerState) consume(event pointerEvent) {
	if state.failed {
		return
	}
	if event.typeCode == 0 {
		switch event.code {
		case 0: // SYN_REPORT commits one complete packet atomically.
			state.commit()
		case 3: // SYN_DROPPED cannot establish continuous evidence.
			state.reject("pointer event stream lost records", true)
		default:
			state.reject("unsupported pointer synchronization event", false)
		}
		return
	}
	state.frameEvents++
	if state.frameEvents > maximumPointerFrameEvents {
		state.reject("pointer event packet exceeds the supported limit", false)
		return
	}
	switch event.typeCode {
	case 1:
		if event.code < 0x110 || event.code > 0x112 {
			return
		}
		index := int(event.code - 0x110)
		if !state.hasButton(pointerPrimaryButtons[index]) {
			state.reject("pointer emitted an unadvertised primary button", false)
			return
		}
		if event.value == 2 { // Repeats prove neither a press nor a release.
			return
		}
		if event.value != 0 && event.value != 1 || state.transition[index] != 0 {
			state.reject("invalid pointer button transition", false)
			return
		}
		if event.value == 1 {
			state.transition[index] = 1
		} else {
			state.transition[index] = -1
		}
	case 2: // Each frame's net relative direction is one observation per axis.
		if event.code == 0 {
			state.x += int64(event.value)
		} else if event.code == 1 {
			state.y += int64(event.value)
		}
	case 3:
		state.reject("absolute events are outside the relative-pointer test", false)
	}
}

func (state *pointerState) hasButton(name string) bool {
	for _, button := range state.result.Buttons {
		if button.Button == name {
			return true
		}
	}
	return false
}

func (state *pointerState) commit() {
	if state.result.Frames == math.MaxUint64 {
		state.reject("pointer observation counter overflow", false)
		return
	}
	// Validate the entire packet before committing any motion or buttons.
	for index, transition := range state.transition {
		if transition == 1 && state.down[index] || transition == -1 && !state.down[index] {
			state.reject("unmatched pointer button transition", false)
			return
		}
	}
	state.result.Frames++
	if state.x > 0 {
		state.result.XPositive++
	} else if state.x < 0 {
		state.result.XNegative++
	}
	if state.y > 0 {
		state.result.YPositive++
	} else if state.y < 0 {
		state.result.YNegative++
	}
	for index, transition := range state.transition {
		for buttonIndex := range state.result.Buttons {
			button := &state.result.Buttons[buttonIndex]
			if button.Button != pointerPrimaryButtons[index] {
				continue
			}
			if transition == 1 {
				button.Presses++
				state.down[index] = true
			} else if transition == -1 {
				button.Releases++
				button.Pairs++
				state.down[index] = false
			}
		}
	}
	state.x, state.y, state.frameEvents, state.transition = 0, 0, 0, [3]int8{}
}

func (state *pointerState) complete() bool {
	result := state.result
	if state.failed || state.frameEvents != 0 || len(result.Buttons) == 0 || result.Frames == 0 || result.XPositive == 0 || result.XNegative == 0 || result.YPositive == 0 || result.YNegative == 0 {
		return false
	}
	for _, button := range result.Buttons {
		if button.Pairs == 0 || button.Presses != button.Pairs || button.Releases != button.Pairs {
			return false
		}
	}
	return true
}
