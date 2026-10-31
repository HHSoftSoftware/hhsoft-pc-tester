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

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/keyboardevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/pointerevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
)

type sessionWorker struct {
	ID, Architecture, OutputPath string
	Timeout                      time.Duration
}

type sessionFinalization struct {
	Profile                                                   plan.Profile
	Previous                                                  *verifiedPreviousReport
	PlanPath, EvidenceDirectory, FingerprintKeyPath, KeyEpoch string
	FixtureComponents, OutputPath                             string
	Gate                                                      sessionFinalGate
	Outcomes                                                  []string
}

type sessionDependencies struct {
	ReadRetestInput retestInputReader
	RunWorker       func(context.Context, sessionWorker, io.Reader, io.Writer, io.Writer) int
	ObserveSafety   func(context.Context) safemode.Result
	StartMonitor    func(time.Time, context.CancelFunc) (sessionSafetyMonitor, error)
	StartThermal    func(time.Time, context.CancelFunc, context.CancelFunc) (sessionThermalMonitor, error)
	Finalize        func(context.Context, sessionFinalization, io.Writer, io.Writer) sessionFinalizationResult
	FinishInput     func() error
	Now             func() time.Time
}

type sessionSafetyMonitor interface {
	Failed() bool
	StopWithJoin(context.Context) safemode.StopReceipt
}

func runSession(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	return runSessionWithInput(ctx, arguments, input, stdout, stderr, prepareSessionInput)
}

func runSessionWithInput(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer,
	prepare func(context.Context, io.Reader) (context.Context, io.Reader, func() error, error)) (exitCode int) {
	ctx, input, finish, err := prepare(ctx, input)
	if err != nil {
		fmt.Fprintf(stderr, "prepare session input: %v\n", err)
		if errors.Is(err, errSessionInputCleanup) {
			return workerCleanupUncertainExitCode
		}
		return 1
	}
	// This boundary also owns input on returns before the coordinator starts its
	// monitors. The preparer retains one finish receipt for both boundaries.
	defer func() {
		if err := finish(); err != nil {
			fmt.Fprintf(stderr, "finish session input: %v\n", err)
			if errors.Is(err, errSessionInputCleanup) {
				exitCode = workerCleanupUncertainExitCode
			} else if exitCode == 0 {
				exitCode = 1
			}
		}
	}()
	return runSessionWith(ctx, arguments, input, stdout, stderr, sessionDependencies{
		RunWorker: runSessionWorker, ObserveSafety: safemode.Observe,
		StartMonitor: func(started time.Time, cancel context.CancelFunc) (sessionSafetyMonitor, error) {
			return safemode.StartMonitor(safemode.Observe, safemode.MonitorOptions{Started: started, Cancel: cancel})
		},
		StartThermal: func(started time.Time, cancel, fail context.CancelFunc) (sessionThermalMonitor, error) {
			return thermal.Start(thermal.Options{Started: started, Root: "/", CancelWork: cancel, Fail: fail})
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
	flags.SetOutput(io.Discard)
	planPath := flags.String("plan", "", "frozen test plan matching the selected profile")
	keyPath := flags.String("fingerprint-key", "", "session fingerprint key")
	keyEpoch := flags.String("key-epoch", "ephemeral-session", "fingerprint-key epoch")
	fixture := flags.String("fixture-components", "", "selected deployment fixture component IDs")
	directory := flags.String("evidence-dir", "/run/hhpt", "direct directory for fresh worker evidence")
	output := flags.String("output", "", "new signed report file")
	profile := flags.String("profile", "quick", "quick or standard session")
	budget := flags.Duration("timeout", 5*time.Minute, "immutable cooperative session budget")
	var parentOptions previousReportFlags
	parentOptions.register(flags)
	if flags.Parse(arguments) != nil {
		fmt.Fprintln(stderr, "session: invalid arguments")
		return 2
	}
	parentOptions.recordPresence(flags)
	executionProfile := plan.Profile(*profile)
	if executionProfile != plan.ProfileQuick && executionProfile != plan.ProfileStandard {
		fmt.Fprintln(stderr, "session: unsupported execution profile")
		return 2
	}
	timeoutSet := false
	flags.Visit(func(value *flag.Flag) { timeoutSet = timeoutSet || value.Name == "timeout" })
	if executionProfile == plan.ProfileStandard && !timeoutSet {
		*budget = 15 * time.Minute
	}
	resultDefinitions, definitionsErr := executionResultDefinitions(executionProfile)
	if definitionsErr != nil {
		fmt.Fprintln(stderr, "session: unsupported execution profile")
		return 2
	}
	workerIDs := sessionWorkerIDs[:]
	journalSchema := sessionJournalSchema
	if executionProfile == plan.ProfileStandard {
		ids := sessionevidence.StandardWorkerIDs()
		workerIDs = ids[:]
		journalSchema = sessionevidence.SchemaV10
	}
	if !parentOptions.valid() {
		fmt.Fprintln(stderr, "session: invalid predecessor options")
		return 2
	}
	if flags.NArg() != 0 || *planPath == "" || *keyPath == "" || *output == "" || *output == "-" || *budget < 10*time.Millisecond || *budget > 15*time.Minute {
		fmt.Fprintln(stderr, "session requires -plan, -fingerprint-key, -output, and a 10ms-15m timeout")
		return 2
	}
	if ctx == nil || dependencies.RunWorker == nil || dependencies.ObserveSafety == nil || dependencies.StartMonitor == nil || dependencies.StartThermal == nil || dependencies.Finalize == nil || dependencies.FinishInput == nil || dependencies.Now == nil {
		fmt.Fprintln(stderr, "session dependencies are incomplete")
		return 1
	}
	readInput := dependencies.ReadRetestInput
	if readInput == nil {
		readInput = readRetestInput
	}
	previous, parentErr := parentOptions.load(ctx, readInput)
	if parentErr != nil {
		fmt.Fprintln(stderr, "session: predecessor input or verification failed")
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
	if err != nil || frozen.Profile != executionProfile || frozen.HardwareGraph.KeyEpoch != *keyEpoch {
		fmt.Fprintln(stderr, "session requires a matching explicit profile, valid plan and key epoch")
		return 1
	}
	layout, err := commandLayoutForPlan(frozen, executionProfile)
	if err != nil {
		fmt.Fprintln(stderr, "session requires a checked execution layout")
		return 1
	}
	resultDefinitions, workerIDs, journalSchema = layout.definitions, layout.workerIDs, layout.schema
	definitions := make(map[string]quickResultDefinition, len(resultDefinitions))
	for _, definition := range resultDefinitions {
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
	// Current plan validation already makes these CPU prerequisites inseparable.
	// Keep the execution boundary explicit if future catalogs permit selection.
	if executionProfile == plan.ProfileStandard && planned["cpu.stress"].Status == plan.TestScheduled &&
		planned["cpu.quick"].Status != plan.TestScheduled {
		fmt.Fprintln(stderr, "scheduled CPU stress requires scheduled CPU quick checks")
		return 1
	}
	journalPath := filepath.Join(*directory, "session-journal.json")
	monitorPath := filepath.Join(*directory, "session-safety-monitor.json")
	thermalPath := filepath.Join(*directory, "session-thermal-monitor.json")
	readyPath := filepath.Join(*directory, "session-ready.txt")
	freshPaths := []string{journalPath, monitorPath, thermalPath, readyPath, filepath.Join(*directory, "session-safety-pre-sign.json"), *output, filepath.Join(*directory, "session-safety-preflight.json"), filepath.Join(*directory, "session-safety-final.json")}
	journal := sessionJournal{Schema: journalSchema, PlanID: frozen.ID,
		StopReason: "completed", BudgetMilliseconds: budget.Milliseconds(), LocalThermalSources: []sessionevidence.LocalThermalSource{}}
	if executionProfile == plan.ProfileStandard {
		journal.Profile = "standard"
	}
	scheduled := 0
	for _, id := range workerIDs {
		test, exists := planned[id]
		if !exists {
			fmt.Fprintln(stderr, "session plan omits a required worker")
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
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "session cancelled before initial observations")
		return 1
	}
	started := dependencies.Now()
	deadline := started.Add(*budget)
	runContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	integrityContext, cancelIntegrity := context.WithCancel(context.Background())
	defer cancelIntegrity()
	fail := func() { cancel(); cancelIntegrity() }
	monitor, err := dependencies.StartMonitor(started, fail)
	if err != nil || monitor == nil {
		fmt.Fprintln(stderr, "start session safety monitor failed")
		return 1
	}
	var thermalMonitor sessionThermalMonitor
	inputFinished := false
	finishInput := func() error {
		if inputFinished {
			return nil
		}
		inputFinished = true
		return dependencies.FinishInput()
	}
	stopped, monitorCode := false, 0
	finishMonitors := func(stopContext context.Context) int {
		if stopped {
			return monitorCode
		}
		stopped = true
		journal.StopRequestedMilliseconds = dependencies.Now().Sub(started).Milliseconds()
		safetyReceipt, thermalReceipt := joinSessionMonitors(stopContext, monitor, thermalMonitor)
		journal.Monitor = safetyReceipt.Result
		journal.SafetyJoinedMilliseconds = safetyReceipt.JoinedMilliseconds
		journal.Thermal = thermalReceipt
		journal.ElapsedMilliseconds = dependencies.Now().Sub(started).Milliseconds()
		if !safetyReceipt.Joined || !safetyReceipt.Result.CleanupConfirmed || thermalMonitor != nil && (!thermalReceipt.Joined || !thermalReceipt.CleanupConfirmed) {
			fmt.Fprintln(stderr, "HHPT_SESSION_FAILED reason=monitor_cleanup_unconfirmed")
			monitorCode = workerCleanupUncertainExitCode
		} else if validateSessionMonitor(journal.Monitor, journal.Monitor.ElapsedMilliseconds) != nil || monitor.Failed() || thermalMonitor == nil || thermalMonitor.Failed() || thermal.ValidateResult(thermalReceipt, true) != nil {
			monitorCode = 1
		}
		if writeQuickResult(journal.Monitor, monitorPath, stdout, stderr, "session safety monitor") != 0 && monitorCode == 0 {
			monitorCode = 1
		}
		if thermalMonitor != nil && writeQuickResult(journal.Thermal, thermalPath, stdout, stderr, "session thermal monitor") != 0 && monitorCode == 0 {
			monitorCode = 1
		}
		return monitorCode
	}
	defer func() {
		// Even an unsuccessful worker must release console ownership while both
		// monitors are still alive. The production input owner makes this idempotent.
		if err := finishInput(); err != nil {
			fmt.Fprintf(stderr, "finish session input: %v\n", err)
			if errors.Is(err, errSessionInputCleanup) {
				exitCode = workerCleanupUncertainExitCode
			} else if exitCode == 0 {
				exitCode = 1
			}
		}
		if !stopped {
			bounded, stop := context.WithTimeout(context.Background(), thermal.StopTimeout)
			code := finishMonitors(bounded)
			stop()
			if code == workerCleanupUncertainExitCode || exitCode == 0 && code != 0 {
				exitCode = code
			}
		}
	}()
	thermalMonitor, err = dependencies.StartThermal(started, cancel, fail)
	if err != nil || thermalMonitor == nil {
		fmt.Fprintln(stderr, "start session thermal monitor failed")
		return 1
	}
	initialContext, finishInitial := context.WithTimeout(integrityContext, thermal.FinalTimeout)
	err = thermalMonitor.Ready(initialContext)
	initialContextErr := initialContext.Err()
	finishInitial()
	if err != nil || initialContextErr != nil || monitor.Failed() || thermalMonitor.Failed() {
		fmt.Fprintln(stderr, "initial thermal observation failed")
		return 1
	}
	heat := func() bool {
		if len(journal.LocalThermalSources) > 0 {
			return true
		}
		select {
		case <-thermalMonitor.Finding():
			return true
		default:
			return false
		}
	}
	stopReason := func(worker context.Context, workerDeadline time.Time) string {
		if heat() {
			return "thermal_limit"
		}
		return sessionStopReason(ctx, runContext, worker, dependencies.Now(), deadline, workerDeadline)
	}
	unsafe := func() bool { return monitor.Failed() || thermalMonitor.Failed() || integrityContext.Err() != nil }
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
	if !observe(integrityContext, "preflight") {
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
		if unsafe() {
			return 1
		}
		record := &journal.Workers[index]
		if record.State != "not_started" {
			reportWorker(*record)
			continue
		}
		if reason := stopReason(nil, time.Time{}); reason != "" {
			journal.StopReason = reason
			break
		}
		if !observe(integrityContext, "before-"+record.ID) {
			return 1
		}
		if unsafe() {
			return 1
		}
		// An input stop racing the safety observation must not start a worker.
		if reason := stopReason(nil, time.Time{}); reason != "" {
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
		workerFinished := dependencies.Now()
		reason := stopReason(workerContext, workerDeadline)
		stopWorker()
		if code == workerCleanupUncertainExitCode {
			fmt.Fprintf(stderr, "HHPT_SESSION_FAILED worker=%s reason=cleanup_unconfirmed\n", record.ID)
			return workerCleanupUncertainExitCode
		}
		if unsafe() {
			return 1
		}
		workerEvidence, workerReadErr := readOptionalEvidence(worker.OutputPath)
		outcome, state, evidenceErr := sessionWorkerOutcomeEvidence(workerEvidence, workerReadErr, definitions[record.ID].schema, code, reason != "")
		if evidenceErr != nil {
			fmt.Fprintf(stderr, "session worker evidence: %v\n", evidenceErr)
			if errors.Is(evidenceErr, errQuickCleanupUnconfirmed) || errors.Is(evidenceErr, keyboardevidence.ErrCleanupUnconfirmed) || errors.Is(evidenceErr, quick.ErrCPUStressCleanupUnconfirmed) || touchpadCleanupError(evidenceErr) {
				fmt.Fprintf(stderr, "HHPT_SESSION_FAILED worker=%s reason=cleanup_unconfirmed\n", record.ID)
				return workerCleanupUncertainExitCode
			}
			return 1
		}
		record.State, record.Outcome = state, outcome
		record.Timing = &sessionevidence.WorkerTiming{StartedMilliseconds: workerStart.Sub(started).Milliseconds(), FinishedMilliseconds: workerFinished.Sub(started).Milliseconds()}
		if record.ID == touchpadExecutionID {
			if err := validateTouchpadWorkerRecord(*record, workerEvidence, workerReadErr, outcome, nil); err != nil {
				if touchpadCleanupError(err) {
					return workerCleanupUncertainExitCode
				}
				fmt.Fprintln(stderr, "touchpad worker timing or evidence binding failed")
				return 1
			}
		}
		if err := retainLocalThermalSource(&journal, *record, workerEvidence, workerReadErr); err != nil {
			fmt.Fprintln(stderr, "local thermal evidence binding failed")
			return 1
		}
		if heat() {
			cancel()
			reason = "thermal_limit"
		}
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
		if outcome == "fail" || outcome == "aborted" || (record.ID == "cpu.quick" || record.ID == "cpu.stress" || record.ID == "memory.quick") && outcome != "pass" {
			journal.StopReason = "worker_stop"
			break
		}
	}
	if unsafe() {
		return 1
	}
	if journal.StopReason == "completed" {
		if reason := stopReason(nil, time.Time{}); reason != "" {
			journal.StopReason = reason
		}
	}
	for _, record := range journal.Workers {
		reportWorker(record)
	}
	if err := finishInput(); err != nil {
		fmt.Fprintf(stderr, "finish session input: %v\n", err)
		if errors.Is(err, errSessionInputCleanup) {
			return workerCleanupUncertainExitCode
		}
		return 1
	}
	// No worker or input reader remains active. Final observations and report
	// creation have independent bounded contexts so a clean stop stays reportable.
	if !observe(context.Background(), "final") {
		return 1
	}
	journal.PreparationStartedMilliseconds = dependencies.Now().Sub(started).Milliseconds()
	gateCalled, gatePassed := false, false
	request := sessionFinalization{Previous: previous, PlanPath: *planPath, EvidenceDirectory: *directory, FingerprintKeyPath: *keyPath, KeyEpoch: *keyEpoch, FixtureComponents: *fixture, OutputPath: *output}
	if executionProfile == plan.ProfileStandard {
		request.Profile = plan.ProfileStandard
	}
	request.Gate = func(gateContext context.Context) (sessionJournal, error) {
		if gateCalled {
			gatePassed = false
			return sessionJournal{}, errors.New("session final gate already consumed")
		}
		gateCalled = true
		journal.InventoryPreparedMilliseconds = dependencies.Now().Sub(started).Milliseconds()
		if gateContext.Err() != nil || unsafe() || !observe(gateContext, "pre-sign") {
			return sessionJournal{}, errors.New("pre-sign safety failed")
		}
		finalObservation, stopFinal := context.WithTimeout(gateContext, thermal.FinalTimeout)
		err := thermalMonitor.ObserveFinal(finalObservation)
		observationErr := finalObservation.Err()
		stopFinal()
		if err != nil || observationErr != nil || unsafe() {
			return sessionJournal{}, errors.New("final thermal observation failed")
		}
		bounded, stop := sessionCleanupContext(gateContext)
		code := finishMonitors(bounded)
		stop()
		if code != 0 || gateContext.Err() != nil {
			return sessionJournal{}, errors.New("session monitors did not complete safely")
		}
		if sessionevidence.HasHeat(journal) {
			journal.StopReason = "thermal_limit"
		}
		if err := writeSessionJournal(journalPath, journal); err != nil {
			return sessionJournal{}, err
		}
		gatePassed = true
		return sessionevidence.CloneJournal(journal), nil
	}
	for _, record := range journal.Workers {
		request.Outcomes = append(request.Outcomes, record.ID+"="+record.Outcome)
	}
	finalContext, finishFinal := context.WithTimeout(integrityContext, 30*time.Second)
	defer finishFinal()
	finalOutput := &sessionFinalOutput{destination: stdout}
	finalResult := dependencies.Finalize(finalContext, request, finalOutput, stderr)
	if layout.touchpad && finalResult.Journal != nil {
		owned := sessionevidence.CloneJournal(*finalResult.Journal)
		finalResult.Journal = &owned
	}
	if monitorCode == workerCleanupUncertainExitCode {
		return monitorCode
	}
	if finalResult.Code == 0 {
		if finalContext.Err() != nil || !gatePassed || finalResult.Journal == nil || validateSessionJournalStructure(*finalResult.Journal) != nil || !sameSessionJournal(*finalResult.Journal, journal) {
			fmt.Fprintln(stderr, "session finalization lacks the completed live gate")
			return 1
		}
		journal = sessionevidence.CloneJournal(*finalResult.Journal)
		if err := writeSessionReady(readyPath, *output, *keyEpoch, frozen, journal, finalResult.Integrity, finalOutput); err != nil {
			fmt.Fprintf(stderr, "write session readiness: %v; report retained when available\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "HHPT_SESSION_STOP reason=%s completed=%d scheduled=%d elapsed_ms=%d\n", journal.StopReason, completed, scheduled, journal.ElapsedMilliseconds)
		fmt.Fprintf(stdout, "HHPT_SESSION_RESULT reason=%s elapsed_ms=%d journal=%s\n", journal.StopReason, journal.ElapsedMilliseconds, journalPath)
	}
	return finalResult.Code
}

func printSessionWorkerResult(output io.Writer, record sessionWorkerRecord) {
	labels := map[string]string{"cpu.quick": "CPU", "cpu.stress": "CPU_STRESS", "memory.quick": "MEMORY", "audio.readiness.quick": "AUDIO_READINESS",
		"input.readiness.quick": "INPUT_READINESS", "input.keyboard.quick": "KEYBOARD", "input.pointer.quick": "POINTER", touchpadExecutionID: "TOUCHPAD", "network.readiness.quick": "NETWORK_READINESS", "storage.readiness.quick": "STORAGE_READINESS",
		"usb.readiness.quick": "USB_READINESS", "graphics.quick": "GRAPHICS", "display.operator.quick": "DISPLAY", "inspection.exterior.quick": "EXTERIOR_INSPECTION"}
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

func sessionWorkerOutcomeEvidence(encoded []byte, readErr error, schema string, code int, interrupted bool) (string, string, error) {
	if schema == inputtest.TouchpadSchema {
		return touchpadWorkerOutcomeEvidence(encoded, readErr, code, interrupted)
	}
	if schema == quick.CPUStressSchema {
		if code == workerCleanupUncertainExitCode {
			return "", "", quick.ErrCPUStressCleanupUnconfirmed
		}
		if readErr != nil || code != 0 {
			return "", "", errors.New("dispatched CPU stress requires valid emitted evidence")
		}
		status, err := testEvidenceStatus(encoded, schema)
		if err != nil {
			return "", "", err
		}
		state := "completed"
		if interrupted && (status == "aborted" || status == "error") {
			state = "interrupted"
		}
		return status, state, nil
	}
	if schema == exteriorinspection.Schema {
		if readErr != nil {
			if readErr != os.ErrNotExist {
				return "", "", exteriorinspection.ErrEvidence
			}
			if interrupted {
				return "aborted", "interrupted", nil
			}
			return "error", "completed", nil
		}
		value, err := exteriorinspection.Decode(encoded)
		if err != nil {
			return "", "", err
		}
		wantCode := 0
		if value.Status == "error" {
			wantCode = 1
		}
		if code != wantCode {
			return "", "", exteriorinspection.ErrEvidence
		}
		if value.Status == "fail" {
			return "fail", "completed", nil
		}
		if interrupted && (value.Status == "aborted" || value.Status == "error") {
			return value.Status, "interrupted", nil
		}
		return value.Status, "completed", nil
	}
	if schema == inputtest.PointerSchema && readErr != nil && readErr != os.ErrNotExist {
		return "", "", pointerevidence.ErrInvalid
	}
	if schema == inputtest.KeyboardSchema && readErr != nil && readErr != os.ErrNotExist {
		return "", "", keyboardevidence.ErrInvalid
	}
	if (schema == quick.CPUSchema || schema == quick.MemorySchema) && readErr != nil && readErr != os.ErrNotExist {
		// Only direct absence permits an execution note. Existing quick
		// evidence must establish that the thermal observer was joined.
		return "", "", errInvalidQuickSafety
	}
	status, statusErr := testEvidenceStatus(encoded, schema)
	if readErr == nil && errors.Is(statusErr, pointerevidence.ErrInvalid) {
		return "", "", statusErr
	}
	if readErr == nil && (errors.Is(statusErr, keyboardevidence.ErrCleanupUnconfirmed) || errors.Is(statusErr, keyboardevidence.ErrInvalid)) {
		return "", "", statusErr
	}
	if readErr == nil && (errors.Is(statusErr, errQuickCleanupUnconfirmed) || errors.Is(statusErr, errInvalidQuickSafety)) {
		return "", "", statusErr
	}
	if errors.Is(statusErr, errMemoryFailureDowngrade) || errors.Is(statusErr, errGraphicsFailureDowngrade) || errors.Is(statusErr, errDisplayFailureDowngrade) {
		return "", "", statusErr
	}
	if readErr == nil && statusErr == nil {
		if status == "fail" {
			return "fail", "completed", nil
		}
		if code == 0 || schema == quick.CPUSchema || schema == quick.MemorySchema || schema == inputtest.KeyboardSchema || schema == inputtest.PointerSchema {
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
