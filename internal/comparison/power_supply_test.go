package comparison

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func powerComparisonReport(t *testing.T, after bool) Report {
	t.Helper()
	capacity, state := "50", "reported"
	if after {
		capacity, state = "", "unavailable"
	}
	attributes := map[string]string{"supply_type": "Battery", "supply_type_state": "reported", "scope": "System", "scope_state": "reported", "capacity_percent_state": state}
	if capacity != "" {
		attributes["capacity_percent"] = capacity
	}
	payload, err := app.BuildInventoryPayload(inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"wwid": "fixture-exterior-disk", "model": "Fixture disk"}),
		inventory.NewComponent(inventory.ComponentType("power_supply"), "power_supply:1", attributes),
	}), bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	want := app.SystemFingerprint{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", KeyID: "hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44", Value: "hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449", IdentifiedParts: 1, UnidentifiedParts: 1}
	if payload.SystemFingerprint != want || len(payload.Components) != 2 || payload.Components[0].Identity.Value != "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af" || payload.Components[1].Identity != (app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) {
		t.Fatal("literal identity prerequisite")
	}
	ref := ReportReference{SessionID: "11111111-1111-4111-8111-111111111111", GeneratedTime: "2026-01-01T00:00:00Z", SoftwareVersion: "0.0.1-fixture", SignerKeyID: "power-signer", ManifestDigest: "sha256:" + strings.Repeat("1", 64), SystemFingerprint: want, ComponentCount: 2}
	if after {
		ref.SessionID = "22222222-2222-4222-8222-222222222222"
		ref.GeneratedTime = "2026-01-02T00:00:00Z"
		ref.ManifestDigest = "sha256:" + strings.Repeat("2", 64)
	}
	r := Report{Reference: ref, Inventory: payload}
	if err := validateReport(r); err != nil {
		t.Fatal("valid complete report prerequisite", err)
	}
	return r
}

func powerComparisonExpected(before, after Report) Result {
	control := Component{Type: inventory.ComponentBlock, ID: "block:sda", IdentitySource: "wwid", Identity: "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af", IdentityConfidence: "durable", Attributes: []inventory.Attribute{{Name: "model", Value: "Fixture disk"}}}
	left := Component{Type: inventory.ComponentType("power_supply"), ID: "power_supply:1", IdentityConfidence: "unavailable", Attributes: []inventory.Attribute{{Name: "capacity_percent", Value: "50"}, {Name: "capacity_percent_state", Value: "reported"}, {Name: "scope", Value: "System"}, {Name: "scope_state", Value: "reported"}, {Name: "supply_type", Value: "Battery"}, {Name: "supply_type_state", Value: "reported"}}}
	right := Component{Type: inventory.ComponentType("power_supply"), ID: "power_supply:1", IdentityConfidence: "unavailable", Attributes: []inventory.Attribute{{Name: "capacity_percent_state", Value: "unavailable"}, {Name: "scope", Value: "System"}, {Name: "scope_state", Value: "reported"}, {Name: "supply_type", Value: "Battery"}, {Name: "supply_type_state", Value: "reported"}}}
	return Result{Schema: "hhsoft.pc-tester.hardware-comparison/v1", Policy: "hhsoft.parts-swap.v1", Status: StatusInconclusive, Comparable: true, Complete: false, Before: before.Reference, After: after.Reference, Summary: Summary{Matched: 1, Unresolved: 1}, Matched: []Match{{Before: control, After: control, LocationChange: false}}, Unresolved: []UnresolvedChange{{Before: []Component{left}, After: []Component{right}, Reason: "stable identity is unavailable at the same observed location; continuity or replacement cannot be proven"}}}
}

func TestPowerSupplyComparisonKeepsUnresolvedRowsAndExactReferences(t *testing.T) {
	before, after := powerComparisonReport(t, false), powerComparisonReport(t, true)
	original, _ := json.Marshal([]Report{before, after})
	got, err := Compare(before, after)
	if err != nil || !reflect.DeepEqual(got, powerComparisonExpected(before, after)) {
		t.Fatal("power telemetry acquired continuity or lost exact report references", err, got)
	}
	final, _ := json.Marshal([]Report{before, after})
	if !bytes.Equal(original, final) {
		t.Fatal("comparison mutated its inputs")
	}
	// These literal names are the public field contract, not a producer list.
	for _, name := range []string{"supply_type", "scope", "present", "online", "status", "capacity_percent", "cycle_count", "energy_full_uwh", "energy_full_design_uwh", "charge_full_uah", "charge_full_design_uah"} {
		for _, attribute := range []string{name, name + "_state"} {
			base := Component{Type: inventory.ComponentType("power_supply"), ID: "power_supply:1"}
			changed := base
			changed.Attributes = []inventory.Attribute{{Name: attribute, Value: "changed"}}
			if observationKeyFor(base) != observationKeyFor(changed) {
				t.Fatal("declared power observation changed its comparison projection", attribute)
			}
			// A CPU did not previously discard these power-specific names.
			base.Type = inventory.ComponentCPU
			changed.Type = inventory.ComponentCPU
			if observationKeyFor(base) == observationKeyFor(changed) {
				t.Fatal("new power volatility leaked into CPU policy", attribute)
			}
		}
	}
	base := Component{Type: inventory.ComponentType("power_supply"), ID: "power_supply:1"}
	changed := base
	changed.Attributes = []inventory.Attribute{{Name: "future_attribute", Value: "kept"}}
	if observationKeyFor(base) == observationKeyFor(changed) {
		t.Fatal("undeclared power metadata was silently discarded")
	}
}

func TestPowerSupplyComparisonRejectsForgedIdentityAfterValidReports(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Report)
	}{
		{"source_without_identity", func(r *Report) { r.Inventory.Components[1].Identity.Source = "serial" }},
		{"identifying_confidence", func(r *Report) { r.Inventory.Components[1].Identity.Confidence = "durable" }},
		{"invented_serial_identity", func(r *Report) {
			p := &r.Inventory.Components[1]
			p.Identity.Source = "serial"
			p.Identity.Value = "hmac-sha256:" + strings.Repeat("a", 64)
			p.Identity.Confidence = "durable"
			r.Inventory.SystemFingerprint.IdentifiedParts = 2
			r.Inventory.SystemFingerprint.UnidentifiedParts = 0
			r.Reference.SystemFingerprint = r.Inventory.SystemFingerprint
		}},
		{"invented_wwid_identity", func(r *Report) {
			p := &r.Inventory.Components[1]
			p.Identity.Source = "wwid"
			p.Identity.Value = "hmac-sha256:" + strings.Repeat("b", 64)
			p.Identity.Confidence = "durable"
			r.Inventory.SystemFingerprint.IdentifiedParts = 2
			r.Inventory.SystemFingerprint.UnidentifiedParts = 0
			r.Reference.SystemFingerprint = r.Inventory.SystemFingerprint
		}},
		{"wrong_algorithm", func(r *Report) { r.Inventory.Components[1].Identity.Algorithm = "sha256" }},
		{"wrong_epoch", func(r *Report) { r.Inventory.Components[1].Identity.KeyEpoch = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, side := range []string{"before", "after"} {
				before, after := powerComparisonReport(t, false), powerComparisonReport(t, true)
				if got, err := Compare(before, after); err != nil || !reflect.DeepEqual(got, powerComparisonExpected(before, after)) {
					t.Fatal("valid comparison prerequisite", err)
				}
				target := &after
				if side == "before" {
					target = &before
				}
				prior, _ := json.Marshal(target)
				test.mutate(target)
				changed, _ := json.Marshal(target)
				if bytes.Equal(prior, changed) {
					t.Fatal("ineffective identity fault")
				}
				if got, err := Compare(before, after); err == nil || !reflect.DeepEqual(got, Result{}) {
					t.Fatal("forged power identity accepted", side, err)
				}
			}
		})
	}
}
