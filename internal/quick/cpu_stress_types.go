package quick

import "errors"

const (
	CPUStressSchema                         = "hhsoft.pc-tester.cpu-stress/v1"
	CPUStressThermalPolicy                  = "available-observations/v1"
	CPUStressLoadMilliseconds         int64 = 600000
	CPUStressWatchdogMilliseconds     int64 = 660000
	CPUStressThermalLimitMilliCelsius       = 90000
)

var (
	ErrCPUStressOptions            = errors.New("invalid CPU stress options or context")
	ErrCPUStressAccounting         = errors.New("CPU stress accounting could not be validated")
	ErrCPUStressCleanupUnconfirmed = errors.New("CPU stress cleanup was not confirmed")
)

// CPUStressOptions selects bounded arithmetic workers and the thermal observation root.
type CPUStressOptions struct {
	Workers int
	Root    string
}

type CPUStressResult struct {
	Schema                        string                 `json:"schema"`
	Status                        Status                 `json:"status"`
	StopReason                    string                 `json:"stop_reason"`
	RequestedLoadMilliseconds     int64                  `json:"requested_load_ms"`
	LocalWatchdogMilliseconds     int64                  `json:"local_watchdog_ms"`
	ThermalPolicy                 string                 `json:"thermal_policy"`
	WorkerSelection               string                 `json:"worker_selection"`
	WorkersRequested              int                    `json:"workers_requested"`
	WorkersLaunched               int                    `json:"workers_launched"`
	WorkersReady                  int                    `json:"workers_ready"`
	EffectiveDeadlineMilliseconds int64                  `json:"effective_deadline_ms"`
	EffectiveDeadlineSource       string                 `json:"effective_deadline_source"`
	LoadStarted                   bool                   `json:"load_started"`
	LoadStartedMilliseconds       *int64                 `json:"load_started_ms"`
	LoadStoppedMilliseconds       *int64                 `json:"load_stopped_ms"`
	ObservedLoadMilliseconds      int64                  `json:"observed_load_ms"`
	WorkersJoinedMilliseconds     int64                  `json:"workers_joined_ms"`
	FinishedMilliseconds          int64                  `json:"finished_ms"`
	Iterations                    uint64                 `json:"iterations"`
	ChecksPassed                  uint64                 `json:"checks_passed"`
	StateAggregate                uint64                 `json:"state_aggregate"`
	KnownAnswerSHA256             string                 `json:"known_answer_sha256"`
	ScalarSuite                   string                 `json:"scalar_suite"`
	ScalarVectorCount             int                    `json:"scalar_vector_count"`
	CalculationFailures           CPUCalculationFailures `json:"calculation_failures"`
	AccountingValid               bool                   `json:"accounting_valid"`
	StopFacts                     CPUStressStopFacts     `json:"stop_facts"`
	Safety                        SafetyEvidence         `json:"safety"`
	Workers                       []CPUStressWorker      `json:"workers"`
}

type CPUStressWorker struct {
	Index                   int                    `json:"index"`
	StartedMilliseconds     int64                  `json:"started_ms"`
	ReadyMilliseconds       *int64                 `json:"ready_ms"`
	LoadEnteredMilliseconds *int64                 `json:"load_entered_ms"`
	StoppedMilliseconds     int64                  `json:"stopped_ms"`
	ExitReason              string                 `json:"exit_reason"`
	PreparationIterations   uint64                 `json:"preparation_iterations"`
	PreparationChecksPassed uint64                 `json:"preparation_checks_passed"`
	Iterations              uint64                 `json:"iterations"`
	ChecksPassed            uint64                 `json:"checks_passed"`
	LoadIterations          uint64                 `json:"load_iterations"`
	LoadChecksPassed        uint64                 `json:"load_checks_passed"`
	CalculationFailures     CPUCalculationFailures `json:"calculation_failures"`
	FailurePhase            string                 `json:"failure_phase"`
	Mismatch                *CPUScalarMismatch     `json:"mismatch"`
}

// CPUStressStopFacts retains independent causes even when another cause is primary.
type CPUStressStopFacts struct {
	CleanupUnconfirmed         bool `json:"cleanup_unconfirmed"`
	ClockInvalid               bool `json:"clock_invalid"`
	CounterOverflow            bool `json:"counter_overflow"`
	CoverageLost               bool `json:"coverage_lost"`
	EarlyWorkerExit            bool `json:"early_worker_exit"`
	IncompleteLoadChecks       bool `json:"incomplete_load_checks"`
	InitialCoveragePartial     bool `json:"initial_coverage_partial"`
	InitialCoverageUnavailable bool `json:"initial_coverage_unavailable"`
	InsufficientBudget         bool `json:"insufficient_budget"`
	InvalidObservation         bool `json:"invalid_observation"`
	LaunchIncomplete           bool `json:"launch_incomplete"`
	NormalLoadComplete         bool `json:"normal_load_complete"`
	ObservationTimeout         bool `json:"observation_timeout"`
	ObserverPanic              bool `json:"observer_panic"`
	ParentCancelled            bool `json:"parent_cancelled"`
	ParentDeadline             bool `json:"parent_deadline"`
	ReadinessIncomplete        bool `json:"readiness_incomplete"`
	ThermalLimit               bool `json:"thermal_limit"`
	WatchdogDeadline           bool `json:"watchdog_deadline"`
	WorkerPanic                bool `json:"worker_panic"`
}
