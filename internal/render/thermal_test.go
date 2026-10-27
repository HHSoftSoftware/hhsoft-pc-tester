package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func thermalReportFixture(t *testing.T, name string) app.InventoryPayload {
	t.Helper()
	encoded, err := os.ReadFile("../app/testdata/" + name + ".json")
	if err != nil {
		t.Fatal("synthetic report fixture unavailable")
	}
	var payload app.InventoryPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestThermalSummaryKeepsHistoricalSessionViewsByteIdentical(t *testing.T) {
	payload := thermalReportFixture(t, "historical-session-v3")
	html, err := InventoryHTMLForVersion(payload, "0.1.0-dev.0")
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := InventoryPDFForVersion(payload, "0.1.0-dev.0")
	if err != nil {
		t.Fatal(err)
	}
	// Recorded from the preceding renderer before the conditional thermal block.
	if len(html) != 29239 || fmt.Sprintf("%x", sha256.Sum256(html)) != "072ecdc65acd5bf36ec230400a9d2c965f96b00a24fb40abb240b1089d8b5642" ||
		len(pdf) != 19039 || fmt.Sprintf("%x", sha256.Sum256(pdf)) != "242c9d9834daf681cb6c463951d7dfb6bedd2470ac980f1f833b11c40c7953d9" {
		t.Fatal("historical session rendering changed")
	}
}

func TestThermalSelectionPreservesHistoricalAbsentEvidenceRenderingBoundary(t *testing.T) {
	payload := testPayload()
	payload.TestResults = []app.TestResult{{ID: "cpu.quick", Version: 1, Status: app.TestSkip}}
	for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
		_, err := render(payload)
		// Empty evidence already failed the renderer's generic bounds check.
		// The thermal selector must not reinterpret it as a new session claim.
		if err == nil || !strings.Contains(err.Error(), "report evidence") {
			t.Fatal("historical empty evidence changed its rendering contract")
		}
	}
}

func TestThermalSummariesDistinguishCoverageAndHeat(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		phrases []string
	}{
		{"session-v4-unavailable", []string{"Unavailable: no valid sensor readings were obtained.", "No limit observed in the available readings.", "90.000 C"}},
		{"session-v4-global-heat", []string{"Temperature limit reached.", "95.000 C", "thermal_limit"}},
		{"session-v4-local-heat", []string{"Temperature limit reached.", "42.000 C", "95.125 C", "cpu.quick", "2 to 5 ms after session start", "Local peaks do not change global sensor coverage"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			payload := thermalReportFixture(t, fixture.name)
			for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
				encoded, err := render(payload)
				if err != nil {
					t.Fatal(err)
				}
				for _, phrase := range append(fixture.phrases, "Sensor coverage", "Global observed peak", "Maximum sampling gap", "Monitor cleanup", "Sensor availability does not establish hardware health.") {
					if !bytes.Contains(encoded, []byte(phrase)) {
						t.Fatalf("thermal summary omitted %q", phrase)
					}
				}
				if bytes.Contains(bytes.ToLower(encoded), []byte("thermal pass")) {
					t.Fatal("sensor gaps became a hardware-health pass")
				}
				if bytes.HasPrefix(encoded, []byte("<!doctype html>")) {
					if !bytes.Contains(encoded, []byte("hhsoft.pc-tester.run-thermal/v1")) || bytes.Count(encoded, []byte("<pre>")) != 22 {
						t.Fatal("typed summary removed generic HTML evidence")
					}
				} else if bytes.Count(encoded, []byte("Evidence:")) != 22 {
					t.Fatal("typed summary removed generic PDF evidence")
				}
			}
		})
	}
}

func TestThermalSummaryShowsPartialCoverageWithoutHeat(t *testing.T) {
	payload := thermalReportFixture(t, "session-v4-unavailable")
	index := len(payload.TestResults) - 1
	value, err := sessionevidence.DecodeIntegrity(payload.TestResults[index].Evidence)
	if err != nil {
		t.Fatal(err)
	}
	sample := value.Session.Thermal.Final.Observation
	sample.Coverage, sample.Sensors, sample.PeakMilliCelsius = "available", 1, 41000
	last := *sample
	value.Session.Thermal.LastObservation = &last
	aggregate := &value.Session.Thermal.Aggregate
	aggregate.Coverage, aggregate.AvailableObservations, aggregate.UnavailableObservations = "partial", 1, 1
	aggregate.ValidReadings, aggregate.MaximumSensors, aggregate.PeakMilliCelsius = 1, 1, 41000
	payload.TestResults[index].Evidence, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, render := range []func(app.InventoryPayload) ([]byte, error){InventoryHTML, InventoryPDF} {
		encoded, err := render(payload)
		if err != nil || !bytes.Contains(encoded, []byte("Partial: some observations or readings were unavailable or rejected.")) ||
			!bytes.Contains(encoded, []byte("41.000 C")) {
			t.Fatalf("partial coverage missing: %v", err)
		}
	}
}

func TestThermalRenderingRejectsForgedEvidenceBeforeWriting(t *testing.T) {
	payload := thermalReportFixture(t, "session-v4-local-heat")
	index := len(payload.TestResults) - 1
	payload.TestResults[index].Evidence = bytes.Replace(payload.TestResults[index].Evidence,
		[]byte("sha256:d0ade46bdda5d5cde8647afd1a1b93c01d51346d10ffcc28cdf71c91688e46d9"), []byte("sha256:"+strings.Repeat("0", 64)), 1)
	var destination bytes.Buffer
	if RenderInventoryHTML(&destination, payload) == nil || destination.Len() != 0 {
		t.Fatal("invalid thermal claim reached HTML output")
	}
	if result, err := InventoryPDF(payload); err == nil || result != nil {
		t.Fatal("invalid thermal claim reached PDF output")
	}
}
