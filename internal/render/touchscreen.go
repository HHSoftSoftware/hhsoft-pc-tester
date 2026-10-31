package render

import (
	"fmt"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

const touchscreenScope = "Guided single-contact DIRECT X/Y direction and release criteria. Counts are engineering criteria, not distance or full surface coverage. Calibration, multitouch, gestures, pressure and stylus behavior were not tested."

type touchscreenDocument struct {
	Scope, Status, Finding, StopReason      string
	LimitMilliseconds, DurationMilliseconds int64
	CleanupConfirmed                        bool
	Devices                                 []touchscreenDeviceDocument
}

type touchscreenDeviceDocument struct {
	Interface, Status, Finding, Identity, Capabilities, Configuration       string
	Readable, Supported, BaselineNeutral, FinalNeutral, CleanupConfirmed    bool
	Frames, ContactStarts, ContactReleases, XYBaselines, QualifyingContacts uint64
	XPositive, XNegative, YPositive, YNegative                              uint64
}

// All strings and nested slices in this document are constructed from a fresh
// strict decode before an output writer can run. No caller attributes are kept.
func touchscreenReportDocument(payload app.InventoryPayload) (*touchscreenDocument, error) {
	result, err := app.ValidatedTouchscreen(payload)
	if err != nil || result == nil {
		return nil, err
	}
	view := &touchscreenDocument{Scope: touchscreenScope, Status: string(result.Status), StopReason: result.StopReason,
		LimitMilliseconds: result.LimitMilliseconds, DurationMilliseconds: result.DurationMilliseconds,
		CleanupConfirmed: result.CleanupConfirmed,
		Finding:          "Criteria were not completed on every candidate. Missing actions do not establish a defective touchscreen."}
	if result.Status == inputtest.StatusPass {
		view.Finding = "The limited contact, direction and release criteria completed on every reported candidate."
	}
	for _, device := range result.Devices {
		row := touchscreenDeviceDocument{Interface: device.Interface, Status: string(device.Status),
			Readable: device.Readable, Supported: device.Supported, BaselineNeutral: device.BaselineNeutral,
			FinalNeutral: device.FinalNeutral, CleanupConfirmed: device.CleanupConfirmed,
			Frames: device.Frames, ContactStarts: device.ContactStarts, ContactReleases: device.ContactReleases,
			XYBaselines: device.XYBaselines, QualifyingContacts: device.QualifyingContacts,
			XPositive: device.XPositive, XNegative: device.XNegative, YPositive: device.YPositive, YNegative: device.YNegative,
			Identity: "Unavailable", Capabilities: "Unavailable; an empty advertised set was not established.",
			Configuration: "Unavailable for unsupported, unreadable or uninitialized observations.",
			Finding:       "Incomplete criteria or terminal proof; this does not identify a hardware defect."}
		if !device.Readable {
			row.Finding = "No readable observation was established."
		}
		if device.Readable && !device.Supported {
			row.Finding = "The advertised interface is outside the supported single-contact shape."
		}
		if device.Status == inputtest.StatusPass {
			row.Finding = "Contact, direction, release and terminal criteria completed."
		}
		if device.Identity != nil {
			id := device.Identity
			row.Identity = fmt.Sprintf("bus=%04x vendor=%04x product=%04x version=%04x", id.Bus, id.Vendor, id.Product, id.Version)
		}
		if device.Capabilities != nil {
			c := device.Capabilities
			axes := make([]uint16, len(c.Axes))
			for i, axis := range c.Axes {
				axes[i] = axis.Code
			}
			row.Capabilities = fmt.Sprintf("EV=%v KEY=%v REL=%v ABS=%v PROP=%v MSC=%v", c.Events, c.Keys, c.Relative, axes, c.Properties, c.Misc)
			if device.Supported && device.XThreshold > 0 && device.YThreshold > 0 {
				parts := []string{fmt.Sprintf("Direction criteria X=%d Y=%d; static admitted configuration, not measured travel.", device.XThreshold, device.YThreshold)}
				for _, axis := range c.Axes {
					parts = append(parts, fmt.Sprintf("ABS %d: minimum=%d maximum=%d fuzz=%d flat=%d resolution=%d",
						axis.Code, axis.Minimum, axis.Maximum, axis.Fuzz, axis.Flat, axis.Resolution))
				}
				row.Configuration = strings.Join(parts, " ")
			}
		}
		view.Devices = append(view.Devices, row)
	}
	return view, nil
}

func appendTouchscreenPDF(lines *pdfLines, view *touchscreenDocument) {
	if view == nil {
		return
	}
	lines.add("", "GUIDED SINGLE-CONTACT TOUCHSCREEN OBSERVATIONS")
	lines.field("Scope", view.Scope, "  ")
	lines.field("Status", view.Status, "  ")
	lines.field("Finding", view.Finding, "  ")
	lines.field("Stop reason", view.StopReason, "  ")
	lines.field("Duration / limit (ms)", fmt.Sprintf("%d / %d", view.DurationMilliseconds, view.LimitMilliseconds), "  ")
	lines.field("Cleanup confirmed", fmt.Sprintf("%t", view.CleanupConfirmed), "  ")
	if len(view.Devices) == 0 {
		lines.add("No touchscreen interface observations were obtained.")
	}
	for _, row := range view.Devices {
		lines.field("Interface", row.Interface, "  ")
		lines.field("Status", row.Status, "    ")
		lines.field("Finding", row.Finding, "    ")
		lines.field("Readable / supported", fmt.Sprintf("%t / %t", row.Readable, row.Supported), "    ")
		lines.field("Numeric identity", row.Identity, "    ")
		lines.field("Advertised codes", row.Capabilities, "    ")
		lines.field("Configuration", row.Configuration, "    ")
		lines.field("Frames", fmt.Sprintf("%d", row.Frames), "    ")
		lines.field("Contact starts / releases", fmt.Sprintf("%d / %d", row.ContactStarts, row.ContactReleases), "    ")
		lines.field("Fresh XY baselines / qualifying contacts", fmt.Sprintf("%d / %d", row.XYBaselines, row.QualifyingContacts), "    ")
		lines.field("X positive / negative", fmt.Sprintf("%d / %d", row.XPositive, row.XNegative), "    ")
		lines.field("Y positive / negative", fmt.Sprintf("%d / %d", row.YPositive, row.YNegative), "    ")
		lines.field("Baseline / final neutral / cleanup", fmt.Sprintf("%t / %t / %t", row.BaselineNeutral, row.FinalNeutral, row.CleanupConfirmed), "    ")
	}
}
