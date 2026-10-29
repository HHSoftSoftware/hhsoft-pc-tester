package comparison

import (
	"bytes"
	"encoding/json"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"reflect"
	"strings"
	"testing"
)

func memoryComparisonAttributes(capacity string) []inventory.Attribute {
	return []inventory.Attribute{{Name: "capacity_bytes", Value: capacity}, {Name: "capacity_bytes_state", Value: "reported"}, {Name: "form_factor", Value: "dimm"}, {Name: "form_factor_state", Value: "reported"}, {Name: "memory_type", Value: "ddr5"}, {Name: "memory_type_state", Value: "reported"}, {Name: "observation_source", Value: "smbios_type17"}}
}
func memoryComparisonReport(t *testing.T, after bool) Report {
	t.Helper()
	capacity := "17179869184"
	if after {
		capacity = "8589934592"
	}
	payload, err := app.BuildInventoryPayload(inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"wwid": "fixture-exterior-disk", "model": "Fixture disk"}),
		{Type: inventory.ComponentType("memory_device"), ID: "memory_device:1", Attributes: memoryComparisonAttributes(capacity)},
	}), bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := app.SystemFingerprint{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", KeyID: "hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44", Value: "hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449", IdentifiedParts: 1, UnidentifiedParts: 1}
	if payload.SystemFingerprint != fingerprint || len(payload.Components) != 2 || payload.Components[0].Identity.Value != "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af" || payload.Components[1].Identity != (app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) {
		t.Fatal("literal identity/count prerequisite")
	}
	ref := ReportReference{SessionID: "11111111-1111-4111-8111-111111111111", GeneratedTime: "2026-01-01T00:00:00Z", SoftwareVersion: "0.0.1-fixture", SignerKeyID: "memory-signer", ManifestDigest: "sha256:" + strings.Repeat("1", 64), SystemFingerprint: fingerprint, ComponentCount: 2}
	if after {
		ref.SessionID = "22222222-2222-4222-8222-222222222222"
		ref.GeneratedTime = "2026-01-02T00:00:00Z"
		ref.ManifestDigest = "sha256:" + strings.Repeat("2", 64)
	}
	report := Report{Reference: ref, Inventory: payload}
	if err := validateReport(report); err != nil {
		t.Fatal("valid report prerequisite", err)
	}
	return report
}
func TestMemoryDeviceComparisonRetainsBothObservationsWithoutContinuity(t *testing.T) {
	before, after := memoryComparisonReport(t, false), memoryComparisonReport(t, true)
	original, _ := json.Marshal([]Report{before, after})
	control := Component{Type: inventory.ComponentBlock, ID: "block:sda", IdentitySource: "wwid", Identity: "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af", IdentityConfidence: "durable", Attributes: []inventory.Attribute{{Name: "model", Value: "Fixture disk"}}}
	left := Component{Type: inventory.ComponentType("memory_device"), ID: "memory_device:1", IdentityConfidence: "unavailable", Attributes: memoryComparisonAttributes("17179869184")}
	right := Component{Type: inventory.ComponentType("memory_device"), ID: "memory_device:1", IdentityConfidence: "unavailable", Attributes: memoryComparisonAttributes("8589934592")}
	want := Result{Schema: "hhsoft.pc-tester.hardware-comparison/v1", Policy: "hhsoft.parts-swap.v1", Status: StatusInconclusive, Comparable: true, Complete: false, Before: before.Reference, After: after.Reference, Summary: Summary{Matched: 1, Unresolved: 1}, Matched: []Match{{Before: control, After: control}}, Unresolved: []UnresolvedChange{{Before: []Component{left}, After: []Component{right}, Reason: "stable identity is unavailable at the same observed location; continuity or replacement cannot be proven"}}}
	got, err := Compare(before, after)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("finite observations/references lost", err, got)
	}
	final, _ := json.Marshal([]Report{before, after})
	if !bytes.Equal(original, final) {
		t.Fatal("input mutated")
	}
}
func TestMemoryDeviceEveryObservationAttributeStaysNonvolatile(t *testing.T) {
	for _, name := range []string{"capacity_bytes", "capacity_bytes_state", "form_factor", "form_factor_state", "memory_type", "memory_type_state", "observation_source", "future_field"} {
		t.Run(name, func(t *testing.T) {
			a := Component{Type: inventory.ComponentType("memory_device"), ID: "memory_device:1"}
			b := a
			b.Attributes = []inventory.Attribute{{Name: name, Value: "changed"}}
			if observationKeyFor(a) == observationKeyFor(b) {
				t.Fatal("new attribute silently volatile")
			}
			a.Type = inventory.ComponentCPU
			b.Type = inventory.ComponentCPU
			if observationKeyFor(a) == observationKeyFor(b) {
				t.Fatal("old policy widened")
			}
		})
	}
}
func TestMemoryDeviceComparisonRejectsForgedIdentity(t *testing.T) {
	for _, fault := range []string{"source_without_value", "confidence", "serial", "wwid", "wrong_epoch", "wrong_algorithm"} {
		t.Run(fault, func(t *testing.T) {
			for _, side := range []string{"before", "after"} {
				before, after := memoryComparisonReport(t, false), memoryComparisonReport(t, true)
				if got, err := Compare(before, after); err != nil || got.Summary.Unresolved != 1 || got.Summary.Matched != 1 {
					t.Fatal("positive prerequisite", err)
				}
				target := &before
				if side == "after" {
					target = &after
				}
				identity := &target.Inventory.Components[1].Identity
				switch fault {
				case "source_without_value":
					identity.Source = "serial"
				case "confidence":
					identity.Confidence = "durable"
				case "wrong_epoch":
					identity.KeyEpoch = "other"
				case "wrong_algorithm":
					identity.Algorithm = "sha256"
				default:
					identity.Source = fault
					identity.Value = "hmac-sha256:" + strings.Repeat("a", 64)
					identity.Confidence = "durable"
					target.Inventory.SystemFingerprint.IdentifiedParts = 2
					target.Inventory.SystemFingerprint.UnidentifiedParts = 0
					target.Reference.SystemFingerprint = target.Inventory.SystemFingerprint
				}
				if got, err := Compare(before, after); err == nil || !reflect.DeepEqual(got, Result{}) {
					t.Fatal("forged memory identity accepted", side)
				}
			}
		})
	}
}
