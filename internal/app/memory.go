package app

import (
	"errors"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

// ValidatedMemory returns a copied shared summary, after report/session binding.
func ValidatedMemory(payload InventoryPayload) (*quickevidence.MemorySummary, error) {
	if err := ValidateCoverage(payload); err != nil {
		return nil, err
	}
	for _, row := range payload.TestResults {
		if row.ID == "memory.quick" && row.Version == 2 && HasMemoryClaim(row.Evidence) {
			local, err := quickevidence.DecodeLocal(row.ID, row.Evidence)
			if err != nil {
				return nil, err
			}
			view := local.Memory
			return &view, nil
		}
	}
	return nil, nil
}

func validateMemoryReport(payload InventoryPayload) error {
	claimed, anySession, strictSession := false, false, false
	for _, row := range payload.TestResults {
		anySession = anySession || hasReportClaim(row.Evidence, anySessionClaim)
		strictSession = strictSession || HasSessionThermalClaim(row.Evidence)
		if row.ID == "run.integrity.quick" && HasMemoryClaim(row.Evidence) && HasSessionThermalClaim(row.Evidence) {
			claimed = true
			continue
		}
		if HasMemoryClaim(row.Evidence) || strings.EqualFold(row.ID, "memory.quick") && row.Version != 1 {
			if row.ID != "memory.quick" || row.Version != 2 {
				return errors.New("memory inversion evidence requires memory definition 2")
			}
			claimed = true
		}
	}
	if !claimed {
		return nil
	}
	if payload.CoverageScope != CoverageDirectCapture && payload.CoverageScope != CoverageFrozenPlan {
		return errors.New("memory inversion evidence requires explicit report coverage")
	}
	if anySession && !strictSession {
		return errors.New("memory inversion evidence cannot use an opaque historical session")
	}
	for _, row := range payload.TestResults {
		if row.ID != "memory.quick" {
			continue
		}
		if row.Version != 2 {
			return errors.New("invalid memory inversion definition")
		}
		if HasMemoryClaim(row.Evidence) {
			local, err := quickevidence.DecodeLocal(row.ID, row.Evidence)
			if err != nil || local.Schema != quick.MemorySchema || !local.Memory.Present || string(local.Status) != string(row.Status) || payload.CoverageScope == CoverageFrozenPlan && row.PlannedStatus != PlannedScheduled {
				return errors.New("invalid memory inversion report evidence")
			}
		} else if payload.CoverageScope != CoverageFrozenPlan || sessionevidence.ValidateMemoryExecutionNote(sessionevidence.ResultBinding{
			ID: row.ID, Version: row.Version, PlannedStatus: string(row.PlannedStatus), ReportStatus: string(row.Status), Evidence: row.Evidence,
		}) != nil {
			return errors.New("invalid memory execution note")
		}
	}
	return nil
}
