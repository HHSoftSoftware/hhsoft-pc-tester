package render

import (
	"fmt"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

const keyboardScope = "Press and release left Shift, right Shift, Up, Down, Left, and Right on each keyboard interface. Only these six controls are checked; typed text is not recorded."

type keyboardDocument struct {
	Scope, Status, Finding, StopReason string
	Devices                            []keyboardDeviceDocument
}

type keyboardDeviceDocument struct {
	Interface, Status, Completed, Missing string
}

func keyboardReportDocument(payload app.InventoryPayload) (*keyboardDocument, error) {
	result, err := app.ValidatedKeyboard(payload)
	if err != nil || result == nil {
		return nil, err
	}
	view := &keyboardDocument{Scope: keyboardScope, Status: string(result.Status), StopReason: result.StopReason,
		Finding: "Incomplete coverage. Missing actions do not establish a defective key."}
	if result.Status == inputtest.StatusPass {
		view.Finding = "All six controls completed on every reported keyboard interface. Other keys and keyboard functions were not tested."
	}
	labels := [6]string{"left Shift", "right Shift", "Up", "Down", "Left", "Right"}
	for _, device := range result.Devices {
		completed := 0
		missing := make([]string, 0, len(labels))
		for index, label := range labels {
			if len(device.Actions) == len(labels) && device.Actions[index].PairCompleted {
				completed++
			} else {
				missing = append(missing, label)
			}
		}
		detail := strings.Join(missing, ", ")
		if detail == "" {
			detail = "None"
		}
		view.Devices = append(view.Devices, keyboardDeviceDocument{Interface: device.Interface, Status: string(device.Status),
			Completed: fmt.Sprintf("%d of 6 press-and-release pairs", completed), Missing: detail})
	}
	return view, nil
}

func appendKeyboardPDF(lines *pdfLines, keyboard *keyboardDocument) {
	if keyboard == nil {
		return
	}
	lines.add("", "GUIDED KEYBOARD OBSERVATIONS")
	lines.field("Scope", keyboard.Scope, "  ")
	lines.field("Status", keyboard.Status, "  ")
	lines.field("Finding", keyboard.Finding, "  ")
	lines.field("Stop reason", keyboard.StopReason, "  ")
	if len(keyboard.Devices) == 0 {
		lines.add("No keyboard interface observations were obtained.")
	}
	for _, device := range keyboard.Devices {
		lines.field("Interface", device.Interface, "  ")
		lines.field("Status", device.Status, "    ")
		lines.field("Completed controls", device.Completed, "    ")
		lines.field("Missing controls", device.Missing, "    ")
	}
}
