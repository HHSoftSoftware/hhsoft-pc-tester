package keyboardevidence

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

func TestRealKeyboardPlatformErrorsRemainStrictDiagnostics(t *testing.T) {
	result := inputtest.RunKeyboard(context.Background(), inputtest.KeyboardOptions{Root: t.TempDir(), Duration: time.Second})
	if result.Status == inputtest.StatusPass || !result.CleanupConfirmed || ValidateResult(result) != nil {
		t.Fatal("platform/registry diagnostic is not strict or falsely passed")
	}
	result = inputtest.RunKeyboard(nil, inputtest.KeyboardOptions{})
	if ValidateResult(result) != nil || result.Detail != "invalid_options" {
		t.Fatal("real invalid-options diagnostic rejected")
	}
}

const completeEvidence = `{"schema":"hhsoft.pc-tester.keyboard-functional/v1","status":"pass","stop_reason":"completed","limit_ms":30000,"duration_ms":1200,"cleanup_confirmed":true,"devices":[{"interface":"event2","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":4660,"product":22136,"version":256},"capabilities":{"event_key":true,"fixed_keys":["left_shift","right_shift","up","down","left","right"]},"actions":[{"name":"left_shift","press_observed":true,"release_observed":true,"pair_completed":true},{"name":"right_shift","press_observed":true,"release_observed":true,"pair_completed":true},{"name":"up","press_observed":true,"release_observed":true,"pair_completed":true},{"name":"down","press_observed":true,"release_observed":true,"pair_completed":true},{"name":"left","press_observed":true,"release_observed":true,"pair_completed":true},{"name":"right","press_observed":true,"release_observed":true,"pair_completed":true}],"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]}`

func validKeyboard(t *testing.T) inputtest.KeyboardResult {
	t.Helper()
	result, err := Decode([]byte(completeEvidence))
	if err != nil {
		t.Fatalf("independent complete fixture rejected: %v", err)
	}
	return result
}

func TestDecodeKeyboardStrictShapeAndTextFreeScope(t *testing.T) {
	validKeyboard(t)
	cases := map[string]string{
		"unknown top":       strings.Replace(completeEvidence, `"schema":`, `"typed_text":"private","schema":`, 1),
		"duplicate top":     strings.Replace(completeEvidence, `"status":"pass"`, `"status":"pass","status":"pass"`, 1),
		"case alias":        strings.Replace(completeEvidence, `"status":`, `"Status":`, 1),
		"decoded duplicate": strings.Replace(completeEvidence, `"status":"pass"`, `"status":"pass","st\u0061tus":"pass"`, 1),
		"missing bool":      strings.Replace(completeEvidence, `"baseline_neutral":true,`, "", 1),
		"null bool":         strings.Replace(completeEvidence, `"baseline_neutral":true`, `"baseline_neutral":null`, 1),
		"null devices":      strings.Replace(completeEvidence, completeEvidence[strings.Index(completeEvidence, `[{"interface"`):len(completeEvidence)-1], "null", 1),
		"nested duplicate":  strings.Replace(completeEvidence, `"bus":3`, `"bus":3,"bus":3`, 1),
		"raw code":          strings.Replace(completeEvidence, `"name":"up"`, `"name":"up","key_code":103`, 1),
		"unknown control":   strings.Replace(completeEvidence, `"name":"up"`, `"name":"enter"`, 1),
		"trailing document": completeEvidence + `{}`,
		"too large":         strings.Repeat(" ", 1<<20) + completeEvidence,
		"invalid utf8":      strings.Replace(completeEvidence, "left_shift", string([]byte{255}), 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := Decode([]byte(raw))
			if !errors.Is(err, ErrInvalid) || result.Schema != "" {
				t.Fatal("malformed shape supplied typed evidence")
			}
		})
	}
}

func TestValidateKeyboardContradictions(t *testing.T) {
	cases := map[string]func(*inputtest.KeyboardResult){
		"cleanup detail contradiction": func(v *inputtest.KeyboardResult) {
			v.Status = inputtest.StatusInconclusive
			v.StopReason = "error"
			v.Detail = "cleanup_unconfirmed"
		},
		"device cleanup detail contradiction": func(v *inputtest.KeyboardResult) {
			v.Devices[0].Status = inputtest.StatusError
			v.Devices[0].Detail = "cleanup_unconfirmed"
		},
		"deadline detail contradiction": func(v *inputtest.KeyboardResult) {
			v.Status = inputtest.StatusError
			v.StopReason = "deadline"
			v.Detail = "unsupported_platform"
		},
		"unknown schema":         func(v *inputtest.KeyboardResult) { v.Schema = "v0" },
		"null slices":            func(v *inputtest.KeyboardResult) { v.Devices = nil },
		"empty pass":             func(v *inputtest.KeyboardResult) { v.Devices = []inputtest.KeyboardDeviceResult{} },
		"duplicate interface":    func(v *inputtest.KeyboardResult) { v.Devices = append(v.Devices, v.Devices[0]) },
		"noncanonical interface": func(v *inputtest.KeyboardResult) { v.Devices[0].Interface = "event02" },
		"too many devices": func(v *inputtest.KeyboardResult) {
			for len(v.Devices) < 129 {
				v.Devices = append(v.Devices, v.Devices[0])
			}
		},
		"late pass":            func(v *inputtest.KeyboardResult) { v.DurationMilliseconds = v.LimitMilliseconds + 1 },
		"unbounded window":     func(v *inputtest.KeyboardResult) { v.LimitMilliseconds = 90001 },
		"negative duration":    func(v *inputtest.KeyboardResult) { v.DurationMilliseconds = -1 },
		"unknown status":       func(v *inputtest.KeyboardResult) { v.Status = "fail" },
		"pass timeout":         func(v *inputtest.KeyboardResult) { v.StopReason = "observation_timeout" },
		"prose detail":         func(v *inputtest.KeyboardResult) { v.Detail = "unbounded input text" },
		"loss pass":            func(v *inputtest.KeyboardResult) { v.Devices[0].EvidenceLoss = true },
		"no baseline":          func(v *inputtest.KeyboardResult) { v.Devices[0].BaselineNeutral = false },
		"no neutral end":       func(v *inputtest.KeyboardResult) { v.Devices[0].FinalNeutral = false },
		"release without pair": func(v *inputtest.KeyboardResult) { v.Devices[0].Actions[0].PairCompleted = false },
		"pair without press":   func(v *inputtest.KeyboardResult) { v.Devices[0].Actions[0].PressObserved = false },
		"action order": func(v *inputtest.KeyboardResult) {
			v.Devices[0].Actions[0], v.Devices[0].Actions[1] = v.Devices[0].Actions[1], v.Devices[0].Actions[0]
		},
		"capability order": func(v *inputtest.KeyboardResult) {
			v.Devices[0].Capabilities.FixedKeys[0], v.Devices[0].Capabilities.FixedKeys[1] = v.Devices[0].Capabilities.FixedKeys[1], v.Devices[0].Capabilities.FixedKeys[0]
		},
		"missing event capability":     func(v *inputtest.KeyboardResult) { v.Devices[0].Capabilities.EventKey = false },
		"missing identity":             func(v *inputtest.KeyboardResult) { v.Devices[0].Identity = nil },
		"unreadable identity":          func(v *inputtest.KeyboardResult) { v.Devices[0].Readable = false },
		"unsupported pass":             func(v *inputtest.KeyboardResult) { v.Devices[0].Supported = false },
		"hidden cleanup failure":       func(v *inputtest.KeyboardResult) { v.Devices[0].CleanupConfirmed = false },
		"cleanup status contradiction": func(v *inputtest.KeyboardResult) { v.CleanupConfirmed = false },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			result := validKeyboard(t)
			change(&result)
			if !errors.Is(ValidateResult(result), ErrInvalid) {
				t.Fatal("impossible typed evidence accepted")
			}
			raw, _ := json.Marshal(result)
			if _, err := Decode(raw); !errors.Is(err, ErrInvalid) {
				t.Fatal("typed and raw validation diverged")
			}
		})
	}
}

func TestKeyboardDiagnosticsAndCleanupDisposition(t *testing.T) {
	for _, condition := range []string{"timeout", "cancelled", "deadline", "console cleanup", "device cleanup", "unreadable", "unsupported", "no devices", "invalid options"} {
		t.Run(condition, func(t *testing.T) {
			result := validKeyboard(t)
			switch condition {
			case "timeout":
				result.Status = inputtest.StatusInconclusive
				result.StopReason = "observation_timeout"
				result.Detail = "observation_timeout"
			case "cancelled":
				result.MarkInterrupted(errors.New("cancelled"))
			case "deadline":
				result.Status = inputtest.StatusError
				result.StopReason = "deadline"
				result.Detail = "deadline"
			case "console cleanup", "device cleanup":
				result.Status = inputtest.StatusError
				result.StopReason = "cleanup_unconfirmed"
				result.Detail = "cleanup_unconfirmed"
				result.CleanupConfirmed = false
				if condition == "device cleanup" {
					result.Devices[0].Status = inputtest.StatusError
					result.Devices[0].Detail = "cleanup_unconfirmed"
					result.Devices[0].CleanupConfirmed = false
				}
			case "unreadable":
				result.Status = inputtest.StatusInconclusive
				result.StopReason = "error"
				result.Detail = "incomplete_actions"
				result.Devices[0] = inputtest.KeyboardDeviceResult{Interface: "event2", Status: inputtest.StatusInconclusive, Actions: []inputtest.KeyboardAction{}, CleanupConfirmed: true, Detail: "identity_unavailable"}
			case "unsupported":
				result.Status = inputtest.StatusInconclusive
				result.StopReason = "error"
				result.Detail = "incomplete_actions"
				device := &result.Devices[0]
				device.Status = inputtest.StatusInconclusive
				device.Detail = "unsupported_capabilities"
				device.Supported = false
				device.Capabilities.FixedKeys = []string{"left_shift"}
				device.Actions = []inputtest.KeyboardAction{}
				device.BaselineNeutral = false
				device.FinalNeutral = false
			case "no devices":
				result.Status = inputtest.StatusInconclusive
				result.StopReason = "error"
				result.Detail = "no_keyboards"
				result.Devices = []inputtest.KeyboardDeviceResult{}
			case "invalid options":
				result.Status = inputtest.StatusError
				result.StopReason = "error"
				result.Detail = "invalid_options"
				result.Devices = []inputtest.KeyboardDeviceResult{}
				result.DurationMilliseconds = 0
				result.LimitMilliseconds = 0
			}
			want := error(nil)
			if condition == "console cleanup" || condition == "device cleanup" {
				want = ErrCleanupUnconfirmed
			}
			raw, _ := json.Marshal(result)
			decoded, err := Decode(raw)
			if !errors.Is(err, want) || decoded.Schema != inputtest.KeyboardSchema {
				t.Fatalf("valid diagnostic disposition lost: %v", err)
			}
			if want != nil {
				decoded.MarkInterrupted(errors.New("cancelled"))
				if decoded.StopReason != "cleanup_unconfirmed" {
					t.Fatal("cleanup disposition weakened")
				}
				decoded.Devices[0].Actions[0].Name = "typed text"
				if !errors.Is(ValidateResult(decoded), ErrInvalid) {
					t.Fatal("cleanup error masked invalid evidence")
				}
			}
		})
	}
}
