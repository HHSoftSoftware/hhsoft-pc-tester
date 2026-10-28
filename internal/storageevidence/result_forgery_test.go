package storageevidence

import (
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
	"strings"
	"testing"
)

func TestStorageResultRejectsForgedProjectionAndCredit(t *testing.T) {
	cases := []struct {
		name   string
		change func(*storagetest.Result)
	}{
		{"schema", func(r *storagetest.Result) { r.Schema = "hhsoft.pc-tester.storage-sample/v2" }},
		{"policy", func(r *storagetest.Result) { r.Policy = "uncached" }},
		{"media write", func(r *storagetest.Result) { r.MediaWrites = true }},
		{"cache bypass", func(r *storagetest.Result) { r.CacheBypass = true }},
		{"snapshot digest", func(r *storagetest.Result) { r.SnapshotSHA256 = strings.Repeat("a", 64) }},
		{"missing node", func(r *storagetest.Result) {
			r.Snapshot.Discovery.Nodes = []storagetest.Node{}
			r.Snapshot.NodeFixtureMasks = []uint8{}
			rehash(t, r)
		}},
		{"unknown node", func(r *storagetest.Result) { r.Snapshot.Discovery.Nodes[0].Kind = "guess"; rehash(t, r) }},
		{"missing geometry", func(r *storagetest.Result) { r.Snapshot.Discovery.Nodes[0].Geometry = nil; rehash(t, r) }},
		{"unknown memory", func(r *storagetest.Result) {
			r.Snapshot.Discovery.MemoryMounts = []storagetest.MemoryMount{{Number: "0:31", Filesystem: "overlay"}}
			rehash(t, r)
		}},
		{"false backing complete", func(r *storagetest.Result) { r.Snapshot.Discovery.BackingComplete = false; rehash(t, r) }},
		{"active swap", func(r *storagetest.Result) { r.Snapshot.Discovery.SwapState = "active"; rehash(t, r) }},
		{"mounted claim", func(r *storagetest.Result) { r.Snapshot.Discovery.MountedNumbers = []string{"8:0"}; rehash(t, r) }},
		{"fabricated mask", func(r *storagetest.Result) { r.Snapshot.NodeFixtureMasks[0] = 1; rehash(t, r) }},
		{"resolved mask", func(r *storagetest.Result) { r.Snapshot.ResolvedFixtureMask = 1; rehash(t, r) }},
		{"unaccepted discovery", func(r *storagetest.Result) { r.DiscoveryAccepted = false }},
		{"wrong requested", func(r *storagetest.Result) { r.Devices[0].Requested = false }},
		{"wrong eligibility", func(r *storagetest.Result) { r.Devices[0].Eligibility = "excluded" }},
		{"missing final binding", func(r *storagetest.Result) { r.Devices[0].FinalBindingConfirmed = false }},
		{"missing close", func(r *storagetest.Result) { r.Devices[0].CloseConfirmed = false }},
		{"aggregate read", func(r *storagetest.Result) { r.AcceptedReadBytes++ }},
		{"aggregate compare", func(r *storagetest.Result) { r.AcceptedComparedBytes++ }},
		{"aggregate matched", func(r *storagetest.Result) { r.AcceptedMatchedRanges++ }},
		{"failure flag", func(r *storagetest.Result) { r.FailureObserved = true }},
		{"diagnostic flag", func(r *storagetest.Result) { r.DiagnosticFailure = true }},
		{"wrong identity", func(r *storagetest.Result) { r.Devices[0].Observation.Identity.Minor = 1 }},
		{"null ranges", func(r *storagetest.Result) { r.Devices[0].Observation.Ranges = nil }},
		{"wrong schedule", func(r *storagetest.Result) { r.Devices[0].Observation.Ranges[0].Offset = 512 }},
		{"zero work pass", func(r *storagetest.Result) { r.Devices[0].Observation.Ranges[0].ReadCount = 0 }},
		{"late pass", func(r *storagetest.Result) { r.DurationMilliseconds = 1001 }},
		{"late acceptance", func(r *storagetest.Result) { r.BudgetMilliseconds = 8 }},
		{"zero budget", func(r *storagetest.Result) { r.BudgetMilliseconds = 0 }},
		{"negative interval", func(r *storagetest.Result) { r.Devices[0].StartedMilliseconds = -1 }},
		{"bad planned ID", func(r *storagetest.Result) { r.Scope = "planned"; r.PlanID = "future" }},
		{"unsupported after reads", func(r *storagetest.Result) {
			r.Status = "error"
			r.StopReason = "error"
			r.Error = "unsupported_platform"
		}},
		{"invented missing component", func(r *storagetest.Result) { r.Status = "error"; r.StopReason = "error"; r.Error = "missing_component" }},
		{"close failure disguised as clean", func(r *storagetest.Result) { r.Status = "error"; r.StopReason = "error"; r.Error = "close_error" }},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) { r := literalPass(); item.change(&r); requireInvalidResult(t, r) })
	}
}

func TestStorageResultDisklessMissingAndExcludedStayZeroWork(t *testing.T) {
	r := literalPass()
	r.Snapshot.Discovery.Nodes = []storagetest.Node{}
	r.Snapshot.NodeFixtureMasks = []uint8{}
	r.Devices = []storagetest.DeviceResult{}
	r.AcceptedReadBytes = 0
	r.AcceptedComparedBytes = 0
	r.AcceptedMatchedRanges = 0
	r.Status = "inconclusive"
	rehash(t, &r)
	if err := ValidateResult(r); err != nil {
		t.Fatalf("diskless: %v", err)
	}
	r.Status = "pass"
	requireInvalidResult(t, r)
	r.Status = "error"
	r.StopReason = "error"
	r.Error = "missing_component"
	r.Scope = "planned"
	r.PlanID = "sha256:" + strings.Repeat("a", 64)
	r.ComponentIDs = []string{"block:z"}
	r.MissingComponents = []string{"block:z"}
	r.DiscoveryAccepted = false
	if err := ValidateResult(r); err != nil {
		t.Fatalf("explicit missing: %v", err)
	}
	r.MissingComponents = []string{}
	requireInvalidResult(t, r)
	r = literalPass()
	r.Snapshot = nil
	r.SnapshotSHA256 = ""
	r.Devices = []storagetest.DeviceResult{}
	r.DiscoveryAccepted = false
	r.AcceptedReadBytes = 0
	r.AcceptedComparedBytes = 0
	r.AcceptedMatchedRanges = 0
	r.Status = "error"
	r.StopReason = "error"
	r.Error = "unsupported_platform"
	if err := ValidateResult(r); err != nil {
		t.Fatalf("unavailable: %v", err)
	}
	r.Status = "inconclusive"
	r.StopReason = "completed"
	r.Error = "none"
	requireInvalidResult(t, r)
	r = literalPass()
	r.Snapshot.Discovery.Nodes[0].USBAncestors = []string{"usb:1-2"}
	rehash(t, &r)
	r.Status = "inconclusive"
	r.AcceptedReadBytes = 0
	r.AcceptedComparedBytes = 0
	r.AcceptedMatchedRanges = 0
	r.Devices = []storagetest.DeviceResult{{Interface: "a", Requested: true, Eligibility: "excluded", Exclusion: "usb", Status: "excluded", Error: "none", CloseConfirmed: true}}
	if err := ValidateResult(r); err != nil {
		t.Fatalf("USB exclusion: %v", err)
	}
	r.Status = "pass"
	requireInvalidResult(t, r)
}
