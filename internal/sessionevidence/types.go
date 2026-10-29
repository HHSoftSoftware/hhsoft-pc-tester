// Package sessionevidence validates signed quick-session claims without I/O.
package sessionevidence

import (
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
)

const SchemaV4 = "hhsoft.pc-tester.quick-session/v4"
const SchemaV5 = "hhsoft.pc-tester.quick-session/v5"
const SchemaV6 = "hhsoft.pc-tester.quick-session/v6"
const SchemaV7 = "hhsoft.pc-tester.quick-session/v7"
const SchemaV8 = "hhsoft.pc-tester.quick-session/v8"

// Schema activates the bounded operator inspection; storage execution remains inactive.
const Schema = SchemaV7
const IntegritySchema = "hhsoft.pc-tester.run-integrity/v1"
const MaximumBytes = 64 << 10
const Scope = "Quick-session execution, cleanup, final inventory and final safety gate; report encoding, output and saving are outside this monitoring window."
const StandardScope = "Standard-session execution, cleanup, final inventory and final safety gate; report encoding, output and saving are outside this monitoring window."

func IsStrictSchema(schema string) bool {
	return schema == SchemaV4 || schema == SchemaV5 || schema == SchemaV6 || schema == SchemaV7 || schema == SchemaV8
}

func supportsCurrentWorkerEvidence(schema string) bool {
	return schema == SchemaV5 || schema == SchemaV6 || schema == SchemaV7 || schema == SchemaV8
}

func WorkerIDs() [12]string {
	return [12]string{"cpu.quick", "memory.quick", "audio.readiness.quick", "input.readiness.quick", "input.keyboard.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick", "graphics.quick", "display.operator.quick", "inspection.exterior.quick"}
}

func StandardWorkerIDs() [13]string {
	return [13]string{"cpu.quick", "cpu.stress", "memory.quick", "audio.readiness.quick", "input.readiness.quick", "input.keyboard.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick", "graphics.quick", "display.operator.quick", "inspection.exterior.quick"}
}

func workerIDs(schema string) []string {
	switch schema {
	case SchemaV4:
		return []string{"cpu.quick", "memory.quick", "audio.readiness.quick", "input.readiness.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick", "graphics.quick", "display.operator.quick"}
	case SchemaV5:
		return []string{"cpu.quick", "memory.quick", "audio.readiness.quick", "input.readiness.quick", "input.keyboard.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick", "graphics.quick", "display.operator.quick"}
	case SchemaV7:
		ids := WorkerIDs()
		return ids[:]
	case SchemaV8:
		ids := StandardWorkerIDs()
		return ids[:]
	case SchemaV6:
		return []string{"cpu.quick", "memory.quick", "audio.readiness.quick", "input.readiness.quick", "input.keyboard.quick", "input.pointer.quick", "network.readiness.quick", "storage.readiness.quick", "storage.quick", "usb.readiness.quick", "graphics.quick", "display.operator.quick"}
	default:
		return nil
	}
}

type Journal struct {
	Schema                         string                 `json:"schema"`
	PlanID                         string                 `json:"plan_id"`
	StopReason                     string                 `json:"stop_reason"`
	BudgetMilliseconds             int64                  `json:"budget_milliseconds"`
	ElapsedMilliseconds            int64                  `json:"elapsed_milliseconds"`
	Workers                        []WorkerRecord         `json:"workers"`
	Safety                         []SafetyRecord         `json:"safety"`
	Monitor                        safemode.MonitorResult `json:"monitor"`
	PreparationStartedMilliseconds int64                  `json:"preparation_started_ms"`
	InventoryPreparedMilliseconds  int64                  `json:"inventory_prepared_ms"`
	StopRequestedMilliseconds      int64                  `json:"stop_requested_ms"`
	SafetyJoinedMilliseconds       int64                  `json:"safety_joined_ms"`
	Thermal                        thermal.Result         `json:"thermal"`
	LocalThermalSources            []LocalThermalSource   `json:"local_thermal_sources"`
	// The versioned encoder emits this only in v6, including its required null.
	StorageSource *StorageSource `json:"-"`
	// These members exist only in the separately versioned standard journal.
	Profile         string        `json:"-"`
	CPUStressSource *StressSource `json:"-"`
}

type StressSource struct {
	WorkerID            string `json:"worker_id"`
	EvidenceSchema      string `json:"evidence_schema"`
	EvidenceFileSHA256  string `json:"evidence_file_sha256"`
	EvidenceTypedSHA256 string `json:"evidence_typed_sha256"`
}

type StorageSource struct {
	WorkerID            string `json:"worker_id"`
	EvidenceSchema      string `json:"evidence_schema"`
	EvidenceFileSHA256  string `json:"evidence_file_sha256"`
	EvidenceTypedSHA256 string `json:"evidence_typed_sha256"`
}
type WorkerRecord struct {
	ID      string        `json:"id"`
	State   string        `json:"state"`
	Outcome string        `json:"outcome"`
	Timing  *WorkerTiming `json:"timing,omitempty"`
}
type WorkerTiming struct {
	StartedMilliseconds  int64 `json:"started_ms"`
	FinishedMilliseconds int64 `json:"finished_ms"`
}
type SafetyRecord struct {
	Phase  string          `json:"phase"`
	Result safemode.Result `json:"result"`
}
type LocalThermalSource struct {
	WorkerID             string `json:"worker_id"`
	EvidenceSchema       string `json:"evidence_schema"`
	EvidenceFileSHA256   string `json:"evidence_file_sha256"`
	EvidenceTypedSHA256  string `json:"evidence_typed_sha256"`
	PeakMilliCelsius     int    `json:"peak_millicelsius"`
	LimitMilliCelsius    int    `json:"limit_millicelsius"`
	StartedMilliseconds  int64  `json:"started_ms"`
	FinishedMilliseconds int64  `json:"finished_ms"`
}
type ResultBinding struct {
	ID, PlannedStatus, ReportStatus string
	Version                         int
	Evidence                        []byte
}
type ThermalView struct {
	Scope, Finding, Coverage, StopReason string
	PeakObserved                         bool
	PeakMilliCelsius, LimitMilliCelsius  int
	AcceptedObservations                 uint64
	MaximumGapMilliseconds               int64
	CleanupConfirmed                     bool
	LocalSources                         []LocalThermalSource
}

// IntegrityEvidence preserves the graph-integrity v1 envelope. Strict v4-v7
// claims use its decoder; earlier evidence remains opaque.
type IntegrityEvidence struct {
	Schema                string                      `json:"schema"`
	Status                string                      `json:"status"`
	PlanID                string                      `json:"plan_id"`
	PlannedHardwareGraph  string                      `json:"planned_hardware_graph"`
	FinalHardwareGraph    string                      `json:"final_hardware_graph"`
	PlannedComponentCount int                         `json:"planned_component_count"`
	FinalComponentCount   int                         `json:"final_component_count"`
	AddedComponents       []plan.HardwareComponentRef `json:"added_components,omitempty"`
	RemovedComponents     []plan.HardwareComponentRef `json:"removed_components,omitempty"`
	ChangedComponents     []plan.HardwareComponentRef `json:"changed_components,omitempty"`
	ExcludedTests         []string                    `json:"excluded_tests,omitempty"`
	Session               *Journal                    `json:"session,omitempty"`
}
