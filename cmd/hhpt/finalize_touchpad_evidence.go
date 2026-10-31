package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"os"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchpadevidence"
)

const touchpadExecutionID = "input.touchpad.quick"
const touchpadExecutionLimitMilliseconds = 30000

type commandExecutionLayout struct {
	definitions []quickResultDefinition
	workerIDs   []string
	schema      string
	touchpad    bool
	touchscreen bool
}

// Call only after the owner has checked the frozen plan. This finite selector
// preserves versioned layouts; it does not replace current-catalog Plan.Decode.
func commandLayoutForPlan(frozen plan.Plan, profile plan.Profile) (commandExecutionLayout, error) {
	requestedProfile := profile
	if profile == "" {
		profile = plan.ProfileQuick
	}
	definitions, err := executionResultDefinitions(profile)
	if err != nil || requestedProfile != "" && frozen.Profile != profile ||
		requestedProfile == "" && frozen.Profile == plan.ProfileStandard {
		return commandExecutionLayout{}, touchpadevidence.ErrInvalid
	}
	layout := commandExecutionLayout{definitions: definitions, schema: sessionevidence.SchemaV7}
	ids := sessionevidence.WorkerIDsV7()
	layout.workerIDs = append([]string(nil), ids[:]...)
	if profile == plan.ProfileStandard {
		standard := sessionevidence.WorkerIDsV8()
		layout.workerIDs = append([]string(nil), standard[:]...)
		layout.schema = sessionevidence.SchemaV8
	}
	seen := make(map[string]bool, len(frozen.Tests))
	for _, test := range frozen.Tests {
		if test.ID == "" || seen[test.ID] {
			return commandExecutionLayout{}, touchpadevidence.ErrInvalid
		}
		seen[test.ID] = true
		if test.ID == touchscreenExecutionID {
			if test.Version == 1 {
				if test.Status == plan.TestScheduled {
					return commandExecutionLayout{}, touchpadevidence.ErrInvalid
				}
				continue
			}
			if test.Version != 2 || frozen.Profile != profile || !frozen.Frozen ||
				frozen.SchemaVersion != plan.SchemaVersion || frozen.ID == "" ||
				test.Capability != "input.touchscreen" || test.Destructive ||
				test.EstimatedSeconds != 45 || test.TimeoutSeconds != 120 {
				return commandExecutionLayout{}, touchpadevidence.ErrInvalid
			}
			switch test.Status {
			case plan.TestScheduled, plan.TestExcluded, plan.TestNotPresent, plan.TestUnsupported, plan.TestMissingDriver:
			default:
				return commandExecutionLayout{}, touchpadevidence.ErrInvalid
			}
			layout.touchscreen = true
			continue
		}
		if test.ID != touchpadExecutionID {
			continue
		}
		if test.Version == 1 {
			if test.Status == plan.TestScheduled {
				return commandExecutionLayout{}, touchpadevidence.ErrInvalid
			}
			continue
		}
		if test.Version != 2 || frozen.Profile != profile || !frozen.Frozen ||
			frozen.SchemaVersion != plan.SchemaVersion || frozen.ID == "" ||
			test.Capability != "input.touchpad" || test.Destructive ||
			test.EstimatedSeconds != 30 || test.TimeoutSeconds != 90 {
			return commandExecutionLayout{}, touchpadevidence.ErrInvalid
		}
		switch test.Status {
		case plan.TestScheduled, plan.TestExcluded, plan.TestNotPresent, plan.TestUnsupported, plan.TestMissingDriver:
		default:
			return commandExecutionLayout{}, touchpadevidence.ErrInvalid
		}
		layout.touchpad = true
	}
	if layout.touchpad {
		layout.definitions = definitionsWithTouchpad(definitions)
		quick := sessionevidence.WorkerIDsV9()
		layout.workerIDs = append([]string(nil), quick[:]...)
		layout.schema = sessionevidence.SchemaV9
		if profile == plan.ProfileStandard {
			standard := sessionevidence.WorkerIDsV10()
			layout.workerIDs = append([]string(nil), standard[:]...)
			layout.schema = sessionevidence.SchemaV10
		}
	}
	if layout.touchscreen {
		// Current touchscreen contracts retain the touchpad worker and its
		// owned finalization path. An isolated version upgrade is invalid.
		if !layout.touchpad {
			return commandExecutionLayout{}, touchpadevidence.ErrInvalid
		}
		layout.definitions = definitionsWithTouchscreen(layout.definitions)
		quick := sessionevidence.WorkerIDsV11()
		layout.workerIDs = append([]string(nil), quick[:]...)
		layout.schema = sessionevidence.SchemaV11
		if profile == plan.ProfileStandard {
			standard := sessionevidence.WorkerIDsV12()
			layout.workerIDs = append([]string(nil), standard[:]...)
			layout.schema = sessionevidence.SchemaV12
		}
	}
	return layout, nil
}

// This owned list is also used for preliminary -outcome syntax checking. That
// check cannot select a route; the checked frozen plan must select the same list.
func definitionsWithTouchpad(before []quickResultDefinition) []quickResultDefinition {
	result := make([]quickResultDefinition, 0, len(before)+1)
	for _, definition := range before {
		result = append(result, definition)
		if definition.id == "input.pointer.quick" {
			result = append(result, quickResultDefinition{
				id: touchpadExecutionID, filename: "touchpad.json", schema: inputtest.TouchpadSchema,
			})
		}
	}
	return result
}

// Keep preliminary syntax selection owned and separate from plan admission.
func definitionsWithTouchscreen(before []quickResultDefinition) []quickResultDefinition {
	result := make([]quickResultDefinition, 0, len(before)+1)
	for _, definition := range before {
		result = append(result, definition)
		if definition.id == touchpadExecutionID {
			result = append(result, quickResultDefinition{
				id: touchscreenExecutionID, filename: "touchscreen.json", schema: inputtest.TouchscreenSchema,
			})
		}
	}
	return result
}

func currentSessionProfileForPlan(journal sessionJournal, frozen plan.Plan, profile plan.Profile) bool {
	layout, err := commandLayoutForPlan(frozen, profile)
	if err != nil || journal.Schema != layout.schema {
		return false
	}
	if !layout.touchpad {
		return currentSessionProfile(journal, profile)
	}
	if frozen.Profile == plan.ProfileStandard {
		return journal.Profile == "standard"
	}
	return journal.Profile == "" && journal.CPUStressSource == nil
}

// Standalone frozen-plan binding is allowed without a session-monitoring claim.
// The caller separately enforces live owner authority whenever a journal exists.
func newTouchpadFinalResult(test plan.Test, sourceStatus string, raw []byte, readErr error) (app.TestResult, error) {
	if test.ID != touchpadExecutionID || test.Version != 2 {
		return app.TestResult{}, touchpadevidence.ErrInvalid
	}
	status, ok := normalizeOutcome(sourceStatus)
	if !ok {
		return app.TestResult{}, touchpadevidence.ErrInvalid
	}
	if readErr != nil {
		if readErr != os.ErrNotExist || len(raw) != 0 {
			return app.TestResult{}, touchpadevidence.ErrInvalid
		}
		result, err := newExecutionNote(test, status, sourceStatus)
		if err != nil {
			return app.TestResult{}, err
		}
		binding := sessionevidence.ResultBinding{
			ID: test.ID, Version: test.Version, PlannedStatus: string(test.Status),
			ReportStatus: string(status), Evidence: result.Evidence,
		}
		if err := sessionevidence.ValidateTouchpadNote(binding); err != nil {
			return app.TestResult{}, err
		}
		return result, nil
	}
	value, err := touchpadevidence.Decode(raw)
	if err != nil {
		return app.TestResult{}, err
	}
	if test.Status != plan.TestScheduled || string(value.Status) != sourceStatus ||
		string(status) != sourceStatus {
		return app.TestResult{}, touchpadevidence.ErrInvalid
	}
	return app.NewEncodedTestResult(test.ID, test.Version, status, append([]byte(nil), raw...))
}

func validateTouchpadPlanEvidence(raw []byte, test plan.Test, frozen, final plan.Plan, snapshot inventory.Snapshot) error {
	value, err := touchpadevidence.Decode(raw)
	if err != nil {
		return err
	}
	return touchpadevidence.ValidatePlanBinding(value, test, frozen, final, snapshot)
}

// Preserve original attributes, order, duplicates and nil slices. Canonicalizing
// the snapshot here could hide malformed input before the strict binder sees it.
func freezeTouchpadFinalInputs(frozen plan.Plan, snapshot inventory.Snapshot, key []byte) (plan.Plan, inventory.Snapshot, []byte, error) {
	layout, err := commandLayoutForPlan(frozen, frozen.Profile)
	if err != nil || !layout.touchpad || len(key) != 32 || snapshot.SchemaVersion != inventory.SnapshotSchemaVersion {
		return plan.Plan{}, inventory.Snapshot{}, nil, touchpadevidence.ErrInvalid
	}
	ownedPlan := frozen
	ownedPlan.Tests = append(frozen.Tests[:0:0], frozen.Tests...)
	for index := range ownedPlan.Tests {
		ownedPlan.Tests[index].ComponentIDs = append(frozen.Tests[index].ComponentIDs[:0:0], frozen.Tests[index].ComponentIDs...)
	}
	ownedPlan.Capabilities = append(frozen.Capabilities[:0:0], frozen.Capabilities...)
	for index := range ownedPlan.Capabilities {
		ownedPlan.Capabilities[index].ComponentIDs = append(frozen.Capabilities[index].ComponentIDs[:0:0], frozen.Capabilities[index].ComponentIDs...)
	}
	ownedPlan.HardwareGraph.Components = append(frozen.HardwareGraph.Components[:0:0], frozen.HardwareGraph.Components...)
	ownedPlan.ExcludedTests = append(frozen.ExcludedTests[:0:0], frozen.ExcludedTests...)
	ownedSnapshot := snapshot
	ownedSnapshot.Components = append(snapshot.Components[:0:0], snapshot.Components...)
	for index := range ownedSnapshot.Components {
		ownedSnapshot.Components[index].Attributes = append(snapshot.Components[index].Attributes[:0:0], snapshot.Components[index].Attributes...)
	}
	return ownedPlan, ownedSnapshot, append([]byte(nil), key...), nil
}

func recheckTouchpadEvidence(path string, original []byte, present bool, read func(string) ([]byte, error)) error {
	if read == nil || present && len(original) == 0 || !present && len(original) != 0 {
		return touchpadevidence.ErrInvalid
	}
	wanted := append([]byte(nil), original...)
	current, err := read(path)
	if !present {
		if err == os.ErrNotExist && len(current) == 0 {
			return nil
		}
		return touchpadevidence.ErrInvalid
	}
	if err != nil || !bytes.Equal(wanted, current) {
		return touchpadevidence.ErrInvalid
	}
	return nil
}

func touchpadWorkerOutcomeEvidence(raw []byte, readErr error, code int, interrupted bool) (string, string, error) {
	if code == workerCleanupUncertainExitCode {
		return "", "", touchpadevidence.ErrCleanupUnconfirmed
	}
	if readErr != nil {
		if readErr != os.ErrNotExist || len(raw) != 0 {
			return "", "", touchpadevidence.ErrInvalid
		}
		if interrupted {
			return "aborted", "interrupted", nil
		}
		return "error", "completed", nil
	}
	value, err := touchpadevidence.Decode(raw)
	if err != nil {
		return "", "", err
	}
	// The touchpad command returns zero after a successful publication, including
	// ordinary nonpass observations. A nonzero exit cannot be rescued by a file.
	if code != 0 || value.LimitMilliseconds != touchpadExecutionLimitMilliseconds {
		return "", "", touchpadevidence.ErrInvalid
	}
	state := "completed"
	if interrupted && (value.Status == inputtest.StatusAborted || value.Status == inputtest.StatusError) {
		state = "interrupted"
	}
	return string(value.Status), state, nil
}

func validateTouchpadWorkerRecord(worker sessionWorkerRecord, raw []byte, readErr error, sourceStatus string, statusErr error) error {
	if touchpadCleanupError(statusErr) {
		return statusErr
	}
	if worker.ID != touchpadExecutionID {
		return touchpadevidence.ErrInvalid
	}
	if worker.State == "not_started" || worker.State == "not_scheduled" {
		if readErr != os.ErrNotExist || len(raw) != 0 {
			return touchpadevidence.ErrInvalid
		}
		return nil
	}
	if (worker.State != "completed" && worker.State != "interrupted") || worker.Timing == nil ||
		worker.Timing.StartedMilliseconds < 0 || worker.Timing.FinishedMilliseconds < worker.Timing.StartedMilliseconds {
		return touchpadevidence.ErrInvalid
	}
	if readErr != nil {
		if readErr != os.ErrNotExist || len(raw) != 0 || (worker.Outcome != "error" && worker.Outcome != "aborted") {
			return touchpadevidence.ErrInvalid
		}
		return nil
	}
	value, err := touchpadevidence.Decode(raw)
	if err != nil {
		return err
	}
	if statusErr != nil {
		return statusErr
	}
	if sourceStatus != worker.Outcome || string(value.Status) != worker.Outcome ||
		value.LimitMilliseconds != touchpadExecutionLimitMilliseconds ||
		value.DurationMilliseconds > worker.Timing.FinishedMilliseconds-worker.Timing.StartedMilliseconds {
		return touchpadevidence.ErrInvalid
	}
	return nil
}

// Keep the cleanup type available through callers that compose ordinary errors.
func touchpadCleanupError(err error) bool {
	return errors.Is(err, touchpadevidence.ErrCleanupUnconfirmed)
}

var errTouchpadFinalizationInterrupted = errors.New("touchpad finalization interrupted")

// These are actual signing inputs, not a replacement for report validation.
// Return no key material after a callback or context makes completion uncertain.
func touchpadSigningInputs(ctx context.Context, random io.Reader, now func() time.Time) (ed25519.PublicKey, ed25519.PrivateKey, time.Time, error) {
	if nilRetestInputValue(ctx) || nilRetestInputValue(random) || now == nil {
		return nil, nil, time.Time{}, touchpadevidence.ErrInvalid
	}
	if ctx.Err() != nil {
		return nil, nil, time.Time{}, errTouchpadFinalizationInterrupted
	}
	public, private, err := ed25519.GenerateKey(random)
	if ctx.Err() != nil {
		return nil, nil, time.Time{}, errors.Join(err, errTouchpadFinalizationInterrupted)
	}
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	observed := now()
	if ctx.Err() != nil {
		return nil, nil, time.Time{}, errTouchpadFinalizationInterrupted
	}
	return public, private, observed, nil
}

// The destination retains ownership of its Close result. Already written bytes
// stay in place after late cancellation; neither retry nor rollback is attempted.
func writeTouchpadFinalOutput(ctx context.Context, path string, stdout io.Writer, writeOutput destinationWriter, raw []byte) error {
	if nilRetestInputValue(ctx) || writeOutput == nil || len(raw) == 0 {
		return touchpadevidence.ErrInvalid
	}
	frozen := append([]byte(nil), raw...)
	if ctx.Err() != nil {
		return errTouchpadFinalizationInterrupted
	}
	calls := 0
	var callbackErr error
	outputErr := writeOutput(path, stdout, func(destination io.Writer) error {
		calls++
		if calls != 1 || nilRetestInputValue(destination) {
			callbackErr = io.ErrClosedPipe
			return callbackErr
		}
		if ctx.Err() != nil {
			callbackErr = errTouchpadFinalizationInterrupted
			return callbackErr
		}
		callbackErr = writeAll(destination, frozen)
		if ctx.Err() != nil {
			callbackErr = errors.Join(callbackErr, errTouchpadFinalizationInterrupted)
		}
		return callbackErr
	})
	failure := errors.Join(outputErr, callbackErr)
	if calls != 1 {
		failure = errors.Join(failure, io.ErrClosedPipe)
	}
	if ctx.Err() != nil {
		failure = errors.Join(failure, errTouchpadFinalizationInterrupted)
	}
	return failure
}
