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
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchpadevidence"
)

// Literal action accounting is independent of the producer and command helpers.
// X and Y overlap; each axis uses all four frames between contact start/release.
const commandRouteTouchpadLiteral = `{"schema":"hhsoft.pc-tester.touchpad-functional/v1","status":"pass","stop_reason":"completed","limit_ms":30000,"duration_ms":100,"cleanup_confirmed":true,"devices":[{"interface":"event4","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":4660,"product":22136,"version":1},"capabilities":{"protocol_version":65537,"events":[0,1,3,4],"keys":[272,273,274,325,330],"relative":[],"properties":[0,2],"misc":[4,5],"axes":[{"code":0,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1},{"code":1,"minimum":-500,"maximum":500,"fuzz":0,"flat":0,"resolution":1}]},"x_threshold":50,"y_threshold":50,"frames":6,"contact_starts":1,"contact_releases":1,"xy_baselines":1,"qualifying_contacts":1,"x_positive":2,"x_negative":2,"y_positive":2,"y_negative":2,"buttons":[{"button":"left","presses":1,"releases":1,"pairs":1},{"button":"right","presses":1,"releases":1,"pairs":1},{"button":"middle","presses":1,"releases":1,"pairs":1}],"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]}`

func commandRouteValue(t *testing.T) inputtest.TouchpadResult {
	t.Helper()
	var value inputtest.TouchpadResult
	if err := json.Unmarshal([]byte(commandRouteTouchpadLiteral), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func commandRouteEncode(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func commandRouteComponent() inventory.Component {
	return inventory.NewComponent(inventory.ComponentInput, "input:touchpad-fixture", map[string]string{
		"category": "touchpad", "touchpad_candidate": "true", "interface": "event4",
		"bus": "0003", "vendor_id": "1234", "product_id": "5678", "version": "0001",
		"bitmap_word_bits": "64", "event_capabilities": "1b", "key_capabilities": "420 70000 0 0 0 0",
		"relative_capabilities": "0", "absolute_capabilities": "3", "properties": "5", "msc_capabilities": "30",
	})
}

// This fixture uses the current catalog, real HMAC builder and strict plan decoder.
// Historical typed layouts are constructed explicitly by the tests that need them.
func commandRouteCurrentPlan(t *testing.T, profile plan.Profile) (plan.Plan, inventory.Snapshot, []byte) {
	t.Helper()
	snapshot := inventory.NewSnapshot([]inventory.Component{commandRouteComponent()})
	key := bytes.Repeat([]byte{0x52}, 32)
	frozen, err := plan.Build(snapshot, profile, "x86_64", key, "route-fixture")
	if err != nil {
		t.Fatal(err)
	}
	frozen, err = plan.Decode(commandRouteEncode(t, frozen))
	if err != nil {
		t.Fatal("current touchpad fixture did not pass execution admission", err)
	}
	row := commandRouteTest(t, frozen)
	if row.Version != 2 || row.Status != plan.TestScheduled || !reflect.DeepEqual(row.ComponentIDs, []string{"input:touchpad-fixture"}) {
		t.Fatal("current catalog did not schedule the derived touchpad candidate")
	}
	return frozen, snapshot, key
}

func commandRouteTest(t *testing.T, frozen plan.Plan) plan.Test {
	t.Helper()
	for _, row := range frozen.Tests {
		if row.ID == "input.touchpad.quick" {
			return row
		}
	}
	t.Fatal("touchpad fixture row missing")
	return plan.Test{}
}

func commandRouteSetAttribute(component *inventory.Component, name, value string) {
	for i := range component.Attributes {
		if component.Attributes[i].Name == name {
			component.Attributes[i].Value = value
			return
		}
	}
	component.Attributes = append(component.Attributes, inventory.Attribute{Name: name, Value: value})
}

func TestTouchpadFinalResultLiteralAndFiniteNotes(t *testing.T) {
	for _, name := range []string{"pass", "inconclusive", "deadline", "aborted", "excluded", "not_present", "unsupported", "missing_driver", "absent_error", "absent_aborted"} {
		t.Run(name, func(t *testing.T) {
			frozen, _, _ := commandRouteCurrentPlan(t, plan.ProfileQuick)
			test := commandRouteTest(t, frozen)
			value := commandRouteValue(t)
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
				raw = commandRouteEncode(t, value)
			}
			got, err := newTouchpadFinalResult(test, source, raw, readErr)
			want := source
			switch name {
			case "excluded", "not_present", "unsupported":
				want = "skip"
			case "missing_driver":
				want = "inconclusive"
			}
			if err != nil || got.ID != "input.touchpad.quick" || got.Version != 2 || string(got.Status) != want {
				t.Fatalf("literal result/disposition rejected: %v", err)
			}
			if name == "pass" && got.Status != app.TestPass {
				t.Fatal("literal pass status changed")
			}
			if readErr == nil {
				if _, err := touchpadevidence.Decode(got.Evidence); err != nil {
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
				if sessionevidence.HasTouchpadClaim(got.Evidence) {
					t.Fatal("note claimed observed touchpad data")
				}
			}
		})
	}
}

func TestTouchpadFinalResultRejectsPublicationContradictions(t *testing.T) {
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
			*r = bytes.Replace(*r, []byte(`"schema":`), []byte(`"schema":"hhsoft.pc-tester.touchpad-functional/v2","schema":`), 1)
		}},
		{"schema_alias", func(_ *plan.Test, _ *string, r *[]byte, _ *error) {
			*r = bytes.Replace(*r, []byte(`"schema":`), []byte(`"Schema":`), 1)
		}},
		{"unknown_version", func(_ *plan.Test, _ *string, r *[]byte, _ *error) {
			*r = bytes.Replace(*r, []byte("touchpad-functional/v1"), []byte("touchpad-functional/v2"), 1)
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
			p, _, _ := commandRouteCurrentPlan(t, plan.ProfileQuick)
			test := commandRouteTest(t, p)
			status, raw, readErr := "pass", []byte(commandRouteTouchpadLiteral), error(nil)
			c.mutate(&test, &status, &raw, &readErr)
			got, err := newTouchpadFinalResult(test, status, raw, readErr)
			if err == nil || got.ID != "" {
				t.Fatal("contradictory file/note accepted")
			}
		})
	}
	t.Run("typed_cleanup_before_ordinary_status", func(t *testing.T) {
		p, _, _ := commandRouteCurrentPlan(t, plan.ProfileQuick)
		v := commandRouteValue(t)
		v.Status, v.StopReason, v.Detail, v.CleanupConfirmed = inputtest.StatusError, "cleanup_unconfirmed", "cleanup_unconfirmed", false
		got, err := newTouchpadFinalResult(commandRouteTest(t, p), "aborted", commandRouteEncode(t, v), nil)
		if !errors.Is(err, touchpadevidence.ErrCleanupUnconfirmed) || got.ID != "" {
			t.Fatal("cleanup uncertainty was lost")
		}
	})
}

func TestTouchpadFinalizerBindsCompleteFrozenAndFinalSets(t *testing.T) {
	for _, name := range []string{"quick", "standard", "retained_pass_after_cancel"} {
		t.Run(name, func(t *testing.T) {
			profile := plan.ProfileQuick
			if name == "standard" {
				profile = plan.ProfileStandard
			}
			frozen, snapshot, key := commandRouteCurrentPlan(t, profile)
			final, _, _ := commandRouteCurrentPlan(t, profile)
			graph, err := plan.ObserveHardwareGraph(snapshot, key, "route-fixture")
			if err != nil || !reflect.DeepEqual(graph, final.HardwareGraph) {
				t.Fatal("independent final HMAC prerequisite")
			}
			value := commandRouteValue(t)
			if name == "retained_pass_after_cancel" {
				value.Status, value.StopReason, value.Detail = inputtest.StatusAborted, "operator_cancelled", "operator_cancelled"
			}
			if err := validateTouchpadPlanEvidence(commandRouteEncode(t, value), commandRouteTest(t, frozen), frozen, final, snapshot); err != nil {
				t.Fatal(err)
			}
		})
	}
	cases := []struct {
		name   string
		change func(*plan.Test, *plan.Plan, *plan.Plan, *inventory.Snapshot, *inputtest.TouchpadResult)
	}{
		{"wrong_plan_test", func(test *plan.Test, _ *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			test.TimeoutSeconds = 89
		}},
		{"wrong_version", func(test *plan.Test, _ *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			test.Version = 1
		}},
		{"too_short_timeout", func(test *plan.Test, _ *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			test.TimeoutSeconds = 1
		}},
		{"duplicate_planned_refs", func(test *plan.Test, p *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			test.ComponentIDs = append(test.ComponentIDs, test.ComponentIDs[0])
			for i := range p.Tests {
				if p.Tests[i].ID == test.ID {
					p.Tests[i] = *test
				}
			}
		}},
		{"missing_planned_graph_component", func(_ *plan.Test, p *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			p.HardwareGraph.Components = nil
		}},
		{"duplicate_final_test", func(test *plan.Test, _ *plan.Plan, p *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			p.Tests = append(p.Tests, *test)
		}},
		{"final_old_definition", func(_ *plan.Test, _ *plan.Plan, p *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			for i := range p.Tests {
				if p.Tests[i].ID == "input.touchpad.quick" {
					p.Tests[i].Version = 1
				}
			}
		}},
		{"final_excluded", func(_ *plan.Test, _ *plan.Plan, p *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			for i := range p.Tests {
				if p.Tests[i].ID == "input.touchpad.quick" {
					p.Tests[i].Status = plan.TestExcluded
				}
			}
		}},
		{"changed_component_hmac", func(_ *plan.Test, _ *plan.Plan, p *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			p.HardwareGraph.Components[0].Value = "hmac-sha256:" + strings.Repeat("0", 64)
		}},
		{"changed_epoch", func(_ *plan.Test, _ *plan.Plan, p *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			p.HardwareGraph.KeyEpoch = "other"
		}},
		{"changed_algorithm", func(_ *plan.Test, _ *plan.Plan, p *plan.Plan, _ *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			p.HardwareGraph.Algorithm = "other"
		}},
		{"removed_candidate", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			s.Components = []inventory.Component{}
		}},
		{"added_candidate", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			c := commandRouteComponent()
			c.ID = "input:second"
			commandRouteSetAttribute(&c, "interface", "event5")
			s.Components = append(s.Components, c)
		}},
		{"replaced_interface", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			commandRouteSetAttribute(&s.Components[0], "interface", "event5")
		}},
		{"identity_changed", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			commandRouteSetAttribute(&s.Components[0], "vendor_id", "1235")
		}},
		{"identity_alias_conflict", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			commandRouteSetAttribute(&s.Components[0], "vendor", "1235")
		}},
		{"unknown_width", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			commandRouteSetAttribute(&s.Components[0], "bitmap_word_bits", "16")
		}},
		{"missing_msc", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			commandRouteSetAttribute(&s.Components[0], "msc_capabilities", "")
		}},
		{"hidden_extra_msc", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, s *inventory.Snapshot, _ *inputtest.TouchpadResult) {
			commandRouteSetAttribute(&s.Components[0], "msc_capabilities", "31")
		}},
		{"omitted_advertised_key", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, v *inputtest.TouchpadResult) {
			v.Devices[0].Capabilities.Keys = []uint16{272, 325, 330}
			v.Devices[0].Buttons = v.Devices[0].Buttons[:1]
		}},
		{"duplicate_observation", func(_ *plan.Test, _ *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, v *inputtest.TouchpadResult) {
			v.Devices = append(v.Devices, v.Devices[0])
		}},
		{"retained_pass_unplanned", func(test *plan.Test, p *plan.Plan, _ *plan.Plan, _ *inventory.Snapshot, v *inputtest.TouchpadResult) {
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
			frozen, snapshot, _ := commandRouteCurrentPlan(t, plan.ProfileQuick)
			final, _, _ := commandRouteCurrentPlan(t, plan.ProfileQuick)
			test, value := commandRouteTest(t, frozen), commandRouteValue(t)
			c.change(&test, &frozen, &final, &snapshot, &value)
			if err := validateTouchpadPlanEvidence(commandRouteEncode(t, value), test, frozen, final, snapshot); err == nil {
				t.Fatal("unbound functional observation accepted")
			}
		})
	}
}

func TestTouchpadFinalInputOwnershipAndOriginalHMACFacts(t *testing.T) {
	for _, name := range []string{"test_refs", "capability_refs", "graph", "exclusions", "component", "attributes", "key"} {
		t.Run(name, func(t *testing.T) {
			frozen, snapshot, key := commandRouteCurrentPlan(t, plan.ProfileQuick)
			frozen.Capabilities = append(frozen.Capabilities, plan.Capability{ID: "synthetic-owner", ComponentIDs: []string{"literal"}})
			frozen.ExcludedTests = []string{"literal-exclusion"}
			owned, view, secret, err := freezeTouchpadFinalInputs(frozen, snapshot, key)
			if err != nil {
				t.Fatal(err)
			}
			planBefore, snapshotBefore, keyBefore := commandRouteEncode(t, owned), commandRouteEncode(t, view), append([]byte(nil), secret...)
			callback := func() {
				switch name {
				case "test_refs":
					for i := range frozen.Tests {
						if frozen.Tests[i].ID == "input.touchpad.quick" {
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
			if !bytes.Equal(planBefore, commandRouteEncode(t, owned)) || !bytes.Equal(snapshotBefore, commandRouteEncode(t, view)) || !bytes.Equal(keyBefore, secret) {
				t.Fatal("callback changed owned authority")
			}
			graph, err := plan.ObserveHardwareGraph(view, secret, "route-fixture")
			if err != nil || !reflect.DeepEqual(graph, owned.HardwareGraph) {
				t.Fatal("owned original inventory/key graph changed")
			}
		})
	}
	t.Run("copy_does_not_normalize_duplicate_attributes", func(t *testing.T) {
		p, s, k := commandRouteCurrentPlan(t, plan.ProfileQuick)
		s.Components[0].Attributes = append(s.Components[0].Attributes, s.Components[0].Attributes[0])
		_, owned, _, err := freezeTouchpadFinalInputs(p, s, k)
		if err != nil || !reflect.DeepEqual(owned, s) {
			t.Fatal("copy normalized raw attributes")
		}
		if err := touchpadevidence.ValidateInventoryBinding(commandRouteValue(t), owned.Components); err == nil {
			t.Fatal("duplicate original attribute hidden")
		}
	})
	for _, length := range []int{0, 31, 33} {
		t.Run(fmt.Sprintf("key_length_%d", length), func(t *testing.T) {
			p, s, _ := commandRouteCurrentPlan(t, plan.ProfileQuick)
			owned, view, key, err := freezeTouchpadFinalInputs(p, s, make([]byte, length))
			if err == nil || owned.ID != "" || view.Components != nil || key != nil {
				t.Fatal("invalid key produced authority")
			}
		})
	}
}

func TestTouchpadFinalEvidenceReadbackIsExactAndOwned(t *testing.T) {
	for _, name := range []string{"same", "changed", "missing", "read_error", "wrapped_absence", "appeared", "absent", "absent_bytes", "callback_mutation", "nil_reader", "empty_present"} {
		t.Run(name, func(t *testing.T) {
			original := []byte(commandRouteTouchpadLiteral)
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
				if path != "fixed-touchpad.json" {
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
					return []byte(commandRouteTouchpadLiteral), nil
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
			err := recheckTouchpadEvidence("fixed-touchpad.json", original, present, reader)
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

func TestTouchpadCurrentCatalogRequiresDecodedExecutionPlan(t *testing.T) {
	for _, profile := range []plan.Profile{plan.ProfileQuick, plan.ProfileStandard} {
		t.Run(string(profile), func(t *testing.T) {
			current, _, _ := commandRouteCurrentPlan(t, profile)
			layout, err := commandLayoutForPlan(current, profile)
			wantSchema, wantWorkers := "hhsoft.pc-tester.quick-session/v9", commandRouteTouchpadQuickWorkers
			if profile == plan.ProfileStandard {
				wantSchema, wantWorkers = "hhsoft.pc-tester.quick-session/v10", commandRouteTouchpadStandardWorkers
			}
			if err != nil || !layout.touchpad || layout.schema != wantSchema || !reflect.DeepEqual(layout.workerIDs, strings.Fields(wantWorkers)) {
				t.Fatal("decoded current plan selected the wrong execution layout")
			}
			commandRouteChangeTest(&current, func(row *plan.Test) { row.Version = 1; row.Status = plan.TestUnsupported })
			if _, err := plan.Decode(commandRouteEncode(t, current)); err == nil {
				t.Fatal("historical typed layout authorized current execution")
			}
			historical, err := commandLayoutForPlan(current, profile)
			if err != nil || historical.touchpad {
				t.Fatal("historical typed selector was lost")
			}
		})
	}
}

type commandRouteMutationContext struct {
	context.Context
	mutate func()
}

func (c *commandRouteMutationContext) Err() error {
	if c.mutate != nil {
		f := c.mutate
		c.mutate = nil
		f()
	}
	return nil
}

func TestTouchpadFinalizerCopiesKeyBeforeContextCallback(t *testing.T) {
	directory := t.TempDir()
	backing := bytes.Repeat([]byte{0x27}, 32)
	snapshot := inventory.NewSnapshot([]inventory.Component{})
	frozen, err := plan.BuildWithSelection(snapshot, plan.ProfileQuick, "x86_64", backing, "owned-fixture", []string{"inspection.exterior.quick"})
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(directory, "plan.json")
	if err := os.WriteFile(planPath, commandRouteEncode(t, frozen), 0600); err != nil {
		t.Fatal("write owned plan")
	}
	arguments := []string{"-plan", planPath, "-fingerprint-key", "synthetic-key", "-key-epoch", "owned-fixture", "-evidence-dir", directory, "-output", "synthetic-report"}
	// This explicit current set keeps the key-ownership fixture independent of layout helpers.
	for _, id := range strings.Fields("audio.readiness.quick cpu.quick display.operator.quick graphics.quick input.keyboard.quick input.pointer.quick input.touchpad.quick input.readiness.quick inspection.exterior.quick memory.quick network.readiness.quick storage.readiness.quick usb.readiness.quick") {
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

type commandRouteEntropyReader struct {
	source   *bytes.Reader
	callback func()
	failure  error
	calls    int
}

func (r *commandRouteEntropyReader) Read(p []byte) (int, error) {
	r.calls++
	if r.callback != nil {
		r.callback()
	}
	if r.failure != nil {
		return 0, r.failure
	}
	return r.source.Read(p)
}

func TestTouchpadStandaloneSigningChecksRealCallbackBoundaries(t *testing.T) {
	for _, name := range []string{"success", "pre_cancel", "random_cancel", "now_cancel", "random_error", "nil_clock", "nil_context", "nil_entropy"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var admitted context.Context = ctx
			entropy := &commandRouteEntropyReader{source: bytes.NewReader(bytes.Repeat([]byte{0x61}, 64))}
			nowCalls := 0
			now := func() time.Time {
				nowCalls++
				if name == "now_cancel" {
					cancel()
				}
				return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			}
			var random io.Reader = entropy
			switch name {
			case "pre_cancel":
				cancel()
			case "random_cancel":
				entropy.callback = cancel
			case "random_error":
				entropy.failure = errors.New("synthetic entropy failure")
			case "nil_clock":
				now = nil
			case "nil_context":
				admitted = nil
			case "nil_entropy":
				random = nil
			}
			public, private, observed, err := touchpadSigningInputs(admitted, random, now)
			if name == "success" {
				if err != nil || len(public) != 32 || len(private) != 64 || observed.IsZero() || nowCalls != 1 {
					t.Fatal("complete owned signing inputs rejected")
				}
			} else {
				if err == nil || public != nil || private != nil || !observed.IsZero() {
					t.Fatal("uncertain signing inputs escaped")
				}
			}
			if name == "pre_cancel" && entropy.calls != 0 {
				t.Fatal("entropy acquired after cancellation")
			}
			if (name == "pre_cancel" || name == "random_cancel" || name == "random_error") && nowCalls != 0 {
				t.Fatal("clock callback after failed signing prerequisite")
			}
			if name == "now_cancel" && nowCalls != 1 {
				t.Fatal("clock witness did not execute exactly once")
			}
		})
	}
}

type commandRouteWriter func([]byte) (int, error)

func (write commandRouteWriter) Write(p []byte) (int, error) { return write(p) }

func TestTouchpadStandaloneOutputCancellationAndOwnership(t *testing.T) {
	for _, name := range []string{"success", "pre_cancel", "before_callback_cancel", "during_write_cancel", "after_close_cancel", "close_error", "cancel_and_close_error", "zero_write", "no_callback", "repeated_callback", "nil_destination", "caller_mutation"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			raw := []byte("independent bounded signed-output fixture")
			wanted := append([]byte(nil), raw...)
			var retained bytes.Buffer
			ownerCalls, writes := 0, 0
			destination := commandRouteWriter(func(p []byte) (int, error) {
				writes++
				if name == "zero_write" {
					return 0, nil
				}
				n, err := retained.Write(p)
				if name == "during_write_cancel" {
					cancel()
				}
				return n, err
			})
			closeFailure := errors.New("synthetic output close failure")
			owner := func(path string, _ io.Writer, produce func(io.Writer) error) error {
				ownerCalls++
				if path != "fixed-output" {
					t.Fatal("output path changed")
				}
				if name == "no_callback" {
					return nil
				}
				if name == "caller_mutation" {
					raw[0] = 'x'
				}
				if name == "before_callback_cancel" {
					cancel()
				}
				if name == "nil_destination" {
					_ = produce(nil)
					return nil
				}
				err := produce(destination)
				if name == "repeated_callback" {
					_ = produce(destination)
					return nil
				}
				if name == "after_close_cancel" || name == "cancel_and_close_error" {
					cancel()
				}
				if name == "close_error" || name == "cancel_and_close_error" {
					return errors.Join(err, closeFailure)
				}
				return err
			}
			if name == "pre_cancel" {
				cancel()
			}
			err := writeTouchpadFinalOutput(ctx, "fixed-output", io.Discard, owner, raw)
			positive := name == "success" || name == "caller_mutation"
			if (err == nil) != positive {
				t.Fatalf("output boundary: %v", err)
			}
			if name == "pre_cancel" {
				if ownerCalls != 0 || writes != 0 {
					t.Fatal("destination acquired after cancellation")
				}
				return
			}
			if ownerCalls != 1 {
				t.Fatal("destination owner called more than once")
			}
			if name == "during_write_cancel" || name == "after_close_cancel" || name == "cancel_and_close_error" || name == "close_error" || name == "repeated_callback" || positive {
				if !bytes.Equal(retained.Bytes(), wanted) || writes != 1 {
					t.Fatal("uncertain bytes were retried, lost or changed")
				}
			}
			if name == "before_callback_cancel" || name == "no_callback" || name == "nil_destination" {
				if retained.Len() != 0 || writes != 0 {
					t.Fatal("unadmitted output was written")
				}
			}
			if (name == "close_error" || name == "cancel_and_close_error") && !errors.Is(err, closeFailure) {
				t.Fatal("Close failure was masked")
			}
		})
	}
}
