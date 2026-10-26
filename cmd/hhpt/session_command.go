package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
)

type sessionWorker struct {
	ID, Architecture, OutputPath string
	Timeout                      time.Duration
}

type sessionFinalization struct {
	PlanPath, EvidenceDirectory, FingerprintKeyPath, KeyEpoch    string
	FixtureComponents, OutputPath, SafetyJournalPath, StopReason string
	Outcomes                                                     []string
}

type sessionDependencies struct {
	RunWorker     func(context.Context, sessionWorker, io.Reader, io.Writer, io.Writer) int
	ObserveSafety func(context.Context) safemode.Result
	StartMonitor  func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error)
	Finalize      func(context.Context, sessionFinalization, io.Writer, io.Writer) int
	FinishInput   func() error
	Now           func() time.Time
}

type sessionSafetyMonitor interface {
	Failed() bool
	Stop(context.Context) safemode.MonitorResult
}

func runSession(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	ctx, input, finish, err := prepareSessionInput(ctx, input)
	if err != nil {
		fmt.Fprintf(stderr, "prepare session input: %v\n", err)
		return 1
	}
	defer finish()
	return runSessionWith(ctx, arguments, input, stdout, stderr, sessionDependencies{
		RunWorker: runSessionWorker, ObserveSafety: safemode.Observe,
		StartMonitor: func(started time.Time, cancel context.CancelFunc) (sessionSafetyMonitor, error) {
			return safemode.StartMonitor(safemode.Observe, safemode.MonitorOptions{Started: started, Cancel: cancel})
		},
		Finalize: finalizeSession, FinishInput: finish, Now: time.Now,
	})
}

// Adapters run synchronously: finalization cannot overlap a worker that has not
// returned. Context deadlines request cooperative cancellation; they cannot
// preempt an uninterruptible kernel operation. External graphics tools retain
// workerexec's separate ownership and confirmed-cleanup requirement.
func runSessionWith(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer, dependencies sessionDependencies) (exitCode int) {
	flags := flag.NewFlagSet("session", flag.ContinueOnError)
	flags.SetOutput(stderr)
	planPath := flags.String("plan", "", "frozen quick-test plan")
	keyPath := flags.String("fingerprint-key", "", "session fingerprint key")
	keyEpoch := flags.String("key-epoch", "ephemeral-session", "fingerprint-key epoch")
	fixture := flags.String("fixture-components", "", "selected deployment fixture component IDs")
	directory := flags.String("evidence-dir", "/run/hhpt", "direct directory for fresh worker evidence")
	output := flags.String("output", "", "new signed report file")
	budget := flags.Duration("timeout", 5*time.Minute, "immutable cooperative quick-session budget")
	if flags.Parse(arguments) != nil {
		return 2
	}
	if flags.NArg() != 0 || *planPath == "" || *keyPath == "" || *output == "" || *output == "-" || *budget < 10*time.Millisecond || *budget > 15*time.Minute {
		fmt.Fprintln(stderr, "session requires -plan, -fingerprint-key, -output, and a 10ms-15m timeout")
		return 2
	}
	if ctx == nil || dependencies.RunWorker == nil || dependencies.ObserveSafety == nil || dependencies.StartMonitor == nil || dependencies.Finalize == nil || dependencies.FinishInput == nil || dependencies.Now == nil {
		fmt.Fprintln(stderr, "session dependencies are incomplete")
		return 1
	}
	if err := validateEvidenceDirectory(*directory); err != nil {
		fmt.Fprintf(stderr, "validate session directory: %v\n", err)
		return 1
	}
	encoded, err := readSmallRegularFile(*planPath, 1<<20)
	if err != nil {
		fmt.Fprintf(stderr, "read session plan: %v\n", err)
		return 1
	}
	frozen, err := plan.Decode(encoded)
	if err != nil || frozen.Profile != plan.ProfileQuick || frozen.HardwareGraph.KeyEpoch != *keyEpoch {
		fmt.Fprintln(stderr, "session requires a valid quick plan with the matching key epoch")
		return 1
	}
	definitions := make(map[string]quickResultDefinition, len(quickResultDefinitions))
	for _, definition := range quickResultDefinitions {
		definitions[definition.id] = definition
	}
	planned := make(map[string]plan.Test, len(frozen.Tests))
	for _, test := range frozen.Tests {
		if test.Status == plan.TestScheduled {
			if _, ok := definitions[test.ID]; !ok {
				fmt.Fprintln(stderr, "session plan schedules an unknown worker")
				return 1
			}
		}
		planned[test.ID] = test
	}
	journalPath := filepath.Join(*directory, "session-journal.json")
	monitorPath := filepath.Join(*directory, "session-safety-monitor.json")
	readyPath := filepath.Join(*directory, "session-ready.txt")
	freshPaths := []string{journalPath, monitorPath, readyPath, *output, filepath.Join(*directory, "session-safety-preflight.json"), filepath.Join(*directory, "session-safety-final.json")}
	journal := sessionJournal{Schema: sessionJournalSchema, PlanID: frozen.ID,
		StopReason: "completed", BudgetMilliseconds: budget.Milliseconds()}
	scheduled := 0
	for _, id := range sessionWorkerIDs {
		test, exists := planned[id]
		if !exists {
			fmt.Fprintln(stderr, "session plan omits a required quick worker")
			return 1
		}
		record := sessionWorkerRecord{ID: id, State: "not_scheduled", Outcome: string(test.Status)}
		if test.Status == plan.TestScheduled {
			record.State, record.Outcome = "not_started", "aborted"
			scheduled++
		}
		journal.Workers = append(journal.Workers, record)
		freshPaths = append(freshPaths, filepath.Join(*directory, definitions[id].filename))
		freshPaths = append(freshPaths, filepath.Join(*directory, "session-safety-before-"+id+".json"))
	}
	for _, path := range freshPaths {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stderr, "session requires fresh worker, journal, and report destinations")
			return 1
		}
	}
	started := dependencies.Now()
	deadline := started.Add(*budget)
	runContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	monitor, err := dependencies.StartMonitor(started, cancel)
	if err != nil || monitor == nil {
		fmt.Fprintln(stderr, "start session safety monitor failed")
		return 1
	}
	// Monitoring has its own lifetime: operator cancellation must not stop it
	// before worker/input cleanup and the final safety observation. Every exit
	// joins it and preserves diagnostic evidence, even when an earlier step failed.
	monitorStopped, monitorCode := false, 0
	finishMonitor := func() int {
		if monitorStopped {
			return monitorCode
		}
		monitorStopped = true
		bounded, stop := context.WithTimeout(context.Background(), safemode.MonitorStopTimeout)
		journal.Monitor = monitor.Stop(bounded)
		stop()
		journal.ElapsedMilliseconds = journal.Monitor.ElapsedMilliseconds
		if !journal.Monitor.CleanupConfirmed {
			fmt.Fprintln(stderr, "HHPT_SESSION_FAILED reason=safety_monitor_cleanup_unconfirmed")
			monitorCode = workerCleanupUncertainExitCode
		} else if err := validateSessionMonitor(journal.Monitor, journal.ElapsedMilliseconds); err != nil || monitor.Failed() {
			fmt.Fprintln(stderr, "HHPT_SESSION_UNSAFE phase=monitor")
			monitorCode = 1
		}
		if writeQuickResult(journal.Monitor, monitorPath, stdout, stderr, "session safety monitor") != 0 && monitorCode == 0 {
			monitorCode = 1
		}
		return monitorCode
	}
	defer func() {
		code := finishMonitor()
		if code == workerCleanupUncertainExitCode || exitCode == 0 && code != 0 {
			exitCode = code
		}
	}()
	observe := func(observationContext context.Context, phase string) bool {
		bounded, stop := context.WithTimeout(observationContext, 5*time.Second)
		result := dependencies.ObserveSafety(bounded)
		observationErr := bounded.Err()
		stop()
		journal.Safety = append(journal.Safety, sessionSafetyRecord{Phase: phase, Result: result})
		if writeQuickResult(result, filepath.Join(*directory, "session-safety-"+phase+".json"), stdout, stderr, "session safety") != 0 {
			return false
		}
		if !safeSessionObservation(result) || observationErr != nil {
			fmt.Fprintf(stderr, "HHPT_SESSION_UNSAFE phase=%s\n", phase)
			return false
		}
		return true
	}
	// A cancellation before any kernel observation cannot establish a safe run.
	if !observe(runContext, "preflight") {
		return 1
	}
	completed := 0
	reported := make(map[string]bool)
	reportWorker := func(record sessionWorkerRecord) {
		if !reported[record.ID] {
			printSessionWorkerResult(stdout, record)
			reported[record.ID] = true
		}
	}
	fmt.Fprintf(stdout, "HHPT_SESSION_START budget_ms=%d scheduled=%d\n", budget.Milliseconds(), scheduled)
	for index := range journal.Workers {
		if monitor.Failed() {
			return 1
		}
		record := &journal.Workers[index]
		if record.State != "not_started" {
			reportWorker(*record)
			continue
		}
		if reason := sessionStopReason(ctx, runContext, nil, dependencies.Now(), deadline, time.Time{}); reason != "" {
			journal.StopReason = reason
			break
		}
		if !observe(runContext, "before-"+record.ID) {
			return 1
		}
		if monitor.Failed() {
			return 1
		}
		// An input stop racing the safety observation must not start a worker.
		if reason := sessionStopReason(ctx, runContext, nil, dependencies.Now(), deadline, time.Time{}); reason != "" {
			journal.Safety = journal.Safety[:len(journal.Safety)-1]
			journal.StopReason = reason
			break
		}
		test := planned[record.ID]
		workerStart := dependencies.Now()
		workerDeadline := workerStart.Add(time.Duration(test.TimeoutSeconds) * time.Second)
		if deadline.Before(workerDeadline) {
			workerDeadline = deadline
		}
		workerContext, stopWorker := context.WithDeadline(runContext, workerDeadline)
		worker := sessionWorker{ID: record.ID, Architecture: frozen.Architecture,
			OutputPath: filepath.Join(*directory, definitions[record.ID].filename), Timeout: time.Duration(test.TimeoutSeconds) * time.Second}
		fmt.Fprintf(stdout, "HHPT_SESSION_WORKER id=%s completed=%d scheduled=%d elapsed_ms=%d remaining_ms=%d\n",
			record.ID, completed, scheduled, workerStart.Sub(started).Milliseconds(), max(0, deadline.Sub(workerStart).Milliseconds()))
		code := dependencies.RunWorker(workerContext, worker, input, stdout, stderr)
		reason := sessionStopReason(ctx, runContext, workerContext, dependencies.Now(), deadline, workerDeadline)
		stopWorker()
		if code == workerCleanupUncertainExitCode {
			fmt.Fprintf(stderr, "HHPT_SESSION_FAILED worker=%s reason=cleanup_unconfirmed\n", record.ID)
			return workerCleanupUncertainExitCode
		}
		if monitor.Failed() {
			return 1
		}
		outcome, state, evidenceErr := sessionWorkerOutcome(worker.OutputPath, definitions[record.ID].schema, code, reason != "")
		if evidenceErr != nil {
			fmt.Fprintf(stderr, "session worker evidence: %v\n", evidenceErr)
			return 1
		}
		record.State, record.Outcome = state, outcome
		if state == "completed" {
			completed++
		}
		fmt.Fprintf(stdout, "HHPT_SESSION_WORKER_RESULT id=%s state=%s outcome=%s\n", record.ID, state, outcome)
		reportWorker(*record)
		if (record.ID == "network.readiness.quick" || record.ID == "storage.readiness.quick") &&
			(state != "completed" || outcome != "pass" && outcome != "inconclusive") {
			fmt.Fprintln(stderr, "HHPT_SESSION_UNSAFE phase=readiness")
			return 1
		}
		if sessionSafetyWorker(record.ID) {
			encoded, readErr := readOptionalEvidence(worker.OutputPath)
			sourceStatus, statusErr := testEvidenceStatus(encoded, definitions[record.ID].schema)
			if err := validateSessionWorkerEvidence(&journal, record.ID, encoded, readErr, sourceStatus, statusErr); err != nil {
				fmt.Fprintf(stderr, "HHPT_SESSION_UNSAFE phase=readiness detail=%s\n", err)
				return 1
			}
		}
		if reason != "" {
			journal.StopReason = reason
			break
		}
		if outcome == "fail" || outcome == "aborted" || (record.ID == "cpu.quick" || record.ID == "memory.quick") && outcome != "pass" {
			journal.StopReason = "worker_stop"
			break
		}
	}
	if monitor.Failed() {
		return 1
	}
	if journal.StopReason == "completed" {
		if reason := sessionStopReason(ctx, runContext, nil, dependencies.Now(), deadline, time.Time{}); reason != "" {
			journal.StopReason = reason
		}
	}
	for _, record := range journal.Workers {
		reportWorker(record)
	}
	if err := dependencies.FinishInput(); err != nil {
		fmt.Fprintf(stderr, "finish session input: %v\n", err)
		return 1
	}
	// No worker or input reader remains active. Final observations and report
	// creation have independent bounded contexts so a clean stop stays reportable.
	if !observe(context.Background(), "final") {
		return 1
	}
	if code := finishMonitor(); code != 0 {
		return code
	}
	if err := writeSessionJournal(journalPath, journal); err != nil {
		fmt.Fprintf(stderr, "write session journal: %v\n", err)
		return 1
	}
	request := sessionFinalization{PlanPath: *planPath, EvidenceDirectory: *directory, FingerprintKeyPath: *keyPath,
		KeyEpoch: *keyEpoch, FixtureComponents: *fixture, OutputPath: *output, SafetyJournalPath: journalPath, StopReason: journal.StopReason}
	for _, record := range journal.Workers {
		request.Outcomes = append(request.Outcomes, record.ID+"="+record.Outcome)
	}
	fmt.Fprintf(stdout, "HHPT_SESSION_STOP reason=%s completed=%d scheduled=%d elapsed_ms=%d\n", journal.StopReason, completed, scheduled, journal.ElapsedMilliseconds)
	finalContext, finishFinal := context.WithTimeout(context.Background(), 30*time.Second)
	defer finishFinal()
	finalOutput := &sessionFinalOutput{destination: stdout}
	code := dependencies.Finalize(finalContext, request, finalOutput, stderr)
	if code == 0 {
		if finalContext.Err() != nil {
			fmt.Fprintln(stderr, "session finalization exceeded its deadline; report retained when available")
			return 1
		}
		if err := writeSessionReady(readyPath, *output, *keyEpoch, frozen, journal, finalOutput); err != nil {
			fmt.Fprintf(stderr, "write session readiness: %v; report retained when available\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "HHPT_SESSION_RESULT reason=%s elapsed_ms=%d journal=%s\n", journal.StopReason, journal.ElapsedMilliseconds, journalPath)
	}
	return code
}

func printSessionWorkerResult(output io.Writer, record sessionWorkerRecord) {
	labels := map[string]string{"cpu.quick": "CPU", "memory.quick": "MEMORY", "audio.readiness.quick": "AUDIO_READINESS",
		"input.readiness.quick": "INPUT_READINESS", "input.pointer.quick": "POINTER", "network.readiness.quick": "NETWORK_READINESS", "storage.readiness.quick": "STORAGE_READINESS",
		"usb.readiness.quick": "USB_READINESS", "graphics.quick": "GRAPHICS", "display.operator.quick": "DISPLAY"}
	fmt.Fprintf(output, "HHPT_%s status=%s\n", labels[record.ID], record.Outcome)
}

func sessionStopReason(parent, run, worker context.Context, now, runDeadline, workerDeadline time.Time) string {
	if parent.Err() != nil {
		if errors.Is(parent.Err(), context.DeadlineExceeded) {
			return "run_deadline"
		}
		return "operator_cancelled"
	}
	if run.Err() != nil || !now.Before(runDeadline) {
		return "run_deadline"
	}
	if worker != nil && (worker.Err() != nil || !now.Before(workerDeadline)) {
		return "test_deadline"
	}
	return ""
}

func sessionWorkerOutcome(path, schema string, code int, interrupted bool) (string, string, error) {
	encoded, readErr := readOptionalEvidence(path)
	status, statusErr := testEvidenceStatus(encoded, schema)
	if errors.Is(statusErr, errMemoryFailureDowngrade) || errors.Is(statusErr, errGraphicsFailureDowngrade) || errors.Is(statusErr, errDisplayFailureDowngrade) {
		return "", "", statusErr
	}
	if readErr == nil && statusErr == nil {
		if status == "fail" {
			return "fail", "completed", nil
		}
		if code == 0 {
			if interrupted && (status == "aborted" || status == "error") {
				return status, "interrupted", nil
			}
			return status, "completed", nil
		}
	}
	if interrupted {
		return "aborted", "interrupted", nil
	}
	return "error", "completed", nil
}
