package main

import (
	"context"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
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
	{id: "cpu.quick", filename: "cpu.json", schema: quick.CPUSchema},
	{id: "display.operator.quick", filename: "display.json", schema: "hhsoft.pc-tester.display-visual/v1"},
	{id: "graphics.quick", filename: "graphics.json", schema: "hhsoft.pc-tester.graphics/v1"},
	{id: "input.pointer.quick", filename: "pointer.json", schema: "hhsoft.pc-tester.pointer-functional/v1"},
	{id: "input.readiness.quick", filename: "input.json", schema: "hhsoft.pc-tester.input-readiness/v1"},
	{id: "memory.quick", filename: "memory.json", schema: quick.MemorySchema},
	{id: "network.readiness.quick", filename: "network.json", schema: "hhsoft.pc-tester.network-readiness/v1"},
	{id: "storage.readiness.quick", filename: "storage.json", schema: "hhsoft.pc-tester.storage-readiness/v1"},
	{id: "usb.readiness.quick", filename: "usb.json", schema: "hhsoft.pc-tester.usb-readiness/v1"},
}

func runFinalize(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	collector := inventory.NewLinuxCollector("/")
	return runFinalizeWith(ctx, arguments, stdout, stderr, collector.Collect, cryptorand.Reader, time.Now)
}

func runFinalizeWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, random io.Reader, now func() time.Time) int {
	return runFinalizeWithSafety(ctx, arguments, stdout, stderr, collect, random, now, safemode.Observe)
}

func runFinalizeWithSafety(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, random io.Reader, now func() time.Time, observe func(context.Context) safemode.Result) int {
	flags := flag.NewFlagSet("finalize", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fingerprintKeyPath := flags.String("fingerprint-key", "", "file containing exactly 32 raw fingerprint-key bytes")
	keyEpoch := flags.String("key-epoch", "ephemeral-session", "non-secret fingerprint-key epoch")
	evidenceDirectory := flags.String("evidence-dir", "/run/hhpt", "direct directory containing bounded test evidence")
	planPath := flags.String("plan", "", "frozen test plan that binds all reported results")
	sessionID := flags.String("session-id", "", "session identifier; random when omitted")
	outputPath := flags.String("output", "", "new self-issued .hhpcr output file")
	fixtureComponents := flags.String("fixture-components", "", "internal file binding selected test-fixture component IDs")
	journalPath := flags.String("session-journal", "", "bounded session execution and safety journal")
	runStop := flags.String("run-stop", "", "session stop reason bound to the journal")
	var outcomes repeatedString
	flags.Var(&outcomes, "outcome", "required TEST_ID=SOURCE_STATUS execution outcome; repeat for every implemented quick test")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *fingerprintKeyPath == "" || *planPath == "" || *outputPath == "" {
		fmt.Fprintln(stderr, "finalize requires -fingerprint-key, -plan, -output, and no positional arguments")
		return 2
	}
	journalFlag, stopFlag := false, false
	flags.Visit(func(value *flag.Flag) {
		journalFlag = journalFlag || value.Name == "session-journal"
		stopFlag = stopFlag || value.Name == "run-stop"
	})
	if journalFlag != stopFlag || journalFlag && (*journalPath == "" || *runStop == "") {
		fmt.Fprintln(stderr, "finalize requires nonempty -session-journal and -run-stop together")
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
	var journal *sessionJournal
	if journalFlag {
		journal, err = readSessionJournal(*journalPath)
		if err == nil {
			err = validateSessionJournal(*journal, frozenPlan, outcomeValues, *runStop, *evidenceDirectory)
		}
		if err != nil {
			fmt.Fprintf(stderr, "validate session journal: %v\n", err)
			return 1
		}
	}
	plannedTests := make(map[string]plan.Test, len(frozenPlan.Tests))
	for _, test := range frozenPlan.Tests {
		plannedTests[test.ID] = test
	}
	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		resolvedSessionID, err = randomSessionID(random)
		if err != nil {
			fmt.Fprintf(stderr, "generate session ID: %v\n", err)
			return 1
		}
	}

	testResults := make([]app.TestResult, 0, len(frozenPlan.Tests)+1)
	covered := make(map[string]bool, len(quickResultDefinitions))
	for _, definition := range quickResultDefinitions {
		plannedTest := plannedTests[definition.id]
		covered[definition.id] = true
		sourceStatus := outcomeValues[definition.id]
		normalizedStatus, ok := normalizeOutcome(sourceStatus)
		if !ok {
			fmt.Fprintf(stderr, "validate outcome %s: unsupported status %q\n", definition.id, sourceStatus)
			return 2
		}
		path := filepath.Join(*evidenceDirectory, definition.filename)
		encoded, readErr := readOptionalEvidence(path)
		if readErr != nil {
			// Only readOptionalEvidence's direct absence sentinel permits a
			// missing-file note. Existing unreadable or oversized quick evidence
			// cannot establish that the thermal observer was joined.
			if (definition.schema == quick.CPUSchema || definition.schema == quick.MemorySchema) && readErr != os.ErrNotExist {
				fmt.Fprintf(stderr, "validate evidence %s: %v\n", definition.id, errInvalidQuickSafety)
				return 1
			}
			if err := validateSessionWorkerEvidence(journal, definition.id, encoded, readErr, "", nil); err != nil {
				fmt.Fprintf(stderr, "validate session evidence %s: %v\n", definition.id, err)
				return 1
			}
		}
		plannedStatus := plannedTest.Status
		var testResult app.TestResult
		if readErr == nil {
			if plannedStatus != plan.TestScheduled {
				fmt.Fprintf(stderr, "validate evidence %s: evidence exists for a non-scheduled test\n", definition.id)
				return 1
			}
			evidenceSourceStatus, statusErr := testEvidenceStatus(encoded, definition.schema)
			if errors.Is(statusErr, errQuickCleanupUnconfirmed) || errors.Is(statusErr, errInvalidQuickSafety) {
				fmt.Fprintf(stderr, "validate evidence %s: %v\n", definition.id, statusErr)
				return 1
			}
			if err := validateSessionWorkerEvidence(journal, definition.id, encoded, nil, evidenceSourceStatus, statusErr); err != nil {
				fmt.Fprintf(stderr, "validate session evidence %s: %v\n", definition.id, err)
				return 1
			}
			evidenceStatus, normalized := normalizeOutcome(evidenceSourceStatus)
			if errors.Is(statusErr, errDisplayFailureDowngrade) ||
				(definition.schema == graphics.VisualSchema && statusErr == nil && evidenceStatus == app.TestFail && normalizedStatus != app.TestFail) {
				fmt.Fprintf(stderr, "validate evidence %s: explicit display failure cannot be replaced by another outcome\n", definition.id)
				return 1
			}
			if errors.Is(statusErr, errMemoryFailureDowngrade) || errors.Is(statusErr, errGraphicsFailureDowngrade) ||
				(statusErr == nil && evidenceStatus == app.TestFail && normalizedStatus != app.TestFail) {
				fmt.Fprintf(stderr, "validate evidence %s: explicit worker failure cannot be replaced by another outcome\n", definition.id)
				return 1
			}
			if statusErr != nil || !normalized || evidenceStatus != normalizedStatus {
				if normalizedStatus != app.TestError && normalizedStatus != app.TestAborted {
					fmt.Fprintf(stderr, "validate evidence %s: schema or status does not match supervisor outcome\n", definition.id)
					return 1
				}
			} else {
				testResult, err = app.NewEncodedTestResult(definition.id, plannedTest.Version, normalizedStatus, encoded)
			}
		}
		if testResult.ID == "" && plannedStatus == plan.TestScheduled && normalizedStatus != app.TestError && normalizedStatus != app.TestAborted {
			fmt.Fprintf(stderr, "validate evidence %s: completed outcome requires bounded evidence\n", definition.id)
			return 1
		}
		if testResult.ID == "" {
			testResult, err = newExecutionNote(plannedTest, normalizedStatus, sourceStatus)
		}
		if err != nil {
			fmt.Fprintf(stderr, "build result %s: %v\n", definition.id, err)
			return 1
		}
		testResults = append(testResults, testResult)
	}
	for _, test := range frozenPlan.Tests {
		if covered[test.ID] {
			continue
		}
		status, ok := normalizeOutcome(string(test.Status))
		if !ok || test.Status == plan.TestScheduled {
			fmt.Fprintf(stderr, "validate frozen plan: test %s has no execution adapter\n", test.ID)
			return 1
		}
		result, err := newExecutionNote(test, status, string(test.Status))
		if err != nil {
			fmt.Fprintf(stderr, "build planned disposition %s: %v\n", test.ID, err)
			return 1
		}
		testResults = append(testResults, result)
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
	finalPlan, err := plan.BuildWithSelection(snapshot, frozenPlan.Profile, frozenPlan.Architecture, fingerprintKey, *keyEpoch, frozenPlan.ExcludedTests)
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
	// The rebuilt plan can establish attribute-dependent applicability that
	// the opaque hardware HMACs alone cannot. Preserve hot-plug evidence, but
	// require exact agreement when the stable hardware graph is unchanged.
	if !changed && finalPlan.ID != frozenPlan.ID {
		fmt.Fprintln(stderr, "validate frozen plan: plan does not match unchanged hardware")
		return 1
	}
	for _, result := range testResults {
		if result.ID == "input.pointer.quick" && (result.Status == app.TestPass || pointerEvidenceHasPassedDevice(result.Evidence)) &&
			!validatePointerPlanEvidence(result.Evidence, plannedTests[result.ID], frozenPlan, finalPlan, snapshot) {
			fmt.Fprintln(stderr, "validate pointer evidence: interfaces or capabilities do not match the frozen hardware")
			return 1
		}
	}
	for index, result := range testResults {
		testResults[index], err = app.BindTestResult(result, frozenPlan.ID, frozenPlan.HardwareGraph.Value)
		if err != nil {
			fmt.Fprintf(stderr, "bind result %s: %v\n", result.ID, err)
			return 1
		}
		testResults[index].PlannedStatus = app.PlannedStatus(plannedTests[result.ID].Status)
	}
	integrityStatus := app.TestPass
	if changed {
		integrityStatus = app.TestFail
	}
	if journal != nil {
		if ctx.Err() != nil || observe == nil {
			fmt.Fprintln(stderr, "session finalization has no live safety observation context")
			return 1
		}
		currentSafety := observe(ctx)
		if ctx.Err() != nil || !safeSessionObservation(currentSafety) {
			fmt.Fprintln(stderr, "session finalization safety is unsafe or incomplete; report not signed")
			return 1
		}
		journal.Safety = append(journal.Safety, sessionSafetyRecord{Phase: "pre-sign", Result: currentSafety})
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
		ExcludedTests         []string                    `json:"excluded_tests,omitempty"`
		Session               *sessionJournal             `json:"session,omitempty"`
	}{"hhsoft.pc-tester.run-integrity/v1", integrityStatus, frozenPlan.ID, frozenPlan.HardwareGraph.Value, finalPlan.HardwareGraph.Value, len(frozenPlan.HardwareGraph.Components), len(finalPlan.HardwareGraph.Components), delta.Added, delta.Removed, delta.Changed, frozenPlan.ExcludedTests, journal})
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
	if journal != nil && ctx.Err() != nil {
		fmt.Fprintln(stderr, "session finalization cancelled before signing")
		return 1
	}
	encoded, err := captureSelfIssuedBundle(snapshot, testResults, fingerprintKey, *keyEpoch, privateKey, publicKey, resolvedSessionID, now(), app.CoverageFrozenPlan)
	if err != nil {
		fmt.Fprintf(stderr, "build self-issued report: %v\n", err)
		return 1
	}
	if journal != nil && ctx.Err() != nil {
		fmt.Fprintln(stderr, "session finalization cancelled before report output")
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
	implemented := make(map[string]bool, len(quickResultDefinitions))
	for _, definition := range quickResultDefinitions {
		implemented[definition.id] = true
	}
	planned := make(map[string]plan.TestStatus, len(frozenPlan.Tests))
	for _, test := range frozenPlan.Tests {
		if _, exists := planned[test.ID]; exists {
			return fmt.Errorf("duplicate planned test %s", test.ID)
		}
		if test.Status == plan.TestScheduled && !implemented[test.ID] {
			return fmt.Errorf("scheduled test %s has no execution adapter", test.ID)
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

func newExecutionNote(test plan.Test, status app.TestStatus, sourceStatus string) (app.TestResult, error) {
	detail := "bounded evidence was not produced; outcome recorded by the run supervisor"
	if test.Status != plan.TestScheduled {
		detail = "not run; disposition recorded in the frozen plan"
	}
	return app.NewTestResult(test.ID, test.Version, status, struct {
		Schema       string         `json:"schema"`
		Status       app.TestStatus `json:"status"`
		SourceStatus string         `json:"source_status"`
		Detail       string         `json:"detail"`
	}{"hhsoft.pc-tester.execution-note/v1", status, sourceStatus, detail})
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
