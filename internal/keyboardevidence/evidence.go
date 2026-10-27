// Package keyboardevidence validates the bounded, text-free keyboard evidence.
package keyboardevidence

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

var ErrInvalid = errors.New("invalid keyboard evidence")
var ErrCleanupUnconfirmed = errors.New("keyboard cleanup is unconfirmed")

func Decode(raw []byte) (inputtest.KeyboardResult, error) {
	var result inputtest.KeyboardResult
	if !utf8.Valid(raw) || evidencejson.ValidateShape(raw, reflect.TypeOf(result)) != nil || json.Unmarshal(raw, &result) != nil {
		return inputtest.KeyboardResult{}, ErrInvalid
	}
	err := ValidateResult(result)
	if err != nil && !errors.Is(err, ErrCleanupUnconfirmed) {
		return inputtest.KeyboardResult{}, err
	}
	return result, err
}

func allowedDetail(value string) bool {
	switch value {
	case "", "invalid_options", "unsupported_platform", "registry_unavailable", "device_limit", "no_keyboards", "incomplete_actions", "identity_unavailable", "unsupported_capabilities", "unsupported_record_layout", "identity_changed", "registry_changed", "baseline_unavailable", "malformed_events", "event_loss", "disconnected", "observation_timeout", "operator_cancelled", "deadline", "cleanup_unconfirmed", "console_unavailable":
		return true
	}
	return false
}

func canonicalInterface(name string) bool {
	if !strings.HasPrefix(name, "event") {
		return false
	}
	number := strings.TrimPrefix(name, "event")
	value, err := strconv.ParseUint(number, 10, 32)
	return err == nil && strconv.FormatUint(value, 10) == number
}

func ValidateResult(result inputtest.KeyboardResult) error {
	if result.Schema != inputtest.KeyboardSchema || result.Devices == nil || len(result.Devices) > 128 || result.DurationMilliseconds < 0 || result.DurationMilliseconds > 3600000 || !allowedDetail(result.Detail) {
		return ErrInvalid
	}
	if result.LimitMilliseconds < 10 || result.LimitMilliseconds > 90000 {
		if result.LimitMilliseconds == 0 && result.DurationMilliseconds == 0 && len(result.Devices) == 0 && result.Status == inputtest.StatusError && result.StopReason == "error" && result.Detail == "invalid_options" && result.CleanupConfirmed {
			return nil
		}
		return ErrInvalid
	}
	switch result.Status {
	case inputtest.StatusPass:
		if result.StopReason != "completed" || result.Detail != "" || len(result.Devices) == 0 || !result.CleanupConfirmed || result.DurationMilliseconds > result.LimitMilliseconds {
			return ErrInvalid
		}
	case inputtest.StatusInconclusive:
		if result.StopReason != "error" && result.StopReason != "observation_timeout" {
			return ErrInvalid
		}
	case inputtest.StatusError:
		if result.StopReason != "error" && result.StopReason != "deadline" && result.StopReason != "cleanup_unconfirmed" {
			return ErrInvalid
		}
	case inputtest.StatusAborted:
		if result.StopReason != "operator_cancelled" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if result.Status != inputtest.StatusPass && result.Detail == "" {
		return ErrInvalid
	}
	if result.Detail == "invalid_options" || result.Detail == "cleanup_unconfirmed" && result.StopReason != "cleanup_unconfirmed" {
		return ErrInvalid
	}
	if (result.StopReason == "deadline" || result.StopReason == "operator_cancelled" || result.StopReason == "observation_timeout") && result.Detail != result.StopReason {
		return ErrInvalid
	}
	if result.StopReason == "cleanup_unconfirmed" {
		if result.CleanupConfirmed || result.Detail != "cleanup_unconfirmed" {
			return ErrInvalid
		}
	} else if !result.CleanupConfirmed {
		return ErrInvalid
	}
	previous := ""
	for _, device := range result.Devices {
		if !canonicalInterface(device.Interface) || device.Interface <= previous || !validDevice(device) || result.CleanupConfirmed && !device.CleanupConfirmed || result.Status == inputtest.StatusPass && device.Status != inputtest.StatusPass {
			return ErrInvalid
		}
		previous = device.Interface
	}
	if !result.CleanupConfirmed {
		return ErrCleanupUnconfirmed
	}
	return nil
}

func validDevice(device inputtest.KeyboardDeviceResult) bool {
	if device.Actions == nil || !allowedDetail(device.Detail) {
		return false
	}
	switch device.Status {
	case inputtest.StatusPass:
		if device.Detail != "" || !device.CleanupConfirmed || !device.FinalNeutral || !device.Supported || !device.Readable || device.EvidenceLoss {
			return false
		}
	case inputtest.StatusInconclusive, inputtest.StatusError, inputtest.StatusAborted:
		if device.Detail == "" {
			return false
		}
	default:
		return false
	}
	if !device.CleanupConfirmed && (device.Status != inputtest.StatusError || device.Detail != "cleanup_unconfirmed") {
		return false
	}
	if device.CleanupConfirmed && device.Detail == "cleanup_unconfirmed" {
		return false
	}
	if !device.Readable {
		return device.Identity == nil && device.Capabilities == nil && !device.Supported && len(device.Actions) == 0 && !device.BaselineNeutral && !device.FinalNeutral && !device.EvidenceLoss
	}
	if device.Identity == nil || device.Capabilities == nil || device.Capabilities.FixedKeys == nil {
		return false
	}
	names, previous := inputtest.KeyboardActions(), -1
	for _, name := range device.Capabilities.FixedKeys {
		found := -1
		for index, known := range names {
			if known == name {
				found = index
			}
		}
		if found <= previous {
			return false
		}
		previous = found
	}
	if !device.Capabilities.EventKey && len(device.Capabilities.FixedKeys) != 0 || device.Supported != (device.Capabilities.EventKey && len(device.Capabilities.FixedKeys) == len(names)) {
		return false
	}
	if !device.Supported {
		return len(device.Actions) == 0 && !device.BaselineNeutral && !device.FinalNeutral && !device.EvidenceLoss
	}
	if len(device.Actions) != len(names) {
		return false
	}
	all := true
	for index, action := range device.Actions {
		if action.Name != names[index] || action.ReleaseObserved != action.PairCompleted || action.PairCompleted && !action.PressObserved || action.PressObserved && !device.BaselineNeutral {
			return false
		}
		all = all && action.PairCompleted
	}
	return !device.FinalNeutral || device.BaselineNeutral && all && !device.EvidenceLoss
}
