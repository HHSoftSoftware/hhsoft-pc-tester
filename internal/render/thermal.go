package render

import (
	"fmt"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
)

type thermalDocument struct {
	Scope, Finding, Coverage, StopReason string
	Peak, Limit, Cleanup                 string
	Observations                         uint64
	MaximumGapMilliseconds               int64
	LocalSources                         []thermalSourceDocument
}

type thermalSourceDocument struct {
	Worker, Peak, Limit, Interval string
}

func sessionThermalDocument(payload app.InventoryPayload) (*thermalDocument, error) {
	view, err := app.ValidatedSessionThermal(payload)
	if err != nil || view == nil {
		return nil, err
	}
	result := &thermalDocument{
		Scope: view.Scope, StopReason: view.StopReason, Peak: "Unavailable",
		Limit: thermalTemperature(view.LimitMilliCelsius), Cleanup: "Confirmed",
		Observations: view.AcceptedObservations, MaximumGapMilliseconds: view.MaximumGapMilliseconds,
		Finding: "No limit observed in the available readings.",
	}
	if view.Finding == "limit_reached" {
		result.Finding = "Temperature limit reached."
	}
	switch view.Coverage {
	case "available":
		result.Coverage = "Available for every accepted observation."
	case "partial":
		result.Coverage = "Partial: some observations or readings were unavailable or rejected."
	case "unavailable":
		result.Coverage = "Unavailable: no valid sensor readings were obtained."
	}
	if view.PeakObserved {
		result.Peak = thermalTemperature(view.PeakMilliCelsius)
	}
	for _, source := range view.LocalSources {
		result.LocalSources = append(result.LocalSources, thermalSourceDocument{
			Worker: source.WorkerID, Peak: thermalTemperature(source.PeakMilliCelsius),
			Limit:    thermalTemperature(source.LimitMilliCelsius),
			Interval: fmt.Sprintf("%d to %d ms after session start", source.StartedMilliseconds, source.FinishedMilliseconds),
		})
	}
	return result, nil
}

func thermalTemperature(milliCelsius int) string {
	return fmt.Sprintf("%d.%03d C", milliCelsius/1000, milliCelsius%1000)
}

func appendThermalPDF(lines *pdfLines, thermal *thermalDocument) {
	if thermal == nil {
		return
	}
	lines.add("", "SESSION TEMPERATURE OBSERVATIONS")
	lines.field("Scope", thermal.Scope, "  ")
	lines.field("Finding", thermal.Finding, "  ")
	lines.field("Sensor coverage", thermal.Coverage, "  ")
	lines.field("Global observed peak", thermal.Peak, "  ")
	lines.field("Stop limit", thermal.Limit, "  ")
	lines.field("Accepted global observations", fmt.Sprint(thermal.Observations), "  ")
	lines.field("Maximum sampling gap", fmt.Sprintf("%d ms", thermal.MaximumGapMilliseconds), "  ")
	lines.field("Monitor cleanup", thermal.Cleanup, "  ")
	lines.field("Session stop reason", thermal.StopReason, "  ")
	lines.add("  Sampling is not continuous coverage. Sensor availability does not establish hardware health.")
	if len(thermal.LocalSources) != 0 {
		lines.add("", "LOCAL CPU/RAM TEMPERATURE FINDINGS")
		lines.add("  Local peaks do not change global sensor coverage or observation counts.")
		for _, source := range thermal.LocalSources {
			lines.field("Worker", source.Worker, "  ")
			lines.field("Local observed peak", source.Peak, "    ")
			lines.field("Stop limit", source.Limit, "    ")
			lines.field("Execution interval", source.Interval, "    ")
		}
	}
}
