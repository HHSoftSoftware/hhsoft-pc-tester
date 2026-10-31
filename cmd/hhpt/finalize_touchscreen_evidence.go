package main

import (
	"bytes"
	"errors"
	"os"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchscreenevidence"
)

const touchscreenExecutionID = "input.touchscreen.quick"
const touchscreenExecutionLimitMilliseconds = 45000

// The owner supplies checked plan/inventory inputs and retains direct file
// absence, command exit, cleanup, and worker-interval authority.
func newTouchscreenFinalResult(test plan.Test, sourceStatus string, raw []byte, readErr error) (app.TestResult, error) {
	if test.ID != touchscreenExecutionID || test.Version != 2 {
		return app.TestResult{}, touchscreenevidence.ErrInvalid
	}
	status, ok := normalizeOutcome(sourceStatus)
	if !ok {
		return app.TestResult{}, touchscreenevidence.ErrInvalid
	}
	if readErr != nil {
		if readErr != os.ErrNotExist || len(raw) != 0 {
			return app.TestResult{}, touchscreenevidence.ErrInvalid
		}
		result, err := newExecutionNote(test, status, sourceStatus)
		if err != nil {
			return app.TestResult{}, err
		}
		binding := sessionevidence.ResultBinding{
			ID: test.ID, Version: test.Version, PlannedStatus: string(test.Status),
			ReportStatus: string(status), Evidence: result.Evidence,
		}
		if err := sessionevidence.ValidateTouchscreenNote(binding); err != nil {
			return app.TestResult{}, err
		}
		return result, nil
	}
	value, err := touchscreenevidence.Decode(raw)
	if err != nil {
		return app.TestResult{}, err
	}
	if test.Status != plan.TestScheduled || string(value.Status) != sourceStatus ||
		string(status) != sourceStatus {
		return app.TestResult{}, touchscreenevidence.ErrInvalid
	}
	return app.NewEncodedTestResult(test.ID, test.Version, status, append([]byte(nil), raw...))
}

func validateTouchscreenPlanEvidence(raw []byte, test plan.Test, frozen, final plan.Plan, snapshot inventory.Snapshot) error {
	value, err := touchscreenevidence.Decode(raw)
	if err != nil {
		return err
	}
	return touchscreenevidence.ValidatePlanBinding(value, test, frozen, final, snapshot)
}

func recheckTouchscreenEvidence(path string, original []byte, present bool, read func(string) ([]byte, error)) error {
	if read == nil || present && len(original) == 0 || !present && len(original) != 0 {
		return touchscreenevidence.ErrInvalid
	}
	wanted := append([]byte(nil), original...)
	current, err := read(path)
	if !present {
		if err == os.ErrNotExist && len(current) == 0 {
			return nil
		}
		return touchscreenevidence.ErrInvalid
	}
	if err != nil || !bytes.Equal(wanted, current) {
		return touchscreenevidence.ErrInvalid
	}
	return nil
}

func touchscreenWorkerOutcomeEvidence(raw []byte, readErr error, code int, interrupted bool) (string, string, error) {
	if code == workerCleanupUncertainExitCode {
		return "", "", touchscreenevidence.ErrCleanupUnconfirmed
	}
	if readErr != nil {
		if readErr != os.ErrNotExist || len(raw) != 0 {
			return "", "", touchscreenevidence.ErrInvalid
		}
		if interrupted {
			return "aborted", "interrupted", nil
		}
		return "error", "completed", nil
	}
	value, err := touchscreenevidence.Decode(raw)
	if err != nil {
		return "", "", err
	}
	// The touchscreen command returns zero after a successful publication, including
	// ordinary nonpass observations. A nonzero exit cannot be rescued by a file.
	if code != 0 || value.LimitMilliseconds != touchscreenExecutionLimitMilliseconds {
		return "", "", touchscreenevidence.ErrInvalid
	}
	state := "completed"
	if interrupted && (value.Status == inputtest.StatusAborted || value.Status == inputtest.StatusError) {
		state = "interrupted"
	}
	return string(value.Status), state, nil
}

func validateTouchscreenWorkerRecord(worker sessionWorkerRecord, raw []byte, readErr error, sourceStatus string, statusErr error) error {
	if touchscreenCleanupError(statusErr) {
		return statusErr
	}
	if worker.ID != touchscreenExecutionID {
		return touchscreenevidence.ErrInvalid
	}
	if worker.State == "not_started" || worker.State == "not_scheduled" {
		if readErr != os.ErrNotExist || len(raw) != 0 {
			return touchscreenevidence.ErrInvalid
		}
		return nil
	}
	if (worker.State != "completed" && worker.State != "interrupted") || worker.Timing == nil ||
		worker.Timing.StartedMilliseconds < 0 || worker.Timing.FinishedMilliseconds < worker.Timing.StartedMilliseconds {
		return touchscreenevidence.ErrInvalid
	}
	if readErr != nil {
		if readErr != os.ErrNotExist || len(raw) != 0 || (worker.Outcome != "error" && worker.Outcome != "aborted") {
			return touchscreenevidence.ErrInvalid
		}
		return nil
	}
	value, err := touchscreenevidence.Decode(raw)
	if err != nil {
		return err
	}
	if statusErr != nil {
		return statusErr
	}
	if sourceStatus != worker.Outcome || string(value.Status) != worker.Outcome ||
		value.LimitMilliseconds != touchscreenExecutionLimitMilliseconds ||
		value.DurationMilliseconds > worker.Timing.FinishedMilliseconds-worker.Timing.StartedMilliseconds {
		return touchscreenevidence.ErrInvalid
	}
	return nil
}

func touchscreenCleanupError(err error) bool {
	return errors.Is(err, touchscreenevidence.ErrCleanupUnconfirmed)
}
