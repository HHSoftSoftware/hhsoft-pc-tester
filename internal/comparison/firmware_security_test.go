package comparison

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"reflect"
	"strings"
	"testing"
)

func firmwareComparisonReport(t *testing.T, after bool) Report {
	t.Helper()
	boot, version := "1", "2"
	if after {
		boot, version = "0", "1"
	}
	payload, err := app.BuildInventoryPayload(inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"wwid": "fixture-exterior-disk", "model": "Fixture disk"}),
		inventory.NewComponent(inventory.ComponentType("firmware_security"), "firmware_security:uefi", map[string]string{"secure_boot": boot, "secure_boot_state": "reported", "setup_mode_flag": "1", "setup_mode_flag_state": "reported", "uefi_platform_bits": "64", "uefi_platform_bits_state": "reported"}),
		inventory.NewComponent(inventory.ComponentType("tpm"), "tpm:1", map[string]string{"version_major": version, "version_major_state": "reported"}),
	}), bytes.Repeat([]byte{0x39}, 32), "power-fixture")
	if err != nil {
		t.Fatal(err)
	}
	want := app.SystemFingerprint{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", KeyID: "hmac-sha256:ab836abf4fbfd5f5bbe333c0b65affd2ee378235d766d8f8f89e51225a543f44", Value: "hmac-sha256:7143984e0e469197bed406f4fa5265603622d2d8a0552a999d084b767c51e449", IdentifiedParts: 1, UnidentifiedParts: 2}
	if payload.SystemFingerprint != want || len(payload.Components) != 3 || payload.Components[0].Identity.Value != "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af" {
		t.Fatal("independent identity prerequisite")
	}
	for _, i := range []int{1, 2} {
		if payload.Components[i].Identity != (app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) {
			t.Fatal("new component identified")
		}
	}
	ref := ReportReference{SessionID: "11111111-1111-4111-8111-111111111111", GeneratedTime: "2026-01-01T00:00:00Z", SoftwareVersion: "0.0.1-fixture", SignerKeyID: "firmware-signer", ManifestDigest: "sha256:" + strings.Repeat("1", 64), SystemFingerprint: want, ComponentCount: 3}
	if after {
		ref.SessionID = "22222222-2222-4222-8222-222222222222"
		ref.GeneratedTime = "2026-01-02T00:00:00Z"
		ref.ManifestDigest = "sha256:" + strings.Repeat("2", 64)
	}
	r := Report{Reference: ref, Inventory: payload}
	if err := validateReport(r); err != nil {
		t.Fatal("valid report prerequisite", err)
	}
	return r
}
func firmwareComparisonRows(boot, version string) []Component {
	return []Component{
		{Type: inventory.ComponentType("firmware_security"), ID: "firmware_security:uefi", IdentityConfidence: "unavailable", Attributes: []inventory.Attribute{{Name: "secure_boot", Value: boot}, {Name: "secure_boot_state", Value: "reported"}, {Name: "setup_mode_flag", Value: "1"}, {Name: "setup_mode_flag_state", Value: "reported"}, {Name: "uefi_platform_bits", Value: "64"}, {Name: "uefi_platform_bits_state", Value: "reported"}}},
		{Type: inventory.ComponentType("tpm"), ID: "tpm:1", IdentityConfidence: "unavailable", Attributes: []inventory.Attribute{{Name: "version_major", Value: version}, {Name: "version_major_state", Value: "reported"}}},
	}
}
func TestFirmwareSecurityComparisonRetainsBothConfigurationsWithoutIdentity(t *testing.T) {
	before, after := firmwareComparisonReport(t, false), firmwareComparisonReport(t, true)
	original, _ := json.Marshal([]Report{before, after})
	control := Component{Type: inventory.ComponentBlock, ID: "block:sda", IdentitySource: "wwid", Identity: "hmac-sha256:166124c103a9f3460b95772ccc28cbdb015931924d8d021ebed574af73df01af", IdentityConfidence: "durable", Attributes: []inventory.Attribute{{Name: "model", Value: "Fixture disk"}}}
	left, right := firmwareComparisonRows("1", "2"), firmwareComparisonRows("0", "1")
	want := Result{Schema: "hhsoft.pc-tester.hardware-comparison/v1", Policy: "hhsoft.parts-swap.v1", Status: StatusInconclusive, Comparable: true, Complete: false, Before: before.Reference, After: after.Reference, Summary: Summary{Matched: 1, Unresolved: 2}, Matched: []Match{{Before: control, After: control}}, Unresolved: []UnresolvedChange{
		{Before: []Component{left[0]}, After: []Component{right[0]}, Reason: "stable identity is unavailable at the same observed location; continuity or replacement cannot be proven"},
		{Before: []Component{left[1]}, After: []Component{right[1]}, Reason: "stable identity is unavailable at the same observed location; continuity or replacement cannot be proven"},
	}}
	got, err := Compare(before, after)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("finite configurations or references lost", err, got)
	}
	final, _ := json.Marshal([]Report{before, after})
	if !bytes.Equal(original, final) {
		t.Fatal("comparison mutated input")
	}
}
func TestFirmwareSecurityObservationKeysRetainEveryNewAttribute(t *testing.T) {
	for _, tc := range []struct {
		typ  inventory.ComponentType
		name string
	}{
		{"firmware_security", "secure_boot"}, {"firmware_security", "secure_boot_state"}, {"firmware_security", "setup_mode_flag"}, {"firmware_security", "setup_mode_flag_state"}, {"firmware_security", "uefi_platform_bits"}, {"firmware_security", "uefi_platform_bits_state"}, {"tpm", "version_major"}, {"tpm", "version_major_state"}, {"firmware_security", "future_field"}, {"tpm", "future_field"},
	} {
		t.Run(string(tc.typ)+"/"+tc.name, func(t *testing.T) {
			a := Component{Type: tc.typ, ID: "fixture"}
			b := a
			b.Attributes = []inventory.Attribute{{Name: tc.name, Value: "changed"}}
			if observationKeyFor(a) == observationKeyFor(b) {
				t.Fatal("configuration became volatile")
			}
			a.Type = inventory.ComponentCPU
			b.Type = inventory.ComponentCPU
			if observationKeyFor(a) == observationKeyFor(b) {
				t.Fatal("old component policy widened")
			}
		})
	}
}
func TestFirmwareSecurityComparisonRejectsForgedIdentities(t *testing.T) {
	for _, index := range []int{1, 2} {
		for _, fault := range []string{"source_without_value", "confidence", "serial", "wwid", "wrong_epoch", "wrong_algorithm"} {
			t.Run(fmt.Sprintf("%d/%s", index, fault), func(t *testing.T) {
				for _, side := range []string{"before", "after"} {
					before, after := firmwareComparisonReport(t, false), firmwareComparisonReport(t, true)
					if got, err := Compare(before, after); err != nil || got.Summary.Unresolved != 2 || got.Summary.Matched != 1 {
						t.Fatal("valid prerequisite", err)
					}
					target := &before
					if side == "after" {
						target = &after
					}
					r := &target.Inventory.Components[index].Identity
					switch fault {
					case "source_without_value":
						r.Source = "serial"
					case "confidence":
						r.Confidence = "durable"
					case "wrong_epoch":
						r.KeyEpoch = "other"
					case "wrong_algorithm":
						r.Algorithm = "sha256"
					default:
						r.Source = fault
						r.Value = "hmac-sha256:" + strings.Repeat("a", 64)
						r.Confidence = "durable"
						target.Inventory.SystemFingerprint.IdentifiedParts = 2
						target.Inventory.SystemFingerprint.UnidentifiedParts = 1
						target.Reference.SystemFingerprint = target.Inventory.SystemFingerprint
					}
					if got, err := Compare(before, after); err == nil || !reflect.DeepEqual(got, Result{}) {
						t.Fatal("forged new-type identity admitted", side)
					}
				}
			})
		}
	}
}
