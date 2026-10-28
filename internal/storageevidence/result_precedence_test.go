package storageevidence

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/workerexec"
)

func TestStorageResultFailureDiagnosticAndCleanupPrecedence(t *testing.T) {
	r := literalPass()
	observation := r.Devices[0].Observation
	observation.Status = "fail"
	observation.Error = "short_read"
	observation.Ranges[0] = storagetest.RangeObservation{Offset: 0, Length: 512, ReadCount: 1, FirstBytes: 257, Outcome: "short_read"}
	observation.ReadBytes = 257
	observation.ComparedBytes = 0
	observation.MatchedRanges = 0
	r.Devices[0].Status = "fail"
	r.Devices[0].Error = "short_read"
	r.AcceptedReadBytes = 257
	r.AcceptedComparedBytes = 0
	r.AcceptedMatchedRanges = 0
	r.Status = "fail"
	r.Error = "short_read"
	r.FailureObserved = true
	for _, stop := range []string{"completed", "operator_cancelled", "deadline", "error"} {
		r.StopReason = stop
		if err := ValidateResult(r); err != nil {
			t.Fatalf("accepted failure lost after %s: %v", stop, err)
		}
	}
	r.Status = "aborted"
	r.Error = "cancelled"
	r.StopReason = "operator_cancelled"
	requireInvalidResult(t, r)
	r.Devices[0].Accepted = false
	r.AcceptedReadBytes = 0
	r.FailureObserved = false
	r.DiagnosticFailure = true
	if err := ValidateResult(r); err != nil {
		t.Fatalf("diagnostic failure: %v", err)
	}
	view, err := Summary(r)
	if err != nil || view.AcceptedReadBytes != 0 || view.DiagnosticReadBytes != 257 {
		t.Fatalf("diagnostic counters: %+v %v", view, err)
	}
	r.AcceptedReadBytes = 257
	requireInvalidResult(t, r)
	r.AcceptedReadBytes = 0
	r.CleanupConfirmed = false
	r.Status = "error"
	r.StopReason = "cleanup_unconfirmed"
	r.Error = "cleanup_unconfirmed"
	r.Devices[0].CloseConfirmed = false
	if err := ValidateResult(r); !errors.Is(err, workerexec.ErrCleanupUncertain) {
		t.Fatalf("cleanup type lost: %v", err)
	}
	raw, _ := json.Marshal(r)
	got, err := Decode(raw)
	if !errors.Is(err, workerexec.ErrCleanupUncertain) || !reflect.DeepEqual(got, r) {
		t.Fatal("cleanup diagnostic was discarded or accepted")
	}
	if _, err := CanonicalTypedBytes(r); !errors.Is(err, workerexec.ErrCleanupUncertain) {
		t.Fatal("uncertain cleanup canonicalized for signing")
	}
	if _, err := Summary(r); !errors.Is(err, workerexec.ErrCleanupUncertain) {
		t.Fatal("uncertain cleanup rendered")
	}
}

func TestStorageResultInterruptedRangeRetainsOnlyDiagnosticWork(t *testing.T) {
	for _, count := range []uint8{1, 2} {
		r := literalPass()
		r.Status = "aborted"
		r.StopReason = "operator_cancelled"
		r.Error = "cancelled"
		r.AcceptedReadBytes = 0
		r.AcceptedComparedBytes = 0
		r.AcceptedMatchedRanges = 0
		d := &r.Devices[0]
		d.Accepted = false
		d.Status = "aborted"
		d.Error = "cancelled"
		d.FinalBindingConfirmed = false
		o := d.Observation
		o.Status = "aborted"
		o.Error = "cancelled"
		o.Ranges[0].Outcome = "interrupted"
		o.Ranges[0].ReadCount = count
		o.Ranges[0].SecondBytes = 0
		if count == 2 {
			o.Ranges[0].SecondBytes = 512
		}
		o.ReadBytes = uint64(count) * 512
		o.ComparedBytes = 0
		o.MatchedRanges = 0
		if err := ValidateResult(r); err != nil {
			t.Fatalf("interrupted: %v", err)
		}
		view, err := Summary(r)
		if err != nil || view.DiagnosticReadBytes != uint64(count)*512 || view.AcceptedComparedBytes != 0 || view.DiagnosticComparedBytes != 0 {
			t.Fatalf("interrupted credit: %+v %v", view, err)
		}
		r.AcceptedReadBytes = o.ReadBytes
		requireInvalidResult(t, r)
	}
	r := literalPass()
	r.Devices[0].Accepted = false
	r.Devices[0].Status = "error"
	r.Devices[0].Error = "deadline"
	r.Status = "error"
	r.StopReason = "deadline"
	r.Error = "deadline"
	r.AcceptedReadBytes = 0
	r.AcceptedComparedBytes = 0
	r.AcceptedMatchedRanges = 0
	if err := ValidateResult(r); err != nil {
		t.Fatalf("late full observation diagnostic: %v", err)
	}
	r.Status = "inconclusive"
	r.StopReason = "completed"
	r.Error = "none"
	requireInvalidResult(t, r)
}

func TestStorageResultNoWorkAfterTerminalFailureOrUnstartedGap(t *testing.T) {
	r := literalPass()
	b := literalPass()
	b.Snapshot.Discovery.Nodes[0].ID = "block:b"
	b.Snapshot.Discovery.Nodes[0].Number = "8:1"
	b.Devices[0].Interface = "b"
	b.Devices[0].Observation.Interface = "b"
	b.Devices[0].Observation.Identity.Minor = 1
	b.Devices[0].StartedMilliseconds = 10
	b.Devices[0].FinishedMilliseconds = 18
	r.Snapshot.Discovery.Nodes = append(r.Snapshot.Discovery.Nodes, b.Snapshot.Discovery.Nodes[0])
	r.Snapshot.NodeFixtureMasks = append(r.Snapshot.NodeFixtureMasks, 0)
	r.Devices = append(r.Devices, b.Devices[0])
	r.DurationMilliseconds = 20
	r.AcceptedReadBytes = 2048
	r.AcceptedComparedBytes = 1024
	r.AcceptedMatchedRanges = 2
	rehash(t, &r)
	if err := ValidateResult(r); err != nil {
		t.Fatalf("sequential control: %v", err)
	}
	r.Devices[1].StartedMilliseconds = 8
	requireInvalidResult(t, r)
	r.Devices[1].StartedMilliseconds = 10
	r.Devices[0] = storagetest.DeviceResult{Interface: "a", Requested: true, Eligibility: "eligible", Exclusion: "none", Status: "not_started", Error: "none", CloseConfirmed: true}
	r.AcceptedReadBytes = 1024
	r.AcceptedComparedBytes = 512
	r.AcceptedMatchedRanges = 1
	r.Status = "inconclusive"
	requireInvalidResult(t, r)
	r.Devices[0].Status = "error"
	r.Devices[0].Error = "open_error"
	r.Status = "error"
	r.StopReason = "error"
	r.Error = "open_error"
	requireInvalidResult(t, r)
}

func TestStorageResultAcceptedFaultSurvivesLaterDeviceStop(t *testing.T) {
	for _, stop := range []string{"completed", "error", "operator_cancelled"} {
		r := literalPass()
		first := &r.Devices[0]
		first.Status = "fail"
		first.Error = "read_error"
		first.Observation.Status = "fail"
		first.Observation.Error = "read_error"
		first.Observation.Ranges[0] = storagetest.RangeObservation{Offset: 0, Length: 512, ReadCount: 1, Outcome: "read_error"}
		first.Observation.ReadBytes = 0
		first.Observation.ComparedBytes = 0
		first.Observation.MatchedRanges = 0
		second := literalPass()
		node := second.Snapshot.Discovery.Nodes[0]
		node.ID = "block:b"
		node.Number = "8:1"
		device := second.Devices[0]
		device.Interface = "b"
		device.Observation.Interface = "b"
		device.Observation.Identity.Minor = 1
		device.StartedMilliseconds = 10
		device.FinishedMilliseconds = 18
		r.Snapshot.Discovery.Nodes = append(r.Snapshot.Discovery.Nodes, node)
		r.Snapshot.NodeFixtureMasks = append(r.Snapshot.NodeFixtureMasks, 0)
		r.Devices = append(r.Devices, device)
		r.DurationMilliseconds = 20
		r.Status = "fail"
		r.StopReason = stop
		r.Error = "read_error"
		r.FailureObserved = true
		if stop != "completed" {
			r.AcceptedReadBytes = 0
			r.AcceptedComparedBytes = 0
			r.AcceptedMatchedRanges = 0
			r.Devices[1] = storagetest.DeviceResult{Interface: "b", Requested: true, Eligibility: "eligible", Exclusion: "none", Status: "not_started", Error: "none", CloseConfirmed: true}
			if stop == "error" {
				r.Devices[1].Status = "error"
				r.Devices[1].Error = "open_error"
				r.Devices[1].StartedMilliseconds = 10
				r.Devices[1].FinishedMilliseconds = 11
			}
		}
		rehash(t, &r)
		if err := ValidateResult(r); err != nil {
			t.Fatalf("accepted zero-byte read failure lost after %s: %v", stop, err)
		}
		r.Status = "error"
		r.Error = "open_error"
		r.StopReason = "error"
		requireInvalidResult(t, r)
	}
}

func TestStorageResultUnstartedEligibleWorkRequiresExplicitStop(t *testing.T) {
	for _, prefix := range []bool{false, true} {
		r := literalPass()
		tail := storagetest.DeviceResult{Interface: "a", Requested: true, Eligibility: "eligible", Exclusion: "none", Status: "not_started", Error: "none", CloseConfirmed: true}
		if prefix {
			node := literalPass().Snapshot.Discovery.Nodes[0]
			node.ID = "block:b"
			node.Number = "8:1"
			r.Snapshot.Discovery.Nodes = append(r.Snapshot.Discovery.Nodes, node)
			r.Snapshot.NodeFixtureMasks = append(r.Snapshot.NodeFixtureMasks, 0)
			tail.Interface = "b"
			r.Devices = append(r.Devices, tail)
		} else {
			r.Devices = []storagetest.DeviceResult{tail}
			r.AcceptedReadBytes = 0
			r.AcceptedComparedBytes = 0
			r.AcceptedMatchedRanges = 0
		}
		r.Status = "inconclusive"
		rehash(t, &r)
		requireInvalidResult(t, r)
		for _, stop := range []struct{ status, reason, code string }{{"aborted", "operator_cancelled", "cancelled"}, {"error", "deadline", "deadline"}, {"error", "error", "safety_changed"}} {
			r.Status = stop.status
			r.StopReason = stop.reason
			r.Error = stop.code
			if err := ValidateResult(r); err != nil {
				t.Fatalf("explicit stop tail rejected prefix=%v stop=%s: %v", prefix, stop.reason, err)
			}
		}
	}
}

func TestStorageResultTimingFloorsPreserveSubmillisecondPossibility(t *testing.T) {
	r := literalPass()
	r.BudgetMilliseconds = 0
	r.DurationMilliseconds = 0
	r.Devices[0].StartedMilliseconds = 0
	r.Devices[0].FinishedMilliseconds = 0
	if err := ValidateResult(r); err != nil {
		t.Fatalf("floored zero budget was mistaken for exact zero time: %v", err)
	}
	r = literalPass()
	r.Status = "error"
	r.StopReason = "deadline"
	r.Error = "deadline"
	r.Devices[0].Accepted = false
	r.Devices[0].Status = "error"
	r.Devices[0].Error = "deadline"
	r.AcceptedReadBytes = 0
	r.AcceptedComparedBytes = 0
	r.AcceptedMatchedRanges = 0
	r.BudgetMilliseconds = 1
	r.Devices[0].StartedMilliseconds = 2
	requireInvalidResult(t, r)
}
