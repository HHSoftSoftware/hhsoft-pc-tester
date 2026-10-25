package render

import "github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"

func coverageDescription(scope app.CoverageScope) string {
	switch scope {
	case app.CoverageDirectCapture:
		return "Direct capture: only the listed checks were collected. No frozen plan or complete quick-profile coverage is claimed."
	case app.CoverageFrozenPlan:
		return "Frozen-plan coverage: every planned test is listed, including tests that did not run. This does not establish full hardware qualification or an overall hardware pass."
	default:
		return ""
	}
}

func plannedStatusDescription(status app.PlannedStatus) string {
	switch status {
	case app.PlannedScheduled:
		return "Selected for execution; see the recorded outcome."
	case app.PlannedNotPresent:
		return "Not run: applicable hardware was not detected."
	case app.PlannedMissingDriver:
		return "Not run: hardware was detected without a usable driver."
	case app.PlannedUnsupported:
		return "Not run: this test is not implemented for this image."
	case app.PlannedExcluded:
		return "Not run: excluded by the selected profile or operator choice."
	default:
		return ""
	}
}
