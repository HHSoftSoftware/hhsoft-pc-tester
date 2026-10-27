package inputtest

import (
	"context"
	"encoding/binary"
	"errors"
	"slices"
	"sort"
	"time"
)

const maximumKeyboardDevices = 128

var errKeyboardIdle = errors.New("keyboard queue idle")
var errKeyboardRetry = errors.New("keyboard read interrupted")
var errKeyboardCleanup = errors.New("keyboard temporary descriptor cleanup unconfirmed")

type keyboardOpened struct {
	identity              KeyboardIdentity
	capabilities          KeyboardCapabilities
	eventSize, typeOffset int
	read                  func([]byte) (int, error)
	validate              func() error
	neutral               func() (bool, error)
	close                 func() error
}
type keyboardBackend struct {
	discover func(context.Context, string) ([]string, error)
	open     func(context.Context, string, string) (keyboardOpened, error)
}
type observedKeyboard struct {
	source        keyboardOpened
	state         *keyboardState
	resultIndex   int
	pending       []byte
	baselineBytes int
	baselineFrame bool
}

func runKeyboardWith(ctx context.Context, options KeyboardOptions, backend keyboardBackend) (result KeyboardResult) {
	result = newKeyboardResult(options)
	if ctx == nil || options.Duration < 10*time.Millisecond || options.Duration > 90*time.Second || backend.discover == nil || backend.open == nil {
		result.LimitMilliseconds, result.Detail = 0, "invalid_options"
		return
	}
	started := time.Now()
	deadline := started.Add(options.Duration)
	var opened []struct {
		source keyboardOpened
		index  int
	}
	var devices []observedKeyboard
	defer func() {
		for index := range devices {
			device := &devices[index]
			if len(device.pending) != 0 || device.state.frameEvents != 0 {
				device.state.reject("malformed_events", false)
			}
		}
		for _, owned := range opened {
			device := &result.Devices[owned.index]
			if device.Status == StatusPass {
				for index := range devices {
					observed := &devices[index]
					if observed.resultIndex != owned.index {
						continue
					}
					if err := recheckKeyboardPass(observed); errors.Is(err, errKeyboardCleanup) {
						result.CleanupConfirmed = false
					}
					break
				}
				if device.Status != StatusPass && result.Status == StatusPass {
					result.Status, result.StopReason, result.Detail = StatusInconclusive, "error", "incomplete_actions"
				}
			}
			if err := owned.source.close(); err != nil {
				device.Status, device.Detail = StatusError, "cleanup_unconfirmed"
				result.CleanupConfirmed = false
			} else {
				device.CleanupConfirmed = true
			}
		}
		result.DurationMilliseconds = time.Since(started).Milliseconds()
		if !result.CleanupConfirmed {
			result.Status, result.StopReason, result.Detail = StatusError, "cleanup_unconfirmed", "cleanup_unconfirmed"
			return
		}
		result.MarkInterrupted(pointerContextError(ctx))
		if result.Status == StatusPass && !time.Now().Before(deadline) {
			result.Status, result.StopReason, result.Detail = StatusInconclusive, "observation_timeout", "observation_timeout"
		}
	}()
	stop := func() bool {
		if !result.CleanupConfirmed {
			result.Status, result.StopReason, result.Detail = StatusError, "cleanup_unconfirmed", "cleanup_unconfirmed"
			return true
		}
		if err := pointerContextError(ctx); err != nil {
			result.MarkInterrupted(err)
			return true
		}
		if !time.Now().Before(deadline) {
			result.Status, result.StopReason, result.Detail = StatusInconclusive, "observation_timeout", "observation_timeout"
			return true
		}
		return false
	}
	observeError := func(err error) {
		if errors.Is(err, errKeyboardCleanup) {
			result.CleanupConfirmed = false
		}
	}
	if stop() {
		return
	}
	if options.Root == "" {
		options.Root = "/"
	}
	interfaces, err := backend.discover(ctx, options.Root)
	if err != nil {
		observeError(err)
		result.Detail = "registry_unavailable"
		return
	}
	sort.Strings(interfaces)
	if len(interfaces) > maximumKeyboardDevices {
		result.Detail = "device_limit"
		return
	}
	for index, name := range interfaces {
		if !pointerInterface(name) || index > 0 && name == interfaces[index-1] {
			result.Detail = "registry_unavailable"
			return
		}
	}
	if len(interfaces) == 0 {
		result.Status, result.Detail = StatusInconclusive, "no_keyboards"
		return
	}
	for _, name := range interfaces {
		result.Devices = append(result.Devices, KeyboardDeviceResult{Interface: name, Status: StatusInconclusive,
			Actions: []KeyboardAction{}, CleanupConfirmed: true, Detail: "incomplete_actions"})
	}
	for index, name := range interfaces {
		if stop() {
			return
		}
		device := &result.Devices[index]
		source, err := backend.open(ctx, options.Root, name)
		if source.close != nil {
			opened = append(opened, struct {
				source keyboardOpened
				index  int
			}{source, index})
			device.CleanupConfirmed = false
		}
		if err != nil {
			observeError(err)
			device.Detail = "identity_unavailable"
			continue
		}
		if source.close == nil || source.read == nil || source.validate == nil || source.neutral == nil {
			device.Detail = "identity_unavailable"
			continue
		}
		device.Readable, device.Identity = true, &source.identity
		capabilities := source.capabilities
		capabilities.FixedKeys = append([]string{}, capabilities.FixedKeys...)
		device.Capabilities = &capabilities
		device.Supported = supportedKeyboard(capabilities)
		if !device.Supported {
			device.Detail = "unsupported_capabilities"
			continue
		}
		state := newKeyboardState(device)
		if source.eventSize < 8 || source.eventSize > 64 || source.typeOffset != source.eventSize-8 {
			state.reject("unsupported_record_layout", false)
			continue
		}
		devices = append(devices, observedKeyboard{source: source, state: state, resultIndex: index})
	}
	buffer := make([]byte, 4096)
	for {
		if stop() {
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
				observeError(err)
				device.state.reject("identity_changed", false)
				continue
			}
			count, readErr := device.source.read(buffer)
			if stop() {
				return
			}
			if count < 0 || count > len(buffer) {
				device.state.reject("malformed_events", false)
				continue
			}
			device.pending = append(device.pending, buffer[:count]...)
			if !device.state.result.BaselineNeutral {
				device.baselineBytes += count
				if device.baselineBytes > 64<<10 {
					device.state.reject("baseline_unavailable", false)
				}
			}
			for len(device.pending) >= device.source.eventSize && !device.state.failed {
				if stop() {
					return
				}
				record := device.pending[device.source.typeOffset:device.source.eventSize]
				event := keyboardEvent{binary.NativeEndian.Uint16(record), binary.NativeEndian.Uint16(record[2:]), int32(binary.NativeEndian.Uint32(record[4:]))}
				if device.state.result.BaselineNeutral {
					device.state.consume(event)
				} else {
					if event.typeCode == 0 {
						if event.code == 3 {
							device.state.reject("event_loss", true)
						} else if event.code != 0 || event.value != 0 {
							device.state.reject("malformed_events", false)
						}
					}
					device.baselineFrame = event.typeCode != 0 || event.code != 0 || event.value != 0
				}
				device.pending = device.pending[device.source.eventSize:]
			}
			if readErr != nil && !errors.Is(readErr, errKeyboardIdle) && !errors.Is(readErr, errKeyboardRetry) || count == 0 && readErr == nil {
				device.state.reject("disconnected", false)
			}
			idle := count == 0 && errors.Is(readErr, errKeyboardIdle)
			if !device.state.failed && idle && !device.state.result.BaselineNeutral {
				if len(device.pending) != 0 || device.baselineFrame {
					device.state.reject("malformed_events", false)
				} else {
					neutral, err := device.source.neutral()
					if err != nil {
						observeError(err)
						device.state.reject("baseline_unavailable", false)
					} else if neutral {
						device.state.result.BaselineNeutral = true
					}
				}
			}
			if !device.state.failed {
				device.state.result.Status, device.state.result.Detail, device.state.result.FinalNeutral = StatusInconclusive, "incomplete_actions", false
				if idle && len(device.pending) == 0 && device.state.complete() {
					neutral, err := device.source.neutral()
					if err != nil {
						observeError(err)
						device.state.reject("identity_changed", false)
					} else if neutral {
						if err := device.source.validate(); err != nil {
							observeError(err)
							device.state.reject("identity_changed", false)
						} else if !stop() {
							device.state.result.Status, device.state.result.Detail, device.state.result.FinalNeutral = StatusPass, "", true
						}
					}
				}
				if !device.state.failed && device.state.result.Status != StatusPass {
					unfinished = true
				}
			}
		}
		if stop() {
			return
		}
		if !unfinished {
			current, err := backend.discover(ctx, options.Root)
			observeError(err)
			sort.Strings(current)
			if err != nil || !slices.Equal(current, interfaces) {
				for index := range devices {
					devices[index].state.reject("registry_changed", false)
				}
				result.Detail = "registry_changed"
				return
			}
			result.Status, result.StopReason, result.Detail = StatusInconclusive, "error", "incomplete_actions"
			passed := 0
			for index := range devices {
				device := &devices[index]
				if device.state.result.Status == StatusPass {
					if err := device.source.validate(); err != nil {
						observeError(err)
						device.state.reject("identity_changed", false)
					} else {
						passed++
					}
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

// An earlier completion is only a candidate until its descriptor is closed.
// This last bounded observation also runs on cancellation. It never interprets
// or credits queued post-stop events, and starts no additional time window.
func recheckKeyboardPass(device *observedKeyboard) error {
	if err := device.source.validate(); err != nil {
		device.state.reject("identity_changed", false)
		return err
	}
	var buffer [4096]byte
	count, err := device.source.read(buffer[:])
	if count != 0 || !errors.Is(err, errKeyboardIdle) || len(device.pending) != 0 || !device.state.complete() {
		device.state.reject("incomplete_actions", false)
		return err
	}
	neutral, err := device.source.neutral()
	if err != nil || !neutral {
		device.state.reject("incomplete_actions", false)
		return err
	}
	if err := device.source.validate(); err != nil {
		device.state.reject("identity_changed", false)
		return err
	}
	return nil
}
