//go:build linux

package inputtest

import (
	"context"
	"encoding/binary"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type inputOpenEvent struct {
	observation *inputObservation
	name        string
	index       int
}

func runInputWithOperations(ctx context.Context, options Options, operations inputReadinessOperations) (result Result) {
	result = Result{Schema: Schema, Status: StatusError, Mode: "readiness"}
	if nilInputValue(ctx) || operations.openRoot == nil || operations.openReadable == nil || operations.now == nil || operations.wait == nil {
		result.Detail = "input readiness operations are unavailable"
		return result
	}
	if err := ctx.Err(); err != nil {
		recordInputFailure(&result, err)
		return result
	}
	if options.Duration < 0 || options.Duration > 5*time.Minute {
		result.Detail = "duration must be between 0 and 5m"
		return result
	}
	if options.Duration > 0 {
		result.Mode = "activity"
	}
	if options.Root == "" {
		options.Root = "/"
	}
	if !filepath.IsAbs(options.Root) || len(options.Root) > 4096 || strings.ContainsRune(options.Root, 0) {
		result.Detail = "input readiness root must be an absolute filesystem path"
		return result
	}
	categories, err := requestedCategories(options.Categories)
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	result.RequestedCategories = categories
	root, openErr := operations.openRoot(options.Root)
	if nilInputValue(root) {
		result.Detail = "cannot access input readiness root"
		return result
	}
	defer func() {
		if root.Close() != nil {
			recordInputFailure(&result, errInputClose)
		} else {
			cancelInputResult(ctx, &result)
		}
	}()
	if openErr != nil {
		result.Detail = "cannot access input readiness root"
		return result
	}
	registry, err := acquireInputMetadata(ctx, root, operations, "proc/bus/input/devices", maximumInputRegistryBytes, 4096)
	if err != nil {
		recordInputFailure(&result, err)
		return result
	}
	devices, err := parseInputRegistry(ctx, registry, categories)
	if err != nil {
		recordInputFailure(&result, err)
		return result
	}
	result.Status = StatusInconclusive
	if cancelInputResult(ctx, &result) {
		return result
	}
	if len(devices) == 0 {
		result.Detail = "no requested input event devices were discovered"
		return result
	}
	if len(options.Categories) == 0 {
		categories = deviceCategories(devices)
		result.RequestedCategories = categories
	}
	// A separate sysfs root permits ordinary class links within sysfs while
	// preventing a selected dev attribute from escaping that filesystem tree.
	sys, sysErr := root.OpenRoot("sys")
	if nilInputValue(sys) {
		recordInputFailure(&result, errInputAcquire)
		return result
	}
	defer func() {
		if sys.Close() != nil {
			recordInputFailure(&result, errInputClose)
		} else {
			cancelInputResult(ctx, &result)
		}
	}()
	if sysErr != nil {
		recordInputFailure(&result, errInputAcquire)
		return result
	}
	seenNumbers := make(map[inputDeviceNumber]bool)
	var pending []inputOpenEvent
	// Early failure cleanup opens no further metadata and grants no credit.
	defer func() {
		for _, event := range pending {
			if err := closeInputObservation(event.observation); err != nil {
				recordInputFailure(&result, err)
			}
		}
		cancelInputResult(ctx, &result)
	}()
	for _, device := range devices {
		if cancelInputResult(ctx, &result) {
			return result
		}
		result.Devices = append(result.Devices, DeviceResult{Interface: device.interfaceName, Category: device.category, Name: device.name})
		index := len(result.Devices) - 1
		number, err := inputRegistryNumber(ctx, sys, operations, device.interfaceName)
		if err != nil {
			recordInputFailure(&result, err)
			return result
		}
		if seenNumbers[number] {
			recordInputFailure(&result, errInputIdentity)
			return result
		}
		seenNumbers[number] = true
		observation, err := openInputObservation(ctx, root, operations, "dev/input/"+device.interfaceName, &number)
		if err == errInputUnavailable {
			if cancelInputResult(ctx, &result) {
				return result
			}
			continue
		}
		if err != nil {
			recordInputFailure(&result, err)
			return result
		}
		if options.Duration == 0 {
			if err := finishInputEvent(ctx, root, sys, operations, observation, device.interfaceName); err != nil {
				recordInputFailure(&result, err)
				return result
			}
			result.Devices[index].Readable = true
			result.ObservedCategories = readableCategories(result.Devices)
		} else {
			pending = append(pending, inputOpenEvent{observation, device.interfaceName, index})
		}
	}
	if cancelInputResult(ctx, &result) {
		return result
	}
	if options.Duration == 0 {
		result.ObservedCategories = readableCategories(result.Devices)
		if len(result.ObservedCategories) == 0 || !allInputReadable(result.Devices) || !hasEveryCategory(result.ObservedCategories, categories) {
			result.Detail = "one or more requested input categories are absent or unreadable"
			return result
		}
		result.Status, result.Detail = StatusPass, ""
		return result
	}
	if len(pending) == len(result.Devices) && hasEveryCategory(deviceCategories(devices), categories) {
		observeInputActivity(ctx, options.Duration, operations, pending, categories, &result)
	} else {
		result.Detail = "one or more requested input categories are absent or unreadable"
	}
	validate := result.Status != StatusError && result.Status != StatusAborted && ctx.Err() == nil
	for _, event := range pending {
		if validate {
			if err := finishInputEvent(ctx, root, sys, operations, event.observation, event.name); err != nil {
				recordInputFailure(&result, err)
				validate = false
			} else {
				result.Devices[event.index].Readable = true
			}
		} else if err := closeInputObservation(event.observation); err != nil {
			recordInputFailure(&result, err)
		}
	}
	pending = nil
	result.ObservedCategories = categoryList(result.Devices, func(device DeviceResult) bool {
		return device.Readable && !device.EvidenceLoss && qualifyingActivity(device)
	})
	cancelInputResult(ctx, &result)
	return result
}

func allInputReadable(devices []DeviceResult) bool {
	for _, device := range devices {
		if !device.Readable {
			return false
		}
	}
	return true
}

func observeInputActivity(ctx context.Context, duration time.Duration, operations inputReadinessOperations, opened []inputOpenEvent, categories []string, result *Result) {
	started := operations.now()
	deadline := started.Add(duration)
	defer func() {
		elapsed := operations.now().Sub(started)
		if elapsed < 0 {
			recordInputFailure(result, errInputStream)
		} else {
			result.DurationMilliseconds = elapsed.Milliseconds()
		}
	}()
	eventSize := int(unsafe.Sizeof(inputEvent{}))
	typeOffset := int(unsafe.Offsetof(inputEvent{}.Type))
	var readBuffer [4096]byte
	buffers := make([][]byte, len(opened))
	for operations.now().Before(deadline) {
		if cancelInputResult(ctx, result) {
			return
		}
		for index, event := range opened {
			if cancelInputResult(ctx, result) {
				return
			}
			n, err := event.observation.reader.Read(readBuffer[:])
			if n < 0 || n > len(readBuffer) || n == 0 && err == nil || n > 0 && err != nil {
				recordInputFailure(result, errInputStream)
				return
			}
			if err != nil && err != syscall.EAGAIN && err != syscall.EINTR {
				recordInputFailure(result, errInputStream)
				return
			}
			if cancelInputResult(ctx, result) {
				return
			}
			if n == 0 {
				continue
			}
			buffers[index] = append(buffers[index], readBuffer[:n]...)
			complete := len(buffers[index]) / eventSize * eventSize
			for offset := 0; offset < complete; offset += eventSize {
				record := buffers[index][offset : offset+eventSize]
				value := inputEvent{Type: binary.NativeEndian.Uint16(record[typeOffset:]), Code: binary.NativeEndian.Uint16(record[typeOffset+2:]), Value: int32(binary.NativeEndian.Uint32(record[typeOffset+4:]))}
				if accountEvent(&result.Devices[event.index], value) {
					result.Status, result.Detail = StatusInconclusive, "input event stream lost records"
					return
				}
			}
			buffers[index] = append(buffers[index][:0], buffers[index][complete:]...)
		}
		if cancelInputResult(ctx, result) {
			return
		}
		if activityComplete(result.Devices, categories) {
			break
		}
		operations.wait(ctx, min(20*time.Millisecond, max(time.Duration(0), deadline.Sub(operations.now()))))
	}
	if cancelInputResult(ctx, result) {
		return
	}
	for _, buffer := range buffers {
		if len(buffer) != 0 {
			recordInputFailure(result, errInputStream)
			return
		}
	}
	if activityComplete(result.Devices, categories) {
		result.Status, result.Detail = StatusPass, ""
	} else {
		result.Status, result.Detail = StatusInconclusive, "not every requested input category produced activity"
	}
}
