package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

func pointerRow(t *testing.T, payload *InventoryPayload) *TestResult {
	t.Helper()
	for index := range payload.TestResults {
		if payload.TestResults[index].ID == "input.pointer.quick" {
			return &payload.TestResults[index]
		}
	}
	t.Fatal("synthetic pointer row unavailable")
	return nil
}

func changePointer(t *testing.T, payload *InventoryPayload, change func(*inputtest.PointerResult)) {
	t.Helper()
	row := pointerRow(t, payload)
	var result inputtest.PointerResult
	if json.Unmarshal(row.Evidence, &result) != nil {
		t.Fatal("synthetic pointer evidence unavailable")
	}
	change(&result)
	var err error
	row.Evidence, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidatedPointerStandaloneAndSessionOwnTheirViews(t *testing.T) {
	for _, name := range []string{"pointer-v2-standalone-pass", "session-v5-pointer-v2-pass"} {
		t.Run(name, func(t *testing.T) {
			payload := thermalFixture(t, name)
			before, _ := json.Marshal(payload)
			result, err := ValidatedPointer(payload)
			if err != nil || result == nil || result.Schema != inputtest.PointerSchema || len(result.Devices) != 1 || len(result.Devices[0].Wheels) != 2 {
				t.Fatalf("valid wheel observations rejected: %v", err)
			}
			result.Devices[0].Capabilities.WheelAxes[0].Axis = "unrelated"
			result.Devices[0].Wheels[0].PositiveFrames = 0
			again, err := ValidatedPointer(payload)
			after, _ := json.Marshal(payload)
			if err != nil || again.Devices[0].Capabilities.WheelAxes[0].Axis != "vertical" || again.Devices[0].Wheels[0].PositiveFrames != 1 || !bytes.Equal(before, after) {
				t.Fatal("pointer view shared storage or rewrote signed evidence")
			}
			thermal, err := ValidatedSessionThermal(payload)
			if err != nil || (thermal == nil) != (name == "pointer-v2-standalone-pass") {
				t.Fatal("standalone observations acquired or lost session coverage")
			}
		})
	}
}

func TestPointerV1RemainsMovementAndButtonEvidence(t *testing.T) {
	old := thermalFixture(t, "pointer-v1-standalone-pass")
	oldRow := *pointerRow(t, &old)
	for _, schema := range []string{"", sessionevidence.SchemaV4, sessionevidence.Schema} {
		payload := thermalFixture(t, "session-v5-pointer-v2-pass")
		*pointerRow(t, &payload) = oldRow
		mutateIntegrity(t, &payload, func(value *sessionevidence.IntegrityEvidence) {
			if schema == "" {
				value.Session = nil
				return
			}
			value.Session.Schema = schema
			if schema == sessionevidence.SchemaV4 {
				value.Session.Workers = append(value.Session.Workers[:4], value.Session.Workers[5:]...)
			}
		})
		if schema == sessionevidence.SchemaV4 {
			keyboardRow(t, &payload).Version = 1
		}
		before := append([]byte(nil), pointerRow(t, &payload).Evidence...)
		view, err := ValidatedPointer(payload)
		if err != nil || view == nil || view.Schema != inputtest.PointerSchemaV1 || view.Devices[0].Wheels != nil || view.Devices[0].Capabilities.WheelAxes != nil {
			t.Fatalf("historical pointer scope changed: %v", err)
		}
		if !bytes.Equal(before, pointerRow(t, &payload).Evidence) {
			t.Fatal("historical pointer bytes were reserialized")
		}
	}
}

func TestPointerClaimsRejectVersionShapeAndInventoryConfusion(t *testing.T) {
	cases := []struct {
		name   string
		change func(*InventoryPayload)
	}{
		{"no coverage", func(p *InventoryPayload) { p.CoverageScope = "" }},
		{"direct capture", func(p *InventoryPayload) { p.CoverageScope = CoverageDirectCapture }},
		{"relabel definition", func(p *InventoryPayload) { pointerRow(t, p).Version = 1 }},
		{"unknown definition", func(p *InventoryPayload) { pointerRow(t, p).Version = 3 }},
		{"relabel row", func(p *InventoryPayload) { pointerRow(t, p).ID = "input.unknown.quick" }},
		{"wrong report outcome", func(p *InventoryPayload) { pointerRow(t, p).Status = TestInconclusive }},
		{"not scheduled", func(p *InventoryPayload) { pointerRow(t, p).PlannedStatus = PlannedExcluded }},
		{"missing inventory", func(p *InventoryPayload) { p.Components = nil }},
		{"changed identity", func(p *InventoryPayload) {
			changePointer(t, p, func(v *inputtest.PointerResult) { v.Devices[0].Identity.Product++ })
		}},
		{"unknown bitmap width", func(p *InventoryPayload) {
			for i := range p.Components[0].Attributes {
				if p.Components[0].Attributes[i].Name == "bitmap_word_bits" {
					p.Components[0].Attributes[i].Value = "unknown"
				}
			}
		}},
		{"unobserved final interface", func(p *InventoryPayload) {
			second := p.Components[0]
			second.ID = "event1"
			second.Attributes = append([]inventory.Attribute(nil), second.Attributes...)
			for i := range second.Attributes {
				if second.Attributes[i].Name == "interface" {
					second.Attributes[i].Value = "event1"
				}
				if second.Attributes[i].Name == "handlers" {
					second.Attributes[i].Value = "mouse1 event1"
				}
			}
			p.Components = append(p.Components, second)
		}},
		{"nil wheel observations", func(p *InventoryPayload) {
			changePointer(t, p, func(v *inputtest.PointerResult) { v.Devices[0].Wheels = nil })
		}},
		{"nil advertised axes", func(p *InventoryPayload) {
			changePointer(t, p, func(v *inputtest.PointerResult) { v.Devices[0].Capabilities.WheelAxes = nil })
		}},
		{"missing wheel direction", func(p *InventoryPayload) {
			changePointer(t, p, func(v *inputtest.PointerResult) { v.Devices[0].Wheels[0].NegativeFrames = 0 })
		}},
		{"wheel counter overflow", func(p *InventoryPayload) {
			changePointer(t, p, func(v *inputtest.PointerResult) { v.Devices[0].Wheels[0].PositiveFrames = ^uint64(0) })
		}},
		{"unadvertised axis", func(p *InventoryPayload) {
			changePointer(t, p, func(v *inputtest.PointerResult) { v.Devices[0].Wheels[0].Axis = "depth" })
		}},
		{"schema alias", func(p *InventoryPayload) {
			changePointer(t, p, func(v *inputtest.PointerResult) { v.Schema = strings.ToUpper(v.Schema) })
		}},
		{"unknown schema", func(p *InventoryPayload) {
			changePointer(t, p, func(v *inputtest.PointerResult) { v.Schema = "hhsoft.pc-tester.pointer-functional/v3" })
		}},
	}
	for _, fixture := range cases {
		t.Run(fixture.name, func(t *testing.T) {
			payload := thermalFixture(t, "pointer-v2-standalone-pass")
			fixture.change(&payload)
			if ValidateCoverage(payload) == nil {
				t.Fatal("forged pointer claim accepted")
			}
			if view, err := ValidatedPointer(payload); err == nil || view != nil {
				t.Fatal("forged pointer observations produced a view")
			}
			if _, err := ValidatedSessionThermal(payload); err == nil {
				t.Fatal("standalone pointer validation was skipped by thermal accessor")
			}
		})
	}
	for _, bits := range []string{"803", "943", "43", "1843"} {
		payload := thermalFixture(t, "pointer-v2-standalone-pass")
		for i := range payload.Components[0].Attributes {
			if payload.Components[0].Attributes[i].Name == "rel_capabilities" {
				payload.Components[0].Attributes[i].Value = bits
			}
		}
		if ValidateCoverage(payload) == nil {
			t.Fatal("changed advertised wheel bit accepted")
		}
	}
}

func TestPointerRetainedDevicePassCannotHideBehindCancellation(t *testing.T) {
	payload := thermalFixture(t, "pointer-v2-standalone-pass")
	pointerRow(t, &payload).Status = TestAborted
	changePointer(t, &payload, func(v *inputtest.PointerResult) {
		v.Status = inputtest.StatusAborted
		v.StopReason = "operator_cancelled"
		v.Devices = append(v.Devices, inputtest.PointerDeviceResult{Interface: "event1", Status: inputtest.StatusInconclusive,
			Wheels: []inputtest.PointerWheelResult{}, Detail: "event interface unavailable"})
	})
	if view, err := ValidatedPointer(payload); err != nil || view == nil || len(view.Devices) != 2 || view.Devices[0].Status != inputtest.StatusPass {
		t.Fatalf("honest retained pointer pass rejected: %v", err)
	}
	changePointer(t, &payload, func(v *inputtest.PointerResult) { v.Devices[0].Identity.Bus++ })
	if ValidateCoverage(payload) == nil {
		t.Fatal("aggregate cancellation hid a forged retained pass")
	}
}

func TestPointerPassBindsEveryInterfaceSeparately(t *testing.T) {
	payload := thermalFixture(t, "pointer-v2-standalone-pass")
	second := payload.Components[0]
	second.ID = "event1"
	second.Attributes = append([]inventory.Attribute(nil), second.Attributes...)
	for index := range second.Attributes {
		switch second.Attributes[index].Name {
		case "interface":
			second.Attributes[index].Value = "event1"
		case "handlers":
			second.Attributes[index].Value = "mouse1 event1"
		case "product":
			second.Attributes[index].Value = "0003"
		}
	}
	payload.Components = append(payload.Components, second)
	payload.SystemFingerprint.UnidentifiedParts = 2
	for index := range payload.TestResults {
		row := &payload.TestResults[index]
		if row.ID == "run.integrity.quick" {
			var value sessionevidence.IntegrityEvidence
			if json.Unmarshal(row.Evidence, &value) != nil {
				t.Fatal("synthetic integrity evidence unavailable")
			}
			value.PlannedComponentCount, value.FinalComponentCount = 2, 2
			row.Evidence, _ = json.Marshal(value)
		}
	}
	changePointer(t, &payload, func(v *inputtest.PointerResult) {
		device := v.Devices[0]
		device.Interface = "event1"
		identity := *device.Identity
		identity.Product = 3
		device.Identity = &identity
		device.Wheels = append([]inputtest.PointerWheelResult(nil), device.Wheels...)
		v.Devices = append(v.Devices, device)
	})
	if result, err := ValidatedPointer(payload); err != nil || result == nil || len(result.Devices) != 2 {
		t.Fatalf("complete multi-interface observations rejected: %v", err)
	}
	changePointer(t, &payload, func(v *inputtest.PointerResult) {
		v.Devices[0].Wheels[0].PositiveFrames = 2
		v.Devices[1].Wheels[0].PositiveFrames = 0
	})
	if ValidateCoverage(payload) == nil {
		t.Fatal("directions from one interface satisfied another interface")
	}
}

func TestPointerV2SessionRequiresCurrentSchemaAndWorkerInterval(t *testing.T) {
	for _, defect := range []string{"v4", "v3", "duration", "wrong worker outcome"} {
		t.Run(defect, func(t *testing.T) {
			payload := thermalFixture(t, "session-v5-pointer-v2-pass")
			if defect == "duration" {
				changePointer(t, &payload, func(v *inputtest.PointerResult) { v.DurationMilliseconds = 7 })
			}
			if defect == "v3" {
				payload.TestResults[len(payload.TestResults)-1].Evidence = []byte(`{"session":{"schema":"hhsoft.pc-tester.quick-session/v3"}}`)
			} else {
				mutateIntegrity(t, &payload, func(v *sessionevidence.IntegrityEvidence) {
					if defect == "v4" {
						v.Session.Schema = sessionevidence.SchemaV4
						v.Session.Workers = append(v.Session.Workers[:4], v.Session.Workers[5:]...)
					}
					if defect == "wrong worker outcome" {
						v.Session.Workers[5].Outcome = "inconclusive"
					}
				})
				if defect == "v4" {
					keyboardRow(t, &payload).Version = 1
				}
			}
			if ValidateCoverage(payload) == nil {
				t.Fatal("new pointer evidence escaped its session binding")
			}
		})
	}
}

func TestPointerStandaloneNonfunctionalNotesRemainStrict(t *testing.T) {
	for _, disposition := range []string{"excluded", "not_present", "unsupported", "missing_driver", "error", "aborted"} {
		payload := thermalFixture(t, "pointer-v2-standalone-pass")
		row := pointerRow(t, &payload)
		row.PlannedStatus, row.Status = PlannedStatus(disposition), TestSkip
		if disposition == "missing_driver" {
			row.Status = TestInconclusive
		}
		if disposition == "error" || disposition == "aborted" {
			row.PlannedStatus, row.Status = PlannedScheduled, TestStatus(disposition)
		}
		row.Evidence, _ = json.Marshal(map[string]string{"schema": "hhsoft.pc-tester.execution-note/v1", "status": string(row.Status), "source_status": disposition, "detail": "no observations were collected"})
		if view, err := ValidatedPointer(payload); err != nil || view != nil {
			t.Fatalf("nonfunctional note rejected: %v", err)
		}
		for _, hidden := range []string{inputtest.PointerSchema, strings.ToUpper(inputtest.PointerSchema), "hhsoft.pc-tester.pointer-functional/v99"} {
			original := append([]byte(nil), row.Evidence...)
			row.Evidence = bytes.Replace(original, []byte("no observations were collected"), []byte(hidden), 1)
			if ValidateCoverage(payload) == nil {
				t.Fatal("execution note hid functional pointer evidence")
			}
			row.Evidence = original
		}
	}
	payload := thermalFixture(t, "session-v4-unavailable")
	pointerRow(t, &payload).Version = 2
	if ValidateCoverage(payload) == nil {
		t.Fatal("a nonfunctional note relabeled historical v4 pointer coverage")
	}
}

func TestPointerRawClaimsSelectStrictValidation(t *testing.T) {
	for _, raw := range []string{
		`{"schema":"hhsoft.pc-tester.pointer-functional/v1"}`,
		`{"schema":"HHSOFT.PC-TESTER.POINTER-FUNCTIONAL/V2"}`,
		`{"schema":"hhsoft.pc-tester.pointer-functional/v99"}`,
		`{"evidence":{"schem\u0061":"hhsoft.pc-tester.pointer-functional/v2"},"evidence":null}`,
		`{"id":"INPUT.POINTER.QUICK","version":2,"id":"cpu.quick","version":1}`,
		`{"id":"input.pointer.quick","version":"2"}`,
		`{"id":"input.pointer.quick","version":null}`,
		`{"wheel_axe\u0073":[]}`,
		`{"WHEELS":null}`,
		`{"schema":"hhsoft.pc-tester.pointer-functional/v2"`,
	} {
		if !HasPointerClaim([]byte(raw)) {
			t.Fatal("new, hidden or malformed pointer claim lost strict validation")
		}
	}
	for _, raw := range [][]byte{nil, {}, []byte(`{"id":"input.pointer.quick","version":1}`), []byte(`{"id":"input.pointer.quick","state":"completed"}`), []byte(`{"id":"cpu.quick","version":2}`)} {
		if HasPointerClaim(raw) {
			t.Fatal("unversioned historical worker acquired a pointer claim")
		}
	}
}
