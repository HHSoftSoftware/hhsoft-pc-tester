package inputtest

import "testing"

func touchscreenTestCapabilities() TouchscreenCapabilities {
	return TouchscreenCapabilities{
		ProtocolVersion: 0x010001,
		Events:          []uint16{0, 1, 3}, Keys: []uint16{0x14a},
		Relative: []uint16{}, Properties: []uint16{1}, Misc: []uint16{},
		Axes: []TouchscreenAxis{
			{Code: 0, Minimum: -100, Maximum: 100},
			{Code: 1, Minimum: -100, Maximum: 100},
		},
	}
}

func touchscreenTestState() (*touchscreenState, *TouchscreenDeviceResult) {
	c := touchscreenTestCapabilities()
	r := &TouchscreenDeviceResult{Interface: "event0", Status: StatusInconclusive,
		Supported: true, Readable: true, Capabilities: &c, BaselineNeutral: true, CleanupConfirmed: true}
	return newTouchscreenState(r), r
}

func touchscreenTestFrame(s *touchscreenState, events ...touchscreenEvent) {
	for _, event := range events {
		s.consume(event)
	}
	s.consume(touchscreenEvent{0, 0, 0})
}

// Four literal frames: fresh contact, positive motion, negative motion, release.
// No finger tool, pointer button, pressure or ioctl current value is involved.
func touchscreenTestCompleteEvents() []touchscreenEvent {
	return []touchscreenEvent{
		{1, 0x14a, 1}, {3, 0, -20}, {3, 1, 30}, {0, 0, -7},
		{3, 0, 20}, {3, 1, 70}, {0, 0, 0},
		{3, 0, -40}, {3, 1, 10}, {0, 0, 0},
		{1, 0x14a, 0}, {0, 0, 0},
	}
}

func TestTouchscreenExactDirectSingleContactCapabilities(t *testing.T) {
	base := touchscreenTestCapabilities()
	x, y, ok := ValidateTouchscreenCapabilities(base)
	if !ok || x != 10 || y != 10 {
		t.Fatal("literal DIRECT/BTN_TOUCH/X/Y shape rejected")
	}
	cases := []struct {
		name   string
		change func(*TouchscreenCapabilities)
	}{
		{"protocol", func(c *TouchscreenCapabilities) { c.ProtocolVersion++ }},
		{"missing_syn", func(c *TouchscreenCapabilities) { c.Events = []uint16{1, 3} }},
		{"unknown_event", func(c *TouchscreenCapabilities) { c.Events = []uint16{0, 1, 3, 5} }},
		{"missing_touch", func(c *TouchscreenCapabilities) { c.Keys = []uint16{} }},
		{"finger_tool", func(c *TouchscreenCapabilities) { c.Keys = []uint16{0x145, 0x14a} }},
		{"stylus", func(c *TouchscreenCapabilities) { c.Keys = []uint16{0x140, 0x14a} }},
		{"primary_button", func(c *TouchscreenCapabilities) { c.Keys = []uint16{0x110, 0x14a} }},
		{"duplicate_key", func(c *TouchscreenCapabilities) { c.Keys = []uint16{0x14a, 0x14a} }},
		{"pointer_only", func(c *TouchscreenCapabilities) { c.Properties = []uint16{0} }},
		{"pointer_and_direct", func(c *TouchscreenCapabilities) { c.Properties = []uint16{0, 1} }},
		{"unknown_property", func(c *TouchscreenCapabilities) { c.Properties = []uint16{1, 7} }},
		{"missing_direct", func(c *TouchscreenCapabilities) { c.Properties = []uint16{} }},
		{"relative", func(c *TouchscreenCapabilities) { c.Relative = []uint16{0} }},
		{"pressure", func(c *TouchscreenCapabilities) { c.Axes = append(c.Axes, TouchscreenAxis{Code: 24, Maximum: 100}) }},
		{"multitouch", func(c *TouchscreenCapabilities) { c.Axes = append(c.Axes, TouchscreenAxis{Code: 47, Maximum: 10}) }},
		{"one_axis", func(c *TouchscreenCapabilities) { c.Axes = c.Axes[:1] }},
		{"reversed_axes", func(c *TouchscreenCapabilities) { c.Axes[0], c.Axes[1] = c.Axes[1], c.Axes[0] }},
		{"flat_range", func(c *TouchscreenCapabilities) { c.Axes[0].Maximum = c.Axes[0].Minimum }},
		{"negative_fuzz", func(c *TouchscreenCapabilities) { c.Axes[0].Fuzz = -1 }},
		{"negative_flat", func(c *TouchscreenCapabilities) { c.Axes[0].Flat = -1 }},
		{"negative_resolution", func(c *TouchscreenCapabilities) { c.Axes[0].Resolution = -1 }},
		{"msc_without_event", func(c *TouchscreenCapabilities) { c.Misc = []uint16{4} }},
		{"msc_without_bits", func(c *TouchscreenCapabilities) { c.Events = []uint16{0, 1, 3, 4} }},
		{"unknown_msc", func(c *TouchscreenCapabilities) { c.Events = []uint16{0, 1, 3, 4}; c.Misc = []uint16{0} }},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			c := touchscreenTestCapabilities()
			row.change(&c)
			if _, _, ok := ValidateTouchscreenCapabilities(c); ok {
				t.Fatal("unsupported shape became a supported subset")
			}
		})
	}
	c := touchscreenTestCapabilities()
	c.Events, c.Misc = []uint16{0, 1, 3, 4}, []uint16{4, 5}
	if _, _, ok := ValidateTouchscreenCapabilities(c); !ok {
		t.Fatal("bounded scan/timestamp metadata rejected")
	}
}

func TestTouchscreenThresholdUsesSignedWideArithmetic(t *testing.T) {
	for _, row := range []struct {
		name string
		axis TouchscreenAxis
		want int64
		ok   bool
	}{
		{"signed_extrema", TouchscreenAxis{Minimum: -2147483648, Maximum: 2147483647}, 214748365, true},
		{"one_unit", TouchscreenAxis{Minimum: -1, Maximum: 0}, 1, true},
		{"round_up", TouchscreenAxis{Minimum: -10, Maximum: 11}, 2, true},
		{"fuzz_floor", TouchscreenAxis{Minimum: -100, Maximum: 100, Fuzz: 3}, 13, true},
		{"flat_floor", TouchscreenAxis{Minimum: -100, Maximum: 100, Flat: 7}, 15, true},
		{"unreachable_fuzz", TouchscreenAxis{Minimum: 0, Maximum: 10, Fuzz: 3}, 13, false},
		{"unreachable_flat", TouchscreenAxis{Minimum: 0, Maximum: 10, Flat: 5}, 11, false},
		{"reversed", TouchscreenAxis{Minimum: 1, Maximum: 0}, 0, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, ok := TouchscreenThreshold(row.axis)
			if got != row.want || ok != row.ok {
				t.Fatalf("threshold %d/%v, want %d/%v", got, ok, row.want, row.ok)
			}
		})
	}
}

func TestTouchscreenReleasedContactIsTheOnlyMotionCredit(t *testing.T) {
	s, r := touchscreenTestState()
	events := touchscreenTestCompleteEvents()
	for _, event := range events[:len(events)-2] {
		s.consume(event)
	}
	if s.complete() || r.QualifyingContacts != 0 || r.XPositive != 0 || r.XNegative != 0 ||
		r.YPositive != 0 || r.YNegative != 0 || s.incompleteReason() != "release_required" {
		t.Fatal("unreleased contact escaped the pending observation state")
	}
	for _, event := range events[len(events)-2:] {
		s.consume(event)
	}
	if !s.complete() || s.failed || r.Frames != 4 || r.ContactStarts != 1 || r.ContactReleases != 1 ||
		r.XYBaselines != 1 || r.QualifyingContacts != 1 ||
		r.XPositive != 1 || r.XNegative != 1 || r.YPositive != 1 || r.YNegative != 1 {
		t.Fatal("literal same-contact signed motion/release did not qualify")
	}
	if r.FinalNeutral {
		t.Fatal("state alone asserted the terminal key/identity proof")
	}
}

func TestTouchscreenFreshAxesAndBaselineCannotSupplyMotion(t *testing.T) {
	for _, name := range []string{"stale_xy", "missing_x", "missing_y", "baseline_frame", "below_threshold"} {
		t.Run(name, func(t *testing.T) {
			s, r := touchscreenTestState()
			touchscreenTestFrame(s, touchscreenEvent{3, 0, -80}, touchscreenEvent{3, 1, -80})
			switch name {
			case "stale_xy":
				touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 1})
			case "missing_x":
				touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 1}, touchscreenEvent{3, 1, -50})
				touchscreenTestFrame(s, touchscreenEvent{3, 1, 50})
			case "missing_y":
				touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 1}, touchscreenEvent{3, 0, -50})
				touchscreenTestFrame(s, touchscreenEvent{3, 0, 50})
			case "baseline_frame":
				touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 1}, touchscreenEvent{3, 0, -100})
				touchscreenTestFrame(s, touchscreenEvent{3, 0, 100}, touchscreenEvent{3, 1, 100})
			case "below_threshold":
				touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 1}, touchscreenEvent{3, 0, 0}, touchscreenEvent{3, 1, 0})
				touchscreenTestFrame(s, touchscreenEvent{3, 0, 9}, touchscreenEvent{3, 1, 9})
				touchscreenTestFrame(s, touchscreenEvent{3, 0, -9}, touchscreenEvent{3, 1, -9})
			}
			touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 0})
			if s.failed || s.complete() || r.QualifyingContacts != 0 || r.XPositive != 0 ||
				r.XNegative != 0 || r.YPositive != 0 || r.YNegative != 0 {
				t.Fatal("stale, missing, baseline or subthreshold coordinates supplied credit")
			}
		})
	}
}

func TestTouchscreenDirectionsCannotCombineAcrossContacts(t *testing.T) {
	s, r := touchscreenTestState()
	touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 1}, touchscreenEvent{3, 0, 0}, touchscreenEvent{3, 1, 0})
	touchscreenTestFrame(s, touchscreenEvent{3, 0, 10}, touchscreenEvent{3, 1, 10})
	touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 0})
	touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 1}, touchscreenEvent{3, 0, 0}, touchscreenEvent{3, 1, 0})
	touchscreenTestFrame(s, touchscreenEvent{3, 0, -10}, touchscreenEvent{3, 1, -10})
	touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 0})
	if s.failed || s.complete() || r.ContactStarts != 2 || r.ContactReleases != 2 || r.XYBaselines != 2 ||
		r.QualifyingContacts != 0 || r.XPositive != 1 || r.XNegative != 1 || r.YPositive != 1 || r.YNegative != 1 {
		t.Fatal("different released contacts were promoted to one qualifying gesture")
	}
}

func TestTouchscreenMalformedFrameAndLossFailClosed(t *testing.T) {
	for _, name := range []string{"repeat", "release_without_start", "duplicate_key", "duplicate_axis", "range", "unknown_key", "unknown_axis", "unknown_event", "unknown_sync", "lost"} {
		t.Run(name, func(t *testing.T) {
			s, r := touchscreenTestState()
			switch name {
			case "repeat":
				s.consume(touchscreenEvent{1, 0x14a, 2})
			case "release_without_start":
				touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 0})
			case "duplicate_key":
				touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 1}, touchscreenEvent{1, 0x14a, 0})
			case "duplicate_axis":
				touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 1}, touchscreenEvent{3, 0, 0}, touchscreenEvent{3, 0, 1})
			case "range":
				touchscreenTestFrame(s, touchscreenEvent{1, 0x14a, 1}, touchscreenEvent{3, 0, 101})
			case "unknown_key":
				s.consume(touchscreenEvent{1, 0x145, 1})
			case "unknown_axis":
				s.consume(touchscreenEvent{3, 24, 0})
			case "unknown_event":
				s.consume(touchscreenEvent{2, 0, 1})
			case "unknown_sync":
				s.consume(touchscreenEvent{0, 2, 0})
			case "lost":
				s.consume(touchscreenEvent{0, 3, 0})
			}
			if !s.failed || s.complete() || r.Status != StatusError || r.Frames != 0 ||
				r.ContactStarts != 0 || r.QualifyingContacts != 0 || r.EvidenceLoss != (name == "lost") {
				t.Fatal("malformed frame committed a valid prefix or lost its error")
			}
		})
	}
}

func TestTouchscreenMetadataCopyAndRecordBounds(t *testing.T) {
	s, r := touchscreenTestState()
	r.Capabilities.Axes[0].Maximum = -100
	for _, event := range touchscreenTestCompleteEvents() {
		s.consume(event)
	}
	if !s.complete() {
		t.Fatal("provider-owned metadata changed the admitted state")
	}
	s, r = touchscreenTestState()
	s.records = maximumTouchscreenRecords
	s.consume(touchscreenEvent{0, 0, 0})
	if !s.failed || r.Detail != "record_limit" || r.Frames != 0 {
		t.Fatal("record quota accepted an extra event")
	}
	s, r = touchscreenTestState()
	s.frameEvents = maximumTouchscreenFrameEvents
	s.consume(touchscreenEvent{3, 0, 0})
	if !s.failed || r.Detail != "frame_limit" || r.Frames != 0 {
		t.Fatal("frame quota accepted an extra field")
	}
	var device, total uint64 = 0, maximumTouchscreenTotalRecords
	if touchscreenRecordQuota(&device, &total) || device != 0 || total != maximumTouchscreenTotalRecords {
		t.Fatal("aggregate record quota was not atomic")
	}
}
