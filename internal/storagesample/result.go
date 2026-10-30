package storagesample

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storageevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/workerexec"
)

// A projection is a frozen metadata description, not authority to open media.
// Live acquisition, continuous backing ownership and final checks remain the
// responsibility of the producer that supplies these facts and parent outcomes.
type sampleProjection struct {
	snapshot storagetest.Snapshot
	digest   string
	fixtures []string
	targets  []storagetest.Target
	devices  []storagetest.DeviceResult
}

func prepareStandaloneSampleProjection(ctx context.Context, discovery storagetest.Discovery, fixtures []string, current inventory.Snapshot) (sampleProjection, error) {
	if nilSampleValue(ctx) || len(discovery.Nodes) > storagetest.MaximumDevices || fixtures == nil || len(fixtures) > storagetest.MaximumFixtures {
		return sampleProjection{}, errSampleProtocol
	}
	all := []string{}
	for _, node := range discovery.Nodes {
		if node.Kind != "partition" {
			all = append(all, node.ID)
		}
	}
	// Admission is a pure metadata walk. Use an owned context so it cannot
	// call into the caller while discovery and fixture inputs are borrowed.
	// Observe caller cancellation only after every derived fact is frozen.
	admission, err := storagetest.Admit(context.Background(), discovery, all, fixtures, current)
	if err != nil {
		return sampleProjection{}, errSampleProtocol
	}
	snapshot := storagetest.Snapshot{
		Discovery: discovery, FixtureObservations: admission.FixtureObservations,
		ResolvedFixtureMask: admission.ResolvedFixtureMask, NodeFixtureMasks: admission.NodeFixtureMasks,
	}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) > 256<<10 {
		return sampleProjection{}, errSampleProtocol
	}
	// Copy all nested nodes, geometry and relations before any later callback.
	var frozen storagetest.Snapshot
	if json.Unmarshal(raw, &frozen) != nil {
		return sampleProjection{}, errSampleProtocol
	}
	snapshot = frozen
	sum := sha256.Sum256(raw)
	projection := sampleProjection{
		snapshot: snapshot, digest: hex.EncodeToString(sum[:]),
		fixtures: append([]string{}, admission.FixtureIDs...),
		targets:  []storagetest.Target{}, devices: []storagetest.DeviceResult{},
	}
	byID := map[string]storagetest.Target{}
	for _, target := range admission.Targets {
		byID[target.ID] = target
	}
	for index, node := range snapshot.Discovery.Nodes {
		target := byID[node.ID]
		if node.Kind == "partition" {
			target = storagetest.Target{ID: node.ID, Number: node.Number,
				CapacityBytes: node.Geometry.CapacityBytes, LogicalBlockBytes: node.Geometry.LogicalBlockBytes,
				Exclusions: []string{}, Ranges: []storagetest.Range{}, FixtureMask: snapshot.NodeFixtureMasks[index]}
		}
		requested := node.Kind != "partition" && target.FixtureMask == 0
		exclusion := standaloneSampleExclusion(node, target)
		device := storagetest.DeviceResult{Interface: strings.TrimPrefix(node.ID, "block:"),
			Requested: requested, Eligibility: "excluded", Exclusion: exclusion,
			Status: "excluded", Error: "none", CloseConfirmed: true}
		if exclusion == "none" {
			if !requested {
				return sampleProjection{}, errSampleProtocol
			}
			if _, err := prepareSampleObservation(target); err != nil {
				return sampleProjection{}, err
			}
			device.Eligibility, device.Status = "eligible", "not_started"
			projection.targets = append(projection.targets, copyAdmittedSampleTarget(target))
		}
		projection.devices = append(projection.devices, device)
	}
	if ctx.Err() != nil {
		return sampleProjection{}, errSampleProtocol
	}
	return projection, nil
}

func standaloneSampleExclusion(node storagetest.Node, target storagetest.Target) string {
	if target.FixtureMask != 0 {
		return "fixture"
	}
	if node.Kind == "partition" {
		return "partition"
	}
	reasons := map[string]bool{}
	for _, reason := range target.Exclusions {
		reasons[reason] = true
	}
	switch {
	case reasons["usb"]:
		return "usb"
	case reasons["removable"]:
		return "removable"
	case reasons["optical"]:
		return "optical"
	case reasons["floppy"]:
		return "floppy"
	case reasons["stacked"] || reasons["loop"]:
		return "mapped"
	case reasons["mounted"] || reasons["boot"]:
		return "mounted"
	case reasons["transport_unknown"]:
		return "unsupported_transport"
	case reasons["empty_media"]:
		return "empty_media"
	case reasons["unsupported_geometry"]:
		return "unsupported_geometry"
	default:
		return "none"
	}
}

// These elapsed values must come from the run-wide clock and final ownership
// ledger. This pure assembler checks their feasibility, not their provenance.
type sampleRunCompletion struct {
	limitMilliseconds    int64
	budgetMilliseconds   int64
	durationMilliseconds int64
	reason               string
	cleanupConfirmed     bool
}

func sampleCompletionReason(reason string) bool {
	switch reason {
	case "none", "unsupported_platform", "discovery_failed", "registry_invalid",
		"topology_invalid", "metadata_limit", "safety_view_invalid", "swap_active",
		"fixture_changed", "identity_changed", "safety_changed", "target_busy",
		"open_error", "close_error", "protocol_error", "spawn_error", "output_error",
		"clock_invalid", "deadline", "cancelled", "cleanup_unconfirmed":
		return true
	}
	return false
}

// finishStandaloneSample retains raw observations but independently recomputes
// all accepted totals. It rejects continuation after a target failure or stop.
// No metadata-only projection or unaccepted child observation earns read credit.
func finishStandaloneSample(projection *sampleProjection, completion sampleRunCompletion, outcomes []sampleParentOutcome) (storagetest.Result, error) {
	if completion.limitMilliseconds < 1 || completion.limitMilliseconds > 180000 ||
		completion.budgetMilliseconds < 0 || completion.budgetMilliseconds > completion.limitMilliseconds ||
		completion.durationMilliseconds < 0 || completion.durationMilliseconds > 3600000 ||
		!sampleCompletionReason(completion.reason) || outcomes == nil {
		return storagetest.Result{}, errSampleProtocol
	}
	if completion.reason == "cleanup_unconfirmed" && completion.cleanupConfirmed {
		return storagetest.Result{}, errSampleProtocol
	}
	result := storagetest.Result{Schema: storagetest.Schema, Policy: storagetest.Policy, Scope: "standalone",
		Status: "error", StopReason: "error", Error: "protocol_error",
		LimitMilliseconds: completion.limitMilliseconds, BudgetMilliseconds: completion.budgetMilliseconds,
		DurationMilliseconds: completion.durationMilliseconds, CleanupConfirmed: completion.cleanupConfirmed,
		ComponentIDs: []string{}, FixtureIDs: []string{}, MissingComponents: []string{}, Devices: []storagetest.DeviceResult{}}
	if projection == nil {
		if len(outcomes) != 0 || completion.reason == "none" {
			return storagetest.Result{}, errSampleProtocol
		}
	} else {
		result.DiscoveryAccepted = true
		result.Snapshot, result.SnapshotSHA256 = &projection.snapshot, projection.digest
		result.FixtureIDs = append([]string{}, projection.fixtures...)
		result.Devices = append([]storagetest.DeviceResult{}, projection.devices...)
	}
	reason, hardware := "none", ""
	terminal := false
	next := 0
	for index := range result.Devices {
		if result.Devices[index].Eligibility != "eligible" {
			continue
		}
		if next == len(outcomes) {
			continue
		}
		if terminal || next >= len(projection.targets) {
			return storagetest.Result{}, errSampleProtocol
		}
		outcome := outcomes[next]
		target := projection.targets[next]
		if !sampleCompletionReason(outcome.Error) || outcome.Error == "cleanup_unconfirmed" && outcome.CleanupConfirmed {
			return storagetest.Result{}, errSampleProtocol
		}
		if outcome.Device.Interface != result.Devices[index].Interface ||
			!outcome.Device.Requested || outcome.Device.Eligibility != "eligible" || outcome.Device.Exclusion != "none" {
			return storagetest.Result{}, errSampleProtocol
		}
		summary := storagetest.ObservationSummary{}
		if outcome.Device.Observation != nil {
			var err error
			summary, err = storagetest.ValidateObservation(target, *outcome.Device.Observation)
			if err != nil {
				return storagetest.Result{}, errSampleProtocol
			}
		}
		if !reflect.DeepEqual(summary, outcome.Summary) {
			return storagetest.Result{}, errSampleProtocol
		}
		result.Devices[index] = outcome.Device
		result.CleanupConfirmed = result.CleanupConfirmed && outcome.CleanupConfirmed
		if reason == "none" && outcome.Error != "none" {
			reason = outcome.Error
		}
		if outcome.Device.Accepted {
			if outcome.Error != "none" {
				return storagetest.Result{}, errSampleProtocol
			}
			result.AcceptedReadBytes += summary.ReadBytes
			result.AcceptedComparedBytes += summary.ComparedBytes
			result.AcceptedMatchedRanges += uint32(summary.MatchedRanges)
			if summary.FailureObserved {
				hardware = outcome.Device.Observation.Error
				result.FailureObserved, terminal = true, true
			}
		} else {
			result.DiagnosticFailure = result.DiagnosticFailure || summary.FailureObserved
			terminal = true
		}
		next++
	}
	if next != len(outcomes) {
		return storagetest.Result{}, errSampleProtocol
	}
	if reason == "none" {
		reason = completion.reason
	}
	remaining := projection != nil && next < len(projection.targets)
	if remaining && reason == "none" && hardware == "" {
		return storagetest.Result{}, errSampleProtocol
	}
	if !result.CleanupConfirmed {
		result.Status, result.StopReason, result.Error = "error", "cleanup_unconfirmed", "cleanup_unconfirmed"
	} else {
		switch reason {
		case "none":
			result.StopReason = "completed"
			if remaining {
				result.StopReason = "error"
			}
			result.Status, result.Error = "inconclusive", "none"
			requested, allPassed := 0, true
			for _, device := range result.Devices {
				if device.Requested {
					requested++
					allPassed = allPassed && device.Accepted && device.Status == "pass"
				}
			}
			if requested > 0 && allPassed && !terminal {
				result.Status = "pass"
			}
		case "cancelled":
			result.Status, result.StopReason, result.Error = "aborted", "operator_cancelled", "cancelled"
		case "deadline":
			result.Status, result.StopReason, result.Error = "error", "deadline", "deadline"
		default:
			result.Status, result.StopReason, result.Error = "error", "error", reason
		}
		if hardware != "" {
			result.Status, result.Error = "fail", hardware
		}
	}
	// The independent reader rederives the complete graph, exact public device
	// projection, lifecycle feasibility, diagnostic precedence and all counters.
	raw, err := json.Marshal(result)
	if err != nil || len(raw)+1 > storagetest.MaximumResultBytes {
		return storagetest.Result{}, errSampleProtocol
	}
	checked, err := storageevidence.Decode(raw)
	if err != nil && !errors.Is(err, workerexec.ErrCleanupUncertain) {
		return storagetest.Result{}, errSampleProtocol
	}
	return checked, err
}
