package storageevidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

type derivedDevice struct {
	target    storagetest.Target
	requested bool
	exclusion string
}

func digest(value string, prefix bool) bool {
	if prefix {
		if !strings.HasPrefix(value, "sha256:") {
			return false
		}
		value = strings.TrimPrefix(value, "sha256:")
	}
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func blockID(value string) bool {
	if !strings.HasPrefix(value, "block:") || len(value) < 7 || len(value) > 70 {
		return false
	}
	name := strings.TrimPrefix(value, "block:")
	if name == "." || name == ".." {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c)) {
			return false
		}
	}
	return true
}
func sortedIDs(values []string, limit int, valid func(string) bool) bool {
	if values == nil || len(values) > limit {
		return false
	}
	for i, value := range values {
		if !valid(value) || i > 0 && values[i-1] >= value {
			return false
		}
	}
	return true
}

func projection(result storagetest.Result) ([]derivedDevice, error) {
	if result.Snapshot == nil {
		if result.SnapshotSHA256 != "" || result.DiscoveryAccepted || len(result.Devices) != 0 || len(result.MissingComponents) != 0 {
			return nil, ErrInvalid
		}
		return []derivedDevice{}, nil
	}
	snapshot := result.Snapshot
	if snapshot.FixtureObservations == nil || snapshot.NodeFixtureMasks == nil || len(snapshot.FixtureObservations) != len(result.FixtureIDs) || snapshot.Discovery.Nodes == nil || len(snapshot.Discovery.Nodes) > storagetest.MaximumDevices || len(snapshot.NodeFixtureMasks) != len(snapshot.Discovery.Nodes) {
		return nil, ErrInvalid
	}
	components := make([]inventory.Component, 0, len(snapshot.FixtureObservations))
	for i, observed := range snapshot.FixtureObservations {
		if observed.ID != result.FixtureIDs[i] {
			return nil, ErrInvalid
		}
		row := inventory.Component{Type: inventory.ComponentType(observed.Type), ID: observed.ID, Attributes: []inventory.Attribute{}}
		if strings.HasPrefix(observed.ID, "block:") {
			row.Attributes = append(row.Attributes, inventory.Attribute{Name: "device_number", Value: observed.DeviceNumber})
		} else if observed.DeviceNumber != "" {
			return nil, ErrInvalid
		}
		components = append(components, row)
	}
	all := []string{}
	nodes := snapshot.Discovery.Nodes
	present := map[string]bool{}
	for _, node := range nodes {
		present[node.ID] = true
		if node.Kind != "partition" {
			all = append(all, node.ID)
		}
	}
	admission, err := storagetest.Admit(context.Background(), snapshot.Discovery, all, result.FixtureIDs, inventory.Snapshot{SchemaVersion: inventory.SnapshotSchemaVersion, Components: components})
	if err != nil || !reflect.DeepEqual(admission.FixtureObservations, snapshot.FixtureObservations) || admission.ResolvedFixtureMask != snapshot.ResolvedFixtureMask || !reflect.DeepEqual(admission.NodeFixtureMasks, []uint8(snapshot.NodeFixtureMasks)) {
		return nil, ErrInvalid
	}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) > 256<<10 {
		return nil, ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if result.SnapshotSHA256 != hex.EncodeToString(sum[:]) {
		return nil, ErrInvalid
	}
	missing := []string{}
	wanted := map[string]bool{}
	for _, id := range result.ComponentIDs {
		if !present[id] {
			missing = append(missing, id)
		} else {
			wanted[id] = true
		}
	}
	if !reflect.DeepEqual(missing, result.MissingComponents) || len(missing) > 0 && result.DiscoveryAccepted {
		return nil, ErrInvalid
	}
	byID := map[string]storagetest.Target{}
	for _, target := range admission.Targets {
		byID[target.ID] = target
	}
	derived := make([]derivedDevice, 0, len(nodes))
	for i, node := range nodes {
		target := byID[node.ID]
		if node.Kind == "partition" {
			if wanted[node.ID] {
				return nil, ErrInvalid
			}
			target = storagetest.Target{ID: node.ID, Number: node.Number, CapacityBytes: node.Geometry.CapacityBytes, LogicalBlockBytes: node.Geometry.LogicalBlockBytes, Exclusions: []string{}, Ranges: []storagetest.Range{}, FixtureMask: snapshot.NodeFixtureMasks[i]}
		}
		requested := wanted[node.ID]
		if result.Scope == "standalone" {
			requested = node.Kind != "partition" && target.FixtureMask == 0
		}
		reasons := map[string]bool{}
		for _, reason := range target.Exclusions {
			reasons[reason] = true
		}
		exclusion := "none"
		switch {
		case target.FixtureMask != 0:
			exclusion = "fixture"
		case node.Kind == "partition":
			exclusion = "partition"
		case !requested:
			exclusion = "outside_plan"
		case reasons["usb"]:
			exclusion = "usb"
		case reasons["removable"]:
			exclusion = "removable"
		case reasons["optical"]:
			exclusion = "optical"
		case reasons["floppy"]:
			exclusion = "floppy"
		case reasons["stacked"] || reasons["loop"]:
			exclusion = "mapped"
		case reasons["mounted"] || reasons["boot"]:
			exclusion = "mounted"
		case reasons["transport_unknown"]:
			exclusion = "unsupported_transport"
		case reasons["empty_media"]:
			exclusion = "empty_media"
		case reasons["unsupported_geometry"]:
			exclusion = "unsupported_geometry"
		}
		derived = append(derived, derivedDevice{target, requested, exclusion})
	}
	return derived, nil
}
