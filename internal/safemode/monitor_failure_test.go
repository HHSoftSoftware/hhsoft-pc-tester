package safemode

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMonitorUnsafeSnapshotLatchesBeforeCancellation(t *testing.T) {
	var calls, cancellations atomic.Int32
	ready := make(chan *Monitor, 1)
	monitor, err := StartMonitor(func(context.Context) Result {
		calls.Add(1)
		value := monitorSafeResult()
		value.Status, value.ActiveNonLoopInterfaces = "fail", 1
		return value
	}, MonitorOptions{Started: time.Now(), Cancel: func() {
		current := <-ready
		if !current.Failed() {
			panic("failure was not latched before cancellation")
		}
		cancellations.Add(1)
	}})
	if err != nil {
		t.Fatal(err)
	}
	ready <- monitor
	t.Cleanup(func() { stopTestMonitor(t, monitor) })
	awaitMonitorFailure(t, monitor)
	result := stopTestMonitor(t, monitor)
	if result.Status != "fail" || !result.CleanupConfirmed || result.FirstFailure == nil ||
		result.FirstFailure.Reason != "unsafe_observation" || result.FirstFailure.Observation == nil ||
		result.FirstFailure.Observation.ActiveNonLoopInterfaces != 1 || result.CompletedObservations != 1 ||
		calls.Load() != 1 || cancellations.Load() != 1 {
		t.Fatalf("unsafe latch: %+v", result)
	}
	baseline := stopTestMonitor(t, monitor)
	result.FirstFailure.Observation.Status = "caller mutation"
	if result.LastObservation.Status != "fail" {
		t.Fatal("failure and last observation share mutable storage")
	}
	result.LastObservation.Status = "another mutation"
	if again := stopTestMonitor(t, monitor); !reflect.DeepEqual(again, baseline) || calls.Load() != 1 {
		t.Fatal("caller mutation or later observation changed frozen result")
	}
}

func TestMonitorUnknownAndContradictorySnapshotsFailClosed(t *testing.T) {
	cases := map[string]func(*Result){
		"wrong schema with unsafe counters": func(value *Result) {
			value.Schema = "untrusted schema"
			value.Status = "fail"
			value.ActiveNonLoopInterfaces = 1
		},
		"schema":                 func(value *Result) { value.Schema = "untrusted schema" },
		"status":                 func(value *Result) { value.Status = "untrusted status" },
		"missing storage":        func(value *Result) { value.StorageObserved = false },
		"missing network":        func(value *Result) { value.NetworkObserved = false },
		"negative mount count":   func(value *Result) { value.WritableBlockMounts = -1 },
		"negative network count": func(value *Result) { value.ActiveNonLoopInterfaces = -1 },
		"unsafe pass":            func(value *Result) { value.ActiveNonLoopInterfaces = 1 },
		"storage detail":         func(value *Result) { value.StorageDetail = "untrusted location" },
		"network detail":         func(value *Result) { value.NetworkDetail = "untrusted location" },
		"error":                  func(value *Result) { value.Status = "error" },
		"aborted":                func(value *Result) { value.Status = "aborted" },
		"unexplained fail":       func(value *Result) { value.Status = "fail" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value := monitorSafeResult()
			mutate(&value)
			monitor, err := StartMonitor(func(context.Context) Result { return value }, MonitorOptions{Started: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stopTestMonitor(t, monitor) })
			awaitMonitorFailure(t, monitor)
			result := stopTestMonitor(t, monitor)
			if result.Status != "error" || !result.CleanupConfirmed || result.FirstFailure == nil || result.FirstFailure.Reason != "invalid_observation" ||
				result.CompletedObservations != 1 || result.FirstFailure.Observation == nil {
				t.Fatalf("invalid result accepted: %+v", result)
			}
			encoded, err := json.Marshal(result)
			if err != nil || strings.Contains(string(encoded), "untrusted") {
				t.Fatal("arbitrary observer text entered evidence")
			}
		})
	}
}
