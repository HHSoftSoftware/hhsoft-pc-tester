package inputtest

import (
	"context"
	"errors"
	"time"
)

const (
	PointerSchema   = "hhsoft.pc-tester.pointer-functional/v2"
	PointerSchemaV1 = "hhsoft.pc-tester.pointer-functional/v1"
)

type PointerOptions struct {
	Root     string
	Duration time.Duration
}

type PointerIdentity struct {
	Bus     uint16 `json:"bus"`
	Vendor  uint16 `json:"vendor"`
	Product uint16 `json:"product"`
	Version uint16 `json:"version"`
}

// Only advertised criteria tested by this worker are exported. Extra buttons,
// wheel resolution, and absolute coordinates are not qualified.
type PointerCapabilities struct {
	Key       bool                     `json:"key"`
	Relative  bool                     `json:"relative"`
	RelativeX bool                     `json:"relative_x"`
	RelativeY bool                     `json:"relative_y"`
	Absolute  bool                     `json:"absolute"`
	Buttons   []string                 `json:"buttons"`
	WheelAxes []PointerWheelCapability `json:"wheel_axes"`
}

type PointerWheelCapability struct {
	Axis           string `json:"axis"`
	Legacy         bool   `json:"legacy"`
	HighResolution bool   `json:"high_resolution"`
}

type PointerWheelResult struct {
	Axis           string `json:"axis"`
	PositiveFrames uint64 `json:"positive_frames"`
	NegativeFrames uint64 `json:"negative_frames"`
}

type PointerButtonResult struct {
	Button   string `json:"button"`
	Presses  uint64 `json:"presses"`
	Releases uint64 `json:"releases"`
	Pairs    uint64 `json:"pairs"`
}

type PointerDeviceResult struct {
	Interface    string                `json:"interface"`
	Status       Status                `json:"status"`
	Supported    bool                  `json:"supported"`
	Readable     bool                  `json:"readable"`
	Identity     *PointerIdentity      `json:"identity,omitempty"`
	Capabilities *PointerCapabilities  `json:"capabilities,omitempty"`
	XPositive    uint64                `json:"x_positive"`
	XNegative    uint64                `json:"x_negative"`
	YPositive    uint64                `json:"y_positive"`
	YNegative    uint64                `json:"y_negative"`
	Buttons      []PointerButtonResult `json:"buttons,omitempty"`
	Wheels       []PointerWheelResult  `json:"wheels"`
	Frames       uint64                `json:"frames"`
	EvidenceLoss bool                  `json:"evidence_loss"`
	Detail       string                `json:"detail,omitempty"`
}

type PointerResult struct {
	Schema               string                `json:"schema"`
	Status               Status                `json:"status"`
	StopReason           string                `json:"stop_reason"`
	LimitMilliseconds    int64                 `json:"limit_ms"`
	DurationMilliseconds int64                 `json:"duration_ms"`
	Devices              []PointerDeviceResult `json:"devices,omitempty"`
	Detail               string                `json:"detail,omitempty"`
}

func RunPointer(ctx context.Context, options PointerOptions) PointerResult {
	return runPointerPlatform(ctx, options)
}

// MarkInterrupted retains observations and any explicit failure while
// preventing a result at a cancellation boundary from claiming pass.
func (result *PointerResult) MarkInterrupted(err error) {
	if err == nil {
		return
	}
	status, reason, detail := StatusAborted, "operator_cancelled", "pointer observation cancelled"
	if errors.Is(err, context.DeadlineExceeded) {
		status, reason, detail = StatusError, "deadline", "pointer command deadline exceeded"
	}
	if result.Status != Status("fail") {
		result.Status = status
	}
	result.StopReason, result.Detail = reason, detail
}
