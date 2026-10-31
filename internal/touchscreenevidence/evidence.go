// Package touchscreenevidence checks bounded touchscreen claims and their bindings.
// A valid claim does not independently establish kernel or physical history.
package touchscreenevidence

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

var ErrInvalid = errors.New("invalid touchscreen evidence")
var ErrCleanupUnconfirmed = errors.New("touchscreen cleanup is unconfirmed")

const (
	maxDevices            = 128
	maxRecords     uint64 = 1 << 20
	maxTotalFrames uint64 = 8 << 20
)

// Decode returns no observation for invalid input. A structurally valid cleanup
// diagnostic is returned with ErrCleanupUnconfirmed, never as success.
func Decode(raw []byte) (inputtest.TouchscreenResult, error) {
	var result inputtest.TouchscreenResult
	if !utf8.Valid(raw) || evidencejson.ValidateShape(raw, reflect.TypeOf(result)) != nil ||
		json.Unmarshal(raw, &result) != nil {
		return inputtest.TouchscreenResult{}, ErrInvalid
	}
	err := ValidateResult(result)
	if err != nil && !errors.Is(err, ErrCleanupUnconfirmed) {
		return inputtest.TouchscreenResult{}, ErrInvalid
	}
	return result, err
}

func canonicalInterface(name string) bool {
	suffix, ok := strings.CutPrefix(name, "event")
	if !ok || len(suffix) == 0 || len(suffix) > 10 {
		return false
	}
	number, err := strconv.ParseUint(suffix, 10, 32)
	return err == nil && strconv.FormatUint(number, 10) == suffix
}

func aggregateErrorDetail(detail string) bool {
	switch detail {
	case "registry_unavailable", "registry_changed", "identity_unavailable",
		"unsupported_record_layout", "identity_changed", "baseline_unavailable",
		"malformed_events", "event_loss", "disconnected", "invalid_owner",
		"ownership_alias", "record_limit", "frame_limit", "terminal_not_idle",
		"terminal_not_neutral", "unsupported_capabilities":
		return true
	}
	return false
}

func validAggregate(result inputtest.TouchscreenResult) bool {
	switch result.Status {
	case inputtest.StatusPass:
		return result.StopReason == "completed" && result.Detail == "" &&
			len(result.Devices) != 0 && result.CleanupConfirmed &&
			result.DurationMilliseconds <= result.LimitMilliseconds
	case inputtest.StatusAborted:
		return result.StopReason == "operator_cancelled" &&
			result.Detail == "operator_cancelled" && result.CleanupConfirmed
	case inputtest.StatusError:
		switch result.StopReason {
		case "cleanup_unconfirmed":
			return !result.CleanupConfirmed && result.Detail == "cleanup_unconfirmed"
		case "deadline":
			return result.CleanupConfirmed && result.Detail == "deadline"
		case "error":
			return result.CleanupConfirmed && aggregateErrorDetail(result.Detail)
		}
	case inputtest.StatusInconclusive:
		if !result.CleanupConfirmed {
			return false
		}
		switch result.StopReason {
		case "error":
			return result.Detail == "incomplete_actions" ||
				result.Detail == "unsupported_or_unreadable_device" && len(result.Devices) != 0
		case "observation_timeout":
			return result.Detail == "observation_timeout"
		case "no_touchscreens", "unsupported_platform":
			return result.Detail == result.StopReason && len(result.Devices) == 0
		}
	}
	return false
}

func ValidateResult(result inputtest.TouchscreenResult) error {
	if result.Schema != inputtest.TouchscreenSchema || result.Devices == nil ||
		len(result.Devices) > maxDevices || result.DurationMilliseconds < 0 ||
		result.DurationMilliseconds > 3600000 {
		return ErrInvalid
	}
	if result.LimitMilliseconds < 10 || result.LimitMilliseconds > 120000 {
		if result.LimitMilliseconds == 0 && result.DurationMilliseconds == 0 &&
			len(result.Devices) == 0 && result.Status == inputtest.StatusError &&
			result.StopReason == "error" && result.Detail == "invalid_options" && result.CleanupConfirmed {
			return nil
		}
		return ErrInvalid
	}
	if !validAggregate(result) {
		return ErrInvalid
	}
	var total uint64
	previous := ""
	for _, device := range result.Devices {
		if !canonicalInterface(device.Interface) || device.Interface <= previous ||
			!validDevice(device) || device.Frames > maxTotalFrames-total ||
			result.CleanupConfirmed && !device.CleanupConfirmed ||
			result.Status == inputtest.StatusPass && device.Status != inputtest.StatusPass {
			return ErrInvalid
		}
		total += device.Frames
		previous = device.Interface
	}
	if !result.CleanupConfirmed {
		return ErrCleanupUnconfirmed
	}
	return nil
}

func codes(values []uint16, maximum uint16, count int) bool {
	if values == nil || len(values) > count {
		return false
	}
	for i, value := range values {
		if value > maximum || i > 0 && values[i-1] >= value {
			return false
		}
	}
	return true
}

func boundedCapabilities(c inputtest.TouchscreenCapabilities) bool {
	if !codes(c.Events, 63, 8) || !codes(c.Keys, 767, 8) || !codes(c.Relative, 63, 4) ||
		!codes(c.Properties, 63, 8) || !codes(c.Misc, 63, 2) || c.Axes == nil || len(c.Axes) > 4 {
		return false
	}
	for i, axis := range c.Axes {
		if axis.Code > 63 || i > 0 && c.Axes[i-1].Code >= axis.Code {
			return false
		}
		if !slices.Contains([]uint16{0, 1}, axis.Code) &&
			(axis.Minimum != 0 || axis.Maximum != 0 || axis.Fuzz != 0 || axis.Flat != 0 || axis.Resolution != 0) {
			// An unqueried ABS code is a marker, not measured configuration.
			return false
		}
	}
	return true
}

func noActions(d inputtest.TouchscreenDeviceResult) bool {
	if d.Frames != 0 || d.ContactStarts != 0 || d.ContactReleases != 0 ||
		d.XYBaselines != 0 || d.QualifyingContacts != 0 || d.XPositive != 0 ||
		d.XNegative != 0 || d.YPositive != 0 || d.YNegative != 0 {
		return false
	}
	return true
}

func noState(d inputtest.TouchscreenDeviceResult) bool {
	return d.XThreshold == 0 && d.YThreshold == 0 &&
		noActions(d) && !d.BaselineNeutral && !d.FinalNeutral && !d.EvidenceLoss
}

func completeActions(d inputtest.TouchscreenDeviceResult) bool {
	if !d.BaselineNeutral || d.EvidenceLoss || d.QualifyingContacts == 0 ||
		d.ContactStarts != d.ContactReleases {
		return false
	}
	return true
}

// Details are phase-compatible, not a one-to-one map from text to status.
// In particular terminal revocation can be inconclusive after complete actions.
func validDeviceDetail(d inputtest.TouchscreenDeviceResult, initialized bool) bool {
	if d.Status == inputtest.StatusPass {
		return d.Detail == "" && d.CleanupConfirmed && d.FinalNeutral && completeActions(d)
	}
	if d.Status != inputtest.StatusInconclusive && d.Status != inputtest.StatusError {
		return false
	}
	if d.Detail == "cleanup_unconfirmed" {
		return d.Status == inputtest.StatusError && !d.CleanupConfirmed
	}
	if d.Status == inputtest.StatusError {
		switch d.Detail {
		case "identity_changed", "registry_changed", "baseline_unavailable",
			"malformed_events", "event_loss", "disconnected", "record_limit", "frame_limit":
			return initialized
		}
		return false
	}
	switch d.Detail {
	case "":
		return initialized && completeActions(d)
	case "incomplete_actions":
		return true
	case "identity_unavailable":
		return !d.Readable
	case "unsupported_capabilities":
		return d.Readable && !d.Supported
	case "unsupported_record_layout":
		return d.Supported && !initialized
	case "identity_changed", "registry_changed", "baseline_unavailable", "malformed_events",
		"event_loss", "disconnected", "record_limit", "frame_limit", "incomplete_events",
		"terminal_not_idle", "terminal_not_neutral":
		return initialized
	case "fresh_xy_missing":
		return initialized && d.BaselineNeutral && d.XYBaselines == 0 && d.QualifyingContacts == 0
	case "release_required":
		// Pending directions are intentionally absent from the wire format.
		return initialized && d.BaselineNeutral && d.QualifyingContacts == 0 &&
			d.ContactStarts > d.ContactReleases && d.XYBaselines > 0
	case "contact_incomplete":
		return initialized && d.BaselineNeutral && d.QualifyingContacts == 0
	case "controls_not_neutral":
		return initialized && d.BaselineNeutral && d.QualifyingContacts > 0 &&
			!completeActions(d)
	}
	return false
}

func validDevice(d inputtest.TouchscreenDeviceResult) bool {
	if d.CleanupConfirmed && d.Detail == "cleanup_unconfirmed" {
		return false
	}
	if !d.CleanupConfirmed && !(d.Status == inputtest.StatusError && d.Detail == "cleanup_unconfirmed") &&
		!(d.Status == inputtest.StatusInconclusive && !d.Readable &&
			(d.Detail == "identity_unavailable" || d.Detail == "incomplete_actions")) {
		return false
	}
	if !d.Readable {
		return d.Identity == nil && d.Capabilities == nil && !d.Supported &&
			noState(d) && validDeviceDetail(d, false)
	}
	if d.Identity == nil {
		return false
	}
	var x, y int64
	supported := false
	if d.Capabilities != nil {
		if !boundedCapabilities(*d.Capabilities) {
			return false
		}
		x, y, supported = inputtest.ValidateTouchscreenCapabilities(*d.Capabilities)
	}
	if d.Supported != supported {
		return false
	}
	if !supported {
		return noState(d) && validDeviceDetail(d, false)
	}
	if noState(d) {
		return d.Status != inputtest.StatusPass && validDeviceDetail(d, false)
	}
	if d.XThreshold != x || d.YThreshold != y || d.Frames > maxRecords {
		return false
	}
	for _, count := range []uint64{d.ContactStarts, d.ContactReleases, d.XYBaselines,
		d.QualifyingContacts, d.XPositive, d.XNegative, d.YPositive, d.YNegative} {
		if count > maxRecords {
			return false
		}
	}
	if d.ContactStarts > d.Frames || d.ContactReleases > d.Frames-d.ContactStarts ||
		d.ContactReleases > d.ContactStarts ||
		d.ContactStarts-d.ContactReleases > 1 || d.XYBaselines > d.ContactStarts ||
		d.QualifyingContacts > d.ContactReleases || d.QualifyingContacts > d.XYBaselines ||
		d.XPositive > d.Frames || d.XNegative > d.Frames-d.XPositive ||
		d.YPositive > d.Frames || d.YNegative > d.Frames-d.YPositive ||
		d.QualifyingContacts > d.XPositive || d.QualifyingContacts > d.XNegative ||
		d.QualifyingContacts > d.YPositive || d.QualifyingContacts > d.YNegative {
		return false
	}
	// Start/release frames cannot contribute motion; different axes may overlap.
	remaining := d.Frames - d.ContactStarts - d.ContactReleases
	if d.XPositive > remaining || d.XNegative > remaining-d.XPositive ||
		d.YPositive > remaining || d.YNegative > remaining-d.YPositive ||
		(d.XPositive != 0 || d.XNegative != 0 || d.YPositive != 0 || d.YNegative != 0) &&
			(d.ContactReleases == 0 || d.XYBaselines == 0) {
		return false
	}
	if !d.BaselineNeutral && !noActions(d) ||
		d.FinalNeutral && (!completeActions(d) ||
			!(d.Status == inputtest.StatusPass && d.Detail == "" ||
				d.Status == inputtest.StatusError && d.Detail == "cleanup_unconfirmed")) {
		return false
	}
	return validDeviceDetail(d, true)
}
