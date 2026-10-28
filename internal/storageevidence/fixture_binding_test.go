package storageevidence

import (
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
	"strings"
	"testing"
)

func TestStorageResultReconstructsSelectedFixtureFactsAndReservedScope(t *testing.T) {
	r := literalPass()
	r.FixtureIDs = []string{"usb:1-2"}
	r.Snapshot.FixtureObservations = []storagetest.FixtureObservation{{ID: "usb:1-2", Type: "usb"}}
	r.Snapshot.ResolvedFixtureMask = 1
	rehash(t, &r)
	if err := ValidateResult(r); err != nil {
		t.Fatalf("zero-descendant fixture hid healthy target: %v", err)
	}
	r.Snapshot.NodeFixtureMasks[0] = 1
	rehash(t, &r)
	requireInvalidResult(t, r)
	r.Snapshot.NodeFixtureMasks[0] = 0
	r.Snapshot.FixtureObservations[0].Type = "block"
	rehash(t, &r)
	requireInvalidResult(t, r)
	r.FixtureIDs = []string{"block:a"}
	r.Snapshot.FixtureObservations = []storagetest.FixtureObservation{{ID: "block:a", Type: "block", DeviceNumber: "8:0"}}
	r.Snapshot.NodeFixtureMasks[0] = 1
	r.Status = "inconclusive"
	r.AcceptedReadBytes = 0
	r.AcceptedComparedBytes = 0
	r.AcceptedMatchedRanges = 0
	r.Devices = []storagetest.DeviceResult{{Interface: "a", Eligibility: "excluded", Exclusion: "fixture", Status: "excluded", Error: "none", CloseConfirmed: true}}
	rehash(t, &r)
	if err := ValidateResult(r); err != nil {
		t.Fatalf("standalone reserved node: %v", err)
	}
	r.Scope = "planned"
	r.PlanID = "sha256:" + strings.Repeat("b", 64)
	r.ComponentIDs = []string{"block:a"}
	r.Devices[0].Requested = true
	if err := ValidateResult(r); err != nil {
		t.Fatalf("planned reserved target not retained: %v", err)
	}
	r.Snapshot.FixtureObservations[0].DeviceNumber = "8:1"
	rehash(t, &r)
	requireInvalidResult(t, r)
}

func TestStorageResultUnknownOrContradictoryLoopFactsNeverBecomeEmpty(t *testing.T) {
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
		t.Fatalf("typed excluded backing: %v", err)
	}
	r.Snapshot.Discovery.MemoryMounts[0].Filesystem = "ramfs"
	rehash(t, &r)
	requireInvalidResult(t, r)
	r.Snapshot.Discovery.MemoryMounts = []storagetest.MemoryMount{}
	rehash(t, &r)
	requireInvalidResult(t, r)
	node.LoopState = "unbound"
	node.LoopBacking = nil
	node.Geometry.CapacityBytes = 0
	rehash(t, &r)
	if err := ValidateResult(r); err != nil {
		t.Fatalf("strict zero-work unbound loop: %v", err)
	}
	node.LoopState = "unknown"
	rehash(t, &r)
	requireInvalidResult(t, r)
}
