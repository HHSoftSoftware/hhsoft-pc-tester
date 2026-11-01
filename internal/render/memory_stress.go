package render

import (
	"fmt"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
)

const memoryStressScope = "Repeated byte-pattern verification in one bounded working set. This is not full-RAM coverage, a memory-bandwidth measurement or proof of hardware stability beyond this run."

type memoryStressDocument struct {
	Scope, Outcome, StopReason, Mismatch                                string
	Requested, Observed                                                 int64
	Allocated, LoadCycles, CompletedCycles, CompletedPhases, PhaseBytes uint64
	ReadBytes, WrittenBytes, UncreditedBytes                            uint64
	TerminalReadBytes, TerminalUncreditedBytes                          uint64
	Cleanup                                                             bool
}

func memoryStressReportDocument(payload app.InventoryPayload) (*memoryStressDocument, error) {
	value, err := app.ValidatedMemoryStress(payload)
	if err != nil || value == nil {
		return nil, err
	}
	c := value.Coverage
	document := &memoryStressDocument{Scope: memoryStressScope, Outcome: string(value.Status), StopReason: value.StopReason,
		Requested: value.RequestedLoadMilliseconds, Observed: value.ObservedLoadMilliseconds, Allocated: value.AllocatedBytes,
		LoadCycles: c.LoadCycles, CompletedCycles: c.CompletedCycles, CompletedPhases: c.CompletedPhases, PhaseBytes: c.PhaseBytes,
		ReadBytes: c.ReadBytes, WrittenBytes: c.WrittenBytes, UncreditedBytes: c.UncreditedBytes,
		TerminalReadBytes: c.TerminalReadBytes, TerminalUncreditedBytes: c.TerminalUncreditedBytes,
		Cleanup:  value.Safety.CleanupConfirmed && (!value.Availability.Opened || value.Availability.CloseConfirmed) && !value.StopFacts.CleanupUnconfirmed,
		Mismatch: "none observed"}
	if m := value.Mismatch; m != nil {
		document.Mismatch = fmt.Sprintf("%s cycle %d phase %d offset %d; expected byte %d, observed byte %d",
			m.Stage, m.Cycle, m.Phase, m.Offset, m.ExpectedByte, m.ActualByte)
	}
	return document, nil
}

func appendMemoryStressPDF(lines *pdfLines, value *memoryStressDocument) {
	if value == nil {
		return
	}
	lines.add("", "MEMORY STRESS (EXTENDED PROFILE)")
	lines.field("Scope", value.Scope, "  ")
	lines.field("Outcome", value.Outcome, "  ")
	lines.field("Stop reason", value.StopReason, "  ")
	lines.field("Requested load (ms)", fmt.Sprintf("%d", value.Requested), "  ")
	lines.field("Observed load window (ms)", fmt.Sprintf("%d", value.Observed), "  ")
	lines.field("Working-set bytes", fmt.Sprintf("%d", value.Allocated), "  ")
	lines.field("Verified load cycles", fmt.Sprintf("%d", value.LoadCycles), "  ")
	lines.field("Completed cycles including finishing cycle", fmt.Sprintf("%d", value.CompletedCycles), "  ")
	lines.field("Completed cycle phases", fmt.Sprintf("%d", value.CompletedPhases), "  ")
	lines.field("Accepted current-phase bytes", fmt.Sprintf("%d", value.PhaseBytes), "  ")
	lines.field("Accepted cycle read / write bytes", fmt.Sprintf("%d / %d", value.ReadBytes, value.WrittenBytes), "  ")
	lines.field("Uncredited cycle prefix bytes", fmt.Sprintf("%d", value.UncreditedBytes), "  ")
	lines.field("Accepted terminal read bytes", fmt.Sprintf("%d", value.TerminalReadBytes), "  ")
	lines.field("Uncredited terminal prefix bytes", fmt.Sprintf("%d", value.TerminalUncreditedBytes), "  ")
	lines.field("Mismatch", value.Mismatch, "  ")
	lines.field("Cleanup confirmed", fmt.Sprintf("%t", value.Cleanup), "  ")
}
