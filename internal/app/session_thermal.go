package app

import (
	"errors"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

// ValidatedSessionThermal returns a copied view only after validating the new
// v4/v5 session claim and its other signed result rows. Journals v1-v3 remain
// opaque and return nil; they are not interpreted against the current catalog.
func ValidatedSessionThermal(payload InventoryPayload) (*sessionevidence.ThermalView, error) {
	if err := validateCoverage(payload); err != nil {
		return nil, err
	}
	if err := validateCPUReport(payload); err != nil {
		return nil, err
	}
	if err := validatePointerReport(payload); err != nil {
		return nil, err
	}
	return validatedSessionThermal(payload)
}

func validatedSessionThermal(payload InventoryPayload) (*sessionevidence.ThermalView, error) {
	claimed := false
	keyboardClaim := false
	for _, result := range payload.TestResults {
		if HasKeyboardClaim(result.Evidence) || strings.EqualFold(result.ID, "input.keyboard.quick") && result.Version != 1 {
			if result.ID != "input.keyboard.quick" || result.Version != 2 {
				return nil, errors.New("keyboard functional evidence requires keyboard definition 2")
			}
			keyboardClaim = true
		}
		if HasSessionThermalClaim(result.Evidence) {
			if result.ID != "run.integrity.quick" {
				return nil, errors.New("run-wide thermal evidence requires a run-integrity record")
			}
			claimed = true
		}
	}
	if !claimed {
		if keyboardClaim {
			if err := validateUnexecutedKeyboardReport(payload); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	if payload.CoverageScope != CoverageFrozenPlan {
		return nil, errors.New("run-wide thermal evidence requires frozen-plan coverage")
	}
	var integrity *TestResult
	bindings := make([]sessionevidence.ResultBinding, 0, len(payload.TestResults))
	for index := range payload.TestResults {
		result := &payload.TestResults[index]
		if result.ID == "run.integrity.quick" {
			if integrity != nil || result.Version != 1 {
				return nil, errors.New("invalid run-integrity thermal claim")
			}
			integrity = result
			continue
		}
		bindings = append(bindings, sessionevidence.ResultBinding{
			ID: result.ID, Version: result.Version, PlannedStatus: string(result.PlannedStatus),
			ReportStatus: string(result.Status), Evidence: result.Evidence,
		})
	}
	if integrity == nil {
		return nil, errors.New("missing run-integrity thermal claim")
	}
	decoded, err := sessionevidence.DecodeIntegrity(integrity.Evidence)
	if err != nil || decoded.Session == nil || decoded.Status != string(integrity.Status) ||
		decoded.PlanID != integrity.PlanID || decoded.PlannedHardwareGraph != integrity.HardwareGraph {
		return nil, errors.New("run-integrity thermal claim has invalid evidence or bindings")
	}
	if err := sessionevidence.ValidateBindings(*decoded.Session, bindings); err != nil {
		return nil, err
	}
	if keyboardClaim && decoded.Session.Schema != sessionevidence.Schema {
		return nil, errors.New("keyboard functional evidence requires a current quick session")
	}
	if err := validateKeyboardInventory(payload); err != nil {
		return nil, err
	}
	view := sessionevidence.ThermalViewOf(*decoded.Session)
	return &view, nil
}
