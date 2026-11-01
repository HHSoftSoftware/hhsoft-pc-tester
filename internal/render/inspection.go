package render

import (
	"fmt"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
)

const exteriorInspectionScope = "Operator-reported observation of safely visible exterior enclosure surfaces. It covers only surfaces inspected without moving or opening the device."
const exteriorInspectionTiming = "Observation time is capped at the observation deadline; preparation and cleanup are excluded."

type inspectionCriterion struct{ Name, Finding string }

type inspectionDocument struct {
	Criteria                                       []inspectionCriterion
	ResponsesCompleted                             int
	Scope, TimingNote, Finding, Status, StopReason string
	ResponseComplete                               bool
	ObservationMilliseconds                        int64
}

func inspectionReportDocument(payload app.InventoryPayload) (*inspectionDocument, error) {
	condition, err := app.ValidatedExteriorCondition(payload)
	if err != nil {
		return nil, err
	}
	if condition != nil {
		return &inspectionDocument{
			Scope:      "Operator-reported visual exterior condition only; not an automated hardware pass or a port-function check.",
			TimingNote: exteriorInspectionTiming, Status: condition.Status, StopReason: condition.StopReason,
			ResponsesCompleted: condition.ResponsesCompleted, ObservationMilliseconds: condition.ObservationMilliseconds,
			Criteria: []inspectionCriterion{
				{"Enclosure integrity", condition.Findings.EnclosureIntegrity},
				{"Visible hinge and mount condition (not_applicable means the operator reports no hinge)", condition.Findings.HingeMountCondition},
				{"Visible port exterior condition", condition.Findings.PortExteriorCondition},
				{"Visible surface finish condition", condition.Findings.SurfaceFinishCondition},
			},
		}, nil
	}
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
	if len(inspection.Criteria) != 0 {
		for _, criterion := range inspection.Criteria {
			lines.field(criterion.Name, criterion.Finding, "  ")
		}
		lines.field("Responses completed", fmt.Sprintf("%d / 4", inspection.ResponsesCompleted), "  ")
	} else {
		lines.field("Finding", inspection.Finding, "  ")
	}
	lines.field("Status", inspection.Status, "  ")
	if len(inspection.Criteria) == 0 {
		lines.field("Response complete", fmt.Sprintf("%t", inspection.ResponseComplete), "  ")
	}
	lines.field("Stop reason", inspection.StopReason, "  ")
	lines.field("Bounded observation time (ms)", fmt.Sprintf("%d", inspection.ObservationMilliseconds), "  ")
	lines.field("Timing", inspection.TimingNote, "  ")
}
