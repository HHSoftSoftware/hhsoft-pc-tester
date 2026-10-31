package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchscreenevidence"
)

// Literal single-contact action counters are independent of the producer.

const touchscreenRouteTouchscreenLiteral = `{"schema":"hhsoft.pc-tester.touchscreen-functional/v1","status":"pass","stop_reason":"completed","limit_ms":45000,"duration_ms":100,"cleanup_confirmed":true,"devices":[{"interface":"event4","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":4660,"product":22136,"version":1},"capabilities":{"protocol_version":65537,"events":[0,1,3,4],"keys":[330],"relative":[],"properties":[1],"misc":[4,5],"axes":[{"code":0,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1},{"code":1,"minimum":-500,"maximum":500,"fuzz":0,"flat":0,"resolution":1}]},"x_threshold":50,"y_threshold":50,"frames":6,"contact_starts":1,"contact_releases":1,"xy_baselines":1,"qualifying_contacts":1,"x_positive":2,"x_negative":2,"y_positive":2,"y_negative":2,"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]}`

func touchscreenRouteValue(t *testing.T) inputtest.TouchscreenResult {
	t.Helper()
	var value inputtest.TouchscreenResult
	if err := json.Unmarshal([]byte(touchscreenRouteTouchscreenLiteral), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func touchscreenRouteEncode(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func touchscreenRouteComponent() inventory.Component {
	return inventory.NewComponent(inventory.ComponentInput, "input:touchscreen-fixture", map[string]string{
		"category": "touchscreen", "touchscreen_candidate": "true", "interface": "event4",
		"bus": "0003", "vendor_id": "1234", "product_id": "5678", "version": "0001",
		"bitmap_word_bits": "64", "event_capabilities": "1b", "key_capabilities": "400 0 0 0 0 0",
		"relative_capabilities": "0", "absolute_capabilities": "3", "properties": "2", "msc_capabilities": "30",
	})
}

func touchscreenRouteCurrentPlan(t *testing.T, profile plan.Profile) (plan.Plan, inventory.Snapshot, []byte) {
	t.Helper()
	snapshot := inventory.NewSnapshot([]inventory.Component{touchscreenRouteComponent()})
	key := bytes.Repeat([]byte{0x52}, 32)
	frozen, err := plan.Build(snapshot, profile, "x86_64", key, "route-fixture")
	if err != nil {
		t.Fatal(err)
	}
	frozen, err = plan.Decode(touchscreenRouteEncode(t, frozen))
	if err != nil {
		t.Fatal("current touchscreen fixture did not pass execution admission", err)
	}
	row := touchscreenRouteTest(t, frozen)
	if row.Version != 2 || row.Status != plan.TestScheduled || !reflect.DeepEqual(row.ComponentIDs, []string{"input:touchscreen-fixture"}) {
		t.Fatal("current catalog did not schedule the derived touchscreen candidate")
	}
	return frozen, snapshot, key
}

func touchscreenRouteTest(t *testing.T, frozen plan.Plan) plan.Test {
	t.Helper()
	for _, row := range frozen.Tests {
		if row.ID == "input.touchscreen.quick" {
			return row
		}
	}
	t.Fatal("touchscreen fixture row missing")
	return plan.Test{}
}

func touchscreenRouteSetAttribute(component *inventory.Component, name, value string) {
	for i := range component.Attributes {
		if component.Attributes[i].Name == name {
			component.Attributes[i].Value = value
			return
		}
	}
	component.Attributes = append(component.Attributes, inventory.Attribute{Name: name, Value: value})
}

func TestTouchscreenFinalResultLiteralAndFiniteNotes(t *testing.T) {
	for _, name := range []string{"pass", "inconclusive", "deadline", "aborted", "excluded", "not_present", "unsupported", "missing_driver", "absent_error", "absent_aborted"} {
		t.Run(name, func(t *testing.T) {
			frozen, _, _ := touchscreenRouteCurrentPlan(t, plan.ProfileQuick)
			test := touchscreenRouteTest(t, frozen)
			value := touchscreenRouteValue(t)
			source, readErr, raw := "pass", error(nil), []byte(nil)
			switch name {
			case "inconclusive":
				value.Status, value.StopReason, value.Detail = inputtest.StatusInconclusive, "observation_timeout", "observation_timeout"
				source = "inconclusive"
			case "deadline":
				value.Status, value.StopReason, value.Detail = inputtest.StatusError, "deadline", "deadline"
				source = "error"
			case "aborted":
				value.Status, value.StopReason, value.Detail = inputtest.StatusAborted, "operator_cancelled", "operator_cancelled"
				source = "aborted"
			case "excluded", "not_present", "unsupported", "missing_driver":
				test.Status, source, readErr = plan.TestStatus(name), name, os.ErrNotExist
			case "absent_error":
				source, readErr = "error", os.ErrNotExist
			case "absent_aborted":
				source, readErr = "aborted", os.ErrNotExist
			}
			if readErr == nil {
				raw = touchscreenRouteEncode(t, value)
			}
			got, err := newTouchscreenFinalResult(test, source, raw, readErr)
			want := source
			switch name {
			case "excluded", "not_present", "unsupported":
				want = "skip"
			case "missing_driver":
				want = "inconclusive"
			}
			if err != nil || got.ID != "input.touchscreen.quick" || got.Version != 2 || string(got.Status) != want {
				t.Fatalf("literal result/disposition rejected: %v", err)
			}
			if name == "pass" && got.Status != app.TestPass {
				t.Fatal("literal pass status changed")
			}
			if readErr == nil {
				if _, err := touchscreenevidence.Decode(got.Evidence); err != nil {
					t.Fatal(err)
				}
				saved := append([]byte(nil), got.Evidence...)
				for i := range raw {
					raw[i] = 'x'
				}
				if !bytes.Equal(saved, got.Evidence) {
					t.Fatal("result retained caller bytes")
				}
			} else {
				if sessionevidence.HasTouchscreenClaim(got.Evidence) {
					t.Fatal("note claimed observed touchscreen data")
				}
			}
		})
	}
}

func TestTouchscreenFinalResultRejectsPublicationContradictions(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*plan.Test, *string, *[]byte, *error)
	}{
		{"wrong_id", func(p *plan.Test, _ *string, _ *[]byte, _ *error) { p.ID = "input.pointer.quick" }},
		{"old_definition", func(p *plan.Test, _ *string, _ *[]byte, _ *error) { p.Version = 1 }},
		{"future_definition", func(p *plan.Test, _ *string, _ *[]byte, _ *error) { p.Version = 3 }},
		{"nonscheduled_file", func(p *plan.Test, _ *string, _ *[]byte, _ *error) { p.Status = plan.TestExcluded }},
		{"status_downgrade", func(_ *plan.Test, s *string, _ *[]byte, _ *error) { *s = "error" }},
		{"status_alias", func(_ *plan.Test, s *string, _ *[]byte, _ *error) { *s = "PASS" }},
		{"malformed", func(_ *plan.Test, _ *string, r *[]byte, _ *error) { *r = []byte("{") }},
		{"empty_present", func(_ *plan.Test, _ *string, r *[]byte, _ *error) { *r = nil }},
		{"duplicate_schema", func(_ *plan.Test, _ *string, r *[]byte, _ *error) {
			*r = bytes.Replace(*r, []byte(`"schema":`), []byte(`"schema":"hhsoft.pc-tester.touchscreen-functional/v2","schema":`), 1)
		}},
		{"schema_alias", func(_ *plan.Test, _ *string, r *[]byte, _ *error) {
			*r = bytes.Replace(*r, []byte(`"schema":`), []byte(`"Schema":`), 1)
		}},
		{"unknown_version", func(_ *plan.Test, _ *string, r *[]byte, _ *error) {
			*r = bytes.Replace(*r, []byte("touchscreen-functional/v1"), []byte("touchscreen-functional/v2"), 1)
		}},
		{"trailing_value", func(_ *plan.Test, _ *string, r *[]byte, _ *error) { *r = append(*r, []byte(" {}")...) }},
		{"read_error", func(_ *plan.Test, _ *string, r *[]byte, e *error) {
			*r = nil
			*e = errors.New("synthetic read failure")
		}},
		{"wrapped_absence", func(_ *plan.Test, _ *string, r *[]byte, e *error) {
			*r = nil
			*e = fmt.Errorf("wrapped: %w", os.ErrNotExist)
		}},
		{"absence_with_bytes", func(_ *plan.Test, _ *string, _ *[]byte, e *error) { *e = os.ErrNotExist }},
		{"absent_pass", func(_ *plan.Test, _ *string, r *[]byte, e *error) { *r = nil; *e = os.ErrNotExist }},
		{"absent_fail", func(_ *plan.Test, s *string, r *[]byte, e *error) { *s = "fail"; *r = nil; *e = os.ErrNotExist }},
		{"nonscheduled_changed_status", func(p *plan.Test, s *string, r *[]byte, e *error) {
			p.Status = plan.TestExcluded
			*s = "unsupported"
			*r = nil
			*e = os.ErrNotExist
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, _, _ := touchscreenRouteCurrentPlan(t, plan.ProfileQuick)
			test := touchscreenRouteTest(t, p)
			status, raw, readErr := "pass", []byte(touchscreenRouteTouchscreenLiteral), error(nil)
			c.mutate(&test, &status, &raw, &readErr)
			got, err := newTouchscreenFinalResult(test, status, raw, readErr)
			if err == nil || got.ID != "" {
				t.Fatal("contradictory file/note accepted")
			}
		})
	}
	t.Run("typed_cleanup_before_ordinary_status", func(t *testing.T) {
		p, _, _ := touchscreenRouteCurrentPlan(t, plan.ProfileQuick)
		v := touchscreenRouteValue(t)
		v.Status, v.StopReason, v.Detail, v.CleanupConfirmed = inputtest.StatusError, "cleanup_unconfirmed", "cleanup_unconfirmed", false
		got, err := newTouchscreenFinalResult(touchscreenRouteTest(t, p), "aborted", touchscreenRouteEncode(t, v), nil)
		if !errors.Is(err, touchscreenevidence.ErrCleanupUnconfirmed) || got.ID != "" {
			t.Fatal("cleanup uncertainty was lost")
		}
	})
}

func TestTouchscreenFinalizerBindsCompleteFrozenAndFinalSets(t *testing.T) {
	for _, name := range []string{"quick", "standard", "retained_pass_after_cancel"} {
		t.Run(name, func(t *testing.T) {
			profile := plan.ProfileQuick
			if name == "standard" {
				profile = plan.ProfileStandard
			}
			frozen, snapshot, key := touchscreenRouteCurrentPlan(t, profile)
			final, _, _ := touchscreenRouteCurrentPlan(t, profile)
			graph, err := plan.ObserveHardwareGraph(snapshot, key, "route-fixture")
			if err != nil || !reflect.DeepEqual(graph, final.HardwareGraph) {
				t.Fatal("independent final HMAC prerequisite")
			}
			value := touchscreenRouteValue(t)
			if name == "retained_pass_after_cancel" {
				value.Status, value.StopReason, value.Detail = inputtest.StatusAborted, "operator_cancelled", "operator_cancelled"
			}
			if err := validateTouchscreenPlanEvidence(touchscreenRouteEncode(t, value), touchscreenRouteTest(t, frozen), frozen, final, snapshot); err != nil {
				t.Fatal(err)
			}
		})
	}
	cases := []struct {
		name   string
		change func(*plan.Test, *plan.Plan, *plan.Plan, *inventory.Snapshot, *inputtest.TouchscreenResult)
	}{
		{"wrong_plan_test", func(test *plan.Test, _ *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			test.TimeoutSeconds = 119
		}},
		{"wrong_version", func(test *plan.Test, _ *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			test.Version = 1
		}},
		{"too_short_timeout", func(test *plan.Test, _ *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			test.TimeoutSeconds = 1
		}},
		{"duplicate_planned_refs", func(test *plan.Test, p *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			test.ComponentIDs = append(test.ComponentIDs, test.ComponentIDs[0])
			for i := range p.Tests {
				if p.Tests[i].ID == test.ID {
					p.Tests[i] = *test
				}
			}
		}},
		{"missing_planned_graph_component", func(_ *plan.Test, p *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			p.HardwareGraph.Components = nil
		}},
		{"duplicate_final_test", func(test *plan.Test, _ *plan.Plan, p *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			p.Tests = append(p.Tests, *test)
		}},
		{"final_old_definition", func(_ *plan.Test, _ *plan.Plan, p *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			for i := range p.Tests {
				if p.Tests[i].ID == "input.touchscreen.quick" {
					p.Tests[i].Version = 1
				}
			}
		}},
		{"final_excluded", func(_ *plan.Test, _ *plan.Plan, p *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			for i := range p.Tests {
				if p.Tests[i].ID == "input.touchscreen.quick" {
					p.Tests[i].Status = plan.TestExcluded
				}
			}
		}},
		{"changed_component_hmac", func(_ *plan.Test, _ *plan.Plan, p *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			p.HardwareGraph.Components[0].Value = "hmac-sha256:" + strings.Repeat("0", 64)
		}},
		{"changed_epoch", func(_ *plan.Test, _ *plan.Plan, p *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			p.HardwareGraph.KeyEpoch = "other"
		}},
		{"changed_algorithm", func(_ *plan.Test, _ *plan.Plan, p *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			p.HardwareGraph.Algorithm = "other"
		}},
		{"removed_candidate", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			s.Components = []inventory.Component{}
		}},
		{"added_candidate", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			c := touchscreenRouteComponent()
			c.ID = "input:second"
			touchscreenRouteSetAttribute(&c, "interface", "event5")
			s.Components = append(s.Components, c)
		}},
		{"replaced_interface", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			touchscreenRouteSetAttribute(&s.Components[0], "interface", "event5")
		}},
		{"identity_changed", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			touchscreenRouteSetAttribute(&s.Components[0], "vendor_id", "1235")
		}},
		{"identity_alias_conflict", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			touchscreenRouteSetAttribute(&s.Components[0], "vendor", "1235")
		}},
		{"unknown_width", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			touchscreenRouteSetAttribute(&s.Components[0], "bitmap_word_bits", "16")
		}},
		{"missing_msc", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			touchscreenRouteSetAttribute(&s.Components[0], "msc_capabilities", "")
		}},
		{"hidden_extra_msc", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchscreenResult) {
			touchscreenRouteSetAttribute(&s.Components[0], "msc_capabilities", "31")
		}},
		{"omitted_advertised_key", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, v *inputtest.TouchscreenResult) {
			v.Devices[0].Capabilities.Keys = []uint16{}
		}},
		{"duplicate_observation", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, v *inputtest.TouchscreenResult) {
			v.Devices = append(v.Devices, v.Devices[0])
		}},
		{"retained_pass_unplanned", func(test *plan.Test, p *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, v *inputtest.TouchscreenResult) {
			v.Status, v.StopReason, v.Detail = inputtest.StatusAborted, "operator_cancelled", "operator_cancelled"
			test.ComponentIDs = []string{"input:other"}
			for i := range p.Tests {
				if p.Tests[i].ID == test.ID {
					p.Tests[i] = *test
				}
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			frozen, snapshot, _ := touchscreenRouteCurrentPlan(t, plan.ProfileQuick)
			final, _, _ := touchscreenRouteCurrentPlan(t, plan.ProfileQuick)
			test, value := touchscreenRouteTest(t, frozen), touchscreenRouteValue(t)
			c.change(&test, &frozen, &final, &snapshot, &value)
			if err := validateTouchscreenPlanEvidence(touchscreenRouteEncode(t, value), test, frozen, final, snapshot); err == nil {
				t.Fatal("unbound functional observation accepted")
			}
		})
	}
}

func TestTouchscreenFinalInputOwnershipAndOriginalHMACFacts(t *testing.T) {
	for _, name := range []string{"test_refs", "capability_refs", "graph", "exclusions", "component", "attributes", "key"} {
		t.Run(name, func(t *testing.T) {
			frozen, snapshot, key := touchscreenRouteCurrentPlan(t, plan.ProfileQuick)
			frozen.Capabilities = append(frozen.Capabilities, plan.Capability{ID: "synthetic-owner", ComponentIDs: []string{"literal"}})
			frozen.ExcludedTests = []string{"literal-exclusion"}
			owned, view, secret, err := freezeTouchpadFinalInputs(frozen, snapshot, key)
			if err != nil {
				t.Fatal(err)
			}
			planBefore, snapshotBefore, keyBefore := touchscreenRouteEncode(t, owned), touchscreenRouteEncode(t, view), append([]byte(nil), secret...)
			callback := func() {
				switch name {
				case "test_refs":
					for i := range frozen.Tests {
						if frozen.Tests[i].ID == "input.touchscreen.quick" {
							frozen.Tests[i].ComponentIDs[0] = "changed"
						}
					}
				case "capability_refs":
					frozen.Capabilities[len(frozen.Capabilities)-1].ComponentIDs[0] = "changed"
				case "graph":
					frozen.HardwareGraph.Components[0].Value = "changed"
				case "exclusions":
					frozen.ExcludedTests[0] = "changed"
				case "component":
					snapshot.Components[0].ID = "changed"
				case "attributes":
					snapshot.Components[0].Attributes[0].Value = "changed"
				case "key":
					key[0] ^= 0xff
				}
			}
			callback()
			if !bytes.Equal(planBefore, touchscreenRouteEncode(t, owned)) || !bytes.Equal(snapshotBefore, touchscreenRouteEncode(t, view)) || !bytes.Equal(keyBefore, secret) {
				t.Fatal("callback changed owned authority")
			}
			graph, err := plan.ObserveHardwareGraph(view, secret, "route-fixture")
			if err != nil || !reflect.DeepEqual(graph, owned.HardwareGraph) {
				t.Fatal("owned original inventory/key graph changed")
			}
		})
	}
	t.Run("copy_does_not_normalize_duplicate_attributes", func(t *testing.T) {
		p, s, k := touchscreenRouteCurrentPlan(t, plan.ProfileQuick)
		s.Components[0].Attributes = append(s.Components[0].Attributes, s.Components[0].Attributes[0])
		_, owned, _, err := freezeTouchpadFinalInputs(p, s, k)
		if err != nil || !reflect.DeepEqual(owned, s) {
			t.Fatal("copy normalized raw attributes")
		}
		if err := touchscreenevidence.ValidateInventoryBinding(touchscreenRouteValue(t), owned.Components); err == nil {
			t.Fatal("duplicate original attribute hidden")
		}
	})
	for _, row := range []struct {
		name   string
		length int
	}{{"key_length_0", 0}, {"key_length_31", 31}, {"key_length_33", 33}} {
		t.Run(row.name, func(t *testing.T) {
			p, s, _ := touchscreenRouteCurrentPlan(t, plan.ProfileQuick)
			owned, view, key, err := freezeTouchpadFinalInputs(p, s, make([]byte, row.length))
			if err == nil || owned.ID != "" || view.Components != nil || key != nil {
				t.Fatal("invalid key produced authority")
			}
		})
	}
}

func TestTouchscreenFinalEvidenceReadbackIsExactAndOwned(t *testing.T) {
	for _, name := range []string{"same", "changed", "missing", "read_error", "wrapped_absence", "appeared", "absent", "absent_bytes", "callback_mutation", "nil_reader", "empty_present"} {
		t.Run(name, func(t *testing.T) {
			original := []byte(touchscreenRouteTouchscreenLiteral)
			present := true
			if name == "appeared" || name == "absent" || name == "absent_bytes" || name == "wrapped_absence" {
				original = nil
				present = false
			}
			want := append([]byte(nil), original...)
			if name == "empty_present" {
				original = nil
			}
			calls := 0
			reader := func(path string) ([]byte, error) {
				calls++
				if path != "fixed-touchscreen.json" {
					t.Fatal("unexpected evidence path")
				}
				switch name {
				case "changed":
					return []byte("{}"), nil
				case "missing", "absent":
					return nil, os.ErrNotExist
				case "wrapped_absence":
					return nil, fmt.Errorf("wrapped: %w", os.ErrNotExist)
				case "read_error":
					return nil, errors.New("synthetic close failure")
				case "appeared":
					return []byte(touchscreenRouteTouchscreenLiteral), nil
				case "absent_bytes":
					return []byte("x"), os.ErrNotExist
				case "callback_mutation":
					original[0] = 'x'
					return want, nil
				}
				return want, nil
			}
			if name == "nil_reader" {
				reader = nil
			}
			err := recheckTouchscreenEvidence("fixed-touchscreen.json", original, present, reader)
			positive := name == "same" || name == "absent" || name == "callback_mutation"
			if (err == nil) != positive {
				t.Fatalf("readback admission differs: %v", err)
			}
			if name != "nil_reader" && name != "empty_present" && calls != 1 {
				t.Fatal("readback called reader more than once")
			}
		})
	}
}

func TestTouchscreenFinalizerCopiesKeyBeforeContextCallback(t *testing.T) {
	directory := t.TempDir()
	backing := bytes.Repeat([]byte{0x27}, 32)
	snapshot := inventory.NewSnapshot([]inventory.Component{})
	frozen, err := plan.BuildWithSelection(snapshot, plan.ProfileQuick, "x86_64", backing, "owned-fixture", []string{"inspection.exterior.quick"})
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(directory, "plan.json")
	if err := os.WriteFile(planPath, touchscreenRouteEncode(t, frozen), 0600); err != nil {
		t.Fatal("write owned plan")
	}
	arguments := []string{"-plan", planPath, "-fingerprint-key", "synthetic-key", "-key-epoch", "owned-fixture", "-evidence-dir", directory, "-output", "synthetic-report"}
	// This explicit current set keeps the key-ownership fixture independent of layout helpers.
	for _, id := range strings.Fields("audio.readiness.quick cpu.quick display.operator.quick graphics.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.touchscreen.quick input.readiness.quick inspection.exterior.quick memory.quick network.readiness.quick storage.readiness.quick usb.readiness.quick") {
		found := false
		for _, row := range frozen.Tests {
			if row.ID == id {
				if row.Status == plan.TestScheduled {
					t.Fatal("unobserved current fixture row is scheduled")
				}
				arguments = append(arguments, "-outcome", id+"="+string(row.Status))
				found = true
			}
		}
		if !found {
			t.Fatal("current fixture row absent")
		}
	}
	ctx := &commandRouteMutationContext{Context: context.Background()}
	mutations, reads, writes := 0, 0, 0
	readKey := func(_ context.Context, _ string, kind retestInputKind) ([]byte, error) {
		if kind != retestFingerprintKeyInput {
			t.Fatal("unexpected input role")
		}
		reads++
		ctx.mutate = func() { mutations++; backing[0] ^= 0xff }
		return backing, nil
	}
	var reportBytes, stdout, stderr bytes.Buffer
	write := func(_ string, _ io.Writer, produce func(io.Writer) error) error {
		writes++
		return produce(&reportBytes)
	}
	result := runFinalizeOwnedDestinationWithPreviousForProfile(ctx, arguments, &stdout, &stderr,
		func(context.Context) (inventory.Snapshot, error) { return snapshot, nil },
		bytes.NewReader(bytes.Repeat([]byte{0x33}, 1024)), func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
		nil, write, nil, readKey, "")
	if result.Code != 0 || result.Integrity.Status != "pass" || reads != 1 || mutations != 1 || writes != 1 || reportBytes.Len() == 0 {
		t.Fatalf("key owner boundary lost: code=%d reads=%d mutations=%d writes=%d", result.Code, reads, mutations, writes)
	}
}
