package app

import "errors"

type CoverageScope string

const (
	CoverageDirectCapture CoverageScope = "direct_capture"
	CoverageFrozenPlan    CoverageScope = "frozen_plan"
)

type PlannedStatus string

const (
	PlannedScheduled     PlannedStatus = "scheduled"
	PlannedExcluded      PlannedStatus = "excluded"
	PlannedNotPresent    PlannedStatus = "not_present"
	PlannedUnsupported   PlannedStatus = "unsupported"
	PlannedMissingDriver PlannedStatus = "missing_driver"
)

// ValidateCoverage validates optional coverage metadata without interpreting a
// historical report against today's test catalog. Reports predating these
// fields retain their existing representation and verification behavior.
func ValidateCoverage(payload InventoryPayload) error {
	if err := validateCoverage(payload); err != nil {
		return err
	}
	if err := validatePointerReport(payload); err != nil {
		return err
	}
	_, err := validatedSessionThermal(payload)
	return err
}

func validateCoverage(payload InventoryPayload) error {
	switch payload.CoverageScope {
	case "", CoverageDirectCapture, CoverageFrozenPlan:
	default:
		return errors.New("invalid report coverage scope")
	}
	seen := make(map[string]bool, len(payload.TestResults))
	planID, graph := "", ""
	planned, integrity := 0, 0
	for _, result := range payload.TestResults {
		if err := validatePlannedStatus(result); err != nil {
			return err
		}
		if payload.CoverageScope == "" {
			if result.PlannedStatus != "" {
				return errors.New("planned status requires frozen-plan coverage")
			}
			continue
		}
		if err := validateTestResult(result); err != nil {
			return err
		}
		if seen[result.ID] {
			return errors.New("duplicate result in report coverage")
		}
		seen[result.ID] = true
		if payload.CoverageScope == CoverageDirectCapture {
			if result.PlanID != "" || result.HardwareGraph != "" || result.PlannedStatus != "" {
				return errors.New("direct capture cannot claim a frozen plan")
			}
			continue
		}
		if result.PlanID == "" || result.HardwareGraph == "" {
			return errors.New("frozen-plan coverage requires result bindings")
		}
		if planID == "" {
			planID, graph = result.PlanID, result.HardwareGraph
		}
		if result.PlanID != planID || result.HardwareGraph != graph {
			return errors.New("report coverage has inconsistent plan bindings")
		}
		if result.ID == "run.integrity.quick" {
			integrity++
			if result.PlannedStatus != "" || result.Status != TestPass && result.Status != TestFail {
				return errors.New("invalid run-integrity coverage record")
			}
		} else {
			if result.PlannedStatus == "" {
				return errors.New("frozen-plan result is missing its planned status")
			}
			planned++
		}
	}
	if payload.CoverageScope == CoverageFrozenPlan && (planned == 0 || integrity != 1) {
		return errors.New("frozen-plan coverage requires planned results and run integrity")
	}
	return nil
}

func validatePlannedStatus(result TestResult) error {
	if result.PlannedStatus == "" {
		return nil
	}
	if !planIDPattern.MatchString(result.PlanID) || !hardwareGraphPattern.MatchString(result.HardwareGraph) {
		return errors.New("planned status requires valid plan and hardware bindings")
	}
	switch result.PlannedStatus {
	case PlannedScheduled:
		if !validTestStatus(result.Status) {
			return errors.New("invalid scheduled result status")
		}
	case PlannedNotPresent, PlannedExcluded, PlannedUnsupported:
		if result.Status != TestSkip {
			return errors.New("non-scheduled test must retain its skipped outcome")
		}
	case PlannedMissingDriver:
		if result.Status != TestInconclusive {
			return errors.New("missing-driver test must remain inconclusive")
		}
	default:
		return errors.New("invalid planned test status")
	}
	return nil
}
