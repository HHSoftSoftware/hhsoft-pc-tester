package storageevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

func literalPass() storagetest.Result {
	return storagetest.Result{Schema: storagetest.Schema, Policy: storagetest.Policy, Scope: "standalone", Status: "pass", StopReason: "completed", Error: "none", LimitMilliseconds: 1000, BudgetMilliseconds: 1000, DurationMilliseconds: 10, CleanupConfirmed: true, DiscoveryAccepted: true, ComponentIDs: []string{}, FixtureIDs: []string{}, MissingComponents: []string{}, Snapshot: &storagetest.Snapshot{
		Discovery: storagetest.Discovery{Complete: true, BackingComplete: true, SwapState: "none", Nodes: []storagetest.Node{{ID: "block:a", Number: "8:0", Kind: "physical", Transport: "ata", Geometry: &storagetest.Geometry{CapacityBytes: 512, LogicalBlockBytes: 512}, USBAncestors: []string{}, PhysicalChildren: []string{}, Holders: []string{}, Slaves: []string{}}}, MountedNumbers: []string{}, MemoryMounts: []storagetest.MemoryMount{}, BootBackingNumbers: []string{}}, FixtureObservations: []storagetest.FixtureObservation{}, NodeFixtureMasks: []uint8{0}},
		SnapshotSHA256: "e55713dfab1962c1d42a4b3304b418646ae3cec9e6b3aa287d847013fd9a400b",
		Devices:        []storagetest.DeviceResult{{Interface: "a", Requested: true, Eligibility: "eligible", Exclusion: "none", Status: "pass", Error: "none", Accepted: true, StartedMilliseconds: 1, FinishedMilliseconds: 9, CloseConfirmed: true, FinalBindingConfirmed: true, Observation: &storagetest.SampleObservation{Interface: "a", Identity: storagetest.Identity{Major: 8, Minor: 0, CapacityBytes: 512, LogicalBlockBytes: 512}, Status: "pass", Error: "none", Ranges: []storagetest.RangeObservation{{Offset: 0, Length: 512, ReadCount: 2, FirstBytes: 512, SecondBytes: 512, Outcome: "matched"}}, ReadBytes: 1024, ComparedBytes: 512, MatchedRanges: 1}}}, AcceptedReadBytes: 1024, AcceptedComparedBytes: 512, AcceptedMatchedRanges: 1}
}
func rehash(t *testing.T, r *storagetest.Result) {
	t.Helper()
	raw, err := json.Marshal(r.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	r.SnapshotSHA256 = hex.EncodeToString(sum[:])
}
func requireInvalidResult(t *testing.T, r storagetest.Result) {
	t.Helper()
	if err := ValidateResult(r); err != ErrInvalid {
		t.Fatalf("forged result admitted: %v", err)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Decode(raw); err != ErrInvalid || !reflect.DeepEqual(got, storagetest.Result{}) {
		t.Fatalf("decoder retained invalid value: %+v %v", got, err)
	}
}

func TestStorageResultLiteralPassAndCanonicalSnapshot(t *testing.T) {
	r := literalPass()
	if err := ValidateResult(r); err != nil {
		t.Fatal(err)
	}
	want := r.SnapshotSHA256
	rehash(t, &r)
	if r.SnapshotSHA256 != want {
		t.Fatalf("literal independent snapshot hash changed: %s", r.SnapshotSHA256)
	}
	raw, err := CanonicalTypedBytes(r)
	if err != nil || len(raw) == 0 || raw[len(raw)-1] == '\n' {
		t.Fatalf("canonical bytes: %v", err)
	}
	got, err := Decode(append(raw, '\n'))
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatalf("typed round trip: %+v %v", got, err)
	}
	view, err := Summary(r)
	if err != nil || view.AcceptedReadBytes != 1024 || view.AcceptedComparedBytes != 512 || view.AcceptedMatchedRanges != 1 || view.DiagnosticReadBytes != 0 || len(view.Devices) != 1 {
		t.Fatalf("summary: %+v %v", view, err)
	}
	view.Devices[0].Interface = "changed"
	if r.Devices[0].Interface != "a" {
		t.Fatal("summary aliases result")
	}
}
