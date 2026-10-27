package inputtest

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func wheelState(axes ...PointerWheelCapability) *pointerState {
	capabilities := pointerTestCapabilities("left")
	capabilities.WheelAxes = append([]PointerWheelCapability{}, axes...)
	return newPointerState(&PointerDeviceResult{Supported: true, Readable: true, Capabilities: &capabilities})
}

func TestPointerWheelDirectionRequirementsSelectAdvertisedStream(t *testing.T) {
	for _, test := range []struct {
		name  string
		axes  []PointerWheelCapability
		codes []uint16
	}{
		{"none", nil, nil},
		{"vertical legacy", []PointerWheelCapability{{Axis: "vertical", Legacy: true}}, []uint16{8}},
		{"horizontal legacy", []PointerWheelCapability{{Axis: "horizontal", Legacy: true}}, []uint16{6}},
		{"vertical high resolution only", []PointerWheelCapability{{Axis: "vertical", HighResolution: true}}, []uint16{11}},
		{"horizontal high resolution only", []PointerWheelCapability{{Axis: "horizontal", HighResolution: true}}, []uint16{12}},
		{"mixed", []PointerWheelCapability{{Axis: "vertical", Legacy: true}, {Axis: "horizontal", Legacy: true, HighResolution: true}}, []uint16{8, 12}},
		{"both high resolution", []PointerWheelCapability{{Axis: "vertical", Legacy: true, HighResolution: true}, {Axis: "horizontal", HighResolution: true}}, []uint16{11, 12}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := wheelState(test.axes...)
			for _, event := range pointerCompleteEvents(0x110) {
				state.consume(event)
			}
			if state.complete() != (len(test.codes) == 0) {
				t.Fatal("advertised wheel criteria were not required")
			}
			for _, code := range test.codes {
				pointerPacket(state, pointerEvent{2, code, 1})
				if state.complete() {
					t.Fatal("one direction passed")
				}
				pointerPacket(state, pointerEvent{2, code, -1})
			}
			if !state.complete() || state.result.Wheels == nil {
				t.Fatal("complete selected streams did not pass")
			}
			for _, wheel := range state.result.Wheels {
				if wheel.PositiveFrames != 1 || wheel.NegativeFrames != 1 {
					t.Fatal("direction counts imply a detent threshold or duplicate credit")
				}
			}
		})
	}
}

func TestPointerHighResolutionCompanionsNeverSupplyOrContradictCredit(t *testing.T) {
	for _, pair := range [][2]uint16{{8, 11}, {6, 12}} {
		axis := "vertical"
		if pair[0] == 6 {
			axis = "horizontal"
		}
		state := wheelState(PointerWheelCapability{Axis: axis, Legacy: true, HighResolution: true})
		pointerPacket(state, pointerEvent{2, pair[0], 3})
		pointerPacket(state, pointerEvent{2, pair[0], -3})
		if state.failed || state.result.Wheels[0].PositiveFrames != 0 || state.result.Wheels[0].NegativeFrames != 0 {
			t.Fatal("quiet high-resolution stream fell back to legacy")
		}
		pointerPacket(state, pointerEvent{2, pair[1], 1}, pointerEvent{2, pair[0], -10})
		pointerPacket(state, pointerEvent{2, pair[0], 20})
		pointerPacket(state, pointerEvent{2, pair[1], -1})
		if state.failed || state.result.Wheels[0].PositiveFrames != 1 || state.result.Wheels[0].NegativeFrames != 1 {
			t.Fatal("nonaligned companions changed selected-stream directions")
		}
	}
}

func TestPointerWheelPacketValidationIsAtomic(t *testing.T) {
	for _, test := range []struct {
		name string
		tail []pointerEvent
	}{
		{"unmatched button", []pointerEvent{{1, 0x110, 0}}},
		{"unadvertised legacy", []pointerEvent{{2, 8, 1}}},
		{"unadvertised horizontal", []pointerEvent{{2, 12, 1}}},
		{"lost", []pointerEvent{{0, 3, 0}}},
		{"absolute", []pointerEvent{{3, 0, 1}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := wheelState(PointerWheelCapability{Axis: "vertical", HighResolution: true})
			state.consume(pointerEvent{2, 11, 1})
			state.consume(pointerEvent{2, 0, 1})
			for _, event := range test.tail {
				state.consume(event)
			}
			state.consume(pointerEvent{0, 0, 0})
			if !state.failed || state.result.Frames != 0 || state.result.XPositive != 0 || state.result.Wheels[0].PositiveFrames != 0 {
				t.Fatal("bad packet retained partial criteria")
			}
		})
	}
	state := wheelState(PointerWheelCapability{Axis: "vertical", Legacy: true})
	pointerPacket(state, pointerEvent{2, 8, 0}, pointerEvent{2, 8, 31}, pointerEvent{2, 8, -31})
	if state.failed || state.result.Frames != 1 || state.result.Wheels[0].PositiveFrames != 0 || state.result.Wheels[0].NegativeFrames != 0 {
		t.Fatal("net zero manufactured directions")
	}
	state.consume(pointerEvent{2, 8, -1})
	if state.result.Wheels[0].NegativeFrames != 0 {
		t.Fatal("partial packet counted")
	}
}

func TestPointerWheelAccumulatorsAndCountersStayBounded(t *testing.T) {
	state := wheelState(PointerWheelCapability{Axis: "horizontal", HighResolution: true})
	for index := 0; index < maximumPointerFrameEvents; index++ {
		state.consume(pointerEvent{2, 12, math.MaxInt32})
	}
	state.consume(pointerEvent{0, 0, 0})
	if state.failed || state.result.Wheels[0].PositiveFrames != 1 {
		t.Fatal("bounded signed frame sum overflowed")
	}
	for index := 0; index <= maximumPointerFrameEvents; index++ {
		state.consume(pointerEvent{2, 12, math.MinInt32})
	}
	state.consume(pointerEvent{0, 0, 0})
	if !state.failed || state.result.Frames != 1 || state.result.Wheels[0].NegativeFrames != 0 {
		t.Fatal("oversized packet credited")
	}
	state = wheelState(PointerWheelCapability{Axis: "vertical", Legacy: true})
	state.result.Frames, state.result.Wheels[0].PositiveFrames = math.MaxUint64-1, math.MaxUint64-1
	pointerPacket(state, pointerEvent{2, 8, 1})
	pointerPacket(state, pointerEvent{2, 8, 1})
	if !state.failed || state.result.Frames != math.MaxUint64 || state.result.Wheels[0].PositiveFrames != math.MaxUint64 {
		t.Fatal("counter wrapped or failed frame changed evidence")
	}
}

func wheelSource(size int) *pointerFixtureSource {
	source := completePointerSource(size)
	source.capabilities.WheelAxes = []PointerWheelCapability{{Axis: "vertical", Legacy: true, HighResolution: true}}
	source.data = append(source.data, pointerFixtureBytes(size, []pointerEvent{{2, 11, 1}, {0, 0, 0}, {2, 11, -1}, {0, 0, 0}})...)
	return source
}

func TestPointerWheelRunnerKeepsDevicesIndependentAndStopsBeforeCredit(t *testing.T) {
	for _, size := range []int{16, 24} {
		first, second := wheelSource(size), wheelSource(size)
		result := runPointerWith(t.Context(), PointerOptions{Duration: time.Second}, pointerFixtureBackend(first, second))
		if result.Status != StatusPass || first.closes != 1 || second.closes != 1 || len(result.Devices[0].Wheels) != 1 {
			t.Fatal("complete wheel sources did not join and pass independently")
		}
		first, second = wheelSource(size), wheelSource(size)
		second.data = second.data[:len(second.data)-size*2]
		result = runPointerWith(t.Context(), PointerOptions{Duration: 20 * time.Millisecond}, pointerFixtureBackend(first, second))
		if result.Status == StatusPass || result.Devices[1].Wheels[0].NegativeFrames != 0 || first.closes != 1 || second.closes != 1 {
			t.Fatal("one device supplied another's missing wheel direction")
		}
		source := wheelSource(size)
		ctx, cancel := context.WithCancel(t.Context())
		source.beforeRead = func() {
			if source.reads == 2 {
				cancel()
			}
		}
		result = runPointerWith(ctx, PointerOptions{Duration: time.Second}, pointerFixtureBackend(source))
		cancel()
		if result.Status != StatusAborted || source.closes != 1 || result.Devices[0].Wheels[0].PositiveFrames != 0 {
			t.Fatal("post-stop events received credit")
		}
	}
}

func TestPointerWheelArraysSurviveEveryEarlyDeviceReturn(t *testing.T) {
	for _, kind := range []string{"open failure", "unsupported", "record layout", "cancel before open", "retained pass with close failure"} {
		t.Run(kind, func(t *testing.T) {
			source := wheelSource(24)
			backend := pointerFixtureBackend(source)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch kind {
			case "open failure":
				backend.open = func(context.Context, string, string) (pointerOpened, error) {
					return pointerOpened{}, errors.New("unavailable")
				}
			case "unsupported":
				source.capabilities.Absolute = true
			case "record layout":
				source.size = 7
			case "cancel before open":
				discover := backend.discover
				backend.discover = func(ctx context.Context, root string) ([]string, error) {
					names, err := discover(ctx, root)
					cancel()
					return names, err
				}
			case "retained pass with close failure":
				source.closeError = errors.New("uncertain")
			}
			result := runPointerWith(ctx, PointerOptions{Duration: time.Second}, backend)
			if result.Status == StatusPass || len(result.Devices) != 1 || result.Devices[0].Wheels == nil {
				t.Fatal("early return lost explicit wheel observation array")
			}
			if kind == "record layout" && (len(result.Devices[0].Wheels) != 1 || result.Devices[0].Wheels[0].PositiveFrames != 0) {
				t.Fatal("readable supported criteria disappeared before record-layout rejection")
			}
			if kind == "retained pass with close failure" && result.Devices[0].Status != StatusPass {
				t.Fatal("aggregate cleanup failure erased completed device observations")
			}
		})
	}
}

func TestPointerStaleWheelDirectionsNeverSupplyNewCriteria(t *testing.T) {
	source := wheelSource(24)
	source.stale = pointerFixtureBytes(24, []pointerEvent{{2, 11, 1}, {0, 0, 0}, {2, 11, -1}, {0, 0, 0}})
	source.data = pointerFixtureBytes(24, pointerCompleteEvents(0x110, 0x111))
	result := runPointerWith(t.Context(), PointerOptions{Duration: 20 * time.Millisecond}, pointerFixtureBackend(source))
	if result.Status != StatusInconclusive || result.StopReason != "observation_timeout" || source.closes != 1 ||
		result.Devices[0].Wheels[0].PositiveFrames != 0 || result.Devices[0].Wheels[0].NegativeFrames != 0 || result.Devices[0].XPositive == 0 {
		t.Fatal("queued wheel actions were credited or fresh movement was not reached")
	}
}

func TestPointerPartialAggregateKeepsCompletedWheelDevice(t *testing.T) {
	first, second := wheelSource(24), wheelSource(24)
	second.capabilities.Absolute = true
	result := runPointerWith(t.Context(), PointerOptions{Duration: time.Second}, pointerFixtureBackend(first, second))
	if result.Status == StatusPass || result.Devices[0].Status != StatusPass || result.Devices[0].Wheels[0].PositiveFrames != 1 ||
		result.Devices[0].Wheels[0].NegativeFrames != 1 || len(result.Devices[1].Wheels) != 0 || result.Devices[1].Wheels == nil || first.closes != 1 || second.closes != 1 {
		t.Fatal("unsupported neighbor erased or supplied completed wheel criteria")
	}
}

// A retry supplies neither a baseline queue-empty observation nor the final
// queue-empty observation. Keep the fixture's queued data untouched on retry.
func pointerRetryBackend(source *pointerFixtureSource, retry func(int) bool) pointerBackend {
	backend := pointerFixtureBackend(source)
	open := backend.open
	backend.open = func(ctx context.Context, root, name string) (pointerOpened, error) {
		value, err := open(ctx, root, name)
		if err != nil {
			return value, err
		}
		read, calls := value.read, 0
		value.read = func(buffer []byte) (int, error) {
			calls++
			if retry(calls) {
				return 0, errPointerRetry
			}
			return read(buffer)
		}
		return value, nil
	}
	return backend
}

func TestPointerInterruptedReadCannotProveEmptyQueue(t *testing.T) {
	for _, condition := range []string{"stale baseline", "queued loss", "partial tail"} {
		t.Run(condition, func(t *testing.T) {
			source := wheelSource(24)
			interruptAt := 3
			switch condition {
			case "stale baseline":
				source.stale, source.data = source.data, nil
				interruptAt = 1
			case "queued loss":
				source.chunk = len(source.data)
				source.data = append(source.data, pointerFixtureBytes(24, []pointerEvent{{0, 3, 0}})...)
			case "partial tail":
				source.chunk = len(source.data)
				source.data = append(source.data, 1)
			}
			backend := pointerRetryBackend(source, func(call int) bool { return call == interruptAt })
			result := runPointerWith(context.Background(), PointerOptions{Duration: 50 * time.Millisecond}, backend)
			if result.Status == StatusPass || len(result.Devices) != 1 || result.Devices[0].Status == StatusPass || source.closes != 1 || len(source.stale) != 0 || len(source.data) != 0 {
				t.Fatal("interrupted read established idle or left the queued counterexample unread")
			}
			device := result.Devices[0]
			if condition == "stale baseline" {
				if device.Frames != 0 || device.Wheels[0].PositiveFrames != 0 || device.Wheels[0].NegativeFrames != 0 || result.StopReason != "observation_timeout" {
					t.Fatal("interrupted baseline credited stale criteria")
				}
			} else if device.Frames == 0 || device.Wheels[0].PositiveFrames != 1 || device.Wheels[0].NegativeFrames != 1 {
				t.Fatal("queue uncertainty erased the already observed criteria")
			}
			if condition == "queued loss" && !device.EvidenceLoss {
				t.Fatal("interrupted final read hid queued loss")
			}
			if condition == "partial tail" && (device.Detail != "pointer observation ended with a partial event packet" || result.StopReason != "observation_timeout") {
				t.Fatal("interrupted final read hid a partial tail")
			}
		})
	}
}

func TestPointerInterruptedReadResumesWithoutLosingCriteria(t *testing.T) {
	for _, interruptAt := range []int{1, 3} {
		source := wheelSource(24)
		backend := pointerRetryBackend(source, func(call int) bool { return call == interruptAt })
		result := runPointerWith(t.Context(), PointerOptions{Duration: time.Second}, backend)
		if result.Status != StatusPass || source.closes != 1 || result.Devices[0].Wheels[0].PositiveFrames != 1 || result.Devices[0].Wheels[0].NegativeFrames != 1 {
			t.Fatal("transient interruption changed complete observed criteria")
		}
	}
}

func TestPointerRepeatedInterruptedReadsRemainBounded(t *testing.T) {
	for _, phase := range []string{"baseline", "completion"} {
		for _, end := range []string{"duration", "context deadline", "cancellation"} {
			t.Run(phase+"/"+end, func(t *testing.T) {
				source := wheelSource(24)
				ctx := context.Background()
				cancel := func() {}
				if end == "cancellation" {
					ctx, cancel = context.WithCancel(ctx)
				} else if end == "context deadline" {
					ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				}
				defer cancel()
				first := 1
				if phase == "completion" {
					first = 3
				}
				retries := 0
				backend := pointerRetryBackend(source, func(call int) bool {
					if call < first {
						return false
					}
					retries++
					if retries == 3 && end == "cancellation" {
						cancel()
					}
					return true
				})
				duration := 50 * time.Millisecond
				if end != "duration" {
					duration = time.Second
				}
				result := runPointerWith(ctx, PointerOptions{Duration: duration}, backend)
				if result.Status == StatusPass || source.closes != 1 || retries == 0 || result.Devices[0].Status == StatusPass {
					t.Fatal("repeated interruption passed or lost owned cleanup")
				}
				if end == "duration" && (result.Status != StatusInconclusive || result.StopReason != "observation_timeout" || result.DurationMilliseconds < duration.Milliseconds()) {
					t.Fatal("finite duration did not stop retries with a background context")
				}
				if end == "cancellation" && (result.Status != StatusAborted || result.StopReason != "operator_cancelled" || retries != 3) {
					t.Fatal("cancellation did not stop retries before further observation")
				}
				if end == "context deadline" && (result.Status != StatusError || result.StopReason != "deadline") {
					t.Fatal("parent deadline did not stop interrupted reads")
				}
				device := result.Devices[0]
				if phase == "baseline" && device.Frames != 0 {
					t.Fatal("retry supplied a baseline")
				}
				if phase == "completion" && (device.Wheels[0].PositiveFrames != 1 || device.Wheels[0].NegativeFrames != 1) {
					t.Fatal("terminal retry erased completed observations")
				}
			})
		}
	}
}
