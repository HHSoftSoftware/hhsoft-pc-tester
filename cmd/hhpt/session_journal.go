package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
)

const sessionJournalSchema = sessionevidence.Schema
const maximumSessionJournalBytes = sessionevidence.MaximumBytes

type sessionJournal = sessionevidence.Journal
type sessionWorkerRecord = sessionevidence.WorkerRecord
type sessionSafetyRecord = sessionevidence.SafetyRecord

func safeSessionObservation(result safemode.Result) bool {
	return result.Safe() && result.StorageDetail == "" && result.NetworkDetail == ""
}

var sessionWorkerIDs = sessionevidence.WorkerIDs()

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
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	retained, err := readSessionJournal(path)
	if err != nil {
		return err
	}
	if !sameSessionJournal(*retained, journal) {
		return errors.New("retained session journal changed")
	}
	return nil
}

func readSessionJournal(path string) (*sessionJournal, error) {
	encoded, err := readSmallRegularFile(path, maximumSessionJournalBytes)
	if err != nil {
		return nil, err
	}
	journal, err := sessionevidence.Decode(encoded)
	if err != nil {
		return nil, err
	}
	return &journal, nil
}

func validateSessionJournalStructure(journal sessionJournal) error {
	return sessionevidence.ValidateStructure(journal)
}
func validateSessionMonitor(result safemode.MonitorResult, elapsed int64) error {
	return sessionevidence.ValidateMonitor(result, elapsed)
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
