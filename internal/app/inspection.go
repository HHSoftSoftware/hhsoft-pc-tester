package app

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

// HasExteriorInspectionClaim selects strict validation before decoding can hide
// an ID or schema-family claim. It does not itself validate an observation.
func HasExteriorInspectionClaim(raw []byte) bool { return sessionevidence.HasInspectionClaim(raw) }

// ValidatedExteriorInspection returns a copy after validating the complete
// report. An absent inspection or valid execution note has no observed result.
func ValidatedExteriorInspection(payload InventoryPayload) (*exteriorinspection.Result, error) {
	if err := ValidateCoverage(payload); err != nil {
		return nil, err
	}
	for _, row := range payload.TestResults {
		if row.ID == exteriorinspection.TestID && row.Version == 1 && HasExteriorInspectionClaim(row.Evidence) {
			value, err := exteriorinspection.Decode(row.Evidence)
			if err != nil {
				return nil, err
			}
			return &value, nil
		}
	}
	return nil, nil
}

func validateInspectionReportV1(payload InventoryPayload) error {
	claimed, anySession := false, false
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
			if row.ID != exteriorinspection.TestID || row.Version != 1 {
				return errors.New("inspection evidence requires exterior inspection definition 1")
			}
			claimed = true
		}
	}
	if !claimed {
		return nil
	}
	if payload.CoverageScope != CoverageFrozenPlan {
		return errors.New("inspection evidence requires frozen-plan report coverage")
	}
	if anySession {
		found := false
		for _, row := range payload.TestResults {
			if row.ID != "run.integrity.quick" {
				continue
			}
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if found || err != nil || value.Session == nil || !sessionevidence.SupportsInspection(value.Session.Schema) || sessionevidence.SupportsInspectionV2(value.Session.Schema) {
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
		if row.Version != 1 {
			return errors.New("invalid inspection definition")
		}
		if HasExteriorInspectionClaim(row.Evidence) {
			value, err := exteriorinspection.Decode(row.Evidence)
			if err != nil || row.PlannedStatus != PlannedScheduled || value.Status != string(row.Status) {
				return errors.New("inspection evidence contradicts its report bindings")
			}
		} else if sessionevidence.ValidateInspectionNote(sessionevidence.ResultBinding{
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

// Standalone integrity uses the existing finite metadata shape. Only its exact
// excluded-test reference may explain an inspection ID without a session.
func inspectionExcludedReference(row TestResult) bool {
	var value sessionevidence.IntegrityEvidence
	if row.Version != 1 || evidencejson.ValidateShape(row.Evidence, reflect.TypeOf(value)) != nil ||
		json.Unmarshal(row.Evidence, &value) != nil || value.Schema != sessionevidence.IntegritySchema ||
		value.Session != nil || value.Status != string(row.Status) || value.PlanID != row.PlanID ||
		value.PlannedHardwareGraph != row.HardwareGraph {
		return false
	}
	found := false
	for _, id := range value.ExcludedTests {
		if !strings.EqualFold(id, exteriorinspection.TestID) {
			continue
		}
		if id != exteriorinspection.TestID || found {
			return false
		}
		found = true
	}
	return found
}
