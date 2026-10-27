package render

import (
	"bytes"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
)

func TestCPUScalarViewsUseValidatedSummary(t *testing.T) {
	for _, name := range []string{"cpu-v3-direct-pass", "session-v5-cpu-v3-local-heat"} {
		p := thermalReportFixture(t, name)
		for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
			raw, err := render(p)
			if err != nil || !bytes.Contains(bytes.ToLower(raw), []byte("cpu scalar calculation observations")) || !bytes.Contains(raw, []byte("binary64-normal-exact-v1")) {
				t.Fatal("scalar summary missing", err)
			}
		}
		for index := range p.TestResults {
			if p.TestResults[index].ID == "cpu.quick" {
				p.TestResults[index].Version = 1
			}
		}
		if _, err := InventoryHTML(p); err == nil {
			t.Fatal("relabelled scalar rendered")
		}
		if _, err := InventoryPDF(p); err == nil {
			t.Fatal("relabelled scalar PDF rendered")
		}
	}
	for _, name := range []string{"session-v4-local-heat", "session-v5-keyboard-pass"} {
		if view, err := cpuScalarReportDocument(thermalReportFixture(t, name)); err != nil || view != nil {
			t.Fatal("historical CPU acquired scalar coverage", err)
		}
	}
}
