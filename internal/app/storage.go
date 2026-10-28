package app

import (
	"errors"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storageevidence"
)

// HasStorageClaim selects the strict consumer path even for a viewless bundle.
func HasStorageClaim(raw []byte) bool { return sessionevidence.HasStorageClaim(raw) }

// ValidatedStorage returns an owned summary after the result, inventory and
// any v6 session bindings have passed the same validation used by verification.
func ValidatedStorage(payload InventoryPayload) (*storageevidence.View, error) {
	if err := ValidateCoverage(payload); err != nil {
		return nil, err
	}
	for _, row := range payload.TestResults {
		if row.ID != "storage.quick" || !HasStorageClaim(row.Evidence) {
			continue
		}
		result, err := storageevidence.Decode(row.Evidence)
		if err != nil {
			return nil, err
		}
		view, err := storageevidence.Summary(result)
		if err != nil {
			return nil, err
		}
		return &view, nil
	}
	return nil, nil
}

func validateStorageReport(payload InventoryPayload) error {
	claimed, anySession := false, false
	for _, row := range payload.TestResults {
		anySession = anySession || hasReportClaim(row.Evidence, anySessionClaim)
		if row.ID == "run.integrity.quick" && HasStorageClaim(row.Evidence) && HasSessionThermalClaim(row.Evidence) {
			claimed = true
			continue
		}
		if HasStorageClaim(row.Evidence) || strings.EqualFold(row.ID, "storage.quick") && row.Version != 1 {
			if row.ID != "storage.quick" || row.Version != 2 {
				return errors.New("sampling evidence requires storage definition 2")
			}
			claimed = true
		}
	}
	if !claimed {
		return nil
	}
	if payload.CoverageScope != CoverageFrozenPlan {
		return errors.New("sampling evidence requires frozen-plan report coverage")
	}
	if anySession {
		found := false
		for _, row := range payload.TestResults {
			if row.ID != "run.integrity.quick" {
				continue
			}
			value, err := sessionevidence.DecodeIntegrity(row.Evidence)
			if found || err != nil || value.Session == nil || value.Session.Schema != sessionevidence.SchemaV6 {
				return errors.New("sampling evidence requires a strict v6 session")
			}
			found = true
		}
		if !found {
			return errors.New("sampling evidence has no v6 session binding")
		}
	}
	components := make([]inventory.Component, 0, len(payload.Components))
	for _, component := range payload.Components {
		components = append(components, inventory.Component{Type: component.Type, ID: component.ID, Attributes: component.Attributes})
	}
	for _, row := range payload.TestResults {
		if row.ID != "storage.quick" {
			continue
		}
		if row.Version != 2 {
			return errors.New("invalid sampling definition")
		}
		if HasStorageClaim(row.Evidence) {
			result, err := storageevidence.Decode(row.Evidence)
			if err != nil {
				return err
			}
			if row.PlannedStatus != PlannedScheduled || result.Status != string(row.Status) ||
				anySession && (result.Scope != "planned" || result.PlanID != row.PlanID) ||
				!anySession && (result.Scope != "standalone" || result.PlanID != "") {
				return errors.New("sampling evidence contradicts its report bindings")
			}
			if err := storageevidence.ValidateInventoryBinding(result, components); err != nil {
				return err
			}
		} else if !anySession && sessionevidence.ValidateUnexecutedStorage(sessionevidence.ResultBinding{
			ID: row.ID, Version: row.Version, PlannedStatus: string(row.PlannedStatus), ReportStatus: string(row.Status), Evidence: row.Evidence,
		}) != nil {
			return errors.New("invalid nonexecuted sampling disposition")
		}
	}
	return nil
}
