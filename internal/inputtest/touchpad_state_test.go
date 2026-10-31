package inputtest

import (
	"reflect"
	"testing"
)

func touchpadTestCapabilities() TouchpadCapabilities {
	return TouchpadCapabilities{ProtocolVersion: 0x010001, Events: []uint16{0, 1, 3},
		Keys: []uint16{0x110, 0x145, 0x14a}, Relative: []uint16{}, Properties: []uint16{0}, Misc: []uint16{},
		Axes: []TouchpadAxis{{Code: 0, Minimum: 50, Maximum: 150}, {Code: 1, Minimum: 50, Maximum: 150}}}
}

func touchpadTestState() *touchpadState {
	c := touchpadTestCapabilities()
	return newTouchpadState(&TouchpadDeviceResult{Status: StatusInconclusive, Capabilities: &c, BaselineNeutral: true})
}

func touchpadTestFrame(s *touchpadState, events ...touchpadEvent) {
	for _, e := range events {
		s.consume(e)
	}
	s.consume(touchpadEvent{0, 0, 0})
}

func touchpadTestLanding(s *touchpadState) {
	touchpadTestFrame(s, touchpadEvent{1, 0x14a, 1}, touchpadEvent{1, 0x145, 1}, touchpadEvent{3, 0, 100}, touchpadEvent{3, 1, 100})
}

func touchpadTestRelease(s *touchpadState) {
	touchpadTestFrame(s, touchpadEvent{1, 0x14a, 0}, touchpadEvent{1, 0x145, 0})
}

func touchpadTestButton(s *touchpadState) {
	touchpadTestFrame(s, touchpadEvent{1, 0x110, 1})
	touchpadTestFrame(s, touchpadEvent{1, 0x110, 0})
}

func touchpadTestCompleteEvents() []touchpadEvent {
	return []touchpadEvent{
		{1, 0x14a, 1}, {1, 0x145, 1}, {3, 0, 100}, {3, 1, 100}, {0, 0, 0},
		{3, 0, 105}, {0, 0, 0}, {3, 0, 100}, {0, 0, 0},
		{3, 1, 105}, {0, 0, 0}, {3, 1, 100}, {0, 0, 0},
		{1, 0x14a, 0}, {1, 0x145, 0}, {0, 0, 0},
		{1, 0x110, 1}, {0, 0, 0}, {1, 0x110, 0}, {0, 0, 0},
	}
}

func TestTouchpadThresholdLiteralBoundaries(t *testing.T) {
	for _, c := range []struct {
		name  string
		axis  TouchpadAxis
		want  int64
		valid bool
	}{
		{"unit", TouchpadAxis{Minimum: 0, Maximum: 1}, 1, true},
		{"rounded", TouchpadAxis{Minimum: 0, Maximum: 21}, 2, true},
		{"signed_span", TouchpadAxis{Minimum: -2147483648, Maximum: 2147483647}, 214748365, true},
		{"fuzz", TouchpadAxis{Minimum: 0, Maximum: 100, Fuzz: 3}, 13, true},
		{"flat", TouchpadAxis{Minimum: 0, Maximum: 100, Flat: 7}, 15, true},
		{"fuzz_exceeds_threshold", TouchpadAxis{Minimum: 0, Maximum: 100, Fuzz: 25}, 101, false},
		{"flat_exceeds_threshold", TouchpadAxis{Minimum: 0, Maximum: 100, Flat: 50}, 101, false},
		{"equal", TouchpadAxis{Minimum: 1, Maximum: 1}, 0, false},
		{"reversed", TouchpadAxis{Minimum: 2, Maximum: 1}, 0, false},
		{"negative_fuzz", TouchpadAxis{Maximum: 100, Fuzz: -1}, 0, false},
		{"negative_flat", TouchpadAxis{Maximum: 100, Flat: -1}, 0, false},
		{"negative_resolution", TouchpadAxis{Maximum: 100, Resolution: -1}, 0, false},
		{"oversized_fuzz", TouchpadAxis{Maximum: 100, Fuzz: 101}, 0, false},
		{"oversized_flat", TouchpadAxis{Maximum: 100, Flat: 101}, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := TouchpadThreshold(c.axis)
			if got != c.want || ok != c.valid {
				t.Fatalf("literal threshold mismatch: %d %v", got, ok)
			}
		})
	}
}

func TestTouchpadFiniteCapabilitiesAndOwnedMetadata(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*TouchpadCapabilities)
		want   bool
	}{
		{"base", func(*TouchpadCapabilities) {}, true},
		{"optional", func(c *TouchpadCapabilities) {
			c.Keys = []uint16{0x110, 0x111, 0x112, 0x145, 0x14a}
			c.Events = append(c.Events, 4)
			c.Misc = []uint16{4, 5}
			c.Properties = append(c.Properties, 2)
			c.Axes = append(c.Axes, TouchpadAxis{Code: 24, Maximum: 255}, TouchpadAxis{Code: 28, Maximum: 31})
		}, true},
		{"version", func(c *TouchpadCapabilities) { c.ProtocolVersion = 0 }, false},
		{"missing_syn", func(c *TouchpadCapabilities) { c.Events = []uint16{1, 3} }, false},
		{"relative_class", func(c *TouchpadCapabilities) { c.Events = []uint16{0, 1, 2, 3} }, false},
		{"relative_axis", func(c *TouchpadCapabilities) { c.Relative = []uint16{0} }, false},
		{"missing_touch", func(c *TouchpadCapabilities) { c.Keys = []uint16{0x110, 0x145} }, false},
		{"missing_finger", func(c *TouchpadCapabilities) { c.Keys = []uint16{0x110, 0x14a} }, false},
		{"missing_button", func(c *TouchpadCapabilities) { c.Keys = []uint16{0x145, 0x14a} }, false},
		{"multi_finger", func(c *TouchpadCapabilities) { c.Keys = append(c.Keys, 0x14d) }, false},
		{"pen", func(c *TouchpadCapabilities) { c.Keys = []uint16{0x110, 0x140, 0x145, 0x14a} }, false},
		{"duplicate", func(c *TouchpadCapabilities) { c.Keys = []uint16{0x110, 0x110, 0x145, 0x14a} }, false},
		{"unordered", func(c *TouchpadCapabilities) { c.Keys = []uint16{0x145, 0x110, 0x14a} }, false},
		{"direct", func(c *TouchpadCapabilities) { c.Properties = []uint16{0, 1} }, false},
		{"semi_mt", func(c *TouchpadCapabilities) { c.Properties = []uint16{0, 3} }, false},
		{"topbutton", func(c *TouchpadCapabilities) { c.Properties = []uint16{0, 4} }, false},
		{"missing_pointer", func(c *TouchpadCapabilities) { c.Properties = nil }, false},
		{"mt_slot", func(c *TouchpadCapabilities) { c.Axes = append(c.Axes, TouchpadAxis{Code: 47, Maximum: 9}) }, false},
		{"unknown_abs", func(c *TouchpadCapabilities) { c.Axes = append(c.Axes, TouchpadAxis{Code: 2, Maximum: 9}) }, false},
		{"missing_y", func(c *TouchpadCapabilities) { c.Axes = c.Axes[:1] }, false},
		{"bad_optional_range", func(c *TouchpadCapabilities) { c.Axes = append(c.Axes, TouchpadAxis{Code: 24, Maximum: 1, Fuzz: 2}) }, false},
		{"unknown_msc", func(c *TouchpadCapabilities) { c.Events = append(c.Events, 4); c.Misc = []uint16{0} }, false},
		{"orphan_msc", func(c *TouchpadCapabilities) { c.Misc = []uint16{4} }, false},
		{"empty_msc", func(c *TouchpadCapabilities) { c.Events = append(c.Events, 4) }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			caps := touchpadTestCapabilities()
			c.change(&caps)
			x, y, ok := ValidateTouchpadCapabilities(caps)
			if ok != c.want || ok && (x != 5 || y != 5) {
				t.Fatal("wrong finite capability admission")
			}
		})
	}
	m := touchpadMetadata{capabilities: touchpadTestCapabilities()}
	copy, ok := copyTouchpadMetadata(m)
	if !ok {
		t.Fatal("copy rejected")
	}
	m.capabilities.Axes[0].Minimum = 101
	m.capabilities.Keys[0] = 0
	if copy.capabilities.Axes[0].Minimum != 50 || copy.capabilities.Keys[0] != 0x110 {
		t.Fatal("metadata authority aliases caller")
	}
	s := touchpadTestState()
	s.result.Capabilities.Axes[0].Minimum = 101
	touchpadTestLanding(s)
	if s.failed || s.result.XYBaselines != 1 {
		t.Fatal("state aliases exported capabilities")
	}
	m.capabilities.Keys = make([]uint16, 9)
	if _, ok := copyTouchpadMetadata(m); ok {
		t.Fatal("oversized metadata copied")
	}
}

func TestTouchpadFixedAnchorLiteralTraces(t *testing.T) {
	for _, c := range []struct {
		name               string
		positions          []int32
		positive, negative uint64
		anchor             int64
	}{
		{"small_positive_steps", []int32{101, 102, 103, 104, 105}, 1, 0, 105},
		{"oscillating_path", []int32{104, 96, 104, 96, 104, 96}, 0, 0, 100},
		{"updated_equalities", []int32{105, 104, 101, 100, 99, 96, 95, 100}, 2, 2, 100},
		{"one_large_jump", []int32{120}, 1, 0, 120},
		{"small_negative_steps", []int32{99, 98, 97, 96, 95}, 0, 1, 95},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := touchpadTestState()
			touchpadTestLanding(s)
			for _, position := range c.positions {
				touchpadTestFrame(s, touchpadEvent{3, 0, position})
			}
			if s.pendingMotion.xPositive != c.positive || s.pendingMotion.xNegative != c.negative || s.anchor[0] != c.anchor ||
				s.pendingMotion.yPositive != 0 || s.pendingMotion.yNegative != 0 || s.result.XPositive != 0 {
				t.Fatal("fixed-anchor or pending-credit rule changed")
			}
			touchpadTestRelease(s)
			if s.failed || s.result.XPositive != c.positive || s.result.XNegative != c.negative || s.result.QualifyingContacts != 0 {
				t.Fatal("release committed incorrect motion")
			}
		})
	}
}

func TestTouchpadLateAxisNeedsRepeatedSameContactCycle(t *testing.T) {
	for _, name := range []string{"late_y", "late_x"} {
		t.Run(name, func(t *testing.T) {
			s := touchpadTestState()
			frames := [][]touchpadEvent{
				{{1, 0x145, 1}, {1, 0x14a, 1}, {3, 0, 100}}, {{3, 0, 95}}, {{3, 0, 105}}, {{3, 1, 95}}, {{3, 1, 105}},
				{{3, 0, 95}}, {{3, 0, 105}}, {{3, 1, 95}}, {{3, 1, 105}}, {{1, 0x14a, 0}, {1, 0x145, 0}},
			}
			for index, frame := range frames {
				if name == "late_x" {
					for j := range frame {
						if frame[j].typeCode == 3 {
							frame[j].code ^= 1
						}
					}
				}
				touchpadTestFrame(s, frame...)
				if index < 3 && s.result.XYBaselines != 0 {
					t.Fatal("missing axis supplied baseline")
				}
				if index == 3 && (s.result.XYBaselines != 1 || s.pendingMotion != (touchpadMotion{})) {
					t.Fatal("baseline frame supplied credit")
				}
				if index == 4 && s.pendingMotion.complete() {
					t.Fatal("first incomplete cycle qualified")
				}
			}
			want := touchpadMotion{1, 1, 2, 1}
			if name == "late_x" {
				want = touchpadMotion{2, 1, 1, 1}
			}
			if s.failed || s.result.QualifyingContacts != 1 ||
				(touchpadMotion{s.result.XPositive, s.result.XNegative, s.result.YPositive, s.result.YNegative}) != want ||
				s.complete() {
				t.Fatal("late-axis released cycle or separate button criterion wrong")
			}
			touchpadTestButton(s)
			if !s.complete() || s.result.FinalNeutral {
				t.Fatal("event completion confused with terminal neutrality")
			}
		})
	}
}

func TestTouchpadContactBoundariesCannotCombineOrSeedMotion(t *testing.T) {
	for _, name := range []string{"split_contacts", "never_y", "landing_only", "release_jump", "missing_release"} {
		t.Run(name, func(t *testing.T) {
			s := touchpadTestState()
			switch name {
			case "split_contacts":
				touchpadTestLanding(s)
				touchpadTestFrame(s, touchpadEvent{3, 0, 105})
				touchpadTestFrame(s, touchpadEvent{3, 0, 100})
				touchpadTestRelease(s)
				touchpadTestLanding(s)
				touchpadTestFrame(s, touchpadEvent{3, 1, 105})
				touchpadTestFrame(s, touchpadEvent{3, 1, 100})
				touchpadTestRelease(s)
			case "never_y":
				touchpadTestFrame(s, touchpadEvent{1, 0x145, 1}, touchpadEvent{1, 0x14a, 1}, touchpadEvent{3, 0, 100})
				touchpadTestFrame(s, touchpadEvent{3, 0, 150})
				touchpadTestFrame(s, touchpadEvent{3, 0, 50})
				touchpadTestRelease(s)
			case "landing_only":
				touchpadTestFrame(s, touchpadEvent{3, 0, 50}, touchpadEvent{3, 1, 150})
				touchpadTestLanding(s)
				touchpadTestRelease(s)
			case "release_jump":
				touchpadTestLanding(s)
				touchpadTestFrame(s, touchpadEvent{1, 0x14a, 0}, touchpadEvent{1, 0x145, 0}, touchpadEvent{3, 0, 150}, touchpadEvent{3, 1, 50})
			case "missing_release":
				events := touchpadTestCompleteEvents()
				for _, e := range events[:13] {
					s.consume(e)
				}
			}
			touchpadTestButton(s)
			if s.failed || s.complete() || s.result.QualifyingContacts != 0 {
				t.Fatal("contact boundary supplied unsupported completion")
			}
			if name == "never_y" && s.result.XYBaselines != 0 {
				t.Fatal("unobserved axis supplied baseline")
			}
			if name == "landing_only" || name == "release_jump" {
				if s.result.XPositive+s.result.XNegative+s.result.YPositive+s.result.YNegative != 0 {
					t.Fatal("off-contact or lift motion credited")
				}
			}
		})
	}
}

func TestTouchpadPacketValidationIsAtomicAndOrderIndependent(t *testing.T) {
	first, second := touchpadTestState(), touchpadTestState()
	touchpadTestLanding(first)
	touchpadTestFrame(second, touchpadEvent{3, 1, 100}, touchpadEvent{1, 0x145, 1}, touchpadEvent{3, 0, 100}, touchpadEvent{1, 0x14a, 1})
	if !reflect.DeepEqual(first.result, second.result) || first.anchor != second.anchor {
		t.Fatal("field ordering became authority")
	}
	for _, c := range []struct {
		name string
		tail []touchpadEvent
	}{
		{"duplicate_axis", []touchpadEvent{{3, 0, 105}}},
		{"duplicate_key", []touchpadEvent{{1, 0x14a, 0}}},
		{"repeat", []touchpadEvent{{1, 0x110, 2}}},
		{"unmatched_release", []touchpadEvent{{1, 0x110, 0}}},
		{"outside_range", []touchpadEvent{{3, 1, 151}}},
		{"unadvertised_abs", []touchpadEvent{{3, 24, 1}}},
		{"unadvertised_key", []touchpadEvent{{1, 0x111, 1}}},
		{"relative", []touchpadEvent{{2, 0, 1}}},
		{"misc", []touchpadEvent{{4, 4, 1}}},
		{"unknown_sync", []touchpadEvent{{0, 2, 0}}},
		{"loss", []touchpadEvent{{0, 3, 0}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := touchpadTestState()
			frame := []touchpadEvent{{1, 0x145, 1}, {1, 0x14a, 1}, {3, 0, 100}}
			frame = append(frame, c.tail...)
			touchpadTestFrame(s, frame...)
			if !s.failed || s.result.Frames != 0 || s.result.ContactStarts != 0 || s.result.XYBaselines != 0 {
				t.Fatal("invalid suffix committed a prefix")
			}
			for _, e := range touchpadTestCompleteEvents() {
				s.consume(e)
			}
			if s.result.QualifyingContacts != 0 || s.result.FinalNeutral || s.result.EvidenceLoss != (c.name == "loss") {
				t.Fatal("latched rejection was erased")
			}
		})
	}
	s := touchpadTestState()
	touchpadTestFrame(s, touchpadEvent{1, 0x14a, 1})
	if !s.failed || s.result.ContactStarts != 0 {
		t.Fatal("contact without finger accepted")
	}
}

func TestTouchpadSignedReportValuesAndEmptyFrames(t *testing.T) {
	for _, c := range []struct {
		name  string
		value int32
	}{{"zero", 0}, {"positive", 1}, {"negative", -1}, {"minimum", -2147483648}, {"maximum", 2147483647}} {
		t.Run(c.name, func(t *testing.T) {
			s := touchpadTestState()
			s.consume(touchpadEvent{0, 0, c.value})
			if s.result.ContactStarts != 0 || s.result.Buttons[0].Pairs != 0 {
				t.Fatal("empty report supplied actions")
			}
			for _, e := range touchpadTestCompleteEvents() {
				if e.typeCode == 0 {
					e.value = c.value
				}
				s.consume(e)
			}
			if s.failed || !s.complete() || s.result.FinalNeutral {
				t.Fatal("undefined report value changed frame meaning")
			}
			for _, code := range []uint16{2, 3} {
				fault := touchpadTestState()
				fault.consume(touchpadEvent{0, code, c.value})
				if !fault.failed || fault.result.EvidenceLoss != (code == 3) {
					t.Fatal("report compatibility weakened sync rejection")
				}
			}
		})
	}
}

func TestTouchpadStateAndGlobalRecordBounds(t *testing.T) {
	device, total := uint64(1048575), uint64(8388607)
	if !touchpadRecordQuota(&device, &total) || device != 1048576 || total != 8388608 {
		t.Fatal("last admitted record mismatch")
	}
	if touchpadRecordQuota(&device, &total) || device != 1048576 || total != 8388608 {
		t.Fatal("rejected record mutated quotas")
	}
	device, total = 0, 8388608
	if touchpadRecordQuota(&device, &total) || device != 0 {
		t.Fatal("global cap advanced local quota")
	}
	device, total = 1048576, 0
	if touchpadRecordQuota(&device, &total) || total != 0 {
		t.Fatal("device cap advanced global quota")
	}
	s := touchpadTestState()
	s.records = 1048576
	s.consume(touchpadEvent{0, 0, 0})
	if !s.failed || s.result.Frames != 0 {
		t.Fatal("record cap wrapped")
	}
	s = touchpadTestState()
	s.result.Frames = ^uint64(0)
	s.consume(touchpadEvent{0, 0, 0})
	if !s.failed || s.result.Frames != ^uint64(0) {
		t.Fatal("counter overflow changed evidence")
	}
	for _, count := range []int{4096, 4097} {
		c := touchpadTestCapabilities()
		c.Events = append(c.Events, 4)
		c.Misc = []uint16{4}
		s = newTouchpadState(&TouchpadDeviceResult{Capabilities: &c, BaselineNeutral: true})
		for i := 0; i < count; i++ {
			s.consume(touchpadEvent{4, 4, 123})
		}
		s.consume(touchpadEvent{0, 0, 0})
		if s.failed != (count == 4097) || s.result.QualifyingContacts != 0 {
			t.Fatal("packet bound or auxiliary credit changed")
		}
	}
}

func TestTouchpadSignedExtremesUseWidenedMotionArithmetic(t *testing.T) {
	c := touchpadTestCapabilities()
	c.Axes[0].Minimum, c.Axes[1].Minimum = -2147483648, -2147483648
	c.Axes[0].Maximum, c.Axes[1].Maximum = 2147483647, 2147483647
	s := newTouchpadState(&TouchpadDeviceResult{Capabilities: &c, BaselineNeutral: true})
	touchpadTestFrame(s, touchpadEvent{1, 0x145, 1}, touchpadEvent{1, 0x14a, 1}, touchpadEvent{3, 0, -2147483648}, touchpadEvent{3, 1, -2147483648})
	touchpadTestFrame(s, touchpadEvent{3, 0, 2147483647}, touchpadEvent{3, 1, 2147483647})
	touchpadTestFrame(s, touchpadEvent{3, 0, -2147483648}, touchpadEvent{3, 1, -2147483648})
	touchpadTestRelease(s)
	touchpadTestButton(s)
	if !s.complete() || s.result.XThreshold != 214748365 || s.result.YThreshold != 214748365 ||
		s.result.XPositive != 1 || s.result.XNegative != 1 || s.result.YPositive != 1 || s.result.YNegative != 1 {
		t.Fatal("32-bit narrowing or multi-threshold overcount")
	}
}

func TestTouchpadOptionalAxesValidateButNeverSupplyMotion(t *testing.T) {
	for _, name := range []string{"valid_uncredited", "invalid_pressure_suffix", "invalid_width_suffix"} {
		t.Run(name, func(t *testing.T) {
			c := touchpadTestCapabilities()
			c.Events = append(c.Events, 4)
			c.Misc = []uint16{4, 5}
			c.Axes = append(c.Axes, TouchpadAxis{Code: 24, Maximum: 255}, TouchpadAxis{Code: 28, Maximum: 31})
			s := newTouchpadState(&TouchpadDeviceResult{Capabilities: &c, BaselineNeutral: true})
			touchpadTestFrame(s, touchpadEvent{1, 0x145, 1}, touchpadEvent{1, 0x14a, 1},
				touchpadEvent{3, 24, 255}, touchpadEvent{3, 28, 31}, touchpadEvent{4, 4, 12345}, touchpadEvent{4, 5, -12345})
			if s.failed || s.result.XYBaselines != 0 || s.pendingMotion != (touchpadMotion{}) {
				t.Fatal("optional payload supplied coordinate baseline")
			}
			touchpadTestFrame(s, touchpadEvent{3, 0, 100}, touchpadEvent{3, 1, 100})
			if name == "valid_uncredited" {
				touchpadTestFrame(s, touchpadEvent{3, 24, 0}, touchpadEvent{3, 28, 0})
				if s.failed || s.pendingMotion != (touchpadMotion{}) || s.result.QualifyingContacts != 0 {
					t.Fatal("optional payload supplied motion")
				}
			} else {
				code, value := uint16(24), int32(256)
				if name == "invalid_width_suffix" {
					code, value = 28, -1
				}
				touchpadTestFrame(s, touchpadEvent{3, 0, 105}, touchpadEvent{3, code, value})
				if !s.failed || s.result.Frames != 2 || s.pendingMotion != (touchpadMotion{}) {
					t.Fatal("invalid optional suffix committed motion prefix")
				}
			}
		})
	}
}

func TestTouchpadOptionalButtonsNeedNeutralityNotExtraPairs(t *testing.T) {
	for _, name := range []string{"unused_optional", "held_optional"} {
		t.Run(name, func(t *testing.T) {
			c := touchpadTestCapabilities()
			c.Keys = []uint16{0x110, 0x111, 0x112, 0x145, 0x14a}
			s := newTouchpadState(&TouchpadDeviceResult{Capabilities: &c, BaselineNeutral: true})
			for _, e := range touchpadTestCompleteEvents() {
				s.consume(e)
			}
			if !s.complete() || len(s.result.Buttons) != 3 || s.result.Buttons[1].Pairs != 0 || s.result.Buttons[2].Pairs != 0 {
				t.Fatal("optional buttons invented required actions")
			}
			if name == "held_optional" {
				touchpadTestFrame(s, touchpadEvent{1, 0x111, 1})
				if s.complete() {
					t.Fatal("held advertised button retained completion")
				}
				touchpadTestFrame(s, touchpadEvent{1, 0x111, 0})
				if !s.complete() || s.result.Buttons[1].Pairs != 1 {
					t.Fatal("fresh optional release was lost")
				}
			}
		})
	}
}
