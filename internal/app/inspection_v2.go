package app

import (
	"errors"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

// A family claim selects a strict versioned consumer, never fallback authority.
func validateInspectionReport(payload InventoryPayload) error {
	for _, row := range payload.TestResults {
		if row.ID != "run.integrity.quick" && row.Version == 2 &&
			(strings.EqualFold(row.ID, exteriorinspection.TestID) || HasExteriorInspectionClaim(row.Evidence)) {
			return validateInspectionConditionReport(payload)
		}
	}
	return validateInspectionReportV1(payload)
}

// ValidatedExteriorCondition returns an owned value after complete report checks.
func ValidatedExteriorCondition(payload InventoryPayload) (*exteriorinspection.ResultV2, error) {
	if err := ValidateCoverage(payload); err != nil {
		return nil, err
	}
	for _, row := range payload.TestResults {
		if row.ID == exteriorinspection.TestID && row.Version == 2 && HasExteriorInspectionClaim(row.Evidence) {
			value, err := exteriorinspection.DecodeV2(row.Evidence)
			if err != nil {
				return nil, err
			}
			return &value, nil
		}
	}
	return nil, nil
}

func validateInspectionConditionReport(payload InventoryPayload) error {
	claimed, anySession := false, false
	requiresSession := false
	integrityReference, excludedReference := false, false
	for _, row := range payload.TestResults {
		anySession = anySession || hasReportClaim(row.Evidence, anySessionClaim)
		if row.ID == "run.integrity.quick" && HasExteriorInspectionClaim(row.Evidence) {
			// Integrity may name a worker or an exclusion; it cannot contain an
			// observation schema, even hidden in an earlier duplicate member.
			if sessionevidence.HasInspectionSchemaClaim(row.Evidence) {
				return errors.New("inspection observations cannot appear under run integrity")
			}
			if !hasReportClaim(row.Evidence, anySessionClaim) {
				if !inspectionExcludedReference(row) {
					return errors.New("invalid inspection exclusion reference")
				}
				excludedReference = true
			}
			claimed, integrityReference = true, true
			continue
		}
		if HasExteriorInspectionClaim(row.Evidence) || strings.EqualFold(row.ID, exteriorinspection.TestID) {
			if row.ID != exteriorinspection.TestID || row.Version != 2 {
				return errors.New("inspection evidence requires exterior inspection definition 2")
			}
			claimed = true
			// Fixed nonexecution notes carry no observation authority.
			// Actual condition claims still require their current journal.
			requiresSession = requiresSession || HasExteriorInspectionClaim(row.Evidence)
		}
	}
	if !claimed {
		return nil
	}
	if payload.CoverageScope != CoverageFrozenPlan {
		return errors.New("inspection evidence requires frozen-plan report coverage")
	}
	if anySession || requiresSession {
		found := false
		for _, row := range payload.TestResults {
			if row.ID != "run.integrity.quick" {
				continue
			}
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if found || err != nil || value.Session == nil || !sessionevidence.SupportsInspectionV2(value.Session.Schema) {
				return errors.New("inspection evidence requires a strict inspection session")
			}
			found = true
		}
		if !found {
			return errors.New("inspection evidence has no supported session binding")
		}
	}
	foundRow := false
	for _, row := range payload.TestResults {
		if row.ID != exteriorinspection.TestID {
			continue
		}
		foundRow = true
		if excludedReference && row.PlannedStatus != PlannedExcluded {
			return errors.New("inspection exclusion reference contradicts its result")
		}
		if row.Version != 2 {
			return errors.New("invalid inspection definition")
		}
		if HasExteriorInspectionClaim(row.Evidence) {
			value, err := exteriorinspection.DecodeV2(row.Evidence)
			if err != nil || row.PlannedStatus != PlannedScheduled || value.Status != string(row.Status) {
				return errors.New("inspection evidence contradicts its report bindings")
			}
		} else if sessionevidence.ValidateInspectionConditionNote(sessionevidence.ResultBinding{
			ID: row.ID, Version: row.Version, PlannedStatus: string(row.PlannedStatus),
			ReportStatus: string(row.Status), Evidence: row.Evidence,
		}) != nil {
			return errors.New("invalid inspection execution note")
		}
	}
	if integrityReference && !foundRow {
		return errors.New("inspection integrity reference has no actual result")
	}
	return nil
}
