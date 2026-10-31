package touchpadevidence

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

// Independent wire literal: two credits in each direction, with three controls
// sharing frames. No validator/producer builder supplies these expected facts.
const literalPass = `{"schema":"hhsoft.pc-tester.touchpad-functional/v1","status":"pass","stop_reason":"completed","limit_ms":1000,"duration_ms":100,"cleanup_confirmed":true,"devices":[{"interface":"event4","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":4660,"product":22136,"version":1},"capabilities":{"protocol_version":65537,"events":[0,1,3,4],"keys":[272,273,274,325,330],"relative":[],"properties":[0,2],"misc":[4,5],"axes":[{"code":0,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1},{"code":1,"minimum":-500,"maximum":500,"fuzz":0,"flat":0,"resolution":1}]},"x_threshold":50,"y_threshold":50,"frames":6,"contact_starts":1,"contact_releases":1,"xy_baselines":1,"qualifying_contacts":1,"x_positive":2,"x_negative":2,"y_positive":2,"y_negative":2,"buttons":[{"button":"left","presses":1,"releases":1,"pairs":1},{"button":"right","presses":1,"releases":1,"pairs":1},{"button":"middle","presses":1,"releases":1,"pairs":1}],"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]}`

func passFixture(t *testing.T) inputtest.TouchpadResult {
	t.Helper()
	var result inputtest.TouchpadResult
	if err := json.Unmarshal([]byte(literalPass), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func wire(t *testing.T, result inputtest.TouchpadResult) []byte {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func emptyState(d *inputtest.TouchpadDeviceResult) {
	d.Status, d.Detail = inputtest.StatusInconclusive, "fresh_xy_missing"
	d.Frames, d.ContactStarts, d.ContactReleases, d.XYBaselines, d.QualifyingContacts = 0, 0, 0, 0, 0
	d.XPositive, d.XNegative, d.YPositive, d.YNegative = 0, 0, 0, 0
	d.FinalNeutral = false
	for i := range d.Buttons {
		d.Buttons[i].Presses, d.Buttons[i].Releases, d.Buttons[i].Pairs = 0, 0, 0
	}
}

func diagnosticFixture(t *testing.T) inputtest.TouchpadResult {
	t.Helper()
	result := passFixture(t)
	result.Status, result.StopReason, result.Detail = inputtest.StatusInconclusive, "observation_timeout", "observation_timeout"
	emptyState(&result.Devices[0])
	return result
}

func preState(d *inputtest.TouchpadDeviceResult) {
	emptyState(d)
	d.Detail, d.XThreshold, d.YThreshold = "incomplete_actions", 0, 0
	d.Buttons = []inputtest.PointerButtonResult{}
	d.BaselineNeutral = false
}

func unreadableFixture(t *testing.T) inputtest.TouchpadResult {
	t.Helper()
	result := diagnosticFixture(t)
	d := &result.Devices[0]
	preState(d)
	d.Readable, d.Supported, d.Identity, d.Capabilities = false, false, nil, nil
	d.Detail = "identity_unavailable"
	result.StopReason, result.Detail = "error", "unsupported_or_unreadable_device"
	return result
}

func TestTouchpadEvidenceLiteralAndOwnership(t *testing.T) {
	t.Run("literal_cross_control_counts", func(t *testing.T) {
		got, err := Decode([]byte(literalPass))
		if err != nil || !reflect.DeepEqual(got, passFixture(t)) {
			t.Fatalf("literal decode: %v", err)
		}
		// X/Y together exceed Frames, as do contact plus all button transitions.
		if got.Devices[0].XPositive+got.Devices[0].XNegative+got.Devices[0].YPositive+got.Devices[0].YNegative <= got.Devices[0].Frames {
			t.Fatal("fixture lost independent cross-axis witness")
		}
	})
	t.Run("decoded_storage_owned", func(t *testing.T) {
		raw := []byte(literalPass)
		first, err := Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		for i := range raw {
			raw[i] = 'x'
		}
		second, err := Decode([]byte(literalPass))
		if err != nil {
			t.Fatal(err)
		}
		first.Devices[0].Capabilities.Keys[0] = 0
		first.Devices[0].Identity.Bus = 0
		first.Devices[0].Buttons[0].Pairs = 0
		if !reflect.DeepEqual(second, passFixture(t)) {
			t.Fatal("decoded alias")
		}
	})
	t.Run("unused_optional_buttons", func(t *testing.T) {
		result := passFixture(t)
		for i := 1; i < 3; i++ {
			result.Devices[0].Buttons[i] = inputtest.PointerButtonResult{Button: []string{"left", "right", "middle"}[i]}
		}
		if err := ValidateResult(result); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("lexicographic_interfaces", func(t *testing.T) {
		result := passFixture(t)
		result.Devices = append(result.Devices, passFixture(t).Devices[0])
		result.Devices[0].Interface, result.Devices[1].Interface = "event10", "event2"
		if err := ValidateResult(result); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTouchpadEvidenceRejectsJSONAmbiguity(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		{"empty", nil},
		{"top_null", []byte("null")},
		{"top_array", []byte("[]")},
		{"malformed", []byte("{")},
		{"invalid_utf8", append([]byte(literalPass), 0xff)},
		{"trailing_value", []byte(literalPass + " {}")},
		{"duplicate_schema", []byte(strings.Replace(literalPass, `"schema":`, `"schema":"hhsoft.pc-tester.touchpad-functional/v1","schema":`, 1))},
		{"case_alias", []byte(strings.Replace(literalPass, `"status":`, `"Status":`, 1))},
		{"unknown_member", []byte(strings.Replace(literalPass, `"status":`, `"raw_path":"/synthetic","status":`, 1))},
		{"duplicate_nested", []byte(strings.Replace(literalPass, `"bus":3`, `"bus":3,"bus":3`, 1))},
		{"unknown_axis_value", []byte(strings.Replace(literalPass, `"code":0`, `"code":0,"value":100`, 1))},
		{"missing_boolean", []byte(strings.Replace(literalPass, `"cleanup_confirmed":true,`, "", 1))},
		{"missing_counter", []byte(strings.Replace(literalPass, `"frames":6,`, "", 1))},
		{"missing_capability_array", []byte(strings.Replace(literalPass, `"relative":[],`, "", 1))},
		{"null_devices", nullMember(t, "devices")},
		{"null_buttons", nullMember(t, "buttons")},
		{"null_relative", []byte(strings.Replace(literalPass, `"relative":[]`, `"relative":null`, 1))},
		{"null_identity", nullMember(t, "identity")},
		{"null_capabilities", nullMember(t, "capabilities")},
		{"numeric_boolean", []byte(strings.Replace(literalPass, `"supported":true`, `"supported":1`, 1))},
		{"string_integer", []byte(strings.Replace(literalPass, `"frames":6`, `"frames":"6"`, 1))},
		{"fraction_integer", []byte(strings.Replace(literalPass, `"frames":6`, `"frames":6.0`, 1))},
		{"exponent_integer", []byte(strings.Replace(literalPass, `"frames":6`, `"frames":6e0`, 1))},
		{"negative_unsigned", []byte(strings.Replace(literalPass, `"frames":6`, `"frames":-1`, 1))},
		{"uint64_overflow", []byte(strings.Replace(literalPass, `"frames":6`, `"frames":18446744073709551616`, 1))},
		{"int32_overflow", []byte(strings.Replace(literalPass, `"minimum":0`, `"minimum":2147483648`, 1))},
		{"uint16_overflow", []byte(strings.Replace(literalPass, `"bus":3`, `"bus":65536`, 1))},
		{"deep_nesting", []byte(strings.Repeat("[", 34) + strings.Repeat("]", 34))},
		{"byte_limit", []byte(strings.Repeat(" ", 1<<20) + literalPass)},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			got, err := Decode(item.raw)
			if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(got, inputtest.TouchpadResult{}) {
				t.Fatalf("accepted ambiguous JSON: %v", err)
			}
		})
	}
}

func TestTouchpadEvidenceRejectsContradictoryClaims(t *testing.T) {
	cases := []struct {
		name   string
		change func(*inputtest.TouchpadResult)
	}{
		{"wrong_schema", func(r *inputtest.TouchpadResult) { r.Schema += "/v2" }},
		{"unknown_status", func(r *inputtest.TouchpadResult) { r.Status = "failed" }},
		{"pass_wrong_reason", func(r *inputtest.TouchpadResult) { r.StopReason = "error" }},
		{"pass_detail", func(r *inputtest.TouchpadResult) { r.Detail = "incomplete_actions" }},
		{"pass_empty", func(r *inputtest.TouchpadResult) { r.Devices = []inputtest.TouchpadDeviceResult{} }},
		{"nil_devices", func(r *inputtest.TouchpadResult) { r.Devices = nil }},
		{"too_many_devices", func(r *inputtest.TouchpadResult) { r.Devices = make([]inputtest.TouchpadDeviceResult, 129) }},
		{"zero_limit", func(r *inputtest.TouchpadResult) { r.LimitMilliseconds = 0 }},
		{"short_limit", func(r *inputtest.TouchpadResult) { r.LimitMilliseconds = 9 }},
		{"long_limit", func(r *inputtest.TouchpadResult) { r.LimitMilliseconds = 90001 }},
		{"negative_duration", func(r *inputtest.TouchpadResult) { r.DurationMilliseconds = -1 }},
		{"duration_wire_limit", func(r *inputtest.TouchpadResult) { r.DurationMilliseconds = 3600001 }},
		{"late_pass", func(r *inputtest.TouchpadResult) { r.DurationMilliseconds = 1001 }},
		{"leading_zero_interface", func(r *inputtest.TouchpadResult) { r.Devices[0].Interface = "event04" }},
		{"interface_overflow", func(r *inputtest.TouchpadResult) { r.Devices[0].Interface = "event4294967296" }},
		{"interface_path", func(r *inputtest.TouchpadResult) { r.Devices[0].Interface = "../event4" }},
		{"duplicate_interface", func(r *inputtest.TouchpadResult) { r.Devices = append(r.Devices, r.Devices[0]) }},
		{"row_aborted", func(r *inputtest.TouchpadResult) { r.Devices[0].Status = inputtest.StatusAborted }},
		{"row_not_pass", func(r *inputtest.TouchpadResult) { r.Devices[0].Status = inputtest.StatusInconclusive }},
		{"row_free_text", func(r *inputtest.TouchpadResult) { r.Devices[0].Detail = "synthetic arbitrary text" }},
		{"identity_missing", func(r *inputtest.TouchpadResult) { r.Devices[0].Identity = nil }},
		{"capabilities_missing", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities = nil }},
		{"support_pruned", func(r *inputtest.TouchpadResult) {
			r.Devices[0].Capabilities.Events = append(r.Devices[0].Capabilities.Events, 5)
		}},
		{"support_false", func(r *inputtest.TouchpadResult) { r.Devices[0].Supported = false }},
		{"readable_false", func(r *inputtest.TouchpadResult) { r.Devices[0].Readable = false }},
		{"protocol_mismatch", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.ProtocolVersion = 65538 }},
		{"nil_events", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Events = nil }},
		{"nil_keys", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Keys = nil }},
		{"nil_relative", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Relative = nil }},
		{"nil_properties", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Properties = nil }},
		{"nil_misc", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Misc = nil }},
		{"nil_axes", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Axes = nil }},
		{"duplicate_event", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Events = []uint16{0, 1, 1, 3, 4} }},
		{"unsorted_key", func(r *inputtest.TouchpadResult) {
			r.Devices[0].Capabilities.Keys[0], r.Devices[0].Capabilities.Keys[1] = 273, 272
		}},
		{"unknown_property", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Properties = []uint16{0, 1, 2} }},
		{"relative_advertisement", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Relative = []uint16{0} }},
		{"misc_event_mismatch", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Misc = []uint16{} }},
		{"unknown_axis_measured", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Axes[1].Code = 2 }},
		{"bad_axis_range", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Axes[0].Maximum = 0 }},
		{"negative_fuzz", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Axes[0].Fuzz = -1 }},
		{"unattainable_threshold", func(r *inputtest.TouchpadResult) { r.Devices[0].Capabilities.Axes[0].Fuzz = 1000 }},
		{"threshold_forged", func(r *inputtest.TouchpadResult) { r.Devices[0].XThreshold++ }},
		{"zero_threshold_with_actions", func(r *inputtest.TouchpadResult) { r.Devices[0].XThreshold = 0 }},
		{"no_baseline_actions", func(r *inputtest.TouchpadResult) { r.Devices[0].BaselineNeutral = false }},
		{"no_terminal_proof", func(r *inputtest.TouchpadResult) { r.Devices[0].FinalNeutral = false }},
		{"loss_pass", func(r *inputtest.TouchpadResult) { r.Devices[0].EvidenceLoss = true }},
		{"cleanup_mismatch", func(r *inputtest.TouchpadResult) { r.Devices[0].CleanupConfirmed = false }},
		{"frame_limit", func(r *inputtest.TouchpadResult) { r.Devices[0].Frames = 1048577 }},
		{"contact_over_frames", func(r *inputtest.TouchpadResult) { r.Devices[0].ContactStarts = 7 }},
		{"release_without_contact", func(r *inputtest.TouchpadResult) { r.Devices[0].ContactReleases = 2 }},
		{"multiple_open_contacts", func(r *inputtest.TouchpadResult) { r.Devices[0].ContactStarts = 3 }},
		{"baseline_without_contact", func(r *inputtest.TouchpadResult) { r.Devices[0].XYBaselines = 2 }},
		{"qualification_without_release", func(r *inputtest.TouchpadResult) { r.Devices[0].QualifyingContacts = 2 }},
		{"qualification_without_xpositive", func(r *inputtest.TouchpadResult) { r.Devices[0].XPositive = 0 }},
		{"qualification_without_xnegative", func(r *inputtest.TouchpadResult) { r.Devices[0].XNegative = 0 }},
		{"qualification_without_ypositive", func(r *inputtest.TouchpadResult) { r.Devices[0].YPositive = 0 }},
		{"qualification_without_ynegative", func(r *inputtest.TouchpadResult) { r.Devices[0].YNegative = 0 }},
		{"axis_sum_over_frames", func(r *inputtest.TouchpadResult) { r.Devices[0].XPositive = 5 }},
		{"count_uint64_wrap", func(r *inputtest.TouchpadResult) { r.Devices[0].XPositive = math.MaxUint64 }},
		{"nil_buttons", func(r *inputtest.TouchpadResult) { r.Devices[0].Buttons = nil }},
		{"missing_optional_button_row", func(r *inputtest.TouchpadResult) { r.Devices[0].Buttons = r.Devices[0].Buttons[:2] }},
		{"duplicate_button", func(r *inputtest.TouchpadResult) { r.Devices[0].Buttons[1].Button = "left" }},
		{"button_order", func(r *inputtest.TouchpadResult) {
			r.Devices[0].Buttons[0], r.Devices[0].Buttons[1] = r.Devices[0].Buttons[1], r.Devices[0].Buttons[0]
		}},
		{"pairs_mismatch", func(r *inputtest.TouchpadResult) { r.Devices[0].Buttons[0].Pairs = 0 }},
		{"button_release_without_press", func(r *inputtest.TouchpadResult) { r.Devices[0].Buttons[0].Presses = 0 }},
		{"multiple_open_button", func(r *inputtest.TouchpadResult) { r.Devices[0].Buttons[0].Presses = 3 }},
		{"per_button_frame_sum", func(r *inputtest.TouchpadResult) {
			r.Devices[0].Buttons[0].Presses = 4
			r.Devices[0].Buttons[0].Releases = 4
			r.Devices[0].Buttons[0].Pairs = 4
		}},
		{"primary_unexercised", func(r *inputtest.TouchpadResult) {
			r.Devices[0].Buttons[0] = inputtest.PointerButtonResult{Button: "left"}
		}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			result := passFixture(t)
			item.change(&result)
			if !errors.Is(ValidateResult(result), ErrInvalid) {
				t.Fatal("contradiction accepted")
			}
			got, err := Decode(wire(t, result))
			if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(got, inputtest.TouchpadResult{}) {
				t.Fatal("invalid partial observation")
			}
		})
	}
}

func nullMember(t *testing.T, member string) []byte {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(literalPass), &top); err != nil {
		t.Fatal(err)
	}
	if member == "devices" {
		top[member] = json.RawMessage("null")
	} else {
		var rows []map[string]json.RawMessage
		if err := json.Unmarshal(top["devices"], &rows); err != nil {
			t.Fatal(err)
		}
		rows[0][member] = json.RawMessage("null")
		raw, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		top["devices"] = raw
	}
	raw, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTouchpadEvidencePreservesProducerDiagnostics(t *testing.T) {
	cases := []struct {
		name string
		make func(*testing.T) inputtest.TouchpadResult
	}{
		{"unreadable", unreadableFixture},
		{"unsupported_projection_omitted", func(t *testing.T) inputtest.TouchpadResult {
			r := unreadableFixture(t)
			d := &r.Devices[0]
			d.Readable = true
			d.Identity = &inputtest.TouchpadIdentity{}
			d.Detail = "unsupported_capabilities"
			return r
		}},
		{"unknown_axis_marker", func(t *testing.T) inputtest.TouchpadResult {
			r := diagnosticFixture(t)
			d := &r.Devices[0]
			preState(d)
			d.Supported = false
			d.Detail = "unsupported_capabilities"
			d.Capabilities.Axes = append(d.Capabilities.Axes, inputtest.TouchpadAxis{Code: 63})
			return r
		}},
		{"unsupported_known_range", func(t *testing.T) inputtest.TouchpadResult {
			r := diagnosticFixture(t)
			d := &r.Devices[0]
			preState(d)
			d.Supported = false
			d.Detail = "unsupported_capabilities"
			d.Capabilities.Axes[0].Maximum = 0
			return r
		}},
		{"unsupported_protocol_empty_projection", func(t *testing.T) inputtest.TouchpadResult {
			r := diagnosticFixture(t)
			d := &r.Devices[0]
			preState(d)
			d.Supported = false
			d.Detail = "unsupported_capabilities"
			d.Capabilities = &inputtest.TouchpadCapabilities{ProtocolVersion: -1, Events: []uint16{}, Keys: []uint16{}, Relative: []uint16{}, Properties: []uint16{}, Misc: []uint16{}, Axes: []inputtest.TouchpadAxis{}}
			return r
		}},
		{"supported_before_state", func(t *testing.T) inputtest.TouchpadResult {
			r := diagnosticFixture(t)
			preState(&r.Devices[0])
			r.Status = inputtest.StatusError
			r.StopReason = "error"
			r.Detail = "unsupported_record_layout"
			return r
		}},
		{"initialized_before_baseline", func(t *testing.T) inputtest.TouchpadResult {
			r := diagnosticFixture(t)
			d := &r.Devices[0]
			d.BaselineNeutral = false
			d.Detail = "baseline_unavailable"
			return r
		}},
		{"baseline_event_loss", func(t *testing.T) inputtest.TouchpadResult {
			r := diagnosticFixture(t)
			d := &r.Devices[0]
			d.BaselineNeutral = false
			d.EvidenceLoss = true
			d.Status = inputtest.StatusError
			d.Detail = "event_loss"
			r.Status = inputtest.StatusError
			r.StopReason = "error"
			r.Detail = "event_loss"
			return r
		}},
		{"active_identity_error", func(t *testing.T) inputtest.TouchpadResult {
			r := diagnosticFixture(t)
			d := &r.Devices[0]
			d.Status = inputtest.StatusError
			d.Detail = "identity_changed"
			r.Status = inputtest.StatusError
			r.StopReason = "error"
			r.Detail = "identity_changed"
			return r
		}},
		{"terminal_identity_inconclusive", func(t *testing.T) inputtest.TouchpadResult {
			r := passFixture(t)
			d := &r.Devices[0]
			d.Status = inputtest.StatusInconclusive
			d.Detail = "identity_changed"
			d.FinalNeutral = false
			r.Status = inputtest.StatusError
			r.StopReason = "error"
			r.Detail = "identity_changed"
			return r
		}},
		{"terminal_malformed_inconclusive", func(t *testing.T) inputtest.TouchpadResult {
			r := passFixture(t)
			d := &r.Devices[0]
			d.Status = inputtest.StatusInconclusive
			d.Detail = "malformed_events"
			d.FinalNeutral = false
			r.Status = inputtest.StatusError
			r.StopReason = "error"
			r.Detail = "malformed_events"
			return r
		}},
		{"pending_release_no_public_directions", func(t *testing.T) inputtest.TouchpadResult {
			r := diagnosticFixture(t)
			d := &r.Devices[0]
			d.Frames = 4
			d.ContactStarts = 1
			d.XYBaselines = 1
			d.Detail = "release_required"
			return r
		}},
		{"complete_before_cancelled_candidate", func(t *testing.T) inputtest.TouchpadResult {
			r := passFixture(t)
			d := &r.Devices[0]
			d.Status = inputtest.StatusInconclusive
			d.FinalNeutral = false
			r.Status = inputtest.StatusAborted
			r.StopReason = "operator_cancelled"
			r.Detail = "operator_cancelled"
			return r
		}},
		{"retained_pass_after_cancel", func(t *testing.T) inputtest.TouchpadResult {
			r := passFixture(t)
			r.Status = inputtest.StatusAborted
			r.StopReason = "operator_cancelled"
			r.Detail = "operator_cancelled"
			return r
		}},
		{"retained_pass_after_deadline", func(t *testing.T) inputtest.TouchpadResult {
			r := passFixture(t)
			r.Status = inputtest.StatusError
			r.StopReason = "deadline"
			r.Detail = "deadline"
			r.DurationMilliseconds = 1001
			return r
		}},
		{"retained_pass_after_timeout", func(t *testing.T) inputtest.TouchpadResult {
			r := passFixture(t)
			r.Status = inputtest.StatusInconclusive
			r.StopReason = "observation_timeout"
			r.Detail = "observation_timeout"
			return r
		}},
		{"no_touchpads", func(t *testing.T) inputtest.TouchpadResult {
			r := diagnosticFixture(t)
			r.Devices = []inputtest.TouchpadDeviceResult{}
			r.StopReason = "no_touchpads"
			r.Detail = "no_touchpads"
			return r
		}},
		{"unsupported_platform", func(t *testing.T) inputtest.TouchpadResult {
			r := diagnosticFixture(t)
			r.Devices = []inputtest.TouchpadDeviceResult{}
			r.StopReason = "unsupported_platform"
			r.Detail = "unsupported_platform"
			return r
		}},
		{"invalid_options", func(t *testing.T) inputtest.TouchpadResult {
			r := diagnosticFixture(t)
			r.Devices = []inputtest.TouchpadDeviceResult{}
			r.Status = inputtest.StatusError
			r.StopReason = "error"
			r.Detail = "invalid_options"
			r.LimitMilliseconds = 0
			r.DurationMilliseconds = 0
			return r
		}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			want := item.make(t)
			got, err := Decode(wire(t, want))
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("lost diagnostic: %v", err)
			}
		})
	}
}

func TestTouchpadEvidenceCleanupPrecedence(t *testing.T) {
	cases := []struct {
		name string
		make func(*testing.T) inputtest.TouchpadResult
	}{
		{"no_owner_inconclusive", unreadableFixture},
		{"no_owner_before_open_completed", func(t *testing.T) inputtest.TouchpadResult {
			r := unreadableFixture(t)
			r.Devices[0].Detail = "incomplete_actions"
			return r
		}},
		{"close_after_terminal_proof", passFixture},
		{"discovery_cleanup_before_rows", func(t *testing.T) inputtest.TouchpadResult {
			r := diagnosticFixture(t)
			r.Devices = []inputtest.TouchpadDeviceResult{}
			return r
		}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			r := item.make(t)
			r.Status, r.StopReason, r.Detail, r.CleanupConfirmed = inputtest.StatusError, "cleanup_unconfirmed", "cleanup_unconfirmed", false
			if len(r.Devices) > 0 {
				r.Devices[0].CleanupConfirmed = false
				if r.Devices[0].Readable {
					r.Devices[0].Status, r.Devices[0].Detail = inputtest.StatusError, "cleanup_unconfirmed"
				}
			}
			got, err := Decode(wire(t, r))
			if !errors.Is(err, ErrCleanupUnconfirmed) || !reflect.DeepEqual(got, r) {
				t.Fatalf("uncertainty not retained: %v", err)
			}
			if !errors.Is(ValidateInventoryBinding(r, nil), ErrCleanupUnconfirmed) {
				t.Fatal("binding suppressed cleanup")
			}
			r.Devices = append(r.Devices, inputtest.TouchpadDeviceResult{Interface: "../event0"})
			got, err = Decode(wire(t, r))
			if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(got, inputtest.TouchpadResult{}) {
				t.Fatal("cleanup masked invalid evidence")
			}
		})
	}
}

func TestTouchpadEvidenceDiagnosticContradictions(t *testing.T) {
	cases := []struct {
		name   string
		change func(*inputtest.TouchpadResult)
	}{
		{"aborted_wrong_detail", func(r *inputtest.TouchpadResult) {
			r.Status = inputtest.StatusAborted
			r.StopReason = "operator_cancelled"
			r.Detail = "deadline"
		}},
		{"error_progress_detail", func(r *inputtest.TouchpadResult) {
			r.Status = inputtest.StatusError
			r.StopReason = "error"
			r.Detail = "fresh_xy_missing"
		}},
		{"no_touchpads_with_row", func(r *inputtest.TouchpadResult) { r.StopReason = "no_touchpads"; r.Detail = "no_touchpads" }},
		{"unsupported_platform_with_row", func(r *inputtest.TouchpadResult) {
			r.StopReason = "unsupported_platform"
			r.Detail = "unsupported_platform"
		}},
		{"cleanup_without_uncertainty", func(r *inputtest.TouchpadResult) {
			r.Status = inputtest.StatusError
			r.StopReason = "cleanup_unconfirmed"
			r.Detail = "cleanup_unconfirmed"
		}},
		{"uncertainty_wrong_aggregate", func(r *inputtest.TouchpadResult) { r.CleanupConfirmed = false }},
		{"empty_incomplete_detail", func(r *inputtest.TouchpadResult) { r.Devices[0].Detail = "" }},
		{"fresh_xy_after_baseline", func(r *inputtest.TouchpadResult) {
			r.Devices[0].Frames = 1
			r.Devices[0].ContactStarts = 1
			r.Devices[0].XYBaselines = 1
		}},
		{"release_without_open_contact", func(r *inputtest.TouchpadResult) { r.Devices[0].Detail = "release_required" }},
		{"primary_without_qualifying", func(r *inputtest.TouchpadResult) { r.Devices[0].Detail = "primary_button_incomplete" }},
		{"controls_without_qualifying", func(r *inputtest.TouchpadResult) { r.Devices[0].Detail = "controls_not_neutral" }},
		{"row_aggregate_only_detail", func(r *inputtest.TouchpadResult) { r.Devices[0].Detail = "operator_cancelled" }},
		{"event_domain", func(r *inputtest.TouchpadResult) {
			preState(&r.Devices[0])
			r.Devices[0].Supported = false
			r.Devices[0].Detail = "unsupported_capabilities"
			r.Devices[0].Capabilities.Events = []uint16{64}
		}},
		{"key_domain", func(r *inputtest.TouchpadResult) {
			preState(&r.Devices[0])
			r.Devices[0].Supported = false
			r.Devices[0].Detail = "unsupported_capabilities"
			r.Devices[0].Capabilities.Keys = []uint16{768}
		}},
		{"unknown_axis_configuration", func(r *inputtest.TouchpadResult) {
			preState(&r.Devices[0])
			r.Devices[0].Supported = false
			r.Devices[0].Detail = "unsupported_capabilities"
			r.Devices[0].Capabilities.Axes = append(r.Devices[0].Capabilities.Axes, inputtest.TouchpadAxis{Code: 63, Resolution: 1})
		}},
		{"too_many_misc", func(r *inputtest.TouchpadResult) {
			preState(&r.Devices[0])
			r.Devices[0].Supported = false
			r.Devices[0].Detail = "unsupported_capabilities"
			r.Devices[0].Capabilities.Misc = []uint16{4, 5, 6}
		}},
		{"prestate_loss_claim", func(r *inputtest.TouchpadResult) { preState(&r.Devices[0]); r.Devices[0].EvidenceLoss = true }},
		{"partial_prestate_threshold", func(r *inputtest.TouchpadResult) { preState(&r.Devices[0]); r.Devices[0].XThreshold = 50 }},
		{"baseline_false_with_frame", func(r *inputtest.TouchpadResult) {
			r.Devices[0].BaselineNeutral = false
			r.Devices[0].Detail = "baseline_unavailable"
			r.Devices[0].Frames = 1
		}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			r := diagnosticFixture(t)
			item.change(&r)
			if !errors.Is(ValidateResult(r), ErrInvalid) {
				t.Fatal("invalid diagnostic accepted")
			}
		})
	}
	t.Run("terminal_neutrality_contradicts_revocation", func(t *testing.T) {
		r := passFixture(t)
		r.Status = inputtest.StatusError
		r.StopReason = "error"
		r.Detail = "terminal_not_neutral"
		r.Devices[0].Status = inputtest.StatusInconclusive
		r.Devices[0].Detail = "terminal_not_neutral"
		if !errors.Is(ValidateResult(r), ErrInvalid) {
			t.Fatal("contradictory terminal claim")
		}
	})
}

func TestTouchpadEvidencePortableArithmetic(t *testing.T) {
	t.Run("signed32_full_span", func(t *testing.T) {
		r := passFixture(t)
		for i := range r.Devices[0].Capabilities.Axes {
			r.Devices[0].Capabilities.Axes[i].Minimum = math.MinInt32
			r.Devices[0].Capabilities.Axes[i].Maximum = math.MaxInt32
		}
		r.Devices[0].XThreshold, r.Devices[0].YThreshold = 214748365, 214748365
		if err := ValidateResult(r); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("noise_threshold_literal", func(t *testing.T) {
		r := passFixture(t)
		r.Devices[0].Capabilities.Axes[0].Fuzz = 13
		r.Devices[0].Capabilities.Axes[0].Flat = 27
		r.Devices[0].XThreshold = 55
		if err := ValidateResult(r); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("optional_axis_validated", func(t *testing.T) {
		r := passFixture(t)
		r.Devices[0].Capabilities.Axes = append(r.Devices[0].Capabilities.Axes, inputtest.TouchpadAxis{Code: 24, Maximum: 255})
		if err := ValidateResult(r); err != nil {
			t.Fatal(err)
		}
		r.Devices[0].Capabilities.Axes[2].Flat = 256
		if !errors.Is(ValidateResult(r), ErrInvalid) {
			t.Fatal("invalid optional range")
		}
	})
	t.Run("total_frames_exact_and_over", func(t *testing.T) {
		r := passFixture(t)
		r.Devices = nil
		for i := 0; i < 8; i++ {
			d := passFixture(t).Devices[0]
			d.Interface = "event" + string(rune('0'+i))
			d.Frames = 1048576
			r.Devices = append(r.Devices, d)
		}
		if err := ValidateResult(r); err != nil {
			t.Fatal(err)
		}
		d := passFixture(t).Devices[0]
		d.Interface = "event8"
		r.Devices = append(r.Devices, d)
		if !errors.Is(ValidateResult(r), ErrInvalid) {
			t.Fatal("aggregate record budget")
		}
	})
}

func TestTouchpadEvidenceContactFrameAccounting(t *testing.T) {
	t.Run("eight_transitions_cannot_fit_six_frames", func(t *testing.T) {
		r := passFixture(t)
		r.Devices[0].ContactStarts, r.Devices[0].ContactReleases = 4, 4
		if r.Devices[0].Frames != 6 || r.Devices[0].ContactStarts+r.Devices[0].ContactReleases != 8 {
			t.Fatal("literal same-control witness changed")
		}
		if !errors.Is(ValidateResult(r), ErrInvalid) {
			t.Fatal("same contact transitioned more than once per frame")
		}
		got, err := Decode(wire(t, r))
		if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(got, inputtest.TouchpadResult{}) {
			t.Fatal("invalid contact observation escaped Decode")
		}
	})
	t.Run("closed_contact_exact_boundary", func(t *testing.T) {
		r := diagnosticFixture(t)
		d := &r.Devices[0]
		d.Frames, d.ContactStarts, d.ContactReleases, d.XYBaselines = 6, 3, 3, 3
		d.Detail = "contact_incomplete"
		if err := ValidateResult(r); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("open_contact_exact_boundary", func(t *testing.T) {
		r := diagnosticFixture(t)
		d := &r.Devices[0]
		d.Frames, d.ContactStarts, d.ContactReleases, d.XYBaselines = 5, 3, 2, 3
		d.Detail = "contact_incomplete"
		if err := ValidateResult(r); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("different_controls_share_frames", func(t *testing.T) {
		r := passFixture(t)
		d := r.Devices[0]
		transitions := d.ContactStarts + d.ContactReleases
		for _, button := range d.Buttons {
			transitions += button.Presses + button.Releases
		}
		if transitions != 8 || d.Frames != 6 {
			t.Fatal("literal cross-control fixture changed")
		}
		if err := ValidateResult(r); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("starts_over_frames_short_circuit", func(t *testing.T) {
		r := passFixture(t)
		r.Devices[0].ContactStarts = 7
		if !errors.Is(ValidateResult(r), ErrInvalid) {
			t.Fatal("subtraction underflow admitted a contact")
		}
	})
}

func TestTouchpadEvidenceMotionFrameAccounting(t *testing.T) {
	t.Run("x_motion_cannot_use_start_and_release_frames", func(t *testing.T) {
		r := passFixture(t)
		r.Devices[0].XPositive, r.Devices[0].XNegative = 3, 3
		if !errors.Is(ValidateResult(r), ErrInvalid) {
			t.Fatal("six X-motion frames escaped a six-frame contact")
		}
		got, err := Decode(wire(t, r))
		if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(got, inputtest.TouchpadResult{}) {
			t.Fatal("invalid X budget escaped Decode")
		}
	})
	t.Run("y_motion_cannot_use_start_and_release_frames", func(t *testing.T) {
		r := passFixture(t)
		r.Devices[0].YPositive, r.Devices[0].YNegative = 3, 3
		if !errors.Is(ValidateResult(r), ErrInvalid) {
			t.Fatal("six Y-motion frames escaped a six-frame contact")
		}
	})
	t.Run("positive_exceeds_remaining_before_subtraction", func(t *testing.T) {
		r := passFixture(t)
		r.Devices[0].XPositive, r.Devices[0].XNegative = 5, 1
		if !errors.Is(ValidateResult(r), ErrInvalid) {
			t.Fatal("positive motion exceeded the four remaining frames")
		}
	})
	t.Run("unanchored_starts_still_consume_frames", func(t *testing.T) {
		r := passFixture(t)
		r.Status, r.StopReason, r.Detail = inputtest.StatusInconclusive, "observation_timeout", "observation_timeout"
		d := &r.Devices[0]
		d.Status, d.Detail, d.FinalNeutral = inputtest.StatusInconclusive, "controls_not_neutral", false
		d.Frames, d.ContactStarts, d.ContactReleases, d.XYBaselines = 8, 3, 2, 1
		d.YPositive, d.YNegative = 1, 1
		if !errors.Is(ValidateResult(r), ErrInvalid) {
			t.Fatal("four X-motion frames escaped three remaining frames")
		}
	})
	t.Run("public_motion_requires_release", func(t *testing.T) {
		for direction := 0; direction < 4; direction++ {
			r := diagnosticFixture(t)
			d := &r.Devices[0]
			d.Frames, d.ContactStarts, d.XYBaselines = 4, 1, 1
			counters := []*uint64{&d.XPositive, &d.XNegative, &d.YPositive, &d.YNegative}
			*counters[direction] = 1
			d.Detail = "release_required"
			if !errors.Is(ValidateResult(r), ErrInvalid) {
				t.Fatalf("unreleased direction %d escaped into public totals", direction)
			}
		}
	})
	t.Run("public_motion_requires_baseline", func(t *testing.T) {
		for direction := 0; direction < 4; direction++ {
			r := diagnosticFixture(t)
			d := &r.Devices[0]
			d.Frames, d.ContactStarts, d.ContactReleases = 4, 1, 1
			counters := []*uint64{&d.XPositive, &d.XNegative, &d.YPositive, &d.YNegative}
			*counters[direction] = 1
			d.Detail = "fresh_xy_missing"
			if !errors.Is(ValidateResult(r), ErrInvalid) {
				t.Fatalf("unanchored direction %d escaped into public totals", direction)
			}
		}
	})
	t.Run("exact_budget_keeps_cross_axis_and_button_overlap", func(t *testing.T) {
		r := passFixture(t)
		d := r.Devices[0]
		if d.Frames != 6 || d.ContactStarts != 1 || d.ContactReleases != 1 ||
			d.XPositive+d.XNegative != 4 || d.YPositive+d.YNegative != 4 ||
			d.XPositive+d.XNegative+d.YPositive+d.YNegative != 8 {
			t.Fatal("literal per-axis boundary changed")
		}
		if err := ValidateResult(r); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("closed_contacts_leave_zero_motion_budget", func(t *testing.T) {
		r := diagnosticFixture(t)
		d := &r.Devices[0]
		d.Frames, d.ContactStarts, d.ContactReleases, d.XYBaselines = 6, 3, 3, 3
		d.Detail = "contact_incomplete"
		if err := ValidateResult(r); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("open_contact_retains_earlier_released_motion", func(t *testing.T) {
		r := passFixture(t)
		r.Status, r.StopReason, r.Detail = inputtest.StatusInconclusive, "observation_timeout", "observation_timeout"
		d := &r.Devices[0]
		d.Status, d.Detail, d.FinalNeutral = inputtest.StatusInconclusive, "controls_not_neutral", false
		d.Frames, d.ContactStarts, d.ContactReleases, d.XYBaselines = 7, 2, 1, 1
		if err := ValidateResult(r); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("active_unreleased_motion_stays_private", func(t *testing.T) {
		r := diagnosticFixture(t)
		d := &r.Devices[0]
		d.Frames, d.ContactStarts, d.XYBaselines = 4, 1, 1
		d.Detail = "release_required"
		if err := ValidateResult(r); err != nil {
			t.Fatal(err)
		}
	})
}
