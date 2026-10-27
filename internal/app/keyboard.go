package app

import (
	"errors"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/keyboardevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

// ValidatedKeyboard returns independent typed observations only after checking
// the complete session and every retained device pass against signed inventory.
// A historical or never-started keyboard row has no functional observations.
func ValidatedKeyboard(payload InventoryPayload) (*inputtest.KeyboardResult, error) {
	if err := ValidateCoverage(payload); err != nil {
		return nil, err
	}
	for _, row := range payload.TestResults {
		if row.ID == "input.keyboard.quick" && HasKeyboardClaim(row.Evidence) {
			result, err := keyboardevidence.Decode(row.Evidence)
			if err != nil {
				return nil, err
			}
			return &result, nil
		}
	}
	return nil, nil
}

func validateKeyboardInventory(payload InventoryPayload) error {
	for _, row := range payload.TestResults {
		if row.ID != "input.keyboard.quick" || !HasKeyboardClaim(row.Evidence) {
			continue
		}
		result, err := keyboardevidence.Decode(row.Evidence)
		if err != nil || row.Version != 2 || row.PlannedStatus != PlannedScheduled || string(row.Status) != string(result.Status) {
			return errors.New("invalid keyboard functional report evidence")
		}
		components := make([]inventory.Component, 0, len(payload.Components))
		for _, component := range payload.Components {
			components = append(components, inventory.Component{Type: component.Type, ID: component.ID, Attributes: component.Attributes})
		}
		if err := keyboardevidence.ValidateInventoryBinding(result, components); err != nil {
			return errors.New("keyboard observations do not match signed final inventory")
		}
	}
	return nil
}

func validateUnexecutedKeyboardReport(payload InventoryPayload) error {
	if payload.CoverageScope != CoverageFrozenPlan {
		return errors.New("keyboard definition 2 requires frozen-plan coverage")
	}
	for _, row := range payload.TestResults {
		if hasReportClaim(row.Evidence, anySessionClaim) {
			return errors.New("keyboard definition 2 cannot use a historical or opaque session")
		}
		if row.ID == "input.keyboard.quick" && sessionevidence.ValidateUnexecutedKeyboard(sessionevidence.ResultBinding{
			ID: row.ID, Version: row.Version, PlannedStatus: string(row.PlannedStatus), ReportStatus: string(row.Status), Evidence: row.Evidence,
		}) != nil {
			return errors.New("keyboard observations require a current quick session")
		}
	}
	return nil
}
