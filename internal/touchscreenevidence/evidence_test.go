package touchscreenevidence

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

// Literal independent wire: the two axes share four movement frames.
// No producer or validator constructor supplies the expected observations.
const literalPass = `{"schema":"hhsoft.pc-tester.touchscreen-functional/v1","status":"pass","stop_reason":"completed","limit_ms":45000,"duration_ms":100,"cleanup_confirmed":true,"devices":[{"interface":"event2","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":1,"product":2,"version":1},"capabilities":{"protocol_version":65537,"events":[0,1,3],"keys":[330],"relative":[],"properties":[1],"misc":[],"axes":[{"code":0,"minimum":-100,"maximum":100,"fuzz":0,"flat":0,"resolution":0},{"code":1,"minimum":-100,"maximum":100,"fuzz":0,"flat":0,"resolution":0}]},"x_threshold":10,"y_threshold":10,"frames":6,"contact_starts":1,"contact_releases":1,"xy_baselines":1,"qualifying_contacts":1,"x_positive":2,"x_negative":2,"y_positive":2,"y_negative":2,"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]}`

func passFixture(t *testing.T) inputtest.TouchscreenResult {
	t.Helper()
	var result inputtest.TouchscreenResult
	if err := json.Unmarshal([]byte(literalPass), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func wire(t *testing.T, result inputtest.TouchscreenResult) []byte {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTouchscreenEvidenceLiteralOwnershipAndSharedFrames(t *testing.T) {
	raw := []byte(literalPass)
	got, err := Decode(raw)
	if err != nil || !reflect.DeepEqual(got, passFixture(t)) {
		t.Fatal("independent literal did not decode")
	}
	d := got.Devices[0]
	if d.XPositive+d.XNegative+d.YPositive+d.YNegative <= d.Frames {
		t.Fatal("cross-axis frame witness is missing")
	}
	for i := range raw {
		raw[i] = 'x'
	}
	got.Devices[0].Capabilities.Keys[0] = 0
	again, err := Decode([]byte(literalPass))
	if err != nil || !reflect.DeepEqual(again, passFixture(t)) {
		t.Fatal("decoder retained caller-owned storage")
	}
}

func TestTouchscreenEvidenceRejectsImpossibleOrIncompletePass(t *testing.T) {
	for _, name := range []string{"no_baseline", "not_released", "no_qualifying_contact", "no_x_positive", "no_y_negative", "too_many_starts", "too_many_releases", "too_many_baselines", "too_many_qualifying", "motion_in_start_frame", "counter_overflow", "not_terminal", "lost", "not_clean", "wrong_threshold", "indirect", "finger", "pressure", "nil_relative", "empty_devices", "zero_limit", "over_limit", "wrong_schema"} {
		t.Run(name, func(t *testing.T) {
			r := passFixture(t)
			d := &r.Devices[0]
			switch name {
			case "no_baseline":
				d.BaselineNeutral = false
			case "not_released":
				d.ContactReleases = 0
			case "no_qualifying_contact":
				d.QualifyingContacts = 0
			case "no_x_positive":
				d.XPositive = 0
			case "no_y_negative":
				d.YNegative = 0
			case "too_many_starts":
				d.ContactStarts = 7
			case "too_many_releases":
				d.ContactReleases = 2
			case "too_many_baselines":
				d.XYBaselines = 2
			case "too_many_qualifying":
				d.QualifyingContacts = 2
			case "motion_in_start_frame":
				d.Frames = 5
			case "counter_overflow":
				d.XPositive = ^uint64(0)
			case "not_terminal":
				d.FinalNeutral = false
			case "lost":
				d.EvidenceLoss = true
			case "not_clean":
				d.CleanupConfirmed = false
			case "wrong_threshold":
				d.XThreshold = 9
			case "indirect":
				d.Capabilities.Properties = []uint16{0}
			case "finger":
				d.Capabilities.Keys = []uint16{325, 330}
			case "pressure":
				d.Capabilities.Axes = append(d.Capabilities.Axes, inputtest.TouchscreenAxis{Code: 24, Maximum: 100})
			case "nil_relative":
				d.Capabilities.Relative = nil
			case "empty_devices":
				r.Devices = []inputtest.TouchscreenDeviceResult{}
			case "zero_limit":
				r.LimitMilliseconds = 0
			case "over_limit":
				r.LimitMilliseconds = 120001
			case "wrong_schema":
				r.Schema = "hhsoft.pc-tester.touchscreen-functional/v2"
			}
			if !errors.Is(ValidateResult(r), ErrInvalid) {
				t.Fatal("impossible or unsupported pass admitted")
			}
		})
	}
}

func TestTouchscreenEvidenceWireDiscrimination(t *testing.T) {
	for _, name := range []string{"duplicate", "escaped_duplicate", "case_key", "future", "case_schema", "unknown_coordinate", "buttons", "trailing", "invalid_utf8"} {
		t.Run(name, func(t *testing.T) {
			raw := literalPass
			switch name {
			case "duplicate":
				raw = strings.Replace(raw, `"schema":`, `"schema":"ignored","schema":`, 1)
			case "escaped_duplicate":
				raw = strings.Replace(raw, `"schema":`, `"s\u0063hema":"ignored","schema":`, 1)
			case "case_key":
				raw = strings.Replace(raw, `"schema":`, `"Schema":`, 1)
			case "future":
				raw = strings.Replace(raw, "touchscreen-functional/v1", "touchscreen-functional/v2", 1)
			case "case_schema":
				raw = strings.Replace(raw, "touchscreen-functional", "Touchscreen-Functional", 1)
			case "unknown_coordinate":
				raw = strings.Replace(raw, `"x_threshold":`, `"raw_x":17,"x_threshold":`, 1)
			case "buttons":
				raw = strings.Replace(raw, `"x_threshold":`, `"buttons":[],"x_threshold":`, 1)
			case "trailing":
				raw += "{}"
			case "invalid_utf8":
				raw += string([]byte{0xff})
			}
			if _, err := Decode([]byte(raw)); !errors.Is(err, ErrInvalid) {
				t.Fatal("malformed or foreign claim admitted")
			}
		})
	}
}

func TestTouchscreenEvidenceDiagnosticPhaseAndCleanup(t *testing.T) {
	r := passFixture(t)
	r.Status, r.StopReason, r.Detail = inputtest.StatusInconclusive, "error", "incomplete_actions"
	d := &r.Devices[0]
	d.Status, d.Detail, d.FinalNeutral = inputtest.StatusInconclusive, "contact_incomplete", false
	d.Frames, d.ContactStarts, d.ContactReleases, d.XYBaselines, d.QualifyingContacts = 6, 2, 2, 2, 0
	d.XPositive, d.XNegative, d.YPositive, d.YNegative = 1, 1, 1, 1
	if err := ValidateResult(r); err != nil {
		t.Fatal("released nonqualifying contacts lost diagnostic counts")
	}
	r.Status, r.StopReason, r.Detail = inputtest.StatusPass, "completed", ""
	d.Status, d.Detail, d.FinalNeutral = inputtest.StatusPass, "", true
	if !errors.Is(ValidateResult(r), ErrInvalid) {
		t.Fatal("aggregate directions supplied a missing qualifying contact")
	}

	r = passFixture(t)
	r.Status, r.StopReason, r.Detail, r.CleanupConfirmed = inputtest.StatusError, "cleanup_unconfirmed", "cleanup_unconfirmed", false
	r.Devices[0].Status, r.Devices[0].Detail, r.Devices[0].CleanupConfirmed = inputtest.StatusError, "cleanup_unconfirmed", false
	decoded, err := Decode(wire(t, r))
	if !errors.Is(err, ErrCleanupUnconfirmed) || !reflect.DeepEqual(decoded, r) {
		t.Fatal("cleanup diagnostic was discarded or promoted to success")
	}
}

func TestTouchscreenEvidenceUnqueriedAxisIsOnlyAnUnsupportedMarker(t *testing.T) {
	r := inputtest.TouchscreenResult{Schema: inputtest.TouchscreenSchema, Status: inputtest.StatusInconclusive,
		StopReason: "error", Detail: "unsupported_or_unreadable_device", LimitMilliseconds: 45000, CleanupConfirmed: true,
		Devices: []inputtest.TouchscreenDeviceResult{{Interface: "event2", Status: inputtest.StatusInconclusive,
			Readable: true, Identity: &inputtest.TouchscreenIdentity{Bus: 3}, CleanupConfirmed: true, Detail: "unsupported_capabilities"}}}
	c := passFixture(t).Devices[0].Capabilities
	c.Axes = append(c.Axes, inputtest.TouchscreenAxis{Code: 24})
	r.Devices[0].Capabilities = c
	if err := ValidateResult(r); err != nil {
		t.Fatal("unsupported pressure marker was lost")
	}
	c.Axes[2].Maximum = 100
	if !errors.Is(ValidateResult(r), ErrInvalid) {
		t.Fatal("unqueried axis acquired measured configuration")
	}
}
