package render

import (
	"fmt"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

const pointerWheelScope = "Relative X/Y motion, primary and advertised right/middle buttons, and both directions of every advertised wheel axis. Wheel counts describe complete frames, not detents, distance or resolution."

type pointerDocument struct {
	Scope, Status, Finding, StopReason string
	Devices                            []pointerDeviceDocument
}

type pointerDeviceDocument struct {
	Interface, Status, WheelCoverage string
	Wheels                           []pointerWheelDocument
}

type pointerWheelDocument struct {
	Axis, Stream string
	Positive     uint64
	Negative     uint64
}

func pointerReportDocument(payload app.InventoryPayload) (*pointerDocument, error) {
	result, err := app.ValidatedPointer(payload)
	if err != nil || result == nil {
		return nil, err
	}
	// Historical views remain byte-compatible. A v1 pass did not qualify wheels;
	// its unchanged evidence remains in the functional evidence section.
	if result.Schema == inputtest.PointerSchemaV1 {
		return nil, nil
	}
	view := &pointerDocument{Scope: pointerWheelScope, Status: string(result.Status), StopReason: result.StopReason,
		Finding: "Incomplete coverage. Missing actions do not establish a defective pointer or wheel."}
	if result.Status == inputtest.StatusPass {
		view.Finding = "Motion, button and advertised wheel criteria completed on every reported pointer interface. Other pointer functions were not tested."
	}
	for _, device := range result.Devices {
		row := pointerDeviceDocument{Interface: device.Interface, Status: string(device.Status),
			WheelCoverage: "No accepted wheel observations; absence of wheel capability was not established."}
		if device.Supported && device.Readable && device.Capabilities != nil {
			row.WheelCoverage = "Both directions are required on each advertised axis."
			if len(device.Capabilities.WheelAxes) == 0 {
				row.WheelCoverage = "No wheel axes were advertised by this readable relative-pointer interface."
			}
			for index, wheel := range device.Wheels {
				stream := "legacy events"
				if device.Capabilities.WheelAxes[index].HighResolution {
					stream = "high-resolution events"
				}
				row.Wheels = append(row.Wheels, pointerWheelDocument{Axis: wheel.Axis, Stream: stream,
					Positive: wheel.PositiveFrames, Negative: wheel.NegativeFrames})
			}
		}
		view.Devices = append(view.Devices, row)
	}
	return view, nil
}

func appendPointerPDF(lines *pdfLines, pointer *pointerDocument) {
	if pointer == nil {
		return
	}
	lines.add("", "GUIDED POINTER WHEEL OBSERVATIONS")
	lines.field("Scope", pointer.Scope, "  ")
	lines.field("Status", pointer.Status, "  ")
	lines.field("Finding", pointer.Finding, "  ")
	lines.field("Stop reason", pointer.StopReason, "  ")
	if len(pointer.Devices) == 0 {
		lines.add("No pointer interface observations were obtained.")
	}
	for _, device := range pointer.Devices {
		lines.field("Interface", device.Interface, "  ")
		lines.field("Status", device.Status, "    ")
		lines.field("Wheel coverage", device.WheelCoverage, "    ")
		for _, wheel := range device.Wheels {
			lines.field("Wheel axis", wheel.Axis, "    ")
			lines.field("Credited stream", wheel.Stream, "      ")
			lines.field("Positive frames", fmt.Sprintf("%d", wheel.Positive), "      ")
			lines.field("Negative frames", fmt.Sprintf("%d", wheel.Negative), "      ")
		}
	}
}
