package render

import (
	"fmt"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storageevidence"
)

type storageDocument struct {
	ScopeNote string
	Finding   string
	View      storageevidence.View
}

func storageReportDocument(payload app.InventoryPayload) (*storageDocument, error) {
	view, err := app.ValidatedStorage(payload)
	if err != nil || view == nil {
		return nil, err
	}
	finding := "Only accepted repeated sample observations contribute coverage; excluded and incomplete devices remain explicit."
	if view.FailureObserved {
		finding = "An accepted read failure, short read or mismatch remains a failure even after a later stop."
	} else if view.DiagnosticFailure {
		finding = "A diagnostic failure was retained without granting accepted sample coverage."
	}
	return &storageDocument{ScopeNote: "Up to three bounded buffered sample windows, with two reads per completed comparison. These signed producer observations do not establish cache bypass, full-media health, throughput, durable identity or physical-device qualification.", Finding: finding, View: *view}, nil
}

func appendStoragePDF(lines *pdfLines, value *storageDocument) {
	if value == nil {
		return
	}
	view := value.View
	lines.add("", "STORAGE SAMPLE OBSERVATIONS")
	lines.field("Scope", value.ScopeNote, "  ")
	lines.field("Finding", value.Finding, "  ")
	lines.field("Selection scope", view.Scope, "  ")
	lines.field("Status / stop", view.Status+" / "+view.StopReason, "  ")
	lines.field("Accepted read bytes", fmt.Sprint(view.AcceptedReadBytes), "  ")
	lines.field("Accepted compared bytes", fmt.Sprint(view.AcceptedComparedBytes), "  ")
	lines.field("Accepted matched ranges", fmt.Sprint(view.AcceptedMatchedRanges), "  ")
	lines.field("Diagnostic read bytes", fmt.Sprint(view.DiagnosticReadBytes), "  ")
	lines.field("Diagnostic compared bytes", fmt.Sprint(view.DiagnosticComparedBytes), "  ")
	lines.field("Cleanup confirmed", fmt.Sprint(view.CleanupConfirmed), "  ")
	for _, device := range view.Devices {
		lines.field("Device", device.Interface, "  ")
		lines.field("Status / exclusion", device.Status+" / "+device.Exclusion, "    ")
		lines.field("Accepted observation", fmt.Sprint(device.Accepted), "    ")
		lines.field("Observed read / compared bytes", fmt.Sprintf("%d / %d", device.ReadBytes, device.ComparedBytes), "    ")
		lines.field("Observed matched ranges", fmt.Sprint(device.MatchedRanges), "    ")
	}
}
