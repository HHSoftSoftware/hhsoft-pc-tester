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
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/keyboardevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/pointerevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
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
	{id: "input.keyboard.quick", filename: "keyboard.json", schema: inputtest.KeyboardSchema},
	{id: "input.pointer.quick", filename: "pointer.json", schema: inputtest.PointerSchema},
	{id: "input.readiness.quick", filename: "input.json", schema: "hhsoft.pc-tester.input-readiness/v1"},
	{id: "inspection.exterior.quick", filename: "exterior-inspection.json", schema: exteriorinspection.Schema},
	{id: "memory.quick", filename: "memory.json", schema: quick.MemorySchema},
	{id: "network.readiness.quick", filename: "network.json", schema: "hhsoft.pc-tester.network-readiness/v1"},
	{id: "storage.readiness.quick", filename: "storage.json", schema: "hhsoft.pc-tester.storage-readiness/v1"},
	{id: "usb.readiness.quick", filename: "usb.json", schema: "hhsoft.pc-tester.usb-readiness/v1"},
}

// Return a fresh definition slice; profile selection cannot mutate Quick.
func executionResultDefinitions(profile plan.Profile) ([]quickResultDefinition, error) {
	if profile != "" && profile != plan.ProfileQuick && profile != plan.ProfileStandard && profile != plan.ProfileExtended {
		return nil, errors.New("unsupported execution profile")
	}
	definitions := make([]quickResultDefinition, 0, len(quickResultDefinitions)+1)
	for _, definition := range quickResultDefinitions {
		definitions = append(definitions, definition)
		if profile == plan.ProfileExtended && definition.id == "memory.quick" {
			definitions = append(definitions, quickResultDefinition{id: "memory.stress", filename: "memory-stress.json", schema: quick.MemoryStressSchema})
		}
		if profile == plan.ProfileStandard && definition.id == "cpu.quick" {
			definitions = append(definitions, quickResultDefinition{id: "cpu.stress", filename: "cpu-stress.json", schema: quick.CPUStressSchema})
		}
	}
	return definitions, nil
}

func runFinalize(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	collector := inventory.NewLinuxCollector("/")
	return runFinalizeWith(ctx, arguments, stdout, stderr, collector.Collect, cryptorand.Reader, time.Now)
}

func runFinalizeWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, random io.Reader, now func() time.Time) int {
	return runFinalizeOwned(ctx, arguments, stdout, stderr, collect, random, now, nil).Code
}

func runFinalizeOwned(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, random io.Reader, now func() time.Time, gate sessionFinalGate) (result sessionFinalizationResult) {
	return runFinalizeOwnedDestination(ctx, arguments, stdout, stderr, collect, random, now, gate, writeDestination)
}

func runFinalizeOwnedDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, random io.Reader, now func() time.Time, gate sessionFinalGate, writeOutput destinationWriter) (result sessionFinalizationResult) {
	return runFinalizeOwnedDestinationWithPrevious(ctx, arguments, stdout, stderr, collect, random, now, gate, writeOutput, nil, readRetestInput)
}

func runFinalizeOwnedDestinationWithPrevious(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, random io.Reader, now func() time.Time, gate sessionFinalGate, writeOutput destinationWriter, previous *verifiedPreviousReport, readInput retestInputReader) (result sessionFinalizationResult) {
	return runFinalizeOwnedDestinationWithPreviousForProfile(ctx, arguments, stdout, stderr, collect, random, now, gate, writeOutput, previous, readInput, "")
}

func runFinalizeOwnedDestinationWithPreviousForProfile(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, random io.Reader, now func() time.Time, gate sessionFinalGate, writeOutput destinationWriter, previous *verifiedPreviousReport, readInput retestInputReader, executionProfile plan.Profile) (result sessionFinalizationResult) {
	resultDefinitions, profileErr := executionResultDefinitions(executionProfile)
	if profileErr != nil || (executionProfile == plan.ProfileStandard || executionProfile == plan.ProfileExtended) && gate == nil {
		fmt.Fprintln(stderr, "finalize: execution profile requires its live coordinator")
		return sessionFinalizationResult{Code: 1}
	}

	if previous != nil {
		copy := *previous
		previous = &copy
	}
	result.Code = func() int {

		flags := flag.NewFlagSet("finalize", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		fingerprintKeyPath := flags.String("fingerprint-key", "", "file containing exactly 32 raw fingerprint-key bytes")
		keyEpoch := flags.String("key-epoch", "ephemeral-session", "non-secret fingerprint-key epoch")
		evidenceDirectory := flags.String("evidence-dir", "/run/hhpt", "direct directory containing bounded test evidence")
		planPath := flags.String("plan", "", "frozen test plan that binds all reported results")
		sessionID := flags.String("session-id", "", "session identifier; random when omitted")
		outputPath := flags.String("output", "", "new self-issued .hhpcr output file")
		fixtureComponents := flags.String("fixture-components", "", "internal file binding selected test-fixture component IDs")
		journalPath := flags.String("session-journal", "", "bounded session execution and safety journal")
		runStop := flags.String("run-stop", "", "session stop reason bound to the journal")
		var parentOptions previousReportFlags
		parentOptions.register(flags)
		var outcomes repeatedString
		flags.Var(&outcomes, "outcome", "required TEST_ID=SOURCE_STATUS execution outcome; repeat for every implemented quick test")
		if err := flags.Parse(arguments); err != nil {
			fmt.Fprintln(stderr, "finalize: invalid arguments")
			return 2
		}
		parentOptions.recordPresence(flags)
		if !parentOptions.valid() || previous != nil && parentOptions.present() {
			fmt.Fprintln(stderr, "finalize: invalid or conflicting predecessor options")
			return 2
		}
		if previous != nil && !previous.valid() {
			fmt.Fprintln(stderr, "finalize: invalid verified predecessor")
			return 1
		}
		if flags.NArg() != 0 || *fingerprintKeyPath == "" || *planPath == "" || *outputPath == "" {
			fmt.Fprintln(stderr, "finalize requires -fingerprint-key, -plan, -output, and no positional arguments")
			return 2
		}
		journalFlag, sessionIDSet := false, false
		flags.Visit(func(value *flag.Flag) {
			journalFlag = journalFlag || value.Name == "session-journal" || value.Name == "run-stop"
			sessionIDSet = sessionIDSet || value.Name == "session-id"
		})
		if journalFlag || *journalPath != "" || *runStop != "" {
			fmt.Fprintln(stderr, "session finalization requires the live coordinator gate; a journal file cannot authorize signing")
			return 2
		}
		if parentOptions.present() {
			if nilRetestInputValue(ctx) || readInput == nil {
				fmt.Fprintln(stderr, "finalize: predecessor input or verification failed")
				return 1
			}
			var parentErr error
			previous, parentErr = parentOptions.load(ctx, readInput)
			if parentErr != nil {
				fmt.Fprintln(stderr, "finalize: predecessor input or verification failed")
				return 1
			}
		}
		if previous != nil && (nilRetestInputValue(ctx) || ctx.Err() != nil) {
			fmt.Fprintln(stderr, "finalize: linked retest cancelled")
			return 1
		}
		outcomeValues, err := parseOutcomesWithDefinitions(outcomes, resultDefinitions)
		if err != nil {
			outcomeValues, err = parseOutcomesWithDefinitions(outcomes, definitionsWithTouchpad(resultDefinitions))
		}
		if err != nil {
			outcomeValues, err = parseOutcomesWithDefinitions(outcomes, definitionsWithTouchscreen(definitionsWithTouchpad(resultDefinitions)))
		}
		if err != nil {
			fmt.Fprintf(stderr, "validate outcomes: %v\n", err)
			return 2
		}
		if err := validateEvidenceDirectory(*evidenceDirectory); err != nil {
			fmt.Fprintf(stderr, "validate evidence directory: %v\n", err)
			return 1
		}
		if nilRetestInputValue(ctx) || readInput == nil {
			fmt.Fprintln(stderr, "finalize input acquisition failed")
			return 1
		}
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "finalize input acquisition failed")
			return 1
		}
		fingerprintKey, err := readInput(ctx, *fingerprintKeyPath, retestFingerprintKeyInput)
		if err != nil || len(fingerprintKey) != 32 {
			fmt.Fprintln(stderr, "read fingerprint key: input acquisition failed")
			return 1
		}
		// The input reader owns its returned backing storage. Freeze before Err.
		fingerprintKey = append([]byte(nil), fingerprintKey...)
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "finalize input acquisition failed")
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
		if executionProfile == "" && (frozenPlan.Profile == plan.ProfileStandard || frozenPlan.Profile == plan.ProfileExtended) ||
			executionProfile != "" && frozenPlan.Profile != executionProfile {
			fmt.Fprintln(stderr, "finalize: plan does not match its execution profile")
			return 1
		}
		if frozenPlan.HardwareGraph.KeyEpoch != strings.TrimSpace(*keyEpoch) {
			fmt.Fprintln(stderr, "validate frozen plan: fingerprint-key epoch changed")
			return 1
		}
		layout, err := commandLayoutForPlan(frozenPlan, executionProfile)
		if err != nil {
			fmt.Fprintln(stderr, "validate frozen plan: invalid execution layout")
			return 1
		}
		if layout.touchpad && ctx.Err() != nil {
			fmt.Fprintln(stderr, "touchpad finalization interrupted during plan admission")
			return 1
		}
		resultDefinitions = layout.definitions
		outcomeValues, err = parseOutcomesWithDefinitions(outcomes, resultDefinitions)
		if err != nil {
			fmt.Fprintln(stderr, "validate outcomes: frozen plan requires its exact result set")
			return 2
		}
		if err := validatePlanOutcomesWithDefinitions(frozenPlan, outcomeValues, resultDefinitions); err != nil {
			fmt.Fprintf(stderr, "validate frozen plan outcomes: %v\n", err)
			return 1
		}
		var journal *sessionJournal
		plannedTests := make(map[string]plan.Test, len(frozenPlan.Tests))
		for _, test := range frozenPlan.Tests {
			plannedTests[test.ID] = test
		}
		resolvedSessionID := strings.TrimSpace(*sessionID)
		if previous != nil && sessionIDSet && (!report.ValidRetestID(*sessionID) || *sessionID == previous.sessionID) {
			fmt.Fprintln(stderr, "finalize: linked retest requires a distinct canonical UUIDv4 session ID")
			return 2
		}
		if resolvedSessionID == "" {
			if previous == nil {
				resolvedSessionID, err = randomSessionID(random)
			} else {
				resolvedSessionID, err = randomRetestSessionID(random)
			}
			if err != nil {
				fmt.Fprintf(stderr, "generate session ID: %v\n", err)
				return 1
			}
		}

		if layout.touchpad && ctx.Err() != nil {
			fmt.Fprintln(stderr, "touchpad finalization interrupted during session identification")
			return 1
		}
		var relation *report.PreviousReport
		if previous != nil {
			if *sessionID != strings.TrimSpace(*sessionID) || !report.ValidRetestID(resolvedSessionID) || resolvedSessionID == previous.sessionID {
				fmt.Fprintln(stderr, "finalize: linked retest requires a distinct UUIDv4")
				return 2
			}
			copy := previous.reference
			relation = &copy
		}
		testResults := make([]app.TestResult, 0, len(frozenPlan.Tests)+1)
		covered := make(map[string]bool, len(resultDefinitions))
		retainedEvidence := make(map[string][]byte, len(resultDefinitions))
		for _, definition := range resultDefinitions {
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
			if readErr == nil {
				retainedEvidence[definition.id] = append([]byte(nil), encoded...)
			}
			if definition.schema == inputtest.TouchpadSchema {
				touchpadResult, touchpadErr := newTouchpadFinalResult(plannedTest, sourceStatus, encoded, readErr)
				if touchpadCleanupError(touchpadErr) {
					fmt.Fprintln(stderr, "validate touchpad evidence: cleanup unconfirmed")
					return workerCleanupUncertainExitCode
				}
				if touchpadErr != nil {
					fmt.Fprintln(stderr, "validate touchpad evidence: invalid observations or disposition")
					return 1
				}
				testResults = append(testResults, touchpadResult)
				continue
			}
			if definition.schema == inputtest.TouchscreenSchema {
				touchscreenResult, touchscreenErr := newTouchscreenFinalResult(plannedTest, sourceStatus, encoded, readErr)
				if touchscreenCleanupError(touchscreenErr) {
					fmt.Fprintln(stderr, "validate touchscreen evidence: cleanup unconfirmed")
					return workerCleanupUncertainExitCode
				}
				if touchscreenErr != nil {
					fmt.Fprintln(stderr, "validate touchscreen evidence: invalid observations or disposition")
					return 1
				}
				testResults = append(testResults, touchscreenResult)
				continue
			}
			if readErr != nil {
				if definition.schema == quick.MemoryStressSchema && readErr != os.ErrNotExist {
					fmt.Fprintln(stderr, "validate memory stress evidence: existing evidence is unreadable")
					return 1
				}
				if definition.schema == quick.CPUStressSchema && readErr != os.ErrNotExist {
					fmt.Fprintln(stderr, "validate CPU stress evidence: existing evidence is unreadable")
					return 1
				}
				if (definition.schema == exteriorinspection.Schema || definition.schema == exteriorinspection.SchemaV2) && readErr != os.ErrNotExist {
					fmt.Fprintln(stderr, "validate inspection evidence: existing evidence is unreadable")
					return 1
				}
				if definition.schema == inputtest.PointerSchema && readErr != os.ErrNotExist {
					fmt.Fprintln(stderr, "validate pointer evidence: existing evidence is unreadable")
					return 1
				}
				if definition.schema == inputtest.KeyboardSchema && readErr != os.ErrNotExist {
					fmt.Fprintln(stderr, "validate keyboard evidence: existing evidence cannot establish confirmed cleanup")
					return 1
				}
				// Only readOptionalEvidence's direct absence sentinel permits a
				// missing-file note. Existing unreadable or oversized quick evidence
				// cannot establish that the thermal observer was joined.
				if (definition.schema == quick.CPUSchema || definition.schema == quick.MemorySchema) && readErr != os.ErrNotExist {
					fmt.Fprintf(stderr, "validate evidence %s: %v\n", definition.id, errInvalidQuickSafety)
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
				if definition.schema == quick.MemoryStressSchema {
					if errors.Is(statusErr, quick.ErrMemoryStressCleanupUnconfirmed) {
						fmt.Fprintln(stderr, "validate memory stress evidence: cleanup unconfirmed")
						return workerCleanupUncertainExitCode
					}
					if statusErr != nil || evidenceSourceStatus != sourceStatus || gate == nil || executionProfile != plan.ProfileExtended {
						fmt.Fprintln(stderr, "validate memory stress evidence: invalid observations or session binding")
						return 1
					}
				}
				if definition.schema == quick.CPUStressSchema {
					if errors.Is(statusErr, quick.ErrCPUStressCleanupUnconfirmed) {
						fmt.Fprintln(stderr, "validate CPU stress evidence: cleanup unconfirmed")
						return workerCleanupUncertainExitCode
					}
					if statusErr != nil || evidenceSourceStatus != sourceStatus || gate == nil || executionProfile != plan.ProfileStandard {
						fmt.Fprintln(stderr, "validate CPU stress evidence: invalid observations or session binding")
						return 1
					}
				}
				if errors.Is(statusErr, pointerevidence.ErrInvalid) {
					fmt.Fprintln(stderr, "validate pointer evidence: malformed observations")
					return 1
				}
				if errors.Is(statusErr, keyboardevidence.ErrCleanupUnconfirmed) {
					fmt.Fprintln(stderr, "validate keyboard evidence: cleanup unconfirmed")
					return workerCleanupUncertainExitCode
				}
				if errors.Is(statusErr, keyboardevidence.ErrInvalid) {
					fmt.Fprintln(stderr, "validate keyboard evidence: malformed observations")
					return 1
				}
				if definition.schema == inputtest.KeyboardSchema && gate == nil {
					fmt.Fprintln(stderr, "functional keyboard evidence requires the live current session gate")
					return 1
				}
				if errors.Is(statusErr, errQuickCleanupUnconfirmed) || errors.Is(statusErr, errInvalidQuickSafety) {
					fmt.Fprintf(stderr, "validate evidence %s: %v\n", definition.id, statusErr)
					return 1
				}
				evidenceStatus, normalized := normalizeOutcome(evidenceSourceStatus)
				if (definition.schema == exteriorinspection.Schema || definition.schema == exteriorinspection.SchemaV2) && (statusErr != nil || !normalized || evidenceStatus != normalizedStatus) {
					fmt.Fprintln(stderr, "validate inspection evidence: observations do not match the supervisor outcome")
					return 1
				}
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

		if layout.touchpad && ctx.Err() != nil {
			fmt.Fprintln(stderr, "touchpad finalization interrupted before final inventory")
			return 1
		}
		snapshot, err := collect(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "collect final inventory: %v\n", err)
			return 1
		}
		if layout.touchpad {
			frozenPlan, snapshot, fingerprintKey, err = freezeTouchpadFinalInputs(frozenPlan, snapshot, fingerprintKey)
			if err != nil {
				fmt.Fprintln(stderr, "freeze touchpad final inputs: invalid owned binding")
				return 1
			}
			if ctx.Err() != nil {
				fmt.Fprintln(stderr, "touchpad finalization interrupted during final inventory")
				return 1
			}
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
			if result.ID == touchpadExecutionID {
				if raw, exists := retainedEvidence[result.ID]; exists {
					if err := validateTouchpadPlanEvidence(raw, plannedTests[result.ID], frozenPlan, finalPlan, snapshot); err != nil {
						if touchpadCleanupError(err) {
							return workerCleanupUncertainExitCode
						}
						fmt.Fprintln(stderr, "validate touchpad evidence: frozen and final inventory binding changed")
						return 1
					}
				}
			}
			if result.ID == touchscreenExecutionID {
				if raw, exists := retainedEvidence[result.ID]; exists {
					if err := validateTouchscreenPlanEvidence(raw, plannedTests[result.ID], frozenPlan, finalPlan, snapshot); err != nil {
						if touchscreenCleanupError(err) {
							return workerCleanupUncertainExitCode
						}
						fmt.Fprintln(stderr, "validate touchscreen evidence: frozen and final inventory binding changed")
						return 1
					}
				}
			}
			if result.ID == "input.keyboard.quick" {
				if raw, exists := retainedEvidence[result.ID]; exists {
					value, decodeErr := keyboardevidence.Decode(raw)
					if decodeErr != nil || keyboardevidence.ValidatePlanBinding(value, plannedTests[result.ID], frozenPlan, finalPlan, snapshot) != nil {
						fmt.Fprintln(stderr, "validate keyboard evidence: interfaces or capabilities do not match the frozen hardware")
						return 1
					}
				}
			}
			if result.ID == "input.pointer.quick" {
				if raw, present := retainedEvidence[result.ID]; present &&
					!validatePointerPlanEvidence(raw, plannedTests[result.ID], frozenPlan, finalPlan, snapshot) {
					fmt.Fprintln(stderr, "validate pointer evidence: interfaces or capabilities do not match the frozen hardware")
					return 1
				}
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
		if gate != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(stderr, "session preparation cancelled before live gate")
				return 1
			}
			value, gateErr := gate(ctx)
			value = sessionevidence.CloneJournal(value)
			if gateErr != nil || ctx.Err() != nil {
				fmt.Fprintln(stderr, "session live final gate failed; report not signed")
				return 1
			}
			if !currentSessionProfileForPlan(value, frozenPlan, executionProfile) {
				fmt.Fprintln(stderr, "live finalization requires the current session journal schema")
				return 1
			}
			journal = &value
			if err := validateSessionJournalForProfile(value, frozenPlan, outcomeValues, value.StopReason, *evidenceDirectory, executionProfile); err != nil {
				fmt.Fprintf(stderr, "validate live session journal: %v\n", err)
				return 1
			}
			bindings := make([]sessionevidence.ResultBinding, 0, len(testResults))
			for _, row := range testResults {
				bindings = append(bindings, sessionevidence.ResultBinding{ID: row.ID, Version: row.Version, PlannedStatus: string(row.PlannedStatus), ReportStatus: string(row.Status), Evidence: row.Evidence})
				raw, present := retainedEvidence[row.ID]
				sourceStatus, statusErr := "", error(nil)
				readErr := error(os.ErrNotExist)
				if present {
					readErr = nil
					for _, definition := range resultDefinitions {
						if definition.id == row.ID {
							sourceStatus, statusErr = testEvidenceStatus(raw, definition.schema)
							break
						}
					}
				}
				if err := validateSessionWorkerEvidence(journal, row.ID, raw, readErr, sourceStatus, statusErr); err != nil {
					fmt.Fprintln(stderr, "live session worker evidence does not match")
					return 1
				}
			}
			if sessionevidence.ValidateBindings(value, bindings) != nil {
				fmt.Fprintln(stderr, "live session evidence bindings do not match")
				return 1
			}
			for _, source := range value.LocalThermalSources {
				raw, present := retainedEvidence[source.WorkerID]
				if !present || sourceFileDigest(raw) != source.EvidenceFileSHA256 {
					fmt.Fprintln(stderr, "local thermal evidence file binding changed")
					return 1
				}
			}
			if value.MemoryStressSource != nil {
				raw, present := retainedEvidence["memory.stress"]
				if !present || sourceFileDigest(raw) != value.MemoryStressSource.EvidenceFileSHA256 {
					fmt.Fprintln(stderr, "memory stress evidence file binding changed")
					return 1
				}
			}
			if value.CPUStressSource != nil {
				raw, present := retainedEvidence["cpu.stress"]
				if !present || sourceFileDigest(raw) != value.CPUStressSource.EvidenceFileSHA256 {
					fmt.Fprintln(stderr, "CPU stress evidence file binding changed")
					return 1
				}
			}
			result.Journal = &value
		}
		if layout.touchpad {
			raw, present := retainedEvidence[touchpadExecutionID]
			if err := recheckTouchpadEvidence(filepath.Join(*evidenceDirectory, "touchpad.json"), raw, present, readOptionalEvidence); err != nil {
				fmt.Fprintln(stderr, "validate touchpad evidence: retained file changed")
				return 1
			}
		}
		if layout.touchscreen {
			raw, present := retainedEvidence[touchscreenExecutionID]
			if err := recheckTouchscreenEvidence(filepath.Join(*evidenceDirectory, "touchscreen.json"), raw, present, readOptionalEvidence); err != nil {
				fmt.Fprintln(stderr, "validate touchscreen evidence: retained file changed")
				return 1
			}
		}
		result.Integrity = sessionIntegrity{Status: string(integrityStatus), PlanID: frozenPlan.ID, PlannedComponents: uint64(len(frozenPlan.HardwareGraph.Components)), FinalComponents: uint64(len(finalPlan.HardwareGraph.Components)), Added: uint64(len(delta.Added)), Removed: uint64(len(delta.Removed)), Changed: uint64(len(delta.Changed))}
		integrityEvidence := sessionevidence.IntegrityEvidence{Schema: sessionevidence.IntegritySchema, Status: string(integrityStatus), PlanID: frozenPlan.ID, PlannedHardwareGraph: frozenPlan.HardwareGraph.Value, FinalHardwareGraph: finalPlan.HardwareGraph.Value, PlannedComponentCount: len(frozenPlan.HardwareGraph.Components), FinalComponentCount: len(finalPlan.HardwareGraph.Components), AddedComponents: delta.Added, RemovedComponents: delta.Removed, ChangedComponents: delta.Changed, ExcludedTests: frozenPlan.ExcludedTests, Session: journal}
		integrityResult, err := app.NewTestResult("run.integrity.quick", 1, integrityStatus, integrityEvidence)
		if err == nil {
			integrityResult, err = app.BindTestResult(integrityResult, frozenPlan.ID, frozenPlan.HardwareGraph.Value)
		}
		if err != nil {
			fmt.Fprintf(stderr, "build run integrity result: %v\n", err)
			return 1
		}
		testResults = append(testResults, integrityResult)
		fmt.Fprintf(stdout, "HHPT_RUN_INTEGRITY status=%s plan=%s planned_components=%d final_components=%d added=%d removed=%d changed=%d\n", integrityStatus, frozenPlan.ID, len(frozenPlan.HardwareGraph.Components), len(finalPlan.HardwareGraph.Components), len(delta.Added), len(delta.Removed), len(delta.Changed))
		if layout.touchpad && ctx.Err() != nil {
			fmt.Fprintln(stderr, "touchpad finalization interrupted before signing")
			return 1
		}
		if previous != nil && ctx.Err() != nil {
			fmt.Fprintln(stderr, "finalize: linked retest cancelled before signing")
			return 1
		}
		var publicKey ed25519.PublicKey
		var privateKey ed25519.PrivateKey
		var observed time.Time
		if layout.touchpad {
			publicKey, privateKey, observed, err = touchpadSigningInputs(ctx, random, now)
		} else {
			publicKey, privateKey, err = ed25519.GenerateKey(random)
		}
		if err != nil {
			if layout.touchpad {
				fmt.Fprintln(stderr, "prepare touchpad signing inputs failed")
			} else {
				fmt.Fprintf(stderr, "generate self-issued report key: %v\n", err)
			}
			return 1
		}
		if (journal != nil || previous != nil || layout.touchpad) && ctx.Err() != nil {
			fmt.Fprintln(stderr, "session finalization cancelled before signing")
			return 1
		}
		if !layout.touchpad {
			observed = now()
		}
		var encoded []byte
		if relation == nil {
			encoded, err = captureSelfIssuedBundle(snapshot, testResults, fingerprintKey, *keyEpoch, privateKey, publicKey, resolvedSessionID, observed, app.CoverageFrozenPlan)
		} else {
			encoded, err = captureSelfIssuedBundleWithPrevious(snapshot, testResults, fingerprintKey, *keyEpoch, privateKey, publicKey, resolvedSessionID, observed, app.CoverageFrozenPlan, relation)
		}
		if err != nil {
			fmt.Fprintf(stderr, "build self-issued report: %v\n", err)
			return 1
		}
		if (journal != nil || previous != nil || layout.touchpad) && ctx.Err() != nil {
			fmt.Fprintln(stderr, "session finalization cancelled before report output")
			return 1
		}
		var outputErr error
		if layout.touchpad {
			outputErr = writeTouchpadFinalOutput(ctx, *outputPath, stdout, writeOutput, encoded)
		} else {
			outputErr = writeOutput(*outputPath, stdout, func(destination io.Writer) error {
				return writeAll(destination, encoded)
			})
		}
		if outputErr != nil {
			if layout.touchpad {
				fmt.Fprintln(stderr, "write touchpad report failed; completed output may remain")
			} else {
				fmt.Fprintf(stderr, "write report: %v\n", outputErr)
			}
			return 1
		}
		if layout.touchpad {
			if writeAll(stdout, []byte("finalized: signed self-issued report (reduced identity assurance)\n")) != nil || ctx.Err() != nil {
				return 1
			}
		} else {
			fmt.Fprintln(stdout, "finalized: signed self-issued report (reduced identity assurance)")
		}
		return 0
	}()
	return result
}

func validatePlanOutcomes(frozenPlan plan.Plan, outcomes map[string]string) error {
	return validatePlanOutcomesWithDefinitions(frozenPlan, outcomes, quickResultDefinitions)
}

func validatePlanOutcomesWithDefinitions(frozenPlan plan.Plan, outcomes map[string]string, definitions []quickResultDefinition) error {
	implemented := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
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
	for _, definition := range definitions {
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
	return parseOutcomesWithDefinitions(values, quickResultDefinitions)
}

func parseOutcomesWithDefinitions(values []string, definitions []quickResultDefinition) (map[string]string, error) {
	known := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
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
