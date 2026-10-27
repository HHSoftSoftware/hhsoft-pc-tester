package app

import (
	"errors"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/pointerevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

// ValidatedPointer returns independent observations after validating report
// coverage and retained device passes against signed final inventory. Historical
// v1 evidence retains its schema and does not acquire wheel observations.
func ValidatedPointer(payload InventoryPayload) (*inputtest.PointerResult, error) {
	if err := ValidateCoverage(payload); err != nil {
		return nil, err
	}
	for _, row := range payload.TestResults {
		if row.ID == "input.pointer.quick" && HasPointerClaim(row.Evidence) {
			result, err := pointerevidence.Decode(row.Evidence)
			if err != nil {
				return nil, err
			}
			return &result, nil
		}
	}
	return nil, nil
}

// Pointer collection also has a standalone frozen-plan workflow. Its evidence
// must be checked even when no session thermal journal is present.
func validatePointerReport(payload InventoryPayload) error {
	claimed, current, strictSession, anySession := false, false, false, false
	for _, row := range payload.TestResults {
		strictSession = strictSession || HasSessionThermalClaim(row.Evidence)
		anySession = anySession || hasReportClaim(row.Evidence, anySessionClaim)
		if HasPointerClaim(row.Evidence) || strings.EqualFold(row.ID, "input.pointer.quick") && row.Version != 1 {
			if row.ID != "input.pointer.quick" || row.Version != 1 && row.Version != 2 {
				return errors.New("pointer evidence requires a supported pointer definition")
			}
			claimed = true
			current = current || row.Version == 2
		}
	}
	if !claimed {
		return nil
	}
	if payload.CoverageScope != CoverageFrozenPlan {
		return errors.New("pointer observations require frozen-plan coverage")
	}
	if current && anySession && !strictSession {
		return errors.New("pointer definition 2 cannot use a historical or opaque session")
	}
	components := make([]inventory.Component, 0, len(payload.Components))
	for _, component := range payload.Components {
		components = append(components, inventory.Component{Type: component.Type, ID: component.ID, Attributes: component.Attributes})
	}
	for _, row := range payload.TestResults {
		if row.ID != "input.pointer.quick" {
			continue
		}
		if HasPointerClaim(row.Evidence) {
			result, err := pointerevidence.Decode(row.Evidence)
			if err != nil || row.PlannedStatus != PlannedScheduled || string(row.Status) != string(result.Status) ||
				row.Version == 1 && result.Schema != inputtest.PointerSchemaV1 || row.Version == 2 && result.Schema != inputtest.PointerSchema {
				return errors.New("invalid pointer functional report evidence")
			}
			if err := pointerevidence.ValidateInventoryBinding(result, components); err != nil {
				return errors.New("pointer observations do not match signed final inventory")
			}
			continue
		}
		// Strict session validation checks worker state and any permitted missing
		// evidence note. Standalone notes must satisfy the same shared wire shape.
		if row.Version == 2 && !strictSession && sessionevidence.ValidatePointerNote(sessionevidence.ResultBinding{
			ID: row.ID, Version: row.Version, PlannedStatus: string(row.PlannedStatus), ReportStatus: string(row.Status), Evidence: row.Evidence,
		}) != nil {
			return errors.New("invalid standalone pointer execution note")
		}
	}
	return nil
}
