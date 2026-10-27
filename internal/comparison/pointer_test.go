package comparison

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
)

func pointerComparisonFixture(t *testing.T, name string) Report {
	t.Helper()
	encoded, err := os.ReadFile("../app/testdata/" + name + ".json")
	if err != nil {
		t.Fatal("synthetic pointer comparison fixture unavailable")
	}
	var payload app.InventoryPayload
	if json.Unmarshal(encoded, &payload) != nil {
		t.Fatal("synthetic pointer comparison fixture invalid")
	}
	return Report{Inventory: payload, Reference: ReportReference{SessionID: "synthetic", GeneratedTime: "2026-10-26T00:00:00Z", SoftwareVersion: "0.1.0-dev.0", SignerKeyID: "synthetic", ManifestDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111", SystemFingerprint: payload.SystemFingerprint, ComponentCount: len(payload.Components)}}
}

func TestComparisonValidatesStandaloneAndSessionPointerEvidence(t *testing.T) {
	for _, name := range []string{"pointer-v1-standalone-pass", "pointer-v2-standalone-pass", "session-v5-pointer-v2-pass"} {
		before := pointerComparisonFixture(t, name)
		if _, err := Compare(before, before); err != nil {
			t.Fatalf("valid pointer report rejected: %v", err)
		}
		for _, defect := range []string{"definition", "inventory", "duplicate wheel field"} {
			after := pointerComparisonFixture(t, name)
			for index := range after.Inventory.TestResults {
				row := &after.Inventory.TestResults[index]
				if row.ID != "input.pointer.quick" {
					continue
				}
				switch defect {
				case "definition":
					row.Version = 3
				case "inventory":
					after.Inventory.Components[0].ID = "event1"
					after.Inventory.Components[0].Attributes = nil
				case "duplicate wheel field":
					if name == "pointer-v1-standalone-pass" {
						row.Evidence = bytes.Replace(row.Evidence, []byte(`"schema":`), []byte(`"wheels":[],"schema":`), 1)
					} else {
						row.Evidence = bytes.Replace(row.Evidence, []byte(`"wheels":`), []byte(`"wheels":[],"wheels":`), 1)
					}
				}
			}
			if _, err := Compare(before, after); err == nil {
				t.Fatal("comparison accepted forged pointer evidence")
			}
		}
	}
}
