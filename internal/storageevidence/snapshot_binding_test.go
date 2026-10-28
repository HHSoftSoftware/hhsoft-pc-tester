package storageevidence

import (
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
	"strings"
	"testing"
)

func TestStorageSnapshotDigestBindsReadOnlyAndExactDeviceIdentity(t *testing.T) {
	for _, change := range []func(*storagetest.Result){
		func(r *storagetest.Result) { r.Snapshot.Discovery.Nodes[0].ReadOnly = true },
		func(r *storagetest.Result) {
			r.Snapshot.Discovery.Nodes[0].Number = "8:1"
			r.Devices[0].Observation.Identity.Minor = 1
		},
		func(r *storagetest.Result) {
			r.Snapshot.Discovery.Nodes[0].ID = "block:b"
			r.Devices[0].Interface = "b"
			r.Devices[0].Observation.Interface = "b"
		},
	} {
		r := literalPass()
		before := r.SnapshotSHA256
		change(&r)
		requireInvalidResult(t, r)
		rehash(t, &r)
		if r.SnapshotSHA256 == before {
			t.Fatal("changed metadata retained old snapshot digest")
		}
		if err := ValidateResult(r); err != nil {
			t.Fatalf("coherent newly bound metadata: %v", err)
		}
	}
}

func TestStorageSnapshotDigestBindsLoopTupleAndUnboundTransition(t *testing.T) {
	r := literalPass()
	node := &r.Snapshot.Discovery.Nodes[0]
	node.Kind = "loop"
	node.Transport = "virtual"
	node.LoopState = "configured"
	node.LoopBacking = &storagetest.LoopBacking{Kind: "tmpfs", Device: "0:31", BindingSHA256: strings.Repeat("a", 64)}
	r.Snapshot.Discovery.MemoryMounts = []storagetest.MemoryMount{{Number: "0:31", Filesystem: "tmpfs"}}
	r.Status = "inconclusive"
	r.AcceptedReadBytes = 0
	r.AcceptedComparedBytes = 0
	r.AcceptedMatchedRanges = 0
	r.Devices = []storagetest.DeviceResult{{Interface: "a", Requested: true, Eligibility: "excluded", Exclusion: "mapped", Status: "excluded", Error: "none", CloseConfirmed: true}}
	rehash(t, &r)
	if err := ValidateResult(r); err != nil {
		t.Fatal(err)
	}
	before := r.SnapshotSHA256
	node.LoopBacking.BindingSHA256 = strings.Repeat("b", 64)
	requireInvalidResult(t, r)
	rehash(t, &r)
	if r.SnapshotSHA256 == before {
		t.Fatal("same-filesystem numeric tuple replacement unbound")
	}
	if err := ValidateResult(r); err != nil {
		t.Fatal(err)
	}
	before = r.SnapshotSHA256
	node.LoopState = "unbound"
	node.LoopBacking = nil
	node.Geometry.CapacityBytes = 0
	requireInvalidResult(t, r)
	rehash(t, &r)
	if r.SnapshotSHA256 == before {
		t.Fatal("configured-to-unbound transition lost")
	}
	if err := ValidateResult(r); err != nil {
		t.Fatal(err)
	}
}
