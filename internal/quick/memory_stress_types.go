package quick

import "errors"

const (
	MemoryStressSchema                          = "hhsoft.pc-tester.memory-stress/v1"
	MemoryStressThermalPolicy                   = "available-observations/v1"
	MemoryStressLoadMilliseconds         int64  = 1800000
	MemoryStressWatchdogMilliseconds     int64  = 1860000
	MemoryStressThermalLimitMilliCelsius        = 90000
	MemoryStressMinimumBytes             uint64 = 4 << 20
	MemoryStressMaximumBytes             uint64 = 64 << 20
)

var (
	ErrMemoryStressOptions            = errors.New("invalid memory stress options or context")
	ErrMemoryStressAccounting         = errors.New("memory stress accounting could not be validated")
	ErrMemoryStressCleanupUnconfirmed = errors.New("memory stress cleanup was not confirmed")
)

type MemoryStressOptions struct {
	Bytes int64
	Root  string
}

type MemoryStressAvailability struct {
	Started        bool   `json:"started"`
	Opened         bool   `json:"opened"`
	RawBytes       uint64 `json:"raw_bytes"`
	ReadCalls      uint64 `json:"read_calls"`
	EOF            bool   `json:"eof"`
	Valid          bool   `json:"valid"`
	CloseConfirmed bool   `json:"close_confirmed"`
	Failure        string `json:"failure"`
}

type MemoryStressCoverage struct {
	CompletedPhases         uint64 `json:"completed_phases"`
	CompletedCycles         uint64 `json:"completed_cycles"`
	LoadCycles              uint64 `json:"load_cycles"`
	PhaseBytes              uint64 `json:"phase_bytes"`
	ReadBytes               uint64 `json:"read_bytes"`
	WrittenBytes            uint64 `json:"written_bytes"`
	UncreditedBytes         uint64 `json:"uncredited_bytes"`
	TerminalReadBytes       uint64 `json:"terminal_read_bytes"`
	TerminalUncreditedBytes uint64 `json:"terminal_uncredited_bytes"`
}

type MemoryStressMismatch struct {
	Stage        string `json:"stage"`
	Cycle        uint64 `json:"cycle"`
	Phase        int    `json:"phase"`
	Offset       uint64 `json:"offset"`
	ExpectedByte int    `json:"expected_byte"`
	ActualByte   int    `json:"actual_byte"`
}

type MemoryStressStopFacts struct {
	CleanupUnconfirmed         bool `json:"cleanup_unconfirmed"`
	ClockInvalid               bool `json:"clock_invalid"`
	CounterOverflow            bool `json:"counter_overflow"`
	WorkerPanic                bool `json:"worker_panic"`
	ParentCancelled            bool `json:"parent_cancelled"`
	ParentDeadline             bool `json:"parent_deadline"`
	WatchdogDeadline           bool `json:"watchdog_deadline"`
	InsufficientBudget         bool `json:"insufficient_budget"`
	NormalLoadComplete         bool `json:"normal_load_complete"`
	InitialCoveragePartial     bool `json:"initial_coverage_partial"`
	InitialCoverageUnavailable bool `json:"initial_coverage_unavailable"`
	CoverageLost               bool `json:"coverage_lost"`
	ObservationTimeout         bool `json:"observation_timeout"`
	ObserverPanic              bool `json:"observer_panic"`
	InvalidObservation         bool `json:"invalid_observation"`
	ThermalLimit               bool `json:"thermal_limit"`
}

type MemoryStressResult struct {
	Schema                           string                   `json:"schema"`
	Status                           Status                   `json:"status"`
	StopReason                       string                   `json:"stop_reason"`
	Suite                            string                   `json:"suite"`
	RequestedLoadMilliseconds        int64                    `json:"requested_load_ms"`
	LocalWatchdogMilliseconds        int64                    `json:"local_watchdog_ms"`
	ThermalPolicy                    string                   `json:"thermal_policy"`
	RequestedBytes                   uint64                   `json:"requested_bytes"`
	AvailableBytes                   uint64                   `json:"available_bytes"`
	AllocatedBytes                   uint64                   `json:"allocated_bytes"`
	EffectiveDeadlineMilliseconds    int64                    `json:"effective_deadline_ms"`
	EffectiveDeadlineSource          string                   `json:"effective_deadline_source"`
	AvailabilityFinishedMilliseconds *int64                   `json:"availability_finished_ms"`
	AllocationFinishedMilliseconds   *int64                   `json:"allocation_finished_ms"`
	LoadStartedMilliseconds          *int64                   `json:"load_started_ms"`
	LoadStoppedMilliseconds          *int64                   `json:"load_stopped_ms"`
	ObservedLoadMilliseconds         int64                    `json:"observed_load_ms"`
	TerminalStartedMilliseconds      *int64                   `json:"terminal_started_ms"`
	TerminalStoppedMilliseconds      *int64                   `json:"terminal_stopped_ms"`
	FinishedMilliseconds             int64                    `json:"finished_ms"`
	AccountingValid                  bool                     `json:"accounting_valid"`
	Availability                     MemoryStressAvailability `json:"availability"`
	Coverage                         MemoryStressCoverage     `json:"coverage"`
	Mismatch                         *MemoryStressMismatch    `json:"mismatch"`
	StopFacts                        MemoryStressStopFacts    `json:"stop_facts"`
	Safety                           SafetyEvidence           `json:"safety"`
}
