// Package pointerevidence validates versioned pointer observations without I/O.
package pointerevidence

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

var ErrInvalid = errors.New("invalid pointer evidence")

func Decode(raw []byte) (inputtest.PointerResult, error) {
	var header struct {
		Schema string `json:"schema"`
	}
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) || json.Unmarshal(raw, &header) != nil {
		return inputtest.PointerResult{}, ErrInvalid
	}
	var result inputtest.PointerResult
	switch header.Schema {
	case inputtest.PointerSchemaV1:
		var old legacyResult
		if evidencejson.ValidateShape(raw, reflect.TypeOf(old)) != nil || json.Unmarshal(raw, &old) != nil {
			return inputtest.PointerResult{}, ErrInvalid
		}
		result = old.view()
	case inputtest.PointerSchema:
		if evidencejson.ValidateShape(raw, reflect.TypeOf(result)) != nil || json.Unmarshal(raw, &result) != nil {
			return inputtest.PointerResult{}, ErrInvalid
		}
	default:
		return inputtest.PointerResult{}, ErrInvalid
	}
	if ValidateResult(result) != nil {
		return inputtest.PointerResult{}, ErrInvalid
	}
	return result, nil
}

func ValidateResult(result inputtest.PointerResult) error {
	if result.Schema != inputtest.PointerSchema && result.Schema != inputtest.PointerSchemaV1 ||
		result.DurationMilliseconds < 0 || result.DurationMilliseconds > 3600000 ||
		result.Schema == inputtest.PointerSchema && len(result.Devices) > 128 {
		return ErrInvalid
	}
	maximumLimit := int64(300000)
	if result.Schema == inputtest.PointerSchema {
		maximumLimit = 90000
	}
	if result.LimitMilliseconds < 10 || result.LimitMilliseconds > maximumLimit {
		if result.Status == inputtest.StatusError && result.StopReason == "error" && result.LimitMilliseconds == 0 && result.DurationMilliseconds == 0 && len(result.Devices) == 0 {
			return nil
		}
		return ErrInvalid
	}
	switch result.Status {
	case inputtest.StatusPass:
		if result.StopReason != "completed" || result.DurationMilliseconds > result.LimitMilliseconds || result.Detail != "" || len(result.Devices) == 0 {
			return ErrInvalid
		}
	case inputtest.StatusInconclusive:
		if result.StopReason != "observation_timeout" && result.StopReason != "error" {
			return ErrInvalid
		}
	case inputtest.StatusAborted:
		if result.StopReason != "operator_cancelled" {
			return ErrInvalid
		}
	case inputtest.StatusError:
		if result.StopReason != "error" && result.StopReason != "deadline" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	previous := ""
	for _, device := range result.Devices {
		if !canonicalInterface(device.Interface) || device.Interface <= previous || !validDevice(device, result.Schema == inputtest.PointerSchemaV1) || result.Status == inputtest.StatusPass && device.Status != inputtest.StatusPass {
			return ErrInvalid
		}
		previous = device.Interface
	}
	return nil
}

func HasPassedDevice(result inputtest.PointerResult) bool {
	for _, device := range result.Devices {
		if device.Status == inputtest.StatusPass {
			return true
		}
	}
	return false
}

func canonicalInterface(name string) bool {
	value, found := strings.CutPrefix(name, "event")
	if !found || len(value) == 0 || len(value) > 10 {
		return false
	}
	number, err := strconv.ParseUint(value, 10, 32)
	return err == nil && strconv.FormatUint(number, 10) == value
}

func buttonOrder(name string) int {
	switch name {
	case "left":
		return 0
	case "right":
		return 1
	case "middle":
		return 2
	}
	return -1
}

func wheelOrder(name string) int {
	switch name {
	case "vertical":
		return 0
	case "horizontal":
		return 1
	}
	return -1
}

func validDevice(device inputtest.PointerDeviceResult, legacy bool) bool {
	switch device.Status {
	case inputtest.StatusPass, inputtest.StatusInconclusive, inputtest.StatusError, inputtest.StatusAborted:
	default:
		return false
	}
	if device.XPositive > device.Frames || device.XNegative > device.Frames-device.XPositive ||
		device.YPositive > device.Frames || device.YNegative > device.Frames-device.YPositive {
		return false
	}
	advertised := []string(nil)
	if capabilities := device.Capabilities; capabilities != nil {
		last := -1
		for _, button := range capabilities.Buttons {
			order := buttonOrder(button)
			if order <= last {
				return false
			}
			last = order
		}
		advertised = capabilities.Buttons
		supported := device.Identity != nil && capabilities.Key && capabilities.Relative && capabilities.RelativeX && capabilities.RelativeY && !capabilities.Absolute && slices.Contains(advertised, "left")
		if device.Supported != supported {
			return false
		}
	} else if device.Supported {
		return false
	}
	if (!device.Readable || !device.Supported) && device.Frames != 0 {
		return false
	}
	if device.Supported && device.Readable && (device.Status == inputtest.StatusPass || device.Frames > 0 || len(device.Buttons) > 0) && len(device.Buttons) != len(advertised) {
		return false
	}
	last := -1
	for _, button := range device.Buttons {
		order := buttonOrder(button.Button)
		if order <= last || !slices.Contains(advertised, button.Button) || button.Presses > device.Frames || button.Releases > device.Frames-button.Presses || button.Pairs > button.Presses || button.Pairs > button.Releases {
			return false
		}
		last = order
		if device.Status == inputtest.StatusPass && (button.Pairs == 0 || button.Pairs != button.Presses || button.Pairs != button.Releases) {
			return false
		}
	}
	if !validWheels(device, legacy) {
		return false
	}
	if device.Status == inputtest.StatusPass {
		return device.Supported && device.Readable && !device.EvidenceLoss && device.Detail == "" && device.Frames > 0 &&
			device.XPositive > 0 && device.XNegative > 0 && device.YPositive > 0 && device.YNegative > 0
	}
	return true
}

func validWheels(device inputtest.PointerDeviceResult, legacy bool) bool {
	if legacy {
		return device.Wheels == nil && (device.Capabilities == nil || device.Capabilities.WheelAxes == nil)
	}
	if device.Wheels == nil {
		return false
	}
	var axes []inputtest.PointerWheelCapability
	if capabilities := device.Capabilities; capabilities != nil {
		axes = capabilities.WheelAxes
		if axes == nil || len(axes) > 0 && !capabilities.Relative {
			return false
		}
		last := -1
		for _, axis := range axes {
			order := wheelOrder(axis.Axis)
			if order <= last || !axis.Legacy && !axis.HighResolution {
				return false
			}
			last = order
		}
	}
	if !device.Readable || !device.Supported {
		return len(device.Wheels) == 0
	}
	if len(device.Wheels) != len(axes) {
		return false
	}
	for index, wheel := range device.Wheels {
		if wheel.Axis != axes[index].Axis || wheel.PositiveFrames > device.Frames || wheel.NegativeFrames > device.Frames-wheel.PositiveFrames {
			return false
		}
		if device.Status == inputtest.StatusPass && (wheel.PositiveFrames == 0 || wheel.NegativeFrames == 0) {
			return false
		}
	}
	return true
}
