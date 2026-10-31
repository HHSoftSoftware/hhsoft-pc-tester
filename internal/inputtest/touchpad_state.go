package inputtest

import "slices"

type touchpadEvent struct {
	typeCode, code uint16
	value          int32
}

type touchpadMotion struct {
	xPositive, xNegative, yPositive, yNegative uint64
}

func (m touchpadMotion) complete() bool {
	return m.xPositive != 0 && m.xNegative != 0 && m.yPositive != 0 && m.yNegative != 0
}

type touchpadState struct {
	result        *TouchpadDeviceResult
	capabilities  TouchpadCapabilities
	threshold     [2]int64
	down          [5]bool // left, right, middle, finger, touch
	transitions   [5]int8
	axisPresent   [4]bool
	axisValues    [4]int32
	frameEvents   int
	records       uint64
	failed        bool
	fresh         [2]bool
	position      [2]int32
	anchor        [2]int64
	anchored      bool
	pendingMotion touchpadMotion
}

func newTouchpadState(result *TouchpadDeviceResult) *touchpadState {
	state := &touchpadState{result: result}
	if result.Capabilities == nil {
		state.reject("unsupported_capabilities", false)
		return state
	}
	copy, ok := copyTouchpadMetadata(touchpadMetadata{capabilities: *result.Capabilities})
	x, y, supported := ValidateTouchpadCapabilities(copy.capabilities)
	if !ok || !supported {
		state.reject("unsupported_capabilities", false)
		return state
	}
	state.capabilities, state.threshold = copy.capabilities, [2]int64{x, y}
	result.XThreshold, result.YThreshold = x, y
	result.Buttons = []PointerButtonResult{}
	for i, code := range []uint16{0x110, 0x111, 0x112} {
		if slices.Contains(state.capabilities.Keys, code) {
			result.Buttons = append(result.Buttons, PointerButtonResult{Button: [...]string{"left", "right", "middle"}[i]})
		}
	}
	return state
}

func (state *touchpadState) reject(detail string, lost bool) {
	state.failed = true
	state.result.Status, state.result.Detail = StatusError, detail
	state.result.FinalNeutral = false
	state.result.EvidenceLoss = state.result.EvidenceLoss || lost
}

func (state *touchpadState) consume(event touchpadEvent) {
	if state.failed {
		return
	}
	if state.records >= maximumTouchpadRecords {
		state.reject("record_limit", false)
		return
	}
	state.records++
	if event.typeCode == 0 {
		switch event.code {
		case 0:
			// The signed SYN_REPORT value is undefined and supplies no credit.
			state.commit()
		case 3:
			state.reject("event_loss", true)
		default:
			state.reject("malformed_events", false)
		}
		return
	}
	state.frameEvents++
	if state.frameEvents > maximumTouchpadFrameEvents {
		state.reject("frame_limit", false)
		return
	}
	switch event.typeCode {
	case 1:
		index := slices.Index([]uint16{0x110, 0x111, 0x112, 0x145, 0x14a}, event.code)
		if index < 0 || !slices.Contains(state.capabilities.Keys, event.code) ||
			(event.value != 0 && event.value != 1) || state.transitions[index] != 0 {
			state.reject("malformed_events", false)
			return
		}
		state.transitions[index] = -1
		if event.value == 1 {
			state.transitions[index] = 1
		}
	case 3:
		index := slices.IndexFunc(state.capabilities.Axes, func(axis TouchpadAxis) bool { return axis.Code == event.code })
		if index < 0 || state.axisPresent[index] ||
			event.value < state.capabilities.Axes[index].Minimum || event.value > state.capabilities.Axes[index].Maximum {
			state.reject("malformed_events", false)
			return
		}
		state.axisPresent[index], state.axisValues[index] = true, event.value
	case 4:
		if !slices.Contains(state.capabilities.Events, uint16(4)) || !slices.Contains(state.capabilities.Misc, event.code) {
			state.reject("malformed_events", false)
		}
		// Admitted scan/timestamp payloads never enter state or evidence.
	default:
		state.reject("malformed_events", false)
	}
}

func (state *touchpadState) commit() {
	next := state.down
	for i, change := range state.transitions {
		if change == 1 && next[i] || change == -1 && !next[i] {
			state.reject("malformed_events", false)
			return
		}
		if change != 0 {
			next[i] = change == 1
		}
	}
	if next[4] && !next[3] {
		state.reject("malformed_events", false)
		return
	}
	r := state.result
	// Check every counter before any mutation, including empty report frames.
	for _, count := range []uint64{r.Frames, r.ContactStarts, r.ContactReleases, r.XYBaselines,
		r.QualifyingContacts, r.XPositive, r.XNegative, r.YPositive, r.YNegative} {
		if count >= maximumTouchpadRecords {
			state.reject("record_limit", false)
			return
		}
	}
	for _, b := range r.Buttons {
		if b.Presses >= maximumTouchpadRecords || b.Releases >= maximumTouchpadRecords || b.Pairs >= maximumTouchpadRecords {
			state.reject("record_limit", false)
			return
		}
	}
	if state.down[4] && !next[4] {
		m := state.pendingMotion
		for _, pair := range [][2]uint64{{r.XPositive, m.xPositive}, {r.XNegative, m.xNegative}, {r.YPositive, m.yPositive}, {r.YNegative, m.yNegative}} {
			if pair[1] > maximumTouchpadRecords-pair[0] {
				state.reject("record_limit", false)
				return
			}
		}
	}
	// All fields are valid now. Event ordering within the packet cannot commit
	// a valid prefix before discovering a later invalid field.
	r.Frames++
	if !state.down[4] && next[4] {
		r.ContactStarts++
		state.fresh, state.position, state.anchor = [2]bool{}, [2]int32{}, [2]int64{}
		state.anchored, state.pendingMotion = false, touchpadMotion{}
	}
	if next[4] {
		for i := 0; i < 2; i++ {
			if state.axisPresent[i] {
				state.fresh[i], state.position[i] = true, state.axisValues[i]
			}
		}
		if !state.anchored && state.fresh[0] && state.fresh[1] {
			state.anchored = true
			state.anchor = [2]int64{int64(state.position[0]), int64(state.position[1])}
			r.XYBaselines++
			// The whole first fresh-XY frame is baseline, including either
			// axis already seen in earlier frames of this contact.
		} else if state.anchored {
			for i := 0; i < 2; i++ {
				if !state.axisPresent[i] {
					continue
				}
				delta := int64(state.position[i]) - state.anchor[i]
				direction := 0
				if delta >= state.threshold[i] {
					direction = 1
				} else if delta <= -state.threshold[i] {
					direction = -1
				}
				if direction != 0 {
					state.anchor[i] = int64(state.position[i])
					switch {
					case i == 0 && direction > 0:
						state.pendingMotion.xPositive++
					case i == 0:
						state.pendingMotion.xNegative++
					case direction > 0:
						state.pendingMotion.yPositive++
					default:
						state.pendingMotion.yNegative++
					}
				}
			}
		}
	}
	if state.down[4] && !next[4] {
		r.ContactReleases++
		m := state.pendingMotion
		r.XPositive += m.xPositive
		r.XNegative += m.xNegative
		r.YPositive += m.yPositive
		r.YNegative += m.yNegative
		if state.anchored && m.complete() {
			r.QualifyingContacts++
		}
		state.fresh, state.anchored, state.pendingMotion = [2]bool{}, false, touchpadMotion{}
	}
	for i, change := range state.transitions[:3] {
		for index := range r.Buttons {
			b := &r.Buttons[index]
			if b.Button != [...]string{"left", "right", "middle"}[i] {
				continue
			}
			if change == 1 {
				b.Presses++
			} else if change == -1 {
				b.Releases++
				b.Pairs++
			}
		}
	}
	state.down = next
	state.transitions, state.axisPresent, state.axisValues, state.frameEvents = [5]int8{}, [4]bool{}, [4]int32{}, 0
}

func (state *touchpadState) complete() bool {
	if state.failed || !state.result.BaselineNeutral || state.frameEvents != 0 ||
		state.result.QualifyingContacts == 0 || len(state.result.Buttons) == 0 || state.result.Buttons[0].Pairs == 0 {
		return false
	}
	for _, down := range state.down {
		if down {
			return false
		}
	}
	return true
}

func (state *touchpadState) incompleteReason() string {
	switch {
	case !state.result.BaselineNeutral:
		return "baseline_unavailable"
	case state.result.QualifyingContacts == 0 && state.result.XYBaselines == 0:
		return "fresh_xy_missing"
	case state.result.QualifyingContacts == 0 && state.pendingMotion.complete():
		return "release_required"
	case state.result.QualifyingContacts == 0:
		return "contact_incomplete"
	case len(state.result.Buttons) == 0 || state.result.Buttons[0].Pairs == 0:
		return "primary_button_incomplete"
	case !state.complete():
		return "controls_not_neutral"
	default:
		return ""
	}
}
