package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

// Existing finite CPU/memory/safety/inventory fixtures surround the real
// inspection dispatcher and profile-aware finalizer. This is no hardware run.
func inspectionSignedProfile(t *testing.T, profile string) bundle.Artifact {
	t.Helper()
	var h *sessionHarness
	switch profile {
	case "quick":
		h = newSessionHarness(t, "x86_64", nil)
		useStandardFinalizer(t, h, nil)
	case "standard":
		h = newStandardHarness(t, commandStressPass())
	case "extended":
		h = newExtendedMemoryHarness(t, commandMemoryPass())
	default:
		t.Fatal("unknown explicit fixture profile")
	}
	prior := h.dependencies.RunWorker
	dispatches := 0
	h.dependencies.RunWorker = func(ctx context.Context, worker sessionWorker, input io.Reader, out, diagnostic io.Writer) int {
		if worker.ID != "inspection.exterior.quick" {
			return prior(ctx, worker, input, out, diagnostic)
		}
		if worker.Version != 2 || worker.Timeout != 45*time.Second || filepath.Base(worker.OutputPath) != "exterior-condition.json" || ctx.Err() != nil {
			t.Fatal("current definition was not dispatched to the bounded condition command")
		}
		dispatches++
		h.calls = append(h.calls, worker.ID)
		code := runSessionWorker(ctx, worker, input, out, diagnostic)
		raw, err := os.ReadFile(worker.OutputPath)
		value, decodeErr := exteriorinspection.DecodeV2(raw)
		if err != nil || decodeErr != nil || code != 0 || value.Status != "pass" {
			t.Fatal("actual inspection command prerequisite", code, err, decodeErr)
		}
		h.clock = h.clock.Add(time.Duration(value.ObservationMilliseconds) * time.Millisecond)
		return code
	}
	answers := strings.NewReader("N\r\nA\r\nN\r\nN\r\nfuture\n")
	if code := runSessionWith(t.Context(), h.arguments, answers, &h.stdout, &h.stderr, h.dependencies); code != 0 || !h.finalized || dispatches != 1 {
		t.Fatalf("current connected profile failed: %d %s", code, &h.stderr)
	}
	if answers.Len() != len("future\n") {
		t.Fatal("four-answer command consumed later input")
	}
	if _, err := os.Lstat(filepath.Join(h.directory, "exterior-inspection.json")); !os.IsNotExist(err) {
		t.Fatal("current dispatch produced a legacy evidence file")
	}
	artifact := extendedMemoryArtifact(t, h)
	payload := stressSignedPayload(t, artifact)
	wantSchema, wantWorkers, wantBudget := "hhsoft.pc-tester.quick-session/v14", 14, int64(300000)
	if profile == "standard" {
		wantSchema, wantWorkers, wantBudget = "hhsoft.pc-tester.quick-session/v15", 15, 900000
	}
	if profile == "extended" {
		wantSchema, wantWorkers, wantBudget = "hhsoft.pc-tester.quick-session/v16", 15, 2700000
	}
	if h.journal.Schema != wantSchema || len(h.journal.Workers) != wantWorkers || h.journal.BudgetMilliseconds != wantBudget ||
		h.journal.Workers[wantWorkers-1].ID != "inspection.exterior.quick" || h.journal.Workers[wantWorkers-1].Outcome != "pass" {
		t.Fatal("literal current profile authority changed")
	}
	thermal, err := app.ValidatedSessionThermal(payload)
	if err != nil || thermal == nil {
		t.Fatal("current session did not reach the thermal consumer", err)
	}
	if profile == "standard" {
		if value, err := app.ValidatedCPUStress(payload); err != nil || value == nil {
			t.Fatal("v15 lost Standard CPU evidence", err)
		}
	}
	if profile == "extended" {
		if thermal.Scope != sessionevidence.ExtendedScope {
			t.Fatal("v16 lost Extended thermal scope")
		}
		if value, err := app.ValidatedMemoryStress(payload); err != nil || value == nil {
			t.Fatal("v16 lost Extended memory evidence", err)
		}
	}
	return artifact
}

func inspectionSignedRow(t *testing.T, payload *app.InventoryPayload) *app.TestResult {
	t.Helper()
	for i := range payload.TestResults {
		if payload.TestResults[i].ID == "inspection.exterior.quick" {
			return &payload.TestResults[i]
		}
	}
	t.Fatal("inspection row prerequisite missing")
	return nil
}

func inspectionPDFVisibleText(t *testing.T, raw []byte) string {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		starts, finishes := strings.HasPrefix(line, "("), strings.HasSuffix(line, ") Tj")
		if starts != finishes {
			t.Fatal("canonical PDF text framing")
		}
		if starts {
			lines = append(lines, line[1:len(line)-4])
		}
	}
	// The independent scope and short criterion names below contain no
	// escaped PDF characters. Only line-wrapping whitespace is normalized.
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}

func TestExteriorConditionAllProfilesReachSignedViewsAndOwnedAccessor(t *testing.T) {
	for _, profile := range []string{"quick", "standard", "extended"} {
		t.Run(profile, func(t *testing.T) {
			artifact := inspectionSignedProfile(t, profile)
			payload := stressSignedPayload(t, artifact)
			value, err := app.ValidatedExteriorCondition(payload)
			if err != nil || value == nil || value.ResponsesCompleted != 4 || value.Status != "pass" ||
				value.Findings != (exteriorinspection.ConditionFindingsV2{
					EnclosureIntegrity: "no_issue_observed", HingeMountCondition: "not_applicable",
					PortExteriorCondition: "no_issue_observed", SurfaceFinishCondition: "no_issue_observed",
				}) {
				t.Fatal("four literal signed criteria changed", err)
			}
			value.Findings.EnclosureIntegrity = "issue_present"
			again, err := app.ValidatedExteriorCondition(payload)
			if err != nil || again == nil || again.Findings.EnclosureIntegrity != "no_issue_observed" {
				t.Fatal("accessor exposed report storage", err)
			}
			if legacy, err := app.ValidatedExteriorInspection(payload); err != nil || legacy != nil {
				t.Fatal("current evidence acquired legacy observation authority", err)
			}
			const scope = "Operator-reported visual exterior condition only; not an automated hardware pass or a port-function check."
			pdfText := inspectionPDFVisibleText(t, artifact.RenderedPDF)
			if !bytes.Contains(artifact.RenderedReport, []byte(scope)) || !strings.Contains(pdfText, scope) {
				t.Fatal("signed view lost the complete manual scope")
			}
			for _, label := range []string{"Enclosure integrity", "Visible hinge and mount condition", "Visible port exterior condition", "Visible surface finish condition"} {
				if !bytes.Contains(artifact.RenderedReport, []byte(label)) || !strings.Contains(pdfText, label) {
					t.Fatal("signed view omitted a criterion", label)
				}
			}
			for _, views := range []bool{false, true} {
				var html, pdf []byte
				if views {
					html, pdf = artifact.RenderedReport, artifact.RenderedPDF
				}
				raw := extendedMemorySignedArchive(t, artifact, artifact.InventoryJSON, html, pdf)
				if _, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw))); err != nil {
					t.Fatal("independent signature/member-digest positive prerequisite", err)
				}
			}
		})
	}
}

func TestExteriorConditionSignedConsumersRejectIndependentForgeries(t *testing.T) {
	artifact := inspectionSignedProfile(t, "quick")
	positive := extendedMemorySignedArchive(t, artifact, artifact.InventoryJSON, nil, nil)
	if _, err := bundle.VerifySelfIssued(bytes.NewReader(positive), int64(len(positive))); err != nil {
		t.Fatal("no-view positive", err)
	}
	for _, mode := range []string{
		"wrong_id", "case_id", "version", "planned_status", "outcome", "coverage", "plan", "graph",
		"missing_criterion", "unknown_criterion", "na_outside_hinge", "unobserved_promoted",
		"duplicate_criterion", "escaped_duplicate", "schema_case", "future_schema",
		"old_session", "missing_session", "worker_outcome", "observation_interval",
	} {
		t.Run(mode, func(t *testing.T) {
			payload := stressSignedPayload(t, artifact)
			row := inspectionSignedRow(t, &payload)
			switch mode {
			case "wrong_id":
				row.ID = "inspection.exterior.quick.extra"
			case "case_id":
				row.ID = "INSPECTION.EXTERIOR.QUICK"
			case "version":
				row.Version = 1
			case "planned_status":
				row.PlannedStatus = app.PlannedExcluded
			case "outcome":
				row.Status = app.TestFail
			case "coverage":
				payload.CoverageScope = ""
			case "plan":
				row.PlanID = "sha256:" + strings.Repeat("1", 64)
			case "graph":
				row.HardwareGraph = "hmac-sha256:" + strings.Repeat("2", 64)
			case "missing_criterion", "unknown_criterion":
				var outer, findings map[string]json.RawMessage
				if json.Unmarshal(row.Evidence, &outer) != nil || json.Unmarshal(outer["findings"], &findings) != nil {
					t.Fatal("criterion fixture")
				}
				if mode == "missing_criterion" {
					delete(findings, "surface_finish_condition")
				} else {
					findings["unknown_condition"] = json.RawMessage(`"unobserved"`)
				}
				var err error
				outer["findings"], err = json.Marshal(findings)
				if err != nil {
					t.Fatal(err)
				}
				row.Evidence, err = json.Marshal(outer)
				if err != nil {
					t.Fatal(err)
				}
			case "na_outside_hinge":
				row.Evidence = bytes.Replace(row.Evidence, []byte(`"enclosure_integrity":"no_issue_observed"`), []byte(`"enclosure_integrity":"not_applicable"`), 1)
			case "unobserved_promoted":
				row.Evidence = bytes.Replace(row.Evidence, []byte(`"surface_finish_condition":"no_issue_observed"`), []byte(`"surface_finish_condition":"unobserved"`), 1)
			case "duplicate_criterion", "escaped_duplicate":
				member := `"enclosure_integrity":"unobserved",`
				if mode == "escaped_duplicate" {
					member = `"enclosure_\u0069ntegrity":"unobserved",`
				}
				row.Evidence = bytes.Replace(row.Evidence, []byte(`"enclosure_integrity":`), []byte(member+`"enclosure_integrity":`), 1)
			case "schema_case":
				row.Evidence = bytes.Replace(row.Evidence, []byte("hhsoft.pc-tester.exterior-inspection/v2"), []byte("HHSOFT.pc-tester.exterior-inspection/v2"), 1)
			case "future_schema":
				row.Evidence = bytes.Replace(row.Evidence, []byte("exterior-inspection/v2"), []byte("exterior-inspection/v3"), 1)
			case "old_session", "missing_session", "worker_outcome":
				found := false
				for i := range payload.TestResults {
					parent := &payload.TestResults[i]
					if parent.ID != "run.integrity.quick" {
						continue
					}
					found = true
					if mode == "old_session" {
						parent.Evidence = bytes.Replace(parent.Evidence, []byte("quick-session/v14"), []byte("quick-session/v11"), 1)
					} else if mode == "missing_session" {
						var outer map[string]json.RawMessage
						if json.Unmarshal(parent.Evidence, &outer) != nil {
							t.Fatal("integrity fixture")
						}
						if _, ok := outer["session"]; !ok {
							t.Fatal("session member prerequisite")
						}
						delete(outer, "session")
						var err error
						parent.Evidence, err = json.Marshal(outer)
						if err != nil {
							t.Fatal(err)
						}
					} else {
						value, err := sessionevidence.DecodeIntegrity(parent.Evidence)
						if err != nil || value.Session == nil {
							t.Fatal("worker fixture", err)
						}
						value.Session.Workers[len(value.Session.Workers)-1].Outcome = "fail"
						parent.Evidence, err = json.Marshal(value)
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				if !found {
					t.Fatal("integrity mutation missed")
				}
			case "observation_interval":
				var outer map[string]json.RawMessage
				if json.Unmarshal(row.Evidence, &outer) != nil {
					t.Fatal("interval fixture")
				}
				outer["observation_milliseconds"] = json.RawMessage("44999")
				var err error
				row.Evidence, err = json.Marshal(outer)
				if err != nil {
					t.Fatal(err)
				}
			}
			raw, err := json.Marshal(payload)
			if err != nil || bytes.Equal(raw, artifact.InventoryJSON) {
				t.Fatal("forgery ineffective", err)
			}
			if app.ValidateCoverage(payload) == nil {
				t.Fatal("app admitted forged condition authority")
			}
			if value, err := app.ValidatedExteriorCondition(payload); err == nil || value != nil {
				t.Fatal("accessor released forged observations")
			}
			if _, err := render.InventoryHTML(payload); err == nil {
				t.Fatal("HTML rendered forged observations")
			}
			if _, err := render.InventoryPDF(payload); err == nil {
				t.Fatal("PDF rendered forged observations")
			}
			raw = extendedMemorySignedArchive(t, artifact, raw, nil, nil)
			if _, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw))); err == nil {
				t.Fatal("valid signature bypassed no-view semantic binding")
			}
		})
	}
}

func TestExteriorConditionNoViewRejectsStrippedReferences(t *testing.T) {
	artifact := inspectionSignedProfile(t, "quick")
	for _, mode := range []string{"stripped", "escaped_id", "case_id"} {
		t.Run(mode, func(t *testing.T) {
			payload := stressSignedPayload(t, artifact)
			row := *inspectionSignedRow(t, &payload)
			row.Evidence, row.PlanID, row.HardwareGraph, row.PlannedStatus = json.RawMessage(`{}`), "", "", ""
			if mode == "case_id" {
				row.ID = "INSPECTION.EXTERIOR.QUICK"
			}
			payload.TestResults, payload.CoverageScope = []app.TestResult{row}, ""
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]json.RawMessage
			if json.Unmarshal(raw, &object) != nil {
				t.Fatal("inventory-only fixture")
			}
			delete(object, "schema_version")
			delete(object, "coverage_scope")
			raw, err = json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "escaped_id" {
				before := bytes.Clone(raw)
				raw = bytes.Replace(raw, []byte(`"inspection.exterior.quick"`), []byte(`"\u0069nspection.exterior.quick"`), 1)
				if bytes.Equal(raw, before) || !json.Valid(raw) {
					t.Fatal("escaped reference prerequisite")
				}
			}
			raw = extendedMemorySignedArchive(t, artifact, raw, nil, nil)
			if _, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw))); err == nil {
				t.Fatal("stripped reference bypassed condition authority")
			}
		})
	}
}

func TestExteriorConditionEscapedSchemaPreservesTypedMeaning(t *testing.T) {
	artifact := inspectionSignedProfile(t, "quick")
	payload := stressSignedPayload(t, artifact)
	row := inspectionSignedRow(t, &payload)
	before := bytes.Clone(row.Evidence)
	row.Evidence = bytes.Replace(before, []byte("hhsoft.pc-tester.exterior-inspection/v2"), []byte(`\u0068hsoft.pc-tester.exterior-inspection/v2`), 1)
	first, err1 := exteriorinspection.DecodeV2(before)
	second, err2 := exteriorinspection.DecodeV2(row.Evidence)
	if bytes.Equal(before, row.Evidence) || err1 != nil || err2 != nil || first != second || app.ValidateCoverage(payload) != nil {
		t.Fatal("effective escaped-schema equality prerequisite")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw = extendedMemorySignedArchive(t, artifact, raw, nil, nil)
	if _, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw))); err != nil {
		t.Fatal("escaped valid evidence rejected", err)
	}
}

func TestExteriorConditionCanonicalViewsRejectResignedChanges(t *testing.T) {
	artifact := inspectionSignedProfile(t, "quick")
	for _, mode := range []string{"html", "pdf"} {
		t.Run(mode, func(t *testing.T) {
			html, pdf := bytes.Clone(artifact.RenderedReport), bytes.Clone(artifact.RenderedPDF)
			if mode == "html" {
				html = append(html, []byte("\n<!-- changed condition claim -->")...)
			} else {
				pdf = append(pdf, []byte("\n% changed condition claim")...)
			}
			raw := extendedMemorySignedArchive(t, artifact, artifact.InventoryJSON, html, pdf)
			if _, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw))); err == nil {
				t.Fatal("noncanonical resigned condition view admitted")
			}
		})
	}
}

func TestExteriorConditionHelpKeepsBothExplicitCommands(t *testing.T) {
	var out bytes.Buffer
	usage(&out)
	for _, text := range []string{
		"hhpt inspect-exterior [-timeout 45s] [-output FILE]",
		"hhpt inspect-exterior-condition [-timeout 45s] [-output FILE]",
		"four D/N/I answers; A only for a device without a hinge",
	} {
		if strings.Count(out.String(), text) != 1 {
			t.Fatal("current/legacy help route is missing or duplicated", text)
		}
	}
}
