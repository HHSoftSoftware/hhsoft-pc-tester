package render

import (
	"fmt"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
)

type memoryDocument struct {
	Suite, Scope, Phase, Direction, Finding                     string
	Allocated, AcceptedReads, AcceptedWrites                    int64
	ObservedMatches, AttemptedReads, ObservedWrites, Uncredited int64
	Passes, CompletedPasses, CompletedPhases                    int
}

func memoryReportDocument(payload app.InventoryPayload) (*memoryDocument, error) {
	summary, err := app.ValidatedMemory(payload)
	if err != nil || summary == nil {
		return nil, err
	}
	v := &memoryDocument{Suite: summary.Suite,
		Scope:   "Three byte patterns and forward/reverse 0xaa/0x55 inversions over an owned userspace working set. Logical byte counts do not establish full installed-memory, physical DRAM, ECC or general hardware-fault coverage.",
		Finding: "Coverage describes completed work; the worker result and thermal observations remain authoritative.",
		Phase:   strings.ReplaceAll(summary.Phase, "_", " "), Direction: summary.Direction,
		Allocated: summary.AllocatedBytes, AcceptedReads: summary.ExaminedBytes, AcceptedWrites: summary.WrittenBytes,
		ObservedMatches: summary.ObservedMatchedBytes, AttemptedReads: summary.AttemptedReadBytes,
		ObservedWrites: summary.ObservedWrittenBytes, Uncredited: summary.UncreditedBytes,
		Passes: summary.Passes, CompletedPasses: summary.CompletedPasses, CompletedPhases: summary.CompletedPhases}
	if summary.Mismatch != nil {
		v.Finding = fmt.Sprintf("Retained mismatch in pass %d at buffer offset %d: expected 0x%02x, observed 0x%02x. A mismatch remains a failure after a stop.", summary.CurrentPass+1, summary.Mismatch.Offset, summary.Mismatch.ExpectedByte, summary.Mismatch.ActualByte)
	}
	return v, nil
}

func appendMemoryPDF(lines *pdfLines, v *memoryDocument) {
	if v == nil {
		return
	}
	lines.add("", "RAM PATTERN AND INVERSION OBSERVATIONS")
	lines.field("Suite", v.Suite, "  ")
	lines.field("Scope", v.Scope, "  ")
	lines.field("Finding", v.Finding, "  ")
	lines.field("Allocated bytes", fmt.Sprint(v.Allocated), "  ")
	lines.field("Completed/requested passes", fmt.Sprintf("%d/%d", v.CompletedPasses, v.Passes), "  ")
	lines.field("Completed phases", fmt.Sprint(v.CompletedPhases), "  ")
	lines.field("Phase / direction", v.Phase+" / "+v.Direction, "  ")
	lines.field("Accepted matching read bytes", fmt.Sprint(v.AcceptedReads), "  ")
	lines.field("Accepted write bytes", fmt.Sprint(v.AcceptedWrites), "  ")
	lines.field("Observed matching read bytes", fmt.Sprint(v.ObservedMatches), "  ")
	lines.field("Attempted read bytes (including mismatch)", fmt.Sprint(v.AttemptedReads), "  ")
	lines.field("Observed write bytes", fmt.Sprint(v.ObservedWrites), "  ")
	lines.field("Uncredited terminal units", fmt.Sprint(v.Uncredited), "  ")
}
