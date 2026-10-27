package pointerevidence

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

const historicalPointer = `{"schema":"hhsoft.pc-tester.pointer-functional/v1","status":"pass","stop_reason":"completed","limit_ms":300000,"duration_ms":10,"devices":[{"interface":"event2","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":4660,"product":22136,"version":258},"capabilities":{"key":true,"relative":true,"relative_x":true,"relative_y":true,"absolute":false,"buttons":["left"]},"x_positive":1,"x_negative":1,"y_positive":1,"y_negative":1,"buttons":[{"button":"left","presses":1,"releases":1,"pairs":1}],"frames":4,"evidence_loss":false}]}`

func validPointer(axes ...inputtest.PointerWheelCapability) inputtest.PointerResult {
	device := inputtest.PointerDeviceResult{Interface: "event2", Status: inputtest.StatusPass, Supported: true, Readable: true,
		Identity:     &inputtest.PointerIdentity{Bus: 3, Vendor: 0x1234, Product: 0x5678, Version: 0x0102},
		Capabilities: &inputtest.PointerCapabilities{Key: true, Relative: true, RelativeX: true, RelativeY: true, Buttons: []string{"left"}, WheelAxes: append([]inputtest.PointerWheelCapability{}, axes...)},
		XPositive:    1, XNegative: 1, YPositive: 1, YNegative: 1, Frames: 4,
		Buttons: []inputtest.PointerButtonResult{{Button: "left", Presses: 1, Releases: 1, Pairs: 1}}, Wheels: []inputtest.PointerWheelResult{}}
	for _, axis := range axes {
		device.Wheels = append(device.Wheels, inputtest.PointerWheelResult{Axis: axis.Axis, PositiveFrames: 1, NegativeFrames: 1})
	}
	return inputtest.PointerResult{Schema: inputtest.PointerSchema, Status: inputtest.StatusPass, StopReason: "completed", LimitMilliseconds: 90000, DurationMilliseconds: 10, Devices: []inputtest.PointerDeviceResult{device}}
}

func encodePointer(t *testing.T, result inputtest.PointerResult) []byte {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestHistoricalPointerWireRemainsExactAndOwnsItsView(t *testing.T) {
	raw := []byte(historicalPointer)
	value, err := Decode(raw)
	if err != nil || value.Schema != inputtest.PointerSchemaV1 || value.LimitMilliseconds != 300000 || value.Devices[0].Wheels != nil || value.Devices[0].Capabilities.WheelAxes != nil {
		t.Fatal("historical movement/button coverage changed")
	}
	value.Devices[0].Buttons[0].Pairs = 999
	value.Devices[0].Capabilities.Buttons[0] = "changed"
	value.Devices[0].Identity.Vendor = 0
	again, err := Decode(raw)
	if err != nil || again.Devices[0].Buttons[0].Pairs != 1 || again.Devices[0].Capabilities.Buttons[0] != "left" || again.Devices[0].Identity.Vendor != 0x1234 || string(raw) != historicalPointer {
		t.Fatal("decoded view aliases source bytes or another view")
	}
	for _, suffix := range []string{"", `,"devices":null`, `,"devices":[]`} {
		data := `{"schema":"hhsoft.pc-tester.pointer-functional/v1","status":"error","stop_reason":"error","limit_ms":0,"duration_ms":0` + suffix + `}`
		if _, err := Decode([]byte(data)); err != nil {
			t.Fatal("old omitted/null/empty device semantics changed")
		}
	}
	for _, replacement := range []string{`"buttons":null`, `"buttons":[]`} {
		data := strings.Replace(historicalPointer, `"status":"pass"`, `"status":"inconclusive"`, 1)
		data = strings.Replace(data, `"stop_reason":"completed"`, `"stop_reason":"error"`, 1)
		data = strings.Replace(data, `"status":"pass","supported":true,"readable":true`, `"status":"inconclusive","supported":false,"readable":true`, 1)
		data = strings.Replace(data, `"buttons":["left"]`, replacement, 1)
		data = strings.Replace(data, `"buttons":[{"button":"left","presses":1,"releases":1,"pairs":1}],`, "", 1)
		for _, name := range []string{"x_positive", "x_negative", "y_positive", "y_negative"} {
			data = strings.Replace(data, `"`+name+`":1`, `"`+name+`":0`, 1)
		}
		data = strings.Replace(data, `"frames":4`, `"frames":0`, 1)
		if _, err := Decode([]byte(data)); err != nil {
			t.Fatal("old unsupported capability array semantics changed")
		}
	}
}

func TestPointerV2StrictWheelShapeAndVersionBoundaries(t *testing.T) {
	valid := encodePointer(t, validPointer(inputtest.PointerWheelCapability{Axis: "vertical", Legacy: true, HighResolution: true}))
	if _, err := Decode(valid); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func([]byte) []byte{
		"downgrade": func(b []byte) []byte {
			return bytes.Replace(b, []byte(inputtest.PointerSchema), []byte(inputtest.PointerSchemaV1), 1)
		},
		"unknown schema": func(b []byte) []byte { return bytes.Replace(b, []byte("functional/v2"), []byte("functional/v3"), 1) },
		"case schema": func(b []byte) []byte {
			return bytes.Replace(b, []byte(inputtest.PointerSchema), []byte(strings.ToUpper(inputtest.PointerSchema)), 1)
		},
		"missing wheels": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"wheels":[{"axis":"vertical","positive_frames":1,"negative_frames":1}],`), nil, 1)
		},
		"null wheels": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"wheels":[{"axis":"vertical","positive_frames":1,"negative_frames":1}]`), []byte(`"wheels":null`), 1)
		},
		"null axes": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"wheel_axes":[{"axis":"vertical","legacy":true,"high_resolution":true}]`), []byte(`"wheel_axes":null`), 1)
		},
		"case alias": func(b []byte) []byte { return bytes.Replace(b, []byte(`"wheels":`), []byte(`"Wheels":`), 1) },
		"duplicate escaped wheels": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"wheels":`), []byte(`"\u0077heels":[],"wheels":`), 1)
		},
		"unknown field": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"frames":`), []byte(`"raw_events":[],"frames":`), 1)
		},
		"counter null": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"positive_frames":1`), []byte(`"positive_frames":null`), 1)
		},
		"counter bool": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"positive_frames":1`), []byte(`"positive_frames":true`), 1)
		},
		"counter negative": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"positive_frames":1`), []byte(`"positive_frames":-1`), 1)
		},
		"counter overflow": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"positive_frames":1`), []byte(`"positive_frames":18446744073709551616`), 1)
		},
		"trailing":     func(b []byte) []byte { return append(b, []byte(` {}`)...) },
		"invalid utf8": func(b []byte) []byte { return append(b, 0xff) },
	} {
		t.Run(name, func(t *testing.T) {
			raw := edit(bytes.Clone(valid))
			if bytes.Equal(raw, valid) {
				t.Fatal("fixture did not change bytes")
			}
			if _, err := Decode(raw); err == nil {
				t.Fatal("ambiguous or unsupported wire accepted")
			}
		})
	}
	for _, member := range []string{`,"wheels":[]`, `,"wheels":null`, `,"wheel_axes":[]`} {
		old := strings.TrimSuffix(historicalPointer, "}") + member + "}"
		if _, err := Decode([]byte(old)); err == nil {
			t.Fatal("v1 accepted a new wheel field")
		}
	}
	for _, old := range []string{
		strings.Replace(historicalPointer, `"frames":4`, `"wheels":[],"frames":4`, 1),
		strings.Replace(historicalPointer, `"absolute":false`, `"wheel_axes":[],"absolute":false`, 1),
		strings.Replace(historicalPointer, `"absolute":false`, `"\u0077heel_axes":null,"absolute":false`, 1),
	} {
		if _, err := Decode([]byte(old)); err == nil {
			t.Fatal("historical nested shape accepted wheel fields")
		}
	}
}

func TestPointerWheelSemanticsRejectInventedOrMissingCoverage(t *testing.T) {
	for name, edit := range map[string]func(*inputtest.PointerResult){
		"null observed":   func(v *inputtest.PointerResult) { v.Devices[0].Wheels = nil },
		"null capability": func(v *inputtest.PointerResult) { v.Devices[0].Capabilities.WheelAxes = nil },
		"unknown axis":    func(v *inputtest.PointerResult) { v.Devices[0].Capabilities.WheelAxes[0].Axis = "extra" },
		"empty flags":     func(v *inputtest.PointerResult) { v.Devices[0].Capabilities.WheelAxes[0].Legacy = false },
		"missing axis":    func(v *inputtest.PointerResult) { v.Devices[0].Wheels = v.Devices[0].Wheels[:1] },
		"extra axis": func(v *inputtest.PointerResult) {
			v.Devices[0].Wheels = append(v.Devices[0].Wheels, v.Devices[0].Wheels[0])
		},
		"reordered axes": func(v *inputtest.PointerResult) {
			v.Devices[0].Capabilities.WheelAxes[0], v.Devices[0].Capabilities.WheelAxes[1] = v.Devices[0].Capabilities.WheelAxes[1], v.Devices[0].Capabilities.WheelAxes[0]
		},
		"repeated axes": func(v *inputtest.PointerResult) {
			v.Devices[0].Capabilities.WheelAxes[1] = v.Devices[0].Capabilities.WheelAxes[0]
		},
		"wrong observed axis": func(v *inputtest.PointerResult) { v.Devices[0].Wheels[0].Axis = "horizontal" },
		"missing positive":    func(v *inputtest.PointerResult) { v.Devices[0].Wheels[1].PositiveFrames = 0 },
		"missing negative":    func(v *inputtest.PointerResult) { v.Devices[0].Wheels[0].NegativeFrames = 0 },
		"invented frames":     func(v *inputtest.PointerResult) { v.Devices[0].Wheels[0].PositiveFrames = 4 },
		"overflow":            func(v *inputtest.PointerResult) { v.Devices[0].Wheels[0].PositiveFrames = math.MaxUint64 },
		"unsupported observations": func(v *inputtest.PointerResult) {
			v.Status = inputtest.StatusInconclusive
			v.StopReason = "error"
			v.Devices[0].Status = inputtest.StatusInconclusive
			v.Devices[0].Supported = false
			v.Devices[0].Capabilities.Absolute = true
			v.Devices[0].Frames = 0
			v.Devices[0].XPositive = 0
			v.Devices[0].XNegative = 0
			v.Devices[0].YPositive = 0
			v.Devices[0].YNegative = 0
			v.Devices[0].Buttons = nil
		},
		"new schema old limit": func(v *inputtest.PointerResult) { v.LimitMilliseconds = 90001 },
		"lost evidence":        func(v *inputtest.PointerResult) { v.Devices[0].EvidenceLoss = true },
		"unpaired primary":     func(v *inputtest.PointerResult) { v.Devices[0].Buttons[0].Pairs = 0 },
		"missing movement":     func(v *inputtest.PointerResult) { v.Devices[0].YNegative = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			v := validPointer(inputtest.PointerWheelCapability{Axis: "vertical", Legacy: true}, inputtest.PointerWheelCapability{Axis: "horizontal", HighResolution: true})
			edit(&v)
			if ValidateResult(v) == nil {
				t.Fatal("contradictory coverage accepted")
			}
		})
	}
	for _, schema := range []string{inputtest.PointerSchemaV1, inputtest.PointerSchema} {
		v := validPointer()
		v.Schema = schema
		if schema == inputtest.PointerSchemaV1 {
			v.Devices[0].Wheels = nil
			v.Devices[0].Capabilities.WheelAxes = nil
		}
		if ValidateResult(v) != nil {
			t.Fatal("valid no-wheel observation rejected")
		}
	}
}

func TestPointerPartialAndUnknownWheelScopeRemainHonest(t *testing.T) {
	for _, device := range []inputtest.PointerDeviceResult{
		{Interface: "event2", Status: inputtest.StatusInconclusive, Wheels: []inputtest.PointerWheelResult{}},
		{Interface: "event2", Status: inputtest.StatusInconclusive, Readable: true, Identity: &inputtest.PointerIdentity{}, Capabilities: &inputtest.PointerCapabilities{Absolute: true, WheelAxes: []inputtest.PointerWheelCapability{}, Buttons: []string{}}, Wheels: []inputtest.PointerWheelResult{}},
	} {
		v := inputtest.PointerResult{Schema: inputtest.PointerSchema, Status: inputtest.StatusInconclusive, StopReason: "error", LimitMilliseconds: 1000, Devices: []inputtest.PointerDeviceResult{device}}
		if _, err := Decode(encodePointer(t, v)); err != nil {
			t.Fatal("honest unreadable/unsupported result rejected")
		}
	}
	v := validPointer(inputtest.PointerWheelCapability{Axis: "vertical", Legacy: true})
	v.Status = inputtest.StatusError
	v.StopReason = "deadline"
	v.Detail = "deadline"
	if !HasPassedDevice(v) || ValidateResult(v) != nil {
		t.Fatal("aggregate error erased a retained pass")
	}
	v.Devices[0].Status = inputtest.StatusInconclusive
	v.Devices[0].Wheels[0].NegativeFrames = 0
	if HasPassedDevice(v) || ValidateResult(v) != nil {
		t.Fatal("partial wheel direction was promoted or rejected")
	}
}

func TestPointerProducerErrorsRemainValidWithoutDevices(t *testing.T) {
	for _, duration := range []time.Duration{0, time.Millisecond, 91 * time.Second, 10 * time.Millisecond} {
		result := inputtest.RunPointer(t.Context(), inputtest.PointerOptions{Root: t.TempDir(), Duration: duration})
		if result.Status != inputtest.StatusError || len(result.Devices) != 0 || ValidateResult(result) != nil {
			t.Fatal("invalid options or unavailable platform emitted contradictory evidence")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := inputtest.RunPointer(ctx, inputtest.PointerOptions{Root: t.TempDir(), Duration: time.Second})
	if result.Status != inputtest.StatusAborted || ValidateResult(result) != nil {
		t.Fatal("honest pre-cancellation was not reportable")
	}
}
