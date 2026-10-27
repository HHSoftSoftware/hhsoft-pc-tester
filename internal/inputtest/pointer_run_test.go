package inputtest

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type pointerFixtureSource struct {
	capabilities          PointerCapabilities
	data, stale           []byte
	size, chunk           int
	terminal              error
	endError              error
	reads, checks, closes int
	ready                 bool
	beforeRead            func()
	check                 func(int) error
	closeError            error
}

func pointerFixtureBytes(size int, events []pointerEvent) []byte {
	encoded := make([]byte, size*len(events))
	for index, event := range events {
		record := encoded[index*size+size-8:]
		binary.NativeEndian.PutUint16(record, event.typeCode)
		binary.NativeEndian.PutUint16(record[2:], event.code)
		binary.NativeEndian.PutUint32(record[4:], uint32(event.value))
	}
	return encoded
}

func pointerFixtureBackend(sources ...*pointerFixtureSource) pointerBackend {
	names := []string{"event2", "event7", "event9"}
	return pointerBackend{
		discover: func(context.Context, string) ([]string, error) {
			return append([]string(nil), names[:len(sources)]...), nil
		},
		open: func(_ context.Context, _, name string) (pointerOpened, error) {
			for index, source := range sources {
				if names[index] != name {
					continue
				}
				return pointerOpened{identity: PointerIdentity{3, 1, uint16(index + 1), 1}, capabilities: source.capabilities,
					eventSize: source.size, typeOffset: source.size - 8,
					validate: func() error {
						source.checks++
						if source.check != nil {
							return source.check(source.checks)
						}
						return nil
					},
					close: func() error { source.closes++; return source.closeError },
					read: func(buffer []byte) (int, error) {
						source.reads++
						if source.beforeRead != nil {
							source.beforeRead()
						}
						data := &source.data
						if !source.ready {
							data = &source.stale
						}
						if len(*data) == 0 {
							if source.ready && source.endError != nil {
								return 0, source.endError
							}
							source.ready = true
							return 0, errPointerIdle
						}
						count := min(len(buffer), len(*data))
						if source.chunk > 0 {
							count = min(count, source.chunk)
						}
						copy(buffer, (*data)[:count])
						*data = (*data)[count:]
						return count, source.terminal
					}}, nil
			}
			return pointerOpened{}, errors.New("fixture interface unavailable")
		},
	}
}

func completePointerSource(size int) *pointerFixtureSource {
	return &pointerFixtureSource{capabilities: pointerTestCapabilities("left", "right"), size: size,
		data: pointerFixtureBytes(size, pointerCompleteEvents(0x110, 0x111))}
}

func TestPointerRunnerKeepsDeviceCriteriaIndependent(t *testing.T) {
	for _, size := range []int{16, 24} {
		first, second := completePointerSource(size), completePointerSource(size)
		result := runPointerWith(t.Context(), PointerOptions{Duration: time.Second}, pointerFixtureBackend(first, second))
		if result.Status != StatusPass || result.StopReason != "completed" || len(result.Devices) != 2 || first.closes != 1 || second.closes != 1 {
			t.Fatalf("complete interfaces were not independently passed: %#v", result)
		}
		first, second = completePointerSource(size), completePointerSource(size)
		second.capabilities.WheelAxes = []PointerWheelCapability{{Axis: "vertical", Legacy: true}}
		second.data = pointerFixtureBytes(size, []pointerEvent{{2, 8, 1}, {0, 0, 0}})
		result = runPointerWith(t.Context(), PointerOptions{Duration: 20 * time.Millisecond}, pointerFixtureBackend(first, second))
		if result.Status == StatusPass || result.StopReason != "observation_timeout" || result.Devices[1].XPositive != 0 || first.closes != 1 || second.closes != 1 {
			t.Fatal("one interface supplied another interface's movement")
		}
	}
}

func TestPointerRunnerRejectsEndOfStreamIdentityAndPartialFrames(t *testing.T) {
	for _, condition := range []string{"disconnect", "next read disconnect", "lost after complete", "next chunk loss", "next chunk release", "partial frame", "partial record", "replacement", "close failure"} {
		t.Run(condition, func(t *testing.T) {
			source := completePointerSource(24)
			switch condition {
			case "disconnect":
				source.terminal = io.EOF
			case "next read disconnect":
				source.endError = io.EOF
			case "lost after complete":
				source.data = append(source.data, pointerFixtureBytes(24, []pointerEvent{{0, 3, 0}})...)
			case "next chunk loss":
				source.chunk = len(source.data)
				source.data = append(source.data, pointerFixtureBytes(24, []pointerEvent{{0, 3, 0}})...)
			case "next chunk release":
				source.chunk = len(source.data)
				source.data = append(source.data, pointerFixtureBytes(24, []pointerEvent{{1, 0x110, 0}, {0, 0, 0}})...)
			case "partial frame":
				source.data = append(source.data, pointerFixtureBytes(24, []pointerEvent{{2, 0, 1}})...)
			case "partial record":
				source.data = append(source.data, 1)
			case "replacement":
				source.check = func(count int) error {
					if count > 3 {
						return errors.New("changed")
					}
					return nil
				}
			case "close failure":
				source.closeError = errors.New("not closed")
			}
			result := runPointerWith(t.Context(), PointerOptions{Duration: time.Second}, pointerFixtureBackend(source))
			if result.Status == StatusPass || source.closes != 1 || len(result.Devices) != 1 {
				t.Fatal("uncertain terminal observations passed or input was not closed")
			}
			device := result.Devices[0]
			if device.Frames == 0 || source.reads < 2 || len(source.data) != 0 {
				t.Fatal("fixture did not process the complete sequence and injected tail")
			}
			if condition == "partial frame" || condition == "partial record" {
				if result.Status != StatusInconclusive || result.StopReason != "observation_timeout" || device.Detail != "pointer observation ended with a partial event packet" {
					t.Fatal("partial-tail case did not reach its intended observation-window stop")
				}
				return
			}
			if result.StopReason != "error" {
				t.Fatal("an unrelated timeout or cancellation hid the injected failure")
			}
			switch condition {
			case "disconnect", "next read disconnect":
				if device.Detail != "pointer event stream disconnected or became unreadable" {
					t.Fatal("disconnect was not observed")
				}
			case "lost after complete", "next chunk loss":
				if !device.EvidenceLoss || device.Detail != "pointer event stream lost records" {
					t.Fatal("queued record loss was not observed")
				}
			case "next chunk release":
				if device.Detail != "unmatched pointer button transition" {
					t.Fatal("queued unmatched release was not observed")
				}
			case "replacement":
				if source.checks < 4 || device.Detail != "pointer identity changed before completion" {
					t.Fatal("replacement was not detected at final identity validation")
				}
			case "close failure":
				if result.Status != StatusError || result.Detail != "pointer descriptor cleanup failed" || device.Status != StatusPass {
					t.Fatal("cleanup failure did not follow otherwise complete observations")
				}
			}
			if (condition == "next read disconnect" || condition == "next chunk loss" || condition == "next chunk release") && source.reads < 3 {
				t.Fatal("post-completion read was never reached")
			}
		})
	}
}

func TestPointerRunnerStopsBeforeLateEventsCanPass(t *testing.T) {
	for _, condition := range []string{"before discovery", "during read", "final validation", "parent deadline", "observation deadline"} {
		t.Run(condition, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			source := completePointerSource(16)
			duration := time.Second
			if condition == "before discovery" {
				cancel()
			}
			if condition == "during read" {
				source.beforeRead = func() {
					if source.ready {
						cancel()
					}
				}
			}
			if condition == "final validation" {
				source.check = func(count int) error {
					if count > 3 {
						cancel()
					}
					return nil
				}
			}
			if condition == "parent deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 10*time.Millisecond)
				defer stop()
				source.beforeRead = func() {
					if source.ready {
						time.Sleep(20 * time.Millisecond)
					}
				}
			}
			if condition == "observation deadline" {
				duration = 10 * time.Millisecond
				source.beforeRead = func() {
					if source.ready {
						time.Sleep(20 * time.Millisecond)
					}
				}
			}
			result := runPointerWith(ctx, PointerOptions{Duration: duration}, pointerFixtureBackend(source))
			if result.Status == StatusPass {
				t.Fatal("late observations passed")
			}
			if condition == "before discovery" && (source.reads != 0 || source.closes != 0) {
				t.Fatal("pre-cancelled run opened input")
			}
			if condition != "before discovery" && source.closes != 1 {
				t.Fatal("interrupted input was not closed")
			}
			if condition == "parent deadline" && (result.Status != StatusError || result.StopReason != "deadline") {
				t.Fatal("parent deadline was hidden")
			}
		})
	}
}

func TestPointerRunnerHandlesFragmentedRecordsAndUnsupportedDevices(t *testing.T) {
	source := completePointerSource(16)
	source.chunk = 11
	result := runPointerWith(t.Context(), PointerOptions{Duration: time.Second}, pointerFixtureBackend(source))
	if result.Status != StatusPass {
		t.Fatal("fragmented complete event records were lost")
	}
	first, second := completePointerSource(24), completePointerSource(24)
	second.capabilities.Absolute = true
	result = runPointerWith(t.Context(), PointerOptions{Duration: time.Second}, pointerFixtureBackend(first, second))
	if result.Status == StatusPass || result.Devices[1].Supported || second.reads != 0 || second.closes != 1 {
		t.Fatal("absolute pointer was read or passed")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"\"code\"", "\"name\"", "\"events\"", "\"text\"", "\"uniq\"", "\"phys\""} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("raw input data entered aggregate evidence")
		}
	}
}

func TestPointerInitialQueueAndHeldButtonsCannotSupplyFreshCriteria(t *testing.T) {
	for _, kind := range []string{"stale complete", "initially held", "repeat release", "stale partial", "stale lost"} {
		t.Run(kind, func(t *testing.T) {
			source := completePointerSource(24)
			switch kind {
			case "stale complete":
				source.stale, source.data = source.data, nil
			case "initially held":
				source.stale = pointerFixtureBytes(24, []pointerEvent{{1, 0x110, 1}, {0, 0, 0}})
				source.data = append(pointerFixtureBytes(24, []pointerEvent{{1, 0x110, 0}, {0, 0, 0}}), source.data...)
			case "repeat release":
				source.data = append(pointerFixtureBytes(24, []pointerEvent{{1, 0x110, 2}, {1, 0x110, 0}, {0, 0, 0}}), source.data...)
			case "stale partial":
				source.stale = pointerFixtureBytes(24, []pointerEvent{{2, 0, 1}})
			case "stale lost":
				source.stale = pointerFixtureBytes(24, []pointerEvent{{0, 3, 0}, {0, 0, 0}})
			}
			result := runPointerWith(t.Context(), PointerOptions{Duration: time.Second}, pointerFixtureBackend(source))
			if result.Status == StatusPass || len(result.Devices) != 1 || result.Devices[0].XPositive != 0 {
				t.Fatal("stale or unmatched observations manufactured fresh criteria")
			}
			device := result.Devices[0]
			if source.closes != 1 || len(source.stale) != 0 || source.reads == 0 || device.Frames != 0 {
				t.Fatal("initial queue was not inspected and discarded without committed evidence")
			}
			if kind == "stale complete" {
				if !source.ready || source.reads < 3 || result.StopReason != "observation_timeout" || result.Status != StatusInconclusive {
					t.Fatal("stale-only fixture did not establish an empty queue and await fresh activity")
				}
				return
			}
			if result.StopReason != "error" {
				t.Fatal("an unrelated timeout or cancellation hid the initial-stream failure")
			}
			switch kind {
			case "initially held", "repeat release":
				minimumReads := 2
				if kind == "initially held" {
					minimumReads = 3
				}
				if !source.ready || source.reads < minimumReads || len(source.data) != 0 || device.Detail != "unmatched pointer button transition" {
					t.Fatal("unmatched fresh release was not reached after the initial queue")
				}
			case "stale partial":
				if !source.ready || source.reads < 2 || device.Detail != "pointer queue began with a partial event packet" {
					t.Fatal("initial partial packet was not rejected after the idle read")
				}
			case "stale lost":
				if !device.EvidenceLoss || device.Detail != "pointer event stream lost records before observation" {
					t.Fatal("initial lost-record event was not observed")
				}
			}
		})
	}
}

func TestPointerDiscoveryErrorsAndInvalidBoundsNeverOpenDevices(t *testing.T) {
	backend := pointerBackend{discover: func(context.Context, string) ([]string, error) { return []string{"event2", "event2"}, nil },
		open: func(context.Context, string, string) (pointerOpened, error) {
			t.Fatal("invalid discovery opened input")
			return pointerOpened{}, nil
		}}
	for _, duration := range []time.Duration{0, time.Millisecond, 6 * time.Minute, time.Second} {
		result := runPointerWith(t.Context(), PointerOptions{Duration: duration}, backend)
		if result.Status != StatusError {
			t.Fatal("invalid bounds or duplicate interfaces accepted")
		}
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		result := PointerResult{Status: Status("fail"), Devices: []PointerDeviceResult{{XPositive: 2}}}
		result.MarkInterrupted(cause)
		if result.Status != Status("fail") || result.Devices[0].XPositive != 2 || result.StopReason == "" {
			t.Fatal("interruption erased failure evidence")
		}
	}
}

func TestPointerPublicWorkerFailsClosedWithoutItsLinuxEnvironment(t *testing.T) {
	options := PointerOptions{Root: t.TempDir(), Duration: 10 * time.Millisecond}
	result := RunPointer(t.Context(), options)
	if result.Status != StatusError || result.Schema != PointerSchema || result.StopReason != "error" || len(result.Devices) != 0 {
		t.Fatal("missing environment produced functional evidence")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result = RunPointer(ctx, options)
	if result.Status != StatusAborted || result.StopReason != "operator_cancelled" || len(result.Devices) != 0 {
		t.Fatal("public worker ignored pre-cancellation")
	}
}
