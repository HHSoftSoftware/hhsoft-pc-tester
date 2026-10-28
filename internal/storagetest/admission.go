package storagetest

import (
	"context"
	"sort"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

// Admit validates every node before deriving whole-family exclusions. Nil
// requested IDs selects whole nodes outside reserved fixture families; an empty slice explicitly selects none.
// Empty coverage never implies a successful sampled-read result.
func Admit(ctx context.Context, discovery Discovery, requested, fixtures []string, current inventory.Snapshot) (Admission, error) {
	if ctx == nil {
		return Admission{}, ErrAdmission
	}
	if err := ctx.Err(); err != nil {
		return Admission{}, err
	}
	if len(requested) > MaximumDevices || fixtures == nil || len(fixtures) > MaximumFixtures {
		return Admission{}, ErrAdmission
	}
	graph, err := validateNodes(discovery)
	if err != nil || graph.connect(discovery.Nodes) != nil {
		return Admission{}, ErrAdmission
	}
	for _, relation := range []struct {
		values []string
		reason string
	}{{discovery.MountedNumbers, "mounted"}, {discovery.BootBackingNumbers, "boot"}} {
		for _, number := range relation.values {
			if graph.memory[number] != "" {
				continue
			}
			index, ok := graph.numbers[number]
			if !ok || discovery.Nodes[index].LoopState == "unbound" {
				return Admission{}, ErrAdmission
			}
			graph.reasons[index][relation.reason] = true
		}
	}
	fixtureCopy := append([]string{}, fixtures...)
	sort.Strings(fixtureCopy)
	if !sortedUnique(fixtureCopy, MaximumFixtures, fixtureID) {
		return Admission{}, ErrAdmission
	}
	fixtureObservations, err := resolveFixtures(ctx, current, fixtureCopy, discovery.Nodes, graph)
	if err != nil {
		return Admission{}, err
	}
	for bit, fixture := range fixtureCopy {
		for i, node := range discovery.Nodes {
			if node.ID == fixture || contains(node.USBAncestors, fixture) {
				graph.reasons[i]["fixture"] = true
				graph.fixtureMasks[i] |= uint8(1 << bit)
			}
		}
	}
	graph.excludeFamilies()
	wanted := append([]string{}, requested...)
	if requested == nil {
		for i, node := range discovery.Nodes {
			if node.Kind != "partition" && graph.fixtureMasks[i] == 0 {
				wanted = append(wanted, node.ID)
			}
		}
	}
	sort.Strings(wanted)
	if !sortedUnique(wanted, MaximumDevices, func(s string) bool { return componentID(s, "block:") }) {
		return Admission{}, ErrAdmission
	}
	result := Admission{RequestedIDs: wanted, FixtureIDs: fixtureCopy, FixtureObservations: fixtureObservations, ResolvedFixtureMask: uint8((uint16(1) << len(fixtureCopy)) - 1), NodeFixtureMasks: append([]uint8{}, graph.fixtureMasks...), Targets: []Target{}}
	for _, id := range wanted {
		i, ok := graph.ids[id]
		if !ok || discovery.Nodes[i].Kind == "partition" {
			return Admission{}, ErrAdmission
		}
		node := discovery.Nodes[i]
		target := Target{ID: id, Number: node.Number, CapacityBytes: node.Geometry.CapacityBytes, LogicalBlockBytes: node.Geometry.LogicalBlockBytes, Exclusions: []string{}, Ranges: []Range{}, FixtureMask: graph.fixtureMasks[i]}
		for reason := range graph.reasons[i] {
			target.Exclusions = append(target.Exclusions, reason)
		}
		sort.Strings(target.Exclusions)
		if len(target.Exclusions) == 0 {
			target.Ranges, err = SampleRanges(node.Geometry.CapacityBytes, node.Geometry.LogicalBlockBytes)
			if err != nil {
				return Admission{}, ErrAdmission
			}
		}
		result.Targets = append(result.Targets, target)
	}
	if err := ctx.Err(); err != nil {
		return Admission{}, err
	}
	return result, nil
}
