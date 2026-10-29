package plan

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestCPUStressStandardOnlyFrozenApplicability(t *testing.T) {
	for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
		t.Run(architecture, func(t *testing.T) {
			snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil)})
			for _, profile := range []Profile{ProfileQuick, ProfileStandard, ProfileExtended, ProfileDestructive} {
				value, err := Build(snapshot, profile, architecture, bytes.Repeat([]byte{0x31}, 32), "cpu-fixture")
				if err != nil {
					t.Fatal(err)
				}
				want := TestExcluded
				if profile == ProfileStandard {
					want = TestScheduled
				}
				assertTestStatus(t, value, "cpu.stress", want)
				if len(value.Tests) != 22 {
					t.Fatal("catalog membership changed")
				}
				found := false
				for _, row := range value.Tests {
					if row.ID == "cpu.stress" {
						found = true
						if row.Version != 1 || row.EstimatedSeconds != 600 || row.TimeoutSeconds != 660 {
							t.Fatal("stress limits changed")
						}
					}
				}
				if !found {
					t.Fatal("stress row absent")
				}
				raw, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := Decode(raw)
				if err != nil || decoded.ID != value.ID {
					t.Fatal("current plan did not round trip")
				}
			}
		})
	}
}

func TestCPUStressCannotBeSelectedThroughQuickExclusions(t *testing.T) {
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil)})
	for _, profile := range []Profile{ProfileQuick, ProfileStandard} {
		if _, err := BuildWithSelection(snapshot, profile, "x86_64", bytes.Repeat([]byte{0x31}, 32), "cpu-fixture", []string{"cpu.stress"}); err == nil {
			t.Fatal("quick selection transport accepted sustained-load override")
		}
	}
}
