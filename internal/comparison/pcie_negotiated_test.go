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

func pcieComparisonReport(t *testing.T, after bool, ids, addresses []string) Report {
	t.Helper()
	var parts []inventory.Component
	for i, id := range ids {
		attrs := map[string]string{"address": addresses[i], "vendor_id": "0x1234", "max_link_speed_state": "reported", "max_link_speed_mts": "32000", "current_link_speed_state": "reported", "current_link_speed_mts": "2500", "current_link_width_state": "reported", "current_link_width_lanes": "1"}
		if after {
			attrs["current_link_speed_state"] = "read_error"
			delete(attrs, "current_link_speed_mts")
			attrs["current_link_width_lanes"] = "16"
		}
		parts = append(parts, inventory.NewComponent(inventory.ComponentPCI, id, attrs))
	}
	payload, err := app.BuildInventoryPayload(inventory.NewSnapshot(parts), bytes.Repeat([]byte{0x39}, 32), "pcie-fixture")
	if err != nil {
		t.Fatal(err)
	}
	reference := ReportReference{SessionID: "11111111-1111-4111-8111-111111111111", GeneratedTime: "2026-01-01T00:00:00Z", SoftwareVersion: "0.0.1-fixture", SignerKeyID: "pcie-signer", ManifestDigest: "sha256:" + strings.Repeat("1", 64), SystemFingerprint: payload.SystemFingerprint, ComponentCount: len(parts)}
	if after {
		reference.SessionID = "22222222-2222-4222-8222-222222222222"
		reference.GeneratedTime = "2026-01-02T00:00:00Z"
		reference.ManifestDigest = "sha256:" + strings.Repeat("2", 64)
	}
	r := Report{Reference: reference, Inventory: payload}
	if err := validateReport(r); err != nil {
		t.Fatal("valid PCI report prerequisite", err)
	}
	return r
}

func TestPCIeNegotiatedComparisonPreservesObservationsWithoutIdentity(t *testing.T) {
	for _, tc := range []struct {
		name                                                 string
		beforeIDs, beforeAddresses, afterIDs, afterAddresses []string
		unresolved                                           int
	}{
		{"same_location", []string{"pci:a"}, []string{"a"}, []string{"pci:a"}, []string{"a"}, 1},
		{"address_and_id_changed", []string{"pci:a"}, []string{"a"}, []string{"pci:b"}, []string{"b"}, 2},
		// Same attributes with different IDs is synthetic grouping evidence only.
		{"synthetic_same_attributes", []string{"pci:a"}, []string{"a"}, []string{"pci:b"}, []string{"a"}, 1},
		{"repeated_records_and_count_change", []string{"pci:a", "pci:b"}, []string{"a", "a"}, []string{"pci:c"}, []string{"a"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, after := pcieComparisonReport(t, false, tc.beforeIDs, tc.beforeAddresses), pcieComparisonReport(t, true, tc.afterIDs, tc.afterAddresses)
			input, _ := json.Marshal([]Report{before, after})
			got, err := Compare(before, after)
			if err != nil || !got.Comparable || got.Complete || got.Status != StatusInconclusive || got.Summary.Matched != 0 || got.Summary.Replaced != 0 || got.Summary.Moved != 0 || got.Summary.Unresolved != tc.unresolved {
				t.Fatal("PCI telemetry acquired continuity/replacement credit", err, got.Summary)
			}
			if !reflect.DeepEqual(got.Before, before.Reference) || !reflect.DeepEqual(got.After, after.Reference) {
				t.Fatal("authenticated report references changed")
			}
			var left, right []Component
			for _, row := range got.Unresolved {
				left = append(left, row.Before...)
				right = append(right, row.After...)
			}
			if !reflect.DeepEqual(left, components(before.Inventory.Components)) || !reflect.DeepEqual(right, components(after.Inventory.Components)) {
				t.Fatal("comparison lost original before/after attributes")
			}
			final, _ := json.Marshal([]Report{before, after})
			if !bytes.Equal(input, final) {
				t.Fatal("comparison rewrote source observations")
			}
		})
	}
}

func TestPCIeNegotiatedComparisonProjectionIsFinite(t *testing.T) {
	for _, name := range []string{"current_link_speed_state", "current_link_speed_mts", "current_link_width_state", "current_link_width_lanes", "current_link_speed_mts_extra", "current_other", "max_link_speed_state", "max_link_speed_mts", "max_link_width_state", "max_link_width_lanes", "address", "vendor_id"} {
		for _, kind := range []inventory.ComponentType{inventory.ComponentPCI, inventory.ComponentCPU} {
			before := Component{Type: kind, ID: "fixture"}
			after := before
			after.Attributes = []inventory.Attribute{{Name: name, Value: "changed"}}
			excluded := kind == inventory.ComponentPCI && (name == "current_link_speed_state" || name == "current_link_speed_mts" || name == "current_link_width_state" || name == "current_link_width_lanes")
			if (observationKeyFor(before) == observationKeyFor(after)) != excluded {
				t.Fatal("finite/type-scoped comparison projection changed", kind, name)
			}
		}
	}
}

func TestPCIeNegotiatedComparisonRejectsInventedDurableIdentity(t *testing.T) {
	before := pcieComparisonReport(t, false, []string{"pci:a"}, []string{"a"})
	after := pcieComparisonReport(t, true, []string{"pci:a"}, []string{"a"})
	if _, err := Compare(before, after); err != nil {
		t.Fatal(err)
	}
	after.Inventory.Components[0].Identity = app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "pcie-fixture", Source: "serial", Value: "hmac-sha256:" + strings.Repeat("a", 64), Confidence: "durable"}
	after.Inventory.SystemFingerprint.IdentifiedParts = 1
	after.Inventory.SystemFingerprint.UnidentifiedParts = 0
	after.Reference.SystemFingerprint = after.Inventory.SystemFingerprint
	if result, err := Compare(before, after); err == nil || !reflect.DeepEqual(result, Result{}) {
		t.Fatal("invented PCI identity accepted")
	}
}
