package storagesample

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storageevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/workerexec"
)

func fixtureSampleDiscovery() storagetest.Discovery {
	return storagetest.Discovery{Complete: true, BackingComplete: true, SwapState: "none",
		Nodes: []storagetest.Node{{ID: "block:a", Number: "8:0", Kind: "physical", Transport: "ata",
			Geometry:     &storagetest.Geometry{CapacityBytes: 512, LogicalBlockBytes: 512},
			USBAncestors: []string{}, PhysicalChildren: []string{}, Holders: []string{}, Slaves: []string{}}},
		MountedNumbers: []string{}, MemoryMounts: []storagetest.MemoryMount{}, BootBackingNumbers: []string{}}
}
func fixtureSampleInventory() inventory.Snapshot {
	return inventory.Snapshot{SchemaVersion: inventory.SnapshotSchemaVersion, Components: []inventory.Component{}}
}
func fixtureSampleResultOutcome() sampleParentOutcome {
	return sampleParentOutcome{Error: "none", CleanupConfirmed: true,
		Summary: storagetest.ObservationSummary{ReadBytes: 1024, ComparedBytes: 512, MatchedRanges: 1},
		Device: storagetest.DeviceResult{Interface: "a", Requested: true, Eligibility: "eligible", Exclusion: "none",
			Status: "pass", Error: "none", Accepted: true, StartedMilliseconds: 1, FinishedMilliseconds: 9,
			CloseConfirmed: true, FinalBindingConfirmed: true,
			Observation: &storagetest.SampleObservation{Interface: "a", Status: "pass", Error: "none",
				Identity:  storagetest.Identity{Major: 8, Minor: 0, CapacityBytes: 512, LogicalBlockBytes: 512},
				Ranges:    []storagetest.RangeObservation{{Offset: 0, Length: 512, ReadCount: 2, FirstBytes: 512, SecondBytes: 512, Outcome: "matched"}},
				ReadBytes: 1024, ComparedBytes: 512, MatchedRanges: 1}}}
}
func fixtureSampleCompletion() sampleRunCompletion {
	return sampleRunCompletion{limitMilliseconds: 1000, budgetMilliseconds: 1000, durationMilliseconds: 10, reason: "none", cleanupConfirmed: true}
}
func fixtureSampleProjection(t *testing.T) sampleProjection {
	t.Helper()
	projection, err := prepareStandaloneSampleProjection(context.Background(), fixtureSampleDiscovery(), []string{}, fixtureSampleInventory())
	if err != nil {
		t.Fatal(err)
	}
	return projection
}
func makeSampleMismatch(outcome *sampleParentOutcome) {
	outcome.Device.Status, outcome.Device.Error = "fail", "mismatch"
	outcome.Device.Observation.Status, outcome.Device.Observation.Error = "fail", "mismatch"
	outcome.Device.Observation.Ranges[0].Outcome = "mismatch"
	outcome.Device.Observation.ComparedBytes, outcome.Device.Observation.MatchedRanges = 0, 0
	outcome.Summary = storagetest.ObservationSummary{ReadBytes: 1024, FailureObserved: true}
}

func TestStorageSampleProjectionAndResultFreezeMetadata(t *testing.T) {
	discovery := fixtureSampleDiscovery()
	projection, err := prepareStandaloneSampleProjection(context.Background(), discovery, []string{}, fixtureSampleInventory())
	if err != nil || projection.digest != "e55713dfab1962c1d42a4b3304b418646ae3cec9e6b3aa287d847013fd9a400b" ||
		len(projection.targets) != 1 || !reflect.DeepEqual(projection.targets[0].Ranges, []storagetest.Range{{Offset: 0, Length: 512}}) {
		t.Fatal("independent literal projection changed")
	}
	discovery.Nodes[0].Geometry.CapacityBytes = 2048
	discovery.Nodes[0].ID = "block:changed"
	result, err := finishStandaloneSample(&projection, fixtureSampleCompletion(), []sampleParentOutcome{fixtureSampleResultOutcome()})
	if err != nil || result.Status != "pass" || result.SnapshotSHA256 != "e55713dfab1962c1d42a4b3304b418646ae3cec9e6b3aa287d847013fd9a400b" ||
		result.AcceptedReadBytes != 1024 || result.AcceptedComparedBytes != 512 || result.AcceptedMatchedRanges != 1 || result.DiagnosticFailure || result.FailureObserved {
		t.Fatalf("literal completed result changed: %+v,%v", result, err)
	}
	if err := storageevidence.ValidateResult(result); err != nil {
		t.Fatal(err)
	}
	result.Snapshot.Discovery.Nodes[0].Geometry.CapacityBytes = 4096
	result.Devices[0].Observation.Ranges[0].Offset = 1
	if projection.snapshot.Discovery.Nodes[0].Geometry.CapacityBytes != 512 || projection.targets[0].Ranges[0].Offset != 0 {
		t.Fatal("result aliases frozen projection")
	}
}

func TestStorageSampleProjectionExcludesFamiliesAndPreservesUnrelatedMedia(t *testing.T) {
	for _, mode := range []string{"mounted", "boot", "mounted_child", "reserved_fixture", "usb", "removable", "optical", "floppy", "unknown", "empty", "geometry"} {
		t.Run(mode, func(t *testing.T) {
			discovery := fixtureSampleDiscovery()
			other := fixtureSampleDiscovery().Nodes[0]
			other.ID, other.Number = "block:b", "8:16"
			discovery.Nodes = append(discovery.Nodes, other)
			fixtures := []string{}
			current := fixtureSampleInventory()
			want := "mounted"
			switch mode {
			case "mounted":
				discovery.MountedNumbers = []string{"8:0"}
			case "boot":
				discovery.BootBackingNumbers = []string{"8:0"}
			case "mounted_child":
				child := fixtureSampleDiscovery().Nodes[0]
				child.ID, child.Number, child.Kind, child.Parent = "block:ap1", "8:1", "partition", "block:a"
				discovery.Nodes[0].PhysicalChildren = []string{"block:ap1"}
				discovery.Nodes = []storagetest.Node{discovery.Nodes[0], child, other}
				discovery.MountedNumbers = []string{"8:1"}
			case "reserved_fixture":
				fixtures = []string{"block:a"}
				current.Components = []inventory.Component{{ID: "block:a", Type: inventory.ComponentBlock, Attributes: []inventory.Attribute{{Name: "device_number", Value: "8:0"}, {Name: "model", Value: "ignored fixture attribute"}}}}
				want = "fixture"
			case "usb":
				discovery.Nodes[0].Transport = "usb"
				want = "usb"
			case "removable":
				discovery.Nodes[0].Removable = true
				want = "removable"
			case "optical":
				discovery.Nodes[0].Transport = "optical"
				want = "optical"
			case "floppy":
				discovery.Nodes[0].Transport = "floppy"
				want = "floppy"
			case "unknown":
				discovery.Nodes[0].Transport = "unknown"
				want = "unsupported_transport"
			case "empty":
				discovery.Nodes[0].Geometry.CapacityBytes = 0
				want = "empty_media"
			case "geometry":
				discovery.Nodes[0].Geometry.CapacityBytes = 513
				want = "unsupported_geometry"
			}
			projection, err := prepareStandaloneSampleProjection(context.Background(), discovery, fixtures, current)
			if err != nil || len(projection.targets) != 1 || projection.targets[0].ID != "block:b" || projection.devices[0].Exclusion != want || projection.devices[0].Status != "excluded" {
				t.Fatalf("family exclusion or unrelated target lost: %+v,%v", projection, err)
			}
			outcome := fixtureSampleResultOutcome()
			outcome.Device.Interface, outcome.Device.Observation.Interface = "b", "b"
			outcome.Device.Observation.Identity.Minor = 16
			result, err := finishStandaloneSample(&projection, fixtureSampleCompletion(), []sampleParentOutcome{outcome})
			wantStatus := "inconclusive"
			if mode == "reserved_fixture" {
				wantStatus = "pass"
			}
			if err != nil || result.Status != wantStatus || result.AcceptedReadBytes != 1024 || result.Devices[0].Observation != nil {
				t.Fatalf("excluded or hidden fixture gained read credit: %+v,%v", result, err)
			}
			if mode == "mounted_child" && (len(result.Devices) != 3 || result.Devices[1].Exclusion != "partition" || result.Devices[1].Requested) {
				t.Fatal("partition projection lost")
			}
			if mode == "reserved_fixture" && (result.Devices[0].Requested || len(result.Snapshot.FixtureObservations) != 1 || len(result.FixtureIDs) != 1) {
				t.Fatal("fixture safety projection lost")
			}
		})
	}
}

func TestStorageSampleResultAcceptedAndDiagnosticTotals(t *testing.T) {
	for _, mode := range []string{"pass", "accepted_mismatch", "diagnostic_mismatch", "raw_pass_cancelled", "cleanup_after_pass", "cleanup_after_mismatch", "final_cancelled"} {
		t.Run(mode, func(t *testing.T) {
			projection := fixtureSampleProjection(t)
			outcome := fixtureSampleResultOutcome()
			completion := fixtureSampleCompletion()
			wantStatus, wantError, wantReason := "pass", "none", "completed"
			wantReads, wantCompared, wantMatched := uint64(1024), uint64(512), uint32(1)
			failure, diagnostic, uncertain := false, false, false
			switch mode {
			case "accepted_mismatch":
				makeSampleMismatch(&outcome)
				wantStatus, wantError = "fail", "mismatch"
				wantCompared, wantMatched, failure = 0, 0, true
			case "diagnostic_mismatch":
				makeSampleMismatch(&outcome)
				outcome.Device.Accepted = false
				outcome.Error = "cancelled"
				wantStatus, wantError, wantReason = "aborted", "cancelled", "operator_cancelled"
				wantReads, wantCompared, wantMatched, diagnostic = 0, 0, 0, true
			case "raw_pass_cancelled":
				outcome.Device.Accepted = false
				outcome.Device.Status, outcome.Device.Error = "aborted", "cancelled"
				outcome.Error = "cancelled"
				wantStatus, wantError, wantReason = "aborted", "cancelled", "operator_cancelled"
				wantReads, wantCompared, wantMatched = 0, 0, 0
			case "cleanup_after_pass":
				completion.cleanupConfirmed = false
				wantStatus, wantError, wantReason = "error", "cleanup_unconfirmed", "cleanup_unconfirmed"
				uncertain = true
			case "cleanup_after_mismatch":
				makeSampleMismatch(&outcome)
				completion.cleanupConfirmed = false
				wantStatus, wantError, wantReason = "error", "cleanup_unconfirmed", "cleanup_unconfirmed"
				uncertain = true
				wantCompared, wantMatched, failure = 0, 0, true
			case "final_cancelled":
				completion.reason = "cancelled"
				wantStatus, wantError, wantReason = "aborted", "cancelled", "operator_cancelled"
			}
			result, err := finishStandaloneSample(&projection, completion, []sampleParentOutcome{outcome})
			if (err != nil) != uncertain || uncertain && !errors.Is(err, workerexec.ErrCleanupUncertain) ||
				result.Status != wantStatus || result.Error != wantError || result.StopReason != wantReason ||
				result.AcceptedReadBytes != wantReads || result.AcceptedComparedBytes != wantCompared || result.AcceptedMatchedRanges != wantMatched ||
				result.FailureObserved != failure || result.DiagnosticFailure != diagnostic {
				t.Fatalf("independent totals/precedence changed: %+v,%v", result, err)
			}
			if uncertain {
				if _, err := storageevidence.CanonicalTypedBytes(result); !errors.Is(err, workerexec.ErrCleanupUncertain) {
					t.Fatal("uncertain result became ordinary evidence")
				}
			}
		})
	}
}

func TestStorageSampleResultZeroWorkAndEarlyStops(t *testing.T) {
	for _, mode := range []string{"diskless", "all_excluded", "pre_discovery_error", "unsupported", "cancelled", "deadline", "cleanup"} {
		t.Run(mode, func(t *testing.T) {
			completion := fixtureSampleCompletion()
			var projection *sampleProjection
			wantStatus, wantError := "error", "discovery_failed"
			switch mode {
			case "diskless", "all_excluded":
				discovery := fixtureSampleDiscovery()
				if mode == "diskless" {
					discovery.Nodes = []storagetest.Node{}
				} else {
					discovery.MountedNumbers = []string{"8:0"}
				}
				value, err := prepareStandaloneSampleProjection(context.Background(), discovery, []string{}, fixtureSampleInventory())
				if err != nil {
					t.Fatal(err)
				}
				projection = &value
				wantStatus, wantError = "inconclusive", "none"
			case "pre_discovery_error":
				completion.reason = "discovery_failed"
			case "unsupported":
				completion.reason = "unsupported_platform"
				wantError = "unsupported_platform"
			case "cancelled":
				completion.reason = "cancelled"
				wantStatus, wantError = "aborted", "cancelled"
			case "deadline":
				completion.reason = "deadline"
				wantError = "deadline"
			case "cleanup":
				completion.reason = "cleanup_unconfirmed"
				completion.cleanupConfirmed = false
				wantError = "cleanup_unconfirmed"
			}
			result, err := finishStandaloneSample(projection, completion, []sampleParentOutcome{})
			if mode == "cleanup" {
				if !errors.Is(err, workerexec.ErrCleanupUncertain) {
					t.Fatal("cleanup uncertainty lost")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if result.Status != wantStatus || result.Error != wantError || result.Status == "pass" || result.AcceptedReadBytes != 0 {
				t.Fatal("zero work became a pass")
			}
		})
	}
}

func TestStorageSampleResultRejectsForgedAccountingAndLifecycle(t *testing.T) {
	for _, mode := range []string{"summary", "identity", "range", "counter", "device", "not_requested", "unclosed", "unbound", "late", "negative_time", "unknown_reason", "nil_outcomes", "extra_outcome", "missing_outcome", "cleanup_flag"} {
		t.Run(mode, func(t *testing.T) {
			projection := fixtureSampleProjection(t)
			outcome := fixtureSampleResultOutcome()
			completion := fixtureSampleCompletion()
			outcomes := []sampleParentOutcome{outcome}
			switch mode {
			case "summary":
				outcomes[0].Summary.ReadBytes++
			case "identity":
				outcomes[0].Device.Observation.Identity.Minor++
			case "range":
				outcomes[0].Device.Observation.Ranges[0].Offset++
			case "counter":
				outcomes[0].Device.Observation.ReadBytes++
			case "device":
				outcomes[0].Device.Interface = "different"
			case "not_requested":
				outcomes[0].Device.Requested = false
			case "unclosed":
				outcomes[0].Device.CloseConfirmed = false
			case "unbound":
				outcomes[0].Device.FinalBindingConfirmed = false
			case "late":
				outcomes[0].Device.FinishedMilliseconds = 1001
				completion.durationMilliseconds = 1001
			case "negative_time":
				outcomes[0].Device.StartedMilliseconds = -1
			case "unknown_reason":
				completion.reason = "arbitrary diagnostic text"
			case "nil_outcomes":
				outcomes = nil
			case "extra_outcome":
				outcomes = append(outcomes, fixtureSampleResultOutcome())
			case "missing_outcome":
				outcomes = []sampleParentOutcome{}
			case "cleanup_flag":
				outcomes[0].Error = "cleanup_unconfirmed"
			}
			result, err := finishStandaloneSample(&projection, completion, outcomes)
			if !errors.Is(err, errSampleProtocol) || !reflect.DeepEqual(result, storagetest.Result{}) {
				t.Fatalf("forged completion admitted: %+v,%v", result, err)
			}
		})
	}
}

func TestStorageSampleResultStopsAfterAcceptedFailure(t *testing.T) {
	discovery := fixtureSampleDiscovery()
	second := fixtureSampleDiscovery().Nodes[0]
	second.ID, second.Number = "block:b", "8:16"
	discovery.Nodes = append(discovery.Nodes, second)
	projection, err := prepareStandaloneSampleProjection(context.Background(), discovery, []string{}, fixtureSampleInventory())
	if err != nil {
		t.Fatal(err)
	}
	first := fixtureSampleResultOutcome()
	makeSampleMismatch(&first)
	result, err := finishStandaloneSample(&projection, fixtureSampleCompletion(), []sampleParentOutcome{first})
	if err != nil || result.Status != "fail" || result.Error != "mismatch" || result.StopReason != "error" || result.AcceptedReadBytes != 1024 || result.Devices[1].Status != "not_started" {
		t.Fatalf("accepted failure or unsampled suffix changed: %+v,%v", result, err)
	}
	next := fixtureSampleResultOutcome()
	next.Device.Interface, next.Device.Observation.Interface = "b", "b"
	next.Device.Observation.Identity.Minor = 16
	if _, err := finishStandaloneSample(&projection, fixtureSampleCompletion(), []sampleParentOutcome{first, next}); !errors.Is(err, errSampleProtocol) {
		t.Fatal("sampling continued after first fault")
	}
}

func TestStorageSampleProjectionRejectsIncompleteMetadata(t *testing.T) {
	for _, mode := range []string{"incomplete", "backing", "swap", "nil_nodes", "duplicate", "missing_geometry", "unknown_mount", "unknown_boot", "missing_fixture", "nil_fixtures", "nil_context", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			discovery := fixtureSampleDiscovery()
			fixtures := []string{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var input context.Context = ctx
			switch mode {
			case "incomplete":
				discovery.Complete = false
			case "backing":
				discovery.BackingComplete = false
			case "swap":
				discovery.SwapState = "active"
			case "nil_nodes":
				discovery.Nodes = nil
			case "duplicate":
				discovery.Nodes = append(discovery.Nodes, discovery.Nodes[0])
			case "missing_geometry":
				discovery.Nodes[0].Geometry = nil
			case "unknown_mount":
				discovery.MountedNumbers = []string{"8:99"}
			case "unknown_boot":
				discovery.BootBackingNumbers = []string{"8:99"}
			case "missing_fixture":
				fixtures = []string{"block:a"}
			case "nil_fixtures":
				fixtures = nil
			case "nil_context":
				input = nil
			case "cancelled":
				cancel()
			}
			value, err := prepareStandaloneSampleProjection(input, discovery, fixtures, fixtureSampleInventory())
			if !errors.Is(err, errSampleProtocol) || !reflect.DeepEqual(value, sampleProjection{}) {
				t.Fatal("incomplete metadata produced a target projection")
			}
		})
	}
}

type fixtureSampleProjectionContext struct {
	context.Context
	check func() error
}

func (input *fixtureSampleProjectionContext) Err() error { return input.check() }

func TestStorageSampleProjectionContextCannotMutateFrozenFacts(t *testing.T) {
	for _, mode := range []string{"partition_nil_geometry", "transport_and_geometry", "mounted_numbers", "fixture_inventory", "fixture_id_slice", "partition_relations", "cancelled_after_freeze"} {
		t.Run(mode, func(t *testing.T) {
			discovery := fixtureSampleDiscovery()
			other := fixtureSampleDiscovery().Nodes[0]
			other.ID, other.Number = "block:b", "8:16"
			discovery.Nodes = append(discovery.Nodes, other)
			discovery.MountedNumbers = []string{"8:16"}
			fixtures := []string{}
			current := fixtureSampleInventory()
			fixtureCase := mode == "fixture_inventory" || mode == "fixture_id_slice"
			if fixtureCase {
				fixtures = []string{"block:b"}
				current.Components = []inventory.Component{{ID: "block:b", Type: inventory.ComponentBlock,
					Attributes: []inventory.Attribute{{Name: "device_number", Value: "8:16"}}}}
			}
			if mode == "partition_relations" {
				child := fixtureSampleDiscovery().Nodes[0]
				child.ID, child.Number, child.Kind, child.Parent = "block:ap1", "8:1", "partition", "block:a"
				discovery.Nodes[0].PhysicalChildren = []string{"block:ap1"}
				discovery.Nodes = []storagetest.Node{discovery.Nodes[0], child, other}
			}
			checks := 0
			input := &fixtureSampleProjectionContext{Context: context.Background(), check: func() error {
				checks++
				switch mode {
				case "partition_nil_geometry", "cancelled_after_freeze":
					discovery.Nodes[0].Kind, discovery.Nodes[0].Geometry = "partition", nil
				case "transport_and_geometry":
					discovery.Nodes[0].Transport = "usb"
					discovery.Nodes[0].Geometry.CapacityBytes = 4096
				case "mounted_numbers":
					discovery.MountedNumbers[0] = "8:0"
				case "fixture_inventory":
					current.Components[0].Type = inventory.ComponentUSB
					current.Components[0].Attributes[0].Value = "8:0"
				case "fixture_id_slice":
					fixtures[0] = "block:a"
				case "partition_relations":
					discovery.Nodes[0].PhysicalChildren[0] = "block:missing"
					discovery.Nodes[1].Parent = "block:missing"
				}
				if mode == "cancelled_after_freeze" {
					return context.Canceled
				}
				return nil
			}}
			projection, err := prepareStandaloneSampleProjection(input, discovery, fixtures, current)
			if checks == 0 {
				t.Fatal("caller cancellation was not observed after freezing metadata")
			}
			if mode == "cancelled_after_freeze" {
				if !errors.Is(err, errSampleProtocol) || !reflect.DeepEqual(projection, sampleProjection{}) {
					t.Fatal("observed cancellation returned a projection")
				}
				return
			}
			if err != nil || len(projection.targets) != 1 || projection.targets[0].ID != "block:a" ||
				projection.targets[0].Number != "8:0" || projection.targets[0].CapacityBytes != 512 ||
				projection.targets[0].LogicalBlockBytes != 512 || projection.targets[0].FixtureMask != 0 ||
				!reflect.DeepEqual(projection.targets[0].Ranges, []storagetest.Range{{Offset: 0, Length: 512}}) {
				t.Fatal("context callback changed the original admitted schedule")
			}
			node := projection.snapshot.Discovery.Nodes[0]
			if node.Kind != "physical" || node.Transport != "ata" || node.Geometry == nil ||
				node.Geometry.CapacityBytes != 512 || node.Geometry.LogicalBlockBytes != 512 ||
				!reflect.DeepEqual(projection.snapshot.Discovery.MountedNumbers, []string{"8:16"}) ||
				projection.devices[0].Eligibility != "eligible" || projection.devices[0].Exclusion != "none" {
				t.Fatal("context callback changed frozen discovery or eligibility")
			}
			if mode == "partition_relations" {
				if len(projection.devices) != 3 || !reflect.DeepEqual(node.PhysicalChildren, []string{"block:ap1"}) ||
					projection.snapshot.Discovery.Nodes[1].Parent != "block:a" ||
					projection.devices[1].Requested || projection.devices[1].Exclusion != "partition" {
					t.Fatal("context callback changed the original partition relation")
				}
			}
			last := len(projection.devices) - 1
			if fixtureCase {
				want := []storagetest.FixtureObservation{{ID: "block:b", Type: "block", DeviceNumber: "8:16"}}
				if !reflect.DeepEqual(projection.fixtures, []string{"block:b"}) ||
					!reflect.DeepEqual(projection.snapshot.FixtureObservations, want) ||
					projection.snapshot.ResolvedFixtureMask != 1 ||
					projection.snapshot.NodeFixtureMasks[last] != 1 ||
					projection.devices[last].Requested || projection.devices[last].Exclusion != "fixture" {
					t.Fatal("context callback changed original fixture authority")
				}
			} else if len(projection.fixtures) != 0 || len(projection.snapshot.FixtureObservations) != 0 ||
				projection.snapshot.ResolvedFixtureMask != 0 || projection.devices[last].Exclusion != "mounted" {
				t.Fatal("context callback changed original mount exclusion")
			}
		})
	}
}
