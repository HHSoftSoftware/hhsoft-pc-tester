package app

import (
	"errors"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

// ValidatedCPUScalar returns the shared decoder's copied scalar summary only
// after report coverage, CPU definition and any session bindings are checked.
func ValidatedCPUScalar(payload InventoryPayload) (*quickevidence.CPUScalarSummary, error) {
	if err := ValidateCoverage(payload); err != nil {
		return nil, err
	}
	for _, row := range payload.TestResults {
		if row.ID == "cpu.quick" && row.Version == 2 && HasCPUScalarClaim(row.Evidence) {
			local, err := quickevidence.DecodeLocal(row.ID, row.Evidence)
			if err != nil {
				return nil, err
			}
			view := local.Scalar
			return &view, nil
		}
	}
	return nil, nil
}

func validateCPUReport(payload InventoryPayload) error {
	claimed, anySession, strictSession := false, false, false
	for _, row := range payload.TestResults {
		anySession = anySession || hasReportClaim(row.Evidence, anySessionClaim)
		strictSession = strictSession || HasSessionThermalClaim(row.Evidence)
		if row.ID == "cpu.stress" && HasCPUStressClaim(row.Evidence) {
			if row.Version != 1 {
				return errors.New("invalid CPU stress definition")
			}
			if _, _, err := quickevidence.DecodeCPUStress(row.Evidence); err != nil {
				return err
			}
			continue
		}
		if row.ID == "run.integrity.quick" && HasCPUScalarClaim(row.Evidence) && HasSessionThermalClaim(row.Evidence) {
			claimed = true
			continue
		}
		if HasCPUScalarClaim(row.Evidence) || strings.EqualFold(row.ID, "cpu.quick") && row.Version != 1 {
			if row.ID != "cpu.quick" || row.Version != 2 {
				return errors.New("scalar CPU evidence requires CPU definition 2")
			}
			claimed = true
		}
	}
	if !claimed {
		return nil
	}
	if payload.CoverageScope != CoverageDirectCapture && payload.CoverageScope != CoverageFrozenPlan {
		return errors.New("scalar CPU evidence requires explicit report coverage")
	}
	if anySession && !strictSession {
		return errors.New("scalar CPU evidence cannot use an opaque historical session")
	}
	for _, row := range payload.TestResults {
		if row.ID != "cpu.quick" {
			continue
		}
		if row.Version != 2 {
			return errors.New("invalid scalar CPU definition")
		}
		if HasCPUScalarClaim(row.Evidence) {
			local, err := quickevidence.DecodeLocal(row.ID, row.Evidence)
			if err != nil || local.Schema != quick.CPUSchema || !local.Scalar.Present || string(local.Status) != string(row.Status) ||
				payload.CoverageScope == CoverageFrozenPlan && row.PlannedStatus != PlannedScheduled {
				return errors.New("invalid scalar CPU report evidence")
			}
		} else if payload.CoverageScope != CoverageFrozenPlan || sessionevidence.ValidateCPUExecutionNote(sessionevidence.ResultBinding{
			ID: row.ID, Version: row.Version, PlannedStatus: string(row.PlannedStatus), ReportStatus: string(row.Status), Evidence: row.Evidence,
		}) != nil {
			return errors.New("invalid scalar CPU execution note")
		}
	}
	return nil
}
