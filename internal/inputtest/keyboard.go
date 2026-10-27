package inputtest

import (
	"context"
	"errors"
	"time"
)

const KeyboardSchema = "hhsoft.pc-tester.keyboard-functional/v1"

type KeyboardOptions struct {
	Root     string
	Duration time.Duration
}
type KeyboardIdentity struct {
	Bus     uint16 `json:"bus"`
	Vendor  uint16 `json:"vendor"`
	Product uint16 `json:"product"`
	Version uint16 `json:"version"`
}
type KeyboardCapabilities struct {
	EventKey  bool     `json:"event_key"`
	FixedKeys []string `json:"fixed_keys"`
}
type KeyboardAction struct {
	Name            string `json:"name"`
	PressObserved   bool   `json:"press_observed"`
	ReleaseObserved bool   `json:"release_observed"`
	PairCompleted   bool   `json:"pair_completed"`
}
type KeyboardDeviceResult struct {
	Interface        string                `json:"interface"`
	Status           Status                `json:"status"`
	Supported        bool                  `json:"supported"`
	Readable         bool                  `json:"readable"`
	Identity         *KeyboardIdentity     `json:"identity,omitempty"`
	Capabilities     *KeyboardCapabilities `json:"capabilities,omitempty"`
	Actions          []KeyboardAction      `json:"actions"`
	BaselineNeutral  bool                  `json:"baseline_neutral"`
	FinalNeutral     bool                  `json:"final_neutral"`
	EvidenceLoss     bool                  `json:"evidence_loss"`
	CleanupConfirmed bool                  `json:"cleanup_confirmed"`
	Detail           string                `json:"detail,omitempty"`
}
type KeyboardResult struct {
	Schema               string                 `json:"schema"`
	Status               Status                 `json:"status"`
	StopReason           string                 `json:"stop_reason"`
	LimitMilliseconds    int64                  `json:"limit_ms"`
	DurationMilliseconds int64                  `json:"duration_ms"`
	CleanupConfirmed     bool                   `json:"cleanup_confirmed"`
	Devices              []KeyboardDeviceResult `json:"devices"`
	Detail               string                 `json:"detail,omitempty"`
}

// KeyboardActions returns the immutable scope in canonical order, not input order.
func KeyboardActions() [6]string {
	return [6]string{"left_shift", "right_shift", "up", "down", "left", "right"}
}

func RunKeyboard(ctx context.Context, options KeyboardOptions) KeyboardResult {
	return runKeyboardPlatform(ctx, options)
}

func (result *KeyboardResult) MarkInterrupted(err error) {
	if err == nil || !result.CleanupConfirmed {
		return
	}
	result.Status, result.StopReason, result.Detail = StatusAborted, "operator_cancelled", "operator_cancelled"
	if errors.Is(err, context.DeadlineExceeded) {
		result.Status, result.StopReason, result.Detail = StatusError, "deadline", "deadline"
	}
}

func newKeyboardResult(options KeyboardOptions) KeyboardResult {
	return KeyboardResult{Schema: KeyboardSchema, Status: StatusError, StopReason: "error",
		LimitMilliseconds: options.Duration.Milliseconds(), CleanupConfirmed: true,
		Devices: []KeyboardDeviceResult{}}
}
