package inputtest

import (
	"context"
	"errors"
	"slices"
	"time"
)

const TouchscreenSchema = "hhsoft.pc-tester.touchscreen-functional/v1"

const (
	maximumTouchscreenDevices       = 128
	maximumTouchscreenFrameEvents   = 4096
	maximumTouchscreenRecords       = 1 << 20
	maximumTouchscreenTotalRecords  = 8 << 20
	maximumTouchscreenBaselineBytes = 64 << 10
)

type TouchscreenOptions struct {
	Root     string
	Duration time.Duration
}

type TouchscreenIdentity struct {
	Bus     uint16 `json:"bus"`
	Vendor  uint16 `json:"vendor"`
	Product uint16 `json:"product"`
	Version uint16 `json:"version"`
}

// Value is deliberately absent: an ioctl coordinate is not fresh action input.
type TouchscreenAxis struct {
	Code       uint16 `json:"code"`
	Minimum    int32  `json:"minimum"`
	Maximum    int32  `json:"maximum"`
	Fuzz       int32  `json:"fuzz"`
	Flat       int32  `json:"flat"`
	Resolution int32  `json:"resolution"`
}

// Code lists are complete increasing sets, not selected advertised bits.
type TouchscreenCapabilities struct {
	ProtocolVersion int32             `json:"protocol_version"`
	Events          []uint16          `json:"events"`
	Keys            []uint16          `json:"keys"`
	Relative        []uint16          `json:"relative"`
	Properties      []uint16          `json:"properties"`
	Misc            []uint16          `json:"misc"`
	Axes            []TouchscreenAxis `json:"axes"`
}

type TouchscreenDeviceResult struct {
	Interface          string                   `json:"interface"`
	Status             Status                   `json:"status"`
	Supported          bool                     `json:"supported"`
	Readable           bool                     `json:"readable"`
	Identity           *TouchscreenIdentity     `json:"identity,omitempty"`
	Capabilities       *TouchscreenCapabilities `json:"capabilities,omitempty"`
	XThreshold         int64                    `json:"x_threshold"`
	YThreshold         int64                    `json:"y_threshold"`
	Frames             uint64                   `json:"frames"`
	ContactStarts      uint64                   `json:"contact_starts"`
	ContactReleases    uint64                   `json:"contact_releases"`
	XYBaselines        uint64                   `json:"xy_baselines"`
	QualifyingContacts uint64                   `json:"qualifying_contacts"`
	XPositive          uint64                   `json:"x_positive"`
	XNegative          uint64                   `json:"x_negative"`
	YPositive          uint64                   `json:"y_positive"`
	YNegative          uint64                   `json:"y_negative"`
	BaselineNeutral    bool                     `json:"baseline_neutral"`
	FinalNeutral       bool                     `json:"final_neutral"`
	EvidenceLoss       bool                     `json:"evidence_loss"`
	CleanupConfirmed   bool                     `json:"cleanup_confirmed"`
	Detail             string                   `json:"detail,omitempty"`
}

type TouchscreenResult struct {
	Schema               string                    `json:"schema"`
	Status               Status                    `json:"status"`
	StopReason           string                    `json:"stop_reason"`
	LimitMilliseconds    int64                     `json:"limit_ms"`
	DurationMilliseconds int64                     `json:"duration_ms"`
	CleanupConfirmed     bool                      `json:"cleanup_confirmed"`
	Devices              []TouchscreenDeviceResult `json:"devices"`
	Detail               string                    `json:"detail,omitempty"`
}

func (result *TouchscreenResult) MarkInterrupted(err error) {
	if err == nil || !result.CleanupConfirmed || result.Status == StatusError && result.Detail != "" {
		return
	}
	result.Status, result.StopReason, result.Detail = StatusAborted, "operator_cancelled", "operator_cancelled"
	if errors.Is(err, context.DeadlineExceeded) {
		result.Status, result.StopReason, result.Detail = StatusError, "deadline", "deadline"
	}
}

func validTouchscreenAxis(axis TouchscreenAxis) bool {
	span := int64(axis.Maximum) - int64(axis.Minimum)
	return span > 0 && axis.Fuzz >= 0 && axis.Flat >= 0 && axis.Resolution >= 0 &&
		int64(axis.Fuzz) <= span && int64(axis.Flat) <= span
}

// TouchscreenThreshold is an engineering criterion, not physical distance.
// Widen before every operation, including on 32-bit targets.
func TouchscreenThreshold(axis TouchscreenAxis) (int64, bool) {
	if !validTouchscreenAxis(axis) {
		return 0, false
	}
	span := int64(axis.Maximum) - int64(axis.Minimum)
	threshold := max(int64(1), (span+19)/20, 4*int64(axis.Fuzz)+1, 2*int64(axis.Flat)+1)
	return threshold, threshold <= span
}

func touchscreenCodeSet(got, allowed, required []uint16) bool {
	if len(got) > len(allowed) {
		return false
	}
	for i, code := range got {
		if i > 0 && got[i-1] >= code || !slices.Contains(allowed, code) {
			return false
		}
	}
	for _, code := range required {
		if !slices.Contains(got, code) {
			return false
		}
	}
	return true
}

// ValidateTouchscreenCapabilities admits only DIRECT single-contact X/Y with BTN_TOUCH.
// Unknown bits are not discarded to manufacture a supported subset.
func ValidateTouchscreenCapabilities(c TouchscreenCapabilities) (x, y int64, supported bool) {
	if c.ProtocolVersion != 0x010001 ||
		!touchscreenCodeSet(c.Events, []uint16{0, 1, 3, 4}, []uint16{0, 1, 3}) ||
		!touchscreenCodeSet(c.Keys, []uint16{0x14a}, []uint16{0x14a}) ||
		!touchscreenCodeSet(c.Properties, []uint16{1}, []uint16{1}) ||
		len(c.Relative) != 0 || !touchscreenCodeSet(c.Misc, []uint16{4, 5}, nil) ||
		slices.Contains(c.Events, uint16(4)) != (len(c.Misc) != 0) || len(c.Axes) != 2 {
		return 0, 0, false
	}
	for i, axis := range c.Axes {
		if i > 0 && c.Axes[i-1].Code >= axis.Code ||
			!slices.Contains([]uint16{0, 1}, axis.Code) || !validTouchscreenAxis(axis) {
			return 0, 0, false
		}
	}
	if c.Axes[0].Code != 0 || c.Axes[1].Code != 1 {
		return 0, 0, false
	}
	var xOK, yOK bool
	x, xOK = TouchscreenThreshold(c.Axes[0])
	y, yOK = TouchscreenThreshold(c.Axes[1])
	return x, y, xOK && yOK
}

type touchscreenMetadata struct {
	identity     TouchscreenIdentity
	capabilities TouchscreenCapabilities
}

// Freeze before crossing any context/provider callback. Oversized unsupported
// metadata is rejected without an unbounded copy or partial advertised subset.
func copyTouchscreenMetadata(m touchscreenMetadata) (touchscreenMetadata, bool) {
	c := m.capabilities
	if len(c.Events) > 8 || len(c.Keys) > 8 || len(c.Relative) > 4 ||
		len(c.Properties) > 8 || len(c.Misc) > 2 || len(c.Axes) > 4 {
		return touchscreenMetadata{}, false
	}
	c.Events = append([]uint16{}, c.Events...)
	c.Keys = append([]uint16{}, c.Keys...)
	c.Relative = append([]uint16{}, c.Relative...)
	c.Properties = append([]uint16{}, c.Properties...)
	c.Misc = append([]uint16{}, c.Misc...)
	c.Axes = append([]TouchscreenAxis{}, c.Axes...)
	m.capabilities = c
	return m, true
}

func sameTouchscreenMetadata(a, b touchscreenMetadata) bool {
	x, y := a.capabilities, b.capabilities
	return a.identity == b.identity && x.ProtocolVersion == y.ProtocolVersion &&
		slices.Equal(x.Events, y.Events) && slices.Equal(x.Keys, y.Keys) &&
		slices.Equal(x.Relative, y.Relative) && slices.Equal(x.Properties, y.Properties) &&
		slices.Equal(x.Misc, y.Misc) && slices.Equal(x.Axes, y.Axes)
}

// RunTouchscreen observes only the supported DIRECT single-contact protocol.
// Catalog and command activation are separate from this platform entry point.
func RunTouchscreen(ctx context.Context, options TouchscreenOptions) TouchscreenResult {
	return runTouchscreenPlatform(ctx, options)
}

func unsupportedTouchscreenPlatform(ctx context.Context, options TouchscreenOptions) TouchscreenResult {
	result := TouchscreenResult{Schema: TouchscreenSchema, Status: StatusError, StopReason: "error",
		LimitMilliseconds: options.Duration.Milliseconds(), CleanupConfirmed: true, Devices: []TouchscreenDeviceResult{}}
	if nilInputValue(ctx) || options.Duration < 10*time.Millisecond || options.Duration > 120*time.Second {
		result.LimitMilliseconds, result.Detail = 0, "invalid_options"
		return result
	}
	result.Status, result.StopReason, result.Detail = StatusInconclusive, "unsupported_platform", "unsupported_platform"
	result.MarkInterrupted(pointerContextError(ctx))
	return result
}
