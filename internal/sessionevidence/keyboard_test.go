package sessionevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
)

func TestOriginalV4FixturesRemainStrict(t *testing.T) {
	pins := map[string]string{
		"session-v4-global-heat.json": "196e214c0ab86bfc4d37b2e48ebcb324c9a66a9ffb35cd4f4c0502778ae8f4f7",
		"session-v4-local-heat.json":  "57ef4ac1a2bf04f43bb5d64ebba0fae22ec7aee8ce333dfcc8750db3abf2fc8e",
		"session-v4-unavailable.json": "105e9bf0b0b62e0cf3a58744e7b0384cac190ef5132f09b85c4c5d993256fc78",
	}
	for name, want := range pins {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "app", "testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(raw)
			if hex.EncodeToString(digest[:]) != want {
				t.Fatal("historical fixture bytes changed")
			}
			var payload struct {
				TestResults []struct {
					ID            string          `json:"id"`
					Version       int             `json:"version"`
					Status        string          `json:"status"`
					PlannedStatus string          `json:"planned_status"`
					Evidence      json.RawMessage `json:"evidence"`
				} `json:"test_results"`
			}
			if json.Unmarshal(raw, &payload) != nil {
				t.Fatal("fixture JSON invalid")
			}
			var journal *Journal
			bindings := []ResultBinding{}
			for _, row := range payload.TestResults {
				bindings = append(bindings, ResultBinding{ID: row.ID, Version: row.Version, ReportStatus: row.Status, PlannedStatus: row.PlannedStatus, Evidence: row.Evidence})
				if row.ID == "run.integrity.quick" {
					integrity, err := DecodeIntegrity(row.Evidence)
					if err != nil {
						t.Fatal(err)
					}
					journal = integrity.Session
				}
			}
			if journal == nil || journal.Schema != SchemaV4 || len(journal.Workers) != 10 || ValidateBindings(*journal, bindings) != nil {
				t.Fatal("original v4 report no longer validates")
			}
			for _, defect := range []string{"version", "initial", "join", "extra_worker", "modern_keyboard", "functional_case_alias"} {
				t.Run(defect, func(t *testing.T) {
					value := CloneJournal(*journal)
					rows := append([]ResultBinding(nil), bindings...)
					switch defect {
					case "version":
						value.Schema = Schema
					case "initial":
						value.Thermal.Initial.Completed = false
					case "join":
						value.Thermal.Joined = false
					case "extra_worker":
						value.Workers = append(value.Workers, WorkerRecord{ID: "input.keyboard.quick", State: "not_scheduled", Outcome: "excluded"})
					case "modern_keyboard":
						for index := range rows {
							if rows[index].ID == "input.keyboard.quick" {
								rows[index].Version = 2
							}
						}
					case "functional_case_alias":
						for index := range rows {
							if rows[index].ID == "input.keyboard.quick" {
								rows[index].Evidence, _ = json.Marshal(map[string]string{"schema": strings.ToUpper(inputtest.KeyboardSchema)})
							}
						}
					}
					if ValidateBindings(value, rows) == nil {
						t.Fatal("invalid historical claim accepted")
					}
				})
			}
		})
	}
}

func keyboardBindingFixture(t *testing.T) (Journal, []ResultBinding) {
	t.Helper()
	j := shortJournal()
	j.Workers[4] = WorkerRecord{ID: "input.keyboard.quick", State: "completed", Outcome: "pass", Timing: &WorkerTiming{StartedMilliseconds: 10, FinishedMilliseconds: 30}}
	j.Safety = append(j.Safety[:1:1], append([]SafetyRecord{{Phase: "before-input.keyboard.quick", Result: j.Safety[0].Result}}, j.Safety[1:]...)...)
	rows := shortBindings(j)
	device := inputtest.KeyboardDeviceResult{Interface: "event0", Status: inputtest.StatusPass, Supported: true, Readable: true,
		Identity:     &inputtest.KeyboardIdentity{Bus: 3, Vendor: 1, Product: 2, Version: 1},
		Capabilities: &inputtest.KeyboardCapabilities{EventKey: true, FixedKeys: []string{}}, Actions: []inputtest.KeyboardAction{},
		BaselineNeutral: true, FinalNeutral: true, CleanupConfirmed: true}
	for _, action := range inputtest.KeyboardActions() {
		device.Capabilities.FixedKeys = append(device.Capabilities.FixedKeys, action)
		device.Actions = append(device.Actions, inputtest.KeyboardAction{Name: action, PressObserved: true, ReleaseObserved: true, PairCompleted: true})
	}
	result := inputtest.KeyboardResult{Schema: inputtest.KeyboardSchema, Status: inputtest.StatusPass, StopReason: "completed", LimitMilliseconds: 10, DurationMilliseconds: 10, CleanupConfirmed: true, Devices: []inputtest.KeyboardDeviceResult{device}}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	rows[4] = ResultBinding{ID: "input.keyboard.quick", Version: 2, PlannedStatus: "scheduled", ReportStatus: "pass", Evidence: raw}
	return j, rows
}

func TestKeyboardJournalBindingRejectsDowngradesAndLostCoverage(t *testing.T) {
	j, rows := keyboardBindingFixture(t)
	if ValidateBindings(j, rows) != nil {
		t.Fatal("valid v5 keyboard evidence rejected")
	}
	for _, defect := range []string{"version", "schema", "duration", "status", "cleanup", "empty_devices", "misplaced", "duplicate_schema", "unknown_journal"} {
		t.Run(defect, func(t *testing.T) {
			value := CloneJournal(j)
			bindings := append([]ResultBinding(nil), rows...)
			var keyboard inputtest.KeyboardResult
			if json.Unmarshal(rows[4].Evidence, &keyboard) != nil {
				t.Fatal("fixture decode failed")
			}
			switch defect {
			case "version":
				bindings[4].Version = 1
			case "schema":
				value.Schema = SchemaV4
			case "unknown_journal":
				value.Schema = "hhsoft.pc-tester.quick-session/v99"
			case "duration":
				keyboard.DurationMilliseconds = 21
				keyboard.LimitMilliseconds = 30
			case "status":
				keyboard.Status = inputtest.StatusInconclusive
				keyboard.StopReason = "observation_timeout"
				keyboard.Detail = "observation_timeout"
			case "cleanup":
				keyboard.Status = inputtest.StatusError
				keyboard.StopReason = "cleanup_unconfirmed"
				keyboard.Detail = "cleanup_unconfirmed"
				keyboard.CleanupConfirmed = false
			case "empty_devices":
				keyboard.Devices = []inputtest.KeyboardDeviceResult{}
			case "misplaced":
				bindings[5].Evidence = rows[4].Evidence
			case "duplicate_schema":
				bindings[4].Evidence = append([]byte(`{"schema":"ignored",`), rows[4].Evidence[1:]...)
			}
			if defect == "duration" || defect == "status" || defect == "cleanup" || defect == "empty_devices" {
				bindings[4].Evidence, _ = json.Marshal(keyboard)
			}
			if ValidateBindings(value, bindings) == nil {
				t.Fatal("invalid keyboard claim accepted")
			}
		})
	}
}

func TestStandaloneKeyboardNotesCannotClaimExecution(t *testing.T) {
	for _, disposition := range []string{"excluded", "not_present", "unsupported", "missing_driver"} {
		t.Run(disposition, func(t *testing.T) {
			note := executionNote{Schema: "hhsoft.pc-tester.execution-note/v1", Status: reportStatus(disposition), SourceStatus: disposition, Detail: "not run"}
			raw, _ := json.Marshal(note)
			binding := ResultBinding{ID: "input.keyboard.quick", Version: 2, PlannedStatus: disposition, ReportStatus: note.Status, Evidence: raw}
			if ValidateUnexecutedKeyboard(binding) != nil {
				t.Fatal("unexecuted disposition rejected")
			}
			for _, defect := range []string{"version", "scheduled", "pass", "source", "schema", "hidden_claim", "case_alias", "unicode_alias", "escaped_alias"} {
				t.Run(defect, func(t *testing.T) {
					value, changed := binding, note
					switch defect {
					case "version":
						value.Version = 1
					case "scheduled":
						value.PlannedStatus = "scheduled"
					case "pass":
						value.ReportStatus = "pass"
						changed.Status = "pass"
					case "source":
						changed.SourceStatus = "aborted"
					case "schema":
						changed.Schema = inputtest.KeyboardSchema
					case "hidden_claim":
						changed.Detail = inputtest.KeyboardSchema
					case "case_alias", "escaped_alias":
						changed.Detail = strings.ToUpper(inputtest.KeyboardSchema)
					case "unicode_alias":
						changed.Detail = strings.Replace(strings.ToUpper(inputtest.KeyboardSchema), "KEYBOARD", "\u212aEYBOARD", 1)
					}
					value.Evidence, _ = json.Marshal(changed)
					if defect == "escaped_alias" {
						value.Evidence = []byte(strings.Replace(string(value.Evidence), "KEYBOARD", `\u004bEYBOARD`, 1))
					}
					if ValidateUnexecutedKeyboard(value) == nil {
						t.Fatal("executed or substituted evidence accepted")
					}
				})
			}
		})
	}
}

func TestV5KeyboardNotesCannotHideFunctionalClaims(t *testing.T) {
	for _, claim := range []string{inputtest.KeyboardSchema, strings.ToUpper(inputtest.KeyboardSchema), strings.Replace(inputtest.KeyboardSchema, "keyboard", "\u212aeyboard", 1)} {
		j := shortJournal()
		rows := shortBindings(j)
		rows[4].Evidence, _ = json.Marshal(executionNote{Schema: "hhsoft.pc-tester.execution-note/v1", Status: "skip", SourceStatus: "excluded", Detail: claim})
		if ValidateBindings(j, rows) == nil {
			t.Fatal("v5 note hid a functional keyboard claim")
		}
	}
}
