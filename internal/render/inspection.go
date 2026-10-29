package render

import (
	"fmt"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
)

const exteriorInspectionScope = "Operator-reported observation of safely visible exterior enclosure surfaces. It covers only surfaces inspected without moving or opening the device."
const exteriorInspectionTiming = "Observation time is capped at the observation deadline; preparation and cleanup are excluded."

type inspectionDocument struct {
	Scope, TimingNote, Finding, Status, StopReason string
	ResponseComplete                               bool
	ObservationMilliseconds                        int64
}

func inspectionReportDocument(payload app.InventoryPayload) (*inspectionDocument, error) {
	result, err := app.ValidatedExteriorInspection(payload)
	if err != nil || result == nil {
		return nil, err
	}
	return &inspectionDocument{
		Scope: exteriorInspectionScope, TimingNote: exteriorInspectionTiming,
		Finding: result.Finding, Status: result.Status, StopReason: result.StopReason,
		ResponseComplete: result.ResponseComplete, ObservationMilliseconds: result.ObservationMilliseconds,
	}, nil
}

func appendInspectionPDF(lines *pdfLines, inspection *inspectionDocument) {
	if inspection == nil {
		return
	}
	lines.add("", "OPERATOR-REPORTED EXTERIOR INSPECTION")
	lines.field("Scope", inspection.Scope, "  ")
	lines.field("Finding", inspection.Finding, "  ")
	lines.field("Status", inspection.Status, "  ")
	lines.field("Response complete", fmt.Sprintf("%t", inspection.ResponseComplete), "  ")
	lines.field("Stop reason", inspection.StopReason, "  ")
	lines.field("Bounded observation time (ms)", fmt.Sprintf("%d", inspection.ObservationMilliseconds), "  ")
	lines.field("Timing", inspection.TimingNote, "  ")
}
