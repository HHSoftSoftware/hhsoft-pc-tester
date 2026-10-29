package quickevidence_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
)

func stressLiteral(t *testing.T) quick.CPUStressResult {
	t.Helper()
	var v quick.CPUStressResult
	if json.Unmarshal([]byte(stressPassLiteral), &v) != nil {
		t.Fatal("literal fixture")
	}
	return v
}

func stressBytes(t *testing.T, v quick.CPUStressResult) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func requireStress(t *testing.T, v quick.CPUStressResult) {
	t.Helper()
	if _, _, err := quickevidence.DecodeCPUStress(stressBytes(t, v)); err != nil {
		t.Fatal("valid semantic prerequisite", err)
	}
}

func TestCPUStressLiteralWindowAndTypedDigest(t *testing.T) {
	raw := []byte(stressPassLiteral)
	got, digest, err := quickevidence.DecodeCPUStress(raw)
	if err != nil || digest != "sha256:db80ee5817ade0a530be8ac426698de09d14533404faaaaaaf2df4619cfdf3f5" ||
		got.Status != quick.StatusPass || got.Iterations != 256 || got.ChecksPassed != 24 || got.Workers[0].LoadIterations != 255 || got.Workers[0].LoadChecksPassed != 12 {
		t.Fatal("literal lifetime/load boundary or independent hash", err, digest)
	}
	*got.LoadStartedMilliseconds = 99
	*got.Workers[0].ReadyMilliseconds = 99
	got.Workers[0].ChecksPassed = 99
	again, againDigest, err := quickevidence.DecodeCPUStress(raw)
	if err != nil || againDigest != digest || *again.LoadStartedMilliseconds != 2 || *again.Workers[0].ReadyMilliseconds != 1 || again.ChecksPassed != 24 {
		t.Fatal("returned pointers alias later decoding")
	}
	sameMillisecond := stressLiteral(t)
	*sameMillisecond.Workers[0].ReadyMilliseconds = 2
	requireStress(t, sameMillisecond)
	// A positive real margin can disappear in serialization. It is a truthful
	// incomplete result, never permission to round up into a passing deadline.
	conservative := stressLiteral(t)
	conservative.FinishedMilliseconds = 660000 - 1
	conservative.EffectiveDeadlineSource = "parent"
	conservative.EffectiveDeadlineMilliseconds = conservative.FinishedMilliseconds
	conservative.Status, conservative.StopReason = quick.StatusInconclusive, "incomplete_work"
	requireStress(t, conservative)
}

func TestCPUStressRejectsOneRelationForgeries(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*quick.CPUStressResult)
	}{
		{"schema", func(v *quick.CPUStressResult) { v.Schema += "9" }},
		{"requested_duration", func(v *quick.CPUStressResult) { v.RequestedLoadMilliseconds-- }},
		{"watchdog", func(v *quick.CPUStressResult) { v.LocalWatchdogMilliseconds++ }},
		{"unknown_policy", func(v *quick.CPUStressResult) { v.ThermalPolicy = "cpu-zone-proof" }},
		{"unknown_selection", func(v *quick.CPUStressResult) { v.WorkerSelection = "all_cores" }},
		{"zero_requested", func(v *quick.CPUStressResult) { v.WorkersRequested = 0 }},
		{"too_many_requested", func(v *quick.CPUStressResult) { v.WorkersRequested = 65 }},
		{"launch_count", func(v *quick.CPUStressResult) { v.WorkersLaunched = 0 }},
		{"ready_count", func(v *quick.CPUStressResult) { v.WorkersReady = 0 }},
		{"index", func(v *quick.CPUStressResult) { v.Workers[0].Index = 1 }},
		{"preparation_replay", func(v *quick.CPUStressResult) { v.Workers[0].PreparationIterations = 2 }},
		{"load_count", func(v *quick.CPUStressResult) { v.Workers[0].LoadIterations++ }},
		{"load_checks", func(v *quick.CPUStressResult) { v.Workers[0].LoadChecksPassed++ }},
		{"sum_iterations", func(v *quick.CPUStressResult) { v.Iterations++ }},
		{"sum_checks", func(v *quick.CPUStressResult) { v.ChecksPassed++ }},
		{"known_answer", func(v *quick.CPUStressResult) { v.KnownAnswerSHA256 = strings.Repeat("0", 64) }},
		{"suite", func(v *quick.CPUStressResult) { v.ScalarSuite = "other" }},
		{"vector_count", func(v *quick.CPUStressResult) { v.ScalarVectorCount = 11 }},
		{"unreported_failure", func(v *quick.CPUStressResult) { v.CalculationFailures.Integer = true }},
		{"invented_phase", func(v *quick.CPUStressResult) { v.Workers[0].FailurePhase = "load" }},
		{"nullable_start", func(v *quick.CPUStressResult) { v.LoadStartedMilliseconds = nil }},
		{"ready_after_release", func(v *quick.CPUStressResult) { *v.Workers[0].ReadyMilliseconds = 3 }},
		{"entry_before_release", func(v *quick.CPUStressResult) { *v.Workers[0].LoadEnteredMilliseconds = 1 }},
		{"join_before_stop", func(v *quick.CPUStressResult) { v.WorkersJoinedMilliseconds = 600001 }},
		{"freeze_before_join", func(v *quick.CPUStressResult) { v.FinishedMilliseconds = 600002 }},
		{"round_up_short_window", func(v *quick.CPUStressResult) { v.ObservedLoadMilliseconds = 599999 }},
		{"invented_early_exit", func(v *quick.CPUStressResult) { v.StopFacts.EarlyWorkerExit = true }},
		{"invented_budget", func(v *quick.CPUStressResult) { v.StopFacts.InsufficientBudget = true }},
		{"deadline_at_finish", func(v *quick.CPUStressResult) {
			v.EffectiveDeadlineSource = "parent"
			v.EffectiveDeadlineMilliseconds = v.FinishedMilliseconds
		}},
		{"deadline_at_release_target", func(v *quick.CPUStressResult) {
			v.EffectiveDeadlineSource = "parent"
			v.EffectiveDeadlineMilliseconds = 600002
		}},
		{"accounting", func(v *quick.CPUStressResult) { v.AccountingValid = false }},
		{"counter_overflow", func(v *quick.CPUStressResult) { v.StopFacts.CounterOverflow = true }},
		{"clock_invalid", func(v *quick.CPUStressResult) { v.StopFacts.ClockInvalid = true }},
		{"worker_panic", func(v *quick.CPUStressResult) { v.StopFacts.WorkerPanic = true }},
		{"hidden_heat", func(v *quick.CPUStressResult) { v.Safety.PeakMilliCelsius = 90000 }},
		{"thermal_count", func(v *quick.CPUStressResult) { v.Safety.ThermalSamples = 2 }},
		{"running_monitor", func(v *quick.CPUStressResult) { v.Safety.MonitorStatus = "running" }},
		{"false_cleanup", func(v *quick.CPUStressResult) { v.Safety.CleanupConfirmed = false }},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := stressLiteral(t)
			requireStress(t, v)
			before := stressBytes(t, v)
			test.change(&v)
			after := stressBytes(t, v)
			if bytes.Equal(before, after) {
				t.Fatal("ineffective fault")
			}
			if _, _, err := quickevidence.DecodeCPUStress(after); err == nil {
				t.Fatal("forged relation accepted")
			}
		})
	}
}

func stressNoLoad(t *testing.T) quick.CPUStressResult {
	v := stressLiteral(t)
	v.Status, v.StopReason = quick.StatusInconclusive, "insufficient_budget"
	v.WorkersLaunched, v.WorkersReady = 0, 0
	v.EffectiveDeadlineSource, v.EffectiveDeadlineMilliseconds = "parent", 600000
	v.LoadStarted = false
	v.LoadStartedMilliseconds = nil
	v.LoadStoppedMilliseconds = nil
	v.ObservedLoadMilliseconds, v.WorkersJoinedMilliseconds, v.FinishedMilliseconds = 0, 0, 0
	v.Iterations, v.ChecksPassed, v.StateAggregate = 0, 0, 0
	v.Workers = []quick.CPUStressWorker{}
	v.StopFacts = quick.CPUStressStopFacts{InsufficientBudget: true, LaunchIncomplete: true, ReadinessIncomplete: true}
	v.Safety = quick.SafetyEvidence{GlobalTimeoutMilliseconds: 660000, ThermalLimitMilliCelsius: 90000, MonitorStatus: "not_started", CleanupConfirmed: true, ThermalCoverage: "unavailable"}
	return v
}

func TestCPUStressFiniteNonPassEvidenceAndRetainedCauses(t *testing.T) {
	noLoad := stressNoLoad(t)
	requireStress(t, noLoad)
	t.Run("budget_cannot_heal", func(t *testing.T) {
		v := noLoad
		v.EffectiveDeadlineMilliseconds = 600001
		if _, _, err := quickevidence.DecodeCPUStress(stressBytes(t, v)); err == nil {
			t.Fatal("invented failed margin")
		}
	})
	for _, initial := range []bool{true, false} {
		name := "later_partial"
		if initial {
			name = "initial_partial"
		}
		t.Run(name, func(t *testing.T) {
			v := stressNoLoad(t)
			v.EffectiveDeadlineSource, v.EffectiveDeadlineMilliseconds = "watchdog", 660000
			v.StopFacts.InsufficientBudget = false
			v.StopFacts.ObservationTimeout = true
			v.StopFacts.ThermalLimit = true
			v.Status, v.StopReason = quick.StatusError, "observer_failure"
			v.FinishedMilliseconds = 10
			v.Safety = quick.SafetyEvidence{GlobalTimeoutMilliseconds: 660000, ThermalLimitMilliCelsius: 90000, ThermalSensors: 1, ThermalSamples: 1, PeakMilliCelsius: 90000, MonitorStatus: "failed", MonitorFailure: "observation_timeout", CleanupConfirmed: true, ThermalObservations: 1, ThermalCoverage: "partial", ThermalRejectedReadings: 1}
			if initial {
				v.StopFacts.InitialCoveragePartial = true
			} else {
				v.StopFacts.CoverageLost = true
				v.Safety.ThermalObservations = 2
				v.Safety.ThermalSamples = 2
			}
			requireStress(t, v)
			if initial {
				v.StopFacts.InitialCoveragePartial = false
			} else {
				v.StopFacts.CoverageLost = false
			}
			if _, _, err := quickevidence.DecodeCPUStress(stressBytes(t, v)); err == nil {
				t.Fatal("hard first cause hid independent coverage loss")
			}
		})
	}
	t.Run("fractional_early_stop", func(t *testing.T) {
		v := stressLiteral(t)
		v.Status, v.StopReason = quick.StatusInconclusive, "incomplete_work"
		v.ObservedLoadMilliseconds = 599999
		v.StopFacts.NormalLoadComplete = false
		v.StopFacts.EarlyWorkerExit = true
		v.Workers[0].ExitReason = "incomplete_work"
		requireStress(t, v)
		v.StopFacts.EarlyWorkerExit = false
		if _, _, err := quickevidence.DecodeCPUStress(stressBytes(t, v)); err == nil {
			t.Fatal("fractional early stop lost its fact")
		}
	})
	t.Run("early_worker_cannot_claim_completed", func(t *testing.T) {
		v := stressLiteral(t)
		v.Status, v.StopReason = quick.StatusInconclusive, "incomplete_work"
		*v.LoadStoppedMilliseconds = 600001
		v.Workers[0].StoppedMilliseconds = 600001
		v.ObservedLoadMilliseconds = 599999
		v.StopFacts.NormalLoadComplete = false
		v.StopFacts.EarlyWorkerExit = true
		v.Workers[0].ExitReason = "incomplete_work"
		requireStress(t, v)
		v.Workers[0].ExitReason = "completed"
		if _, _, err := quickevidence.DecodeCPUStress(stressBytes(t, v)); err == nil {
			t.Fatal("early completed row")
		}
	})
	t.Run("released_without_entry", func(t *testing.T) {
		v := stressLiteral(t)
		v.Status, v.StopReason = quick.StatusInconclusive, "incomplete_work"
		v.StopFacts.EarlyWorkerExit = true
		v.StopFacts.IncompleteLoadChecks = true
		v.Iterations, v.ChecksPassed = 1, 12
		w := &v.Workers[0]
		w.LoadEnteredMilliseconds = nil
		w.Iterations, w.ChecksPassed, w.LoadIterations, w.LoadChecksPassed = 1, 12, 0, 0
		requireStress(t, v)
	})
	t.Run("partial_readiness", func(t *testing.T) {
		v := stressNoLoad(t)
		v.EffectiveDeadlineSource, v.EffectiveDeadlineMilliseconds = "watchdog", 660000
		v.Status, v.StopReason = quick.StatusAborted, "parent_cancelled"
		v.StopFacts.InsufficientBudget = false
		v.StopFacts.ParentCancelled = true
		v.StopFacts.LaunchIncomplete = false
		v.WorkersLaunched = 1
		v.ChecksPassed = 5
		v.WorkersJoinedMilliseconds, v.FinishedMilliseconds = 10, 11
		v.Safety = stressLiteral(t).Safety
		v.Workers = []quick.CPUStressWorker{{Index: 0, StoppedMilliseconds: 9, ExitReason: "cancelled", PreparationChecksPassed: 5, ChecksPassed: 5, FailurePhase: "none"}}
		requireStress(t, v)
	})
	t.Run("mismatch_outranks_heat", func(t *testing.T) {
		v := stressLiteral(t)
		v.Status, v.StopReason = quick.StatusFail, "calculation_mismatch"
		v.StopFacts.NormalLoadComplete = false
		v.StopFacts.EarlyWorkerExit = true
		v.StopFacts.IncompleteLoadChecks = true
		v.StopFacts.ThermalLimit = true
		v.ChecksPassed = 23
		*v.LoadStoppedMilliseconds = 1000
		v.ObservedLoadMilliseconds = 998
		v.WorkersJoinedMilliseconds, v.FinishedMilliseconds = 1001, 1002
		w := &v.Workers[0]
		w.StoppedMilliseconds = 1000
		w.ChecksPassed, w.LoadChecksPassed = 23, 11
		w.CalculationFailures.Scalar = true
		w.FailurePhase = "load"
		w.ExitReason = "calculation_mismatch"
		w.Mismatch = &quick.CPUScalarMismatch{Vector: "div3", ExpectedBits: "3d70000000000000", ActualBits: "3d70000000000001"}
		v.CalculationFailures.Scalar = true
		v.Safety.PeakMilliCelsius = 90000
		v.Safety.MonitorStatus, v.Safety.MonitorFailure = "failed", "thermal_limit"
		requireStress(t, v)
		v.Status, v.StopReason = quick.StatusAborted, "thermal_limit"
		if _, _, err := quickevidence.DecodeCPUStress(stressBytes(t, v)); err == nil {
			t.Fatal("mismatch downgraded")
		}
		v.Status, v.StopReason = quick.StatusFail, "calculation_mismatch"
		v.Safety.CleanupConfirmed = false
		if _, _, err := quickevidence.DecodeCPUStress(stressBytes(t, v)); !errors.Is(err, quick.ErrCPUStressCleanupUnconfirmed) {
			t.Fatal("cleanup signability lost", err)
		}
	})
}

func TestCPUStressExactJSONShapeBoundaries(t *testing.T) {
	good := []byte(stressPassLiteral)
	if _, _, err := quickevidence.DecodeCPUStress(good); err != nil {
		t.Fatal(err)
	}
	bounded := append(append([]byte{}, good...), bytes.Repeat([]byte(" "), (1<<20)-len(good))...)
	if _, _, err := quickevidence.DecodeCPUStress(bounded); err != nil {
		t.Fatal("exact evidence cap", err)
	}
	if _, _, err := quickevidence.DecodeCPUStress(append(bounded, byte(32))); err == nil {
		t.Fatal("evidence cap plus one")
	}
	for _, test := range []struct {
		name string
		raw  []byte
	}{
		{"duplicate", bytes.Replace(good, []byte(`"status":"pass"`), []byte(`"status":"fail","status":"pass"`), 1)},
		{"case_alias", bytes.Replace(good, []byte(`"requested_load_ms"`), []byte(`"Requested_load_ms"`), 1)},
		{"missing", bytes.Replace(good, []byte(`"load_started_ms":2,`), nil, 1)},
		{"unknown", bytes.Replace(good, []byte(`"schema":`), []byte(`"unknown":false,"schema":`), 1)},
		{"null_boolean", bytes.Replace(good, []byte(`"accounting_valid":true`), []byte(`"accounting_valid":null`), 1)},
		{"bool_integer", bytes.Replace(good, []byte(`"workers_requested":1`), []byte(`"workers_requested":true`), 1)},
		{"fractional_integer", bytes.Replace(good, []byte(`"workers_requested":1`), []byte(`"workers_requested":1.5`), 1)},
		{"trailing", append(append([]byte{}, good...), []byte(`{}`)...)},
		{"invalid_utf8", append([]byte{0xff}, good...)},
		{"over_bound", bytes.Repeat([]byte(" "), (1<<20)+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := quickevidence.DecodeCPUStress(test.raw); err == nil {
				t.Fatal("invalid exact shape")
			}
		})
	}
	escaped := bytes.Replace(good, []byte(`"schema"`), []byte(`"\u0073chema"`), 1)
	if _, digest, err := quickevidence.DecodeCPUStress(escaped); err != nil || digest != "sha256:db80ee5817ade0a530be8ac426698de09d14533404faaaaaaf2df4619cfdf3f5" {
		t.Fatal("valid escaped spelling", err)
	}
}

const stressPassLiteral = `{"schema":"hhsoft.pc-tester.cpu-stress/v1","status":"pass","stop_reason":"completed","requested_load_ms":600000,"local_watchdog_ms":660000,"thermal_policy":"available-observations/v1","worker_selection":"explicit","workers_requested":1,"workers_launched":1,"workers_ready":1,"effective_deadline_ms":660000,"effective_deadline_source":"watchdog","load_started":true,"load_started_ms":2,"load_stopped_ms":600002,"observed_load_ms":600000,"workers_joined_ms":600003,"finished_ms":600004,"iterations":256,"checks_passed":24,"state_aggregate":17,"known_answer_sha256":"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad","scalar_suite":"binary64-normal-exact-v1","scalar_vector_count":12,"calculation_failures":{"integer":false,"sha256":false,"scalar":false},"accounting_valid":true,"stop_facts":{"cleanup_unconfirmed":false,"clock_invalid":false,"counter_overflow":false,"coverage_lost":false,"early_worker_exit":false,"incomplete_load_checks":false,"initial_coverage_partial":false,"initial_coverage_unavailable":false,"insufficient_budget":false,"invalid_observation":false,"launch_incomplete":false,"normal_load_complete":true,"observation_timeout":false,"observer_panic":false,"parent_cancelled":false,"parent_deadline":false,"readiness_incomplete":false,"thermal_limit":false,"watchdog_deadline":false,"worker_panic":false},"safety":{"global_timeout_ms":660000,"thermal_limit_millicelsius":90000,"thermal_sensors":1,"thermal_samples":1,"peak_millicelsius":40000,"monitor_status":"completed","cleanup_confirmed":true,"thermal_observations":1,"thermal_coverage":"available","thermal_rejected_readings":0,"thermal_unavailable_readings":0,"thermal_enumeration_errors":0},"workers":[{"index":0,"started_ms":0,"ready_ms":1,"load_entered_ms":2,"stopped_ms":600002,"exit_reason":"completed","preparation_iterations":1,"preparation_checks_passed":12,"iterations":256,"checks_passed":24,"load_iterations":255,"load_checks_passed":12,"calculation_failures":{"integer":false,"sha256":false,"scalar":false},"failure_phase":"none","mismatch":null}]}`

func TestCPUStressIndependentScalarMismatchOracles(t *testing.T) {
	for _, test := range []struct{ id, bits string }{
		{"add1", "400e000000000000"}, {"add2", "c01e000000000000"}, {"add3", "3eb8000000000000"},
		{"sub1", "4011000000000000"}, {"sub2", "c00e000000000000"}, {"sub3", "4260000000000000"},
		{"mul1", "400e000000000000"}, {"mul2", "bfe8000000000000"}, {"mul3", "4090000000000000"},
		{"div1", "4008000000000000"}, {"div2", "c002000000000000"}, {"div3", "3d70000000000000"},
	} {
		t.Run(test.id, func(t *testing.T) {
			slot := uint64(0)
			for _, id := range []string{"add1", "add2", "add3", "sub1", "sub2", "sub3", "mul1", "mul2", "mul3", "div1", "div2", "div3"} {
				if id == test.id {
					break
				}
				slot++
			}
			v := stressNoLoad(t)
			v.Status, v.StopReason = quick.StatusFail, "calculation_mismatch"
			v.EffectiveDeadlineSource, v.EffectiveDeadlineMilliseconds = "watchdog", 660000
			v.WorkersLaunched = 1
			v.WorkersJoinedMilliseconds, v.FinishedMilliseconds = 6, 7
			v.ChecksPassed = slot
			v.StateAggregate = 0x0123456789abcdef
			v.StopFacts = quick.CPUStressStopFacts{ReadinessIncomplete: true}
			v.CalculationFailures.Scalar = true
			v.Safety = stressLiteral(t).Safety
			v.Workers = []quick.CPUStressWorker{{Index: 0, StoppedMilliseconds: 5, ExitReason: "calculation_mismatch", PreparationChecksPassed: slot, ChecksPassed: slot, CalculationFailures: quick.CPUCalculationFailures{Scalar: true}, FailurePhase: "readiness", Mismatch: &quick.CPUScalarMismatch{Vector: test.id, ExpectedBits: test.bits, ActualBits: "0000000000000000"}}}
			requireStress(t, v)
			v.Workers[0].Mismatch.ExpectedBits = "ffffffffffffffff"
			if _, _, err := quickevidence.DecodeCPUStress(stressBytes(t, v)); err == nil {
				t.Fatal("forged expected arithmetic bits")
			}
		})
	}
}

func TestCPUStressCheckedAggregateCannotWrap(t *testing.T) {
	v := stressLiteral(t)
	other := v.Workers[0]
	other.Index = 1
	v.Workers = append(v.Workers, other)
	v.WorkersRequested, v.WorkersLaunched, v.WorkersReady = 2, 2, 2
	v.Iterations, v.ChecksPassed = 512, 48
	requireStress(t, v)
	w := &v.Workers[0]
	w.Iterations = math.MaxUint64
	w.ChecksPassed = (math.MaxUint64/256)*12 + 12
	w.LoadIterations = w.Iterations - 1
	w.LoadChecksPassed = w.ChecksPassed - 12
	v.Iterations = 255
	v.ChecksPassed = w.ChecksPassed + 24
	if _, _, err := quickevidence.DecodeCPUStress(stressBytes(t, v)); err == nil {
		t.Fatal("checked worker sum accepted wrapped iterations")
	}
}
