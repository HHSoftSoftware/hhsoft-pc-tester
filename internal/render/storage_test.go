package render

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

func TestStorageHTMLAndPDFShowBoundedSamplingWithoutHealthOrIdentityCredit(t *testing.T) {
	p := thermalReportFixture(t, "storage-sample-standalone-pass-v1")
	for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
		raw, err := render(p)
		if err != nil || !bytes.Contains(bytes.ToLower(raw), []byte("storage sample observations")) || !bytes.Contains(raw, []byte("131072")) || !bytes.Contains(raw, []byte("65536")) || !bytes.Contains(raw, []byte("buffered")) || !bytes.Contains(raw, []byte("full-media health")) {
			t.Fatal("sampling summary or scope missing", err)
		}
	}
	view, err := storageReportDocument(p)
	if err != nil || view.View.AcceptedMatchedRanges != 1 || len(view.View.Devices) != 1 {
		t.Fatal("one deduplicated window misrepresented", err)
	}
	for _, name := range []string{"session-v4-local-heat", "session-v5-keyboard-pass", "historical-session-v3"} {
		p := thermalReportFixture(t, name)
		if view, err := storageReportDocument(p); err != nil || view != nil {
			t.Fatal("legacy report acquired sampling view", err)
		}
	}
}

func TestStorageRenderRejectsCleanupAndSeparatesDiagnosticCounts(t *testing.T) {
	p := thermalReportFixture(t, "storage-sample-standalone-pass-v1")
	var result storagetest.Result
	if json.Unmarshal(p.TestResults[0].Evidence, &result) != nil {
		t.Fatal("fixture")
	}
	result.Status, result.StopReason, result.Error = "error", "error", "identity_changed"
	result.AcceptedReadBytes, result.AcceptedComparedBytes, result.AcceptedMatchedRanges = 0, 0, 0
	d := &result.Devices[0]
	d.Status = "error"
	d.Error = "identity_changed"
	d.Accepted = false
	d.FinalBindingConfirmed = false
	p.TestResults[0].Status = app.TestError
	p.TestResults[0].Evidence, _ = json.Marshal(result)
	view, err := storageReportDocument(p)
	if err != nil || view.View.AcceptedReadBytes != 0 || view.View.DiagnosticReadBytes != 131072 || view.View.Devices[0].Accepted {
		t.Fatal("diagnostic pass became accepted coverage", err)
	}
	for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
		if _, err := render(p); err != nil {
			t.Fatal("valid diagnostic result rejected", err)
		}
	}
	result.CleanupConfirmed = false
	result.StopReason = "cleanup_unconfirmed"
	result.Error = "cleanup_unconfirmed"
	p.TestResults[0].Evidence, _ = json.Marshal(result)
	for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
		if raw, err := render(p); err == nil || len(raw) != 0 {
			t.Fatal("uncertain cleanup produced a view")
		}
	}
}
