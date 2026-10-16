package main

import (
	"context"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

type repeatedString []string

func (values *repeatedString) String() string { return strings.Join(*values, ",") }
func (values *repeatedString) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type quickResultDefinition struct {
	id       string
	filename string
	schema   string
}

var quickResultDefinitions = []quickResultDefinition{
	{id: "audio.readiness.quick", filename: "audio.json", schema: "hhsoft.pc-tester.audio-readiness/v1"},
	{id: "cpu.quick", filename: "cpu.json", schema: "hhsoft.pc-tester.cpu/v1"},
	{id: "display.operator.quick", filename: "display.json", schema: "hhsoft.pc-tester.display-visual/v1"},
	{id: "graphics.quick", filename: "graphics.json", schema: "hhsoft.pc-tester.graphics/v1"},
	{id: "input.readiness.quick", filename: "input.json", schema: "hhsoft.pc-tester.input-readiness/v1"},
	{id: "memory.quick", filename: "memory.json", schema: "hhsoft.pc-tester.memory/v1"},
	{id: "network.readiness.quick", filename: "network.json", schema: "hhsoft.pc-tester.network-readiness/v1"},
	{id: "storage.readiness.quick", filename: "storage.json", schema: "hhsoft.pc-tester.storage-readiness/v1"},
	{id: "usb.readiness.quick", filename: "usb.json", schema: "hhsoft.pc-tester.usb-readiness/v1"},
}

func runFinalize(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	collector := inventory.NewLinuxCollector("/")
	return runFinalizeWith(ctx, arguments, stdout, stderr, collector.Collect, cryptorand.Reader, time.Now)
}

func runFinalizeWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, random io.Reader, now func() time.Time) int {
	flags := flag.NewFlagSet("finalize", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fingerprintKeyPath := flags.String("fingerprint-key", "", "file containing exactly 32 raw fingerprint-key bytes")
	keyEpoch := flags.String("key-epoch", "ephemeral-session", "non-secret fingerprint-key epoch")
	evidenceDirectory := flags.String("evidence-dir", "/run/hhpt", "direct directory containing bounded test evidence")
	planPath := flags.String("plan", "", "frozen test plan that binds all reported results")
	sessionID := flags.String("session-id", "", "session identifier; random when omitted")
	outputPath := flags.String("output", "", "new self-issued .hhpcr output file")
	fixtureComponents := flags.String("fixture-components", "", "internal file binding selected test-fixture component IDs")
	var outcomes repeatedString
	flags.Var(&outcomes, "outcome", "required TEST_ID=SOURCE_STATUS execution outcome; repeat for every implemented quick test")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *fingerprintKeyPath == "" || *planPath == "" || *outputPath == "" {
		fmt.Fprintln(stderr, "finalize requires -fingerprint-key, -plan, -output, and no positional arguments")
		return 2
	}
	outcomeValues, err := parseOutcomes(outcomes)
	if err != nil {
		fmt.Fprintf(stderr, "validate outcomes: %v\n", err)
		return 2
	}
	if err := validateEvidenceDirectory(*evidenceDirectory); err != nil {
		fmt.Fprintf(stderr, "validate evidence directory: %v\n", err)
		return 1
	}
	fingerprintKey, err := readRawKey(*fingerprintKeyPath, 32)
	if err != nil {
		fmt.Fprintf(stderr, "read fingerprint key: %v\n", err)
		return 1
	}
	encodedPlan, err := readSmallRegularFile(*planPath, 1<<20)
	if err != nil {
		fmt.Fprintf(stderr, "read frozen plan: %v\n", err)
		return 1
	}
	frozenPlan, err := plan.Decode(encodedPlan)
	if err != nil {
		fmt.Fprintf(stderr, "validate frozen plan: %v\n", err)
		return 1
	}
	if frozenPlan.HardwareGraph.KeyEpoch != strings.TrimSpace(*keyEpoch) {
		fmt.Fprintln(stderr, "validate frozen plan: fingerprint-key epoch changed")
		return 1
	}
	if err := validatePlanOutcomes(frozenPlan, outcomeValues); err != nil {
		fmt.Fprintf(stderr, "validate frozen plan outcomes: %v\n", err)
		return 1
	}
	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		resolvedSessionID, err = randomSessionID(random)
		if err != nil {
			fmt.Fprintf(stderr, "generate session ID: %v\n", err)
			return 1
		}
	}

	testResults := make([]app.TestResult, 0, len(quickResultDefinitions))
	for _, definition := range quickResultDefinitions {
		sourceStatus := outcomeValues[definition.id]
		normalizedStatus, ok := normalizeOutcome(sourceStatus)
		if !ok {
			fmt.Fprintf(stderr, "validate outcome %s: unsupported status %q\n", definition.id, sourceStatus)
			return 2
		}
		path := filepath.Join(*evidenceDirectory, definition.filename)
		encoded, readErr := readOptionalEvidence(path)
		plannedStatus := frozenTestStatus(frozenPlan, definition.id)
		var testResult app.TestResult
		if readErr == nil {
			if plannedStatus != plan.TestScheduled {
				fmt.Fprintf(stderr, "validate evidence %s: evidence exists for a non-scheduled test\n", definition.id)
				return 1
			}
			evidenceSourceStatus, statusErr := testEvidenceStatus(encoded, definition.schema)
			evidenceStatus, normalized := normalizeOutcome(evidenceSourceStatus)
			if statusErr != nil || !normalized || evidenceStatus != normalizedStatus {
				if normalizedStatus != app.TestError && normalizedStatus != app.TestAborted {
					fmt.Fprintf(stderr, "validate evidence %s: schema or status does not match supervisor outcome\n", definition.id)
					return 1
				}
			} else {
				testResult, err = app.NewEncodedTestResult(definition.id, 1, normalizedStatus, encoded)
			}
		}
		if testResult.ID == "" && plannedStatus == plan.TestScheduled && normalizedStatus != app.TestError && normalizedStatus != app.TestAborted {
			fmt.Fprintf(stderr, "validate evidence %s: completed outcome requires bounded evidence\n", definition.id)
			return 1
		}
		if testResult.ID == "" {
			testResult, err = app.NewTestResult(definition.id, 1, normalizedStatus, struct {
				Schema       string         `json:"schema"`
				Status       app.TestStatus `json:"status"`
				SourceStatus string         `json:"source_status"`
				Detail       string         `json:"detail"`
			}{"hhsoft.pc-tester.execution-note/v1", normalizedStatus, sourceStatus, "bounded evidence was not produced; outcome recorded by the run supervisor"})
		}
		if err != nil {
			fmt.Fprintf(stderr, "build result %s: %v\n", definition.id, err)
			return 1
		}
		testResults = append(testResults, testResult)
	}

	snapshot, err := collect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "collect final inventory: %v\n", err)
		return 1
	}
	if *fixtureComponents != "" {
		snapshot, err = excludeFixtureComponentsFromFile(snapshot, *fixtureComponents)
		if err != nil {
			fmt.Fprintf(stderr, "exclude deployment-key fixture: %v\n", err)
			return 1
		}
	}
	finalPlan, err := plan.Build(snapshot, frozenPlan.Profile, frozenPlan.Architecture, fingerprintKey, *keyEpoch)
	if err != nil || finalPlan.DefinitionsDigest != frozenPlan.DefinitionsDigest {
		fmt.Fprintln(stderr, "validate frozen plan: test definitions changed")
		return 1
	}
	delta := plan.CompareHardwareGraphs(frozenPlan.HardwareGraph, finalPlan.HardwareGraph)
	changed := len(delta.Added) != 0 || len(delta.Removed) != 0 || len(delta.Changed) != 0
	if changed == (frozenPlan.HardwareGraph.Value == finalPlan.HardwareGraph.Value) {
		fmt.Fprintln(stderr, "validate frozen plan: inconsistent hardware graph")
		return 1
	}
	for index, result := range testResults {
		testResults[index], err = app.BindTestResult(result, frozenPlan.ID, frozenPlan.HardwareGraph.Value)
		if err != nil {
			fmt.Fprintf(stderr, "bind result %s: %v\n", result.ID, err)
			return 1
		}
	}
	integrityStatus := app.TestPass
	if changed {
		integrityStatus = app.TestFail
	}
	integrityResult, err := app.NewTestResult("run.integrity.quick", 1, integrityStatus, struct {
		Schema                string                      `json:"schema"`
		Status                app.TestStatus              `json:"status"`
		PlanID                string                      `json:"plan_id"`
		PlannedHardwareGraph  string                      `json:"planned_hardware_graph"`
		FinalHardwareGraph    string                      `json:"final_hardware_graph"`
		PlannedComponentCount int                         `json:"planned_component_count"`
		FinalComponentCount   int                         `json:"final_component_count"`
		AddedComponents       []plan.HardwareComponentRef `json:"added_components,omitempty"`
		RemovedComponents     []plan.HardwareComponentRef `json:"removed_components,omitempty"`
		ChangedComponents     []plan.HardwareComponentRef `json:"changed_components,omitempty"`
	}{"hhsoft.pc-tester.run-integrity/v1", integrityStatus, frozenPlan.ID, frozenPlan.HardwareGraph.Value, finalPlan.HardwareGraph.Value, len(frozenPlan.HardwareGraph.Components), len(finalPlan.HardwareGraph.Components), delta.Added, delta.Removed, delta.Changed})
	if err == nil {
		integrityResult, err = app.BindTestResult(integrityResult, frozenPlan.ID, frozenPlan.HardwareGraph.Value)
	}
	if err != nil {
		fmt.Fprintf(stderr, "build run integrity result: %v\n", err)
		return 1
	}
	testResults = append(testResults, integrityResult)
	fmt.Fprintf(stdout, "HHPT_RUN_INTEGRITY status=%s plan=%s planned_components=%d final_components=%d added=%d removed=%d changed=%d\n", integrityStatus, frozenPlan.ID, len(frozenPlan.HardwareGraph.Components), len(finalPlan.HardwareGraph.Components), len(delta.Added), len(delta.Removed), len(delta.Changed))
	publicKey, privateKey, err := ed25519.GenerateKey(random)
	if err != nil {
		fmt.Fprintf(stderr, "generate self-issued report key: %v\n", err)
		return 1
	}
	encoded, err := captureSelfIssuedBundle(snapshot, testResults, fingerprintKey, *keyEpoch, privateKey, publicKey, resolvedSessionID, now())
	if err != nil {
		fmt.Fprintf(stderr, "build self-issued report: %v\n", err)
		return 1
	}
	destination, closeDestination, err := openDestination(*outputPath, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "open output: %v\n", err)
		return 1
	}
	defer closeDestination()
	if err := writeAll(destination, encoded); err != nil {
		fmt.Fprintf(stderr, "write report: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "finalized: signed self-issued report (reduced identity assurance)")
	return 0
}

func validatePlanOutcomes(frozenPlan plan.Plan, outcomes map[string]string) error {
	planned := make(map[string]plan.TestStatus, len(frozenPlan.Tests))
	for _, test := range frozenPlan.Tests {
		if _, exists := planned[test.ID]; exists {
			return fmt.Errorf("duplicate planned test %s", test.ID)
		}
		planned[test.ID] = test.Status
	}
	for _, definition := range quickResultDefinitions {
		plannedStatus, exists := planned[definition.id]
		if !exists {
			return fmt.Errorf("required test %s is absent", definition.id)
		}
		if plannedStatus != plan.TestScheduled && outcomes[definition.id] != string(plannedStatus) {
			return fmt.Errorf("outcome %s does not match non-scheduled plan status %s", definition.id, plannedStatus)
		}
	}
	return nil
}

func frozenTestStatus(frozenPlan plan.Plan, id string) plan.TestStatus {
	for _, test := range frozenPlan.Tests {
		if test.ID == id {
			return test.Status
		}
	}
	return ""
}

func parseOutcomes(values []string) (map[string]string, error) {
	known := make(map[string]bool, len(quickResultDefinitions))
	for _, definition := range quickResultDefinitions {
		known[definition.id] = true
	}
	parsed := make(map[string]string, len(values))
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) != 2 || !known[parts[0]] || strings.TrimSpace(parts[1]) != parts[1] || parts[1] == "" || len(parts[1]) > 32 {
			return nil, fmt.Errorf("invalid -outcome %q", value)
		}
		if _, duplicate := parsed[parts[0]]; duplicate {
			return nil, fmt.Errorf("duplicate outcome %s", parts[0])
		}
		parsed[parts[0]] = parts[1]
	}
	if len(parsed) != len(known) {
		return nil, errors.New("every implemented quick test requires exactly one outcome")
	}
	return parsed, nil
}

func normalizeOutcome(status string) (app.TestStatus, bool) {
	switch app.TestStatus(status) {
	case app.TestPass, app.TestFail, app.TestSkip, app.TestInconclusive, app.TestError, app.TestAborted:
		return app.TestStatus(status), true
	case "not_present", "excluded", "unsupported":
		return app.TestSkip, true
	case "missing_driver":
		return app.TestInconclusive, true
	default:
		return "", false
	}
}

func validateEvidenceDirectory(path string) error {
	metadata, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if metadata.Mode()&os.ModeSymlink != 0 || !metadata.IsDir() {
		return errors.New("evidence path must be a direct directory")
	}
	return nil
}

func readOptionalEvidence(path string) ([]byte, error) {
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	encoded, err := readSmallRegularFile(path, 1<<20)
	return encoded, err
}

func testEvidenceStatus(encoded []byte, expectedSchema string) (string, error) {
	var header struct {
		Schema             string            `json:"schema"`
		Status             string            `json:"status"`
		Readiness          string            `json:"readiness"`
		MediaStreamsOpened *bool             `json:"media_streams_opened"`
		ReadyEndpoints     *int              `json:"ready_endpoints"`
		Cards              []json.RawMessage `json:"cards"`
		Endpoints          []json.RawMessage `json:"endpoints"`
	}
	if err := json.Unmarshal(encoded, &header); err != nil || header.Schema != expectedSchema || header.Status == "" {
		return "", errors.New("evidence schema or status is invalid")
	}
	if expectedSchema == "hhsoft.pc-tester.audio-readiness/v1" {
		if header.MediaStreamsOpened == nil || *header.MediaStreamsOpened || header.ReadyEndpoints == nil {
			return "", errors.New("audio evidence safety fields are invalid")
		}
		switch header.Status {
		case "pass":
			if header.Readiness != "ready" || *header.ReadyEndpoints <= 0 || len(header.Cards) == 0 || len(header.Endpoints) == 0 {
				return "", errors.New("audio pass evidence is incomplete")
			}
		case "inconclusive":
			if header.Readiness != "no_device" && header.Readiness != "missing_nodes" && header.Readiness != "no_pcm" {
				return "", errors.New("audio inconclusive evidence is inconsistent")
			}
		case "error", "aborted":
			if header.Readiness != "unknown" {
				return "", errors.New("audio error evidence is inconsistent")
			}
		default:
			return "", errors.New("audio evidence status is invalid")
		}
	}
	return header.Status, nil
}
