package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

func renderPointerChange(t *testing.T, payload *app.InventoryPayload, change func(*inputtest.PointerResult)) {
	t.Helper()
	for index := range payload.TestResults {
		row := &payload.TestResults[index]
		if row.ID != "input.pointer.quick" {
			continue
		}
		var result inputtest.PointerResult
		if json.Unmarshal(row.Evidence, &result) != nil {
			t.Fatal("synthetic pointer evidence unavailable")
		}
		change(&result)
		row.Status = app.TestStatus(result.Status)
		var err error
		row.Evidence, err = json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("synthetic pointer row unavailable")
}

func TestPointerWheelSummariesUseValidatedAxisCounts(t *testing.T) {
	for _, name := range []string{"pointer-v2-standalone-pass", "session-v5-pointer-v2-pass"} {
		payload := thermalReportFixture(t, name)
		for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
			encoded, err := render(payload)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(strings.Fields(strings.ReplaceAll(string(encoded), ") Tj\nT*\n(", " ")), " ")
			for _, phrase := range []string{"vertical", "horizontal", "high-resolution events", "legacy events", "Positive frames", "Negative frames", "not detents, distance or resolution"} {
				if !strings.Contains(text, phrase) {
					t.Fatalf("wheel view omitted %q", phrase)
				}
			}
			if bytes.HasPrefix(encoded, []byte("<!doctype html>")) && bytes.Count(encoded, []byte("<pre>")) != 22 {
				t.Fatal("wheel summary replaced raw signed evidence rows")
			}
			if bytes.HasPrefix(encoded, []byte("%PDF")) && bytes.Count(encoded, []byte("Evidence:")) != 22 {
				t.Fatal("wheel PDF changed result coverage")
			}
		}
	}
}

func TestPointerWheelSummaryDistinguishesAbsenceFromUnavailable(t *testing.T) {
	for _, absent := range []bool{false, true} {
		payload := thermalReportFixture(t, "pointer-v2-standalone-pass")
		renderPointerChange(t, &payload, func(result *inputtest.PointerResult) {
			if absent {
				result.Devices[0].Capabilities.WheelAxes = []inputtest.PointerWheelCapability{}
				result.Devices[0].Wheels = []inputtest.PointerWheelResult{}
			} else {
				result.Status, result.StopReason, result.DurationMilliseconds = inputtest.StatusInconclusive, "observation_timeout", 30000
				result.Devices[0] = inputtest.PointerDeviceResult{Interface: "event0", Status: inputtest.StatusInconclusive, Wheels: []inputtest.PointerWheelResult{}, Detail: "event interface unavailable"}
			}
		})
		if absent {
			for i := range payload.Components[0].Attributes {
				if payload.Components[0].Attributes[i].Name == "rel_capabilities" {
					payload.Components[0].Attributes[i].Value = "3"
				}
			}
		}
		view, err := pointerReportDocument(payload)
		if err != nil || view == nil || len(view.Devices) != 1 || len(view.Devices[0].Wheels) != 0 {
			t.Fatalf("empty-wheel view failed: %v", err)
		}
		if strings.Contains(view.Devices[0].WheelCoverage, "No wheel axes were advertised") != absent {
			t.Fatal("unknown wheel capability became proven absence")
		}
		if !absent && (view.Status != "inconclusive" || !strings.Contains(view.Finding, "Incomplete coverage")) {
			t.Fatal("unreadable interface acquired a pass")
		}
	}
}

func TestPointerWheelPartialDirectionsDoNotBecomeHardwareFailure(t *testing.T) {
	payload := thermalReportFixture(t, "pointer-v2-standalone-pass")
	renderPointerChange(t, &payload, func(result *inputtest.PointerResult) {
		result.Status, result.StopReason = inputtest.StatusInconclusive, "observation_timeout"
		result.Devices[0].Status = inputtest.StatusInconclusive
		result.Devices[0].Wheels[0].NegativeFrames = 0
	})
	view, err := pointerReportDocument(payload)
	if err != nil || view == nil || view.Status != "inconclusive" || view.Devices[0].Wheels[0].Negative != 0 || !strings.Contains(view.Finding, "do not establish a defective") {
		t.Fatalf("incomplete wheel coverage changed: %v", err)
	}
	for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
		if _, err := render(payload); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPointerV1DoesNotAcquireWheelSummary(t *testing.T) {
	payload := thermalReportFixture(t, "pointer-v1-standalone-pass")
	if view, err := pointerReportDocument(payload); err != nil || view != nil {
		t.Fatal("historical pointer evidence acquired wheel qualification")
	}
	for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
		encoded, err := render(payload)
		if err != nil || bytes.Contains(bytes.ToLower(encoded), []byte("guided pointer wheel observations")) {
			t.Fatalf("historical pointer view changed: %v", err)
		}
	}
}

func TestMalformedPointerCannotReachRenderedViews(t *testing.T) {
	payload := thermalReportFixture(t, "pointer-v2-standalone-pass")
	renderPointerChange(t, &payload, func(result *inputtest.PointerResult) { result.Devices[0].Capabilities.WheelAxes = nil })
	for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
		if encoded, err := render(payload); err == nil || len(encoded) != 0 {
			t.Fatal("unvalidated pointer evidence reached the report")
		}
	}
}
