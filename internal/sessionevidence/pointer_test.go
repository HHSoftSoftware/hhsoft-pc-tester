package sessionevidence

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

func pointerBindingFixture(t *testing.T) (Journal, []ResultBinding) {
	t.Helper()
	j := shortJournal()
	j.Workers[5] = WorkerRecord{ID: "input.pointer.quick", State: "completed", Outcome: "pass", Timing: &WorkerTiming{StartedMilliseconds: 10, FinishedMilliseconds: 30}}
	j.Safety = append(j.Safety[:1:1], append([]SafetyRecord{{Phase: "before-input.pointer.quick", Result: j.Safety[0].Result}}, j.Safety[1:]...)...)
	value := inputtest.PointerResult{Schema: inputtest.PointerSchema, Status: inputtest.StatusPass, StopReason: "completed", LimitMilliseconds: 10, DurationMilliseconds: 10,
		Devices: []inputtest.PointerDeviceResult{{Interface: "event0", Status: inputtest.StatusPass, Supported: true, Readable: true,
			Identity: &inputtest.PointerIdentity{Bus: 3, Vendor: 1, Product: 2, Version: 1},
			Capabilities: &inputtest.PointerCapabilities{Key: true, Relative: true, RelativeX: true, RelativeY: true, Buttons: []string{"left"},
				WheelAxes: []inputtest.PointerWheelCapability{{Axis: "vertical", Legacy: true}}},
			XPositive: 1, XNegative: 1, YPositive: 1, YNegative: 1, Frames: 4,
			Buttons: []inputtest.PointerButtonResult{{Button: "left", Presses: 1, Releases: 1, Pairs: 1}},
			Wheels:  []inputtest.PointerWheelResult{{Axis: "vertical", PositiveFrames: 1, NegativeFrames: 1}},
		}}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	rows := shortBindings(j)
	rows[5] = ResultBinding{ID: "input.pointer.quick", Version: 2, PlannedStatus: "scheduled", ReportStatus: "pass", Evidence: raw}
	return j, rows
}

func historicalPointerBinding(t *testing.T, current ResultBinding) ResultBinding {
	t.Helper()
	var raw map[string]any
	if json.Unmarshal(current.Evidence, &raw) != nil {
		t.Fatal("invalid owned pointer fixture")
	}
	raw["schema"] = inputtest.PointerSchemaV1
	for _, value := range raw["devices"].([]any) {
		device := value.(map[string]any)
		delete(device, "wheels")
		delete(device["capabilities"].(map[string]any), "wheel_axes")
	}
	current.Version = 1
	current.Evidence, _ = json.Marshal(raw)
	return current
}

func historicalPointerSession(j Journal, rows []ResultBinding) (Journal, []ResultBinding) {
	j.Schema = SchemaV4
	j.Workers = append(j.Workers[:4:4], j.Workers[5:]...)
	return j, append(rows[:4:4], rows[5:]...)
}

func TestPointerSessionVersionsRetainTheirOwnCoverage(t *testing.T) {
	j, rows := pointerBindingFixture(t)
	if ValidateBindings(j, rows) != nil {
		t.Fatal("current wheel evidence rejected")
	}
	rows[5] = historicalPointerBinding(t, rows[5])
	if ValidateBindings(j, rows) != nil {
		t.Fatal("v5 session lost historical movement/buttons-only coverage")
	}
	j, rows = historicalPointerSession(j, rows)
	if ValidateBindings(j, rows) != nil {
		t.Fatal("v4 session lost historical pointer evidence")
	}
}

func TestPointerSessionRejectsUnboundWheelClaims(t *testing.T) {
	for _, defect := range []string{"definition_downgrade", "future_definition", "historical_schema", "duration", "status", "misplaced", "duplicate_schema", "historical_session", "incomplete_wheel"} {
		t.Run(defect, func(t *testing.T) {
			j, rows := pointerBindingFixture(t)
			var value inputtest.PointerResult
			if json.Unmarshal(rows[5].Evidence, &value) != nil {
				t.Fatal("invalid owned fixture")
			}
			switch defect {
			case "definition_downgrade":
				rows[5].Version = 1
			case "future_definition":
				rows[5].Version = 3
			case "historical_schema":
				rows[5].Evidence = historicalPointerBinding(t, rows[5]).Evidence
			case "duration":
				value.LimitMilliseconds, value.DurationMilliseconds = 30, 21
			case "status":
				rows[5].ReportStatus = "inconclusive"
			case "misplaced":
				rows[6].Evidence = append([]byte(nil), rows[5].Evidence...)
			case "duplicate_schema":
				rows[5].Evidence = append([]byte(`{"schema":"ignored",`), rows[5].Evidence[1:]...)
			case "historical_session":
				j, rows = historicalPointerSession(j, rows)
			case "incomplete_wheel":
				value.Devices[0].Wheels[0].NegativeFrames = 0
			}
			if defect == "duration" || defect == "incomplete_wheel" {
				rows[5].Evidence, _ = json.Marshal(value)
			}
			if ValidateBindings(j, rows) == nil {
				t.Fatal("unbound or incomplete wheel claim accepted")
			}
		})
	}
}

func TestPointerNotesPreserveStandaloneFailureAndApplicability(t *testing.T) {
	for _, version := range []int{1, 2} {
		for _, disposition := range []string{"excluded", "not_present", "unsupported", "missing_driver", "error", "aborted"} {
			note := executionNote{Schema: "hhsoft.pc-tester.execution-note/v1", Status: reportStatus(disposition), SourceStatus: disposition, Detail: "no functional observations"}
			raw, _ := json.Marshal(note)
			planned := disposition
			if disposition == "error" || disposition == "aborted" {
				planned = "scheduled"
			}
			binding := ResultBinding{ID: "input.pointer.quick", Version: version, PlannedStatus: planned, ReportStatus: note.Status, Evidence: raw}
			if ValidatePointerNote(binding) != nil {
				t.Fatal("honest nonfunctional pointer outcome rejected")
			}
			for _, defect := range []string{"status", "source", "alias", "hidden_v1", "hidden_v2", "hidden_unknown", "extra", "duplicate"} {
				value, changed := binding, note
				switch defect {
				case "status":
					value.ReportStatus = "pass"
				case "source":
					changed.SourceStatus = "pass"
				case "alias":
					value.ID = "INPUT.POINTER.QUICK"
				case "hidden_v1":
					changed.Detail = inputtest.PointerSchemaV1
				case "hidden_v2":
					changed.Detail = strings.ToUpper(inputtest.PointerSchema)
				case "hidden_unknown":
					changed.Detail = "hhsoft.pc-tester.pointer-functional/v99"
				}
				value.Evidence, _ = json.Marshal(changed)
				if defect == "extra" {
					value.Evidence = append([]byte(`{"wheels":[],`), value.Evidence[1:]...)
				} else if defect == "duplicate" {
					value.Evidence = append([]byte(`{"schema":"ignored",`), value.Evidence[1:]...)
				}
				if ValidatePointerNote(value) == nil {
					t.Fatalf("pointer note accepted %s", defect)
				}
			}
		}
	}
}

func TestHistoricalSessionCannotCarryCurrentPointerNotes(t *testing.T) {
	j := shortJournal()
	rows := shortBindings(j)
	j, rows = historicalPointerSession(j, rows)
	if ValidateBindings(j, rows) != nil {
		t.Fatal("historical control rejected")
	}
	rows[4].Version = 2
	if ValidateBindings(j, rows) == nil {
		t.Fatal("v4 session claimed current pointer definition through an execution note")
	}
}
