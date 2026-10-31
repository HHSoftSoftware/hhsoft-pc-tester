package inputtest

import "slices"

type touchscreenEvent struct {
	typeCode, code uint16
	value          int32
}

type touchscreenMotion struct {
	xPositive, xNegative, yPositive, yNegative uint64
}

func (m touchscreenMotion) complete() bool {
	return m.xPositive != 0 && m.xNegative != 0 && m.yPositive != 0 && m.yNegative != 0
}

type touchscreenState struct {
	result        *TouchscreenDeviceResult
	capabilities  TouchscreenCapabilities
	threshold     [2]int64
	down          [1]bool // touch
	transitions   [1]int8
	axisPresent   [2]bool
	axisValues    [2]int32
	frameEvents   int
	records       uint64
	failed        bool
	fresh         [2]bool
	position      [2]int32
	anchor        [2]int64
	anchored      bool
	pendingMotion touchscreenMotion
}

func newTouchscreenState(result *TouchscreenDeviceResult) *touchscreenState {
	state := &touchscreenState{result: result}
	if result.Capabilities == nil {
		state.reject("unsupported_capabilities", false)
		return state
	}
	copy, ok := copyTouchscreenMetadata(touchscreenMetadata{capabilities: *result.Capabilities})
	x, y, supported := ValidateTouchscreenCapabilities(copy.capabilities)
	if !ok || !supported {
		state.reject("unsupported_capabilities", false)
		return state
	}
	state.capabilities, state.threshold = copy.capabilities, [2]int64{x, y}
	result.XThreshold, result.YThreshold = x, y
	return state
}

func (state *touchscreenState) reject(detail string, lost bool) {
	state.failed = true
	state.result.Status, state.result.Detail = StatusError, detail
	state.result.FinalNeutral = false
	state.result.EvidenceLoss = state.result.EvidenceLoss || lost
}

func (state *touchscreenState) consume(event touchscreenEvent) {
	if state.failed {
		return
	}
	if state.records >= maximumTouchscreenRecords {
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
	if state.frameEvents > maximumTouchscreenFrameEvents {
		state.reject("frame_limit", false)
		return
	}
	switch event.typeCode {
	case 1:
		index := slices.Index([]uint16{0x14a}, event.code)
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
		index := slices.IndexFunc(state.capabilities.Axes, func(axis TouchscreenAxis) bool { return axis.Code == event.code })
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

func (state *touchscreenState) commit() {
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
	r := state.result
	// Check every counter before any mutation, including empty report frames.
	for _, count := range []uint64{r.Frames, r.ContactStarts, r.ContactReleases, r.XYBaselines,
		r.QualifyingContacts, r.XPositive, r.XNegative, r.YPositive, r.YNegative} {
		if count >= maximumTouchscreenRecords {
			state.reject("record_limit", false)
			return
		}
	}
	if state.down[0] && !next[0] {
		m := state.pendingMotion
		for _, pair := range [][2]uint64{{r.XPositive, m.xPositive}, {r.XNegative, m.xNegative}, {r.YPositive, m.yPositive}, {r.YNegative, m.yNegative}} {
			if pair[1] > maximumTouchscreenRecords-pair[0] {
				state.reject("record_limit", false)
				return
			}
		}
	}
	// All fields are valid now. Event ordering within the packet cannot commit
	// a valid prefix before discovering a later invalid field.
	r.Frames++
	if !state.down[0] && next[0] {
		r.ContactStarts++
		state.fresh, state.position, state.anchor = [2]bool{}, [2]int32{}, [2]int64{}
		state.anchored, state.pendingMotion = false, touchscreenMotion{}
	}
	if next[0] {
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
	if state.down[0] && !next[0] {
		r.ContactReleases++
		m := state.pendingMotion
		r.XPositive += m.xPositive
		r.XNegative += m.xNegative
		r.YPositive += m.yPositive
		r.YNegative += m.yNegative
		if state.anchored && m.complete() {
			r.QualifyingContacts++
		}
		state.fresh, state.anchored, state.pendingMotion = [2]bool{}, false, touchscreenMotion{}
	}
	state.down = next
	state.transitions, state.axisPresent, state.axisValues, state.frameEvents = [1]int8{}, [2]bool{}, [2]int32{}, 0
}

func (state *touchscreenState) complete() bool {
	if state.failed || !state.result.BaselineNeutral || state.frameEvents != 0 ||
		state.result.QualifyingContacts == 0 {
		return false
	}
	for _, down := range state.down {
		if down {
			return false
		}
	}
	return true
}

func (state *touchscreenState) incompleteReason() string {
	switch {
	case !state.result.BaselineNeutral:
		return "baseline_unavailable"
	case state.result.QualifyingContacts == 0 && state.result.XYBaselines == 0:
		return "fresh_xy_missing"
	case state.result.QualifyingContacts == 0 && state.pendingMotion.complete():
		return "release_required"
	case state.result.QualifyingContacts == 0:
		return "contact_incomplete"
	case !state.complete():
		return "controls_not_neutral"
	default:
		return ""
	}
}
