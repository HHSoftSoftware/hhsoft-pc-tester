package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
)

const sessionJournalSchema = "hhsoft.pc-tester.quick-session/v3"
const maximumSessionJournalBytes = 64 << 10

type sessionJournal struct {
	Schema              string                 `json:"schema"`
	PlanID              string                 `json:"plan_id"`
	StopReason          string                 `json:"stop_reason"`
	BudgetMilliseconds  int64                  `json:"budget_milliseconds"`
	ElapsedMilliseconds int64                  `json:"elapsed_milliseconds"`
	Workers             []sessionWorkerRecord  `json:"workers"`
	Safety              []sessionSafetyRecord  `json:"safety"`
	Monitor             safemode.MonitorResult `json:"monitor"`
}

type sessionWorkerRecord struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Outcome string `json:"outcome"`
}

type sessionSafetyRecord struct {
	Phase  string          `json:"phase"`
	Result safemode.Result `json:"result"`
}

func safeSessionObservation(result safemode.Result) bool {
	return result.Safe() && result.StorageDetail == "" && result.NetworkDetail == ""
}

var sessionWorkerIDs = [...]string{
	"cpu.quick", "memory.quick", "audio.readiness.quick", "input.readiness.quick",
	"input.pointer.quick",
	"network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick",
	"graphics.quick", "display.operator.quick",
}

// The coordinator creates this file once, after workers and console readers
// have stopped. The finalizer independently validates its claims and observes
// current safety; a journal is never authority to bypass an unsafe observation.
func writeSessionJournal(path string, journal sessionJournal) error {
	if err := validateSessionJournalStructure(journal); err != nil {
		return err
	}
	encoded, err := json.Marshal(journal)
	if err != nil || len(encoded)+1 > maximumSessionJournalBytes {
		return errors.New("session journal exceeds its encoding limit")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	writeErr := writeAll(file, append(encoded, '\n'))
	return errors.Join(writeErr, file.Close())
}

func readSessionJournal(path string) (*sessionJournal, error) {
	encoded, err := readSmallRegularFile(path, maximumSessionJournalBytes)
	if err != nil {
		return nil, err
	}
	var journal sessionJournal
	if err := evidenceShape(encoded, reflect.TypeOf(journal), 0); err != nil {
		return nil, errors.New("session journal has an invalid JSON shape")
	}
	if err := json.Unmarshal(encoded, &journal); err != nil {
		return nil, errors.New("session journal has invalid field values")
	}
	if err := validateSessionJournalStructure(journal); err != nil {
		return nil, err
	}
	return &journal, nil
}

func validateSessionJournalStructure(journal sessionJournal) error {
	if journal.Schema != sessionJournalSchema || journal.BudgetMilliseconds < 10 || journal.BudgetMilliseconds > 15*60*1000 ||
		journal.ElapsedMilliseconds < 0 || journal.ElapsedMilliseconds > 60*60*1000 ||
		len(journal.PlanID) != 71 || !strings.HasPrefix(journal.PlanID, "sha256:") || strings.ToLower(journal.PlanID) != journal.PlanID {
		return errors.New("invalid session journal header")
	}
	if _, err := hex.DecodeString(journal.PlanID[7:]); err != nil {
		return errors.New("invalid session journal plan binding")
	}
	if err := validateSessionMonitor(journal.Monitor, journal.ElapsedMilliseconds); err != nil {
		return err
	}
	switch journal.StopReason {
	case "completed", "operator_cancelled", "run_deadline", "test_deadline", "worker_stop":
	default:
		return errors.New("invalid session stop reason")
	}
	if len(journal.Workers) != len(sessionWorkerIDs) {
		return errors.New("session journal must contain every quick worker")
	}
	phases := []string{"preflight"}
	stopped := false
	for index, worker := range journal.Workers {
		if worker.ID != sessionWorkerIDs[index] {
			return errors.New("session workers are missing, duplicated, or out of order")
		}
		switch worker.State {
		case "not_scheduled":
			switch worker.Outcome {
			case "excluded", "not_present", "unsupported", "missing_driver":
			default:
				return errors.New("invalid non-scheduled session outcome")
			}
		case "not_started":
			if worker.Outcome != "aborted" || journal.StopReason == "completed" {
				return errors.New("invalid unfinished session outcome")
			}
			stopped = true
		case "completed", "interrupted":
			if stopped {
				return errors.New("session workers started after the session stopped")
			}
			if worker.State == "interrupted" {
				if journal.StopReason == "completed" || worker.Outcome != "aborted" && worker.Outcome != "error" {
					return errors.New("invalid interrupted session outcome")
				}
				stopped = true
			} else {
				switch worker.Outcome {
				case "pass", "fail", "skip", "inconclusive", "error", "aborted":
				default:
					return errors.New("invalid completed session outcome")
				}
			}
			phases = append(phases, "before-"+worker.ID)
		default:
			return errors.New("invalid session worker state")
		}
	}
	phases = append(phases, "final")
	if len(journal.Safety) != len(phases) {
		return errors.New("session safety observations are incomplete")
	}
	for index, observation := range journal.Safety {
		if observation.Phase != phases[index] || !safeSessionObservation(observation.Result) {
			return errors.New("session safety observations are unsafe, incomplete, or out of order")
		}
	}
	return nil
}

// The bounded summary covers both edges of the monitored span, not only gaps
// between samples. Final snapshots cannot erase an earlier failure or an
// unjoined observer, and a short session cannot invent an observation.
func validateSessionMonitor(result safemode.MonitorResult, elapsed int64) error {
	interval := safemode.MonitorInterval.Milliseconds()
	if result.Schema != safemode.MonitorSchema || result.Status != "pass" ||
		result.IntervalMilliseconds != interval || result.ObservationTimeoutMilliseconds != safemode.MonitorObservationTimeout.Milliseconds() ||
		result.MaximumAllowedGapMilliseconds != safemode.MonitorMaximumGap.Milliseconds() ||
		elapsed < 0 || elapsed > 60*60*1000 || result.ElapsedMilliseconds != elapsed ||
		!result.CleanupConfirmed || result.FirstFailure != nil || result.AttemptedObservations != result.CompletedObservations ||
		result.CompletedObservations > uint64(elapsed/interval) || result.MaximumGapMilliseconds < 0 ||
		result.MaximumGapMilliseconds > result.MaximumAllowedGapMilliseconds || result.MaximumGapMilliseconds > elapsed {
		return errors.New("session monitoring was unsafe, incomplete, or inconsistent")
	}
	count := result.CompletedObservations
	first, last, gap := result.FirstCompletedMilliseconds, result.LastCompletedMilliseconds, result.MaximumGapMilliseconds
	if count == 0 {
		if elapsed >= interval || first != 0 || last != 0 || gap != elapsed || result.LastObservation != nil {
			return errors.New("session monitoring omitted required observations")
		}
		return nil
	}
	if first < interval || last < first || last > elapsed || result.LastObservation == nil || !safeSessionObservation(*result.LastObservation) {
		return errors.New("session monitoring has invalid observation coverage")
	}
	leadingAndTrailing := max(first, elapsed-last)
	if count == 1 {
		if first != last || gap != leadingAndTrailing {
			return errors.New("session monitoring has inconsistent single-sample coverage")
		}
		return nil
	}
	// Counts were bounded above before conversion or arithmetic. Completion
	// timestamps may share a millisecond when a queued tick finishes quickly.
	span, intervals := last-first, int64(count-1)
	minimumGap := max(leadingAndTrailing, (span+intervals-1)/intervals)
	maximumGap := max(leadingAndTrailing, span)
	if last < int64(count)*interval || gap < minimumGap || gap > maximumGap {
		return errors.New("session monitoring count and coverage cannot agree")
	}
	return nil
}

func validateSessionJournal(journal sessionJournal, frozen plan.Plan, outcomes map[string]string, stopReason, evidenceDirectory string) error {
	if err := validateSessionJournalStructure(journal); err != nil {
		return err
	}
	if frozen.Profile != plan.ProfileQuick || journal.PlanID != frozen.ID || journal.StopReason != stopReason {
		return errors.New("session journal does not match the frozen quick plan or stop reason")
	}
	planned := make(map[string]plan.TestStatus, len(frozen.Tests))
	for _, test := range frozen.Tests {
		planned[test.ID] = test.Status
	}
	for _, worker := range journal.Workers {
		status, present := planned[worker.ID]
		if !present || worker.Outcome != outcomes[worker.ID] || (worker.State == "not_scheduled") != (status != plan.TestScheduled) {
			return fmt.Errorf("session worker %s does not match its frozen outcome", worker.ID)
		}
		if worker.State == "not_scheduled" && worker.Outcome != string(status) {
			return errors.New("session journal changed a non-scheduled disposition")
		}
		if worker.State == "not_started" || worker.State == "not_scheduled" {
			for _, definition := range quickResultDefinitions {
				if definition.id != worker.ID {
					continue
				}
				if _, err := os.Lstat(filepath.Join(evidenceDirectory, definition.filename)); !errors.Is(err, os.ErrNotExist) {
					return errors.New("evidence exists or cannot be excluded for an unstarted session worker")
				}
			}
		}
		if sessionSafetyWorker(worker.ID) && worker.State != "not_started" && worker.State != "not_scheduled" &&
			(worker.State != "completed" || worker.Outcome != "pass" && worker.Outcome != "inconclusive") {
			return errors.New("interrupted or unsuccessful safety readiness cannot authorize session signing")
		}
	}
	return nil
}

func sessionSafetyWorker(id string) bool {
	return id == "network.readiness.quick" || id == "storage.readiness.quick"
}

// Completed observations require their actual evidence. Ordinary command
// errors and interrupted workers retain the existing error/aborted note path;
// the finalizer separately rejects every explicit failure downgrade.
func validateSessionWorkerEvidence(journal *sessionJournal, id string, encoded []byte, readErr error, sourceStatus string, statusErr error) error {
	if journal == nil {
		return nil
	}
	for _, worker := range journal.Workers {
		if worker.ID != id {
			continue
		}
		if worker.State == "not_started" || worker.State == "not_scheduled" {
			if !errors.Is(readErr, os.ErrNotExist) {
				return errors.New("evidence appeared for an unstarted session worker")
			}
			return nil
		}
		if worker.State != "completed" {
			return nil
		}
		if !sessionSafetyWorker(id) && (worker.Outcome == "error" || worker.Outcome == "aborted") {
			return nil
		}
		if readErr != nil || statusErr != nil || sourceStatus != worker.Outcome {
			return errors.New("completed session worker lacks matching valid evidence")
		}
		if id == "network.readiness.quick" {
			var result networktest.Result
			if json.Unmarshal(encoded, &result) != nil || !result.RequireOffline || result.NetworkTrafficGenerated || result.ActiveNonLoopInterfaces != 0 {
				return errors.New("network observations cannot authorize safe session signing")
			}
			for _, network := range result.Interfaces {
				if network.AdministrativeUp {
					return errors.New("active network observation cannot be cleared by a later snapshot")
				}
			}
		}
		if id == "storage.readiness.quick" {
			var result storage.Result
			if json.Unmarshal(encoded, &result) != nil || result.MediaContentRead || result.MediaContentWrites || result.WritableMounts != 0 {
				return errors.New("storage observations cannot authorize safe session signing")
			}
			for _, device := range result.Devices {
				if device.WritableMount {
					return errors.New("writable mount observation cannot be cleared by a later snapshot")
				}
			}
		}
	}
	return nil
}
