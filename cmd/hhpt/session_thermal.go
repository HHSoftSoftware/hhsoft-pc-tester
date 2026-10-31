package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
)

type sessionFinalGate func(context.Context) (sessionevidence.Journal, error)
type sessionFinalizationResult struct {
	Code      int
	Journal   *sessionevidence.Journal
	Integrity sessionIntegrity
}
type sessionIntegrity struct {
	Status, PlanID                                              string
	PlannedComponents, FinalComponents, Added, Removed, Changed uint64
}
type sessionThermalMonitor interface {
	Ready(context.Context) error
	ObserveFinal(context.Context) error
	Finding() <-chan struct{}
	Failed() bool
	Stop(context.Context) thermal.Result
}

// Both owners receive the same deadline. Waiting for both synchronous Stop
// calls prevents a finalizer from outliving the owners it claims to have joined.
func joinSessionMonitors(ctx context.Context, safety sessionSafetyMonitor, heat sessionThermalMonitor) (safemode.StopReceipt, thermal.Result) {
	safetyDone := make(chan safemode.StopReceipt, 1)
	go func() { safetyDone <- safety.StopWithJoin(ctx) }()
	if heat == nil {
		return <-safetyDone, thermal.Result{}
	}
	heatDone := make(chan thermal.Result, 1)
	go func() { heatDone <- heat.Stop(ctx) }()
	return <-safetyDone, <-heatDone
}

// A terminal observation failure cancels preparation, not its cleanup reserve.
// Retain the preparation deadline so normal finalization stays inside 30 seconds.
func sessionCleanupContext(preparation context.Context) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(thermal.StopTimeout)
	if outer, ok := preparation.Deadline(); ok && outer.Before(deadline) {
		deadline = outer
	}
	return context.WithDeadline(context.Background(), deadline)
}

func sourceFileDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func sameSessionJournal(a, b sessionJournal) bool { return reflect.DeepEqual(a, b) }

func retainLocalThermalSource(journal *sessionJournal, worker sessionWorkerRecord, raw []byte, readErr error) error {
	if worker.ID == "cpu.stress" {
		if !sessionevidence.IsStandardSchema(journal.Schema) || journal.Profile != "standard" || journal.CPUStressSource != nil ||
			readErr != nil || worker.Timing == nil {
			return errors.New("CPU stress source cannot be retained")
		}
		value, digest, err := quickevidence.DecodeCPUStress(raw)
		if err != nil {
			return err
		}
		if worker.Timing.StartedMilliseconds < 0 || worker.Timing.FinishedMilliseconds < worker.Timing.StartedMilliseconds ||
			value.FinishedMilliseconds > worker.Timing.FinishedMilliseconds-worker.Timing.StartedMilliseconds {
			return errors.New("CPU stress elapsed time exceeds its worker interval")
		}
		journal.CPUStressSource = &sessionevidence.StressSource{
			WorkerID: worker.ID, EvidenceSchema: quick.CPUStressSchema,
			EvidenceFileSHA256: sourceFileDigest(raw), EvidenceTypedSHA256: digest,
		}
		if value.Safety.ThermalSamples > 0 && value.Safety.PeakMilliCelsius >= thermal.LimitMilliCelsius {
			journal.LocalThermalSources = append(journal.LocalThermalSources, sessionevidence.LocalThermalSource{
				WorkerID: worker.ID, EvidenceSchema: value.Schema,
				EvidenceFileSHA256: sourceFileDigest(raw), EvidenceTypedSHA256: digest,
				PeakMilliCelsius: value.Safety.PeakMilliCelsius, LimitMilliCelsius: value.Safety.ThermalLimitMilliCelsius,
				StartedMilliseconds: worker.Timing.StartedMilliseconds, FinishedMilliseconds: worker.Timing.FinishedMilliseconds,
			})
		}
		return nil
	}
	if worker.ID != "cpu.quick" && worker.ID != "memory.quick" {
		return nil
	}
	if readErr != nil {
		if readErr == os.ErrNotExist {
			return nil
		}
		return errInvalidQuickSafety
	}
	local, err := quickevidence.DecodeLocal(worker.ID, raw)
	if err != nil {
		return err
	}
	if local.Safety.ThermalObservations > 0 && local.Safety.ThermalLimitMilliCelsius != thermal.LimitMilliCelsius {
		return errors.New("local thermal limit differs from session policy")
	}
	if local.Safety.ThermalSamples == 0 || local.Safety.PeakMilliCelsius < thermal.LimitMilliCelsius {
		return nil
	}
	if worker.Timing == nil {
		return errors.New("local thermal evidence lacks execution timing")
	}
	journal.LocalThermalSources = append(journal.LocalThermalSources, sessionevidence.LocalThermalSource{
		WorkerID: worker.ID, EvidenceSchema: local.Schema, EvidenceFileSHA256: sourceFileDigest(raw), EvidenceTypedSHA256: local.TypedSHA256,
		PeakMilliCelsius: local.Safety.PeakMilliCelsius, LimitMilliCelsius: local.Safety.ThermalLimitMilliCelsius,
		StartedMilliseconds: worker.Timing.StartedMilliseconds, FinishedMilliseconds: worker.Timing.FinishedMilliseconds,
	})
	return nil
}
