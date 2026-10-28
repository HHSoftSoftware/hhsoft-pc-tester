package inputtest

import (
	"context"
	"encoding/binary"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maximumPointerDevices = 128

var errPointerIdle = errors.New("no pointer events available")
var errPointerRetry = errors.New("pointer event read interrupted")

type pointerOpened struct {
	identity              PointerIdentity
	capabilities          PointerCapabilities
	eventSize, typeOffset int
	read                  func([]byte) (int, error)
	validate              func() error
	close                 func() error
}

type pointerBackend struct {
	discover func(context.Context, string) ([]string, error)
	open     func(context.Context, string, string) (pointerOpened, error)
}

func pointerInterface(name string) bool {
	value, err := strconv.ParseUint(strings.TrimPrefix(name, "event"), 10, 32)
	return err == nil && name == "event"+strconv.FormatUint(value, 10)
}

func pointerContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

func runPointerWith(ctx context.Context, options PointerOptions, backend pointerBackend) (result PointerResult) {
	result = PointerResult{Schema: PointerSchema, Status: StatusError, StopReason: "error", LimitMilliseconds: options.Duration.Milliseconds()}
	if nilInputValue(ctx) || backend.discover == nil || backend.open == nil || options.Duration < 10*time.Millisecond || options.Duration > 90*time.Second {
		result.LimitMilliseconds = 0
		result.Detail = "pointer duration must be between 10ms and 90s with a valid context"
		return
	}
	result.Status = StatusInconclusive // Initial state is not an observed hard failure.
	started := time.Now()
	deadline := started.Add(options.Duration)
	var opened []pointerOpened
	defer func() {
		for index := len(opened) - 1; index >= 0; index-- {
			source := opened[index]
			if err := source.close(); err != nil {
				result.Status, result.StopReason, result.Detail = StatusError, "error", "pointer descriptor cleanup failed"
			}
		}
		result.DurationMilliseconds = time.Since(started).Milliseconds()
		result.MarkInterrupted(pointerContextError(ctx))
		if result.Status == StatusPass && !time.Now().Before(deadline) {
			result.Status, result.StopReason, result.Detail = StatusInconclusive, "observation_timeout", "pointer observation window ended before confirmed completion"
		}
	}()
	stop := func() bool {
		if err := pointerContextError(ctx); err != nil {
			result.MarkInterrupted(err)
			return true
		}
		if !time.Now().Before(deadline) {
			if result.Status != StatusError {
				result.Status, result.StopReason, result.Detail = StatusInconclusive, "observation_timeout", "pointer criteria incomplete within the observation window"
			}
			return true
		}
		return false
	}
	if stop() {
		return
	}
	if options.Root == "" {
		options.Root = "/"
	}
	interfaces, err := backend.discover(ctx, options.Root)
	if err != nil {
		if err == context.Canceled || err == context.DeadlineExceeded {
			result.MarkInterrupted(err)
		} else {
			result.Status, result.Detail = StatusError, "pointer registry unavailable or invalid"
		}
		return
	}
	sort.Strings(interfaces)
	if len(interfaces) > maximumPointerDevices {
		result.Status, result.Detail = StatusError, "pointer registry exceeds the supported device limit"
		return
	}
	for index, name := range interfaces {
		if !pointerInterface(name) || index > 0 && interfaces[index-1] == name {
			result.Status, result.Detail = StatusError, "pointer registry contains an invalid or repeated interface"
			return
		}
	}
	result.Devices = make([]PointerDeviceResult, len(interfaces))
	for index, name := range interfaces {
		result.Devices[index] = PointerDeviceResult{Interface: name, Status: StatusInconclusive, Wheels: []PointerWheelResult{}, Detail: "pointer interface was not observed"}
	}
	if len(interfaces) == 0 {
		result.Status, result.Detail = StatusInconclusive, "no pointer event interfaces were discovered"
		return
	}
	type observedPointer struct {
		source        pointerOpened
		state         *pointerState
		pending       []byte
		baseline      bool
		baselineBytes int
		baselineFrame bool
	}
	var devices []observedPointer
	for index, name := range interfaces {
		if stop() {
			return
		}
		device := &result.Devices[index]
		source, err := backend.open(ctx, options.Root, name)
		// Even an error return can transfer an owner. Register it before
		// interpreting the error or checking interruption.
		if source.close != nil {
			opened = append(opened, source)
		}
		if err != nil {
			device.Detail = "pointer descriptor or identity could not be verified"
			if err == errInputUnavailable && source.close == nil && source.read == nil && source.validate == nil {
				if stop() {
					return
				}
				continue
			}
			if (err == context.Canceled || err == context.DeadlineExceeded) && source.close == nil && source.read == nil && source.validate == nil {
				result.MarkInterrupted(err)
			} else {
				result.Status, result.Detail = StatusError, "pointer descriptor acquisition failed"
			}
			return
		}
		if source.close == nil || source.read == nil || source.validate == nil {
			result.Status, result.Detail = StatusError, "pointer descriptor provider is incomplete"
			return
		}
		if stop() {
			return
		}
		device.Readable, device.Identity, device.Capabilities = true, &source.identity, &source.capabilities
		device.Supported = supportedPointer(source.capabilities)
		if !device.Supported {
			device.Detail = "requires relative X/Y, a left button, and no absolute axes"
			continue
		}
		device.Wheels = pointerWheelObservations(source.capabilities)
		if source.eventSize < 8 || source.eventSize > 64 || source.typeOffset != source.eventSize-8 {
			device.Detail = "unsupported pointer event record layout"
			result.Status, result.Detail = StatusError, device.Detail
			return
		}
		device.Detail = "pointer criteria not yet complete"
		devices = append(devices, observedPointer{source: source, state: newPointerState(device)})
	}
	buffer := make([]byte, 4096)
	for {
		if stop() {
			for index := range devices {
				device := &devices[index]
				if len(device.pending) != 0 || device.state.frameEvents != 0 {
					device.state.reject("pointer observation ended with a partial event packet", false)
				}
			}
			return
		}
		unfinished := false
		for index := range devices {
			device := &devices[index]
			if device.state.failed {
				continue
			}
			if stop() {
				return
			}
			if err := device.source.validate(); err != nil {
				if err == context.Canceled || err == context.DeadlineExceeded {
					result.MarkInterrupted(err)
					return
				}
				device.state.reject("pointer interface or capabilities changed", false)
				result.Status, result.Detail = StatusError, device.state.result.Detail
				return
			}
			count, readErr := device.source.read(buffer)
			if count < 0 || count > len(buffer) {
				device.state.reject("invalid pointer event read", false)
				result.Status, result.Detail = StatusError, device.state.result.Detail
				return
			}
			// Only the exact zero-byte idle/retry sentinels are benign.
			hardRead := readErr != nil && !(count == 0 && (readErr == errPointerIdle || readErr == errPointerRetry)) || count == 0 && readErr == nil
			if hardRead {
				result.Status, result.Detail = StatusError, "pointer event stream disconnected or became unreadable"
			}
			if stop() {
				if hardRead {
					device.state.reject(result.Detail, false)
				}
				return
			}
			if count > 0 && device.state.result.Status == StatusPass {
				device.state.result.Status, device.state.result.Detail = StatusInconclusive, "pointer criteria not yet complete"
			}
			device.pending = append(device.pending, buffer[:count]...)
			if !device.baseline {
				device.baselineBytes += count
				if device.baselineBytes > 64<<10 {
					device.state.reject("pointer queue did not become idle before observation", false)
				}
			}
			for len(device.pending) >= device.source.eventSize && !device.state.failed {
				if stop() {
					return
				}
				record := device.pending[device.source.typeOffset:device.source.eventSize]
				event := pointerEvent{binary.NativeEndian.Uint16(record), binary.NativeEndian.Uint16(record[2:]), int32(binary.NativeEndian.Uint32(record[4:]))}
				if device.baseline {
					device.state.consume(event)
				} else if event.typeCode == 0 && event.code == 3 {
					device.state.reject("pointer event stream lost records before observation", true)
				} else {
					device.baselineFrame = event.typeCode != 0 || event.code != 0
				}
				device.pending = device.pending[device.source.eventSize:]
			}
			if hardRead {
				device.state.reject("pointer event stream disconnected or became unreadable", false)
			}
			idle := count == 0 && readErr == errPointerIdle
			if !device.baseline && idle {
				if len(device.pending) != 0 || device.baselineFrame {
					device.state.reject("pointer queue began with a partial event packet", false)
				} else {
					device.baseline = true
				}
			}
			if device.state.failed {
				result.Status, result.Detail = StatusError, device.state.result.Detail
				return
			}
			if device.baseline && device.state.complete() && len(device.pending) == 0 && idle {
				if err := device.source.validate(); err != nil {
					if err == context.Canceled || err == context.DeadlineExceeded {
						result.MarkInterrupted(err)
						return
					}
					device.state.reject("pointer identity changed before completion", false)
					result.Status, result.Detail = StatusError, device.state.result.Detail
					return
				}
				device.state.result.Status, device.state.result.Detail = StatusPass, ""
			} else {
				unfinished = true
			}
		}
		if !unfinished {
			result.Status, result.StopReason, result.Detail = StatusInconclusive, "error", "one or more pointer interfaces lack complete verified criteria"
			passed := 0
			for index := range devices {
				if devices[index].state.result.Status == StatusPass {
					passed++
				}
			}
			if passed == len(result.Devices) && !stop() {
				result.Status, result.StopReason, result.Detail = StatusPass, "completed", ""
			}
			return
		}
		timer := time.NewTimer(min(5*time.Millisecond, time.Until(deadline)))
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
}
