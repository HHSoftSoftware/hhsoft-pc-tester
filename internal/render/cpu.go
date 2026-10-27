package render

import (
	"fmt"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
)

type cpuScalarDocument struct {
	Suite, Scope, Finding                        string
	Checks                                       uint64
	Started, Completed, Mismatched               int
	IntegerFailure, SHA256Failure, ScalarFailure bool
}

func cpuScalarReportDocument(payload app.InventoryPayload) (*cpuScalarDocument, error) {
	summary, err := app.ValidatedCPUScalar(payload)
	if err != nil || summary == nil {
		return nil, err
	}
	view := &cpuScalarDocument{Suite: summary.Suite,
		Scope:   "Twelve fixed binary64 addition, subtraction, multiplication and division checks at bounded intervals. Legacy targets may use software arithmetic; these observations do not establish hardware FPU or full instruction coverage.",
		Finding: "Coverage records completed checks; incomplete coverage is not a calculation failure.",
		Checks:  summary.ChecksPassed, Started: summary.WorkersStarted, Completed: summary.WorkersCompleted, Mismatched: summary.MismatchedWorkers,
		IntegerFailure: summary.CalculationFailures.Integer, SHA256Failure: summary.CalculationFailures.SHA256, ScalarFailure: summary.CalculationFailures.Scalar}
	if view.IntegerFailure || view.SHA256Failure || view.ScalarFailure {
		view.Finding = "A known-answer calculation mismatch was retained, including after cancellation."
	}
	return view, nil
}

func appendCPUScalarPDF(lines *pdfLines, value *cpuScalarDocument) {
	if value == nil {
		return
	}
	lines.add("", "CPU SCALAR CALCULATION OBSERVATIONS")
	lines.field("Suite", value.Suite, "  ")
	lines.field("Scope", value.Scope, "  ")
	lines.field("Finding", value.Finding, "  ")
	lines.field("Accepted scalar checks", fmt.Sprint(value.Checks), "  ")
	lines.field("Started workers", fmt.Sprint(value.Started), "  ")
	lines.field("Workers completing at least one suite", fmt.Sprint(value.Completed), "  ")
	lines.field("Workers with scalar mismatches", fmt.Sprint(value.Mismatched), "  ")
	lines.field("Integer mismatch", fmt.Sprint(value.IntegerFailure), "  ")
	lines.field("SHA-256 mismatch", fmt.Sprint(value.SHA256Failure), "  ")
	lines.field("Scalar mismatch", fmt.Sprint(value.ScalarFailure), "  ")
}
