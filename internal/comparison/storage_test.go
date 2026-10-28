package comparison

import (
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

func TestComparisonValidatesStorageWithoutCreatingDurableIdentity(t *testing.T) {
	before := pointerComparisonFixture(t, "storage-sample-standalone-pass-v1")
	value, err := Compare(before, before)
	if err != nil {
		t.Fatal("valid sampled report rejected", err)
	}
	if value.Complete || value.Status != StatusInconclusive || value.Summary.Matched != 0 || value.Summary.Replaced != 0 || value.Summary.Unresolved != 1 {
		t.Fatal("sampled metadata became durable continuity evidence")
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) == 0 {
		t.Fatal("comparison unavailable", err)
	}
	if before.Inventory.Components[0].Identity.Value != "" {
		t.Fatal("sampling invented a content identity")
	}
	for _, defect := range []string{"definition", "number", "capacity", "cleanup", "accepted total"} {
		after := pointerComparisonFixture(t, "storage-sample-standalone-pass-v1")
		switch defect {
		case "definition":
			after.Inventory.TestResults[0].Version = 1
		case "number":
			after.Inventory.Components[0].Attributes[0].Value = "8:1"
		case "capacity":
			after.Inventory.Components[0].Attributes[4].Value = "129"
		default:
			var r storagetest.Result
			if json.Unmarshal(after.Inventory.TestResults[0].Evidence, &r) != nil {
				t.Fatal("fixture")
			}
			if defect == "cleanup" {
				r.CleanupConfirmed = false
				r.Status = "error"
				r.StopReason = "cleanup_unconfirmed"
				r.Error = "cleanup_unconfirmed"
				after.Inventory.TestResults[0].Status = "error"
			} else {
				r.AcceptedReadBytes++
			}
			after.Inventory.TestResults[0].Evidence, _ = json.Marshal(r)
		}
		if _, err := Compare(before, after); err == nil {
			t.Fatal("invalid storage evidence compared", defect)
		}
	}
}
