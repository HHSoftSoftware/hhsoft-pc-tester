package render

import (
	"bytes"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
)

func TestMemoryViewsRequireSharedValidatedCoverage(t *testing.T) {
	for _, name := range []string{"memory-v3-direct-pass", "session-v5-memory-v3-local-heat"} {
		p := thermalReportFixture(t, name)
		for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
			raw, err := render(p)
			if err != nil || !bytes.Contains(bytes.ToLower(raw), []byte("ram pattern and inversion observations")) || !bytes.Contains(raw, []byte("byte-patterns-aa55-inversions-v1")) {
				t.Fatal("memory summary missing", err)
			}
		}
		for index := range p.TestResults {
			if p.TestResults[index].ID == "memory.quick" {
				p.TestResults[index].Version = 1
			}
		}
		if _, err := InventoryHTML(p); err == nil {
			t.Fatal("relabelled memory rendered")
		}
		if _, err := InventoryPDF(p); err == nil {
			t.Fatal("relabelled memory PDF rendered")
		}
	}
	view, err := memoryReportDocument(thermalReportFixture(t, "memory-v3-direct-pass"))
	if err != nil || view.AcceptedReads != 25165824 || view.AcceptedWrites != 25165824 || view.AttemptedReads != 25165824 || view.CompletedPhases != 10 {
		t.Fatal("logical counts changed", err)
	}
	for _, name := range []string{"session-v4-local-heat", "session-v5-keyboard-pass"} {
		if view, err := memoryReportDocument(thermalReportFixture(t, name)); err != nil || view != nil {
			t.Fatal("historical memory acquired inversion coverage", err)
		}
	}
}
