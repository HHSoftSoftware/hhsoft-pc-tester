package render

import (
	"fmt"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
)

const cpuStressScope = "Checked arithmetic during a shared elapsed load window. Workload counters do not measure CPU time or utilization."

type cpuStressDocument struct {
	Scope, Outcome, StopReason string
	Requested, Observed        int64
	Ready, RequestedWorkers    int
	LoadChecks                 uint64
	Cleanup                    bool
	Workers                    []cpuStressWorkerDocument
}

type cpuStressWorkerDocument struct {
	Index              int
	Entry              string
	Stop               int64
	Iterations, Checks uint64
	Reason             string
}

func cpuStressReportDocument(payload app.InventoryPayload) (*cpuStressDocument, error) {
	value, err := app.ValidatedCPUStress(payload)
	if err != nil || value == nil {
		return nil, err
	}
	document := &cpuStressDocument{Scope: cpuStressScope, Outcome: string(value.Status), StopReason: value.StopReason,
		Requested: value.RequestedLoadMilliseconds, Observed: value.ObservedLoadMilliseconds,
		Ready: value.WorkersReady, RequestedWorkers: value.WorkersRequested, Cleanup: value.Safety.CleanupConfirmed,
		Workers: make([]cpuStressWorkerDocument, 0, len(value.Workers))}
	for _, worker := range value.Workers {
		entry := "not entered"
		if worker.LoadEnteredMilliseconds != nil {
			entry = fmt.Sprintf("%d", *worker.LoadEnteredMilliseconds)
		}
		document.LoadChecks += worker.LoadChecksPassed // Bounded by the validated checked lifetime sum.
		document.Workers = append(document.Workers, cpuStressWorkerDocument{Index: worker.Index, Entry: entry,
			Stop: worker.StoppedMilliseconds, Iterations: worker.LoadIterations, Checks: worker.LoadChecksPassed, Reason: worker.ExitReason})
	}
	return document, nil
}

func appendCPUStressPDF(lines *pdfLines, value *cpuStressDocument) {
	if value == nil {
		return
	}
	lines.add("", "CPU STRESS (STANDARD PROFILE)")
	lines.field("Scope", value.Scope, "  ")
	lines.field("Outcome", value.Outcome, "  ")
	lines.field("Stop reason", value.StopReason, "  ")
	lines.field("Requested load (ms)", fmt.Sprintf("%d", value.Requested), "  ")
	lines.field("Observed load window (ms)", fmt.Sprintf("%d", value.Observed), "  ")
	lines.field("Ready workers", fmt.Sprintf("%d/%d", value.Ready, value.RequestedWorkers), "  ")
	lines.field("Load scalar checks", fmt.Sprintf("%d", value.LoadChecks), "  ")
	lines.field("Cleanup confirmed", fmt.Sprintf("%t", value.Cleanup), "  ")
	for _, worker := range value.Workers {
		lines.field(fmt.Sprintf("Worker %d", worker.Index), fmt.Sprintf("entry %s ms; stop %d ms; load iterations %d; load checks %d; %s", worker.Entry, worker.Stop, worker.Iterations, worker.Checks, worker.Reason), "  ")
	}
}
