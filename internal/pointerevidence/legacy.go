package pointerevidence

import "github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"

// These types preserve the exact v1 wire shape. The common decoded view is
// read-only historical evidence, never a replacement encoding for its bytes.
type legacyCapabilities struct {
	Key       bool     `json:"key"`
	Relative  bool     `json:"relative"`
	RelativeX bool     `json:"relative_x"`
	RelativeY bool     `json:"relative_y"`
	Absolute  bool     `json:"absolute"`
	Buttons   []string `json:"buttons"`
}

type legacyDevice struct {
	Interface    string                          `json:"interface"`
	Status       inputtest.Status                `json:"status"`
	Supported    bool                            `json:"supported"`
	Readable     bool                            `json:"readable"`
	Identity     *inputtest.PointerIdentity      `json:"identity,omitempty"`
	Capabilities *legacyCapabilities             `json:"capabilities,omitempty"`
	XPositive    uint64                          `json:"x_positive"`
	XNegative    uint64                          `json:"x_negative"`
	YPositive    uint64                          `json:"y_positive"`
	YNegative    uint64                          `json:"y_negative"`
	Buttons      []inputtest.PointerButtonResult `json:"buttons,omitempty"`
	Frames       uint64                          `json:"frames"`
	EvidenceLoss bool                            `json:"evidence_loss"`
	Detail       string                          `json:"detail,omitempty"`
}

type legacyResult struct {
	Schema               string           `json:"schema"`
	Status               inputtest.Status `json:"status"`
	StopReason           string           `json:"stop_reason"`
	LimitMilliseconds    int64            `json:"limit_ms"`
	DurationMilliseconds int64            `json:"duration_ms"`
	Devices              []legacyDevice   `json:"devices,omitempty"`
	Detail               string           `json:"detail,omitempty"`
}

func (old legacyResult) view() inputtest.PointerResult {
	result := inputtest.PointerResult{Schema: old.Schema, Status: old.Status, StopReason: old.StopReason,
		LimitMilliseconds: old.LimitMilliseconds, DurationMilliseconds: old.DurationMilliseconds, Detail: old.Detail}
	if old.Devices != nil {
		result.Devices = make([]inputtest.PointerDeviceResult, len(old.Devices))
	}
	for index, device := range old.Devices {
		value := inputtest.PointerDeviceResult{Interface: device.Interface, Status: device.Status,
			Supported: device.Supported, Readable: device.Readable, Identity: device.Identity,
			XPositive: device.XPositive, XNegative: device.XNegative, YPositive: device.YPositive, YNegative: device.YNegative,
			Buttons: device.Buttons, Frames: device.Frames, EvidenceLoss: device.EvidenceLoss, Detail: device.Detail}
		if c := device.Capabilities; c != nil {
			value.Capabilities = &inputtest.PointerCapabilities{Key: c.Key, Relative: c.Relative,
				RelativeX: c.RelativeX, RelativeY: c.RelativeY, Absolute: c.Absolute, Buttons: c.Buttons}
		}
		result.Devices[index] = value
	}
	return result
}
