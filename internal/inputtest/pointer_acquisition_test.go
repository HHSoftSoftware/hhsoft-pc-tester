package inputtest

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

type pointerMutableContext struct {
	context.Context
	stopped error
}

func (ctx *pointerMutableContext) Err() error { return ctx.stopped }

func TestPointerHardDispositionSurvivesRepeatedInterruption(t *testing.T) {
	for _, stopped := range []error{context.Canceled, context.DeadlineExceeded} {
		result := PointerResult{Schema: PointerSchema, Status: StatusError, StopReason: "error", Detail: "pointer descriptor cleanup failed", LimitMilliseconds: 1000,
			Devices: []PointerDeviceResult{{Interface: "event0", Readable: true, Frames: 4}}}
		before := result
		result.MarkInterrupted(stopped)
		result.MarkInterrupted(stopped)
		if !reflect.DeepEqual(result, before) {
			t.Fatal("hard cause or retained observations were rewritten")
		}
		result = PointerResult{Schema: PointerSchema, Status: StatusInconclusive, StopReason: "error"}
		result.MarkInterrupted(stopped)
		if stopped == context.Canceled && (result.Status != StatusAborted || result.StopReason != "operator_cancelled") || stopped == context.DeadlineExceeded && (result.Status != StatusError || result.StopReason != "deadline") {
			t.Fatal("initial non-hard state concealed interruption")
		}
	}
}

func TestPointerReturnedOwnerIsClosedEvenWithProviderError(t *testing.T) {
	for _, mode := range []string{"handle error", "handle unavailable", "missing read", "missing validate", "missing close", "invalid layout", "close and cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			opens, closes, reads := 0, 0, 0
			backend := pointerBackend{discover: func(context.Context, string) ([]string, error) { return []string{"event0", "event1"}, nil }, open: func(context.Context, string, string) (pointerOpened, error) {
				opens++
				source := pointerOpened{identity: PointerIdentity{3, 1, 2, 1}, capabilities: pointerTestCapabilities("left"), eventSize: 24, typeOffset: 16,
					read: func([]byte) (int, error) { reads++; return 0, errPointerIdle }, validate: func() error { return nil }, close: func() error {
						closes++
						if mode == "close and cancel" {
							cancel()
							return errors.New("private cleanup cause")
						}
						return nil
					}}
				switch mode {
				case "handle error", "close and cancel":
					return source, errors.New("private acquisition cause")
				case "handle unavailable":
					return source, errInputUnavailable
				case "missing read":
					source.read = nil
				case "missing validate":
					source.validate = nil
				case "missing close":
					source.close = nil
				case "invalid layout":
					source.eventSize = 0
				}
				return source, nil
			}}
			result := runPointerWith(ctx, PointerOptions{Duration: time.Second}, backend)
			wantClosed := 1
			if mode == "missing close" {
				wantClosed = 0
			}
			if result.Status != StatusError || result.StopReason != "error" || opens != 1 || closes != wantClosed || reads != 0 || strings.Contains(result.Detail, "private") {
				t.Fatalf("ownership/provider failure escaped: %#v opens=%d closes=%d reads=%d", result, opens, closes, reads)
			}
		})
	}
}

func TestPointerHardReadAndProtocolFaultPrecedeCancellation(t *testing.T) {
	for _, mode := range []string{"negative", "oversized", "zero progress", "read error", "joined idle", "joined retry", "masked idle", "partial idle", "partial error", "EOF"} {
		for _, stopped := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(mode+"/"+stopped.Error(), func(t *testing.T) {
				ctx := &pointerMutableContext{Context: t.Context()}
				reads, closes := 0, 0
				backend := pointerBackend{discover: func(context.Context, string) ([]string, error) { return []string{"event0"}, nil }, open: func(context.Context, string, string) (pointerOpened, error) {
					return pointerOpened{identity: PointerIdentity{3, 1, 2, 1}, capabilities: pointerTestCapabilities("left"), eventSize: 24, typeOffset: 16,
						validate: func() error { return nil }, close: func() error { closes++; return nil }, read: func(p []byte) (int, error) {
							reads++
							ctx.stopped = stopped
							switch mode {
							case "negative":
								return -1, errors.New("private read")
							case "oversized":
								return len(p) + 1, nil
							case "zero progress":
								return 0, nil
							case "joined idle":
								return 0, errors.Join(errPointerIdle, errors.New("private read"))
							case "joined retry":
								return 0, errors.Join(errPointerRetry, errors.New("private read"))
							case "masked idle":
								return 0, pointerMaskedIdle{}
							case "partial idle":
								return 1, errPointerIdle
							case "partial error":
								return 1, errors.New("private read")
							case "EOF":
								return 0, io.EOF
							default:
								return 0, errors.New("private read")
							}
						}}, nil
				}}
				result := runPointerWith(ctx, PointerOptions{Duration: time.Second}, backend)
				if result.Status != StatusError || result.StopReason != "error" || reads != 1 || closes != 1 || len(result.Devices) != 1 || result.Devices[0].Frames != 0 || strings.Contains(result.Detail, "private") {
					t.Fatalf("read fault hidden by interruption: %#v", result)
				}
				before := result.Detail
				result.MarkInterrupted(stopped)
				if result.Detail != before || result.Status != StatusError || result.StopReason != "error" {
					t.Fatal("second interruption erased hard evidence")
				}
			})
		}
	}
}

type pointerMaskedIdle struct{}

func (pointerMaskedIdle) Error() string        { return "private masked failure" }
func (pointerMaskedIdle) Is(target error) bool { return target == errPointerIdle }

func TestPointerCloseFaultAfterCompleteObservationPrecedesInterrupt(t *testing.T) {
	for _, stopped := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(stopped.Error(), func(t *testing.T) {
			ctx := &pointerMutableContext{Context: t.Context()}
			source := completePointerSource(24)
			backend := pointerFixtureBackend(source)
			open := backend.open
			// The hook is installed on the actual returned owner, after its successful
			// read/validation path; cancellation occurs only at that owner's Close.
			backend.open = func(inner context.Context, root, name string) (pointerOpened, error) {
				value, err := open(inner, root, name)
				close := value.close
				value.close = func() error { _ = close(); ctx.stopped = stopped; return errors.New("private cleanup") }
				return value, err
			}
			result := runPointerWith(ctx, PointerOptions{Duration: time.Second}, backend)
			if result.Status != StatusError || result.StopReason != "error" || result.Detail != "pointer descriptor cleanup failed" || source.closes != 1 || result.Devices[0].Status != StatusPass || !result.Devices[0].Readable || result.Devices[0].Frames != 6 {
				t.Fatalf("complete observations or close cause lost: %#v", result)
			}
			result.MarkInterrupted(stopped)
			if result.Detail != "pointer descriptor cleanup failed" {
				t.Fatal("CLI interruption would erase cleanup")
			}
		})
	}
}

func TestPointerLaterReadFaultRetainsExactEarlierDevice(t *testing.T) {
	first, second, third := completePointerSource(24), completePointerSource(24), completePointerSource(24)
	second.terminal = errors.New("private second-device error")
	result := runPointerWith(t.Context(), PointerOptions{Duration: time.Second}, pointerFixtureBackend(first, second, third))
	identity := PointerIdentity{3, 1, 1, 1}
	capabilities := pointerTestCapabilities("left", "right")
	expected := PointerDeviceResult{Interface: "event2", Status: StatusInconclusive, Supported: true, Readable: true, Identity: &identity, Capabilities: &capabilities,
		XPositive: 1, XNegative: 1, YPositive: 1, YNegative: 1, Frames: 6, Detail: "pointer criteria not yet complete", Wheels: []PointerWheelResult{}, Buttons: []PointerButtonResult{{Button: "left", Presses: 1, Releases: 1, Pairs: 1}, {Button: "right", Presses: 1, Releases: 1, Pairs: 1}}}
	if result.Status != StatusError || result.StopReason != "error" || len(result.Devices) != 3 || !reflect.DeepEqual(result.Devices[0], expected) || third.reads != 1 || len(third.data) == 0 || first.closes != 1 || second.closes != 1 || third.closes != 1 {
		t.Fatalf("later fault hid exact prior row or read later data: %#v", result)
	}
}

func TestPointerPrecancelAndOrdinaryUnavailableRemainDistinct(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	backend := pointerBackend{discover: func(context.Context, string) ([]string, error) { calls++; return nil, nil }, open: func(context.Context, string, string) (pointerOpened, error) {
		calls++
		return pointerOpened{}, errInputUnavailable
	}}
	result := runPointerWith(ctx, PointerOptions{Duration: time.Second}, backend)
	if result.Status != StatusAborted || calls != 0 {
		t.Fatal("pre-cancelled runner touched providers or became a hard placeholder error")
	}
	backend.discover = func(context.Context, string) ([]string, error) { return []string{"event0", "event1"}, nil }
	result = runPointerWith(t.Context(), PointerOptions{Duration: time.Second}, backend)
	if result.Status != StatusInconclusive || calls != 2 || result.Devices[0].Readable {
		t.Fatal("ordinary absent interfaces lost non-pass diagnostics")
	}
}

func TestPointerPositiveReadErrorRetainsOnlyCompleteDiagnosticFrames(t *testing.T) {
	for _, mode := range []string{"complete error bytes", "partial error record", "partial error frame", "error after honest prefix", "cancelled error bytes"} {
		t.Run(mode, func(t *testing.T) {
			source := completePointerSource(24)
			backend := pointerFixtureBackend(source)
			open := backend.open
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reads := 0
			backend.open = func(inner context.Context, root, name string) (pointerOpened, error) {
				value, err := open(inner, root, name)
				value.read = func(p []byte) (int, error) {
					reads++
					if reads == 1 {
						return 0, errPointerIdle
					}
					if mode == "error after honest prefix" && reads == 2 {
						return copy(p, pointerFixtureBytes(24, []pointerEvent{{2, 0, 7}, {0, 0, 0}})), nil
					}
					data := pointerFixtureBytes(24, pointerCompleteEvents(0x110, 0x111))
					switch mode {
					case "partial error record":
						data = data[:1]
					case "partial error frame":
						data = pointerFixtureBytes(24, []pointerEvent{{2, 0, 7}})
					case "error after honest prefix":
						data = []byte{1}
					case "cancelled error bytes":
						cancel()
					}
					return copy(p, data), errors.New("private read failure")
				}
				return value, err
			}
			result := runPointerWith(ctx, PointerOptions{Duration: time.Second}, backend)
			wantFrames, wantPositive := uint64(0), uint64(0)
			if mode == "complete error bytes" {
				wantFrames, wantPositive = 6, 1
			}
			if mode == "error after honest prefix" {
				wantFrames, wantPositive = 1, 1
			}
			row := result.Devices[0]
			if result.Status != StatusError || result.StopReason != "error" || row.Status == StatusPass || row.Frames != wantFrames || row.XPositive != wantPositive || !row.Readable || source.closes != 1 {
				t.Fatalf("failed-read bytes gained pass/partial counters or hid prior diagnostics: %#v", result)
			}
		})
	}
}

func TestPointerVerifiedEarlierPassSurvivesLaterDeviceFault(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "hard read", true: "hard read and cancel"}[cancelled], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			first, second, third := completePointerSource(24), completePointerSource(24), completePointerSource(24)
			backend := pointerFixtureBackend(first, second, third)
			open := backend.open
			secondReads := 0
			backend.open = func(inner context.Context, root, name string) (pointerOpened, error) {
				source, err := open(inner, root, name)
				if name == "event7" {
					source.read = func([]byte) (int, error) {
						secondReads++
						if secondReads < 3 {
							return 0, errPointerIdle
						}
						if cancelled {
							cancel()
						}
						return 0, errors.New("private later fault")
					}
				}
				return source, err
			}
			result := runPointerWith(ctx, PointerOptions{Duration: time.Second}, backend)
			identity := PointerIdentity{3, 1, 1, 1}
			capabilities := pointerTestCapabilities("left", "right")
			expected := PointerDeviceResult{Interface: "event2", Status: StatusPass, Supported: true, Readable: true, Identity: &identity, Capabilities: &capabilities,
				XPositive: 1, XNegative: 1, YPositive: 1, YNegative: 1, Frames: 6, Wheels: []PointerWheelResult{}, Buttons: []PointerButtonResult{{Button: "left", Presses: 1, Releases: 1, Pairs: 1}, {Button: "right", Presses: 1, Releases: 1, Pairs: 1}}}
			if result.Status != StatusError || result.StopReason != "error" || !reflect.DeepEqual(result.Devices[0], expected) || first.checks != 4 || third.reads != 2 || first.closes != 1 || second.closes != 1 || third.closes != 1 {
				t.Fatalf("earlier verified row lost or later device continued: %#v", result)
			}
		})
	}
}
